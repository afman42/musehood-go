package musehood

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

// setupProto mirrors setup() but mounts a protocol-specific gateway mock at
// the configured base URL and applies the given protocol setting.
func setupProto(t *testing.T, proto, model string, gateway http.HandlerFunc) fixture {
	t.Helper()
	gw := httptest.NewServer(gateway)
	t.Cleanup(gw.Close)
	dir := t.TempDir()
	app, err := New(Options{DataDir: dir, Frontend: fstest.MapFS{"index.html": {Data: []byte("<!doctype html>Musehood")}}, PollInterval: 5 * time.Millisecond, MuseWait: 150 * time.Millisecond, TrustedHosts: "muse.test"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(app.Close)
	server := httptest.NewServer(app)
	t.Cleanup(server.Close)
	f := fixture{t: t, server: server, dir: dir, app: app}
	f.api("POST", "/api/settings", object{"settings": object{"baseUrl": gw.URL, "apiKey": secret, "gatewayProtocol": proto}, "seats": []object{{"code": "M10", "model": model}}}, 200)
	return f
}

func replyOf(m object) string { text, _ := m["text"].(string); return text }

func TestAnthropicProtocol(t *testing.T) {
	var lastBody object
	f := setupProto(t, "anthropic", "sse", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != secret || r.Header.Get("anthropic-version") != "2023-06-01" {
			http.Error(w, "auth anthropic asli", 401)
			return
		}
		if r.Header.Get("Authorization") != "" {
			t.Error("anthropic tidak boleh pakai Authorization header")
		}
		switch r.Method + " " + r.URL.Path {
		case "GET /v1/models":
			io.WriteString(w, `{"data":[{"id":"sse"}]}`)
		case "POST /v1/messages":
			body, _ := io.ReadAll(r.Body)
			json.Unmarshal(body, &lastBody)
			if lastBody["max_tokens"] != float64(1024) {
				http.Error(w, "max_tokens mock salah", 400)
				return
			}
			if system, _ := lastBody["system"].(string); !strings.Contains(system, "Nama pemilik: Arman") {
				http.Error(w, "system prompt harus field terpisah", 400)
				return
			}
			msgs, _ := lastBody["messages"].([]any)
			if len(msgs) == 0 {
				http.Error(w, "messages kosong", 400)
				return
			}
			if msg, _ := msgs[0].(map[string]any); msg["role"] == "system" {
				http.Error(w, "system tidak boleh di dalam messages", 400)
				return
			}
			w.Header().Set("Content-Type", "text/event-stream")
			io.WriteString(w, "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"Halo \"}}\n\nevent: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"anthropic\"}}\n\nevent: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
		default:
			http.NotFound(w, r)
		}
	})
	res := f.api("POST", "/api/test-connection", nil, 200)
	if models := modelsOf(res); len(models) != 1 || models[0] != "sse" {
		t.Fatal(res)
	}
	f.api("POST", "/api/message", object{"text": "@Opus uji"}, 202)
	m := f.finish(2)[1]
	if m["seat"] != "M10" || replyOf(m) != "Halo anthropic" {
		t.Fatal(m)
	}
	// Kesalahan provider ditampilkan apa adanya (status + isi body).
	f.api("POST", "/api/settings", object{"settings": object{"apiKey": "kunci-salah"}}, 200)
	f.api("POST", "/api/message", object{"text": "@Opus uji lagi"}, 202)
	m = f.finish(4)[3]
	if m["error"] != "HTTP 401: auth anthropic asli\n" {
		t.Fatal(m)
	}
	_ = f
}

func modelsOf(res object) []any { v, _ := res["models"].([]any); return v }

func TestGeminiProtocol(t *testing.T) {
	f := setupProto(t, "gemini", "gemini-mock", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("key") != secret {
			http.Error(w, "key gemini asli salah", 401)
			return
		}
		if r.Header.Get("Authorization") != "" || r.Header.Get("x-api-key") != "" {
			t.Error("gemini tidak boleh pakai auth header")
		}
		switch {
		case r.Method == "GET" && r.URL.Path == "/v1beta/models":
			io.WriteString(w, `{"models":[{"name":"models/gemini-mock"},{"displayName":"bukan model"}]}`)
		case r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/models/gemini-mock:generateContent"):
			var body object
			json.NewDecoder(r.Body).Decode(&body)
			if si, _ := body["systemInstruction"].(map[string]any); si == nil {
				http.Error(w, "systemInstruction wajib", 400)
				return
			}
			contents, _ := body["contents"].([]any)
			lastContent := ""
			if len(contents) > 0 {
				if last, _ := contents[len(contents)-1].(map[string]any); last != nil {
					if parts, _ := last["parts"].([]any); len(parts) > 0 {
						if part, _ := parts[0].(map[string]any); part != nil {
							lastContent, _ = part["text"].(string)
						}
					}
				}
			}
			if strings.Contains(lastContent, "mintaSSE") {
				w.Header().Set("Content-Type", "text/event-stream")
				io.WriteString(w, "data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"Balasan \"}]}}]}\n\ndata: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"gemini SSE\"}]}}]}\n\n")
				return
			}
			io.WriteString(w, `{"candidates":[{"content":{"parts":[{"text":"Balasan "},{"text":"gemini"}]}}]}`)
		default:
			http.NotFound(w, r)
		}
	})
	if res := f.api("POST", "/api/test-connection", nil, 200); len(modelsOf(res)) == 0 {
		t.Fatal(res)
	}
	f.api("POST", "/api/message", object{"text": "@Opus uji"}, 202)
	m := f.finish(2)[1]
	if replyOf(m) != "Balasan gemini" {
		t.Fatal(m)
	}
	// URL /v1beta jangan digandakan bila baseUrl sudah memuatnya.
	f.api("POST", "/api/settings", object{"settings": object{"baseUrl": f.state()["settings"].(object)["baseUrl"].(string) + "/v1beta"}}, 200)
	f.api("POST", "/api/message", object{"text": "@Opus mintaSSE"}, 202)
	m = f.finish(4)[3]
	if replyOf(m) != "Balasan gemini SSE" {
		t.Fatal(m)
	}
	f.api("POST", "/api/settings", object{"settings": object{"apiKey": "kunci-salah"}}, 200)
	f.api("POST", "/api/message", object{"text": "@Opus uji salah"}, 202)
	m = f.finish(6)[5]
	if m["error"] != "HTTP 401: key gemini asli salah\n" {
		t.Fatal(m)
	}
}

func TestOllamaProtocol(t *testing.T) {
	f := setupProto(t, "ollama", "sse", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "GET /api/tags":
			io.WriteString(w, `{"models":[{"name":"sse"},{"name":"json-ollama"}]}`)
		case "POST /api/chat":
			var body object
			json.NewDecoder(r.Body).Decode(&body)
			if body["model"] == "sse" {
				w.Header().Set("Content-Type", "application/x-ndjson")
				io.WriteString(w, `{"message":{"content":"Balasan "},"done":false}`+"\n"+`{"message":{"content":"ollama NDJSON"},"done":true}`+"\n")
				return
			}
			if body["stream"] != false {
				http.Error(w, "mock minta stream false", 400)
				return
			}
			io.WriteString(w, `{"message":{"content":"Balasan ollama JSON"},"done":true}`)
		default:
			http.NotFound(w, r)
		}
	})
	if res := f.api("POST", "/api/test-connection", nil, 200); len(modelsOf(res)) == 0 {
		t.Fatal(res)
	}
	f.api("POST", "/api/message", object{"text": "@Opus uji"}, 202)
	m := f.finish(2)[1]
	if replyOf(m) != "Balasan ollama NDJSON" {
		t.Fatal(m)
	}
	// Ollama boleh jalan tanpa API key.
	f.api("POST", "/api/settings", object{"settings": object{"clearApiKey": true}}, 200)
	f.api("POST", "/api/message", object{"text": "@Opus uji tanpa kunci"}, 202)
	m = f.finish(4)[3]
	if replyOf(m) != "Balasan ollama NDJSON" {
		t.Fatal(m)
	}
}

