package musehood

// E2E test menyeluruh: menjalankan alur pengguna lengkap lewat HTTP terhadap
// handler aplikasi asli (httptest) dengan gateway mock dan direktori data
// sementara. Setiap subtest memetakan satu fitur aplikasi.

import (
	"bytes"
	"encoding/base64"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func (f fixture) upload(path string, fields map[string]string, fileField, fileName string, content []byte, status int) object {
	f.t.Helper()
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	for k, v := range fields {
		if err := w.WriteField(k, v); err != nil {
			f.t.Fatal(err)
		}
	}
	if fileField != "" {
		part, err := w.CreateFormFile(fileField, fileName)
		if err != nil {
			f.t.Fatal(err)
		}
		if _, err := part.Write(content); err != nil {
			f.t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		f.t.Fatal(err)
	}
	code, raw, _ := f.raw("POST", path, &body, w.FormDataContentType())
	if code != status {
		f.t.Fatalf("upload %s wanted %d; got %d %s", path, status, code, raw)
	}
	if status >= 400 {
		return object{"raw": string(raw)}
	}
	return object{"raw": string(raw)}
}

func seatByCode(s object, code string) object {
	for _, seat := range asObjects(s["seats"]) {
		if seat["code"] == code {
			return seat
		}
	}
	return nil
}

func TestE2EAllFeatures(t *testing.T) {
	f := setup(t, nil)

	t.Run("halaman utama dan fallback SPA", func(t *testing.T) {
		code, raw, _ := f.raw("GET", "/", nil, "")
		if code != 200 || !strings.Contains(string(raw), "Musehood") {
			t.Fatalf("GET / = %d %s", code, raw)
		}
		code, _, _ = f.raw("GET", "/rute/spa/acak", nil, "")
		if code != 200 {
			t.Fatalf("SPA fallback = %d", code)
		}
		// avatar bawaan dilayani dari build frontend asli (diuji pada tes binary);
		// di sini pastikan jalur tak dikenal mengembalikan JSON, bukan HTML.
		code, raw, hdr := f.raw("GET", "/api/tidak-ada", nil, "")
		if code != 404 || !strings.Contains(hdr.Get("Content-Type"), "json") {
			t.Fatalf("api tak dikenal = %d %s %s", code, hdr.Get("Content-Type"), raw)
		}
	})

	t.Run("state awal lengkap", func(t *testing.T) {
		s := f.state()
		if len(asObjects(s["seats"])) != 7 || s["owner"].(object)["name"] != "Arman" {
			t.Fatal(s)
		}
		if s["settings"].(object)["hasApiKey"] != true {
			t.Fatal(s["settings"])
		}
	})

	t.Run("pengaturan: level review, mode gelap, demo, bergilir", func(t *testing.T) {
		f.api("POST", "/api/settings", object{"settings": object{"reviewLevel": "Ketat", "darkMode": true, "demoFallback": true, "roundRobin": false}}, 200)
		st := f.state()["settings"].(object)
		if st["reviewLevel"] != "Ketat" || st["darkMode"] != true || st["demoFallback"] != true || st["roundRobin"] != false {
			t.Fatal(st)
		}
		f.api("POST", "/api/settings", object{"settings": object{"reviewLevel": "Ngawur"}}, 400)
		f.api("POST", "/api/settings", object{"settings": object{"gatewayProtocol": "ngawur"}}, 400)
		// kembalikan ke bawaan untuk subtest berikutnya
		f.api("POST", "/api/settings", object{"settings": object{"reviewLevel": "Sedang", "darkMode": false, "demoFallback": false, "roundRobin": true}}, 200)
	})

	t.Run("kustomisasi kursi: peran, provider, model, prompt, integrator", func(t *testing.T) {
		f.api("POST", "/api/settings", object{
			"seats":    []object{{"code": "MUSE", "role": "Koordinator Uji", "provider": "gateway", "model": "sse", "prompt": "Prompt kustom uji"}},
			"settings": object{"integratorSeat": "M10"},
		}, 200)
		s := f.state()
		muse := seatByCode(s, "MUSE")
		if muse["role"] != "Koordinator Uji" || muse["provider"] != "gateway" || muse["model"] != "sse" || muse["prompt"] != "Prompt kustom uji" {
			t.Fatal(muse)
		}
		if s["settings"].(object)["integratorSeat"] != "M10" {
			t.Fatal(s["settings"])
		}
		f.api("POST", "/api/settings", object{"settings": object{"integratorSeat": "TIDAK-ADA"}}, 400)
		f.api("POST", "/api/settings", object{"seats": []object{{"code": "MUSE", "provider": "ngawur"}}}, 400)
		// pulihkan MUSE ke antrean muse dan integrator bawaan
		f.api("POST", "/api/settings", object{
			"seats":    []object{{"code": "MUSE", "role": "Admin dan Integrasi", "provider": "muse", "model": "", "prompt": ""}},
			"settings": object{"integratorSeat": "MUSE"},
		}, 200)
	})

	t.Run("CRUD provider gateway + alias + fetch/test models", func(t *testing.T) {
		gw := f.state()["settings"].(object)["baseUrl"].(string)
		created := f.api("POST", "/api/providers", object{"name": "Kedua", "baseUrl": gw, "apiKey": secret, "protocol": "openai", "alias": "kd"}, 201)
		id := created["id"].(string)
		if created["alias"] != "kd" || created["hasApiKey"] != true || created["active"] != false {
			t.Fatal(created)
		}
		f.api("POST", "/api/providers", object{"name": "AliasBuruk", "baseUrl": gw, "apiKey": "k", "protocol": "openai", "alias": "a/b"}, 400)
		list := f.api("GET", "/api/providers", nil, 200)
		if len(asObjects(list["providers"])) != 2 {
			t.Fatal(list)
		}
		models := f.api("GET", "/api/providers/"+id+"/models", nil, 200)
		if len(models["models"].([]any)) == 0 {
			t.Fatal(models)
		}
		tested := f.api("POST", "/api/providers/"+id+"/test-models", object{"models": []string{"sse", "denied"}}, 200)
		results := asObjects(tested["results"])
		if len(results) != 2 || results[0]["ok"] != true || results[1]["ok"] != false {
			t.Fatal(tested)
		}
		// update dengan key kosong mempertahankan key lama
		f.api("POST", "/api/providers/"+id, object{"name": "Kedua Baru", "baseUrl": gw, "protocol": "openai", "alias": "kb"}, 200)
		f.api("POST", "/api/providers/"+id+"/activate", object{}, 200)
		if f.state()["settings"].(object)["providerName"] != "Kedua Baru" {
			t.Fatal(f.state()["settings"])
		}
		f.api("DELETE", "/api/providers/"+id, nil, 200)
		// provider pertama kembali aktif
		if f.state()["settings"].(object)["providerName"] == "Kedua Baru" {
			t.Fatal("provider terhapus masih aktif")
		}
	})

	t.Run("tes koneksi dan tes model", func(t *testing.T) {
		out := f.api("POST", "/api/test-connection", object{}, 200)
		if len(out["models"].([]any)) == 0 || out["testedAt"] == nil {
			t.Fatal(out)
		}
		ok := f.api("POST", "/api/test-model", object{"model": "sse"}, 200)
		if ok["ok"] != true || ok["reply"] == nil {
			t.Fatal(ok)
		}
		// error provider diteruskan apa adanya (401 dari gateway mock)
		f.api("POST", "/api/test-model", object{"model": "denied"}, 401)
		f.api("POST", "/api/test-model", object{"model": "  "}, 400)
	})

	t.Run("chat biasa: diskusi bergilir semua kursi", func(t *testing.T) {
		// roundRobin aktif: pesan biasa dijawab semua kursi berurutan,
		// Muse (antrean pekerja) pertama.
		f.api("POST", "/api/message", object{"text": "halo semua"}, 202)
		q := f.pending("chat")
		if q["seat"] != "MUSE" {
			t.Fatal(q)
		}
		f.api("POST", "/api/queue/muse/"+q["id"].(string), object{"text": "Halo dari Muse"}, 200)
		m := f.finish(8)
		for i, code := range []string{"MUSE", "M10", "M01", "M02", "M03", "M04", "M05"} {
			if m[i+1]["seat"] != code || m[i+1]["text"] == "" {
				t.Fatal(m)
			}
		}
	})

	t.Run("mention mengarahkan ke kursi tertentu", func(t *testing.T) {
		f.api("POST", "/api/message", object{"text": "@Plamo kerjakan ini"}, 202)
		m := f.finish(10)
		if m[9]["seat"] != "M01" {
			t.Fatal(m)
		}
	})

	t.Run("antrean Muse: tanpa bergilir, pekerja menjawab lewat API", func(t *testing.T) {
		f.api("POST", "/api/settings", object{"settings": object{"roundRobin": false}}, 200)
		f.api("POST", "/api/message", object{"text": "halo muse saja"}, 202)
		q := f.pending("chat")
		f.api("POST", "/api/queue/muse/"+q["id"].(string), object{"text": "Halo dari Muse"}, 200)
		m := f.finish(12)
		if m[11]["text"] != "Halo dari Muse" || m[11]["seat"] != "MUSE" {
			t.Fatal(m)
		}
		f.api("POST", "/api/settings", object{"settings": object{"roundRobin": true}}, 200)
	})

	t.Run("pipeline Tugas enam tahap + progres", func(t *testing.T) {
		f.api("POST", "/api/message", object{"text": "Tugas: Buat rencana piknik"}, 202)
		q := f.pending("summary")
		respFile := filepath.Join(f.dir, "queue", "responses", q["id"].(string)+".json")
		if err := os.WriteFile(respFile, []byte(`{"text":"Hasil piknik"}`), 0600); err != nil {
			t.Fatal(err)
		}
		m := f.finish(19)
		for i, code := range []string{"M10", "M01", "M04", "M03", "M02", "MUSE"} {
			if m[13+i]["seat"] != code {
				t.Fatal(m)
			}
		}
		if m[18]["label"] != "Hasil Akhir - Integrasi" || m[18]["text"] != "Hasil piknik" {
			t.Fatal(m[18])
		}
		f.checkImage(m[16]["imageUrl"])
		f.api("POST", "/api/progress", object{}, 202)
		pq := f.pending("progress")
		f.api("POST", "/api/queue/muse/"+pq["id"].(string), object{"text": "Selesai semua"}, 200)
		m = f.finish(21)
		if m[20]["text"] != "Selesai semua" {
			t.Fatal(m)
		}
	})

	t.Run("Gambar: menghasilkan gambar asli", func(t *testing.T) {
		f.api("POST", "/api/message", object{"text": "Gambar: kucing di taman"}, 202)
		m := f.finish(23)
		f.checkImage(m[22]["imageUrl"])
	})

	t.Run("operasi pesan: edit, hapus, balas, reaksi", func(t *testing.T) {
		f.api("POST", "/api/settings", object{"settings": object{"roundRobin": false}}, 200)
		f.api("POST", "/api/message", object{"text": "pesan untuk diedit"}, 202)
		q := f.pending("chat")
		f.api("POST", "/api/queue/muse/"+q["id"].(string), object{"text": "balasan kursi"}, 200)
		m := f.finish(25)
		ownerID := m[23]["id"].(string)
		seatID := m[24]["id"].(string)
		// Edit menjalankan ulang diskusi (fitur 13): balasan lama hilang,
		// pekerjaan baru menunggu antrean Muse.
		f.api("POST", "/api/message/"+ownerID+"/edit", object{"text": "sudah diedit"}, 202)
		q = f.pending("chat")
		f.api("POST", "/api/queue/muse/"+q["id"].(string), object{"text": "balasan kursi"}, 200)
		m = f.finish(25)
		seatID = m[24]["id"].(string)
		f.api("POST", "/api/message/"+seatID+"/edit", object{"text": "tidak boleh"}, 403)
		f.api("POST", "/api/message/"+ownerID+"/edit", object{"text": "  "}, 400)
		f.api("POST", "/api/message/"+ownerID+"/react", object{"emoji": "👍"}, 200)
		f.api("POST", "/api/message/"+ownerID+"/react", object{"emoji": "💥"}, 400)
		f.api("POST", "/api/message", object{"text": "balasan", "replyTo": ownerID}, 202)
		q = f.pending("chat")
		f.api("POST", "/api/queue/muse/"+q["id"].(string), object{"text": "balasan kedua"}, 200)
		m = f.finish(27)
		if m[25]["replyTo"] != ownerID {
			t.Fatal(m[25])
		}
		f.api("POST", "/api/message", object{"text": "balas hantu", "replyTo": "tidak-ada"}, 400)
		f.api("POST", "/api/message/"+ownerID+"/delete", object{}, 200)
		s := f.state()
		for _, msg := range messages(s) {
			if msg["id"] == ownerID {
				t.Fatal("pesan terhapus masih ada")
			}
		}
		f.api("POST", "/api/settings", object{"settings": object{"roundRobin": true}}, 200)
	})

	t.Run("unggah avatar, gambar, dan file", func(t *testing.T) {
		png, err := base64.StdEncoding.DecodeString(pixel)
		if err != nil {
			t.Fatal(err)
		}
		out := f.upload("/api/upload-avatar", map[string]string{"target": "owner"}, "avatar", "a.png", png, 200)
		if !strings.Contains(out["raw"].(string), "/avatars/") {
			t.Fatal(out)
		}
		f.upload("/api/upload-avatar", map[string]string{"target": "owner"}, "avatar", "a.txt", []byte("bukan gambar"), 400)
		out = f.upload("/api/upload-image", nil, "image", "g.png", png, 200)
		if !strings.Contains(out["raw"].(string), "/images/") {
			t.Fatal(out)
		}
		f.upload("/api/upload-image", nil, "image", "g.svg", []byte("<svg/>"), 400)
		out = f.upload("/api/upload-file", nil, "file", "dokumen.txt", []byte("isi dokumen"), 200)
		if !strings.Contains(out["raw"].(string), "/uploads/") {
			t.Fatal(out)
		}
	})

	t.Run("kursi kustom: buat dan hapus", func(t *testing.T) {
		created := f.api("POST", "/api/seats", object{"name": "Uji", "role": "Penguji", "provider": "gateway", "model": "sse"}, 201)
		code := created["code"].(string)
		if seatByCode(f.state(), code) == nil {
			t.Fatal(created)
		}
		f.api("DELETE", "/api/seats/MUSE", nil, 400)
		f.api("DELETE", "/api/seats/"+code, nil, 200)
		if seatByCode(f.state(), code) != nil {
			t.Fatal("kursi kustom masih ada")
		}
	})

	t.Run("ekspor JSON dan Markdown tanpa key", func(t *testing.T) {
		code, raw, _ := f.raw("GET", "/api/export?format=json", nil, "")
		if code != 200 || strings.Contains(string(raw), "fixture-key") {
			t.Fatalf("export json = %d", code)
		}
		code, raw, _ = f.raw("GET", "/api/export?format=md", nil, "")
		if code != 200 || !strings.Contains(string(raw), "Hasil Akhir - Integrasi") || !strings.Contains(string(raw), "Arman") {
			t.Fatalf("export md = %d %s", code, raw)
		}
		f.api("GET", "/api/export?format=pdf", nil, 400)
	})

	t.Run("keamanan: host, origin, metode, JSON rusak", func(t *testing.T) {
		req, _ := http.NewRequest("GET", f.server.URL+"/api/state", nil)
		req.Host = "jahat.example"
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != 403 {
			t.Fatalf("host jahat = %d", res.StatusCode)
		}
		code, _, _ := f.raw("PUT", "/api/state", nil, "")
		if code != 405 {
			t.Fatalf("PUT state = %d", code)
		}
		code, raw, _ := f.raw("POST", "/api/message", strings.NewReader("{rusak"), "application/json")
		if code != 400 || !strings.Contains(string(raw), "tidak valid") {
			t.Fatalf("json rusak = %d %s", code, raw)
		}
	})

	t.Run("bersihkan chat mempertahankan konfigurasi", func(t *testing.T) {
		f.api("POST", "/api/clear", object{}, 200)
		s := f.state()
		if len(messages(s)) != 0 {
			t.Fatal(s["messages"])
		}
		if s["settings"].(object)["hasApiKey"] != true || len(asObjects(s["seats"])) != 7 {
			t.Fatal(s)
		}
	})
}
