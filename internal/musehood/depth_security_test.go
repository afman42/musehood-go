package musehood

// Tes keamanan untuk fitur pendalaman: PIN/sesi/token pekerja, kebocoran
// rahasia baru (PINHash, WorkerToken), pemasangan skill, isolasi antar
// ruang untuk fork/rerun/ekspor, dan pemotongan argumen log audit tool.

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

// loginCookie memasang PIN (bila belum) dan mengembalikan header Cookie
// sesi yang sah untuk fixture.
func loginCookie(t *testing.T, f fixture, pin string) map[string]string {
	t.Helper()
	status, header, raw := doRaw(t, "POST", f.server.URL+"/api/login", object{"pin": pin}, nil)
	if status != 200 {
		t.Fatalf("login = %d %s", status, raw)
	}
	return map[string]string{"Cookie": sessionCookie(t, header)}
}

func TestSecurityPINBypassAttempts(t *testing.T) {
	f := setup(t, nil)
	base := f.server.URL
	if status, _, raw := doRaw(t, "POST", base+"/api/pin", object{"pin": "2468"}, nil); status != 200 {
		t.Fatalf("set PIN = %d %s", status, raw)
	}
	auth := loginCookie(t, f, "2468")
	cookie := auth["Cookie"]

	// Cookie dimodifikasi (tanda tangan diganti) ditolak.
	parts := strings.SplitN(cookie, "=", 2)
	value := parts[1]
	tampered := value[:len(value)-1] + "0"
	if tampered == value {
		tampered = value[:len(value)-1] + "1"
	}
	if status, _, _ := doRaw(t, "GET", base+"/api/state", nil, map[string]string{"Cookie": sessionCookieName + "=" + tampered}); status != 401 {
		t.Fatalf("cookie dimodifikasi harus 401, dapat %d", status)
	}
	// Cookie dengan masa berlaku digeser (tanpa tanda tangan baru) ditolak.
	expired := strings.SplitN(value, ".", 2)
	forged := "9999999999." + expired[1]
	if status, _, _ := doRaw(t, "GET", base+"/api/state", nil, map[string]string{"Cookie": sessionCookieName + "=" + forged}); status != 401 {
		t.Fatalf("cookie dipalsukan harus 401, dapat %d", status)
	}
	// Cookie kedaluwarsa yang ditandatangani sah ditolak.
	f.app.mu.Lock()
	pinHash := f.app.data.Settings.PINHash
	f.app.mu.Unlock()
	oldToken := makeSessionToken(pinHash, time.Now().Add(-time.Hour))
	if status, _, _ := doRaw(t, "GET", base+"/api/state", nil, map[string]string{"Cookie": sessionCookieName + "=" + oldToken}); status != 401 {
		t.Fatalf("cookie kedaluwarsa harus 401, dapat %d", status)
	}
	// Token sesi dari PIN lain (hash berbeda) ditolak.
	otherHash, err := hashPIN("9999")
	if err != nil {
		t.Fatal(err)
	}
	foreign := makeSessionToken(otherHash, time.Now().Add(time.Hour))
	if status, _, _ := doRaw(t, "GET", base+"/api/state", nil, map[string]string{"Cookie": sessionCookieName + "=" + foreign}); status != 401 {
		t.Fatalf("cookie PIN asing harus 401, dapat %d", status)
	}
	// Cookie kosong / sampah ditolak.
	for _, bad := range []string{"", "bukan-token", ".", "123.abc"} {
		if status, _, _ := doRaw(t, "GET", base+"/api/state", nil, map[string]string{"Cookie": sessionCookieName + "=" + bad}); status != 401 {
			t.Fatalf("cookie %q harus 401, dapat %d", bad, status)
		}
	}
	// Cookie sah tetap bekerja.
	if status, _, _ := doRaw(t, "GET", base+"/api/state", nil, auth); status != 200 {
		t.Fatalf("cookie sah harus 200, dapat %d", status)
	}

	// Antrean pekerja: token salah / kosong ditolak pada GET dan POST.
	tokenRaw, err := os.ReadFile(filepath.Join(f.dir, "worker-token.txt"))
	if err != nil {
		t.Fatal(err)
	}
	token := strings.TrimSpace(string(tokenRaw))
	for _, given := range []string{"", "token-salah", token + "x", strings.ToUpper(token)} {
		headers := map[string]string{}
		if given != "" {
			headers["X-Worker-Token"] = given
		}
		if status, _, _ := doRaw(t, "GET", base+"/api/queue/muse", nil, headers); status != 401 {
			t.Fatalf("GET antrean token %q harus 401, dapat %d", given, status)
		}
		if status, _, _ := doRaw(t, "POST", base+"/api/queue/muse/00000000-0000-4000-8000-000000000000", object{"text": "x"}, headers); status != 401 {
			t.Fatalf("POST antrean token %q harus 401, dapat %d", given, status)
		}
	}
	// Cookie sesi TIDAK menggantikan token pekerja untuk antrean.
	if status, _, _ := doRaw(t, "GET", base+"/api/queue/muse", nil, auth); status != 401 {
		t.Fatalf("antrean dengan cookie saja harus 401, dapat %d", status)
	}
	// Token pekerja TIDAK menggantikan cookie untuk endpoint biasa.
	if status, _, _ := doRaw(t, "GET", base+"/api/state", nil, map[string]string{"X-Worker-Token": token}); status != 401 {
		t.Fatalf("state dengan token pekerja saja harus 401, dapat %d", status)
	}
	// Token sah diterima.
	if status, _, _ := doRaw(t, "GET", base+"/api/queue/muse", nil, map[string]string{"X-Worker-Token": token}); status != 200 {
		t.Fatalf("antrean token sah harus 200, dapat %d", status)
	}
}