func TestResponsesProtocol(t *testing.T) {
	var lastInput []any
	f := setupProto(t, "openai-responses", "sse", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+secret {
			http.Error(w, "auth responses asli", 401)
			return
		}
		switch r.Method + " " + r.URL.Path {
		case "GET /v1/models":
			io.WriteString(w, `{"data":[{"id":"sse"}]}`)
		case "POST /v1/responses":
			if r.Header.Get("Authorization") != "Bearer "+secret {
				http.Error(w, "auth responses asli", 401)
				return
			}
			var body object
			json.NewDecoder(r.Body).Decode(&body)
			input, _ := body["input"].([]any)
			lastInput = input
			t.Logf("responses POST auth=%q", r.Header.Get("Authorization"))
			if body["messages"] != nil {
				http.Error(w, "responses harus pakai input", 400)
				return
			}
			last, _ := input[len(input)-1].(map[string]any)
			if strings.Contains(last["content"].(string), "mintaSSE") {
				w.Header().Set("Content-Type", "text/event-stream")
				io.WriteString(w, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"Balasan \"}\n\ndata: {\"type\":\"response.output_text.delta\",\"delta\":\"responses SSE\"}\n\ndata: {\"type\":\"response.completed\",\"response\":{\"output\":[{\"content\":[{\"type\":\"output_text\",\"text\":\"JIKA INI MUNCUL BERARTI DOBEL\"}]}]}}\n\n")
				return
			}
			io.WriteString(w, `{"output":[{"type":"message","content":[{"type":"output_text","text":"Balasan responses JSON"}]}]}`)
		default:
			http.NotFound(w, r)
		}
	})
	if res := f.api("POST", "/api/test-connection", nil, 200); len(modelsOf(res)) == 0 {
		t.Fatal(res)
	}
	f.api("POST", "/api/message", object{"text": "@Opus uji"}, 202)
	m := f.finish(2)[1]
	if replyOf(m) != "Balasan responses JSON" {
		t.Fatal(m)
	}
	if len(lastInput) == 0 || lastInput[0].(map[string]any)["role"] != "system" {
		t.Fatal(lastInput)
	}
	f.api("POST", "/api/message", object{"text": "@Opus mintaSSE"}, 202)
	m = f.finish(4)[3]
	if replyOf(m) != "Balasan responses SSE" || strings.Contains(replyOf(m), "DOBEL") {
		t.Fatal(m)
	}
	f.api("POST", "/api/settings", object{"settings": object{"apiKey": "kunci-salah"}}, 200)
	f.api("POST", "/api/message", object{"text": "@Opus uji salah"}, 202)
	ms := f.finish(6)
	m = ms[5]
	if m["error"] != "HTTP 401: auth responses asli\n" {
		for i, mm := range ms {
			t.Logf("msg %d: %v", i, mm)
		}
		t.Fatal(m)
	}
}

