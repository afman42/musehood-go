package musehood

// Tes unit 13 item gaya omp (28 Nov 2026): ast-grep, LSP (fake server),
// eval persisten, debug DAP (fake server), browser CDP, GitHub (server
// lokal), security_scan, hub, checkpoint/rewind, memori durable + learn,
// todo, workflow, kebijakan per tool + katalog on-demand.

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ompSeat adalah kursi uji standar untuk tes omp.
func ompSeat() seat {
	model := "plain"
	return seat{Code: "M01", Name: "Plamo", Model: &model}
}

// ompRun menjalankan tool lewat jalur dispatch penuh (runTool).
func ompRun(t *testing.T, f fixture, ctx context.Context, name, arg string) string {
	t.Helper()
	f.app.mu.Lock()
	cfg := f.app.data.Settings
	f.app.mu.Unlock()
	out, _ := f.app.runTool(ctx, cfg, ompSeat(), name, arg, true)
	return out
}

func ompRoomCtx() context.Context {
	return context.WithValue(context.Background(), toolRunInfoKey{}, toolRunInfo{RoomID: defaultRoomID})
}

// ompApproveNext menunggu persetujuan berikutnya lalu menyetujuinya.
func ompApproveNext(t *testing.T, f fixture, kind string) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for {
		pending := f.app.pendingApprovals()
		if len(pending) > 0 {
			if kind != "" && pending[0].Kind != kind {
				t.Fatalf("kind persetujuan = %q, mau %q", pending[0].Kind, kind)
			}
			f.app.decideApproval(pending[0].ID, true)
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("persetujuan tidak muncul")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func ompWriteProjectFile(t *testing.T, f fixture, project, rel, content string) {
	t.Helper()
	dir, err := f.app.ensureProject(project)
	if err != nil {
		t.Fatal(err)
	}
	full := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// --- 1. ast-grep ---

func TestOmpASTGrepEdit(t *testing.T) {
	if findAstGrep() == "" {
		t.Skip("ast-grep tidak terpasang")
	}
	f := setup(t, nil)
	ctx := ompRoomCtx()
	ompWriteProjectFile(t, f, "ast-uji", "main.go", "package main\n\nfunc halo() {}\n\nfunc main() { halo() }\n")

	out := ompRun(t, f, ctx, "ast_grep", "project:ast-uji\nfunc $NAME() { $$$ }\ngo")
	if !strings.Contains(out, "main.go") || !strings.Contains(out, "halo") {
		t.Fatalf("ast_grep = %q", out)
	}
	// Pratinjau tidak mengubah file.
	out = ompRun(t, f, ctx, "ast_edit", "project:ast-uji\nfunc halo() {}\nfunc halo() { println(\"x\") }\ngo")
	if !strings.Contains(out, "Pratinjau") {
		t.Fatalf("ast_edit pratinjau = %q", out)
	}
	dir, _ := f.app.ensureProject("ast-uji")
	raw, _ := os.ReadFile(filepath.Join(dir, "main.go"))
	if strings.Contains(string(raw), "println") {
		t.Fatal("pratinjau ast_edit tidak boleh mengubah file")
	}
	// Apply dengan persetujuan mengubah file.
	done := make(chan string, 1)
	go func() { done <- ompRun(t, f, ctx, "ast_edit", "project:ast-uji\nfunc halo() {}\nfunc halo() { println(\"x\") }\ngo\napply") }()
	ompApproveNext(t, f, "edit")
	if out := <-done; !strings.Contains(out, "diterapkan") {
		t.Fatalf("ast_edit apply = %q", out)
	}
	raw, _ = os.ReadFile(filepath.Join(dir, "main.go"))
	if !strings.Contains(string(raw), "println") {
		t.Fatalf("apply tidak mengubah file: %s", raw)
	}
	// Tanpa binary: degradasi jelas (env menunjuk file tak ada + PATH kosong tidak mudah;
	// cukup verifikasi pesan format untuk pola kosong).
	if out := ompRun(t, f, ctx, "ast_grep", "project:ast-uji\n"); !strings.Contains(out, "Error") {
		t.Fatalf("ast_grep pola kosong = %q", out)
	}
}

// --- 2. LSP dengan server palsu ---

const fakeLSPServer = `import sys, json

def read_msg():
    length = 0
    while True:
        line = sys.stdin.buffer.readline().decode().strip()
        if line == "":
            break
        if line.lower().startswith("content-length:"):
            length = int(line.split(":")[1])
    return json.loads(sys.stdin.buffer.read(length))

def send(obj):
    raw = json.dumps(obj).encode()
    sys.stdout.buffer.write(b"Content-Length: %d\r\n\r\n" % len(raw) + raw)
    sys.stdout.buffer.flush()

uri = ""
while True:
    try:
        msg = read_msg()
    except Exception:
        break
    method = msg.get("method", "")
    mid = msg.get("id")
    if method == "initialize":
        send({"jsonrpc": "2.0", "id": mid, "result": {"capabilities": {}}})
    elif method == "textDocument/didOpen":
        uri = msg["params"]["textDocument"]["uri"]
        send({"jsonrpc": "2.0", "method": "textDocument/publishDiagnostics", "params": {"uri": uri, "diagnostics": [
            {"range": {"start": {"line": 2, "character": 0}, "end": {"line": 2, "character": 5}}, "severity": 1, "message": "simbol tidak dikenal (palsu)"}]}})
    elif method == "textDocument/definition":
        send({"jsonrpc": "2.0", "id": mid, "result": [{"uri": uri, "range": {"start": {"line": 2, "character": 5}, "end": {"line": 2, "character": 9}}}]})
    elif method == "textDocument/references":
        send({"jsonrpc": "2.0", "id": mid, "result": [
            {"uri": uri, "range": {"start": {"line": 2, "character": 5}, "end": {"line": 2, "character": 9}}},
            {"uri": uri, "range": {"start": {"line": 4, "character": 14}, "end": {"line": 4, "character": 18}}}]})
    elif method == "textDocument/rename":
        send({"jsonrpc": "2.0", "id": mid, "result": {"changes": {uri: [
            {"range": {"start": {"line": 2, "character": 5}, "end": {"line": 2, "character": 9}}, "newText": msg["params"]["newName"]},
            {"range": {"start": {"line": 4, "character": 14}, "end": {"line": 4, "character": 18}}, "newText": msg["params"]["newName"]}]}}})
    elif method == "shutdown":
        send({"jsonrpc": "2.0", "id": mid, "result": None})
    elif method == "exit":
        break
`

func TestOmpLSPFakeServer(t *testing.T) {
	f := setup(t, nil)
	ctx := ompRoomCtx()
	script := filepath.Join(t.TempDir(), "fake_lsp.py")
	if err := os.WriteFile(script, []byte(fakeLSPServer), 0o600); err != nil {
		t.Fatal(err)
	}
	ompWriteProjectFile(t, f, "lsp-uji", "main.go", "package main\n\nfunc halo() {}\n\nfunc main() { halo() }\n")
	f.api("POST", "/api/settings", map[string]any{"settings": map[string]any{
		"lspServers": map[string]any{"go": "python3 " + script},
	}}, 200)

	out := ompRun(t, f, ctx, "lsp_definition", "project:lsp-uji\nmain.go\n5\n14")
	if !strings.Contains(out, "main.go:3:6") {
		t.Fatalf("lsp_definition = %q", out)
	}
	out = ompRun(t, f, ctx, "lsp_references", "project:lsp-uji\nmain.go\n3\n6")
	if !strings.Contains(out, "main.go:3:6") || !strings.Contains(out, "main.go:5:15") {
		t.Fatalf("lsp_references = %q", out)
	}
	out = ompRun(t, f, ctx, "lsp_diagnostics", "project:lsp-uji\nmain.go")
	if !strings.Contains(out, "simbol tidak dikenal") {
		t.Fatalf("lsp_diagnostics = %q", out)
	}
	// Rename diterapkan setelah persetujuan.
	done := make(chan string, 1)
	go func() { done <- ompRun(t, f, ctx, "lsp_rename", "project:lsp-uji\nmain.go\n3\n6\nmenyapa") }()
	ompApproveNext(t, f, "edit")
	if out := <-done; !strings.Contains(out, "Rename diterapkan") {
		t.Fatalf("lsp_rename = %q", out)
	}
	dir, _ := f.app.ensureProject("lsp-uji")
	raw, _ := os.ReadFile(filepath.Join(dir, "main.go"))
	if !strings.Contains(string(raw), "func menyapa()") || !strings.Contains(string(raw), "menyapa() }") {
		t.Fatalf("rename tidak diterapkan: %s", raw)
	}
	// Bahasa tanpa server: pesan jelas.
	ompWriteProjectFile(t, f, "lsp-uji", "x.rs", "fn main() {}\n")
	out = ompRun(t, f, ctx, "lsp_definition", "project:lsp-uji\nx.rs\n1\n4")
	if !strings.Contains(out, "language server") && !strings.Contains(out, "tidak ditemukan") {
		t.Fatalf("lsp tanpa server = %q", out)
	}
}

// --- 3. eval persisten ---

func TestOmpEvalPersistence(t *testing.T) {
	f := setup(t, nil)
	ctx := ompRoomCtx()
	out := ompRun(t, f, ctx, "eval", "python\nnilai = 7")
	if strings.Contains(out, "[error]") {
		t.Fatalf("eval 1 = %q", out)
	}
	out = ompRun(t, f, ctx, "eval", "python\nprint(nilai * 6)")
	if !strings.Contains(out, "42") {
		t.Fatalf("eval 2 (state) = %q", out)
	}
	out = ompRun(t, f, ctx, "eval", "javascript\nlet a = 3")
	if strings.Contains(out, "[error]") {
		t.Fatalf("eval js 1 = %q", out)
	}
	out = ompRun(t, f, ctx, "eval", "javascript\na * 2")
	if !strings.Contains(out, "6") {
		t.Fatalf("eval js 2 (state) = %q", out)
	}
	out = ompRun(t, f, ctx, "eval", "python\nreset")
	if !strings.Contains(out, "direset") {
		t.Fatalf("eval reset = %q", out)
	}
	out = ompRun(t, f, ctx, "eval", "python\nprint(nilai)")
	if !strings.Contains(out, "[error]") {
		t.Fatalf("eval sesudah reset harus error: %q", out)
	}
}

// --- 4. Debug DAP dengan server palsu ---

const fakeDAPServer = `#!/usr/bin/env python3
import sys, json, socket

port = 0
for arg in sys.argv:
    if arg.startswith("--listen="):
        port = int(arg.split(":")[-1])
srv = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
srv.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
srv.bind(("127.0.0.1", port))
srv.listen(1)
conn, _ = srv.accept()
f = conn.makefile("rwb")
seq = 0

def read_msg():
    length = 0
    while True:
        line = f.readline().decode().strip()
        if line == "":
            break
        if line.lower().startswith("content-length:"):
            length = int(line.split(":")[1])
    return json.loads(f.read(length))

def send(obj):
    global seq
    seq += 1
    obj["seq"] = seq
    raw = json.dumps(obj).encode()
    f.write(b"Content-Length: %d\r\n\r\n" % len(raw) + raw)
    f.flush()

def respond(req, body=None):
    send({"type": "response", "request_seq": req["seq"], "command": req["command"], "success": True, "body": body or {}})

while True:
    try:
        msg = read_msg()
    except Exception:
        break
    cmd = msg.get("command", "")
    if cmd == "initialize":
        respond(msg, {"supportsConfigurationDoneRequest": True})
    elif cmd == "launch":
        send({"type": "event", "event": "output", "body": {"category": "stdout", "output": "Building palsu\n"}})
        send({"type": "event", "event": "initialized", "body": {}})
        respond(msg)
    elif cmd == "setBreakpoints":
        bps = [{"verified": True, "line": b["line"]} for b in msg["arguments"].get("breakpoints", [])]
        respond(msg, {"breakpoints": bps})
    elif cmd == "configurationDone":
        respond(msg)
        send({"type": "event", "event": "stopped", "body": {"reason": "entry", "threadId": 1}})
    elif cmd in ("continue", "next", "stepIn", "stepOut"):
        respond(msg, {"allThreadsContinued": True})
        send({"type": "event", "event": "stopped", "body": {"reason": "breakpoint", "threadId": 1}})
    elif cmd == "stackTrace":
        respond(msg, {"stackFrames": [{"id": 1, "name": "main.main", "line": 6, "source": {"path": msg.get("arguments", {}).get("path", "/tmp/main.go")}}]})
    elif cmd == "scopes":
        respond(msg, {"scopes": [{"name": "Locals", "variablesReference": 1000}]})
    elif cmd == "variables":
        respond(msg, {"variables": [{"name": "x", "value": "42"}]})
    elif cmd == "disconnect":
        respond(msg)
        break
`

func TestOmpDebugFakeDAP(t *testing.T) {
	f := setup(t, nil)
	ctx := ompRoomCtx()
	script := filepath.Join(t.TempDir(), "fake-dlv")
	if err := os.WriteFile(script, []byte(fakeDAPServer), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MUSEHOOD_DLV", script)
	ompWriteProjectFile(t, f, "debug-uji", "main.go", "package main\n\nfunc main() {}\n")

	out := ompRun(t, f, ctx, "debug_launch", "project:debug-uji\n.")
	if !strings.Contains(out, "Sesi debug dimulai") {
		t.Fatalf("debug_launch = %q", out)
	}
	out = ompRun(t, f, ctx, "debug_breakpoint", "project:debug-uji\nmain.go\n3")
	if !strings.Contains(out, "terverifikasi") {
		t.Fatalf("debug_breakpoint = %q", out)
	}
	out = ompRun(t, f, ctx, "debug_step", "continue")
	if !strings.Contains(out, "berhenti") {
		t.Fatalf("debug_step = %q", out)
	}
	out = ompRun(t, f, ctx, "debug_inspect", "")
	if !strings.Contains(out, "x = 42") {
		t.Fatalf("debug_inspect = %q", out)
	}
	out = ompRun(t, f, ctx, "debug_stop", "")
	if !strings.Contains(out, "dihentikan") {
		t.Fatalf("debug_stop = %q", out)
	}
}

// --- 5. Browser CDP ---

func TestOmpBrowserCDP(t *testing.T) {
	if findChromium() == "" {
		t.Skip("Chromium tidak tersedia")
	}
	f := setup(t, nil)
	ctx := ompRoomCtx()
	page := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<html><head><title>Halaman CDP</title></head><body><h1>Judul CDP</h1><input id="nama"><button id="tombol">Klik Saya</button></body></html>`)
	}))
	defer page.Close()

	// Penjaga SSRF menolak loopback di jalur tool.
	out := ompRun(t, f, ctx, "browser_snapshot", page.URL)
	if !strings.Contains(out, "Error") {
		t.Fatalf("browser_snapshot loopback harus ditolak: %q", out)
	}
	// Plumbing CDP langsung (tanpa penjaga tool) terhadap server lokal.
	browser, err := startCDPBrowser()
	if err != nil {
		t.Skip("CDP tidak bisa dimulai: " + err.Error())
	}
	defer stopCDPBrowser()
	target, err := browser.tabForURL(page.URL)
	if err != nil {
		t.Fatal(err)
	}
	value, err := browser.evaluate(target, page.URL, `document.title`)
	if err != nil || value != "Halaman CDP" {
		t.Fatalf("evaluate title = %v, %v", value, err)
	}
	value, err = browser.evaluate(target, "", `(() => { const el = document.querySelector("#nama"); el.focus(); el.value = "Arman"; return el.value; })()`)
	if err != nil {
		// Satu kali coba lagi: Chromium bisa putus sesaat di bawah beban.
		value, err = browser.evaluate(target, "", `(() => { const el = document.querySelector("#nama"); el.focus(); el.value = "Arman"; return el.value; })()`)
	}
	if err != nil || value != "Arman" {
		t.Fatalf("evaluate type = %v, %v", value, err)
	}
	// browser_tabs lewat tool (dengan coba ulang: tab bisa hilang sesaat
	// bila renderer Chromium crash di bawah beban).
	out = ""
	for i := 0; i < 6; i++ {
		out = ompRun(t, f, ctx, "browser_tabs", "list")
		if strings.Contains(out, "Halaman CDP") {
			break
		}
		if newTarget, err := browser.tabForURL(page.URL); err == nil {
			target = newTarget
			_, _ = browser.evaluate(target, page.URL, "document.title")
		}
		time.Sleep(500 * time.Millisecond)
	}
	if !strings.Contains(out, "Halaman CDP") {
		t.Fatalf("browser_tabs list = %q", out)
	}
}

// --- 6. GitHub via server lokal ---

func TestOmpGitHubTools(t *testing.T) {
	f := setup(t, nil)
	ctx := ompRoomCtx()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/repos/o/r":
			fmt.Fprint(w, `{"full_name":"o/r","description":"Repo uji","private":false,"default_branch":"main","stargazers_count":7,"forks_count":2,"open_issues_count":1,"language":"Go","html_url":"https://github.com/o/r","pushed_at":"2026-11-28T00:00:00Z"}`)
		case r.URL.Path == "/repos/o/r/contents/README.md":
			fmt.Fprint(w, `{"content":"IyBSZWFkbWUgVWpp\n","encoding":"base64","size":12}`)
		case r.URL.Path == "/search/repositories":
			fmt.Fprint(w, `{"total_count":1,"items":[{"full_name":"o/r","description":"Repo uji","html_url":"https://github.com/o/r"}]}`)
		case r.URL.Path == "/repos/o/r/actions/runs":
			fmt.Fprint(w, `{"workflow_runs":[{"id":99,"name":"CI","status":"completed","conclusion":"success","head_branch":"main","html_url":"https://github.com/o/r/actions/runs/99"}]}`)
		default:
			http.Error(w, `{"message":"Not Found"}`, 404)
		}
	}))
	defer server.Close()
	t.Setenv("MUSEHOOD_GITHUB_API", server.URL)

	out := ompRun(t, f, ctx, "github_read", "repo o/r")
	if !strings.Contains(out, "Repo o/r") || !strings.Contains(out, "bintang: 7") {
		t.Fatalf("github_read repo = %q", out)
	}
	out = ompRun(t, f, ctx, "github_read", "file o/r README.md")
	if !strings.Contains(out, "# Readme Uji") {
		t.Fatalf("github_read file = %q", out)
	}
	out = ompRun(t, f, ctx, "github_search", "repos\nuji")
	if !strings.Contains(out, "o/r") {
		t.Fatalf("github_search = %q", out)
	}
	out = ompRun(t, f, ctx, "github_actions", "runs o/r")
	if !strings.Contains(out, "CI") || !strings.Contains(out, "success") {
		t.Fatalf("github_actions = %q", out)
	}
	out = ompRun(t, f, ctx, "github_read", "repo o/tidak-ada")
	if !strings.Contains(out, "tidak ditemukan") {
		t.Fatalf("github_read 404 = %q", out)
	}
	// Push tanpa token: pesan jelas.
	out = ompRun(t, f, ctx, "github_push", "project:apa-saja")
	if !strings.Contains(out, "token") {
		t.Fatalf("github_push tanpa token = %q", out)
	}
}

// --- 7. security_scan ---

func TestOmpSecurityScan(t *testing.T) {
	f := setup(t, nil)
	ctx := ompRoomCtx()
	ompWriteProjectFile(t, f, "scan-uji", "config.txt", "api_key = \"sk-1234567890abcdefghijklmnopqrstuv\"\n")
	ompWriteProjectFile(t, f, "scan-uji", "main.py", "print('halo')\n")
	out := ompRun(t, f, ctx, "security_scan", "project:scan-uji")
	if !strings.Contains(out, "Pola secret ditemukan") || !strings.Contains(out, "config.txt") {
		t.Fatalf("security_scan = %q", out)
	}
	if strings.Contains(out, "sk-1234567890") {
		t.Fatal("security_scan tidak boleh menampilkan nilai secret")
	}
	// Proyek Go: govulncheck atau fallback go vet disebut.
	ompWriteProjectFile(t, f, "scan-go", "go.mod", "module scan-go\n\ngo 1.22\n")
	ompWriteProjectFile(t, f, "scan-go", "main.go", "package main\n\nfunc main() {}\n")
	out = ompRun(t, f, ctx, "security_scan", "project:scan-go")
	if !strings.Contains(out, "govulncheck") && !strings.Contains(out, "go vet") {
		t.Fatalf("security_scan go = %q", out)
	}
}

// --- 8. Hub ---

func TestOmpHub(t *testing.T) {
	f := setup(t, nil)
	ctx := ompRoomCtx()
	out := ompRun(t, f, ctx, "hub_message", "M02\nTolong cek bagianmu.")
	if !strings.Contains(out, "terkirim") {
		t.Fatalf("hub_message = %q", out)
	}
	f.app.mu.Lock()
	found := false
	for _, m := range f.app.data.Messages {
		if strings.Contains(m.Text, "[hub →") && strings.Contains(m.Text, "Tolong cek bagianmu.") {
			found = true
		}
	}
	f.app.mu.Unlock()
	if !found {
		t.Fatal("pesan hub tidak tercatat di ruang")
	}
	out = ompRun(t, f, ctx, "hub_processes", "")
	if out == "" {
		t.Fatal("hub_processes kosong")
	}
	out = ompRun(t, f, ctx, "hub_stop", "aneh")
	if !strings.Contains(out, "Error") {
		t.Fatalf("hub_stop invalid = %q", out)
	}
}

// --- 9. Checkpoint/rewind ---

func TestOmpCheckpointRewind(t *testing.T) {
	f := setup(t, nil)
	ctx := ompRoomCtx()
	f.app.mu.Lock()
	f.app.data.Messages = append(f.app.data.Messages,
		&message{ID: uuid(), CreatedAt: now(), AuthorType: "owner", Text: "satu", Status: "completed", RoomID: defaultRoomID, BranchID: "main"},
		&message{ID: uuid(), CreatedAt: now(), AuthorType: "owner", Text: "dua", Status: "completed", RoomID: defaultRoomID, BranchID: "main"})
	f.app.mu.Unlock()

	out := ompRun(t, f, ctx, "checkpoint", "titik-a")
	if !strings.Contains(out, "Checkpoint dibuat") {
		t.Fatalf("checkpoint = %q", out)
	}
	f.app.mu.Lock()
	f.app.data.Messages = append(f.app.data.Messages,
		&message{ID: uuid(), CreatedAt: now(), AuthorType: "owner", Text: "tiga", Status: "completed", RoomID: defaultRoomID, BranchID: "main"})
	f.app.mu.Unlock()

	// Rewind lewat API pemilik.
	resp := f.api("POST", "/api/rooms/"+defaultRoomID+"/rewind", map[string]any{"checkpointId": "titik-a"}, 200)
	if resp["removed"] == nil {
		t.Fatalf("rewind API = %v", resp)
	}
	f.app.mu.Lock()
	count := 0
	for _, m := range f.app.data.Messages {
		if m.RoomID == defaultRoomID {
			count++
		}
	}
	f.app.mu.Unlock()
	if count != 2 {
		t.Fatalf("pesan sesudah rewind = %d, mau 2", count)
	}
	// Checkpoint lewat API.
	resp = f.api("POST", "/api/rooms/"+defaultRoomID+"/checkpoint", map[string]any{"label": "titik-b"}, 201)
	if resp["checkpoint"] == nil {
		t.Fatalf("checkpoint API = %v", resp)
	}
}

// --- 10. Memori durable + learn ---

func TestOmpMemoryLearn(t *testing.T) {
	f := setup(t, nil)
	ctx := ompRoomCtx()
	out := ompRun(t, f, ctx, "memory_retain", "Arman suka jawaban singkat\npreferensi")
	if !strings.Contains(out, "tersimpan") {
		t.Fatalf("memory_retain = %q", out)
	}
	out = ompRun(t, f, ctx, "memory_recall", "singkat")
	if !strings.Contains(out, "Arman suka jawaban singkat") {
		t.Fatalf("memory_recall = %q", out)
	}
	out = ompRun(t, f, ctx, "memory_reflect", "preferensi")
	if !strings.Contains(out, "sintesis") {
		t.Fatalf("memory_reflect = %q", out)
	}
	out = ompRun(t, f, ctx, "learn", "skill:uji_skill\nSelalu jalankan tes sebelum commit.")
	if !strings.Contains(out, "skill") {
		t.Fatalf("learn = %q", out)
	}
	skillPath := filepath.Join(f.app.skillsDir(), "uji_skill", "SKILL.md")
	if _, err := os.Stat(skillPath); err != nil {
		t.Fatalf("skill tidak dibuat: %v", err)
	}
	out = ompRun(t, f, ctx, "manage_skill", "list")
	if !strings.Contains(out, "uji_skill") {
		t.Fatalf("manage_skill list = %q", out)
	}
	out = ompRun(t, f, ctx, "manage_skill", "retire uji_skill")
	if !strings.Contains(out, "dipensiunkan") {
		t.Fatalf("manage_skill retire = %q", out)
	}
	// API memori.
	resp := f.api("GET", "/api/memories", nil, 200)
	memories, _ := resp["memories"].([]any)
	if len(memories) == 0 {
		t.Fatalf("GET /api/memories kosong: %v", resp)
	}
	// Toggle mati: tool menolak.
	f.api("POST", "/api/settings", map[string]any{"settings": map[string]any{"memoryEnabled": false}}, 200)
	out = ompRun(t, f, ctx, "memory_recall", "singkat")
	if !strings.Contains(out, "dimatikan") {
		t.Fatalf("memory_recall saat mati = %q", out)
	}
}

// --- 11. Todo ---

func TestOmpTodo(t *testing.T) {
	f := setup(t, nil)
	ctx := ompRoomCtx()
	out := ompRun(t, f, ctx, "todo_add", "tulis tes")
	if !strings.Contains(out, "ditambahkan") {
		t.Fatalf("todo_add = %q", out)
	}
	out = ompRun(t, f, ctx, "todo_update", "1\nactive")
	if !strings.Contains(out, "active") {
		t.Fatalf("todo_update = %q", out)
	}
	out = ompRun(t, f, ctx, "todo_list", "")
	if !strings.Contains(out, "tulis tes") {
		t.Fatalf("todo_list = %q", out)
	}
	// API pemilik.
	resp := f.api("POST", "/api/rooms/"+defaultRoomID+"/todos", map[string]any{"text": "dari UI"}, 201)
	todo, _ := resp["todo"].(map[string]any)
	if todo == nil || todo["id"] == nil {
		t.Fatalf("todo API = %v", resp)
	}
	f.api("POST", "/api/rooms/"+defaultRoomID+"/todos/"+todo["id"].(string)+"/status", map[string]any{"status": "done"}, 200)
	f.api("DELETE", "/api/rooms/"+defaultRoomID+"/todos/"+todo["id"].(string), nil, 200)
	// State memuat todos.
	state := f.state()
	rooms, _ := state["rooms"].([]any)
	found := false
	for _, item := range rooms {
		room, _ := item.(map[string]any)
		if todos, _ := room["todos"].([]any); len(todos) == 1 {
			found = true
		}
	}
	if !found {
		t.Fatal("state tidak memuat todos ruang")
	}
}

// --- 12. Workflow ---

func TestOmpWorkflows(t *testing.T) {
	f := setup(t, nil)
	// Proyek git untuk commit workflow.
	dir, err := f.app.ensureProject("wf-uji")
	if err != nil {
		t.Fatal(err)
	}
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=Uji", "-c", "user.email=uji@localhost"}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %s", args, out)
		}
	}
	git("init")
	ompWriteProjectFile(t, f, "wf-uji", "main.go", "package main\n\nfunc main() {}\n")
	git("add", ".")
	git("commit", "-m", "awal")
	ompWriteProjectFile(t, f, "wf-uji", "main.go", "package main\n\nfunc main() { println(1) }\n")
	ompWriteProjectFile(t, f, "wf-uji", "README.md", "# Uji\n")

	resp := f.api("POST", "/api/commit-workflow", map[string]any{"project": "wf-uji"}, 200)
	groups, _ := resp["groups"].([]any)
	if len(groups) == 0 {
		t.Fatalf("commit-workflow usulan kosong: %v", resp)
	}
	resp = f.api("POST", "/api/commit-workflow", map[string]any{"project": "wf-uji", "approve": true, "groups": groups}, 200)
	if committed, _ := resp["committed"].([]any); len(committed) == 0 {
		t.Fatalf("commit-workflow approve = %v", resp)
	}

	// Autoresearch: 2 iterasi skrip sederhana.
	resp = f.api("POST", "/api/autoresearch", map[string]any{"project": "wf-uji", "script": "echo 5", "iterations": 2}, 200)
	results, _ := resp["results"].([]any)
	if len(results) != 2 {
		t.Fatalf("autoresearch results = %v", resp)
	}

	// Review: membuat pekerjaan di ruang.
	resp = f.api("POST", "/api/review", map[string]any{"project": "wf-uji", "roomId": defaultRoomID}, 202)
	if resp["jobId"] == nil {
		t.Fatalf("review = %v", resp)
	}

	// Goal/plan/vibe lewat roomInput + prompt.
	f.api("POST", "/api/rooms/"+defaultRoomID, map[string]any{"goal": "Selesaikan fitur X", "planMode": true, "vibeMode": true}, 200)
	f.app.mu.Lock()
	room := f.app.data.findRoom(defaultRoomID)
	prompt := roomModePrompt(room, ompSeat(), f.app.data.Settings)
	f.app.mu.Unlock()
	if !strings.Contains(prompt, "Selesaikan fitur X") || !strings.Contains(prompt, "Plan mode AKTIF") || !strings.Contains(prompt, "Vibe mode AKTIF") {
		t.Fatalf("roomModePrompt = %q", prompt)
	}
}

// --- 13. Kebijakan per tool + on-demand ---

func TestOmpToolPolicies(t *testing.T) {
	f := setup(t, nil)
	ctx := ompRoomCtx()
	f.api("POST", "/api/settings", map[string]any{"settings": map[string]any{
		"toolPolicies": map[string]any{"current_time": "deny", "password_gen": "prompt"},
	}}, 200)
	out := ompRun(t, f, ctx, "current_time", "")
	if !strings.Contains(out, "dilarang") {
		t.Fatalf("kebijakan deny = %q", out)
	}
	done := make(chan string, 1)
	go func() { done <- ompRun(t, f, ctx, "password_gen", "12") }()
	ompApproveNext(t, f, "tool")
	if out := <-done; !strings.Contains(out, "Password:") {
		t.Fatalf("kebijakan prompt sesudah approve = %q", out)
	}
	// On-demand: deskripsi tool non-inti tidak dimuat penuh.
	f.api("POST", "/api/settings", map[string]any{"settings": map[string]any{"onDemandTools": true}}, 200)
	text := f.app.buildToolInstructions(true, ompSeat(), nil, true)
	if strings.Contains(text, "- ast_grep: cari pola") {
		t.Fatal("on-demand masih memuat deskripsi penuh ast_grep")
	}
	if !strings.Contains(text, "tool_info") || !strings.Contains(text, "ast_grep") {
		t.Fatal("on-demand tidak menyebut daftar nama tool")
	}
	out = ompRun(t, f, ctx, "tool_info", "ast_grep")
	if !strings.Contains(out, "ast-grep") {
		t.Fatalf("tool_info = %q", out)
	}
	// Plan mode: Write ditolak, Read boleh.
	f.api("POST", "/api/rooms/"+defaultRoomID, map[string]any{"planMode": true}, 200)
	out = ompRun(t, f, ctx, "write_file", "plan.txt\nisi")
	if !strings.Contains(out, "plan mode") {
		t.Fatalf("plan mode write_file = %q", out)
	}
	out = ompRun(t, f, ctx, "current_time", "")
	if strings.Contains(out, "plan mode") {
		t.Fatalf("plan mode tidak boleh memblokir Read (selain deny): %q", out)
	}
	f.api("POST", "/api/rooms/"+defaultRoomID, map[string]any{"planMode": false}, 200)
	out = ompRun(t, f, ctx, "write_file", "plan.txt\nisi")
	if !strings.Contains(out, "plan.txt") {
		t.Fatalf("write_file sesudah plan mode mati = %q", out)
	}
}

// --- Settings omp + backup ---

func TestOmpSettingsAndBackup(t *testing.T) {
	f := setup(t, nil)
	f.api("POST", "/api/settings", map[string]any{"settings": map[string]any{
		"githubToken": "token-rahasia-uji",
		"lspServers":  map[string]any{"go": "gopls"},
	}}, 200)
	state := f.state()
	settings, _ := state["settings"].(map[string]any)
	if settings["hasGithubToken"] != true {
		t.Fatalf("hasGithubToken = %v", settings["hasGithubToken"])
	}
	if _, leaked := settings["githubToken"]; leaked {
		t.Fatal("githubToken bocor di state")
	}
	// Backup mengosongkan token.
	req, _ := http.NewRequest("GET", f.server.URL+"/api/backup", nil)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	raw := make([]byte, 0, 4096)
	buf := make([]byte, 4096)
	for {
		n, err := res.Body.Read(buf)
		raw = append(raw, buf[:n]...)
		if err != nil {
			break
		}
	}
	if strings.Contains(string(raw), "token-rahasia-uji") {
		t.Fatal("token GitHub bocor di backup")
	}
}
