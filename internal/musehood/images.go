package musehood

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const imageLimit = 10 * 1024 * 1024
const avatarLimit = 2 * 1024 * 1024

var localImage = regexp.MustCompile(`^/images/[a-zA-Z0-9_-]+\.(png|jpe?g|gif|webp)$`)
var inlineImage = regexp.MustCompile(`^data:image/(png|jpeg|gif|webp);base64,`)
var uploadName = regexp.MustCompile(`^[a-zA-Z0-9_-]+\.(png|jpe?g|gif|webp)$`)

func imageExtension(raw []byte) string {
	switch {
	case len(raw) >= 24 && string(raw[:8]) == "\x89PNG\r\n\x1a\n":
		return "png"
	case len(raw) >= 4 && string(raw[:3]) == "\xff\xd8\xff":
		return "jpg"
	case len(raw) >= 10 && (string(raw[:6]) == "GIF87a" || string(raw[:6]) == "GIF89a"):
		return "gif"
	case len(raw) >= 16 && string(raw[:4]) == "RIFF" && string(raw[8:12]) == "WEBP":
		return "webp"
	default:
		return ""
	}
}
func (a *App) saveImage(raw []byte, folder string) (string, error) {
	ext := imageExtension(raw)
	if ext == "" {
		return "", problem(400, "File bukan gambar PNG, JPEG, GIF, atau WebP yang valid. SVG upload tidak diizinkan.")
	}
	if len(raw) > imageLimit {
		return "", problem(413, "Gambar hasil terlalu besar (maksimal 10 MB).")
	}
	filename := uuid() + "." + ext
	if err := os.WriteFile(filepath.Join(a.dir, folder, filename), raw, 0600); err != nil {
		return "", err
	}
	return "/" + folder + "/" + filename, nil
}
func (a *App) resolveImage(ctx context.Context, value string) (string, error) {
	if localImage.MatchString(value) {
		f, err := os.Open(filepath.Join(a.dir, strings.TrimPrefix(value, "/")))
		if err != nil {
			return "", problem(400, "File gambar pekerja tidak ditemukan di images/.")
		}
		defer f.Close()
		raw, large, err := limitedRead(f, imageLimit)
		if large {
			return "", problem(413, "Gambar hasil terlalu besar (maksimal 10 MB).")
		}
		if err != nil || imageExtension(raw) == "" {
			return "", problem(400, "File gambar pekerja tidak valid.")
		}
		return value, nil
	}
	if prefix := inlineImage.FindString(value); prefix != "" {
		encoded := strings.Join(strings.Fields(strings.TrimPrefix(value, prefix)), "")
		if len(encoded) > base64.StdEncoding.EncodedLen(imageLimit) {
			return "", problem(413, "Gambar hasil terlalu besar (maksimal 10 MB).")
		}
		raw, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil {
			return "", problem(400, "Data gambar Base64 tidak valid.")
		}
		return a.saveImage(raw, "images")
	}
	u, err := url.Parse(value)
	if err != nil || !validHTTPURL(u) {
		return "", problem(400, "URL gambar harus HTTP/HTTPS atau file lokal /images/.")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return "", problem(400, "URL gambar tidak valid.")
	}
	res, err := a.client.Do(req)
	if err != nil {
		return "", problem(502, "Gagal mengambil gambar: "+err.Error())
	}
	defer res.Body.Close()
	raw, large, err := limitedRead(res.Body, imageLimit)
	if large {
		return "", problem(413, "Gambar hasil terlalu besar (maksimal 10 MB).")
	}
	if err != nil {
		return "", problem(502, "Gagal membaca gambar: "+err.Error())
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return "", problem(502, fmt.Sprintf("HTTP %d: %s", res.StatusCode, raw))
	}
	return a.saveImage(raw, "images")
}
