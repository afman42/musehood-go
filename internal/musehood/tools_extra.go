package musehood

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// --- Tool tambahan: git (baca-saja), screenshot URL, PDF dari Markdown,
// ringkas URL. ---

var gitReadOnlyCommands = map[string]bool{
	"status": true, "diff": true, "log": true, "show": true, "branch": true, "remote": true,
}

func (a *App) toolGit(ctx context.Context, arg string) string {
	fields := strings.Fields(strings.TrimSpace(arg))
	if len(fields) == 0 {
		return "Error: argumen kosong. Contoh: status, diff, log --oneline -5, show HEAD."
	}
	if !gitReadOnlyCommands[fields[0]] {
		return "Error: hanya perintah git baca-saja yang diizinkan (status, diff, log, show, branch, remote)."
	}
	for _, f := range fields[1:] {
		lower := strings.ToLower(f)
		if strings.Contains(lower, "upload-pack") || strings.Contains(lower, "ext::") ||
			strings.HasPrefix(lower, "--output") || strings.HasPrefix(lower, "--exec") || lower == "-c" {
			return "Error: opsi git tersebut tidak diizinkan."
		}
	}
	dir := a.sandboxDir()
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "Error: folder kerja tidak tersedia."
	}
	runCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(runCtx, "git", append([]string{"-C", dir}, fields...)...)
	cmd.Env = append(os.Environ(), "HOME="+dir, "GIT_TERMINAL_PROMPT=0", "GIT_CONFIG_NOSYSTEM=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return truncateToolResult("Error: git " + fields[0] + " gagal: " + strings.TrimSpace(string(out)))
	}
	if strings.TrimSpace(string(out)) == "" {
		return "(tidak ada keluaran)"
	}
	return truncateToolResult(string(out))
}

func findChromeBinary() string {
	if env := strings.TrimSpace(os.Getenv("CHROME_BIN")); env != "" {
		if _, err := os.Stat(env); err == nil {
			return env
		}
	}
	for _, name := range []string{"chromium", "chromium-browser", "google-chrome", "google-chrome-stable", "chrome"} {
		if path, err := exec.LookPath(name); err == nil {
			return path
		}
	}
	home, _ := os.UserHomeDir()
	matches, _ := filepath.Glob(filepath.Join(home, ".cache", "ms-playwright", "chromium-*", "chrome-linux", "chrome"))
	if len(matches) > 0 {
		return matches[len(matches)-1]
	}
	return ""
}

func (a *App) toolScreenshotURL(ctx context.Context, arg string) (string, string) {
	raw := strings.TrimSpace(arg)
	if err := checkPublicURL(ctx, raw); err != nil {
		return "Error: " + err.Error(), ""
	}
	chrome := findChromeBinary()
	if chrome == "" {
		return "Error: browser Chromium tidak ditemukan di server (atur CHROME_BIN).", ""
	}
	dir := a.sandboxDir()
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "Error: folder kerja tidak tersedia.", ""
	}
	shotPath := filepath.Join(dir, "shot-"+uuid()+".png")
	runCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	cmd := exec.CommandContext(runCtx, chrome,
		"--headless=new", "--disable-gpu", "--no-sandbox", "--ignore-certificate-errors",
		"--window-size=1280,800", "--virtual-time-budget=8000",
		"--screenshot="+shotPath, raw)
	cmd.Env = os.Environ()
	out, err := cmd.CombinedOutput()
	if err != nil {
		return truncateToolResult("Error: screenshot gagal: " + strings.TrimSpace(string(out))), ""
	}
	rawImage, err := os.ReadFile(shotPath)
	if err != nil {
		return "Error: file screenshot tidak terbentuk.", ""
	}
	defer os.Remove(shotPath)
	url, err := a.saveImage(rawImage, "images")
	if err != nil {
		return "Error: " + err.Error(), ""
	}
	return "Screenshot " + raw + " terlampir pada balasan.", url
}

func (a *App) toolSummarizeURL(ctx context.Context, arg string) string {
	content := a.toolFetchURL(ctx, arg)
	if strings.HasPrefix(content, "Error:") {
		return content
	}
	return "Isi halaman berikut. Ringkas untuk pengguna dalam Bahasa Indonesia (poin-poin utama, tanpa mengarang):\n\n" + content
}

// --- PDF minimal dari Markdown ---

func pdfEscape(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '\\':
			b.WriteString(`\\`)
		case r == '(':
			b.WriteString(`\(`)
		case r == ')':
			b.WriteString(`\)`)
		case r >= 32 && r <= 126:
			b.WriteRune(r)
		case r == '–' || r == '—':
			b.WriteByte('-')
		case r == '‘' || r == '’':
			b.WriteByte('\'')
		case r == '“' || r == '”':
			b.WriteByte('"')
		case r > 126 && r <= 255:
			b.WriteRune(r)
		default:
			b.WriteByte('?')
		}
	}
	return b.String()
}

func stripInlineMarkdown(s string) string {
	s = strings.ReplaceAll(s, "**", "")
	s = strings.ReplaceAll(s, "__", "")
	s = strings.ReplaceAll(s, "*", "")
	s = strings.ReplaceAll(s, "`", "")
	return s
}

func wrapText(s string, maxChars int) []string {
	if maxChars < 10 {
		maxChars = 10
	}
	words := strings.Fields(s)
	if len(words) == 0 {
		return []string{""}
	}
	var lines []string
	current := ""
	for _, w := range words {
		for len([]rune(w)) > maxChars {
			runes := []rune(w)
			if current != "" {
				lines = append(lines, current)
				current = ""
			}
			lines = append(lines, string(runes[:maxChars]))
			w = string(runes[maxChars:])
		}
		if current == "" {
			current = w
		} else if len([]rune(current))+1+len([]rune(w)) <= maxChars {
			current += " " + w
		} else {
			lines = append(lines, current)
			current = w
		}
	}
	if current != "" {
		lines = append(lines, current)
	}
	return lines
}

