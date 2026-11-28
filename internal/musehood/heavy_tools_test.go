package musehood

// Tes berat sapu bersih 111 tool bawaan (28 Nov 2026): tiap tool dipanggil
// sekali dengan argumen valid minimal dan sekali dengan argumen invalid.
// Tool jaringan dimock lewat toolHTTPClient (RoundTripper palsu) +
// lookupIP yang mengembalikan IP publik, tanpa melemahkan penjaga SSRF.

import (
	"context"
	"encoding/base64"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type fakeTransport struct{}

func (fakeTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	u := req.URL.String()
	body := ""
	ctype := "text/html; charset=utf-8"
	switch {
	case strings.Contains(u, "duckduckgo"):
		body = `<a class="result-link" href="//duckduckgo.com/l/?uddg=https%3A%2F%2Fexample.com%2Fberita">Berita Contoh</a><td class="result-snippet">Cuplikan berita contoh.</td>`
	case strings.Contains(u, "mymemory") || strings.Contains(u, "translated"):
		body = `{"responseData":{"translatedText":"Hello world"}}`
		ctype = "application/json"
	case strings.Contains(u, "frankfurter"):
		body = `{"amount":1,"base":"USD","date":"2026-11-27","rates":{"IDR":16000}}`
		ctype = "application/json"
	case strings.Contains(u, "qrserver"):
		raw, _ := base64.StdEncoding.DecodeString(pixel)
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"image/png"}},
			Body: io.NopCloser(strings.NewReader(string(raw))), Request: req}, nil
	case strings.Contains(u, "youtube") && strings.Contains(u, "watch"):
		body = `<script>var ytInitialPlayerResponse = {"captions":{"playerCaptionsTracklistRenderer":{"captionTracks":[{"baseUrl":"https://www.youtube.com/api/timedtext?v=abc123&lang=id","languageCode":"id"}]}}};</script>`
	case strings.Contains(u, "timedtext"):
		body = `<?xml version="1.0"?><transcript><text start="0" dur="2">Halo dunia</text><text start="2" dur="2">ini transkrip</text></transcript>`
		ctype = "text/xml"
	case strings.Contains(u, "feed") || strings.Contains(u, "rss"):
		body = `<?xml version="1.0"?><rss version="2.0"><channel><title>Feed Uji</title><item><title>Item Satu</title><link>https://example.com/1</link><description>Isi satu</description></item><item><title>Item Dua</title><link>https://example.com/2</link><description>Isi dua</description></item></channel></rss>`
		ctype = "application/rss+xml"
	default:
		body = `<html><head><title>Halaman Uji</title></head><body><h1>Judul Uji</h1><p>Isi halaman uji untuk tool jaringan.</p></body></html>`
	}
	return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {ctype}},
		Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
}

