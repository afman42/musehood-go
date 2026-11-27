package musehood

// Tes integrasi lintas fitur pendalaman: arsip+pencarian, rerun
// membatalkan job berjalan, fork me-remap replyTo, balasan berantai pada
// kursi antrean, health + CRUD provider, log audit dari alur chat, dan
// persona yang benar-benar dipakai diskusi.

import (
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestIntegrationArchiveSearch(t *testing.T) {
	f := setup(t, nil)
	// Ruang kedua dengan kata kunci unik, lalu diarsipkan.
	roomB := f.api("POST", "/api/rooms", object{"name": "Arsip Integrasi"}, 201)["activeRoomId"].(string)
	f.api("POST", "/api/message", object{"text": "@Plamo kataunikarsip integrasi"}, 202)
	f.wait(func(s object) bool { return completedSeatReplies(s) >= 1 })
	roomA := defaultRoomID
	f.api("POST", "/api/rooms/"+roomA+"/activate", object{}, 200)
	f.api("POST", "/api/rooms/"+roomB+"/archive", object{"archived": true}, 200)

	// Pencarian bawaan: ruang terarsip dikecualikan.
	out := f.api("GET", "/api/search?q=kataunikarsip", nil, 200)
	if results, _ := out["results"].([]any); len(results) != 0 {
		t.Fatalf("pencarian bawaan harus mengecualikan arsip: %v", out)
	}
	// includeArchived=1: ditemukan kembali.
	out = f.api("GET", "/api/search?q=kataunikarsip&includeArchived=1", nil, 200)
	results, _ := out["results"].([]any)
	if len(results) == 0 {
		t.Fatalf("includeArchived harus menemukan pesan arsip: %v", out)
	}
	found := false
	for _, item := range results {
		entry, _ := item.(map[string]any)
		if entry["roomId"] == roomB {
			found = true
		}
	}
	if !found {
		t.Fatalf("hasil harus dari ruang terarsip: %v", results)
	}
	// State menandai ruang terarsip dengan flag archived.
	state := f.state()
	archivedSeen := false
	for _, item := range asObjects(state["rooms"]) {
		if item["id"] == roomB && item["archived"] == true {
			archivedSeen = true
		}
	}
	if !archivedSeen {
		t.Fatalf("ruang terarsip harus berflag archived di state: %v", state["rooms"])
	}
	// Mengaktifkan ruang terarsip membuka arsipnya kembali.
	f.api("POST", "/api/rooms/"+roomB+"/activate", object{}, 200)
	state = f.state()
	for _, item := range asObjects(state["rooms"]) {
		if item["id"] == roomB && item["archived"] == true {
			t.Fatal("ruang aktif tidak boleh tetap terarsip")
		}
	}
	out = f.api("GET", "/api/search?q=kataunikarsip", nil, 200)
	if results, _ := out["results"].([]any); len(results) == 0 {
		t.Fatal("setelah dibuka arsipnya, pencarian bawaan harus menemukan lagi")
	}
}

func TestIntegrationRerunCancelsRunningJob(t *testing.T) {
	var calls atomic.Int32
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	var mu sync.Mutex
	bodies := []string{}
	extra := func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}
		raw, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, string(raw))
		mu.Unlock()
		n := calls.Add(1)
		if n == 2 {
			started <- struct{}{}
			<-release
			io.WriteString(w, `{"choices":[{"message":{"content":"HASIL LAMA HARUS HILANG"}}]}`)
			return
		}
		io.WriteString(w, `{"choices":[{"message":{"content":"balasan integrasi rerun"}}]}`)
	}
	f := setup(t, extra)
	f.api("POST", "/api/message", object{"text": "@Plamo pertama integrasi"}, 202)
	state := f.wait(func(s object) bool { return completedSeatReplies(s) >= 1 })
	owner1 := findOwnerMessage(state, "@Plamo pertama integrasi")

	f.api("POST", "/api/message", object{"text": "@Plamo kedua integrasi"}, 202)
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("panggilan gateway kedua tidak mulai")
	}
	// Rerun dari pesan pertama: job kedua yang berjalan dibatalkan.
	f.api("POST", "/api/message/"+owner1["id"].(string)+"/rerun", object{}, 202)
	close(release)

	state = f.wait(func(s object) bool {
		for _, j := range asObjects(s["jobs"]) {
			if j["status"] != "completed" {
				return false
			}
		}
		return completedSeatReplies(s) >= 1
	})
	all := ""
	for _, m := range messagesOf(state) {
		all += m["text"].(string) + "\n"
	}
	if strings.Contains(all, "kedua integrasi") {
		t.Fatalf("pesan sesudah titik rerun harus hilang: %q", all)
	}
	if strings.Contains(all, "HASIL LAMA HARUS HILANG") {
		t.Fatalf("hasil job yang dibatalkan tidak boleh mendarat: %q", all)
	}
	// Konteks panggilan gateway untuk rerun tidak memuat pesan terhapus.
	mu.Lock()
	defer mu.Unlock()
	if len(bodies) < 3 {
		t.Fatalf("harus ada panggilan gateway ketiga (rerun), dapat %d", len(bodies))
	}
	if strings.Contains(bodies[2], "kedua integrasi") {
		t.Fatal("konteks kursi sesudah rerun tidak boleh memuat pesan yang dihapus")
	}
	if !strings.Contains(bodies[2], "pertama integrasi") {
		t.Fatal("konteks rerun harus memuat pesan titik rerun")
	}
}

