package musehood

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMessageActionsPreserveStateWhenSaveFails(t *testing.T) {
	for _, tc := range []struct {
		name, action, body string
		reactions          []reaction
		lastTaskMessage    bool
	}{
		{name: "edit", action: "edit", body: `{"text":"changed"}`},
		{name: "delete middle", action: "delete", body: `{}`},
		{name: "delete last task message", action: "delete", body: `{}`, lastTaskMessage: true},
		{name: "add reaction", action: "react", body: `{"emoji":"🔥"}`, reactions: []reaction{{Emoji: "👍", Count: 1, ByOwner: true}}},
		{name: "remove reaction", action: "react", body: `{"emoji":"👍"}`, reactions: []reaction{{Emoji: "👍", Count: 1, ByOwner: true}, {Emoji: "❤️", Count: 1, ByOwner: true}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			d, err := loadData(dir)
			if err != nil {
				t.Fatal(err)
			}
			msg := &message{ID: uuid(), AuthorType: "owner", JobID: "task", Text: "original", EditedAt: "original timestamp", Status: "completed", Reactions: tc.reactions, RoomID: d.Rooms[0].ID}
			d.Messages = []*message{
				{ID: uuid(), JobID: "other", Text: "first", Status: "completed"},
				msg,
				{ID: uuid(), JobID: "other", Text: "last", Status: "completed"},
			}
			if tc.lastTaskMessage {
				d.Messages = []*message{msg}
			}
			d.Task = &activeTask{JobID: "task", Title: "keep task"}
			a := &App{dir: dir, data: d, hosts: map[string]bool{"localhost": true}}
			if err := a.saveLocked(); err != nil {
				t.Fatal(err)
			}
			before, err := json.Marshal(a.data)
			if err != nil {
				t.Fatal(err)
			}
			// A directory at data.json makes the atomic rename fail even as root.
			file := filepath.Join(dir, "data.json")
			backup := filepath.Join(dir, "saved.json")
			if err := os.Rename(file, backup); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(file, 0700); err != nil {
				t.Fatal(err)
			}
			r := httptest.NewRequest(http.MethodPost, "http://localhost/api/message/"+msg.ID+"/"+tc.action, strings.NewReader(tc.body))
			w := httptest.NewRecorder()
			a.ServeHTTP(w, r)
			if w.Code != http.StatusInternalServerError {
				t.Fatalf("wanted 500, got %d: %s", w.Code, w.Body.String())
			}
			after, err := json.Marshal(a.data)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(before, after) {
				t.Errorf("failed %s changed live state", tc.action)
			}
			if err := os.Remove(file); err != nil {
				t.Fatal(err)
			}
			if err := os.Rename(backup, file); err != nil {
				t.Fatal(err)
			}
			// A later successful save must not persist the rejected operation.
			if err := a.saveLocked(); err != nil {
				t.Fatal(err)
			}
			saved, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			var compact bytes.Buffer
			if err := json.Compact(&compact, saved); err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(before, compact.Bytes()) {
				t.Error("a later save persisted the rejected operation")
			}
		})
	}
}

func TestRoutingLoadedSeatCodeDoesNotPanic(t *testing.T) {
	dir := t.TempDir()
	d, err := loadData(dir)
	if err != nil {
		t.Fatal(err)
	}
	d.Seats = append(d.Seats, seat{identity: identity{Name: "Custom"}, Code: "C("})
	if err := writeJSONFile(filepath.Join(dir, "data.json"), d); err != nil {
		t.Fatal(err)
	}
	d, err = loadData(dir)
	if err != nil {
		t.Fatal(err)
	}
	a := &App{data: d}
	_, steps := a.routeLocked("@Custom hello", nil)
	if len(steps) != 1 || steps[0].Seat != "C(" {
		t.Fatalf("unexpected routing: %+v", steps)
	}
}

type errorAuditTransport func(*http.Request) (*http.Response, error)

func (f errorAuditTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestGeminiConnectionErrorRedactsEncodedKey(t *testing.T) {
	a := &App{client: &http.Client{Transport: errorAuditTransport(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("connection refused")
	})}}
	a.rememberKeyLocked(secret)
	_, err := a.protocolChat(context.Background(), settings{BaseURL: "https://gateway.invalid", APIKey: secret, GatewayProtocol: "gemini"}, "test", nil)
	if err == nil {
		t.Fatal("wanted a connection error")
	}
	w := httptest.NewRecorder()
	a.writeJSON(w, 0, nil, err)
	if w.Code != http.StatusBadGateway {
		t.Fatalf("wanted 502, got %d", w.Code)
	}
	body := w.Body.String()
	if strings.Contains(body, url.QueryEscape(secret)) || strings.Contains(body, secret) {
		t.Fatal("connection error exposed the API key")
	}
	if !strings.Contains(body, "[RAHASIA DISEMBUNYIKAN]") {
		t.Fatal("connection error did not redact the encoded key")
	}
}

func TestResponsesStreamPropagatesTerminalErrors(t *testing.T) {
	for _, terminal := range []string{
		`{"type":"response.failed","response":{"status":"failed","error":{"code":"server_error","message":"backend failed"}}}`,
		`{"type":"error","code":"server_error","message":"backend failed"}`,
	} {
		t.Run(terminal, func(t *testing.T) {
			body := "data: {\"type\":\"response.output_text.delta\",\"delta\":\"partial answer\"}\n\ndata: " + terminal + "\n\n"
			text, err := parseResponsesText(body)
			if err == nil || !strings.Contains(err.Error(), "backend failed") || text != "" {
				t.Fatalf("failed stream returned text=%q, error=%v", text, err)
			}
		})
	}
}
