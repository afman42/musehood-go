package musehood

// Tes performa / beban / stres untuk jalur panas fitur pendalaman:
// benchmark PIN/sesi/log/cache/instruksi tool, state besar dengan log
// penuh, konkurensi rerun+fork+arsip, dan stres percobaan login PIN.

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

func BenchmarkVerifyPIN(b *testing.B) {
	hash, err := hashPIN("123456")
	if err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if !verifyPIN("123456", hash) {
			b.Fatal("verify gagal")
		}
	}
}

func BenchmarkSessionToken(b *testing.B) {
	hash, err := hashPIN("123456")
	if err != nil {
		b.Fatal(err)
	}
	expiry := time.Now().Add(7 * 24 * time.Hour)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		token := makeSessionToken(hash, expiry)
		if !validSessionToken(token, hash) {
			b.Fatal("token tidak sah")
		}
	}
}

func BenchmarkRecordToolLog(b *testing.B) {
	app := &App{data: data{}}
	s := seat{Code: "M01", Name: "Plamo"}
	arg := strings.Repeat("argumen panjang untuk diuji ", 20)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		app.recordToolLog(s, "web_search", arg, "hasil", time.Millisecond)
	}
}

func BenchmarkCacheGetHit(b *testing.B) {
	app := &App{cache: map[string]cacheEntry{}}
	cfg := settings{BaseURL: "https://gateway.test", GatewayProtocol: "openai"}
	history := []gatewayMessage{{"system", "sistem"}, {"user", "halo"}}
	key := responseCacheKey(cfg, "model", history)
	app.cachePut(key, "jawaban")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, ok := app.cacheGet(key); !ok {
			b.Fatal("cache miss tak terduga")
		}
	}
}

func BenchmarkCacheGetMiss(b *testing.B) {
	app := &App{cache: map[string]cacheEntry{}}
	cfg := settings{BaseURL: "https://gateway.test", GatewayProtocol: "openai"}
	history := []gatewayMessage{{"system", "sistem"}, {"user", "halo"}}
	key := responseCacheKey(cfg, "model", history)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, ok := app.cacheGet(key); ok {
			b.Fatal("cache hit tak terduga")
		}
	}
}

func BenchmarkBuildToolInstructionsAllowlist(b *testing.B) {
	app := &App{}
	s := seat{Code: "M01", DisabledTools: []string{"run_code", "run_shell", "delete_file", "http_post"}}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		out := app.buildToolInstructions(true, s)
		if out == "" {
			b.Fatal("instruksi kosong")
		}
	}
}

func TestLoadStateWithFullToolLog(t *testing.T) {
	if testing.Short() {
		t.Skip("tes beban dilewati pada -short")
	}
	f := setup(t, nil)
	f.app.mu.Lock()
	for i := 0; i < 3000; i++ {
		f.app.data.Messages = append(f.app.data.Messages, &message{
			ID: fmt.Sprintf("lm%d", i), AuthorType: "owner",
			Text:   fmt.Sprintf("pesan beban log nomor %d", i),
			Status: "completed", RoomID: defaultRoomID, CreatedAt: now(),
		})
	}
	for i := 0; i < maxToolLogEntries; i++ {
		f.app.data.ToolLog = append(f.app.data.ToolLog, toolLogEntry{
			Time: now(), SeatCode: "M01", SeatName: "Plamo", Tool: "web_search",
			Arg: strings.Repeat("kata kunci ", 10), OK: i%2 == 0, DurationMs: 5,
		})
	}
	if err := f.app.saveLocked(); err != nil {
		f.app.mu.Unlock()
		t.Fatal(err)
	}
	f.app.mu.Unlock()

	start := time.Now()
	status, raw := rawRequest(t, f, "GET", "/api/state", nil, nil)
	stateElapsed := time.Since(start)
	if status != 200 {
		t.Fatalf("state beban: %d", status)
	}
	t.Logf("state 3000 pesan + log penuh: %d byte dalam %v", len(raw), stateElapsed)
	if stateElapsed > 5*time.Second {
		t.Fatalf("state beban terlalu lambat: %v", stateElapsed)
	}

	start = time.Now()
	out := f.api("GET", "/api/tool-log?limit=500", nil, 200)
	logElapsed := time.Since(start)
	if entries, _ := out["entries"].([]any); len(entries) != maxToolLogEntries {
		t.Fatalf("tool-log harus %d entri: %v", maxToolLogEntries, out)
	}
	t.Logf("tool-log 500 entri dalam %v", logElapsed)
	if logElapsed > 5*time.Second {
		t.Fatalf("tool-log terlalu lambat: %v", logElapsed)
	}
}

