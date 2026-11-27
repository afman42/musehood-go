package musehood

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// raw performs a request and returns status, body and response headers
// without the JSON-only assertions of fixture.api.
func (f fixture) raw(method, path string, body io.Reader, contentType string) (int, []byte, http.Header) {
	f.t.Helper()
	req, err := http.NewRequest(method, f.server.URL+path, body)
	if err != nil {
		f.t.Fatal(err)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		f.t.Fatal(err)
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		f.t.Fatal(err)
	}
	escaped, _ := json.Marshal(secret)
	if bytes.Contains(raw, []byte(secret)) || bytes.Contains(raw, escaped[1:len(escaped)-1]) {
		f.t.Fatal("credential leaked in " + path)
	}
	return res.StatusCode, raw, res.Header
}

func idOf(m object) string { id, _ := m["id"].(string); return id }

func findMessageBy(t *testing.T, f fixture, match func(object) bool) object {
	t.Helper()
	for _, m := range messages(f.state()) {
		if match(m) {
			return m
		}
	}
	t.Fatal("message not found")
	return nil
}

func TestEditDeleteReplyReact(t *testing.T) {
	f := setup(t, nil)
	f.api("POST", "/api/settings", object{"settings": object{"roundRobin": false}}, 200)
	f.api("POST", "/api/message", object{"text": "@Opus uji"}, 202)
	ms := f.finish(2)
	ownerID, replyID := idOf(ms[0]), idOf(ms[1])

	// Edit pesan sendiri (fitur 13: edit menjalankan ulang diskusi, 202).
	res, _, _ := f.raw("POST", "/api/message/"+ownerID+"/edit", strings.NewReader(`{"text":"@Opus teks baru"}`), "application/json")
	if res != 202 {
		t.Fatalf("edit status %d", res)
	}
	ms = f.finish(2)
	replyID = idOf(ms[1])
	m := findMessageBy(t, f, func(m object) bool { return idOf(m) == ownerID })
	if m["text"] != "@Opus teks baru" || m["editedAt"] == nil {
		t.Fatal(m)
	}
	// Edit kosong ditolak, edit pesan kursi ditolak, ID tak dikenal 404.
	f.api("POST", "/api/message/"+ownerID+"/edit", object{"text": "   "}, 400)
	f.api("POST", "/api/message/"+replyID+"/edit", object{"text": "bajakan"}, 403)
	f.api("POST", "/api/message/id-tidak-ada/edit", object{"text": "x"}, 404)

	// Balasan dengan replyTo.
	f.api("POST", "/api/message", object{"text": "@Opus balas lagi", "replyTo": ownerID}, 202)
	ms = f.finish(4)
	if ms[2]["replyTo"] != ownerID || ms[2]["text"] != "@Opus balas lagi" {
		t.Fatal(ms[2])
	}
	f.api("POST", "/api/message", object{"text": "@Opus x", "replyTo": "id-hantu"}, 400)

	// Reaksi toggle.
	f.api("POST", "/api/message/"+replyID+"/react", object{"emoji": "👍"}, 200)
	m = findMessageBy(t, f, func(m object) bool { return idOf(m) == replyID })
	if len(asObjects(m["reactions"])) != 1 || asObjects(m["reactions"])[0]["emoji"] != "👍" || asObjects(m["reactions"])[0]["count"] != float64(1) || asObjects(m["reactions"])[0]["byOwner"] != true {
		t.Fatal(m)
	}
	f.api("POST", "/api/message/"+replyID+"/react", object{"emoji": "👍"}, 200)
	m = findMessageBy(t, f, func(m object) bool { return idOf(m) == replyID })
	if reactions := asObjects(m["reactions"]); len(reactions) != 0 {
		t.Fatal(m)
	}
	f.api("POST", "/api/message/"+replyID+"/react", object{"emoji": "🤖"}, 400)

	// Hapus pesan kursi.
	f.api("POST", "/api/message/"+replyID+"/delete", nil, 200)
	s := f.state()
	if len(messages(s)) != 3 {
		t.Fatal(s)
	}
	f.api("POST", "/api/message/"+replyID+"/delete", nil, 404)

	// Pesan yang sedang diproses tidak bisa diedit/dihapus.
	f.api("POST", "/api/message", object{"text": "Halo grup"}, 202)
	typing := f.wait(func(s object) bool {
		for _, m := range messages(s) {
			if m["status"] == "typing" {
				return true
			}
		}
		return false
	})
	typingMsg := messages(typing)[len(messages(typing))-1]
	f.api("POST", "/api/message/"+idOf(typingMsg)+"/delete", nil, 409)
	f.api("POST", "/api/message/"+idOf(typingMsg)+"/edit", object{"text": "x"}, 403)
}