func TestSecurityPINAndWorkerTokenNeverLeak(t *testing.T) {
	f := setup(t, nil)
	base := f.server.URL
	if status, _, _ := doRaw(t, "POST", base+"/api/pin", object{"pin": "1357"}, nil); status != 200 {
		t.Fatal("set PIN gagal")
	}
	auth := loginCookie(t, f, "1357")

	f.app.mu.Lock()
	pinHash := f.app.data.Settings.PINHash
	workerToken := f.app.data.WorkerToken
	f.app.mu.Unlock()
	if pinHash == "" || workerToken == "" {
		t.Fatal("PINHash/WorkerToken harus terisi untuk tes kebocoran")
	}
	salt, hash, _ := strings.Cut(pinHash, ":")

	assertNoLeak := func(name string, raw []byte) {
		t.Helper()
		for _, secretValue := range []string{pinHash, salt, hash, workerToken} {
			if secretValue != "" && strings.Contains(string(raw), secretValue) {
				t.Fatalf("%s membocorkan rahasia %q", name, secretValue)
			}
		}
		if strings.Contains(string(raw), `"pinHash":"`) && !strings.Contains(string(raw), `"pinHash":""`) {
			t.Fatalf("%s memuat pinHash non-kosong", name)
		}
		if strings.Contains(string(raw), `"workerToken":"`) && !strings.Contains(string(raw), `"workerToken":""`) {
			t.Fatalf("%s memuat workerToken non-kosong", name)
		}
	}

	_, _, stateRaw := doRaw(t, "GET", base+"/api/state", nil, auth)
	assertNoLeak("state", stateRaw)
	_, _, backupRaw := doRaw(t, "GET", base+"/api/backup", nil, auth)
	assertNoLeak("backup", backupRaw)
	_, _, exportRaw := doRaw(t, "GET", base+"/api/export?format=json", nil, auth)
	assertNoLeak("ekspor json", exportRaw)
	_, _, exportMDRaw := doRaw(t, "GET", base+"/api/export?format=md", nil, auth)
	assertNoLeak("ekspor md", exportMDRaw)
	_, _, providersRaw := doRaw(t, "GET", base+"/api/providers", nil, auth)
	assertNoLeak("providers", providersRaw)
	_, _, settingsRaw := doRaw(t, "POST", base+"/api/settings", object{"settings": object{"reviewLevel": "Sedang"}}, auth)
	assertNoLeak("settings patch", settingsRaw)
	_, _, toolLogRaw := doRaw(t, "GET", base+"/api/tool-log", nil, auth)
	assertNoLeak("tool-log", toolLogRaw)
}

