package musehood

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

// --- Grup B: edit multi-file + snapshot ---

const maxSnapshotsPerProject = 10

func (a *App) snapshotsDir(project string) string {
	return filepath.Join(a.dir, "snapshots", project)
}

// copyProjectTree menyalin pohon proyek (melewati folder abaian).
func copyProjectTree(src, dst string) error {
	return filepath.WalkDir(src, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return nil
		}
		if rel == "." {
			return os.MkdirAll(dst, 0o777)
		}
		if entry.IsDir() {
			if projectIgnoreDirs[entry.Name()] {
				return filepath.SkipDir
			}
			return os.MkdirAll(filepath.Join(dst, rel), 0o777)
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		return writeProjectFile(filepath.Join(dst, rel), raw)
	})
}

// createSnapshot membuat snapshot proyek dan memangkas yang tertua.
func (a *App) createSnapshot(project, label string) (string, error) {
	dir, err := a.ensureProject(project)
	if err != nil {
		return "", err
	}
	id := time.Now().Format("20060102-150405") + "-" + uuid()[:6]
	if label != "" {
		safe := regexp.MustCompile(`[^a-zA-Z0-9_-]+`).ReplaceAllString(label, "-")
		id += "-" + safe
	}
	dst := filepath.Join(a.snapshotsDir(project), id)
	if err := copyProjectTree(dir, dst); err != nil {
		return "", fmt.Errorf("gagal membuat snapshot")
	}
	entries, _ := os.ReadDir(a.snapshotsDir(project))
	var ids []string
	for _, e := range entries {
		if e.IsDir() {
			ids = append(ids, e.Name())
		}
	}
	slices.Sort(ids)
	for len(ids) > maxSnapshotsPerProject {
		_ = os.RemoveAll(filepath.Join(a.snapshotsDir(project), ids[0]))
		ids = ids[1:]
	}
	return id, nil
}

func (a *App) toolProjectSnapshot(ctx context.Context, s seat, arg string) string {
	project, rest := parseProjectArg(arg)
	fields := strings.Fields(strings.TrimSpace(rest))
	if len(fields) == 0 {
		return "Error: format project_snapshot — create [label], list, restore <id>, atau delete <id>."
	}
	switch fields[0] {
	case "create":
		label := ""
		if len(fields) > 1 {
			label = strings.Join(fields[1:], "-")
		}
		id, err := a.createSnapshot(project, label)
		if err != nil {
			return "Error: " + err.Error()
		}
		return fmt.Sprintf("Snapshot %s dibuat untuk proyek %q.", id, project)
	case "list":
		entries, err := os.ReadDir(a.snapshotsDir(project))
		if err != nil {
			return fmt.Sprintf("Belum ada snapshot untuk proyek %q.", project)
		}
		var ids []string
		for _, e := range entries {
			if e.IsDir() {
				ids = append(ids, e.Name())
			}
		}
		if len(ids) == 0 {
			return fmt.Sprintf("Belum ada snapshot untuk proyek %q.", project)
		}
		slices.Sort(ids)
		return fmt.Sprintf("Snapshot proyek %q (%d):\n%s", project, len(ids), strings.Join(ids, "\n"))
	case "restore":
		if len(fields) < 2 {
			return "Error: restore membutuhkan id snapshot."
		}
		id := fields[1]
		src := filepath.Join(a.snapshotsDir(project), id)
		if _, err := os.Stat(src); err != nil {
			return fmt.Sprintf("Error: snapshot %q tidak ditemukan.", id)
		}
		detail := fmt.Sprintf("restore snapshot %s ke proyek %s (isi proyek saat ini akan ditimpa)", id, project)
		if !a.requestApproval(ctx, s, "delete", "", detail) {
			return fmt.Sprintf("Snapshot TIDAK dipulihkan: pemilik menolak atau tidak menyetujui dalam %d detik.", int(approvalTimeout.Seconds()))
		}
		dir, err := a.ensureProject(project)
		if err != nil {
			return "Error: " + err.Error()
		}
		entries, _ := os.ReadDir(dir)
		for _, e := range entries {
			_ = os.RemoveAll(filepath.Join(dir, e.Name()))
		}
		if err := copyProjectTree(src, dir); err != nil {
			return "Error: gagal memulihkan snapshot."
		}
		return fmt.Sprintf("Proyek %q dipulihkan dari snapshot %s.", project, id)
	case "delete":
		if len(fields) < 2 {
			return "Error: delete membutuhkan id snapshot."
		}
		id := fields[1]
		target := filepath.Join(a.snapshotsDir(project), id)
		if _, err := os.Stat(target); err != nil {
			return fmt.Sprintf("Error: snapshot %q tidak ditemukan.", id)
		}
		if err := os.RemoveAll(target); err != nil {
			return "Error: gagal menghapus snapshot."
		}
		return fmt.Sprintf("Snapshot %s dihapus.", id)
	default:
		return "Error: subperintah harus create, list, restore, atau delete."
	}
}

