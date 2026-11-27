package musehood

// Tes kontrak untuk endpoint fitur pendalaman: bentuk respons, status,
// dan bentuk error {error} yang konsisten.

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestContractLoginAndPIN(t *testing.T) {
	f := setup(t, nil)
	base := f.server.URL

	// Login saat PIN tidak aktif: 400 {error}.
	status, _, raw := doRaw(t, "POST", base+"/api/login", object{"pin": "1234"}, nil)
	assertErrorShape(t, status, raw, 400)

	// Pasang PIN: bentuk {ok:true, hasPin:true}.
	status, _, raw = doRaw(t, "POST", base+"/api/pin", object{"pin": "1234"}, nil)
	if status != 200 {
		t.Fatalf("set PIN = %d %s", status, raw)
	}
	var pinOut object
	if err := json.Unmarshal(raw, &pinOut); err != nil || pinOut["ok"] != true || pinOut["hasPin"] != true {
		t.Fatalf("bentuk respons pin = %s", raw)
	}

	// Login salah: 401 {error:"PIN salah."}.
	status, _, raw = doRaw(t, "POST", base+"/api/login", object{"pin": "0000"}, nil)
	assertErrorShape(t, status, raw, 401)
	if !strings.Contains(string(raw), "PIN salah") {
		t.Fatalf("pesan login salah = %s", raw)
	}

	// Login benar: 200 {ok:true} + Set-Cookie sesi.
	status, header, raw := doRaw(t, "POST", base+"/api/login", object{"pin": "1234"}, nil)
	if status != 200 {
		t.Fatalf("login = %d %s", status, raw)
	}
	var loginOut object
	if err := json.Unmarshal(raw, &loginOut); err != nil || loginOut["ok"] != true {
		t.Fatalf("bentuk respons login = %s", raw)
	}
	auth := map[string]string{"Cookie": sessionCookie(t, header)}

	// Ganti PIN dengan PIN lama salah: 401 {error}.
	status, _, raw = doRaw(t, "POST", base+"/api/pin", object{"pin": "5678", "oldPin": "salah"}, auth)
	assertErrorShape(t, status, raw, 401)
	// PIN terlalu pendek: 400.
	status, _, raw = doRaw(t, "POST", base+"/api/pin", object{"pin": "12", "oldPin": "1234"}, auth)
	assertErrorShape(t, status, raw, 400)
	// PIN terlalu panjang: 400.
	status, _, raw = doRaw(t, "POST", base+"/api/pin", object{"pin": strings.Repeat("9", 65), "oldPin": "1234"}, auth)
	assertErrorShape(t, status, raw, 400)
	// Ganti PIN benar: 200, hasPin tetap true.
	status, _, raw = doRaw(t, "POST", base+"/api/pin", object{"pin": "5678", "oldPin": "1234"}, auth)
	if status != 200 {
		t.Fatalf("ganti PIN = %d %s", status, raw)
	}
	auth = loginCookie(t, f, "5678")
	// Hapus PIN: hasPin false.
	status, _, raw = doRaw(t, "POST", base+"/api/pin", object{"pin": "", "oldPin": "5678"}, auth)
	if status != 200 {
		t.Fatalf("hapus PIN = %d %s", status, raw)
	}
	if err := json.Unmarshal(raw, &pinOut); err != nil || pinOut["hasPin"] != false {
		t.Fatalf("hasPin setelah hapus = %s", raw)
	}
	// State melaporkan hasPin false dan tidak memuat pinHash.
	state := f.state()
	settingsMap, _ := state["settings"].(map[string]any)
	if settingsMap["hasPin"] != false {
		t.Fatalf("hasPin state = %v", settingsMap["hasPin"])
	}
	if _, ok := settingsMap["pinHash"]; ok {
		t.Fatal("state tidak boleh memuat field pinHash")
	}
}

func assertErrorShape(t *testing.T, status int, raw []byte, want int) {
	t.Helper()
	if status != want {
		t.Fatalf("status = %d, mau %d: %s", status, want, raw)
	}
	var out object
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("error bukan JSON: %s", raw)
	}
	msg, ok := out["error"].(string)
	if !ok || strings.TrimSpace(msg) == "" {
		t.Fatalf("error harus {error: string non-kosong}: %s", raw)
	}
}

