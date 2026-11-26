package musehood

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

type protocol string

const (
	protoOpenAI    protocol = "openai"
	protoResponses protocol = "openai-responses"
	protoAnthropic protocol = "anthropic"
	protoGemini    protocol = "gemini"
	protoOllama    protocol = "ollama"
)

var validProtocols = map[string]bool{
	"":                 true,
	"openai":           true,
	"openai-responses": true,
	"anthropic":        true,
	"gemini":           true,
	"ollama":           true,
}

func protocolOf(cfg settings) protocol {
	switch cfg.GatewayProtocol {
	case "openai-responses":
		return protoResponses
	case "anthropic":
		return protoAnthropic
	case "gemini":
		return protoGemini
	case "ollama":
		return protoOllama
	default:
		return protoOpenAI
	}
}

// Base URL normalization. Providers based on OpenAI accept a base with or
// without a trailing /v1; Gemini uses /v1beta; Ollama mounts /api at the root.
func openAIBase(cfg settings) string {
	return strings.TrimSuffix(strings.TrimRight(cfg.BaseURL, "/"), "/v1")
}
func geminiBase(cfg settings) string {
	base := strings.TrimRight(cfg.BaseURL, "/")
	base = strings.TrimSuffix(base, "/v1beta")
	return strings.TrimSuffix(base, "/v1")
}
func ollamaBase(cfg settings) string { return strings.TrimRight(cfg.BaseURL, "/") }

func (a *App) requireGatewayConfig(cfg settings) error {
	if cfg.BaseURL == "" {
		return problem(400, "Base URL gateway belum diisi. Buka Pengaturan untuk menyimpan koneksi.")
	}
	if cfg.APIKey == "" && protocolOf(cfg) != protoOllama {
		return problem(400, "API key gateway belum diisi. Simpan melalui Pengaturan.")
	}
	return nil
}

// gatewayDo sends one request to the gateway and returns the raw body, keeping
// provider errors verbatim (status plus original body).
func (a *App) gatewayDo(ctx context.Context, verb, target string, body any, headers map[string]string) ([]byte, error) {
	var input io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		input = strings.NewReader(string(raw))
	}
	req, err := http.NewRequestWithContext(ctx, verb, target, input)
	if err != nil {
		return nil, problem(502, "Koneksi gateway gagal: "+err.Error())
	}
	req.Header.Set("Accept", "application/json, text/event-stream, text/plain")
	// WAF sebagian provider memblokir User-Agent bawaan Go (HTTP 567).
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36")
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := a.client.Do(req)
	if err != nil {
		return nil, problem(502, "Koneksi gateway gagal: "+err.Error())
	}
	defer res.Body.Close()
	raw, large, err := limitedRead(res.Body, gatewayLimit)
	if large {
		return nil, problem(502, "HTTP "+strconv.Itoa(res.StatusCode)+": Respons gateway melebihi 20 MB.")
	}
	if err != nil {
		return nil, problem(502, "HTTP "+strconv.Itoa(res.StatusCode)+": Gagal membaca respons gateway: "+err.Error())
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		status := res.StatusCode
		if status < 400 || status > 599 {
			status = 502
		}
		return nil, problem(status, "HTTP "+strconv.Itoa(res.StatusCode)+": "+string(raw))
	}
	return raw, nil
}

func splitSystem(history []gatewayMessage) (string, []gatewayMessage) {
	if len(history) > 0 && history[0].Role == "system" {
		return history[0].Content, history[1:]
	}
	return "", history
}

