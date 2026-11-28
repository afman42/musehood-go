package musehood

// Suite skenario agen (28 Nov 2026): rantai tool nyata lewat jalur
// dispatch penuh (runTool) layaknya AI agent sungguhan memakai banyak
// tool dalam satu alur kerja. Gateway dimock (fixture setup), jaringan
// dimock lewat fakeTransport + lookupIP (pola heavy_tools_test.go).

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// scenarioSetup menyiapkan fixture + mock jaringan untuk skenario.
func scenarioSetup(t *testing.T) fixture {
	t.Helper()
	f := setup(t, nil)
	oldClient := toolHTTPClient
	toolHTTPClient = &http.Client{Transport: fakeTransport{}, Timeout: 10 * time.Second}
	t.Cleanup(func() { toolHTTPClient = oldClient })
	oldLookup := lookupIP
	lookupIP = func(ctx context.Context, host string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("93.184.216.34")}, nil
	}
	t.Cleanup(func() { lookupIP = oldLookup })
	return f
}

// (a) Alur coding: create_project -> tulis kode -> cari -> run_tests ->
// perbaiki -> git_commit (disetujui).
func TestScenarioCodingFlow(t *testing.T) {
	f := scenarioSetup(t)
	ctx := ompRoomCtx()

	out := ompRun(t, f, ctx, "create_project", "skenario-kode\ngo")
	if !strings.Contains(out, "skenario-kode") {
		t.Fatalf("create_project = %q", out)
	}
	// Tulis fungsi Tambah + tesnya lewat edit_many.
	out = ompRun(t, f, ctx, "edit_many", "project:skenario-kode\nfile:main.go\n<<<OLD>>>\nfunc main() {\n<<<NEW>>>\nfunc Tambah(a, b int) int { return a - b }\n\nfunc main() {")
	if strings.Contains(out, "Error") {
		t.Fatalf("edit_many main.go = %q", out)
	}
	out = ompRun(t, f, ctx, "edit_many", "project:skenario-kode\nfile:main_test.go\n<<<OLD>>>\n\t// Tes awal: pastikan paket terkompilasi.\n<<<NEW>>>\n\tif Tambah(2, 3) != 5 {\n\t\tt.Fatal(\"Tambah salah\")\n\t}")
	if strings.Contains(out, "Error") {
		t.Fatalf("edit_many main_test.go = %q", out)
	}
	// Cari simbol (ast_grep bila ada, selain itu search_code).
	if findAstGrep() != "" {
		out = ompRun(t, f, ctx, "ast_grep", "project:skenario-kode\nfunc Tambah($$$) int { $$$ }\ngo")
	} else {
		out = ompRun(t, f, ctx, "search_code", "project:skenario-kode\nTambah")
	}
	if !strings.Contains(out, "main.go") {
		t.Fatalf("pencarian simbol = %q", out)
	}
	// Tes harus GAGAL dulu (implementasi a-b salah).
	out = ompRun(t, f, ctx, "run_tests", "project:skenario-kode")
	if !strings.Contains(out, "FAIL") && !strings.Contains(out, "gagal") {
		t.Fatalf("run_tests seharusnya gagal: %q", out)
	}
	// Perbaiki implementasi.
	out = ompRun(t, f, ctx, "edit_many", "project:skenario-kode\nfile:main.go\n<<<OLD>>>\nreturn a - b\n<<<NEW>>>\nreturn a + b")
	if strings.Contains(out, "Error") {
		t.Fatalf("edit perbaikan = %q", out)
	}
	out = ompRun(t, f, ctx, "run_tests", "project:skenario-kode")
	if strings.Contains(out, "FAIL") {
		t.Fatalf("run_tests sesudah perbaikan masih gagal: %q", out)
	}
	// Commit dengan persetujuan pemilik.
	done := make(chan string, 1)
	go func() { done <- ompRun(t, f, ctx, "git_commit", "project:skenario-kode\nfeat: fungsi Tambah") }()
	ompApproveNext(t, f, "")
	if out := <-done; strings.Contains(out, "TIDAK") {
		t.Fatalf("git_commit = %q", out)
	}
}

