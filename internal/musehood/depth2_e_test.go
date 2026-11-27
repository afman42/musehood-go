//go:build (linux && (386 || amd64 || arm || arm64 || loong64 || ppc64le || riscv64 || s390x)) || (darwin && (amd64 || arm64)) || (windows && (386 || amd64 || arm64)) || (freebsd && (386 || amd64 || arm || arm64)) || (netbsd && amd64) || (openbsd && (amd64 || arm64)) || (android && arm64) || (illumos && amd64)

package musehood

// Tes fitur pendalaman grup E: audit login + rate limit PIN (28),
// indeks full-text (35), backend SQLite + migrasi (34).

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoginRateLimitAndAudit(t *testing.T) {
	f := setup(t, nil)
	base := f.server.URL
	if status, _, raw := doRaw(t, "POST", base+"/api/pin", object{"pin": "1234"}, nil); status != 200 {
		t.Fatalf("set PIN = %d %s", status, raw)
	}
	// 5 kegagalan dari IP yang sama → percobaan berikutnya 429, bahkan
	// dengan PIN benar.
	for i := 0; i < 5; i++ {
		if status, _, _ := doRaw(t, "POST", base+"/api/login", object{"pin": "salah"}, nil); status != 401 {
			t.Fatalf("login salah ke-%d = %d", i+1, status)
		}
	}
	if status, _, _ := doRaw(t, "POST", base+"/api/login", object{"pin": "1234"}, nil); status != 429 {
		t.Fatalf("login saat diblokir harus 429, dapat %d", status)
	}
	// IP lain (X-Forwarded-For) tidak ikut terblokir dan suksesnya
	// tercatat di log audit.
	other := map[string]string{"X-Forwarded-For": "203.0.113.9, 10.0.0.1"}
	status, header, _ := doRaw(t, "POST", base+"/api/login", object{"pin": "1234"}, other)
	if status != 200 {
		t.Fatalf("login IP lain = %d", status)
	}
	cookie := sessionCookie(t, header)
	auth := map[string]string{"Cookie": cookie}
	status, _, raw := doRaw(t, "GET", base+"/api/state", nil, auth)
	if status != 200 {
		t.Fatalf("state = %d", status)
	}
	var state object
	if json.Unmarshal(raw, &state) != nil {
		t.Fatal("state tidak valid")
	}
	log := asObjects(state["loginLog"])
	if len(log) != 7 { // 5 gagal + 1 terblokir + 1 sukses
		t.Fatalf("loginLog harus 7 entri, dapat %d: %v", len(log), log)
	}
	last := log[len(log)-1]
	if last["success"] != true || last["ip"] != "203.0.113.9" {
		t.Fatal("entri sukses salah:", last)
	}
	for _, entry := range log[:6] {
		if entry["success"] != false {
			t.Fatal("entri gagal salah:", entry)
		}
	}
}

func TestFullTextIndexSearch(t *testing.T) {
	f := setup(t, nil)
	f.api("POST", "/api/message", object{"text": "@M01 kataunikzebra dan pisang"}, 202)
	f.finish(2)
	// Kata tunggal utuh lewat indeks.
	out := f.api("GET", "/api/search?q=kataunikzebra", nil, 200)
	if len(asObjects(out["results"])) == 0 {
		t.Fatal("kata utuh tidak ditemukan")
	}
	// Multi-kata AND: kedua kata ada di pesan yang sama.
	out = f.api("GET", "/api/search?q=kataunikzebra+pisang", nil, 200)
	if len(asObjects(out["results"])) == 0 {
		t.Fatal("AND dua kata tidak ditemukan")
	}
	// Multi-kata AND dengan kata yang tidak ada → kosong.
	out = f.api("GET", "/api/search?q=kataunikzebra+tidakadakataini", nil, 200)
	if len(asObjects(out["results"])) != 0 {
		t.Fatal("AND dengan kata absen harus kosong:", out)
	}
	// Kata sebagian (bukan token utuh) jatuh ke substring.
	out = f.api("GET", "/api/search?q=unikzebra", nil, 200)
	if len(asObjects(out["results"])) == 0 {
		t.Fatal("substring sebagian tidak ditemukan")
	}
	// Pesan terhapus tidak lagi ditemukan (indeks dibangun ulang).
	ownerMsg := findMessageBy(t, f, func(m object) bool {
		text, _ := m["text"].(string)
		return strings.Contains(text, "kataunikzebra")
	})
	f.api("POST", "/api/message/"+idOf(ownerMsg)+"/delete", nil, 200)
	out = f.api("GET", "/api/search?q=kataunikzebra", nil, 200)
	for _, result := range asObjects(out["results"]) {
		if strings.Contains(result["text"].(string), "kataunikzebra") && result["id"] == idOf(ownerMsg) {
			t.Fatal("pesan terhapus masih ditemukan")
		}
	}
}

func TestSQLiteStoreRoundtripAndMigration(t *testing.T) {
	dir := t.TempDir()
	// Tulis data.json lewat backend JSON.
	js := jsonStore{dir: dir}
	d := defaultData()
	d.Messages = append(d.Messages, &message{ID: "msg-uji", AuthorType: "owner", Text: "halo sqlite", RoomID: defaultRoomID, CreatedAt: now()})
	if err := js.save(d); err != nil {
		t.Fatal(err)
	}
	// Buka backend SQLite: harus memigrasi dari data.json.
	ss, err := openSQLiteStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer ss.db.Close()
	got, err := ss.load()
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Messages) != 1 || got.Messages[0].Text != "halo sqlite" {
		t.Fatal("migrasi gagal:", got.Messages)
	}
	// data.json dipertahankan sebagai cadangan.
	if _, err := os.Stat(filepath.Join(dir, "data.json")); err != nil {
		t.Fatal("data.json cadangan hilang:", err)
	}
	// Simpan perubahan lewat SQLite lalu muat ulang.
	got.Messages[0].Text = "berubah di sqlite"
	if err := ss.save(got); err != nil {
		t.Fatal(err)
	}
	again, err := ss.load()
	if err != nil {
		t.Fatal(err)
	}
	if again.Messages[0].Text != "berubah di sqlite" {
		t.Fatal("roundtrip sqlite gagal:", again.Messages[0].Text)
	}
	// openStore: env sqlite memilih SQLite; tanpa env dan tanpa
	// data.db memilih JSON.
	t.Setenv("MUSEHOOD_STORAGE", "sqlite")
	store, err := openStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := store.(*sqliteStore); !ok {
		t.Fatal("env sqlite harus memilih sqliteStore")
	}
	t.Setenv("MUSEHOOD_STORAGE", "")
	fresh := t.TempDir()
	store, err = openStore(fresh)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := store.(jsonStore); !ok {
		t.Fatal("direktori baru tanpa env harus memilih jsonStore")
	}
}