// --- apply_patch: parser unified diff (format git) ---

type patchHunk struct {
	oldStart, oldCount int
	newStart, newCount int
	lines              []string // baris mentah termasuk awalan ' ', '-', '+'
}

type patchFile struct {
	oldPath string // "" berarti file baru (/dev/null)
	newPath string // "" berarti file dihapus (/dev/null)
	hunks   []patchHunk
}

var hunkHeaderPattern = regexp.MustCompile(`^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@`)

func stripDiffPrefix(p string) string {
	p = strings.TrimSpace(p)
	if p == "/dev/null" {
		return ""
	}
	if strings.HasPrefix(p, "a/") || strings.HasPrefix(p, "b/") {
		return p[2:]
	}
	return p
}

func parseUnifiedDiff(diff string) ([]patchFile, error) {
	var files []patchFile
	var current *patchFile
	var hunk *patchHunk
	flushHunk := func() {
		if current != nil && hunk != nil {
			current.hunks = append(current.hunks, *hunk)
			hunk = nil
		}
	}
	flushFile := func() {
		flushHunk()
		if current != nil {
			files = append(files, *current)
			current = nil
		}
	}
	for _, line := range strings.Split(diff, "\n") {
		switch {
		case strings.HasPrefix(line, "diff --git "):
			flushFile()
			current = &patchFile{}
		case strings.HasPrefix(line, "--- "):
			if current == nil {
				current = &patchFile{}
			}
			flushHunk()
			current.oldPath = stripDiffPrefix(strings.TrimPrefix(line, "--- "))
		case strings.HasPrefix(line, "+++ "):
			if current == nil {
				current = &patchFile{}
			}
			current.newPath = stripDiffPrefix(strings.TrimPrefix(line, "+++ "))
		case strings.HasPrefix(line, "@@ "):
			if current == nil {
				return nil, fmt.Errorf("hunk tanpa file")
			}
			m := hunkHeaderPattern.FindStringSubmatch(line)
			if m == nil {
				return nil, fmt.Errorf("header hunk tidak valid: %q", line)
			}
			flushHunk()
			hunk = &patchHunk{}
			hunk.oldStart, _ = strconv.Atoi(m[1])
			hunk.oldCount = 1
			if m[2] != "" {
				hunk.oldCount, _ = strconv.Atoi(m[2])
			}
			hunk.newStart, _ = strconv.Atoi(m[3])
			hunk.newCount = 1
			if m[4] != "" {
				hunk.newCount, _ = strconv.Atoi(m[4])
			}
		case hunk != nil && (strings.HasPrefix(line, " ") || strings.HasPrefix(line, "-") || strings.HasPrefix(line, "+") || strings.HasPrefix(line, "\\")):
			hunk.lines = append(hunk.lines, line)
		case strings.HasPrefix(line, "index ") || strings.HasPrefix(line, "new file") || strings.HasPrefix(line, "deleted file") || strings.HasPrefix(line, "similarity") || strings.HasPrefix(line, "rename "):
			// metadata git diabaikan
		case strings.TrimSpace(line) == "":
			// baris kosong di luar hunk diabaikan
		default:
			if hunk != nil {
				// baris konteks tanpa awalan spasi (diff longgar) diperlakukan sebagai konteks
				hunk.lines = append(hunk.lines, " "+line)
			}
		}
	}
	flushFile()
	if len(files) == 0 {
		return nil, fmt.Errorf("tidak ada file dalam diff")
	}
	return files, nil
}