func TestContractToolLogShape(t *testing.T) {
	f := setup(t, nil)
	s := seat{Code: "M01", Name: "Plamo"}
	f.app.recordToolLog(s, "web_search", "berita hari ini", "hasil", 12*time.Millisecond)
	f.app.recordToolLog(s, "run_code", "python\nprint(1)", "Error: gagal", 3*time.Millisecond)

	out := f.api("GET", "/api/tool-log", nil, 200)
	entries, _ := out["entries"].([]any)
	if len(entries) != 2 {
		t.Fatalf("entries = %v", out)
	}
	// Terbaru dulu.
	first, _ := entries[0].(map[string]any)
	if first["tool"] != "run_code" || first["ok"] != false {
		t.Fatalf("entri terbaru salah: %v", first)
	}
	for _, key := range []string{"time", "seatCode", "seatName", "tool", "arg", "ok", "durationMs"} {
		if _, ok := first[key]; !ok {
			t.Fatalf("entri log kehilangan field %q: %v", key, first)
		}
	}
	if first["seatCode"] != "M01" || first["seatName"] != "Plamo" || first["durationMs"] != float64(3) {
		t.Fatalf("isi entri salah: %v", first)
	}
	// limit dihormati.
	out = f.api("GET", "/api/tool-log?limit=1", nil, 200)
	if entries, _ := out["entries"].([]any); len(entries) != 1 {
		t.Fatalf("limit=1 harus 1 entri: %v", out)
	}
	// limit tidak valid jatuh ke bawaan (tetap 200).
	f.api("GET", "/api/tool-log?limit=abc", nil, 200)
	f.api("GET", "/api/tool-log?limit=-5", nil, 200)
	// Log kosong berbentuk array kosong, bukan null.
	f2 := setup(t, nil)
	out = f2.api("GET", "/api/tool-log", nil, 200)
	if entries, ok := out["entries"].([]any); !ok || len(entries) != 0 {
		t.Fatalf("log kosong harus []: %v", out)
	}
}

func TestContractPersonasShape(t *testing.T) {
	f := setup(t, nil)
	out := f.api("GET", "/api/personas", nil, 200)
	personas, _ := out["personas"].([]any)
	if len(personas) != 6 {
		t.Fatalf("personas = %v", out)
	}
	for _, item := range personas {
		p, _ := item.(map[string]any)
		for _, key := range []string{"id", "name", "role", "prompt"} {
			if value, _ := p[key].(string); strings.TrimSpace(value) == "" {
				t.Fatalf("persona kehilangan %q: %v", key, p)
			}
		}
	}
	// Terapkan: 200 {seat:{...}} dengan role/prompt persona.
	applied := f.api("POST", "/api/seats/M02/persona", object{"personaId": "analis-data"}, 200)
	seatMap, _ := applied["seat"].(map[string]any)
	if seatMap["code"] != "M02" || seatMap["role"] != "Analis Data" {
		t.Fatalf("seat hasil persona = %v", seatMap)
	}
	// Persona tak dikenal 404; kursi tak dikenal 404; body kosong 404/400 {error}.
	status, raw := rawRequest(t, f, "POST", "/api/seats/M02/persona", mustJSON(object{"personaId": "ngawur"}), nil)
	assertErrorShape(t, status, raw, 404)
	status, raw = rawRequest(t, f, "POST", "/api/seats/ZZZ/persona", mustJSON(object{"personaId": "programmer"}), nil)
	assertErrorShape(t, status, raw, 404)
	status, raw = rawRequest(t, f, "POST", "/api/seats/M02/persona", mustJSON(object{}), nil)
	if status != 404 && status != 400 {
		t.Fatalf("personaId kosong harus 400/404, dapat %d", status)
	}
	assertErrorShape(t, status, raw, status)
}

func TestContractDebateValidation(t *testing.T) {
	f := setup(t, nil)
	cases := []struct {
		name string
		body object
		want int
	}{
		{"kursi sama", object{"seatA": "M01", "seatB": "M01", "topic": "Topik"}, 400},
		{"kursi antrean muse", object{"seatA": "MUSE", "seatB": "M02", "topic": "Topik"}, 400},
		{"kursi tak dikenal", object{"seatA": "M01", "seatB": "ZZZ", "topic": "Topik"}, 404},
		{"topik kosong", object{"seatA": "M01", "seatB": "M02", "topic": ""}, 400},
		{"topik spasi", object{"seatA": "M01", "seatB": "M02", "topic": "   "}, 400},
		{"ruang tak dikenal", object{"seatA": "M01", "seatB": "M02", "topic": "Topik", "roomId": "room-ngawur"}, 400},
	}
	for _, c := range cases {
		status, raw := rawRequest(t, f, "POST", "/api/debate", mustJSON(c.body), nil)
		if status != c.want {
			t.Fatalf("%s: status %d mau %d: %s", c.name, status, c.want, raw)
		}
		assertErrorShape(t, status, raw, c.want)
	}
	// Sah: 202 dengan jobId.
	out := f.api("POST", "/api/debate", object{"seatA": "M01", "seatB": "M02", "topic": "Topik kontrak"}, 202)
	if jobID, _ := out["jobId"].(string); jobID == "" {
		t.Fatalf("respons debat harus memuat jobId: %v", out)
	}
}

