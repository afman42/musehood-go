package musehood

// Tes unit untuk fitur pendalaman: hash PIN, token sesi, kunci/TTL/cap
// cache respons, validasi parameter tool kustom, definisi persona,
// filter urutan kursi ruang, dan toggle reaksi.

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestUnitPINHashVerify(t *testing.T) {
	first, err := hashPIN("1234")
	if err != nil {
		t.Fatal(err)
	}
	second, err := hashPIN("1234")
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("salt acak harus membuat hash berbeda untuk PIN yang sama")
	}
	salt1, _, _ := strings.Cut(first, ":")
	salt2, _, _ := strings.Cut(second, ":")
	if salt1 == salt2 {
		t.Fatal("salt harus berbeda antar hash")
	}
	if !verifyPIN("1234", first) || !verifyPIN("1234", second) {
		t.Fatal("PIN benar harus terverifikasi")
	}
	for _, wrong := range []string{"1235", "", "1234 ", " 1234", "12345"} {
		if verifyPIN(wrong, first) {
			t.Fatalf("PIN %q tidak boleh terverifikasi", wrong)
		}
	}
	// Format tersimpan rusak selalu ditolak, tidak panic.
	for _, stored := range []string{"", "tanpa-titik-dua", "zz:abc", ":abc", "abc:", ":", "a:b:c"} {
		if verifyPIN("1234", stored) {
			t.Fatalf("hash tersimpan %q tidak boleh terverifikasi", stored)
		}
	}
	// Hash yang dimodifikasi satu karakter ditolak.
	tampered := first[:len(first)-1] + "0"
	if tampered == first {
		tampered = first[:len(first)-1] + "1"
	}
	if verifyPIN("1234", tampered) {
		t.Fatal("hash dimodifikasi tidak boleh terverifikasi")
	}
}

func TestUnitSessionToken(t *testing.T) {
	hash, err := hashPIN("4321")
	if err != nil {
		t.Fatal(err)
	}
	token := makeSessionToken(hash, time.Now().Add(time.Hour))
	if !validSessionToken(token, hash) {
		t.Fatal("token sah harus valid")
	}
	// Kedaluwarsa.
	if validSessionToken(makeSessionToken(hash, time.Now().Add(-time.Second)), hash) {
		t.Fatal("token kedaluwarsa harus tidak valid")
	}
	// Tanda tangan dimodifikasi.
	exp, sig, _ := strings.Cut(token, ".")
	badSig := sig[:len(sig)-1] + "0"
	if badSig == sig {
		badSig = sig[:len(sig)-1] + "1"
	}
	if validSessionToken(exp+"."+badSig, hash) {
		t.Fatal("tanda tangan dimodifikasi harus tidak valid")
	}
	// Masa berlaku digeser tanpa tanda tangan baru.
	if validSessionToken("9999999999."+sig, hash) {
		t.Fatal("expiry digeser harus tidak valid")
	}
	// Hash PIN berbeda (PIN diganti) membatalkan token lama.
	otherHash, _ := hashPIN("8765")
	if validSessionToken(token, otherHash) {
		t.Fatal("token dari PIN lama harus tidak valid setelah PIN diganti")
	}
	// Bentuk rusak.
	for _, bad := range []string{"", ".", "abc", "abc.def", "12a3." + sig, "." + sig, exp + "."} {
		if validSessionToken(bad, hash) {
			t.Fatalf("token %q harus tidak valid", bad)
		}
	}
	if validSessionToken(token, "") {
		t.Fatal("hash kosong harus tidak valid")
	}
}

