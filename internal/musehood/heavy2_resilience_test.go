package musehood

// Tes berat ronde 2 — ketahanan (28 Nov 2026): kernel eval dibunuh di
// tengah panggilan lalu pulih, LSP server mati/menggantung tidak
// membuat tool hang, sesi debug tanpa dlv memberi pesan instalasi,
// browser tanpa Chromium berdegradasi anggun, dan browser CDP yang
// dibunuh bisa dimulai lagi.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestHeavy2EvalKernelKilledMidCall: kernel yang dibunuh saat panggilan
// berjalan membuat panggilan itu error cepat (bukan hang 30 dtk), dan
// panggilan berikutnya langsung pulih dengan sesi baru.
func TestHeavy2EvalKernelKilledMidCall(t *testing.T) {
	f := setup(t, nil)
	heavy2StopAllEval()
	defer heavy2StopAllEval()
	ctx := heavy2Ctx("heavy2-eval-kill")
	s := heavy2Seat("M01", "Plamo")
	key := evalKey("heavy2-eval-kill", "M01", "python")

	if out := heavy2Run(f, ctx, s, "eval", "python\nnilai = 5"); strings.Contains(out, "[error]") {
		t.Fatalf("eval awal = %q", out)
	}

	// Panggilan lambat di latar, lalu bunuh proses kernelnya.
	done := make(chan string, 1)
	go func() {
		done <- heavy2Run(f, ctx, s, "eval", "python\nimport time\ntime.sleep(10)\nprint('tidak akan sampai')")
	}()
	// Beri waktu panggilan masuk ke kernel.
	time.Sleep(700 * time.Millisecond)
	evalMu.Lock()
	session := evalSessions[key]
	evalMu.Unlock()
	if session == nil || session.cmd == nil || session.cmd.Process == nil {
		t.Fatal("sesi eval tidak ditemukan untuk dibunuh")
	}
	_ = session.cmd.Process.Kill()

	start := time.Now()
	select {
	case out := <-done:
		if !strings.Contains(out, "[error]") {
			t.Fatalf("panggilan yang kernelnya dibunuh = %q, mau error", out)
		}
		t.Logf("panggilan terputus kembali dalam %v", time.Since(start))
	case <-time.After(10 * time.Second):
		t.Fatal("panggilan eval hang sesudah kernel dibunuh")
	}

	// Panggilan berikutnya langsung pulih (sesi baru, state kosong).
	out := heavy2Run(f, ctx, s, "eval", "python\nprint(20 + 22)")
	if !strings.Contains(out, "42") {
		t.Fatalf("eval sesudah kernel dibunuh tidak pulih: %q", out)
	}
	out = heavy2Run(f, ctx, s, "eval", "python\nprint(nilai)")
	if !strings.Contains(out, "[error]") {
		t.Fatalf("state sesi lama harus hilang sesudah kernel mati: %q", out)
	}
}

