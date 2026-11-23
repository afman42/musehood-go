package musehood

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestUploadFileAndAttachment(t *testing.T) {
	f := setup(t, nil)
	upload := func(name string, content []byte) (int, object) {
		var body bytes.Buffer
		w := multipart.NewWriter(&body)
		part, _ := w.CreateFormFile("file", name)
		part.Write(content)
		w.Close()
		res, err := http.Post(f.server.URL+"/api/upload-file", w.FormDataContentType(), &body)
		if err != nil {
			f.t.Fatal(err)
		}
		defer res.Body.Close()
		var out object
		json.NewDecoder(res.Body).Decode(&out)
		return res.StatusCode, out
	}
	// Text file: happy path, original name kept, served back.
	status, out := upload("catatan penting.txt", []byte("isi catatan"))
	if status != 200 {
		t.Fatal(status, out)
	}
	att, _ := out["url"].(string)
	if !strings.HasPrefix(att, "/uploads/") || out["name"] != "catatan_penting.txt" || out["size"] != float64(11) {
		t.Fatalf("lampiran tidak sesuai: %v", out)
	}
	res, raw, _ := f.raw("GET", att, nil, "")
	if res != 200 || string(raw) != "isi catatan" {
		t.Fatalf("lampiran tidak bisa diambil: %d %q", res, raw)
	}
	// Dangerous filename is sanitized (no path traversal, no spaces).
	status, out = upload("../../etc/passwd", []byte("x"))
	if status != 200 {
		t.Fatal(status, out)
	}
	if strings.Contains(out["url"].(string), "..") || strings.Contains(out["url"].(string), " ") {
		t.Fatalf("nama file berbahaya tidak disanitasi: %v", out)
	}
	// Image upload works and is detectable as an image.
	status, out = upload("foto.png", []byte("\x89PNG\r\n\x1a\n12345678"))
	if status != 200 {
		t.Fatal(status, out)
	}
	if !strings.Contains(out["mime"].(string), "image/png") {
		t.Fatalf("mime gambar tidak terdeteksi: %v", out)
	}
	// Oversize is rejected (11 MB > 10 MB).
	status, out = upload("besar.bin", make([]byte, 11*1024*1024))
	if status != 413 {
		t.Fatal(status, out)
	}
	// Empty file is rejected.
	status, out = upload("kosong.txt", []byte{})
	if status != 400 {
		t.Fatal(status, out)
	}
	// Message with attachment only (no text) is accepted; forged URL is rejected.
	f.api("POST", "/api/settings", object{"settings": object{"roundRobin": false}}, 200)
	f.api("POST", "/api/message", object{"attachment": object{"url": att, "name": "catatan_penting.txt", "size": 11, "mime": "text/plain"}}, 202)
	f.api("POST", "/api/message", object{"text": "x", "attachment": object{"url": "/uploads/palsu.txt", "name": "palsu.txt", "size": 1, "mime": "text/plain"}}, 400)
	f.api("POST", "/api/message", object{"text": "x", "attachment": object{"url": "http://evil.example/x.txt", "name": "x.txt", "size": 1, "mime": "text/plain"}}, 400)
	deadline := time.Now().Add(4 * time.Second)
	for {
		m := findMessageBy(t, f, func(m object) bool { return m["attachment"] != nil })
		if m != nil {
			got, _ := m["attachment"].(object)
			if got["url"] == att {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatal(f.state())
		}
		time.Sleep(7 * time.Millisecond)
	}
	// sanitizeUploadName unit checks.
	if got := sanitizeUploadName("a/b\\c d.txt"); got != "b_c_d.txt" {
		t.Fatalf("sanitizeUploadName path: %q", got)
	}
	if got := sanitizeUploadName("..."); got != "file" {
		t.Fatalf("sanitizeUploadName dots: %q", got)
	}
}
