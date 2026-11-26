package musehood

// Tes keamanan: penjaga SSRF, path traversal sandbox, zip-slip, deteksi
// kode berbahaya, kebocoran kredensial, dan gerbang persetujuan.

import (
	"archive/zip"
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSecuritySSRFMatrix(t *testing.T) {
	ctx := context.Background()
	rejected := []string{
		"http://127.0.0.1/admin",
		"http://127.0.0.1:3001/api/state",
		"http://10.0.0.5/",
		"http://192.168.1.1/",
		"http://172.16.0.9/",
		"http://169.254.169.254/latest/meta-data",
		"http://[::1]/",
		"http://0.0.0.0/",
		"http://localhost/",
		"http://LOCALHOST:8080/",
		"ftp://example.com/file",
		"file:///etc/passwd",
		"bukan-url",
		"",
	}
	for _, raw := range rejected {
		if err := checkPublicURL(ctx, raw); err == nil {
			t.Fatalf("URL harus ditolak: %q", raw)
		}
	}
	// IP publik literal lolos (tanpa DNS).
	if err := checkPublicURL(ctx, "https://8.8.8.8/dns"); err != nil {
		t.Fatalf("IP publik harus lolos: %v", err)
	}
}

func TestSecuritySandboxTraversal(t *testing.T) {
	app := &App{dir: t.TempDir()}
	bad := []string{"../data.json", "..\\data.json", "/etc/passwd", "sub/file.txt", "..", "", "a/../b"}
	for _, name := range bad {
		if _, err := app.sandboxPath(name); err == nil {
			t.Fatalf("nama harus ditolak: %q", name)
		}
	}
	// Tool file menolak traversal dan tidak menulis apa pun di luar sandbox.
	result := app.toolWriteFile("../jahat.txt\nisi")
	if !strings.Contains(result, "Error") {
		t.Fatalf("write traversal: %s", result)
	}
	if _, err := os.Stat(filepath.Join(app.dir, "jahat.txt")); !os.IsNotExist(err) {
		t.Fatal("file traversal tidak boleh tercipta di luar sandbox")
	}
	if result := app.toolReadFile("../../data.json"); !strings.Contains(result, "Error") {
		t.Fatalf("read traversal: %s", result)
	}
	// Batas ukuran write_file.
	big := "besar.txt\n" + strings.Repeat("x", maxSandboxFileBytes+1)
	if result := app.toolWriteFile(big); !strings.Contains(result, "terlalu besar") {
		t.Fatalf("write besar: %s", result)
	}
}

func TestSecurityZipSlip(t *testing.T) {
	app := &App{dir: t.TempDir()}
	if err := os.MkdirAll(app.sandboxDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	writer := zip.NewWriter(&buf)
	for name, content := range map[string]string{
		"../jahat.txt":     "jahat",
		"../../etc/passwd": "jahat",
		"folder/aman.txt":  "aman",
		"/absolut.txt":     "absolut",
		"folder\\win.txt":  "win",
	} {
		w, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(app.sandboxDir(), "uji.zip"), buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	result := app.toolUnzipFile("uji.zip")
	if strings.Contains(result, "Error") {
		t.Fatalf("unzip: %s", result)
	}
	// Semua hasil harus rata di dalam sandbox; tidak ada yang lolos keluar.
	entries, _ := os.ReadDir(app.sandboxDir())
	for _, entry := range entries {
		if entry.IsDir() {
			t.Fatalf("unzip tidak boleh membuat folder: %s", entry.Name())
		}
	}
	if _, err := os.Stat(filepath.Join(app.dir, "jahat.txt")); !os.IsNotExist(err) {
		t.Fatal("zip-slip lolos ke luar sandbox")
	}
	if _, err := os.Stat(filepath.Join(app.sandboxDir(), "aman.txt")); err != nil {
		t.Fatalf("file aman harus terekstrak rata: %v", err)
	}
}

func TestSecurityDangerousCodeMatrix(t *testing.T) {
	dangerous := []string{
		`import os; os.system("ls")`,
		`import subprocess; subprocess.run(["ls"])`,
		`import socket; socket.socket()`,
		`import urllib.request; urllib.request.urlopen("http://x")`,
		`fetch("https://example.com")`,
		`rm -rf /`,
		`sudo apt install x`,
		`open("/etc/passwd").read()`,
		`open("/home/user/.ssh/id_rsa")`,
		`while True: pass`,
		`for(;;){}`,
		`eval("1+1")`,
		`import shutil; shutil.rmtree("x")`,
	}
	for _, code := range dangerous {
		if _, ok := dangerousCode(code); !ok {
			t.Fatalf("kode harus terdeteksi berbahaya: %q", code)
		}
	}
	safe := []string{
		`print(6*7)`,
		`import math; print(math.sqrt(144))`,
		`data = [1,2,3]; print(sum(data))`,
		`console.log("halo")`,
		`open("data.txt").read()`,
	}
	for _, code := range safe {
		if reason, ok := dangerousCode(code); ok {
			t.Fatalf("kode aman terdeteksi %q: %q", reason, code)
		}
	}
}

func TestSecurityCredentialNeverLeaks(t *testing.T) {
	f := setup(t, nil)
	// fixture.api sudah memeriksa kebocoran secret di setiap respons;
	// di sini periksa jalur mentah: export, backup, state, providers.
	for _, path := range []string{"/api/state", "/api/backup", "/api/export?format=json", "/api/export?format=html", "/api/providers", "/api/stats"} {
		_, raw := rawRequest(t, f, "GET", path, nil, nil)
		if bytes.Contains(raw, []byte(secret)) {
			t.Fatalf("secret bocor di %s", path)
		}
	}
	// Error gateway yang menggemakan secret tidak boleh diteruskan utuh.
	f.api("POST", "/api/settings", object{"seats": []object{{"code": "M01", "model": "echo"}}}, 200)
	f.api("POST", "/api/message", object{"text": "@Plamo uji"}, 202)
	f.wait(func(s object) bool {
		for _, item := range s["messages"].([]any) {
			m := item.(map[string]any)
			if m["seat"] == "M01" && m["status"] == "error" {
				return true
			}
		}
		return false
	})
}

func TestSecurityApprovalContextCancel(t *testing.T) {
	app := &App{dir: t.TempDir(), approvals: map[string]*approval{}}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan bool, 1)
	go func() {
		done <- app.requestApproval(ctx, seat{Code: "M01", Name: "Plamo"}, "shell", "", "echo halo")
	}()
	// Tunggu permintaan tercatat, lalu batalkan konteks.
	deadline := time.Now().Add(2 * time.Second)
	for {
		app.approvalMu.Lock()
		count := len(app.approvals)
		app.approvalMu.Unlock()
		if count == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("permintaan persetujuan tidak tercatat")
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	select {
	case approved := <-done:
		if approved {
			t.Fatal("konteks batal harus menolak")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("requestApproval tidak berhenti setelah konteks batal")
	}
	// Permintaan harus dibersihkan dari daftar menunggu.
	app.approvalMu.Lock()
	left := len(app.approvals)
	app.approvalMu.Unlock()
	if left != 0 {
		t.Fatalf("persetujuan tersisa: %d", left)
	}
	// Keputusan untuk ID asing tidak berpengaruh.
	app.decideApproval("tidak-ada", true)
}
