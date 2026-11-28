package musehood

// Tes fitur batch 3 (21–34): peserta ruang, preset peran, uji A/B,
// rantai prompt, koreksi otomatis, dokumen RAG, ringkasan harian,
// ekspor Markdown, rating, dan deteksi Ollama.

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func roomByID(s object, id string) object {
	for _, r := range asObjects(s["rooms"]) {
		if r["id"] == id {
			return r
		}
	}
	return nil
}

func (f fixture) rawRequest(method, path string, body io.Reader, contentType string) (int, http.Header, []byte) {
	f.t.Helper()
	req, err := http.NewRequest(method, f.server.URL+path, body)
	if err != nil {
		f.t.Fatal(err)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		f.t.Fatal(err)
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		f.t.Fatal(err)
	}
	return res.StatusCode, res.Header, raw
}

func TestRoomParticipants(t *testing.T) {
	f := setup(t, nil)
	roomID := f.state()["activeRoomId"].(string)
	f.api("POST", "/api/rooms/"+roomID+"/participants", object{"name": "Budi"}, 200)
	f.api("POST", "/api/rooms/"+roomID+"/participants", object{"name": "budi"}, 400)  // duplikat case-insensitive
	f.api("POST", "/api/rooms/"+roomID+"/participants", object{"name": "Arman"}, 400) // nama pemilik
	f.api("POST", "/api/rooms/"+roomID+"/participants", object{"name": "  "}, 400)    // kosong
	f.api("POST", "/api/rooms/tidak-ada/participants", object{"name": "Sari"}, 404)
	room := roomByID(f.state(), roomID)
	participants, _ := room["participants"].([]any)
	if len(participants) != 1 || participants[0] != "Budi" {
		t.Fatal(room)
	}
	// Pesan atas nama peserta tersimpan dengan Author.
	f.api("POST", "/api/message", object{"text": "@M01 halo dari Budi", "author": "budi"}, 202)
	msgs := f.finish(2)
	if msgs[0]["author"] != "Budi" {
		t.Fatal(msgs[0])
	}
	// Pengirim bukan peserta ditolak.
	f.api("POST", "/api/message", object{"text": "@M01 halo", "author": "Orang Asing"}, 400)
	// Hapus peserta.
	f.api("POST", "/api/rooms/"+roomID+"/participants/remove", object{"name": "Budi"}, 200)
	f.api("POST", "/api/rooms/"+roomID+"/participants/remove", object{"name": "Budi"}, 404)
	room = roomByID(f.state(), roomID)
	if participants, _ := room["participants"].([]any); len(participants) != 0 {
		t.Fatal(room)
	}
	// Batas 10 peserta.
	for i := 0; i < 10; i++ {
		f.api("POST", "/api/rooms/"+roomID+"/participants", object{"name": "Tamu" + string(rune('A'+i))}, 200)
	}
	f.api("POST", "/api/rooms/"+roomID+"/participants", object{"name": "TamuK"}, 400)
}

func TestRolePresets(t *testing.T) {
	f := setup(t, nil)
	out := f.api("GET", "/api/roles", nil, 200)
	roles := asObjects(out["roles"])
	if len(roles) != 6 {
		t.Fatal(out)
	}
	ids := map[string]bool{}
	for _, r := range roles {
		ids[r["id"].(string)] = true
		if r["prompt"] == "" || r["name"] == "" {
			t.Fatal(r)
		}
	}
	for _, id := range []string{"reviewer", "coder", "peneliti", "editor", "pengajar", "analis"} {
		if !ids[id] {
			t.Fatal("preset hilang: " + id)
		}
	}
	f.api("POST", "/api/settings", object{"seats": []object{{"code": "M01", "rolePreset": "coder"}}}, 200)
	if seatByCode(f.state(), "M01")["rolePreset"] != "coder" {
		t.Fatal(f.state())
	}
	f.api("POST", "/api/settings", object{"seats": []object{{"code": "M01", "rolePreset": "tidak-ada"}}}, 400)
	// Prompt preset tergabung ke system prompt setelah Memory/Skills.
	f.app.mu.Lock()
	seat := *f.app.data.findSeat("M01")
	prompt := f.app.systemPrompt(seat, f.app.data)
	f.app.mu.Unlock()
	if !strings.Contains(prompt, "Preset peran:") || !strings.Contains(prompt, "programmer (coder)") {
		t.Fatal(prompt)
	}
	// Kosongkan kembali.
	f.api("POST", "/api/settings", object{"seats": []object{{"code": "M01", "rolePreset": ""}}}, 200)
	if _, ok := seatByCode(f.state(), "M01")["rolePreset"]; ok {
		t.Fatal(f.state())
	}
}

