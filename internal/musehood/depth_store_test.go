package musehood

// Tes penyimpanan untuk field fitur pendalaman: roundtrip field baru,
// default aman untuk data lama, cap log audit setelah reload, data rusak
// sebagian, penulisan atomik bersamaan, dan ketahanan PIN/token pekerja
// melewati restart App.

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestStoreDepthFieldsRoundtrip(t *testing.T) {
	dir := t.TempDir()
	d, err := loadData(dir)
	if err != nil {
		t.Fatal(err)
	}
	pinHash, err := hashPIN("9876")
	if err != nil {
		t.Fatal(err)
	}
	cacheOn := true
	chainOn := true
	d.Settings.PINHash = pinHash
	d.Settings.ResponseCacheEnabled = &cacheOn
	d.Settings.ChainReplies = &chainOn
	d.Settings.Density = "compact"
	d.WorkerToken = "token-pekerja-uji"
	d.ToolLog = []toolLogEntry{
		{Time: now(), SeatCode: "M01", SeatName: "Plamo", Tool: "web_search", Arg: "berita", OK: true, DurationMs: 42},
		{Time: now(), SeatCode: "M02", SeatName: "GLM", Tool: "run_code", Arg: "python\nprint(1)", OK: false, DurationMs: 7},
	}
	d.Rooms = append(d.Rooms, room{ID: "room-depth", Name: "Depth", CreatedAt: now(),
		SeatCodes: []string{"M01", "M02"}, SeatOrder: []string{"M02", "M01"}, Archived: true})
	d.Messages = append(d.Messages, &message{ID: "md1", AuthorType: "seat", Seat: "M01", Text: "halo",
		Status: "completed", RoomID: defaultRoomID, CreatedAt: now(),
		Reactions: []reaction{{Emoji: "👍", Count: 1, ByOwner: true}}})
	if err := writeJSONFile(filepath.Join(dir, "data.json"), d); err != nil {
		t.Fatal(err)
	}
	loaded, err := loadData(dir)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Settings.PINHash != pinHash {
		t.Fatalf("PINHash roundtrip = %q", loaded.Settings.PINHash)
	}
	if !loaded.Settings.responseCacheOn() || !loaded.Settings.chainRepliesOn() {
		t.Fatal("toggle cache/chain harus bertahan")
	}
	if loaded.Settings.Density != "compact" {
		t.Fatalf("density = %q", loaded.Settings.Density)
	}
	if loaded.WorkerToken != "token-pekerja-uji" {
		t.Fatalf("WorkerToken = %q", loaded.WorkerToken)
	}
	if len(loaded.ToolLog) != 2 || loaded.ToolLog[0].Tool != "web_search" || loaded.ToolLog[1].OK {
		t.Fatalf("ToolLog roundtrip = %+v", loaded.ToolLog)
	}
	r := loaded.findRoom("room-depth")
	if r == nil || !r.Archived || fmt.Sprint(r.SeatOrder) != "[M02 M01]" {
		t.Fatalf("ruang depth roundtrip = %+v", r)
	}
	if len(loaded.Messages) != 1 || len(loaded.Messages[0].Reactions) != 1 || loaded.Messages[0].Reactions[0].Emoji != "👍" {
		t.Fatalf("reaksi roundtrip = %+v", loaded.Messages)
	}
	if !verifyPIN("9876", loaded.Settings.PINHash) {
		t.Fatal("PIN hasil roundtrip harus terverifikasi")
	}
}

