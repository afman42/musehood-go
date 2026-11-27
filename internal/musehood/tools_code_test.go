package musehood

import (
	"archive/zip"
	"bytes"
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func codeApp(t *testing.T) *App {
	t.Helper()
	return &App{dir: t.TempDir()}
}

func runProjectTool(t *testing.T, app *App, name, arg string) string {
	t.Helper()
	out, _ := app.runToolInner(context.Background(), settings{}, seat{Code: "M01", Name: "Plamo"}, name, arg, true)
	return out
}

// runProjectToolApprove menjalankan tool yang meminta persetujuan dan
// menyetujuinya secara otomatis.
func runProjectToolApprove(t *testing.T, app *App, name, arg string) string {
	t.Helper()
	done := make(chan string, 1)
	go func() {
		out, _ := app.runToolInner(context.Background(), settings{}, seat{Code: "M01", Name: "Plamo"}, name, arg, true)
		done <- out
	}()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case out := <-done:
			return out
		case <-time.After(20 * time.Millisecond):
		}
		if pending := app.pendingApprovals(); len(pending) > 0 {
			app.decideApproval(pending[0].ID, true)
			return <-done
		}
	}
	t.Fatal("persetujuan tidak muncul")
	return ""
}

func TestProjectCreateAndFiles(t *testing.T) {
	app := codeApp(t)
	out := runProjectTool(t, app, "create_project", "demo\ngo")
	if !strings.Contains(out, "dibuat") {
		t.Fatalf("create_project = %q", out)
	}
	if out := runProjectTool(t, app, "create_project", "demo\ngo"); !strings.Contains(out, "sudah ada") {
		t.Fatalf("create_project duplikat = %q", out)
	}
	if out := runProjectTool(t, app, "create_project", "Bad Name\ngo"); !strings.Contains(out, "tidak valid") {
		t.Fatalf("create_project nama buruk = %q", out)
	}
	out = runProjectTool(t, app, "list_tree", "project:demo")
	if !strings.Contains(out, "main.go") || !strings.Contains(out, "go.mod") {
		t.Fatalf("list_tree = %q", out)
	}
	out = runProjectTool(t, app, "read_file_lines", "project:demo\nmain.go\n1\n3")
	if !strings.Contains(out, "1: package main") {
		t.Fatalf("read_file_lines = %q", out)
	}
	if out := runProjectTool(t, app, "mkdir", "project:demo\nsub/dir"); !strings.Contains(out, "dibuat") {
		t.Fatalf("mkdir = %q", out)
	}
	if out := runProjectTool(t, app, "copy_file", "project:demo\nmain.go\nsub/salinan.go"); !strings.Contains(out, "disalin") {
		t.Fatalf("copy_file = %q", out)
	}
	if out := runProjectTool(t, app, "copy_file", "project:demo\nmain.go\nsub/salinan.go"); !strings.Contains(out, "sudah ada") {
		t.Fatalf("copy_file menimpa = %q", out)
	}
	if out := runProjectTool(t, app, "move_file", "project:demo\nsub/salinan.go\nsub/pindah.go"); !strings.Contains(out, "dipindahkan") {
		t.Fatalf("move_file = %q", out)
	}
	out = runProjectTool(t, app, "search_code", "project:demo\nHalo dari")
	if !strings.Contains(out, "main.go") {
		t.Fatalf("search_code = %q", out)
	}
	out = runProjectTool(t, app, "find_files", "project:demo\n**/*.go")
	if !strings.Contains(out, "main.go") || !strings.Contains(out, "sub/pindah.go") {
		t.Fatalf("find_files = %q", out)
	}
	// Path keluar proyek ditolak.
	if out := runProjectTool(t, app, "read_file_lines", "project:demo\n../data.json\n1\n1"); !strings.Contains(out, "Error") {
		t.Fatalf("traversal harus ditolak: %q", out)
	}
	if out := runProjectTool(t, app, "read_file_lines", "project:demo\n/etc/passwd\n1\n1"); !strings.Contains(out, "Error") {
		t.Fatalf("path absolut harus ditolak: %q", out)
	}
}