func TestContractSkillInstallShape(t *testing.T) {
	oldFetch := skillFetchURL
	skillFetchURL = func(ctx context.Context, rawURL string) ([]byte, error) {
		return []byte("---\nname: skill-kontrak\ndescription: Uji kontrak\n---\nIsi."), nil
	}
	defer func() { skillFetchURL = oldFetch }()

	f := setup(t, nil)
	out := f.api("POST", "/api/skills/install", object{"url": "https://contoh.test/skill.md"}, 200)
	if out["name"] != "skill-kontrak" || out["source"] != "lokal" {
		t.Fatalf("bentuk install = %v", out)
	}
	status, raw := rawRequest(t, f, "POST", "/api/skills/install", mustJSON(object{"url": "https://contoh.test/skill.md"}), nil)
	assertErrorShape(t, status, raw, 409)
	status, raw = rawRequest(t, f, "POST", "/api/skills/install", mustJSON(object{"url": "bukan url"}), nil)
	assertErrorShape(t, status, raw, 400)
	status, raw = rawRequest(t, f, "POST", "/api/skills/install", mustJSON(object{}), nil)
	assertErrorShape(t, status, raw, 400)
}

func TestContractHealthAndLatencyShape(t *testing.T) {
	delays := map[string]time.Duration{"lambat": 90 * time.Millisecond, "cepat": 5 * time.Millisecond}
	extra := func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "GET /v1/models":
			io.WriteString(w, `{"data":[{"id":"lambat"},{"id":"cepat"},{"id":"gagal"}]}`)
		case "POST /v1/chat/completions":
			var body struct{ Model string }
			json.NewDecoder(r.Body).Decode(&body)
			if body.Model == "gagal" {
				w.WriteHeader(500)
				io.WriteString(w, "rusak")
				return
			}
			time.Sleep(delays[body.Model])
			io.WriteString(w, `{"choices":[{"message":{"content":"OK"}}]}`)
		default:
			http.NotFound(w, r)
		}
	}
	f := setup(t, extra)
	id := providerID(t, f)

	out := f.api("POST", "/api/providers/"+id+"/health", object{}, 200)
	health, _ := out["health"].(map[string]any)
	if health["ok"] != true {
		t.Fatalf("health = %v", out)
	}
	if _, ok := health["latencyMs"].(float64); !ok {
		t.Fatalf("latencyMs harus angka: %v", health)
	}
	if checkedAt, _ := health["checkedAt"].(string); checkedAt == "" {
		t.Fatalf("checkedAt kosong: %v", health)
	}
	status, raw := rawRequest(t, f, "POST", "/api/providers/tidak-ada/health", mustJSON(object{}), nil)
	assertErrorShape(t, status, raw, 404)

	out = f.api("POST", "/api/providers/"+id+"/latency-test", object{}, 200)
	if out["providerId"] != id {
		t.Fatalf("providerId = %v", out)
	}
	results, _ := out["results"].([]any)
	if len(results) != 3 {
		t.Fatalf("results = %v", out)
	}
	prevOK := true
	prevMs := float64(-1)
	for _, item := range results {
		entry, _ := item.(map[string]any)
		ok, _ := entry["ok"].(bool)
		ms, _ := entry["latencyMs"].(float64)
		if prevOK && !ok {
			// transisi ok→gagal diizinkan sekali
		} else if !prevOK && ok {
			t.Fatalf("model gagal harus di akhir: %v", results)
		}
		if ok && prevOK && ms < prevMs {
			t.Fatalf("hasil ok harus terurut naik latency: %v", results)
		}
		if !ok {
			if _, hasErr := entry["error"]; !hasErr {
				t.Fatalf("model gagal harus memuat error: %v", entry)
			}
		}
		prevOK, prevMs = ok, ms
	}
	status, raw = rawRequest(t, f, "POST", "/api/providers/tidak-ada/latency-test", mustJSON(object{}), nil)
	assertErrorShape(t, status, raw, 404)
}