func TestABTest(t *testing.T) {
	f := setup(t, nil)
	out := f.api("POST", "/api/abtest", object{"text": "Apa ibu kota Indonesia?", "modelA": "alpha", "modelB": "beta"}, 202)
	group := out["group"].(string)
	ids := out["messages"].([]any)
	if len(ids) != 2 {
		t.Fatal(out)
	}
	s := f.wait(func(s object) bool {
		done := 0
		for _, m := range messages(s) {
			if m["abGroup"] == group && m["status"] == "completed" {
				done++
			}
		}
		return done == 2
	})
	var aMsg, bMsg object
	for _, m := range messages(s) {
		if m["abGroup"] != group {
			continue
		}
		switch m["abLabel"] {
		case "A":
			aMsg = m
		case "B":
			bMsg = m
		}
	}
	if aMsg == nil || bMsg == nil || aMsg["model"] != "alpha" || bMsg["model"] != "beta" {
		t.Fatal(messages(s))
	}
	if aMsg["text"] != "balasan alpha" || bMsg["text"] != "balasan beta" {
		t.Fatal(aMsg, bMsg)
	}
	// Pilih pemenang B.
	f.api("POST", "/api/abtest/"+group+"/pick", object{"messageId": bMsg["id"]}, 200)
	s = f.state()
	for _, m := range messages(s) {
		if m["abGroup"] != group {
			continue
		}
		if m["id"] == bMsg["id"] && m["abWinner"] != true {
			t.Fatal(m)
		}
		if m["id"] == aMsg["id"] && m["abWinner"] == true {
			t.Fatal(m)
		}
	}
	f.api("POST", "/api/abtest/"+group+"/pick", object{"messageId": "tidak-ada"}, 404)
	// Model kosong ditolak.
	f.api("POST", "/api/abtest", object{"text": "halo", "modelA": "alpha", "modelB": ""}, 400)
	// Satu sisi gagal tidak menggagalkan sisi lain.
	out = f.api("POST", "/api/abtest", object{"text": "halo lagi", "modelA": "denied", "modelB": "beta"}, 202)
	group2 := out["group"].(string)
	s = f.wait(func(s object) bool {
		var aDone, bDone bool
		for _, m := range messages(s) {
			if m["abGroup"] != group2 {
				continue
			}
			if m["abLabel"] == "A" && m["status"] == "error" {
				aDone = true
			}
			if m["abLabel"] == "B" && m["status"] == "completed" {
				bDone = true
			}
		}
		return aDone && bDone
	})
	_ = s
}

