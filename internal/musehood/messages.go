package musehood

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"
)

var messageActionRE = regexp.MustCompile(`^/api/message/([a-f0-9-]{36})/(edit|delete|react)$`)

var reactionEmojis = []string{"👍", "❤️", "😂", "😮", "😢", "🔥"}

func messageAction(path string) (string, string, bool) {
	m := messageActionRE.FindStringSubmatch(path)
	if m == nil {
		return "", "", false
	}
	return m[1], m[2], true
}

func (a *App) messageAction(w http.ResponseWriter, r *http.Request, id, action string) (int, json.RawMessage, error) {
	if err := method(w, r, http.MethodPost); err != nil {
		return 0, nil, err
	}
	switch action {
	case "edit":
		var body struct {
			Text string `json:"text"`
		}
		if err := readObject(w, r, &body); err != nil {
			return 0, nil, err
		}
		return a.respondLocked(200, func() (any, error) {
			msg, err := a.editMessageLocked(id, body.Text)
			if err != nil {
				return nil, err
			}
			return msg, nil
		})
	case "delete":
		return a.respondLocked(200, func() (any, error) { return map[string]bool{"ok": true}, a.deleteMessageLocked(id) })
	case "react":
		var body struct {
			Emoji string `json:"emoji"`
		}
		if err := readObject(w, r, &body); err != nil {
			return 0, nil, err
		}
		return a.respondLocked(200, func() (any, error) {
			msg, err := a.reactMessageLocked(id, body.Emoji)
			if err != nil {
				return nil, err
			}
			return msg, nil
		})
	}
	return 0, nil, problem(404, "Rute tidak ditemukan.")
}

func (a *App) findMessageLocked(id string) *message {
	for _, m := range a.data.Messages {
		if m.ID == id {
			return m
		}
	}
	return nil
}

func (a *App) editMessageLocked(id, text string) (*message, error) {
	msg := a.findMessageLocked(id)
	if msg == nil {
		return nil, problem(404, "Pesan tidak ditemukan atau sudah dihapus.")
	}
	if msg.AuthorType != "owner" {
		return nil, problem(403, "Hanya pesan pemilik yang bisa diedit.")
	}
	if msg.Status == "typing" || msg.Status == "waiting" {
		return nil, problem(409, "Pesan sedang diproses dan belum bisa diedit.")
	}
	text = strings.TrimSpace(text)
	if text == "" || jsLength(text) > 20000 {
		return nil, problem(400, "Pesan wajib berisi teks, maksimal 20.000 karakter.")
	}
	msg.Text = a.redactLocked(text)
	msg.EditedAt = now()
	if err := a.saveLocked(); err != nil {
		return nil, err
	}
	return msg, nil
}

func (a *App) deleteMessageLocked(id string) error {
	msg := a.findMessageLocked(id)
	if msg == nil {
		return problem(404, "Pesan tidak ditemukan atau sudah dihapus.")
	}
	if msg.Status == "typing" || msg.Status == "waiting" {
		return problem(409, "Pesan sedang diproses dan belum bisa dihapus.")
	}
	kept := a.data.Messages[:0]
	for _, m := range a.data.Messages {
		if m.ID != id {
			kept = append(kept, m)
		}
	}
	a.data.Messages = kept
	// Bersihkan status tugas aktif bila tidak ada pesan tugas tersisa.
	if a.data.Task != nil {
		remaining := false
		for _, m := range a.data.Messages {
			if m.JobID == a.data.Task.JobID {
				remaining = true
				break
			}
		}
		if !remaining {
			a.data.Task = nil
		}
	}
	return a.saveLocked()
}

func (a *App) reactMessageLocked(id, emoji string) (*message, error) {
	msg := a.findMessageLocked(id)
	if msg == nil {
		return nil, problem(404, "Pesan tidak ditemukan atau sudah dihapus.")
	}
	allowed := false
	for _, candidate := range reactionEmojis {
		if candidate == emoji {
			allowed = true
			break
		}
	}
	if !allowed {
		return nil, problem(400, "Reaksi harus salah satu dari 👍 ❤️ 😂 😮 😢 🔥.")
	}
	found := -1
	for i, r := range msg.Reactions {
		if r.Emoji == emoji && r.ByOwner {
			found = i
			break
		}
	}
	if found >= 0 {
		msg.Reactions = append(msg.Reactions[:found], msg.Reactions[found+1:]...)
	} else {
		msg.Reactions = append(msg.Reactions, reaction{Emoji: emoji, Count: 1, ByOwner: true})
	}
	if err := a.saveLocked(); err != nil {
		return nil, err
	}
	return msg, nil
}

