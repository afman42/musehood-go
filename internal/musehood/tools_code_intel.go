package musehood

import (
	"cmp"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// --- Grup E: intelijen kode ---

type outlineEntry struct {
	kind string
	name string
	line int
}

var (
	goFuncPattern  = regexp.MustCompile(`^func\s+(?:\([^)]*\)\s*)?([A-Za-z_]\w*)`)
	goTypePattern  = regexp.MustCompile(`^type\s+([A-Za-z_]\w*)`)
	pyDefPattern   = regexp.MustCompile(`^(\s*)(def|class)\s+([A-Za-z_]\w*)`)
	jsFuncPattern  = regexp.MustCompile(`^(?:export\s+)?(?:async\s+)?function\s+([A-Za-z_$][\w$]*)`)
	jsClassPattern = regexp.MustCompile(`^(?:export\s+)?class\s+([A-Za-z_$][\w$]*)`)
	jsArrowPattern = regexp.MustCompile(`^(?:export\s+)?const\s+([A-Za-z_$][\w$]*)\s*=\s*(?:async\s*)?(?:\(|function)`)
)

func outlineFile(rel string, lines []string) []outlineEntry {
	var out []outlineEntry
	ext := strings.ToLower(filepath.Ext(rel))
	for i, line := range lines {
		switch ext {
		case ".go":
			if m := goFuncPattern.FindStringSubmatch(line); m != nil {
				out = append(out, outlineEntry{"func", m[1], i + 1})
			} else if m := goTypePattern.FindStringSubmatch(line); m != nil {
				out = append(out, outlineEntry{"type", m[1], i + 1})
			}
		case ".py":
			if m := pyDefPattern.FindStringSubmatch(line); m != nil {
				out = append(out, outlineEntry{m[2], m[3], i + 1})
			}
		case ".js", ".ts", ".jsx", ".tsx", ".mjs", ".cjs":
			trimmed := strings.TrimSpace(line)
			if m := jsFuncPattern.FindStringSubmatch(trimmed); m != nil {
				out = append(out, outlineEntry{"function", m[1], i + 1})
			} else if m := jsClassPattern.FindStringSubmatch(trimmed); m != nil {
				out = append(out, outlineEntry{"class", m[1], i + 1})
			} else if m := jsArrowPattern.FindStringSubmatch(trimmed); m != nil {
				out = append(out, outlineEntry{"const", m[1], i + 1})
			}
		}
	}
	return out
}

func (a *App) toolOutline(arg string) string {
	project, rest := parseProjectArg(arg)
	lines := splitArgLines(rest)
	if len(lines) < 1 {
		return "Error: format outline — argumen: path file."
	}
	full, err := a.projectPath(project, lines[0])
	if err != nil {
		return "Error: " + err.Error()
	}
	raw, err := os.ReadFile(full)
	if err != nil {
		return "Error: file tidak ditemukan."
	}
	entries := outlineFile(lines[0], strings.Split(string(raw), "\n"))
	if len(entries) == 0 {
		return fmt.Sprintf("Tidak ada fungsi/kelas terdeteksi di %s (bahasa didukung: Go, Python, JS/TS).", lines[0])
	}
	var out []string
	for _, e := range entries {
		out = append(out, fmt.Sprintf("%d: %s %s", e.line, e.kind, e.name))
	}
	return truncateToolResult(fmt.Sprintf("Outline %s (%d entri):\n%s", lines[0], len(entries), strings.Join(out, "\n")))
}

func (a *App) toolFindReferences(arg string) string {
	project, rest := parseProjectArg(arg)
	symbol := strings.TrimSpace(rest)
	if symbol == "" || strings.ContainsAny(symbol, " \t\n") {
		return "Error: argumen harus satu simbol (tanpa spasi)."
	}
	re, err := regexp.Compile(`\b` + regexp.QuoteMeta(symbol) + `\b`)
	if err != nil {
		return "Error: simbol tidak valid."
	}
	dir, err := a.ensureProject(project)
	if err != nil {
		return "Error: " + err.Error()
	}
	var out []string
	count := 0
	walkProject(dir, 5000, func(rel, full string, info os.FileInfo) bool {
		if count >= 100 || info.Size() > maxProjectFileBytes {
			return count < 100
		}
		raw, err := os.ReadFile(full)
		if err != nil || isBinaryContent(raw) {
			return true
		}
		for i, line := range strings.Split(string(raw), "\n") {
			if count >= 100 {
				break
			}
			if re.MatchString(line) {
				count++
				out = append(out, fmt.Sprintf("%s:%d: %s", rel, i+1, strings.TrimSpace(line)))
			}
		}
		return true
	})
	if count == 0 {
		return fmt.Sprintf("Simbol %q tidak ditemukan di proyek %q.", symbol, project)
	}
	return truncateToolResult(fmt.Sprintf("%d referensi %q di proyek %q:\n%s", count, symbol, project, strings.Join(out, "\n")))
}

var (
	goImportSinglePattern = regexp.MustCompile(`^\s*import\s+(?:\w+\s+)?"([^"]+)"`)
	goImportItemPattern   = regexp.MustCompile(`^\s*(?:\w+\s+)?"([^"]+)"\s*$`)
	pyImportPattern       = regexp.MustCompile(`^\s*import\s+([\w.]+)`)
	pyFromPattern         = regexp.MustCompile(`^\s*from\s+([\w.]+)\s+import`)
	jsImportPattern       = regexp.MustCompile(`(?:from\s+|require\(\s*)['"]([^'"]+)['"]`)
)

func (a *App) toolDependencyGraph(arg string) string {
	project, _ := parseProjectArg(arg)
	dir, err := a.ensureProject(project)
	if err != nil {
		return "Error: " + err.Error()
	}
	module := ""
	if raw, err := os.ReadFile(filepath.Join(dir, "go.mod")); err == nil {
		for _, line := range strings.Split(string(raw), "\n") {
			if strings.HasPrefix(line, "module ") {
				module = strings.TrimSpace(strings.TrimPrefix(line, "module "))
				break
			}
		}
	}
	pyModules := map[string]bool{}
	walkProject(dir, 2000, func(rel, full string, info os.FileInfo) bool {
		if strings.HasSuffix(rel, ".py") {
			base := strings.TrimSuffix(filepath.Base(rel), ".py")
			pyModules[base] = true
			pyModules[strings.Split(rel, "/")[0]] = true
		}
		return true
	})
	var out []string
	walkProject(dir, 2000, func(rel, full string, info os.FileInfo) bool {
		if info.Size() > maxProjectFileBytes {
			return true
		}
		raw, err := os.ReadFile(full)
		if err != nil || isBinaryContent(raw) {
			return true
		}
		ext := strings.ToLower(filepath.Ext(rel))
		var internal, external []string
		lines := strings.Split(string(raw), "\n")
		switch ext {
		case ".go":
			inBlock := false
			for _, line := range lines {
				trimmed := strings.TrimSpace(line)
				if strings.HasPrefix(trimmed, "import (") {
					inBlock = true
					continue
				}
				if inBlock && trimmed == ")" {
					inBlock = false
					continue
				}
				var path string
				if inBlock {
					if m := goImportItemPattern.FindStringSubmatch(line); m != nil {
						path = m[1]
					}
				} else if m := goImportSinglePattern.FindStringSubmatch(line); m != nil {
					path = m[1]
				}
				if path == "" {
					continue
				}
				if module != "" && (path == module || strings.HasPrefix(path, module+"/")) {
					internal = append(internal, path)
				} else {
					external = append(external, path)
				}
			}
		case ".py":
			for _, line := range lines {
				var mod string
				if m := pyFromPattern.FindStringSubmatch(line); m != nil {
					mod = m[1]
				} else if m := pyImportPattern.FindStringSubmatch(line); m != nil {
					mod = m[1]
				}
				if mod == "" {
					continue
				}
				top := strings.Split(mod, ".")[0]
				if pyModules[top] {
					internal = append(internal, mod)
				} else {
					external = append(external, mod)
				}
			}
		case ".js", ".ts", ".jsx", ".tsx", ".mjs", ".cjs":
			for _, m := range jsImportPattern.FindAllStringSubmatch(string(raw), -1) {
				path := m[1]
				if strings.HasPrefix(path, "./") || strings.HasPrefix(path, "../") {
					internal = append(internal, path)
				} else {
					external = append(external, path)
				}
			}
		default:
			return true
		}
		if len(internal)+len(external) == 0 {
			return true
		}
		slices.Sort(internal)
		slices.Sort(external)
		line := rel + " →"
		if len(internal) > 0 {
			line += " internal: " + strings.Join(internal, ", ")
		}
		if len(external) > 0 {
			line += " | eksternal: " + strings.Join(external, ", ")
		}
		out = append(out, line)
		return true
	})
	if len(out) == 0 {
		return fmt.Sprintf("Tidak ada dependensi terdeteksi di proyek %q.", project)
	}
	slices.Sort(out)
	return truncateToolResult(fmt.Sprintf("Peta dependensi proyek %q:\n%s", project, strings.Join(out, "\n")))
}

var todoPattern = regexp.MustCompile(`\b(TODO|FIXME|HACK|XXX)\b`)

func (a *App) toolTodoScan(arg string) string {
	project, _ := parseProjectArg(arg)
	dir, err := a.ensureProject(project)
	if err != nil {
		return "Error: " + err.Error()
	}
	var out []string
	count := 0
	walkProject(dir, 5000, func(rel, full string, info os.FileInfo) bool {
		if count >= 100 || info.Size() > maxProjectFileBytes {
			return count < 100
		}
		raw, err := os.ReadFile(full)
		if err != nil || isBinaryContent(raw) {
			return true
		}
		for i, line := range strings.Split(string(raw), "\n") {
			if count >= 100 {
				break
			}
			if todoPattern.MatchString(line) {
				count++
				out = append(out, fmt.Sprintf("%s:%d: %s", rel, i+1, strings.TrimSpace(line)))
			}
		}
		return true
	})
	if count == 0 {
		return fmt.Sprintf("Tidak ada TODO/FIXME/HACK/XXX di proyek %q.", project)
	}
	return truncateToolResult(fmt.Sprintf("%d penanda di proyek %q:\n%s", count, project, strings.Join(out, "\n")))
}

func (a *App) toolComplexityReport(arg string) string {
	project, _ := parseProjectArg(arg)
	dir, err := a.ensureProject(project)
	if err != nil {
		return "Error: " + err.Error()
	}
	type fileStat struct {
		rel                         string
		total, code, blank, comment int
	}
	var stats []fileStat
	type funcSpan struct {
		file, name string
		length     int
	}
	var spans []funcSpan
	walkProject(dir, 5000, func(rel, full string, info os.FileInfo) bool {
		if info.Size() > maxProjectFileBytes {
			return true
		}
		raw, err := os.ReadFile(full)
		if err != nil || isBinaryContent(raw) {
			return true
		}
		lines := strings.Split(string(raw), "\n")
		st := fileStat{rel: rel, total: len(lines)}
		for _, line := range lines {
			trimmed := strings.TrimSpace(line)
			switch {
			case trimmed == "":
				st.blank++
			case strings.HasPrefix(trimmed, "//") || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "/*") || strings.HasPrefix(trimmed, "*"):
				st.comment++
			default:
				st.code++
			}
		}
		stats = append(stats, st)
		entries := outlineFile(rel, lines)
		for i, e := range entries {
			end := len(lines)
			if i+1 < len(entries) {
				end = entries[i+1].line - 1
			}
			spans = append(spans, funcSpan{rel, e.name, end - e.line + 1})
		}
		return true
	})
	if len(stats) == 0 {
		return fmt.Sprintf("Proyek %q kosong.", project)
	}
	slices.SortFunc(stats, func(a, b fileStat) int { return cmp.Compare(b.total, a.total) })
	var out []string
	out = append(out, "10 file terbesar (total/kode/kosong/komentar):")
	for i, st := range stats {
		if i >= 10 {
			break
		}
		out = append(out, fmt.Sprintf("%s: %d/%d/%d/%d", st.rel, st.total, st.code, st.blank, st.comment))
	}
	slices.SortFunc(spans, func(a, b funcSpan) int { return cmp.Compare(b.length, a.length) })
	if len(spans) > 0 {
		out = append(out, "Fungsi terpanjang:")
		for i, sp := range spans {
			if i >= 10 {
				break
			}
			out = append(out, fmt.Sprintf("%s %s: %d baris", sp.file, sp.name, sp.length))
		}
	}
	return truncateToolResult(fmt.Sprintf("Kompleksitas proyek %q (%d file):\n%s", project, len(stats), strings.Join(out, "\n")))
}