// applyHunks menerapkan hunk ke isi file; mengembalikan isi baru.
func applyHunks(content string, hunks []patchHunk) (string, error) {
	lines := strings.Split(content, "\n")
	// strings.Split pada konten berakhiran \n menghasilkan elemen kosong
	// terakhir; perlakukan sebagai baris biasa agar konsisten.
	var out []string
	pos := 0 // indeks 0-based di lines
	for _, h := range hunks {
		start := h.oldStart - 1
		if h.oldCount == 0 {
			start = h.oldStart // penyisipan sesudah baris oldStart
		}
		if start < pos {
			return "", fmt.Errorf("hunk tumpang tindih")
		}
		if start > len(lines) {
			return "", fmt.Errorf("posisi hunk di luar file")
		}
		out = append(out, lines[pos:start]...)
		pos = start
		for _, hl := range h.lines {
			if hl == "" {
				continue
			}
			switch hl[0] {
			case ' ':
				if pos >= len(lines) || lines[pos] != hl[1:] {
					return "", fmt.Errorf("baris konteks tidak cocok di baris %d", pos+1)
				}
				out = append(out, hl[1:])
				pos++
			case '-':
				if pos >= len(lines) || lines[pos] != hl[1:] {
					return "", fmt.Errorf("baris hapus tidak cocok di baris %d", pos+1)
				}
				pos++
			case '+':
				out = append(out, hl[1:])
			case '\\':
				// penanda "no newline" diabaikan
			}
		}
	}
	out = append(out, lines[pos:]...)
	return strings.Join(out, "\n"), nil
}

func (a *App) toolApplyPatch(arg string) string {
	project, rest := parseProjectArg(arg)
	diff := strings.TrimSpace(rest)
	if diff == "" {
		return "Error: diff kosong."
	}
	files, err := parseUnifiedDiff(diff)
	if err != nil {
		return "Error: gagal mengurai diff: " + err.Error()
	}
	dir, err := a.ensureProject(project)
	if err != nil {
		return "Error: " + err.Error()
	}
	// Hitung semua hasil dulu (atomik): tidak ada file berubah bila gagal.
	type change struct {
		path    string // path relatif tujuan ("" = hapus oldPath)
		oldRel  string
		content *string // nil = hapus file
	}
	var changes []change
	for _, f := range files {
		oldRel, newRel := f.oldPath, f.newPath
		if oldRel == "" && newRel == "" {
			return "Error: entri diff tanpa path file."
		}
		var content string
		if oldRel != "" {
			full, err := a.projectPath(project, oldRel)
			if err != nil {
				return "Error: " + err.Error()
			}
			raw, err := os.ReadFile(full)
			if err != nil {
				return fmt.Sprintf("Error: file %q tidak ditemukan untuk di-patch.", oldRel)
			}
			content = string(raw)
		} else {
			// File baru: pastikan belum ada.
			full, err := a.projectPath(project, newRel)
			if err != nil {
				return "Error: " + err.Error()
			}
			if _, err := os.Stat(full); err == nil {
				return fmt.Sprintf("Error: file baru %q sudah ada.", newRel)
			}
		}
		updated, err := applyHunks(content, f.hunks)
		if err != nil {
			target := newRel
			if target == "" {
				target = oldRel
			}
			return fmt.Sprintf("Error: patch gagal pada %q: %s. Tidak ada file yang berubah.", target, err.Error())
		}
		if len(updated) > maxProjectFileBytes {
			return "Error: hasil patch melebihi batas 1 MB per file."
		}
		if newRel == "" {
			changes = append(changes, change{path: "", oldRel: oldRel, content: nil})
		} else {
			changes = append(changes, change{path: newRel, oldRel: oldRel, content: &updated})
		}
	}
	snapID, err := a.createSnapshot(project, "auto-patch")
	if err != nil {
		return "Error: " + err.Error()
	}
	var summary []string
	for _, ch := range changes {
		if ch.content == nil {
			full, _ := a.projectPath(project, ch.oldRel)
			_ = os.Remove(full)
			summary = append(summary, "hapus "+ch.oldRel)
			continue
		}
		full, _ := a.projectPath(project, ch.path)
		if err := writeProjectFile(full, []byte(*ch.content)); err != nil {
			return "Error: gagal menulis hasil patch."
		}
		if ch.oldRel != "" && ch.oldRel != ch.path {
			oldFull, _ := a.projectPath(project, ch.oldRel)
			_ = os.Remove(oldFull)
			summary = append(summary, fmt.Sprintf("ganti nama %s → %s", ch.oldRel, ch.path))
		} else if ch.oldRel == "" {
			summary = append(summary, "tambah "+ch.path)
		} else {
			summary = append(summary, "ubah "+ch.path)
		}
	}
	_ = dir
	return fmt.Sprintf("Patch diterapkan ke proyek %q (%s). Snapshot sebelum patch: %s.", project, strings.Join(summary, ", "), snapID)
}

