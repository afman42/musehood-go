package musehood

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestReadWorkerReply(t *testing.T) {
	for _, tc := range []struct {
		name      string
		body      string
		missing   bool
		directory bool
		invalidID bool
		status    int
		readError bool
	}{
		{name: "invalid ID", invalidID: true, status: 400},
		{name: "missing reply", missing: true},
		{name: "partial JSON", body: `{"text":"still writing`},
		{name: "complete reply", body: `{"text":"done"}`},
		{name: "oversized reply", body: strings.Repeat(" ", 16*1024*1024+1), status: 413},
		{name: "unreadable reply", directory: true, readError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := &App{dir: t.TempDir()}
			id := uuid()
			if tc.invalidID {
				id = "../../outside"
			} else {
				file := a.queuePath("responses", id)
				if err := os.MkdirAll(filepath.Dir(file), 0700); err != nil {
					t.Fatal(err)
				}
				if tc.directory {
					if err := os.Mkdir(file, 0700); err != nil {
						t.Fatal(err)
					}
				} else if !tc.missing {
					if err := os.WriteFile(file, []byte(tc.body), 0600); err != nil {
						t.Fatal(err)
					}
				}
			}
			raw, err := a.readWorkerReply(id)
			if tc.status != 0 {
				var apiErr *apiError
				if !errors.As(err, &apiErr) || apiErr.status != tc.status || raw != nil {
					t.Fatalf("want HTTP %d with no reply; got %q, %v", tc.status, raw, err)
				}
				return
			}
			if tc.readError {
				if err == nil {
					t.Fatal("expected a filesystem read error")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if tc.name == "complete reply" {
				if string(raw) != tc.body {
					t.Fatalf("reply=%q, want %q", raw, tc.body)
				}
			} else if raw != nil {
				t.Fatalf("reply not ready: got %q, want nil", raw)
			}
			if tc.name == "partial JSON" {
				// A later poll must consume the completed file, not lose the reply.
				complete := tc.body + `"}`
				if err := os.WriteFile(a.queuePath("responses", id), []byte(complete), 0600); err != nil {
					t.Fatal(err)
				}
				raw, err := a.readWorkerReply(id)
				if err != nil || string(raw) != complete {
					t.Fatalf("completed reply=%q, error=%v", raw, err)
				}
			}
		})
	}
}

func TestValidateWorkerLimits(t *testing.T) {
	t.Parallel()
	atLimit := strings.Repeat("😀", 50000) // 100,000 UTF-16 code units.
	for _, tc := range []struct {
		name  string
		reply workerReply
		want  workerReply
		fail  bool
	}{
		{name: "text at UTF-16 limit", reply: workerReply{Text: atLimit}, want: workerReply{Text: atLimit}},
		{name: "text over UTF-16 limit", reply: workerReply{Text: atLimit + "x"}, fail: true},
		{name: "blank text", reply: workerReply{Text: " \n\t "}, fail: true},
		{name: "invalid image URL", reply: workerReply{ImageURL: "file:///outside.png"}, fail: true},
		{
			name:  "error truncated by rune and takes precedence",
			reply: workerReply{Error: strings.Repeat("😀", 30001), Text: atLimit + "x", ImageURL: "file:///outside.png"},
			want:  workerReply{Error: strings.Repeat("😀", 30000)},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := (&App{}).validateWorker(context.Background(), "chat", tc.reply)
			if tc.fail {
				var apiErr *apiError
				if !errors.As(err, &apiErr) || apiErr.status != 400 || !reflect.DeepEqual(got, workerReply{}) {
					t.Fatalf("want HTTP 400 and empty reply; error=%v", err)
				}
				return
			}
			if err != nil || !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("worker reply differs: text bytes=%d, error runes=%d, error=%v", len(got.Text), len([]rune(got.Error)), err)
			}
		})
	}
}
