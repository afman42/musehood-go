package musehood

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseToolBlock(t *testing.T) {
	name, arg, found := parseToolBlock("Baik.\n<<TOOL:web_search>>\nharga emas hari ini\n<</TOOL>>")
	if !found || name != "web_search" || arg != "harga emas hari ini" {
		t.Fatalf("parse = %q %q %v", name, arg, found)
	}
	if _, _, found := parseToolBlock("jawaban biasa tanpa tool"); found {
		t.Fatal("blok tool tidak boleh terdeteksi")
	}
	if got := stripToolBlocks("halo <<TOOL:current_time>><</TOOL>> dunia"); got != "halo  dunia" {
		t.Fatalf("strip = %q", got)
	}
}

func TestSandboxTools(t *testing.T) {
	app := &App{dir: t.TempDir()}
	if _, err := app.sandboxPath("../rahasia.txt"); err == nil {
		t.Fatal("path traversal harus ditolak")
	}
	if _, err := app.sandboxPath("sub/file.txt"); err == nil {
		t.Fatal("subfolder harus ditolak")
	}
	ctx := context.Background()
	out, _ := app.runTool(ctx, settings{}, seat{}, "write_file", "catatan.txt\nisi catatan", true)
	if !strings.Contains(out, "tersimpan") {
		t.Fatalf("write_file = %q", out)
	}
	if got, _ := app.runTool(ctx, settings{}, seat{}, "read_file", "catatan.txt", true); got != "isi catatan" {
		t.Fatalf("read_file = %q", got)
	}
	if got, _ := app.runTool(ctx, settings{}, seat{}, "list_files", "", true); !strings.Contains(got, "catatan.txt") {
		t.Fatalf("list_files = %q", got)
	}
	if got, _ := app.runTool(ctx, settings{}, seat{}, "tidak_ada", "", true); !strings.Contains(got, "tidak dikenal") {
		t.Fatalf("tool asing = %q", got)
	}
}

func TestCheckPublicURLRejectsPrivate(t *testing.T) {
	ctx := context.Background()
	for _, raw := range []string{
		"http://127.0.0.1:3001/api/state",
		"http://localhost/admin",
		"http://10.0.0.5/",
		"http://192.168.1.1/",
		"http://[::1]/",
		"ftp://example.com/file",
		"bukan url",
	} {
		if err := checkPublicURL(ctx, raw); err == nil {
			t.Fatalf("%s harus ditolak", raw)
		}
	}
}

func TestChatWithToolsFlow(t *testing.T) {
	calls := 0
	handler := func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "GET /v1/models":
			io.WriteString(w, `{"data":[{"id":"toolbot"}]}`)
		case "POST /v1/chat/completions":
			var body struct {
				Model    string
				Messages []struct {
					Role    string
					Content string
				}
			}
			json.NewDecoder(r.Body).Decode(&body)
			calls++
			if calls == 1 {
				io.WriteString(w, `{"choices":[{"message":{"content":"<<TOOL:write_file>>\ncatatan.txt\nisi dari tool\n<</TOOL>>"}}]}`)
				return
			}
			last := body.Messages[len(body.Messages)-1].Content
			if !strings.Contains(last, "Hasil tool write_file") || !strings.Contains(last, "tersimpan") {
				http.Error(w, "hasil tool tidak diteruskan: "+last, 400)
				return
			}
			io.WriteString(w, `{"choices":[{"message":{"content":"Selesai, file catatan.txt sudah tersimpan."}}]}`)
		default:
			http.NotFound(w, r)
		}
	}
	f := setup(t, handler)
	f.api("POST", "/api/settings", object{"seats": []object{{"code": "M01", "model": "toolbot"}}}, 200)
	f.api("POST", "/api/message", object{"text": "@Plamo tolong simpan catatan"}, 202)
	msgs := f.finish(2)
	reply := msgs[1]
	if reply["status"] != "completed" || !strings.Contains(reply["text"].(string), "Selesai") {
		t.Fatalf("balasan = %v", reply)
	}
	tools, _ := reply["toolsUsed"].([]any)
	if len(tools) != 1 || tools[0] != "write_file" {
		t.Fatalf("toolsUsed = %v", reply["toolsUsed"])
	}
	raw, err := os.ReadFile(filepath.Join(f.dir, "sandbox", "catatan.txt"))
	if err != nil || string(raw) != "isi dari tool" {
		t.Fatalf("file sandbox = %q err=%v", raw, err)
	}
	if calls != 2 {
		t.Fatalf("panggilan gateway = %d, mau 2", calls)
	}
}