func TestIntegrationForkReplyToRemap(t *testing.T) {
	f := setup(t, nil)
	f.api("POST", "/api/message", object{"text": "@Plamo awal fork integrasi"}, 202)
	state := f.wait(func(s object) bool { return completedSeatReplies(s) >= 1 })
	var seatMsg map[string]any
	for _, m := range messagesOf(state) {
		if m["authorType"] == "seat" {
			seatMsg = m
		}
	}
	// Pemilik membalas pesan kursi itu (replyTo terisi).
	f.api("POST", "/api/message", object{"text": "@Plamo balasan pemilik fork", "replyTo": seatMsg["id"].(string)}, 202)
	state = f.wait(func(s object) bool { return completedSeatReplies(s) >= 2 })
	reply := findOwnerMessage(state, "@Plamo balasan pemilik fork")
	if reply["replyTo"] != seatMsg["id"] {
		t.Fatalf("replyTo pemilik = %v", reply)
	}

	out := f.api("POST", "/api/message/"+reply["id"].(string)+"/fork", object{}, 200)
	forkRoom := out["roomId"].(string)
	state = f.state()
	if state["activeRoomId"] != forkRoom {
		t.Fatal("fork harus menjadi ruang aktif")
	}
	msgs := messagesOf(state)
	if len(msgs) != 3 {
		t.Fatalf("fork harus berisi 3 pesan, dapat %d", len(msgs))
	}
	// Semua ID baru; replyTo salinan menunjuk ID baru pesan kursi di fork.
	ids := map[string]bool{}
	for _, m := range msgs {
		ids[m["id"].(string)] = true
	}
	if ids[seatMsg["id"].(string)] || ids[reply["id"].(string)] {
		t.Fatal("pesan fork harus ber-ID baru")
	}
	var copiedReply, copiedSeat map[string]any
	for _, m := range msgs {
		if m["text"] == "@Plamo balasan pemilik fork" {
			copiedReply = m
		}
		if m["authorType"] == "seat" && m["text"] == seatMsg["text"] {
			copiedSeat = m
		}
	}
	if copiedReply == nil || copiedSeat == nil {
		t.Fatalf("salinan fork tidak lengkap: %v", msgs)
	}
	if copiedReply["replyTo"] != copiedSeat["id"] {
		t.Fatalf("replyTo fork harus ter-remap ke ID baru: %v vs %v", copiedReply["replyTo"], copiedSeat["id"])
	}
}

