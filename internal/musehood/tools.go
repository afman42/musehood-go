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
func (a *App) chatWithTools(ctx context.Context, cfg settings, s seat, history []gatewayMessage, tools bool) (workerReply, error) {
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
		result := a.runTool(ctx, name, arg)
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

func (a *App) runTool(ctx context.Context, name, arg string) string {
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
