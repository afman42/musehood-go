package musehood

// Tes fitur pendalaman grup B: circuit breaker (7), retry+backoff (8),
// streaming anthropic/gemini/ollama (9), tool-calling native (10),
// load balancing (12).

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func fastRetry(t *testing.T) {
	t.Helper()
	oldDelays := retryDelays
	retryDelays = []time.Duration{time.Millisecond, time.Millisecond}
	t.Cleanup(func() { retryDelays = oldDelays })
}

func TestRetryThenSuccessAndNoRetry400(t *testing.T) {
	fastRetry(t)
	var hits500, hits400 atomic.Int32
	gateway := func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" {
			io.WriteString(w, `{"data":[{"id":"m"}]}`)
			return
		}
		var body struct{ Model string }
		json.NewDecoder(r.Body).Decode(&body)
		switch body.Model {
		case "flaky":
			if hits500.Add(1) == 1 {
				w.WriteHeader(500)
				io.WriteString(w, "sementara")
				return
			}
			io.WriteString(w, `{"choices":[{"message":{"content":"akhirnya bisa"}}]}`)
		case "badreq":
			hits400.Add(1)
			w.WriteHeader(400)
			io.WriteString(w, "permintaan buruk")
		default:
			fmt.Fprintf(w, `{"choices":[{"message":{"content":"balasan %s"}}]}`, body.Model)
		}
	}
	f := setup(t, gateway)
	f.api("POST", "/api/settings", object{"settings": object{"toolsEnabled": false, "nativeToolsEnabled": false}}, 200)
	f.api("POST", "/api/settings", object{"seats": []object{{"code": "M01", "model": "flaky"}}}, 200)
	f.api("POST", "/api/message", object{"text": "@M01 coba"}, 202)
	msgs := f.finish(2)
	if msgs[1]["text"] != "akhirnya bisa" {
		t.Fatal(msgs[1])
	}
	if hits500.Load() != 2 {
		t.Fatal("retry 500 tidak tepat 2 percobaan:", hits500.Load())
	}
	f.api("POST", "/api/settings", object{"seats": []object{{"code": "M01", "model": "badreq"}}}, 200)
	f.api("POST", "/api/message", object{"text": "@M01 buruk"}, 202)
	msgs = f.finish(4)
	if errText, _ := msgs[3]["error"].(string); !strings.Contains(errText, "400") {
		t.Fatal(msgs[3])
	}
	if hits400.Load() != 1 {
		t.Fatal("400 tidak boleh di-retry:", hits400.Load())
	}
}

func TestCircuitBreakerOpensAndCloses(t *testing.T) {
	fastRetry(t)
	oldOpen := breakerOpenDuration
	breakerOpenDuration = 80 * time.Millisecond
	t.Cleanup(func() { breakerOpenDuration = oldOpen })
	var hits atomic.Int32
	var failing atomic.Bool
	failing.Store(true)
	gateway := func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" {
			io.WriteString(w, `{"data":[{"id":"m"}]}`)
			return
		}
		hits.Add(1)
		if failing.Load() {
			w.WriteHeader(500)
			io.WriteString(w, "rusak")
			return
		}
		io.WriteString(w, `{"choices":[{"message":{"content":"pulih"}}]}`)
	}
	f := setup(t, gateway)
	f.api("POST", "/api/settings", object{"settings": object{"toolsEnabled": false, "nativeToolsEnabled": false}}, 200)
	// 5 kegagalan logis membuka breaker (tiap pesan = 3 percobaan HTTP).
	for i := 0; i < 5; i++ {
		f.api("POST", "/api/message", object{"text": "@M01 gagal"}, 202)
		f.finish(2 + i*2)
	}
	hitsAfterOpen := hits.Load()
	if hitsAfterOpen != 15 {
		t.Fatal("percobaan HTTP tidak sesuai:", hitsAfterOpen)
	}
	// Panggilan berikutnya gagal cepat tanpa menyentuh server.
	f.api("POST", "/api/message", object{"text": "@M01 cepat"}, 202)
	msgs := f.finish(12)
	if errText, _ := msgs[11]["error"].(string); !strings.Contains(errText, "Circuit breaker") {
		t.Fatal("breaker tidak gagal cepat:", msgs[11])
	}
	if hits.Load() != hitsAfterOpen {
		t.Fatal("server tetap disentuh saat breaker terbuka")
	}
	// Setelah jeda half-open, sukses menutup breaker.
	failing.Store(false)
	time.Sleep(100 * time.Millisecond)
	f.api("POST", "/api/message", object{"text": "@M01 pulih"}, 202)
	msgs = f.finish(14)
	if msgs[13]["text"] != "pulih" {
		t.Fatal(msgs[13])
	}
	// Status breaker tampil di health provider sebagai closed.
	f.api("POST", "/api/providers/"+asObjects(f.state()["providers"])[0]["id"].(string)+"/health", object{}, 200)
	health, _ := asObjects(f.state()["providers"])[0]["health"].(object)
	if health == nil || health["circuit"] != "closed" {
		t.Fatal("status circuit tidak tampil:", health)
	}
}

