package musehood

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func providerList(t *testing.T, f fixture) []any {
	t.Helper()
	out := f.api("GET", "/api/providers", nil, 200)
	list, ok := out["providers"].([]any)
	if !ok {
		// GET /api/providers mengembalikan array di tingkat atas.
		t.Fatalf("daftar provider bukan array: %#v", out)
	}
	return list
}

func providerByID(t *testing.T, list []any, id string) map[string]any {
	t.Helper()
	for _, item := range list {
		p, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if p["id"] == id {
			return p
		}
	}
	t.Fatalf("provider %s tidak ada di daftar", id)
	return nil
}

func TestProviderCRUD(t *testing.T) {
	f := setup(t, nil)
	settings, _ := f.state()["settings"].(map[string]any)
	// Setup fixture mengonfigurasi gateway lewat /api/settings; migrasi
	// tulis-balik harus sudah membuat satu provider aktif dari sana.
	list := providerList(t, f)
	if len(list) != 1 {
		t.Fatalf("provider awal = %d, mau 1", len(list))
	}
	first := list[0].(map[string]any)
	if first["active"] != true || first["hasApiKey"] != true {
		t.Fatalf("provider awal = %#v", first)
	}
	if _, ok := first["apiKey"]; ok {
		t.Fatal("apiKey tidak boleh muncul di daftar provider")
	}
	// State harus menampilkan hasApiKey yang sama dan tanpa field apiKey.
	stateProviders, ok := f.state()["providers"].([]any)
	if !ok || len(stateProviders) != 1 {
		t.Fatalf("state providers = %#v", f.state()["providers"])
	}
	sp := stateProviders[0].(map[string]any)
	if sp["hasApiKey"] != true {
		t.Fatalf("state provider hasApiKey = %#v", sp)
	}
	if _, ok := sp["apiKey"]; ok {
		t.Fatal("apiKey tidak boleh muncul di state providers")
	}
	if settings["baseUrl"] == "" || settings["providerName"] != "" && first["baseUrl"] != settings["baseUrl"] {
		t.Fatalf("settings tidak sinkron dengan provider aktif: %#v vs %#v", settings, first)
	}

	created := f.api("POST", "/api/providers", object{"name": "Kedua", "baseUrl": "https://kedua.invalid/v1/", "apiKey": "kunci-kedua-123", "protocol": "openai-responses"}, 201)
	if created["name"] != "Kedua" || created["baseUrl"] != "https://kedua.invalid/v1" || created["protocol"] != "openai-responses" || created["hasApiKey"] != true || created["active"] != false {
		t.Fatalf("provider baru = %#v", created)
	}
	if _, ok := created["apiKey"]; ok {
		t.Fatal("apiKey tidak boleh muncul di respons create")
	}
	secondID, _ := created["id"].(string)
	if secondID == "" {
		t.Fatal("provider baru tidak punya id")
	}

	// Validasi: nama kosong, URL bukan HTTP, protokol tidak dikenal, dan
	// key wajib kecuali Ollama.
	f.api("POST", "/api/providers", object{"name": "", "baseUrl": "https://x.invalid", "apiKey": "k", "protocol": "openai"}, 400)
	f.api("POST", "/api/providers", object{"name": "Buruk", "baseUrl": "ftp://x.invalid", "apiKey": "k", "protocol": "openai"}, 400)
	f.api("POST", "/api/providers", object{"name": "Buruk", "baseUrl": "https://x.invalid", "apiKey": "k", "protocol": "tidak-ada"}, 400)
	f.api("POST", "/api/providers", object{"name": "TanpaKey", "baseUrl": "https://x.invalid", "protocol": "openai"}, 400)
	ollama := f.api("POST", "/api/providers", object{"name": "Lokal", "baseUrl": "http://127.0.0.1:11434", "protocol": "ollama"}, 201)
	if ollama["hasApiKey"] != false {
		t.Fatalf("ollama tanpa key = %#v", ollama)
	}
	ollamaID, _ := ollama["id"].(string)

	// Update: ganti nama dan key; key kosong mempertahankan key lama.
	updated := f.api("POST", "/api/providers/"+secondID, object{"name": "Kedua Baru", "apiKey": "kunci-baru-456"}, 200)
	if updated["name"] != "Kedua Baru" || updated["hasApiKey"] != true {
		t.Fatalf("provider setelah update = %#v", updated)
	}
	// Key lama tidak boleh bocor lewat state.
	raw, _ := json.Marshal(f.state())
	if strings.Contains(string(raw), "kunci-kedua-123") {
		t.Fatal("key lama bocor di state setelah rotasi")
	}
	if strings.Contains(string(raw), "kunci-baru-456") {
		t.Fatal("key baru bocor di state")
	}
	f.api("POST", "/api/providers/"+secondID, object{"protocol": "tidak-ada"}, 400)
	f.api("POST", "/api/providers/tidak-ada", object{"name": "X"}, 404)
	f.api("DELETE", "/api/providers/tidak-ada", nil, 404)

	// Aktifkan provider kedua: settings harus ikut berpindah.
	f.api("POST", "/api/providers/"+secondID+"/activate", object{}, 200)
	state := f.state()
	settings, _ = state["settings"].(map[string]any)
	if settings["baseUrl"] != "https://kedua.invalid/v1" || settings["providerName"] != "Kedua Baru" || settings["gatewayProtocol"] != "openai-responses" {
		t.Fatalf("settings setelah aktivasi = %#v", settings)
	}
	list = providerList(t, f)
	if providerByID(t, list, secondID)["active"] != true {
		t.Fatal("provider kedua harus aktif")
	}

	// Hapus provider aktif: provider pertama kembali aktif.
	f.api("DELETE", "/api/providers/"+secondID, nil, 200)
	state = f.state()
	settings, _ = state["settings"].(map[string]any)
	if settings["baseUrl"] == "https://kedua.invalid/v1" {
		t.Fatal("settings masih menunjuk provider yang dihapus")
	}
	list = providerList(t, f)
	if providerByID(t, list, first["id"].(string))["active"] != true {
		t.Fatal("provider pertama harus kembali aktif")
	}

	// Hapus semuanya: gateway ikut kosong.
	f.api("DELETE", "/api/providers/"+first["id"].(string), nil, 200)
	f.api("DELETE", "/api/providers/"+ollamaID, nil, 200)
	if list := providerList(t, f); len(list) != 0 {
		t.Fatalf("provider tersisa = %d", len(list))
	}
	state = f.state()
	settings, _ = state["settings"].(map[string]any)
	if settings["baseUrl"] != "" || settings["hasApiKey"] != false {
		t.Fatalf("settings setelah hapus semua = %#v", settings)
	}
}

