//go:build integration

package musehood

import (
	"bytes"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCloseCancelsInFlightGateway(t *testing.T) {
	started, canceled, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	f := setup(t, func(w http.ResponseWriter, r *http.Request) {
		// Drain the request so net/http can watch the connection for cancellation.
		if _, err := io.Copy(io.Discard, r.Body); err != nil {
			http.Error(w, "cannot read gateway request", http.StatusBadRequest)
			return
		}
		close(started)
		select {
		case <-r.Context().Done():
			close(canceled)
		case <-release:
		}
	})
	// Release the handler even if a cancellation assertion fails, so fixture
	// cleanup can always close its server.
	t.Cleanup(func() { close(release) })
	f.api("POST", "/api/message", object{"text": "@Opus wait for shutdown"}, 202)
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("gateway request did not start")
	}
	file := filepath.Join(f.dir, "data.json")
	before, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	closed := make(chan struct{})
	go func() {
		f.app.Close()
		close(closed)
	}()
	select {
	case <-closed:
	case <-time.After(3 * time.Second):
		t.Fatal("Close did not join the application's background goroutines")
	}
	select {
	case <-canceled:
	case <-time.After(3 * time.Second):
		t.Fatal("shutdown did not cancel the gateway request")
	}
	f.api("GET", "/api/state", nil, http.StatusServiceUnavailable)
	after, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("canceled request changed persisted job state during shutdown")
	}
}