func TestSecurityRestoreKeepsLocalPINAndWorkerToken(t *testing.T) {
	f := setup(t, nil)
	base := f.server.URL
	if status, _, _ := doRaw(t, "POST", base+"/api/pin", object{"pin": "4321"}, nil); status != 200 {
		t.Fatal("set PIN gagal")
	}
	auth := loginCookie(t, f, "4321")
	f.app.mu.Lock()
	localPIN := f.app.data.Settings.PINHash
	localToken := f.app.data.WorkerToken
	f.app.mu.Unlock()

	// Backup sah (PIN/token dikosongkan) lalu disusupi PINHash & token asing.
	_, _, backupRaw := doRaw(t, "GET", base+"/api/backup", nil, auth)
	var backup object
	if err := json.Unmarshal(backupRaw, &backup); err != nil {
		t.Fatal(err)
	}
	foreignHash, err := hashPIN("0000")
	if err != nil {
		t.Fatal(err)
	}
	settingsMap, _ := backup["settings"].(map[string]any)
	settingsMap["pinHash"] = foreignHash
	backup["workerToken"] = "token-asing-buatan"

	status, _, raw := doRaw(t, "POST", base+"/api/restore", backup, auth)
	if status != 200 {
		t.Fatalf("restore = %d %s", status, raw)
	}
	f.app.mu.Lock()
	gotPIN := f.app.data.Settings.PINHash
	gotToken := f.app.data.WorkerToken
	f.app.mu.Unlock()
	if gotPIN != localPIN {
		t.Fatal("restore tidak boleh mengganti PIN lokal dengan PIN asing")
	}
	if gotToken != localToken {
		t.Fatal("restore tidak boleh mengganti token pekerja lokal")
	}
	// PIN lokal masih bisa login; PIN asing tidak.
	if status, _, _ := doRaw(t, "POST", base+"/api/login", object{"pin": "0000"}, nil); status != 401 {
		t.Fatalf("PIN asing harus ditolak setelah restore, dapat %d", status)
	}
	if status, _, _ := doRaw(t, "POST", base+"/api/login", object{"pin": "4321"}, nil); status != 200 {
		t.Fatalf("PIN lokal harus tetap bisa login setelah restore, dapat %d", status)
	}
	// Token pekerja lokal tetap sah; token asing ditolak.
	if status, _, _ := doRaw(t, "GET", base+"/api/queue/muse", nil, map[string]string{"X-Worker-Token": "token-asing-buatan"}); status != 401 {
		t.Fatalf("token asing harus 401, dapat %d", status)
	}
	if status, _, _ := doRaw(t, "GET", base+"/api/queue/muse", nil, map[string]string{"X-Worker-Token": localToken}); status != 200 {
		t.Fatalf("token lokal harus 200, dapat %d", status)
	}
}

