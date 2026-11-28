package musehood

// Tes berat ketahanan data (27 Nov 2026): SQLite 1000 pesan, migrasi data
// besar, data.db rusak, restart di tengah job, sapu metode endpoint.

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"
)

func TestHeavySQLiteThousandMessages(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("MUSEHOOD_STORAGE", "sqlite")
	store, err := openStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	d, err := store.load()
	if err != nil {
		t.Fatal(err)
	}
	roomID := d.Rooms[0].ID
	for i := 0; i < 1000; i++ {
		d.Messages = append(d.Messages, &message{
			ID: fmt.Sprintf("sqlite-%04d", i), CreatedAt: now(), AuthorType: "owner",
			Text:   fmt.Sprintf("Pesan sqlite nomor %d dengan isi yang cukup panjang.", i),
			Status: "completed", RoomID: roomID, BranchID: "main",
		})
	}
	start := time.Now()
	if err := store.save(d); err != nil {
		t.Fatal(err)
	}
	saveDur := time.Since(start)

	// Buka ulang dari disk (store baru).
	store2, err := openStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	start = time.Now()
	again, err := store2.load()
	if err != nil {
		t.Fatal(err)
	}
	loadDur := time.Since(start)
	t.Logf("SQLite save 1000 pesan: %v, load: %v", saveDur, loadDur)
	if len(again.Messages) != 1000 {
		t.Fatalf("pesan sesudah reload %d, mau 1000", len(again.Messages))
	}
	if again.Messages[999].Text != "Pesan sqlite nomor 999 dengan isi yang cukup panjang." {
		t.Fatalf("isi pesan terakhir rusak: %q", again.Messages[999].Text)
	}
	// Simpan lagi sesudah reload tetap utuh (tidak korup berantai).
	if err := store2.save(again); err != nil {
		t.Fatal(err)
	}
	third, err := store2.load()
	if err != nil || len(third.Messages) != 1000 {
		t.Fatalf("reload kedua: %v %d pesan", err, len(third.Messages))
	}
}

func TestHeavySQLiteMigrationLargeJSON(t *testing.T) {
	dir := t.TempDir()
	// Tulis data.json besar lewat jsonStore.
	js := jsonStore{dir: dir}
	d, err := js.load()
	if err != nil {
		t.Fatal(err)
	}
	roomID := d.Rooms[0].ID
	for i := 0; i < 500; i++ {
		d.Messages = append(d.Messages, &message{
			ID: fmt.Sprintf("migrasi-%04d", i), CreatedAt: now(), AuthorType: "owner",
			Text: strings.Repeat("isi migrasi ", 20), Status: "completed",
			RoomID: roomID, BranchID: "main",
		})
	}
	if err := js.save(d); err != nil {
		t.Fatal(err)
	}
	// Buka dengan SQLite: migrasi otomatis dari data.json.
	t.Setenv("MUSEHOOD_STORAGE", "sqlite")
	store, err := openStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	got, err := store.load()
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Messages) != 500 {
		t.Fatalf("pesan termigrasi %d, mau 500", len(got.Messages))
	}
	// data.json dipertahankan sebagai cadangan.
	if _, err := os.Stat(filepath.Join(dir, "data.json")); err != nil {
		t.Fatal("data.json cadangan hilang sesudah migrasi")
	}
	if _, err := os.Stat(filepath.Join(dir, "data.db")); err != nil {
		t.Fatal("data.db tidak dibuat")
	}
}

func TestHeavyCorruptSQLiteFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "data.db"), []byte("ini bukan file sqlite sama sekali, hanya teks acak 12345"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MUSEHOOD_STORAGE", "sqlite")
	_, err := openStore(dir)
	if err == nil {
		// Bila open lolos (lazy), load harus gagal jelas.
		store, _ := openStore(dir)
		if _, lerr := store.load(); lerr == nil {
			t.Fatal("data.db rusak harus menghasilkan error jelas")
		} else {
			t.Logf("error load data.db rusak (diharapkan): %v", lerr)
		}
		return
	}
	t.Logf("error open data.db rusak (diharapkan): %v", err)
}