func TestToolsDisabledSkipsLoop(t *testing.T) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" {
			io.WriteString(w, `{"data":[{"id":"toolbot"}]}`)
			return
		}
		io.WriteString(w, `{"choices":[{"message":{"content":"<<TOOL:current_time>><</TOOL>>"}}]}`)
	}
	f := setup(t, handler)
	f.api("POST", "/api/settings", object{"settings": object{"toolsEnabled": false}, "seats": []object{{"code": "M01", "model": "toolbot"}}}, 200)
	f.api("POST", "/api/message", object{"text": "@Plamo jam berapa?"}, 202)
	msgs := f.finish(2)
	reply := msgs[1]
	if _, ok := reply["toolsUsed"]; ok {
		t.Fatalf("toolsUsed tidak boleh ada saat tool mati: %v", reply)
	}
	if !strings.Contains(reply["text"].(string), "<<TOOL:current_time>>") {
		t.Fatalf("teks mentah harus diteruskan apa adanya: %v", reply["text"])
	}
}

func TestDangerousCode(t *testing.T) {
	for _, code := range []string{
		`import os; os.system("ls")`,
		`open("/etc/passwd").read()`,
		`import subprocess; subprocess.run(["ls"])`,
		`fetch("http://example.com")`,
		`while True: pass`,
		`rm -rf /`,
	} {
		if _, dangerous := dangerousCode(code); !dangerous {
			t.Fatalf("harus terdeteksi berbahaya: %s", code)
		}
	}
	for _, code := range []string{
		`print(2 + 2)`,
		`for i in range(5): print(i)`,
		`data = [3, 1, 2]; print(sorted(data))`,
		`console.log("halo")`,
	} {
		if match, dangerous := dangerousCode(code); dangerous {
			t.Fatalf("kode aman terdeteksi berbahaya (%s): %s", match, code)
		}
	}
}

func TestRunCodeSafe(t *testing.T) {
	app := &App{dir: t.TempDir()}
	s := seat{Code: "M01", Name: "Plamo"}
	out := app.toolRunCode(context.Background(), s, "python\nprint(6 * 7)")
	if !strings.Contains(out, "42") {
		t.Fatalf("run_code python = %q", out)
	}
	out = app.toolRunCode(context.Background(), s, "javascript\nconsole.log('js', 1 + 1)")
	if !strings.Contains(out, "js 2") {
		t.Fatalf("run_code javascript = %q", out)
	}
	// Path relatif keluar sandbox: di dalam bwrap, /work adalah satu-satunya
	// folder yang terlihat, jadi ../data.json tidak ada.
	out = app.toolRunCode(context.Background(), s, "python\nprint(open('../data.json').read())")
	if !strings.Contains(out, "error") && !strings.Contains(out, "Error") {
		t.Fatalf("baca file di luar sandbox harus gagal: %q", out)
	}
}

func toolGatewayHandler(t *testing.T, dangerous bool) http.HandlerFunc {
	t.Helper()
	calls := 0
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "GET /v1/models":
			io.WriteString(w, `{"data":[{"id":"toolbot"}]}`)
		case "POST /v1/chat/completions":
			var body struct {
				Model    string
				Messages []struct {
					Role    string
					Content string
				}
			}
			json.NewDecoder(r.Body).Decode(&body)
			calls++
			if calls == 1 {
				code := "print('halo aman')"
				if dangerous {
					code = `import os; os.system("echo halo")`
				}
				payload, _ := json.Marshal(object{"choices": []object{{"message": object{"content": "<<TOOL:run_code>>\npython\n" + code + "\n<</TOOL>>"}}}})
				w.Write(payload)
				return
			}
			last := body.Messages[len(body.Messages)-1].Content
			if !strings.Contains(last, "Hasil tool run_code") {
				http.Error(w, "hasil tool tidak diteruskan: "+last, 400)
				return
			}
			payload, _ := json.Marshal(object{"choices": []object{{"message": object{"content": "Selesai. Hasil tool: " + last}}}})
			w.Write(payload)
		default:
			http.NotFound(w, r)
		}
	}
}

