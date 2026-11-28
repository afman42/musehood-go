package musehood

// Tes berat ronde 2 — keamanan tool omp (28 Nov 2026): sapu plan mode
// atas 111 tool, kebijakan deny/prompt di semua jalur (langsung,
// execute Code Mode, pipeline, MCP), ast_edit/lsp_rename tanpa
// persetujuan, github_push tanpa token/persetujuan, redirect SSRF
// GitHub, CDP SSRF, security_scan tanpa kebocoran secret, gate eval
// berbahaya, dan rewind via tool tanpa persetujuan.

import (
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

// heavy2ShortApproval memperpendek approvalTimeout selama satu tes.
func heavy2ShortApproval(t *testing.T, d time.Duration) {
	t.Helper()
	old := approvalTimeout
	approvalTimeout = d
	t.Cleanup(func() { approvalTimeout = old })
}

// TestHeavy2PlanModeSweepAllTools: plan mode menolak SEMUA tool
// non-Read dari 111 tool bawaan dan mengizinkan kelas Read.
func TestHeavy2PlanModeSweepAllTools(t *testing.T) {
	f := setup(t, nil)
	ctx := ompRoomCtx()
	f.api("POST", "/api/rooms/"+defaultRoomID, map[string]any{"planMode": true}, 200)

	if len(builtinToolNames) != 111 {
		t.Fatalf("jumlah tool bawaan = %d, mau 111", len(builtinToolNames))
	}
	for name := range builtinToolNames {
		blocked := f.app.enforcePlanMode(ctx, name, "argumen")
		if readOnlyTools[name] {
			if blocked != "" {
				t.Errorf("tool Read %s diblokir plan mode: %q", name, blocked)
			}
		} else if !strings.Contains(blocked, "plan mode") {
			t.Errorf("tool Write/Exec %s TIDAK diblokir plan mode: %q", name, blocked)
		}
	}
	// env_file: baca boleh, mutasi ditolak.
	if blocked := f.app.enforcePlanMode(ctx, "env_file", "project:x\nlist"); blocked != "" {
		t.Errorf("env_file list diblokir: %q", blocked)
	}
	if blocked := f.app.enforcePlanMode(ctx, "env_file", "project:x\nset KUNCI=nilai"); !strings.Contains(blocked, "plan mode") {
		t.Errorf("env_file set tidak diblokir: %q", blocked)
	}
	if blocked := f.app.enforcePlanMode(ctx, "env_file", "project:x\ndelete KUNCI"); !strings.Contains(blocked, "plan mode") {
		t.Errorf("env_file delete tidak diblokir: %q", blocked)
	}

	// Jalur dispatch penuh: tool tulis representatif ditolak dan tidak
	// mengubah state apa pun.
	for _, tc := range []struct{ name, arg string }{
		{"write_file", "plan-berat.txt\nisi"},
		{"todo_add", "tugas plan"},
		{"checkpoint", "titik-plan"},
		{"memory_retain", "memori plan"},
		{"spawn_subagent", "seat:M01\nhalo"},
		{"execute", "console.log(1)"},
		{"eval", "python\nprint(1)"},
		{"browser_tabs", "list"},
		{"mcp_tidak_ada_tool", "{}"},
	} {
		out := ompRun(t, f, ctx, tc.name, tc.arg)
		if !strings.Contains(out, "plan mode") {
			t.Errorf("%s saat plan mode = %q, mau penolakan plan mode", tc.name, out)
		}
	}
	if _, err := os.Stat(filepath.Join(f.app.sandboxDir(), "plan-berat.txt")); !os.IsNotExist(err) {
		t.Fatal("write_file saat plan mode tetap membuat file")
	}
	f.app.mu.Lock()
	room := f.app.data.findRoom(defaultRoomID)
	todos, checkpoints, memories := len(room.Todos), len(room.Checkpoints), len(f.app.data.Memories)
	f.app.mu.Unlock()
	if todos != 0 || checkpoints != 0 || memories != 0 {
		t.Fatalf("state berubah saat plan mode: todo=%d checkpoint=%d memori=%d", todos, checkpoints, memories)
	}

	// Pipeline kustom (bukan kelas Read) juga ditolak sebelum langkahnya.
	writeCustomTool(t, f.app, "pipa_plan.json", string(mustJSON(customTool{
		Name: "pipa_plan", Type: "pipeline", Steps: []string{"current_time"},
	})))
	if out := ompRun(t, f, ctx, "pipa_plan", ""); !strings.Contains(out, "plan mode") {
		t.Fatalf("pipeline saat plan mode = %q", out)
	}
}

// TestHeavy2ToolPolicyAllPaths: deny/prompt ditegakkan lewat jalur
// langsung, execute Code Mode, pipeline, dan MCP.
func TestHeavy2ToolPolicyAllPaths(t *testing.T) {
	f := setup(t, nil)
	ctx := ompRoomCtx()
	heavy2ShortApproval(t, 300*time.Millisecond)

	f.api("POST", "/api/settings", map[string]any{"settings": map[string]any{
		"toolPolicies": map[string]any{"write_file": "deny", "password_gen": "prompt"},
	}}, 200)

	// Langsung: deny.
	out := ompRun(t, f, ctx, "write_file", "deny-langsung.txt\nisi")
	if !strings.Contains(out, "dilarang") {
		t.Fatalf("deny langsung = %q", out)
	}
	// Lewat execute Code Mode: deny tetap berlaku untuk tool bersarang.
	out = ompRun(t, f, ctx, "execute", `const r = await tool("write_file", "deny-execute.txt\nisi"); console.log("HASIL:" + r);`)
	if !strings.Contains(out, "dilarang") {
		t.Fatalf("deny lewat execute = %q", out)
	}
	// Lewat pipeline: deny berlaku pada langkah pipeline.
	writeCustomTool(t, f.app, "pipa_tulis.json", string(mustJSON(customTool{
		Name: "pipa_tulis", Type: "pipeline", Steps: []string{"write_file"},
	})))
	out = ompRun(t, f, ctx, "pipa_tulis", "deny-pipeline.txt\nisi")
	if !strings.Contains(out, "dilarang") {
		t.Fatalf("deny lewat pipeline = %q", out)
	}
	for _, name := range []string{"deny-langsung.txt", "deny-execute.txt", "deny-pipeline.txt"} {
		if _, err := os.Stat(filepath.Join(f.app.sandboxDir(), name)); !os.IsNotExist(err) {
			t.Fatalf("file %s tetap dibuat meski deny", name)
		}
	}

	// Prompt tanpa persetujuan: langsung dan lewat execute sama-sama ditolak.
	out = ompRun(t, f, ctx, "password_gen", "12")
	if !strings.Contains(out, "TIDAK dijalankan") {
		t.Fatalf("prompt langsung tanpa approve = %q", out)
	}
	out = ompRun(t, f, ctx, "execute", `const r = await tool("password_gen", "12"); console.log("HASIL:" + r);`)
	if !strings.Contains(out, "TIDAK dijalankan") {
		t.Fatalf("prompt lewat execute tanpa approve = %q", out)
	}
	// Prompt dengan persetujuan lewat execute: jalan.
	done := make(chan string, 1)
	go func() {
		done <- ompRun(t, f, ctx, "execute", `const r = await tool("password_gen", "12"); console.log("HASIL:" + r);`)
	}()
	ompApproveNext(t, f, "tool")
	if out := <-done; !strings.Contains(out, "Password:") {
		t.Fatalf("prompt lewat execute dengan approve = %q", out)
	}
}

// TestHeavy2ToolPolicyDenyMCP: deny pada tool MCP menolak sebelum
// persetujuan MCP diminta.
func TestHeavy2ToolPolicyDenyMCP(t *testing.T) {
	f := setup(t, nil)
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
	f.api("POST", "/api/settings", map[string]any{"settings": map[string]any{
		"toolPolicies": map[string]any{"mcp_uji_echo_text": "deny"},
	}}, 200)
	out := ompRun(t, f, ctx, "mcp_uji_echo_text", `{"text":"halo"}`)
	if !strings.Contains(out, "dilarang") {
		t.Fatalf("deny MCP = %q", out)
	}
	if pending := f.app.pendingApprovals(); len(pending) != 0 {
		t.Fatalf("deny MCP tidak boleh meminta persetujuan, ada %d tertunda", len(pending))
	}
}

// TestHeavy2ASTEditNoApproval: ast_edit apply tanpa persetujuan tidak
// mengubah file; pratinjau juga tidak.
func TestHeavy2ASTEditNoApproval(t *testing.T) {
	if findAstGrep() == "" {
		t.Skip("ast-grep tidak terpasang")
	}
	f := setup(t, nil)
	ctx := ompRoomCtx()
	heavy2ShortApproval(t, 300*time.Millisecond)
	ompWriteProjectFile(t, f, "ast-berat", "main.go", "package main\n\nfunc halo() {}\n\nfunc main() { halo() }\n")
	dir, _ := f.app.ensureProject("ast-berat")
	before, _ := os.ReadFile(filepath.Join(dir, "main.go"))

	out := ompRun(t, f, ctx, "ast_edit", "project:ast-berat\nfunc halo() {}\nfunc halo() { println(\"x\") }\ngo")
	if !strings.Contains(out, "Pratinjau") {
		t.Fatalf("ast_edit pratinjau = %q", out)
	}
	out = ompRun(t, f, ctx, "ast_edit", "project:ast-berat\nfunc halo() {}\nfunc halo() { println(\"x\") }\ngo\napply")
	if !strings.Contains(out, "TIDAK diterapkan") {
		t.Fatalf("ast_edit apply tanpa approve = %q", out)
	}
	after, _ := os.ReadFile(filepath.Join(dir, "main.go"))
	if string(after) != string(before) {
		t.Fatalf("file berubah tanpa persetujuan:\n%s", after)
	}
}

// TestHeavy2LSPRenameNoApproval: lsp_rename tanpa persetujuan ditolak
// dan file tidak berubah.
func TestHeavy2LSPRenameNoApproval(t *testing.T) {
	f := setup(t, nil)
	ctx := ompRoomCtx()
	heavy2ShortApproval(t, 300*time.Millisecond)
	script := filepath.Join(t.TempDir(), "fake_lsp.py")
	if err := os.WriteFile(script, []byte(fakeLSPServer), 0o600); err != nil {
		t.Fatal(err)
	}
	ompWriteProjectFile(t, f, "lsp-berat", "main.go", "package main\n\nfunc halo() {}\n\nfunc main() { halo() }\n")
	f.api("POST", "/api/settings", map[string]any{"settings": map[string]any{
		"lspServers": map[string]any{"go": "python3 " + script},
	}}, 200)

	out := ompRun(t, f, ctx, "lsp_rename", "project:lsp-berat\nmain.go\n3\n6\nmenyapa")
	if !strings.Contains(out, "TIDAK diterapkan") {
		t.Fatalf("lsp_rename tanpa approve = %q", out)
	}
	dir, _ := f.app.ensureProject("lsp-berat")
	raw, _ := os.ReadFile(filepath.Join(dir, "main.go"))
	if strings.Contains(string(raw), "menyapa") {
		t.Fatalf("rename diterapkan tanpa persetujuan:\n%s", raw)
	}
}

// TestHeavy2GitHubPushGuards: github_push ditolak tanpa token, tanpa
// remote, dengan remote non-GitHub, dan tanpa persetujuan.
func TestHeavy2GitHubPushGuards(t *testing.T) {
	f := setup(t, nil)
	ctx := ompRoomCtx()
	heavy2ShortApproval(t, 300*time.Millisecond)

	// Tanpa token.
	out := ompRun(t, f, ctx, "github_push", "project:push-berat")
	if !strings.Contains(out, "token GitHub belum diisi") {
		t.Fatalf("github_push tanpa token = %q", out)
	}

	f.api("POST", "/api/settings", map[string]any{"settings": map[string]any{
		"githubToken": "token-uji-berat",
	}}, 200)
	dir, err := f.app.ensureProject("push-berat")
	if err != nil {
		t.Fatal(err)
	}
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=Uji", "-c", "user.email=uji@localhost"}, args...)...)
		if raw, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %s", args, raw)
		}
	}
	git("init")
	ompWriteProjectFile(t, f, "push-berat", "a.txt", "isi\n")
	git("add", ".")
	git("commit", "-m", "awal")

	// Tanpa remote origin.
	out = ompRun(t, f, ctx, "github_push", "project:push-berat")
	if !strings.Contains(out, "remote") {
		t.Fatalf("github_push tanpa remote = %q", out)
	}
	// Remote non-GitHub.
	git("remote", "add", "origin", "https://example.com/o/r.git")
	out = ompRun(t, f, ctx, "github_push", "project:push-berat")
	if !strings.Contains(out, "bukan GitHub") {
		t.Fatalf("github_push remote non-GitHub = %q", out)
	}
	// Remote GitHub tetapi tanpa persetujuan: push tidak dilakukan.
	git("remote", "set-url", "origin", "https://github.com/o/r.git")
	out = ompRun(t, f, ctx, "github_push", "project:push-berat")
	if !strings.Contains(out, "Push TIDAK dilakukan") {
		t.Fatalf("github_push tanpa approve = %q", out)
	}
	// Token tidak pernah muncul di hasil/log.
	if strings.Contains(out, "token-uji-berat") {
		t.Fatal("token GitHub bocor di hasil github_push")
	}
	logOut := f.api("GET", "/api/tool-log", nil, 200)
	if strings.Contains(fmt.Sprint(logOut), "token-uji-berat") {
		t.Fatal("token GitHub bocor di log audit tool")
	}
}

