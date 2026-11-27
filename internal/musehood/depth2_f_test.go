package musehood

// Tes fitur pendalaman grup F: webhook keluar (41), API eksternal
// ber-token (42), jembatan Telegram (43), auto-update (45).

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestWebhookOnCompleted(t *testing.T) {
	received := make(chan map[string]any, 4)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		_ = json.NewDecoder(r.Body).Decode(&payload)
		received <- payload
	}))
	defer server.Close()
	f := setup(t, nil)
	// URL privat non-localhost ditolak saat menyimpan pengaturan.
	f.api("POST", "/api/settings", object{"settings": object{"webhookUrl": "http://192.168.1.5/hook"}}, 400)
	f.api("POST", "/api/settings", object{"settings": object{"webhookUrl": server.URL, "webhookEnabled": true}}, 200)
	f.api("POST", "/api/message", object{"text": "@M01 halo webhook"}, 202)
	f.finish(2)
	select {
	case payload := <-received:
		if payload["event"] != "message.completed" || payload["seat"] != "M01" {
			t.Fatal(payload)
		}
		if text, _ := payload["text"].(string); text == "" {
			t.Fatal(payload)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("webhook tidak terkirim")
	}
}

func TestExternalAPIToken(t *testing.T) {
	f := setup(t, nil)
	base := f.server.URL
	out := f.api("POST", "/api/external-token/regenerate", nil, 200)
	token, _ := out["token"].(string)
	if len(token) != 64 {
		t.Fatal("token tidak valid:", out)
	}
	// Token benar → 202 dan pekerjaan berjalan.
	status, _, raw := doRaw(t, "POST", base+"/api/external/message", object{"text": "@M01 dari luar"}, map[string]string{"Authorization": "Bearer " + token})
	if status != 202 {
		t.Fatalf("token benar harus 202, dapat %d %s", status, raw)
	}
	f.finish(2)
	// Token salah / tanpa token → 401.
	if status, _, _ := doRaw(t, "POST", base+"/api/external/message", object{"text": "x"}, map[string]string{"Authorization": "Bearer salah"}); status != 401 {
		t.Fatalf("token salah harus 401, dapat %d", status)
	}
	if status, _, _ := doRaw(t, "POST", base+"/api/external/message", object{"text": "x"}, nil); status != 401 {
		t.Fatalf("tanpa token harus 401, dapat %d", status)
	}
	// Token tidak pernah tampil di state.
	s := f.state()
	rawState, _ := json.Marshal(s)
	if strings.Contains(string(rawState), token) {
		t.Fatal("token API bocor di state")
	}
	// Dengan PIN aktif, endpoint eksternal tetap bisa memakai token.
	f.api("POST", "/api/pin", object{"pin": "1234"}, 200)
	if status, _, _ := doRaw(t, "POST", base+"/api/external/message", object{"text": "@M01 lagi"}, map[string]string{"Authorization": "Bearer " + token}); status != 202 {
		t.Fatalf("dengan PIN aktif token benar harus 202, dapat %d", status)
	}
}

func TestTelegramBridge(t *testing.T) {
	var mu sync.Mutex
	sent := []map[string]any{}
	polls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "getUpdates") {
			mu.Lock()
			polls++
			first := polls == 1
			mu.Unlock()
			if first {
				io.WriteString(w, `{"ok":true,"result":[{"update_id":10,"message":{"text":"@M01 halo telegram","chat":{"id":12345}}},{"update_id":11,"message":{"text":"dari chat lain","chat":{"id":999}}}]}`)
				return
			}
			io.WriteString(w, `{"ok":true,"result":[]}`)
			return
		}
		if strings.Contains(r.URL.Path, "sendMessage") {
			var payload map[string]any
			_ = json.NewDecoder(r.Body).Decode(&payload)
			mu.Lock()
			sent = append(sent, payload)
			mu.Unlock()
			io.WriteString(w, `{"ok":true}`)
			return
		}
		w.WriteHeader(404)
	}))
	defer server.Close()
	origBase, origTimeout := telegramBaseURL, telegramPollTimeout
	telegramBaseURL, telegramPollTimeout = server.URL, 1
	t.Cleanup(func() { telegramBaseURL, telegramPollTimeout = origBase, origTimeout })

	f := setup(t, nil)
	f.api("POST", "/api/settings", object{"settings": object{"telegramBotToken": "tok-uji", "telegramChatId": "12345", "telegramEnabled": true}}, 200)
	// Pesan dari chat terdaftar menjadi pesan pemilik dan dijawab kursi.
	f.wait(func(s object) bool {
		for _, m := range messages(s) {
			if text, _ := m["text"].(string); strings.Contains(text, "halo telegram") {
				return true
			}
		}
		return false
	})
	f.finish(2)
	// Balasan kursi terkirim kembali ke Telegram.
	deadline := time.Now().Add(6 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		count := len(sent)
		mu.Unlock()
		if count > 0 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(sent) == 0 {
		t.Fatal("balasan tidak terkirim ke Telegram")
	}
	if sent[0]["chat_id"] != "12345" {
		t.Fatal(sent[0])
	}
	// Token tidak tampil di state.
	s := f.state()
	rawState, _ := json.Marshal(s)
	if strings.Contains(string(rawState), "tok-uji") {
		t.Fatal("token Telegram bocor di state")
	}
}

func TestUpdateCheckAndApply(t *testing.T) {
	assetName := updateAssetName()
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/releases/latest") {
			io.WriteString(w, `{"tag_name":"v9.9.9","assets":[{"name":"`+assetName+`","browser_download_url":"`+server.URL+`/download"}]}`)
			return
		}
		if r.URL.Path == "/download" {
			io.WriteString(w, "BINARY-BARU")
			return
		}
		w.WriteHeader(404)
	}))
	defer server.Close()
	origBase, origRestart, origExe := githubAPIBase, restartProcess, executablePath
	githubAPIBase = server.URL
	restarted := ""
	restartProcess = func(path string) error { restarted = path; return nil }
	exeDir := t.TempDir()
	exePath := filepath.Join(exeDir, "musehood-go")
	if err := os.WriteFile(exePath, []byte("BINARY-LAMA"), 0o755); err != nil {
		t.Fatal(err)
	}
	executablePath = func() (string, error) { return exePath, nil }
	t.Cleanup(func() { githubAPIBase, restartProcess, executablePath = origBase, origRestart, origExe })

	f := setup(t, nil)
	f.app.version = "v1.0.0"
	out := f.api("GET", "/api/update/check", nil, 200)
	if out["updateAvailable"] != true || out["latest"] != "v9.9.9" || out["assetName"] != assetName {
		t.Fatal(out)
	}
	out = f.api("POST", "/api/update/apply", nil, 200)
	if out["ok"] != true {
		t.Fatal(out)
	}
	if restarted != exePath {
		t.Fatal("restart tidak dipanggil ke binary baru:", restarted)
	}
	raw, err := os.ReadFile(exePath)
	if err != nil || string(raw) != "BINARY-BARU" {
		t.Fatalf("binary tidak terganti: %q %v", raw, err)
	}
	bak, err := os.ReadFile(exePath + ".bak")
	if err != nil || string(bak) != "BINARY-LAMA" {
		t.Fatalf("cadangan .bak salah: %q %v", bak, err)
	}
}
