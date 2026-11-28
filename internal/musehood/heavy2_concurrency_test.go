package musehood

// Tes berat ronde 2 — konkurensi tool omp (28 Nov 2026): eval paralel
// per (ruang+kursi+bahasa) tanpa kebocoran state, hub/todo paralel,
// checkpoint/rewind saat job berjalan, spawn_subagent paralel dengan
// kedalaman maks 1, dan tab CDP paralel.

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// heavy2Seat membuat kursi uji dengan kode/nama tertentu.
func heavy2Seat(code, name string) seat {
	model := "plain"
	return seat{Code: code, Name: name, Model: &model}
}

// heavy2Ctx membuat konteks tool untuk satu ruang.
func heavy2Ctx(roomID string) context.Context {
	return context.WithValue(context.Background(), toolRunInfoKey{}, toolRunInfo{RoomID: roomID})
}

// heavy2Run menjalankan tool lewat dispatch penuh untuk kursi tertentu.
func heavy2Run(f fixture, ctx context.Context, s seat, name, arg string) string {
	out, _ := f.app.runTool(ctx, toolCfg(f), s, name, arg, true)
	return out
}

// heavy2StopAllEval menghentikan semua sesi eval global (isolasi antar tes).
func heavy2StopAllEval() {
	evalMu.Lock()
	defer evalMu.Unlock()
	for key, session := range evalSessions {
		session.stop()
		delete(evalSessions, key)
	}
}

// TestHeavy2EvalParallelIsolation: 8 kernel (2 ruang × 2 kursi × 2 bahasa)
// dipakai bersamaan; state tidak boleh bocor/tertukar antar sesi, dan
// panggilan bersamaan ke sesi yang sama terserialisasi dengan benar.
func TestHeavy2EvalParallelIsolation(t *testing.T) {
	f := setup(t, nil)
	heavy2StopAllEval()
	defer heavy2StopAllEval()

	type combo struct {
		room, seatCode, seatName, lang string
		value                          int
	}
	combos := []combo{
		{"heavy2-eval-a", "M01", "Plamo", "python", 11},
		{"heavy2-eval-a", "M02", "GLM", "python", 22},
		{"heavy2-eval-b", "M01", "Plamo", "python", 33},
		{"heavy2-eval-b", "M02", "GLM", "python", 44},
		{"heavy2-eval-a", "M01", "Plamo", "javascript", 55},
		{"heavy2-eval-a", "M02", "GLM", "javascript", 66},
		{"heavy2-eval-b", "M01", "Plamo", "javascript", 77},
		{"heavy2-eval-b", "M02", "GLM", "javascript", 88},
	}
	setCode := func(c combo) string {
		if c.lang == "python" {
			return fmt.Sprintf("python\nnilai = %d", c.value)
		}
		return fmt.Sprintf("javascript\nglobalThis.nilai = %d", c.value)
	}
	readCode := func(c combo) string {
		if c.lang == "python" {
			return "python\nprint(nilai * 10)"
		}
		return "javascript\nglobalThis.nilai * 10"
	}

	// Isi state tiap sesi secara paralel.
	var wg sync.WaitGroup
	errs := make(chan error, 256)
	for _, c := range combos {
		wg.Add(1)
		go func(c combo) {
			defer wg.Done()
			out := heavy2Run(f, heavy2Ctx(c.room), heavy2Seat(c.seatCode, c.seatName), "eval", setCode(c))
			if strings.Contains(out, "[error]") || strings.Contains(out, "Error") {
				errs <- fmt.Errorf("set %s/%s/%s = %q", c.room, c.seatCode, c.lang, out)
			}
		}(c)
	}
	wg.Wait()

	// Prune berjalan bersamaan dengan panggilan (jalur lastUsed).
	stopPrune := make(chan struct{})
	var pruneWG sync.WaitGroup
	pruneWG.Add(1)
	go func() {
		defer pruneWG.Done()
		for {
			select {
			case <-stopPrune:
				return
			default:
				pruneEvalSessions()
			}
		}
	}()

	// Baca state tiap sesi 6x secara paralel; nilai harus milik sesi itu.
	for _, c := range combos {
		for i := 0; i < 6; i++ {
			wg.Add(1)
			go func(c combo) {
				defer wg.Done()
				out := heavy2Run(f, heavy2Ctx(c.room), heavy2Seat(c.seatCode, c.seatName), "eval", readCode(c))
				want := fmt.Sprint(c.value * 10)
				if !strings.Contains(out, want) {
					errs <- fmt.Errorf("read %s/%s/%s = %q, mau mengandung %s", c.room, c.seatCode, c.lang, out, want)
				}
			}(c)
		}
	}
	// Serialisasi sesi yang sama: 24 append bersamaan ke satu kernel Python.
	ctxA := heavy2Ctx("heavy2-eval-a")
	seatA := heavy2Seat("M01", "Plamo")
	if out := heavy2Run(f, ctxA, seatA, "eval", "python\ndaftar = []"); strings.Contains(out, "[error]") {
		t.Fatalf("eval daftar = %q", out)
	}
	for i := 0; i < 24; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			out := heavy2Run(f, ctxA, seatA, "eval", "python\ndaftar.append(1)")
			if strings.Contains(out, "[error]") {
				errs <- fmt.Errorf("append bersamaan = %q", out)
			}
		}()
	}
	wg.Wait()
	close(stopPrune)
	pruneWG.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	if out := heavy2Run(f, ctxA, seatA, "eval", "python\nprint(len(daftar))"); !strings.Contains(out, "24") {
		t.Fatalf("len(daftar) sesudah 24 append bersamaan = %q, mau 24", out)
	}
	// Batas sesi: tepat 8 sesi hidup, tidak lebih.
	evalMu.Lock()
	live := len(evalSessions)
	evalMu.Unlock()
	if live != len(combos) {
		t.Fatalf("sesi eval hidup = %d, mau %d", live, len(combos))
	}
}

