package musehood

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// --- Tool debug (fitur omp 4): client DAP minimal untuk delve ---
//
// Satu sesi debug per ruang: debug_launch menjalankan
// `dlv dap --listen=127.0.0.1:<port>` lalu client DAP berbicara
// Content-Length JSON-RPC di atas TCP: initialize → launch →
// (event initialized) setBreakpoints → configurationDone →
// continue/next/stepIn/stepOut → stackTrace/scopes/variables →
// disconnect. Tanpa delve terpasang, tool mengembalikan pesan jelas.
// Target debug adalah proyek Go (mode "debug" delve).

const (
	debugTimeout     = 30 * time.Second
	debugLaunchWait  = 120 * time.Second // build pertama delve bisa lambat
	debugMaxSessions = 4
)

const debugMissingError = "Error: delve (dlv) tidak ditemukan di server. Pasang dengan: go install github.com/go-delve/delve/cmd/dlv@latest (binary ke ~/.local/bin), lalu coba lagi."

type debugBreakpoint struct {
	Path string
	Line int
}

type debugSession struct {
	key         string
	project     string
	projectDir  string
	program     string
	cmd         *exec.Cmd
	conn        net.Conn
	reader      *bufio.Reader
	seq         int
	mu          sync.Mutex
	breakpoints []debugBreakpoint
	stopped     bool
	threadID    int
	lastEvent   string
}

var (
	debugMu       sync.Mutex
	debugSessions = map[string]*debugSession{}
)

func findDelve() string {
	if path := strings.TrimSpace(os.Getenv("MUSEHOOD_DLV")); path != "" {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	return resolveLocalBinary("dlv")
}

// --- Protokol DAP ---

func (s *debugSession) send(command string, arguments any) int {
	s.seq++
	payload := map[string]any{"seq": s.seq, "type": "request", "command": command}
	if arguments != nil {
		payload["arguments"] = arguments
	}
	raw, _ := json.Marshal(payload)
	_, _ = fmt.Fprintf(s.conn, "Content-Length: %d\r\n\r\n%s", len(raw), raw)
	return s.seq
}

func (s *debugSession) readMessage() (map[string]any, error) {
	length := 0
	for {
		line, err := s.reader.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		if name, value, ok := strings.Cut(line, ":"); ok && strings.EqualFold(strings.TrimSpace(name), "Content-Length") {
			length, _ = strconv.Atoi(strings.TrimSpace(value))
		}
	}
	if length <= 0 || length > 32*1024*1024 {
		return nil, fmt.Errorf("Content-Length DAP tidak valid: %d", length)
	}
	buf := make([]byte, length)
	if _, err := io.ReadFull(s.reader, buf); err != nil {
		return nil, err
	}
	var msg map[string]any
	if err := json.Unmarshal(buf, &msg); err != nil {
		return nil, err
	}
	return msg, nil
}

// waitResponse menunggu respons untuk request_seq tertentu, sambil
// mencatat event yang lewat. Mengembalikan body respons.
func (s *debugSession) waitResponse(requestSeq int, timeout time.Duration) (map[string]any, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		_ = s.conn.SetReadDeadline(deadline)
		msg, err := s.readMessage()
		if err != nil {
			return nil, err
		}
		s.noteMessage(msg)
		if msg["type"] == "response" {
			if rs, ok := msg["request_seq"].(float64); ok && int(rs) == requestSeq {
				if success, _ := msg["success"].(bool); !success {
					return nil, fmt.Errorf("DAP: %v", msg["message"])
				}
				body, _ := msg["body"].(map[string]any)
				return body, nil
			}
		}
	}
	return nil, fmt.Errorf("batas waktu menunggu respons DAP")
}