func TestDeleteAllMessagesClearsActiveTask(t *testing.T) {
	f := setup(t, nil)
	f.api("POST", "/api/message", object{"text": "Tugas: uji hapus"}, 202)
	q := f.pending("summary")
	respFile := filepath.Join(f.dir, "queue", "responses", q["id"].(string)+".json")
	if err := os.WriteFile(respFile, []byte(`{"text":"Ringkasan tugas"}`), 0600); err != nil {
		t.Fatal(err)
	}
	ms := f.finish(7)
	if ms[6]["label"] != "Hasil Akhir - Integrasi" {
		t.Fatal(ms[6])
	}
	if s := f.state(); s["activeTask"] == nil {
		t.Fatal("activeTask harus ada")
	}
	// Ekspor Markdown memuat label akhir, nama, dan penanda gambar.
	status, md, headers := f.raw("GET", "/api/export?format=md", nil, "")
	if status != 200 || !strings.Contains(string(md), "Hasil Akhir - Integrasi") || !strings.Contains(string(md), "**Arman**") || !strings.Contains(string(md), "[gambar:") {
		t.Fatalf("export md %d: %s", status, md)
	}
	if headers.Get("Content-Disposition") == "" {
		t.Fatal("Content-Disposition wajib ada")
	}
	for _, m := range ms {
		f.api("POST", "/api/message/"+idOf(m)+"/delete", nil, 200)
	}
	s := f.wait(func(s object) bool { return s["activeTask"] == nil && len(messages(s)) == 0 })
	_ = s
}

func TestExportChat(t *testing.T) {
	f := setup(t, nil)
	f.api("POST", "/api/message", object{"text": "@Opus uji ekspor"}, 202)
	f.finish(2)
	status, raw, headers := f.raw("GET", "/api/export?format=json", nil, "")
	if status != 200 || headers.Get("Content-Type") == "" {
		t.Fatalf("export json %d", status)
	}
	var out object
	if json.Unmarshal(raw, &out) != nil {
		t.Fatalf("bukan JSON: %s", raw)
	}
	if len(asObjects(out["messages"])) != 2 || out["settings"] == nil {
		t.Fatal(out)
	}
	f.api("GET", "/api/export?format=pdf", nil, 400)
}

func TestDarkModeSetting(t *testing.T) {
	f := setup(t, nil)
	f.api("POST", "/api/settings", object{"settings": object{"darkMode": true}}, 200)
	if s := f.state()["settings"].(object); s["darkMode"] != true {
		t.Fatal(s)
	}
	f.api("POST", "/api/settings", object{"settings": object{"darkMode": false}}, 200)
	if s := f.state()["settings"].(object); s["darkMode"] != false {
		t.Fatal(s)
	}
}

func TestUploadImageAndChatImage(t *testing.T) {
	f := setup(t, nil)
	data, err := base64.StdEncoding.DecodeString(pixel)
	if err != nil {
		t.Fatal(err)
	}
	upload := func(name string, content []byte) (int, object) {
		var body bytes.Buffer
		w := multipart.NewWriter(&body)
		part, _ := w.CreateFormFile("image", name)
		part.Write(content)
		w.Close()
		res, err := http.Post(f.server.URL+"/api/upload-image", w.FormDataContentType(), &body)
		if err != nil {
			f.t.Fatal(err)
		}
		defer res.Body.Close()
		var out object
		json.NewDecoder(res.Body).Decode(&out)
		return res.StatusCode, out
	}
	status, out := upload("Foto Bunga.png", data)
	if status != 200 {
		t.Fatal(status, out)
	}
	imageURL := out["imageUrl"].(string)
	if !strings.HasPrefix(imageURL, "/images/") || !strings.HasSuffix(imageURL, ".png") || strings.ContainsAny(imageURL, " ") {
		t.Fatal(out)
	}
	res, raw, _ := f.raw("GET", imageURL, nil, "")
	if res != 200 || len(raw) == 0 {
		t.Fatalf("gambar tidak bisa diambil: %d", res)
	}
	// SVG ditolak, lebih dari 2 MB ditolak, tanpa file ditolak.
	status, out = upload("nakal.svg", []byte("<svg/>"))
	if status != 400 {
		t.Fatal(status, out)
	}
	var empty bytes.Buffer
	form := multipart.NewWriter(&empty)
	if err := form.Close(); err != nil {
		t.Fatal(err)
	}
	if status, raw, _ := f.raw("POST", "/api/upload-image", &empty, form.FormDataContentType()); status != 400 {
		t.Fatalf("missing image: status=%d, body=%s", status, raw)
	}
	status, out = upload("besar.png", make([]byte, 2*1024*1024+1))
	if status != 413 {
		t.Fatal(status, out)
	}
	f.api("POST", "/api/message", object{"text": "cek gambar", "imageUrl": "http://evil.example/x.png"}, 400)
	f.api("POST", "/api/message", object{"text": "cek gambar", "imageUrl": "/images/hantu.png"}, 400)

	f.api("POST", "/api/settings", object{"settings": object{"roundRobin": false}}, 200)
	f.api("POST", "/api/message", object{"text": "cek gambar", "imageUrl": imageURL}, 202)
	deadline := time.Now().Add(4 * time.Second)
	for {
		m := findMessageBy(t, f, func(m object) bool { return m["imageUrl"] == imageURL })
		if m != nil && m["imageUrl"] == imageURL {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal(f.state())
		}
		time.Sleep(7 * time.Millisecond)
	}
}
