package musehood

import (
	"bytes"
	"cmp"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// Tool proyek coding: proyek bernama di <dataDir>/projects/<nama>/.
// Semua tool proyek menerima baris pertama opsional "project:<nama>";
// tanpa itu dipakai proyek "default". Path di dalam proyek selalu
// relatif dan tidak boleh keluar dari folder proyek.
var projectNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,40}$`)

const defaultProjectName = "default"
const maxProjectFileBytes = 1024 * 1024

// projectIgnoreDirs adalah folder yang diabaikan saat berjalan menyusuri
// proyek (daftar, pencarian, snapshot, ekspor).
var projectIgnoreDirs = map[string]bool{
	"node_modules": true, ".git": true, "vendor": true, "dist": true,
	".gocache": true, ".gomodcache": true, "__pycache__": true,
}

func (a *App) projectsDir() string {
	return filepath.Join(a.dir, "projects")
}

func (a *App) projectDir(name string) string {
	return filepath.Join(a.projectsDir(), name)
}

// parseProjectArg memisahkan baris pertama "project:<nama>" (opsional)
// dari argumen tool proyek. Mengembalikan nama proyek dan sisa argumen.
func parseProjectArg(arg string) (string, string) {
	first, rest, found := strings.Cut(arg, "\n")
	trimmed := strings.TrimSpace(first)
	if strings.HasPrefix(trimmed, "project:") {
		name := strings.TrimSpace(strings.TrimPrefix(trimmed, "project:"))
		if name == "" {
			name = defaultProjectName
		}
		if !found {
			return name, ""
		}
		return name, rest
	}
	return defaultProjectName, arg
}

func (a *App) ensureProject(name string) (string, error) {
	if !projectNamePattern.MatchString(name) {
		return "", fmt.Errorf("nama proyek tidak valid (huruf kecil, angka, - dan _, maks 41 karakter)")
	}
	dir := a.projectDir(name)
	if err := os.MkdirAll(dir, 0o777); err != nil {
		return "", fmt.Errorf("gagal membuat folder proyek")
	}
	_ = os.Chmod(dir, 0o777)
	return dir, nil
}

// projectPath memvalidasi path relatif agar tetap di dalam folder proyek.
func (a *App) projectPath(project, rel string) (string, error) {
	rel = strings.TrimSpace(rel)
	if rel == "" {
		return "", fmt.Errorf("path kosong")
	}
	if filepath.IsAbs(rel) || strings.HasPrefix(rel, "/") || strings.HasPrefix(rel, "\\") {
		return "", fmt.Errorf("path harus relatif terhadap folder proyek")
	}
	dir := a.projectDir(project)
	full := filepath.Join(dir, rel)
	if full != dir && !strings.HasPrefix(full, dir+string(os.PathSeparator)) {
		return "", fmt.Errorf("path keluar dari folder proyek")
	}
	return full, nil
}

// writeProjectFile menulis file proyek dengan izin yang bisa ditulis
// ulang oleh proses sandbox (user nobody).
func writeProjectFile(full string, content []byte) error {
	if err := os.MkdirAll(filepath.Dir(full), 0o777); err != nil {
		return err
	}
	_ = os.Chmod(filepath.Dir(full), 0o777)
	if err := os.WriteFile(full, content, 0o666); err != nil {
		return err
	}
	return os.Chmod(full, 0o666)
}

func mkdirProject(full string) error {
	if err := os.MkdirAll(full, 0o777); err != nil {
		return err
	}
	return os.Chmod(full, 0o777)
}

// projectRel mengembalikan path relatif (pemisah "/") dari folder proyek.
func projectRel(dir, full string) string {
	rel, err := filepath.Rel(dir, full)
	if err != nil {
		return full
	}
	return filepath.ToSlash(rel)
}

// walkProject menyusuri file proyek (melewati folder abaian) dan
// memanggil fn untuk tiap file; berhenti bila fn mengembalikan false
// atau batas entri tercapai.
func walkProject(dir string, maxEntries int, fn func(rel, full string, info os.FileInfo) bool) {
	count := 0
	_ = filepath.WalkDir(dir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if entry.IsDir() {
			if path != dir && projectIgnoreDirs[entry.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		count++
		if count > maxEntries {
			return filepath.SkipAll
		}
		info, err := entry.Info()
		if err != nil {
			return nil
		}
		if !fn(projectRel(dir, path), path, info) {
			return filepath.SkipAll
		}
		return nil
	})
}

func isBinaryContent(raw []byte) bool {
	n := len(raw)
	if n > 512 {
		n = 512
	}
	return bytes.IndexByte(raw[:n], 0) >= 0
}

// --- Grup A: file & proyek ---

func (a *App) toolCreateProject(arg string) string {
	lines := splitArgLines(arg)
	if len(lines) < 2 {
		return "Error: format create_project — baris pertama nama proyek, baris kedua template (go, node, python, web)."
	}
	name := strings.ToLower(lines[0])
	template := strings.ToLower(lines[1])
	if !projectNamePattern.MatchString(name) {
		return "Error: nama proyek tidak valid (huruf kecil, angka, - dan _, maks 41 karakter)."
	}
	dir := a.projectDir(name)
	if _, err := os.Stat(dir); err == nil {
		return fmt.Sprintf("Error: proyek %q sudah ada.", name)
	}
	files := map[string]string{}
	switch template {
	case "go":
		files["go.mod"] = "module " + name + "\n\ngo 1.22\n"
		files["main.go"] = "package main\n\nimport \"fmt\"\n\nfunc main() {\n\tfmt.Println(\"Halo dari " + name + "\")\n}\n"
		files["main_test.go"] = "package main\n\nimport \"testing\"\n\nfunc TestMain(t *testing.T) {\n\t// Tes awal: pastikan paket terkompilasi.\n}\n"
	case "node":
		files["package.json"] = "{\n  \"name\": \"" + name + "\",\n  \"version\": \"1.0.0\",\n  \"scripts\": {\n    \"start\": \"node index.js\",\n    \"test\": \"node --test\"\n  }\n}\n"
		files["index.js"] = "console.log('Halo dari " + name + "');\n"
		files["index.test.js"] = "const { test } = require('node:test');\nconst assert = require('node:assert');\n\ntest('awal', () => {\n  assert.strictEqual(1 + 1, 2);\n});\n"
	case "python":
		files["main.py"] = "def main():\n    print(\"Halo dari " + name + "\")\n\n\nif __name__ == \"__main__\":\n    main()\n"
		files["test_main.py"] = "import unittest\n\n\nclass TestAwal(unittest.TestCase):\n    def test_awal(self):\n        self.assertEqual(1 + 1, 2)\n\n\nif __name__ == \"__main__\":\n    unittest.main()\n"
	case "web":
		files["index.html"] = "<!doctype html>\n<html lang=\"id\">\n<head><meta charset=\"utf-8\"><title>" + name + "</title></head>\n<body><h1>Halo dari " + name + "</h1></body>\n</html>\n"
	default:
		return "Error: template harus go, node, python, atau web."
	}
	if _, err := a.ensureProject(name); err != nil {
		return "Error: " + err.Error()
	}
	var written []string
	for rel, content := range files {
		full := filepath.Join(dir, rel)
		if err := writeProjectFile(full, []byte(content)); err != nil {
			return "Error: gagal menulis file awal proyek."
		}
		written = append(written, rel)
	}
	slices.Sort(written)
	return fmt.Sprintf("Proyek %q dibuat dari template %s: %s. Pakai awalan \"project:%s\" pada tool proyek lain untuk bekerja di proyek ini.", name, template, strings.Join(written, ", "), name)
}

func (a *App) toolListTree(arg string) string {
	project, _ := parseProjectArg(arg)
	dir, err := a.ensureProject(project)
	if err != nil {
		return "Error: " + err.Error()
	}
	var lines []string
	count := 0
	var walk func(path string, depth int)
	walk = func(path string, depth int) {
		if depth > 6 || count >= 500 {
			return
		}
		entries, err := os.ReadDir(path)
		if err != nil {
			return
		}
		slices.SortFunc(entries, func(a, b os.DirEntry) int { return cmp.Compare(a.Name(), b.Name()) })
		for _, entry := range entries {
			if count >= 500 {
				return
			}
			count++
			indent := strings.Repeat("  ", depth)
			if entry.IsDir() {
				if projectIgnoreDirs[entry.Name()] {
					lines = append(lines, indent+entry.Name()+"/ (diabaikan)")
					continue
				}
				lines = append(lines, indent+entry.Name()+"/")
				walk(filepath.Join(path, entry.Name()), depth+1)
				continue
			}
			info, err := entry.Info()
			size := int64(0)
			if err == nil {
				size = info.Size()
			}
			lines = append(lines, fmt.Sprintf("%s%s (%d byte)", indent, entry.Name(), size))
		}
	}
	walk(dir, 0)
	if len(lines) == 0 {
		return fmt.Sprintf("Proyek %q kosong.", project)
	}
	header := fmt.Sprintf("Pohon proyek %q (%d entri):", project, count)
	return truncateToolResult(header + "\n" + strings.Join(lines, "\n"))
}

func (a *App) toolReadFileLines(arg string) string {
	project, rest := parseProjectArg(arg)
	lines := splitArgLines(rest)
	if len(lines) < 3 {
		return "Error: format read_file_lines — baris pertama path, baris kedua baris awal, baris ketiga baris akhir (1-based)."
	}
	full, err := a.projectPath(project, lines[0])
	if err != nil {
		return "Error: " + err.Error()
	}
	start, err1 := strconv.Atoi(lines[1])
	end, err2 := strconv.Atoi(lines[2])
	if err1 != nil || err2 != nil || start < 1 || end < start {
		return "Error: rentang baris tidak valid."
	}
	if end-start+1 > 500 {
		end = start + 499
	}
	raw, err := os.ReadFile(full)
	if err != nil {
		return "Error: file tidak ditemukan."
	}
	all := strings.Split(string(raw), "\n")
	if start > len(all) {
		return fmt.Sprintf("Error: baris awal %d melebihi jumlah baris file (%d).", start, len(all))
	}
	if end > len(all) {
		end = len(all)
	}
	var out []string
	for i := start; i <= end; i++ {
		out = append(out, fmt.Sprintf("%d: %s", i, all[i-1]))
	}
	return truncateToolResult(strings.Join(out, "\n"))
}

func (a *App) toolMoveFile(arg string) string {
	project, rest := parseProjectArg(arg)
	lines := splitArgLines(rest)
	if len(lines) < 2 {
		return "Error: format move_file — baris pertama path sumber, baris kedua path tujuan."
	}
	src, err := a.projectPath(project, lines[0])
	if err != nil {
		return "Error: " + err.Error()
	}
	dst, err := a.projectPath(project, lines[1])
	if err != nil {
		return "Error: " + err.Error()
	}
	if _, err := os.Stat(src); err != nil {
		return "Error: file sumber tidak ditemukan."
	}
	if _, err := os.Stat(dst); err == nil {
		return "Error: file tujuan sudah ada; move_file tidak menimpa."
	}
	if err := mkdirProject(filepath.Dir(dst)); err != nil {
		return "Error: gagal membuat folder tujuan."
	}
	if err := os.Rename(src, dst); err != nil {
		return "Error: gagal memindahkan file."
	}
	return fmt.Sprintf("File dipindahkan: %s → %s.", lines[0], lines[1])
}

func (a *App) toolCopyFile(arg string) string {
	project, rest := parseProjectArg(arg)
	lines := splitArgLines(rest)
	if len(lines) < 2 {
		return "Error: format copy_file — baris pertama path sumber, baris kedua path tujuan."
	}
	src, err := a.projectPath(project, lines[0])
	if err != nil {
		return "Error: " + err.Error()
	}
	dst, err := a.projectPath(project, lines[1])
	if err != nil {
		return "Error: " + err.Error()
	}
	raw, err := os.ReadFile(src)
	if err != nil {
		return "Error: file sumber tidak ditemukan."
	}
	if len(raw) > maxProjectFileBytes {
		return "Error: file sumber melebihi batas 1 MB."
	}
	if _, err := os.Stat(dst); err == nil {
		return "Error: file tujuan sudah ada; copy_file tidak menimpa."
	}
	if err := writeProjectFile(dst, raw); err != nil {
		return "Error: gagal menyalin file."
	}
	return fmt.Sprintf("File disalin: %s → %s (%d byte).", lines[0], lines[1], len(raw))
}

func (a *App) toolMkdir(arg string) string {
	project, rest := parseProjectArg(arg)
	lines := splitArgLines(rest)
	if len(lines) < 1 {
		return "Error: format mkdir — argumen: path folder."
	}
	full, err := a.projectPath(project, lines[0])
	if err != nil {
		return "Error: " + err.Error()
	}
	if _, err := a.ensureProject(project); err != nil {
		return "Error: " + err.Error()
	}
	if err := mkdirProject(full); err != nil {
		return "Error: gagal membuat folder."
	}
	return fmt.Sprintf("Folder %s dibuat di proyek %q.", lines[0], project)
}

func (a *App) toolSearchCode(arg string) string {
	project, rest := parseProjectArg(arg)
	pattern := strings.TrimSpace(rest)
	if pattern == "" {
		return "Error: pola pencarian kosong."
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return "Error: pola regex tidak valid: " + err.Error()
	}
	dir, err := a.ensureProject(project)
	if err != nil {
		return "Error: " + err.Error()
	}
	var out []string
	matches := 0
	walkProject(dir, 5000, func(rel, full string, info os.FileInfo) bool {
		if matches >= 100 {
			return false
		}
		if info.Size() > maxProjectFileBytes {
			return true
		}
		raw, err := os.ReadFile(full)
		if err != nil || isBinaryContent(raw) {
			return true
		}
		lines := strings.Split(string(raw), "\n")
		for i, line := range lines {
			if matches >= 100 {
				break
			}
			if !re.MatchString(line) {
				continue
			}
			matches++
			for c := i - 2; c <= i+2; c++ {
				if c < 0 || c >= len(lines) {
					continue
				}
				marker := " "
				if c == i {
					marker = ">"
				}
				out = append(out, fmt.Sprintf("%s %s:%d: %s", marker, rel, c+1, lines[c]))
			}
			out = append(out, "--")
		}
		return true
	})
	if matches == 0 {
		return fmt.Sprintf("Tidak ada hasil untuk %q di proyek %q.", pattern, project)
	}
	return truncateToolResult(fmt.Sprintf("%d hasil untuk %q di proyek %q:\n%s", matches, pattern, project, strings.Join(out, "\n")))
}

// matchGlob mencocokkan pola glob dengan dukungan segmen "**".
func matchGlob(pattern, name string) bool {
	patSegs := strings.Split(pattern, "/")
	nameSegs := strings.Split(name, "/")
	var match func(pi, ni int) bool
	match = func(pi, ni int) bool {
		for pi < len(patSegs) {
			if patSegs[pi] == "**" {
				if pi == len(patSegs)-1 {
					return true
				}
				for skip := ni; skip <= len(nameSegs); skip++ {
					if match(pi+1, skip) {
						return true
					}
				}
				return false
			}
			if ni >= len(nameSegs) {
				return false
			}
			ok, err := filepath.Match(patSegs[pi], nameSegs[ni])
			if err != nil || !ok {
				return false
			}
			pi++
			ni++
		}
		return ni == len(nameSegs)
	}
	return match(0, 0)
}

func (a *App) toolFindFiles(arg string) string {
	project, rest := parseProjectArg(arg)
	pattern := strings.TrimSpace(rest)
	if pattern == "" {
		return "Error: pola glob kosong."
	}
	dir, err := a.ensureProject(project)
	if err != nil {
		return "Error: " + err.Error()
	}
	var found []string
	walkProject(dir, 5000, func(rel, full string, info os.FileInfo) bool {
		if matchGlob(pattern, rel) {
			found = append(found, rel)
		}
		return len(found) < 200
	})
	if len(found) == 0 {
		return fmt.Sprintf("Tidak ada file cocok dengan %q di proyek %q.", pattern, project)
	}
	slices.Sort(found)
	return truncateToolResult(fmt.Sprintf("%d file cocok dengan %q di proyek %q:\n%s", len(found), pattern, project, strings.Join(found, "\n")))
}
