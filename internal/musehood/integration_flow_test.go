package musehood

// Tes integrasi + E2E layanan: alur penuh lewat HTTP API dengan gateway
// tiruan — injeksi skill, fallback model, persetujuan via API, tool kustom,
// voting, dan isolasi ruang.

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

type capturedCall struct {
	Model  string
	System string
	User   string
}

func chatJSON(w http.ResponseWriter, text string) {
	w.Header().Set("Content-Type", "application/json")
	raw, _ := json.Marshal(object{"choices": []any{object{"message": object{"content": text}}}})
	_, _ = w.Write(raw)
}

func decodeCall(r *http.Request) capturedCall {
	var body struct {
		Model    string
		Messages []struct{ Role, Content string }
	}
	raw, _ := io.ReadAll(r.Body)
	_ = json.Unmarshal(raw, &body)
	call := capturedCall{Model: body.Model}
	for _, m := range body.Messages {
		if m.Role == "system" {
			call.System = m.Content
		}
		if m.Role == "user" {
			call.User = m.Content
		}
	}
	return call
}

func setSeatModel(t *testing.T, f fixture, code, model string, extra object) {
	t.Helper()
	patch := object{"code": code, "model": model}
	for key, value := range extra {
		patch[key] = value
	}
	f.api("POST", "/api/settings", object{"seats": []object{patch}}, 200)
}

func TestIntegrationSkillInjection(t *testing.T) {
	var mu sync.Mutex
	var calls []capturedCall
	f := setup(t, func(w http.ResponseWriter, r *http.Request) {
		call := decodeCall(r)
		mu.Lock()
		calls = append(calls, call)
		mu.Unlock()
		chatJSON(w, "siap")
	})
	writeSkill(t, f.app, "uji-skill", "---\nname: uji-skill\ndescription: Skill uji integrasi\n---\nSelalu akhiri jawaban dengan kata INTEGRASI.")
	setSeatModel(t, f, "M01", "model-uji", object{"skills": []string{"uji-skill"}})
	f.api("POST", "/api/message", object{"text": "@Plamo halo"}, 202)
	f.wait(func(s object) bool {
		for _, item := range s["messages"].([]any) {
			m := item.(map[string]any)
			if m["seat"] == "M01" && m["status"] == "completed" {
				return true
			}
		}
		return false
	})
	mu.Lock()
	defer mu.Unlock()
	found := false
	for _, call := range calls {
		if strings.Contains(call.System, "## Skill: uji-skill") && strings.Contains(call.System, "kata INTEGRASI") {
			found = true
		}
	}
	if !found {
		t.Fatalf("skill tidak terinjeksi ke system prompt: %+v", calls)
	}
}

func TestIntegrationFallbackModel(t *testing.T) {
	f := setup(t, nil)
	setSeatModel(t, f, "M01", "denied", object{"fallbackModel": "model-cadangan"})
	f.api("POST", "/api/message", object{"text": "@Plamo halo"}, 202)
	state := f.wait(func(s object) bool {
		for _, item := range s["messages"].([]any) {
			m := item.(map[string]any)
			if m["seat"] == "M01" && (m["status"] == "completed" || m["status"] == "error") {
				return true
			}
		}
		return false
	})
	for _, item := range state["messages"].([]any) {
		m := item.(map[string]any)
		if m["seat"] == "M01" {
			if m["status"] != "completed" || !strings.Contains(m["text"].(string), "balasan model-cadangan") {
				t.Fatalf("fallback tidak dipakai: %v", m)
			}
		}
	}
}

