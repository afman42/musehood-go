package musehood

import (
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// allowPublicLookup membuat checkPublicURL lolos untuk server tes lokal.
func allowPublicLookup(t *testing.T) {
	t.Helper()
	old := lookupIP
	lookupIP = func(ctx context.Context, host string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("93.184.216.34")}, nil
	}
	t.Cleanup(func() { lookupIP = old })
}

func writeSandboxPNG(t *testing.T, app *App, name string) {
	t.Helper()
	if err := os.MkdirAll(app.sandboxDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	img.Set(0, 0, color.White)
	f, err := os.Create(filepath.Join(app.sandboxDir(), name))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
}

func TestPasswordGen(t *testing.T) {
	out := toolPasswordGen("")
	if !strings.HasPrefix(out, "Password: ") || len(strings.TrimPrefix(out, "Password: ")) != 16 {
		t.Fatalf("password bawaan = %q", out)
	}
	out = toolPasswordGen("24 nosymbol")
	pw := strings.TrimPrefix(out, "Password: ")
	if len(pw) != 24 || strings.ContainsAny(pw, "!@#$%^&*") {
		t.Fatalf("password nosymbol = %q", out)
	}
	if !strings.ContainsAny(pw, "0123456789") || !strings.ContainsAny(pw, "ABCDEFGHIJKLMNOPQRSTUVWXYZ") {
		t.Fatalf("password harus memuat digit + huruf besar: %q", pw)
	}
	if got := len(strings.TrimPrefix(toolPasswordGen("2"), "Password: ")); got != 8 {
		t.Fatalf("panjang minimum harus 8, dapat %d", got)
	}
}

func TestTranslateText(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("langpair") != "id|en" {
			t.Errorf("langpair = %q", r.URL.Query().Get("langpair"))
		}
		io.WriteString(w, `{"responseData":{"translatedText":"good morning"},"responseStatus":200}`)
	}))
	defer server.Close()
	old := translateAPIURL
	translateAPIURL = server.URL
	defer func() { translateAPIURL = old }()

	app := &App{dir: t.TempDir()}
	if out := app.toolTranslateText(context.Background(), "id>en\nselamat pagi"); out != "good morning" {
		t.Fatalf("translate = %q", out)
	}
	if out := app.toolTranslateText(context.Background(), "selamat pagi"); !strings.Contains(out, "Error") {
		t.Fatalf("tanpa pasangan bahasa harus error: %q", out)
	}
}

func TestCurrencyConvert(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"amount":100,"base":"USD","date":"2026-11-27","rates":{"IDR":1666500}}`)
	}))
	defer server.Close()
	old := currencyAPIURL
	currencyAPIURL = server.URL
	defer func() { currencyAPIURL = old }()

	app := &App{dir: t.TempDir()}
	out := app.toolCurrencyConvert(context.Background(), "100 USD IDR")
	if !strings.Contains(out, "100 USD = 1666500 IDR") || !strings.Contains(out, "2026-11-27") {
		t.Fatalf("currency = %q", out)
	}
	if out := app.toolCurrencyConvert(context.Background(), "USD"); !strings.Contains(out, "Error") {
		t.Fatalf("format salah harus error: %q", out)
	}
}

func TestRSSFeed(t *testing.T) {
	allowPublicLookup(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "atom") {
			io.WriteString(w, `<?xml version="1.0"?><feed xmlns="http://www.w3.org/2005/Atom"><title>Contoh</title><entry><title>Entri Atom</title><link href="https://example.com/a"/><updated>2026-11-27T00:00:00Z</updated></entry></feed>`)
			return
		}
		io.WriteString(w, `<?xml version="1.0"?><rss version="2.0"><channel><title>Contoh</title><item><title>Berita Satu</title><link>https://example.com/1</link><pubDate>Fri, 27 Nov 2026 00:00:00 GMT</pubDate></item><item><title>Berita Dua</title><link>https://example.com/2</link></item></channel></rss>`)
	}))
	defer server.Close()

	app := &App{dir: t.TempDir()}
	out := app.toolRSSFeed(context.Background(), server.URL+"/feed.xml")
	if !strings.Contains(out, "Berita Satu") || !strings.Contains(out, "Berita Dua") {
		t.Fatalf("rss = %q", out)
	}
	out = app.toolRSSFeed(context.Background(), server.URL+"/atom.xml\n1")
	if !strings.Contains(out, "Entri Atom") {
		t.Fatalf("atom = %q", out)
	}
	if out := app.toolRSSFeed(context.Background(), "http://127.0.0.1/feed"); !strings.Contains(out, "Error") {
		t.Fatalf("localhost harus ditolak: %q", out)
	}
}

