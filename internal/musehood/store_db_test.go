package musehood

// Tes lapisan penyimpanan (database): roundtrip data.json, data rusak,
// migrasi data lama, penulisan atomik, konkurensi, dan restore.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestStoreRoundtrip(t *testing.T) {
	dir := t.TempDir()
	d, err := loadData(dir)
	if err != nil {
		t.Fatal(err)
	}
	d.Owner.Name = "Pemilik Uji"
	d.Seats[2].Memory = "catatan penting"
	d.Seats[2].Skills = []string{"contoh"}
	d.Messages = append(d.Messages, &message{ID: "m1", AuthorType: "owner", Text: "halo", Status: "completed", RoomID: defaultRoomID, CreatedAt: now()})
	if err := writeJSONFile(filepath.Join(dir, "data.json"), d); err != nil {
		t.Fatal(err)
	}
	loaded, err := loadData(dir)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Owner.Name != "Pemilik Uji" || loaded.Seats[2].Memory != "catatan penting" ||
		len(loaded.Seats[2].Skills) != 1 || len(loaded.Messages) != 1 || loaded.Messages[0].Text != "halo" {
		t.Fatalf("roundtrip tidak utuh: %+v", loaded.Seats[2])
	}
	// Tidak ada file temp tertinggal setelah penulisan atomik.
	entries, _ := os.ReadDir(dir)
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".tmp") {
			t.Fatalf("file temp tertinggal: %s", entry.Name())
		}
	}
}

func TestStoreCorruptData(t *testing.T) {
	cases := map[string]string{
		"bukan json":      "{ini bukan json",
		"tanpa settings":  `{"seats":[]}`,
		"kursi kurang":    `{"settings":{},"seats":[],"messages":[]}`,
		"messages null":   `{"settings":{},"seats":[],"messages":null}`,
		"urutan salah":    "",
		"kode duplikat":   "",
		"nama duplikat":   "",
		"custom tanpa C":  "",
		"pesan tak valid": "",
	}
	// Bangun varian berbasis data sah.
	base, err := loadData(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	mutate := func(fn func(*data)) string {
		clone := base
		clone.Seats = append([]seat{}, base.Seats...)
		clone.Messages = append([]*message{}, base.Messages...)
		fn(&clone)
		raw, _ := json.Marshal(clone)
		return string(raw)
	}
	cases["urutan salah"] = mutate(func(d *data) { d.Seats[0], d.Seats[1] = d.Seats[1], d.Seats[0] })
	cases["kode duplikat"] = mutate(func(d *data) { d.Seats[1].Code = d.Seats[0].Code })
	cases["nama duplikat"] = mutate(func(d *data) { d.Seats[1].Name = d.Seats[0].Name })
	cases["custom tanpa C"] = mutate(func(d *data) {
		d.Seats = append(d.Seats, seat{identity: identity{"Ekstra", ""}, Code: "X01", Role: "Uji", Provider: "gateway", Model: ptr(""), Present: true})
	})
	cases["pesan tak valid"] = mutate(func(d *data) {
		d.Messages = append(d.Messages, nil)
	})

	for name, content := range cases {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "data.json"), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := loadData(dir); err == nil {
			t.Fatalf("%s: loadData harus gagal", name)
		}
	}
}

func TestStoreLegacyMigration(t *testing.T) {
	dir := t.TempDir()
	d, err := loadData(dir)
	if err != nil {
		t.Fatal(err)
	}
	// Simulasikan data lama: tanpa rooms, tanpa providers (gateway masih di
	// settings tunggal), pesan tanpa roomId.
	d.Rooms = nil
	d.ActiveRoomID = ""
	d.Providers = nil
	d.Settings.BaseURL = "https://gateway.example.com/v1"
	d.Settings.APIKey = "kunci-lama"
	d.Settings.ProviderName = "Gateway Lama"
	d.Messages = append(d.Messages, &message{ID: "lama", AuthorType: "owner", Text: "pesan lama", Status: "completed", CreatedAt: now()})
	if err := writeJSONFile(filepath.Join(dir, "data.json"), d); err != nil {
		t.Fatal(err)
	}
	loaded, err := loadData(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Rooms) != 1 || loaded.Rooms[0].ID != defaultRoomID || loaded.ActiveRoomID != defaultRoomID {
		t.Fatalf("migrasi ruang gagal: %+v", loaded.Rooms)
	}
	if len(loaded.Providers) != 1 || loaded.Providers[0].ID != "prov-utama" || loaded.Providers[0].APIKey != "kunci-lama" {
		t.Fatalf("migrasi provider gagal: %+v", loaded.Providers)
	}
	if loaded.Messages[0].RoomID != defaultRoomID {
		t.Fatalf("pesan lama harus masuk ruang bawaan: %+v", loaded.Messages[0])
	}
}

func TestStoreConcurrentSaves(t *testing.T) {
	f := setup(t, nil)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			f.app.mu.Lock()
			_ = f.app.saveLocked()
			f.app.mu.Unlock()
		}()
	}
	wg.Wait()
	// File hasil harus tetap JSON sah dan bisa dimuat.
	if _, err := loadData(f.dir); err != nil {
		t.Fatalf("data rusak setelah save konkuren: %v", err)
	}
}

func TestStoreRestorePreservesProviderKeys(t *testing.T) {
	f := setup(t, nil)
	// Backup dari state sekarang (key dikosongkan), lalu restore.
	backup := f.api("GET", "/api/backup", nil, 200)
	f.api("POST", "/api/restore", backup, 200)
	// Provider utama tetap punya key (fixture memakai secret).
	out := f.api("GET", "/api/providers", nil, 200)
	found := false
	for _, item := range out["providers"].([]any) {
		view := item.(map[string]any)
		if view["hasApiKey"] == true {
			found = true
		}
	}
	if !found {
		t.Fatal("restore harus mempertahankan API key provider yang ada")
	}
	// Restore dengan kursi kurang dari 7 ditolak.
	broken := object{"version": 1, "settings": object{}, "seats": []any{}, "messages": []any{}}
	status, _ := rawRequest(t, f, "POST", "/api/restore", mustJSON(broken), nil)
	if status != 400 {
		t.Fatalf("restore rusak: mau 400, dapat %d", status)
	}
	// Restore dengan job index di luar rentang ditolak (regresi audit STLC).
	badJob := f.api("GET", "/api/backup", nil, 200)
	badJob["jobs"] = []any{object{"id": "j1", "index": 99, "steps": []any{}}}
	status, _ = rawRequest(t, f, "POST", "/api/restore", mustJSON(badJob), nil)
	if status != 400 {
		t.Fatalf("restore job rusak: mau 400, dapat %d", status)
	}
}
