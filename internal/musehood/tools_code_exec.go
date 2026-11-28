package musehood

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// --- Grup C: build, tes, kualitas, server, shell allowlist ---

// findGoToolchain mencari GOROOT dari binary go yang tersedia.
func findGoToolchain() string {
	candidates := []string{}
	if path, err := exec.LookPath("go"); err == nil {
		candidates = append(candidates, path)
	}
	candidates = append(candidates,
		"/home/hatch/go-toolchain/go/bin/go",
		"/usr/local/go/bin/go",
		"/usr/lib/go/bin/go",
	)
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return filepath.Dir(filepath.Dir(c))
		}
	}
	return ""
}

// projectSandboxArgs membangun argumen bwrap untuk menjalankan perintah
// di folder proyek (tanpa jaringan, user nobody, proyek ter-bind rw).
func projectSandboxArgs(projectDir string, argv []string, extraEnv []string) []string {
	args := []string{
		"--ro-bind", "/usr", "/usr",
		"--ro-bind", "/lib", "/lib",
		"--ro-bind", "/lib64", "/lib64",
		"--ro-bind", "/bin", "/bin",
		"--ro-bind", "/etc/alternatives", "/etc/alternatives",
		"--dev", "/dev",
		"--tmpfs", "/tmp",
		"--bind", projectDir, "/work",
		"--chdir", "/work",
		"--unshare-user", "--unshare-ipc", "--unshare-net", "--unshare-uts", "--unshare-cgroup",
		"--die-with-parent",
		"--uid", "65534", "--gid", "65534",
		"--clearenv",
	}
	path := "/usr/bin:/bin"
	if goroot := findGoToolchain(); goroot != "" {
		args = append(args, "--ro-bind", goroot, "/goroot")
		path = "/goroot/bin:" + path
		// Binary go ter-trimpath tidak bisa menurunkan GOROOT dari
		// path-nya sendiri; tetapkan eksplisit.
		args = append(args, "--setenv", "GOROOT", "/goroot", "--setenv", "GOTELEMETRY", "off")
	}
	args = append(args,
		"--setenv", "PATH", path,
		"--setenv", "HOME", "/tmp",
		"--setenv", "GOCACHE", "/work/.gocache",
		"--setenv", "GOMODCACHE", "/work/.gomodcache",
		"--setenv", "GOPROXY", "off",
		"--setenv", "GOFLAGS", "-mod=mod",
		"--setenv", "GOTOOLCHAIN", "local",
		"--setenv", "CGO_ENABLED", "0",
		"--setenv", "npm_config_cache", "/tmp/.npm",
		"--setenv", "PYTHONDONTWRITEBYTECODE", "1",
	)
	for _, kv := range extraEnv {
		k, v, _ := strings.Cut(kv, "=")
		args = append(args, "--setenv", k, v)
	}
	return append(args, argv...)
}

// execInProject menjalankan argv di sandbox proyek dengan timeout detik.
// Mengembalikan keluaran gabungan dan error proses (nil bila exit 0).
func (a *App) execInProject(ctx context.Context, projectDir string, argv []string, timeoutSec int, extraEnv []string) (string, error) {
	bwrapArgs := projectSandboxArgs(projectDir, append([]string{"timeout", fmt.Sprint(timeoutSec)}, argv...), extraEnv)
	runCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSec+20)*time.Second)
	defer cancel()
	scopeArgs := append([]string{"--quiet", "--scope", "-p", "MemoryMax=1G", "-p", "TasksMax=256", "-p", "CPUQuota=100%", "bwrap"}, bwrapArgs...)
	cmd := exec.CommandContext(runCtx, "systemd-run", scopeArgs...)
	out, err := cmd.CombinedOutput()
	if err != nil && strings.Contains(string(out), "Failed to") {
		cmd = exec.CommandContext(runCtx, "bwrap", bwrapArgs...)
		out, err = cmd.CombinedOutput()
	}
	return string(out), err
}