func TestHeavyAllBuiltinTools(t *testing.T) {
	f := setup(t, nil)
	oldClient := toolHTTPClient
	toolHTTPClient = &http.Client{Transport: fakeTransport{}, Timeout: 10 * time.Second}
	defer func() { toolHTTPClient = oldClient }()
	oldDownload := downloadHTTPClient
	downloadHTTPClient = &http.Client{Transport: fakeTransport{}, Timeout: 10 * time.Second}
	defer func() { downloadHTTPClient = oldDownload }()
	oldLookup := lookupIP
	lookupIP = func(ctx context.Context, host string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("93.184.216.34")}, nil
	}
	defer func() { lookupIP = oldLookup }()
	oldApproval := approvalTimeout
	approvalTimeout = 150 * time.Millisecond
	defer func() { approvalTimeout = oldApproval }()

	ctx := context.WithValue(context.Background(), toolRunInfoKey{}, toolRunInfo{RoomID: defaultRoomID})
	model := "plain"
	s := seat{Code: "M01", Name: "Plamo", Model: &model}
	// generate_image memakai peran kursi Visual (M03) dari data fixture.
	f.app.mu.Lock()
	var visualSeat seat
	for _, st := range f.app.data.Seats {
		if st.Code == "M03" {
			visualSeat = st
		}
	}
	f.app.mu.Unlock()
	run := func(name, arg string) string {
		f.app.mu.Lock()
		cfg := f.app.data.Settings
		f.app.mu.Unlock()
		useSeat := s
		if name == "generate_image" {
			useSeat = visualSeat
		}
		out, _ := f.app.runTool(ctx, cfg, useSeat, name, arg, true)
		return out
	}

	// Persiapan berkas sandbox: teks, CSV, gambar PNG.
	run("write_file", "catatan.txt\nisi catatan uji")
	// Proyek Go terpisah yang terkompilasi untuk kasus debug (proyek
	// uji-proyek punya main ganda dari kasus copy_file).
	run("create_project", "uji-debug\ngo")
	// Satu pesan di ruang bawaan agar checkpoint punya titik acuan.
	f.app.mu.Lock()
	f.app.data.Messages = append(f.app.data.Messages, &message{ID: uuid(), CreatedAt: now(), AuthorType: "owner", Text: "pesan awal sweep", Status: "completed", RoomID: defaultRoomID, BranchID: "main"})
	f.app.mu.Unlock()
	run("write_file", "data.csv\nnama,umur\nBudi,30\nSari,25")
	sandbox := f.app.sandboxDir()
	_ = os.MkdirAll(sandbox, 0700)
	pngRaw, _ := base64.StdEncoding.DecodeString(pixel)
	if err := os.WriteFile(filepath.Join(sandbox, "gambar.png"), pngRaw, 0600); err != nil {
		t.Fatal(err)
	}

	type toolCase struct {
		name      string
		valid     string
		wantValid string // substring yang harus muncul pada hasil valid
		invalid   string
		wantInval string // substring yang harus muncul pada hasil invalid
	}
	cases := []toolCase{
		{"web_search", "berita hari ini", "Berita Contoh", "", "Error"},
		{"fetch_url", "https://example.com/halaman", "Judul Uji", "bukan-url", "Error"},
		{"list_files", "", "catatan.txt", "", ""}, // list_files mengabaikan argumen
		{"read_file", "catatan.txt", "isi catatan uji", "tidak-ada.txt", "Error"},
		{"write_file", "baru.txt\nisi baru", "baru.txt", "../x.txt\nisi", "Error"},
		{"append_file", "catatan.txt\ntambahan", "catatan.txt", "../x.txt\nisi", "Error"},
		{"edit_file", "catatan.txt\n<<<OLD>>>\nisi catatan uji\n<<<NEW>>>\nisi diganti", "catatan.txt", "format-salah", "Error"},
		{"delete_file", "baru.txt", "TIDAK dihapus", "tidak-ada.txt", "Error"},
		{"download_file", "https://example.com/file.txt\nunduhan.txt", "unduhan.txt", "bukan-url\nx.txt", "Error"},
		{"zip_files", "arsip.zip\ncatatan.txt", "arsip.zip", "x.zip\ntidak-ada.txt", "Error"},
		{"unzip_file", "arsip.zip", "Diekstrak", "tidak-ada.zip", "Error"},
		{"file_hash", "catatan.txt", "SHA-256", "tidak-ada.txt", "Error"},
		{"image_info", "gambar.png", "1", "catatan.txt", "Error"},
		{"resize_image", "gambar.png\n8", "gambar", "catatan.txt\n8", "Error"},
		{"convert_image", "gambar.png\njpg", "gambar", "catatan.txt\njpg", "Error"},
		{"pdf_text", "tidak-ada.pdf", "Error", "catatan.txt", "Error"},
		{"csv_json", "data.csv\ncsv2json", "Budi", "data.csv\narah-salah", "Error"},
		{"generate_image", "Peran: Visual. kucing lucu", "Gambar", "", "Error"},
		{"memory_save", "fakta penting uji", "memori", "", "Error"},
		{"current_time", "", "2026", "", "2026"},
		{"run_code", "python\nprint('halo sandbox')", "halo sandbox", "python\nimport os; os.system('rm -rf /')", "TIDAK dijalankan"},
		{"run_shell", "echo halo", "TIDAK dijalankan", "", "Error"},
		{"git", "status --short", "", "push origin main", "Error"},
		{"screenshot_url", "https://example.com", "", "bukan-url", "Error"},
		{"pdf_from_markdown", "# Judul\n\nIsi dokumen.", "pdf", "", "Error"},
		{"summarize_url", "https://example.com/artikel", "Judul Uji", "bukan-url", "Error"},
		{"translate_text", "id>en\nhalo dunia", "Hello world", "format-salah", "Error"},
		{"ocr_image", "gambar.png", "", "catatan.txt", "Error"},
		{"image_caption", "gambar.png\nApa isi gambar ini?", "", "tidak-ada.png", "Error"},
		{"qr_code", "https://example.com", "QR", "", "Error"},
		{"rss_feed", "https://example.com/feed.xml", "Item Satu", "bukan-url", "Error"},
		{"youtube_transcript", "https://www.youtube.com/watch?v=abc123XYZ09", "Halo dunia", "bukan video", "Error"},
		{"currency_convert", "100 USD IDR", "IDR", "abc", "Error"},
		{"text_to_speech", "halo dunia", "", "", "Error"},
		{"http_post", "https://example.com/api\n{\"halo\":\"dunia\"}", "TIDAK", "https://example.com/api\nbukan json", "Error"},
		{"password_gen", "16", "Password:", "abc", "Password:"}, // argumen non-angka jatuh ke panjang bawaan (perilaku longgar yang disengaja)
		{"create_project", "uji-proyek\ngo", "uji-proyek", "Nama Buruk!\ngo", "Error"},
		{"list_tree", "project:uji-proyek", "main.go", "project:tidak-ada", "kosong"},
		{"read_file_lines", "project:uji-proyek\nmain.go\n1\n3", "package", "project:uji-proyek\ntidak-ada.go\n1\n3", "Error"},
		{"move_file", "project:uji-proyek\nmain.go\nmain2.go", "main2.go", "project:uji-proyek\ntidak-ada.go\nx.go", "Error"},
		{"copy_file", "project:uji-proyek\nmain2.go\nsalinan.go", "salinan.go", "project:uji-proyek\ntidak-ada.go\nx.go", "Error"},
		{"mkdir", "project:uji-proyek\nfolder-baru", "folder-baru", "project:uji-proyek\n../keluar", "Error"},
		{"search_code", "project:uji-proyek\npackage", "main2.go", "project:uji-proyek\n[", "Error"},
		{"find_files", "project:uji-proyek\n**/*.go", ".go", "project:tidak-ada\n*.go", "Tidak ada file"},
		{"apply_patch", "project:uji-proyek\n--- a/main2.go\n+++ b/main2.go\n@@ -1,3 +1,3 @@\n package main\n \n-func main() {}\n+func main() { println(\"x\") }", "", "project:uji-proyek\npatch rusak", "Error"},
		{"edit_many", "project:uji-proyek\nmain2.go\n<<<OLD>>>\npackage main\n<<<NEW>>>\npackage main // diubah", "", "project:uji-proyek\nformat-salah", "Error"},
		{"project_snapshot", "project:uji-proyek\nlist", "", "project:tidak-ada\nlist", "Belum ada snapshot"},
		{"run_build", "project:uji-proyek", "", "project:tidak-ada", "Error"},
		{"run_tests", "project:uji-proyek", "", "project:tidak-ada", "Error"},
		{"run_lint", "project:uji-proyek", "", "project:tidak-ada", "Error"},
		{"format_code", "project:uji-proyek", "", "project:tidak-ada", "Error"},
		{"run_server", "project:uji-proyek\nstatus", "", "project:tidak-ada\nstatus", "Tidak ada server"},
		{"allowlist_shell", "project:uji-proyek\npwd", "", "project:uji-proyek\nrm -rf /", "Error"},
		{"git_commit", "project:uji-proyek\npesan commit", "TIDAK", "project:tidak-ada\nx", "Tidak ada perubahan"},
		{"git_branch", "project:uji-proyek\nlist", "", "project:tidak-ada\nlist", "Belum ada branch"},
		{"git_stash", "project:uji-proyek\nlist", "", "project:tidak-ada\nlist", ""},
		{"outline", "project:uji-proyek\nmain2.go", "main", "project:uji-proyek\ntidak-ada.go", "Error"},
		{"find_references", "project:uji-proyek\nmain", "main2.go", "project:uji-proyek\n", "Error"},
		{"dependency_graph", "project:uji-proyek", "", "project:tidak-ada", "Tidak ada dependensi"},
		{"todo_scan", "project:uji-proyek", "", "project:tidak-ada", ""},
		{"complexity_report", "project:uji-proyek", "", "project:tidak-ada", ""},
		{"generate_tests", "project:uji-proyek\nmain2.go", "_test.go", "project:uji-proyek\ntidak-ada.go", "Error"},
		{"export_project", "project:uji-proyek", ".zip", "project:tidak-ada", ".zip"}, // proyek tak ada = ekspor kosong (longgar)
		{"import_project", "tidak-ada.zip", "Error", "", "Error"},
		{"env_file", "project:uji-proyek\nlist", "", "project:uji-proyek\nperintah-aneh", "Error"},
		{"ask_question", "Pilih warna?\nMerah\nBiru", "tidak menjawab", "", "Error"}, // approvalTimeout 150ms di tes ini
		{"spawn_subagent", "seat:M01\nHalo subagent uji", "Sesi subagent dimulai", "", "Error"},
		{"check_subagent", "id-tidak-ada", "tidak ditemukan", "", "Error"},
		{"execute", "console.log(\"halo execute\")", "halo execute", "", "Error"},
		{"worktree_create", "project:uji-proyek\ncabang-a", "belum punya commit", "project:uji-proyek\nNama Buruk", "Error"},
		{"worktree_list", "project:uji-proyek", "Worktree proyek", "project:Nama Buruk", "Error"},
		{"worktree_remove", "project:uji-proyek\ntidak-ada", "tidak ditemukan", "project:uji-proyek\nNama Buruk", "Error"},
		{"browser_open", "https://example.com/halaman", "", "bukan-url", "Error"},
		{"browser_screenshot", "https://example.com", "", "bukan-url", "Error"},
		{"browser_links", "https://example.com/halaman", "", "bukan-url", "Error"},
		// Tool gaya omp (13 item, 28 Nov 2026).
		{"ast_grep", "project:uji-proyek\nfunc $NAME() { $$$ }\ngo", "main2.go", "project:uji-proyek\n", "Error"},
		{"ast_edit", "project:uji-proyek\nfunc main() { $$$ }\nfunc main() { println(\"y\") }\ngo", "Pratinjau", "project:uji-proyek\nsatu baris", "Error"},
		{"lsp_definition", "project:uji-proyek\nmain2.go\n5\n6", "Definisi", "project:uji-proyek\ntidak-ada.go\n1\n1", "Error"},
		{"lsp_references", "project:uji-proyek\nmain2.go\n5\n6", "", "project:uji-proyek\ntidak-ada.go\n1\n1", "Error"},
		{"lsp_diagnostics", "project:uji-proyek\nmain2.go", "", "project:uji-proyek\ntidak-ada.go", "Error"},
		{"lsp_rename", "project:uji-proyek\nmain_test.go\n5\n6\nTestUtama", "TIDAK diterapkan", "project:uji-proyek\nmain2.go\n3", "Error"},
		{"eval", "python\nx = 40 + 2\nprint(x)", "42", "ruby\nputs 1", "Error"},
		{"debug_launch", "project:uji-debug\n.", "Sesi debug", "project:tidak-ada\n.", "Error"},
		{"debug_breakpoint", "project:uji-debug\nmain.go\n6", "Breakpoint", "project:uji-debug\nmain.go\nabc", "Error"},
		{"debug_step", "continue", "", "lompat", "Error"},
		{"debug_inspect", "", "", "", ""},
		{"debug_stop", "", "", "", ""},
		{"browser_click", "https://example.com\nh1", "", "bukan-url\nh1", "Error"},
		{"browser_type", "https://example.com\ninput\nteks uji", "", "bukan-url\ninput\nx", "Error"},
		{"browser_snapshot", "https://example.com", "", "bukan-url", "Error"},
		{"browser_tabs", "list", "", "perintah-aneh", "Error"},
		{"github_read", "repo afman42/musehood-go", "", "repo format-salah", "Error"},
		{"github_search", "repos\nmusehood", "", "jenis-aneh\nx", "Error"},
		{"github_actions", "runs afman42/musehood-go", "", "runs format-salah", "Error"},
		{"github_push", "project:uji-proyek", "token", "project:uji-proyek", "token"},
		{"security_scan", "project:uji-proyek", "govulncheck", "project:tidak-ada", "Pola secret"},
		{"hub_message", "M02\nHalo dari sweep", "terkirim", "M99\nHalo", "Error"},
		{"hub_processes", "", "", "", ""},
		{"hub_stop", "browser", "", "aneh", "Error"},
		{"checkpoint", "titik sweep", "Checkpoint dibuat", "", ""},
		{"rewind", "titik sweep", "TIDAK dilakukan", "label-aneh", "Error"},
		{"memory_retain", "fakta sweep uji\ntag1", "tersimpan", "", "Error"},
		{"memory_recall", "uji", "", "", "Error"},
		{"memory_reflect", "sweep", "", "", "Error"},
		{"memory_edit", "delete id-aneh", "Error", "perintah-aneh", "Error"},
		{"learn", "Pelajaran sweep\nIsi pelajaran sweep.", "tersimpan", "satu baris", "Error"},
		{"manage_skill", "list", "", "perintah-aneh", "Error"},
		{"todo_add", "tugas sweep", "ditambahkan", "", "Error"},
		{"todo_update", "1\ndone", "done", "1\nstatus-aneh", "Error"},
		{"todo_list", "", "tugas sweep", "", ""},
		{"tool_info", "ast_grep", "ast_grep", "tool-aneh", "Error"},
	}

	if len(cases) != len(builtinToolNames) {
		t.Fatalf("kasus %d, tool bawaan %d — ada tool yang belum disapu", len(cases), len(builtinToolNames))
	}
	for _, c := range cases {
		if !builtinToolNames[c.name] {
			t.Fatalf("tool %s tidak dikenal", c.name)
		}
		out := run(c.name, c.valid)
		if out == "" {
			t.Fatalf("tool %s arg valid: hasil kosong", c.name)
		}
		if c.wantValid != "" && !strings.Contains(out, c.wantValid) {
			t.Fatalf("tool %s arg valid: mau mengandung %q, dapat %q", c.name, c.wantValid, out)
		}
		out = run(c.name, c.invalid)
		if out == "" {
			t.Fatalf("tool %s arg invalid: hasil kosong", c.name)
		}
		if c.wantInval != "" && !strings.Contains(out, c.wantInval) {
			t.Fatalf("tool %s arg invalid: mau mengandung %q, dapat %q", c.name, c.wantInval, out)
		}
		if strings.Contains(out, secret) {
			t.Fatalf("tool %s membocorkan secret", c.name)
		}
	}
}
