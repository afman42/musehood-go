package musehood

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// Tes kompresi gzip respons API JSON: hanya badan >1 KB, menghormati
// Accept-Encoding (termasuk q=0), dan jalur streaming gateway→server
// tetap mengalir baris demi baris (kompresi hanya menyentuh badan
// JSON utuh di writeJSON/cadangan, bukan stream).

var bigJSONValue = json.RawMessage(`{"pesan":"` + strings.Repeat("halo dunia musehood ", 200) + `"}`)

func apiJSONRequest(headers map[string]string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/api/state", nil)
	req.Host = "localhost"
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	return req
}

func TestAPIJSONGzipCompression(t *testing.T) {
	app := staticApp(t)
	rec := httptest.NewRecorder()
	app.writeJSON(rec, apiJSONRequest(map[string]string{"Accept-Encoding": "gzip"}), 200, bigJSONValue, nil)
	if rec.Code != 200 {
		t.Fatalf("status %d", rec.Code)
	}
	if got := rec.Header().Get("Content-Encoding"); got != "gzip" {
		t.Fatalf("Content-Encoding %q, mau gzip", got)
	}
	if got := rec.Header().Get("Vary"); !strings.Contains(got, "Accept-Encoding") {
		t.Fatalf("Vary %q, mau memuat Accept-Encoding", got)
	}
	body := rec.Body.Bytes()
	if len(body) >= len(bigJSONValue) {
		t.Fatalf("hasil gzip %d byte tidak lebih kecil dari asli %d", len(body), len(bigJSONValue))
	}
	want := append(append([]byte{}, bigJSONValue...), '\n')
	if got := gunzip(t, body); string(got) != string(want) {
		t.Fatalf("hasil gunzip tidak sama dengan badan asli")
	}
}

func TestAPIJSONGzipQZero(t *testing.T) {
	app := staticApp(t)
	rec := httptest.NewRecorder()
	app.writeJSON(rec, apiJSONRequest(map[string]string{"Accept-Encoding": "gzip;q=0, deflate"}), 200, bigJSONValue, nil)
	if got := rec.Header().Get("Content-Encoding"); got != "" {
		t.Fatalf("gzip;q=0 tetap dikompresi: %q", got)
	}
	if rec.Body.String() != string(bigJSONValue)+"\n" {
		t.Fatalf("badan berubah walau tidak dikompresi")
	}
}

func TestAPIJSONBelowThreshold(t *testing.T) {
	app := staticApp(t)
	small := json.RawMessage(`{"ok":true}`)
	rec := httptest.NewRecorder()
	app.writeJSON(rec, apiJSONRequest(map[string]string{"Accept-Encoding": "gzip"}), 200, small, nil)
	if got := rec.Header().Get("Content-Encoding"); got != "" {
		t.Fatalf("badan kecil ikut dikompresi: %q", got)
	}
	if rec.Body.String() != string(small)+"\n" {
		t.Fatalf("badan kecil berubah")
	}
}

func TestAPIJSONNoAcceptEncoding(t *testing.T) {
	app := staticApp(t)
	rec := httptest.NewRecorder()
	app.writeJSON(rec, apiJSONRequest(nil), 200, bigJSONValue, nil)
	if got := rec.Header().Get("Content-Encoding"); got != "" {
		t.Fatalf("tanpa Accept-Encoding tetap dikompresi: %q", got)
	}
}

func TestAPIJSONErrorNotCompressed(t *testing.T) {
	app := staticApp(t)
	rec := httptest.NewRecorder()
	app.writeJSON(rec, apiJSONRequest(map[string]string{"Accept-Encoding": "gzip"}), 0, nil, problem(400, "Permintaan tidak valid."))
	if rec.Code != 400 {
		t.Fatalf("status %d, mau 400", rec.Code)
	}
	if got := rec.Header().Get("Content-Encoding"); got != "" {
		t.Fatalf("error kecil ikut dikompresi: %q", got)
	}
	if !strings.Contains(rec.Body.String(), "Permintaan tidak valid.") {
		t.Fatalf("pesan error hilang: %s", rec.Body.String())
	}
}