func TestIntegrationChainRepliesQueueSeat(t *testing.T) {
	f := setup(t, nil)
	// Ruang hanya M01 lalu MUSE; round-robin + balasan berantai menyala.
	f.api("POST", "/api/rooms", object{"name": "Rantai Integrasi", "seatCodes": []string{"M01", "MUSE"}, "seatOrder": []string{"M01", "MUSE"}}, 201)
	f.api("POST", "/api/settings", object{"settings": object{"roundRobin": true, "chainReplies": true}}, 200)
	f.api("POST", "/api/message", object{"text": "topik rantai integrasi"}, 202)

	state := f.wait(func(s object) bool { return completedSeatReplies(s) >= 1 })
	var m01Msg map[string]any
	for _, m := range messagesOf(state) {
		if m["authorType"] == "seat" && m["seat"] == "M01" {
			m01Msg = m
		}
	}
	if m01Msg == nil {
		t.Fatal("balasan M01 tidak ditemukan")
	}
	pending := f.pending("chat")
	if pending["seat"] != "MUSE" {
		t.Fatalf("langkah kedua harus antrean MUSE: %v", pending)
	}
	if prompt, _ := pending["prompt"].(string); !strings.Contains(prompt, "tanggapi secara eksplisit") {
		t.Fatalf("prompt antrean harus memuat instruksi rantai: %q", prompt)
	}
	f.api("POST", "/api/queue/muse/"+pending["id"].(string), object{"text": "Menanggapi Plamo: setuju."}, 200)
	state = f.wait(func(s object) bool { return completedSeatReplies(s) >= 2 })
	for _, m := range messagesOf(state) {
		if m["seat"] == "MUSE" {
			if m["replyTo"] != m01Msg["id"] {
				t.Fatalf("replyTo pesan Muse harus pesan M01: %v", m)
			}
			return
		}
	}
	t.Fatal("pesan MUSE tidak ditemukan")
}

func TestIntegrationHealthProviderDelete(t *testing.T) {
	f := setup(t, nil)
	// Provider baru via CRUD.
	created := f.api("POST", "/api/providers", object{"name": "Provider Integrasi", "baseUrl": "http://127.0.0.1:1/v1", "apiKey": "kunci-integrasi", "protocol": "openai"}, 201)
	id := created["id"].(string)

	// Belum pernah dicek: tidak ada field health.
	out := f.api("GET", "/api/providers", nil, 200)
	for _, item := range asObjects(out["providers"]) {
		if item["id"] == id {
			if _, ok := item["health"]; ok {
				t.Fatalf("provider baru tidak boleh punya health: %v", item)
			}
		}
	}
	// Cek health: 127.0.0.1:1 menolak koneksi → ok=false tercatat.
	checked := f.api("POST", "/api/providers/"+id+"/health", object{}, 200)
	health, _ := checked["health"].(map[string]any)
	if health["ok"] != false || health["error"] == nil || health["error"] == "" {
		t.Fatalf("health gagal harus tercatat dengan error: %v", checked)
	}
	out = f.api("GET", "/api/providers", nil, 200)
	seen := false
	for _, item := range asObjects(out["providers"]) {
		if item["id"] == id {
			if _, ok := item["health"]; ok {
				seen = true
			}
		}
	}
	if !seen {
		t.Fatal("health harus ter-overlay di daftar provider")
	}
	// Hapus provider: hilang dari daftar dan health tidak bisa dicek lagi.
	f.api("DELETE", "/api/providers/"+id, nil, 200)
	out = f.api("GET", "/api/providers", nil, 200)
	for _, item := range asObjects(out["providers"]) {
		if item["id"] == id {
			t.Fatal("provider terhapus masih muncul")
		}
	}
	status, raw := rawRequest(t, f, "POST", "/api/providers/"+id+"/health", mustJSON(object{}), nil)
	assertErrorShape(t, status, raw, 404)
	f.app.healthMu.Lock()
	_, stillThere := f.app.health[id]
	f.app.healthMu.Unlock()
	if stillThere {
		t.Fatal("peta health harus dibersihkan saat provider dihapus")
	}
}

