package musehood

import (
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
)

func messagesOf(state object) []map[string]any {
	out := []map[string]any{}
	for _, item := range state["messages"].([]any) {
		m, _ := item.(map[string]any)
		out = append(out, m)
	}
	return out
}

func findOwnerMessage(state object, text string) map[string]any {
	for _, m := range messagesOf(state) {
		if m["authorType"] == "owner" && m["text"] == text {
			return m
		}
	}
	return nil
}

func completedSeatReplies(state object) int {
	count := 0
	for _, m := range messagesOf(state) {
		if m["authorType"] == "seat" && m["status"] == "completed" {
			count++
		}
	}
	return count
}

func TestRerunFromOwnerMessage(t *testing.T) {
	f := setup(t, nil)
	f.api("POST", "/api/message", object{"text": "@Plamo pertanyaan pertama"}, 202)
	state := f.wait(func(s object) bool { return completedSeatReplies(s) >= 1 })
	owner := findOwnerMessage(state, "@Plamo pertanyaan pertama")
	if owner == nil {
		t.Fatal("pesan pemilik tidak ditemukan")
	}
	oldJob := owner["jobId"].(string)
	f.api("POST", "/api/message", object{"text": "@Plamo pertanyaan kedua"}, 202)
	f.wait(func(s object) bool { return completedSeatReplies(s) >= 2 })

	// Rerun hanya untuk pesan pemilik.
	var seatMsg map[string]any
	for _, m := range messagesOf(f.state()) {
		if m["authorType"] == "seat" {
			seatMsg = m
			break
		}
	}
	f.api("POST", "/api/message/"+seatMsg["id"].(string)+"/rerun", object{}, 403)

	out := f.api("POST", "/api/message/"+owner["id"].(string)+"/rerun", object{}, 202)
	if out["jobId"] == oldJob || out["jobId"] == "" {
		t.Fatalf("job rerun harus baru: %v vs %s", out, oldJob)
	}
	state = f.state()
	if findOwnerMessage(state, "@Plamo pertanyaan kedua") != nil {
		t.Fatal("pesan sesudah titik rerun harus terhapus")
	}
	rerunOwner := findOwnerMessage(state, "@Plamo pertanyaan pertama")
	if rerunOwner == nil || rerunOwner["jobId"] != out["jobId"] {
		t.Fatalf("pesan pemilik harus menunjuk job baru: %v", rerunOwner)
	}
	f.api("POST", "/api/message/00000000-0000-4000-8000-000000000000/rerun", object{}, 404)
}

func TestForkMessage(t *testing.T) {
	f := setup(t, nil)
	f.api("POST", "/api/message", object{"text": "@Plamo mulai fork"}, 202)
	state := f.wait(func(s object) bool { return completedSeatReplies(s) >= 1 })
	var seatMsg map[string]any
	for _, m := range messagesOf(state) {
		if m["authorType"] == "seat" {
			seatMsg = m
		}
	}
	originalRoom := state["activeRoomId"].(string)
	out := f.api("POST", "/api/message/"+seatMsg["id"].(string)+"/fork", object{}, 200)
	newRoom := out["roomId"].(string)
	if newRoom == originalRoom {
		t.Fatal("fork harus membuat ruang baru")
	}
	state = f.state()
	if state["activeRoomId"] != newRoom {
		t.Fatalf("ruang fork harus aktif: %v", state["activeRoomId"])
	}
	if got := len(messagesOf(state)); got != 2 {
		t.Fatalf("ruang fork harus berisi 2 pesan salinan, dapat %d", got)
	}
	roomName := ""
	for _, item := range state["rooms"].([]any) {
		r, _ := item.(map[string]any)
		if r["id"] == newRoom {
			roomName = r["name"].(string)
		}
	}
	if !strings.HasPrefix(roomName, "Fork: ") {
		t.Fatalf("nama ruang fork = %q", roomName)
	}
	// Ruang asal tetap utuh.
	f.api("POST", "/api/rooms/"+originalRoom+"/activate", object{}, 200)
	if got := len(messagesOf(f.state())); got < 2 {
		t.Fatalf("ruang asal harus tetap berisi pesan, dapat %d", got)
	}
}

func TestExportDiscussion(t *testing.T) {
	f := setup(t, nil)
	f.api("POST", "/api/message", object{"text": "@Plamo diskusi satu"}, 202)
	state := f.wait(func(s object) bool { return completedSeatReplies(s) >= 1 })
	owner := findOwnerMessage(state, "@Plamo diskusi satu")
	jobID := owner["jobId"].(string)
	f.api("POST", "/api/message", object{"text": "@Plamo diskusi dua"}, 202)
	f.wait(func(s object) bool { return completedSeatReplies(s) >= 2 })

	export := f.api("GET", "/api/export?format=json&message="+owner["id"].(string), nil, 200)
	exported, _ := export["messages"].([]any)
	if len(exported) != 2 {
		t.Fatalf("ekspor diskusi harus berisi 2 pesan, dapat %d", len(exported))
	}
	for _, item := range exported {
		m, _ := item.(map[string]any)
		if m["jobId"] != jobID {
			t.Fatalf("pesan ekspor harus satu job: %v", m)
		}
	}
	full := f.api("GET", "/api/export?format=json", nil, 200)
	if len(full["messages"].([]any)) <= len(exported) {
		t.Fatal("ekspor penuh harus lebih banyak dari ekspor diskusi")
	}
	f.api("GET", "/api/export?format=json&message=00000000-0000-4000-8000-000000000000", nil, 400)
}

