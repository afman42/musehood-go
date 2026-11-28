package musehood

import (
	"cmp"
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
	"slices"
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
- ask_question: berhenti dan bertanya kepada pemilik dengan pilihan jawaban; jawaban pemilik menjadi hasil tool. Baris pertama argumen: pertanyaan; baris berikutnya: pilihan jawaban (maks 6). Pemilik juga dapat menjawab bebas.
- spawn_subagent: mulai sesi anak di ruang baru untuk kursi lain (atau kursimu sendiri) dan lanjutkan pekerjaanmu; hasilnya dibaca dengan check_subagent. Baris pertama argumen (opsional): "seat:<kode kursi>"; baris berikutnya: prompt untuk sesi anak.
- check_subagent: baca status/hasil sesi anak. Argumen: id ruang atau id pekerjaan dari hasil spawn_subagent.
- browser_open: render halaman web dengan browser Chromium headless (termasuk konten JavaScript) dan kembalikan teksnya. Argumen: URL lengkap (http/https).
- browser_screenshot: ambil screenshot halaman ter-render; gambar terlampir pada balasanmu. Argumen: URL lengkap (http/https).
- browser_links: daftar tautan dari halaman ter-render. Argumen: URL lengkap (http/https).

Tool proyek coding bekerja pada proyek bernama. Awali argumen dengan baris "project:<nama>" untuk memilih proyek (tanpa itu dipakai proyek "default"). Path selalu relatif terhadap folder proyek.
- create_project: buat proyek baru. Baris 1: nama proyek; baris 2: template (go, node, python, web).
- list_tree: pohon folder proyek (kedalaman maks 6). Argumen: kosongkan.
- read_file_lines: baca rentang baris file. Baris 1: path; baris 2: baris awal; baris 3: baris akhir (maks 500 baris).
- move_file: pindah/ganti nama file (tidak menimpa). Baris 1: path sumber; baris 2: path tujuan.
- copy_file: salin file (tidak menimpa). Baris 1: path sumber; baris 2: path tujuan.
- mkdir: buat folder. Argumen: path folder.
- search_code: cari regex (RE2) lintas proyek dengan 2 baris konteks. Argumen: pola regex.
- find_files: cari file dengan pola glob, mis. "**/*.go". Argumen: pola glob.
- apply_patch: terapkan unified diff format git (snapshot otomatis dibuat dulu; gagal = tidak ada file berubah). Argumen: isi diff.
- edit_many: banyak edit sekaligus. Blok per file diawali "file:<path>", lalu pasangan <<<OLD>>>/<<<NEW>>> (snapshot otomatis dulu).
- project_snapshot: kelola snapshot proyek. Argumen: "create [label]", "list", "restore <id>" (persetujuan pemilik), atau "delete <id>".
- git_commit: commit semua perubahan proyek. Argumen: pesan commit. Memerlukan persetujuan pemilik.
- git_branch: kelola branch proyek. Argumen: "list", "create <nama>", atau "checkout <nama>".
- git_stash: stash proyek. Argumen: "push", "pop" (persetujuan pemilik), atau "list".
- outline: daftar fungsi/kelas dalam file (Go/Python/JS/TS). Argumen: path file.
- find_references: cari semua pemakaian simbol lintas proyek. Argumen: nama simbol.
- dependency_graph: peta import proyek (internal vs eksternal). Argumen: kosongkan.
- todo_scan: daftar TODO/FIXME/HACK/XXX proyek. Argumen: kosongkan.
- complexity_report: LOC per file dan fungsi terpanjang. Argumen: kosongkan.
- generate_tests: buat file tes kerangka untuk file sumber. Argumen: path file sumber.
- export_project: ekspor proyek sebagai zip yang bisa diunduh. Argumen: kosongkan.
- import_project: impor proyek dari zip di folder uploads. Argumen: nama file zip. Menimpa proyek lama memerlukan persetujuan pemilik.
- env_file: kelola file .env proyek. Argumen: "list", "get <KUNCI>" (nilai ter-mask), "set <KUNCI>=<nilai>", atau "delete <KUNCI>".
- worktree_create: buat git worktree proyek pada branch "wt-<nama>" di folder <proyek>-wt/<nama> (proyek harus sudah punya commit). Argumen: nama worktree.
- worktree_list: daftar worktree proyek. Argumen: kosongkan.
- worktree_remove: hapus worktree proyek. Argumen: nama worktree; tambahkan "force" untuk menghapus worktree kotor (persetujuan pemilik).
- ast_grep: cari pola sintaks (AST) lintas proyek dengan ast-grep, mis. "fmt.Println($$$)" atau "func $NAME() { $$$ }". Baris 1 argumen: pola; baris 2 (opsional): bahasa (go, python, javascript, typescript, rust, java, c, cpp, html, css, json, yaml); baris 3 (opsional): glob file.
- ast_edit: codemod struktural ast-grep. Baris 1: pola lama; baris 2: pola baru; baris 3 (opsional): bahasa; baris 4 (opsional): "apply" untuk menerapkan (tanpa apply hanya pratinjau diff; apply memerlukan persetujuan pemilik).
- lsp_definition: cari definisi simbol memakai language server. Baris 1: path file; baris 2: baris (1-based); baris 3: kolom (1-based).
- lsp_references: cari semua referensi simbol memakai language server. Baris 1: path file; baris 2: baris; baris 3: kolom.
- lsp_diagnostics: diagnostik (error/peringatan) file dari language server. Argumen: path file.
- lsp_rename: ganti nama simbol secara semantik memakai language server. Baris 1: path file; baris 2: baris; baris 3: kolom; baris 4: nama baru. Memerlukan persetujuan pemilik.
- browser_click: klik elemen halaman di browser interaktif (CDP). Baris 1: URL (buka tab baru bila belum ada); baris 2: selector CSS elemen.
- browser_type: ketik teks ke elemen halaman di browser interaktif. Baris 1: URL; baris 2: selector CSS; baris 3: teks yang diketik.
- browser_snapshot: ringkasan teks/DOM halaman aktif di browser interaktif. Argumen: URL (atau kosongkan untuk tab aktif).
- browser_tabs: kelola tab browser interaktif. Argumen: "list", "open <url>", "close <id>", atau "activate <id>".
- github_read: baca repo/file/issue/PR/diff GitHub. Argumen: "repo <owner>/<repo>", "file <owner>/<repo> <path> [ref]", "issue <owner>/<repo> <nomor>", "pr <owner>/<repo> <nomor>", atau "diff <owner>/<repo> <nomor-pr>".
- github_search: cari di GitHub. Baris 1: jenis (repos, code, issues); baris 2: kata kunci.
- github_actions: status GitHub Actions. Argumen: "runs <owner>/<repo>" atau "run <owner>/<repo> <id>".
- github_push: push proyek lokal ke remote GitHub-nya. Argumen: kosongkan (atau awalan project:<nama>). Memerlukan persetujuan pemilik dan token GitHub di Pengaturan.
- hub_message: kirim pesan ke kursi lain (sebagai pesan sistem di ruang ini). Baris 1: kode kursi tujuan; baris berikutnya: isi pesan.
- hub_processes: daftar proses latar yang hidup (dev server, sesi eval, sesi debug, browser). Argumen: kosongkan.
- hub_stop: hentikan proses latar. Argumen: id proses dari hub_processes (mis. "server:<proyek>", "eval:<kunci>", "debug:<id>", "browser").
- checkpoint: tandai titik balik percakapan ruang ini. Argumen (opsional): label checkpoint.
- rewind: kembali ke checkpoint (memotong pesan sesudahnya). Argumen: id atau label checkpoint. Memerlukan persetujuan pemilik.
- memory_recall: cari memori durable bersama. Argumen: kata kunci.
- memory_retain: simpan fakta/pelajaran ke memori durable bersama. Baris 1: teks; baris 2 (opsional): tag dipisah koma.
- memory_reflect: ambil memori terkait untuk kamu sintesis. Argumen: topik.
- memory_edit: ubah/hapus memori durable. Argumen: "delete <id>" atau "update <id>\n<teks baru>".
- learn: simpan pelajaran reusable; opsional jadikan skill. Baris 1: judul pelajaran; baris berikutnya: isi; awali baris pertama dengan "skill:<nama>" untuk juga membuat skill terkelola.
- manage_skill: kelola skill terkelola. Argumen: "list", "create <nama>\n<deskripsi>\n<isi>", "update <nama>\n<isi baru>", atau "retire <nama>".
- todo_add: tambah item checklist ruang. Argumen: teks item.
- todo_update: ubah status item checklist. Baris 1: id atau nomor item; baris 2: status (pending, active, done, blocked).
- todo_list: daftar checklist ruang ini. Argumen: kosongkan.
- tool_info: deskripsi lengkap satu tool. Argumen: nama tool.

Gunakan tool hanya bila benar-benar perlu (mis. butuh info terkini dari web atau isi file). Bila tidak perlu, jawab langsung tanpa blok tool.`

const codeToolInstruction = `
- run_code: jalankan kode di sandbox terisolasi (tanpa jaringan, maks 10 detik). Baris pertama argumen: bahasa (python atau javascript); baris berikutnya: kode. Hasil stdout/stderr dikembalikan kepadamu. Kode yang terdeteksi berbahaya memerlukan persetujuan pemilik sebelum dijalankan.
- run_shell: jalankan perintah shell sederhana di sandbox yang sama (mis. ls, cat, grep). Argumen: perintah shell. Selalu memerlukan persetujuan pemilik.
- run_build: build proyek di sandbox (go build / npm run build / compileall Python, maks 120 detik). Argumen: kosongkan (atau awalan project:<nama>).
- run_tests: jalankan tes proyek di sandbox (go test / npm test / pytest, maks 180 detik). Argumen: kosongkan (atau awalan project:<nama>).
- run_lint: lint proyek (go vet + gofmt -l, py_compile + ruff, eslint bila terpasang). Argumen: kosongkan (atau awalan project:<nama>).
- format_code: format kode proyek (gofmt, black/prettier bila tersedia). Argumen: kosongkan (atau awalan project:<nama>).
- run_server: jalankan dev server proyek sebagai proses latar di 127.0.0.1. Argumen: perintah server (mis. "go run .", "npm start", "python3 -m http.server"), atau "status"/"stop". Satu server per proyek.
- allowlist_shell: jalankan perintah aman tanpa persetujuan (ls, cat, head, tail, wc, pwd, echo, grep, find tanpa -exec, du, df, go/node/python3 --version, git status/log/diff) di folder proyek. Tanpa metakarakter shell.
- execute: jalankan JavaScript di sandbox yang sama (maks 30 detik) dengan fungsi tool(nama, argumen) untuk memanggil tool lain dari dalam kode (maks 20 panggilan; setiap panggilan melewati cek izin/persetujuan yang sama). Argumen: kode JavaScript; gunakan await tool("web_search", "...") dan console.log/return untuk hasil. Kode berbahaya memerlukan persetujuan pemilik.
- eval: kernel persisten Python/JavaScript di sandbox (state bertahan antar-panggilan untuk ruang+kursi+bahasa yang sama, maks 30 detik per panggilan). Baris pertama argumen: bahasa (python atau javascript); baris berikutnya: kode. Argumen khusus "reset" (setelah baris bahasa) mengosongkan state. Kode berbahaya memerlukan persetujuan pemilik.
- debug_launch: mulai sesi debug DAP (delve) untuk proyek Go. Baris 1: path program (mis. "." atau "./cmd/app"); baris 2 (opsional): argumen program.
- debug_breakpoint: pasang breakpoint pada sesi debug aktif. Baris 1: path file; baris 2: nomor baris.
- debug_step: lanjutkan eksekusi debug. Argumen: "continue", "next", "stepIn", atau "stepOut".
- debug_inspect: periksa stack trace + variabel sesi debug saat berhenti. Argumen: kosongkan.
- debug_stop: hentikan sesi debug aktif. Argumen: kosongkan.
- security_scan: pindai keamanan proyek (govulncheck/go vet untuk Go, npm audit untuk Node, pola secret untuk semua). Argumen: kosongkan (atau awalan project:<nama>).`

func (a *App) buildToolInstructions(code bool, s seat, mcpServers []mcpServerConfig, onDemand bool) string {
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
	if onDemand {
		text = onDemandToolText(text)
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
	text += a.mcpInstructionLines(s, mcpServers)
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
			Arg:    truncateRunes(redactToolArg(name, arg), 300),
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
		Arg: truncateRunes(redactToolArg(name, arg), 200), OK: toolResultOK(result),
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
	// Kebijakan per tool (fitur omp 13): deny menolak, prompt meminta
	// persetujuan pemilik (kind "tool") sebelum tool dijalankan.
	if blocked := a.enforceToolPolicy(ctx, cfg, s, name, arg); blocked != "" {
		return blocked, ""
	}
	// Plan mode (fitur omp 12): selama aktif di ruang ini, hanya tool
	// kelas Read yang boleh dipakai.
	if blocked := a.enforcePlanMode(ctx, name, arg); blocked != "" {
		return blocked, ""
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
	case "ask_question":
		return a.toolAskQuestion(ctx, s, arg), ""
	case "spawn_subagent":
		return a.toolSpawnSubagent(ctx, s, arg), ""
	case "check_subagent":
		return a.toolCheckSubagent(arg), ""
	case "browser_open":
		return a.toolBrowserOpen(ctx, arg), ""
	case "browser_screenshot":
		return a.toolBrowserScreenshot(ctx, arg)
	case "browser_links":
		return a.toolBrowserLinks(ctx, arg), ""
	case "execute":
		if !code {
			return "Error: tool coding dimatikan di Pengaturan.", ""
		}
		return a.toolExecute(ctx, cfg, s, arg, code), ""
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
	case "create_project":
		return a.toolCreateProject(arg), ""
	case "list_tree":
		return a.toolListTree(arg), ""
	case "read_file_lines":
		return a.toolReadFileLines(arg), ""
	case "move_file":
		return a.toolMoveFile(arg), ""
	case "copy_file":
		return a.toolCopyFile(arg), ""
	case "mkdir":
		return a.toolMkdir(arg), ""
	case "search_code":
		return a.toolSearchCode(arg), ""
	case "find_files":
		return a.toolFindFiles(arg), ""
	case "apply_patch":
		return a.toolApplyPatch(arg), ""
	case "edit_many":
		return a.toolEditMany(arg), ""
	case "project_snapshot":
		return a.toolProjectSnapshot(ctx, s, arg), ""
	case "run_build":
		if !code {
			return "Error: tool coding dimatikan di Pengaturan.", ""
		}
		return a.toolRunBuild(ctx, arg), ""
	case "run_tests":
		if !code {
			return "Error: tool coding dimatikan di Pengaturan.", ""
		}
		return a.toolRunTests(ctx, arg), ""
	case "run_lint":
		if !code {
			return "Error: tool coding dimatikan di Pengaturan.", ""
		}
		return a.toolRunLint(ctx, arg), ""
	case "format_code":
		if !code {
			return "Error: tool coding dimatikan di Pengaturan.", ""
		}
		return a.toolFormatCode(ctx, arg), ""
	case "run_server":
		if !code {
			return "Error: tool coding dimatikan di Pengaturan.", ""
		}
		return a.toolRunServer(ctx, arg), ""
	case "allowlist_shell":
		if !code {
			return "Error: tool coding dimatikan di Pengaturan.", ""
		}
		return a.toolAllowlistShell(ctx, arg), ""
	case "git_commit":
		return a.toolGitCommit(ctx, s, arg), ""
	case "git_branch":
		return a.toolGitBranch(ctx, s, arg), ""
	case "git_stash":
		return a.toolGitStash(ctx, s, arg), ""
	case "outline":
		return a.toolOutline(arg), ""
	case "find_references":
		return a.toolFindReferences(arg), ""
	case "dependency_graph":
		return a.toolDependencyGraph(arg), ""
	case "todo_scan":
		return a.toolTodoScan(arg), ""
	case "complexity_report":
		return a.toolComplexityReport(arg), ""
	case "generate_tests":
		return a.toolGenerateTests(arg), ""
	case "export_project":
		return a.toolExportProject(arg), ""
	case "import_project":
		return a.toolImportProject(ctx, s, arg), ""
	case "env_file":
		return a.toolEnvFile(arg), ""
	case "worktree_create":
		return a.toolWorktreeCreate(ctx, arg), ""
	case "worktree_list":
		return a.toolWorktreeList(ctx, arg), ""
	case "worktree_remove":
		return a.toolWorktreeRemove(ctx, s, arg), ""
	case "ast_grep":
		return a.toolASTGrep(ctx, arg), ""
	case "ast_edit":
		return a.toolASTEdit(ctx, s, arg), ""
	case "lsp_definition":
		return a.toolLSPDefinition(ctx, arg), ""
	case "lsp_references":
		return a.toolLSPReferences(ctx, arg), ""
	case "lsp_diagnostics":
		return a.toolLSPDiagnostics(ctx, arg), ""
	case "lsp_rename":
		return a.toolLSPRename(ctx, s, arg), ""
	case "eval":
		if !code {
			return "Error: tool coding dimatikan di Pengaturan.", ""
		}
		return a.toolEval(ctx, s, arg), ""
	case "debug_launch":
		if !code {
			return "Error: tool coding dimatikan di Pengaturan.", ""
		}
		return a.toolDebugLaunch(ctx, arg), ""
	case "debug_breakpoint":
		if !code {
			return "Error: tool coding dimatikan di Pengaturan.", ""
		}
		return a.toolDebugBreakpoint(ctx, arg), ""
	case "debug_step":
		if !code {
			return "Error: tool coding dimatikan di Pengaturan.", ""
		}
		return a.toolDebugStep(ctx, arg), ""
	case "debug_inspect":
		if !code {
			return "Error: tool coding dimatikan di Pengaturan.", ""
		}
		return a.toolDebugInspect(ctx, arg), ""
	case "debug_stop":
		if !code {
			return "Error: tool coding dimatikan di Pengaturan.", ""
		}
		return a.toolDebugStop(ctx, arg), ""
	case "browser_click":
		return a.toolBrowserClick(ctx, arg), ""
	case "browser_type":
		return a.toolBrowserType(ctx, arg), ""
	case "browser_snapshot":
		return a.toolBrowserSnapshot(ctx, arg), ""
	case "browser_tabs":
		return a.toolBrowserTabs(ctx, arg), ""
	case "github_read":
		return a.toolGitHubRead(ctx, cfg, arg), ""
	case "github_search":
		return a.toolGitHubSearch(ctx, cfg, arg), ""
	case "github_actions":
		return a.toolGitHubActions(ctx, cfg, arg), ""
	case "github_push":
		return a.toolGitHubPush(ctx, cfg, s, arg), ""
	case "security_scan":
		if !code {
			return "Error: tool coding dimatikan di Pengaturan.", ""
		}
		return a.toolSecurityScan(ctx, arg), ""
	case "hub_message":
		return a.toolHubMessage(ctx, s, arg), ""
	case "hub_processes":
		return a.toolHubProcesses(), ""
	case "hub_stop":
		return a.toolHubStop(arg), ""
	case "checkpoint":
		return a.toolCheckpoint(ctx, arg), ""
	case "rewind":
		return a.toolRewind(ctx, s, arg), ""
	case "memory_recall":
		return a.toolMemoryRecall(cfg, arg), ""
	case "memory_retain":
		return a.toolMemoryRetain(cfg, s, arg), ""
	case "memory_reflect":
		return a.toolMemoryReflect(cfg, arg), ""
	case "memory_edit":
		return a.toolMemoryEdit(cfg, arg), ""
	case "learn":
		return a.toolLearn(cfg, s, arg), ""
	case "manage_skill":
		return a.toolManageSkill(arg), ""
	case "todo_add":
		return a.toolTodoAdd(ctx, arg), ""
	case "todo_update":
		return a.toolTodoUpdate(ctx, arg), ""
	case "todo_list":
		return a.toolTodoList(ctx), ""
	case "tool_info":
		return a.toolToolInfo(cfg, s, arg, code), ""
	default:
		if strings.HasPrefix(name, "mcp_") {
			return a.runMCPTool(ctx, cfg, s, name, arg), ""
		}
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
	slices.Sort(lines)
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

var toolHTTPClient = &http.Client{Timeout: 25 * time.Second, CheckRedirect: checkRedirectPublic}

// downloadHTTPClient dipakai unduhan file (batas lebih longgar); var agar
// tes dapat menggantinya dengan transport palsu.
var downloadHTTPClient = &http.Client{Timeout: 60 * time.Second, CheckRedirect: checkRedirectPublic}

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

// checkRedirectPublic memvalidasi setiap hop redirect dengan penjaga
// SSRF yang sama seperti URL awal. Tanpa ini, URL publik yang
// me-redirect ke alamat privat (127.0.0.1, metadata cloud) lolos dari
// pemeriksaan awal karena klien HTTP mengikuti redirect secara diam-diam.
func checkRedirectPublic(req *http.Request, via []*http.Request) error {
	if len(via) >= 5 {
		return fmt.Errorf("terlalu banyak redirect")
	}
	return checkPublicURL(req.Context(), req.URL.String())
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

// approvalTimeout adalah batas menunggu persetujuan pemilik; var agar tes
// dapat memperpendeknya.
var approvalTimeout = 180 * time.Second

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
	slices.SortFunc(out, func(a, b approval) int { return cmp.Compare(a.CreatedAt, b.CreatedAt) })
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

// sandboxBwrapArgs menyusun argumen bubblewrap untuk menjalankan
// interpreter di folder kerja sandbox: filesystem di luar folder kerja
// tidak terlihat, tanpa jaringan, sebagai user nobody.
func sandboxBwrapArgs(dir, interpreter, name string, timeout time.Duration) []string {
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
		"timeout", fmt.Sprint(int(timeout.Seconds())), interpreter, name,
	}
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

	bwrapArgs := sandboxBwrapArgs(dir, interpreter, name, codeTimeout)
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
	resp, err := downloadHTTPClient.Do(req)
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
