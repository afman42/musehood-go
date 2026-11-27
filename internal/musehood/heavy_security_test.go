package musehood

// Tes berat keamanan & fuzz (27 Nov 2026): input aneh ke semua endpoint
// (tidak boleh 500/panic), SSRF, brute force PIN, backup jahat, unggahan
// abusif, penyalahgunaan sandbox tool, redaksi secret.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// heavyRaw mengirim body mentah dan mengembalikan status + body.
func heavyRaw(t *testing.T, method, url string, body []byte, contentType string, headers map[string]string) (int, []byte) {
	t.Helper()
	var rdr io.Reader
	if body != nil {
		rdr = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, url, rdr)
	if err != nil {
		t.Fatal(err)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	return res.StatusCode, raw
}

func TestHeavyFuzzEndpointsNever500(t *testing.T) {
	f := setup(t, nil)
	base := f.server.URL
	bigString := strings.Repeat("x", 300*1024) // > batas body 256 KB
	payloads := [][]byte{
		[]byte(`{bukan json`),
		[]byte(`[]`),
		[]byte(`null`),
		[]byte(`{"text":123}`),
		[]byte(`{"text":null}`),
		[]byte(`{"text":"` + bigString + `"}`),
		[]byte(`{"text":"halo\u0000dunia"}`),
		[]byte(`{"settings":{"density":42}}`),
		[]byte(`{"settings":{"integratorSeat":"TIDAKADA"}}`),
		[]byte(`{"name":""}`),
		[]byte(`{"name":"` + strings.Repeat("n", 5000) + `"}`),
		[]byte(`{"pin":"abc"}`),
		[]byte(`{"pin":"123456789012345678901234567890"}`),
		[]byte(`{"modelA":1,"modelB":2,"text":3}`),
		[]byte(`{"steps":"bukan-array"}`),
		[]byte(`{"steps":[{"seatCode":"XXXX","instruction":"x"}]}`),
		[]byte(`{"rating":99}`),
		[]byte(`{"rating":"bagus"}`),
		[]byte(`{"baseUrl":"http://169.254.169.254/latest/meta-data"}`),
		[]byte(`{"baseUrl":"bukan url"}`),
		[]byte(`{"text":"halo","roomId":"tidak-ada"}`),
		[]byte(`{"text":"halo","replyTo":"tidak-ada"}`),
		[]byte(`{"text":"halo","author":"` + strings.Repeat("a", 500) + `"}`),
	}
	targets := []struct{ method, path string }{
		{"POST", "/api/message"}, {"POST", "/api/settings"}, {"POST", "/api/rooms"},
		{"POST", "/api/abtest"},
		{"POST", "/api/chains"}, {"POST", "/api/ollama/detect"}, {"POST", "/api/ollama/add"},
		{"POST", "/api/providers"}, {"POST", "/api/debate"}, {"POST", "/api/vote"},
		{"POST", "/api/templates"}, {"POST", "/api/scheduled"}, {"POST", "/api/presets"},
		{"POST", "/api/skills/install"}, {"POST", "/api/external/message"},
		{"POST", "/api/message/tidak-ada/rate"}, {"POST", "/api/message/tidak-ada/react"},
		{"POST", "/api/message/tidak-ada/edit"}, {"POST", "/api/message/tidak-ada/delete"},
		{"POST", "/api/message/tidak-ada/rerun"}, {"POST", "/api/message/tidak-ada/fork"},
		{"POST", "/api/message/tidak-ada/branch"}, {"POST", "/api/message/tidak-ada/tags"},
		{"POST", "/api/rooms/tidak-ada/participants"}, {"POST", "/api/rooms/tidak-ada/archive"},
		{"POST", "/api/seats/XXXX/clone"}, {"POST", "/api/chains/tidak-ada/run"},
		{"POST", "/api/abtest/tidak-ada/pick"}, {"POST", "/api/approvals/tidak-ada"},
	}
	for _, tg := range targets {
		for i, p := range payloads {
			st, raw := heavyRaw(t, tg.method, base+tg.path, p, "application/json", nil)
			// 502 hanya sah untuk ollama/detect (upstream lokal mati = Bad
			// Gateway yang disengaja); 500 tidak pernah boleh muncul.
			if st == 500 || (st > 500 && tg.path != "/api/ollama/detect") || st == 502 && tg.path != "/api/ollama/detect" {
				t.Fatalf("%s %s payload %d -> %d %s", tg.method, tg.path, i, st, raw)
			}
			if bytes.Contains(raw, []byte(secret)) {
				t.Fatalf("%s %s membocorkan secret", tg.method, tg.path)
			}
		}
	}
	// Path traversal & ID aneh pada endpoint ber-ID (GET).
	for _, path := range []string{
		"/api/rooms/../state", "/api/rooms/%2e%2e/state", "/api/rooms/%2E%2E%2Fstate",
		"/api/rooms/....//documents", "/api/export?room=../../etc",
		"/api/search?q=%00", "/api/search?q=" + strings.Repeat("a", 5000),
		"/api/queue/muse/..%2f..%2fdata.json",
	} {
		st, _ := heavyRaw(t, "GET", base+path, nil, "", nil)
		if st >= 500 {
			t.Fatalf("GET %s -> %d", path, st)
		}
	}
	// Server masih sehat sesudah fuzz.
	f.state()
}

func TestHeavySSRFBlocked(t *testing.T) {
	f := setup(t, nil)
	ctx := context.Background()
	orig := lookupIP
	defer func() { lookupIP = orig }()

	// Host yang resolve ke alamat privat/metadata harus ditolak.
	lookupIP = func(ctx context.Context, host string) ([]net.IP, error) {
		switch host {
		case "metadata.google.internal":
			return []net.IP{net.ParseIP("169.254.169.254")}, nil
		case "rebind.example.com":
			return []net.IP{net.ParseIP("10.0.0.5")}, nil
		case "campuran.example.com":
			return []net.IP{net.ParseIP("8.8.8.8"), net.ParseIP("192.168.1.1")}, nil
		}
		return nil, fmt.Errorf("tidak ada")
	}
	for _, u := range []string{
		"http://169.254.169.254/latest/meta-data/",
		"http://metadata.google.internal/computeMetadata/v1/",
		"http://rebind.example.com/",
		"http://campuran.example.com/",
		"http://localhost:3001/api/state",
		"http://127.0.0.1/admin",
		"http://[::1]/",
		"ftp://example.com/file",
		"gopher://example.com/",
		"file:///etc/passwd",
		"http://",
		"",
	} {
		if err := checkPublicURL(ctx, u); err == nil {
			t.Fatalf("checkPublicURL(%q) harus menolak", u)
		}
		out, _ := f.app.runTool(ctx, settings{}, seat{Code: "M01"}, "fetch_url", u, false)
		if !strings.HasPrefix(out, "Error:") {
			t.Fatalf("fetch_url %q harus error, dapat %q", u, out)
		}
	}
	// Redirect ke IP privat: server publik (dimock) yang mengalihkan ke
	// 169.254.169.254 harus gagal di langkah redirect.
	lookupIP = func(ctx context.Context, host string) ([]net.IP, error) {
		if host == "publik.example.com" {
			return []net.IP{net.ParseIP("93.184.216.34")}, nil
		}
		return orig(ctx, host)
	}
	// Ollama detect ke host publik ditolak; ke metadata ditolak.
	for _, u := range []string{"http://169.254.169.254:11434", "http://8.8.8.8:11434", "http://example.com:11434"} {
		st, _ := heavyRaw(t, "POST", f.server.URL+"/api/ollama/detect",
			[]byte(fmt.Sprintf(`{"baseUrl":%q}`, u)), "application/json", nil)
		if st != 400 {
			t.Fatalf("ollama detect %q: mau 400 dapat %d", u, st)
		}
	}
}

func TestHeavyPINBruteForceRateLimit(t *testing.T) {
	f := setup(t, nil)
	base := f.server.URL
	if st, _ := heavyRaw(t, "POST", base+"/api/pin", []byte(`{"pin":"2468"}`), "application/json", nil); st != 200 {
		t.Fatalf("set PIN: %d", st)
	}
	// 5 login salah dari IP yang sama -> percobaan berikutnya 429.
	for i := 0; i < 5; i++ {
		st, _ := heavyRaw(t, "POST", base+"/api/login", []byte(`{"pin":"0000"}`), "application/json", nil)
		if st != 401 {
			t.Fatalf("login salah %d: mau 401 dapat %d", i, st)
		}
	}
	st, _ := heavyRaw(t, "POST", base+"/api/login", []byte(`{"pin":"0000"}`), "application/json", nil)
	if st != 429 {
		t.Fatalf("login ke-6 harus 429, dapat %d", st)
	}
	// PIN benar pun tertahan selama rate limit aktif (IP sama).
	st, _ = heavyRaw(t, "POST", base+"/api/login", []byte(`{"pin":"2468"}`), "application/json", nil)
	if st != 429 {
		t.Fatalf("login benar saat rate-limit harus 429, dapat %d", st)
	}
	// Semua endpoint biasa tanpa cookie -> 401 saat PIN aktif.
	for _, path := range []string{"/api/state", "/api/rooms", "/api/settings", "/api/stats", "/api/tools", "/api/backup"} {
		st, _ := heavyRaw(t, "GET", base+path, nil, "", nil)
		if st != 401 {
			t.Fatalf("GET %s tanpa auth: mau 401 dapat %d", path, st)
		}
	}
	// Login tercatat di log audit.
	f.app.mu.Lock()
	log := f.app.data.LoginLog
	f.app.mu.Unlock()
	if len(log) < 5 {
		t.Fatalf("LoginLog %d entri, mau >=5", len(log))
	}
}

func TestHeavyMaliciousRestore(t *testing.T) {
	f := setup(t, nil)
	base := f.server.URL
	// Pasang PIN lokal + catat hash & token pekerja.
	if st, _ := heavyRaw(t, "POST", base+"/api/pin", []byte(`{"pin":"1357"}`), "application/json", nil); st != 200 {
		t.Fatal("set PIN gagal")
	}
	auth := loginCookie(t, f, "1357")
	f.app.mu.Lock()
	localHash := f.app.data.Settings.PINHash
	localToken := f.app.data.WorkerToken
	f.app.mu.Unlock()

	// Ambil backup sah lalu rekayasa: PINHash asing, token asing, 1000 kursi.
	st, backupRaw := heavyRaw(t, "GET", base+"/api/backup", nil, "", auth)
	if st != 200 {
		t.Fatalf("backup: %d", st)
	}
	var backup map[string]any
	if err := json.Unmarshal(backupRaw, &backup); err != nil {
		t.Fatal(err)
	}
	settingsMap := backup["settings"].(map[string]any)
	foreignHash, _ := hashPIN("9999")
	settingsMap["pinHash"] = foreignHash
	backup["workerToken"] = "token-asing"
	raw, _ := json.Marshal(backup)
	st, resp := heavyRaw(t, "POST", base+"/api/restore", raw, "application/json", auth)
	if st != 200 {
		t.Fatalf("restore rekayasa: %d %s", st, resp)
	}
	f.app.mu.Lock()
	gotHash := f.app.data.Settings.PINHash
	gotToken := f.app.data.WorkerToken
	f.app.mu.Unlock()
	if gotHash != localHash {
		t.Fatal("restore menimpa PINHash lokal")
	}
	if gotToken != localToken {
		t.Fatal("restore menimpa WorkerToken lokal")
	}
	// Login dengan PIN asing harus gagal; PIN lokal tetap jalan.
	st, _ = heavyRaw(t, "POST", base+"/api/login", []byte(`{"pin":"9999"}`), "application/json", nil)
	if st != 401 {
		t.Fatalf("login PIN asing: mau 401 dapat %d", st)
	}

	// Backup terpotong / bukan JSON -> 400.
	for _, bad := range [][]byte{[]byte(`{"seats":[`), []byte(`bukan json`), []byte(`{}`), []byte(`{"seats":[],"messages":[]}`)} {
		st, _ := heavyRaw(t, "POST", base+"/api/restore", bad, "application/json", auth)
		if st != 400 {
			t.Fatalf("restore rusak %q: mau 400 dapat %d", bad, st)
		}
	}
	// 1000 kursi: kursi bawaan di urutan salah -> 400 (validasi urutan).
	var many map[string]any
	_ = json.Unmarshal(backupRaw, &many)
	seats := many["seats"].([]any)
	for len(seats) < 1000 {
		seats = append(seats, seats[len(seats)-1])
	}
	many["seats"] = seats
	raw, _ = json.Marshal(many)
	st, _ = heavyRaw(t, "POST", base+"/api/restore", raw, "application/json", auth)
	if st >= 500 {
		t.Fatalf("restore 1000 kursi -> %d", st)
	}
	// Server tetap sehat.
	if st, _ := heavyRaw(t, "GET", base+"/api/state", nil, "", auth); st != 200 {
		t.Fatalf("state sesudah restore jahat: %d", st)
	}
}

func TestHeavyUploadAbuse(t *testing.T) {
	f := setup(t, nil)
	base := f.server.URL
	roomID := f.state()["activeRoomId"].(string)
	uploadDoc := func(name string, content []byte) int {
		var buf bytes.Buffer
		mw := multipart.NewWriter(&buf)
		part, _ := mw.CreateFormFile("file", name)
		part.Write(content)
		mw.Close()
		st, _ := heavyRaw(t, "POST", base+"/api/rooms/"+roomID+"/documents", buf.Bytes(), mw.FormDataContentType(), nil)
		return st
	}
	if st := uploadDoc("kosong.txt", nil); st < 400 || st >= 500 {
		t.Fatalf("dokumen kosong: %d", st)
	}
	if st := uploadDoc("besar.txt", bytes.Repeat([]byte("a"), 6*1024*1024)); st != 413 && st != 400 {
		t.Fatalf("dokumen 6MB: mau 413/400 dapat %d", st)
	}
	if st := uploadDoc("aneh.exe", []byte("MZ\x90\x00")); st < 400 || st >= 500 {
		t.Fatalf("dokumen .exe: %d", st)
	}
	if st := uploadDoc("rusak.pdf", []byte("%PDF-1.4 bukan pdf sungguhan")); st < 400 || st >= 500 {
		t.Fatalf("pdf rusak: %d", st)
	}
	// CSV raksasa (4 MB, di bawah batas) diterima atau ditolak rapi, bukan 500.
	if st := uploadDoc("raksasa.csv", bytes.Repeat([]byte("a,b,c\n"), 600000)); st >= 500 {
		t.Fatalf("csv raksasa: %d", st)
	}
	// Nama file traversal.
	if st := uploadDoc("../../etc/passwd.txt", []byte("isi")); st >= 500 {
		t.Fatalf("nama traversal: %d", st)
	}
	if _, err := os.Stat(filepath.Join(f.dir, "passwd.txt")); err == nil {
		t.Fatal("file traversal lolos ke direktori data")
	}
}

func TestHeavyToolSandboxAbuse(t *testing.T) {
	f := setup(t, nil)
	old := approvalTimeout
	approvalTimeout = 200 * time.Millisecond
	defer func() { approvalTimeout = old }()
	ctx := context.Background()
	s := seat{Code: "M01", Name: "Plamo"}
	cases := []struct{ tool, arg string }{
		{"write_file", "../keluar.txt\nisi"},
		{"write_file", "/etc/passwd\nisi"},
		{"write_file", "besar.txt\n" + strings.Repeat("a", 101*1024)},
		{"read_file", "../../data.json"},
		{"read_file", "/etc/passwd"},
		{"append_file", "../x.txt\nisi"},
		{"edit_file", "../x.txt\n<<<OLD>>>\na\n<<<NEW>>>\nb"},
		{"delete_file", "../../data.json"},
		{"download_file", "http://169.254.169.254/latest\nfile.txt"},
		{"zip_files", "tidak-ada.txt\n../keluar.zip"},
		{"unzip_file", "../../data.json"},
		{"file_hash", "/etc/passwd"},
		{"pdf_text", "/etc/passwd"},
		{"csv_json", "/etc/passwd"},
		{"image_info", "/etc/passwd"},
		{"resize_image", "/etc/passwd\n10x10"},
		{"run_shell", "rm -rf /"},
		{"allowlist_shell", "echo halo; cat /etc/passwd"},
		{"allowlist_shell", "echo $(cat /etc/passwd)"},
		{"allowlist_shell", "curl http://169.254.169.254"},
		{"search_code", "project:../\n.*"},
		{"read_file_lines", "../data.json\n1-5"},
		{"move_file", "../../data.json\nx"},
		{"copy_file", "/etc/passwd\nx"},
		{"mkdir", "../keluar"},
		{"env_file", "get ../../data.json"},
		{"import_project", "../../data.json"},
	}
	for _, c := range cases {
		out, _ := f.app.runTool(ctx, settings{}, s, c.tool, c.arg, true)
		if strings.Contains(out, "root:") || strings.Contains(out, secret) {
			t.Fatalf("tool %s arg %q membocorkan isi file sensitif: %q", c.tool, c.arg, out)
		}
	}
	// File di luar sandbox tidak tercipta.
	if _, err := os.Stat(filepath.Join(filepath.Dir(f.dir), "keluar.txt")); err == nil {
		t.Fatal("write_file traversal menciptakan file di luar sandbox")
	}
	// data.json utuh.
	if _, err := os.Stat(filepath.Join(f.dir, "data.json")); err != nil {
		t.Fatal("data.json hilang sesudah penyalahgunaan tool")
	}
}

func TestHeavySecretRedactionEverywhere(t *testing.T) {
	f := setup(t, nil)
	// Picu tool dengan argumen berisi secret (env_file set) lalu periksa log.
	f.app.mu.Lock()
	f.app.data.Settings.APIKey = secret
	f.app.mu.Unlock()
	_, _ = f.app.runTool(context.Background(), settings{}, seat{Code: "M01"}, "env_file", "set KUNCI="+secret, true)
	_, _ = f.app.runTool(context.Background(), settings{}, seat{Code: "M01"}, "password_gen", "16", false)

	for _, path := range []string{"/api/state", "/api/tool-log", "/api/backup", "/api/export?format=json", "/api/export?format=markdown", "/api/stats", "/api/tools"} {
		st, raw := heavyRaw(t, "GET", f.server.URL+path, nil, "", nil)
		if st != 200 {
			t.Fatalf("GET %s: %d", path, st)
		}
		if bytes.Contains(raw, []byte(secret)) {
			t.Fatalf("secret bocor di %s", path)
		}
	}
}

func TestHeavyExternalTokenAndWorkerConcurrent(t *testing.T) {
	f := setup(t, nil)
	base := f.server.URL
	out := f.api("POST", "/api/external-token/regenerate", object{}, 200)
	token := out["token"].(string)
	if token == "" {
		t.Fatal("token eksternal kosong")
	}
	// Token salah/kosong -> 401; token benar -> 202.
	for _, given := range []string{"", "salah", token + "x"} {
		st, _ := heavyRaw(t, "POST", base+"/api/external/message", []byte(`{"text":"halo"}`),
			"application/json", map[string]string{"Authorization": "Bearer " + given})
		if st != 401 {
			t.Fatalf("token eksternal %q: mau 401 dapat %d", given, st)
		}
	}
	st, _ := heavyRaw(t, "POST", base+"/api/external/message", []byte(`{"text":"@Plamo dari luar"}`),
		"application/json", map[string]string{"Authorization": "Bearer " + token})
	if st != 202 {
		t.Fatalf("token eksternal benar: mau 202 dapat %d", st)
	}
	// Token tidak bocor di state.
	if _, raw := heavyRaw(t, "GET", base+"/api/state", nil, "", nil); bytes.Contains(raw, []byte(token)) {
		t.Fatal("token eksternal bocor di state")
	}
}

func TestHeavyApprovalTimeoutAndReject(t *testing.T) {
	f := setup(t, nil)
	old := approvalTimeout
	approvalTimeout = 300 * time.Millisecond
	defer func() { approvalTimeout = old }()

	// Timeout: run_shell selalu butuh persetujuan; tanpa jawaban -> gagal rapi.
	start := time.Now()
	out, _ := f.app.runTool(context.Background(), settings{}, seat{Code: "M01"}, "run_shell", "echo halo", true)
	if time.Since(start) > 5*time.Second {
		t.Fatalf("approval timeout terlalu lama: %v", time.Since(start))
	}
	if !strings.Contains(out, "TIDAK dijalankan") {
		t.Fatalf("run_shell tanpa persetujuan harus ditolak: %q", out)
	}
	// Approval yang menggantung dibersihkan.
	if pending := f.app.pendingApprovals(); len(pending) != 0 {
		t.Fatalf("approval menggantung: %d", len(pending))
	}

	// Penolakan eksplisit lewat API.
	done := make(chan string, 1)
	go func() {
		o, _ := f.app.runTool(context.Background(), settings{}, seat{Code: "M01"}, "run_shell", "echo halo", true)
		done <- o
	}()
	deadline := time.Now().Add(3 * time.Second)
	var approvalID string
	for time.Now().Before(deadline) {
		if p := f.app.pendingApprovals(); len(p) == 1 {
			approvalID = p[0].ID
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if approvalID == "" {
		t.Fatal("approval tidak muncul")
	}
	f.api("POST", "/api/approvals/"+approvalID, object{"approved": false}, 200)
	select {
	case o := <-done:
		if !strings.Contains(o, "TIDAK dijalankan") {
			t.Fatalf("penolakan harus menggagalkan tool: %q", o)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("tool deadlock sesudah penolakan")
	}
}

func TestHeavyConcurrentLoginAttempts(t *testing.T) {
	f := setup(t, nil)
	base := f.server.URL
	if st, _ := heavyRaw(t, "POST", base+"/api/pin", []byte(`{"pin":"2468"}`), "application/json", nil); st != 200 {
		t.Fatal("set PIN gagal")
	}
	// 30 login salah bersamaan: tidak ada 500, rate limit akhirnya aktif.
	var wg sync.WaitGroup
	statuses := make(chan int, 30)
	for i := 0; i < 30; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			st, _ := heavyRaw(t, "POST", base+"/api/login", []byte(`{"pin":"0000"}`), "application/json", nil)
			statuses <- st
		}()
	}
	wg.Wait()
	close(statuses)
	saw429 := false
	for st := range statuses {
		if st >= 500 {
			t.Fatalf("login bersamaan -> %d", st)
		}
		if st == 429 {
			saw429 = true
		}
	}
	if !saw429 {
		t.Fatal("rate limit tidak aktif pada brute force bersamaan")
	}
}