func TestStreamDeltaTextProtocols(t *testing.T) {
	cases := []struct {
		proto   protocol
		payload string
		want    string
	}{
		{protoAnthropic, `{"type":"content_block_delta","delta":{"type":"text_delta","text":"halo"}}`, "halo"},
		{protoAnthropic, `{"type":"message_start"}`, ""},
		{protoGemini, `{"candidates":[{"content":{"parts":[{"text":"satu"},{"text":"dua"}]}}]}`, "satudua"},
		{protoOllama, `{"message":{"role":"assistant","content":"ndjson"},"done":false}`, "ndjson"},
		{protoResponses, `{"type":"response.output_text.delta","delta":"resp"}`, "resp"},
		{protoOpenAI, `{"choices":[{"delta":{"content":"oai"}}]}`, "oai"},
		{protoOpenAI, `[DONE]`, ""},
	}
	for _, c := range cases {
		if got := streamDeltaText(c.proto, c.payload); got != c.want {
			t.Fatalf("%s %s: dapat %q mau %q", c.proto, c.payload, got, c.want)
		}
	}
}

func TestProtocolChatStreamMockServers(t *testing.T) {
	f := setup(t, nil)
	history := []gatewayMessage{{"system", "sys"}, {"user", "halo"}}
	run := func(t *testing.T, proto string, handler http.HandlerFunc, want string) {
		t.Helper()
		srv := httptest.NewServer(handler)
		defer srv.Close()
		cfg := settings{BaseURL: srv.URL, APIKey: "kunci", ProviderName: "uji", GatewayProtocol: proto}
		var deltas []string
		text, err := f.app.protocolChatStream(context.Background(), cfg, "model-uji", history, func(partial string) {
			deltas = append(deltas, partial)
		})
		if err != nil || text != want {
			t.Fatalf("%s: text=%q err=%v", proto, text, err)
		}
		if len(deltas) == 0 || deltas[len(deltas)-1] != want {
			t.Fatalf("%s: delta tidak terakumulasi: %v", proto, deltas)
		}
	}
	t.Run("anthropic", func(t *testing.T) {
		run(t, "anthropic", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			io.WriteString(w, "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"Halo \"}}\n\ndata: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"dunia\"}}\n\n")
		}, "Halo dunia")
	})
	t.Run("gemini", func(t *testing.T) {
		run(t, "gemini", func(w http.ResponseWriter, r *http.Request) {
			if !strings.Contains(r.URL.Path, ":streamGenerateContent") {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "text/event-stream")
			io.WriteString(w, "data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"Gemini \"}]}}]}\n\ndata: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"stream\"}]}}]}\n\n")
		}, "Gemini stream")
	})
	t.Run("ollama", func(t *testing.T) {
		run(t, "ollama", func(w http.ResponseWriter, r *http.Request) {
			io.WriteString(w, "{\"message\":{\"role\":\"assistant\",\"content\":\"Ollama \"},\"done\":false}\n{\"message\":{\"role\":\"assistant\",\"content\":\"ndjson\"},\"done\":true}\n")
		}, "Ollama ndjson")
	})
}

