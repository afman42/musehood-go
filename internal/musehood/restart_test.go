package musehood

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestRestartResumesPendingSummary(t *testing.T) {
	var calls atomic.Int32
	f := setup(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path == "/v1/images/generations" {
			json.NewEncoder(w).Encode(object{"data": []object{{"b64_json": pixel}}})
			return
		}
		io.WriteString(w, "hasil tahap gateway")
	})
	f.api("POST", "/api/message", object{"text": "Tugas: Lanjutkan setelah restart"}, 202)
	request := f.pending("summary")
	id := request["id"].(string)
	if got := calls.Load(); got != 5 {
		t.Fatalf("wanted 5 gateway steps before Muse, got %d", got)
	}
	f.server.Close()
	f.app.Close()
	app, err := New(Options{DataDir: f.dir, PollInterval: 5 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(app.Close)
	server := httptest.NewServer(app)
	t.Cleanup(server.Close)
	f.server = server
	if q := f.pending("summary"); q["id"] != id {
		t.Fatalf("wanted the same file queue request, got %v", q)
	}
	response := filepath.Join(f.dir, "queue", "responses", id+".json")
	if err := os.WriteFile(response, []byte(`{"text":"Ringkasan setelah restart"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if m := f.finish(7)[6]; m["text"] != "Ringkasan setelah restart" || calls.Load() != 5 {
		t.Fatal(m, calls.Load())
	}
}

func TestRestartCompletesNodeJobSavedAfterLastStep(t *testing.T) {
	f := setup(t, nil)
	f.api("POST", "/api/message", object{"text": "@Muse Selesai di Node"}, 202)
	q := f.pending("chat")
	f.api("POST", "/api/queue/muse/"+q["id"].(string), object{"text": "hasil tersimpan"}, 200)
	f.finish(2)
	f.app.Close()

	// Node persists the last step separately from the job's completion status.
	d, err := loadData(f.dir)
	if err != nil {
		t.Fatal(err)
	}
	d.Jobs[0].Status, d.Jobs[0].CompletedAt = "processing", ""
	if err := writeJSONFile(filepath.Join(f.dir, "data.json"), d); err != nil {
		t.Fatal(err)
	}
	app, err := New(Options{DataDir: f.dir})
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	app.mu.Lock()
	defer app.mu.Unlock()
	j := app.data.Jobs[0]
	if j.Status != "completed" || j.CompletedAt == "" || len(app.data.Messages) != 2 {
		t.Fatalf("job not recovered: %+v", j)
	}
}

func TestReviewPromptUsesLatestSettings(t *testing.T) {
	seen := make(chan string, 4)
	f := setup(t, func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Model    string
			Messages []gatewayMessage
		}
		if json.NewDecoder(r.Body).Decode(&body) != nil || len(body.Messages) == 0 {
			http.Error(w, "missing messages", 400)
			return
		}
		seen <- body.Model + " " + body.Messages[0].Content
		io.WriteString(w, "selesai")
	})
	for i, v := range []struct{ level, instruction string }{
		{"Longgar", "Cek ringan saja"}, {"Santai", "Berikan masukan singkat"},
		{"Sedang", "Lakukan review seimbang"}, {"Ketat", "daftar perbaikan wajib"},
	} {
		f.api("POST", "/api/message", object{"text": "@Muse @GLM review"}, 202)
		q := f.pending("chat")
		f.api("POST", "/api/settings", object{
			"owner": object{"name": "Bima"}, "settings": object{"reviewLevel": v.level},
			"seats": []object{{"code": "M02", "model": "baru"}},
		}, 200)
		f.api("POST", "/api/queue/muse/"+q["id"].(string), object{"text": "hasil Muse"}, 200)
		f.finish((i + 1) * 3)
		select {
		case prompt := <-seen:
			if !strings.HasPrefix(prompt, "baru ") || !strings.Contains(prompt, "Nama pemilik: Bima.") || !strings.Contains(prompt, "Level Review "+v.level+":") || !strings.Contains(prompt, v.instruction) {
				t.Fatal(prompt)
			}
		default:
			t.Fatal("gateway did not see the review step")
		}
	}
}

func TestConnectionPreserves401Body(t *testing.T) {
	f := setup(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(401)
		io.WriteString(w, denied)
	})
	if out := f.api("POST", "/api/test-connection", object{}, 401); out["error"] != "HTTP 401: "+denied {
		t.Fatal(out)
	}
}