func TestIntegrationApprovalViaAPI(t *testing.T) {
	var mu sync.Mutex
	count := 0
	f := setup(t, func(w http.ResponseWriter, r *http.Request) {
		_ = decodeCall(r)
		mu.Lock()
		count++
		n := count
		mu.Unlock()
		if n == 1 {
			chatJSON(w, "<<TOOL:run_code>>python\nimport os; os.system(\"echo halo\")<</TOOL>>")
			return
		}
		chatJSON(w, "Kode tidak dijalankan sesuai keputusan pemilik.")
	})
	setSeatModel(t, f, "M01", "model-uji", nil)
	f.api("POST", "/api/message", object{"text": "@Plamo jalankan kode"}, 202)

	// Persetujuan muncul di state.
	state := f.wait(func(s object) bool {
		pending, _ := s["pendingApprovals"].([]any)
		return len(pending) == 1
	})
	pending := state["pendingApprovals"].([]any)[0].(map[string]any)
	if pending["kind"] != "code" || pending["seatCode"] != "M01" {
		t.Fatalf("pendingApprovals: %v", pending)
	}
	id, _ := pending["id"].(string)
	if id == "" {
		t.Fatalf("id persetujuan kosong: %v", pending)
	}
	// ID asing -> 404.
	status, _ := rawRequest(t, f, "POST", "/api/approvals/tidak-ada", mustJSON(object{"approved": true}), nil)
	if status != 404 {
		t.Fatalf("approval asing: mau 404, dapat %d", status)
	}
	// Tolak lewat API -> pesan selesai, kode tidak jalan.
	f.api("POST", "/api/approvals/"+id, object{"approved": false}, 200)
	final := f.wait(func(s object) bool {
		for _, item := range s["messages"].([]any) {
			m := item.(map[string]any)
			if m["seat"] == "M01" && m["status"] == "completed" {
				return true
			}
		}
		return false
	})
	for _, item := range final["messages"].([]any) {
		m := item.(map[string]any)
		if m["seat"] == "M01" {
			tools, _ := m["toolsUsed"].([]any)
			if len(tools) != 1 || tools[0] != "run_code" {
				t.Fatalf("toolsUsed: %v", m["toolsUsed"])
			}
			if strings.Contains(m["text"].(string), "halo\n") && !strings.Contains(m["text"].(string), "tidak dijalankan") {
				t.Fatalf("kode seharusnya tidak jalan: %v", m["text"])
			}
		}
	}
	if pending, _ := final["pendingApprovals"].([]any); len(pending) != 0 {
		t.Fatalf("persetujuan harus bersih: %v", pending)
	}
}

func TestIntegrationCustomShellTool(t *testing.T) {
	var mu sync.Mutex
	count := 0
	f := setup(t, func(w http.ResponseWriter, r *http.Request) {
		_ = decodeCall(r)
		mu.Lock()
		count++
		n := count
		mu.Unlock()
		if n == 1 {
			chatJSON(w, "<<TOOL:sapa>>dunia<</TOOL>>")
			return
		}
		chatJSON(w, "Selesai.")
	})
	writeCustomTool(t, f.app, "sapa.json", `{"name":"sapa","description":"Sapa seseorang","argHint":"nama","type":"shell","command":"echo halo-{{arg}}"}`)
	setSeatModel(t, f, "M01", "model-uji", nil)
	f.api("POST", "/api/message", object{"text": "@Plamo sapa dunia"}, 202)
	state := f.wait(func(s object) bool {
		pending, _ := s["pendingApprovals"].([]any)
		return len(pending) == 1
	})
	pending := state["pendingApprovals"].([]any)[0].(map[string]any)
	if pending["kind"] != "shell" {
		t.Fatalf("kind persetujuan: %v", pending)
	}
	f.api("POST", "/api/approvals/"+pending["id"].(string), object{"approved": true}, 200)
	final := f.wait(func(s object) bool {
		for _, item := range s["messages"].([]any) {
			m := item.(map[string]any)
			if m["seat"] == "M01" && m["status"] == "completed" {
				return true
			}
		}
		return false
	})
	for _, item := range final["messages"].([]any) {
		m := item.(map[string]any)
		if m["seat"] == "M01" {
			tools, _ := m["toolsUsed"].([]any)
			if len(tools) != 1 || tools[0] != "sapa" {
				t.Fatalf("toolsUsed kustom: %v", m["toolsUsed"])
			}
		}
	}
}