// waitStopped menunggu event stopped/terminated/exited.
func (s *debugSession) waitStopped(timeout time.Duration) string {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		_ = s.conn.SetReadDeadline(deadline)
		msg, err := s.readMessage()
		if err != nil {
			return "koneksi DAP berakhir: " + err.Error()
		}
		s.noteMessage(msg)
		if msg["type"] == "event" {
			switch msg["event"] {
			case "stopped":
				return s.describeStop()
			case "terminated", "exited":
				s.stopped = false
				return "Program selesai (event " + fmt.Sprint(msg["event"]) + ")."
			}
		}
	}
	return "Masih berjalan (belum berhenti dalam batas waktu)."
}

func (s *debugSession) noteMessage(msg map[string]any) {
	if msg["type"] != "event" {
		return
	}
	event, _ := msg["event"].(string)
	s.lastEvent = event
	if event == "stopped" {
		s.stopped = true
		if body, ok := msg["body"].(map[string]any); ok {
			if tid, ok := body["threadId"].(float64); ok {
				s.threadID = int(tid)
			}
		}
	}
	if event == "continued" {
		s.stopped = false
	}
}

func (s *debugSession) describeStop() string {
	if s.threadID == 0 {
		s.threadID = 1
	}
	seq := s.send("stackTrace", map[string]any{"threadId": s.threadID, "startFrame": 0, "levels": 5})
	body, err := s.waitResponse(seq, 10*time.Second)
	if err != nil || body == nil {
		return "Program berhenti."
	}
	frames, _ := body["stackFrames"].([]any)
	if len(frames) == 0 {
		return "Program berhenti."
	}
	top, _ := frames[0].(map[string]any)
	source, _ := top["source"].(map[string]any)
	path, _ := source["path"].(string)
	line, _ := top["line"].(float64)
	name, _ := top["name"].(string)
	return fmt.Sprintf("Program berhenti di %s:%d (%s). Gunakan debug_inspect untuk variabel, debug_step untuk melanjutkan.", projectRel(s.projectDir, path), int(line), name)
}

// queueBreakpointRequests mengirim setBreakpoints per berkas tanpa
// menunggu respons (dipakai handshake launch); mengembalikan seq-nya.
func (s *debugSession) queueBreakpointRequests() []int {
	byFile := map[string][]debugBreakpoint{}
	for _, bp := range s.breakpoints {
		byFile[bp.Path] = append(byFile[bp.Path], bp)
	}
	var seqs []int
	for path, bps := range byFile {
		points := make([]map[string]any, 0, len(bps))
		for _, bp := range bps {
			points = append(points, map[string]any{"line": bp.Line})
		}
		seqs = append(seqs, s.send("setBreakpoints", map[string]any{
			"source":      map[string]any{"path": path},
			"breakpoints": points,
		}))
	}
	return seqs
}

func (s *debugSession) sendBreakpoints() string {
	if len(s.breakpoints) == 0 {
		return ""
	}
	byFile := map[string][]debugBreakpoint{}
	for _, bp := range s.breakpoints {
		byFile[bp.Path] = append(byFile[bp.Path], bp)
	}
	var notes []string
	for path, bps := range byFile {
		points := make([]map[string]any, 0, len(bps))
		for _, bp := range bps {
			points = append(points, map[string]any{"line": bp.Line})
		}
		seq := s.send("setBreakpoints", map[string]any{
			"source":      map[string]any{"path": path},
			"breakpoints": points,
		})
		body, err := s.waitResponse(seq, 10*time.Second)
		if err != nil {
			notes = append(notes, "setBreakpoints gagal: "+err.Error())
			continue
		}
		verified := 0
		if body != nil {
			if list, ok := body["breakpoints"].([]any); ok {
				for _, item := range list {
					if bp, ok := item.(map[string]any); ok {
						if v, _ := bp["verified"].(bool); v {
							verified++
						}
					}
				}
			}
		}
		notes = append(notes, fmt.Sprintf("%s: %d/%d breakpoint terverifikasi", projectRel(s.projectDir, path), verified, len(bps)))
	}
	return strings.Join(notes, "; ")
}

