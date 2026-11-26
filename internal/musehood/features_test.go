package musehood

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Data lama tanpa field ruang harus dimigrasi ke ruang bawaan "Utama".
func TestRoomsMigrationFromLegacyData(t *testing.T) {
	dir := t.TempDir()
	a, err := New(Options{DataDir: dir, PollInterval: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	a.mu.Lock()
	a.data.Messages = append(a.data.Messages, &message{ID: uuid(), CreatedAt: now(), AuthorType: "owner", Status: "completed", Text: "halo lama", RoomID: defaultRoomID})
	if err := a.saveLocked(); err != nil {
		t.Fatal(err)
	}
	a.mu.Unlock()
	a.Close()

	raw, err := os.ReadFile(filepath.Join(dir, "data.json"))
	if err != nil {
		t.Fatal(err)
	}
	var legacy map[string]any
	if err := json.Unmarshal(raw, &legacy); err != nil {
		t.Fatal(err)
	}
	delete(legacy, "rooms")
	delete(legacy, "activeRoomId")
	delete(legacy, "presets")
	delete(legacy, "stats")
	for _, m := range legacy["messages"].([]any) {
		delete(m.(map[string]any), "roomId")
	}
	raw, _ = json.Marshal(legacy)
	if err := os.WriteFile(filepath.Join(dir, "data.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}

	d, err := loadData(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Rooms) != 1 || d.Rooms[0].ID != defaultRoomID || d.ActiveRoomID != defaultRoomID {
		t.Fatalf("migrasi ruang gagal: %+v aktif=%s", d.Rooms, d.ActiveRoomID)
	}
	if len(d.Messages) != 1 || d.Messages[0].RoomID != defaultRoomID {
		t.Fatalf("pesan lama tidak pindah ke ruang bawaan: %+v", d.Messages)
	}
}

func TestRoomsCRUDAndIsolation(t *testing.T) {
	f := setup(t, nil)
	f.api("POST", "/api/message", object{"text": "@Plamo halo utama"}, 202)
	f.finish(2)

	out := f.api("POST", "/api/rooms", object{"name": "Coding", "seatCodes": []string{"M01", "M02"}}, 201)
	rooms := asObjects(out["rooms"])
	if len(rooms) != 2 || out["activeRoomId"] == defaultRoomID {
		t.Fatalf("ruang baru tidak aktif: %v", out)
	}
	codingID := out["activeRoomId"].(string)

	s := f.state()
	if len(messages(s)) != 0 {
		t.Fatalf("ruang baru harus kosong: %v", s["messages"])
	}
	// @Opus (M10) bukan peserta ruang Coding: diskusi bergilir peserta ruang saja.
	f.api("POST", "/api/message", object{"text": "@Opus halo coding"}, 202)
	msgs := f.finish(3)
	seats := map[string]bool{}
	for _, m := range msgs {
		if m["authorType"] == "seat" {
			seats[m["seat"].(string)] = true
		}
	}
	if seats["M10"] || !seats["M01"] || !seats["M02"] {
		t.Fatalf("peserta ruang tidak dihormati: %v", seats)
	}

	// Kembali ke ruang utama: pesan Coding tidak ikut tampil.
	f.api("POST", "/api/rooms/"+defaultRoomID+"/activate", object{}, 200)
	s = f.state()
	if len(messages(s)) != 2 {
		t.Fatalf("ruang utama berubah: %d pesan", len(messages(s)))
	}

	// Hapus ruang Coding; ruang terakhir tidak bisa dihapus.
	f.api("DELETE", "/api/rooms/"+codingID, nil, 200)
	f.api("DELETE", "/api/rooms/"+defaultRoomID, nil, 400)
}

func TestUpdateRoom(t *testing.T) {
	f := setup(t, nil)
	out := f.api("POST", "/api/rooms", object{"name": "Awal", "seatCodes": []string{"M01"}}, 201)
	id := out["activeRoomId"].(string)

	updated := f.api("POST", "/api/rooms/"+id, object{"name": "Diganti", "seatCodes": []string{"M01", "M02"}}, 200)
	rooms := asObjects(updated["rooms"])
	found := false
	for _, r := range rooms {
		if r["id"] == id {
			found = true
			if r["name"] != "Diganti" || len(r["seatCodes"].([]any)) != 2 {
				t.Fatalf("ruang tidak terupdate: %v", r)
			}
		}
	}
	if !found {
		t.Fatal("ruang hasil update tidak ditemukan")
	}
	f.api("POST", "/api/rooms/"+id, object{"seatCodes": []string{"TIDAKADA"}}, 400)
	f.api("POST", "/api/rooms/room-hantu", object{"name": "X"}, 404)
	f.api("POST", "/api/rooms/"+id, object{"name": "  "}, 400)
}

func TestVoteOnlyQueueAnswersRejected(t *testing.T) {
	f := setup(t, nil)
	// Jadikan M01 kursi antrean juga, lalu diskusi dijawab dua kursi antrean.
	f.api("POST", "/api/settings", object{"seats": []object{{"code": "M01", "provider": "muse"}}}, 200)
	f.api("POST", "/api/message", object{"text": "@Muse @Plamo halo antrean"}, 202)
	for range 2 {
		q := f.pending("chat")
		f.api("POST", "/api/queue/muse/"+q["id"].(string), object{"text": "jawaban antrean"}, 200)
	}
	f.finish(3)
	// Kandidat ada dua, tetapi tidak ada pemilih gateway.
	f.api("POST", "/api/vote", object{}, 409)
}

func TestPresetApplyNameCollisionRejected(t *testing.T) {
	f := setup(t, nil)
	preset := f.api("POST", "/api/presets", object{"name": "Tabrakan"}, 201)
	id := preset["id"].(string)
	f.app.mu.Lock()
	for i := range f.app.data.Presets {
		if f.app.data.Presets[i].ID == id {
			f.app.data.Presets[i].Seats[1].Name = f.app.data.Presets[i].Seats[0].Name
		}
	}
	f.app.mu.Unlock()
	f.api("POST", "/api/presets/"+id+"/apply", object{}, 400)
}

func TestRestoreRejectsInvalidJob(t *testing.T) {
	f := setup(t, nil)
	res, err := http.Get(f.server.URL + "/api/backup")
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(res.Body)
	res.Body.Close()
	var backup object
	if json.Unmarshal(raw, &backup) != nil {
		t.Fatal("cadangan bukan JSON")
	}
	backup["jobs"] = []object{{"id": uuid(), "prompt": "x", "kind": "chat", "createdAt": now(), "status": "processing", "index": 9, "steps": []object{}}}
	f.api("POST", "/api/restore", backup, 400)
	// Data asli tidak berubah setelah restore ditolak.
	if len(messages(f.state())) != 0 {
		t.Fatal("state berubah setelah restore gagal")
	}
}

func TestPinMessage(t *testing.T) {
	f := setup(t, nil)
	f.api("POST", "/api/message", object{"text": "@Plamo sematkan ini"}, 202)
	msgs := f.finish(2)
	ownerID := msgs[0]["id"].(string)

	out := f.api("POST", "/api/message/"+ownerID+"/pin", object{}, 200)
	if out["pinned"] != true {
		t.Fatalf("pin tidak aktif: %v", out)
	}
	out = f.api("POST", "/api/message/"+ownerID+"/pin", object{}, 200)
	if out["pinned"] == true {
		t.Fatalf("pin tidak lepas: %v", out)
	}
}

func TestSeatMemoryAndFallbackSettings(t *testing.T) {
	f := setup(t, nil)
	f.api("POST", "/api/settings", object{"seats": []object{{"code": "M01", "memory": "Pemilik suka jawaban singkat.", "fallbackModel": "plain"}}}, 200)
	s := f.state()
	for _, seat := range asObjects(s["seats"]) {
		if seat["code"] == "M01" {
			if seat["memory"] != "Pemilik suka jawaban singkat." || seat["fallbackModel"] != "plain" {
				t.Fatalf("memori/fallback tidak tersimpan: %v", seat)
			}
		}
	}
	f.app.mu.Lock()
	member := f.app.data.findSeat("M01")
	prompt := f.app.systemPrompt(*member, f.app.data)
	f.app.mu.Unlock()
	if !strings.Contains(prompt, "Pemilik suka jawaban singkat.") {
		t.Fatalf("memori tidak masuk system prompt: %s", prompt)
	}
}

func TestFallbackModelUsedOnError(t *testing.T) {
	f := setup(t, nil)
	// Model utama "denied" selalu 401 di gateway mock; fallback "plain" berhasil.
	f.api("POST", "/api/settings", object{"seats": []object{{"code": "M01", "model": "denied", "fallbackModel": "plain"}}}, 200)
	f.api("POST", "/api/message", object{"text": "@Plamo coba fallback"}, 202)
	msgs := f.finish(2)
	reply := msgs[1]
	if reply["status"] != "completed" || reply["text"] != "teks polos" || reply["error"] != nil {
		t.Fatalf("fallback tidak dipakai: %v", reply)
	}
}

func TestUsageStatsRecorded(t *testing.T) {
	f := setup(t, nil)
	f.api("POST", "/api/message", object{"text": "@Plamo hitung statistik"}, 202)
	f.finish(2)
	out := f.api("GET", "/api/stats", nil, 200)
	stats, _ := out["stats"].(map[string]any)
	seatStat, _ := stats["seat:M01"].(map[string]any)
	if seatStat == nil || seatStat["requests"].(float64) < 1 || seatStat["success"].(float64) < 1 {
		t.Fatalf("statistik kursi tidak tercatat: %v", stats)
	}
	found := false
	for key := range stats {
		if strings.HasPrefix(key, "model:") && strings.HasSuffix(key, "/array") {
			found = true
		}
	}
	if !found {
		t.Fatalf("statistik model tidak tercatat: %v", stats)
	}
}

func TestPresetsSaveApplyDelete(t *testing.T) {
	f := setup(t, nil)
	preset := f.api("POST", "/api/presets", object{"name": "Susunan awal"}, 201)
	id := preset["id"].(string)

	f.api("POST", "/api/settings", object{"seats": []object{{"code": "M01", "name": "Diganti"}}}, 200)
	out := f.api("POST", "/api/presets/"+id+"/apply", object{}, 200)
	if out["applied"].(float64) != 7 {
		t.Fatalf("preset tidak diterapkan ke semua kursi: %v", out)
	}
	s := f.state()
	for _, seat := range asObjects(s["seats"]) {
		if seat["code"] == "M01" && seat["name"] != "Plamo" {
			t.Fatalf("nama kursi tidak kembali dari preset: %v", seat)
		}
	}
	f.api("DELETE", "/api/presets/"+id, nil, 200)
	if got := asObjects(f.api("GET", "/api/presets", nil, 200)["presets"]); len(got) != 0 {
		t.Fatalf("preset tidak terhapus: %v", got)
	}
}

func TestVoteBestAnswer(t *testing.T) {
	f := setup(t, func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Model    string
			Messages []struct{ Content string }
		}
		json.NewDecoder(r.Body).Decode(&body)
		joined := ""
		for _, m := range body.Messages {
			joined += m.Content + "\n"
		}
		reply := "jawaban " + body.Model
		if strings.Contains(joined, "Jawaban kandidat") {
			reply = "M01 jawaban paling konkret dan lengkap."
		}
		json.NewEncoder(w).Encode(object{"choices": []object{{"message": object{"content": reply}}}})
	})
	f.api("POST", "/api/vote", object{}, 409)

	f.api("POST", "/api/message", object{"text": "@Opus @Plamo diskusi dulu"}, 202)
	f.finish(3)
	f.api("POST", "/api/vote", object{}, 202)
	// 3 pesan diskusi + 2 pesan suara + 1 ringkasan integrator.
	msgs := f.finish(6)
	var winner object
	summary := false
	for _, m := range msgs {
		if m["bestAnswer"] == true {
			winner = m
		}
		if m["label"] == voteLabel && strings.Contains(m["text"].(string), "Hasil voting") {
			summary = true
		}
	}
	if winner == nil || winner["seat"] != "M01" || winner["votes"].(float64) != 1 {
		t.Fatalf("pemenang voting salah: %v", winner)
	}
	if !summary {
		t.Fatal("ringkasan voting tidak ada")
	}
}

func TestBackupRestorePreservesKeys(t *testing.T) {
	f := setup(t, nil)
	res, err := http.Get(f.server.URL + "/api/backup")
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != 200 || strings.Contains(string(raw), secret) {
		t.Fatalf("cadangan bocor atau gagal: %d", res.StatusCode)
	}
	var backup object
	if json.Unmarshal(raw, &backup) != nil {
		t.Fatal("cadangan bukan JSON")
	}
	// Pulihkan cadangan yang sama: kunci provider harus tetap ada.
	f.api("POST", "/api/restore", backup, 200)
	s := f.state()
	if s["settings"].(object)["hasApiKey"] != true {
		t.Fatal("kunci API hilang setelah restore")
	}
	// Cadangan tidak valid ditolak.
	f.api("POST", "/api/restore", object{"seats": []object{}}, 400)
}

func TestSearchAcrossRoomsAndExportHTML(t *testing.T) {
	f := setup(t, nil)
	f.api("POST", "/api/message", object{"text": "@Plamo zebra di utama"}, 202)
	f.finish(2)
	out := f.api("POST", "/api/rooms", object{"name": "Lain"}, 201)
	_ = out
	f.api("POST", "/api/message", object{"text": "@Plamo zebra di ruang lain"}, 202)
	f.finish(2)

	found := f.api("GET", "/api/search?q=zebra", nil, 200)
	results := asObjects(found["results"])
	if len(results) < 2 {
		t.Fatalf("pencarian lintas ruang kurang: %v", results)
	}
	rooms := map[string]bool{}
	for _, r := range results {
		rooms[r["roomName"].(string)] = true
	}
	if !rooms["Utama"] || !rooms["Lain"] {
		t.Fatalf("hasil pencarian tidak mencakup dua ruang: %v", rooms)
	}

	res, err := http.Get(f.server.URL + "/api/export?format=html")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != 200 || !strings.Contains(res.Header.Get("Content-Type"), "text/html") || !strings.Contains(string(body), "zebra di ruang lain") {
		t.Fatalf("ekspor HTML gagal: %d %s", res.StatusCode, res.Header.Get("Content-Type"))
	}
}
