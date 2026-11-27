package musehood

import (
	"strings"
	"testing"
)

func TestPersonaApply(t *testing.T) {
	f := setup(t, nil)
	out := f.api("GET", "/api/personas", nil, 200)
	personas, _ := out["personas"].([]any)
	if len(personas) != 6 {
		t.Fatalf("persona harus 6: %v", out)
	}
	ids := map[string]bool{}
	for _, item := range personas {
		p, _ := item.(map[string]any)
		ids[p["id"].(string)] = true
	}
	for _, want := range []string{"reviewer-ketat", "penulis-santai", "analis-data", "programmer", "penerjemah", "tutor-sabar"} {
		if !ids[want] {
			t.Fatalf("persona %s hilang", want)
		}
	}

	applied := f.api("POST", "/api/seats/M01/persona", object{"personaId": "penerjemah"}, 200)
	seatMap, _ := applied["seat"].(map[string]any)
	if seatMap["role"] != "Penerjemah" {
		t.Fatalf("role persona = %v", seatMap)
	}
	if !strings.Contains(seatMap["prompt"].(string), "penerjemah profesional") {
		t.Fatalf("prompt persona salah: %v", seatMap["prompt"])
	}
	disabled, _ := seatMap["disabledTools"].([]any)
	foundCode := false
	for _, tool := range disabled {
		if tool == "run_code" {
			foundCode = true
		}
	}
	if !foundCode {
		t.Fatalf("disabledTools persona harus memuat run_code: %v", disabled)
	}
	// Tercermin di state.
	state := f.state()
	for _, item := range state["seats"].([]any) {
		s, _ := item.(map[string]any)
		if s["code"] == "M01" && s["role"] != "Penerjemah" {
			t.Fatalf("state M01 role = %v", s["role"])
		}
	}
	f.api("POST", "/api/seats/M01/persona", object{"personaId": "tidak-ada"}, 404)
	f.api("POST", "/api/seats/ZZZ/persona", object{"personaId": "programmer"}, 404)
}

func TestRoomSeatOrder(t *testing.T) {
	f := setup(t, nil)
	f.api("POST", "/api/settings", object{"settings": object{"roundRobin": true}}, 200)
	created := f.api("POST", "/api/rooms", object{"name": "Ruang Urutan", "seatOrder": []string{"M05", "M01"}}, 201)
	roomID := created["activeRoomId"].(string)

	f.api("POST", "/api/message", object{"text": "Halo semua"}, 202)
	f.app.mu.Lock()
	var steps []string
	for _, j := range f.app.data.Jobs {
		if j.RoomID == roomID {
			steps = nil
			for _, s := range j.Steps {
				steps = append(steps, s.Seat)
			}
		}
	}
	f.app.mu.Unlock()
	if len(steps) < 2 || steps[0] != "M05" || steps[1] != "M01" {
		t.Fatalf("urutan langkah harus diawali M05, M01: %v", steps)
	}

	// Ubah urutan lewat update ruang.
	updated := f.api("POST", "/api/rooms/"+roomID, object{"seatOrder": []string{"M02"}}, 200)
	for _, item := range updated["rooms"].([]any) {
		r, _ := item.(map[string]any)
		if r["id"] == roomID {
			order, _ := r["seatOrder"].([]any)
			if len(order) != 1 || order[0] != "M02" {
				t.Fatalf("seatOrder tersimpan = %v", order)
			}
		}
	}
	f.api("POST", "/api/rooms/"+roomID, object{"seatOrder": []string{"ZZZ"}}, 400)
}

func TestRoomArchive(t *testing.T) {
	f := setup(t, nil)
	roomB := f.api("POST", "/api/rooms", object{"name": "Ruang Arsip"}, 201)["activeRoomId"].(string)
	f.api("POST", "/api/message", object{"text": "kataarsip unik sekali"}, 202)
	roomC := f.api("POST", "/api/rooms", object{"name": "Ruang Aktif"}, 201)["activeRoomId"].(string)

	// Ruang aktif tidak bisa diarsipkan.
	f.api("POST", "/api/rooms/"+roomC+"/archive", object{"archived": true}, 400)
	// Arsipkan ruang B.
	out := f.api("POST", "/api/rooms/"+roomB+"/archive", object{"archived": true}, 200)
	archivedSeen := false
	for _, item := range out["rooms"].([]any) {
		r, _ := item.(map[string]any)
		if r["id"] == roomB && r["archived"] == true {
			archivedSeen = true
		}
	}
	if !archivedSeen {
		t.Fatalf("ruang B harus terarsip: %v", out)
	}
	// Pencarian default mengecualikan arsip; includeArchived menyertakan.
	search := f.api("GET", "/api/search?q=kataarsip", nil, 200)
	if len(search["results"].([]any)) != 0 {
		t.Fatalf("pencarian default tidak boleh memuat arsip: %v", search)
	}
	search = f.api("GET", "/api/search?q=kataarsip&includeArchived=1", nil, 200)
	if len(search["results"].([]any)) == 0 {
		t.Fatalf("includeArchived harus memuat pesan arsip: %v", search)
	}
	// Mengaktifkan ruang arsip memulihkannya.
	f.api("POST", "/api/rooms/"+roomB+"/activate", object{}, 200)
	state := f.state()
	if state["activeRoomId"] != roomB {
		t.Fatalf("ruang aktif harus B: %v", state["activeRoomId"])
	}
	for _, item := range state["rooms"].([]any) {
		r, _ := item.(map[string]any)
		if r["id"] == roomB && r["archived"] == true {
			t.Fatal("ruang B harus pulih dari arsip setelah diaktifkan")
		}
	}
	// Pulihkan manual juga bekerja.
	f.api("POST", "/api/rooms/"+roomC+"/archive", object{"archived": true}, 200)
	f.api("POST", "/api/rooms/"+roomC+"/archive", object{"archived": false}, 200)
}
