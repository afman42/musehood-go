package musehood

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Tool kustom deklaratif: definisi JSON di <dataDir>/tools/<nama>.json.
// Tiga tipe:
//   - http_get: GET ke URL template ({{param}} diganti dari baris argumen
//     berurutan, {{arg}} = seluruh argumen); melewati penjaga SSRF.
//   - http_post: POST JSON ke URL template dengan isi dari field "body"
//     (template JSON yang sama); selalu memerlukan persetujuan pemilik
//     (kind "http") dan melewati penjaga SSRF.
//   - shell: perintah shell template yang dijalankan di sandbox yang sama
//     dengan run_shell; selalu memerlukan persetujuan pemilik dan toggle
//     tool coding aktif.
//
// Parameter boleh ditulis sebagai string biasa ("kota") atau objek
// ber-tipe: {"name":"jumlah","type":"number","required":true,"hint":"..."}.
// Tipe yang didukung: "string" (bawaan), "number", "bool". Argumen
// divalidasi terhadap definisi parameter sebelum tool dijalankan.
//
// Contoh tools/cuaca.json:
//   {"name":"cuaca","description":"Cuaca kota (wttr.in)","argHint":"nama kota",
//    "type":"http_get","url":"https://wttr.in/{{kota}}?format=3","params":["kota"]}

type toolParam struct {
	Name     string `json:"name"`
	Type     string `json:"type,omitempty"`
	Required bool   `json:"required,omitempty"`
	Hint     string `json:"hint,omitempty"`
}

// UnmarshalJSON menerima bentuk lama (string nama saja) maupun objek
// ber-tipe, agar definisi tool lama tetap kompatibel.
func (p *toolParam) UnmarshalJSON(raw []byte) error {
	var name string
	if json.Unmarshal(raw, &name) == nil {
		p.Name = name
		p.Type = "string"
		return nil
	}
	type plain toolParam
	var obj plain
	if err := json.Unmarshal(raw, &obj); err != nil {
		return err
	}
	*p = toolParam(obj)
	if p.Type == "" {
		p.Type = "string"
	}
	return nil
}

func validToolParamType(t string) bool {
	return t == "string" || t == "number" || t == "bool"
}

type customTool struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	ArgHint     string      `json:"argHint"`
	Type        string      `json:"type"`
	URL         string      `json:"url"`
	Body        string      `json:"body,omitempty"`
	Command     string      `json:"command"`
	Params      []toolParam `json:"params"`
	// UserAgent opsional untuk http_get/http_post; bawaan "musehood-go/1.0"
	// (sebagian API seperti wttr.in mengembalikan HTML untuk User-Agent browser).
	UserAgent string `json:"userAgent"`
}

const customToolUserAgent = "musehood-go/1.0"

var customToolNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

var builtinToolNames = map[string]bool{
	"web_search": true, "fetch_url": true, "list_files": true, "read_file": true,
	"write_file": true, "append_file": true, "edit_file": true, "delete_file": true,
	"download_file": true, "zip_files": true, "unzip_file": true, "file_hash": true,
	"image_info": true, "resize_image": true, "convert_image": true, "pdf_text": true,
	"csv_json": true, "generate_image": true, "memory_save": true, "current_time": true,
	"run_code": true, "run_shell": true, "git": true, "screenshot_url": true,
	"pdf_from_markdown": true, "summarize_url": true,
	"translate_text": true, "ocr_image": true, "image_caption": true, "qr_code": true,
	"rss_feed": true, "youtube_transcript": true, "currency_convert": true,
	"text_to_speech": true, "http_post": true, "password_gen": true,
}

func (a *App) toolsDir() string {
	return filepath.Join(a.dir, "tools")
}

func (a *App) loadCustomTools() []customTool {
	entries, err := os.ReadDir(a.toolsDir())
	if err != nil {
		return nil
	}
	var tools []customTool
	seen := map[string]bool{}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(a.toolsDir(), entry.Name()))
		if err != nil {
			continue
		}
		var tool customTool
		if err := json.Unmarshal(data, &tool); err != nil {
			continue
		}
		tool.Name = strings.TrimSpace(tool.Name)
		if !customToolNamePattern.MatchString(tool.Name) || builtinToolNames[tool.Name] || seen[tool.Name] {
			continue
		}
		switch tool.Type {
		case "http_get":
			if strings.TrimSpace(tool.URL) == "" {
				continue
			}
		case "http_post":
			if strings.TrimSpace(tool.URL) == "" || strings.TrimSpace(tool.Body) == "" {
				continue
			}
		case "shell":
			if strings.TrimSpace(tool.Command) == "" {
				continue
			}
		default:
			continue
		}
		paramsOK := true
		for _, param := range tool.Params {
			if strings.TrimSpace(param.Name) == "" || !validToolParamType(param.Type) {
				paramsOK = false
				break
			}
		}
		if !paramsOK {
			continue
		}
		seen[tool.Name] = true
		tools = append(tools, tool)
	}
	sort.Slice(tools, func(i, j int) bool { return tools[i].Name < tools[j].Name })
	return tools
}

func (a *App) findCustomTool(name string) (customTool, bool) {
	for _, tool := range a.loadCustomTools() {
		if tool.Name == name {
			return tool, true
		}
	}
	return customTool{}, false
}

