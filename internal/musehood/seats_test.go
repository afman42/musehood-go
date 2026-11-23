package musehood

import (
	"os"
	"path/filepath"
	"testing"
)

func seatsOf(s object) []object { return asObjects(s["seats"]) }

func TestCustomSeats(t *testing.T) {
	f := setup(t, nil)
	// Happy path: create a custom seat.
	created := f.api("POST", "/api/seats", object{"name": "Nova", "role": "Pengamat", "model": "nova-1"}, 201)
	if created["code"] != "C01" || created["name"] != "Nova" || created["role"] != "Pengamat" || created["provider"] != "gateway" {
		t.Fatalf("seat dibuat tidak sesuai: %v", created)
	}
	st := f.state()
	if len(seatsOf(st)) != 8 {
		t.Fatalf("jumlah kursi harus 8, got %d", len(seatsOf(st)))
	}
	// Duplicate name (case-insensitive) is rejected.
	f.api("POST", "/api/seats", object{"name": "nova", "role": "Lain", "model": ""}, 400)
	// Duplicate of a built-in name is rejected.
	f.api("POST", "/api/seats", object{"name": "Opus", "role": "Lain", "model": ""}, 400)
	// Empty name / role rejected.
	f.api("POST", "/api/seats", object{"name": "", "role": "Lain", "model": ""}, 400)
	f.api("POST", "/api/seats", object{"name": "Baru", "role": "", "model": ""}, 400)
	// Second custom seat gets the next code.
	second := f.api("POST", "/api/seats", object{"name": "Vega", "role": "Peneliti", "model": ""}, 201)
	if second["code"] != "C02" {
		t.Fatalf("kursi kedua harus C02, got %v", second["code"])
	}
	// Built-in seats cannot be deleted.
	f.api("DELETE", "/api/seats/M10", nil, 400)
	f.api("DELETE", "/api/seats/MUSE", nil, 400)
	// Unknown code returns 404.
	f.api("DELETE", "/api/seats/C99", nil, 404)
	// Delete a custom seat; it disappears from state.
	f.api("DELETE", "/api/seats/C01", nil, 200)
	st = f.state()
	if len(seatsOf(st)) != 8 {
		t.Fatalf("jumlah kursi harus 8 setelah hapus C01, got %d", len(seatsOf(st)))
	}
	for _, s := range seatsOf(st) {
		if s["code"] == "C01" {
			t.Fatal("C01 masih ada setelah dihapus")
		}
	}
	// Rename a custom seat via settings patch (findSeat is generic).
	f.api("POST", "/api/settings", object{"seats": []object{{"code": "C02", "name": "Vega Baru"}}}, 200)
	st = f.state()
	found := false
	for _, s := range seatsOf(st) {
		if s["code"] == "C02" && s["name"] == "Vega Baru" {
			found = true
		}
	}
	if !found {
		t.Fatal("rename kursi custom via settings gagal")
	}
	// data.json on disk still loads (restart equivalent): reload from the same dir.
	if _, err := loadData(f.dir); err != nil {
		t.Fatalf("loadData setelah kursi custom gagal: %v", err)
	}
}

func TestLoadDataWithCustomSeats(t *testing.T) {
	dir := t.TempDir()
	d, err := loadData(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Seats) != 7 {
		t.Fatalf("data baru harus 7 kursi, got %d", len(d.Seats))
	}
	// Append a custom seat and save, then reload.
	d.Seats = append(d.Seats, seat{identity: identity{"Nova", "/avatars/custom.svg"}, Code: "C01", Role: "Pengamat", Provider: "gateway", Model: ptr("nova-1"), Present: true})
	if err := writeJSONFile(filepath.Join(dir, "data.json"), d); err != nil {
		t.Fatal(err)
	}
	loaded, err := loadData(dir)
	if err != nil {
		t.Fatalf("loadData 8 kursi gagal: %v", err)
	}
	if len(loaded.Seats) != 8 || loaded.Seats[7].Code != "C01" {
		t.Fatalf("kursi custom tidak terbaca: %v", loaded.Seats)
	}
	// A file with fewer than 7 seats is still rejected.
	d.Seats = d.Seats[:6]
	if err := writeJSONFile(filepath.Join(dir, "data.json"), d); err != nil {
		t.Fatal(err)
	}
	if _, err := loadData(dir); err == nil {
		t.Fatal("loadData 6 kursi seharusnya gagal")
	}
	// Duplicate custom names are rejected.
	_ = os.Remove(filepath.Join(dir, "data.json"))
	d, err = loadData(dir)
	if err != nil {
		t.Fatal(err)
	}
	d.Seats = append(d.Seats, seat{identity: identity{"Opus", "/avatars/custom.svg"}, Code: "C01", Role: "Duplikat", Provider: "gateway", Model: ptr(""), Present: true})
	if err := writeJSONFile(filepath.Join(dir, "data.json"), d); err != nil {
		t.Fatal(err)
	}
	if _, err := loadData(dir); err == nil {
		t.Fatal("loadData nama kursi duplikat seharusnya gagal")
	}
}
