package musehood

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func findStateSeat(t *testing.T, state object, code string) object {
	t.Helper()
	seats, ok := state["seats"].([]any)
	if !ok {
		t.Fatal("state seats bukan array")
	}
	for _, item := range seats {
		s, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if s["code"] == code {
			return s
		}
	}
	t.Fatalf("kursi %s tidak ada di state", code)
	return nil
}

func TestCustomPrompt(t *testing.T) {
	app := &App{dir: t.TempDir()}
	d := data{Group: identity{"Musehood", ""}, Owner: owner{identity{"Arman", ""}, true}, Settings: settings{ReviewLevel: "Sedang"}}
	s := seat{identity: identity{"Opus", ""}, Code: "M10", Role: "Perencana"}
	if got := defaultPromptFor(s); got != duties["M10"] {
		t.Fatalf("defaultPromptFor M10 = %q, mau duties", got)
	}
	if got := app.systemPrompt(s, d); !strings.Contains(got, duties["M10"]) {
		t.Fatalf("systemPrompt default tidak memuat duties: %q", got)
	}
	s.Prompt = "  Jawab selalu dalam bentuk tabel.  "
	got := app.systemPrompt(s, d)
	if !strings.Contains(got, "Jawab selalu dalam bentuk tabel.") {
		t.Fatalf("systemPrompt tidak memakai prompt kustom: %q", got)
	}
	if strings.Contains(got, duties["M10"]) {
		t.Fatalf("prompt kustom harus menggantikan duties: %q", got)
	}
	if !strings.Contains(got, "Nama kursi: Opus (M10)") || !strings.Contains(got, "Nama pemilik: Arman") {
		t.Fatalf("identitas kursi harus tetap ada: %q", got)
	}
	s.Prompt = "   "
	if got := app.systemPrompt(s, d); !strings.Contains(got, duties["M10"]) {
		t.Fatalf("prompt kosong harus kembali ke default: %q", got)
	}
	custom := seat{identity: identity{"Nova", ""}, Code: "C01", Role: "Pengamat"}
	if got := defaultPromptFor(custom); got != "Laksanakan peran Pengamat." {
		t.Fatalf("defaultPromptFor kursi custom = %q", got)
	}
	reviewer := seat{identity: identity{"GLM", ""}, Code: "M02", Role: "Peninjau", Prompt: "Periksa dengan teliti."}
	if got := app.systemPrompt(reviewer, d); !strings.Contains(got, "Level Review Sedang") {
		t.Fatalf("M02 harus tetap mendapat Level Review: %q", got)
	}
}

func TestCustomPromptAndProviderSettings(t *testing.T) {
	f := setup(t, nil)
	f.api("POST", "/api/settings", object{
		"settings": object{"providerName": "Xingchi"},
		"seats":    []object{{"code": "M10", "prompt": "Jawab dalam tabel."}, {"code": "M01", "prompt": ""}},
	}, 200)
	state := f.state()
	settings, _ := state["settings"].(map[string]any)
	if settings["providerName"] != "Xingchi" {
		t.Fatalf("providerName = %v", settings["providerName"])
	}
	m10 := findStateSeat(t, state, "M10")
	if m10["prompt"] != "Jawab dalam tabel." {
		t.Fatalf("M10 prompt = %v", m10["prompt"])
	}
	if m10["defaultPrompt"] != duties["M10"] {
		t.Fatalf("M10 defaultPrompt = %v", m10["defaultPrompt"])
	}
	m01 := findStateSeat(t, state, "M01")
	if m01["prompt"] != nil {
		t.Fatalf("M01 prompt harus kosong, dapat %v", m01["prompt"])
	}
	if m01["defaultPrompt"] != duties["M01"] {
		t.Fatalf("M01 defaultPrompt = %v", m01["defaultPrompt"])
	}
	long := strings.Repeat("x", 4001)
	f.api("POST", "/api/settings", object{"seats": []object{{"code": "M10", "prompt": long}}}, 400)
	f.api("POST", "/api/settings", object{"settings": object{"providerName": strings.Repeat("y", 81)}}, 400)
	raw, err := os.ReadFile(filepath.Join(f.dir, "data.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "Jawab dalam tabel.") || !strings.Contains(string(raw), "Xingchi") {
		t.Fatal("prompt kustom / providerName tidak tersimpan di data.json")
	}
	if strings.Contains(string(raw), "defaultPrompt") {
		t.Fatal("defaultPrompt tidak boleh tersimpan di data.json")
	}
}

func TestModelEndpoint(t *testing.T) {
	f := setup(t, nil)
	out := f.api("POST", "/api/test-model", object{"model": "plain"}, 200)
	if out["ok"] != true {
		t.Fatalf("test-model plain ok = %v: %v", out["ok"], out)
	}
	if out["reply"] != "teks polos" {
		t.Fatalf("test-model plain reply = %v", out["reply"])
	}
	if _, ok := out["latencyMs"]; !ok {
		t.Fatal("test-model harus mengembalikan latencyMs")
	}
	out = f.api("POST", "/api/test-model", object{"model": "sse"}, 200)
	if out["reply"] != "Halo dunia" {
		t.Fatalf("test-model sse reply = %v", out["reply"])
	}
	out = f.api("POST", "/api/test-model", object{"model": "missing-from-list"}, 200)
	if out["reply"] != "balasan missing-from-list" {
		t.Fatalf("test-model model di luar daftar tetap diuji: %v", out["reply"])
	}
	f.api("POST", "/api/test-model", object{"model": "denied"}, 401)
	f.api("POST", "/api/test-model", object{"model": ""}, 400)
	f.api("POST", "/api/test-model", object{}, 400)
}