// validateToolParams memeriksa argumen terhadap definisi parameter
// ber-tipe: parameter wajib tidak boleh kosong dan nilai harus cocok
// dengan tipenya. Mengembalikan pesan error yang jelas untuk model.
func validateToolParams(tool customTool, arg string) error {
	lines := splitArgLines(arg)
	for i, param := range tool.Params {
		value := ""
		if i < len(lines) {
			value = strings.TrimSpace(lines[i])
		}
		if value == "" {
			if param.Required {
				return fmt.Errorf("parameter %q wajib diisi", param.Name)
			}
			continue
		}
		switch param.Type {
		case "number":
			if _, err := strconv.ParseFloat(value, 64); err != nil {
				return fmt.Errorf("parameter %q harus berupa angka, diterima %q", param.Name, value)
			}
		case "bool":
			if _, err := strconv.ParseBool(value); err != nil {
				return fmt.Errorf("parameter %q harus berupa boolean (true/false), diterima %q", param.Name, value)
			}
		}
	}
	return nil
}

// substituteParams mengganti {{arg}} dan {{param}} pada template. Nilai
// parameter diambil dari baris argumen berurutan; untuk URL nilai di-escape.
func substituteParams(template string, tool customTool, arg string, forURL bool) string {
	values := map[string]string{"arg": strings.TrimSpace(arg)}
	lines := splitArgLines(arg)
	for i, param := range tool.Params {
		if i < len(lines) {
			values[param.Name] = lines[i]
		} else {
			values[param.Name] = ""
		}
	}
	result := template
	for key, value := range values {
		replacement := value
		if forURL {
			replacement = url.PathEscape(value)
		}
		result = strings.ReplaceAll(result, "{{"+key+"}}", replacement)
	}
	return result
}

// fetchCustomURL melakukan GET dan mengembalikan teks respons. Penjaga SSRF
// dilakukan pemanggil agar fungsi ini bisa diuji dengan server lokal.
func fetchCustomURL(ctx context.Context, rawURL, userAgent string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", err
	}
	if userAgent == "" {
		userAgent = customToolUserAgent
	}
	req.Header.Set("User-Agent", userAgent)
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 512*1024))
	if err != nil {
		return "", err
	}
	text := string(body)
	if strings.Contains(resp.Header.Get("Content-Type"), "text/html") {
		text = htmlToText(text)
	}
	return strings.TrimSpace(text), nil
}

// postCustomJSON mengirim POST JSON dan mengembalikan "HTTP <kode>\n<isi>".
// Penjaga SSRF dilakukan pemanggil agar fungsi ini bisa diuji dengan
// server lokal.
func postCustomJSON(ctx context.Context, rawURL, body, userAgent string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, rawURL, strings.NewReader(body))
	if err != nil {
		return "", err
	}
	if userAgent == "" {
		userAgent = customToolUserAgent
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 512*1024))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("HTTP %d\n%s", resp.StatusCode, strings.TrimSpace(string(raw))), nil
}

func (a *App) runCustomTool(ctx context.Context, s seat, tool customTool, arg string, code bool) string {
	if err := validateToolParams(tool, arg); err != nil {
		return fmt.Sprintf("Error: argumen tool %s tidak valid: %s.", tool.Name, err.Error())
	}
	switch tool.Type {
	case "http_get":
		finalURL := substituteParams(tool.URL, tool, arg, true)
		if err := checkPublicURL(ctx, finalURL); err != nil {
			return "Error: URL tool kustom ditolak penjaga SSRF."
		}
		text, err := fetchCustomURL(ctx, finalURL, tool.UserAgent)
		if err != nil {
			return fmt.Sprintf("Error: tool %s gagal mengambil URL.", tool.Name)
		}
		if text == "" {
			return fmt.Sprintf("Tool %s tidak mengembalikan isi.", tool.Name)
		}
		return truncateToolResult(text)
	case "http_post":
		finalURL := substituteParams(tool.URL, tool, arg, true)
		body := substituteParams(tool.Body, tool, arg, false)
		if !json.Valid([]byte(body)) {
			return fmt.Sprintf("Error: isi JSON tool %s tidak valid setelah substitusi parameter.", tool.Name)
		}
		if err := checkPublicURL(ctx, finalURL); err != nil {
			return "Error: URL tool kustom ditolak penjaga SSRF."
		}
		detail := "POST " + finalURL + "\n" + truncateRunes(body, 500)
		if !a.requestApproval(ctx, s, "http", "", detail) {
			return "Permintaan POST TIDAK dikirim karena pemilik menolak atau persetujuan kedaluwarsa."
		}
		text, err := postCustomJSON(ctx, finalURL, body, tool.UserAgent)
		if err != nil {
			return fmt.Sprintf("Error: tool %s gagal mengirim POST.", tool.Name)
		}
		return truncateToolResult(text)
	case "shell":
		if !code {
			return "Error: tool coding dimatikan di Pengaturan."
		}
		command := substituteParams(tool.Command, tool, arg, false)
		if !a.requestApproval(ctx, s, "shell", "", command) {
			return "Perintah TIDAK dijalankan karena pemilik menolak atau persetujuan kedaluwarsa."
		}
		return a.executeCode(ctx, "sh", "sh", command+"\n")
	}
	return fmt.Sprintf("Error: tipe tool kustom %q tidak dikenal.", tool.Type)
}
