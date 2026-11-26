package musehood

// Tes unit untuk fungsi-fungsi dengan cakupan rendah: konversi HTML,
// tool web, ekstensi/resolusi gambar, parsing usage/Anthropic, ruang,
// dispatch tool, dan tipe error API.

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUnitHTMLToText(t *testing.T) {
	got := htmlToText(`<html><head><style>body{color:red}</style><script>alert(1)</script></head><body><h1>Judul</h1><p>Halo &amp; <b>dunia</b></p></body></html>`)
	if strings.Contains(got, "<") || strings.Contains(got, "alert") || strings.Contains(got, "color:red") {
		t.Fatalf("htmlToText bocor markup/script: %q", got)
	}
	if !strings.Contains(got, "Judul") || !strings.Contains(got, "Halo &") || !strings.Contains(got, "dunia") {
		t.Fatalf("htmlToText kehilangan isi: %q", got)
	}
	if got := htmlToText("teks polos"); got != "teks polos" {
		t.Fatalf("teks polos berubah: %q", got)
	}
	if got := htmlToText(""); got != "" {
		t.Fatalf("kosong: %q", got)
	}
}

func TestUnitToolCurrentTime(t *testing.T) {
	got := toolCurrentTime()
	// Format "Monday, 2 January 2006 15:04 MST" (WIB untuk Asia/Jakarta).
	if !strings.Contains(got, "WIB") && !strings.Contains(got, "UTC") {
		t.Fatalf("zona waktu tidak dikenal: %q", got)
	}
	if len(got) < 20 {
		t.Fatalf("format waktu terlalu pendek: %q", got)
	}
}

func TestUnitToolGet(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/gagal" {
			w.WriteHeader(500)
			return
		}
		_, _ = w.Write([]byte("isi halaman"))
	}))
	defer server.Close()
	body, err := toolGet(context.Background(), server.URL)
	if err != nil || body != "isi halaman" {
		t.Fatalf("toolGet = %q, %v", body, err)
	}
	if _, err := toolGet(context.Background(), server.URL+"/gagal"); err == nil {
		t.Fatal("toolGet 500 harus error")
	}
	if _, err := toolGet(context.Background(), "://rusak"); err == nil {
		t.Fatal("toolGet URL rusak harus error")
	}
}

func TestUnitToolWebToolsReject(t *testing.T) {
	app := &App{dir: t.TempDir()}
	if got := app.toolWebSearch(context.Background(), "   "); !strings.Contains(got, "kata kunci kosong") {
		t.Fatalf("web_search kosong: %s", got)
	}
	if got := app.toolFetchURL(context.Background(), "http://127.0.0.1/rahasia"); !strings.Contains(got, "Error") {
		t.Fatalf("fetch_url localhost harus ditolak: %s", got)
	}
	if got := app.toolFetchURL(context.Background(), "bukan-url"); !strings.Contains(got, "Error") {
		t.Fatalf("fetch_url rusak: %s", got)
	}
}

func TestUnitImageExtension(t *testing.T) {
	cases := []struct {
		raw  []byte
		want string
	}{
		{append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 16)...), "png"},
		{[]byte("\xff\xd8\xff\xe0"), "jpg"},
		{[]byte("GIF89a1234"), "gif"},
		{[]byte("GIF87a1234"), "gif"},
		{[]byte("RIFF1234WEBP1234"), "webp"},
		{[]byte("teks biasa"), ""},
		{[]byte("\x89PN"), ""},
		{nil, ""},
	}
	for _, tc := range cases {
		if got := imageExtension(tc.raw); got != tc.want {
			t.Fatalf("imageExtension(%q) = %q, mau %q", tc.raw, got, tc.want)
		}
	}
}