func TestNativeToolCalling(t *testing.T) {
	var sawTools atomic.Bool
	gateway := func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" {
			io.WriteString(w, `{"data":[{"id":"m"}]}`)
			return
		}
		raw, _ := io.ReadAll(r.Body)
		var body struct {
			Tools    []any `json:"tools"`
			Messages []struct {
				Role string `json:"role"`
			} `json:"messages"`
		}
		json.Unmarshal(raw, &body)
		if len(body.Tools) > 0 {
			sawTools.Store(true)
		}
		// Bila sudah ada hasil tool, jawab teks akhir.
		for _, m := range body.Messages {
			if m.Role == "tool" {
				io.WriteString(w, `{"choices":[{"message":{"content":"waktu sudah dicek"}}]}`)
				return
			}
		}
		io.WriteString(w, `{"choices":[{"message":{"content":"","tool_calls":[{"id":"call_1","type":"function","function":{"name":"current_time","arguments":"{\"arg\":\"\"}"}}]}}]}`)
	}
	f := setup(t, gateway)
	f.api("POST", "/api/message", object{"text": "@M01 jam berapa"}, 202)
	msgs := f.finish(2)
	if !sawTools.Load() {
		t.Fatal("tools[] tidak dikirim")
	}
	if msgs[1]["text"] != "waktu sudah dicek" {
		t.Fatal(msgs[1])
	}
	calls := asObjects(msgs[1]["toolCalls"])
	if len(calls) != 1 || calls[0]["name"] != "current_time" || calls[0]["ok"] != true {
		t.Fatal("tool native tidak tercatat:", msgs[1])
	}
}

func TestNativeToolFallbackToText(t *testing.T) {
	gateway := func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" {
			io.WriteString(w, `{"data":[{"id":"m"}]}`)
			return
		}
		raw, _ := io.ReadAll(r.Body)
		if strings.Contains(string(raw), `"tools"`) {
			w.WriteHeader(400)
			io.WriteString(w, "tools tidak didukung")
			return
		}
		io.WriteString(w, `{"choices":[{"message":{"content":"jalur teks jalan"}}]}`)
	}
	f := setup(t, gateway)
	f.api("POST", "/api/message", object{"text": "@M01 teks saja"}, 202)
	msgs := f.finish(2)
	if msgs[1]["text"] != "jalur teks jalan" {
		t.Fatal(msgs[1])
	}
}

func TestLoadBalancingSecondProvider(t *testing.T) {
	fastRetry(t)
	gateway := func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" {
			io.WriteString(w, `{"data":[{"id":"m"}]}`)
			return
		}
		w.WriteHeader(500)
		io.WriteString(w, "provider satu rusak")
	}
	f := setup(t, gateway)
	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+secret {
			http.Error(w, "wrong authorization", 401)
			return
		}
		if r.URL.Path == "/v1/models" {
			io.WriteString(w, `{"data":[{"id":"m"}]}`)
			return
		}
		io.WriteString(w, `{"choices":[{"message":{"content":"balasan provider dua"}}]}`)
	}))
	t.Cleanup(second.Close)
	f.api("POST", "/api/providers", object{"name": "Cadangan", "baseUrl": second.URL, "apiKey": secret, "protocol": "openai"}, 201)
	f.api("POST", "/api/settings", object{"settings": object{"loadBalanceProviders": true, "nativeToolsEnabled": false}}, 200)
	f.api("POST", "/api/message", object{"text": "@M01 tolong"}, 202)
	msgs := f.finish(2)
	if msgs[1]["text"] != "balasan provider dua" {
		t.Fatal(msgs[1])
	}
}
