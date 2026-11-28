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
	"sort"
	"strconv"
	"strings"
	"time"
)

// --- Tool LSP (fitur omp 2): intelijen bahasa via language server ---
//
// Client LSP minimal di atas stdio JSON-RPC (framing Content-Length):
// satu proses server per panggilan (initialize → didOpen → request →
// shutdown/exit), pola yang sama dengan client MCP. Server per bahasa
// dari settings.LSPServers; bawaan: go→gopls, python→pylsp,
// javascript/typescript→typescript-language-server, rust→rust-analyzer.
// Tanpa server terpasang, tool mengembalikan pesan jelas (degradasi
// anggun). lsp_rename menerapkan WorkspaceEdit hanya setelah
// persetujuan pemilik (approval kind "edit").

const (
	lspTimeout         = 30 * time.Second
	lspDiagnosticsWait = 1500 * time.Millisecond
)

var lspDefaultServers = map[string][]string{
	"go":         {"gopls"},
	"python":     {"pylsp", "pyright-langserver"},
	"javascript": {"typescript-language-server"},
	"typescript": {"typescript-language-server"},
	"rust":       {"rust-analyzer"},
	"java":       {"jdtls"},
	"c":          {"clangd"},
	"cpp":        {"clangd"},
}

var lspLangByExt = map[string]string{
	".go": "go", ".py": "python", ".js": "javascript", ".mjs": "javascript",
	".ts": "typescript", ".tsx": "typescript", ".rs": "rust", ".java": "java",
	".c": "c", ".h": "c", ".cpp": "cpp", ".cc": "cpp", ".hpp": "cpp",
}

// lspServerCommand mencari perintah language server untuk bahasa:
// settings.LSPServers lebih dulu, lalu kandidat bawaan di PATH/~/.local/bin.
func (a *App) lspServerCommand(cfg settings, lang string) (string, []string) {
	if cmdLine := strings.TrimSpace(cfg.LSPServers[lang]); cmdLine != "" {
		parts := strings.Fields(cmdLine)
		if len(parts) > 0 {
			if resolved := resolveLocalBinary(parts[0]); resolved != "" {
				return resolved, parts[1:]
			}
			return parts[0], parts[1:]
		}
	}
	for _, candidate := range lspDefaultServers[lang] {
		if resolved := resolveLocalBinary(candidate); resolved != "" {
			return resolved, nil
		}
	}
	return "", nil
}

// resolveLocalBinary mencari binary di PATH lalu ~/.local/bin dan
// ~/.local/node_modules/.bin.
func resolveLocalBinary(name string) string {
	if strings.Contains(name, "/") {
		if _, err := os.Stat(name); err == nil {
			return name
		}
		return ""
	}
	if path, err := exec.LookPath(name); err == nil {
		return path
	}
	home, _ := os.UserHomeDir()
	for _, candidate := range []string{
		filepath.Join(home, ".local", "bin", name),
		filepath.Join(home, ".local", "node_modules", ".bin", name),
	} {
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return ""
}

func lspMissingError(lang string) string {
	return fmt.Sprintf("Error: language server untuk bahasa %q tidak ditemukan. Pasang servernya (mis. gopls untuk Go: go install golang.org/x/tools/gopls@latest) atau atur perintahnya di Pengaturan → Server LSP.", lang)
}

// --- Client LSP ---

type lspClient struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	reader *bufio.Reader
	nextID int
	cancel context.CancelFunc
	// diagnostics menampung publishDiagnostics per URI.
	diagnostics map[string][]lspDiagnostic
}

type lspDiagnostic struct {
	Range struct {
		Start struct {
			Line      int `json:"line"`
			Character int `json:"character"`
		} `json:"start"`
	} `json:"range"`
	Severity int    `json:"severity"`
	Message  string `json:"message"`
	Source   string `json:"source,omitempty"`
}

type lspPosition struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

type lspRange struct {
	Start lspPosition `json:"start"`
	End   lspPosition `json:"end"`
}

type lspLocation struct {
	URI   string   `json:"uri"`
	Range lspRange `json:"range"`
}

type lspTextEdit struct {
	Range   lspRange `json:"range"`
	NewText string   `json:"newText"`
}