func TestSecuritySkillInstallRejects(t *testing.T) {
	f := setup(t, nil)
	base := f.server.URL

	// Skema non-HTTPS ditolak sebelum jaringan (jalur fetch asli).
	if status, _, raw := doRaw(t, "POST", base+"/api/skills/install", object{"url": "http://contoh.test/skill.md"}, nil); status != 400 {
		t.Fatalf("URL http harus 400, dapat %d %s", status, raw)
	}
	// Host yang resolve ke IP privat ditolak penjaga SSRF.
	oldLookup := lookupIP
	lookupIP = func(ctx context.Context, host string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("10.1.2.3")}, nil
	}
	defer func() { lookupIP = oldLookup }()
	if status, _, raw := doRaw(t, "POST", base+"/api/skills/install", object{"url": "https://internal.test/skill.md"}, nil); status != 400 || !strings.Contains(string(raw), "SSRF") {
		t.Fatalf("URL IP privat harus 400 SSRF, dapat %d %s", status, raw)
	}
	lookupIP = func(ctx context.Context, host string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("127.0.0.1")}, nil
	}
	if status, _, _ := doRaw(t, "POST", base+"/api/skills/install", object{"url": "https://loopback.test/skill.md"}, nil); status != 400 {
		t.Fatalf("URL loopback harus 400, dapat %d", status)
	}
	lookupIP = oldLookup

	// Nama skill traversal / tidak valid ditolak (fetch di-stub ke konten lokal).
	oldFetch := skillFetchURL
	defer func() { skillFetchURL = oldFetch }()
	cases := map[string]string{
		"traversal":   "---\nname: ../jahat\ndescription: x\n---\nisi",
		"spasi":       "---\nname: nama jahat\ndescription: x\n---\nisi",
		"slash":       "---\nname: a/b\ndescription: x\n---\nisi",
		"titik awal":  "---\nname: .tersembunyi\ndescription: x\n---\nisi",
		"huruf besar": "---\nname: SKILL-BESAR\ndescription: x\n---\nisi",
	}
	for name, content := range cases {
		skillFetchURL = func(ctx context.Context, rawURL string) ([]byte, error) {
			return []byte(content), nil
		}
		status, _, raw := doRaw(t, "POST", base+"/api/skills/install", object{"url": "https://contoh.test/skill.md"}, nil)
		if name == "huruf besar" {
			// Nama dinormalisasi ke huruf kecil oleh installSkill: diterima
			// sebagai "skill-besar", bukan traversal.
			if status != 200 {
				t.Fatalf("%s: nama huruf besar harus dinormalisasi, dapat %d %s", name, status, raw)
			}
			continue
		}
		if status != 400 {
			t.Fatalf("%s: harus 400, dapat %d %s", name, status, raw)
		}
	}
	// Nama kosong di frontmatter memakai nama file sebagai cadangan, dan
	// cadangan itu tetap divalidasi: nama file ber-spasi ditolak.
	skillFetchURL = func(ctx context.Context, rawURL string) ([]byte, error) {
		return []byte("---\nname: \ndescription: x\n---\nisi"), nil
	}
	if status, _, _ := doRaw(t, "POST", base+"/api/skills/install", object{"url": "https://contoh.test/nama%20jahat.md"}, nil); status != 400 {
		t.Fatalf("nama cadangan ber-spasi harus 400, dapat %d", status)
	}
	// Tidak ada file yang lolos keluar folder skills.
	if _, err := os.Stat(filepath.Join(f.dir, "jahat")); !os.IsNotExist(err) {
		t.Fatal("traversal skill tidak boleh menulis di luar folder skills")
	}

	// SKILL.md raksasa lewat jalur git ditolak batas 200 KB.
	oldClone := skillGitClone
	defer func() { skillGitClone = oldClone }()
	skillGitClone = func(ctx context.Context, rawURL, dir string) error {
		big := "---\nname: skill-raksasa\ndescription: x\n---\n" + strings.Repeat("a", 300*1024)
		return os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(big), 0o600)
	}
	if status, _, raw := doRaw(t, "POST", base+"/api/skills/install", object{"url": "https://contoh.test/repo.git"}, nil); status != 400 || !strings.Contains(string(raw), "200 KB") {
		t.Fatalf("SKILL.md raksasa harus 400 batas ukuran, dapat %d %s", status, raw)
	}
	// Repo tanpa SKILL.md ditolak.
	skillGitClone = func(ctx context.Context, rawURL, dir string) error { return nil }
	if status, _, _ := doRaw(t, "POST", base+"/api/skills/install", object{"url": "https://contoh.test/kosong.git"}, nil); status != 400 {
		t.Fatalf("repo tanpa SKILL.md harus 400, dapat %d", status)
	}
	// URL kosong / tanpa host ditolak.
	for _, bad := range []string{"", "   ", "bukan-url", "https://"} {
		if status, _, _ := doRaw(t, "POST", base+"/api/skills/install", object{"url": bad}, nil); status != 400 {
			t.Fatalf("URL %q harus 400, dapat %d", bad, status)
		}
	}
}

