package musehood

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

// Tes kompresi gzip + header cache untuk aset frontend tertanam
// (percepatan akses pertama): gzip hanya untuk tipe kompresibel
// >1 KB tanpa Range, ETag untuk revalidasi 304, Cache-Control
// immutable untuk _app/immutable dan no-cache untuk nama tetap.

var bigJS = []byte("console.log('musehood');\n" + strings.Repeat("function f(){return 'halo dunia';}\n", 200))
var bigCSS = []byte(strings.Repeat(".kelas{color:#146b54;margin:0;padding:4px}\n", 100))
var bigPNG = append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte{0x7f}, 4096)...)
var testIndex = []byte("<!doctype html><html><head><style>.skel{color:red}</style></head><body>" + strings.Repeat("isi ", 400) + "</body></html>")

func staticApp(t *testing.T) *App {
	t.Helper()
	app, err := New(Options{
		DataDir: t.TempDir(),
		Frontend: fstest.MapFS{
			"index.html":                        {Data: testIndex},
			"_app/immutable/chunks/besar.js":    {Data: bigJS},
			"_app/immutable/chunks/kecil.js":    {Data: []byte("var a=1;")},
			"style.css":                         {Data: bigCSS},
			"icon-192.png":                      {Data: bigPNG},
			"sw.js":                             {Data: []byte(strings.Repeat("// sw\n", 300))},
			"manifest.webmanifest":              {Data: []byte(strings.Repeat(`{"name":"Musehood"}`, 100))},
			"_app/immutable/assets/gambar.webp": {Data: bigPNG},
		},
		PollInterval: 5 * time.Millisecond,
		MuseWait:     150 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(app.Close)
	return app
}

func staticGet(t *testing.T, app *App, method, path string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	req.Host = "localhost"
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	return rec
}

func gunzip(t *testing.T, b []byte) []byte {
	t.Helper()
	zr, err := gzip.NewReader(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	out, err := io.ReadAll(zr)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestStaticGzipCompression(t *testing.T) {
	app := staticApp(t)
	rec := staticGet(t, app, "GET", "/_app/immutable/chunks/besar.js", map[string]string{"Accept-Encoding": "gzip"})
	if rec.Code != 200 {
		t.Fatalf("status %d", rec.Code)
	}
	if got := rec.Header().Get("Content-Encoding"); got != "gzip" {
		t.Fatalf("Content-Encoding %q, mau gzip", got)
	}
	if got := rec.Header().Get("Vary"); !strings.Contains(got, "Accept-Encoding") {
		t.Fatalf("Vary %q tidak memuat Accept-Encoding", got)
	}
	body := rec.Body.Bytes()
	if len(body) >= len(bigJS) {
		t.Fatalf("hasil gzip %d byte tidak lebih kecil dari asli %d", len(body), len(bigJS))
	}
	if got := gunzip(t, body); !bytes.Equal(got, bigJS) {
		t.Fatal("isi hasil gunzip berbeda dari berkas asli")
	}
	if got := rec.Header().Get("Cache-Control"); got != "public, max-age=31536000, immutable" {
		t.Fatalf("Cache-Control immutable %q", got)
	}
	if rec.Header().Get("ETag") == "" {
		t.Fatal("ETag kosong")
	}
}

func TestStaticNoGzipWithoutAccept(t *testing.T) {
	app := staticApp(t)
	rec := staticGet(t, app, "GET", "/_app/immutable/chunks/besar.js", nil)
	if rec.Code != 200 {
		t.Fatalf("status %d", rec.Code)
	}
	if got := rec.Header().Get("Content-Encoding"); got != "" {
		t.Fatalf("Content-Encoding %q, mau kosong", got)
	}
	if !bytes.Equal(rec.Body.Bytes(), bigJS) {
		t.Fatal("isi tanpa kompresi berbeda dari berkas asli")
	}
}

func TestStaticGzipQZeroRejected(t *testing.T) {
	app := staticApp(t)
	rec := staticGet(t, app, "GET", "/style.css", map[string]string{"Accept-Encoding": "gzip;q=0, deflate"})
	if got := rec.Header().Get("Content-Encoding"); got != "" {
		t.Fatalf("gzip;q=0 tetap dikompresi: %q", got)
	}
}

func TestStaticSmallAndBinaryNotCompressed(t *testing.T) {
	app := staticApp(t)
	rec := staticGet(t, app, "GET", "/_app/immutable/chunks/kecil.js", map[string]string{"Accept-Encoding": "gzip"})
	if got := rec.Header().Get("Content-Encoding"); got != "" {
		t.Fatalf("berkas kecil dikompresi: %q", got)
	}
	rec = staticGet(t, app, "GET", "/icon-192.png", map[string]string{"Accept-Encoding": "gzip"})
	if got := rec.Header().Get("Content-Encoding"); got != "" {
		t.Fatalf("PNG dikompresi: %q", got)
	}
	rec = staticGet(t, app, "GET", "/_app/immutable/assets/gambar.webp", map[string]string{"Accept-Encoding": "gzip"})
	if got := rec.Header().Get("Content-Encoding"); got != "" {
		t.Fatalf("WebP dikompresi: %q", got)
	}
}

func TestStaticRangeNotCompressed(t *testing.T) {
	app := staticApp(t)
	rec := staticGet(t, app, "GET", "/_app/immutable/chunks/besar.js", map[string]string{
		"Accept-Encoding": "gzip",
		"Range":           "bytes=0-9",
	})
	if rec.Code != http.StatusPartialContent {
		t.Fatalf("status %d, mau 206", rec.Code)
	}
	if got := rec.Header().Get("Content-Encoding"); got != "" {
		t.Fatalf("respons Range dikompresi: %q", got)
	}
	if !bytes.Equal(rec.Body.Bytes(), bigJS[:10]) {
		t.Fatal("isi Range berbeda")
	}
}

func TestStaticETagRevalidate(t *testing.T) {
	app := staticApp(t)
	// Varian polos.
	rec := staticGet(t, app, "GET", "/style.css", nil)
	etag := rec.Header().Get("ETag")
	if etag == "" {
		t.Fatal("ETag polos kosong")
	}
	rec = staticGet(t, app, "GET", "/style.css", map[string]string{"If-None-Match": etag})
	if rec.Code != http.StatusNotModified {
		t.Fatalf("revalidasi polos status %d, mau 304", rec.Code)
	}
	// Varian gzip punya ETag sendiri dan juga bisa 304.
	rec = staticGet(t, app, "GET", "/style.css", map[string]string{"Accept-Encoding": "gzip"})
	gzETag := rec.Header().Get("ETag")
	if gzETag == "" || gzETag == etag {
		t.Fatalf("ETag gzip %q harus ada dan berbeda dari polos %q", gzETag, etag)
	}
	rec = staticGet(t, app, "GET", "/style.css", map[string]string{"Accept-Encoding": "gzip", "If-None-Match": gzETag})
	if rec.Code != http.StatusNotModified {
		t.Fatalf("revalidasi gzip status %d, mau 304", rec.Code)
	}
}

func TestStaticCacheHeaders(t *testing.T) {
	app := staticApp(t)
	cases := []struct {
		path      string
		wantCache string
	}{
		{"/style.css", "no-cache"},
		{"/manifest.webmanifest", "no-cache"},
		{"/sw.js", "no-cache"},
		{"/icon-192.png", "no-cache"},
		{"/", "no-cache"},
		{"/_app/immutable/chunks/besar.js", "public, max-age=31536000, immutable"},
	}
	for _, tc := range cases {
		rec := staticGet(t, app, "GET", tc.path, nil)
		if rec.Code != 200 {
			t.Fatalf("%s status %d", tc.path, rec.Code)
		}
		if got := rec.Header().Get("Cache-Control"); got != tc.wantCache {
			t.Fatalf("%s Cache-Control %q, mau %q", tc.path, got, tc.wantCache)
		}
	}
	rec := staticGet(t, app, "GET", "/sw.js", nil)
	if got := rec.Header().Get("Service-Worker-Allowed"); got != "/" {
		t.Fatalf("Service-Worker-Allowed %q", got)
	}
	rec = staticGet(t, app, "GET", "/manifest.webmanifest", map[string]string{"Accept-Encoding": "gzip"})
	if got := rec.Header().Get("Content-Type"); got != "application/manifest+json" {
		t.Fatalf("Content-Type manifest %q", got)
	}
	if got := rec.Header().Get("Content-Encoding"); got != "gzip" {
		t.Fatalf("manifest tidak di-gzip: %q", got)
	}
}

func TestStaticHeadGzip(t *testing.T) {
	app := staticApp(t)
	rec := staticGet(t, app, "HEAD", "/_app/immutable/chunks/besar.js", map[string]string{"Accept-Encoding": "gzip"})
	if rec.Code != 200 {
		t.Fatalf("status %d", rec.Code)
	}
	if got := rec.Header().Get("Content-Encoding"); got != "gzip" {
		t.Fatalf("Content-Encoding %q", got)
	}
	if rec.Body.Len() != 0 {
		t.Fatalf("HEAD membawa body %d byte", rec.Body.Len())
	}
	get := staticGet(t, app, "GET", "/_app/immutable/chunks/besar.js", map[string]string{"Accept-Encoding": "gzip"})
	if rec.Header().Get("Content-Length") != get.Header().Get("Content-Length") {
		t.Fatalf("Content-Length HEAD %q != GET %q", rec.Header().Get("Content-Length"), get.Header().Get("Content-Length"))
	}
}

func TestStaticIndexFallbackGzip(t *testing.T) {
	app := staticApp(t)
	rec := staticGet(t, app, "GET", "/ruang/apa-saja", map[string]string{"Accept-Encoding": "gzip"})
	if rec.Code != 200 {
		t.Fatalf("status %d", rec.Code)
	}
	if got := rec.Header().Get("Content-Encoding"); got != "gzip" {
		t.Fatalf("fallback index tidak di-gzip: %q", got)
	}
	if got := gunzip(t, rec.Body.Bytes()); !bytes.Equal(got, testIndex) {
		t.Fatal("isi fallback berbeda dari index.html")
	}
}

func TestCSPInlineStyleHash(t *testing.T) {
	app := staticApp(t)
	rec := staticGet(t, app, "GET", "/", nil)
	csp := rec.Header().Get("Content-Security-Policy")
	sum := sha256.Sum256([]byte(".skel{color:red}"))
	want := "style-src 'self' 'sha256-" + base64.StdEncoding.EncodeToString(sum[:]) + "'"
	if !strings.Contains(csp, want) {
		t.Fatalf("CSP tidak memuat hash gaya inline.\nCSP: %s\nmau: %s", csp, want)
	}
	if strings.Contains(csp, "unsafe-inline") {
		t.Fatal("CSP memuat unsafe-inline")
	}
}