func TestImageFallsBackToMuseQueueForNonOpenAI(t *testing.T) {
	var imageCalled bool
	f := setupProto(t, "anthropic", "sse", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != secret {
			http.Error(w, "auth anthropic asli", 401)
			return
		}
		if r.Method == "POST" && r.URL.Path == "/v1/messages" {
			w.Write([]byte(`{"content":[{"type":"text","text":"chat saja"}]}`))
			return
		}
		if strings.Contains(r.URL.Path, "images") {
			imageCalled = true
		}
		http.NotFound(w, r)
	})
	f.api("POST", "/api/settings", object{"settings": object{"roundRobin": false}}, 200)
	f.api("POST", "/api/message", object{"text": "Gambar: bunga"}, 202)
	q := f.pending("image")
	// Jalur cadangan antrean file Muse: kursi visual M03 menerima antrean,
	// tanpa memanggil endpoint gambar gateway yang tidak ada.
	if q["seat"] != "M03" {
		t.Fatal(q)
	}
	if imageCalled {
		t.Fatal("endpoint gambar gateway jangan dipanggil untuk protokol anthropic")
	}
	f.api("GET", "/api/state", nil, 200)
}

func TestProtocolValidation(t *testing.T) {
	f := setupProto(t, "openai", "sse", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+secret {
			http.Error(w, "auth asli", 401)
			return
		}
		io.WriteString(w, `{"data":[]}`)
	})
	f.api("POST", "/api/settings", object{"settings": object{"gatewayProtocol": "chatgpt"}}, 400)
	f.api("POST", "/api/settings", object{"settings": object{"gatewayProtocol": "gemini"}}, 200)
	if s := f.state()["settings"].(object); s["gatewayProtocol"] != "gemini" {
		t.Fatal(s)
	}
}