func tailLines(text string, n int) string {
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

func projectHasFile(dir, name string) bool {
	_, err := os.Stat(filepath.Join(dir, name))
	return err == nil
}

func npmScript(dir, script string) bool {
	raw, err := os.ReadFile(filepath.Join(dir, "package.json"))
	if err != nil {
		return false
	}
	return strings.Contains(string(raw), `"`+script+`"`)
}

func (a *App) toolRunBuild(ctx context.Context, arg string) string {
	project, _ := parseProjectArg(arg)
	dir, err := a.ensureProject(project)
	if err != nil {
		return "Error: " + err.Error()
	}
	var argv []string
	var label string
	switch {
	case projectHasFile(dir, "go.mod"):
		if findGoToolchain() == "" {
			return "Error: toolchain Go tidak ditemukan di server."
		}
		argv, label = []string{"go", "build", "./..."}, "go build ./..."
	case projectHasFile(dir, "package.json") && npmScript(dir, "build"):
		argv, label = []string{"npm", "run", "build"}, "npm run build"
	default:
		hasPy := false
		walkProject(dir, 200, func(rel, full string, info os.FileInfo) bool {
			if strings.HasSuffix(rel, ".py") {
				hasPy = true
				return false
			}
			return true
		})
		if !hasPy {
			return "Error: tidak ada sistem build terdeteksi (go.mod, package.json dengan script build, atau file .py)."
		}
		argv, label = []string{"python3", "-m", "compileall", "-q", "."}, "python3 -m compileall"
	}
	out, err := a.execInProject(ctx, dir, argv, 120, nil)
	if err != nil {
		return truncateToolResult(fmt.Sprintf("Build GAGAL (%s) di proyek %q:\n%s", label, project, tailLines(out, 50)))
	}
	summary := fmt.Sprintf("Build SUKSES (%s) di proyek %q.", label, project)
	if strings.TrimSpace(out) != "" {
		summary += "\n" + tailLines(out, 20)
	}
	return truncateToolResult(summary)
}

var (
	pytestPassedPattern = regexp.MustCompile(`(\d+) passed`)
	pytestFailedPattern = regexp.MustCompile(`(\d+) failed`)
	pytestErrorPattern  = regexp.MustCompile(`(\d+) errors?`)
)

func (a *App) toolRunTests(ctx context.Context, arg string) string {
	project, _ := parseProjectArg(arg)
	dir, err := a.ensureProject(project)
	if err != nil {
		return "Error: " + err.Error()
	}
	var argv []string
	var label string
	switch {
	case projectHasFile(dir, "go.mod"):
		if findGoToolchain() == "" {
			return "Error: toolchain Go tidak ditemukan di server."
		}
		argv, label = []string{"go", "test", "./..."}, "go test ./..."
	case projectHasFile(dir, "package.json") && npmScript(dir, "test"):
		argv, label = []string{"npm", "test"}, "npm test"
	default:
		hasPyTest := false
		walkProject(dir, 500, func(rel, full string, info os.FileInfo) bool {
			base := filepath.Base(rel)
			if (strings.HasPrefix(base, "test_") || strings.HasSuffix(base, "_test.py")) && strings.HasSuffix(base, ".py") {
				hasPyTest = true
				return false
			}
			return true
		})
		if !hasPyTest {
			return "Error: tidak ada tes terdeteksi (go.mod, package.json dengan script test, atau file test_*.py/*_test.py)."
		}
		argv, label = []string{"python3", "-m", "pytest", "-q"}, "pytest"
	}
	out, err := a.execInProject(ctx, dir, argv, 180, nil)
	if label == "pytest" && strings.Contains(out, "No module named pytest") {
		return "Error: pytest tidak terpasang di server; tes Python tidak bisa dijalankan."
	}
	var summary string
	switch label {
	case "go test ./...":
		pass := strings.Count(out, "--- PASS")
		fail := strings.Count(out, "--- FAIL")
		pkgOK := strings.Count(out, "\nok  \t") + strings.Count(out, "\nok \t")
		summary = fmt.Sprintf("go test: %d tes lulus, %d tes gagal (%d paket ok).", pass, fail, pkgOK)
	case "pytest":
		get := func(re *regexp.Regexp) string {
			if m := re.FindStringSubmatch(out); m != nil {
				return m[1]
			}
			return "0"
		}
		summary = fmt.Sprintf("pytest: %s lulus, %s gagal, %s error.", get(pytestPassedPattern), get(pytestFailedPattern), get(pytestErrorPattern))
	default:
		if err == nil {
			summary = label + ": SUKSES (exit 0)."
		} else {
			summary = label + ": GAGAL."
		}
	}
	status := "SUKSES"
	if err != nil {
		status = "GAGAL"
	}
	return truncateToolResult(fmt.Sprintf("Tes %s di proyek %q. %s\n%s", status, project, summary, tailLines(out, 40)))
}

func (a *App) toolRunLint(ctx context.Context, arg string) string {
	project, _ := parseProjectArg(arg)
	dir, err := a.ensureProject(project)
	if err != nil {
		return "Error: " + err.Error()
	}
	var sections []string
	ran := false
	if projectHasFile(dir, "go.mod") {
		ran = true
		out, err := a.execInProject(ctx, dir, []string{"go", "vet", "./..."}, 120, nil)
		if err != nil {
			sections = append(sections, "go vet:\n"+tailLines(out, 40))
		} else {
			sections = append(sections, "go vet: bersih.")
		}
		out, _ = a.execInProject(ctx, dir, []string{"gofmt", "-l", "."}, 60, nil)
		if strings.TrimSpace(out) != "" {
			sections = append(sections, "gofmt (file belum terformat):\n"+tailLines(out, 40))
		} else {
			sections = append(sections, "gofmt: semua file terformat.")
		}
	}
	var pyFiles []string
	walkProject(dir, 500, func(rel, full string, info os.FileInfo) bool {
		if strings.HasSuffix(rel, ".py") {
			pyFiles = append(pyFiles, rel)
		}
		return len(pyFiles) < 200
	})
	if len(pyFiles) > 0 {
		ran = true
		out, err := a.execInProject(ctx, dir, append([]string{"python3", "-m", "py_compile"}, pyFiles...), 60, nil)
		if err != nil {
			sections = append(sections, "py_compile:\n"+tailLines(out, 40))
		} else {
			sections = append(sections, "py_compile: bersih.")
		}
		out, err = a.execInProject(ctx, dir, []string{"ruff", "check", "."}, 60, nil)
		if err == nil {
			sections = append(sections, "ruff: bersih.")
		} else if !strings.Contains(out, "not found") && !strings.Contains(out, "No such file") {
			sections = append(sections, "ruff:\n"+tailLines(out, 40))
		}
	}
	if projectHasFile(dir, "package.json") {
		ran = true
		if projectHasFile(dir, filepath.Join("node_modules", ".bin", "eslint")) {
			out, err := a.execInProject(ctx, dir, []string{"./node_modules/.bin/eslint", "."}, 120, nil)
			if err != nil {
				sections = append(sections, "eslint:\n"+tailLines(out, 40))
			} else {
				sections = append(sections, "eslint: bersih.")
			}
		} else {
			sections = append(sections, "eslint: tidak terpasang di proyek (dilewati, tidak diunduh).")
		}
	}
	if !ran {
		return "Error: tidak ada target lint terdeteksi (go.mod, file .py, atau package.json)."
	}
	return truncateToolResult(fmt.Sprintf("Lint proyek %q:\n%s", project, strings.Join(sections, "\n")))
}

func (a *App) toolFormatCode(ctx context.Context, arg string) string {
	project, _ := parseProjectArg(arg)
	dir, err := a.ensureProject(project)
	if err != nil {
		return "Error: " + err.Error()
	}
	var sections []string
	ran := false
	hasGo := false
	hasPy := false
	walkProject(dir, 500, func(rel, full string, info os.FileInfo) bool {
		if strings.HasSuffix(rel, ".go") {
			hasGo = true
		}
		if strings.HasSuffix(rel, ".py") {
			hasPy = true
		}
		return true
	})
	if hasGo {
		ran = true
		out, err := a.execInProject(ctx, dir, []string{"gofmt", "-w", "."}, 60, nil)
		if err != nil {
			sections = append(sections, "gofmt GAGAL:\n"+tailLines(out, 20))
		} else {
			sections = append(sections, "gofmt: file .go terformat.")
		}
	}
	if hasPy {
		ran = true
		out, err := a.execInProject(ctx, dir, []string{"black", "."}, 60, nil)
		if err != nil && (strings.Contains(out, "not found") || strings.Contains(out, "No such file")) {
			sections = append(sections, "black: tidak tersedia di server.")
		} else if err != nil {
			sections = append(sections, "black GAGAL:\n"+tailLines(out, 20))
		} else {
			sections = append(sections, "black: file .py terformat.")
		}
	}
	if projectHasFile(dir, "package.json") {
		ran = true
		if projectHasFile(dir, filepath.Join("node_modules", ".bin", "prettier")) {
			out, err := a.execInProject(ctx, dir, []string{"./node_modules/.bin/prettier", "--write", "."}, 60, nil)
			if err != nil {
				sections = append(sections, "prettier GAGAL:\n"+tailLines(out, 20))
			} else {
				sections = append(sections, "prettier: terformat.")
			}
		} else {
			sections = append(sections, "prettier: tidak terpasang di proyek.")
		}
	}
	if !ran {
		return "Error: tidak ada file yang bisa diformat (.go/.py/package.json)."
	}
	return truncateToolResult(fmt.Sprintf("Format proyek %q:\n%s", project, strings.Join(sections, "\n")))
}

// --- run_server: proses latar per proyek ---

type ringLog struct {
	mu      sync.Mutex
	lines   []string
	partial string
}

func (r *ringLog) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	text := r.partial + string(p)
	parts := strings.Split(text, "\n")
	r.partial = parts[len(parts)-1]
	r.lines = append(r.lines, parts[:len(parts)-1]...)
	if len(r.lines) > 200 {
		r.lines = r.lines[len(r.lines)-200:]
	}
	return len(p), nil
}

