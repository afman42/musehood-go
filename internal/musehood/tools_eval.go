package musehood

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// --- Tool eval (fitur omp 3): kernel persisten Python + JavaScript ---
//
// Satu proses interpreter per (ruang+kursi+bahasa) yang bertahan antar
// panggilan sehingga variabel/fungsi tetap hidup. Protokol: JSON per
// baris di stdin/stdout ({code} -> {output, error}). Kernel berjalan di
// sandbox bubblewrap yang sama dengan run_code (uid nobody, tanpa
// jaringan) dengan folder kerja sandbox persisten per sesi. Maks 8
// sesi; sesi idle >10 menit dimatikan. Argumen "reset" (baris kedua)
// mengosongkan state. Gate sama seperti run_code (code=true + pola
// berbahaya -> approval kind "code").

const (
	evalMaxSessions = 8
	evalIdleLimit   = 10 * time.Minute
	evalCallTimeout = 30 * time.Second
)

const evalPythonKernel = `import sys, json, io, contextlib
ns = {"__name__": "__main__"}
for line in sys.stdin:
    line = line.strip()
    if not line:
        continue
    try:
        req = json.loads(line)
    except Exception:
        continue
    buf = io.StringIO()
    err = ""
    try:
        with contextlib.redirect_stdout(buf), contextlib.redirect_stderr(buf):
            try:
                result = eval(req.get("code", ""), ns)
                if result is not None:
                    print(repr(result))
            except SyntaxError:
                exec(req.get("code", ""), ns)
    except BaseException as e:
        err = type(e).__name__ + ": " + str(e)
    print(json.dumps({"output": buf.getvalue(), "error": err}), flush=True)
`

const evalNodeKernel = `const readline = require("readline");
const rl = readline.createInterface({ input: process.stdin });
const vm = require("vm");
const sandbox = { console, require, process, Buffer, setTimeout, clearTimeout, setInterval, clearInterval };
sandbox.globalThis = sandbox;
const ctx = vm.createContext(sandbox);
rl.on("line", (line) => {
  if (!line.trim()) return;
  let req;
  try { req = JSON.parse(line); } catch { return; }
  let out = [];
  const origLog = console.log, origError = console.error, origWarn = console.warn;
  const cap = (...a) => out.push(a.map(String).join(" "));
  console.log = cap; console.error = cap; console.warn = cap;
  let err = "";
  try {
    const result = vm.runInContext(req.code || "", ctx, { timeout: 28000 });
    if (result !== undefined) out.push(String(result));
  } catch (e) { err = String(e && e.message ? e.message : e); }
  console.log = origLog; console.error = origError; console.warn = origWarn;
  process.stdout.write(JSON.stringify({ output: out.join("\n"), error: err }) + "\n");
});
`

type evalSession struct {
	key      string
	language string
	cmd      *exec.Cmd
	stdin    io.WriteCloser
	reader   *bufio.Reader
	stderr   *ringLog
	done     chan struct{}
	mu       sync.Mutex
	lastUsed time.Time
}

var (
	evalMu       sync.Mutex
	evalSessions = map[string]*evalSession{}
)

func evalKey(roomID, seatCode, language string) string {
	return roomID + "|" + seatCode + "|" + language
}

// evalBwrapArgs seperti sandboxBwrapArgs tetapi tanpa pembungkus
// `timeout` (kernel harus hidup lintas panggilan).
func evalBwrapArgs(dir, interpreter, name string) []string {
	return []string{
		"--ro-bind", "/usr", "/usr",
		"--ro-bind", "/lib", "/lib",
		"--ro-bind", "/lib64", "/lib64",
		"--ro-bind", "/bin", "/bin",
		"--ro-bind", "/etc/alternatives", "/etc/alternatives",
		"--dev", "/dev",
		"--tmpfs", "/tmp",
		"--bind", dir, "/work",
		"--chdir", "/work",
		"--unshare-user", "--unshare-ipc", "--unshare-net", "--unshare-uts", "--unshare-cgroup",
		"--die-with-parent",
		"--uid", "65534", "--gid", "65534",
		"--clearenv", "--setenv", "PATH", "/usr/bin:/bin",
		interpreter, name,
	}
}