// TestHeavy2LSPDeadServerNoHang: language server yang langsung mati
// atau menggantung menghasilkan error jelas dalam batas waktu konteks,
// bukan hang.
func TestHeavy2LSPDeadServerNoHang(t *testing.T) {
	f := setup(t, nil)
	ompWriteProjectFile(t, f, "lsp-mati", "main.go", "package main\n\nfunc halo() {}\n")

	// Server langsung keluar.
	deadScript := filepath.Join(t.TempDir(), "dead_lsp.py")
	if err := os.WriteFile(deadScript, []byte("import sys\nsys.exit(1)\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	f.api("POST", "/api/settings", map[string]any{"settings": map[string]any{
		"lspServers": map[string]any{"go": "python3 " + deadScript},
	}}, 200)
	ctx, cancel := context.WithTimeout(ompRoomCtx(), 10*time.Second)
	start := time.Now()
	out := heavy2Run(f, ctx, ompSeat(), "lsp_definition", "project:lsp-mati\nmain.go\n3\n6")
	cancel()
	if !strings.Contains(out, "Error") {
		t.Fatalf("lsp server mati = %q, mau error jelas", out)
	}
	if elapsed := time.Since(start); elapsed > 8*time.Second {
		t.Fatalf("lsp server mati terlalu lambat: %v", elapsed)
	}
	t.Logf("lsp server mati error dalam %v: %.120q", time.Since(start), out)

	// Server menggantung (tidak pernah menjawab initialize).
	hangScript := filepath.Join(t.TempDir(), "hang_lsp.py")
	if err := os.WriteFile(hangScript, []byte("import sys, time\nfor line in sys.stdin:\n    time.sleep(60)\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	f.api("POST", "/api/settings", map[string]any{"settings": map[string]any{
		"lspServers": map[string]any{"go": "python3 " + hangScript},
	}}, 200)
	ctx, cancel = context.WithTimeout(ompRoomCtx(), 3*time.Second)
	start = time.Now()
	out = heavy2Run(f, ctx, ompSeat(), "lsp_definition", "project:lsp-mati\nmain.go\n3\n6")
	cancel()
	if !strings.Contains(out, "Error") {
		t.Fatalf("lsp server menggantung = %q, mau error jelas", out)
	}
	if elapsed := time.Since(start); elapsed > 8*time.Second {
		t.Fatalf("lsp server menggantung tidak dibatasi konteks: %v", elapsed)
	}
	t.Logf("lsp server menggantung error dalam %v: %.120q", time.Since(start), out)
}