func TestPromptChains(t *testing.T) {
	var mu sync.Mutex
	var seen []string
	f := setup(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method+" "+r.URL.Path != "POST /v1/chat/completions" {
			http.NotFound(w, r)
			return
		}
		var body struct {
			Messages []struct {
				Role    string
				Content string
			}
		}
		json.NewDecoder(r.Body).Decode(&body)
		last := ""
		for _, m := range body.Messages {
			if m.Role == "user" {
				last = m.Content
			}
		}
		mu.Lock()
		seen = append(seen, last)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(object{"choices": []object{{"message": object{"content": "HASIL[" + last + "]"}}}})
	})
	out := f.api("POST", "/api/chains", object{
		"name": "Riset lalu ringkas",
		"steps": []object{
			{"seatCode": "M10", "instruction": "Teliti topik berikut"},
			{"seatCode": "M01", "instruction": "Ringkas hasil berikut"},
		},
	}, 201)
	chainID := out["id"].(string)
	list := f.api("GET", "/api/chains", nil, 200)
	if len(asObjects(list["chains"])) != 1 {
		t.Fatal(list)
	}
	// Validasi: kursi tidak dikenal, langkah kosong, terlalu banyak langkah.
	f.api("POST", "/api/chains", object{"name": "X", "steps": []object{{"seatCode": "ZZZ", "instruction": "a"}}}, 400)
	f.api("POST", "/api/chains", object{"name": "X", "steps": []object{}}, 400)
	// Jalankan rantai.
	run := f.api("POST", "/api/chains/"+chainID+"/run", object{"text": "kucing oren"}, 202)
	ids := run["messages"].([]any)
	if len(ids) != 2 {
		t.Fatal(run)
	}
	s := f.wait(func(s object) bool {
		done := 0
		for _, m := range messages(s) {
			if m["chainId"] == chainID && m["status"] == "completed" {
				done++
			}
		}
		return done == 2
	})
	var step1, step2 object
	for _, m := range messages(s) {
		if m["chainId"] != chainID {
			continue
		}
		if m["chainStep"] == float64(1) {
			step1 = m
		}
		if m["chainStep"] == float64(2) {
			step2 = m
		}
	}
	if step1 == nil || step2 == nil {
		t.Fatal(messages(s))
	}
	// Output langkah 1 menjadi input langkah 2.
	if !strings.Contains(step2["text"].(string), "HASIL[Teliti topik berikut") || !strings.Contains(step2["text"].(string), "kucing oren") {
		t.Fatal(step2)
	}
	mu.Lock()
	if len(seen) != 2 || !strings.HasPrefix(seen[1], "Ringkas hasil berikut\n\nHASIL[") {
		mu.Unlock()
		t.Fatal(seen)
	}
	mu.Unlock()
	// Update + hapus.
	f.api("POST", "/api/chains/"+chainID, object{"name": "Rantai baru"}, 200)
	f.api("POST", "/api/chains/"+chainID+"/delete", object{}, 200)
	f.api("POST", "/api/chains/"+chainID+"/run", object{"text": "x"}, 404)
}

func TestAutoReview(t *testing.T) {
	f := setup(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method+" "+r.URL.Path != "POST /v1/chat/completions" {
			http.NotFound(w, r)
			return
		}
		var body struct {
			Messages []struct {
				Role    string
				Content string
			}
		}
		json.NewDecoder(r.Body).Decode(&body)
		joined := ""
		for _, m := range body.Messages {
			joined += m.Content + "\n"
		}
		reply := "balasan asli"
		if strings.Contains(joined, "Periksa balasan berikut") {
			reply = "TEKS TERKOREKSI"
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(object{"choices": []object{{"message": object{"content": reply}}}})
	})
	f.api("POST", "/api/settings", object{"settings": object{"autoReview": true, "reviewerSeat": "M02"}}, 200)
	// Balasan M01 dikoreksi oleh peninjau M02.
	f.api("POST", "/api/message", object{"text": "@M01 halo"}, 202)
	msgs := f.finish(2)
	if msgs[1]["text"] != "TEKS TERKOREKSI" || msgs[1]["originalText"] != "balasan asli" {
		t.Fatal(msgs[1])
	}
	// Balasan kursi peninjau sendiri tidak ditinjau.
	f.api("POST", "/api/message", object{"text": "@M02 halo"}, 202)
	msgs = f.finish(4)
	if msgs[3]["text"] != "balasan asli" {
		t.Fatal(msgs[3])
	}
	if _, ok := msgs[3]["originalText"]; ok {
		t.Fatal(msgs[3])
	}
	// Matikan: balasan apa adanya.
	f.api("POST", "/api/settings", object{"settings": object{"autoReview": false}}, 200)
	f.api("POST", "/api/message", object{"text": "@M01 halo lagi"}, 202)
	msgs = f.finish(6)
	if msgs[5]["text"] != "balasan asli" {
		t.Fatal(msgs[5])
	}
	// Kursi peninjau tidak dikenal ditolak.
	f.api("POST", "/api/settings", object{"settings": object{"reviewerSeat": "ZZZ"}}, 400)
}