func (c *lspClient) writeMessage(payload any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(c.stdin, "Content-Length: %d\r\n\r\n%s", len(raw), raw)
	return err
}

func (c *lspClient) readMessage() (map[string]any, error) {
	length := 0
	for {
		line, err := c.reader.ReadString('\n')
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
		return nil, fmt.Errorf("Content-Length LSP tidak valid: %d", length)
	}
	buf := make([]byte, length)
	if _, err := io.ReadFull(c.reader, buf); err != nil {
		return nil, err
	}
	var msg map[string]any
	if err := json.Unmarshal(buf, &msg); err != nil {
		return nil, err
	}
	return msg, nil
}

// request mengirim request JSON-RPC dan menunggu respons ber-id sama,
// sambil menampung notifikasi publishDiagnostics yang lewat.
func (c *lspClient) request(method string, params any) (json.RawMessage, error) {
	c.nextID++
	id := c.nextID
	if err := c.writeMessage(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params}); err != nil {
		return nil, err
	}
	for {
		msg, err := c.readMessage()
		if err != nil {
			return nil, err
		}
		if msgID, ok := msg["id"].(float64); ok && int(msgID) == id {
			if rpcErr, ok := msg["error"].(map[string]any); ok {
				return nil, fmt.Errorf("LSP %s: %v", method, rpcErr["message"])
			}
			raw, _ := json.Marshal(msg["result"])
			return raw, nil
		}
		c.captureNotification(msg)
	}
}