func TestLoadConcurrentRerunForkArchive(t *testing.T) {
	if testing.Short() {
		t.Skip("tes beban dilewati pada -short")
	}
	f := setup(t, nil)
	// Tiga ruang masing-masing dengan satu diskusi selesai.
	ownerIDs := []string{}
	roomIDs := []string{defaultRoomID}
	f.api("POST", "/api/message", object{"text": "@Plamo konkuren nol"}, 202)
	f.wait(func(s object) bool { return completedSeatReplies(s) >= 1 })
	ownerIDs = append(ownerIDs, findOwnerMessage(f.state(), "@Plamo konkuren nol")["id"].(string))
	for i := 1; i <= 2; i++ {
		room := f.api("POST", "/api/rooms", object{"name": fmt.Sprintf("Konkuren %d", i)}, 201)["activeRoomId"].(string)
		roomIDs = append(roomIDs, room)
		text := fmt.Sprintf("@Plamo konkuren %d", i)
		f.api("POST", "/api/message", object{"text": text}, 202)
		f.wait(func(s object) bool { return completedSeatReplies(s) >= 1 })
		ownerIDs = append(ownerIDs, findOwnerMessage(f.state(), text)["id"].(string))
	}
	f.api("POST", "/api/rooms/"+defaultRoomID+"/activate", object{}, 200)

	failures := make(chan string, 128)
	var wg sync.WaitGroup
	run := func(fn func() (int, []byte)) {
		defer wg.Done()
		status, raw := fn()
		if status >= 500 {
			failures <- fmt.Sprintf("status %d: %s", status, raw)
		}
	}
	// Fork dari ketiga ruang, rerun dua ruang, arsip+buka ruang ketiga,
	// kirim pesan dan baca state bersamaan.
	for i := 0; i < 3; i++ {
		i := i
		wg.Add(1)
		go run(func() (int, []byte) {
			return rawRequest(t, f, "POST", "/api/message/"+ownerIDs[i]+"/fork", mustJSON(object{}), nil)
		})
	}
	for i := 0; i < 2; i++ {
		i := i
		wg.Add(1)
		go run(func() (int, []byte) {
			return rawRequest(t, f, "POST", "/api/message/"+ownerIDs[i]+"/rerun", mustJSON(object{}), nil)
		})
	}
	wg.Add(1)
	go run(func() (int, []byte) {
		status, raw := rawRequest(t, f, "POST", "/api/rooms/"+roomIDs[2]+"/archive", mustJSON(object{"archived": true}), nil)
		if status >= 500 {
			return status, raw
		}
		return rawRequest(t, f, "POST", "/api/rooms/"+roomIDs[2]+"/archive", mustJSON(object{"archived": false}), nil)
	})
	for i := 0; i < 6; i++ {
		i := i
		wg.Add(1)
		go run(func() (int, []byte) {
			return rawRequest(t, f, "POST", "/api/message", mustJSON(object{"text": fmt.Sprintf("@Plamo pesan konkuren %d", i)}), nil)
		})
		wg.Add(1)
		go run(func() (int, []byte) {
			return rawRequest(t, f, "GET", "/api/state", nil, nil)
		})
	}
	wg.Wait()
	close(failures)
	for failure := range failures {
		t.Fatal(failure)
	}
	// Data di disk tetap bisa dimuat dan konsisten sesudah badai konkuren.
	loaded, err := loadData(f.dir)
	if err != nil {
		t.Fatalf("data korup sesudah operasi konkuren: %v", err)
	}
	if len(loaded.Rooms) < 3 {
		t.Fatalf("ruang hilang sesudah operasi konkuren: %d", len(loaded.Rooms))
	}
	// Server tetap responsif.
	f.state()
}

func TestStressPINLoginAttempts(t *testing.T) {
	if testing.Short() {
		t.Skip("tes stres dilewati pada -short")
	}
	f := setup(t, nil)
	base := f.server.URL
	if status, _, _ := doRaw(t, "POST", base+"/api/pin", object{"pin": "1357"}, nil); status != 200 {
		t.Fatal("set PIN gagal")
	}
	start := time.Now()
	for i := 0; i < 50; i++ {
		// IP berbeda per percobaan agar tidak kena rate limit per IP
		// (fitur 28); perilaku blokir diuji di TestLoginRateLimitAndAudit.
		from := map[string]string{"X-Forwarded-For": fmt.Sprintf("198.51.100.%d", i+1)}
		status, _, raw := doRaw(t, "POST", base+"/api/login", object{"pin": fmt.Sprintf("salah-%d", i)}, from)
		if status != 401 {
			t.Fatalf("percobaan %d: mau 401, dapat %d %s", i, status, raw)
		}
	}
	t.Logf("50 login salah dalam %v", time.Since(start))
	// Server tidak terkunci: login benar tetap bekerja.
	auth := loginCookie(t, f, "1357")
	if status, _, _ := doRaw(t, "GET", base+"/api/state", nil, auth); status != 200 {
		t.Fatalf("state sesudah stres login = %d", status)
	}
	// Dan PIN salah sesudahnya tetap ditolak (tidak ada bypass karena stres).
	if status, _, _ := doRaw(t, "POST", base+"/api/login", object{"pin": "salah-lagi"}, nil); status != 401 {
		t.Fatalf("login salah sesudah stres = %d", status)
	}
}
