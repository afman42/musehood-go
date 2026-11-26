package musehood

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeSkill(t *testing.T, app *App, dir, content string) {
	t.Helper()
	full := filepath.Join(app.skillsDir(), dir)
	if err := os.MkdirAll(full, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(full, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestParseSkill(t *testing.T) {
	skill := parseSkill("cadangan", "---\nname: gaya-singkat\ndescription: Jawab singkat\n---\n\nSelalu jawab maksimal dua kalimat.\n")
	if skill.Name != "gaya-singkat" || skill.Description != "Jawab singkat" || !strings.Contains(skill.Body, "dua kalimat") {
		t.Fatalf("parse = %+v", skill)
	}
	plain := parseSkill("polos", "Isi tanpa frontmatter.")
	if plain.Name != "polos" || plain.Body != "Isi tanpa frontmatter." {
		t.Fatalf("parse polos = %+v", plain)
	}
}

func TestLoadSkillsAndPrompt(t *testing.T) {
	app := &App{dir: t.TempDir()}
	writeSkill(t, app, "gaya-singkat", "---\nname: gaya-singkat\ndescription: Jawab singkat\n---\nSelalu jawab maksimal dua kalimat.")
	if err := os.WriteFile(filepath.Join(app.skillsDir(), "bebas.md"), []byte("Skill file tunggal."), 0o644); err != nil {
		t.Fatal(err)
	}

	skills := app.loadSkills()
	if len(skills) != 2 {
		t.Fatalf("jumlah skill = %d (%+v)", len(skills), skills)
	}

	s := seat{Skills: []string{"gaya-singkat", "tidak-ada"}}
	prompt := app.skillsPrompt(s)
	if !strings.Contains(prompt, "## Skill: gaya-singkat") || !strings.Contains(prompt, "dua kalimat") {
		t.Fatalf("prompt skill: %q", prompt)
	}
	if strings.Contains(prompt, "Skill file tunggal") {
		t.Fatalf("skill nonaktif tidak boleh masuk: %q", prompt)
	}
	if got := app.skillsPrompt(seat{}); got != "" {
		t.Fatalf("tanpa skill harus kosong: %q", got)
	}

	// systemPrompt memuat skill aktif.
	full := app.systemPrompt(seat{identity: identity{"Plamo", ""}, Code: "M01", Role: "Pelaksana", Skills: []string{"bebas"}}, data{Group: identity{"G", ""}, Owner: owner{identity{"Arman", ""}, true}})
	if !strings.Contains(full, "Skill file tunggal") {
		t.Fatalf("systemPrompt tanpa skill: %q", full)
	}
}

func TestSkillsEnabledGate(t *testing.T) {
	app := &App{dir: t.TempDir()}
	writeSkill(t, app, "gaya-singkat", "---\nname: gaya-singkat\ndescription: Jawab singkat\n---\nSelalu jawab maksimal dua kalimat.")
	s := seat{identity: identity{"Plamo", ""}, Code: "M01", Role: "Pelaksana", Skills: []string{"gaya-singkat"}}
	base := data{Group: identity{"G", ""}, Owner: owner{identity{"Arman", ""}, true}}

	// Nil = aktif (bawaan).
	if got := app.systemPrompt(s, base); !strings.Contains(got, "dua kalimat") {
		t.Fatalf("skillsEnabled nil harus aktif: %q", got)
	}
	// Mati: skill tidak disisipkan meski kursi memilihnya.
	off := false
	disabled := base
	disabled.Settings.SkillsEnabled = &off
	if got := app.systemPrompt(s, disabled); strings.Contains(got, "dua kalimat") {
		t.Fatalf("skillsEnabled=false harus menonaktifkan injeksi: %q", got)
	}
	// Nyala eksplisit: kembali disisipkan.
	on := true
	enabled := base
	enabled.Settings.SkillsEnabled = &on
	if got := app.systemPrompt(s, enabled); !strings.Contains(got, "dua kalimat") {
		t.Fatalf("skillsEnabled=true harus menyisipkan: %q", got)
	}
}

func writeCustomTool(t *testing.T, app *App, file, content string) {
	t.Helper()
	if err := os.MkdirAll(app.toolsDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(app.toolsDir(), file), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadCustomToolsValidation(t *testing.T) {
	app := &App{dir: t.TempDir()}
	writeCustomTool(t, app, "cuaca.json", `{"name":"cuaca","description":"Cuaca kota","argHint":"nama kota","type":"http_get","url":"https://wttr.in/{{kota}}?format=3","params":["kota"]}`)
	writeCustomTool(t, app, "tabrakan.json", `{"name":"web_search","description":"x","type":"http_get","url":"https://example.com"}`)
	writeCustomTool(t, app, "rusak.json", `{"name":"Rusak Nama","type":"http_get","url":"https://example.com"}`)
	writeCustomTool(t, app, "tanpa-url.json", `{"name":"tanpa_url","type":"http_get"}`)

	tools := app.loadCustomTools()
	if len(tools) != 1 || tools[0].Name != "cuaca" {
		t.Fatalf("tools = %+v", tools)
	}
	if _, ok := app.findCustomTool("cuaca"); !ok {
		t.Fatal("cuaca harus ditemukan")
	}
	// Instruksi memuat tool kustom.
	instructions := app.buildToolInstructions(false)
	if !strings.Contains(instructions, "- cuaca: Cuaca kota. Argumen: nama kota.") {
		t.Fatalf("instruksi tanpa tool kustom: %q", instructions[len(instructions)-200:])
	}
}

func TestCustomToolHTTPGet(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("cuaca untuk " + r.URL.Path))
	}))
	defer server.Close()

	tool := customTool{Name: "uji", Type: "http_get", URL: server.URL + "/{{kota}}", Params: []string{"kota"}}
	finalURL := substituteParams(tool.URL, tool, "Pontianak", true)
	text, err := fetchCustomURL(context.Background(), finalURL, "")
	if err != nil || text != "cuaca untuk /Pontianak" {
		t.Fatalf("fetch = %q, %v", text, err)
	}
	// SSRF: URL localhost ditolak di runCustomTool.
	app := &App{dir: t.TempDir()}
	result := app.runCustomTool(context.Background(), seat{Code: "M01"}, tool, "Pontianak", false)
	if !strings.Contains(result, "SSRF") {
		t.Fatalf("localhost harus ditolak SSRF: %s", result)
	}
}

func TestCustomToolShellApproval(t *testing.T) {
	app := &App{dir: t.TempDir(), approvals: map[string]*approval{}}
	tool := customTool{Name: "sapa", Type: "shell", Command: "echo halo-{{arg}}"}

	// Tanpa toggle coding: ditolak langsung.
	if result := app.runCustomTool(context.Background(), seat{Code: "M01", Name: "Plamo"}, tool, "dunia", false); !strings.Contains(result, "dimatikan") {
		t.Fatalf("shell tanpa coding harus ditolak: %s", result)
	}

	// Dengan persetujuan: perintah jalan di sandbox.
	go func() {
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) {
			app.approvalMu.Lock()
			for id := range app.approvals {
				app.approvalMu.Unlock()
				app.decideApproval(id, true)
				return
			}
			app.approvalMu.Unlock()
			time.Sleep(10 * time.Millisecond)
		}
	}()
	result := app.runCustomTool(context.Background(), seat{Code: "M01", Name: "Plamo"}, tool, "dunia", true)
	if !strings.Contains(result, "halo-dunia") {
		t.Fatalf("shell kustom: %s", result)
	}
}
