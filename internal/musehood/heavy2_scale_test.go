package musehood

// Tes berat ronde 2 — skala & beban (28 Nov 2026): memori durable 500
// FIFO, todo 100/ruang, checkpoint 50/ruang, 200 ruang dengan
// goal/todo/checkpoint, autoresearch 10 iterasi, review diff besar,
// execute Code Mode 20 panggilan RPC, MCP berulang dengan cache 60 dtk,
// dan benchmark jalur panas omp.

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestHeavy2MemoryFIFO500: memori durable berhenti di 500 (FIFO),
// yang tertua terbuang, recall/edit tetap bekerja pada skala itu.
func TestHeavy2MemoryFIFO500(t *testing.T) {
	f := setup(t, nil)
	ctx := ompRoomCtx()

	// Sisipkan 495 memori langsung, lalu tambah 10 lewat tool.
	f.app.mu.Lock()
	for i := 0; i < 495; i++ {
		f.app.data.Memories = append(f.app.data.Memories, durableMemory{
			ID: fmt.Sprintf("mem-%04d", i), Text: fmt.Sprintf("memori skala token%04d", i),
			SeatCode: "M01", CreatedAt: now(),
		})
	}
	if err := f.app.saveLocked(); err != nil {
		f.app.mu.Unlock()
		t.Fatal(err)
	}
	f.app.mu.Unlock()

	for i := 0; i < 10; i++ {
		out := ompRun(t, f, ctx, "memory_retain", fmt.Sprintf("memori tool tokentool%02d\nskala", i))
		if !strings.Contains(out, "tersimpan") {
			t.Fatalf("memory_retain %d = %q", i, out)
		}
	}

	f.app.mu.Lock()
	count := len(f.app.data.Memories)
	first := f.app.data.Memories[0].Text
	last := f.app.data.Memories[count-1].Text
	f.app.mu.Unlock()
	if count != maxDurableMemories {
		t.Fatalf("jumlah memori = %d, mau %d", count, maxDurableMemories)
	}
	// 495 + 10 = 505 -> 5 tertua (token0000..token0004) terbuang.
	if !strings.Contains(first, "token0005") {
		t.Fatalf("memori pertama sesudah FIFO = %q, mau token0005", first)
	}
	if !strings.Contains(last, "tokentool09") {
		t.Fatalf("memori terakhir = %q, mau tokentool09", last)
	}

	start := time.Now()
	out := ompRun(t, f, ctx, "memory_recall", "token0494")
	recallDur := time.Since(start)
	if !strings.Contains(out, "token0494") {
		t.Fatalf("memory_recall pada 500 memori = %q", out)
	}
	t.Logf("memory_recall atas 500 memori: %v", recallDur)
	out = ompRun(t, f, ctx, "memory_recall", "token0000")
	if strings.Contains(out, "token0000") {
		t.Fatalf("memori terbuang FIFO masih ditemukan: %q", out)
	}
	// Edit pada skala: hapus satu, ubah satu.
	f.app.mu.Lock()
	victimID := f.app.data.Memories[10].ID
	editID := f.app.data.Memories[11].ID
	f.app.mu.Unlock()
	out = ompRun(t, f, ctx, "memory_edit", "delete "+victimID)
	if !strings.Contains(out, "dihapus") {
		t.Fatalf("memory_edit delete = %q", out)
	}
	out = ompRun(t, f, ctx, "memory_edit", "update "+editID+"\nmemori hasil edit skala")
	if !strings.Contains(out, "diperbarui") {
		t.Fatalf("memory_edit update = %q", out)
	}
	f.app.mu.Lock()
	count = len(f.app.data.Memories)
	f.app.mu.Unlock()
	if count != maxDurableMemories-1 {
		t.Fatalf("jumlah memori sesudah delete = %d, mau %d", count, maxDurableMemories-1)
	}
}

