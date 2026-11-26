package musehood

import (
	"context"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Tool-calling untuk kursi gateway memakai protokol teks agar bekerja di
// semua provider/protokol (termasuk jembatan CLI yang hanya mengembalikan
// teks): model menulis blok <<TOOL:nama>>argumen<</TOOL>>, server
// menjalankan tool-nya, lalu hasil tool dikirim kembali sebagai pesan
// lanjutan. Maksimal maxToolRounds putaran per balasan.
const maxToolRounds = 3
const maxToolResultChars = 4000
const maxSandboxFileBytes = 100 * 1024

var toolBlockPattern = regexp.MustCompile(`(?s)<<TOOL:([a-z_]+)>>\s*(.*?)\s*<</TOOL>>`)

const toolInstructions = `

Kamu dapat memakai tool berikut. Untuk memakai tool, tulis blok persis seperti ini (satu tool per balasan, tanpa teks lain di luar blok):
<<TOOL:nama_tool>>
argumen
<</TOOL>>
Setelah itu kamu akan menerima hasil tool sebagai pesan berikutnya dan dapat melanjutkan jawaban untuk pengguna.

Tool yang tersedia:
- web_search: cari informasi di web. Argumen: kata kunci pencarian.
- fetch_url: ambil isi teks halaman web. Argumen: URL lengkap (http/https).
- list_files: daftar file di folder kerja bersama. Argumen: kosongkan.
- read_file: baca isi file di folder kerja. Argumen: nama file.
- write_file: tulis file ke folder kerja. Baris pertama argumen: nama file; baris berikutnya: isi file.
- current_time: tanggal dan waktu sekarang. Argumen: kosongkan.

Gunakan tool hanya bila benar-benar perlu (mis. butuh info terkini dari web atau isi file). Bila tidak perlu, jawab langsung tanpa blok tool.`

const codeToolInstruction = `
- run_code: jalankan kode di sandbox terisolasi (tanpa jaringan, maks 10 detik). Baris pertama argumen: bahasa (python atau javascript); baris berikutnya: kode. Hasil stdout/stderr dikembalikan kepadamu. Kode yang terdeteksi berbahaya memerlukan persetujuan pemilik sebelum dijalankan.`

func buildToolInstructions(code bool) string {
	if code {
		return toolInstructions + codeToolInstruction
	}
	return toolInstructions
}

func parseToolBlock(text string) (name, arg string, found bool) {
	match := toolBlockPattern.FindStringSubmatch(text)
	if match == nil {
		return "", "", false
	}
	return match[1], strings.TrimSpace(match[2]), true
}

func stripToolBlocks(text string) string {
	return strings.TrimSpace(toolBlockPattern.ReplaceAllString(text, ""))
}

func truncateToolResult(s string) string {
	if len(s) > maxToolResultChars {
		return s[:maxToolResultChars] + "\n…(dipotong)"
	}
	return s
}

// chatWithTools menjalankan chat gateway dengan loop tool: bila balasan
// model memuat blok tool, tool dijalankan dan hasilnya dikirim kembali
// sebagai pesan lanjutan (maksimal maxToolRounds putaran). Bila putaran
// lanjutan gagal, teks terakhir yang berhasil dipakai.
func (a *App) chatWithTools(ctx context.Context, cfg settings, s seat, history []gatewayMessage, tools, code bool) (workerReply, error) {
	text, err := a.chat(ctx, cfg, s, history)
	if err != nil {
		return workerReply{}, err
	}
	if !tools {
		return workerReply{Text: text}, nil
	}
	var used []string
	for round := 0; round < maxToolRounds; round++ {
		name, arg, found := parseToolBlock(text)
		if !found {
			break
		}
		result := a.runTool(ctx, s, name, arg, code)
		used = append(used, name)
		history = append(history,
			gatewayMessage{"assistant", text},
			gatewayMessage{"user", fmt.Sprintf("Hasil tool %s:\n%s\n\nLanjutkan jawabanmu untuk pengguna berdasarkan hasil di atas.", name, result)},
		)
		next, err := a.chat(ctx, cfg, s, history)
		if err != nil {
			break
		}
		text = next
	}
	return workerReply{Text: stripToolBlocks(text), ToolsUsed: used}, nil
}

func (a *App) sandboxDir() string {
	return filepath.Join(a.dir, "sandbox")
}

// sandboxPath memvalidasi nama file agar tetap di dalam folder sandbox.
func (a *App) sandboxPath(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || strings.Contains(name, "/") || strings.Contains(name, "\\") || strings.Contains(name, "..") {
		return "", fmt.Errorf("nama file tidak valid")
	}
	dir := a.sandboxDir()
	full := filepath.Join(dir, name)
	if filepath.Dir(full) != dir {
		return "", fmt.Errorf("nama file tidak valid")
	}
	return full, nil
}

func (a *App) runTool(ctx context.Context, s seat, name, arg string, code bool) string {
	switch name {
	case "web_search":
		return a.toolWebSearch(ctx, arg)
	case "fetch_url":
		return a.toolFetchURL(ctx, arg)
	case "list_files":
		return a.toolListFiles()
	case "read_file":
		return a.toolReadFile(arg)
	case "write_file":
		return a.toolWriteFile(arg)
	case "current_time":
		return toolCurrentTime()
	case "run_code":
		if !code {
			return "Error: tool coding dimatikan di Pengaturan."
		}
		return a.toolRunCode(ctx, s, arg)
	default:
		return fmt.Sprintf("Tool %q tidak dikenal.", name)
	}
}

func toolCurrentTime() string {
	loc, err := time.LoadLocation("Asia/Jakarta")
	if err != nil {
		loc = time.Local
	}
	return time.Now().In(loc).Format("Monday, 2 January 2006 15:04 MST")
}

func (a *App) toolListFiles() string {
	entries, err := os.ReadDir(a.sandboxDir())
	if err != nil {
		return "Folder kerja kosong."
	}
	var lines []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		lines = append(lines, fmt.Sprintf("%s (%d byte)", entry.Name(), info.Size()))
	}
	if len(lines) == 0 {
		return "Folder kerja kosong."
	}
	sort.Strings(lines)
	return truncateToolResult(strings.Join(lines, "\n"))
}

