package musehood

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// Grup A: rantai fallback, allowlist tool per kursi, harga provider + estimasi biaya.

func TestFallbackChain(t *testing.T) {
	f := setup(t, nil)
	f.api("POST", "/api/settings", object{"seats": []object{
		{"code": "M01", "model": "denied", "fallbackModels": []string{"denied", "plain"}},
	}}, 200)
	f.api("POST", "/api/message", object{"text": "@Plamo halo"}, 202)
	msgs := f.finish(2)
	reply := msgs[1]
	if reply["text"] != "teks polos" {
		t.Fatalf("rantai fallback harus sampai ke model plain, dapat: %v", reply)
	}
	if reply["error"] != nil && reply["error"] != "" {
		t.Fatalf("balasan fallback tidak boleh error: %v", reply)
	}
}

func TestDisabledToolsPerSeat(t *testing.T) {
	f := setup(t, nil)
	f.api("POST", "/api/settings", object{"seats": []object{
		{"code": "M01", "disabledTools": []string{"web_search", "run_code"}},
	}}, 200)
	s := f.app.data.Seats[2] // M01
	instructions := f.app.buildToolInstructions(true, s)
	if strings.Contains(instructions, "- web_search:") || strings.Contains(instructions, "- run_code:") {
		t.Fatal("instruksi tool harus menyembunyikan tool yang dinonaktifkan")
	}
	if !strings.Contains(instructions, "- fetch_url:") {
		t.Fatal("tool lain harus tetap ditawarkan")
	}
	result, _ := f.app.runTool(context.Background(), f.app.data.Settings, s, "web_search", "uji", true)
	if !strings.Contains(result, "dinonaktifkan") {
		t.Fatalf("runTool harus menolak tool nonaktif, dapat: %q", result)
	}
	f.api("POST", "/api/settings", object{"seats": []object{
		{"code": "M01", "disabledTools": []string{"Nama Buruk!"}},
	}}, 400)
}

func TestProviderPricesAndStatsCost(t *testing.T) {
	f := setup(t, nil)
	out := f.api("POST", "/api/providers", object{
		"name": "TesHarga", "baseUrl": "http://127.0.0.1:9/v1", "apiKey": "kunci-uji-1234",
		"protocol": "openai", "priceInputPer1M": 2.5, "priceOutputPer1M": 10,
	}, 201)
	providers := asObjects(out["providers"])
	_ = providers
	list := f.api("GET", "/api/providers", nil, 200)
	found := false
	for _, p := range asObjects(list["providers"]) {
		if p["name"] == "TesHarga" {
			found = true
			if p["priceInputPer1M"] != 2.5 || p["priceOutputPer1M"] != float64(10) {
				t.Fatalf("harga provider tidak tersimpan: %v", p)
			}
		}
	}
	if !found {
		t.Fatal("provider TesHarga tidak ditemukan")
	}
	f.app.recordStat(seat{Code: "M01"}, settings{ProviderName: "TesHarga"}, "model-x", true, time.Second, 1_000_000, 500_000)
	stats := f.api("GET", "/api/stats", nil, 200)["stats"].(object)
	entry := stats["model:TesHarga/model-x"].(object)
	if entry["estimatedCostUsd"] != 7.5 {
		t.Fatalf("estimasi biaya harus 7.5 USD, dapat: %v", entry["estimatedCostUsd"])
	}
}

// Grup D2: aktivitas tool live + kartu hasil tool.

