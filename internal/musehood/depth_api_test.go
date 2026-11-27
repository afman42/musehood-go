package musehood

// Tes endpoint API untuk fitur pendalaman: matriks metode, body rusak,
// ID tak dikenal, konflik, header keamanan cookie, dan gerbang PIN.

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

func TestAPIMethodMatrixDepthEndpoints(t *testing.T) {
	f := setup(t, nil)
	id := providerID(t, f)
	cases := []struct{ method, path string }{
		{"GET", "/api/login"},
		{"PUT", "/api/login"},
		{"GET", "/api/logout"},
		{"GET", "/api/pin"},
		{"DELETE", "/api/pin"},
		{"POST", "/api/personas"},
		{"DELETE", "/api/personas"},
		{"POST", "/api/tool-log"},
		{"PUT", "/api/tool-log"},
		{"GET", "/api/debate"},
		{"DELETE", "/api/debate"},
		{"GET", "/api/skills/install"},
		{"PUT", "/api/skills/install"},
		{"POST", "/api/export"},
	}
	for _, c := range cases {
		status, raw := rawRequest(t, f, c.method, c.path, nil, nil)
		if status != 405 {
			t.Errorf("%s %s: mau 405, dapat %d %s", c.method, c.path, status, raw)
		}
	}
	// Endpoint yang benar-benar tidak ada tetap 404, bukan 405.
	for _, path := range []string{"/api/tidak-ada", "/api/providers/x/tidak-ada", "/api/rooms/x/tidak-ada", "/api/message/abc/tidak-ada"} {
		status, _ := rawRequest(t, f, "POST", path, nil, nil)
		if status != 404 {
			t.Errorf("POST %s: mau 404, dapat %d", path, status)
		}
	}
	// Sub-rute ber-ID yang hanya dikenal sebagai POST: metode lain 404
	// (perilaku allowlist saat ini untuk pola path ber-ID).
	for _, c := range []struct{ method, path string }{
		{"GET", "/api/message/abc/rerun"},
		{"GET", "/api/message/abc/fork"},
		{"GET", "/api/providers/" + id + "/health"},
		{"DELETE", "/api/providers/" + id + "/health"},
		{"GET", "/api/providers/" + id + "/latency-test"},
		{"PUT", "/api/providers/" + id + "/latency-test"},
		{"GET", "/api/seats/M01/persona"},
		{"DELETE", "/api/seats/M01/persona"},
		{"GET", "/api/rooms/room-x/archive"},
		{"DELETE", "/api/rooms/room-x/archive"},
	} {
		status, _ := rawRequest(t, f, c.method, c.path, nil, nil)
		if status != 404 {
			t.Errorf("%s %s: mau 404, dapat %d", c.method, c.path, status)
		}
	}
}

func TestAPIBrokenBodyDepthEndpoints(t *testing.T) {
	f := setup(t, nil)
	id := providerID(t, f)
	broken := []byte("{ini bukan json")
	cases := []struct{ method, path string }{
		{"POST", "/api/login"},
		{"POST", "/api/pin"},
		{"POST", "/api/debate"},
		{"POST", "/api/skills/install"},
		{"POST", "/api/seats/M01/persona"},
		{"POST", "/api/rooms/room-x/archive"},
		{"POST", "/api/providers/" + id + "/health"},
	}
	for _, c := range cases {
		status, raw := rawRequest(t, f, c.method, c.path, broken, nil)
		// Health tidak membaca body: boleh 200; sisanya harus 400 {error}.
		if c.path == "/api/providers/"+id+"/health" {
			if status != 200 && status != 400 {
				t.Errorf("%s %s body rusak: status %d", c.method, c.path, status)
			}
			continue
		}
		assertErrorShape(t, status, raw, 400)
	}
	// Body JSON valid tapi tipe salah juga 400.
	status, raw := rawRequest(t, f, "POST", "/api/debate", []byte(`{"seatA":123}`), nil)
	assertErrorShape(t, status, raw, 400)
	status, raw = rawRequest(t, f, "POST", "/api/login", []byte(`{"pin":123}`), nil)
	assertErrorShape(t, status, raw, 400)
	status, raw = rawRequest(t, f, "POST", "/api/rooms/room-x/archive", []byte(`{"archived":"ya"}`), nil)
	assertErrorShape(t, status, raw, 400)
	// Body kosong pada endpoint yang butuh field: 400/404 {error}, bukan panic/500.
	for _, path := range []string{"/api/login", "/api/pin", "/api/debate", "/api/skills/install", "/api/seats/M01/persona"} {
		status, raw := rawRequest(t, f, "POST", path, nil, nil)
		if status == 500 || status == 200 {
			t.Errorf("POST %s body kosong: status %d %s", path, status, raw)
		}
	}
}

