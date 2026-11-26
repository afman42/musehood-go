package musehood

// Tes API (endpoint) dan kontrak respons: status code, bentuk JSON, pesan
// error, redaksi kredensial, dan validasi input untuk semua endpoint.

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func rawRequest(t *testing.T, f fixture, method, path string, body []byte, headers map[string]string) (int, []byte) {
	t.Helper()
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, f.server.URL+path, reader)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	return res.StatusCode, raw
}

func TestAPIUnknownEndpointAndMethod(t *testing.T) {
	f := setup(t, nil)
	status, raw := rawRequest(t, f, "GET", "/api/tidak-ada", nil, nil)
	if status != 404 || !strings.Contains(string(raw), "Endpoint API tidak ditemukan") {
		t.Fatalf("endpoint asing: %d %s", status, raw)
	}
	// Metode salah pada endpoint yang ada -> 405.
	status, raw = rawRequest(t, f, "DELETE", "/api/state", nil, nil)
	if status != 405 {
		t.Fatalf("DELETE /api/state: %d %s", status, raw)
	}
	status, _ = rawRequest(t, f, "GET", "/api/message", nil, nil)
	if status != 405 {
		t.Fatalf("GET /api/message: %d", status)
	}
	// Body bukan JSON -> 400 dengan kunci error.
	status, raw = rawRequest(t, f, "POST", "/api/message", []byte("{bukan json"), nil)
	var out object
	if status != 400 || json.Unmarshal(raw, &out) != nil || out["error"] == nil {
		t.Fatalf("body rusak: %d %s", status, raw)
	}
}

func TestAPIGetEndpointsContract(t *testing.T) {
	f := setup(t, nil)
	cases := map[string]string{
		"/api/state":     "seats",
		"/api/providers": "providers",
		"/api/rooms":     "rooms",
		"/api/presets":   "presets",
		"/api/stats":     "stats",
		"/api/skills":    "skills",
		"/api/tools":     "tools",
		"/api/backup":    "version",
	}
	for path, key := range cases {
		out := f.api("GET", path, nil, 200)
		if _, ok := out[key]; !ok {
			t.Fatalf("%s tanpa kunci %q: %v", path, key, out)
		}
	}
	// Pencarian tanpa query tetap 200.
	f.api("GET", "/api/search", nil, 200)
	f.api("GET", "/api/search?q=halo", nil, 200)
	// Export JSON dan HTML.
	status, raw := rawRequest(t, f, "GET", "/api/export?format=json", nil, nil)
	if status != 200 || !strings.Contains(string(raw), "messages") {
		t.Fatalf("export json: %d", status)
	}
	status, raw = rawRequest(t, f, "GET", "/api/export?format=html", nil, nil)
	if status != 200 || !strings.Contains(strings.ToLower(string(raw)), "<html") {
		t.Fatalf("export html: %d", status)
	}
}

func TestAPIStateContract(t *testing.T) {
	f := setup(t, nil)
	state := f.state()
	for _, key := range []string{"version", "group", "owner", "seats", "settings", "messages", "jobs", "rooms", "activeRoomId"} {
		if _, ok := state[key]; !ok {
			t.Fatalf("state tanpa kunci %q", key)
		}
	}
	seats, ok := state["seats"].([]any)
	if !ok || len(seats) != 7 {
		t.Fatalf("seats = %v", state["seats"])
	}
	first, ok := seats[0].(map[string]any)
	if !ok {
		t.Fatalf("seat bukan objek: %v", seats[0])
	}
	for _, key := range []string{"code", "name", "role", "provider", "present"} {
		if _, ok := first[key]; !ok {
			t.Fatalf("seat tanpa kunci %q: %v", key, first)
		}
	}
	settings, ok := state["settings"].(map[string]any)
	if !ok {
		t.Fatalf("settings bukan objek")
	}
	// API key settings harus termask, tidak pernah utuh.
	if key, _ := settings["apiKey"].(string); strings.Contains(key, secret) {
		t.Fatalf("apiKey bocor di state: %q", key)
	}
}

