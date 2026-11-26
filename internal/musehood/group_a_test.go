package musehood

import (
	"context"
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