// startEvalSession menjalankan kernel baru di sandbox bubblewrap.
func (a *App) startEvalSession(language, key string) (*evalSession, error) {
	workDir, err := os.MkdirTemp(a.dir, "eval-")
	if err != nil {
		return nil, err
	}
	// a.dir bisa relatif (direktori kerja): bwrap membutuhkan path
	// absolut untuk bind mount sumber.
	if abs, err := filepath.Abs(workDir); err == nil {
		workDir = abs
	}
	// Kernel berjalan sebagai nobody di sandbox: folder + skrip harus
	// bisa dibaca user itu (pola yang sama dengan sandbox run_code).
	if err := os.Chmod(workDir, 0o755); err != nil {
		return nil, err
	}
	var ext, interpreter, kernel string
	switch language {
	case "python":
		ext, interpreter, kernel = "py", "python3", evalPythonKernel
	case "javascript":
		ext, interpreter, kernel = "js", "node", evalNodeKernel
	}
	name := "kernel." + ext
	if err := os.WriteFile(filepath.Join(workDir, name), []byte(kernel), 0o644); err != nil {
		return nil, err
	}
	bwrapArgs := evalBwrapArgs(workDir, interpreter, name)
	// systemd-run --scope gagal dari dalam service systemd ("Failed to
	// connect to bus"); pola yang sama dengan executeCodeStdin: coba
	// systemd-run dulu, bila proses langsung mati, jatuh ke bwrap
	// langsung.
	attempts := [][]string{
		append([]string{"systemd-run", "--quiet", "--scope", "-p", "MemoryMax=512M", "-p", "TasksMax=64", "-p", "CPUQuota=50%", "bwrap"}, bwrapArgs...),
		append([]string{"bwrap"}, bwrapArgs...),
	}
	var lastErr error
	for _, argv := range attempts {
		session, err := startEvalProcess(argv, workDir, language, key)
		if err == nil {
			return session, nil
		}
		lastErr = err
	}
	return nil, lastErr
}

// startEvalProcess menjalankan satu percobaan kernel dan memastikan
// prosesnya tidak langsung mati (jendela 400 ms).
func startEvalProcess(argv []string, workDir, language, key string) (*evalSession, error) {
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = workDir
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=/tmp", "NO_COLOR=1"}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderrLog := &ringLog{}
	cmd.Stderr = stderrLog
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	session := &evalSession{
		key: key, language: language, cmd: cmd, stdin: stdin,
		reader: bufio.NewReader(stdout), stderr: stderrLog,
		done: make(chan struct{}), lastUsed: time.Now(),
	}
	go func() {
		_ = cmd.Wait()
		close(session.done)
	}()
	select {
	case <-session.done:
		tail := strings.Join(stderrLog.tail(5), " ")
		if tail == "" {
			tail = "proses langsung berakhir"
		}
		return nil, fmt.Errorf("%s: %s", argv[0], tail)
	case <-time.After(400 * time.Millisecond):
		return session, nil
	}
}

func (s *evalSession) stop() {
	_ = s.stdin.Close()
	if s.cmd.Process != nil {
		_ = s.cmd.Process.Kill()
	}
	if s.done != nil {
		select {
		case <-s.done:
		case <-time.After(2 * time.Second):
		}
	}
}

// stderrTail mengembalikan ekor stderr kernel untuk pesan error.
func (s *evalSession) stderrTail() string {
	if s.stderr == nil {
		return ""
	}
	tail := strings.Join(s.stderr.tail(3), " ")
	if tail == "" {
		return ""
	}
	return "stderr: " + tail
}

// evalCall mengirim satu potong kode ke kernel dan menunggu respons.
func (s *evalSession) evalCall(ctx context.Context, code string) (string, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastUsed = time.Now()
	req, _ := json.Marshal(map[string]string{"code": code})
	if _, err := s.stdin.Write(append(req, '\n')); err != nil {
		return "", "kernel eval tidak merespons (proses mati); panggil lagi untuk sesi baru. " + s.stderrTail()
	}
	type result struct {
		line string
		err  error
	}
	done := make(chan result, 1)
	go func() {
		line, err := s.reader.ReadString('\n')
		done <- result{line, err}
	}()
	callCtx, cancel := context.WithTimeout(ctx, evalCallTimeout)
	defer cancel()
	select {
	case <-callCtx.Done():
		s.stop()
		return "", "eval melewati batas 30 detik; sesi dihentikan"
	case res := <-done:
		if res.err != nil {
			return "", "kernel eval berakhir: " + res.err.Error() + ". " + s.stderrTail()
		}
		var resp struct {
			Output string `json:"output"`
			Error  string `json:"error"`
		}
		if err := json.Unmarshal([]byte(res.line), &resp); err != nil {
			return "", "respons kernel tidak valid"
		}
		return resp.Output, resp.Error
	}
}