func TestSecurityCrossRoomIsolation(t *testing.T) {
	f := setup(t, nil)
	// Ruang A (bawaan) berisi pesan rahasia A.
	f.api("POST", "/api/message", object{"text": "@Plamo rahasia ruang A"}, 202)
	f.wait(func(s object) bool { return completedSeatReplies(s) >= 1 })
	roomA := f.state()["activeRoomId"].(string)
	ownerA := findOwnerMessage(f.state(), "@Plamo rahasia ruang A")

	// Ruang B berisi pesan rahasia B.
	roomB := f.api("POST", "/api/rooms", object{"name": "Ruang B"}, 201)["activeRoomId"].(string)
	f.api("POST", "/api/message", object{"text": "@Plamo rahasia ruang B"}, 202)
	f.wait(func(s object) bool { return completedSeatReplies(s) >= 1 })
	ownerB := findOwnerMessage(f.state(), "@Plamo rahasia ruang B")

	// Ekspor dari ruang aktif B dengan ID pesan ruang A: tidak ditemukan,
	// isi ruang A tidak bocor.
	status, raw := rawRequest(t, f, "GET", "/api/export?format=json&message="+ownerA["id"].(string), nil, nil)
	if status != 400 || strings.Contains(string(raw), "rahasia ruang A") {
		t.Fatalf("ekspor lintas ruang harus 400 tanpa bocor, dapat %d", status)
	}
	// Ekspor penuh ruang B tidak memuat pesan ruang A.
	status, raw = rawRequest(t, f, "GET", "/api/export?format=json", nil, nil)
	if status != 200 || strings.Contains(string(raw), "rahasia ruang A") || !strings.Contains(string(raw), "rahasia ruang B") {
		t.Fatalf("ekspor ruang B bocor/salah: %d", status)
	}

	// Fork pesan ruang B: ruang fork hanya berisi pesan B, bukan A.
	out := f.api("POST", "/api/message/"+ownerB["id"].(string)+"/fork", object{}, 200)
	forkID := out["roomId"].(string)
	state := f.state()
	if state["activeRoomId"] != forkID {
		t.Fatal("ruang fork harus aktif")
	}
	for _, m := range messagesOf(state) {
		if strings.Contains(m["text"].(string), "ruang A") {
			t.Fatalf("fork ruang B membocorkan pesan ruang A: %v", m)
		}
	}

	// Rerun pesan ruang B tidak menghapus pesan ruang A.
	f.api("POST", "/api/rooms/"+roomB+"/activate", object{}, 200)
	f.api("POST", "/api/message/"+ownerB["id"].(string)+"/rerun", object{}, 202)
	f.api("POST", "/api/rooms/"+roomA+"/activate", object{}, 200)
	if findOwnerMessage(f.state(), "@Plamo rahasia ruang A") == nil {
		t.Fatal("rerun di ruang B tidak boleh menghapus pesan ruang A")
	}
	// ID tebakan yang tidak ada: fork/rerun 404, tidak membocorkan apa pun.
	f.api("POST", "/api/message/00000000-0000-4000-8000-000000000000/fork", object{}, 404)
	f.api("POST", "/api/message/00000000-0000-4000-8000-000000000000/rerun", object{}, 404)
}

func TestSecurityToolLogArgTruncation(t *testing.T) {
	f := setup(t, nil)
	s := seat{Code: "M01", Name: "Plamo"}
	longArg := strings.Repeat("é", 500) // multi-byte: pastikan potong per rune
	f.app.recordToolLog(s, "uji", longArg, "ok", 0)
	out := f.api("GET", "/api/tool-log", nil, 200)
	entries, _ := out["entries"].([]any)
	if len(entries) != 1 {
		t.Fatalf("log harus 1 entri: %v", out)
	}
	entry, _ := entries[0].(map[string]any)
	arg, _ := entry["arg"].(string)
	// truncateRunes memotong ke 200 rune lalu menambah penanda "…".
	if utf8.RuneCountInString(arg) > 201 || !strings.HasSuffix(arg, "…") {
		t.Fatalf("arg log harus terpotong 200 rune + penanda, dapat %d rune %q", utf8.RuneCountInString(arg), arg)
	}
	if !utf8.ValidString(arg) {
		t.Fatal("arg log harus UTF-8 sah setelah pemotongan")
	}
	// Argumen pendek tidak berubah.
	f.app.recordToolLog(s, "uji", "pendek", "ok", 0)
	out = f.api("GET", "/api/tool-log?limit=1", nil, 200)
	entries, _ = out["entries"].([]any)
	entry, _ = entries[0].(map[string]any)
	if entry["arg"] != "pendek" {
		t.Fatalf("arg pendek berubah: %v", entry)
	}
}