// TestHeavy2TodoCheckpointCaps: todo berhenti di 100/ruang, checkpoint
// berhenti di 50/ruang (FIFO), rewind ke checkpoint tertua yang tersisa
// tetap bekerja.
func TestHeavy2TodoCheckpointCaps(t *testing.T) {
	f := setup(t, nil)
	ctx := ompRoomCtx()

	start := time.Now()
	for i := 0; i < maxRoomTodos; i++ {
		out := ompRun(t, f, ctx, "todo_add", fmt.Sprintf("item skala %d", i))
		if !strings.Contains(out, "ditambahkan") {
			t.Fatalf("todo_add %d = %q", i, out)
		}
	}
	t.Logf("100 todo_add: %v", time.Since(start))
	out := ompRun(t, f, ctx, "todo_add", "item ke-101")
	if !strings.Contains(out, "penuh") {
		t.Fatalf("todo_add ke-101 = %q, mau penolakan penuh", out)
	}
	out = ompRun(t, f, ctx, "todo_list", "")
	if !strings.Contains(out, "item skala 99") {
		t.Fatalf("todo_list pada 100 item tidak memuat item terakhir")
	}

	// Checkpoint: 60 dibuat, 50 tersisa (cp-10..cp-59).
	f.app.mu.Lock()
	f.app.data.Messages = append(f.app.data.Messages, &message{
		ID: uuid(), CreatedAt: now(), AuthorType: "owner", Text: "acuan checkpoint",
		Status: "completed", RoomID: defaultRoomID, BranchID: "main",
	})
	f.app.mu.Unlock()
	for i := 0; i < 60; i++ {
		out := ompRun(t, f, ctx, "checkpoint", fmt.Sprintf("cp-%02d", i))
		if !strings.Contains(out, "Checkpoint dibuat") {
			t.Fatalf("checkpoint %d = %q", i, out)
		}
	}
	f.app.mu.Lock()
	room := f.app.data.findRoom(defaultRoomID)
	cpCount := len(room.Checkpoints)
	firstLabel := room.Checkpoints[0].Label
	lastLabel := room.Checkpoints[cpCount-1].Label
	f.app.mu.Unlock()
	if cpCount != maxRoomCheckpoints {
		t.Fatalf("checkpoint tersimpan = %d, mau %d", cpCount, maxRoomCheckpoints)
	}
	if firstLabel != "cp-10" || lastLabel != "cp-59" {
		t.Fatalf("FIFO checkpoint salah: pertama=%q terakhir=%q, mau cp-10/cp-59", firstLabel, lastLabel)
	}
	// Rewind ke checkpoint tertua yang tersisa lewat API pemilik.
	f.app.mu.Lock()
	f.app.data.Messages = append(f.app.data.Messages, &message{
		ID: uuid(), CreatedAt: now(), AuthorType: "owner", Text: "sesudah checkpoint",
		Status: "completed", RoomID: defaultRoomID, BranchID: "main",
	})
	f.app.mu.Unlock()
	resp := f.api("POST", "/api/rooms/"+defaultRoomID+"/rewind", map[string]any{"checkpointId": "cp-10"}, 200)
	if resp["removed"] == nil {
		t.Fatalf("rewind cp-10 = %v", resp)
	}
	f.app.mu.Lock()
	remaining := len(f.app.data.findRoom(defaultRoomID).Checkpoints)
	f.app.mu.Unlock()
	if remaining != 1 {
		t.Fatalf("checkpoint sesudah rewind ke cp-10 = %d, mau 1", remaining)
	}
}

// TestHeavy2RewindSameTimestampCheckpoints: rewind membuang checkpoint
// yang lebih baru berdasarkan urutan, bukan perbandingan timestamp
// (checkpoint yang dibuat pada milidetik yang sama tidak boleh lolos).
func TestHeavy2RewindSameTimestampCheckpoints(t *testing.T) {
	f := setup(t, nil)
	ctx := ompRoomCtx()
	addMsg := func(text string) {
		f.app.mu.Lock()
		f.app.data.Messages = append(f.app.data.Messages, &message{
			ID: uuid(), CreatedAt: now(), AuthorType: "owner", Text: text,
			Status: "completed", RoomID: defaultRoomID, BranchID: "main",
		})
		f.app.mu.Unlock()
	}
	addMsg("satu")
	ompRun(t, f, ctx, "checkpoint", "titik-1")
	addMsg("dua")
	ompRun(t, f, ctx, "checkpoint", "titik-2")
	addMsg("tiga")
	ompRun(t, f, ctx, "checkpoint", "titik-3")
	addMsg("empat")

	// Samakan semua timestamp checkpoint (kasus milidetik yang sama).
	f.app.mu.Lock()
	room := f.app.data.findRoom(defaultRoomID)
	for i := range room.Checkpoints {
		room.Checkpoints[i].CreatedAt = "2026-11-28T00:00:00.000Z"
	}
	f.app.mu.Unlock()

	f.api("POST", "/api/rooms/"+defaultRoomID+"/rewind", map[string]any{"checkpointId": "titik-1"}, 200)
	f.app.mu.Lock()
	room = f.app.data.findRoom(defaultRoomID)
	remaining := len(room.Checkpoints)
	msgCount := 0
	for _, m := range f.app.data.Messages {
		if m.RoomID == defaultRoomID {
			msgCount++
		}
	}
	f.app.mu.Unlock()
	if remaining != 1 {
		t.Fatalf("checkpoint tersisa sesudah rewind ke titik-1 = %d, mau 1 (checkpoint lebih baru harus terbuang)", remaining)
	}
	if msgCount != 1 {
		t.Fatalf("pesan tersisa = %d, mau 1", msgCount)
	}
}