func (a *App) toolGenerateTests(arg string) string {
	project, rest := parseProjectArg(arg)
	lines := splitArgLines(rest)
	if len(lines) < 1 {
		return "Error: format generate_tests — argumen: path file sumber."
	}
	rel := lines[0]
	full, err := a.projectPath(project, rel)
	if err != nil {
		return "Error: " + err.Error()
	}
	raw, err := os.ReadFile(full)
	if err != nil {
		return "Error: file sumber tidak ditemukan."
	}
	srcLines := strings.Split(string(raw), "\n")
	entries := outlineFile(rel, srcLines)
	ext := strings.ToLower(filepath.Ext(rel))
	base := strings.TrimSuffix(rel, ext)
	var testRel, content string
	switch ext {
	case ".go":
		testRel = base + "_test.go"
		pkg := "main"
		for _, line := range srcLines {
			if strings.HasPrefix(line, "package ") {
				pkg = strings.TrimSpace(strings.TrimPrefix(line, "package "))
				break
			}
		}
		var b strings.Builder
		b.WriteString("package " + pkg + "\n\nimport \"testing\"\n")
		for _, e := range entries {
			if e.kind != "func" || len(e.name) == 0 || e.name[0] < 'A' || e.name[0] > 'Z' {
				continue
			}
			b.WriteString(fmt.Sprintf("\nfunc Test%s(t *testing.T) {\n\tt.Skip(\"TODO: tulis tes untuk %s\")\n}\n", e.name, e.name))
		}
		content = b.String()
	case ".py":
		dirPart := filepath.Dir(rel)
		name := "test_" + filepath.Base(rel)
		if dirPart == "." {
			testRel = name
		} else {
			testRel = filepath.ToSlash(filepath.Join(dirPart, name))
		}
		var b strings.Builder
		b.WriteString("import unittest\n")
		for _, e := range entries {
			if strings.HasPrefix(e.name, "_") {
				continue
			}
			b.WriteString(fmt.Sprintf("\n\nclass Test%s(unittest.TestCase):\n    def test_%s(self):\n        self.skipTest(\"TODO: tulis tes untuk %s\")\n", e.name, strings.ToLower(e.name), e.name))
		}
		b.WriteString("\n\nif __name__ == \"__main__\":\n    unittest.main()\n")
		content = b.String()
	case ".js", ".ts", ".mjs", ".cjs":
		testRel = base + ".test.js"
		var b strings.Builder
		b.WriteString("const { describe, it } = require('node:test');\n")
		for _, e := range entries {
			b.WriteString(fmt.Sprintf("\ndescribe('%s', () => {\n  it('TODO: tulis tes', () => {});\n});\n", e.name))
		}
		content = b.String()
	default:
		return "Error: bahasa tidak didukung untuk generate_tests (Go, Python, JS/TS)."
	}
	testFull, err := a.projectPath(project, testRel)
	if err != nil {
		return "Error: " + err.Error()
	}
	if _, err := os.Stat(testFull); err == nil {
		return fmt.Sprintf("Error: file tes %q sudah ada; generate_tests tidak menimpa.", testRel)
	}
	if err := writeProjectFile(testFull, []byte(content)); err != nil {
		return "Error: gagal menulis file tes."
	}
	return fmt.Sprintf("File tes kerangka dibuat: %s (%d stub).", testRel, len(entries))
}