func TestQRCode(t *testing.T) {
	pngBytes, _ := base64PNG()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("data") != "https://example.com" {
			t.Errorf("data QR = %q", r.URL.Query().Get("data"))
		}
		w.Write(pngBytes)
	}))
	defer server.Close()
	old := qrAPIURL
	qrAPIURL = server.URL + "/"
	defer func() { qrAPIURL = old }()

	app := &App{dir: t.TempDir()}
	if err := os.MkdirAll(filepath.Join(app.dir, "images"), 0o755); err != nil {
		t.Fatal(err)
	}
	msg, imageURL := app.toolQRCode(context.Background(), "https://example.com")
	if !strings.Contains(msg, "berhasil") || !strings.HasPrefix(imageURL, "/images/") {
		t.Fatalf("qr = %q %q", msg, imageURL)
	}
	if _, err := os.Stat(filepath.Join(app.dir, strings.TrimPrefix(imageURL, "/"))); err != nil {
		t.Fatalf("file QR tidak ada: %v", err)
	}
	if msg, _ := app.toolQRCode(context.Background(), ""); !strings.Contains(msg, "Error") {
		t.Fatalf("teks kosong harus error: %q", msg)
	}
}

func base64PNG() ([]byte, error) {
	return []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x06\x00\x00\x00\x1f\x15\xc4\x89\x00\x00\x00\nIDATx\x9cc\x00\x01\x00\x00\x05\x00\x01\r\n-\xb4\x00\x00\x00\x00IEND\xaeB`\x82"), nil
}

func TestYouTubeTranscript(t *testing.T) {
	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	defer server.Close()
	mux.HandleFunc("/watch", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `<html><script>var ytInitialPlayerResponse = {"captions":{"playerCaptionsTracklistRenderer":{"captionTracks":[{"baseUrl":"`+server.URL+`/timedtext?v=abc","languageCode":"en"},{"baseUrl":"`+server.URL+`/timedtext?v=id","languageCode":"id"}]}}};</script></html>`)
	})
	mux.HandleFunc("/timedtext", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `<?xml version="1.0"?><transcript><text start="0" dur="2">Halo &amp; selamat</text><text start="2" dur="2">datang</text></transcript>`)
	})
	old := youtubeBaseURL
	youtubeBaseURL = server.URL
	defer func() { youtubeBaseURL = old }()

	app := &App{dir: t.TempDir()}
	out := app.toolYouTubeTranscript(context.Background(), "https://www.youtube.com/watch?v=dQw4w9WgXcQ")
	if out != "Halo & selamat datang" {
		t.Fatalf("transkrip = %q", out)
	}
	if out := app.toolYouTubeTranscript(context.Background(), "bukan url"); !strings.Contains(out, "Error") {
		t.Fatalf("input salah harus error: %q", out)
	}
}

func TestHTTPPostApproval(t *testing.T) {
	allowPublicLookup(t)
	var gotBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		gotBody = string(raw)
		io.WriteString(w, `{"ok":true}`)
	}))
	defer server.Close()

	app := &App{dir: t.TempDir()}
	s := seat{Code: "M01", Name: "Plamo"}
	// JSON tidak valid ditolak sebelum persetujuan.
	if out := app.toolHTTPPost(context.Background(), s, server.URL+"\nbukan json"); !strings.Contains(out, "Error") {
		t.Fatalf("JSON invalid harus error: %q", out)
	}
	done := make(chan string, 1)
	go func() { done <- app.toolHTTPPost(context.Background(), s, server.URL+"/hook\n{\"halo\":1}") }()
	deadline := time.Now().Add(10 * time.Second)
	for len(app.pendingApprovals()) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("persetujuan tidak muncul")
		}
		time.Sleep(20 * time.Millisecond)
	}
	pending := app.pendingApprovals()[0]
	if pending.Kind != "http" || !strings.Contains(pending.Detail, "POST ") {
		t.Fatalf("approval = %+v", pending)
	}
	app.decideApproval(pending.ID, true)
	out := <-done
	if !strings.Contains(out, "HTTP 200") || !strings.Contains(out, `"ok":true`) {
		t.Fatalf("http_post = %q", out)
	}
	if gotBody != `{"halo":1}` {
		t.Fatalf("body terkirim = %q", gotBody)
	}
}

func TestOCRImage(t *testing.T) {
	app := &App{dir: t.TempDir()}
	if out := app.toolOCRImage(context.Background(), "tidak-ada.png"); !strings.Contains(out, "Error") {
		t.Fatalf("file hilang harus error: %q", out)
	}
	writeSandboxPNG(t, app, "kosong.png")
	out := app.toolOCRImage(context.Background(), "kosong.png")
	if _, err := exec.LookPath("tesseract"); err != nil {
		if !strings.Contains(out, "tidak terpasang") {
			t.Fatalf("tanpa tesseract harus jelas: %q", out)
		}
		return
	}
	if strings.HasPrefix(out, "Error:") {
		t.Fatalf("OCR gambar kosong tidak boleh error: %q", out)
	}
}

func TestTextToSpeech(t *testing.T) {
	app := &App{dir: t.TempDir()}
	if out := app.toolTextToSpeech(context.Background(), "lang=xx-!;rm\nHalo"); !strings.Contains(out, "Error") {
		t.Fatalf("kode bahasa jahat harus error: %q", out)
	}
	out := app.toolTextToSpeech(context.Background(), "lang=id\nHalo Arman, ini tes.")
	if _, err := exec.LookPath("espeak-ng"); err != nil {
		if !strings.Contains(out, "tidak terpasang") {
			t.Fatalf("tanpa espeak-ng harus jelas: %q", out)
		}
		return
	}
	if !strings.Contains(out, "Audio tersimpan") {
		t.Fatalf("tts = %q", out)
	}
	entries, _ := os.ReadDir(app.sandboxDir())
	found := false
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".wav") {
			raw, _ := os.ReadFile(filepath.Join(app.sandboxDir(), e.Name()))
			if len(raw) > 12 && string(raw[:4]) == "RIFF" {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("file WAV RIFF tidak ditemukan")
	}
}

func TestImageCaption(t *testing.T) {
	var sawImage bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Messages []struct {
				Content json.RawMessage `json:"content"`
			} `json:"messages"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		if len(body.Messages) > 0 && strings.Contains(string(body.Messages[0].Content), "image_url") {
			sawImage = true
		}
		io.WriteString(w, `{"choices":[{"message":{"content":"Gambar putih polos."}}]}`)
	}))
	defer server.Close()

	app := &App{dir: t.TempDir(), client: &http.Client{Timeout: 10 * time.Second}}
	writeSandboxPNG(t, app, "putih.png")
	model := "vision-model"
	s := seat{Code: "M01", Name: "Plamo", Model: &model}
	cfg := settings{BaseURL: server.URL, APIKey: "kunci", GatewayProtocol: "openai"}
	out := app.toolImageCaption(context.Background(), cfg, s, "putih.png")
	if out != "Gambar putih polos." || !sawImage {
		t.Fatalf("caption = %q sawImage=%v", out, sawImage)
	}
	if out := app.toolImageCaption(context.Background(), cfg, seat{Code: "M01", Name: "Plamo"}, "putih.png"); !strings.Contains(out, "belum diisi") {
		t.Fatalf("tanpa model harus error jelas: %q", out)
	}
}

func TestNewToolsRegistered(t *testing.T) {
	names := []string{"translate_text", "ocr_image", "image_caption", "qr_code", "rss_feed",
		"youtube_transcript", "currency_convert", "text_to_speech", "http_post", "password_gen"}
	for _, name := range names {
		if !builtinToolNames[name] {
			t.Fatalf("tool %s belum terdaftar di builtinToolNames", name)
		}
		if !strings.Contains(toolInstructions, "- "+name+":") {
			t.Fatalf("tool %s belum ada di instruksi", name)
		}
	}
	app := &App{dir: t.TempDir()}
	out, _ := app.runTool(context.Background(), settings{}, seat{Code: "M01"}, "password_gen", "12", true)
	if !strings.HasPrefix(out, "Password: ") {
		t.Fatalf("dispatch password_gen = %q", out)
	}
}