func TestIntegrationToolLogFromRealChat(t *testing.T) {
	var calls atomic.Int32
	extra := func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}
		if calls.Add(1) == 1 {
			io.WriteString(w, `{"choices":[{"message":{"content":"<<TOOL:current_time>><</TOOL>>"}}]}`)
			return
		}
		io.WriteString(w, `{"choices":[{"message":{"content":"sekarang jam segini"}}]}`)
	}
	f := setup(t, extra)
	f.api("POST", "/api/message", object{"text": "@Plamo jam berapa sekarang integrasi"}, 202)
	f.wait(func(s object) bool { return completedSeatReplies(s) >= 1 })

	out := f.api("GET", "/api/tool-log", nil, 200)
	entries, _ := out["entries"].([]any)
	found := false
	for _, item := range entries {
		entry, _ := item.(map[string]any)
		if entry["tool"] == "current_time" && entry["seatCode"] == "M01" && entry["ok"] == true {
			found = true
		}
	}
	if !found {
		t.Fatalf("log audit harus terisi dari alur chat nyata: %v", out)
	}
	// Kartu tool di pesan kursi juga tercatat.
	state := f.state()
	for _, m := range messagesOf(state) {
		if m["authorType"] == "seat" {
			cards, _ := m["toolCalls"].([]any)
			if len(cards) == 1 {
				card, _ := cards[0].(map[string]any)
				if card["name"] == "current_time" && card["ok"] == true {
					return
				}
			}
		}
	}
	t.Fatal("kartu tool current_time tidak ditemukan di pesan kursi")
}

func TestIntegrationPersonaUsedInDiscussion(t *testing.T) {
	capture := &captureGateway{}
	f := setup(t, capture.handler())
	f.api("POST", "/api/seats/M02/persona", object{"personaId": "reviewer-ketat"}, 200)
	f.api("POST", "/api/message", object{"text": "@GLM tinjau kode ini integrasi"}, 202)
	f.wait(func(s object) bool { return completedSeatReplies(s) >= 1 })
	persona := findPersona("reviewer-ketat")
	if persona == nil {
		t.Fatal("persona reviewer-ketat hilang")
	}
	// Prompt persona (potongan khas) harus masuk body gateway.
	needle := persona.Prompt
	if len([]rune(needle)) > 40 {
		needle = string([]rune(needle)[:40])
	}
	if !strings.Contains(capture.all(), needle) {
		t.Fatal("prompt persona tidak dipakai dalam diskusi")
	}
	// Kursi lain tidak memakai prompt persona itu.
	capture.bodies = nil
	f.api("POST", "/api/message", object{"text": "@Plamo pertanyaan biasa integrasi"}, 202)
	f.wait(func(s object) bool { return completedSeatReplies(s) >= 2 })
	if strings.Contains(capture.all(), needle) {
		t.Fatal("prompt persona bocor ke kursi lain")
	}
	// Persona penerjemah menonaktifkan run_code/run_shell di instruksi tool.
	f.api("POST", "/api/seats/M03/persona", object{"personaId": "penerjemah"}, 200)
	capture.bodies = nil
	f.api("POST", "/api/message", object{"text": "@Argon terjemahkan halo integrasi"}, 202)
	f.wait(func(s object) bool { return completedSeatReplies(s) >= 3 })
	if strings.Contains(capture.all(), "- run_code:") || strings.Contains(capture.all(), "- run_shell:") {
		t.Fatal("persona penerjemah harus menonaktifkan run_code/run_shell")
	}
}
