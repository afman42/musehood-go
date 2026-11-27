package musehood

// Tes fitur pendalaman grup C: edit+regenerasi (13), branching (14),
// mode paralel (15), tag pesan (17), jadwal cron (18).

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"
)

func TestEditRegenerates(t *testing.T) {
	f := setup(t, nil)
	f.api("POST", "/api/message", object{"text": "@M01 pertama"}, 202)
	msgs := f.finish(2)
	ownerID := idOf(msgs[0])
	oldReplyID := idOf(msgs[1])
	out := f.api("POST", "/api/message/"+ownerID+"/edit", object{"text": "@M01 kedua"}, 202)
	if out["jobId"] == nil {
		t.Fatal(out)
	}
	msgs = f.finish(2)
	if msgs[0]["text"] != "@M01 kedua" || msgs[0]["editedAt"] == nil {
		t.Fatal(msgs[0])
	}
	if idOf(msgs[1]) == oldReplyID {
		t.Fatal("balasan lama tidak diganti")
	}
	if msgs[1]["text"] != "bagian teks" {
		t.Fatal(msgs[1])
	}
}

func TestBranchingIsolated(t *testing.T) {
	f := setup(t, nil)
	f.api("POST", "/api/message", object{"text": "@M01 awal"}, 202)
	msgs := f.finish(2)
	ownerID := idOf(msgs[0])
	out := f.api("POST", "/api/message/"+ownerID+"/branch", object{}, 200)
	branchID, _ := out["branchId"].(string)
	if branchID == "" || branchID == "main" {
		t.Fatal(out)
	}
	// Branch berisi salinan sampai titik itu (pesan pemilik saja).
	s := f.state()
	if got := len(messages(s)); got != 1 {
		t.Fatal("branch harus berisi 1 pesan salinan:", got)
	}
	if idOf(messages(s)[0]) == ownerID {
		t.Fatal("salinan branch harus ber-ID baru")
	}
	// Kirim pesan di branch.
	f.api("POST", "/api/message", object{"text": "@M01 di branch"}, 202)
	msgs = f.finish(3)
	roomID := s["activeRoomId"].(string)
	// Kembali ke main: hanya 2 pesan asli.
	f.api("POST", "/api/rooms/"+roomID+"/branch", object{"branchId": "main"}, 200)
	s = f.state()
	if got := len(messages(s)); got != 2 {
		t.Fatal("main harus tetap 2 pesan:", got)
	}
	if messages(s)[0]["text"] != "@M01 awal" {
		t.Fatal(messages(s)[0])
	}
	// Beralih lagi ke branch: 3 pesan.
	f.api("POST", "/api/rooms/"+roomID+"/branch", object{"branchId": branchID}, 200)
	s = f.state()
	if got := len(messages(s)); got != 3 {
		t.Fatal("branch harus 3 pesan:", got)
	}
	// Branch tidak dikenal ditolak.
	f.api("POST", "/api/rooms/"+roomID+"/branch", object{"branchId": "tidak-ada"}, 404)
	// roomViews memuat daftar branch.
	rooms := asObjects(s["rooms"])
	branches := asObjects(rooms[0]["branches"])
	if len(branches) != 2 || rooms[0]["activeBranch"] != branchID {
		t.Fatal(rooms[0])
	}
}

func TestParallelModeConcurrent(t *testing.T) {
	slowGateway := func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" {
			io.WriteString(w, `{"data":[{"id":"m"}]}`)
			return
		}
		var body struct{ Model string }
		json.NewDecoder(r.Body).Decode(&body)
		time.Sleep(300 * time.Millisecond)
		fmt.Fprintf(w, `{"choices":[{"message":{"content":"balasan %s"}}]}`, body.Model)
	}
	f := setup(t, slowGateway)
	f.api("POST", "/api/settings", object{"settings": object{"parallelMode": true, "nativeToolsEnabled": false}}, 200)
	start := time.Now()
	f.api("POST", "/api/message", object{"text": "@M01 @GLM @Fable halo"}, 202)
	msgs := f.finish(4)
	elapsed := time.Since(start)
	if elapsed >= 800*time.Millisecond {
		t.Fatal("mode paralel tidak konkuren:", elapsed)
	}
	// Urutan tampil mengikuti urutan kursi pada langkah.
	if msgs[1]["seat"] != "M01" || msgs[2]["seat"] != "M02" || msgs[3]["seat"] != "M05" {
		t.Fatal(msgs)
	}
	for _, m := range msgs[1:] {
		if m["status"] != "completed" || m["text"] == "" {
			t.Fatal(m)
		}
	}
}