func TestUnitResponseCacheKey(t *testing.T) {
	cfg := settings{BaseURL: "https://gw.test/v1", GatewayProtocol: "openai"}
	history := []gatewayMessage{{"system", "sistem"}, {"user", "halo"}}
	base := responseCacheKey(cfg, "model-a", history)
	if base == "" || base != responseCacheKey(cfg, "model-a", history) {
		t.Fatal("kunci harus stabil untuk input sama")
	}
	variants := map[string]string{
		"model beda":   responseCacheKey(cfg, "model-b", history),
		"sistem beda":  responseCacheKey(cfg, "model-a", []gatewayMessage{{"system", "lain"}, {"user", "halo"}}),
		"pesan beda":   responseCacheKey(cfg, "model-a", []gatewayMessage{{"system", "sistem"}, {"user", "lain"}}),
		"riwayat beda": responseCacheKey(cfg, "model-a", []gatewayMessage{{"system", "sistem"}, {"user", "halo"}, {"assistant", "hai"}}),
		"base beda":    responseCacheKey(settings{BaseURL: "https://lain.test/v1", GatewayProtocol: "openai"}, "model-a", history),
		"proto beda":   responseCacheKey(settings{BaseURL: "https://gw.test/v1", GatewayProtocol: "anthropic"}, "model-a", history),
		"kosong":       responseCacheKey(cfg, "model-a", nil),
	}
	for name, key := range variants {
		if key == base {
			t.Fatalf("kunci %s tidak boleh sama dengan kunci dasar", name)
		}
	}
}

func TestUnitResponseCacheTTLAndCap(t *testing.T) {
	app := &App{cache: map[string]cacheEntry{}}
	app.cachePut("k1", "satu")
	if text, ok := app.cacheGet("k1"); !ok || text != "satu" {
		t.Fatalf("cacheGet k1 = %q %v", text, ok)
	}
	if app.cacheHitCount() != 1 {
		t.Fatalf("cacheHits harus 1, dapat %d", app.cacheHitCount())
	}
	// Entri kedaluwarsa: miss dan terhapus dari peta.
	app.cacheMu.Lock()
	app.cache["k1"] = cacheEntry{text: "satu", at: time.Now().Add(-responseCacheTTL - time.Second)}
	app.cacheMu.Unlock()
	if _, ok := app.cacheGet("k1"); ok {
		t.Fatal("entri kedaluwarsa harus miss")
	}
	app.cacheMu.Lock()
	_, stillThere := app.cache["k1"]
	app.cacheMu.Unlock()
	if stillThere {
		t.Fatal("entri kedaluwarsa harus terhapus")
	}
	if app.cacheHitCount() != 1 {
		t.Fatal("miss tidak boleh menambah cacheHits")
	}

	// Cap 200: entri tertua dibuang lebih dulu.
	app2 := &App{cache: map[string]cacheEntry{}}
	for i := 0; i < responseCacheMax+50; i++ {
		app2.cachePut(fmt.Sprintf("k%d", i), "v")
		// Jaga urutan waktu agar "tertua" deterministik.
		app2.cacheMu.Lock()
		entry := app2.cache[fmt.Sprintf("k%d", i)]
		entry.at = time.Unix(int64(1000+i), 0)
		app2.cache[fmt.Sprintf("k%d", i)] = entry
		app2.cacheMu.Unlock()
	}
	app2.cacheMu.Lock()
	size := len(app2.cache)
	_, oldestKept := app2.cache["k49"]
	_, firstKept := app2.cache["k50"]
	_, newestKept := app2.cache[fmt.Sprintf("k%d", responseCacheMax+49)]
	app2.cacheMu.Unlock()
	if size != responseCacheMax {
		t.Fatalf("cache harus dibatasi %d, dapat %d", responseCacheMax, size)
	}
	if oldestKept {
		t.Fatal("50 entri tertua harus terbuang")
	}
	if !firstKept || !newestKept {
		t.Fatal("entri ke-51 dan terbaru harus bertahan")
	}
	// Menulis ulang kunci yang sama tidak menambah ukuran.
	app2.cachePut("k100", "baru")
	app2.cacheMu.Lock()
	size = len(app2.cache)
	app2.cacheMu.Unlock()
	if size != responseCacheMax {
		t.Fatalf("tulis ulang tidak boleh menambah ukuran, dapat %d", size)
	}
}