// TestHeavy2HubTodoParallel: hub_message + todo_add/update bersamaan
// tidak kehilangan pesan/item dan tidak menduplikasi id.
func TestHeavy2HubTodoParallel(t *testing.T) {
	f := setup(t, nil)
	ctx := ompRoomCtx()

	const hubN = 30
	const todoN = 40
	var wg sync.WaitGroup
	errs := make(chan error, hubN+todoN)
	for i := 0; i < hubN; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			out := heavy2Run(f, ctx, heavy2Seat("M01", "Plamo"), "hub_message", fmt.Sprintf("M02\npesan hub nomor %d", i))
			if !strings.Contains(out, "terkirim") {
				errs <- fmt.Errorf("hub %d = %q", i, out)
			}
		}(i)
	}
	for i := 0; i < todoN; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			out := heavy2Run(f, ctx, heavy2Seat("M02", "GLM"), "todo_add", fmt.Sprintf("item berat %d", i))
			if !strings.Contains(out, "ditambahkan") {
				errs <- fmt.Errorf("todo_add %d = %q", i, out)
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}

	f.app.mu.Lock()
	room := f.app.data.findRoom(defaultRoomID)
	hubCount := 0
	seenHub := map[string]int{}
	for _, m := range f.app.data.Messages {
		if m.RoomID == defaultRoomID && strings.HasPrefix(m.Text, "[hub →") {
			hubCount++
			seenHub[m.Text]++
		}
	}
	todos := append([]roomTodo(nil), room.Todos...)
	f.app.mu.Unlock()
	if hubCount != hubN {
		t.Fatalf("pesan hub tersimpan = %d, mau %d", hubCount, hubN)
	}
	for text, n := range seenHub {
		if n != 1 {
			t.Fatalf("pesan hub %q muncul %d kali", text, n)
		}
	}
	if len(todos) != todoN {
		t.Fatalf("todo tersimpan = %d, mau %d", len(todos), todoN)
	}
	ids := map[string]bool{}
	for _, todo := range todos {
		if ids[todo.ID] {
			t.Fatalf("id todo duplikat: %s", todo.ID)
		}
		ids[todo.ID] = true
	}

	// Update semua todo bersamaan (bergantian done/active).
	errs = make(chan error, todoN)
	for i, todo := range todos {
		wg.Add(1)
		go func(i int, id string) {
			defer wg.Done()
			status := "done"
			if i%2 == 1 {
				status = "active"
			}
			out := heavy2Run(f, ctx, heavy2Seat("M01", "Plamo"), "todo_update", id+"\n"+status)
			if !strings.Contains(out, status) {
				errs <- fmt.Errorf("todo_update %s = %q", id, out)
			}
		}(i, todo.ID)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	f.app.mu.Lock()
	done, active := 0, 0
	for _, todo := range f.app.data.findRoom(defaultRoomID).Todos {
		switch todo.Status {
		case "done":
			done++
		case "active":
			active++
		}
	}
	f.app.mu.Unlock()
	if done != todoN/2 || active != todoN/2 {
		t.Fatalf("status todo done=%d active=%d, mau %d/%d", done, active, todoN/2, todoN/2)
	}
}