// (b) Alur riset: web_search -> fetch_url -> (browser_open bila ada)
// -> memory_retain -> memory_recall.
func TestScenarioResearchFlow(t *testing.T) {
	f := scenarioSetup(t)
	ctx := ompRoomCtx()

	out := ompRun(t, f, ctx, "web_search", "berita contoh")
	if !strings.Contains(out, "Berita Contoh") {
		t.Fatalf("web_search = %q", out)
	}
	out = ompRun(t, f, ctx, "fetch_url", "https://example.com/berita")
	if !strings.Contains(out, "Judul Uji") {
		t.Fatalf("fetch_url = %q", out)
	}
	if findChromium() != "" {
		out = ompRun(t, f, ctx, "browser_open", "https://example.com/berita")
		if out == "" {
			t.Fatal("browser_open kosong")
		}
	} else {
		t.Log("browser_open dilewati: Chromium tidak tersedia")
	}
	out = ompRun(t, f, ctx, "memory_retain", "Berita contoh membahas Judul Uji\nriset")
	if !strings.Contains(out, "tersimpan") {
		t.Fatalf("memory_retain = %q", out)
	}
	out = ompRun(t, f, ctx, "memory_recall", "Judul Uji")
	if !strings.Contains(out, "Berita contoh membahas") {
		t.Fatalf("memory_recall = %q", out)
	}
}

// (c) Alur orkestrasi: todo_add -> spawn_subagent -> hub_message ->
// check_subagent -> checkpoint -> rewind.
func TestScenarioOrchestrationFlow(t *testing.T) {
	f := scenarioSetup(t)
	ctx := ompRoomCtx()
	// Pesan awal agar checkpoint punya acuan.
	f.app.mu.Lock()
	f.app.data.Messages = append(f.app.data.Messages, &message{ID: uuid(), CreatedAt: now(), AuthorType: "owner", Text: "mulai orkestrasi", Status: "completed", RoomID: defaultRoomID, BranchID: "main"})
	f.app.mu.Unlock()

	out := ompRun(t, f, ctx, "todo_add", "kerjakan subtugas")
	if !strings.Contains(out, "ditambahkan") {
		t.Fatalf("todo_add = %q", out)
	}
	out = ompRun(t, f, ctx, "spawn_subagent", "seat:M01\nJawab singkat: halo dari subagent")
	if !strings.Contains(out, "Sesi subagent dimulai") {
		t.Fatalf("spawn_subagent = %q", out)
	}
	// Ambil id ruang sub dari data.
	f.app.mu.Lock()
	subID := ""
	for _, r := range f.app.data.Rooms {
		if r.SubagentOf == defaultRoomID {
			subID = r.ID
		}
	}
	f.app.mu.Unlock()
	if subID == "" {
		t.Fatal("ruang subagent tidak ditemukan")
	}
	out = ompRun(t, f, ctx, "hub_message", "M02\nSubagent sedang bekerja, pantau ya.")
	if !strings.Contains(out, "terkirim") {
		t.Fatalf("hub_message = %q", out)
	}
	// Tunggu subagent selesai (gateway mock menjawab).
	deadline := time.Now().Add(30 * time.Second)
	for {
		out = ompRun(t, f, ctx, "check_subagent", subID)
		if strings.Contains(out, "selesai") || strings.Contains(out, "Halo") || strings.Contains(out, "halo") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("check_subagent tidak selesai: %q", out)
		}
		time.Sleep(200 * time.Millisecond)
	}
	out = ompRun(t, f, ctx, "todo_update", "1\ndone")
	if !strings.Contains(out, "done") {
		t.Fatalf("todo_update = %q", out)
	}
	out = ompRun(t, f, ctx, "checkpoint", "sesudah-orkestrasi")
	if !strings.Contains(out, "Checkpoint dibuat") {
		t.Fatalf("checkpoint = %q", out)
	}
	// Rewind dengan persetujuan: pesan hub (sesudah checkpoint awal?)
	// tetap; yang dipotong adalah pesan sesudah checkpoint ini (tidak
	// ada) — cukup pastikan alur approval + hasil jelas.
	done := make(chan string, 1)
	go func() { done <- ompRun(t, f, ctx, "rewind", "sesudah-orkestrasi") }()
	ompApproveNext(t, f, "edit")
	if out := <-done; !strings.Contains(out, "Rewind") {
		t.Fatalf("rewind = %q", out)
	}
}

// (d) Code Mode: execute memanggil >=3 tool berbeda dan menggabungkan.
func TestScenarioCodeMode(t *testing.T) {
	f := scenarioSetup(t)
	ctx := ompRoomCtx()
	ompRun(t, f, ctx, "write_file", "gabung.txt\nisi gabungan")

	js := `
const waktu = await tool("current_time", "");
const daftar = await tool("list_files", "");
const isi = await tool("read_file", "gabung.txt");
console.log("WAKTU:" + waktu.includes("2026"));
console.log("ADA:" + daftar.includes("gabung.txt"));
console.log("ISI:" + isi.includes("isi gabungan"));
`
	out := ompRun(t, f, ctx, "execute", js)
	if !strings.Contains(out, "ADA:true") || !strings.Contains(out, "ISI:true") || !strings.Contains(out, "WAKTU:true") {
		t.Fatalf("execute gabungan = %q", out)
	}
}

