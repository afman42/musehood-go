package musehood

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func doRaw(t *testing.T, method, url string, body any, headers map[string]string) (int, http.Header, []byte) {
	t.Helper()
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		reader = strings.NewReader(string(raw))
	}
	req, err := http.NewRequest(method, url, reader)
	if err != nil {
		t.Fatal(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	return res.StatusCode, res.Header, raw
}

func sessionCookie(t *testing.T, header http.Header) string {
	t.Helper()
	for _, cookie := range header.Values("Set-Cookie") {
		if strings.HasPrefix(cookie, sessionCookieName+"=") {
			return strings.SplitN(cookie, ";", 2)[0]
		}
	}
	t.Fatalf("cookie sesi tidak ditemukan di %v", header.Values("Set-Cookie"))
	return ""
}

func TestPINLoginFlow(t *testing.T) {
	f := setup(t, nil)
	base := f.server.URL

	// Tanpa PIN: semua bebas (perilaku lama).
	if status, _, _ := doRaw(t, "GET", base+"/api/state", nil, nil); status != 200 {
		t.Fatalf("state tanpa PIN = %d", status)
	}

	// Pasang PIN.
	status, _, raw := doRaw(t, "POST", base+"/api/pin", object{"pin": "1234"}, nil)
	if status != 200 {
		t.Fatalf("set PIN = %d %s", status, raw)
	}

	// Tanpa cookie: ditolak 401 "PIN diperlukan".
	status, _, raw = doRaw(t, "GET", base+"/api/state", nil, nil)
	if status != 401 || !strings.Contains(string(raw), "PIN diperlukan") {
		t.Fatalf("state tanpa cookie = %d %s", status, raw)
	}

	// Login salah ditolak, login benar memberi cookie.
	if status, _, _ := doRaw(t, "POST", base+"/api/login", object{"pin": "salah"}, nil); status != 401 {
		t.Fatalf("login salah = %d", status)
	}
	status, header, _ := doRaw(t, "POST", base+"/api/login", object{"pin": "1234"}, nil)
	if status != 200 {
		t.Fatalf("login benar = %d", status)
	}
	cookie := sessionCookie(t, header)
	auth := map[string]string{"Cookie": cookie}

	status, _, raw = doRaw(t, "GET", base+"/api/state", nil, auth)
	if status != 200 {
		t.Fatalf("state dengan cookie = %d %s", status, raw)
	}
	if strings.Contains(string(raw), "pinHash\":\"") && !strings.Contains(string(raw), `"pinHash":""`) && !strings.Contains(string(raw), `"pinHash": ""`) {
		t.Fatalf("pinHash bocor di state: %s", raw)
	}
	var state object
	if json.Unmarshal(raw, &state) != nil {
		t.Fatal("state bukan JSON")
	}
	settingsMap, _ := state["settings"].(map[string]any)
	if settingsMap["hasPin"] != true {
		t.Fatalf("hasPin harus true: %v", settingsMap)
	}
	if _, leaked := state["workerToken"]; leaked && state["workerToken"] != "" {
		t.Fatalf("workerToken bocor di state: %v", state["workerToken"])
	}

	// Antrean pekerja: tanpa token ditolak, dengan token dari file diterima.
	if status, _, _ := doRaw(t, "GET", base+"/api/queue/muse", nil, nil); status != 401 {
		t.Fatalf("antrean tanpa token = %d", status)
	}
	tokenRaw, err := os.ReadFile(filepath.Join(f.dir, "worker-token.txt"))
	if err != nil {
		t.Fatal(err)
	}
	token := strings.TrimSpace(string(tokenRaw))
	if token == "" {
		t.Fatal("worker-token.txt kosong")
	}
	if status, _, _ := doRaw(t, "GET", base+"/api/queue/muse", nil, map[string]string{"X-Worker-Token": token}); status != 200 {
		t.Fatalf("antrean dengan token = %d", status)
	}
	if status, _, _ := doRaw(t, "POST", base+"/api/queue/muse/00000000-0000-4000-8000-000000000000", object{"text": "halo"}, nil); status != 401 {
		t.Fatalf("POST antrean tanpa token = %d", status)
	}

	// Ganti PIN butuh PIN lama; cookie lama gugur setelah PIN diganti.
	if status, _, _ := doRaw(t, "POST", base+"/api/pin", object{"pin": "5678", "oldPin": "salah"}, auth); status != 401 {
		t.Fatalf("ganti PIN dengan PIN lama salah = %d", status)
	}
	if status, _, _ := doRaw(t, "POST", base+"/api/pin", object{"pin": "5678", "oldPin": "1234"}, auth); status != 200 {
		t.Fatalf("ganti PIN = %d", status)
	}
	if status, _, _ := doRaw(t, "GET", base+"/api/state", nil, auth); status != 401 {
		t.Fatalf("cookie lama harus gugur setelah PIN diganti = %d", status)
	}
	status, header, _ = doRaw(t, "POST", base+"/api/login", object{"pin": "5678"}, nil)
	if status != 200 {
		t.Fatalf("login PIN baru = %d", status)
	}
	auth = map[string]string{"Cookie": sessionCookie(t, header)}

	// Logout menghapus cookie.
	status, header, _ = doRaw(t, "POST", base+"/api/logout", object{}, auth)
	if status != 200 || !strings.Contains(strings.Join(header.Values("Set-Cookie"), ";"), "Max-Age=0") {
		t.Fatalf("logout = %d %v", status, header.Values("Set-Cookie"))
	}

	// Hapus PIN: akses bebas lagi termasuk antrean tanpa token.
	if status, _, _ := doRaw(t, "POST", base+"/api/pin", object{"pin": "", "oldPin": "5678"}, auth); status != 200 {
		t.Fatalf("hapus PIN = %d", status)
	}
	if status, _, _ := doRaw(t, "GET", base+"/api/state", nil, nil); status != 200 {
		t.Fatalf("state setelah PIN dihapus = %d", status)
	}
	if status, _, _ := doRaw(t, "GET", base+"/api/queue/muse", nil, nil); status != 200 {
		t.Fatalf("antrean setelah PIN dihapus = %d", status)
	}
}

func TestToolLogRecordedAndCapped(t *testing.T) {
	f := setup(t, nil)
	s := seat{Code: "M01", Name: "Plamo"}
	result, _ := f.app.runTool(context.Background(), settings{}, s, "password_gen", "16", false)
	if !strings.HasPrefix(result, "Password:") {
		t.Fatalf("password_gen = %q", result)
	}
	out := f.api("GET", "/api/tool-log", nil, 200)
	entries, _ := out["entries"].([]any)
	if len(entries) != 1 {
		t.Fatalf("log harus berisi 1 entri: %v", out)
	}
	entry, _ := entries[0].(map[string]any)
	if entry["tool"] != "password_gen" || entry["seatCode"] != "M01" || entry["ok"] != true {
		t.Fatalf("entri log salah: %v", entry)
	}

	// Cap 500 FIFO.
	for i := 0; i < 505; i++ {
		f.app.recordToolLog(s, "uji", fmt.Sprintf("arg-%d", i), "ok", time.Millisecond)
	}
	f.app.mu.Lock()
	total := len(f.app.data.ToolLog)
	first := f.app.data.ToolLog[0].Arg
	f.app.mu.Unlock()
	if total != maxToolLogEntries {
		t.Fatalf("log harus dibatasi %d, dapat %d", maxToolLogEntries, total)
	}
	// 1 (password_gen) + 505 = 506 entri; 6 tertua dibuang → arg-5 pertama.
	if first != "arg-5" {
		t.Fatalf("entri tertua harus arg-5 (FIFO), dapat %q", first)
	}
}

func TestCustomToolHTTPPostAndTypedParams(t *testing.T) {
	allowPublicLookup(t)
	var gotBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		gotBody = string(raw)
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("content-type = %q", r.Header.Get("Content-Type"))
		}
		io.WriteString(w, `{"ok":true}`)
	}))
	defer server.Close()

	f := setup(t, nil)
	toolsDir := filepath.Join(f.dir, "tools")
	if err := os.MkdirAll(toolsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	def := fmt.Sprintf(`{"name":"kirim","description":"Kirim data","type":"http_post","url":%q,"body":"{\"jumlah\":{{jumlah}},\"label\":\"{{label}}\"}","params":[{"name":"jumlah","type":"number","required":true},{"name":"label","type":"string"}]}`, server.URL+"/hook")
	if err := os.WriteFile(filepath.Join(toolsDir, "kirim.json"), []byte(def), 0o600); err != nil {
		t.Fatal(err)
	}
	// Definisi lama (params string) tetap kompatibel.
	lama := fmt.Sprintf(`{"name":"lama","description":"Tool lama","type":"http_get","url":%q,"params":["kota"]}`, server.URL+"/cuaca/{{kota}}")
	if err := os.WriteFile(filepath.Join(toolsDir, "lama.json"), []byte(lama), 0o600); err != nil {
		t.Fatal(err)
	}
	tool, ok := f.app.findCustomTool("kirim")
	if !ok {
		t.Fatal("tool kirim tidak termuat")
	}
	if _, ok := f.app.findCustomTool("lama"); !ok {
		t.Fatal("tool lama (params string) harus tetap termuat")
	}
	s := seat{Code: "M01", Name: "Plamo"}

	// Validasi tipe & wajib.
	if out := f.app.runCustomTool(context.Background(), s, tool, "abc\nHalo", false); !strings.Contains(out, "harus berupa angka") {
		t.Fatalf("validasi number = %q", out)
	}
	if out := f.app.runCustomTool(context.Background(), s, tool, "", false); !strings.Contains(out, "wajib diisi") {
		t.Fatalf("validasi required = %q", out)
	}

	// Jalur sukses dengan persetujuan.
	done := make(chan string, 1)
	go func() { done <- f.app.runCustomTool(context.Background(), s, tool, "42\nHalo", false) }()
	deadline := time.Now().Add(10 * time.Second)
	for len(f.app.pendingApprovals()) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("persetujuan http_post tidak muncul")
		}
		time.Sleep(20 * time.Millisecond)
	}
	pending := f.app.pendingApprovals()[0]
	if pending.Kind != "http" {
		t.Fatalf("kind persetujuan = %q", pending.Kind)
	}
	f.app.decideApproval(pending.ID, true)
	if out := <-done; !strings.Contains(out, "HTTP 200") {
		t.Fatalf("http_post kustom = %q", out)
	}
	if gotBody != `{"jumlah":42,"label":"Halo"}` {
		t.Fatalf("body terkirim = %q", gotBody)
	}

	// Penolakan pemilik: tidak dikirim.
	gotBody = ""
	go func() { done <- f.app.runCustomTool(context.Background(), s, tool, "7\nLagi", false) }()
	deadline = time.Now().Add(10 * time.Second)
	for len(f.app.pendingApprovals()) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("persetujuan kedua tidak muncul")
		}
		time.Sleep(20 * time.Millisecond)
	}
	f.app.decideApproval(f.app.pendingApprovals()[0].ID, false)
	if out := <-done; !strings.Contains(out, "TIDAK dikirim") {
		t.Fatalf("penolakan = %q", out)
	}
	if gotBody != "" {
		t.Fatalf("body tidak boleh terkirim setelah ditolak: %q", gotBody)
	}
}