func TestContractRerunForkArchive(t *testing.T) {
	f := setup(t, nil)
	f.api("POST", "/api/message", object{"text": "@Plamo kontrak rerun"}, 202)
	state := f.wait(func(s object) bool { return completedSeatReplies(s) >= 1 })
	owner := findOwnerMessage(state, "@Plamo kontrak rerun")
	var seatMsg map[string]any
	for _, m := range messagesOf(state) {
		if m["authorType"] == "seat" {
			seatMsg = m
		}
	}
	// Rerun pesan kursi 403; pesan tak ada 404.
	status, raw := rawRequest(t, f, "POST", "/api/message/"+seatMsg["id"].(string)+"/rerun", mustJSON(object{}), nil)
	assertErrorShape(t, status, raw, 403)
	status, raw = rawRequest(t, f, "POST", "/api/message/00000000-0000-4000-8000-000000000000/rerun", mustJSON(object{}), nil)
	assertErrorShape(t, status, raw, 404)
	// Rerun sah: 202 {jobId}.
	out := f.api("POST", "/api/message/"+owner["id"].(string)+"/rerun", object{}, 202)
	if jobID, _ := out["jobId"].(string); jobID == "" {
		t.Fatalf("rerun harus memuat jobId: %v", out)
	}
	// Fork pesan tak ada 404; sah 200 {roomId}.
	status, raw = rawRequest(t, f, "POST", "/api/message/00000000-0000-4000-8000-000000000000/fork", mustJSON(object{}), nil)
	assertErrorShape(t, status, raw, 404)
	out = f.api("POST", "/api/message/"+owner["id"].(string)+"/fork", object{}, 200)
	if roomID, _ := out["roomId"].(string); roomID == "" {
		t.Fatalf("fork harus memuat roomId: %v", out)
	}
	// Arsip: ruang tak ada 404; ruang aktif 400; sah 200 {rooms, activeRoomId}.
	status, raw = rawRequest(t, f, "POST", "/api/rooms/room-ngawur/archive", mustJSON(object{"archived": true}), nil)
	assertErrorShape(t, status, raw, 404)
	activeID := f.state()["activeRoomId"].(string)
	status, raw = rawRequest(t, f, "POST", "/api/rooms/"+activeID+"/archive", mustJSON(object{"archived": true}), nil)
	assertErrorShape(t, status, raw, 400)
	other := f.api("POST", "/api/rooms", object{"name": "Ruang Kontrak"}, 201)["activeRoomId"].(string)
	f.api("POST", "/api/rooms/"+activeID+"/activate", object{}, 200)
	out = f.api("POST", "/api/rooms/"+other+"/archive", object{"archived": true}, 200)
	if _, ok := out["rooms"].([]any); !ok || out["activeRoomId"] != activeID {
		t.Fatalf("bentuk arsip = %v", out)
	}
}

func TestContractExportDiscussionHeaders(t *testing.T) {
	f := setup(t, nil)
	f.api("POST", "/api/message", object{"text": "@Plamo ekspor kontrak"}, 202)
	state := f.wait(func(s object) bool { return completedSeatReplies(s) >= 1 })
	owner := findOwnerMessage(state, "@Plamo ekspor kontrak")
	id := owner["id"].(string)

	check := func(path, wantType, wantFile string) {
		t.Helper()
		res, err := http.Get(f.server.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		body, _ := io.ReadAll(res.Body)
		if res.StatusCode != 200 {
			t.Fatalf("%s = %d %s", path, res.StatusCode, body)
		}
		if ct := res.Header.Get("Content-Type"); !strings.HasPrefix(ct, wantType) {
			t.Fatalf("%s Content-Type = %q, mau awalan %q", path, ct, wantType)
		}
		if cd := res.Header.Get("Content-Disposition"); !strings.Contains(cd, wantFile) {
			t.Fatalf("%s Content-Disposition = %q, mau memuat %q", path, cd, wantFile)
		}
	}
	check("/api/export?format=json&message="+id, "application/json", "diskusi-musehood.json")
	check("/api/export?format=md&message="+id, "text/markdown", "diskusi-musehood.md")
	check("/api/export?format=html&message="+id, "text/html", "diskusi-musehood.html")
	check("/api/export?format=json", "application/json", "riwayat-musehood.json")
	// Format tak dikenal 400 {error}; pesan tak ada 400 {error}.
	status, raw := rawRequest(t, f, "GET", "/api/export?format=pdf", nil, nil)
	assertErrorShape(t, status, raw, 400)
	status, raw = rawRequest(t, f, "GET", "/api/export?format=json&message=00000000-0000-4000-8000-000000000000", nil, nil)
	assertErrorShape(t, status, raw, 400)
	// Ekspor diskusi JSON memuat exportedAt/room/messages.
	out := f.api("GET", "/api/export?format=json&message="+id, nil, 200)
	for _, key := range []string{"exportedAt", "room", "messages"} {
		if _, ok := out[key]; !ok {
			t.Fatalf("ekspor kehilangan %q: %v", key, out)
		}
	}
	if msgs, _ := out["messages"].([]any); len(msgs) != 2 {
		t.Fatalf("ekspor diskusi harus 2 pesan: %v", out)
	}
}