func TestHeavyRestartMidJob(t *testing.T) {
	dir := t.TempDir()
	var calls atomic.Int32
	release := make(chan struct{})
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+secret {
			http.Error(w, "wrong authorization", 401)
			return
		}
		if r.URL.Path == "/v1/chat/completions" {
			if calls.Add(1) == 1 {
				<-release // panggilan pertama menggantung (simulasi job berjalan saat restart)
			}
			w.Write([]byte(`{"choices":[{"message":{"content":"balasan sesudah restart"}}]}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer gateway.Close()

	opts := Options{DataDir: dir, Frontend: fstest.MapFS{"index.html": {Data: []byte("<!doctype html>Musehood")}},
		PollInterval: 5 * time.Millisecond, MuseWait: 150 * time.Millisecond, TrustedHosts: "muse.test"}
	app1, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	srv1 := httptest.NewServer(app1)
	f1 := fixture{t: t, server: srv1, dir: dir, app: app1}
	f1.api("POST", "/api/settings", object{"settings": object{"baseUrl": gateway.URL + "/v1/", "apiKey": secret, "streamingEnabled": false},
		"seats": []object{{"code": "M01", "model": "plain"}}}, 200)
	f1.api("POST", "/api/message", object{"text": "@Plamo halo restart"}, 202)
	// Tunggu panggilan gateway pertama masuk (job processing), lalu "restart".
	deadline := time.Now().Add(3 * time.Second)
	for calls.Load() < 1 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if calls.Load() < 1 {
		t.Fatal("gateway tidak dipanggil")
	}
	srv1.Close()
	app1.Close()
	close(release)

	// Buka lagi: job tidak boleh menggantung selamanya.
	app2, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	defer app2.Close()
	srv2 := httptest.NewServer(app2)
	defer srv2.Close()
	f2 := fixture{t: t, server: srv2, dir: dir, app: app2}
	s := f2.wait(func(s object) bool {
		for _, j := range asObjects(s["jobs"]) {
			if j["status"] != "completed" {
				return false
			}
		}
		for _, m := range messages(s) {
			if m["authorType"] == "seat" && m["status"] == "completed" && strings.Contains(m["text"].(string), "balasan sesudah restart") {
				return true
			}
		}
		return false
	})
	_ = s
}

func TestHeavyEndpointMethodSweep(t *testing.T) {
	f := setup(t, nil)
	base := f.server.URL
	// Top-level: metode salah -> 405.
	topLevel := []struct{ path, wrong string }{
		{"/api/state", "DELETE"}, {"/api/rooms", "DELETE"}, {"/api/settings", "GET"},
		{"/api/message", "GET"}, {"/api/stats", "POST"}, {"/api/search", "POST"},
		{"/api/tools", "POST"}, {"/api/skills", "DELETE"}, {"/api/personas", "POST"},
		{"/api/roles", "POST"}, {"/api/chains", "DELETE"}, {"/api/tool-log", "POST"},
		{"/api/providers", "DELETE"}, {"/api/templates", "DELETE"}, {"/api/presets", "DELETE"},
		{"/api/scheduled", "DELETE"}, {"/api/backup", "POST"}, {"/api/export", "POST"},
		{"/api/login", "GET"}, {"/api/logout", "GET"}, {"/api/pin", "GET"},
		{"/api/debate", "GET"}, {"/api/abtest", "GET"}, {"/api/vote", "GET"},
		{"/api/clear", "GET"}, {"/api/progress", "GET"}, {"/api/queue/muse", "DELETE"},
		{"/api/update/check", "POST"}, {"/api/update/apply", "GET"},
		{"/api/ollama/detect", "GET"}, {"/api/ollama/add", "GET"},
		{"/api/external/message", "GET"}, {"/api/external-token/regenerate", "GET"},
	}
	for _, tc := range topLevel {
		st, _ := heavyRaw(t, tc.wrong, base+tc.path, nil, "", nil)
		if st != 405 && st != 404 {
			t.Fatalf("%s %s: mau 405/404 dapat %d", tc.wrong, tc.path, st)
		}
		if st >= 500 {
			t.Fatalf("%s %s -> %d", tc.wrong, tc.path, st)
		}
	}
	// Sub-route ber-ID: metode salah -> 404.
	subRoutes := []string{
		"/api/message/abc/delete", "/api/message/abc/edit", "/api/message/abc/rerun",
		"/api/message/abc/fork", "/api/message/abc/branch", "/api/message/abc/tags",
		"/api/message/abc/rate", "/api/message/abc/react", "/api/message/abc/pin",
		"/api/rooms/abc/archive", "/api/rooms/abc/participants", "/api/rooms/abc/documents",
		"/api/providers/abc/activate", "/api/providers/abc/models", "/api/providers/abc/health",
		"/api/chains/abc/run", "/api/abtest/abc/pick", "/api/seats/ABC/clone",
		"/api/approvals/abc", "/api/queue/muse/abc",
	}
	for _, path := range subRoutes {
		for _, method := range []string{"GET", "PUT", "DELETE", "PATCH"} {
			st, _ := heavyRaw(t, method, base+path, nil, "", nil)
			if st >= 500 {
				t.Fatalf("%s %s -> %d", method, path, st)
			}
			if st != 404 && st != 405 && st != 401 {
				t.Fatalf("%s %s: mau 404/405 dapat %d", method, path, st)
			}
		}
	}
}