func (a *App) toolReadFile(name string) string {
	full, err := a.sandboxPath(name)
	if err != nil {
		return "Error: " + err.Error()
	}
	raw, err := os.ReadFile(full)
	if err != nil {
		return "Error: file tidak ditemukan."
	}
	return truncateToolResult(string(raw))
}

func (a *App) toolWriteFile(arg string) string {
	name, content, _ := strings.Cut(arg, "\n")
	full, err := a.sandboxPath(name)
	if err != nil {
		return "Error: " + err.Error()
	}
	if len(content) > maxSandboxFileBytes {
		return "Error: isi file terlalu besar (maks 100 KB)."
	}
	if err := os.MkdirAll(a.sandboxDir(), 0o755); err != nil {
		return "Error: gagal membuat folder kerja."
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		return "Error: gagal menulis file."
	}
	return fmt.Sprintf("File %s tersimpan (%d byte).", filepath.Base(full), len(content))
}

const browserUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"

var toolHTTPClient = &http.Client{Timeout: 25 * time.Second}

func toolGet(ctx context.Context, rawURL string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", browserUserAgent)
	resp, err := toolHTTPClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 512*1024))
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

var (
	scriptStylePattern = regexp.MustCompile(`(?is)<(script|style)[^>]*>.*?</(script|style)>`)
	tagPattern         = regexp.MustCompile(`(?s)<[^>]+>`)
	spacePattern       = regexp.MustCompile(`[ \t]+`)
	blankLinePattern   = regexp.MustCompile(`\n{3,}`)
)

func htmlToText(raw string) string {
	text := scriptStylePattern.ReplaceAllString(raw, " ")
	text = tagPattern.ReplaceAllString(text, "\n")
	text = html.UnescapeString(text)
	text = spacePattern.ReplaceAllString(text, " ")
	text = blankLinePattern.ReplaceAllString(text, "\n\n")
	return strings.TrimSpace(text)
}

// checkPublicURL menolak URL ke alamat privat/loopback (anti-SSRF).
// Catatan: permintaan keluar lewat proxy egress; pemeriksaan DNS di sini
// memblokir target privat yang jelas sebelum permintaan dibuat.
func checkPublicURL(ctx context.Context, raw string) error {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return fmt.Errorf("URL harus http/https yang valid")
	}
	host := u.Hostname()
	if host == "" || strings.EqualFold(host, "localhost") {
		return fmt.Errorf("host tidak valid")
	}
	ips, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
	if err != nil || len(ips) == 0 {
		return fmt.Errorf("host tidak dapat dijangkau")
	}
	for _, ip := range ips {
		if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified() {
			return fmt.Errorf("alamat privat tidak diizinkan")
		}
	}
	return nil
}

