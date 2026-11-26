package musehood

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
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
	out := app.runTool(ctx, "write_file", "catatan.txt\nisi catatan")
	if !strings.Contains(out, "tersimpan") {
		t.Fatalf("write_file = %q", out)
	}
	if got := app.runTool(ctx, "read_file", "catatan.txt"); got != "isi catatan" {
		t.Fatalf("read_file = %q", got)
	}
	if got := app.runTool(ctx, "list_files", ""); !strings.Contains(got, "catatan.txt") {
		t.Fatalf("list_files = %q", got)
	}
	if got := app.runTool(ctx, "tidak_ada", ""); !strings.Contains(got, "tidak dikenal") {
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