func TestAPIUnknownIDsDepthEndpoints(t *testing.T) {
	f := setup(t, nil)
	missing := "00000000-0000-4000-8000-000000000000"
	cases := []struct {
		method, path string
		body         object
		want         int
	}{
		{"POST", "/api/seats/ZZZ/persona", object{"personaId": "programmer"}, 404},
		{"POST", "/api/debate", object{"seatA": "ZZZ", "seatB": "M02", "topic": "T"}, 404},
		{"POST", "/api/debate", object{"seatA": "M01", "seatB": "ZZZ", "topic": "T"}, 404},
		{"POST", "/api/providers/tidak-ada/health", object{}, 404},
		{"POST", "/api/providers/tidak-ada/latency-test", object{}, 404},
		{"POST", "/api/rooms/tidak-ada/archive", object{"archived": true}, 404},
		{"POST", "/api/rooms/tidak-ada/activate", object{}, 404},
		{"POST", "/api/message/" + missing + "/fork", object{}, 404},
		{"POST", "/api/message/" + missing + "/rerun", object{}, 404},
		{"POST", "/api/message/" + missing + "/react", object{"emoji": "👍"}, 404},
	}
	for _, c := range cases {
		status, raw := rawRequest(t, f, c.method, c.path, mustJSON(c.body), nil)
		if status != c.want {
			t.Errorf("%s %s: mau %d, dapat %d %s", c.method, c.path, c.want, status, raw)
		}
		assertErrorShape(t, status, raw, c.want)
	}
}

func TestAPIConflictsDepthEndpoints(t *testing.T) {
	oldFetch := skillFetchURL
	skillFetchURL = func(ctx context.Context, rawURL string) ([]byte, error) {
		return []byte("---\nname: skill-api\ndescription: x\n---\nisi"), nil
	}
	defer func() { skillFetchURL = oldFetch }()

	f := setup(t, nil)
	// Duplikat skill: 409.
	f.api("POST", "/api/skills/install", object{"url": "https://contoh.test/s.md"}, 200)
	status, raw := rawRequest(t, f, "POST", "/api/skills/install", mustJSON(object{"url": "https://contoh.test/s.md"}), nil)
	assertErrorShape(t, status, raw, 409)
	// Arsip ruang aktif: 400.
	activeID := f.state()["activeRoomId"].(string)
	status, raw = rawRequest(t, f, "POST", "/api/rooms/"+activeID+"/archive", mustJSON(object{"archived": true}), nil)
	assertErrorShape(t, status, raw, 400)
	// Rerun pesan kursi: 403.
	f.api("POST", "/api/message", object{"text": "@Plamo konflik api"}, 202)
	state := f.wait(func(s object) bool { return completedSeatReplies(s) >= 1 })
	var seatMsg map[string]any
	for _, m := range messagesOf(state) {
		if m["authorType"] == "seat" {
			seatMsg = m
		}
	}
	status, raw = rawRequest(t, f, "POST", "/api/message/"+seatMsg["id"].(string)+"/rerun", mustJSON(object{}), nil)
	assertErrorShape(t, status, raw, 403)
	// Debat kursi antrean (MUSE): 400.
	status, raw = rawRequest(t, f, "POST", "/api/debate", mustJSON(object{"seatA": "MUSE", "seatB": "M01", "topic": "T"}), nil)
	assertErrorShape(t, status, raw, 400)
}

