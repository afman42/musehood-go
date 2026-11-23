package musehood

import (
	"encoding/json"
	"errors"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const uploadLimit = 10 * 1024 * 1024

// storedUpload matches files saved by saveUpload: <uuid>-<sanitized-name>.
var storedUpload = regexp.MustCompile(`^[a-f0-9-]{36}-[A-Za-z0-9._-]{1,200}$`)

// sanitizeUploadName keeps only filesystem-safe characters from the original
// filename. The result is never a path and never empty.
func sanitizeUploadName(name string) string {
	base := filepath.Base(name)
	var b strings.Builder
	for _, r := range base {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '.' || r == '-' || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	out := strings.Trim(b.String(), "._")
	if out == "" {
		out = "file"
	}
	if len(out) > 100 {
		ext := filepath.Ext(out)
		if len(ext) > 20 {
			ext = ""
		}
		out = out[:100-len(ext)] + ext
	}
	return out
}

// saveUpload stores an uploaded file under uploads/ and returns its metadata.
// The stored filename is <uuid>-<sanitized-original-name> so the original name
// stays visible while the path can never be chosen by the client.
func (a *App) saveUpload(raw []byte, originalName string) (*attachment, error) {
	if len(raw) == 0 {
		return nil, problem(400, "File kosong.")
	}
	if len(raw) > uploadLimit {
		return nil, problem(413, "Ukuran file maksimal 10 MB.")
	}
	name := sanitizeUploadName(originalName)
	stored := uuid() + "-" + name
	if err := os.WriteFile(filepath.Join(a.dir, "uploads", stored), raw, 0600); err != nil {
		return nil, err
	}
	mimeType := mime.TypeByExtension(filepath.Ext(name))
	if mimeType == "" {
		mimeType = http.DetectContentType(raw)
	}
	return &attachment{URL: "/uploads/" + stored, Name: name, Size: int64(len(raw)), Mime: mimeType}, nil
}

// validateAttachment checks that a client-supplied attachment refers to a file
// this server stored via /api/upload-file. It does not trust the URL alone:
// the file must exist on disk with the recorded size.
func (a *App) validateAttachment(att *attachment) error {
	if att == nil {
		return nil
	}
	filename := strings.TrimPrefix(att.URL, "/uploads/")
	if att.URL == filename || !storedUpload.MatchString(filename) {
		return problem(400, "Lampiran harus berupa file hasil unggahan (/uploads/…).")
	}
	if att.Name == "" || att.Size <= 0 || att.Size > uploadLimit {
		return problem(400, "Lampiran tidak valid.")
	}
	stat, err := os.Stat(filepath.Join(a.dir, "uploads", filename))
	if err != nil {
		return problem(400, "File lampiran tidak ditemukan di penyimpanan lokal.")
	}
	if stat.Size() != att.Size {
		return problem(400, "Ukuran lampiran tidak cocok dengan file tersimpan.")
	}
	return nil
}

// uploadFile stores one owner-uploaded file for a chat attachment.
func (a *App) uploadFile(w http.ResponseWriter, r *http.Request) (int, json.RawMessage, error) {
	r.Body = http.MaxBytesReader(w, r.Body, uploadLimit+64*1024)
	if err := r.ParseMultipartForm(uploadLimit + 64*1024); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return 0, nil, problem(413, "Ukuran file maksimal 10 MB.")
		}
		return 0, nil, problem(400, "Unggahan file harus berupa multipart yang valid.")
	}
	defer r.MultipartForm.RemoveAll()
	if len(r.MultipartForm.File["file"]) != 1 {
		return 0, nil, problem(400, "Pilih satu file untuk diunggah.")
	}
	upload := r.MultipartForm.File["file"][0]
	file, err := upload.Open()
	if err != nil {
		return 0, nil, problem(400, "Pilih file untuk diunggah.")
	}
	defer file.Close()
	raw, large, err := limitedRead(file, uploadLimit)
	if large {
		return 0, nil, problem(413, "Ukuran file maksimal 10 MB.")
	}
	if err != nil {
		return 0, nil, err
	}
	att, err := a.saveUpload(raw, upload.Filename)
	if err != nil {
		return 0, nil, err
	}
	return 200, mustJSON(att), nil
}