// uploadImage stores an owner-supplied picture for inline chat display.
func (a *App) uploadImage(w http.ResponseWriter, r *http.Request) (int, json.RawMessage, error) {
	r.Body = http.MaxBytesReader(w, r.Body, avatarLimit+64*1024)
	if err := r.ParseMultipartForm(avatarLimit + 64*1024); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return 0, nil, problem(413, "Ukuran gambar maksimal 2 MB.")
		}
		return 0, nil, problem(400, "Unggahan gambar harus berupa multipart yang valid.")
	}
	defer r.MultipartForm.RemoveAll()
	if len(r.MultipartForm.File["image"]) != 1 {
		return 0, nil, problem(400, "Pilih satu file gambar untuk diunggah.")
	}
	file, err := r.MultipartForm.File["image"][0].Open()
	if err != nil {
		return 0, nil, problem(400, "Pilih file gambar untuk diunggah.")
	}
	defer file.Close()
	raw, large, err := limitedRead(file, avatarLimit)
	if large {
		return 0, nil, problem(413, "Ukuran gambar maksimal 2 MB.")
	}
	if err != nil {
		return 0, nil, err
	}
	path, err := a.saveImage(raw, "images")
	if err != nil {
		return 0, nil, err
	}
	return 200, mustJSON(map[string]string{"imageUrl": path}), nil
}

func mustJSON(value any) json.RawMessage {
	raw, _ := json.Marshal(value)
	return raw
}

// exportChat streams the whole conversation as JSON or Markdown download.
// API keys never leave the app: settings are exported through the same
// redaction path as GET /api/state.
func (a *App) exportChat(w http.ResponseWriter, r *http.Request) {
	if err := method(w, r, http.MethodGet); err != nil {
		a.writeJSON(w, 0, nil, err)
		return
	}
	format := r.URL.Query().Get("format")
	if format == "" {
		format = "json"
	}
	if format != "json" && format != "md" {
		a.writeJSON(w, 0, nil, problem(400, "Format ekspor harus json atau md."))
		return
	}
	a.mu.Lock()
	state := a.stateLocked()
	a.mu.Unlock()
	rawState, err := json.Marshal(state)
	if err != nil {
		a.writeJSON(w, 0, nil, problem(500, "Gagal menyusun ekspor."))
		return
	}
	stateMap := map[string]any{}
	if err := json.Unmarshal(rawState, &stateMap); err != nil {
		a.writeJSON(w, 0, nil, problem(500, "Gagal menyusun ekspor."))
		return
	}
	// Kunci API tidak ikut ter-export sama sekali (bukan hanya ter-mask).
	if settings, ok := stateMap["settings"].(map[string]any); ok {
		delete(settings, "apiKey")
	}

	if format == "json" {
		payload := map[string]any{
			"exportedAt": time.Now().Format(time.RFC3339),
			"group":      stateMap["group"],
			"owner":      stateMap["owner"],
			"seats":      stateMap["seats"],
			"settings":   stateMap["settings"],
			"messages":   stateMap["messages"],
		}
		raw, err := json.MarshalIndent(payload, "", "  ")
		if err != nil {
			a.writeJSON(w, 0, nil, problem(500, "Gagal menyusun ekspor."))
			return
		}
		w.Header().Set("Content-Disposition", `attachment; filename="riwayat-musehood.json"`)
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Write(append(raw, '\n'))
		return
	}

	w.Header().Set("Content-Disposition", `attachment; filename="riwayat-musehood.md"`)
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	var buf bytes.Buffer
	group, _ := stateMap["group"].(map[string]any)
	owner, _ := stateMap["owner"].(map[string]any)
	groupName, _ := group["name"].(string)
	ownerName, _ := owner["name"].(string)
	fmt.Fprintf(&buf, "# Riwayat %s\n\n", groupName)
	fmt.Fprintf(&buf, "Pemilik: %s\n\nDiekspor: %s\n\n---\n\n", ownerName, time.Now().Format("2006-01-02 15:04"))
	names := map[string]string{ownerName: ownerName}
	seats, _ := stateMap["seats"].([]any)
	for _, s := range seats {
		seat, _ := s.(map[string]any)
		code, _ := seat["code"].(string)
		name, _ := seat["name"].(string)
		names[code] = name
	}
	authorName := func(m map[string]any) string {
		authorType, _ := m["authorType"].(string)
		seat, _ := m["seat"].(string)
		if authorType == "owner" {
			return ownerName
		}
		if name, ok := names[seat]; ok {
			return name
		}
		return seat
	}
	messages, _ := stateMap["messages"].([]any)
	currentDay := ""
	for _, raw := range messages {
		m, _ := raw.(map[string]any)
		created, _ := m["createdAt"].(string)
		stamp, err := time.Parse(time.RFC3339Nano, created)
		if err != nil {
			stamp = time.Time{}
		}
		day := stamp.Format("2006-01-02")
		if day != currentDay {
			currentDay = day
			fmt.Fprintf(&buf, "\n## %s\n\n", day)
		}
		author := authorName(m)
		fmt.Fprintf(&buf, "**%s** — %s", author, stamp.Format("15:04"))
		if label, ok := m["label"].(string); ok && label != "" {
			fmt.Fprintf(&buf, " _[%s]_", label)
		}
		buf.WriteString("\n\n")
		if image, ok := m["imageUrl"].(string); ok && image != "" {
			fmt.Fprintf(&buf, "[gambar: %s]\n\n", image)
		}
		if text, ok := m["text"].(string); ok && text != "" {
			fmt.Fprintf(&buf, "%s\n\n", text)
		}
		if errorText, ok := m["error"].(string); ok && errorText != "" {
			fmt.Fprintf(&buf, "> Error: %s\n\n", errorText)
		}
	}
	w.Write(buf.Bytes())
}