func TestProjectPatchEditManySnapshot(t *testing.T) {
	app := codeApp(t)
	runProjectTool(t, app, "create_project", "patchdemo\npython")
	diff := "diff --git a/main.py b/main.py\n" +
		"--- a/main.py\n" +
		"+++ b/main.py\n" +
		"@@ -1,3 +1,3 @@\n" +
		" def main():\n" +
		"-    print(\"Halo dari patchdemo\")\n" +
		"+    print(\"Halo dunia\")\n" +
		" \n" +
		"diff --git a/baru.txt b/baru.txt\n" +
		"--- /dev/null\n" +
		"+++ b/baru.txt\n" +
		"@@ -0,0 +1,2 @@\n" +
		"+baris satu\n" +
		"+baris dua\n"
	out := runProjectTool(t, app, "apply_patch", "project:patchdemo\n"+diff)
	if !strings.Contains(out, "diterapkan") {
		t.Fatalf("apply_patch = %q", out)
	}
	raw, err := os.ReadFile(filepath.Join(app.projectDir("patchdemo"), "main.py"))
	if err != nil || !strings.Contains(string(raw), "Halo dunia") {
		t.Fatalf("main.py sesudah patch = %q err=%v", raw, err)
	}
	if _, err := os.Stat(filepath.Join(app.projectDir("patchdemo"), "baru.txt")); err != nil {
		t.Fatalf("baru.txt tidak dibuat: %v", err)
	}
	// Patch gagal (konteks tidak cocok) tidak mengubah file apa pun.
	badDiff := "--- a/main.py\n+++ b/main.py\n@@ -1,2 +1,2 @@\n def main():\n-    print(\"tidak ada\")\n+    print(\"x\")\n"
	out = runProjectTool(t, app, "apply_patch", "project:patchdemo\n"+badDiff)
	if !strings.Contains(out, "Tidak ada file yang berubah") {
		t.Fatalf("apply_patch gagal harus atomik: %q", out)
	}
	raw, _ = os.ReadFile(filepath.Join(app.projectDir("patchdemo"), "main.py"))
	if !strings.Contains(string(raw), "Halo dunia") {
		t.Fatalf("main.py berubah oleh patch gagal: %q", raw)
	}
	// edit_many
	out = runProjectTool(t, app, "edit_many", "project:patchdemo\nfile:main.py\n<<<OLD>>>\nHalo dunia\n<<<NEW>>>\nHalo edit\nfile:baru.txt\n<<<OLD>>>\nbaris satu\n<<<NEW>>>\nbaris pertama\n")
	if !strings.Contains(out, "diubah") {
		t.Fatalf("edit_many = %q", out)
	}
	raw, _ = os.ReadFile(filepath.Join(app.projectDir("patchdemo"), "baru.txt"))
	if !strings.Contains(string(raw), "baris pertama") {
		t.Fatalf("baru.txt sesudah edit_many = %q", raw)
	}
	// snapshot: list harus memuat snapshot otomatis dari patch/edit.
	out = runProjectTool(t, app, "project_snapshot", "project:patchdemo\nlist")
	if !strings.Contains(out, "auto-patch") || !strings.Contains(out, "auto-edit") {
		t.Fatalf("snapshot list = %q", out)
	}
	out = runProjectTool(t, app, "project_snapshot", "project:patchdemo\ncreate manual")
	if !strings.Contains(out, "dibuat") {
		t.Fatalf("snapshot create = %q", out)
	}
	// Ubah file lalu restore snapshot manual (dengan persetujuan).
	manualID := ""
	for _, line := range strings.Split(runProjectTool(t, app, "project_snapshot", "project:patchdemo\nlist"), "\n") {
		if strings.Contains(line, "manual") {
			manualID = strings.TrimSpace(line)
		}
	}
	if manualID == "" {
		t.Fatal("snapshot manual tidak ditemukan")
	}
	_ = os.WriteFile(filepath.Join(app.projectDir("patchdemo"), "main.py"), []byte("rusak\n"), 0o666)
	out = runProjectToolApprove(t, app, "project_snapshot", "project:patchdemo\nrestore "+manualID)
	if !strings.Contains(out, "dipulihkan") {
		t.Fatalf("snapshot restore = %q", out)
	}
	raw, _ = os.ReadFile(filepath.Join(app.projectDir("patchdemo"), "main.py"))
	if !strings.Contains(string(raw), "Halo edit") {
		t.Fatalf("main.py sesudah restore = %q", raw)
	}
	out = runProjectTool(t, app, "project_snapshot", "project:patchdemo\ndelete "+manualID)
	if !strings.Contains(out, "dihapus") {
		t.Fatalf("snapshot delete = %q", out)
	}
}