func TestProviderMigration(t *testing.T) {
	dir := t.TempDir()
	d, err := loadData(dir)
	if err != nil {
		t.Fatal(err)
	}
	d.Settings.BaseURL = "https://lama.invalid/v1"
	d.Settings.APIKey = "legacy-key-123"
	d.Settings.ProviderName = "Lama"
	d.Settings.GatewayProtocol = "openai-responses"
	raw, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "data.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	d2, err := loadData(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(d2.Providers) != 1 {
		t.Fatalf("providers hasil migrasi = %d", len(d2.Providers))
	}
	p := d2.Providers[0]
	if p.BaseURL != "https://lama.invalid/v1" || p.APIKey != "legacy-key-123" || p.Name != "Lama" || p.Protocol != "openai-responses" {
		t.Fatalf("provider migrasi = %+v", p)
	}
	if d2.Settings.ActiveProviderID != p.ID || d2.Settings.BaseURL != p.BaseURL {
		t.Fatalf("settings tidak sinkron setelah migrasi: %+v", d2.Settings)
	}
	// Muat ulang: migrasi harus idempotent, tidak menduplikasi.
	raw2, err := json.Marshal(d2)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "data.json"), raw2, 0600); err != nil {
		t.Fatal(err)
	}
	d3, err := loadData(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(d3.Providers) != 1 {
		t.Fatalf("providers setelah muat ulang = %d", len(d3.Providers))
	}
}

func TestProviderFetchAndTestModels(t *testing.T) {
	f := setup(t, nil)
	state := f.state()
	settings, _ := state["settings"].(map[string]any)
	baseURL, _ := settings["baseUrl"].(string)

	created := f.api("POST", "/api/providers", object{"name": "Kedua", "baseUrl": baseURL, "apiKey": secret, "protocol": "openai"}, 201)
	id, _ := created["id"].(string)

	// Fetch model dari provider non-aktif: daftar kembali, Connection lama
	// tidak berubah (masih nil karena Tes Koneksi belum pernah jalan).
	fetched := f.api("GET", "/api/providers/"+id+"/models", nil, 200)
	models, ok := fetched["models"].([]any)
	if !ok || len(models) != 6 {
		t.Fatalf("models provider kedua = %#v", fetched["models"])
	}
	if f.state()["connection"] != nil {
		t.Fatal("fetch provider non-aktif tidak boleh mengubah Connection")
	}

	// Aktifkan lalu fetch lagi: Connection ikut terisi seperti Tes Koneksi.
	f.api("POST", "/api/providers/"+id+"/activate", object{}, 200)
	f.api("GET", "/api/providers/"+id+"/models", nil, 200)
	conn, ok := f.state()["connection"].(map[string]any)
	if !ok {
		t.Fatal("connection harus terisi setelah fetch provider aktif")
	}
	if connModels, ok := conn["models"].([]any); !ok || len(connModels) != 6 {
		t.Fatalf("connection models = %#v", conn["models"])
	}

	// Tes massal: sse lolos, denied gagal, echo gagal tanpa membocorkan key.
	// f.api sudah memastikan respons tidak mengandung secret.
	tested := f.api("POST", "/api/providers/"+id+"/test-models", object{"models": []string{"sse", "denied", "echo"}}, 200)
	results, ok := tested["results"].([]any)
	if !ok || len(results) != 3 {
		t.Fatalf("hasil tes = %#v", tested["results"])
	}
	byModel := map[string]map[string]any{}
	for _, item := range results {
		r, _ := item.(map[string]any)
		byModel[r["model"].(string)] = r
	}
	if byModel["sse"]["ok"] != true {
		t.Fatalf("sse harus lolos: %#v", byModel["sse"])
	}
	if reply, _ := byModel["sse"]["reply"].(string); !strings.Contains(reply, "Halo") {
		t.Fatalf("balasan sse = %q", reply)
	}
	if byModel["denied"]["ok"] != false || byModel["denied"]["error"] == nil {
		t.Fatalf("denied harus gagal: %#v", byModel["denied"])
	}
	if byModel["echo"]["ok"] != false {
		t.Fatalf("echo harus gagal: %#v", byModel["echo"])
	}

	// Batas: daftar kosong dan lebih dari 10 model ditolak.
	f.api("POST", "/api/providers/"+id+"/test-models", object{"models": []string{}}, 400)
	many := make([]string, 0, 11)
	for i := 0; i < 11; i++ {
		many = append(many, "sse")
	}
	f.api("POST", "/api/providers/"+id+"/test-models", object{"models": many}, 400)
	f.api("GET", "/api/providers/tidak-ada/models", nil, 404)
	f.api("POST", "/api/providers/tidak-ada/test-models", object{"models": []string{"sse"}}, 404)
}
