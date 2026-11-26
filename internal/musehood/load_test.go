package musehood

// Tes performa / beban / stres: konkurensi endpoint state, pesan konkuren,
// state besar, dan throughput sandbox. Dilewati pada mode -short.

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestLoadStateEndpoint(t *testing.T) {
	if testing.Short() {
		t.Skip("tes beban dilewati pada -short")
	}
	f := setup(t, nil)
	const total = 300
	start := time.Now()
	var wg sync.WaitGroup
	failures := make(chan string, total)
	for i := 0; i < total; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res, err := http.Get(f.server.URL + "/api/state")
			if err != nil {
				failures <- err.Error()
				return
			}
			res.Body.Close()
			if res.StatusCode != 200 {
				failures <- fmt.Sprintf("status %d", res.StatusCode)
			}
		}()
	}
	wg.Wait()
	close(failures)
	for failure := range failures {
		t.Fatalf("request gagal: %s", failure)
	}
	elapsed := time.Since(start)
	t.Logf("%d request /api/state konkuren selesai dalam %v (%.0f req/detik)", total, elapsed, float64(total)/elapsed.Seconds())
	if elapsed > 15*time.Second {
		t.Fatalf("terlalu lambat: %v", elapsed)
	}
}

func TestLoadConcurrentMessages(t *testing.T) {
	if testing.Short() {
		t.Skip("tes beban dilewati pada -short")
	}
	f := setup(t, nil)
	setSeatModel(t, f, "M01", "plain", nil)
	const total = 24
	var wg sync.WaitGroup
	for i := 0; i < total; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			status, _ := rawRequest(t, f, "POST", "/api/message", mustJSON(object{"text": fmt.Sprintf("@Plamo pesan %d", n)}), nil)
			if status != 202 {
				t.Errorf("pesan %d: status %d", n, status)
			}
		}(i)
	}
	wg.Wait()
	// Semua balasan kursi selesai tanpa error.
	deadline := time.Now().Add(20 * time.Second)
	for {
		state := f.state()
		completed, errored := 0, 0
		for _, item := range state["messages"].([]any) {
			m := item.(map[string]any)
			if m["seat"] == "M01" {
				switch m["status"] {
				case "completed":
					completed++
				case "error":
					errored++
				}
			}
		}
		if errored > 0 {
			t.Fatalf("%d balasan error dari %d pesan konkuren", errored, total)
		}
		if completed == total {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("hanya %d/%d balasan selesai", completed, total)
		}
		time.Sleep(25 * time.Millisecond)
	}
	// Data tetap sah setelah beban.
	if _, err := loadData(f.dir); err != nil {
		t.Fatalf("data rusak setelah beban pesan: %v", err)
	}
}

func TestStressLargeState(t *testing.T) {
	if testing.Short() {
		t.Skip("tes stres dilewati pada -short")
	}
	f := setup(t, nil)
	f.app.mu.Lock()
	for i := 0; i < 3000; i++ {
		f.app.data.Messages = append(f.app.data.Messages, &message{
			ID: fmt.Sprintf("m%d", i), AuthorType: "owner",
			Text:   fmt.Sprintf("pesan stres nomor %d dengan isi yang cukup panjang untuk diuji", i),
			Status: "completed", RoomID: defaultRoomID, CreatedAt: now(),
		})
	}
	if err := f.app.saveLocked(); err != nil {
		f.app.mu.Unlock()
		t.Fatal(err)
	}
	f.app.mu.Unlock()

	start := time.Now()
	status, raw := rawRequest(t, f, "GET", "/api/state", nil, nil)
	stateElapsed := time.Since(start)
	if status != 200 {
		t.Fatalf("state besar: %d", status)
	}
	t.Logf("state 3000 pesan: %d byte dalam %v", len(raw), stateElapsed)
	if stateElapsed > 5*time.Second {
		t.Fatalf("state besar terlalu lambat: %v", stateElapsed)
	}

	start = time.Now()
	status, _ = rawRequest(t, f, "GET", "/api/export?format=json", nil, nil)
	exportElapsed := time.Since(start)
	if status != 200 || exportElapsed > 5*time.Second {
		t.Fatalf("export besar: status %d dalam %v", status, exportElapsed)
	}

	start = time.Now()
	results := f.api("GET", "/api/search?q=stres", nil, 200)
	searchElapsed := time.Since(start)
	if list, _ := results["results"].([]any); len(list) == 0 {
		t.Fatal("search pada state besar kosong")
	}
	t.Logf("search 3000 pesan dalam %v", searchElapsed)
	if searchElapsed > 5*time.Second {
		t.Fatalf("search terlalu lambat: %v", searchElapsed)
	}
}

func TestLoadRunCodeThroughput(t *testing.T) {
	if testing.Short() {
		t.Skip("tes beban dilewati pada -short")
	}
	app := &App{dir: t.TempDir(), approvals: map[string]*approval{}}
	ctx := context.Background()
	const total = 20
	start := time.Now()
	for i := 0; i < total; i++ {
		result, _ := app.runTool(ctx, settings{}, seat{Code: "M01"}, "run_code", "python\nprint(6*7)", true)
		if !strings.Contains(result, "42") {
			t.Fatalf("run_code %d: %s", i, result)
		}
	}
	elapsed := time.Since(start)
	t.Logf("%d run_code selesai dalam %v (%.0f ms/eksekusi)", total, elapsed, float64(elapsed.Milliseconds())/total)
	if elapsed > 30*time.Second {
		t.Fatalf("run_code terlalu lambat: %v", elapsed)
	}
}

func TestLoadConcurrentToolWrites(t *testing.T) {
	if testing.Short() {
		t.Skip("tes beban dilewati pada -short")
	}
	app := &App{dir: t.TempDir(), approvals: map[string]*approval{}}
	ctx := context.Background()
	const total = 30
	var wg sync.WaitGroup
	for i := 0; i < total; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			result, _ := app.runTool(ctx, settings{}, seat{Code: "M01"}, "write_file", fmt.Sprintf("file-%02d.txt\nisi %d", n, n), false)
			if strings.Contains(result, "Error") {
				t.Errorf("write %d: %s", n, result)
			}
		}(i)
	}
	wg.Wait()
	entries, err := os.ReadDir(app.sandboxDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != total {
		t.Fatalf("file hasil = %d, mau %d", len(entries), total)
	}
	content, err := os.ReadFile(filepath.Join(app.sandboxDir(), "file-07.txt"))
	if err != nil || string(content) != "isi 7" {
		t.Fatalf("isi file-07: %q %v", content, err)
	}
}