func TestProjectBuildTestLintFormat(t *testing.T) {
	if findGoToolchain() == "" {
		t.Skip("toolchain Go tidak tersedia")
	}
	app := codeApp(t)
	runProjectTool(t, app, "create_project", "builddemo\ngo")
	out := runProjectTool(t, app, "run_build", "project:builddemo")
	if !strings.Contains(out, "SUKSES") {
		t.Fatalf("run_build = %q", out)
	}
	out = runProjectTool(t, app, "run_tests", "project:builddemo")
	if !strings.Contains(out, "SUKSES") {
		t.Fatalf("run_tests = %q", out)
	}
	out = runProjectTool(t, app, "run_lint", "project:builddemo")
	if !strings.Contains(out, "go vet") {
		t.Fatalf("run_lint = %q", out)
	}
	out = runProjectTool(t, app, "format_code", "project:builddemo")
	if !strings.Contains(out, "gofmt") {
		t.Fatalf("format_code = %q", out)
	}
	// allowlist_shell: perintah aman jalan, metakarakter & di luar daftar ditolak.
	out = runProjectTool(t, app, "allowlist_shell", "project:builddemo\nls")
	if !strings.Contains(out, "main.go") {
		t.Fatalf("allowlist ls = %q", out)
	}
	if out := runProjectTool(t, app, "allowlist_shell", "project:builddemo\nls; rm -rf ."); !strings.Contains(out, "metakarakter") {
		t.Fatalf("allowlist metakarakter = %q", out)
	}
	if out := runProjectTool(t, app, "allowlist_shell", "project:builddemo\nrm main.go"); !strings.Contains(out, "allowlist") {
		t.Fatalf("allowlist rm = %q", out)
	}
	if out := runProjectTool(t, app, "allowlist_shell", "project:builddemo\ngo version"); !strings.Contains(out, "go version") {
		t.Fatalf("allowlist go version = %q", out)
	}
}