// protocolChat builds and sends the chat request in the shape each protocol
// expects, including how the per-seat system prompt is carried.
func (a *App) protocolChat(ctx context.Context, cfg settings, model string, history []gatewayMessage) ([]byte, error) {
	if err := a.requireGatewayConfig(cfg); err != nil {
		return nil, err
	}
	switch proto := protocolOf(cfg); proto {
	case protoAnthropic:
		system, msgs := splitSystem(history)
		target := openAIBase(cfg) + "/v1/messages"
		return a.gatewayDo(ctx, http.MethodPost, target, map[string]any{
			"model": model, "max_tokens": 1024, "system": system, "messages": msgs,
		}, map[string]string{"x-api-key": cfg.APIKey, "anthropic-version": "2023-06-01"})
	case protoGemini:
		system, msgs := splitSystem(history)
		contents := make([]map[string]any, 0, len(msgs))
		for _, m := range msgs {
			role := "model"
			if m.Role == "user" {
				role = "user"
			}
			contents = append(contents, map[string]any{"role": role, "parts": []map[string]any{{"text": m.Content}}})
		}
		target := geminiBase(cfg) + "/v1beta/models/" + url.PathEscape(model) + ":generateContent?key=" + url.QueryEscape(cfg.APIKey)
		return a.gatewayDo(ctx, http.MethodPost, target, map[string]any{
			"contents":          contents,
			"systemInstruction": map[string]any{"parts": []map[string]any{{"text": system}}},
		}, nil)
	case protoResponses:
		target := openAIBase(cfg) + "/v1/responses"
		return a.gatewayDo(ctx, http.MethodPost, target, map[string]any{"model": model, "input": history},
			map[string]string{"Authorization": "Bearer " + cfg.APIKey})
	case protoOllama:
		headers := map[string]string{}
		if cfg.APIKey != "" {
			headers["Authorization"] = "Bearer " + cfg.APIKey
		}
		target := ollamaBase(cfg) + "/api/chat"
		return a.gatewayDo(ctx, http.MethodPost, target, map[string]any{"model": model, "messages": history, "stream": false}, headers)
	default:
		target := openAIBase(cfg) + "/v1/chat/completions"
		return a.gatewayDo(ctx, http.MethodPost, target, map[string]any{"model": model, "messages": history, "stream": false},
			map[string]string{"Authorization": "Bearer " + cfg.APIKey})
	}
}

// protocolModels fetches the model list from the endpoint each protocol uses.
func (a *App) protocolModels(ctx context.Context, cfg settings) ([]string, error) {
	if err := a.requireGatewayConfig(cfg); err != nil {
		return nil, err
	}
	var raw []byte
	var err error
	parseNames := func(entries []any, key string) []string {
		models := []string{}
		for _, m := range entries {
			if s, ok := m.(string); ok {
				models = append(models, s)
				continue
			}
			name, _ := field(m, key).(string)
			if name == "" {
				continue
			}
			models = append(models, strings.TrimPrefix(name, "models/"))
		}
		return models
	}
	switch proto := protocolOf(cfg); proto {
	case protoGemini:
		target := geminiBase(cfg) + "/v1beta/models?key=" + url.QueryEscape(cfg.APIKey)
		if raw, err = a.gatewayDo(ctx, http.MethodGet, target, nil, nil); err != nil {
			return nil, err
		}
		var obj any
		if json.Unmarshal(raw, &obj) != nil {
			return nil, problem(502, "Respons daftar model bukan JSON yang valid.")
		}
		entries, ok := field(obj, "models").([]any)
		if !ok {
			return nil, problem(502, "Respons daftar model harus berisi array models.")
		}
		return parseNames(entries, "name"), nil
	case protoOllama:
		target := ollamaBase(cfg) + "/api/tags"
		if raw, err = a.gatewayDo(ctx, http.MethodGet, target, nil, nil); err != nil {
			return nil, err
		}
		var obj any
		if json.Unmarshal(raw, &obj) != nil {
			return nil, problem(502, "Respons daftar model bukan JSON yang valid.")
		}
		entries, ok := field(obj, "models").([]any)
		if !ok {
			return nil, problem(502, "Respons daftar model harus berisi array models.")
		}
		return parseNames(entries, "name"), nil
	default:
		headers := map[string]string{}
		if protocolOf(cfg) == protoAnthropic {
			headers["x-api-key"] = cfg.APIKey
			headers["anthropic-version"] = "2023-06-01"
		} else {
			headers["Authorization"] = "Bearer " + cfg.APIKey
		}
		target := openAIBase(cfg) + "/v1/models"
		if raw, err = a.gatewayDo(ctx, http.MethodGet, target, nil, headers); err != nil {
			return nil, err
		}
		var obj any
		if json.Unmarshal(raw, &obj) != nil {
			return nil, problem(502, "Respons daftar model bukan JSON yang valid.")
		}
		entries, ok := field(obj, "data").([]any)
		if !ok {
			return nil, problem(502, "Respons daftar model harus berisi array data.")
		}
		return parseNames(entries, "id"), nil
	}
}