func (a *App) toolEditMany(arg string) string {
	project, rest := parseProjectArg(arg)
	type fileEdits struct {
		path  string
		pairs [][2]string
	}
	var files []fileEdits
	var current *fileEdits
	lines := strings.Split(rest, "\n")
	i := 0
	for i < len(lines) {
		line := strings.TrimSpace(lines[i])
		if strings.HasPrefix(line, "file:") {
			files = append(files, fileEdits{path: strings.TrimSpace(strings.TrimPrefix(line, "file:"))})
			current = &files[len(files)-1]
			i++
			continue
		}
		if line == "<<<OLD>>>" && current != nil {
			i++
			var oldLines []string
			for i < len(lines) && strings.TrimSpace(lines[i]) != "<<<NEW>>>" {
				oldLines = append(oldLines, lines[i])
				i++
			}
			if i >= len(lines) {
				return "Error: penanda <<<NEW>>> tidak ditemukan."
			}
			i++ // lewati <<<NEW>>>
			var newLines []string
			for i < len(lines) && strings.TrimSpace(lines[i]) != "<<<OLD>>>" && !strings.HasPrefix(strings.TrimSpace(lines[i]), "file:") {
				newLines = append(newLines, lines[i])
				i++
			}
			current.pairs = append(current.pairs, [2]string{strings.Join(oldLines, "\n"), strings.Join(newLines, "\n")})
			continue
		}
		i++
	}
	if len(files) == 0 {
		return "Error: format edit_many — blok per file diawali \"file:<path>\", lalu pasangan <<<OLD>>>/<<<NEW>>>."
	}
	if _, err := a.ensureProject(project); err != nil {
		return "Error: " + err.Error()
	}
	// Hitung per file (atomik per file): file yang gagal tidak berubah.
	type result struct {
		path    string
		content string
		err     string
	}
	var results []result
	anyOK := false
	for _, f := range files {
		full, err := a.projectPath(project, f.path)
		if err != nil {
			results = append(results, result{path: f.path, err: err.Error()})
			continue
		}
		raw, err := os.ReadFile(full)
		if err != nil {
			results = append(results, result{path: f.path, err: "file tidak ditemukan"})
			continue
		}
		content := string(raw)
		failed := ""
		for _, pair := range f.pairs {
			oldText := strings.Trim(pair[0], "\n")
			newText := strings.Trim(pair[1], "\n")
			if oldText == "" || !strings.Contains(content, oldText) {
				failed = "teks lama tidak ditemukan"
				break
			}
			content = strings.Replace(content, oldText, newText, 1)
		}
		if failed != "" {
			results = append(results, result{path: f.path, err: failed})
			continue
		}
		if len(content) > maxProjectFileBytes {
			results = append(results, result{path: f.path, err: "hasil melebihi batas 1 MB"})
			continue
		}
		results = append(results, result{path: f.path, content: content})
		anyOK = true
	}
	if !anyOK {
		var msgs []string
		for _, r := range results {
			msgs = append(msgs, r.path+": "+r.err)
		}
		return "Error: tidak ada file yang diubah (" + strings.Join(msgs, "; ") + ")."
	}
	snapID, err := a.createSnapshot(project, "auto-edit")
	if err != nil {
		return "Error: " + err.Error()
	}
	var summary []string
	for _, r := range results {
		if r.err != "" {
			summary = append(summary, r.path+" GAGAL ("+r.err+")")
			continue
		}
		full, _ := a.projectPath(project, r.path)
		if err := writeProjectFile(full, []byte(r.content)); err != nil {
			summary = append(summary, r.path+" GAGAL (tulis)")
			continue
		}
		summary = append(summary, r.path+" diubah")
	}
	return fmt.Sprintf("edit_many proyek %q: %s. Snapshot sebelum edit: %s.", project, strings.Join(summary, ", "), snapID)
}
