package musehood

import (
	"context"
	"fmt"
	"html"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// --- Tool browser headless (Chromium CLI, server-side) ---
//
// browser_open me-render halaman dengan Chromium headless lalu
// mengembalikan teks DOM hasil render (termasuk konten yang dibangun
// JavaScript); browser_screenshot mengambil PNG halaman ter-render;
// browser_links mendaftar tautan dari DOM ter-render. Semua melewati
// penjaga SSRF checkPublicURL. Klik/form/Lighthouse tidak didukung
// (membutuhkan protokol CDP interaktif) — batas jujur yang dicatat di
// README. Bila Chromium tidak ditemukan, tool mengembalikan error
// jelas berbahasa Indonesia (degradasi anggun seperti ocr_image).

const browserTimeout = 45 * time.Second
const maxBrowserLinks = 50

// chromiumProxyArgs mengembalikan argumen --proxy-server dari variabel
// lingkungan proxy bila ada (egress VM hanya lewat proxy; Chromium
// tidak membaca variabel lingkungan proxy sendiri).
func chromiumProxyArgs() []string {
	for _, name := range []string{"HTTPS_PROXY", "https_proxy", "HTTP_PROXY", "http_proxy"} {
		if value := strings.TrimSpace(os.Getenv(name)); value != "" {
			return []string{"--proxy-server=" + value}
		}
	}
	return nil
}

func chromiumBaseArgs() []string {
	args := []string{
		"--headless=new", "--disable-gpu", "--no-sandbox",
		"--ignore-certificate-errors", "--disable-dev-shm-usage",
		"--window-size=1280,800", "--virtual-time-budget=8000",
	}
	return append(args, chromiumProxyArgs()...)
}

// browserDumpDOM menjalankan Chromium headless dan mengembalikan DOM
// hasil render. Dipisah dari tool agar dapat diuji dengan URL lokal
// (data:/file:) tanpa melemahkan penjaga SSRF pada jalur tool.
func (a *App) browserDumpDOM(ctx context.Context, rawURL string) (string, error) {
	chrome := findChromium()
	if chrome == "" {
		return "", fmt.Errorf("chromium tidak ditemukan")
	}
	runCtx, cancel := context.WithTimeout(ctx, browserTimeout)
	defer cancel()
	args := append(chromiumBaseArgs(), "--dump-dom", rawURL)
	cmd := exec.CommandContext(runCtx, chrome, args...)
	cmd.Env = os.Environ()
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func (a *App) toolBrowserOpen(ctx context.Context, arg string) string {
	raw := strings.TrimSpace(arg)
	if err := checkPublicURL(ctx, raw); err != nil {
		return "Error: " + err.Error()
	}
	if findChromium() == "" {
		return chromiumMissingError
	}
	dom, err := a.browserDumpDOM(ctx, raw)
	if err != nil {
		return "Error: gagal me-render halaman dengan Chromium: " + err.Error()
	}
	text := htmlToText(dom)
	if text == "" {
		return "Halaman ter-render tanpa teks yang terbaca."
	}
	return truncateToolResult(text)
}

func (a *App) toolBrowserScreenshot(ctx context.Context, arg string) (string, string) {
	raw := strings.TrimSpace(arg)
	if err := checkPublicURL(ctx, raw); err != nil {
		return "Error: " + err.Error(), ""
	}
	chrome := findChromium()
	if chrome == "" {
		return chromiumMissingError, ""
	}
	dir := a.sandboxDir()
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "Error: folder kerja tidak tersedia.", ""
	}
	shotPath := filepath.Join(dir, "browser-"+uuid()+".png")
	runCtx, cancel := context.WithTimeout(ctx, browserTimeout)
	defer cancel()
	args := append(chromiumBaseArgs(), "--screenshot="+shotPath, raw)
	cmd := exec.CommandContext(runCtx, chrome, args...)
	cmd.Env = os.Environ()
	out, err := cmd.CombinedOutput()
	if err != nil {
		return truncateToolResult("Error: screenshot browser gagal: " + strings.TrimSpace(string(out))), ""
	}
	rawImage, err := os.ReadFile(shotPath)
	if err != nil {
		return "Error: file screenshot tidak terbentuk.", ""
	}
	defer os.Remove(shotPath)
	imageURL, err := a.saveImage(rawImage, "images")
	if err != nil {
		return "Error: " + err.Error(), ""
	}
	return "Screenshot halaman ter-render " + raw + " terlampir pada balasan.", imageURL
}

var anchorPattern = regexp.MustCompile(`(?is)<a\s[^>]*href\s*=\s*["']([^"']+)["'][^>]*>(.*?)</a>`)

// extractLinks mendaftar tautan dari HTML/DOM: "teks → URL absolut".
func extractLinks(dom, base string) []string {
	baseURL, _ := url.Parse(base)
	seen := map[string]bool{}
	var lines []string
	for _, match := range anchorPattern.FindAllStringSubmatch(dom, -1) {
		href := html.UnescapeString(strings.TrimSpace(match[1]))
		if href == "" || strings.HasPrefix(href, "#") || strings.HasPrefix(href, "javascript:") || strings.HasPrefix(href, "mailto:") || strings.HasPrefix(href, "tel:") {
			continue
		}
		absolute := href
		if baseURL != nil {
			if resolved, err := baseURL.Parse(href); err == nil {
				absolute = resolved.String()
			}
		}
		if seen[absolute] {
			continue
		}
		seen[absolute] = true
		text := strings.TrimSpace(htmlToText(match[2]))
		if text == "" {
			text = "(tanpa teks)"
		}
		lines = append(lines, fmt.Sprintf("%s → %s", truncateRunes(text, 80), absolute))
		if len(lines) >= maxBrowserLinks {
			break
		}
	}
	return lines
}

func (a *App) toolBrowserLinks(ctx context.Context, arg string) string {
	raw := strings.TrimSpace(arg)
	if err := checkPublicURL(ctx, raw); err != nil {
		return "Error: " + err.Error()
	}
	if findChromium() == "" {
		return chromiumMissingError
	}
	dom, err := a.browserDumpDOM(ctx, raw)
	if err != nil {
		return "Error: gagal me-render halaman dengan Chromium: " + err.Error()
	}
	lines := extractLinks(dom, raw)
	if len(lines) == 0 {
		return "Tidak ada tautan ditemukan pada halaman ter-render."
	}
	return truncateToolResult(strings.Join(lines, "\n"))
}
