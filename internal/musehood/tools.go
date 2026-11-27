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
	"path"
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
- append_file: tambahkan teks ke akhir file di folder kerja (dibuat bila belum ada). Baris pertama argumen: nama file; baris berikutnya: teks tambahan.
- edit_file: ganti sebagian isi file. Format argumen: baris pertama nama file, lalu baris <<<OLD>>>, teks lama, baris <<<NEW>>>, teks baru.
- delete_file: hapus file di folder kerja. Argumen: nama file. Memerlukan persetujuan pemilik.
- download_file: unduh file dari URL ke folder kerja (maks 10 MB). Baris pertama argumen: URL; baris kedua (opsional): nama file tujuan.
- zip_files: kompres file ke zip. Baris pertama argumen: nama zip keluaran; baris berikutnya: nama file yang dimasukkan.
- unzip_file: ekstrak zip di folder kerja. Argumen: nama file zip.
- file_hash: hitung SHA-256 file. Argumen: nama file.
- image_info: info gambar (format, dimensi, ukuran). Argumen: nama file gambar.
- resize_image: perkecil gambar. Baris 1: nama file; baris 2: lebar maksimum (px); baris 3 (opsional): nama keluaran.
- convert_image: ubah format gambar. Baris 1: nama file; baris 2: format tujuan (png atau jpeg).
- pdf_text: ekstrak teks dari PDF. Argumen: nama file PDF.
- csv_json: konversi CSV↔JSON. Baris 1: nama file; baris 2: arah (csv2json atau json2csv); baris 3 (opsional): nama keluaran.
- generate_image: buat gambar dari deskripsi teks; gambar terlampir pada balasanmu. Argumen: deskripsi gambar.
- memory_save: simpan catatan penting ke memori kursimu (akan kamu ingat di percakapan berikutnya). Argumen: catatan singkat.
- current_time: tanggal dan waktu sekarang. Argumen: kosongkan.
- git: perintah git baca-saja di folder kerja. Argumen: subperintah, mis. "status", "diff", "log --oneline -5", "show HEAD".
- screenshot_url: ambil screenshot halaman web; gambar terlampir pada balasanmu. Argumen: URL lengkap (http/https).
- summarize_url: ambil isi halaman web untuk kamu ringkas. Argumen: URL lengkap (http/https).
- pdf_from_markdown: buat file PDF dari teks Markdown di folder kerja. Baris pertama argumen (opsional): nama file .pdf; baris berikutnya: isi Markdown.
- translate_text: terjemahkan teks antar bahasa. Baris pertama argumen: pasangan bahasa sumber>tujuan (mis. id>en, en>id, id>ja); baris berikutnya: teks yang diterjemahkan (maks 500 karakter).
- ocr_image: baca teks dari gambar di folder kerja (OCR). Argumen: nama file gambar.
- image_caption: jelaskan isi gambar di folder kerja memakai model vision. Baris pertama argumen: nama file gambar; baris kedua (opsional): pertanyaan khusus tentang gambar.
- qr_code: buat QR code dari teks/URL; gambar QR terlampir pada balasanmu. Argumen: teks atau URL (maks 500 karakter).
- rss_feed: baca feed RSS/Atom. Baris pertama argumen: URL feed; baris kedua (opsional): jumlah item (1-10, bawaan 5).
- youtube_transcript: ambil transkrip/caption video YouTube. Argumen: URL video atau ID video.
- currency_convert: konversi mata uang dengan kurs terkini. Argumen: jumlah dan kode mata uang, mis. "100 USD IDR" (jumlah boleh dikosongkan = 1).
- text_to_speech: ubah teks menjadi file audio WAV di folder kerja. Baris pertama argumen (opsional): lang=id atau lang=en; baris berikutnya: teks (maks 1000 karakter).
- http_post: kirim permintaan POST JSON ke URL. Baris pertama argumen: URL; baris berikutnya: isi JSON. Memerlukan persetujuan pemilik.
- password_gen: buat password acak kuat. Argumen (opsional): panjang 8-128 (bawaan 16), tambahkan "nosymbol" untuk tanpa simbol.