func TestDocumentsRAG(t *testing.T) {
	// Unit: chunking + penilaian relevansi.
	long := strings.Repeat("kalimat panjang tentang kucing oren yang lucu. ", 100)
	chunks := chunkDocumentText(long)
	if len(chunks) < 2 || len(chunks) > maxChunksPerDocument {
		t.Fatal(len(chunks))
	}
	if len([]rune(chunks[0])) > documentChunkRunes {
		t.Fatal(len([]rune(chunks[0])))
	}
	d := data{Documents: []roomDocument{
		{ID: "d1", RoomID: "r1", Chunks: []string{"kucing oren tidur", "anjing cokelat berlari", "kucing oren makan kucing"}},
		{ID: "d2", RoomID: "r2", Chunks: []string{"kucing oren kucing oren kucing"}},
	}}
	got := relevantDocumentChunks(d, "r1", "ceritakan kucing oren", 2)
	if len(got) != 2 || got[0] != "kucing oren makan kucing" {
		t.Fatal(got)
	}

	var mu sync.Mutex
	var systems []string
	f := setup(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method+" "+r.URL.Path != "POST /v1/chat/completions" {
			http.NotFound(w, r)
			return
		}
		var body struct {
			Messages []struct {
				Role    string
				Content string
			}
		}
		json.NewDecoder(r.Body).Decode(&body)
		for _, m := range body.Messages {
			if m.Role == "system" {
				mu.Lock()
				systems = append(systems, m.Content)
				mu.Unlock()
			}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(object{"choices": []object{{"message": object{"content": "siap"}}}})
	})
	roomID := f.state()["activeRoomId"].(string)
	// Unggah dokumen teks.
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	part, _ := mw.CreateFormFile("file", "catatan.txt")
	part.Write([]byte("Kucing oren bernama Momo suka tidur di atas keyboard. Momo adalah kucing oren yang sangat manja."))
	mw.Close()
	status, _, raw := f.rawRequest("POST", "/api/rooms/"+roomID+"/documents", &buf, mw.FormDataContentType())
	if status != 201 {
		t.Fatalf("unggah dokumen: %d %s", status, raw)
	}
	var doc object
	if json.Unmarshal(raw, &doc) != nil || doc["name"] != "catatan.txt" {
		t.Fatal(string(raw))
	}
	list := f.api("GET", "/api/rooms/"+roomID+"/documents", nil, 200)
	docs := asObjects(list["documents"])
	if len(docs) != 1 || docs[0]["chunks"] != float64(1) {
		t.Fatal(list)
	}
	// Ekstensi tidak didukung ditolak.
	buf.Reset()
	mw = multipart.NewWriter(&buf)
	part, _ = mw.CreateFormFile("file", "jahat.exe")
	part.Write([]byte("MZ"))
	mw.Close()
	status, _, _ = f.rawRequest("POST", "/api/rooms/"+roomID+"/documents", &buf, mw.FormDataContentType())
	if status != 400 {
		t.Fatalf("ekstensi exe: %d", status)
	}
	// Pesan dengan kata kunci dokumen menyisipkan konteks ke system prompt.
	f.api("POST", "/api/message", object{"text": "@M01 ceritakan kucing oren Momo"}, 202)
	f.finish(2)
	mu.Lock()
	found := false
	for _, sys := range systems {
		if strings.Contains(sys, "Dokumen terlampir") && strings.Contains(sys, "Momo") {
			found = true
		}
	}
	mu.Unlock()
	if !found {
		t.Fatal(systems)
	}
	// State tidak memuat chunk dokumen.
	if _, ok := f.state()["documents"]; ok {
		t.Fatal("documents bocor ke state")
	}
	// Hapus dokumen.
	f.api("POST", "/api/rooms/"+roomID+"/documents/"+doc["id"].(string)+"/delete", object{}, 200)
	list = f.api("GET", "/api/rooms/"+roomID+"/documents", nil, 200)
	if len(asObjects(list["documents"])) != 0 {
		t.Fatal(list)
	}
}