func TestUnitValidateToolParams(t *testing.T) {
	tool := customTool{Params: []toolParam{
		{Name: "jumlah", Type: "number", Required: true},
		{Name: "aktif", Type: "bool"},
		{Name: "label", Type: "string"},
	}}
	cases := []struct {
		arg     string
		wantErr string
	}{
		{"42\ntrue\nHalo", ""},
		{"-3.5\nfalse", ""},
		{"1e3", ""},
		{"abc", "harus berupa angka"},
		{"", "wajib diisi"},
		{"42\nbukan-bool", "harus berupa boolean"},
		{"42\n1", ""}, // ParseBool menerima 1/0
		{"42\n0", ""},
		{"42\nTRUE", ""},
		{"42", ""}, // parameter opsional sesudahnya boleh tidak diisi
	}
	for _, c := range cases {
		err := validateToolParams(tool, c.arg)
		if c.wantErr == "" && err != nil {
			t.Errorf("arg %q harus sah, dapat error %v", c.arg, err)
		}
		if c.wantErr != "" && (err == nil || !strings.Contains(err.Error(), c.wantErr)) {
			t.Errorf("arg %q harus error %q, dapat %v", c.arg, c.wantErr, err)
		}
	}
	// Tanpa parameter: argumen apa pun sah.
	if err := validateToolParams(customTool{}, "apa saja"); err != nil {
		t.Fatalf("tool tanpa parameter harus sah: %v", err)
	}
	// Bentuk lama (string) dan objek ber-tipe sama-sama terurai dari JSON.
	var legacy customTool
	if err := json.Unmarshal([]byte(`{"name":"x","type":"http_get","url":"https://a.test","params":["kota",{"name":"n","type":"number","required":true},{"name":"b","type":"bool"}]}`), &legacy); err != nil {
		t.Fatal(err)
	}
	if len(legacy.Params) != 3 || legacy.Params[0].Type != "string" || legacy.Params[1].Type != "number" || !legacy.Params[1].Required || legacy.Params[2].Type != "bool" {
		t.Fatalf("parameter terurai salah: %+v", legacy.Params)
	}
	if validToolParamType("tanggal") || !validToolParamType("string") || !validToolParamType("number") || !validToolParamType("bool") {
		t.Fatal("validToolParamType salah")
	}
	// Substitusi: nilai URL di-escape, body tidak.
	tool2 := customTool{Params: []toolParam{{Name: "kota", Type: "string"}}}
	if got := substituteParams("https://a.test/{{kota}}", tool2, "Kota Baru", true); got != "https://a.test/Kota%20Baru" {
		t.Fatalf("substitusi URL = %q", got)
	}
	if got := substituteParams("halo {{kota}} {{arg}}", tool2, "Kota Baru", false); got != "halo Kota Baru Kota Baru" {
		t.Fatalf("substitusi body = %q", got)
	}
}

func TestUnitPersonaDefinitions(t *testing.T) {
	if len(builtinPersonas) != 6 {
		t.Fatalf("persona harus 6, dapat %d", len(builtinPersonas))
	}
	seen := map[string]bool{}
	for _, p := range builtinPersonas {
		if p.ID == "" || seen[p.ID] {
			t.Fatalf("ID persona kosong/duplikat: %q", p.ID)
		}
		seen[p.ID] = true
		if strings.TrimSpace(p.Name) == "" || strings.TrimSpace(p.Role) == "" {
			t.Fatalf("persona %s: nama/peran kosong", p.ID)
		}
		if len([]rune(strings.TrimSpace(p.Prompt))) < 50 {
			t.Fatalf("persona %s: prompt terlalu pendek", p.ID)
		}
		for _, tool := range p.DisabledTools {
			if !builtinToolNames[tool] {
				t.Fatalf("persona %s menonaktifkan tool tak dikenal %q", p.ID, tool)
			}
		}
		if findPersona(p.ID) == nil {
			t.Fatalf("findPersona(%s) harus ketemu", p.ID)
		}
	}
	if findPersona("tidak-ada") != nil {
		t.Fatal("findPersona tak dikenal harus nil")
	}
}