func TestAPIStateGzipEndToEnd(t *testing.T) {
	app := staticApp(t)
	plain := staticGet(t, app, "GET", "/api/state", nil)
	if plain.Code != 200 {
		t.Fatalf("status polos %d", plain.Code)
	}
	if plain.Body.Len() <= apiGzipThreshold {
		t.Fatalf("state bawaan %d byte, tes butuh >%d", plain.Body.Len(), apiGzipThreshold)
	}
	rec := staticGet(t, app, "GET", "/api/state", map[string]string{"Accept-Encoding": "gzip"})
	if rec.Code != 200 {
		t.Fatalf("status gzip %d", rec.Code)
	}
	if got := rec.Header().Get("Content-Encoding"); got != "gzip" {
		t.Fatalf("Content-Encoding %q, mau gzip", got)
	}
	if rec.Body.Len() >= plain.Body.Len() {
		t.Fatalf("gzip %d byte tidak lebih kecil dari polos %d", rec.Body.Len(), plain.Body.Len())
	}
	var state map[string]any
	if err := json.Unmarshal(gunzip(t, rec.Body.Bytes()), &state); err != nil {
		t.Fatalf("hasil gunzip bukan JSON valid: %v", err)
	}
	if _, ok := state["seats"]; !ok {
		t.Fatalf("state hasil gunzip tidak memuat seats: %v", state)
	}
}

func TestBackupGzip(t *testing.T) {
	app := staticApp(t)
	rec := staticGet(t, app, "GET", "/api/backup", map[string]string{"Accept-Encoding": "gzip"})
	if rec.Code != 200 {
		t.Fatalf("status %d", rec.Code)
	}
	if got := rec.Header().Get("Content-Encoding"); got != "gzip" {
		t.Fatalf("Content-Encoding %q, mau gzip", got)
	}
	if got := rec.Header().Get("Content-Disposition"); !strings.Contains(got, "cadangan-musehood.json") {
		t.Fatalf("Content-Disposition hilang: %q", got)
	}
	var backup map[string]any
	if err := json.Unmarshal(gunzip(t, rec.Body.Bytes()), &backup); err != nil {
		t.Fatalf("cadangan hasil gunzip bukan JSON valid: %v", err)
	}
}

// TestGatewayStreamIncremental membuktikan jalur streaming
// gateway→server meneruskan baris begitu tiba (tidak menunggu
// seluruh respons), sehingga kompresi API tidak menahan event.
func TestGatewayStreamIncremental(t *testing.T) {
	app := staticApp(t)
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fl, ok := w.(http.Flusher)
		if !ok {
			t.Error("server tes tidak mendukung Flush")
			return
		}
		fmt.Fprint(w, "data: satu\n\n")
		fl.Flush()
		<-release
		fmt.Fprint(w, "data: dua\n\n")
		fl.Flush()
	}))
	defer srv.Close()

	lines := make(chan string, 4)
	done := make(chan error, 1)
	go func() {
		done <- app.gatewayStream(context.Background(), srv.URL, map[string]any{"model": "tes"}, nil, func(line string) {
			lines <- line
		})
	}()

	select {
	case first := <-lines:
		if first != "data: satu" {
			t.Fatalf("baris pertama %q, mau %q", first, "data: satu")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("baris pertama tidak tiba selagi server masih menahan baris kedua — streaming tertahan buffer")
	}
	close(release)
	select {
	case second := <-lines:
		if second != "data: dua" {
			t.Fatalf("baris kedua %q, mau %q", second, "data: dua")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("baris kedua tidak tiba")
	}
	if err := <-done; err != nil {
		t.Fatalf("gatewayStream error: %v", err)
	}
}