// TestHeavy2Rooms200WithWorkState: 200 ruang masing-masing dengan
// goal, 3 todo, 2 checkpoint, dan 5 pesan; state + pencarian tetap
// responsif dan utuh.
func TestHeavy2Rooms200WithWorkState(t *testing.T) {
	f := setup(t, nil)

	f.app.mu.Lock()
	base := f.app.data.Rooms[0]
	for i := 0; i < 199; i++ {
		r := room{
			ID: fmt.Sprintf("ruang-skala-%03d", i), Name: fmt.Sprintf("Ruang Skala %d", i),
			CreatedAt: now(), SeatCodes: []string{"M01"}, Goal: fmt.Sprintf("objektif skala %d", i),
			PlanMode: i%2 == 0,
		}
		for j := 0; j < 3; j++ {
			r.Todos = append(r.Todos, roomTodo{ID: uuid(), Text: fmt.Sprintf("todo skala %d-%d", i, j), Status: "pending", CreatedAt: now()})
		}
		for j := 0; j < 2; j++ {
			r.Checkpoints = append(r.Checkpoints, roomCheckpoint{ID: uuid(), Label: fmt.Sprintf("cp skala %d-%d", i, j), MessageID: "x", CreatedAt: now()})
		}
		f.app.data.Rooms = append(f.app.data.Rooms, r)
		for j := 0; j < 5; j++ {
			f.app.data.Messages = append(f.app.data.Messages, &message{
				ID: uuid(), CreatedAt: now(), AuthorType: "owner",
				Text:   fmt.Sprintf("pesan skala ruang %d nomor %d tokenruang%03d", i, j, i),
				Status: "completed", RoomID: r.ID, BranchID: "main",
			})
		}
	}
	_ = base
	if err := f.app.saveLocked(); err != nil {
		f.app.mu.Unlock()
		t.Fatal(err)
	}
	f.app.mu.Unlock()

	start := time.Now()
	s := f.state()
	stateDur := time.Since(start)
	rooms := asObjects(s["rooms"])
	if len(rooms) != 200 {
		t.Fatalf("ruang di state = %d, mau 200", len(rooms))
	}
	checked := 0
	for _, item := range rooms {
		if todos, _ := item["todos"].([]any); len(todos) == 3 {
			checked++
		}
	}
	if checked != 199 {
		t.Fatalf("ruang dengan 3 todo di state = %d, mau 199", checked)
	}
	t.Logf("GET /api/state dengan 200 ruang + 995 pesan: %v", stateDur)

	start = time.Now()
	out := f.api("GET", "/api/search?q=tokenruang123", nil, 200)
	searchDur := time.Since(start)
	if !strings.Contains(fmt.Sprint(out), "tokenruang123") {
		t.Fatalf("pencarian tokenruang123 tidak menemukan pesan: %v", out)
	}
	t.Logf("GET /api/search atas 995 pesan: %v", searchDur)

	// Prompt mode ruang memuat goal pada skala ini.
	f.app.mu.Lock()
	r := f.app.data.findRoom("ruang-skala-042")
	prompt := roomModePrompt(r, ompSeat(), f.app.data.Settings)
	f.app.mu.Unlock()
	if !strings.Contains(prompt, "objektif skala 42") {
		t.Fatalf("roomModePrompt ruang skala = %q", prompt)
	}
}