func (c *lspClient) notify(method string, params any) error {
	return c.writeMessage(map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
}

func (c *lspClient) captureNotification(msg map[string]any) {
	if msg["method"] != "textDocument/publishDiagnostics" {
		return
	}
	params, _ := msg["params"].(map[string]any)
	uri, _ := params["uri"].(string)
	rawDiags, _ := json.Marshal(params["diagnostics"])
	var diags []lspDiagnostic
	_ = json.Unmarshal(rawDiags, &diags)
	if c.diagnostics == nil {
		c.diagnostics = map[string][]lspDiagnostic{}
	}
	c.diagnostics[uri] = diags
}

// drainDiagnostics membaca pesan masuk selama jendela singkat untuk
// menampung publishDiagnostics sesudah didOpen.
func (c *lspClient) drainDiagnostics(wait time.Duration) {
	deadline := time.Now().Add(wait)
	for time.Now().Before(deadline) {
		// Baca dengan batas waktu lewat goroutine + kanal.
		done := make(chan map[string]any, 1)
		go func() {
			msg, err := c.readMessage()
			if err != nil {
				done <- nil
				return
			}
			done <- msg
		}()
		select {
		case msg := <-done:
			if msg == nil {
				return
			}
			c.captureNotification(msg)
		case <-time.After(time.Until(deadline)):
			return
		}
	}
}

// startLSP menjalankan server LSP untuk proyek dan melakukan handshake.
func (a *App) startLSP(ctx context.Context, cfg settings, projectDir, lang string) (*lspClient, error) {
	command, args := a.lspServerCommand(cfg, lang)
	if command == "" {
		return nil, fmt.Errorf("server LSP %q tidak ditemukan", lang)
	}
	runCtx, cancel := context.WithTimeout(ctx, lspTimeout)
	cmd := exec.CommandContext(runCtx, command, args...)
	cmd.Dir = projectDir
	cmd.Env = append(os.Environ(), "HOME=/tmp")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	cmd.Stderr = nil
	if err := cmd.Start(); err != nil {
		cancel()
		return nil, err
	}
	client := &lspClient{cmd: cmd, stdin: stdin, reader: bufio.NewReader(stdout), diagnostics: map[string][]lspDiagnostic{}, cancel: cancel}
	rootURI := "file://" + projectDir
	if _, err := client.request("initialize", map[string]any{
		"processId": nil,
		"rootUri":   rootURI,
		"workspaceFolders": []map[string]any{
			{"uri": rootURI, "name": "project"},
		},
		"capabilities": map[string]any{
			"workspace": map[string]any{
				"workspaceFolders": true,
				"workspaceEdit":    map[string]any{"documentChanges": false},
			},
			"textDocument": map[string]any{
				"publishDiagnostics": map[string]any{},
				"definition":         map[string]any{},
				"references":         map[string]any{},
				"rename":             map[string]any{},
			},
		},
	}); err != nil {
		_ = cmd.Process.Kill()
		cancel()
		return nil, err
	}
	_ = client.notify("initialized", map[string]any{})
	// cancel dipanggil saat client ditutup; simpan lewat penutup proses.
	go func() {
		<-runCtx.Done()
		_ = cmd.Process.Kill()
	}()
	return client, nil
}

func (c *lspClient) close() {
	if c.cancel != nil {
		defer c.cancel()
	}
	_, _ = c.request("shutdown", nil)
	_ = c.notify("exit", nil)
	_ = c.stdin.Close()
	done := make(chan struct{})
	go func() {
		_ = c.cmd.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		_ = c.cmd.Process.Kill()
	}
}

func (c *lspClient) didOpen(uri, lang, text string) {
	_ = c.notify("textDocument/didOpen", map[string]any{
		"textDocument": map[string]any{"uri": uri, "languageId": lang, "version": 1, "text": text},
	})
}

// lspOpenProjectFile memvalidasi argumen posisi LSP dan membuka sesi.
func (a *App) lspOpenProjectFile(ctx context.Context, cfg settings, arg string, needPosition bool) (*lspClient, string, string, string, lspPosition, error) {
	project, rest := parseProjectArg(arg)
	lines := strings.Split(rest, "\n")
	if len(lines) < 1 || strings.TrimSpace(lines[0]) == "" {
		return nil, "", "", "", lspPosition{}, fmt.Errorf("format: baris pertama path file")
	}
	rel := strings.TrimSpace(lines[0])
	dir, err := a.ensureProject(project)
	if err != nil {
		return nil, "", "", "", lspPosition{}, err
	}
	full, err := a.projectPath(project, rel)
	if err != nil {
		return nil, "", "", "", lspPosition{}, err
	}
	raw, err := os.ReadFile(full)
	if err != nil {
		return nil, "", "", "", lspPosition{}, fmt.Errorf("file tidak ditemukan: %s", rel)
	}
	lang := lspLangByExt[strings.ToLower(filepath.Ext(rel))]
	if lang == "" {
		return nil, "", "", "", lspPosition{}, fmt.Errorf("ekstensi file tidak didukung LSP: %s", rel)
	}
	pos := lspPosition{}
	if needPosition {
		if len(lines) < 3 {
			return nil, "", "", "", lspPosition{}, fmt.Errorf("format: baris kedua nomor baris (1-based), baris ketiga kolom (1-based)")
		}
		line, err1 := strconv.Atoi(strings.TrimSpace(lines[1]))
		col, err2 := strconv.Atoi(strings.TrimSpace(lines[2]))
		if err1 != nil || err2 != nil || line < 1 || col < 1 {
			return nil, "", "", "", lspPosition{}, fmt.Errorf("baris/kolom harus angka >= 1")
		}
		pos = lspPosition{Line: line - 1, Character: col - 1}
	}
	client, err := a.startLSP(ctx, cfg, dir, lang)
	if err != nil {
		return nil, "", "", "", lspPosition{}, err
	}
	uri := "file://" + full
	client.didOpen(uri, lang, string(raw))
	return client, dir, rel, uri, pos, nil
}

func formatLSPLocations(dir string, raw json.RawMessage) string {
	var single lspLocation
	var many []lspLocation
	var link []struct {
		TargetURI   string   `json:"targetUri"`
		TargetRange lspRange `json:"targetRange"`
	}
	if json.Unmarshal(raw, &many) == nil && len(many) > 0 {
		lines := make([]string, 0, len(many))
		for _, loc := range many {
			lines = append(lines, formatLSPLocation(dir, loc))
		}
		return strings.Join(lines, "\n")
	}
	if json.Unmarshal(raw, &link) == nil && len(link) > 0 {
		lines := make([]string, 0, len(link))
		for _, l := range link {
			lines = append(lines, formatLSPLocation(dir, lspLocation{URI: l.TargetURI, Range: l.TargetRange}))
		}
		return strings.Join(lines, "\n")
	}
	if json.Unmarshal(raw, &single) == nil && single.URI != "" {
		return formatLSPLocation(dir, single)
	}
	return ""
}

func formatLSPLocation(dir string, loc lspLocation) string {
	path := strings.TrimPrefix(loc.URI, "file://")
	if rel, err := filepath.Rel(dir, path); err == nil && !strings.HasPrefix(rel, "..") {
		path = filepath.ToSlash(rel)
	}
	return fmt.Sprintf("%s:%d:%d", path, loc.Range.Start.Line+1, loc.Range.Start.Character+1)
}

func (a *App) toolLSPDefinition(ctx context.Context, arg string) string {
	cfg := a.currentSettings()
	client, dir, _, uri, pos, err := a.lspOpenProjectFile(ctx, cfg, arg, true)
	if err != nil {
		if strings.Contains(err.Error(), "tidak ditemukan") && strings.Contains(err.Error(), "server LSP") {
			return lspMissingError(lspLangOf(arg))
		}
		return "Error: " + err.Error()
	}
	defer client.close()
	raw, err := client.request("textDocument/definition", map[string]any{
		"textDocument": map[string]any{"uri": uri}, "position": pos,
	})
	if err != nil {
		return "Error: " + err.Error()
	}
	out := formatLSPLocations(dir, raw)
	if out == "" {
		return "Definisi tidak ditemukan oleh language server."
	}
	return "Definisi:\n" + truncateToolResult(out)
}

func (a *App) toolLSPReferences(ctx context.Context, arg string) string {
	cfg := a.currentSettings()
	client, dir, _, uri, pos, err := a.lspOpenProjectFile(ctx, cfg, arg, true)
	if err != nil {
		if strings.Contains(err.Error(), "server LSP") {
			return lspMissingError(lspLangOf(arg))
		}
		return "Error: " + err.Error()
	}
	defer client.close()
	raw, err := client.request("textDocument/references", map[string]any{
		"textDocument": map[string]any{"uri": uri}, "position": pos,
		"context": map[string]any{"includeDeclaration": true},
	})
	if err != nil {
		return "Error: " + err.Error()
	}
	out := formatLSPLocations(dir, raw)
	if out == "" {
		return "Referensi tidak ditemukan oleh language server."
	}
	return truncateToolResult(out)
}

func (a *App) toolLSPDiagnostics(ctx context.Context, arg string) string {
	cfg := a.currentSettings()
	client, _, rel, uri, _, err := a.lspOpenProjectFile(ctx, cfg, arg, false)
	if err != nil {
		if strings.Contains(err.Error(), "server LSP") {
			return lspMissingError(lspLangOf(arg))
		}
		return "Error: " + err.Error()
	}
	defer client.close()
	client.drainDiagnostics(lspDiagnosticsWait)
	diags := client.diagnostics[uri]
	if len(diags) == 0 {
		return fmt.Sprintf("Tidak ada diagnostik untuk %s.", rel)
	}
	severity := map[int]string{1: "error", 2: "peringatan", 3: "info", 4: "petunjuk"}
	lines := make([]string, 0, len(diags))
	for _, d := range diags {
		lines = append(lines, fmt.Sprintf("%s:%d:%d [%s] %s", rel, d.Range.Start.Line+1, d.Range.Start.Character+1, severity[d.Severity], d.Message))
	}
	sort.Strings(lines)
	return truncateToolResult(strings.Join(lines, "\n"))
}

func (a *App) toolLSPRename(ctx context.Context, s seat, arg string) string {
	cfg := a.currentSettings()
	project, rest := parseProjectArg(arg)
	lines := strings.Split(rest, "\n")
	if len(lines) < 4 {
		return "Error: format lsp_rename — baris 1 path file, baris 2 baris, baris 3 kolom, baris 4 nama baru."
	}
	newName := strings.TrimSpace(lines[3])
	if newName == "" || len(newName) > 100 || strings.ContainsAny(newName, " \t\r\n") {
		return "Error: nama baru tidak valid."
	}
	client, dir, rel, uri, pos, err := a.lspOpenProjectFile(ctx, cfg, arg, true)
	if err != nil {
		if strings.Contains(err.Error(), "server LSP") {
			return lspMissingError(lspLangOf(arg))
		}
		return "Error: " + err.Error()
	}
	defer client.close()
	raw, err := client.request("textDocument/rename", map[string]any{
		"textDocument": map[string]any{"uri": uri}, "position": pos, "newName": newName,
	})
	if err != nil {
		return "Error: " + err.Error()
	}
	var edit struct {
		Changes         map[string][]lspTextEdit `json:"changes"`
		DocumentChanges []struct {
			TextDocument struct {
				URI string `json:"uri"`
			} `json:"textDocument"`
			Edits []lspTextEdit `json:"edits"`
		} `json:"documentChanges"`
	}
	if json.Unmarshal(raw, &edit) != nil {
		return "Language server tidak menghasilkan perubahan rename."
	}
	for _, dc := range edit.DocumentChanges {
		if edit.Changes == nil {
			edit.Changes = map[string][]lspTextEdit{}
		}
		edit.Changes[dc.TextDocument.URI] = append(edit.Changes[dc.TextDocument.URI], dc.Edits...)
	}
	if len(edit.Changes) == 0 {
		return "Language server tidak menghasilkan perubahan rename."
	}
	total := 0
	for _, edits := range edit.Changes {
		total += len(edits)
	}
	detail := fmt.Sprintf("lsp_rename proyek %s: %s:%d:%d -> %q (%d edit di %d file)", project, rel, pos.Line+1, pos.Character+1, newName, total, len(edit.Changes))
	if !a.requestApproval(ctx, s, "edit", "", detail) {
		return fmt.Sprintf("Rename TIDAK diterapkan: pemilik menolak atau tidak menyetujui dalam %d detik.", int(approvalTimeout.Seconds()))
	}
	if _, err := a.createSnapshot(project, "sebelum lsp_rename"); err != nil {
		return "Error: gagal membuat snapshot proyek sebelum rename."
	}
	changed := []string{}
	for fileURI, edits := range edit.Changes {
		path := strings.TrimPrefix(fileURI, "file://")
		if !strings.HasPrefix(path, dir+string(os.PathSeparator)) {
			continue // hanya file di dalam proyek
		}
		if err := applyLSPTextEdits(path, edits); err != nil {
			return "Error: gagal menerapkan edit ke " + filepath.Base(path) + ": " + err.Error()
		}
		changed = append(changed, projectRel(dir, path))
	}
	sort.Strings(changed)
	return fmt.Sprintf("Rename diterapkan (%d edit): %s", total, strings.Join(changed, ", "))
}

// applyLSPTextEdits menerapkan edit teks LSP ke satu file (dari akhir
// agar offset tidak bergeser).
func applyLSPTextEdits(path string, edits []lspTextEdit) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	lines := strings.SplitAfter(string(raw), "\n")
	offset := func(pos lspPosition) int {
		n := 0
		for i := 0; i < pos.Line && i < len(lines); i++ {
			n += len(lines[i])
		}
		if pos.Line < len(lines) {
			runes := []rune(lines[pos.Line])
			char := pos.Character
			if char > len(runes) {
				char = len(runes)
			}
			n += len(string(runes[:char]))
		}
		return n
	}
	sort.Slice(edits, func(i, j int) bool {
		return offset(edits[i].Range.Start) > offset(edits[j].Range.Start)
	})
	content := string(raw)
	for _, edit := range edits {
		start, end := offset(edit.Range.Start), offset(edit.Range.End)
		if start < 0 || end < start || end > len(content) {
			return fmt.Errorf("rentang edit di luar file")
		}
		content = content[:start] + edit.NewText + content[end:]
	}
	return writeProjectFile(path, []byte(content))
}

// lspLangOf menebak bahasa dari argumen (untuk pesan server hilang).
func lspLangOf(arg string) string {
	_, rest := parseProjectArg(arg)
	first, _, _ := strings.Cut(rest, "\n")
	return lspLangByExt[strings.ToLower(filepath.Ext(strings.TrimSpace(first)))]
}

// currentSettings mengambil salinan settings saat ini.
func (a *App) currentSettings() settings {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.data.Settings
}