// TestHeavy2LSPDiagnosticsNoDiagnostics: server yang tidak mengirim
// diagnostik sama sekali: lsp_diagnostics selesai rapi dan penutupan
// client tidak hang (jalur drain + shutdown).
func TestHeavy2LSPDiagnosticsNoDiagnostics(t *testing.T) {
	f := setup(t, nil)
	ompWriteProjectFile(t, f, "lsp-sepi", "main.go", "package main\n\nfunc halo() {}\n")
	quietScript := filepath.Join(t.TempDir(), "quiet_lsp.py")
	quiet := `import sys, json

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

while True:
    try:
        msg = read_msg()
    except Exception:
        break
    method = msg.get("method", "")
    mid = msg.get("id")
    if method == "initialize":
        send({"jsonrpc": "2.0", "id": mid, "result": {"capabilities": {}}})
    elif method == "shutdown":
        send({"jsonrpc": "2.0", "id": mid, "result": None})
    elif method == "exit":
        break
`
	if err := os.WriteFile(quietScript, []byte(quiet), 0o600); err != nil {
		t.Fatal(err)
	}
	f.api("POST", "/api/settings", map[string]any{"settings": map[string]any{
		"lspServers": map[string]any{"go": "python3 " + quietScript},
	}}, 200)

	done := make(chan string, 1)
	go func() { done <- ompRun(t, f, ompRoomCtx(), "lsp_diagnostics", "project:lsp-sepi\nmain.go") }()
	select {
	case out := <-done:
		if !strings.Contains(out, "Tidak ada diagnostik") {
			t.Fatalf("lsp_diagnostics tanpa diagnostik = %q", out)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("lsp_diagnostics hang saat server tidak mengirim diagnostik")
	}
}

// TestHeavy2DebugWithoutDelve: tanpa dlv, debug_launch memberi pesan
// instalasi yang jelas (bukan error mentah/panic).
func TestHeavy2DebugWithoutDelve(t *testing.T) {
	f := setup(t, nil)
	emptyDir := t.TempDir()
	t.Setenv("MUSEHOOD_DLV", filepath.Join(emptyDir, "dlv-tidak-ada"))
	t.Setenv("HOME", emptyDir)
	t.Setenv("PATH", emptyDir)
	if got := findDelve(); got != "" {
		t.Skipf("dlv masih ditemukan di %q; lingkungan tidak bisa disimulasikan kosong", got)
	}
	out := ompRun(t, f, ompRoomCtx(), "debug_launch", "project:apa-saja\n.")
	if !strings.Contains(out, "tidak ditemukan") || !strings.Contains(out, "go install") {
		t.Fatalf("debug_launch tanpa dlv = %q, mau pesan instalasi", out)
	}
}

// TestHeavy2DebugStopConcurrent: debug_stop bersamaan dengan
// debug_inspect/debug_step tidak hang dan tidak merusak sesi.
func TestHeavy2DebugStopConcurrent(t *testing.T) {
	f := setup(t, nil)
	ctx := ompRoomCtx()
	script := filepath.Join(t.TempDir(), "fake-dlv")
	if err := os.WriteFile(script, []byte(fakeDAPServer), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MUSEHOOD_DLV", script)
	ompWriteProjectFile(t, f, "debug-berat", "main.go", "package main\n\nfunc main() {}\n")

	if out := ompRun(t, f, ctx, "debug_launch", "project:debug-berat\n."); !strings.Contains(out, "Sesi debug dimulai") {
		t.Fatalf("debug_launch = %q", out)
	}
	var wg sync.WaitGroup
	results := make(chan string, 4)
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- ompRun(t, f, ctx, "debug_inspect", "")
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		time.Sleep(50 * time.Millisecond)
		results <- ompRun(t, f, ctx, "debug_stop", "")
	}()
	doneAll := make(chan struct{})
	go func() { wg.Wait(); close(doneAll) }()
	select {
	case <-doneAll:
	case <-time.After(30 * time.Second):
		t.Fatal("debug_stop bersamaan hang")
	}
	close(results)
	stopped := false
	for out := range results {
		if strings.Contains(out, "dihentikan") {
			stopped = true
		}
		if out == "" {
			t.Fatal("hasil debug bersamaan kosong")
		}
	}
	if !stopped {
		t.Fatal("debug_stop tidak melaporkan sesi dihentikan")
	}
	// Sesi sudah tidak ada: inspect berikutnya memberi pesan jelas.
	out := ompRun(t, f, ctx, "debug_inspect", "")
	if !strings.Contains(out, "belum ada sesi debug") {
		t.Fatalf("debug_inspect sesudah stop = %q", out)
	}
}

// TestHeavy2BrowserWithoutChromium: tanpa Chromium, semua tool
// browser berdegradasi dengan pesan jelas berbahasa Indonesia.
func TestHeavy2BrowserWithoutChromium(t *testing.T) {
	f := setup(t, nil)
	ctx := ompRoomCtx()
	stopCDPBrowser()
	oldFind := findChromium
	findChromium = func() string { return "" }
	t.Cleanup(func() { findChromium = oldFind })
	stubLookup(t, "93.184.216.34")

	for _, tc := range []struct{ name, arg string }{
		{"browser_click", "https://example.com/\n#tombol"},
		{"browser_type", "https://example.com/\n#nama\nteks"},
		{"browser_snapshot", "https://example.com/"},
		{"browser_tabs", "list"},
		{"browser_open", "https://example.com/"},
		{"browser_links", "https://example.com/"},
		{"browser_screenshot", "https://example.com/"},
	} {
		out := ompRun(t, f, ctx, tc.name, tc.arg)
		if !strings.Contains(strings.ToLower(out), "chromium") {
			t.Errorf("%s tanpa Chromium = %q, mau pesan Chromium tidak ditemukan", tc.name, out)
		}
	}
}

// TestHeavy2CDPBrowserKilledRecovery: browser CDP yang prosesnya
// dibunuh terdeteksi mati dan start berikutnya membuat browser baru.
func TestHeavy2CDPBrowserKilledRecovery(t *testing.T) {
	if findChromium() == "" {
		t.Skip("Chromium tidak tersedia")
	}
	browser, err := startCDPBrowser()
	if err != nil {
		t.Skip("CDP tidak bisa dimulai: " + err.Error())
	}
	if _, err := browser.listTargets(); err != nil {
		stopCDPBrowser()
		t.Fatalf("listTargets awal: %v", err)
	}
	_ = browser.cmd.Process.Kill()
	// Beri waktu proses benar-benar mati.
	time.Sleep(300 * time.Millisecond)

	browser2, err := startCDPBrowser()
	if err != nil {
		t.Fatalf("startCDPBrowser sesudah dibunuh: %v", err)
	}
	defer stopCDPBrowser()
	if _, err := browser2.listTargets(); err != nil {
		t.Fatalf("browser baru tidak merespons: %v", err)
	}
}
