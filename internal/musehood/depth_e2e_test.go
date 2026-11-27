package musehood

// Tes E2E service untuk fitur pendalaman: alur penuh lewat HTTP dengan
// gateway mock — PIN menyeluruh, debat sampai kesimpulan integrator
// antrean Muse, skill terpasang memengaruhi prompt, dan cache respons
// pada alur pesan nyata.

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestE2EPINFullFlow(t *testing.T) {
	f := setup(t, nil)
	base := f.server.URL

	if status, _, _ := doRaw(t, "POST", base+"/api/pin", object{"pin": "2468"}, nil); status != 200 {
		t.Fatal("set PIN gagal")
	}
	// Tanpa login: state + kirim pesan ditolak.
	if status, _, _ := doRaw(t, "GET", base+"/api/state", nil, nil); status != 401 {
		t.Fatalf("state tanpa sesi = %d", status)
	}
	if status, _, _ := doRaw(t, "POST", base+"/api/message", object{"text": "@Plamo e2e pin"}, nil); status != 401 {
		t.Fatalf("kirim pesan tanpa sesi = %d", status)
	}
	// Login salah lalu benar.
	if status, _, _ := doRaw(t, "POST", base+"/api/login", object{"pin": "0000"}, nil); status != 401 {
		t.Fatalf("login salah = %d", status)
	}
	auth := loginCookie(t, f, "2468")

	// Kirim pesan dengan sesi: kursi membalas sampai selesai.
	status, _, raw := doRaw(t, "POST", base+"/api/message", object{"text": "@Plamo e2e pin"}, auth)
	if status != 202 {
		t.Fatalf("kirim pesan dengan sesi = %d %s", status, raw)
	}
	deadline := time.Now().Add(5 * time.Second)
	var state object
	for {
		_, _, stateRaw := doRaw(t, "GET", base+"/api/state", nil, auth)
		if err := json.Unmarshal(stateRaw, &state); err != nil {
			t.Fatal(err)
		}
		if completedSeatReplies(state) >= 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("balasan kursi tidak selesai: %s", stateRaw)
		}
		time.Sleep(10 * time.Millisecond)
	}
	// Logout: akses ditolak lagi.
	if status, _, _ := doRaw(t, "POST", base+"/api/logout", object{}, auth); status != 200 {
		t.Fatalf("logout = %d", status)
	}
	if status, _, _ := doRaw(t, "GET", base+"/api/state", nil, nil); status != 401 {
		t.Fatalf("state setelah logout = %d", status)
	}
	// Login ulang tetap bekerja.
	auth = loginCookie(t, f, "2468")
	if status, _, _ := doRaw(t, "GET", base+"/api/state", nil, auth); status != 200 {
		t.Fatalf("state setelah login ulang = %d", status)
	}
}

func TestE2EDebateWithQueueIntegrator(t *testing.T) {
	capture := &captureGateway{}
	f := setup(t, capture.handler())
	// Integrator bawaan MUSE (kursi antrean) — kesimpulan lewat antrean.
	f.api("POST", "/api/debate", object{"seatA": "M01", "seatB": "M02", "topic": "Hujan itu baik"}, 202)

	// Tunggu 4 tahap gateway selesai, lalu kesimpulan masuk antrean Muse.
	state := f.wait(func(s object) bool { return completedSeatReplies(s) >= 4 })
	pending := f.pending("debate-summary")
	if pending["seat"] != "MUSE" {
		t.Fatalf("kesimpulan harus di antrean MUSE: %v", pending)
	}
	if prompt, _ := pending["prompt"].(string); !strings.Contains(prompt, "kesimpulan debat") {
		t.Fatalf("prompt kesimpulan kehilangan instruksi: %q", prompt)
	}
	f.api("POST", "/api/queue/muse/"+pending["id"].(string), object{"text": "Kesimpulan: kedua pihak seimbang."}, 200)
	state = f.wait(func(s object) bool { return completedSeatReplies(s) >= 5 })

	wantLabels := []string{"Debat — Pro", "Debat — Kontra", "Debat — Sanggahan", "Debat — Sanggahan", "Debat — Kesimpulan"}
	wantSeats := []string{"M01", "M02", "M01", "M02", "MUSE"}
	seatMsgs := []map[string]any{}
	for _, m := range messagesOf(state) {
		if m["authorType"] == "seat" {
			seatMsgs = append(seatMsgs, m)
		}
	}
	if len(seatMsgs) != 5 {
		t.Fatalf("debat harus 5 balasan, dapat %d", len(seatMsgs))
	}
	for i, m := range seatMsgs {
		if m["label"] != wantLabels[i] || m["seat"] != wantSeats[i] || m["status"] != "completed" {
			t.Fatalf("langkah %d = label %v seat %v status %v", i, m["label"], m["seat"], m["status"])
		}
	}
	if seatMsgs[4]["text"] != "Kesimpulan: kedua pihak seimbang." {
		t.Fatalf("teks kesimpulan = %v", seatMsgs[4]["text"])
	}
	// Job selesai.
	for _, j := range asObjects(state["jobs"]) {
		if j["status"] != "completed" {
			t.Fatalf("job debat belum selesai: %v", j)
		}
	}
}