// TestHeavy2Autoresearch10AndLargeReview: autoresearch 10 iterasi
// penuh dan review atas diff besar (terpotong rapi di jalur proyek).
func TestHeavy2Autoresearch10AndLargeReview(t *testing.T) {
	f := setup(t, nil)
	dir, err := f.app.ensureProject("skala-wf")
	if err != nil {
		t.Fatal(err)
	}
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=Uji", "-c", "user.email=uji@localhost"}, args...)...)
		if raw, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %s", args, raw)
		}
	}
	git("init")
	ompWriteProjectFile(t, f, "skala-wf", "main.go", "package main\n\nfunc main() {}\n")
	git("add", ".")
	git("commit", "-m", "awal")

	resp := f.api("POST", "/api/autoresearch", map[string]any{"project": "skala-wf", "script": "echo 7", "iterations": 10}, 200)
	results, _ := resp["results"].([]any)
	if len(results) != 10 {
		t.Fatalf("autoresearch 10 iterasi: results = %v", resp)
	}
	for i, item := range results {
		res, _ := item.(map[string]any)
		if res["ok"] != true || res["metric"] != "7" {
			t.Fatalf("iterasi %d = %v", i, res)
		}
	}

	// Diff proyek besar: projectDiff memotong di 12000 karakter.
	var big strings.Builder
	big.WriteString("package main\n\nfunc main() {\n")
	for i := 0; i < 800; i++ {
		fmt.Fprintf(&big, "\tprintln(\"baris sangat panjang nomor %d untuk membuat diff besar\")\n", i)
	}
	big.WriteString("}\n")
	ompWriteProjectFile(t, f, "skala-wf", "main.go", big.String())
	diff, err := f.app.projectDiff(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(diff) > 12000+64 || !strings.Contains(diff, "diff dipotong") {
		t.Fatalf("projectDiff besar tidak terpotong rapi: %d byte", len(diff))
	}

	// Review lewat API dengan diff besar eksplisit: job dibuat dengan
	// prompt yang memuat diff terpotong (penyelesaian job melibatkan
	// antrean kursi Muse sehingga tidak ditunggu di sini).
	resp = f.api("POST", "/api/review", map[string]any{"diff": diff, "roomId": defaultRoomID}, 202)
	if resp["jobId"] == nil {
		t.Fatalf("review diff besar = %v", resp)
	}
	f.app.mu.Lock()
	var reviewJob *job
	for _, j := range f.app.data.Jobs {
		if j.ID == resp["jobId"] {
			reviewJob = j
		}
	}
	f.app.mu.Unlock()
	if reviewJob == nil {
		t.Fatal("job review tidak ditemukan di data")
	}
	if !strings.Contains(reviewJob.Prompt, "diff dipotong") || len(reviewJob.Prompt) > 14000 {
		t.Fatalf("prompt review tidak terpotong rapi: %d byte", len(reviewJob.Prompt))
	}
}

// TestHeavy2Execute20RPC: execute Code Mode menjalankan tepat 20
// panggilan RPC; panggilan ke-21 ditolak batas.
func TestHeavy2Execute20RPC(t *testing.T) {
	f := setup(t, nil)
	ctx := ompRoomCtx()
	js := `
let ok = 0;
for (let i = 0; i < 20; i++) {
  const r = await tool("current_time", "");
  if (r.includes("2026")) ok++;
}
const extra = await tool("current_time", "");
console.log("OK:" + ok + " BATAS:" + extra.includes("batas"));
`
	start := time.Now()
	out := ompRun(t, f, ctx, "execute", js)
	t.Logf("execute 20 RPC: %v", time.Since(start))
	if !strings.Contains(out, "OK:20") || !strings.Contains(out, "BATAS:true") {
		t.Fatalf("execute 20 RPC = %q", out)
	}
}

// heavy2CountingMCPServer adalah server MCP palsu yang mencatat metode
// yang dipanggil ke berkas log (path dari env MCP_LOG).
const heavy2CountingMCPServer = `import sys, json, os
log_path = os.environ.get("MCP_LOG", "")
def log(method):
    if log_path:
        with open(log_path, "a") as fh:
            fh.write(method + "\n")
for line in sys.stdin:
    line = line.strip()
    if not line:
        continue
    try:
        req = json.loads(line)
    except Exception:
        continue
    method = req.get("method")
    rid = req.get("id")
    if method:
        log(method)
    if method == "initialize":
        print(json.dumps({"jsonrpc": "2.0", "id": rid, "result": {"protocolVersion": "2024-11-05", "capabilities": {}, "serverInfo": {"name": "hitung", "version": "1.0"}}}), flush=True)
    elif method == "tools/list":
        print(json.dumps({"jsonrpc": "2.0", "id": rid, "result": {"tools": [{"name": "echo_text", "description": "Gema", "inputSchema": {"type": "object"}}]}}), flush=True)
    elif method == "tools/call":
        args = req.get("params", {}).get("arguments", {})
        print(json.dumps({"jsonrpc": "2.0", "id": rid, "result": {"content": [{"type": "text", "text": "ECHO:" + str(args.get("text", ""))}]}}), flush=True)
`