func TestMessageTags(t *testing.T) {
	f := setup(t, nil)
	f.api("POST", "/api/message", object{"text": "@M01 tandai"}, 202)
	msgs := f.finish(2)
	replyID := idOf(msgs[1])
	out := f.api("POST", "/api/message/"+replyID+"/tags", object{"tags": []string{"penting", "riset"}}, 200)
	tags, _ := out["tags"].([]any)
	if len(tags) != 2 || tags[0] != "penting" {
		t.Fatal(out)
	}
	// Pencarian berfilter tag menemukan; tag lain tidak.
	found := f.api("GET", "/api/search?q=&tag=penting", nil, 200)
	if len(asObjects(found["results"])) != 1 {
		t.Fatal(found)
	}
	found = f.api("GET", "/api/search?q=&tag=lain", nil, 200)
	if len(asObjects(found["results"])) != 0 {
		t.Fatal(found)
	}
	// Lebih dari 5 tag ditolak; tag terlalu panjang ditolak.
	f.api("POST", "/api/message/"+replyID+"/tags", object{"tags": []string{"a", "b", "c", "d", "e", "f"}}, 400)
	f.api("POST", "/api/message/"+replyID+"/tags", object{"tags": []string{"1234567890123456789012345678901"}}, 400)
}

func TestCronRepeat(t *testing.T) {
	zone := time.FixedZone("", 7*3600) // WIB
	from := time.Date(2026, 11, 27, 10, 30, 0, 0, zone) // Jumat
	cases := []struct {
		repeat string
		want   time.Time
	}{
		{"0 7 * * *", time.Date(2026, 11, 28, 7, 0, 0, 0, zone)},
		{"30 8 * * 1", time.Date(2026, 11, 30, 8, 30, 0, 0, zone)}, // Senin berikutnya
		{"*/15 * * * *", time.Date(2026, 11, 27, 10, 45, 0, 0, zone)},
		{"0 0 1 * *", time.Date(2026, 12, 1, 0, 0, 0, 0, zone)},
		{"15,45 9-17 * * *", time.Time{}}, // rentang tidak didukung: harus error parse
	}
	for _, c := range cases {
		sched, err := parseCron(c.repeat)
		if c.want.IsZero() {
			if err == nil {
				t.Fatalf("%q harus gagal di-parse", c.repeat)
			}
			continue
		}
		if err != nil {
			t.Fatalf("%q: %v", c.repeat, err)
		}
		next, err := nextCronOccurrence(sched, from, zone)
		if err != nil || !next.Equal(c.want) {
			t.Fatalf("%q: dapat %v (%v), mau %v", c.repeat, next, err, c.want)
		}
	}
	// computeNextAt memakai jalur cron bila Repeat terisi.
	next, err := computeNextAt(scheduledMessage{Repeat: "0 7 * * *", TzOffsetMinutes: 420}, from)
	if err != nil || !next.Equal(time.Date(2026, 11, 28, 7, 0, 0, 0, zone)) {
		t.Fatal(next, err)
	}
	// API: jadwal cron tersimpan dengan nextAt; pola buruk ditolak.
	f := setup(t, nil)
	out := f.api("POST", "/api/scheduled", object{"text": "pengingat cron", "repeat": "*/30 * * * *", "tzOffsetMinutes": 420}, 201)
	if out["repeat"] != "*/30 * * * *" || out["nextAt"] == nil || out["nextAt"] == "" {
		t.Fatal(out)
	}
	f.api("POST", "/api/scheduled", object{"text": "buruk", "repeat": "bukan cron"}, 400)
	f.api("POST", "/api/scheduled", object{"text": "buruk", "repeat": "99 * * * *"}, 400)
}