func TestToolCallsRecordedAndActivity(t *testing.T) {
	calls := 0
	f := setup(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			io.WriteString(w, `{"data":[]}`)
			return
		}
		var body struct{ Stream *bool }
		json.NewDecoder(r.Body).Decode(&body)
		calls++
		text := "Sudah saya cek waktunya."
		if calls == 1 {
			text = "<<TOOL:current_time>>\n<</TOOL>>"
		}
		if body.Stream != nil && *body.Stream {
			w.Header().Set("Content-Type", "text/event-stream")
			raw, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"content": text}}}})
			io.WriteString(w, "data: "+string(raw)+"\n\ndata: [DONE]\n\n")
			return
		}
		raw, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": text}}}})
		w.Write(raw)
	})
	f.api("POST", "/api/message", object{"text": "@Plamo jam berapa sekarang?"}, 202)
	msgs := f.finish(2)
	reply := msgs[1]
	if reply["text"] != "Sudah saya cek waktunya." {
		t.Fatalf("teks akhir salah: %v", reply)
	}
	toolCalls := asObjects(reply["toolCalls"])
	if len(toolCalls) != 1 || toolCalls[0]["name"] != "current_time" || toolCalls[0]["ok"] != true {
		t.Fatalf("toolCalls tidak tercatat benar: %v", reply["toolCalls"])
	}
	if result, _ := toolCalls[0]["result"].(string); !strings.Contains(result, "WIB") && !strings.Contains(result, "2026") {
		t.Fatalf("pratinjau hasil tool kosong: %q", result)
	}
	raw, _ := json.Marshal(reply)
	if !strings.Contains(string(raw), "current_time") {
		t.Fatal("toolCalls harus ikut terkirim ke klien")
	}
}

// Grup D1: streaming.

func TestStreamDeltaParsers(t *testing.T) {
	cases := []struct {
		proto   protocol
		payload string
		want    string
	}{
		{protoOpenAI, `{"choices":[{"delta":{"content":"Halo "}}]}`, "Halo "},
		{protoOpenAI, `{"choices":[{"delta":{"content":[{"text":"dunia"}]}}]}`, "dunia"},
		{protoResponses, `{"type":"response.output_text.delta","delta":"potongan"}`, "potongan"},
		{protoResponses, `{"type":"response.created"}`, ""},
		{protoAnthropic, `{"type":"content_block_delta","delta":{"type":"text_delta","text":"teks"}}`, "teks"},
		{protoGemini, `{"candidates":[{"content":{"parts":[{"text":"gemini "},{"text":"teks"}]}}]}`, "gemini teks"},
		{protoOllama, `{"message":{"role":"assistant","content":"ollama"}}`, "ollama"},
		{protoOpenAI, `[DONE]`, ""},
	}
	for _, c := range cases {
		if got := streamDeltaText(c.proto, c.payload); got != c.want {
			t.Errorf("streamDeltaText(%s, %s) = %q, ingin %q", c.proto, c.payload, got, c.want)
		}
	}
}

func TestStreamingEndToEndAndFallback(t *testing.T) {
	f := setup(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			io.WriteString(w, `{"data":[]}`)
			return
		}
		var body struct {
			Model  string
			Stream *bool
		}
		json.NewDecoder(r.Body).Decode(&body)
		if body.Model == "nostream" {
			if body.Stream != nil && *body.Stream {
				http.Error(w, "stream tidak didukung", 400)
				return
			}
			io.WriteString(w, `{"choices":[{"message":{"content":"jawaban non-stream"}}]}`)
			return
		}
		if body.Stream != nil && *body.Stream {
			w.Header().Set("Content-Type", "text/event-stream")
			io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"Halo \"}}]}\n\n")
			io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"streaming\"}}]}\n\n")
			io.WriteString(w, "data: [DONE]\n\n")
			return
		}
		io.WriteString(w, `{"choices":[{"message":{"content":"jawaban biasa"}}]}`)
	})
	f.api("POST", "/api/settings", object{"settings": object{"streamingEnabled": true}, "seats": []object{{"code": "M01", "model": "streammodel"}, {"code": "M02", "model": "nostream"}}}, 200)
	f.api("POST", "/api/message", object{"text": "@Plamo halo"}, 202)
	msgs := f.finish(2)
	if msgs[1]["text"] != "Halo streaming" {
		t.Fatalf("hasil streaming salah: %v", msgs[1])
	}
	f.api("POST", "/api/message", object{"text": "@GLM halo"}, 202)
	msgs = f.finish(4)
	if msgs[3]["text"] != "jawaban non-stream" {
		t.Fatalf("fallback non-stream salah: %v", msgs[3])
	}
}

// Grup C: regenerate, pesan terjadwal, jadwal berulang.

