package musehood

// Tes berat konkurensi & batas (27 Nov 2026): 100 pesan bersamaan, operasi
// campuran bersamaan, 200 ruang, 5000 pesan, 50 dokumen, rantai 6 langkah,
// A/B dua sisi. Semua helper HTTP di sini aman dipakai dari goroutine
// (tidak memakai f.api yang memanggil t.Fatal).

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

func heavyDo(client *http.Client, base, method, path string, body []byte, contentType string) (int, []byte, error) {
	var rdr io.Reader
	if body != nil {
		rdr = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, base+path, rdr)
	if err != nil {
		return 0, nil, err
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	res, err := client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(res.Body)
	return res.StatusCode, raw, err
}

func heavyJSON(client *http.Client, base, method, path string, payload any) (int, object, error) {
	var body []byte
	if payload != nil {
		body, _ = json.Marshal(payload)
	}
	st, raw, err := heavyDo(client, base, method, path, body, "application/json")
	if err != nil {
		return st, nil, err
	}
	var out object
	_ = json.Unmarshal(raw, &out)
	return st, out, nil
}

func TestHeavyConcurrentMessages(t *testing.T) {
	f := setup(t, nil)
	client := &http.Client{Timeout: 30 * time.Second}
	const n = 100
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			st, _, err := heavyJSON(client, f.server.URL, "POST", "/api/message",
				object{"text": fmt.Sprintf("@Plamo pesan konkuren %d", i)})
			if err != nil {
				errs <- err
				return
			}
			if st != 202 {
				errs <- fmt.Errorf("pesan %d: status %d", i, st)
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	// Semua job selesai: 100 pesan pemilik + 100 balasan, tanpa duplikat.
	s := f.wait(func(s object) bool {
		owner, reply := 0, 0
		for _, m := range messages(s) {
			switch m["authorType"] {
			case "owner":
				owner++
			case "seat":
				if m["status"] == "completed" {
					reply++
				}
			}
		}
		return owner == n && reply == n
	})
	ids := map[string]bool{}
	texts := map[string]int{}
	for _, m := range messages(s) {
		id, _ := m["id"].(string)
		if ids[id] {
			t.Fatalf("ID pesan duplikat: %s", id)
		}
		ids[id] = true
		if m["authorType"] == "owner" {
			texts[m["text"].(string)]++
		}
	}
	for i := 0; i < n; i++ {
		key := fmt.Sprintf("@Plamo pesan konkuren %d", i)
		if texts[key] != 1 {
			t.Fatalf("pesan %q muncul %d kali", key, texts[key])
		}
	}
}

func TestHeavyConcurrentMixedOps(t *testing.T) {
	f := setup(t, nil)
	client := &http.Client{Timeout: 30 * time.Second}
	const workers = 50
	var wg sync.WaitGroup
	errs := make(chan error, workers*4)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			// Baca state.
			st, _, err := heavyJSON(client, f.server.URL, "GET", "/api/state", nil)
			if err != nil || st != 200 {
				errs <- fmt.Errorf("state %d: %d %v", i, st, err)
				return
			}
			// Buat ruang.
			st, room, err := heavyJSON(client, f.server.URL, "POST", "/api/rooms",
				object{"name": fmt.Sprintf("Ruang Campuran %d", i)})
			if err != nil || (st != 200 && st != 201) {
				errs <- fmt.Errorf("rooms %d: %d %v", i, st, err)
				return
			}
			_ = room
			// Patch pengaturan (nested).
			st, _, err = heavyJSON(client, f.server.URL, "POST", "/api/settings",
				object{"settings": object{"density": "compact"}})
			if err != nil || st != 200 {
				errs <- fmt.Errorf("settings %d: %d %v", i, st, err)
				return
			}
			// Cari.
			st, _, err = heavyJSON(client, f.server.URL, "GET", "/api/search?q=halo", nil)
			if err != nil || st != 200 {
				errs <- fmt.Errorf("search %d: %d %v", i, st, err)
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	s := f.state()
	rooms := asObjects(s["rooms"])
	if len(rooms) != workers+1 {
		t.Fatalf("jumlah ruang %d, mau %d", len(rooms), workers+1)
	}
}

func TestHeavyRoomAndMessageLimits(t *testing.T) {
	f := setup(t, nil)
	// 200 ruang lewat API.
	for i := 0; i < 199; i++ {
		f.api("POST", "/api/rooms", object{"name": fmt.Sprintf("Ruang Batas %d", i)}, 201)
	}
	s := f.state()
	if got := len(asObjects(s["rooms"])); got != 200 {
		t.Fatalf("ruang %d, mau 200", got)
	}

	// 5000 pesan di ruang utama: sisipkan langsung ke data (jalur API akan
	// membuat 5000 job; yang diuji di sini adalah serialisasi state).
	f.app.mu.Lock()
	roomID := f.app.data.activeRoom().ID
	for i := 0; i < 5000; i++ {
		f.app.data.Messages = append(f.app.data.Messages, &message{
			ID: fmt.Sprintf("heavy-%05d", i), CreatedAt: now(), AuthorType: "owner",
			Text:   fmt.Sprintf("Pesan batas nomor %d dengan isi yang cukup panjang untuk realistis.", i),
			Status: "completed", RoomID: roomID, BranchID: "main",
		})
	}
	if err := f.app.persistLocked(f.app.data); err != nil {
		f.app.mu.Unlock()
		t.Fatal(err)
	}
	f.app.mu.Unlock()

	start := time.Now()
	s = f.state()
	stateDur := time.Since(start)
	t.Logf("/api/state dengan 5000 pesan + 200 ruang: %v", stateDur)
	if got := len(messages(s)); got < 5000 {
		t.Fatalf("pesan di state %d, mau minimal 5000", got)
	}
	heavy := 0
	for _, m := range messages(s) {
		if strings.HasPrefix(m["id"].(string), "heavy-") {
			heavy++
		}
	}
	if heavy != 5000 {
		t.Fatalf("pesan heavy %d, mau 5000", heavy)
	}
	if stateDur > 10*time.Second {
		t.Fatalf("state terlalu lambat: %v", stateDur)
	}

	start = time.Now()
	out := f.api("GET", "/api/search?q=nomor+4999", nil, 200)
	searchDur := time.Since(start)
	t.Logf("/api/search pada 5000 pesan: %v", searchDur)
	if hits := asObjects(out["results"]); len(hits) != 1 {
		t.Fatalf("hasil cari %d, mau 1", len(hits))
	}
}

func TestHeavyDocumentsCap(t *testing.T) {
	f := setup(t, nil)
	upload := func(roomID, name, content string, want int) object {
		var buf bytes.Buffer
		mw := multipart.NewWriter(&buf)
		part, err := mw.CreateFormFile("file", name)
		if err != nil {
			t.Fatal(err)
		}
		io.WriteString(part, content)
		mw.Close()
		st, raw, err := heavyDo(http.DefaultClient, f.server.URL, "POST",
			"/api/rooms/"+roomID+"/documents", buf.Bytes(), mw.FormDataContentType())
		if err != nil {
			t.Fatal(err)
		}
		if st != want {
			t.Fatalf("upload %s: mau %d dapat %d %s", name, want, st, raw)
		}
		var out object
		_ = json.Unmarshal(raw, &out)
		return out
	}

	rooms := []string{f.state()["activeRoomId"].(string)}
	for i := 0; i < 4; i++ {
		out := f.api("POST", "/api/rooms", object{"name": fmt.Sprintf("Ruang Dok %d", i)}, 201)
		rooms = append(rooms, out["activeRoomId"].(string))
	}
	total := 0
	for _, roomID := range rooms {
		for i := 0; i < 10; i++ {
			upload(roomID, fmt.Sprintf("dok-%d.txt", i),
				fmt.Sprintf("Isi dokumen nomor %d tentang topik uji berat.", i), 201)
			total++
		}
		// Dokumen ke-11 di ruang yang sama ditolak (batas 10/ruang).
		upload(roomID, "dok-lebih.txt", "kelebihan", 400)
	}
	if total != 50 {
		t.Fatalf("total dokumen %d, mau 50", total)
	}
	for _, roomID := range rooms {
		out := f.api("GET", "/api/rooms/"+roomID+"/documents", nil, 200)
		if got := len(asObjects(out["documents"])); got != 10 {
			t.Fatalf("dokumen ruang %s: %d, mau 10", roomID, got)
		}
	}
}

func TestHeavyChainSixStepsAndAB(t *testing.T) {
	f := setup(t, nil)
	steps := make([]object, 6)
	for i := range steps {
		steps[i] = object{"seatCode": "M01", "instruction": fmt.Sprintf("Langkah %d: lanjutkan", i+1)}
	}
	out := f.api("POST", "/api/chains", object{"name": "Rantai Enam", "steps": steps}, 201)
	chainID := out["id"].(string)
	// 7 langkah ditolak.
	steps7 := append(append([]object{}, steps...), object{"seatCode": "M01", "instruction": "lebih"})
	f.api("POST", "/api/chains", object{"name": "Rantai Tujuh", "steps": steps7}, 400)

	f.api("POST", "/api/chains/"+chainID+"/run", object{"text": "awal rantai"}, 202)
	s := f.wait(func(s object) bool {
		done := 0
		for _, m := range messages(s) {
			if m["chainId"] == chainID && m["status"] == "completed" {
				done++
			}
		}
		return done == 6
	})
	var order []int
	for _, m := range messages(s) {
		if m["chainId"] == chainID {
			order = append(order, int(m["chainStep"].(float64)))
		}
	}
	if !sort.IsSorted(sort.Reverse(sort.IntSlice(order))) && len(order) == 6 {
		// Urutan tampil boleh menaik; pastikan semua langkah 1..6 ada.
		seen := map[int]bool{}
		for _, v := range order {
			seen[v] = true
		}
		for i := 1; i <= 6; i++ {
			if !seen[i] {
				t.Fatalf("langkah %d hilang, urutan %v", i, order)
			}
		}
	}

	// A/B dua sisi bersamaan dengan rantai selesai: keduanya completed.
	out = f.api("POST", "/api/abtest", object{"text": "pilih", "modelA": "plain", "modelB": "sse"}, 202)
	group := out["group"].(string)
	f.wait(func(s object) bool {
		done := 0
		for _, m := range messages(s) {
			if m["abGroup"] == group && m["status"] == "completed" {
				done++
			}
		}
		return done == 2
	})
}

func TestHeavyLongMessageAndUnicode(t *testing.T) {
	f := setup(t, nil)
	long := "@Plamo " + strings.Repeat("kata ", 3998) + "akhir" // total tepat 20.000 karakter
	if len([]rune(long)) > 20000 {
		long = string([]rune(long)[:20000])
	}
	f.api("POST", "/api/message", object{"text": long}, 202)
	f.finish(2)
	// Emoji, RTL, karakter kontrol yang aman.
	f.api("POST", "/api/message", object{"text": "@Plamo halo 🫠 مرحبا بالعالم \u200f campuran"}, 202)
	f.finish(4)
	// Tepat di atas batas 20.000 ditolak.
	f.api("POST", "/api/message", object{"text": strings.Repeat("a", 20001)}, 400)
}