// pruneEvalSessions mematikan sesi idle dan menegakkan batas jumlah.
func pruneEvalSessions() {
	evalMu.Lock()
	defer evalMu.Unlock()
	now := time.Now()
	for key, s := range evalSessions {
		if now.Sub(s.lastUsed) > evalIdleLimit {
			s.stop()
			delete(evalSessions, key)
		}
	}
	for len(evalSessions) > evalMaxSessions {
		var oldestKey string
		var oldest time.Time
		for key, s := range evalSessions {
			if oldestKey == "" || s.lastUsed.Before(oldest) {
				oldestKey, oldest = key, s.lastUsed
			}
		}
		evalSessions[oldestKey].stop()
		delete(evalSessions, oldestKey)
	}
}

func (a *App) toolEval(ctx context.Context, s seat, arg string) string {
	language, code, found := strings.Cut(arg, "\n")
	language = strings.ToLower(strings.TrimSpace(language))
	switch language {
	case "py":
		language = "python"
	case "js", "node":
		language = "javascript"
	case "python", "javascript":
	default:
		return "Error: format eval — baris pertama bahasa (python/javascript), baris berikutnya kode (atau \"reset\")."
	}
	info := toolRunInfoFrom(ctx)
	key := evalKey(info.RoomID, s.Code, language)
	if !found || strings.TrimSpace(code) == "reset" {
		evalMu.Lock()
		if existing, ok := evalSessions[key]; ok {
			existing.stop()
			delete(evalSessions, key)
		}
		evalMu.Unlock()
		return fmt.Sprintf("Sesi eval %s direset; state dikosongkan.", language)
	}
	if len(code) > maxSandboxFileBytes {
		return "Error: kode terlalu besar (maks 100 KB)."
	}
	if match, dangerous := dangerousCode(code); dangerous {
		if !a.requestApproval(ctx, s, "code", language, code) {
			return fmt.Sprintf("Kode TIDAK dijalankan: terdeteksi pola berbahaya (%q) dan pemilik menolak atau tidak menyetujui dalam %d detik.", match, int(approvalTimeout.Seconds()))
		}
	}
	pruneEvalSessions()
	evalMu.Lock()
	session, ok := evalSessions[key]
	if !ok {
		if len(evalSessions) >= evalMaxSessions {
			evalMu.Unlock()
			return fmt.Sprintf("Error: batas %d sesi eval tercapai; hentikan sesi lain lewat hub_stop atau tunggu sesi idle berakhir.", evalMaxSessions)
		}
		var err error
		session, err = a.startEvalSession(language, key)
		if err != nil {
			evalMu.Unlock()
			return "Error: gagal memulai kernel eval: " + err.Error()
		}
		evalSessions[key] = session
	}
	evalMu.Unlock()
	output, errText := session.evalCall(ctx, code)
	if errText != "" && strings.Contains(errText, "sesi dihentikan") || strings.Contains(errText, "proses mati") {
		evalMu.Lock()
		delete(evalSessions, key)
		evalMu.Unlock()
	}
	if errText != "" {
		if output != "" {
			return truncateToolResult(output + "\n[error] " + errText)
		}
		return truncateToolResult("[error] " + errText)
	}
	if strings.TrimSpace(output) == "" {
		return "(tidak ada keluaran)"
	}
	return truncateToolResult(output)
}

// evalSessionList mengembalikan daftar sesi eval hidup (untuk hub).
func evalSessionList() []string {
	pruneEvalSessions()
	evalMu.Lock()
	defer evalMu.Unlock()
	out := make([]string, 0, len(evalSessions))
	for key := range evalSessions {
		out = append(out, "eval:"+key)
	}
	return out
}

// stopEvalSession menghentikan sesi eval berdasarkan kunci hub.
func stopEvalSession(key string) bool {
	evalMu.Lock()
	defer evalMu.Unlock()
	session, ok := evalSessions[key]
	if !ok {
		return false
	}
	session.stop()
	delete(evalSessions, key)
	return true
}
