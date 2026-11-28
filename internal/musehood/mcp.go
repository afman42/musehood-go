package musehood

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strings"
	"time"
)

// --- Dukungan MCP (Model Context Protocol, client stdio) ---
//
// Server MCP dikonfigurasi di settings.MCPServers (maks 5). Client
// menjalankan proses server PER PANGGILAN (initialize → tools/list atau
// tools/call → proses dimatikan) agar sederhana dan aman: tidak ada
// proses latar yang tertinggal. Daftar tool di-cache 60 detik per
// server agar instruksi kursi tidak men-spawn proses terus-menerus.
// Tool MCP diekspos ke kursi sebagai mcp_<server>_<tool>; setiap
// panggilan memerlukan persetujuan pemilik (kind "mcp") karena tool
// eksternal dapat melakukan apa saja. Nilai env server tidak pernah
// keluar lewat state/cadangan/API daftar (hanya nama kuncinya).

const (
	maxMCPServers      = 5
	maxMCPToolsPerSrv  = 50
	mcpListTimeout     = 15 * time.Second
	mcpCallTimeout     = 60 * time.Second
	mcpCacheTTL        = 60 * time.Second
	mcpProtocolVersion = "2024-11-05"
)

var (
	mcpServerNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,30}$`)
	mcpEnvKeyPattern     = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	mcpToolSanitize      = regexp.MustCompile(`[^a-z0-9_]+`)
)

// mcpServerConfig adalah konfigurasi satu server MCP stdio.
type mcpServerConfig struct {
	Name    string            `json:"name"`
	Command string            `json:"command"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
}

// mcpToolDef adalah satu tool dari tools/list server MCP.
type mcpToolDef struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

type mcpCacheEntry struct {
	tools []mcpToolDef
	err   error
	at    time.Time
}

// mcpToolView adalah tampilan satu tool MCP untuk GET /api/tools.
type mcpToolView struct {
	Name        string `json:"name"` // nama terekspos: mcp_<server>_<tool>
	Server      string `json:"server"`
	Tool        string `json:"tool"`
	Description string `json:"description,omitempty"`
	Source      string `json:"source"`
}

func validateMCPServer(cfg mcpServerConfig) error {
	if !mcpServerNamePattern.MatchString(cfg.Name) {
		return problem(400, "Nama server MCP tidak valid (huruf kecil diawali huruf, angka, _, maks 31 karakter).")
	}
	if strings.TrimSpace(cfg.Command) == "" || len(cfg.Command) > 500 {
		return problem(400, "Perintah server MCP wajib diisi (maks 500 karakter).")
	}
	if len(cfg.Args) > 20 {
		return problem(400, "Argumen server MCP maksimal 20.")
	}
	for _, arg := range cfg.Args {
		if len(arg) > 500 {
			return problem(400, "Argumen server MCP maksimal 500 karakter per argumen.")
		}
	}
	if len(cfg.Env) > 20 {
		return problem(400, "Variabel env server MCP maksimal 20.")
	}
	for key, value := range cfg.Env {
		if !mcpEnvKeyPattern.MatchString(key) {
			return problem(400, "Nama variabel env tidak valid: "+key)
		}
		if len(value) > 2000 {
			return problem(400, "Nilai variabel env maksimal 2000 karakter.")
		}
	}
	return nil
}

// sanitizeMCPToolName mengubah nama tool MCP menjadi bagian nama tool
// kursi yang valid ([a-z0-9_]).
func sanitizeMCPToolName(name string) string {
	out := mcpToolSanitize.ReplaceAllString(strings.ToLower(strings.TrimSpace(name)), "_")
	out = strings.Trim(out, "_")
	if out == "" {
		out = "tool"
	}
	if len(out) > 40 {
		out = out[:40]
	}
	return out
}

func mcpExposedName(server, tool string) string {
	return "mcp_" + server + "_" + sanitizeMCPToolName(tool)
}

// --- CRUD (dipanggil dengan a.mu terkunci) ---