func TestProjectRunServer(t *testing.T) {
	app := codeApp(t)
	runProjectTool(t, app, "create_project", "webdemo\nweb")
	out := runProjectTool(t, app, "run_server", "project:webdemo\npython3 -m http.server 4155")
	if !strings.Contains(out, "berjalan") {
		t.Fatalf("run_server start = %q", out)
	}
	defer runProjectTool(t, app, "run_server", "project:webdemo\nstop")
	// Server harus menjawab di 127.0.0.1:4155.
	deadline := time.Now().Add(10 * time.Second)
	ok := false
	for time.Now().Before(deadline) {
		resp, err := http.Get("http://127.0.0.1:4155/")
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == 200 {
				ok = true
				break
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	if !ok {
		t.Fatal("server tidak menjawab di 127.0.0.1:4155")
	}
	out = runProjectTool(t, app, "run_server", "project:webdemo\nstatus")
	if !strings.Contains(out, "berjalan") {
		t.Fatalf("run_server status = %q", out)
	}
	if out := runProjectTool(t, app, "run_server", "project:webdemo\npython3 -m http.server 4156"); !strings.Contains(out, "sudah berjalan") {
		t.Fatalf("run_server kedua harus ditolak: %q", out)
	}
	out = runProjectTool(t, app, "run_server", "project:webdemo\nstop")
	if !strings.Contains(out, "dihentikan") {
		t.Fatalf("run_server stop = %q", out)
	}
}

func TestProjectGit(t *testing.T) {
	app := codeApp(t)
	runProjectTool(t, app, "create_project", "gitdemo\npython")
	out := runProjectToolApprove(t, app, "git_commit", "project:gitdemo\ncommit awal")
	if !strings.Contains(out, "Commit") || !strings.Contains(out, "dibuat") {
		t.Fatalf("git_commit = %q", out)
	}
	if out := runProjectTool(t, app, "git_commit", "project:gitdemo\nlagi"); !strings.Contains(out, "Tidak ada perubahan") {
		t.Fatalf("git_commit bersih = %q", out)
	}
	if out := runProjectTool(t, app, "git_branch", "project:gitdemo\ncreate fitur"); !strings.Contains(out, "dibuat") {
		t.Fatalf("git_branch create = %q", out)
	}
	out = runProjectTool(t, app, "git_branch", "project:gitdemo\nlist")
	if !strings.Contains(out, "fitur") {
		t.Fatalf("git_branch list = %q", out)
	}
	if out := runProjectTool(t, app, "git_branch", "project:gitdemo\ncheckout fitur"); !strings.Contains(out, "kini") {
		t.Fatalf("git_branch checkout = %q", out)
	}
	// Stash: ubah file, push, pop (dengan persetujuan).
	_ = os.WriteFile(filepath.Join(app.projectDir("gitdemo"), "main.py"), []byte("print('ubah')\n"), 0o666)
	if out := runProjectTool(t, app, "git_stash", "project:gitdemo\npush"); !strings.Contains(out, "Stash") {
		t.Fatalf("git_stash push = %q", out)
	}
	out = runProjectTool(t, app, "git_stash", "project:gitdemo\nlist")
	if !strings.Contains(out, "stash@") {
		t.Fatalf("git_stash list = %q", out)
	}
	out = runProjectToolApprove(t, app, "git_stash", "project:gitdemo\npop")
	if !strings.Contains(out, "diterapkan") {
		t.Fatalf("git_stash pop = %q", out)
	}
	raw, _ := os.ReadFile(filepath.Join(app.projectDir("gitdemo"), "main.py"))
	if !strings.Contains(string(raw), "ubah") {
		t.Fatalf("main.py sesudah stash pop = %q", raw)
	}
}

func TestProjectIntelligence(t *testing.T) {
	app := codeApp(t)
	runProjectTool(t, app, "create_project", "intel\ngo")
	// Tambah file dengan TODO dan fungsi ekspor.
	extra := "package main\n\n// TODO: perbaiki nanti\nfunc Hitung(a, b int) int {\n\treturn a + b\n}\n"
	if err := writeProjectFile(filepath.Join(app.projectDir("intel"), "hitung.go"), []byte(extra)); err != nil {
		t.Fatal(err)
	}
	out := runProjectTool(t, app, "outline", "project:intel\nhitung.go")
	if !strings.Contains(out, "func Hitung") {
		t.Fatalf("outline = %q", out)
	}
	out = runProjectTool(t, app, "find_references", "project:intel\nHitung")
	if !strings.Contains(out, "hitung.go") {
		t.Fatalf("find_references = %q", out)
	}
	out = runProjectTool(t, app, "dependency_graph", "project:intel")
	if !strings.Contains(out, "main.go") || !strings.Contains(out, "fmt") {
		t.Fatalf("dependency_graph = %q", out)
	}
	out = runProjectTool(t, app, "todo_scan", "project:intel")
	if !strings.Contains(out, "TODO") {
		t.Fatalf("todo_scan = %q", out)
	}
	out = runProjectTool(t, app, "complexity_report", "project:intel")
	if !strings.Contains(out, "file terbesar") {
		t.Fatalf("complexity_report = %q", out)
	}
	out = runProjectTool(t, app, "generate_tests", "project:intel\nhitung.go")
	if !strings.Contains(out, "hitung_test.go") {
		t.Fatalf("generate_tests = %q", out)
	}
	raw, err := os.ReadFile(filepath.Join(app.projectDir("intel"), "hitung_test.go"))
	if err != nil || !strings.Contains(string(raw), "TestHitung") {
		t.Fatalf("hitung_test.go = %q err=%v", raw, err)
	}
	if out := runProjectTool(t, app, "generate_tests", "project:intel\nhitung.go"); !strings.Contains(out, "sudah ada") {
		t.Fatalf("generate_tests menimpa = %q", out)
	}
}

func TestProjectExportImportEnv(t *testing.T) {
	app := codeApp(t)
	runProjectTool(t, app, "create_project", "expdemo\npython")
	out := runProjectTool(t, app, "export_project", "project:expdemo")
	if !strings.Contains(out, "/exports/expdemo-") || !strings.Contains(out, ".zip") {
		t.Fatalf("export_project = %q", out)
	}
	// Ambil nama zip dari keluaran dan pastikan file ada.
	zipName := ""
	for _, field := range strings.Fields(out) {
		if strings.HasPrefix(field, "/exports/") {
			zipName = strings.TrimPrefix(field, "/exports/")
		}
	}
	if zipName == "" || !exportNamePattern.MatchString(zipName) {
		t.Fatalf("nama ekspor tidak valid: %q", zipName)
	}
	if _, err := os.Stat(filepath.Join(app.exportsDir(), zipName)); err != nil {
		t.Fatalf("zip ekspor tidak ada: %v", err)
	}
	// Impor: salin zip ekspor ke uploads lalu impor sebagai proyek baru.
	if err := os.MkdirAll(filepath.Join(app.dir, "uploads"), 0o755); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(app.exportsDir(), zipName))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(app.dir, "uploads", zipName), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	out = runProjectTool(t, app, "import_project", "project:impor1\n"+zipName)
	if !strings.Contains(out, "diimpor") {
		t.Fatalf("import_project = %q", out)
	}
	if _, err := os.Stat(filepath.Join(app.projectDir("impor1"), "main.py")); err != nil {
		t.Fatalf("main.py hasil impor tidak ada: %v", err)
	}
	// Zip-slip ditolak.
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create("../jahat.txt")
	_, _ = w.Write([]byte("x"))
	_ = zw.Close()
	if err := os.WriteFile(filepath.Join(app.dir, "uploads", "jahat.zip"), buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	out = runProjectTool(t, app, "import_project", "project:impor2\njahat.zip")
	if !strings.Contains(out, "tidak aman") {
		t.Fatalf("zip-slip harus ditolak: %q", out)
	}
	// env_file: set/get ter-mask/list/delete + redaksi log.
	out = runProjectTool(t, app, "env_file", "project:expdemo\nset API_KEY=rahasia123456")
	if !strings.Contains(out, "raha***") || strings.Contains(out, "rahasia123456") {
		t.Fatalf("env_file set harus ter-mask: %q", out)
	}
	out = runProjectTool(t, app, "env_file", "project:expdemo\nget API_KEY")
	if out != "API_KEY=raha***" {
		t.Fatalf("env_file get = %q", out)
	}
	out = runProjectTool(t, app, "env_file", "project:expdemo\nlist")
	if !strings.Contains(out, "API_KEY") {
		t.Fatalf("env_file list = %q", out)
	}
	if got := redactToolArg("env_file", "set API_KEY=rahasia123456"); strings.Contains(got, "rahasia") {
		t.Fatalf("redactToolArg bocor: %q", got)
	}
	out = runProjectTool(t, app, "env_file", "project:expdemo\ndelete API_KEY")
	if !strings.Contains(out, "dihapus") {
		t.Fatalf("env_file delete = %q", out)
	}
}