// TestHeavy2GitHubRedirectSSRF: redirect dari API GitHub ke alamat
// privat harus ditolak penjaga SSRF (tidak mengikuti redirect buta).
func TestHeavy2GitHubRedirectSSRF(t *testing.T) {
	f := setup(t, nil)
	ctx := ompRoomCtx()
	secretServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"full_name":"rahasia/internal","description":"ISI-RAHASIA-SSRF"}`)
	}))
	defer secretServer.Close()
	redirectServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, secretServer.URL+"/repos/rahasia/internal", http.StatusFound)
	}))
	defer redirectServer.Close()
	t.Setenv("MUSEHOOD_GITHUB_API", redirectServer.URL)

	out := ompRun(t, f, ctx, "github_read", "repo o/r")
	// Isi server rahasia tidak boleh terbaca; URL redirect boleh muncul
	// di pesan error (itu target yang ditolak, bukan hasil fetch).
	if strings.Contains(out, "ISI-RAHASIA-SSRF") {
		t.Fatalf("redirect SSRF GitHub lolos: %q", out)
	}
	if !strings.Contains(out, "Error") || !strings.Contains(out, "alamat privat") {
		t.Fatalf("github_read redirect privat harus ditolak penjaga SSRF: %q", out)
	}
}

// TestHeavy2CDPSSRF: tool CDP menolak URL loopback/metadata sebelum
// membuka browser.
func TestHeavy2CDPSSRF(t *testing.T) {
	f := setup(t, nil)
	ctx := ompRoomCtx()
	for _, tc := range []struct{ name, arg string }{
		{"browser_click", "http://127.0.0.1/admin\n#tombol"},
		{"browser_click", "http://169.254.169.254/latest/meta-data/\n#x"},
		{"browser_type", "http://localhost:3001/\n#nama\nteks"},
		{"browser_tabs", "open http://10.0.0.5/internal"},
		{"browser_tabs", "open http://192.168.1.1/admin"},
		{"browser_open", "http://127.0.0.1:3001/api/state"},
		{"browser_links", "http://[::1]/admin"},
	} {
		out := ompRun(t, f, ctx, tc.name, tc.arg)
		if !strings.Contains(out, "Error") {
			t.Errorf("%s %q harus ditolak SSRF, dapat %q", tc.name, tc.arg, out)
		}
	}
}

// TestHeavy2SecurityScanNoSecretLeak: banyak pola secret di banyak
// berkas terdeteksi tanpa satu pun nilai secret tampil di hasil/log.
func TestHeavy2SecurityScanNoSecretLeak(t *testing.T) {
	f := setup(t, nil)
	ctx := ompRoomCtx()
	secrets := map[string]string{
		"config.txt":      "aws_key = \"AKIAIOSFODNN7EXAMPLE\"\n",
		"sub/token.txt":   "token: ghp_1234567890abcdefghijklmnopqrstuvwxyzABCD\n",
		"sub/api.txt":     "api_key = \"sk-1234567890abcdefghijklmnopqrstuv\"\n",
		"sub/pass.txt":    "password = \"kata-sandi-sangat-rahasia\"\n",
		"sub/slack.txt":   "slack: xoxb-123456789012-abcdefghijkl\n",
		"sub/private.pem": "-----BEGIN PRIVATE KEY-----\nMIIBVAIBADANBg==\n-----END PRIVATE KEY-----\n",
	}
	for rel, content := range secrets {
		ompWriteProjectFile(t, f, "scan-berat", rel, content)
	}
	ompWriteProjectFile(t, f, "scan-berat", "bersih.txt", "tidak ada secret di sini\n")

	out := ompRun(t, f, ctx, "security_scan", "project:scan-berat")
	for _, value := range []string{
		"AKIAIOSFODNN7EXAMPLE", "ghp_1234567890abcdefghijklmnopqrstuvwxyzABCD",
		"sk-1234567890abcdefghijklmnopqrstuv", "kata-sandi-sangat-rahasia",
		"xoxb-123456789012-abcdefghijkl", "MIIBVAIBADANBg",
	} {
		if strings.Contains(out, value) {
			t.Fatalf("security_scan membocorkan nilai secret %q:\n%s", value, out)
		}
	}
	for _, rel := range []string{"config.txt", "token.txt", "api.txt", "pass.txt", "slack.txt", "private.pem"} {
		if !strings.Contains(out, rel) {
			t.Errorf("temuan %s tidak dilaporkan:\n%s", rel, out)
		}
	}
	if !strings.Contains(out, "nilai disembunyikan") {
		t.Fatalf("security_scan harus menandai nilai disembunyikan:\n%s", out)
	}
	logOut := f.api("GET", "/api/tool-log", nil, 200)
	if strings.Contains(fmt.Sprint(logOut), "kata-sandi-sangat-rahasia") {
		t.Fatal("secret bocor di log audit tool")
	}
}

// TestHeavy2EvalDangerousGate: kode eval berbahaya butuh persetujuan;
// tanpa persetujuan kernel tidak dibuat dan kode tidak jalan.
func TestHeavy2EvalDangerousGate(t *testing.T) {
	f := setup(t, nil)
	heavy2StopAllEval()
	defer heavy2StopAllEval()
	heavy2ShortApproval(t, 300*time.Millisecond)
	ctx := heavy2Ctx("heavy2-eval-gate")
	s := heavy2Seat("M01", "Plamo")

	out := heavy2Run(f, ctx, s, "eval", "python\n__import__(\"os\")")
	if !strings.Contains(out, "TIDAK dijalankan") {
		t.Fatalf("eval berbahaya tanpa approve = %q", out)
	}
	evalMu.Lock()
	_, exists := evalSessions[evalKey("heavy2-eval-gate", "M01", "python")]
	evalMu.Unlock()
	if exists {
		t.Fatal("kernel eval dibuat meski persetujuan ditolak")
	}

	// Dengan persetujuan: kode jalan di sandbox.
	done := make(chan string, 1)
	go func() { done <- heavy2Run(f, ctx, s, "eval", "python\n__import__(\"os\")") }()
	ompApproveNext(t, f, "code")
	if out := <-done; !strings.Contains(out, "module") {
		t.Fatalf("eval berbahaya dengan approve = %q", out)
	}
}

// TestHeavy2RewindToolNeedsApproval: rewind lewat tool tanpa
// persetujuan tidak memotong pesan apa pun.
func TestHeavy2RewindToolNeedsApproval(t *testing.T) {
	f := setup(t, nil)
	ctx := ompRoomCtx()
	heavy2ShortApproval(t, 300*time.Millisecond)
	f.app.mu.Lock()
	f.app.data.Messages = append(f.app.data.Messages,
		&message{ID: uuid(), CreatedAt: now(), AuthorType: "owner", Text: "satu", Status: "completed", RoomID: defaultRoomID, BranchID: "main"})
	f.app.mu.Unlock()
	if out := ompRun(t, f, ctx, "checkpoint", "titik-berat"); !strings.Contains(out, "Checkpoint dibuat") {
		t.Fatalf("checkpoint = %q", out)
	}
	f.app.mu.Lock()
	f.app.data.Messages = append(f.app.data.Messages,
		&message{ID: uuid(), CreatedAt: now(), AuthorType: "owner", Text: "dua", Status: "completed", RoomID: defaultRoomID, BranchID: "main"})
	f.app.mu.Unlock()

	out := ompRun(t, f, ctx, "rewind", "titik-berat")
	if !strings.Contains(out, "TIDAK dilakukan") {
		t.Fatalf("rewind tanpa approve = %q", out)
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
		t.Fatalf("pesan sesudah rewind ditolak = %d, mau tetap 2", count)
	}
}
