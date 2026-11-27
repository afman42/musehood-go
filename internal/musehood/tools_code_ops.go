package musehood

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// --- Grup F: ekspor, impor, .env ---

var exportNamePattern = regexp.MustCompile(`^[A-Za-z0-9._-]+\.zip$`)

func (a *App) exportsDir() string {
	return filepath.Join(a.dir, "exports")
}

func (a *App) toolExportProject(arg string) string {
	project, _ := parseProjectArg(arg)
	dir, err := a.ensureProject(project)
	if err != nil {
		return "Error: " + err.Error()
	}
	if err := os.MkdirAll(a.exportsDir(), 0o755); err != nil {
		return "Error: gagal membuat folder ekspor."
	}
	name := fmt.Sprintf("%s-%s.zip", project, time.Now().Format("20060102-150405"))
	outPath := filepath.Join(a.exportsDir(), name)
	var buf bytes.Buffer
	writer := zip.NewWriter(&buf)
	var total int64
	var fileCount int
	var walkErr error
	walkProject(dir, 10000, func(rel, full string, info os.FileInfo) bool {
		if info.Size() > maxProjectFileBytes*10 {
			return true
		}
		raw, err := os.ReadFile(full)
		if err != nil {
			return true
		}
		total += int64(len(raw))
		if total > 100*1024*1024 {
			walkErr = fmt.Errorf("proyek melebihi batas ekspor 100 MB")
			return false
		}
		w, err := writer.Create(rel)
		if err != nil {
			walkErr = err
			return false
		}
		if _, err := w.Write(raw); err != nil {
			walkErr = err
			return false
		}
		fileCount++
		return true
	})
	if walkErr != nil {
		return "Error: " + walkErr.Error()
	}
	if err := writer.Close(); err != nil {
		return "Error: gagal membuat zip."
	}
	if err := os.WriteFile(outPath, buf.Bytes(), 0o644); err != nil {
		return "Error: gagal menyimpan zip ekspor."
	}
	return fmt.Sprintf("Proyek %q diekspor: %d file, %d byte. Unduh: /exports/%s", project, fileCount, buf.Len(), name)
}

func (a *App) toolImportProject(ctx context.Context, s seat, arg string) string {
	project, rest := parseProjectArg(arg)
	lines := splitArgLines(rest)
	if len(lines) < 1 {
		return "Error: format import_project — argumen: nama file zip di folder uploads."
	}
	zipName := filepath.Base(strings.TrimSpace(lines[0]))
	uploadsDir := filepath.Join(a.dir, "uploads")
	zipPath := filepath.Join(uploadsDir, zipName)
	if _, err := os.Stat(zipPath); err != nil {
		// Coba cocokkan akhiran "-<nama>" dari pola unggahan tersimpan.
		entries, _ := os.ReadDir(uploadsDir)
		found := ""
		for _, e := range entries {
			if strings.HasSuffix(e.Name(), "-"+zipName) || e.Name() == zipName {
				found = e.Name()
				break
			}
		}
		if found == "" {
			return fmt.Sprintf("Error: file zip %q tidak ditemukan di uploads.", zipName)
		}
		zipPath = filepath.Join(uploadsDir, found)
	}
	reader, err := zip.OpenReader(zipPath)
	if err != nil {
		return "Error: file bukan zip yang valid."
	}
	defer reader.Close()
	if len(reader.File) > 1000 {
		return fmt.Sprintf("Error: zip berisi %d entri, melebihi batas 1000.", len(reader.File))
	}
	dir := a.projectDir(project)
	if _, err := os.Stat(dir); err == nil {
		// Proyek sudah ada: menimpa butuh persetujuan.
		detail := fmt.Sprintf("impor zip %s menimpa proyek %s yang sudah ada", zipName, project)
		if !a.requestApproval(ctx, s, "delete", "", detail) {
			return fmt.Sprintf("Impor TIDAK dilakukan: pemilik menolak atau tidak menyetujui dalam %d detik.", int(approvalTimeout.Seconds()))
		}
		if _, err := a.createSnapshot(project, "auto-import"); err != nil {
			return "Error: " + err.Error()
		}
		entries, _ := os.ReadDir(dir)
		for _, e := range entries {
			_ = os.RemoveAll(filepath.Join(dir, e.Name()))
		}
	}
	if _, err := a.ensureProject(project); err != nil {
		return "Error: " + err.Error()
	}
	var total int64
	extracted := 0
	for _, entry := range reader.File {
		name := filepath.ToSlash(entry.Name)
		if entry.FileInfo().IsDir() {
			continue
		}
		// Penjaga zip-slip: path harus tetap di dalam folder proyek.
		target, err := a.projectPath(project, name)
		if err != nil {
			return fmt.Sprintf("Error: entri zip %q tidak aman; impor dibatalkan.", name)
		}
		total += int64(entry.UncompressedSize64)
		if total > 100*1024*1024 {
			return "Error: isi zip melebihi batas 100 MB; impor dibatalkan."
		}
		rc, err := entry.Open()
		if err != nil {
			return "Error: gagal membaca entri zip."
		}
		raw, err := io.ReadAll(io.LimitReader(rc, maxProjectFileBytes*10+1))
		_ = rc.Close()
		if err != nil || len(raw) > maxProjectFileBytes*10 {
			return fmt.Sprintf("Error: entri %q terlalu besar; impor dibatalkan.", name)
		}
		if err := writeProjectFile(target, raw); err != nil {
			return "Error: gagal menulis hasil impor."
		}
		extracted++
	}
	return fmt.Sprintf("Proyek %q diimpor dari %s: %d file diekstrak.", project, zipName, extracted)
}

var envKeyPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func maskEnvValue(v string) string {
	if len(v) <= 4 {
		return "***"
	}
	return v[:4] + "***"
}

func (a *App) toolEnvFile(arg string) string {
	project, rest := parseProjectArg(arg)
	trimmed := strings.TrimSpace(rest)
	dir, err := a.ensureProject(project)
	if err != nil {
		return "Error: " + err.Error()
	}
	envPath := filepath.Join(dir, ".env")
	readLines := func() []string {
		raw, err := os.ReadFile(envPath)
		if err != nil {
			return nil
		}
		return strings.Split(string(raw), "\n")
	}
	parseKV := func(line string) (string, string, bool) {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			return "", "", false
		}
		k, v, found := strings.Cut(line, "=")
		if !found {
			return "", "", false
		}
		return strings.TrimSpace(k), strings.TrimSpace(v), true
	}
	switch {
	case trimmed == "list":
		var keys []string
		for _, line := range readLines() {
			if k, _, ok := parseKV(line); ok {
				keys = append(keys, k)
			}
		}
		if len(keys) == 0 {
			return fmt.Sprintf("File .env proyek %q kosong atau belum ada.", project)
		}
		sort.Strings(keys)
		return fmt.Sprintf("Kunci .env proyek %q: %s", project, strings.Join(keys, ", "))
	case strings.HasPrefix(trimmed, "get "):
		key := strings.TrimSpace(strings.TrimPrefix(trimmed, "get "))
		if !envKeyPattern.MatchString(key) {
			return "Error: nama kunci tidak valid."
		}
		for _, line := range readLines() {
			if k, v, ok := parseKV(line); ok && k == key {
				return fmt.Sprintf("%s=%s", key, maskEnvValue(v))
			}
		}
		return fmt.Sprintf("Kunci %q tidak ditemukan di .env proyek %q.", key, project)
	case strings.HasPrefix(trimmed, "set "):
		kv := strings.TrimSpace(strings.TrimPrefix(trimmed, "set "))
		key, value, found := strings.Cut(kv, "=")
		key = strings.TrimSpace(key)
		if !found || !envKeyPattern.MatchString(key) {
			return "Error: format set adalah \"set KUNCI=nilai\"."
		}
		lines := readLines()
		replaced := false
		for i, line := range lines {
			if k, _, ok := parseKV(line); ok && k == key {
				lines[i] = key + "=" + value
				replaced = true
			}
		}
		if !replaced {
			// Buang baris kosong terakhir agar tidak menumpuk.
			for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
				lines = lines[:len(lines)-1]
			}
			lines = append(lines, key+"="+value)
		}
		if err := writeProjectFile(envPath, []byte(strings.Join(lines, "\n")+"\n")); err != nil {
			return "Error: gagal menulis .env."
		}
		return fmt.Sprintf("Kunci %s disimpan ke .env proyek %q (nilai: %s).", key, project, maskEnvValue(value))
	case strings.HasPrefix(trimmed, "delete "):
		key := strings.TrimSpace(strings.TrimPrefix(trimmed, "delete "))
		if !envKeyPattern.MatchString(key) {
			return "Error: nama kunci tidak valid."
		}
		lines := readLines()
		var kept []string
		removed := false
		for _, line := range lines {
			if k, _, ok := parseKV(line); ok && k == key {
				removed = true
				continue
			}
			kept = append(kept, line)
		}
		if !removed {
			return fmt.Sprintf("Kunci %q tidak ditemukan di .env proyek %q.", key, project)
		}
		if err := writeProjectFile(envPath, []byte(strings.Join(kept, "\n"))); err != nil {
			return "Error: gagal menulis .env."
		}
		return fmt.Sprintf("Kunci %s dihapus dari .env proyek %q.", key, project)
	default:
		return "Error: format env_file — list, get <KUNCI>, set <KUNCI>=<nilai>, atau delete <KUNCI>."
	}
}

// redactToolArg menyensor nilai rahasia dari argumen tool sebelum
// dicatat ke log audit dan kartu tool (env_file set).
func redactToolArg(name, arg string) string {
	if name != "env_file" {
		return arg
	}
	lines := strings.Split(arg, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "set ") {
			kv := strings.TrimSpace(strings.TrimPrefix(trimmed, "set "))
			if key, _, found := strings.Cut(kv, "="); found {
				lines[i] = "set " + strings.TrimSpace(key) + "=***"
			}
		}
	}
	return strings.Join(lines, "\n")
}