func TestRunCodeApprovalApprove(t *testing.T) {
	f := setup(t, toolGatewayHandler(t, true))
	f.api("POST", "/api/settings", object{"seats": []object{{"code": "M01", "model": "toolbot"}}}, 200)
	f.api("POST", "/api/message", object{"text": "@Plamo jalankan kode itu"}, 202)
	s := f.wait(func(s object) bool { return len(asObjects(s["pendingApprovals"])) == 1 })
	approval := asObjects(s["pendingApprovals"])[0]
	if approval["kind"] != "code" || approval["seatCode"] != "M01" || !strings.Contains(approval["detail"].(string), "os.system") {
		t.Fatalf("approval = %v", approval)
	}
	f.api("POST", "/api/approvals/"+approval["id"].(string), object{"approved": true}, 200)
	msgs := f.finish(2)
	reply := msgs[1]
	if reply["status"] != "completed" || !strings.Contains(reply["text"].(string), "halo") {
		t.Fatalf("balasan = %v", reply)
	}
	tools, _ := reply["toolsUsed"].([]any)
	if len(tools) != 1 || tools[0] != "run_code" {
		t.Fatalf("toolsUsed = %v", reply["toolsUsed"])
	}
}

func TestRunCodeApprovalReject(t *testing.T) {
	f := setup(t, toolGatewayHandler(t, true))
	f.api("POST", "/api/settings", object{"seats": []object{{"code": "M01", "model": "toolbot"}}}, 200)
	f.api("POST", "/api/message", object{"text": "@Plamo jalankan kode itu"}, 202)
	s := f.wait(func(s object) bool { return len(asObjects(s["pendingApprovals"])) == 1 })
	approval := asObjects(s["pendingApprovals"])[0]
	f.api("POST", "/api/approvals/"+approval["id"].(string), object{"approved": false}, 200)
	msgs := f.finish(2)
	reply := msgs[1]
	if !strings.Contains(reply["text"].(string), "TIDAK dijalankan") {
		t.Fatalf("kode ditolak harus dilaporkan ke model: %v", reply["text"])
	}
}

func TestEditAppendFile(t *testing.T) {
	app := &App{dir: t.TempDir()}
	if out := app.toolWriteFile("catatan.txt\nbaris satu\nbaris dua"); !strings.Contains(out, "tersimpan") {
		t.Fatalf("write_file = %q", out)
	}
	if out := app.toolAppendFile("catatan.txt\n\nbaris tiga"); !strings.Contains(out, "ditambahkan") {
		t.Fatalf("append_file = %q", out)
	}
	if got := app.toolReadFile("catatan.txt"); got != "baris satu\nbaris dua\nbaris tiga" {
		t.Fatalf("isi setelah append = %q", got)
	}
	if out := app.toolEditFile("catatan.txt\n<<<OLD>>>\nbaris dua\n<<<NEW>>>\nbaris DUA"); !strings.Contains(out, "diubah") {
		t.Fatalf("edit_file = %q", out)
	}
	if got := app.toolReadFile("catatan.txt"); got != "baris satu\nbaris DUA\nbaris tiga" {
		t.Fatalf("isi setelah edit = %q", got)
	}
	if out := app.toolEditFile("catatan.txt\n<<<OLD>>>\ntidak ada\n<<<NEW>>>\nx"); !strings.Contains(out, "tidak ditemukan") {
		t.Fatalf("edit teks tidak ada harus error: %q", out)
	}
	if out := app.toolAppendFile("baru.txt\nhalo"); !strings.Contains(out, "ditambahkan") {
		t.Fatalf("append file baru = %q", out)
	}
}