// TestHeavy2CheckpointRewindWhileJobRuns: rewind lewat tool saat job
// masih berjalan (gateway menggantung) memotong pesan sesudah checkpoint
// tanpa deadlock; job tetap selesai sesudahnya.
func TestHeavy2CheckpointRewindWhileJobRuns(t *testing.T) {
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	gateway := func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" {
			io.WriteString(w, `{"data":[{"id":"plain"},{"id":"array"}]}`)
			return
		}
		if r.URL.Path == "/v1/chat/completions" {
			select {
			case started <- struct{}{}:
			default:
			}
			<-release
			io.WriteString(w, `{"choices":[{"message":{"content":"balasan lambat selesai"}}]}`)
			return
		}
		http.NotFound(w, r)
	}
	f := setup(t, gateway)
	ctx := ompRoomCtx()

	f.app.mu.Lock()
	f.app.data.Messages = append(f.app.data.Messages, &message{
		ID: uuid(), CreatedAt: now(), AuthorType: "owner", Text: "pesan awal",
		Status: "completed", RoomID: defaultRoomID, BranchID: "main",
	})
	f.app.mu.Unlock()
	if out := ompRun(t, f, ctx, "checkpoint", "sebelum-job"); !strings.Contains(out, "Checkpoint dibuat") {
		t.Fatalf("checkpoint = %q", out)
	}

	// Mulai job yang menggantung di gateway.
	f.api("POST", "/api/message", object{"text": "@Plamo tugas lambat"}, 202)
	select {
	case <-started:
	case <-time.After(10 * time.Second):
		t.Fatal("job tidak mulai")
	}
	// Aktivitas lain saat job berjalan.
	for i := 0; i < 5; i++ {
		heavy2Run(f, ctx, heavy2Seat("M01", "Plamo"), "hub_message", fmt.Sprintf("M02\ncatatan saat job jalan %d", i))
		heavy2Run(f, ctx, heavy2Seat("M01", "Plamo"), "todo_add", fmt.Sprintf("tugas saat job jalan %d", i))
	}

	// Rewind lewat tool (butuh persetujuan) saat job masih berjalan.
	done := make(chan string, 1)
	go func() { done <- ompRun(t, f, ctx, "rewind", "sebelum-job") }()
	ompApproveNext(t, f, "edit")
	select {
	case out := <-done:
		if !strings.Contains(out, "dipotong") {
			t.Fatalf("rewind saat job jalan = %q", out)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("rewind deadlock saat job berjalan")
	}

	// Pesan sesudah checkpoint (pesan pemilik job + hub) sudah terpotong;
	// todo (bukan pesan) tetap utuh.
	f.app.mu.Lock()
	msgCount := 0
	for _, m := range f.app.data.Messages {
		if m.RoomID == defaultRoomID {
			msgCount++
		}
	}
	todoCount := len(f.app.data.findRoom(defaultRoomID).Todos)
	f.app.mu.Unlock()
	if msgCount != 1 {
		t.Fatalf("pesan ruang sesudah rewind = %d, mau 1 (pesan awal)", msgCount)
	}
	if todoCount != 5 {
		t.Fatalf("todo sesudah rewind = %d, mau 5", todoCount)
	}

	// Lepas gateway: job selesai tanpa panic/deadlock.
	close(release)
	f.wait(func(s object) bool {
		for _, m := range messages(s) {
			if strings.Contains(fmt.Sprint(m["text"]), "balasan lambat selesai") {
				return true
			}
		}
		// Balasan job yang pesannya terpotong rewind boleh tidak tampil,
		// tetapi job harus berstatus selesai.
		jobs, _ := s["jobs"].([]any)
		for _, item := range jobs {
			job, _ := item.(map[string]any)
			if job["status"] == "completed" {
				return true
			}
		}
		return false
	})
}