func TestStoreLegacyDefaultsSafe(t *testing.T) {
	dir := t.TempDir()
	d, err := loadData(dir)
	if err != nil {
		t.Fatal(err)
	}
	// Simulasikan data lama: semua field pendalaman dikosongkan dari JSON.
	raw, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	var asMap map[string]any
	if err := json.Unmarshal(raw, &asMap); err != nil {
		t.Fatal(err)
	}
	settingsMap, _ := asMap["settings"].(map[string]any)
	for _, key := range []string{"pinHash", "responseCacheEnabled", "chainReplies", "density"} {
		delete(settingsMap, key)
	}
	delete(asMap, "workerToken")
	delete(asMap, "toolLog")
	rooms, _ := asMap["rooms"].([]any)
	for _, item := range rooms {
		rm, _ := item.(map[string]any)
		delete(rm, "seatOrder")
		delete(rm, "archived")
	}
	legacy, err := json.Marshal(asMap)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "data.json"), legacy, 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := loadData(dir)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Settings.PINHash != "" {
		t.Fatal("data lama harus tanpa PIN (akses bebas seperti dulu)")
	}
	if loaded.Settings.responseCacheOn() {
		t.Fatal("cache harus MATI secara bawaan untuk data lama")
	}
	if loaded.Settings.chainRepliesOn() {
		t.Fatal("balasan berantai harus MATI secara bawaan untuk data lama")
	}
	if loaded.Settings.Density != "" && loaded.Settings.Density != "normal" {
		t.Fatalf("density data lama harus kosong/normal, dapat %q", loaded.Settings.Density)
	}
	if len(loaded.ToolLog) != 0 {
		t.Fatal("ToolLog data lama harus kosong")
	}
	for _, r := range loaded.Rooms {
		if r.Archived {
			t.Fatal("ruang data lama tidak boleh terarsip")
		}
		if len(r.SeatOrder) != 0 {
			t.Fatal("SeatOrder data lama harus kosong")
		}
	}
}

func TestStoreToolLogCapAfterReload(t *testing.T) {
	dir := t.TempDir()
	d, err := loadData(dir)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 600; i++ {
		d.ToolLog = append(d.ToolLog, toolLogEntry{Time: now(), SeatCode: "M01", Tool: "uji", Arg: fmt.Sprintf("arg-%d", i), OK: true})
	}
	if err := writeJSONFile(filepath.Join(dir, "data.json"), d); err != nil {
		t.Fatal(err)
	}
	loaded, err := loadData(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.ToolLog) != 600 {
		t.Fatalf("reload harus mempertahankan isi file apa adanya, dapat %d", len(loaded.ToolLog))
	}
	// Pencatatan berikutnya menegakkan cap 500 kembali (FIFO).
	app := &App{dir: dir, data: loaded}
	app.recordToolLog(seat{Code: "M01", Name: "Plamo"}, "uji", "arg-baru", "ok", 0)
	if len(app.data.ToolLog) != maxToolLogEntries {
		t.Fatalf("cap harus %d setelah pencatatan, dapat %d", maxToolLogEntries, len(app.data.ToolLog))
	}
	if app.data.ToolLog[len(app.data.ToolLog)-1].Arg != "arg-baru" {
		t.Fatal("entri terbaru harus di akhir")
	}
	if app.data.ToolLog[0].Arg != "arg-101" {
		t.Fatalf("entri tertua tersisa harus arg-101, dapat %q", app.data.ToolLog[0].Arg)
	}
}

func TestStorePartialCorruptNoPanic(t *testing.T) {
	base, err := loadData(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	baseRaw, _ := json.Marshal(base)
	cases := map[string]func(map[string]any){
		"toolLog bukan array": func(m map[string]any) { m["toolLog"] = "bukan-array" },
		"pinHash angka": func(m map[string]any) {
			m["settings"].(map[string]any)["pinHash"] = 12345
		},
		"seatOrder bukan string": func(m map[string]any) {
			rooms := m["rooms"].([]any)
			rooms[0].(map[string]any)["seatOrder"] = []any{1, 2}
		},
		"archived string": func(m map[string]any) {
			rooms := m["rooms"].([]any)
			rooms[0].(map[string]any)["archived"] = "ya"
		},
		"density angka": func(m map[string]any) {
			m["settings"].(map[string]any)["density"] = 3
		},
		"cache string": func(m map[string]any) {
			m["settings"].(map[string]any)["responseCacheEnabled"] = "nyala"
		},
	}
	for name, mutate := range cases {
		var asMap map[string]any
		if err := json.Unmarshal(baseRaw, &asMap); err != nil {
			t.Fatal(err)
		}
		mutate(asMap)
		raw, _ := json.Marshal(asMap)
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "data.json"), raw, 0o600); err != nil {
			t.Fatal(err)
		}
		// Harus error yang jelas, bukan panic.
		if _, err := loadData(dir); err == nil {
			t.Fatalf("%s: loadData harus gagal", name)
		}
	}
	// JSON terpotong di tengah juga error, bukan panic.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "data.json"), baseRaw[:len(baseRaw)/2], 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadData(dir); err == nil {
		t.Fatal("JSON terpotong harus gagal dimuat")
	}
}