func TestDownloadFile(t *testing.T) {
	app := &App{dir: t.TempDir()}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "isi unduhan")
	}))
	defer server.Close()
	if out := app.downloadToSandbox(context.Background(), server.URL+"/data.txt", "unduhan.txt"); !strings.Contains(out, "terunduh") {
		t.Fatalf("downloadToSandbox = %q", out)
	}
	if got := app.toolReadFile("unduhan.txt"); got != "isi unduhan" {
		t.Fatalf("isi unduhan = %q", got)
	}
	// URL localhost harus ditolak penjaga SSRF.
	if out := app.toolDownloadFile(context.Background(), server.URL+"/data.txt\njahat.txt"); !strings.Contains(out, "Error") {
		t.Fatalf("download localhost harus ditolak: %q", out)
	}
}

func TestMemorySave(t *testing.T) {
	app := &App{dir: t.TempDir(), data: data{Seats: []seat{{Code: "M01", Name: "Plamo"}}}}
	s := seat{Code: "M01", Name: "Plamo"}
	if out := app.toolMemorySave(s, "Arman suka kopi tubruk"); !strings.Contains(out, "tersimpan") {
		t.Fatalf("memory_save = %q", out)
	}
	if got := app.data.Seats[0].Memory; got != "- Arman suka kopi tubruk" {
		t.Fatalf("memori kursi = %q", got)
	}
	if out := app.toolMemorySave(s, "Rapat tiap Senin"); !strings.Contains(out, "tersimpan") {
		t.Fatalf("memory_save kedua = %q", out)
	}
	if got := app.data.Seats[0].Memory; got != "- Arman suka kopi tubruk\n- Rapat tiap Senin" {
		t.Fatalf("memori kursi = %q", got)
	}
	if _, err := os.Stat(filepath.Join(app.dir, "data.json")); err != nil {
		t.Fatalf("data.json harus tertulis: %v", err)
	}
}

// mockToolGateway membalas panggilan chat pertama dengan blok tool dan
// panggilan kedua dengan teks akhir; endpoint gambar mengembalikan PNG 1x1.
func mockToolGateway(t *testing.T, toolName, arg string) http.HandlerFunc {
	t.Helper()
	calls := 0
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "GET /v1/models":
			io.WriteString(w, `{"data":[{"id":"toolbot"}]}`)
		case "POST /v1/images/generations":
			io.WriteString(w, `{"data":[{"b64_json":"iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8Xw8AAwMCAO+ip1sAAAAASUVORK5CYII="}]}`)
		case "POST /v1/chat/completions":
			var body struct {
				Stream   *bool
				Messages []struct {
					Role    string
					Content string
				}
			}
			json.NewDecoder(r.Body).Decode(&body)
			calls++
			text := ""
			if calls == 1 {
				text = "<<TOOL:" + toolName + ">>\n" + arg + "\n<</TOOL>>"
			} else {
				text = "Selesai. " + body.Messages[len(body.Messages)-1].Content
			}
			if body.Stream != nil && *body.Stream {
				w.Header().Set("Content-Type", "text/event-stream")
				payload, _ := json.Marshal(object{"choices": []object{{"delta": object{"content": text}}}})
				io.WriteString(w, "data: "+string(payload)+"\n\ndata: [DONE]\n\n")
				return
			}
			payload, _ := json.Marshal(object{"choices": []object{{"message": object{"content": text}}}})
			w.Write(payload)
		default:
			http.NotFound(w, r)
		}
	}
}