func (a *App) toolFetchURL(ctx context.Context, raw string) string {
	if err := checkPublicURL(ctx, raw); err != nil {
		return "Error: " + err.Error()
	}
	body, err := toolGet(ctx, strings.TrimSpace(raw))
	if err != nil {
		return "Error: gagal mengambil halaman (" + err.Error() + ")."
	}
	return truncateToolResult(htmlToText(body))
}

var (
	ddgLinkPattern    = regexp.MustCompile(`(?is)<a[^>]+class=['"]result-link['"][^>]+href=['"]([^'"]+)['"][^>]*>(.*?)</a>`)
	ddgSnippetPattern = regexp.MustCompile(`(?is)<td[^>]+class=['"]result-snippet['"][^>]*>(.*?)</td>`)
)

func (a *App) toolWebSearch(ctx context.Context, query string) string {
	query = strings.TrimSpace(query)
	if query == "" {
		return "Error: kata kunci kosong."
	}
	body, err := toolGet(ctx, "https://lite.duckduckgo.com/lite/?q="+url.QueryEscape(query))
	if err != nil {
		return "Error: pencarian gagal (" + err.Error() + ")."
	}
	links := ddgLinkPattern.FindAllStringSubmatch(body, 5)
	snippets := ddgSnippetPattern.FindAllStringSubmatch(body, 5)
	if len(links) == 0 {
		text := htmlToText(body)
		if text == "" {
			return "Tidak ada hasil."
		}
		return truncateToolResult(text)
	}
	var lines []string
	for i, link := range links {
		title := strings.TrimSpace(htmlToText(link[2]))
		href := html.UnescapeString(link[1])
		if parsed, err := url.Parse(href); err == nil {
			if target := parsed.Query().Get("uddg"); target != "" {
				href = target
			}
		}
		snippet := ""
		if i < len(snippets) {
			snippet = strings.TrimSpace(htmlToText(snippets[i][1]))
		}
		lines = append(lines, fmt.Sprintf("%d. %s\n%s\n%s", i+1, title, snippet, href))
	}
	return truncateToolResult(strings.Join(lines, "\n\n"))
}

// --- Tool coding (run_code) dengan sandbox + persetujuan pemilik ---

const codeTimeout = 10 * time.Second
const approvalTimeout = 180 * time.Second