func TestAPICookieSecurityHeaders(t *testing.T) {
	f := setup(t, nil)
	base := f.server.URL
	if status, _, _ := doRaw(t, "POST", base+"/api/pin", object{"pin": "1122"}, nil); status != 200 {
		t.Fatal("set PIN gagal")
	}
	status, header, _ := doRaw(t, "POST", base+"/api/login", object{"pin": "1122"}, nil)
	if status != 200 {
		t.Fatalf("login = %d", status)
	}
	joined := strings.Join(header.Values("Set-Cookie"), ";")
	for _, want := range []string{sessionCookieName + "=", "HttpOnly", "SameSite=Lax", "Path=/", "Max-Age="} {
		if !strings.Contains(joined, want) {
			t.Fatalf("Set-Cookie login kehilangan %q: %s", want, joined)
		}
	}
	// Logout mengosongkan cookie dengan Max-Age=0.
	auth := map[string]string{"Cookie": sessionCookie(t, header)}
	status, header, _ = doRaw(t, "POST", base+"/api/logout", object{}, auth)
	if status != 200 {
		t.Fatalf("logout = %d", status)
	}
	joined = strings.Join(header.Values("Set-Cookie"), ";")
	if !strings.Contains(joined, sessionCookieName+"=;") && !strings.Contains(joined, sessionCookieName+"= ") {
		t.Fatalf("logout harus mengosongkan cookie: %s", joined)
	}
	if !strings.Contains(joined, "Max-Age=0") {
		t.Fatalf("logout harus Max-Age=0: %s", joined)
	}
	// Endpoint lain tidak memasang cookie sesi.
	_, header, _ = doRaw(t, "GET", base+"/api/state", nil, auth)
	for _, cookie := range header.Values("Set-Cookie") {
		if strings.HasPrefix(cookie, sessionCookieName+"=") {
			t.Fatalf("/api/state tidak boleh memasang cookie sesi: %s", cookie)
		}
	}
}

func TestAPIStateRequiresPINExact(t *testing.T) {
	f := setup(t, nil)
	base := f.server.URL
	if status, _, _ := doRaw(t, "POST", base+"/api/pin", object{"pin": "3344"}, nil); status != 200 {
		t.Fatal("set PIN gagal")
	}
	// Tanpa sesi: 401 dengan pesan persis "PIN diperlukan".
	status, _, raw := doRaw(t, "GET", base+"/api/state", nil, nil)
	if status != 401 {
		t.Fatalf("state tanpa sesi = %d", status)
	}
	if strings.TrimSpace(string(raw)) != `{"error":"PIN diperlukan"}` {
		t.Fatalf("bentuk error PIN = %s", raw)
	}
	// Semua endpoint API lain juga tergerbang (sampel lintas kategori).
	for _, c := range []struct{ method, path string }{
		{"GET", "/api/stats"},
		{"GET", "/api/search?q=x"},
		{"GET", "/api/personas"},
		{"GET", "/api/tool-log"},
		{"GET", "/api/skills"},
		{"GET", "/api/tools"},
		{"POST", "/api/message"},
		{"POST", "/api/debate"},
		{"POST", "/api/pin"},
		{"POST", "/api/logout"},
		{"GET", "/api/backup"},
		{"GET", "/api/export?format=json"},
		{"GET", "/api/providers"},
	} {
		status, _, _ := doRaw(t, c.method, base+c.path, object{}, nil)
		if status != 401 {
			t.Errorf("%s %s tanpa sesi harus 401, dapat %d", c.method, c.path, status)
		}
	}
	// Halaman SPA tetap bisa dimuat (layar login dirender klien).
	res, err := http.Get(base + "/")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("halaman utama harus 200 walau PIN aktif, dapat %d", res.StatusCode)
	}
}
