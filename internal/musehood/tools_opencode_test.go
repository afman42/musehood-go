package musehood

// Tes tool gaya OpenCode (28 Nov 2026): ask_question, spawn_subagent /
// check_subagent, execute (Code Mode RPC), worktree git, browser
// headless, dan dukungan MCP stdio.

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func toolCfg(f fixture) settings {
	f.app.mu.Lock()
	defer f.app.mu.Unlock()
	return f.app.data.Settings
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timeout menunggu: %s", what)
}

// --- ask_question ---

func TestAskQuestionFlow(t *testing.T) {
	f := setup(t, nil)
	model := "plain"
	s := seat{Code: "M02", Name: "GLM", Model: &model}
	result := make(chan string, 1)
	go func() {
		out, _ := f.app.runTool(context.Background(), toolCfg(f), s, "ask_question", "Pilih warna favorit?\nMerah\nBiru\nHijau", true)
		result <- out
	}()
	var questionID string
	waitFor(t, "pertanyaan tertunda muncul di state", func() bool {
		state := f.state()
		pending, _ := state["pendingQuestions"].([]any)
		if len(pending) == 1 {
			q, _ := pending[0].(map[string]any)
			questionID, _ = q["id"].(string)
			return questionID != ""
		}
		return false
	})
	f.api("POST", "/api/questions/"+questionID, object{"answer": "Biru"}, 200)
	select {
	case out := <-result:
		if !strings.Contains(out, "Biru") {
			t.Fatalf("hasil ask_question harus memuat jawaban pemilik, dapat %q", out)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("ask_question tidak selesai setelah dijawab")
	}
	// Jawaban bebas (bukan salah satu pilihan) juga diterima.
	go func() {
		out, _ := f.app.runTool(context.Background(), toolCfg(f), s, "ask_question", "Kota mana?\nJakarta\nBandung", true)
		result <- out
	}()
	waitFor(t, "pertanyaan kedua tertunda", func() bool {
		pending, _ := f.state()["pendingQuestions"].([]any)
		if len(pending) == 1 {
			q, _ := pending[0].(map[string]any)
			questionID, _ = q["id"].(string)
			return questionID != ""
		}
		return false
	})
	f.api("POST", "/api/questions/"+questionID, object{"answer": "Surabaya saja"}, 200)
	select {
	case out := <-result:
		if !strings.Contains(out, "Surabaya saja") {
			t.Fatalf("jawaban bebas harus diteruskan, dapat %q", out)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("ask_question kedua tidak selesai")
	}
}

func TestAskQuestionTimeoutAndInvalid(t *testing.T) {
	f := setup(t, nil)
	old := approvalTimeout
	approvalTimeout = 150 * time.Millisecond
	defer func() { approvalTimeout = old }()
	model := "plain"
	s := seat{Code: "M02", Name: "GLM", Model: &model}
	out, _ := f.app.runTool(context.Background(), toolCfg(f), s, "ask_question", "Siapa namamu?", true)
	if !strings.Contains(out, "tidak menjawab") {
		t.Fatalf("timeout harus menghasilkan teks tidak dijawab, dapat %q", out)
	}
	out, _ = f.app.runTool(context.Background(), toolCfg(f), s, "ask_question", "", true)
	if !strings.HasPrefix(out, "Error") {
		t.Fatalf("pertanyaan kosong harus Error, dapat %q", out)
	}
	out, _ = f.app.runTool(context.Background(), toolCfg(f), s, "ask_question", "Pilih?\n1\n2\n3\n4\n5\n6\n7", true)
	if !strings.HasPrefix(out, "Error") {
		t.Fatalf("lebih dari 6 pilihan harus Error, dapat %q", out)
	}
	// ID pertanyaan tidak dikenal → 404.
	f.api("POST", "/api/questions/tidak-ada", object{"answer": "x"}, 404)
}

// --- spawn_subagent / check_subagent ---

func TestSpawnSubagentFlow(t *testing.T) {
	f := setup(t, nil)
	f.app.mu.Lock()
	mainRoom := f.app.data.activeRoom()
	mainRoomID := mainRoom.ID
	f.app.mu.Unlock()
	model := "plain"
	s := seat{Code: "M02", Name: "GLM", Model: &model}
	ctx := context.WithValue(context.Background(), toolRunInfoKey{}, toolRunInfo{JobID: "job-induk", RoomID: mainRoomID})
	out, _ := f.app.runTool(ctx, toolCfg(f), s, "spawn_subagent", "seat:M01\nCeritakan sesuatu yang singkat", true)
	if !strings.Contains(out, "Sesi subagent dimulai") {
		t.Fatalf("spawn harus berhasil, dapat %q", out)
	}
	var subRoomID, subJobID string
	waitFor(t, "ruang subagent dibuat", func() bool {
		f.app.mu.Lock()
		defer f.app.mu.Unlock()
		for _, r := range f.app.data.Rooms {
			if r.SubagentOf == mainRoomID {
				subRoomID = r.ID
			}
		}
		for _, j := range f.app.data.Jobs {
			if j.RoomID == subRoomID {
				subJobID = j.ID
			}
		}
		return subRoomID != "" && subJobID != ""
	})
	waitFor(t, "pekerjaan subagent selesai", func() bool {
		f.app.mu.Lock()
		defer f.app.mu.Unlock()
		for _, j := range f.app.data.Jobs {
			if j.ID == subJobID {
				return j.Status == "completed"
			}
		}
		return false
	})
	out, _ = f.app.runTool(ctx, toolCfg(f), s, "check_subagent", subRoomID, true)
	if !strings.Contains(out, "bagian teks") {
		t.Fatalf("check_subagent harus memuat balasan kursi anak, dapat %q", out)
	}
	// Ringkasan hasil diposting ke ruang induk.
	f.app.mu.Lock()
	foundSummary := false
	for _, m := range f.app.data.Messages {
		if m.RoomID == mainRoomID && m.Label == "Subagent" && strings.Contains(m.Text, "bagian teks") {
			foundSummary = true
		}
	}
	f.app.mu.Unlock()
	if !foundSummary {
		t.Fatal("ringkasan hasil subagent harus diposting ke ruang induk")
	}
	// Kedalaman 2 ditolak.
	subCtx := context.WithValue(context.Background(), toolRunInfoKey{}, toolRunInfo{JobID: subJobID, RoomID: subRoomID})
	out, _ = f.app.runTool(subCtx, toolCfg(f), s, "spawn_subagent", "seat:M01\nSpawn lagi", true)
	if !strings.Contains(out, "kedalaman maksimum") {
		t.Fatalf("spawn dari ruang sub harus ditolak, dapat %q", out)
	}
	// Argumen invalid.
	out, _ = f.app.runTool(ctx, toolCfg(f), s, "spawn_subagent", "", true)
	if !strings.HasPrefix(out, "Error") {
		t.Fatalf("prompt kosong harus Error, dapat %q", out)
	}
	out, _ = f.app.runTool(ctx, toolCfg(f), s, "spawn_subagent", "seat:TIDAKADA\nHalo", true)
	if !strings.HasPrefix(out, "Error") {
		t.Fatalf("kursi tidak dikenal harus Error, dapat %q", out)
	}
	out, _ = f.app.runTool(ctx, toolCfg(f), s, "check_subagent", "id-tidak-ada", true)
	if !strings.Contains(out, "tidak ditemukan") {
		t.Fatalf("check id tidak dikenal harus melaporkan tidak ditemukan, dapat %q", out)
	}
}

// --- execute (Code Mode) ---

func TestExecuteCodeMode(t *testing.T) {
	f := setup(t, nil)
	model := "plain"
	s := seat{Code: "M02", Name: "GLM", Model: &model}
	ctx := context.Background()
	out, _ := f.app.runTool(ctx, toolCfg(f), s, "execute", `const r = await tool("current_time", ""); console.log("WAKTU:" + r);`, true)
	if !strings.Contains(out, "WAKTU:") || !strings.Contains(out, "2026") {
		t.Fatalf("execute harus memanggil tool dan mencetak hasil, dapat %q", out)
	}
	out, _ = f.app.runTool(ctx, toolCfg(f), s, "execute", `return 40 + 2;`, true)
	if !strings.Contains(out, "42") {
		t.Fatalf("nilai return harus menjadi hasil, dapat %q", out)
	}
	out, _ = f.app.runTool(ctx, toolCfg(f), s, "execute", `for (let i = 0; i < 21; i++) { const r = await tool("current_time", ""); if (r.includes("batas")) { console.log("BATAS-TERCAPAI"); break; } }`, true)
	if !strings.Contains(out, "BATAS-TERCAPAI") {
		t.Fatalf("panggilan ke-21 harus kena batas, dapat %q", out)
	}
	out, _ = f.app.runTool(ctx, toolCfg(f), s, "execute", `console.log(await tool("execute", "console.log(1)"));`, true)
	if !strings.Contains(out, "tidak dapat memanggil execute") {
		t.Fatalf("execute bersarang harus ditolak, dapat %q", out)
	}
	out, _ = f.app.runTool(ctx, toolCfg(f), s, "execute", "", true)
	if !strings.HasPrefix(out, "Error") {
		t.Fatalf("kode kosong harus Error, dapat %q", out)
	}
	out, _ = f.app.runTool(ctx, toolCfg(f), s, "execute", `throw new Error("ledakan uji");`, true)
	if !strings.Contains(out, "ledakan uji") {
		t.Fatalf("error JS harus diteruskan, dapat %q", out)
	}
}

// --- worktree git ---

func TestWorktreeTools(t *testing.T) {
	f := setup(t, nil)
	ctx := context.Background()
	model := "plain"
	s := seat{Code: "M02", Name: "GLM", Model: &model}
	cfg := toolCfg(f)
	// Proyek tanpa commit: worktree_create harus menolak dengan jelas.
	out, _ := f.app.runTool(ctx, cfg, s, "worktree_create", "project:wtuji\ncabang-a", true)
	if !strings.Contains(out, "belum punya commit") {
		t.Fatalf("proyek tanpa commit harus ditolak, dapat %q", out)
	}
	// Buat commit awal langsung lewat git proyek.
	dir, err := f.app.ensureProject("wtuji")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("isi awal\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := f.app.ensureProjectGit(ctx, dir); err != nil {
		t.Fatal(err)
	}
	if out, err := f.app.runProjectGit(ctx, dir, "add", "-A"); err != nil {
		t.Fatalf("git add: %s", out)
	}
	if out, err := f.app.runProjectGit(ctx, dir, "commit", "-q", "-m", "awal"); err != nil {
		t.Fatalf("git commit: %s", out)
	}
	out, _ = f.app.runTool(ctx, cfg, s, "worktree_create", "project:wtuji\ncabang-a", true)
	if !strings.Contains(out, "dibuat") {
		t.Fatalf("worktree_create harus berhasil, dapat %q", out)
	}
	if _, err := os.Stat(filepath.Join(f.app.worktreeDir("wtuji", "cabang-a"), "a.txt")); err != nil {
		t.Fatalf("file proyek harus ada di worktree: %v", err)
	}
	out, _ = f.app.runTool(ctx, cfg, s, "worktree_list", "project:wtuji", true)
	if !strings.Contains(out, "cabang-a") {
		t.Fatalf("worktree_list harus memuat cabang-a, dapat %q", out)
	}
	out, _ = f.app.runTool(ctx, cfg, s, "worktree_create", "project:wtuji\ncabang-a", true)
	if !strings.HasPrefix(out, "Error") {
		t.Fatalf("worktree ganda harus Error, dapat %q", out)
	}
	out, _ = f.app.runTool(ctx, cfg, s, "worktree_remove", "project:wtuji\ncabang-a", true)
	if !strings.Contains(out, "dihapus") {
		t.Fatalf("worktree_remove harus berhasil, dapat %q", out)
	}
	out, _ = f.app.runTool(ctx, cfg, s, "worktree_remove", "project:wtuji\ncabang-a", true)
	if !strings.Contains(out, "tidak ditemukan") {
		t.Fatalf("hapus worktree yang sudah hilang harus melaporkan tidak ditemukan, dapat %q", out)
	}
	out, _ = f.app.runTool(ctx, cfg, s, "worktree_create", "project:wtuji\nNama Buruk", true)
	if !strings.HasPrefix(out, "Error") {
		t.Fatalf("nama worktree invalid harus Error, dapat %q", out)
	}
}

// --- browser headless ---

func TestBrowserDegradationAndLinks(t *testing.T) {
	f := setup(t, nil)
	oldLookup := lookupIP
	lookupIP = func(ctx context.Context, host string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("93.184.216.34")}, nil
	}
	defer func() { lookupIP = oldLookup }()
	oldFinder := findChromium
	findChromium = func() string { return "" }
	defer func() { findChromium = oldFinder }()
	model := "plain"
	s := seat{Code: "M02", Name: "GLM", Model: &model}
	ctx := context.Background()
	for _, name := range []string{"browser_open", "browser_screenshot", "browser_links"} {
		out, _ := f.app.runTool(ctx, toolCfg(f), s, name, "https://example.com/halaman", true)
		if !strings.Contains(out, "npx playwright install chromium") {
			t.Fatalf("%s tanpa Chromium harus memberi petunjuk pasang, dapat %q", name, out)
		}
	}
	// SSRF tetap dijaga sebelum cek Chromium.
	out, _ := f.app.runTool(ctx, toolCfg(f), s, "browser_open", "http://127.0.0.1/admin", true)
	if !strings.HasPrefix(out, "Error") {
		t.Fatalf("URL loopback harus ditolak, dapat %q", out)
	}
	// Ekstraksi tautan (unit).
	dom := `<html><body><a href="/satu">Satu</a><a href="https://lain.example/dua">Dua</a><a href="mailto:a@b.c">Email</a><a href="/satu">Duplikat</a><a href="#frag">Fragmen</a></body></html>`
	lines := extractLinks(dom, "https://example.com/dir/halaman")
	if len(lines) != 2 {
		t.Fatalf("harus 2 tautan unik non-mailto, dapat %v", lines)
	}
	if !strings.Contains(lines[0], "https://example.com/satu") || !strings.Contains(lines[1], "https://lain.example/dua") {
		t.Fatalf("tautan harus absolut, dapat %v", lines)
	}
}

func TestBrowserRealRender(t *testing.T) {
	chrome := findChromeBinary()
	if chrome == "" {
		t.Skip("Chromium tidak tersedia di lingkungan ini")
	}
	f := setup(t, nil)
	dom, err := f.app.browserDumpDOM(context.Background(), "data:text/html,<html><body><h1>Halo Render</h1><script>document.body.innerHTML += '<p>dari-js</p>'</script></body></html>")
	if err != nil {
		t.Fatalf("dump-dom gagal: %v", err)
	}
	if !strings.Contains(dom, "Halo Render") || !strings.Contains(dom, "dari-js") {
		t.Fatalf("DOM hasil render harus memuat konten statis + JS, dapat %.300s", dom)
	}
}

// --- MCP ---

const fakeMCPServer = `import sys, json
for line in sys.stdin:
    line = line.strip()
    if not line:
        continue
    try:
        req = json.loads(line)
    except Exception:
        continue
    method = req.get("method")
    rid = req.get("id")
    if method == "initialize":
        print(json.dumps({"jsonrpc": "2.0", "id": rid, "result": {"protocolVersion": "2024-11-05", "capabilities": {}, "serverInfo": {"name": "fake", "version": "1.0"}}}), flush=True)
    elif method == "tools/list":
        print(json.dumps({"jsonrpc": "2.0", "id": rid, "result": {"tools": [{"name": "echo_text", "description": "Gema teks yang diberikan", "inputSchema": {"type": "object", "properties": {"text": {"type": "string"}}}}]}}), flush=True)
    elif method == "tools/call":
        args = req.get("params", {}).get("arguments", {})
        print(json.dumps({"jsonrpc": "2.0", "id": rid, "result": {"content": [{"type": "text", "text": "ECHO:" + str(args.get("text", ""))}]}}), flush=True)
`

func TestMCPFlow(t *testing.T) {
	f := setup(t, nil)
	script := filepath.Join(t.TempDir(), "fake_mcp.py")
	if err := os.WriteFile(script, []byte(fakeMCPServer), 0o644); err != nil {
		t.Fatal(err)
	}
	// Validasi nama.
	f.api("POST", "/api/mcp-servers", object{"name": "Nama Buruk", "command": "python3"}, 400)
	// Buat server dengan env rahasia.
	out := f.api("POST", "/api/mcp-servers", object{
		"name": "uji", "command": "python3", "args": []string{script},
		"env": object{"KUNCI_UJI": "nilai-rahasia-uji"},
	}, 200)
	if strings.Contains(fmt.Sprint(out), "nilai-rahasia-uji") {
		t.Fatal("nilai env MCP bocor di respons POST")
	}
	out = f.api("GET", "/api/mcp-servers", nil, 200)
	if strings.Contains(fmt.Sprint(out), "nilai-rahasia-uji") {
		t.Fatal("nilai env MCP bocor di respons GET")
	}
	if !strings.Contains(fmt.Sprint(out), "KUNCI_UJI") {
		t.Fatalf("GET harus memuat nama kunci env, dapat %v", out)
	}
	// State juga tidak boleh memuat nilai env.
	if strings.Contains(fmt.Sprint(f.state()), "nilai-rahasia-uji") {
		t.Fatal("nilai env MCP bocor di state")
	}
	// Tool muncul di /api/tools dengan sumber mcp (cache diisi latar).
	waitFor(t, "tool MCP muncul di /api/tools", func() bool {
		tools := f.api("GET", "/api/tools", nil, 200)
		return strings.Contains(fmt.Sprint(tools["mcp"]), "mcp_uji_echo_text")
	})
	// Panggilan tool dengan persetujuan pemilik.
	model := "plain"
	s := seat{Code: "M02", Name: "GLM", Model: &model}
	result := make(chan string, 1)
	go func() {
		out, _ := f.app.runTool(context.Background(), toolCfg(f), s, "mcp_uji_echo_text", `{"text":"halo mcp"}`, true)
		result <- out
	}()
	var approvalID string
	waitFor(t, "persetujuan MCP tertunda", func() bool {
		pending, _ := f.state()["pendingApprovals"].([]any)
		for _, item := range pending {
			entry, _ := item.(map[string]any)
			if entry["kind"] == "mcp" {
				approvalID, _ = entry["id"].(string)
				return approvalID != ""
			}
		}
		return false
	})
	f.api("POST", "/api/approvals/"+approvalID, object{"approved": true}, 200)
	select {
	case out := <-result:
		if !strings.Contains(out, "ECHO:halo mcp") {
			t.Fatalf("hasil MCP harus ECHO:halo mcp, dapat %q", out)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("panggilan MCP tidak selesai")
	}
	// Penolakan persetujuan (timeout pendek).
	old := approvalTimeout
	approvalTimeout = 150 * time.Millisecond
	out2, _ := f.app.runTool(context.Background(), toolCfg(f), s, "mcp_uji_echo_text", `{"text":"x"}`, true)
	approvalTimeout = old
	if !strings.Contains(out2, "TIDAK dijalankan") {
		t.Fatalf("tanpa persetujuan MCP harus ditolak, dapat %q", out2)
	}
	// Tool tidak dikenal.
	out2, _ = f.app.runTool(context.Background(), toolCfg(f), s, "mcp_tidak_ada", "", true)
	if !strings.Contains(out2, "tidak dikenal") {
		t.Fatalf("tool MCP tidak dikenal harus dilaporkan, dapat %q", out2)
	}
	// Batas 5 server.
	for i := 0; i < 4; i++ {
		f.api("POST", "/api/mcp-servers", object{"name": fmt.Sprintf("srv%d", i), "command": "python3", "args": []string{script}}, 200)
	}
	f.api("POST", "/api/mcp-servers", object{"name": "srvx", "command": "python3"}, 400)
	// Hapus server.
	f.api("POST", "/api/mcp-servers/uji/delete", object{}, 200)
	out = f.api("GET", "/api/mcp-servers", nil, 200)
	if strings.Contains(fmt.Sprint(out), "\"uji\"") {
		t.Fatalf("server uji harus terhapus, dapat %v", out)
	}
	f.api("POST", "/api/mcp-servers/uji/delete", object{}, 404)
}

// TestMCPToolsListRaw menguji client MCP langsung terhadap server palsu.
func TestMCPToolsListRaw(t *testing.T) {
	script := filepath.Join(t.TempDir(), "fake_mcp.py")
	if err := os.WriteFile(script, []byte(fakeMCPServer), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := mcpServerConfig{Name: "uji", Command: "python3", Args: []string{script}}
	tools, err := mcpListToolsRaw(context.Background(), cfg)
	if err != nil {
		t.Fatalf("tools/list gagal: %v", err)
	}
	if len(tools) != 1 || tools[0].Name != "echo_text" {
		t.Fatalf("tools/list harus memuat echo_text, dapat %+v", tools)
	}
	result, err := mcpCallToolRaw(context.Background(), cfg, "echo_text", map[string]any{"text": "langsung"})
	if err != nil {
		t.Fatalf("tools/call gagal: %v", err)
	}
	if result != "ECHO:langsung" {
		t.Fatalf("hasil tools/call harus ECHO:langsung, dapat %q", result)
	}
	// Perintah tidak ada → error, bukan panic.
	bad := mcpServerConfig{Name: "uji", Command: "/tidak/ada/binary-ini"}
	if _, err := mcpListToolsRaw(context.Background(), bad); err == nil {
		t.Fatal("perintah tidak ada harus error")
	}
	// Nama terekspos tersanitasi.
	if got := mcpExposedName("uji", "Echo Text!"); got != "mcp_uji_echo_text" {
		t.Fatalf("sanitasi nama salah: %q", got)
	}
	// JSON argumen tidak valid di tools/call tetap aman (dibungkus {"arg":...} di runMCPTool).
	raw, _ := json.Marshal(map[string]any{"arg": "teks"})
	if !strings.Contains(string(raw), "teks") {
		t.Fatal("marshal argumen gagal")
	}
}
