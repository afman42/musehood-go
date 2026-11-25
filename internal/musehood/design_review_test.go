package musehood

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func newDesignReviewApp(t *testing.T) *App {
	t.Helper()
	a, err := New(Options{DataDir: t.TempDir(), PollInterval: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(a.Close)
	return a
}

func TestExportRedactsStoredSecrets(t *testing.T) {
	a := newDesignReviewApp(t)
	const oldSecret = "previous-export-secret"
	a.mu.Lock()
	a.rememberKeyLocked(oldSecret)
	a.rememberKeyLocked(secret)
	// Existing history can contain credentials recorded before they were known
	// to the redactor. Export must apply the same protection as GET /api/state.
	a.data.Settings.APIKey = secret
	a.data.Group.Name = "group " + oldSecret
	a.data.Messages = []*message{{
		ID: uuid(), CreatedAt: now(), AuthorType: "owner", Status: "completed",
		Text:       "stored " + secret + " and " + oldSecret,
		Attachment: &attachment{URL: "/uploads/example.txt", Name: "example.txt", Size: 1, Mime: secret},
	}}
	a.mu.Unlock()

	for _, format := range []string{"json", "md"} {
		t.Run(format, func(t *testing.T) {
			w := httptest.NewRecorder()
			a.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "http://localhost/api/export?format="+format, nil))
			if w.Code != http.StatusOK {
				t.Fatalf("export status = %d", w.Code)
			}
			for _, key := range []string{secret, oldSecret} {
				escaped, err := json.Marshal(key)
				if err != nil {
					t.Fatal(err)
				}
				if bytes.Contains(w.Body.Bytes(), []byte(key)) || bytes.Contains(w.Body.Bytes(), escaped[1:len(escaped)-1]) {
					t.Fatal("export leaked a remembered credential")
				}
			}
			if !strings.Contains(w.Body.String(), "[RAHASIA DISEMBUNYIKAN]") {
				t.Fatal("export is missing redacted history")
			}
			if format == "json" {
				var out struct {
					Settings map[string]json.RawMessage `json:"settings"`
				}
				if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
					t.Fatal(err)
				}
				if _, exists := out.Settings["apiKey"]; exists {
					t.Fatal("export must omit the API key field")
				}
			}
		})
	}
	// Export must redact its snapshot without rewriting stored history.
	a.mu.Lock()
	defer a.mu.Unlock()
	if !strings.Contains(a.data.Messages[0].Text, secret) {
		t.Fatal("export modified application state")
	}
}

func TestExportConcurrentMessageUpdates(t *testing.T) {
	a := newDesignReviewApp(t)
	a.mu.Lock()
	for range 64 {
		a.data.Messages = append(a.data.Messages, &message{
			ID: uuid(), CreatedAt: now(), AuthorType: "owner", Status: "completed", Text: "initial",
		})
	}
	a.data.Task = &activeTask{JobID: uuid(), Title: "concurrent export", Status: "processing"}
	a.mu.Unlock()

	stop, done, started := make(chan struct{}), make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		close(started)
		for n := 0; ; n++ {
			select {
			case <-stop:
				return
			default:
			}
			text := "even"
			if n%2 != 0 {
				text = "odd"
			}
			a.mu.Lock()
			for _, m := range a.data.Messages {
				m.Text = text
				m.EditedAt = now()
				m.Reactions = []reaction{{Emoji: "👍", Count: 1, ByOwner: true}}
			}
			a.data.Task.Status = text
			a.mu.Unlock()
			runtime.Gosched()
		}
	}()
	defer func() {
		close(stop)
		<-done
	}()
	<-started

	for n := range 100 {
		format := "json"
		if n%2 != 0 {
			format = "md"
		}
		w := httptest.NewRecorder()
		a.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "http://localhost/api/export?format="+format, nil))
		if w.Code != http.StatusOK {
			t.Fatalf("export status = %d", w.Code)
		}
		if format == "json" {
			var out struct {
				Messages []message `json:"messages"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
				t.Fatal(err)
			}
			if len(out.Messages) != 64 {
				t.Fatalf("snapshot has %d messages", len(out.Messages))
			}
			for _, m := range out.Messages {
				if m.Text != out.Messages[0].Text {
					t.Fatal("export mixed messages from different state snapshots")
				}
			}
		}
	}
}

func TestDeleteSeatRejectsUnfinishedJobs(t *testing.T) {
	for _, status := range []string{"processing", "waiting"} {
		t.Run(status, func(t *testing.T) {
			a := newDesignReviewApp(t)
			a.mu.Lock()
			defer a.mu.Unlock()
			s, err := a.createSeatLocked("Audit", "Test", "")
			if err != nil {
				t.Fatal(err)
			}
			code := s.Code
			a.data.Jobs = append(a.data.Jobs, &job{
				ID: uuid(), Status: status,
				Steps: []step{{Seat: "MUSE", Status: "running"}, {Seat: code, Status: "pending"}},
			})
			if err := a.saveLocked(); err != nil {
				t.Fatal(err)
			}
			err = a.deleteSeatLocked(code)
			var apiErr *apiError
			if !errors.As(err, &apiErr) || apiErr.status != http.StatusConflict {
				t.Fatalf("deleting a seat needed by a %s job: want 409, got %v", status, err)
			}
			if a.data.findSeat(code) == nil {
				t.Fatal("rejected deletion removed the seat")
			}
			if _, err := loadData(a.dir); err != nil {
				t.Fatalf("rejected deletion damaged persisted state: %v", err)
			}
		})
	}
}

func TestRestartAfterDeletingCompletedJobSeat(t *testing.T) {
	a := newDesignReviewApp(t)
	a.mu.Lock()
	defer a.mu.Unlock()
	s, err := a.createSeatLocked("Audit", "Test", "")
	if err != nil {
		t.Fatal(err)
	}
	code := s.Code
	a.data.Jobs = append(a.data.Jobs, &job{
		ID: uuid(), Status: "completed", Index: 1,
		Steps: []step{{Seat: code, Status: "completed"}},
	})
	if err := a.saveLocked(); err != nil {
		t.Fatal(err)
	}
	if err := a.deleteSeatLocked(code); err != nil {
		t.Fatal(err)
	}
	d, err := loadData(a.dir)
	if err != nil {
		t.Fatalf("completed job history prevents restart after seat deletion: %v", err)
	}
	if d.findSeat(code) != nil || len(d.Jobs) != 1 || d.Jobs[0].Steps[0].Seat != code {
		t.Fatal("seat deletion must preserve completed job history")
	}

	// A missing seat is still invalid when the job can execute that step.
	d.Jobs[0].Status = "processing"
	d.Jobs[0].Index = 0
	if err := writeJSONFile(filepath.Join(a.dir, "data.json"), d); err != nil {
		t.Fatal(err)
	}
	if _, err := loadData(a.dir); err == nil {
		t.Fatal("unfinished job with a missing seat must still fail validation")
	}
}
