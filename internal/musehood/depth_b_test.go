package musehood

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestResponseCache(t *testing.T) {
	var calls atomic.Int32
	extra := func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" && r.URL.Path == "/v1/chat/completions" {
			calls.Add(1)
			io.WriteString(w, `{"choices":[{"message":{"content":"jawaban tetap"}}]}`)
			return
		}
		http.NotFound(w, r)
	}
	f := setup(t, extra)
	f.api("POST", "/api/settings", object{"settings": object{"responseCacheEnabled": true}}, 200)

	f.app.mu.Lock()
	cfg := f.app.data.Settings
	f.app.mu.Unlock()
	model := "array"
	s := seat{Code: "M01", Name: "Plamo", Provider: "gateway", Model: &model}
	history := []gatewayMessage{{"system", "sistem"}, {"user", "halo"}}

	text, err := f.app.chat(context.Background(), cfg, s, history)
	if err != nil || text != "jawaban tetap" {
		t.Fatalf("chat pertama = %q, %v", text, err)
	}
	if calls.Load() != 1 {
		t.Fatalf("gateway harus dipukul 1x, dapat %d", calls.Load())
	}
	text, err = f.app.chat(context.Background(), cfg, s, history)
	if err != nil || text != "jawaban tetap" {
		t.Fatalf("chat kedua (cache) = %q, %v", text, err)
	}
	if calls.Load() != 1 {
		t.Fatalf("panggilan identik kedua harus dari cache, gateway dipukul %d", calls.Load())
	}
	// Riwayat berbeda tidak memakai cache.
	if _, err := f.app.chat(context.Background(), cfg, s, []gatewayMessage{{"system", "sistem"}, {"user", "lain"}}); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 {
		t.Fatalf("riwayat berbeda harus memukul gateway, total %d", calls.Load())
	}
	stats := f.api("GET", "/api/stats", nil, 200)
	if stats["cacheHits"] != float64(1) {
		t.Fatalf("cacheHits harus 1: %v", stats)
	}
	// Cache dimatikan: panggilan identik memukul gateway lagi.
	f.api("POST", "/api/settings", object{"settings": object{"responseCacheEnabled": false}}, 200)
	if _, err := f.app.chat(context.Background(), cfg, s, history); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 3 {
		t.Fatalf("cache mati harus memukul gateway, total %d", calls.Load())
	}
}

func providerID(t *testing.T, f fixture) string {
	t.Helper()
	out := f.api("GET", "/api/providers", nil, 200)
	providers, _ := out["providers"].([]any)
	if len(providers) == 0 {
		t.Fatal("tidak ada provider")
	}
	first, _ := providers[0].(map[string]any)
	id, _ := first["id"].(string)
	if id == "" {
		t.Fatalf("id provider kosong: %v", first)
	}
	return id
}

func TestProviderHealth(t *testing.T) {
	f := setup(t, nil)
	id := providerID(t, f)
	out := f.api("POST", "/api/providers/"+id+"/health", object{}, 200)
	health, _ := out["health"].(map[string]any)
	if health["ok"] != true {
		t.Fatalf("health harus ok: %v", out)
	}
	if _, ok := health["latencyMs"]; !ok {
		t.Fatalf("latencyMs hilang: %v", health)
	}
	providers := f.api("GET", "/api/providers", nil, 200)
	list, _ := providers["providers"].([]any)
	first, _ := list[0].(map[string]any)
	viewHealth, _ := first["health"].(map[string]any)
	if viewHealth["ok"] != true {
		t.Fatalf("health harus tampil di provider view: %v", first)
	}
	f.api("POST", "/api/providers/tidak-ada/health", object{}, 404)
}

func TestLatencyTestOrder(t *testing.T) {
	delays := map[string]time.Duration{"lambat": 120 * time.Millisecond, "sedang": 60 * time.Millisecond, "cepat": 5 * time.Millisecond}
	extra := func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "GET /v1/models":
			io.WriteString(w, `{"data":[{"id":"lambat"},{"id":"cepat"},{"id":"sedang"}]}`)
		case "POST /v1/chat/completions":
			var body struct{ Model string }
			json.NewDecoder(r.Body).Decode(&body)
			time.Sleep(delays[body.Model])
			io.WriteString(w, `{"choices":[{"message":{"content":"OK"}}]}`)
		default:
			http.NotFound(w, r)
		}
	}
	f := setup(t, extra)
	id := providerID(t, f)
	out := f.api("POST", "/api/providers/"+id+"/latency-test", object{}, 200)
	results, _ := out["results"].([]any)
	if len(results) != 3 {
		t.Fatalf("hasil harus 3 model: %v", out)
	}
	models := []string{}
	for _, item := range results {
		entry, _ := item.(map[string]any)
		if entry["ok"] != true {
			t.Fatalf("model harus ok: %v", entry)
		}
		models = append(models, entry["model"].(string))
	}
	if models[0] != "cepat" || models[1] != "sedang" || models[2] != "lambat" {
		t.Fatalf("urutan tercepat salah: %v", models)
	}
}

func TestSkillInstall(t *testing.T) {
	content := "---\nname: skill-uji\ndescription: Skill untuk pengujian\n---\nIsi skill uji."
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, content)
	}))
	defer server.Close()

	oldFetch := skillFetchURL
	skillFetchURL = func(ctx context.Context, rawURL string) ([]byte, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
		if err != nil {
			return nil, err
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		return io.ReadAll(resp.Body)
	}
	defer func() { skillFetchURL = oldFetch }()

	oldClone := skillGitClone
	skillGitClone = func(ctx context.Context, rawURL, dir string) error {
		sub := filepath.Join(dir, "paket")
		if err := os.MkdirAll(sub, 0o755); err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(sub, "SKILL.md"), []byte("---\nname: skill-repo\ndescription: Dari repo\n---\nIsi repo."), 0o600)
	}
	defer func() { skillGitClone = oldClone }()

	f := setup(t, nil)
	out := f.api("POST", "/api/skills/install", object{"url": server.URL + "/skill.md"}, 200)
	if out["name"] != "skill-uji" {
		t.Fatalf("nama skill = %v", out)
	}
	skills := f.api("GET", "/api/skills", nil, 200)
	found := false
	for _, item := range skills["skills"].([]any) {
		skill, _ := item.(map[string]any)
		if skill["name"] == "skill-uji" && skill["source"] == "lokal" {
			found = true
		}
	}
	if !found {
		t.Fatalf("skill terpasang tidak muncul: %v", skills)
	}
	// Duplikat ditolak 409, timpa diterima.
	f.api("POST", "/api/skills/install", object{"url": server.URL + "/skill.md"}, 409)
	f.api("POST", "/api/skills/install", object{"url": server.URL + "/skill.md", "overwrite": true}, 200)
	// Jalur repo git.
	out = f.api("POST", "/api/skills/install", object{"url": "https://example.com/repo-skill.git"}, 200)
	if out["name"] != "skill-repo" {
		t.Fatalf("nama skill repo = %v", out)
	}
	if _, err := os.Stat(filepath.Join(f.dir, "skills", "skill-repo", "SKILL.md")); err != nil {
		t.Fatal(err)
	}
}