// TestHeavy2SpawnSubagentParallelDepth: spawn bersamaan membuat ruang
// sub unik; spawn dari dalam ruang sub selalu ditolak (kedalaman maks 1).
func TestHeavy2SpawnSubagentParallelDepth(t *testing.T) {
	f := setup(t, nil)
	ctx := ompRoomCtx()

	const n = 8
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			seatCode := "M01"
			if i%2 == 1 {
				seatCode = "M02"
			}
			out := heavy2Run(f, ctx, heavy2Seat("M01", "Plamo"), "spawn_subagent",
				fmt.Sprintf("seat:%s\nJawab singkat: subagent nomor %d", seatCode, i))
			if !strings.Contains(out, "Sesi subagent dimulai") {
				errs <- fmt.Errorf("spawn %d = %q", i, out)
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}

	var subIDs []string
	f.app.mu.Lock()
	for _, r := range f.app.data.Rooms {
		if r.SubagentOf == defaultRoomID {
			subIDs = append(subIDs, r.ID)
		}
	}
	f.app.mu.Unlock()
	if len(subIDs) != n {
		t.Fatalf("ruang sub = %d, mau %d", len(subIDs), n)
	}
	uniq := map[string]bool{}
	for _, id := range subIDs {
		if uniq[id] {
			t.Fatalf("id ruang sub duplikat: %s", id)
		}
		uniq[id] = true
	}

	// Spawn dari dalam ruang sub: selalu ditolak, tidak ada ruang baru.
	subCtx := heavy2Ctx(subIDs[0])
	errs = make(chan error, 4)
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			out := heavy2Run(f, subCtx, heavy2Seat("M01", "Plamo"), "spawn_subagent", "seat:M01\nSpawn cucu")
			if !strings.Contains(out, "kedalaman maksimum 1") {
				errs <- fmt.Errorf("spawn cucu %d = %q", i, out)
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	f.app.mu.Lock()
	subCount := 0
	for _, r := range f.app.data.Rooms {
		if r.SubagentOf != "" {
			subCount++
		}
	}
	f.app.mu.Unlock()
	if subCount != n {
		t.Fatalf("ruang sub sesudah spawn cucu = %d, mau tetap %d", subCount, n)
	}
}

// TestHeavy2BrowserCDPParallelTabs: banyak tab dibuka + dievaluasi +
// didaftar bersamaan tanpa tab tertukar atau crash.
func TestHeavy2BrowserCDPParallelTabs(t *testing.T) {
	if findChromium() == "" {
		t.Skip("Chromium tidak tersedia")
	}
	f := setup(t, nil)
	ctx := ompRoomCtx()
	page := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, `<html><head><title>Halaman Berat %s</title></head><body><h1>Judul</h1><input id="nama"></body></html>`, r.URL.Query().Get("n"))
	}))
	defer page.Close()

	browser, err := startCDPBrowser()
	if err != nil {
		t.Skip("CDP tidak bisa dimulai: " + err.Error())
	}
	defer stopCDPBrowser()

	const tabs = 6
	targets := make([]cdpTarget, tabs)
	var wg sync.WaitGroup
	errs := make(chan error, tabs*3)
	for i := 0; i < tabs; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			target, err := browser.tabForURL(fmt.Sprintf("%s?n=%d", page.URL, i))
			if err != nil {
				errs <- fmt.Errorf("tabForURL %d: %w", i, err)
				return
			}
			targets[i] = target
		}(i)
	}
	wg.Wait()

	// Evaluasi tiap tab bersamaan: isi input dengan nilai unik per tab.
	for i := 0; i < tabs; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			expr := fmt.Sprintf(`(() => { const el = document.querySelector("#nama"); el.value = "tab-%d"; return el.value + "|" + document.title; })()`, i)
			value, err := browser.evaluate(targets[i], "", expr)
			if err != nil {
				// Satu coba ulang: target bisa dibuat ulang saat navigasi.
				value, err = browser.evaluate(targets[i], "", expr)
			}
			if err != nil {
				errs <- fmt.Errorf("evaluate tab %d: %w", i, err)
				return
			}
			text, _ := value.(string)
			if !strings.Contains(text, fmt.Sprintf("tab-%d", i)) {
				errs <- fmt.Errorf("evaluate tab %d = %q (tab tertukar?)", i, text)
			}
		}(i)
	}
	// Daftar tab lewat tool bersamaan dengan evaluasi.
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			out := ompRun(t, f, ctx, "browser_tabs", "list")
			if !strings.Contains(out, "Halaman Berat") {
				errs <- fmt.Errorf("browser_tabs list = %q", out)
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
}
