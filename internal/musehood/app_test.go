package musehood

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

const secret = `fixture-key-"\\1234`
const pixel = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR4nGMQyQ75DwADPQHTHNReowAAAABJRU5ErkJggg=="
const denied = `{"error":{"message":"invalid model credentials"}}`

type object = map[string]any

type fixture struct {
	t      *testing.T
	server *httptest.Server
	dir    string
	app    *App
}

func setup(t *testing.T, gatewayExtra http.HandlerFunc) fixture {
	t.Helper()
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+secret {
			http.Error(w, "wrong authorization", 401)
			return
		}
		if gatewayExtra != nil {
			gatewayExtra(w, r)
			return
		}
		switch r.Method + " " + r.URL.Path {
		case "GET /v1/models":
			io.WriteString(w, `{"data":[{"id":"sse"},"array","plain","delta","denied","echo"]}`)
		case "POST /v1/chat/completions":
			var body struct {
				Model    string
				Stream   *bool
				Messages []struct{ Content string }
			}
			json.NewDecoder(r.Body).Decode(&body)
			if body.Stream == nil || *body.Stream || len(body.Messages) < 2 {
				http.Error(w, "bad request", 400)
				return
			}
			switch body.Model {
			case "sse":
				w.Header().Set("Content-Type", "text/event-stream")
				io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"Halo \"}}]}\r\n\r\ndata: {\"choices\":[{\"delta\":{\"content\":[{\"text\":\"dunia\"}]}}]}\r\n\r\ndata: [DONE]\r\n\r\n")
			case "array":
				io.WriteString(w, `{"choices":[{"message":{"content":[{"text":"bagian "},{"text":{"value":"teks"}}]}}]}`)
			case "plain":
				io.WriteString(w, "teks polos")
			case "delta":
				io.WriteString(w, `{"delta":{"text":"delta langsung"}}`)
			case "denied":
				w.WriteHeader(401)
				io.WriteString(w, denied)
			case "echo":
				w.WriteHeader(401)
				json.NewEncoder(w).Encode(object{"error": secret})
			default:
				fmt.Fprintf(w, "balasan %s", body.Model)
			}
		case "POST /v1/images/generations":
			var body struct{ Model, Prompt string }
			json.NewDecoder(r.Body).Decode(&body)
			if !strings.Contains(body.Prompt, "Peran: Visual.") {
				http.Error(w, "missing visual prompt", 400)
				return
			}
			if body.Model == "denied" {
				w.WriteHeader(401)
				io.WriteString(w, denied)
				return
			}
			fmt.Fprintf(w, `{"data":[{"b64_json":"%s"}]}`, pixel)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(gateway.Close)
	dir := t.TempDir()
	app, err := New(Options{DataDir: dir, Frontend: fstest.MapFS{"index.html": {Data: []byte("<!doctype html>Musehood")}}, PollInterval: 5 * time.Millisecond, MuseWait: 150 * time.Millisecond, TrustedHosts: "muse.test"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(app.Close)
	server := httptest.NewServer(app)
	t.Cleanup(server.Close)
	f := fixture{t: t, server: server, dir: dir, app: app}
	f.api("POST", "/api/settings", object{"settings": object{"baseUrl": gateway.URL + "/v1/", "apiKey": secret, "streamingEnabled": false}, "seats": []object{{"code": "M10", "model": "sse"}, {"code": "M01", "model": "array"}, {"code": "M02", "model": "plain"}, {"code": "M04", "model": "missing-from-list"}, {"code": "M03", "model": "image"}, {"code": "M05", "model": "delta"}}}, 200)
	return f
}
func (f fixture) api(method, path string, body any, status int) object {
	f.t.Helper()
	var data io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			f.t.Fatal(err)
		}
		data = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, f.server.URL+path, data)
	if err != nil {
		f.t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
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
		f.t.Fatal("credential leaked")
	}
	var out object
	if res.StatusCode != status || json.Unmarshal(raw, &out) != nil {
		f.t.Fatalf("%s %s wanted %d; got %d %s", method, path, status, res.StatusCode, raw)
	}
	if status >= 400 && out["error"] == nil {
		f.t.Fatal(out)
	}
	return out
}
func (f fixture) state() object { return f.api("GET", "/api/state", nil, 200) }
func (f fixture) wait(match func(object) bool) object {
	f.t.Helper()
	deadline := time.Now().Add(4 * time.Second)
	for {
		s := f.state()
		if match(s) {
			return s
		}
		if time.Now().After(deadline) {
			f.t.Fatalf("state condition timed out: %v", s)
		}
		time.Sleep(7 * time.Millisecond)
	}
}
func asObjects(value any) []object {
	items, _ := value.([]any)
	out := make([]object, 0, len(items))
	for _, v := range items {
		out = append(out, v.(object))
	}
	return out
}
func messages(s object) []object { return asObjects(s["messages"]) }
func (f fixture) finish(count int) []object {
	f.t.Helper()
	return messages(f.wait(func(s object) bool {
		m := messages(s)
		if len(m) != count {
			return false
		}
		for _, j := range asObjects(s["jobs"]) {
			if j["status"] != "completed" {
				return false
			}
		}
		return true
	}))
}
func (f fixture) pending(kind string) object {
	f.t.Helper()
	deadline := time.Now().Add(4 * time.Second)
	for {
		for _, q := range asObjects(f.api("GET", "/api/queue/muse", nil, 200)["requests"]) {
			if q["kind"] == kind {
				return q
			}
		}
		if time.Now().After(deadline) {
			f.t.Fatalf("queue %q not found", kind)
		}
		time.Sleep(7 * time.Millisecond)
	}
}
func TestConnectionAndSafeSettings(t *testing.T) {
	f := setup(t, nil)
	s := f.state()
	if !reflect.DeepEqual(s["presence"], object{"present": float64(8), "total": float64(8)}) {
		t.Fatal(s)
	}
	if settings := s["settings"].(object); settings["apiKey"] != "••••••••1234" || settings["reviewLevel"] != "Sedang" || settings["demoFallback"] != false || settings["roundRobin"] != true {
		t.Fatal(settings)
	}
	if _, ok := asObjects(s["seats"])[0]["model"]; ok {
		t.Fatal("Muse has model field")
	}
	out := f.api("POST", "/api/test-connection", object{}, 200)
	seats := asObjects(out["seats"])
	if seats[1]["available"] != true || seats[5]["available"] != false || out["testedAt"] == nil {
		t.Fatal(out)
	}
	f.api("POST", "/api/settings", object{"settings": object{"reviewLevel": "invalid"}}, 400)
	f.api("POST", "/api/settings", object{"seats": []object{{"code": "M10", "name": " "}}}, 400)
	f.api("POST", "/api/settings", object{"owner": object{"name": "Bima"}, "settings": object{"clearApiKey": true}}, 200)
	s = f.state()
	if s["connection"] != nil || s["settings"].(object)["hasApiKey"] != false || s["owner"].(object)["name"] != "Bima" {
		t.Fatal(s)
	}
	info, err := os.Stat(filepath.Join(f.dir, "data.json"))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal(info, err)
	}
}
func TestMentionFormatsAndProviderError(t *testing.T) {
	cases := []struct{ name, mention, code, text, providerError, model string }{
		{name: "sse", mention: "@Opus", code: "M10", text: "Halo dunia"},
		{name: "array", mention: "@M01", code: "M01", text: "bagian teks"},
		{name: "plain", mention: "@GLM", code: "M02", text: "teks polos"},
		{name: "delta", mention: "@Fable", code: "M05", text: "delta langsung"},
		{name: "provider_error", mention: "@Opus", code: "M10", providerError: "HTTP 401: " + denied, model: "denied"},
		{name: "redacted_provider_error", mention: "@Opus", code: "M10", providerError: "HTTP 401: {\"error\":\"[RAHASIA DISEMBUNYIKAN]\"}\n", model: "echo"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := setup(t, nil)
			if c.model != "" {
				f.api("POST", "/api/settings", object{"seats": []object{{"code": "M10", "model": c.model}}}, 200)
			}
			f.api("POST", "/api/message", object{"text": c.mention + " uji"}, 202)
			m := f.finish(2)[1]
			if m["seat"] != c.code || m["text"] != c.text || (c.providerError != "" && m["error"] != c.providerError) || m["demo"] == true {
				t.Fatal(m)
			}
		})
	}
	t.Run("demo_fallback", func(t *testing.T) {
		f := setup(t, nil)
		f.api("POST", "/api/settings", object{"settings": object{"demoFallback": true}, "seats": []object{{"code": "M10", "model": "echo"}}}, 200)
		f.api("POST", "/api/message", object{"text": "@Opus demo"}, 202)
		m := f.finish(2)[1]
		if m["demo"] != true || !strings.HasPrefix(m["text"].(string), "DEMO") || m["error"] == nil {
			t.Fatal(m)
		}
	})
}
func TestTaskPipelineAndProgress(t *testing.T) {
	f := setup(t, nil)
	f.api("POST", "/api/progress", object{}, 409)
	f.api("POST", "/api/message", object{"text": "Tugas: Rancang taman"}, 202)
	q := f.pending("summary")
	ctx := q["context"].(object)
	if q["seat"] != "MUSE" || q["createdAt"] == nil || !strings.Contains(q["prompt"].(string), "Nama pemilik: Arman") || len(asObjects(ctx["messages"])) != 6 {
		t.Fatal(q)
	}
	reqFile := filepath.Join(f.dir, "queue", "requests", q["id"].(string)+".json")
	raw, err := os.ReadFile(reqFile)
	if err != nil || !json.Valid(raw) || bytes.Contains(raw, []byte("fixture-key")) {
		t.Fatal("invalid request file", err)
	}
	respFile := filepath.Join(f.dir, "queue", "responses", filepath.Base(reqFile))
	if err := os.WriteFile(respFile, []byte(`{"text":"Ringkasan dari pekerja"}`), 0600); err != nil {
		t.Fatal(err)
	}
	m := f.finish(7)
	for i, code := range []string{"M10", "M01", "M04", "M03", "M02", "MUSE"} {
		if m[i+1]["seat"] != code {
			t.Fatal(m)
		}
	}
	if m[6]["label"] != "Hasil Akhir - Integrasi" || m[6]["text"] != "Ringkasan dari pekerja" || m[6]["demo"] == true {
		t.Fatal(m[6])
	}
	f.checkImage(m[4]["imageUrl"])
	f.api("POST", "/api/progress", object{}, 202)
	q = f.pending("progress")
	task := q["context"].(object)["activeTask"].(object)
	if task["title"] != "Rancang taman" || task["lastCompleted"] != "MUSE" || task["status"] != "completed" {
		t.Fatal(task)
	}
	id := q["id"].(string)
	f.api("POST", "/api/queue/muse/"+id, object{"text": "Sudah selesai"}, 200)
	if m = f.finish(9); m[8]["text"] != "Sudah selesai" {
		t.Fatal(m)
	}
	f.api("POST", "/api/queue/muse/"+id, object{"text": "lama"}, 404)
	f.api("POST", "/api/clear", object{}, 200)
	s := f.state()
	if len(messages(s)) != 0 || s["activeTask"] != nil || s["settings"].(object)["hasApiKey"] != true {
		t.Fatal(s)
	}
}
func (f fixture) checkImage(value any) {
	f.t.Helper()
	path, ok := value.(string)
	if !ok || !strings.HasPrefix(path, "/images/") {
		f.t.Fatal(value)
	}
	res, err := http.Get(f.server.URL + path)
	if err != nil {
		f.t.Fatal(err)
	}
	defer res.Body.Close()
	img, err := png.Decode(res.Body)
	if res.StatusCode != 200 || err != nil || img.Bounds().Dx() != 1 || img.Bounds().Dy() != 1 {
		f.t.Fatal(res.Status, err)
	}
}
func TestImageAndFallback(t *testing.T) {
	f := setup(t, nil)
	f.api("POST", "/api/message", object{"text": "Gambar: bunga"}, 202)
	f.checkImage(f.finish(2)[1]["imageUrl"])
	f.api("POST", "/api/settings", object{"seats": []object{{"code": "M03", "model": "denied"}}, "settings": object{"demoFallback": true}}, 200)
	f.api("POST", "/api/message", object{"text": "Gambar: cadangan"}, 202)
	q := f.pending("image")
	if q["seat"] != "M03" {
		t.Fatal(q)
	}
	path := "/api/queue/muse/" + q["id"].(string)
	f.api("POST", path, object{"text": "tidak ada gambar"}, 400)
	f.api("POST", path, object{"text": "foto pekerja", "imageUrl": "data:image/png;base64," + pixel}, 200)
	m := f.finish(4)[3]
	if m["error"] != "HTTP 401: "+denied || m["text"] != "foto pekerja" || m["demo"] == true {
		t.Fatal(m)
	}
	f.checkImage(m["imageUrl"])
}
func TestMuseTimeoutAndRoundRobin(t *testing.T) {
	f := setup(t, nil)
	f.api("POST", "/api/message", object{"text": "Halo grup"}, 202)
	q := f.pending("chat")
	if q["seat"] != "MUSE" {
		t.Fatal(q)
	}
	s := f.wait(func(s object) bool { m := messages(s); return len(m) == 2 && m[1]["status"] == "waiting" })
	if messages(s)[1]["statusText"] != "Muse belum menjawab — antrean masih menunggu" || messages(s)[1]["text"] != "" {
		t.Fatal(s)
	}
	f.api("POST", "/api/queue/muse/"+q["id"].(string), object{"text": "Respons terlambat"}, 200)
	m := f.finish(8)
	for i, code := range []string{"MUSE", "M10", "M01", "M02", "M03", "M04", "M05"} {
		if m[i+1]["seat"] != code {
			t.Fatal(m)
		}
	}
	f.api("POST", "/api/settings", object{"settings": object{"roundRobin": false}}, 200)
	f.api("POST", "/api/message", object{"text": "Halo lagi"}, 202)
	q = f.pending("chat")
	f.api("POST", "/api/queue/muse/"+q["id"].(string), object{"error": "worker failed"}, 200)
	if m = f.finish(10); m[9]["error"] != "worker failed" || m[9]["seat"] != "MUSE" {
		t.Fatal(m)
	}
}
func TestHostAPIAndAvatars(t *testing.T) {
	f := setup(t, nil)
	for _, c := range []struct {
		host, origin, site, method, path string
		status                           int
	}{
		{"", "", "", "GET", "/rute/spa", 200}, {"bad.test", "", "", "GET", "/api/state", 403},
		{"muse.test", "https://muse.test", "same-origin", "POST", "/api/clear", 200},
		{"[::1]:3001", "", "", "GET", "/api/state", 200},
		{"muse.test", "https://other.test", "", "GET", "/api/state", 403}, {"muse.test", "", "cross-site", "POST", "/api/clear", 403},
		{"muse.test", "", "cross-site", "GET", "/rute/spa", 200}, {"muse.test", "", "cross-site", "GET", "/api/state", 403},
	} {
		req, _ := http.NewRequest(c.method, f.server.URL+c.path, nil)
		if c.host != "" {
			req.Host = c.host
		}
		req.Header.Set("Origin", c.origin)
		req.Header.Set("Sec-Fetch-Site", c.site)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if res.StatusCode != c.status || c.path == "/rute/spa" && !strings.Contains(string(b), "Musehood") {
			t.Fatal(c, res.Status, string(b))
		}
	}
	f.api("POST", "/api/message", object{"text": "  "}, 400)
	f.api("GET", "/api/unknown", nil, 404)
	f.api("GET", "/api/message", nil, 405)
	data, err := base64.StdEncoding.DecodeString(pixel)
	if err != nil {
		t.Fatal(err)
	}
	upload := func(target string, data []byte, status int) object {
		var body bytes.Buffer
		w := multipart.NewWriter(&body)
		w.WriteField("target", target)
		part, _ := w.CreateFormFile("avatar", "../../unsafe.svg")
		part.Write(data)
		w.Close()
		res, err := http.Post(f.server.URL+"/api/upload-avatar", w.FormDataContentType(), &body)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		var out object
		json.NewDecoder(res.Body).Decode(&out)
		if res.StatusCode != status {
			t.Fatal(res.Status, out)
		}
		return out
	}
	for _, target := range []string{"owner", "group", "MUSE", "M10", "M01", "M02", "M03", "M04", "M05"} {
		out := upload(target, data, 200)
		path := out["avatar"].(string)
		if !strings.HasPrefix(path, "/avatars/") || !strings.HasSuffix(path, ".png") {
			t.Fatal(out)
		}
		res, err := http.Get(f.server.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		_, err = png.Decode(res.Body)
		res.Body.Close()
		if err != nil || res.StatusCode != 200 {
			t.Fatal(res.Status, err)
		}
	}
	upload("owner", []byte("<svg/>"), 400)
	upload("owner", make([]byte, 2*1024*1024+1), 413)
}
func TestClearDuringGatewayCall(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	f := setup(t, func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-release
		io.WriteString(w, "hasil lama")
	})
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	f.api("POST", "/api/message", object{"text": "@Opus panggilan lambat"}, 202)
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("gateway call did not start")
	}
	f.api("POST", "/api/clear", object{}, 200)
	close(release)
	f.api("POST", "/api/message", object{"text": "@Muse sesudah clear"}, 202)
	q := f.pending("chat")
	f.api("POST", "/api/queue/muse/"+q["id"].(string), object{"text": "hasil baru"}, 200)
	if m := f.finish(2); m[1]["text"] != "hasil baru" {
		t.Fatal(m)
	}
}
func TestParseCompletionEdges(t *testing.T) {
	for _, c := range []struct {
		body, want string
		fail       bool
	}{
		{"\ufeff data: {\"delta\":\r\ndata: {\"text\":\"multi\"}}\r\n\r\ndata: [DONE]", "multi", false},
		{"data: {\"delta\":\"satu\"}\ndata: {\"delta\":\" dua\"}\ndata: [DONE]", "satu dua", false},
		{"data: {\"error\":{\"message\":\"stream error\"}}", "", true},
		{`{"choices":[{"message":{"content":[]}}]}`, "", true}, {`"teks JSON"`, "teks JSON", false}, {"", "", true},
	} {
		got, err := parseCompletion(c.body)
		if got != c.want || (err != nil) != c.fail {
			t.Errorf("%q => %q, %v", c.body, got, err)
		}
	}
}