func (r *ringLog) tail(n int) []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	lines := append([]string{}, r.lines...)
	if r.partial != "" {
		lines = append(lines, r.partial)
	}
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return lines
}

type projectServer struct {
	cmd       *exec.Cmd
	port      int
	startedAt time.Time
	log       *ringLog
	done      chan struct{}
}

func (s *projectServer) running() bool {
	select {
	case <-s.done:
		return false
	default:
		return true
	}
}

func (s *projectServer) stop() {
	if s.cmd == nil || s.cmd.Process == nil {
		return
	}
	_ = killProcessGroup(s.cmd.Process.Pid, false)
	select {
	case <-s.done:
	case <-time.After(3 * time.Second):
		_ = killProcessGroup(s.cmd.Process.Pid, true)
		<-s.done
	}
}

func (a *App) stopAllProjectServers() {
	a.serverMu.Lock()
	servers := a.servers
	a.servers = map[string]*projectServer{}
	a.serverMu.Unlock()
	for _, srv := range servers {
		srv.stop()
	}
}

var serverPortPattern = regexp.MustCompile(`(?::|port[= ]|PORT[= ])(\d{2,5})|\b(\d{4,5})\s*$`)

func freeProjectPort() int {
	for port := 4100; port < 4200; port++ {
		ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if err == nil {
			_ = ln.Close()
			return port
		}
	}
	return 0
}