func TestDailySummary(t *testing.T) {
	f := setup(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method+" "+r.URL.Path != "POST /v1/chat/completions" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(object{"choices": []object{{"message": object{"content": "RINGKASAN HARIAN UJI"}}}})
	})
	// Validasi pengaturan.
	f.api("POST", "/api/settings", object{"settings": object{"dailySummaryTime": "25:99"}}, 400)
	f.api("POST", "/api/settings", object{"settings": object{"dailySummaryChannel": "pos"}}, 400)
	f.api("POST", "/api/settings", object{"settings": object{"integratorSeat": "M10", "dailySummaryEnabled": true, "dailySummaryTime": "21:00", "dailySummaryChannel": "inbox"}}, 200)
	// Jatuh tempo: jam cocok + belum dibuat hari ini.
	nowJakarta := time.Now().In(jakartaLocation())
	at := time.Date(nowJakarta.Year(), nowJakarta.Month(), nowJakarta.Day(), 21, 0, 0, 0, jakartaLocation())
	f.app.mu.Lock()
	due := f.app.dailySummaryDueLocked(at)
	f.app.data.LastDailySummary = at.Format("2006-01-02")
	notDue := f.app.dailySummaryDueLocked(at)
	f.app.data.LastDailySummary = ""
	wrongHour := f.app.dailySummaryDueLocked(at.Add(time.Hour))
	f.app.mu.Unlock()
	if !due || notDue || wrongHour {
		t.Fatalf("due=%v notDue=%v wrongHour=%v", due, notDue, wrongHour)
	}
	// Jalankan langsung: pesan ringkasan muncul di ruang utama.
	date := nowJakarta.Format("2006-01-02")
	f.api("POST", "/api/message", object{"text": "@M01 halo"}, 202)
	f.finish(2)
	f.app.runDailySummary(date)
	s := f.state()
	found := false
	for _, m := range messages(s) {
		if m["label"] == dailySummaryLabel && m["text"] == "RINGKASAN HARIAN UJI" {
			found = true
		}
	}
	if !found {
		t.Fatal(messages(s))
	}
	// Bahan fallback memuat daftar ruang + jumlah pesan.
	f.app.mu.Lock()
	_, fallback := f.app.dailySummaryMaterialLocked(date)
	f.app.mu.Unlock()
	if !strings.Contains(fallback, "Ruang Utama") {
		t.Fatal(fallback)
	}
}

func TestMessageRating(t *testing.T) {
	f := setup(t, nil)
	f.api("POST", "/api/message", object{"text": "@M01 halo"}, 202)
	msgs := f.finish(2)
	seatMsg := msgs[1]
	ownerMsg := msgs[0]
	out := f.api("POST", "/api/message/"+seatMsg["id"].(string)+"/rate", object{"rating": 1}, 200)
	if out["rating"] != float64(1) {
		t.Fatal(out)
	}
	// Model tercatat pada balasan gateway.
	if seatMsg["model"] != "array" {
		t.Fatal(seatMsg)
	}
	stats := f.api("GET", "/api/stats", nil, 200)
	ratings := stats["ratings"].(object)
	bySeat := ratings["bySeat"].(object)
	if bySeat["M01"].(object)["up"] != float64(1) {
		t.Fatal(ratings)
	}
	byModel := ratings["byModel"].(object)
	if byModel["array"].(object)["up"] != float64(1) {
		t.Fatal(ratings)
	}
	// Rating juga tampil di state.
	stateRatings := f.state()["ratings"].(object)
	if stateRatings["bySeat"].(object)["M01"].(object)["up"] != float64(1) {
		t.Fatal(stateRatings)
	}
	// Ganti ke -1 lalu netral.
	f.api("POST", "/api/message/"+seatMsg["id"].(string)+"/rate", object{"rating": -1}, 200)
	stats = f.api("GET", "/api/stats", nil, 200)
	if stats["ratings"].(object)["bySeat"].(object)["M01"].(object)["down"] != float64(1) {
		t.Fatal(stats)
	}
	f.api("POST", "/api/message/"+seatMsg["id"].(string)+"/rate", object{"rating": 0}, 200)
	// Pesan pemilik tidak bisa dinilai; rating di luar rentang ditolak.
	f.api("POST", "/api/message/"+ownerMsg["id"].(string)+"/rate", object{"rating": 1}, 400)
	f.api("POST", "/api/message/"+seatMsg["id"].(string)+"/rate", object{"rating": 2}, 400)
}