func TestRegenerateMessage(t *testing.T) {
	f := setup(t, nil)
	f.api("POST", "/api/message", object{"text": "@Plamo halo"}, 202)
	msgs := f.finish(2)
	oldID := msgs[1]["id"].(string)
	f.api("POST", "/api/message/"+oldID+"/regenerate", object{}, 202)
	msgs = f.finish(2)
	if msgs[1]["id"] == oldID {
		t.Fatal("regenerate harus membuat pesan baru")
	}
	if msgs[1]["status"] != "completed" {
		t.Fatalf("balasan regenerate tidak selesai: %v", msgs[1])
	}
	// Pesan pemilik tidak bisa di-regenerate.
	ownerID := msgs[0]["id"].(string)
	f.api("POST", "/api/message/"+ownerID+"/regenerate", object{}, 403)
}

func TestScheduledMessagesCRUDAndFire(t *testing.T) {
	f := setup(t, nil)
	// Sekali kirim yang sudah jatuh tempo (1 menit lalu) → terkirim pada tick.
	past := time.Now().Add(-time.Minute).UTC().Format("2006-01-02T15:04:05.000Z")
	created := f.api("POST", "/api/scheduled", object{"text": "@Plamo pesan terjadwal", "sendAt": past}, 201)
	if created["id"] == nil || created["nextAt"] == nil {
		t.Fatalf("jadwal tidak dibuat: %v", created)
	}
	msgs := f.finish(2)
	if msgs[0]["text"] != "@Plamo pesan terjadwal" {
		t.Fatalf("pesan terjadwal tidak terkirim: %v", msgs[0])
	}
	list := f.api("GET", "/api/scheduled", nil, 200)
	if len(asObjects(list["scheduled"])) != 0 {
		t.Fatal("jadwal sekali kirim harus hilang setelah terkirim")
	}
	// Jadwal berulang harian: dibuat, dinonaktifkan, dihapus.
	rec := f.api("POST", "/api/scheduled", object{"text": "ringkasan pagi", "recur": "daily", "timeOfDay": "07:00", "tzOffsetMinutes": 420}, 201)
	recID := rec["id"].(string)
	nextAt, _ := rec["nextAt"].(string)
	parsed, err := time.Parse(time.RFC3339Nano, nextAt)
	if err != nil {
		t.Fatalf("nextAt tidak valid: %q", nextAt)
	}
	// 07:00 WIB = 00:00 UTC.
	if parsed.Hour() != 0 || parsed.Minute() != 0 {
		t.Fatalf("nextAt harian harus 00:00 UTC (07:00 WIB), dapat %v", parsed)
	}
	updated := f.api("POST", "/api/scheduled/"+recID, object{"enabled": false}, 200)
	if updated["enabled"] != false {
		t.Fatalf("toggle jadwal gagal: %v", updated)
	}
	f.api("DELETE", "/api/scheduled/"+recID, nil, 200)
	f.api("GET", "/api/scheduled", nil, 200)
	f.api("DELETE", "/api/scheduled/"+recID, nil, 404)
	// Validasi.
	f.api("POST", "/api/scheduled", object{"text": "", "sendAt": past}, 400)
	f.api("POST", "/api/scheduled", object{"text": "x", "recur": "daily", "timeOfDay": "25:99"}, 400)
}

func TestComputeNextAtWeekly(t *testing.T) {
	// Rabu 2026-11-25 10:00 WIB (UTC+7) → jadwal Senin 07:00 WIB berikutnya = 2026-11-30 00:00 UTC.
	from := time.Date(2026, 11, 25, 3, 0, 0, 0, time.UTC)
	s := scheduledMessage{Recur: "weekly", TimeOfDay: "07:00", Weekday: 1, TzOffsetMinutes: 420}
	next, err := computeNextAt(s, from)
	if err != nil {
		t.Fatal(err)
	}
	want := time.Date(2026, 11, 30, 0, 0, 0, 0, time.UTC)
	if !next.UTC().Equal(want) {
		t.Fatalf("weekly next = %v, ingin %v", next.UTC(), want)
	}
}