func TestStoreConcurrentSaveAndLoadAtomic(t *testing.T) {
	f := setup(t, nil)
	var wg sync.WaitGroup
	errs := make(chan error, 64)
	// Penulis: catat log tool + simpan berulang.
	for w := 0; w < 4; w++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for i := 0; i < 15; i++ {
				f.app.recordToolLog(seat{Code: "M01", Name: "Plamo"}, "uji", fmt.Sprintf("w%d-%d", n, i), "ok", 0)
				f.app.mu.Lock()
				err := f.app.saveLocked()
				f.app.mu.Unlock()
				if err != nil {
					errs <- err
					return
				}
			}
		}(w)
	}
	// Pembaca: muat file berulang; tidak boleh pernah korup/parsial.
	for r := 0; r < 3; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 20; i++ {
				if _, err := loadData(f.dir); err != nil {
					errs <- fmt.Errorf("load saat save konkuren: %w", err)
					return
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	loaded, err := loadData(f.dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.ToolLog) == 0 {
		t.Fatal("ToolLog hasil save konkuren kosong")
	}
	entries, _ := os.ReadDir(f.dir)
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".tmp") {
			t.Fatalf("file temp tertinggal: %s", entry.Name())
		}
	}
}

func TestStorePINAndWorkerTokenSurviveRestart(t *testing.T) {
	f := setup(t, nil)
	base := f.server.URL
	if status, _, _ := doRaw(t, "POST", base+"/api/pin", object{"pin": "7777"}, nil); status != 200 {
		t.Fatal("set PIN gagal")
	}
	f.app.mu.Lock()
	pinBefore := f.app.data.Settings.PINHash
	tokenBefore := f.app.data.WorkerToken
	f.app.mu.Unlock()
	if pinBefore == "" || tokenBefore == "" {
		t.Fatal("PIN/token harus terisi sebelum restart")
	}

	f.server.Close()
	f.app.Close()
	app, err := New(Options{DataDir: f.dir, PollInterval: 5 * time.Millisecond, MuseWait: 150 * time.Millisecond, TrustedHosts: "muse.test"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(app.Close)
	server := httptest.NewServer(app)
	t.Cleanup(server.Close)

	app.mu.Lock()
	pinAfter := app.data.Settings.PINHash
	tokenAfter := app.data.WorkerToken
	app.mu.Unlock()
	if pinAfter != pinBefore {
		t.Fatal("PINHash harus bertahan melewati restart")
	}
	if tokenAfter != tokenBefore {
		t.Fatal("WorkerToken harus bertahan melewati restart (tidak digenerate ulang)")
	}
	// PIN lama tetap bisa login setelah restart; file token konsisten.
	status, header, _ := doRaw(t, "POST", server.URL+"/api/login", object{"pin": "7777"}, nil)
	if status != 200 {
		t.Fatalf("login setelah restart = %d", status)
	}
	auth := map[string]string{"Cookie": sessionCookie(t, header)}
	if status, _, _ := doRaw(t, "GET", server.URL+"/api/state", nil, auth); status != 200 {
		t.Fatalf("state setelah restart+login = %d", status)
	}
	tokenFile, err := os.ReadFile(filepath.Join(f.dir, "worker-token.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(tokenFile)) != tokenBefore {
		t.Fatal("worker-token.txt harus sama dengan token sebelum restart")
	}
	info, err := os.Stat(filepath.Join(f.dir, "worker-token.txt"))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("worker-token.txt harus 0600: %v %v", info, err)
	}
}