func TestExportMarkdown(t *testing.T) {
	f := setup(t, nil)
	f.api("POST", "/api/message", object{"text": "@M01 halo **dunia**"}, 202)
	f.finish(2)
	status, header, raw := f.rawRequest("GET", "/api/export?format=markdown", nil, "")
	if status != 200 {
		t.Fatalf("ekspor markdown: %d %s", status, raw)
	}
	body := string(raw)
	for _, want := range []string{"---\n", "title: \"Utama\"", "tags: [musehood]", "# Utama", "### Arman — ", "halo **dunia**"} {
		if !strings.Contains(body, want) {
			t.Fatalf("ekspor tidak memuat %q:\n%s", want, body)
		}
	}
	disposition := header.Get("Content-Disposition")
	if !strings.Contains(disposition, "utama-") || !strings.Contains(disposition, ".md") {
		t.Fatal(disposition)
	}
	// Format lama tetap jalan.
	status, _, _ = f.rawRequest("GET", "/api/export?format=md", nil, "")
	if status != 200 {
		t.Fatalf("ekspor md lama: %d", status)
	}
	status, _, _ = f.rawRequest("GET", "/api/export?format=xyz", nil, "")
	if status != 400 {
		t.Fatalf("format tidak dikenal: %d", status)
	}
}

func TestOllamaDetectAdd(t *testing.T) {
	// Validasi host.
	for raw, ok := range map[string]bool{
		"http://127.0.0.1:11434": true,
		"http://localhost:11434": true,
		"http://192.168.1.10":    true,
		"http://10.0.0.5:11434":  true,
		"http://172.20.0.9":      true,
		"http://172.32.0.9":      false,
		"http://8.8.8.8":         false,
		"https://example.com":    false,
		"ftp://127.0.0.1":        false,
	} {
		_, err := validateOllamaBaseURL(raw)
		if (err == nil) != ok {
			t.Fatalf("%s: err=%v ingin ok=%v", raw, err, ok)
		}
	}
	f := setup(t, nil)
	ollama := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/tags" {
			io.WriteString(w, `{"models":[{"name":"llama3.1:8b","size":4661224676},{"name":"qwen2.5:7b","size":4683087332}]}`)
			return
		}
		http.NotFound(w, r)
	}))
	defer ollama.Close()
	out := f.api("POST", "/api/ollama/detect", object{"baseUrl": ollama.URL}, 200)
	models := asObjects(out["models"])
	if len(models) != 2 || models[0]["name"] != "llama3.1:8b" {
		t.Fatal(out)
	}
	// Host publik ditolak sebelum request dibuat.
	f.api("POST", "/api/ollama/detect", object{"baseUrl": "https://example.com"}, 400)
	// Tambah provider.
	added := f.api("POST", "/api/ollama/add", object{"baseUrl": ollama.URL, "model": "llama3.1:8b"}, 200)
	if added["created"] != true || added["provider"].(object)["protocol"] != "ollama" {
		t.Fatal(added)
	}
	again := f.api("POST", "/api/ollama/add", object{"baseUrl": ollama.URL, "model": "qwen2.5:7b"}, 200)
	if again["created"] != false {
		t.Fatal(again)
	}
	providers := f.api("GET", "/api/providers", nil, 200)
	count := 0
	for _, p := range asObjects(providers["providers"]) {
		if p["protocol"] == "ollama" {
			count++
		}
	}
	if count != 1 {
		t.Fatal(providers)
	}
}
