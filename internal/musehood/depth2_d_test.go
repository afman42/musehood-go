package musehood

// Tes fitur pendalaman grup D: ikon+warna ruang (19), header tool kustom
// (22), tool kustom javascript (23), pipeline tool (24), izin tool per
// ruang (25), statistik skill (27).

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRoomIconColor(t *testing.T) {
	f := setup(t, nil)
	out := f.api("POST", "/api/rooms", object{"name": "Riset", "icon": "🔬", "color": "#12abEF"}, 201)
	rooms := asObjects(out["rooms"])
	var riset object
	for _, r := range rooms {
		if r["name"] == "Riset" {
			riset = r
		}
	}
	if riset == nil || riset["icon"] != "🔬" || riset["color"] != "#12abEF" {
		t.Fatal(riset)
	}
	roomID := riset["id"].(string)
	f.api("POST", "/api/rooms/"+roomID, object{"color": "merah"}, 400)
	f.api("POST", "/api/rooms/"+roomID, object{"color": "#12345"}, 400)
	f.api("POST", "/api/rooms/"+roomID, object{"icon": "123456789"}, 400)
	f.api("POST", "/api/rooms/"+roomID, object{"icon": "🚀", "color": "#000000"}, 200)
}

func TestCustomToolHeaders(t *testing.T) {
	var gotHeader string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeader = r.Header.Get("X-Test")
		io.WriteString(w, "isi dari server")
	}))
	defer server.Close()
	stubLookup(t, "93.184.216.34")
	f := setup(t, nil)
	tool := customTool{
		Name: "berheader", Type: "http_get", URL: server.URL + "/x",
		Params:  []toolParam{{Name: "kota", Type: "string"}},
		Headers: map[string]string{"X-Test": "halo-{{kota}}"},
	}
	writeCustomTool(t, f.app, "berheader.json", string(mustJSON(tool)))
	got, _ := f.app.runTool(context.Background(), settings{}, seat{Code: "M01", Name: "Plamo"}, "berheader", "Pontianak", false)
	if got != "isi dari server" {
		t.Fatal(got)
	}
	if gotHeader != "halo-Pontianak" {
		t.Fatal("header hasil substitusi salah:", gotHeader)
	}
	// Nilai header tidak masuk log audit tool.
	logOut := f.api("GET", "/api/tool-log", nil, 200)
	raw, _ := json.Marshal(logOut)
	if strings.Contains(string(raw), "halo-Pontianak") {
		t.Fatal("nilai header bocor ke log audit")
	}
}

func TestCustomToolJavaScript(t *testing.T) {
	f := setup(t, nil)
	tool := customTool{
		Name: "js_sapa", Type: "javascript",
		Code: "const fs=require('fs');const d=JSON.parse(fs.readFileSync(0,'utf8'));console.log('halo '+d.nama);",
	}
	writeCustomTool(t, f.app, "js_sapa.json", string(mustJSON(tool)))
	got, _ := f.app.runTool(context.Background(), settings{}, seat{Code: "M01", Name: "Plamo"}, "js_sapa", `{"nama":"dunia"}`, true)
	if !strings.Contains(got, "halo dunia") {
		t.Fatal(got)
	}
	// Tanpa toggle coding: ditolak.
	got, _ = f.app.runTool(context.Background(), settings{}, seat{Code: "M01", Name: "Plamo"}, "js_sapa", `{"nama":"dunia"}`, false)
	if !strings.Contains(got, "dimatikan") {
		t.Fatal(got)
	}
}

func TestCustomToolJavaScriptDangerousApproval(t *testing.T) {
	f := setup(t, nil)
	tool := customTool{
		Name: "js_bahaya", Type: "javascript",
		Code: "const cp=require('child_process');console.log('tidak boleh jalan');",
	}
	writeCustomTool(t, f.app, "js_bahaya.json", string(mustJSON(tool)))
	done := make(chan string, 1)
	go func() {
		got, _ := f.app.runTool(context.Background(), settings{}, seat{Code: "M01", Name: "Plamo"}, "js_bahaya", "{}", true)
		done <- got
	}()
	s := f.wait(func(s object) bool { return len(asObjects(s["pendingApprovals"])) == 1 })
	approval := asObjects(s["pendingApprovals"])[0]
	if approval["kind"] != "code" || approval["language"] != "javascript" {
		t.Fatal(approval)
	}
	f.api("POST", "/api/approvals/"+approval["id"].(string), object{"approved": false}, 200)
	got := <-done
	if !strings.Contains(got, "TIDAK dijalankan") {
		t.Fatal(got)
	}
}