func TestAPIProviderRedactionContract(t *testing.T) {
	f := setup(t, nil)
	out := f.api("GET", "/api/providers", nil, 200)
	providers, ok := out["providers"].([]any)
	if !ok || len(providers) == 0 {
		t.Fatalf("providers = %v", out)
	}
	for _, item := range providers {
		view, ok := item.(map[string]any)
		if !ok {
			t.Fatalf("provider bukan objek: %v", item)
		}
		if _, leaked := view["apiKey"]; leaked {
			t.Fatalf("apiKey tidak boleh ada di tampilan provider: %v", view)
		}
		if _, ok := view["hasApiKey"]; !ok {
			t.Fatalf("provider tanpa hasApiKey: %v", view)
		}
	}
	// Backup: semua apiKey dikosongkan.
	backup := f.api("GET", "/api/backup", nil, 200)
	raw, _ := json.Marshal(backup)
	if strings.Contains(string(raw), secret) {
		t.Fatal("backup membocorkan API key")
	}
	if list, ok := backup["providers"].([]any); ok {
		for _, item := range list {
			if view, ok := item.(map[string]any); ok {
				if key, _ := view["apiKey"].(string); key != "" {
					t.Fatalf("backup apiKey harus kosong: %v", view)
				}
			}
		}
	}
}

func TestAPISettingsValidation(t *testing.T) {
	f := setup(t, nil)
	bad := []object{
		{"seats": []object{{"code": "M99", "name": "Hantu"}}},
		{"seats": []object{{"code": "M01", "provider": "sembarang"}}},
		{"seats": []object{{"code": "M01", "role": ""}}},
		{"seats": []object{{"code": "M01", "name": "GLM"}}}, // nama tabrakan dengan M02
		{"seats": []object{{"code": "M01", "skills": []string{"nama\njahat"}}}},
		{"settings": object{"reviewLevel": "Ekstrem"}},
		{"settings": object{"gatewayProtocol": "grpc"}},
		{"settings": object{"integratorSeat": "M99"}},
	}
	for i, body := range bad {
		status, raw := rawRequest(t, f, "POST", "/api/settings", mustJSON(body), nil)
		if status != 400 {
			t.Fatalf("kasus %d: mau 400, dapat %d %s", i, status, raw)
		}
	}
	// Lebih dari 20 skill ditolak.
	many := make([]string, 21)
	for i := range many {
		many[i] = "skill"
	}
	status, _ := rawRequest(t, f, "POST", "/api/settings", mustJSON(object{"seats": []object{{"code": "M01", "skills": many}}}), nil)
	if status != 400 {
		t.Fatalf("21 skill: mau 400, dapat %d", status)
	}
	// Patch sah diterima.
	f.api("POST", "/api/settings", object{"seats": []object{{"code": "M01", "skills": []string{"contoh"}}}}, 200)
	state := f.state()
	for _, item := range state["seats"].([]any) {
		seat := item.(map[string]any)
		if seat["code"] == "M01" {
			skills, _ := seat["skills"].([]any)
			if len(skills) != 1 || skills[0] != "contoh" {
				t.Fatalf("skills M01 = %v", seat["skills"])
			}
		}
	}
}

func TestAPIMessageValidation(t *testing.T) {
	f := setup(t, nil)
	for _, body := range []object{
		{"text": ""},
		{"text": "   "},
		{},
	} {
		status, _ := rawRequest(t, f, "POST", "/api/message", mustJSON(body), nil)
		if status != 400 {
			t.Fatalf("pesan %v: mau 400, dapat %d", body, status)
		}
	}
	// Pesan sah -> 202 + jobId.
	out := f.api("POST", "/api/message", object{"text": "@Plamo halo"}, 202)
	if out["jobId"] == nil || out["status"] != "processing" {
		t.Fatalf("respons pesan: %v", out)
	}
}

func TestAPIHostAndOriginGuard(t *testing.T) {
	f := setup(t, nil)
	// Host asing ditolak 403 (header Host diganti manual).
	req, err := http.NewRequest("GET", f.server.URL+"/api/state", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Host = "jahat.example.com"
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 403 {
		t.Fatalf("host asing: mau 403, dapat %d", res.StatusCode)
	}
	// Sec-Fetch-Site cross-site pada API ditolak.
	status, _ := rawRequest(t, f, "GET", "/api/state", nil, map[string]string{"Sec-Fetch-Site": "cross-site"})
	if status != 403 {
		t.Fatalf("cross-site API: mau 403, dapat %d", status)
	}
	// Origin asing pada POST ditolak.
	status, _ = rawRequest(t, f, "POST", "/api/clear", []byte("{}"), map[string]string{"Origin": "https://jahat.example.com"})
	if status != 403 {
		t.Fatalf("origin asing: mau 403, dapat %d", status)
	}
}