func (a *App) mcpServerViewsLocked() []map[string]any {
	views := []map[string]any{}
	for _, cfg := range a.data.Settings.MCPServers {
		keys := make([]string, 0, len(cfg.Env))
		for key := range cfg.Env {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		args := cfg.Args
		if args == nil {
			args = []string{}
		}
		views = append(views, map[string]any{
			"name": cfg.Name, "command": cfg.Command, "args": args, "envKeys": keys,
		})
	}
	return views
}

func (a *App) upsertMCPServerLocked(input mcpServerConfig) error {
	if err := validateMCPServer(input); err != nil {
		return err
	}
	servers := a.data.Settings.MCPServers
	for i := range servers {
		if servers[i].Name == input.Name {
			if input.Env == nil {
				input.Env = servers[i].Env // env tidak dikirim = pertahankan
			}
			servers[i] = input
			a.data.Settings.MCPServers = servers
			for _, value := range input.Env {
				a.rememberKeyLocked(value)
			}
			a.invalidateMCPCache(input.Name)
			return a.saveLocked()
		}
	}
	if len(servers) >= maxMCPServers {
		return problem(400, fmt.Sprintf("Maksimal %d server MCP.", maxMCPServers))
	}
	a.data.Settings.MCPServers = append(servers, input)
	for _, value := range input.Env {
		a.rememberKeyLocked(value)
	}
	a.invalidateMCPCache(input.Name)
	return a.saveLocked()
}

func (a *App) deleteMCPServerLocked(name string) error {
	servers := a.data.Settings.MCPServers
	for i := range servers {
		if servers[i].Name == name {
			a.data.Settings.MCPServers = append(servers[:i], servers[i+1:]...)
			a.invalidateMCPCache(name)
			return a.saveLocked()
		}
	}
	return problem(404, "Server MCP tidak ditemukan.")
}

func (a *App) invalidateMCPCache(name string) {
	a.mcpMu.Lock()
	defer a.mcpMu.Unlock()
	for key := range a.mcpCache {
		if strings.HasPrefix(key, name+"\x00") {
			delete(a.mcpCache, key)
		}
	}
}

// --- Client JSON-RPC stdio (proses per panggilan) ---

type mcpRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type mcpRPCResponse struct {
	ID     *int            `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *mcpRPCError    `json:"error"`
}

// mcpSession adalah satu proses server MCP yang hidup selama satu
// rangkaian permintaan (initialize + satu permintaan utama).
type mcpSession struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	reader *bufio.Reader
	nextID int
}

func startMCPSession(ctx context.Context, cfg mcpServerConfig) (*mcpSession, error) {
	cmd := exec.CommandContext(ctx, cfg.Command, cfg.Args...)
	env := os.Environ()
	for key, value := range cfg.Env {
		env = append(env, key+"="+value)
	}
	cmd.Env = env
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return &mcpSession{cmd: cmd, stdin: stdin, reader: bufio.NewReaderSize(stdout, 64*1024), nextID: 1}, nil
}

func (s *mcpSession) close() {
	_ = s.stdin.Close()
	if s.cmd.Process != nil {
		_ = s.cmd.Process.Kill()
	}
	_ = s.cmd.Wait()
}

func (s *mcpSession) send(payload map[string]any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = s.stdin.Write(append(raw, '\n'))
	return err
}

// call mengirim satu permintaan dan menunggu respons ber-id sama
// (pesan lain/notifikasi dilewati).
func (s *mcpSession) call(method string, params any) (json.RawMessage, error) {
	id := s.nextID
	s.nextID++
	if err := s.send(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params}); err != nil {
		return nil, err
	}
	for {
		line, err := s.reader.ReadString('\n')
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			var resp mcpRPCResponse
			if json.Unmarshal([]byte(trimmed), &resp) == nil && resp.ID != nil && *resp.ID == id {
				if resp.Error != nil {
					return nil, fmt.Errorf("server MCP menolak %s: %s", method, resp.Error.Message)
				}
				return resp.Result, nil
			}
		}
		if err != nil {
			return nil, fmt.Errorf("koneksi server MCP terputus saat %s", method)
		}
	}
}

func (s *mcpSession) initialize() error {
	if _, err := s.call("initialize", map[string]any{
		"protocolVersion": mcpProtocolVersion,
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "musehood-go", "version": "1.0"},
	}); err != nil {
		return err
	}
	return s.send(map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"})
}

// mcpListToolsRaw menjalankan proses server dan mengambil tools/list.
func mcpListToolsRaw(ctx context.Context, cfg mcpServerConfig) ([]mcpToolDef, error) {
	runCtx, cancel := context.WithTimeout(ctx, mcpListTimeout)
	defer cancel()
	session, err := startMCPSession(runCtx, cfg)
	if err != nil {
		return nil, err
	}
	defer session.close()
	if err := session.initialize(); err != nil {
		return nil, err
	}
	raw, err := session.call("tools/list", map[string]any{})
	if err != nil {
		return nil, err
	}
	var result struct {
		Tools []mcpToolDef `json:"tools"`
	}
	if json.Unmarshal(raw, &result) != nil {
		return nil, fmt.Errorf("respons tools/list tidak valid")
	}
	if len(result.Tools) > maxMCPToolsPerSrv {
		result.Tools = result.Tools[:maxMCPToolsPerSrv]
	}
	return result.Tools, nil
}

// mcpCallToolRaw menjalankan proses server dan memanggil satu tool.
func mcpCallToolRaw(ctx context.Context, cfg mcpServerConfig, toolName string, arguments map[string]any) (string, error) {
	runCtx, cancel := context.WithTimeout(ctx, mcpCallTimeout)
	defer cancel()
	session, err := startMCPSession(runCtx, cfg)
	if err != nil {
		return "", err
	}
	defer session.close()
	if err := session.initialize(); err != nil {
		return "", err
	}
	raw, err := session.call("tools/call", map[string]any{"name": toolName, "arguments": arguments})
	if err != nil {
		return "", err
	}
	var result struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if json.Unmarshal(raw, &result) != nil {
		return truncateToolResult(string(raw)), nil
	}
	var texts []string
	for _, item := range result.Content {
		if item.Type == "text" && item.Text != "" {
			texts = append(texts, item.Text)
		}
	}
	text := strings.Join(texts, "\n")
	if text == "" {
		text = string(raw)
	}
	if result.IsError {
		return "", fmt.Errorf("%s", truncateRunes(text, 500))
	}
	return truncateToolResult(text), nil
}

// mcpToolsFor mengambil daftar tool satu server dari cache (60 dtk).
// Fungsi ini TIDAK pernah men-spawn proses secara sinkron: pemanggil
// sering memegang a.mu (instruksi kursi, /api/tools), jadi cache yang
// kosong/kedaluwarsa di-refresh di goroutine latar belakang dan hasil
// lama (atau kosong) dikembalikan segera.
func (a *App) mcpToolsFor(cfg mcpServerConfig) []mcpToolDef {
	key := cfg.Name + "\x00" + cfg.Command + "\x00" + strings.Join(cfg.Args, "\x00")
	a.mcpMu.Lock()
	defer a.mcpMu.Unlock()
	if a.mcpCache == nil {
		a.mcpCache = map[string]mcpCacheEntry{}
	}
	if a.mcpRefreshing == nil {
		a.mcpRefreshing = map[string]bool{}
	}
	entry, ok := a.mcpCache[key]
	stale := !ok || time.Since(entry.at) >= mcpCacheTTL
	if stale && !a.mcpRefreshing[key] {
		a.mcpRefreshing[key] = true
		go a.refreshMCPTools(key, cfg)
	}
	return entry.tools
}

func (a *App) refreshMCPTools(key string, cfg mcpServerConfig) {
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	tools, err := mcpListToolsRaw(ctx, cfg)
	a.mcpMu.Lock()
	defer a.mcpMu.Unlock()
	if a.mcpCache == nil {
		a.mcpCache = map[string]mcpCacheEntry{}
	}
	a.mcpCache[key] = mcpCacheEntry{tools: tools, err: err, at: time.Now()}
	delete(a.mcpRefreshing, key)
}

// mcpToolsForSync mengambil daftar tool secara sinkron (dipakai jalur
// panggilan tool yang tidak memegang a.mu, agar tool yang baru
// dikonfigurasi tetap ditemukan).
func (a *App) mcpToolsForSync(ctx context.Context, cfg mcpServerConfig) []mcpToolDef {
	tools := a.mcpToolsFor(cfg)
	if len(tools) > 0 {
		return tools
	}
	fresh, err := mcpListToolsRaw(ctx, cfg)
	key := cfg.Name + "\x00" + cfg.Command + "\x00" + strings.Join(cfg.Args, "\x00")
	a.mcpMu.Lock()
	defer a.mcpMu.Unlock()
	if a.mcpCache == nil {
		a.mcpCache = map[string]mcpCacheEntry{}
	}
	a.mcpCache[key] = mcpCacheEntry{tools: fresh, err: err, at: time.Now()}
	return fresh
}

// mcpToolViews mendaftar semua tool MCP terekspos dari daftar server
// yang diberikan (tanpa mengunci a.mu).
func (a *App) mcpToolViews(servers []mcpServerConfig) []mcpToolView {
	views := []mcpToolView{}
	for _, cfg := range servers {
		for _, tool := range a.mcpToolsFor(cfg) {
			views = append(views, mcpToolView{
				Name:        mcpExposedName(cfg.Name, tool.Name),
				Server:      cfg.Name,
				Tool:        tool.Name,
				Description: tool.Description,
				Source:      "mcp",
			})
		}
	}
	return views
}

// mcpInstructionLines adalah baris instruksi tool MCP untuk system
// prompt kursi (juga menjadi definisi tool native).
func (a *App) mcpInstructionLines(s seat, servers []mcpServerConfig) string {
	views := a.mcpToolViews(servers)
	var lines []string
	for _, view := range views {
		if s.toolDisabled(view.Name) {
			continue
		}
		desc := strings.TrimSpace(view.Description)
		if desc == "" {
			desc = "Tool MCP dari server " + view.Server
		}
		lines = append(lines, fmt.Sprintf("- %s: %s. Argumen: JSON objek argumen tool (mis. {\"path\":\"/tmp\"}), atau kosongkan. Memerlukan persetujuan pemilik.", view.Name, desc))
	}
	if len(lines) == 0 {
		return ""
	}
	return "\n" + strings.Join(lines, "\n")
}

// runMCPTool menjalankan satu panggilan tool MCP dengan persetujuan
// pemilik (kind "mcp"). cfg adalah settings aktif pekerjaan ini.
func (a *App) runMCPTool(ctx context.Context, cfg settings, s seat, name, arg string) string {
	var view *mcpToolView
	var serverCfg *mcpServerConfig
	for i := range cfg.MCPServers {
		server := cfg.MCPServers[i]
		for _, tool := range a.mcpToolsForSync(ctx, server) {
			if mcpExposedName(server.Name, tool.Name) == name {
				view = &mcpToolView{Name: name, Server: server.Name, Tool: tool.Name, Description: tool.Description, Source: "mcp"}
				serverCfg = &server
				break
			}
		}
		if view != nil {
			break
		}
	}
	if view == nil || serverCfg == nil {
		return fmt.Sprintf("Tool %q tidak dikenal (server MCP tidak terkonfigurasi atau tool tidak tersedia).", name)
	}
	arguments := map[string]any{}
	trimmed := strings.TrimSpace(arg)
	if trimmed != "" {
		if json.Unmarshal([]byte(trimmed), &arguments) != nil {
			arguments = map[string]any{"arg": trimmed}
		}
	}
	detail := fmt.Sprintf("MCP %s/%s: %s", view.Server, view.Tool, truncateRunes(trimmed, 300))
	if !a.requestApproval(ctx, s, "mcp", "", detail) {
		return fmt.Sprintf("Panggilan MCP TIDAK dijalankan: pemilik menolak atau tidak menyetujui dalam %d detik.", int(approvalTimeout.Seconds()))
	}
	result, err := mcpCallToolRaw(ctx, *serverCfg, view.Tool, arguments)
	if err != nil {
		return "Error: panggilan MCP gagal: " + err.Error()
	}
	return result
}