func TestIntegrationVoteFlow(t *testing.T) {
	f := setup(t, func(w http.ResponseWriter, r *http.Request) {
		call := decodeCall(r)
		if strings.Contains(call.System, "menilai jawaban") {
			if strings.Contains(call.User, "Kursimu adalah M01") {
				chatJSON(w, "M02 lebih baik.")
			} else {
				chatJSON(w, "M01 lebih baik.")
			}
			return
		}
		chatJSON(w, "jawaban "+call.Model)
	})
	setSeatModel(t, f, "M01", "model-a", nil)
	setSeatModel(t, f, "M02", "model-b", nil)
	f.api("POST", "/api/message", object{"text": "@Plamo @GLM diskusi"}, 202)
	f.wait(func(s object) bool {
		done := 0
		for _, item := range s["messages"].([]any) {
			m := item.(map[string]any)
			if (m["seat"] == "M01" || m["seat"] == "M02") && m["status"] == "completed" {
				done++
			}
		}
		return done == 2
	})
	f.api("POST", "/api/vote", object{}, 202)
	final := f.wait(func(s object) bool {
		for _, item := range s["messages"].([]any) {
			m := item.(map[string]any)
			if strings.Contains(m["text"].(string), "Hasil voting") {
				return true
			}
		}
		return false
	})
	best := 0
	for _, item := range final["messages"].([]any) {
		m := item.(map[string]any)
		if m["bestAnswer"] == true {
			best++
		}
	}
	if best != 2 {
		t.Fatalf("seri harus menandai 2 jawaban terbaik, dapat %d", best)
	}
}

func TestIntegrationRoomIsolation(t *testing.T) {
	f := setup(t, nil)
	setSeatModel(t, f, "M01", "plain", nil)
	f.api("POST", "/api/message", object{"text": "@Plamo pesan ruang utama"}, 202)
	f.wait(func(s object) bool {
		for _, item := range s["messages"].([]any) {
			m := item.(map[string]any)
			if m["seat"] == "M01" && m["status"] == "completed" {
				return true
			}
		}
		return false
	})
	room := f.api("POST", "/api/rooms", object{"name": "Ruang Uji"}, 201)
	roomID, _ := room["id"].(string)
	if roomID == "" {
		// Bentuk respons mungkin membungkus ruang; ambil dari state.
		state := f.state()
		for _, item := range state["rooms"].([]any) {
			r := item.(map[string]any)
			if r["name"] == "Ruang Uji" {
				roomID = r["id"].(string)
			}
		}
	}
	if roomID == "" {
		t.Fatal("ruang uji tidak ditemukan")
	}
	f.api("POST", "/api/rooms/"+roomID+"/activate", object{}, 200)
	state := f.state()
	if messages, _ := state["messages"].([]any); len(messages) != 0 {
		t.Fatalf("ruang baru harus kosong: %v", messages)
	}
	// Pencarian lintas ruang tetap menemukan pesan ruang utama.
	results := f.api("GET", "/api/search?q=pesan+ruang+utama", nil, 200)
	if list, _ := results["results"].([]any); len(list) == 0 {
		t.Fatalf("search lintas ruang kosong: %v", results)
	}
	// Kembali ke ruang utama: pesan lama utuh.
	f.api("POST", "/api/rooms/"+defaultRoomID+"/activate", object{}, 200)
	state = f.state()
	if messages, _ := state["messages"].([]any); len(messages) != 2 {
		t.Fatalf("ruang utama harus berisi 2 pesan: %v", messages)
	}
	// Hapus ruang uji bersih-bersih.
	f.api("DELETE", "/api/rooms/"+roomID, nil, 200)
	if _, err := os.Stat(filepath.Join(f.dir, "data.json")); err != nil {
		t.Fatal(err)
	}
}
