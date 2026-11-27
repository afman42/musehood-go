package musehood

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Deteksi model lokal Ollama (fitur 32): GET <baseUrl>/api/tags untuk
// daftar model, lalu tambah sebagai provider berprotokol "ollama"
// (gateway sudah mendukung /api/chat Ollama). Demi keamanan SSRF, hanya
// host localhost dan IP privat LAN yang diizinkan untuk deteksi ini.

const defaultOllamaBaseURL = "http://127.0.0.1:11434"

// validateOllamaBaseURL memeriksa base URL Ollama: HTTP/HTTPS dengan
// host localhost, loopback, atau IP privat LAN (10/8, 172.16/12,
// 192.168/16).
func validateOllamaBaseURL(raw string) (string, error) {
	base := strings.TrimRight(strings.TrimSpace(raw), "/")
	if base == "" {
		base = defaultOllamaBaseURL
	}
	u, err := url.Parse(base)
	if err != nil || !validHTTPURL(u) || u.RawQuery != "" || u.ForceQuery || strings.Contains(base, "#") {
		return "", problem(400, "Base URL Ollama harus URL HTTP/HTTPS yang valid.")
	}
	host := u.Hostname()
	if !strings.EqualFold(host, "localhost") {
		ip := net.ParseIP(host)
		if ip == nil || (!ip.IsLoopback() && !ip.IsPrivate()) {
			return "", problem(400, "Deteksi Ollama hanya diizinkan untuk localhost dan IP privat LAN (10.*, 172.16–31.*, 192.168.*).")
		}
	}
	u.Host = strings.ToLower(u.Host)
	return strings.TrimRight(u.String(), "/"), nil
}

type ollamaModel struct {
	Name string `json:"name"`
	Size int64  `json:"size"`
}

// detectOllama mengambil daftar model dari endpoint /api/tags Ollama.
func (a *App) detectOllama(ctx context.Context, baseURL string) ([]ollamaModel, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/api/tags", nil)
	if err != nil {
		return nil, problem(400, "Base URL Ollama tidak valid.")
	}
	resp, err := a.client.Do(req)
	if err != nil {
		return nil, problem(502, "Tidak dapat menghubungi Ollama di "+baseURL+". Pastikan Ollama berjalan.")
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, problem(502, "Respons Ollama tidak dapat dibaca.")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, problem(502, "Ollama membalas status "+resp.Status+".")
	}
	var payload struct {
		Models []ollamaModel `json:"models"`
	}
	if json.Unmarshal(raw, &payload) != nil {
		return nil, problem(502, "Respons daftar model Ollama bukan JSON yang valid.")
	}
	models := []ollamaModel{}
	for _, m := range payload.Models {
		if strings.TrimSpace(m.Name) != "" {
			models = append(models, m)
		}
	}
	return models, nil
}

// addOllamaProviderLocked menambahkan provider Ollama untuk baseURL
// (atau mengembalikan yang sudah ada). Pemanggil wajib memegang a.mu.
func (a *App) addOllamaProviderLocked(baseURL, model string) (any, error) {
	base, err := validateOllamaBaseURL(baseURL)
	if err != nil {
		return nil, err
	}
	model = strings.TrimSpace(model)
	if model == "" || jsLength(model) > 200 || badSetting.MatchString(model) {
		return nil, problem(400, "Nama model Ollama tidak valid.")
	}
	for _, p := range a.data.Providers {
		if p.Protocol == "ollama" && p.BaseURL == base {
			return map[string]any{"provider": a.data.providerView(p), "model": model, "created": false}, nil
		}
	}
	host := base
	if u, err := url.Parse(base); err == nil && u.Host != "" {
		host = u.Host
	}
	name := "Ollama (" + host + ")"
	proto := "ollama"
	view, err := a.createProviderLocked(providerInput{Name: &name, BaseURL: &base, Protocol: &proto})
	if err != nil {
		return nil, err
	}
	return map[string]any{"provider": view, "model": model, "created": true}, nil
}