func (a *App) toolRunServer(ctx context.Context, arg string) string {
	project, rest := parseProjectArg(arg)
	command := strings.TrimSpace(rest)
	a.serverMu.Lock()
	if a.servers == nil {
		a.servers = map[string]*projectServer{}
	}
	existing := a.servers[project]
	a.serverMu.Unlock()

	switch command {
	case "stop":
		if existing == nil || !existing.running() {
			return fmt.Sprintf("Tidak ada server berjalan untuk proyek %q.", project)
		}
		existing.stop()
		a.serverMu.Lock()
		delete(a.servers, project)
		a.serverMu.Unlock()
		return fmt.Sprintf("Server proyek %q dihentikan.", project)
	case "status":
		if existing == nil {
			return fmt.Sprintf("Tidak ada server untuk proyek %q.", project)
		}
		if !existing.running() {
			a.serverMu.Lock()
			delete(a.servers, project)
			a.serverMu.Unlock()
			return truncateToolResult(fmt.Sprintf("Server proyek %q sudah berhenti. Log terakhir:\n%s", project, strings.Join(existing.log.tail(20), "\n")))
		}
		uptime := time.Since(existing.startedAt).Round(time.Second)
		return truncateToolResult(fmt.Sprintf("Server proyek %q berjalan di http://127.0.0.1:%d (sejak %s lalu). Log terakhir:\n%s", project, existing.port, uptime, strings.Join(existing.log.tail(20), "\n")))
	case "":
		return "Error: format run_server — argumen perintah server (mis. \"go run .\", \"npm start\", \"python3 -m http.server\"), atau \"stop\"/\"status\"."
	}
	if existing != nil && existing.running() {
		return fmt.Sprintf("Error: server proyek %q sudah berjalan di port %d. Hentikan dulu dengan run_server \"stop\".", project, existing.port)
	}
	dir, err := a.ensureProject(project)
	if err != nil {
		return "Error: " + err.Error()
	}
	port := 0
	if m := serverPortPattern.FindStringSubmatch(command); m != nil {
		for _, g := range m[1:] {
			if g != "" {
				port, _ = strconv.Atoi(g)
				break
			}
		}
	}
	if port == 0 {
		port = freeProjectPort()
		if port == 0 {
			return "Error: tidak ada port bebas di rentang 4100-4199."
		}
	}
	// Server berjalan di sandbox berkas yang sama tetapi tanpa
	// --unshare-net agar bisa dijangkau di 127.0.0.1; HOST dipaksa
	// loopback lewat env.
	bwrapArgs := projectSandboxArgs(dir, []string{"sh", "-c", command}, []string{fmt.Sprintf("PORT=%d", port), "HOST=127.0.0.1"})
	// Buang --unshare-net khusus untuk server.
	filtered := bwrapArgs[:0]
	for _, a := range bwrapArgs {
		if a != "--unshare-net" {
			filtered = append(filtered, a)
		}
	}
	log := &ringLog{}
	cmd := exec.Command("bwrap", filtered...)
	cmd.Stdout = log
	cmd.Stderr = log
	setProcessGroup(cmd)
	if err := cmd.Start(); err != nil {
		return "Error: gagal menjalankan server: " + err.Error()
	}
	srv := &projectServer{cmd: cmd, port: port, startedAt: time.Now(), log: log, done: make(chan struct{})}
	go func() {
		_ = cmd.Wait()
		close(srv.done)
	}()
	a.serverMu.Lock()
	a.servers[project] = srv
	a.serverMu.Unlock()
	// Beri waktu singkat agar kegagalan awal terlihat.
	select {
	case <-srv.done:
		return truncateToolResult(fmt.Sprintf("Server proyek %q langsung berhenti. Log:\n%s", project, strings.Join(log.tail(20), "\n")))
	case <-time.After(800 * time.Millisecond):
	}
	return fmt.Sprintf("Server proyek %q berjalan di http://127.0.0.1:%d (perintah: %s). Cek dengan run_server \"status\", hentikan dengan \"stop\".", project, port, command)
}