func TestUnitSeatOrderFilter(t *testing.T) {
	d, err := loadData(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	d.Rooms = []room{{
		ID: "room-uji", Name: "Uji",
		SeatCodes: []string{"M01", "ZZZ", "M01", "M02"},
		SeatOrder: []string{"M02", "ZZZ", "M02", "M01", ""},
	}}
	d.ActiveRoomID = "room-uji"
	d.migrateRooms()
	r := d.findRoom("room-uji")
	if r == nil {
		t.Fatal("ruang uji hilang setelah migrasi")
	}
	if fmt.Sprint(r.SeatCodes) != "[M01 M02]" {
		t.Fatalf("SeatCodes harus tersaring+dedupe: %v", r.SeatCodes)
	}
	if fmt.Sprint(r.SeatOrder) != "[M02 M01]" {
		t.Fatalf("SeatOrder harus tersaring+dedupe: %v", r.SeatOrder)
	}
	// roomSeatCodes: urutan kustom dulu, peserta lain menyusul urutan global.
	d.Rooms[0].SeatCodes = []string{"M01", "M02", "M03"}
	d.Rooms[0].SeatOrder = []string{"M03", "M01"}
	if got := d.roomSeatCodes(&d.Rooms[0]); fmt.Sprint(got) != "[M03 M01 M02]" {
		t.Fatalf("roomSeatCodes = %v", got)
	}
	// SeatOrder berisi kursi bukan peserta diabaikan.
	d.Rooms[0].SeatOrder = []string{"M05", "M02"}
	if got := d.roomSeatCodes(&d.Rooms[0]); fmt.Sprint(got) != "[M02 M01 M03]" {
		t.Fatalf("roomSeatCodes bukan peserta = %v", got)
	}
	// Tanpa SeatOrder: urutan global peserta.
	d.Rooms[0].SeatOrder = nil
	if got := d.roomSeatCodes(&d.Rooms[0]); fmt.Sprint(got) != "[M01 M02 M03]" {
		t.Fatalf("roomSeatCodes bawaan = %v", got)
	}
	// Ruang aktif tidak boleh arsip setelah migrasi.
	d.Rooms[0].Archived = true
	d.migrateRooms()
	if d.findRoom("room-uji").Archived {
		t.Fatal("ruang aktif harus dipulihkan dari arsip oleh migrasi")
	}
}

func TestUnitReactionToggleIdempotent(t *testing.T) {
	f := setup(t, nil)
	f.api("POST", "/api/message", object{"text": "@Plamo reaksi unit"}, 202)
	state := f.wait(func(s object) bool { return completedSeatReplies(s) >= 1 })
	var seatMsg map[string]any
	for _, m := range messagesOf(state) {
		if m["authorType"] == "seat" {
			seatMsg = m
		}
	}
	id := seatMsg["id"].(string)
	reactionsOf := func() []any {
		for _, m := range messagesOf(f.state()) {
			if m["id"] == id {
				list, _ := m["reactions"].([]any)
				return list
			}
		}
		return nil
	}
	// Toggle menyala lalu mati: kembali kosong (idempoten dua kali).
	f.api("POST", "/api/message/"+id+"/react", object{"emoji": "🔥"}, 200)
	if len(reactionsOf()) != 1 {
		t.Fatalf("reaksi harus 1: %v", reactionsOf())
	}
	f.api("POST", "/api/message/"+id+"/react", object{"emoji": "🔥"}, 200)
	if len(reactionsOf()) != 0 {
		t.Fatalf("reaksi harus kosong setelah toggle kedua: %v", reactionsOf())
	}
	// Dua emoji berbeda hidup berdampingan; mematikan satu tidak mengganggu lain.
	f.api("POST", "/api/message/"+id+"/react", object{"emoji": "👍"}, 200)
	f.api("POST", "/api/message/"+id+"/react", object{"emoji": "👏"}, 200)
	if len(reactionsOf()) != 2 {
		t.Fatalf("reaksi harus 2: %v", reactionsOf())
	}
	f.api("POST", "/api/message/"+id+"/react", object{"emoji": "👍"}, 200)
	list := reactionsOf()
	if len(list) != 1 {
		t.Fatalf("reaksi harus tersisa 1: %v", list)
	}
	entry, _ := list[0].(map[string]any)
	if entry["emoji"] != "👏" || entry["byOwner"] != true || entry["count"] != float64(1) {
		t.Fatalf("entri reaksi salah: %v", entry)
	}
	// Emoji di luar daftar ditolak; pesan tak ada 404.
	f.api("POST", "/api/message/"+id+"/react", object{"emoji": "🚀"}, 400)
	f.api("POST", "/api/message/00000000-0000-4000-8000-000000000000/react", object{"emoji": "👍"}, 404)
	// Semua 8 emoji resmi diterima.
	for _, emoji := range reactionEmojis {
		f.api("POST", "/api/message/"+id+"/react", object{"emoji": emoji}, 200)
	}
}