func TestUnitResolveImage(t *testing.T) {
	app := &App{dir: t.TempDir(), client: http.DefaultClient}
	ctx := context.Background()
	// File lokal tidak ada.
	if _, err := app.resolveImage(ctx, "/images/tidak-ada.png"); err == nil {
		t.Fatal("gambar lokal hilang harus error")
	}
	// File lokal sah.
	png, _ := base64.StdEncoding.DecodeString(pixel)
	if err := os.MkdirAll(filepath.Join(app.dir, "images"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(app.dir, "images", "uji.png"), png, 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := app.resolveImage(ctx, "/images/uji.png")
	if err != nil || got != "/images/uji.png" {
		t.Fatalf("resolve lokal = %q, %v", got, err)
	}
	// File lokal bukan gambar.
	if err := os.WriteFile(filepath.Join(app.dir, "images", "palsu.png"), []byte("bukan gambar"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := app.resolveImage(ctx, "/images/palsu.png"); err == nil {
		t.Fatal("gambar palsu harus error")
	}
	// Inline base64 sah -> tersimpan.
	got, err = app.resolveImage(ctx, "data:image/png;base64,"+pixel)
	if err != nil || !strings.HasPrefix(got, "/images/") {
		t.Fatalf("resolve inline = %q, %v", got, err)
	}
	// Inline base64 rusak.
	if _, err := app.resolveImage(ctx, "data:image/png;base64,%%%rusak%%%"); err == nil {
		t.Fatal("base64 rusak harus error")
	}
	// URL eksternal sah diunduh dan disimpan.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(png)
	}))
	defer server.Close()
	got, err = app.resolveImage(ctx, server.URL+"/gambar.png")
	if err != nil || !strings.HasPrefix(got, "/images/") {
		t.Fatalf("resolve URL = %q, %v", got, err)
	}
	// Nilai asing bukan URL.
	if _, err := app.resolveImage(ctx, "bukan url sama sekali"); err == nil {
		t.Fatal("nilai asing harus error")
	}
}

func TestUnitExtractUsage(t *testing.T) {
	cases := []struct {
		raw            string
		prompt, output int64
	}{
		{`{"usage":{"prompt_tokens":10,"completion_tokens":5}}`, 10, 5},
		{`{"usage":{"input_tokens":7,"output_tokens":3}}`, 7, 3},
		{`{"usageMetadata":{"promptTokenCount":11,"candidatesTokenCount":2}}`, 11, 2},
		{`{"tidak":"ada"}`, 0, 0},
		{`bukan json`, 0, 0},
	}
	for _, tc := range cases {
		prompt, output := extractUsage([]byte(tc.raw))
		if prompt != tc.prompt || output != tc.output {
			t.Fatalf("extractUsage(%s) = %d,%d mau %d,%d", tc.raw, prompt, output, tc.prompt, tc.output)
		}
	}
}

func TestUnitParseAnthropicText(t *testing.T) {
	// Body non-stream dengan blok teks.
	got, err := parseAnthropicText(`{"content":[{"type":"text","text":"Halo "},{"type":"text","text":"dunia"},{"type":"image","source":{}}]}`)
	if err != nil || got != "Halo dunia" {
		t.Fatalf("non-stream = %q, %v", got, err)
	}
	// Stream SSE delta.
	got, err = parseAnthropicText("data: {\"type\":\"content_block_delta\",\"delta\":{\"text\":\"Bagian \"}}\n\ndata: {\"type\":\"content_block_delta\",\"delta\":{\"text\":\"stream\"}}\n\n")
	if err != nil || got != "Bagian stream" {
		t.Fatalf("stream = %q, %v", got, err)
	}
	// Payload error provider.
	if _, err := parseAnthropicText(`{"error":{"message":"kredensial salah"}}`); err == nil {
		t.Fatal("payload error harus menghasilkan error")
	}
}

func TestUnitActiveRoomAndSeatCodes(t *testing.T) {
	d := data{
		Rooms:        []room{{ID: "r1", Name: "Satu"}, {ID: "r2", Name: "Dua", SeatCodes: []string{"M01"}}},
		ActiveRoomID: "r2",
		Seats:        []seat{{Code: "M01"}, {Code: "M02"}},
	}
	if r := d.activeRoom(); r == nil || r.ID != "r2" {
		t.Fatalf("activeRoom = %+v", r)
	}
	d.ActiveRoomID = "tidak-ada"
	if r := d.activeRoom(); r == nil || r.ID != "r1" {
		t.Fatalf("activeRoom fallback = %+v", r)
	}
	d.Rooms = nil
	if r := d.activeRoom(); r != nil {
		t.Fatalf("activeRoom kosong = %+v", r)
	}
	// SeatCodes kosong = semua kursi.
	if codes := d.roomSeatCodes(&room{}); len(codes) != 2 {
		t.Fatalf("semua kursi: %v", codes)
	}
	if codes := d.roomSeatCodes(&room{SeatCodes: []string{"M02"}}); len(codes) != 1 || codes[0] != "M02" {
		t.Fatalf("subset kursi: %v", codes)
	}
	if codes := d.roomSeatCodes(nil); len(codes) != 2 {
		t.Fatalf("ruang nil: %v", codes)
	}
}

func TestUnitRunToolDispatch(t *testing.T) {
	app := &App{dir: t.TempDir(), approvals: map[string]*approval{}}
	ctx := context.Background()
	if got, _ := app.runTool(ctx, settings{}, seat{Code: "M01"}, "current_time", "", false); got == "" || strings.Contains(got, "Error") {
		t.Fatalf("current_time: %s", got)
	}
	if got, _ := app.runTool(ctx, settings{}, seat{Code: "M01"}, "tool_ngawur", "x", false); !strings.Contains(got, "tidak dikenal") {
		t.Fatalf("tool asing: %s", got)
	}
	if got, _ := app.runTool(ctx, settings{}, seat{Code: "M01"}, "run_code", "python\nprint(1)", false); !strings.Contains(got, "dimatikan") {
		t.Fatalf("run_code tanpa toggle: %s", got)
	}
	if got, _ := app.runTool(ctx, settings{}, seat{Code: "M01"}, "run_shell", "echo x", false); !strings.Contains(got, "dimatikan") {
		t.Fatalf("run_shell tanpa toggle: %s", got)
	}
	if got, _ := app.runTool(ctx, settings{}, seat{Code: "M01"}, "list_files", "", false); strings.Contains(got, "Error") {
		t.Fatalf("list_files kosong: %s", got)
	}
}

func TestUnitAPIError(t *testing.T) {
	err := problem(418, "pesan uji")
	if err.Error() != "pesan uji" {
		t.Fatalf("Error() = %q", err.Error())
	}
	var apiErr *apiError
	if !errorAs(err, &apiErr) || apiErr.status != 418 {
		t.Fatalf("apiError: %+v", apiErr)
	}
}

func errorAs(err error, target **apiError) bool {
	for err != nil {
		if e, ok := err.(*apiError); ok {
			*target = e
			return true
		}
		type unwrapper interface{ Unwrap() error }
		u, ok := err.(unwrapper)
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}