// --- allowlist_shell ---

var shellMetaPattern = regexp.MustCompile("[;|&><`$\\n\\r]")

var allowlistCommands = map[string]bool{
	"ls": true, "cat": true, "head": true, "tail": true, "wc": true,
	"pwd": true, "echo": true, "grep": true, "du": true, "df": true, "find": true,
}

func (a *App) toolAllowlistShell(ctx context.Context, arg string) string {
	project, rest := parseProjectArg(arg)
	command := strings.TrimSpace(rest)
	if command == "" {
		return "Error: perintah kosong."
	}
	if shellMetaPattern.MatchString(command) || strings.Contains(command, "$(") {
		return "Error: metakarakter shell tidak diizinkan di allowlist_shell. Gunakan run_shell (dengan persetujuan) untuk perintah kompleks."
	}
	argv := strings.Fields(command)
	if len(argv) == 0 {
		return "Error: perintah kosong."
	}
	base := filepath.Base(argv[0])
	allowed := false
	switch {
	case allowlistCommands[base]:
		allowed = true
		if base == "find" {
			for _, a := range argv[1:] {
				if a == "-exec" || a == "-execdir" || a == "-delete" || a == "-ok" || a == "-fprint" || a == "-fprintf" {
					return "Error: opsi find tersebut tidak diizinkan."
				}
			}
		}
	case base == "go" && len(argv) == 2 && argv[1] == "version":
		allowed = true
	case base == "node" && len(argv) == 2 && (argv[1] == "--version" || argv[1] == "-v"):
		allowed = true
	case base == "python3" && len(argv) == 2 && (argv[1] == "--version" || argv[1] == "-V"):
		allowed = true
	case base == "git" && len(argv) >= 2 && (argv[1] == "status" || argv[1] == "log" || argv[1] == "diff"):
		allowed = true
		argv = append([]string{"git", "-c", "safe.directory=/work"}, argv[1:]...)
	}
	if !allowed {
		return fmt.Sprintf("Error: perintah %q tidak ada di allowlist. Gunakan run_shell (memerlukan persetujuan pemilik) untuk perintah lain.", base)
	}
	dir, err := a.ensureProject(project)
	if err != nil {
		return "Error: " + err.Error()
	}
	out, err := a.execInProject(ctx, dir, argv, 30, nil)
	text := strings.TrimSpace(out)
	if err != nil {
		if text == "" {
			text = err.Error()
		}
		return truncateToolResult("Perintah selesai dengan error:\n" + text)
	}
	if text == "" {
		text = "(tidak ada output)"
	}
	return truncateToolResult(text)
}