func TestDeleteFileApproval(t *testing.T) {
	makeFixture := func() (fixture, string) {
		f := setup(t, mockToolGateway(t, "delete_file", "hapus-aku.txt"))
		f.api("POST", "/api/settings", object{"seats": []object{{"code": "M01", "model": "toolbot"}}}, 200)
		target := filepath.Join(f.app.sandboxDir(), "hapus-aku.txt")
		if err := os.MkdirAll(f.app.sandboxDir(), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, []byte("halo"), 0o644); err != nil {
			t.Fatal(err)
		}
		f.api("POST", "/api/message", object{"text": "@Plamo hapus file hapus-aku.txt"}, 202)
		return f, target
	}

	// Ditolak: file tetap ada dan penolakan dilaporkan ke model.
	f, target := makeFixture()
	s := f.wait(func(s object) bool { return len(asObjects(s["pendingApprovals"])) == 1 })
	approval := asObjects(s["pendingApprovals"])[0]
	if approval["kind"] != "delete" || approval["detail"] != "hapus-aku.txt" {
		t.Fatalf("approval = %v", approval)
	}
	f.api("POST", "/api/approvals/"+approval["id"].(string), object{"approved": false}, 200)
	msgs := f.finish(2)
	if !strings.Contains(msgs[1]["text"].(string), "TIDAK dihapus") {
		t.Fatalf("penolakan harus dilaporkan: %v", msgs[1]["text"])
	}
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("file harus tetap ada setelah ditolak: %v", err)
	}

	// Disetujui: file terhapus.
	f2, target2 := makeFixture()
	s2 := f2.wait(func(s object) bool { return len(asObjects(s["pendingApprovals"])) == 1 })
	approval2 := asObjects(s2["pendingApprovals"])[0]
	f2.api("POST", "/api/approvals/"+approval2["id"].(string), object{"approved": true}, 200)
	msgs2 := f2.finish(2)
	if !strings.Contains(msgs2[1]["text"].(string), "terhapus") {
		t.Fatalf("penghapusan harus dilaporkan: %v", msgs2[1]["text"])
	}
	if _, err := os.Stat(target2); !os.IsNotExist(err) {
		t.Fatalf("file harus terhapus setelah disetujui: %v", err)
	}
}

func TestRunShellApproval(t *testing.T) {
	f := setup(t, mockToolGateway(t, "run_shell", "echo halo-shell && pwd"))
	f.api("POST", "/api/settings", object{"seats": []object{{"code": "M01", "model": "toolbot"}}}, 200)
	f.api("POST", "/api/message", object{"text": "@Plamo jalankan perintah itu"}, 202)
	s := f.wait(func(s object) bool { return len(asObjects(s["pendingApprovals"])) == 1 })
	approval := asObjects(s["pendingApprovals"])[0]
	if approval["kind"] != "shell" || !strings.Contains(approval["detail"].(string), "echo halo-shell") {
		t.Fatalf("approval = %v", approval)
	}
	f.api("POST", "/api/approvals/"+approval["id"].(string), object{"approved": true}, 200)
	msgs := f.finish(2)
	reply := msgs[1]
	if reply["status"] != "completed" || !strings.Contains(reply["text"].(string), "halo-shell") || !strings.Contains(reply["text"].(string), "/work") {
		t.Fatalf("balasan shell = %v", reply)
	}
}

func TestGenerateImageTool(t *testing.T) {
	f := setup(t, mockToolGateway(t, "generate_image", "kucing oranye tidur di atas bantal"))
	f.api("POST", "/api/settings", object{"seats": []object{{"code": "M01", "model": "toolbot"}}}, 200)
	f.api("POST", "/api/message", object{"text": "@Plamo buatkan gambar kucing"}, 202)
	msgs := f.finish(2)
	reply := msgs[1]
	if reply["status"] != "completed" {
		t.Fatalf("status = %v err=%v", reply["status"], reply["error"])
	}
	imageURL, _ := reply["imageUrl"].(string)
	if !strings.HasPrefix(imageURL, "/images/") || !strings.HasSuffix(imageURL, ".png") {
		t.Fatalf("imageUrl = %q", imageURL)
	}
	tools, _ := reply["toolsUsed"].([]any)
	if len(tools) != 1 || tools[0] != "generate_image" {
		t.Fatalf("toolsUsed = %v", reply["toolsUsed"])
	}
}