// TestHeavy2MCPRepeatedCache: panggilan MCP berulang memakai cache
// tools/list 60 detik (tools/list tidak diulang), tools/call tetap
// dijalankan per panggilan dengan persetujuan.
func TestHeavy2MCPRepeatedCache(t *testing.T) {
	f := setup(t, nil)
	ctx := ompRoomCtx()
	script := filepath.Join(t.TempDir(), "counting_mcp.py")
	if err := os.WriteFile(script, []byte(heavy2CountingMCPServer), 0o644); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(t.TempDir(), "mcp.log")
	f.api("POST", "/api/mcp-servers", object{
		"name": "hitung", "command": "python3", "args": []string{script},
		"env": object{"MCP_LOG": logPath},
	}, 200)
	waitFor(t, "tool MCP hitung muncul", func() bool {
		tools := f.api("GET", "/api/tools", nil, 200)
		return strings.Contains(fmt.Sprint(tools["mcp"]), "mcp_hitung_echo_text")
	})
	countMethod := func(method string) int {
		raw, _ := os.ReadFile(logPath)
		n := 0
		for _, line := range strings.Split(string(raw), "\n") {
			if strings.TrimSpace(line) == method {
				n++
			}
		}
		return n
	}
	listBefore := countMethod("tools/list")
	if listBefore < 1 {
		t.Fatalf("tools/list awal = %d, mau >=1", listBefore)
	}

	// 10x sinkronisasi daftar tool: semua dari cache.
	cfg := toolCfg(f).MCPServers[0]
	for i := 0; i < 10; i++ {
		if tools := f.app.mcpToolsForSync(context.Background(), cfg); len(tools) != 1 {
			t.Fatalf("mcpToolsForSync %d = %+v", i, tools)
		}
	}
	// 3x panggilan tool nyata dengan persetujuan.
	for i := 0; i < 3; i++ {
		done := make(chan string, 1)
		go func(i int) {
			done <- ompRun(t, f, ctx, "mcp_hitung_echo_text", fmt.Sprintf(`{"text":"panggilan-%d"}`, i))
		}(i)
		ompApproveNext(t, f, "mcp")
		if out := <-done; !strings.Contains(out, fmt.Sprintf("ECHO:panggilan-%d", i)) {
			t.Fatalf("panggilan MCP %d = %q", i, out)
		}
	}
	if got := countMethod("tools/list"); got != listBefore {
		t.Fatalf("tools/list sesudah panggilan berulang = %d, mau tetap %d (cache 60 dtk)", got, listBefore)
	}
	if got := countMethod("tools/call"); got != 3 {
		t.Fatalf("tools/call = %d, mau 3", got)
	}
}

// --- Benchmark jalur panas omp ---

func BenchmarkHeavy2PolicyDispatch(b *testing.B) {
	app := &App{dir: b.TempDir()}
	cfg := settings{ToolPolicies: map[string]string{"write_file": "deny", "current_time": "allow"}}
	s := heavy2Seat("M01", "Plamo")
	ctx := heavy2Ctx("ruang-bench")
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = app.enforceToolPolicy(ctx, cfg, s, "write_file", "x.txt\nisi")
		_ = app.enforceToolPolicy(ctx, cfg, s, "current_time", "")
		_ = app.enforcePlanMode(ctx, "write_file", "x.txt\nisi")
	}
}

func BenchmarkHeavy2MemoryRecall500(b *testing.B) {
	app := &App{dir: b.TempDir()}
	for i := 0; i < 500; i++ {
		app.data.Memories = append(app.data.Memories, durableMemory{
			ID: fmt.Sprintf("mem-%04d", i), Text: fmt.Sprintf("memori bench token%04d dengan isi cukup panjang untuk dicari", i),
			Tags: []string{"bench"}, CreatedAt: now(),
		})
	}
	cfg := settings{}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		out := app.toolMemoryRecall(cfg, "token0250")
		if !strings.Contains(out, "token0250") {
			b.Fatalf("recall = %q", out)
		}
	}
}

func BenchmarkHeavy2TodoList100(b *testing.B) {
	app := &App{dir: b.TempDir()}
	r := room{ID: "ruang-bench", Name: "Bench", CreatedAt: now()}
	for i := 0; i < 100; i++ {
		r.Todos = append(r.Todos, roomTodo{ID: uuid(), Text: fmt.Sprintf("todo bench %d", i), Status: "pending", CreatedAt: now()})
	}
	app.data.Rooms = append(app.data.Rooms, r)
	ctx := heavy2Ctx("ruang-bench")
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		out := app.toolTodoList(ctx)
		if !strings.Contains(out, "todo bench 99") {
			b.Fatalf("todo_list = %.100q", out)
		}
	}
}