func (s *debugSession) stop() {
	// Tutup koneksi dulu agar operasi yang sedang berjalan (memegang
	// s.mu dan menunggu respons) segera error dan melepas kunci;
	// sebelumnya stop() menulis/membaca koneksi + seq tanpa s.mu
	// sehingga berlomba dengan operasi lain (bug ronde 2). Proses
	// delve dibunuh langsung; jabat tangan disconnect tidak lagi
	// dikirim karena koneksi sudah tertutup.
	if s.conn != nil {
		_ = s.conn.Close()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cmd != nil && s.cmd.Process != nil {
		_ = s.cmd.Process.Kill()
		go func() { _ = s.cmd.Wait() }()
	}
}

func debugSessionFor(ctx context.Context) *debugSession {
	info := toolRunInfoFrom(ctx)
	key := info.RoomID
	debugMu.Lock()
	defer debugMu.Unlock()
	return debugSessions[key]
}

// --- Tool ---

func (a *App) toolDebugLaunch(ctx context.Context, arg string) string {
	dlv := findDelve()
	if dlv == "" {
		return debugMissingError
	}
	project, rest := parseProjectArg(arg)
	lines := strings.Split(rest, "\n")
	program := strings.TrimSpace(lines[0])
	if program == "" {
		program = "."
	}
	var programArgs []string
	if len(lines) > 1 && strings.TrimSpace(lines[1]) != "" {
		programArgs = strings.Fields(lines[1])
	}
	dir, err := a.ensureProject(project)
	if err != nil {
		return "Error: " + err.Error()
	}
	programPath := program
	if !filepath.IsAbs(programPath) {
		programPath = filepath.Join(dir, program)
	}
	if !strings.HasPrefix(programPath, dir) {
		return "Error: program harus di dalam folder proyek."
	}
	info := toolRunInfoFrom(ctx)
	key := info.RoomID

	debugMu.Lock()
	if old, ok := debugSessions[key]; ok {
		old.stop()
		delete(debugSessions, key)
	}
	if len(debugSessions) >= debugMaxSessions {
		debugMu.Unlock()
		return fmt.Sprintf("Error: batas %d sesi debug tercapai; hentikan sesi lain dengan debug_stop/hub_stop.", debugMaxSessions)
	}
	debugMu.Unlock()

	// Port bebas untuk server DAP delve.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "Error: tidak bisa membuka port DAP: " + err.Error()
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()

	cmd := exec.Command(dlv, "dap", "--listen=127.0.0.1:"+strconv.Itoa(port))
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "HOME=/tmp", "GOCACHE="+filepath.Join(a.dir, "gocache-debug"))
	if err := cmd.Start(); err != nil {
		return "Error: gagal menjalankan delve: " + err.Error()
	}
	var conn net.Conn
	for i := 0; i < 50; i++ {
		conn, err = net.DialTimeout("tcp", "127.0.0.1:"+strconv.Itoa(port), 200*time.Millisecond)
		if err == nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if conn == nil {
		_ = cmd.Process.Kill()
		return "Error: server DAP delve tidak merespons."
	}
	session := &debugSession{
		key: key, project: project, projectDir: dir, program: program,
		cmd: cmd, conn: conn, reader: bufio.NewReader(conn),
	}

	debugMu.Lock()
	debugSessions[key] = session
	debugMu.Unlock()

	session.mu.Lock()
	defer session.mu.Unlock()
	seq := session.send("initialize", map[string]any{
		"adapterID": "go", "pathFormat": "path",
		"linesStartAt1": true, "columnsStartAt1": true,
	})
	if _, err := session.waitResponse(seq, 10*time.Second); err != nil {
		return "Error: inisialisasi DAP gagal: " + err.Error()
	}
	launchArgs := map[string]any{
		"mode": "debug", "program": programPath, "cwd": dir,
		"stopOnEntry": true,
	}
	if len(programArgs) > 0 {
		launchArgs["args"] = programArgs
	}
	launchSeq := session.send("launch", launchArgs)
	// Satu loop untuk seluruh handshake: event initialized ->
	// setBreakpoints -> configurationDone -> respons launch. Semua
	// respons dicocokkan berdasarkan request_seq agar tidak ada yang
	// tertelan menunggu respons lain.
	deadline := time.Now().Add(debugLaunchWait)
	configured := false
	launchDone := false
	var launchOutput strings.Builder
	pendingBreakpoints := map[int]bool{}
	for time.Now().Before(deadline) && !launchDone {
		_ = conn.SetReadDeadline(deadline)
		msg, err := session.readMessage()
		if err != nil {
			return "Error: koneksi DAP berakhir saat launch: " + err.Error()
		}
		session.noteMessage(msg)
		switch {
		case msg["type"] == "event" && msg["event"] == "output":
			if body, ok := msg["body"].(map[string]any); ok {
				if text, ok := body["output"].(string); ok {
					launchOutput.WriteString(text)
				}
			}
		case msg["type"] == "event" && msg["event"] == "initialized" && !configured:
			configured = true
			for _, seq := range session.queueBreakpointRequests() {
				pendingBreakpoints[seq] = true
			}
			session.send("configurationDone", nil)
		case msg["type"] == "response":
			rs, _ := msg["request_seq"].(float64)
			switch {
			case int(rs) == launchSeq:
				if success, _ := msg["success"].(bool); !success {
					detail := strings.TrimSpace(launchOutput.String())
					if detail != "" {
						return truncateToolResult(fmt.Sprintf("Error: launch gagal: %v\n%s", msg["message"], detail))
					}
					return fmt.Sprintf("Error: launch gagal: %v", msg["message"])
				}
				launchDone = true
			case pendingBreakpoints[int(rs)]:
				delete(pendingBreakpoints, int(rs))
			}
		}
	}
	if !launchDone {
		return "Error: batas waktu menunggu launch DAP."
	}
	// stopOnEntry: tunggu berhenti di awal.
	status := session.waitStopped(debugTimeout)
	return "Sesi debug dimulai untuk " + program + ". " + status
}

func (a *App) toolDebugBreakpoint(ctx context.Context, arg string) string {
	project, rest := parseProjectArg(arg)
	lines := strings.Split(rest, "\n")
	if len(lines) < 2 {
		return "Error: format debug_breakpoint — baris pertama path file, baris kedua nomor baris."
	}
	rel := strings.TrimSpace(lines[0])
	line, err := strconv.Atoi(strings.TrimSpace(lines[1]))
	if err != nil || line < 1 {
		return "Error: nomor baris harus angka >= 1."
	}
	session := debugSessionFor(ctx)
	if session == nil {
		return "Error: belum ada sesi debug; jalankan debug_launch dulu."
	}
	full, err := a.projectPath(project, rel)
	if err != nil {
		return "Error: " + err.Error()
	}
	session.mu.Lock()
	defer session.mu.Unlock()
	session.breakpoints = append(session.breakpoints, debugBreakpoint{Path: full, Line: line})
	note := session.sendBreakpoints()
	if note == "" {
		note = "breakpoint tercatat"
	}
	return fmt.Sprintf("Breakpoint %s:%d dipasang (%s).", rel, line, note)
}

func (a *App) toolDebugStep(ctx context.Context, arg string) string {
	session := debugSessionFor(ctx)
	if session == nil {
		return "Error: belum ada sesi debug; jalankan debug_launch dulu."
	}
	action := strings.TrimSpace(arg)
	command := map[string]string{"continue": "continue", "next": "next", "stepIn": "stepIn", "stepOut": "stepOut"}[action]
	if command == "" {
		return "Error: format debug_step — continue, next, stepIn, atau stepOut."
	}
	session.mu.Lock()
	defer session.mu.Unlock()
	if session.threadID == 0 {
		session.threadID = 1
	}
	seq := session.send(command, map[string]any{"threadId": session.threadID})
	if _, err := session.waitResponse(seq, 10*time.Second); err != nil {
		return "Error: " + err.Error()
	}
	return session.waitStopped(debugTimeout)
}

func (a *App) toolDebugInspect(ctx context.Context, arg string) string {
	session := debugSessionFor(ctx)
	if session == nil {
		return "Error: belum ada sesi debug; jalankan debug_launch dulu."
	}
	session.mu.Lock()
	defer session.mu.Unlock()
	if !session.stopped {
		return "Program sedang berjalan (tidak berhenti); gunakan debug_step continue sampai breakpoint, lalu debug_inspect."
	}
	if session.threadID == 0 {
		session.threadID = 1
	}
	seq := session.send("stackTrace", map[string]any{"threadId": session.threadID, "startFrame": 0, "levels": 10})
	body, err := session.waitResponse(seq, 10*time.Second)
	if err != nil {
		return "Error: " + err.Error()
	}
	var out strings.Builder
	frames, _ := body["stackFrames"].([]any)
	out.WriteString("Stack trace:\n")
	var topFrameID float64
	for i, item := range frames {
		frame, _ := item.(map[string]any)
		source, _ := frame["source"].(map[string]any)
		path, _ := source["path"].(string)
		line, _ := frame["line"].(float64)
		name, _ := frame["name"].(string)
		out.WriteString(fmt.Sprintf("%d. %s (%s:%d)\n", i, name, projectRel(session.projectDir, path), int(line)))
		if i == 0 {
			topFrameID, _ = frame["id"].(float64)
		}
	}
	if topFrameID != 0 {
		scopeSeq := session.send("scopes", map[string]any{"frameId": int(topFrameID)})
		scopeBody, err := session.waitResponse(scopeSeq, 10*time.Second)
		if err == nil && scopeBody != nil {
			scopes, _ := scopeBody["scopes"].([]any)
			for _, item := range scopes {
				scope, _ := item.(map[string]any)
				ref, _ := scope["variablesReference"].(float64)
				scopeName, _ := scope["name"].(string)
				if ref == 0 {
					continue
				}
				varSeq := session.send("variables", map[string]any{"variablesReference": int(ref)})
				varBody, err := session.waitResponse(varSeq, 10*time.Second)
				if err != nil || varBody == nil {
					continue
				}
				out.WriteString("Variabel (" + scopeName + "):\n")
				vars, _ := varBody["variables"].([]any)
				for _, vItem := range vars {
					v, _ := vItem.(map[string]any)
					out.WriteString(fmt.Sprintf("- %v = %v\n", v["name"], v["value"]))
				}
			}
		}
	}
	return truncateToolResult(strings.TrimSpace(out.String()))
}

func (a *App) toolDebugStop(ctx context.Context, arg string) string {
	info := toolRunInfoFrom(ctx)
	key := info.RoomID
	debugMu.Lock()
	session, ok := debugSessions[key]
	if ok {
		delete(debugSessions, key)
	}
	debugMu.Unlock()
	if !ok {
		return "Tidak ada sesi debug aktif."
	}
	session.stop()
	return "Sesi debug dihentikan."
}

// debugSessionList mengembalikan daftar sesi debug hidup (untuk hub).
func debugSessionList() []string {
	debugMu.Lock()
	defer debugMu.Unlock()
	out := make([]string, 0, len(debugSessions))
	for key := range debugSessions {
		out = append(out, "debug:"+key)
	}
	return out
}

// stopDebugSession menghentikan sesi debug berdasarkan kunci hub.
func stopDebugSession(key string) bool {
	debugMu.Lock()
	session, ok := debugSessions[key]
	if ok {
		delete(debugSessions, key)
	}
	debugMu.Unlock()
	if !ok {
		return false
	}
	session.stop()
	return true
}