type pdfLine struct {
	text string
	size float64
	bold bool
	gap  float64
}

func markdownToPDF(markdown string) []byte {
	const pageW, pageH, margin = 595.0, 842.0, 50.0
	var lines []pdfLine
	for _, rawLine := range strings.Split(markdown, "\n") {
		line := strings.TrimRight(rawLine, " \t\r")
		trimmed := strings.TrimSpace(line)
		size, bold, gap := 11.0, false, 4.0
		switch {
		case strings.HasPrefix(trimmed, "### "):
			trimmed, size, bold, gap = strings.TrimPrefix(trimmed, "### "), 12, true, 8
		case strings.HasPrefix(trimmed, "## "):
			trimmed, size, bold, gap = strings.TrimPrefix(trimmed, "## "), 14, true, 10
		case strings.HasPrefix(trimmed, "# "):
			trimmed, size, bold, gap = strings.TrimPrefix(trimmed, "# "), 18, true, 12
		case strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "* "):
			trimmed = "- " + trimmed[2:]
		case strings.HasPrefix(trimmed, "> "):
			trimmed = trimmed[2:]
		}
		trimmed = stripInlineMarkdown(trimmed)
		if trimmed == "" {
			lines = append(lines, pdfLine{text: "", size: 11, gap: 6})
			continue
		}
		maxChars := int((pageW - 2*margin) / (size * 0.52))
		for i, wrapped := range wrapText(trimmed, maxChars) {
			g := 0.0
			if i == 0 {
				g = gap
			}
			lines = append(lines, pdfLine{text: wrapped, size: size, bold: bold, gap: g})
		}
	}
	// Bagi ke halaman.
	var pages [][]pdfLine
	var current []pdfLine
	y := pageH - margin
	for _, l := range lines {
		need := l.size*1.35 + l.gap
		if y-need < margin && len(current) > 0 {
			pages = append(pages, current)
			current = nil
			y = pageH - margin
		}
		current = append(current, l)
		y -= need
	}
	if len(current) > 0 || len(pages) == 0 {
		pages = append(pages, current)
	}
	// Bangun objek PDF: 1 katalog, 2 pages, 3 font biasa, 4 font tebal,
	// lalu per halaman: page + content.
	var buf bytes.Buffer
	offsets := []int{}
	writeObj := func(body string) int {
		offsets = append(offsets, buf.Len())
		num := len(offsets)
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", num, body)
		return num
	}
	buf.WriteString("%PDF-1.4\n")
	catalogNum := 1
	pagesNum := 2
	fontNum := 3
	fontBoldNum := 4
	pageNums := []int{}
	contentNums := []int{}
	for i := range pages {
		pageNums = append(pageNums, 5+i*2)
		contentNums = append(contentNums, 6+i*2)
	}
	kids := make([]string, len(pageNums))
	for i, n := range pageNums {
		kids[i] = fmt.Sprintf("%d 0 R", n)
	}
	writeObj(fmt.Sprintf("<< /Type /Catalog /Pages %d 0 R >>", pagesNum))
	writeObj(fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", strings.Join(kids, " "), len(pageNums)))
	writeObj("<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding /WinAnsiEncoding >>")
	writeObj("<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica-Bold /Encoding /WinAnsiEncoding >>")
	for i, page := range pages {
		writeObj(fmt.Sprintf("<< /Type /Page /Parent %d 0 R /MediaBox [0 0 %.0f %.0f] /Contents %d 0 R /Resources << /Font << /F1 %d 0 R /F2 %d 0 R >> >> >>",
			pagesNum, pageW, pageH, contentNums[i], fontNum, fontBoldNum))
		var content strings.Builder
		cy := pageH - margin
		for _, l := range page {
			cy -= l.gap + l.size
			if l.text != "" {
				font := "F1"
				if l.bold {
					font = "F2"
				}
				fmt.Fprintf(&content, "BT /%s %.1f Tf %.0f %.1f Td (%s) Tj ET\n", font, l.size, margin, cy, pdfEscape(l.text))
			}
			cy -= l.size * 0.35
		}
		stream := content.String()
		writeObj(fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(stream), stream))
	}
	xrefStart := buf.Len()
	total := len(offsets) + 1
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f \n", total)
	for _, off := range offsets {
		fmt.Fprintf(&buf, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root %d 0 R >>\nstartxref\n%d\n%%%%EOF\n", total, catalogNum, xrefStart)
	return buf.Bytes()
}

func (a *App) toolPDFFromMarkdown(arg string) string {
	lines := strings.SplitN(arg, "\n", 2)
	name := "dokumen.pdf"
	body := arg
	if len(lines) == 2 {
		candidate := strings.TrimSpace(lines[0])
		if strings.HasSuffix(strings.ToLower(candidate), ".pdf") && !strings.Contains(candidate, "/") {
			name = candidate
			body = lines[1]
		}
	}
	if strings.TrimSpace(body) == "" {
		return "Error: isi Markdown kosong."
	}
	path, err := a.sandboxPath(name)
	if err != nil {
		return "Error: " + err.Error()
	}
	pdf := markdownToPDF(body)
	if err := os.MkdirAll(a.sandboxDir(), 0700); err != nil {
		return "Error: folder kerja tidak tersedia."
	}
	if err := os.WriteFile(path, pdf, 0600); err != nil {
		return "Error: gagal menulis PDF."
	}
	return fmt.Sprintf("PDF dibuat: %s (%d byte) di folder kerja.", name, len(pdf))
}
