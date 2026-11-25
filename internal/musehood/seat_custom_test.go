package musehood

import "testing"

// Peran, provider, dan model kursi bawaan (termasuk MUSE) dapat diganti
// lewat Pengaturan dan bertahan setelah reload data.json.
func TestSeatRoleProviderModelPatch(t *testing.T) {
	f := setup(t, nil)
	f.api("POST", "/api/settings", object{"seats": []object{{"code": "MUSE", "role": "Koordinator", "provider": "gateway", "model": "gpt-6.1-sol"}}}, 200)
	muse := findStateSeat(t, f.state(), "MUSE")
	if muse["role"] != "Koordinator" || muse["provider"] != "gateway" || muse["model"] != "gpt-6.1-sol" {
		t.Fatalf("patch MUSE tidak diterapkan: %v", muse)
	}
	// Reload dari disk: nilai kustom tidak boleh dipaksa kembali ke bawaan.
	d, err := loadData(f.dir)
	if err != nil {
		t.Fatal(err)
	}
	loaded := d.findSeat("MUSE")
	if loaded == nil || loaded.Role != "Koordinator" || loaded.Provider != "gateway" || loaded.Model == nil || *loaded.Model != "gpt-6.1-sol" {
		t.Fatalf("patch MUSE tidak bertahan setelah reload: %+v", loaded)
	}
	// Kembali ke antrean Muse.
	f.api("POST", "/api/settings", object{"seats": []object{{"code": "MUSE", "provider": "muse"}}}, 200)
	if muse = findStateSeat(t, f.state(), "MUSE"); muse["provider"] != "muse" {
		t.Fatalf("MUSE harus kembali ke provider muse: %v", muse)
	}
	// Validasi: provider tidak dikenal dan peran kosong ditolak.
	f.api("POST", "/api/settings", object{"seats": []object{{"code": "MUSE", "provider": "lain"}}}, 400)
	f.api("POST", "/api/settings", object{"seats": []object{{"code": "M10", "role": "  "}}}, 400)
	// Peran kursi bawaan lain juga bisa diganti.
	f.api("POST", "/api/settings", object{"seats": []object{{"code": "M10", "role": "Arsitek"}}}, 200)
	if opus := findStateSeat(t, f.state(), "M10"); opus["role"] != "Arsitek" {
		t.Fatalf("peran M10 tidak berubah: %v", opus)
	}
	// Prompt bawaan mengikuti peran baru; peran bawaan tetap memakai duties.
	changed := seat{identity: identity{"Muse", ""}, Code: "MUSE", Role: "Koordinator"}
	if got := defaultPromptFor(changed); got != "Laksanakan peran Koordinator." {
		t.Fatalf("defaultPromptFor peran diganti = %q", got)
	}
	original := seat{identity: identity{"Muse", ""}, Code: "MUSE", Role: "Admin dan Integrasi"}
	if got := defaultPromptFor(original); got != duties["MUSE"] {
		t.Fatalf("defaultPromptFor peran bawaan = %q, mau duties", got)
	}
}

// Patch pengaturan tanpa field gateway tidak boleh mengosongkan nama/BaseURL
// provider aktif (regresi write-through: data.json jadi tidak valid).
func TestSettingsPatchKeepsProviderName(t *testing.T) {
	f := setup(t, nil)
	f.api("POST", "/api/settings", object{"settings": object{"integratorSeat": "M10"}}, 200)
	d, err := loadData(f.dir)
	if err != nil {
		t.Fatalf("loadData setelah patch non-gateway gagal: %v", err)
	}
	active := d.findProvider(d.Settings.ActiveProviderID)
	if active == nil || active.Name == "" || active.BaseURL == "" {
		t.Fatalf("provider aktif rusak setelah patch non-gateway: %+v", active)
	}
}

// Kursi integrator dapat diganti: langkah terakhir pipeline "Tugas:" mengikuti
// pengaturan, kode tidak dikenal ditolak, dan menghapus kursi integrator
// custom mengembalikannya ke MUSE.
func TestIntegratorSeat(t *testing.T) {
	f := setup(t, nil)
	lastStep := func() string {
		f.app.mu.Lock()
		defer f.app.mu.Unlock()
		_, steps := f.app.routeLocked("Tugas: buat rencana")
		return steps[len(steps)-1].Seat
	}
	if got := lastStep(); got != "MUSE" {
		t.Fatalf("integrator bawaan harus MUSE, got %s", got)
	}
	f.api("POST", "/api/settings", object{"settings": object{"integratorSeat": "M10"}}, 200)
	if got := lastStep(); got != "M10" {
		t.Fatalf("integrator harus M10 setelah diganti, got %s", got)
	}
	f.api("POST", "/api/settings", object{"settings": object{"integratorSeat": "C99"}}, 400)
	// Kursi custom sebagai integrator, lalu dihapus → kembali ke MUSE.
	created := f.api("POST", "/api/seats", object{"name": "Nova", "role": "Pengamat", "model": "nova-1"}, 201)
	if created["code"] != "C01" {
		t.Fatalf("kursi custom harus C01, got %v", created["code"])
	}
	f.api("POST", "/api/settings", object{"settings": object{"integratorSeat": "C01"}}, 200)
	if got := lastStep(); got != "C01" {
		t.Fatalf("integrator harus C01, got %s", got)
	}
	f.api("DELETE", "/api/seats/C01", nil, 200)
	if got := lastStep(); got != "MUSE" {
		t.Fatalf("integrator harus kembali ke MUSE setelah C01 dihapus, got %s", got)
	}
	// Pengaturan integrator bertahan setelah reload.
	f.api("POST", "/api/settings", object{"settings": object{"integratorSeat": "M02"}}, 200)
	d, err := loadData(f.dir)
	if err != nil {
		t.Fatal(err)
	}
	if d.Settings.IntegratorSeat != "M02" {
		t.Fatalf("integratorSeat tidak bertahan setelah reload: %q", d.Settings.IntegratorSeat)
	}
}