func TestReactionStats(t *testing.T) {
	f := setup(t, nil)
	f.api("POST", "/api/message", object{"text": "@Plamo beri reaksi"}, 202)
	state := f.wait(func(s object) bool { return completedSeatReplies(s) >= 1 })
	var seatMsg map[string]any
	for _, m := range messagesOf(state) {
		if m["authorType"] == "seat" {
			seatMsg = m
		}
	}
	id := seatMsg["id"].(string)
	f.api("POST", "/api/message/"+id+"/react", object{"emoji": "👍"}, 200)
	f.api("POST", "/api/message/"+id+"/react", object{"emoji": "🙏"}, 200)
	stats := f.api("GET", "/api/stats", nil, 200)
	reactions, _ := stats["reactions"].(map[string]any)
	total, _ := reactions["total"].(map[string]any)
	if total["👍"] != float64(1) || total["🙏"] != float64(1) {
		t.Fatalf("total reaksi salah: %v", total)
	}
	bySeat, _ := reactions["bySeat"].(map[string]any)
	seatReactions, _ := bySeat["M01"].(map[string]any)
	if seatReactions["👍"] != float64(1) {
		t.Fatalf("reaksi per kursi salah: %v", bySeat)
	}
	// Toggle off.
	f.api("POST", "/api/message/"+id+"/react", object{"emoji": "👍"}, 200)
	stats = f.api("GET", "/api/stats", nil, 200)
	reactions, _ = stats["reactions"].(map[string]any)
	total, _ = reactions["total"].(map[string]any)
	if _, ok := total["👍"]; ok {
		t.Fatalf("reaksi 👍 harus terhapus setelah toggle: %v", total)
	}
}

type captureGateway struct {
	mu     sync.Mutex
	bodies []string
}

func (c *captureGateway) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" && r.URL.Path == "/v1/chat/completions" {
			raw, _ := io.ReadAll(r.Body)
			c.mu.Lock()
			c.bodies = append(c.bodies, string(raw))
			c.mu.Unlock()
			io.WriteString(w, `{"choices":[{"message":{"content":"balasan tangkapan"}}]}`)
			return
		}
		http.NotFound(w, r)
	}
}

func (c *captureGateway) all() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return strings.Join(c.bodies, "\n---\n")
}

func TestChainReplies(t *testing.T) {
	capture := &captureGateway{}
	f := setup(t, capture.handler())
	f.api("POST", "/api/settings", object{"settings": object{"chainReplies": true}}, 200)
	f.api("POST", "/api/message", object{"text": "@Plamo @GLM diskusi berantai"}, 202)
	state := f.wait(func(s object) bool { return completedSeatReplies(s) >= 2 })

	var first, second map[string]any
	for _, m := range messagesOf(state) {
		if m["authorType"] != "seat" {
			continue
		}
		if first == nil {
			first = m
		} else {
			second = m
		}
	}
	if first == nil || second == nil {
		t.Fatalf("butuh 2 balasan kursi: %v", messagesOf(state))
	}
	if second["replyTo"] != first["id"] {
		t.Fatalf("replyTo balasan kedua harus menunjuk balasan pertama: %v vs %v", second["replyTo"], first["id"])
	}
	if !strings.Contains(capture.all(), "tanggapi secara eksplisit") {
		t.Fatal("instruksi berantai tidak masuk ke prompt gateway")
	}
}

func TestDebate(t *testing.T) {
	capture := &captureGateway{}
	f := setup(t, capture.handler())
	f.api("POST", "/api/settings", object{"settings": object{"integratorSeat": "M04"}}, 200)

	f.api("POST", "/api/debate", object{"seatA": "M01", "seatB": "M01", "topic": "Topik"}, 400)
	f.api("POST", "/api/debate", object{"seatA": "MUSE", "seatB": "M02", "topic": "Topik"}, 400)
	f.api("POST", "/api/debate", object{"seatA": "M01", "seatB": "M02", "topic": "  "}, 400)

	out := f.api("POST", "/api/debate", object{"seatA": "M01", "seatB": "M02", "topic": "Kopi lebih baik dari teh"}, 202)
	if out["jobId"] == "" {
		t.Fatalf("job debat kosong: %v", out)
	}
	state := f.wait(func(s object) bool { return completedSeatReplies(s) >= 5 })

	owner := findOwnerMessage(state, "Debat: Kopi lebih baik dari teh (@Plamo vs @GLM)")
	if owner == nil {
		t.Fatalf("pesan pemilik debat tidak ditemukan: %v", messagesOf(state))
	}
	wantLabels := []string{"Debat — Pro", "Debat — Kontra", "Debat — Sanggahan", "Debat — Sanggahan", "Debat — Kesimpulan"}
	wantSeats := []string{"M01", "M02", "M01", "M02", "M04"}
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
		if m["label"] != wantLabels[i] || m["seat"] != wantSeats[i] {
			t.Fatalf("langkah %d = %v/%v, ingin %s/%s", i, m["label"], m["seat"], wantLabels[i], wantSeats[i])
		}
	}
	all := capture.all()
	if !strings.Contains(all, "pihak PRO") || !strings.Contains(all, "pihak KONTRA") {
		t.Fatal("instruksi pro/kontra tidak masuk prompt")
	}
}