func TestE2EInstalledSkillInfluencesReply(t *testing.T) {
	oldFetch := skillFetchURL
	skillFetchURL = func(ctx context.Context, rawURL string) ([]byte, error) {
		return []byte("---\nname: gaya-e2e\ndescription: Gaya E2E\n---\nSelalu awali jawaban dengan kata SANDI-E2E."), nil
	}
	defer func() { skillFetchURL = oldFetch }()

	capture := &captureGateway{}
	f := setup(t, capture.handler())
	out := f.api("POST", "/api/skills/install", object{"url": "https://contoh.test/gaya.md"}, 200)
	if out["name"] != "gaya-e2e" {
		t.Fatalf("install = %v", out)
	}
	// Tempel skill ke kursi M01 lewat patch settings.
	f.api("POST", "/api/settings", object{"seats": []object{{"code": "M01", "skills": []string{"gaya-e2e"}}}}, 200)

	f.api("POST", "/api/message", object{"text": "@Plamo halo e2e skill"}, 202)
	f.wait(func(s object) bool { return completedSeatReplies(s) >= 1 })
	if !strings.Contains(capture.all(), "SANDI-E2E") {
		t.Fatal("isi skill terpasang harus masuk prompt gateway kursi")
	}
	// Kursi lain tanpa skill tidak memuatnya.
	capture.bodies = nil
	f.api("POST", "/api/message", object{"text": "@GLM halo tanpa skill"}, 202)
	f.wait(func(s object) bool { return completedSeatReplies(s) >= 2 })
	if strings.Contains(capture.all(), "SANDI-E2E") {
		t.Fatal("kursi tanpa skill tidak boleh memuat isi skill kursi lain")
	}
}

func TestE2ECacheThroughMessageFlow(t *testing.T) {
	var calls atomic.Int32
	extra := func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" && r.URL.Path == "/v1/chat/completions" {
			calls.Add(1)
			w.Write([]byte(`{"choices":[{"message":{"content":"jawaban cache e2e"}}]}`))
			return
		}
		http.NotFound(w, r)
	}
	f := setup(t, extra)
	f.api("POST", "/api/settings", object{"settings": object{"responseCacheEnabled": true}}, 200)

	// Pertanyaan identik di ruang utama.
	f.api("POST", "/api/message", object{"text": "@Plamo pertanyaan cache e2e"}, 202)
	f.wait(func(s object) bool { return completedSeatReplies(s) >= 1 })
	if calls.Load() != 1 {
		t.Fatalf("gateway harus dipukul 1x, dapat %d", calls.Load())
	}
	// Ruang baru: riwayat (system + user) identik → cache hit.
	room := f.api("POST", "/api/rooms", object{"name": "Ruang Cache"}, 201)
	_ = room
	f.api("POST", "/api/message", object{"text": "@Plamo pertanyaan cache e2e"}, 202)
	f.wait(func(s object) bool { return completedSeatReplies(s) >= 1 })
	if calls.Load() != 1 {
		t.Fatalf("pertanyaan identik di ruang lain harus dari cache, gateway dipukul %d", calls.Load())
	}
	stats := f.api("GET", "/api/stats", nil, 200)
	if hits, _ := stats["cacheHits"].(float64); hits < 1 {
		t.Fatalf("cacheHits harus >= 1: %v", stats)
	}
	// Pertanyaan berbeda memukul gateway lagi.
	f.api("POST", "/api/message", object{"text": "@Plamo pertanyaan lain e2e"}, 202)
	f.wait(func(s object) bool { return completedSeatReplies(s) >= 2 })
	if calls.Load() != 2 {
		t.Fatalf("pertanyaan berbeda harus memukul gateway, total %d", calls.Load())
	}
}