Gunakan tool hanya bila benar-benar perlu (mis. butuh info terkini dari web atau isi file). Bila tidak perlu, jawab langsung tanpa blok tool.`

const codeToolInstruction = `
- run_code: jalankan kode di sandbox terisolasi (tanpa jaringan, maks 10 detik). Baris pertama argumen: bahasa (python atau javascript); baris berikutnya: kode. Hasil stdout/stderr dikembalikan kepadamu. Kode yang terdeteksi berbahaya memerlukan persetujuan pemilik sebelum dijalankan.
- run_shell: jalankan perintah shell sederhana di sandbox yang sama (mis. ls, cat, grep). Argumen: perintah shell. Selalu memerlukan persetujuan pemilik.`

func (a *App) buildToolInstructions(code bool, s seat) string {
	text := toolInstructions
	if code {
		text += codeToolInstruction
	}
	if len(s.DisabledTools) > 0 {
		lines := strings.Split(text, "\n")
		kept := lines[:0]
		for _, line := range lines {
			disabled := false
			for _, name := range s.DisabledTools {
				if strings.HasPrefix(line, "- "+name+":") {
					disabled = true
					break
				}
			}
			if !disabled {
				kept = append(kept, line)
			}
		}
		text = strings.Join(kept, "\n")
	}
	for _, tool := range a.loadCustomTools() {
		if (tool.Type == "shell" || tool.Type == "javascript") && !code {
			continue
		}
		if s.toolDisabled(tool.Name) {
			continue
		}
		hint := tool.ArgHint
		if hint == "" {
			hint = "sesuai deskripsi"
		}
		extra := ""
		if tool.Type == "http_post" {
			extra = " Memerlukan persetujuan pemilik."
		}
		text += fmt.Sprintf("\n- %s: %s. Argumen: %s.%s", tool.Name, tool.Description, hint, extra)
	}
	return text
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
func truncateRunes(s string, max int) string {
	runes := []rune(s)
	if len(runes) > max {
		return string(runes[:max]) + "…"
	}
	return s
}

func toolResultOK(result string) bool {
	return !strings.HasPrefix(result, "Error") && !strings.HasPrefix(result, "Gagal") &&
		!strings.Contains(result, "dinonaktifkan untuk kursi ini")
}

func (a *App) chatWithTools(ctx context.Context, cfg settings, s seat, history []gatewayMessage, tools, code, stream bool, onActivity func(toolName string), onDelta func(partial string)) (workerReply, error) {
	// Tool-calling native (fitur 10): untuk protokol openai, kirim
	// tools[] dan parse tool_calls. Bila provider menolak, jatuh ke
	// protokol teks di bawah. Jalur native non-streaming, jadi hanya
	// dipakai bila streaming tidak diminta.
	if tools && !stream && protocolOf(cfg) == protoOpenAI && a.nativeToolsEnabled() {
		if reply, ok, err := a.chatWithNativeTools(ctx, cfg, s, history, code, onActivity, onDelta); ok {
			return reply, err
		}
	}
	chatFn := a.chat
	if stream {
		chatFn = func(ctx context.Context, cfg settings, s seat, history []gatewayMessage) (string, error) {
			return a.chatStream(ctx, cfg, s, history, onDelta)
		}
	}
	text, err := chatFn(ctx, cfg, s, history)
	if err != nil {
		return workerReply{}, err
	}
	if !tools {
		return workerReply{Text: text}, nil
	}
	var used []string
	var calls []toolCallRecord
	var imageURL string
	for round := 0; round < maxToolRounds; round++ {
		name, arg, found := parseToolBlock(text)
		if !found {
			break
		}
		if onActivity != nil {
			onActivity(name)
		}
		result, image := a.runTool(ctx, cfg, s, name, arg, code)
		if image != "" {
			imageURL = image
		}
		used = append(used, name)
		calls = append(calls, toolCallRecord{
			Name:   name,
			Arg:    truncateRunes(arg, 300),
			Result: truncateRunes(result, 600),
			OK:     toolResultOK(result),
		})
		history = append(history,
			gatewayMessage{"assistant", text},
			gatewayMessage{"user", fmt.Sprintf("Hasil tool %s:\n%s\n\nLanjutkan jawabanmu untuk pengguna berdasarkan hasil di atas.", name, result)},
		)
		next, err := chatFn(ctx, cfg, s, history)
		if err != nil {
			break
		}
		text = next
	}
	return workerReply{Text: stripToolBlocks(text), ImageURL: imageURL, ToolsUsed: used, ToolCalls: calls}, nil
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

// runTool menjalankan satu tool dan mencatatnya ke log audit tool;
// nilai kedua adalah URL gambar bila tool menghasilkan gambar
// (generate_image), selain itu kosong.
func (a *App) runTool(ctx context.Context, cfg settings, s seat, name, arg string, code bool) (string, string) {
	start := time.Now()
	result, image := a.runToolInner(ctx, cfg, s, name, arg, code)
	a.recordToolLog(s, name, arg, result, time.Since(start))
	return result, image
}

const maxToolLogEntries = 500

// recordToolLog menambahkan satu entri audit pemakaian tool (maks 500,
// FIFO). Seperti recordStat, penyimpanan ke disk terjadi pada save
// berikutnya.
func (a *App) recordToolLog(s seat, name, arg, result string, duration time.Duration) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.data.ToolLog = append(a.data.ToolLog, toolLogEntry{
		Time: now(), SeatCode: s.Code, SeatName: s.Name, Tool: name,
		Arg: truncateRunes(arg, 200), OK: toolResultOK(result),
		DurationMs: duration.Milliseconds(),
	})
	if len(a.data.ToolLog) > maxToolLogEntries {
		a.data.ToolLog = a.data.ToolLog[len(a.data.ToolLog)-maxToolLogEntries:]
	}
}

func (a *App) runToolInner(ctx context.Context, cfg settings, s seat, name, arg string, code bool) (string, string) {
	if s.toolDisabled(name) {
		return fmt.Sprintf("Tool %s dinonaktifkan untuk kursi ini oleh pemilik.", name), ""
	}
	switch name {
	case "web_search":
		return a.toolWebSearch(ctx, arg), ""
	case "fetch_url":
		return a.toolFetchURL(ctx, arg), ""
	case "list_files":
		return a.toolListFiles(), ""
	case "read_file":
		return a.toolReadFile(arg), ""
	case "write_file":
		return a.toolWriteFile(arg), ""
	case "append_file":
		return a.toolAppendFile(arg), ""
	case "edit_file":
		return a.toolEditFile(arg), ""
	case "delete_file":
		return a.toolDeleteFile(ctx, s, arg), ""
	case "download_file":
		return a.toolDownloadFile(ctx, arg), ""
	case "zip_files":
		return a.toolZipFiles(arg), ""
	case "unzip_file":
		return a.toolUnzipFile(arg), ""
	case "file_hash":
		return a.toolFileHash(arg), ""
	case "image_info":
		return a.toolImageInfo(arg), ""
	case "resize_image":
		return a.toolResizeImage(arg), ""
	case "convert_image":
		return a.toolConvertImage(arg), ""
	case "pdf_text":
		return a.toolPDFText(ctx, arg), ""
	case "csv_json":
		return a.toolCSVJSON(arg), ""
	case "generate_image":
		return a.toolGenerateImage(ctx, cfg, s, arg)
	case "memory_save":
		return a.toolMemorySave(s, arg), ""
	case "current_time":
		return toolCurrentTime(), ""
	case "git":
		return a.toolGit(ctx, arg), ""
	case "screenshot_url":
		return a.toolScreenshotURL(ctx, arg)
	case "summarize_url":
		return a.toolSummarizeURL(ctx, arg), ""
	case "pdf_from_markdown":
		return a.toolPDFFromMarkdown(arg), ""
	case "translate_text":
		return a.toolTranslateText(ctx, arg), ""
	case "ocr_image":
		return a.toolOCRImage(ctx, arg), ""
	case "image_caption":
		return a.toolImageCaption(ctx, cfg, s, arg), ""
	case "qr_code":
		return a.toolQRCode(ctx, arg)
	case "rss_feed":
		return a.toolRSSFeed(ctx, arg), ""
	case "youtube_transcript":
		return a.toolYouTubeTranscript(ctx, arg), ""
	case "currency_convert":
		return a.toolCurrencyConvert(ctx, arg), ""
	case "text_to_speech":
		return a.toolTextToSpeech(ctx, arg), ""
	case "http_post":
		return a.toolHTTPPost(ctx, s, arg), ""
	case "password_gen":
		return toolPasswordGen(arg), ""
	case "run_code":
		if !code {
			return "Error: tool coding dimatikan di Pengaturan.", ""
		}
		return a.toolRunCode(ctx, s, arg), ""
	case "run_shell":
		if !code {
			return "Error: tool coding dimatikan di Pengaturan.", ""
		}
		return a.toolRunShell(ctx, s, arg), ""
	default:
		if tool, ok := a.findCustomTool(name); ok {
			return a.runCustomToolCfg(ctx, cfg, s, tool, arg, code), ""
		}
		return fmt.Sprintf("Tool %q tidak dikenal.", name), ""
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
// lookupIP dapat diganti dalam tes agar jalur sukses tool jaringan bisa
// diuji dengan server lokal tanpa melemahkan penjaga SSRF di produksi.
var lookupIP = func(ctx context.Context, host string) ([]net.IP, error) {
	return net.DefaultResolver.LookupIP(ctx, "ip", host)
}

func checkPublicURL(ctx context.Context, raw string) error {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return fmt.Errorf("URL harus http/https yang valid")
	}
	host := u.Hostname()
	if host == "" || strings.EqualFold(host, "localhost") {
		return fmt.Errorf("host tidak valid")
	}
	ips, err := lookupIP(ctx, host)
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
	return parseDDGResults(body)
}

// parseDDGResults mengubah HTML DuckDuckGo Lite menjadi daftar hasil
// bernomor (judul, cuplikan, URL tujuan setelah membuka redirect uddg).
func parseDDGResults(body string) string {
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

// approval adalah permintaan persetujuan pemilik untuk tindakan kursi
// yang berisiko: menjalankan kode berbahaya (kind "code"), perintah
// shell (kind "shell"), atau menghapus file (kind "delete"). Detail
// berisi kode/perintah/nama file yang akan dijalankan; Language hanya
// untuk kind "code".
type approval struct {
	ID        string `json:"id"`
	SeatCode  string `json:"seatCode"`
	SeatName  string `json:"seatName"`
	Kind      string `json:"kind"`
	Language  string `json:"language,omitempty"`
	Detail    string `json:"detail"`
	CreatedAt string `json:"createdAt"`
	done      chan bool
}

func (a *App) requestApproval(ctx context.Context, s seat, kind, language, detail string) bool {
	req := &approval{
		ID: uuid(), SeatCode: s.Code, SeatName: s.Name,
		Kind: kind, Language: language, Detail: detail, CreatedAt: now(),
		done: make(chan bool, 1),
	}
	a.approvalMu.Lock()
	if a.approvals == nil {
		a.approvals = map[string]*approval{}
	}
	a.approvals[req.ID] = req
	a.approvalMu.Unlock()
	defer func() {
		a.approvalMu.Lock()
		delete(a.approvals, req.ID)
		a.approvalMu.Unlock()
	}()
	select {
	case approved := <-req.done:
		return approved
	case <-time.After(approvalTimeout):
		return false
	case <-ctx.Done():
		return false
	}
}

func (a *App) pendingApprovals() []approval {
	a.approvalMu.Lock()
	defer a.approvalMu.Unlock()
	out := make([]approval, 0, len(a.approvals))
	for _, req := range a.approvals {
		out = append(out, *req)
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
		if !a.requestApproval(ctx, s, "code", language, code) {
			return fmt.Sprintf("Kode TIDAK dijalankan: terdeteksi pola berbahaya (%q) dan pemilik menolak atau tidak menyetujui dalam %d detik. Tulis ulang kode yang lebih aman atau jelaskan kebutuhanmu.", match, int(approvalTimeout.Seconds()))
		}
	}
	return a.executeCode(ctx, interpreter, ext, code)
}

// executeCode menjalankan kode di sandbox bubblewrap sebagai user nobody:
// filesystem di luar folder kerja tidak terlihat, tanpa jaringan, dengan
// batas CPU/memori/proses lewat cgroup systemd-run --scope.
func (a *App) executeCode(ctx context.Context, interpreter, ext, code string) string {
	return a.executeCodeStdin(ctx, interpreter, ext, code, "")
}

// executeCodeStdin seperti executeCode, tetapi memberi stdin ke proses
// (dipakai tool kustom javascript, fitur 23).
func (a *App) executeCodeStdin(ctx context.Context, interpreter, ext, code, stdin string) string {
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
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	out, err := cmd.CombinedOutput()
	if err != nil && strings.Contains(string(out), "Failed to") {
		// systemd-run tidak tersedia di lingkungan ini: jalankan bwrap langsung.
		cmd = exec.CommandContext(runCtx, "bwrap", bwrapArgs...)
		if stdin != "" {
			cmd.Stdin = strings.NewReader(stdin)
		}
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

// --- Tool tambahan: edit/append/delete/download file, shell, gambar, memori ---

func (a *App) toolAppendFile(arg string) string {
	name, content, _ := strings.Cut(arg, "\n")
	full, err := a.sandboxPath(name)
	if err != nil {
		return "Error: " + err.Error()
	}
	if err := os.MkdirAll(a.sandboxDir(), 0o755); err != nil {
		return "Error: gagal membuat folder kerja."
	}
	existing, _ := os.ReadFile(full)
	if len(existing)+len(content) > maxSandboxFileBytes {
		return "Error: isi file akan melebihi batas 100 KB."
	}
	file, err := os.OpenFile(full, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return "Error: gagal membuka file."
	}
	defer file.Close()
	if _, err := file.WriteString(content); err != nil {
		return "Error: gagal menulis file."
	}
	return fmt.Sprintf("Teks ditambahkan ke %s (sekarang %d byte).", filepath.Base(full), len(existing)+len(content))
}

func (a *App) toolEditFile(arg string) string {
	name, rest, found := strings.Cut(arg, "\n")
	if !found {
		return "Error: format edit_file adalah baris pertama nama file, lalu <<<OLD>>>, teks lama, <<<NEW>>>, teks baru."
	}
	full, err := a.sandboxPath(name)
	if err != nil {
		return "Error: " + err.Error()
	}
	oldPart, newPart, found := strings.Cut(rest, "<<<NEW>>>")
	if !found {
		return "Error: penanda <<<NEW>>> tidak ditemukan."
	}
	oldText := strings.TrimPrefix(strings.TrimSpace(oldPart), "<<<OLD>>>")
	oldText = strings.Trim(oldText, "\n")
	newText := strings.Trim(newPart, "\n")
	if oldText == "" {
		return "Error: teks lama kosong."
	}
	raw, err := os.ReadFile(full)
	if err != nil {
		return "Error: file tidak ditemukan."
	}
	content := string(raw)
	if !strings.Contains(content, oldText) {
		return "Error: teks lama tidak ditemukan di file; baca dulu isi file dengan read_file."
	}
	updated := strings.Replace(content, oldText, newText, 1)
	if len(updated) > maxSandboxFileBytes {
		return "Error: isi file akan melebihi batas 100 KB."
	}
	if err := os.WriteFile(full, []byte(updated), 0o644); err != nil {
		return "Error: gagal menulis file."
	}
	return fmt.Sprintf("File %s diubah (%d byte).", filepath.Base(full), len(updated))
}

func (a *App) toolDeleteFile(ctx context.Context, s seat, arg string) string {
	full, err := a.sandboxPath(arg)
	if err != nil {
		return "Error: " + err.Error()
	}
	if _, err := os.Stat(full); err != nil {
		return "Error: file tidak ditemukan."
	}
	if !a.requestApproval(ctx, s, "delete", "", filepath.Base(full)) {
		return fmt.Sprintf("File TIDAK dihapus: pemilik menolak atau tidak menyetujui dalam %d detik.", int(approvalTimeout.Seconds()))
	}
	if err := os.Remove(full); err != nil {
		return "Error: gagal menghapus file."
	}
	return fmt.Sprintf("File %s terhapus.", filepath.Base(full))
}

const maxDownloadBytes = 10 * 1024 * 1024

func (a *App) toolDownloadFile(ctx context.Context, arg string) string {
	rawURL, name, _ := strings.Cut(arg, "\n")
	rawURL = strings.TrimSpace(rawURL)
	name = strings.TrimSpace(name)
	if err := checkPublicURL(ctx, rawURL); err != nil {
		return "Error: " + err.Error()
	}
	if name == "" {
		parsed, err := url.Parse(rawURL)
		if err != nil {
			return "Error: URL tidak valid."
		}
		name = path.Base(parsed.Path)
	}
	return a.downloadToSandbox(ctx, rawURL, name)
}

func (a *App) downloadToSandbox(ctx context.Context, rawURL, name string) string {
	full, err := a.sandboxPath(name)
	if err != nil {
		return "Error: nama file tujuan tidak valid; tentukan nama file di baris kedua."
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "Error: URL tidak valid."
	}
	req.Header.Set("User-Agent", browserUserAgent)
	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "Error: unduhan gagal: " + err.Error()
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Sprintf("Error: unduhan gagal (HTTP %d).", resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxDownloadBytes+1))
	if err != nil {
		return "Error: unduhan gagal: " + err.Error()
	}
	if len(raw) > maxDownloadBytes {
		return "Error: file melebihi batas 10 MB."
	}
	if err := os.MkdirAll(a.sandboxDir(), 0o755); err != nil {
		return "Error: gagal membuat folder kerja."
	}
	if err := os.WriteFile(full, raw, 0o644); err != nil {
		return "Error: gagal menyimpan file."
	}
	return fmt.Sprintf("File %s terunduh (%d byte).", filepath.Base(full), len(raw))
}

func (a *App) toolRunShell(ctx context.Context, s seat, arg string) string {
	command := strings.TrimSpace(arg)
	if command == "" {
		return "Error: perintah shell kosong."
	}
	if len(command) > maxSandboxFileBytes {
		return "Error: perintah terlalu besar (maks 100 KB)."
	}
	if !a.requestApproval(ctx, s, "shell", "", command) {
		return fmt.Sprintf("Perintah TIDAK dijalankan: pemilik menolak atau tidak menyetujui dalam %d detik.", int(approvalTimeout.Seconds()))
	}
	return a.executeCode(ctx, "sh", "sh", command+"\n")
}

func (a *App) toolGenerateImage(ctx context.Context, cfg settings, s seat, arg string) (string, string) {
	prompt := strings.TrimSpace(arg)
	if prompt == "" {
		return "Error: deskripsi gambar kosong.", ""
	}
	image, err := a.generateImage(ctx, cfg, s, prompt)
	if err != nil {
		return "Error: pembuatan gambar gagal: " + err.Error(), ""
	}
	return "Gambar berhasil dibuat dan terlampir pada balasanmu. Sampaikan kepada pengguna bahwa gambarnya sudah jadi.", image
}

const maxSeatMemoryChars = 4000

func (a *App) toolMemorySave(s seat, arg string) string {
	note := strings.TrimSpace(arg)
	if note == "" {
		return "Error: catatan kosong."
	}
	if len(note) > 500 {
		note = note[:500]
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	seat := a.data.findSeat(s.Code)
	if seat == nil {
		return "Error: kursi tidak ditemukan."
	}
	entry := "- " + note
	if seat.Memory != "" {
		seat.Memory += "\n"
	}
	seat.Memory += entry
	// Bila melebihi batas, buang catatan tertua (baris teratas).
	for len(seat.Memory) > maxSeatMemoryChars {
		_, rest, found := strings.Cut(seat.Memory, "\n")
		if !found {
			seat.Memory = seat.Memory[len(seat.Memory)-maxSeatMemoryChars:]
			break
		}
		seat.Memory = rest
	}
	if err := a.saveLocked(); err != nil {
		return "Error: gagal menyimpan memori."
	}
	return "Catatan tersimpan ke memorimu."
}