// (e) Plan mode: Write ditolak -> rencana disetujui (mode mati) -> jalan.
func TestScenarioPlanMode(t *testing.T) {
	f := scenarioSetup(t)
	ctx := ompRoomCtx()
	f.api("POST", "/api/rooms/"+defaultRoomID, map[string]any{"planMode": true}, 200)
	out := ompRun(t, f, ctx, "write_file", "rencana.txt\nisi")
	if !strings.Contains(out, "plan mode") {
		t.Fatalf("write saat plan mode = %q", out)
	}
	out = ompRun(t, f, ctx, "read_file", "tidak-ada.txt")
	if strings.Contains(out, "plan mode") {
		t.Fatalf("read tidak boleh diblokir plan mode: %q", out)
	}
	// Pemilik menyetujui rencana = mematikan plan mode.
	f.api("POST", "/api/rooms/"+defaultRoomID, map[string]any{"planMode": false}, 200)
	out = ompRun(t, f, ctx, "write_file", "rencana.txt\nisi")
	if !strings.Contains(out, "rencana.txt") {
		t.Fatalf("write sesudah plan disetujui = %q", out)
	}
}

// (f) MCP dalam rantai: tool MCP palsu dipanggil sesudah tool lain dan
// dari dalam execute.
func TestScenarioMCPChain(t *testing.T) {
	f := scenarioSetup(t)
	ctx := ompRoomCtx()
	script := filepath.Join(t.TempDir(), "fake_mcp.py")
	if err := os.WriteFile(script, []byte(fakeMCPServer), 0o644); err != nil {
		t.Fatal(err)
	}
	f.api("POST", "/api/mcp-servers", object{"name": "uji", "command": "python3", "args": []string{script}}, 200)
	waitFor(t, "tool MCP muncul", func() bool {
		tools := f.api("GET", "/api/tools", nil, 200)
		return strings.Contains(fmt.Sprint(tools["mcp"]), "mcp_uji_echo_text")
	})
	// Tool lain dulu, lalu MCP (dengan persetujuan).
	ompRun(t, f, ctx, "current_time", "")
	done := make(chan string, 1)
	go func() { done <- ompRun(t, f, ctx, "mcp_uji_echo_text", `{"text":"rantai"}`) }()
	ompApproveNext(t, f, "mcp")
	if out := <-done; !strings.Contains(out, "ECHO:rantai") {
		t.Fatalf("MCP langsung = %q", out)
	}
	// MCP dari dalam execute.
	go func() {
		done <- ompRun(t, f, ctx, "execute", `const r = await tool("mcp_uji_echo_text", "{\"text\":\"via-execute\"}"); console.log(r);`)
	}()
	ompApproveNext(t, f, "mcp")
	if out := <-done; !strings.Contains(out, "ECHO:via-execute") {
		t.Fatalf("MCP via execute = %q", out)
	}
}

// (g) eval persisten: state antar panggilan.
func TestScenarioEvalPersistence(t *testing.T) {
	f := scenarioSetup(t)
	ctx := ompRoomCtx()
	out := ompRun(t, f, ctx, "eval", "python\ndaftar = [1, 2]")
	if strings.Contains(out, "[error]") {
		t.Fatalf("eval 1 = %q", out)
	}
	out = ompRun(t, f, ctx, "eval", "python\ndaftar.append(3)\nprint(sum(daftar))")
	if !strings.Contains(out, "6") {
		t.Fatalf("eval 2 = %q", out)
	}
}

// (h) Kebijakan per tool: deny menolak, prompt memunculkan approval.
func TestScenarioToolPolicy(t *testing.T) {
	f := scenarioSetup(t)
	ctx := ompRoomCtx()
	f.api("POST", "/api/settings", map[string]any{"settings": map[string]any{
		"toolPolicies": map[string]any{"password_gen": "deny", "current_time": "prompt"},
	}}, 200)
	out := ompRun(t, f, ctx, "password_gen", "12")
	if !strings.Contains(out, "dilarang") {
		t.Fatalf("deny = %q", out)
	}
	done := make(chan string, 1)
	go func() { done <- ompRun(t, f, ctx, "current_time", "") }()
	ompApproveNext(t, f, "tool")
	if out := <-done; !strings.Contains(out, "2026") {
		t.Fatalf("prompt sesudah approve = %q", out)
	}
}