func TestPipelineTool(t *testing.T) {
	server1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "hasil-satu")
	}))
	defer server1.Close()
	server2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "dua:"+r.URL.Query().Get("q"))
	}))
	defer server2.Close()
	stubLookup(t, "93.184.216.34")
	f := setup(t, nil)
	writeCustomTool(t, f.app, "satu.json", string(mustJSON(customTool{Name: "satu", Type: "http_get", URL: server1.URL + "/"})))
	writeCustomTool(t, f.app, "dua.json", string(mustJSON(customTool{Name: "dua", Type: "http_get", URL: server2.URL + "/?q={{arg}}"})))
	writeCustomTool(t, f.app, "pipa.json", string(mustJSON(customTool{Name: "pipa", Type: "pipeline", Steps: []string{"satu", "dua"}})))
	writeCustomTool(t, f.app, "pipa2.json", string(mustJSON(customTool{Name: "pipa2", Type: "pipeline", Steps: []string{"pipa"}})))
	got, _ := f.app.runTool(context.Background(), settings{}, seat{Code: "M01", Name: "Plamo"}, "pipa", "awal", false)
	if got != "dua:hasil-satu" {
		t.Fatal(got)
	}
	got, _ = f.app.runTool(context.Background(), settings{}, seat{Code: "M01", Name: "Plamo"}, "pipa2", "awal", false)
	if !strings.Contains(got, "tidak boleh memuat pipeline") {
		t.Fatal(got)
	}
}

func TestRoomDisabledTools(t *testing.T) {
	var bodies []string
	gateway := func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" {
			io.WriteString(w, `{"data":[{"id":"m"}]}`)
			return
		}
		raw, _ := io.ReadAll(r.Body)
		bodies = append(bodies, string(raw))
		io.WriteString(w, `{"choices":[{"message":{"content":"siap"}}]}`)
	}
	f := setup(t, gateway)
	f.api("POST", "/api/settings", object{"settings": object{"nativeToolsEnabled": false}}, 200)
	out := f.api("POST", "/api/rooms", object{"name": "TanpaJam", "seatCodes": []string{"M01"}, "disabledTools": []string{"current_time"}}, 201)
	_ = out
	f.api("POST", "/api/message", object{"text": "@M01 halo"}, 202)
	f.finish(2)
	if len(bodies) == 0 {
		t.Fatal("gateway tidak dipanggil")
	}
	if strings.Contains(bodies[len(bodies)-1], "- current_time:") {
		t.Fatal("tool nonaktif ruang tetap ditawarkan")
	}
	if !strings.Contains(bodies[len(bodies)-1], "- web_search:") {
		t.Fatal("tool lain ikut hilang")
	}
}

func TestSkillUsageCounts(t *testing.T) {
	f := setup(t, nil)
	writeSkill(t, f.app, "gaya-singkat", "---\nname: gaya-singkat\ndescription: Jawab singkat\n---\nSelalu jawab maksimal dua kalimat.")
	f.app.skillsMu.Lock()
	f.app.skillsCache = nil // paksa pindai ulang
	f.app.skillsMu.Unlock()
	f.api("POST", "/api/settings", object{"seats": []object{{"code": "M01", "skills": []string{"gaya-singkat"}}}}, 200)
	f.api("POST", "/api/message", object{"text": "@M01 halo"}, 202)
	f.finish(2)
	out := f.api("GET", "/api/skills", nil, 200)
	found := false
	for _, skill := range asObjects(out["skills"]) {
		if skill["name"] == "gaya-singkat" {
			found = true
			if usage, _ := skill["usage"].(float64); usage < 1 {
				t.Fatal("usage skill tidak naik:", skill)
			}
		}
	}
	if !found {
		t.Fatal("skill tidak terdaftar:", out)
	}
}