// dangerousCodePatterns mendeteksi kode yang memerlukan persetujuan
// pemilik sebelum dijalankan (pola seperti deteksi perintah berbahaya
// pada agent terminal; berbasis pola, bukan jaminan).
var dangerousCodePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\brm\s+-[a-z]*[rf]`),
	regexp.MustCompile(`(?i)\b(sudo|su\s+-|doas)\b`),
	regexp.MustCompile(`(?i)\b(curl|wget|nc|netcat|ssh|scp|ftp)\b`),
	regexp.MustCompile(`(?i)\b(socket|urllib|axios|XMLHttpRequest)\b|\b(requests\.|http\.client|fetch\s*\()`),
	regexp.MustCompile(`(?i)\b(os\.system|subprocess|child_process|__import__)\b|\b(exec|eval|compile)\s*\(`),
	regexp.MustCompile(`(?i)(/etc/(passwd|shadow)|muse-secrets|\.ssh|id_rsa|credentials)`),
	regexp.MustCompile(`(?i)\b(shutil\.rmtree|os\.remove|os\.unlink|fs\.rm|unlinkSync|rmdir)\b`),
	regexp.MustCompile(`(?i)\b(chmod|chown|mkfs|dd\s+if=|shutdown|reboot|killall|pkill)\b`),
	regexp.MustCompile(`(?i)\bwhile\s+(True|1)\b|for\s*\(\s*;\s*;`),
	regexp.MustCompile(`(?i)\b(open|readFileSync|createReadStream)\s*\(\s*['"]/`), // baca path absolut
}

func dangerousCode(code string) (string, bool) {
	for _, pattern := range dangerousCodePatterns {
		if match := pattern.FindString(code); match != "" {
			return match, true
		}
	}
	return "", false
}

// codeApproval adalah permintaan persetujuan pemilik untuk menjalankan
// kode yang terdeteksi berbahaya.
type codeApproval struct {
	ID        string `json:"id"`
	SeatCode  string `json:"seatCode"`
	SeatName  string `json:"seatName"`
	Language  string `json:"language"`
	Code      string `json:"code"`
	CreatedAt string `json:"createdAt"`
	done      chan bool
}

func (a *App) requestCodeApproval(ctx context.Context, s seat, language, code string) bool {
	approval := &codeApproval{
		ID: uuid(), SeatCode: s.Code, SeatName: s.Name,
		Language: language, Code: code, CreatedAt: now(),
		done: make(chan bool, 1),
	}
	a.approvalMu.Lock()
	if a.approvals == nil {
		a.approvals = map[string]*codeApproval{}
	}
	a.approvals[approval.ID] = approval
	a.approvalMu.Unlock()
	defer func() {
		a.approvalMu.Lock()
		delete(a.approvals, approval.ID)
		a.approvalMu.Unlock()
	}()
	select {
	case approved := <-approval.done:
		return approved
	case <-time.After(approvalTimeout):
		return false
	case <-ctx.Done():
		return false
	}
}

func (a *App) pendingApprovals() []codeApproval {
	a.approvalMu.Lock()
	defer a.approvalMu.Unlock()
	out := make([]codeApproval, 0, len(a.approvals))
	for _, approval := range a.approvals {
		out = append(out, *approval)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt < out[j].CreatedAt })
	return out
}

func (a *App) decideApproval(id string, approved bool) bool {
	a.approvalMu.Lock()
	defer a.approvalMu.Unlock()
	approval, ok := a.approvals[id]
	if !ok {
		return false
	}
	select {
	case approval.done <- approved:
	default:
	}
	return true
}

func (a *App) toolRunCode(ctx context.Context, s seat, arg string) string {
	language, code, found := strings.Cut(arg, "\n")
	language = strings.ToLower(strings.TrimSpace(language))
	if !found || strings.TrimSpace(code) == "" {
		return "Error: format run_code adalah baris pertama bahasa (python/javascript), baris berikutnya kode."
	}
	var ext, interpreter string
	switch language {
	case "python", "py":
		language, ext, interpreter = "python", "py", "python3"
	case "javascript", "js", "node":
		language, ext, interpreter = "javascript", "js", "node"
	default:
		return "Error: bahasa harus python atau javascript."
	}
	if len(code) > maxSandboxFileBytes {
		return "Error: kode terlalu besar (maks 100 KB)."
	}
	if match, dangerous := dangerousCode(code); dangerous {
		if !a.requestCodeApproval(ctx, s, language, code) {
			return fmt.Sprintf("Kode TIDAK dijalankan: terdeteksi pola berbahaya (%q) dan pemilik menolak atau tidak menyetujui dalam %d detik. Tulis ulang kode yang lebih aman atau jelaskan kebutuhanmu.", match, int(approvalTimeout.Seconds()))
		}
	}
	return a.executeCode(ctx, interpreter, ext, code)
}

// executeCode menjalankan kode di sandbox bubblewrap sebagai user nobody:
// filesystem di luar folder kerja tidak terlihat, tanpa jaringan, dengan
// batas CPU/memori/proses lewat cgroup systemd-run --scope.
func (a *App) executeCode(ctx context.Context, interpreter, ext, code string) string {
	dir := a.sandboxDir()
	if err := os.MkdirAll(dir, 0o777); err != nil {
		return "Error: gagal membuat folder kerja."
	}
	name := "run-" + uuid() + "." + ext
	full := filepath.Join(dir, name)
	if err := os.WriteFile(full, []byte(code), 0o644); err != nil {
		return "Error: gagal menulis berkas kode."
	}
	defer os.Remove(full)

	bwrapArgs := []string{
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
		"timeout", fmt.Sprint(int(codeTimeout.Seconds())), interpreter, name,
	}
	runCtx, cancel := context.WithTimeout(ctx, codeTimeout+15*time.Second)
	defer cancel()

	args := append([]string{"--quiet", "--scope", "-p", "MemoryMax=256M", "-p", "TasksMax=64", "-p", "CPUQuota=50%", "bwrap"}, bwrapArgs...)
	cmd := exec.CommandContext(runCtx, "systemd-run", args...)
	out, err := cmd.CombinedOutput()
	if err != nil && strings.Contains(string(out), "Failed to") {
		// systemd-run tidak tersedia di lingkungan ini: jalankan bwrap langsung.
		cmd = exec.CommandContext(runCtx, "bwrap", bwrapArgs...)
		out, err = cmd.CombinedOutput()
	}
	text := strings.TrimSpace(string(out))
	if err != nil {
		if runCtx.Err() == context.DeadlineExceeded {
			return truncateToolResult("Error: kode melebihi batas waktu.\n" + text)
		}
		if text == "" {
			text = err.Error()
		}
		return truncateToolResult("Kode selesai dengan error:\n" + text)
	}
	if text == "" {
		text = "(tidak ada output)"
	}
	return truncateToolResult(text)
}
