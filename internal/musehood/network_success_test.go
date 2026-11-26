package musehood

// Tes jalur sukses tool jaringan (fetch_url, web_search, http_get
// kustom) dengan resolver DNS yang di-stub ke IP publik — server lokal
// httptest tetap dipakai untuk HTTP-nya, penjaga SSRF produksi tidak
// dilemahkan. Plus: restore >32MB ditolak, cancelQueueLocked, dan
// saveJobLocked.

import (
	"bytes"
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func stubLookup(t *testing.T, ip string) {
	t.Helper()
	orig := lookupIP
	lookupIP = func(ctx context.Context, host string) ([]net.IP, error) {
		return []net.IP{net.ParseIP(ip)}, nil
	}
	t.Cleanup(func() { lookupIP = orig })
}

func TestToolFetchURLSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><body><h1>Judul Halaman</h1><p>Isi <b>penting</b> di sini.</p><script>jahat()</script></body></html>`))
	}))
	defer server.Close()
	stubLookup(t, "93.184.216.34") // IP publik (example.com)
	app := &App{dir: t.TempDir()}
	got := app.toolFetchURL(context.Background(), server.URL)
	if !strings.Contains(got, "Judul Halaman") || !strings.Contains(got, "Isi") || !strings.Contains(got, "penting") {
		t.Fatalf("toolFetchURL sukses harus memuat teks halaman: %q", got)
	}
	if strings.Contains(got, "jahat") || strings.Contains(got, "<") {
		t.Fatalf("toolFetchURL bocor script/markup: %q", got)
	}
}

func TestToolFetchURLStubPrivateRejected(t *testing.T) {
	stubLookup(t, "10.0.0.5")
	app := &App{dir: t.TempDir()}
	got := app.toolFetchURL(context.Background(), "http://contoh.invalid/halaman")
	if !strings.Contains(got, "alamat privat") {
		t.Fatalf("IP privat hasil resolver harus ditolak: %q", got)
	}
}

func TestParseDDGResults(t *testing.T) {
	body := `<html><body><table>
<tr><td><a rel="nofollow" class="result-link" href="//duckduckgo.com/l/?uddg=https%3A%2F%2Fexample.com%2Fartikel&amp;rut=x">Artikel &amp; Contoh</a></td></tr>
<tr><td class="result-snippet">Cuplikan pertama tentang contoh.</td></tr>
<tr><td><a rel="nofollow" class="result-link" href="https://langsung.example/page">Halaman Langsung</a></td></tr>
<tr><td class="result-snippet">Cuplikan kedua.</td></tr>
</table></body></html>`
	got := parseDDGResults(body)
	for _, want := range []string{"1. Artikel & Contoh", "Cuplikan pertama tentang contoh.", "https://example.com/artikel", "2. Halaman Langsung", "https://langsung.example/page"} {
		if !strings.Contains(got, want) {
			t.Fatalf("parseDDGResults kehilangan %q dalam %q", want, got)
		}
	}
	if strings.Contains(got, "uddg=") {
		t.Fatalf("redirect uddg harus dibuka: %q", got)
	}
}

func TestParseDDGResultsFallback(t *testing.T) {
	if got := parseDDGResults("<html><body><p>teks tanpa hasil</p></body></html>"); !strings.Contains(got, "teks tanpa hasil") {
		t.Fatalf("tanpa link harus fallback ke teks: %q", got)
	}
	if got := parseDDGResults(""); got != "Tidak ada hasil." {
		t.Fatalf("body kosong: %q", got)
	}
}

func TestRunCustomToolHTTPGetSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("q") != "Pontianak" {
			w.WriteHeader(400)
			return
		}
		_, _ = w.Write([]byte("Cuaca Pontianak: hujan, 27C"))
	}))
	defer server.Close()
	stubLookup(t, "93.184.216.34")
	app := &App{dir: t.TempDir()}
	tool := customTool{Name: "cuaca", Type: "http_get", URL: server.URL + "/cuaca?q={{arg}}"}
	got := app.runCustomTool(context.Background(), seat{Code: "M01"}, tool, "Pontianak", false)
	if got != "Cuaca Pontianak: hujan, 27C" {
		t.Fatalf("http_get kustom sukses: %q", got)
	}
}

func TestRestoreOversizeRejected(t *testing.T) {
	f := setup(t, nil)
	big := bytes.Repeat([]byte("x"), 33*1024*1024)
	status, _ := rawRequest(t, f, "POST", "/api/restore", big, nil)
	if status != 400 {
		t.Fatalf("restore >32MB harus 400, dapat %d", status)
	}
	// Data lama utuh setelah penolakan.
	if st := f.state(); len(st["seats"].([]any)) != 7 {
		t.Fatalf("state rusak setelah restore ditolak: %v", st["seats"])
	}
}

func TestCancelQueueLocked(t *testing.T) {
	dir := t.TempDir()
	app := &App{dir: dir}
	id := "12345678-1234-1234-1234-123456789abc"
	for _, folder := range []string{"requests", "responses"} {
		full := filepath.Join(dir, "queue", folder)
		if err := os.MkdirAll(full, 0o755); err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{id + ".json", id + ".tmp", "bukan-id.txt"} {
			if err := os.WriteFile(filepath.Join(full, name), []byte("{}"), 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := app.cancelQueueLocked(); err != nil {
		t.Fatal(err)
	}
	for _, folder := range []string{"requests", "responses"} {
		full := filepath.Join(dir, "queue", folder)
		if _, err := os.Stat(filepath.Join(full, id+".json")); !os.IsNotExist(err) {
			t.Fatalf("%s: file antrean harus terhapus", folder)
		}
		if _, err := os.Stat(filepath.Join(full, "bukan-id.txt")); err != nil {
			t.Fatalf("%s: file non-antrean harus tetap: %v", folder, err)
		}
	}
	// Folder hilang -> error dikembalikan, bukan panic.
	missing := &App{dir: t.TempDir()}
	if err := missing.cancelQueueLocked(); err == nil {
		t.Fatal("cancelQueueLocked tanpa folder harus error")
	}
}

func TestSaveJobLocked(t *testing.T) {
	dir := t.TempDir()
	d, err := loadData(dir)
	if err != nil {
		t.Fatal(err)
	}
	app := &App{dir: dir, data: d}
	app.saveJobLocked()
	loaded, err := loadData(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Seats) != 7 {
		t.Fatalf("data tersimpan tidak utuh: %d kursi", len(loaded.Seats))
	}
}
