package musehood

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"time"
)

var messageActionRE = regexp.MustCompile(`^/api/message/([a-f0-9-]{36})/(edit|delete|react|pin|regenerate|rerun|fork|branch|tags|rate)$`)

var reactionEmojis = []string{"👍", "❤️", "😂", "😮", "😢", "🙏", "🔥", "👏"}

// reactionsStatsLocked menghitung total reaksi per emoji dan per kursi
// penerima (fitur 13).
func (a *App) reactionsStatsLocked() map[string]any {
	total := map[string]int{}
	bySeat := map[string]map[string]int{}
	for _, m := range a.data.Messages {
		for _, r := range m.Reactions {
			total[r.Emoji] += r.Count
			if m.AuthorType == "seat" && m.Seat != "" {
				if bySeat[m.Seat] == nil {
					bySeat[m.Seat] = map[string]int{}
				}
				bySeat[m.Seat][r.Emoji] += r.Count
			}
		}
	}
	return map[string]any{"total": total, "bySeat": bySeat}
}

// ratingsStatsLocked menghitung penilaian kualitas balasan (fitur 30)
// per kursi dan per model: jumlah jempol naik (up) dan turun (down).
func (a *App) ratingsStatsLocked() map[string]any {
	bySeat := map[string]map[string]int{}
	byModel := map[string]map[string]int{}
	add := func(target map[string]map[string]int, key string, rating int) {
		if key == "" {
			return
		}
		if target[key] == nil {
			target[key] = map[string]int{"up": 0, "down": 0}
		}
		if rating > 0 {
			target[key]["up"]++
		} else {
			target[key]["down"]++
		}
	}
	for _, m := range a.data.Messages {
		if m.AuthorType != "seat" || m.Rating == 0 {
			continue
		}
		add(bySeat, m.Seat, m.Rating)
		add(byModel, m.Model, m.Rating)
	}
	return map[string]any{"bySeat": bySeat, "byModel": byModel}
}

// rateMessageLocked menyimpan penilaian pemilik atas satu balasan kursi
// (fitur 30): -1, 0, atau 1. Pemanggil wajib memegang a.mu.
func (a *App) rateMessageLocked(id string, rating int) (*message, error) {
	if rating < -1 || rating > 1 {
		return nil, problem(400, "Rating harus -1, 0, atau 1.")
	}
	m := a.findMessageLocked(id)
	if m == nil {
		return nil, problem(404, "Pesan tidak ditemukan.")
	}
	if m.AuthorType != "seat" {
		return nil, problem(400, "Hanya balasan kursi yang dapat dinilai.")
	}
	previous := m.Rating
	m.Rating = rating
	if err := a.saveLocked(); err != nil {
		m.Rating = previous
		return nil, err
	}
	return m, nil
}

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
		// Edit pesan pemilik (fitur 13): teks diganti, balasan sesudahnya
		// di ruang+branch yang sama dihapus, pekerjaan baru dibuat.
		return a.respondLocked(202, func() (any, error) {
			j, err := a.editMessageLocked(id, body.Text)
			if err != nil {
				return nil, err
			}
			return map[string]any{"jobId": j.ID}, nil
		})
	case "branch":
		return a.respondLocked(200, func() (any, error) { return a.branchMessageLocked(id) })
	case "tags":
		var body struct {
			Tags []string `json:"tags"`
		}
		if err := readObject(w, r, &body); err != nil {
			return 0, nil, err
		}
		return a.respondLocked(200, func() (any, error) {
			msg, err := a.tagsMessageLocked(id, body.Tags)
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
	case "pin":
		return a.respondLocked(200, func() (any, error) {
			msg, err := a.pinMessageLocked(id)
			if err != nil {
				return nil, err
			}
			return msg, nil
		})
	case "regenerate":
		return a.respondLocked(202, func() (any, error) {
			j, err := a.regenerateMessageLocked(id)
			if err != nil {
				return nil, err
			}
			return map[string]any{"jobId": j.ID}, nil
		})
	case "rerun":
		return a.respondLocked(202, func() (any, error) {
			j, err := a.rerunMessageLocked(id)
			if err != nil {
				return nil, err
			}
			return map[string]any{"jobId": j.ID}, nil
		})
	case "fork":
		return a.respondLocked(200, func() (any, error) { return a.forkMessageLocked(id) })
	case "rate":
		var body struct {
			Rating int `json:"rating"`
		}
		if err := readObject(w, r, &body); err != nil {
			return 0, nil, err
		}
		return a.respondLocked(200, func() (any, error) {
			msg, err := a.rateMessageLocked(id, body.Rating)
			if err != nil {
				return nil, err
			}
			return msg, nil
		})
	}
	return 0, nil, problem(404, "Rute tidak ditemukan.")
}

// rerunMessageLocked menjalankan ulang diskusi dari satu pesan pemilik
// (fitur 11): semua pesan sesudahnya di ruang yang sama dihapus, pekerjaan
// terkait dibatalkan, lalu pekerjaan baru dibuat dari teks pesan itu.
func (a *App) rerunMessageLocked(id string) (*job, error) {
	msg := a.findMessageLocked(id)
	if msg == nil {
		return nil, problem(404, "Pesan tidak ditemukan atau sudah dihapus.")
	}
	if msg.AuthorType != "owner" {
		return nil, problem(403, "Hanya pesan pemilik yang bisa dijalankan ulang dari sini.")
	}
	return a.truncateAndRerunLocked(msg)
}

// truncateAndRerunLocked menghapus pesan sesudah msg pada ruang+branch
// yang sama (fitur 13 & 14), membatalkan pekerjaan terkait, lalu membuat
// pekerjaan baru dari teks msg. Pemanggil wajib memegang a.mu; bila
// penyimpanan gagal, keadaan dipulihkan penuh.
func (a *App) truncateAndRerunLocked(msg *message) (*job, error) {
	idx := -1
	for i, m := range a.data.Messages {
		if m.ID == msg.ID {
			idx = i
			break
		}
	}
	roomID := msg.RoomID
	branch := msg.branchID()
	removedJobs := map[string]bool{}
	if msg.JobID != "" {
		removedJobs[msg.JobID] = true
	}
	for _, m := range a.data.Messages[idx+1:] {
		if m.RoomID == roomID && m.branchID() == branch && m.JobID != "" {
			removedJobs[m.JobID] = true
		}
	}
	previous := a.data
	keptMessages := make([]*message, 0, len(a.data.Messages))
	for i, m := range a.data.Messages {
		if i > idx && m.RoomID == roomID && m.branchID() == branch {
			continue
		}
		keptMessages = append(keptMessages, m)
	}
	keptJobs := make([]*job, 0, len(a.data.Jobs))
	removed := []*job{}
	for _, j := range a.data.Jobs {
		if removedJobs[j.ID] {
			removed = append(removed, j)
		} else {
			keptJobs = append(keptJobs, j)
		}
	}
	a.data.Messages = keptMessages
	a.data.Jobs = keptJobs
	if a.data.Task != nil && removedJobs[a.data.Task.JobID] {
		a.data.Task = nil
	}
	r := a.data.findRoom(roomID)
	if r == nil {
		a.data = previous
		return nil, problem(404, "Ruang pesan tidak ditemukan.")
	}
	kind, steps := a.routeLocked(msg.Text, r)
	for i := range steps {
		steps[i].Status = "pending"
	}
	j := &job{ID: uuid(), Prompt: msg.Text, Kind: kind, CreatedAt: now(), Status: "processing", Steps: steps, RoomID: roomID, BranchID: branch}
	a.data.Jobs = append(a.data.Jobs, j)
	oldJobID := msg.JobID
	msg.JobID = j.ID
	if kind == "task" {
		a.data.Task = &activeTask{JobID: j.ID, Title: taskPrefix.ReplaceAllString(msg.Text, ""), CurrentStep: ptr(steps[0].Seat), Status: "processing", CreatedAt: now(), RoomID: roomID}
	}
	if err := a.saveLocked(); err != nil {
		a.data = previous
		msg.JobID = oldJobID
		return nil, err
	}
	queueIDs := map[string]bool{}
	for _, old := range removed {
		if cancel, ok := a.running[old]; ok {
			cancel()
			delete(a.running, old)
		}
		for _, s := range old.Steps {
			if s.QueueID != "" {
				queueIDs[s.QueueID] = true
			}
		}
	}
	_ = a.cancelQueueIDsLocked(queueIDs)
	a.advanceLocked(j)
	return j, nil
}

// forkMessageLocked membuat ruang baru berisi salinan pesan sampai pesan
// yang dipilih (fitur 12), lalu menjadikannya ruang aktif.
func (a *App) forkMessageLocked(id string) (any, error) {
	msg := a.findMessageLocked(id)
	if msg == nil {
		return nil, problem(404, "Pesan tidak ditemukan atau sudah dihapus.")
	}
	src := a.data.findRoom(msg.RoomID)
	if src == nil {
		return nil, problem(404, "Ruang pesan tidak ditemukan.")
	}
	snippet := strings.TrimSpace(msg.Text)
	if runes := []rune(snippet); len(runes) > 40 {
		snippet = string(runes[:40]) + "…"
	}
	if snippet == "" {
		snippet = src.Name
	}
	forked := room{ID: newRoomID(), Name: "Fork: " + snippet, CreatedAt: now(),
		SeatCodes: slices.Clone(src.SeatCodes), SeatOrder: slices.Clone(src.SeatOrder), ActiveBranch: msg.branchID()}
	idMap := map[string]string{}
	copied := []*message{}
	for _, m := range a.data.Messages {
		if m.RoomID != src.ID || m.branchID() != msg.branchID() {
			continue
		}
		cp := *m
		cp.ID = uuid()
		cp.RoomID = forked.ID
		cp.JobID = ""
		cp.QueueID = ""
		cp.Reactions = slices.Clone(m.Reactions)
		cp.ToolCalls = slices.Clone(m.ToolCalls)
		cp.ToolsUsed = slices.Clone(m.ToolsUsed)
		if m.ReplyTo != "" {
			cp.ReplyTo = idMap[m.ReplyTo]
		}
		idMap[m.ID] = cp.ID
		copied = append(copied, &cp)
		if m.ID == id {
			break
		}
	}
	previous := a.data
	a.data.Rooms = append(a.data.Rooms, forked)
	a.data.Messages = append(a.data.Messages, copied...)
	a.data.ActiveRoomID = forked.ID
	if err := a.saveLocked(); err != nil {
		a.data = previous
		return nil, err
	}
	return map[string]any{"roomId": forked.ID}, nil
}

// regenerateMessageLocked menghapus balasan kursi dan menjalankannya ulang
// untuk prompt pemilik yang sama (pekerjaan baru satu langkah).
func (a *App) regenerateMessageLocked(id string) (*job, error) {
	msg := a.findMessageLocked(id)
	if msg == nil {
		return nil, problem(404, "Pesan tidak ditemukan atau sudah dihapus.")
	}
	if msg.AuthorType != "seat" {
		return nil, problem(403, "Hanya balasan kursi yang bisa dibuat ulang.")
	}
	if msg.Status == "typing" || msg.Status == "waiting" {
		return nil, problem(409, "Pesan sedang diproses dan belum bisa dibuat ulang.")
	}
	prompt := ""
	stepKind := "chat"
	for _, j := range a.data.Jobs {
		if j.ID != msg.JobID {
			continue
		}
		prompt = j.Prompt
		for _, s := range j.Steps {
			if s.MessageID == id {
				stepKind = s.Kind
			}
		}
		break
	}
	if prompt == "" {
		return nil, problem(404, "Pekerjaan asal pesan tidak ditemukan.")
	}
	previousMessages, previousJobs := a.data.Messages, a.data.Jobs
	kept := make([]*message, 0, len(a.data.Messages))
	for _, m := range a.data.Messages {
		if m.ID != id {
			kept = append(kept, m)
		}
	}
	a.data.Messages = kept
	j := &job{ID: uuid(), Prompt: prompt, Kind: "chat", CreatedAt: now(), Status: "processing",
		Steps: []step{{Seat: msg.Seat, Kind: stepKind, Status: "pending"}}, RoomID: msg.RoomID, BranchID: msg.branchID()}
	a.data.Jobs = append(a.data.Jobs, j)
	if err := a.saveLocked(); err != nil {
		a.data.Messages, a.data.Jobs = previousMessages, previousJobs
		return nil, err
	}
	a.advanceLocked(j)
	return j, nil
}

// pinMessageLocked menyematkan atau melepas sematan satu pesan.
func (a *App) pinMessageLocked(id string) (*message, error) {
	msg := a.findMessageLocked(id)
	if msg == nil {
		return nil, problem(404, "Pesan tidak ditemukan atau sudah dihapus.")
	}
	previous := msg.Pinned
	msg.Pinned = !msg.Pinned
	if err := a.saveLocked(); err != nil {
		msg.Pinned = previous
		return nil, err
	}
	return msg, nil
}

func (a *App) findMessageLocked(id string) *message {
	for _, m := range a.data.Messages {
		if m.ID == id {
			return m
		}
	}
	return nil
}

// editMessageLocked mengedit pesan pemilik lalu menjalankan ulang diskusi
// dari titik itu (fitur 13): teks diganti, pesan sesudahnya di
// ruang+branch yang sama dihapus, pekerjaan terkait dibatalkan, dan
// pekerjaan baru dibuat dari teks baru.
func (a *App) editMessageLocked(id, text string) (*job, error) {
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
	previousText, previousEditedAt := msg.Text, msg.EditedAt
	msg.Text = a.redactLocked(text)
	msg.EditedAt = now()
	j, err := a.truncateAndRerunLocked(msg)
	if err != nil {
		msg.Text, msg.EditedAt = previousText, previousEditedAt
		return nil, err
	}
	return j, nil
}

// branchMessageLocked membuat branch percakapan baru (fitur 14) berisi
// salinan pesan sampai titik yang dipilih pada branch yang sama, lalu
// mengaktifkannya. Branch lama tetap utuh.
func (a *App) branchMessageLocked(id string) (any, error) {
	msg := a.findMessageLocked(id)
	if msg == nil {
		return nil, problem(404, "Pesan tidak ditemukan atau sudah dihapus.")
	}
	src := a.data.findRoom(msg.RoomID)
	if src == nil {
		return nil, problem(404, "Ruang pesan tidak ditemukan.")
	}
	branch := uuid()
	idMap := map[string]string{}
	copied := []*message{}
	for _, m := range a.data.Messages {
		if m.RoomID != src.ID || m.branchID() != msg.branchID() {
			continue
		}
		cp := *m
		cp.ID = uuid()
		cp.BranchID = branch
		cp.JobID = ""
		cp.QueueID = ""
		cp.Reactions = slices.Clone(m.Reactions)
		cp.ToolCalls = slices.Clone(m.ToolCalls)
		cp.ToolsUsed = slices.Clone(m.ToolsUsed)
		cp.Tags = slices.Clone(m.Tags)
		if m.ReplyTo != "" {
			cp.ReplyTo = idMap[m.ReplyTo]
		}
		idMap[m.ID] = cp.ID
		copied = append(copied, &cp)
		if m.ID == id {
			break
		}
	}
	previous := a.data
	previousBranch := src.ActiveBranch
	a.data.Messages = append(a.data.Messages, copied...)
	src.ActiveBranch = branch
	if err := a.saveLocked(); err != nil {
		a.data = previous
		src.ActiveBranch = previousBranch
		return nil, err
	}
	return map[string]any{"branchId": branch, "roomId": src.ID}, nil
}

// switchBranchLocked mengganti branch aktif satu ruang (fitur 14).
func (a *App) switchBranchLocked(roomID, branchID string) (any, error) {
	r := a.data.findRoom(roomID)
	if r == nil {
		return nil, problem(404, "Ruang tidak ditemukan.")
	}
	branchID = strings.TrimSpace(branchID)
	if branchID == "" {
		branchID = "main"
	}
	if branchID != "main" {
		found := false
		for _, m := range a.data.Messages {
			if m.RoomID == r.ID && m.branchID() == branchID {
				found = true
				break
			}
		}
		if !found {
			return nil, problem(404, "Branch tidak ditemukan.")
		}
	}
	previous := r.ActiveBranch
	r.ActiveBranch = branchID
	if err := a.saveLocked(); err != nil {
		r.ActiveBranch = previous
		return nil, err
	}
	return map[string]any{"roomId": r.ID, "branchId": branchID}, nil
}

// tagsMessageLocked memasang tag pada pesan (fitur 17): maks 5 tag,
// tiap tag maks 30 karakter, tanpa duplikat.
func (a *App) tagsMessageLocked(id string, tags []string) (*message, error) {
	msg := a.findMessageLocked(id)
	if msg == nil {
		return nil, problem(404, "Pesan tidak ditemukan atau sudah dihapus.")
	}
	if len(tags) > 5 {
		return nil, problem(400, "Maksimal 5 tag per pesan.")
	}
	cleaned := []string{}
	seen := map[string]bool{}
	for _, tag := range tags {
		tag = strings.TrimSpace(tag)
		if tag == "" {
			continue
		}
		if jsLength(tag) > 30 || strings.ContainsAny(tag, "\x00\r\n") {
			return nil, problem(400, "Tag maksimal 30 karakter tanpa baris baru.")
		}
		if seen[strings.ToLower(tag)] {
			continue
		}
		seen[strings.ToLower(tag)] = true
		cleaned = append(cleaned, a.redactLocked(tag))
	}
	previous := msg.Tags
	msg.Tags = cleaned
	if err := a.saveLocked(); err != nil {
		msg.Tags = previous
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
	previousMessages, previousTask := a.data.Messages, a.data.Task
	kept := make([]*message, 0, len(a.data.Messages))
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
	if err := a.saveLocked(); err != nil {
		a.data.Messages, a.data.Task = previousMessages, previousTask
		return err
	}
	return nil
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
	previousReactions := msg.Reactions
	msg.Reactions = slices.Clone(msg.Reactions)
	if found >= 0 {
		msg.Reactions = append(msg.Reactions[:found], msg.Reactions[found+1:]...)
	} else {
		msg.Reactions = append(msg.Reactions, reaction{Emoji: emoji, Count: 1, ByOwner: true})
	}
	if err := a.saveLocked(); err != nil {
		msg.Reactions = previousReactions
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

// slugifyFilename mengubah nama ruang menjadi slug aman untuk nama file
// ekspor: huruf kecil, spasi dan karakter lain menjadi tanda hubung.
func slugifyFilename(name string) string {
	var b strings.Builder
	lastDash := false
	for _, r := range strings.ToLower(strings.TrimSpace(name)) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			lastDash = false
		} else if !lastDash && b.Len() > 0 {
			b.WriteRune('-')
			lastDash = true
		}
	}
	slug := strings.Trim(b.String(), "-")
	if slug == "" {
		return "ruang"
	}
	return slug
}

// exportChat streams the whole conversation as JSON or Markdown download.
// API keys never leave the app: settings are exported through the same
// redaction path as GET /api/state.
func (a *App) exportChat(w http.ResponseWriter, r *http.Request) {
	if err := method(w, r, http.MethodGet); err != nil {
		a.writeJSON(w, r, 0, nil, err)
		return
	}
	format := r.URL.Query().Get("format")
	if format == "" {
		format = "json"
	}
	if format != "json" && format != "md" && format != "html" && format != "markdown" {
		a.writeJSON(w, r, 0, nil, problem(400, "Format ekspor harus json, md, html, atau markdown."))
		return
	}
	a.mu.Lock()
	rm := a.data.activeRoom()
	if roomID := r.URL.Query().Get("room"); roomID != "" {
		rm = a.data.findRoom(roomID)
	}
	if rm == nil {
		a.mu.Unlock()
		a.writeJSON(w, r, 0, nil, problem(400, "Ruang tidak ditemukan."))
		return
	}
	roomName := rm.Name
	roomMessages := []*message{}
	for _, m := range a.data.Messages {
		if m.RoomID == rm.ID {
			roomMessages = append(roomMessages, m)
		}
	}
	// Ekspor satu diskusi (fitur 16): pesan terpilih + semua pesan dalam
	// pekerjaan yang sama; pesan pemilik tanpa jobId memakai balasan
	// berikutnya sampai pesan pemilik berikutnya.
	filenamePrefix := "riwayat-musehood"
	if messageID := r.URL.Query().Get("message"); messageID != "" {
		targetIdx := -1
		for i, m := range roomMessages {
			if m.ID == messageID {
				targetIdx = i
				break
			}
		}
		if targetIdx < 0 {
			a.mu.Unlock()
			a.writeJSON(w, r, 0, nil, problem(400, "Pesan untuk ekspor tidak ditemukan."))
			return
		}
		target := roomMessages[targetIdx]
		filtered := []*message{}
		switch {
		case target.JobID != "":
			for _, m := range roomMessages {
				if m.JobID == target.JobID {
					filtered = append(filtered, m)
				}
			}
		case target.AuthorType == "owner":
			filtered = append(filtered, target)
			for _, m := range roomMessages[targetIdx+1:] {
				if m.AuthorType == "owner" {
					break
				}
				filtered = append(filtered, m)
			}
		default:
			filtered = append(filtered, target)
		}
		roomMessages = filtered
		filenamePrefix = "diskusi-musehood"
	}
	// Ekspor Markdown untuk Notion/Obsidian (fitur 29): frontmatter YAML,
	// judul ruang, tiap pesan sebagai blok "### Nama — jam", gambar
	// sebagai tautan relatif.
	if format == "markdown" {
		seatNames := map[string]string{}
		for _, s := range a.data.Seats {
			seatNames[s.Code] = s.Name
		}
		ownerName := a.data.Owner.Name
		exported := make([]*message, len(roomMessages))
		copy(exported, roomMessages)
		a.mu.Unlock()
		today := time.Now().Format("2006-01-02")
		filename := slugifyFilename(roomName) + "-" + today + ".md"
		var buf bytes.Buffer
		buf.WriteString("---\n")
		fmt.Fprintf(&buf, "title: %q\n", roomName)
		fmt.Fprintf(&buf, "date: %s\n", today)
		buf.WriteString("tags: [musehood]\n---\n\n")
		fmt.Fprintf(&buf, "# %s\n\n", roomName)
		for _, m := range exported {
			name := ownerName
			if m.AuthorType == "owner" {
				if m.Author != "" {
					name = m.Author
				}
			} else if seatName, ok := seatNames[m.Seat]; ok {
				name = seatName
			} else if m.Seat != "" {
				name = m.Seat
			} else if m.Model != "" {
				name = m.Model
			}
			stamp, err := time.Parse(time.RFC3339Nano, m.CreatedAt)
			if err != nil {
				stamp = time.Time{}
			}
			fmt.Fprintf(&buf, "### %s — %s\n\n", name, stamp.Format("15:04"))
			if m.ImageURL != "" {
				fmt.Fprintf(&buf, "![gambar](%s)\n\n", m.ImageURL)
			}
			if m.Text != "" {
				fmt.Fprintf(&buf, "%s\n\n", m.Text)
			}
			if m.Error != "" {
				fmt.Fprintf(&buf, "> Error: %s\n\n", m.Error)
			}
		}
		w.Header().Set("Content-Disposition", "attachment; filename=\""+filename+"\"")
		w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
		w.Write(buf.Bytes())
		return
	}
	rawState, err := json.Marshal(a.safeLocked(a.stateLocked()))
	rawMessages, merr := json.Marshal(a.safeLocked(roomMessages))
	a.mu.Unlock()
	if err != nil || merr != nil {
		a.writeJSON(w, r, 0, nil, problem(500, "Gagal menyusun ekspor."))
		return
	}
	stateMap := map[string]any{}
	if err := json.Unmarshal(rawState, &stateMap); err != nil {
		a.writeJSON(w, r, 0, nil, problem(500, "Gagal menyusun ekspor."))
		return
	}
	var roomMessagesAny []any
	if err := json.Unmarshal(rawMessages, &roomMessagesAny); err != nil {
		a.writeJSON(w, r, 0, nil, problem(500, "Gagal menyusun ekspor."))
		return
	}
	stateMap["messages"] = roomMessagesAny
	// Kunci API tidak ikut ter-export sama sekali (bukan hanya ter-mask).
	if settings, ok := stateMap["settings"].(map[string]any); ok {
		delete(settings, "apiKey")
	}

	if format == "json" {
		payload := map[string]any{
			"exportedAt": time.Now().Format(time.RFC3339),
			"room":       roomName,
			"group":      stateMap["group"],
			"owner":      stateMap["owner"],
			"seats":      stateMap["seats"],
			"settings":   stateMap["settings"],
			"messages":   stateMap["messages"],
		}
		raw, err := json.MarshalIndent(payload, "", "  ")
		if err != nil {
			a.writeJSON(w, r, 0, nil, problem(500, "Gagal menyusun ekspor."))
			return
		}
		w.Header().Set("Content-Disposition", "attachment; filename=\""+filenamePrefix+".json\"")
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Write(append(raw, '\n'))
		return
	}

	group, _ := stateMap["group"].(map[string]any)
	owner, _ := stateMap["owner"].(map[string]any)
	groupName, _ := group["name"].(string)
	ownerName, _ := owner["name"].(string)
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

	if format == "html" {
		w.Header().Set("Content-Disposition", "attachment; filename=\""+filenamePrefix+".html\"")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		var buf bytes.Buffer
		fmt.Fprintf(&buf, "<!DOCTYPE html><html lang=\"id\"><head><meta charset=\"utf-8\"><title>Riwayat %s — %s</title><style>body{font-family:system-ui,sans-serif;max-width:760px;margin:2rem auto;padding:0 1rem;line-height:1.5}h2{border-bottom:1px solid #ccc;padding-bottom:.3rem}.msg{margin:1rem 0}.meta{color:#666;font-size:.85em}.err{color:#b00;white-space:pre-wrap}pre{white-space:pre-wrap;font:inherit}@media print{body{margin:0}}</style></head><body>", html.EscapeString(groupName), html.EscapeString(roomName))
		fmt.Fprintf(&buf, "<h1>Riwayat %s — Ruang %s</h1><p>Pemilik: %s · Diekspor: %s</p>", html.EscapeString(groupName), html.EscapeString(roomName), html.EscapeString(ownerName), time.Now().Format("2006-01-02 15:04"))
		currentDay := ""
		for _, raw := range messages {
			m, _ := raw.(map[string]any)
			created, _ := m["createdAt"].(string)
			stamp, err := time.Parse(time.RFC3339Nano, created)
			if err != nil {
				stamp = time.Time{}
			}
			if day := stamp.Format("2006-01-02"); day != currentDay {
				currentDay = day
				fmt.Fprintf(&buf, "<h2>%s</h2>", day)
			}
			fmt.Fprintf(&buf, "<div class=\"msg\"><div class=\"meta\"><strong>%s</strong> — %s", html.EscapeString(authorName(m)), stamp.Format("15:04"))
			if label, ok := m["label"].(string); ok && label != "" {
				fmt.Fprintf(&buf, " [%s]", html.EscapeString(label))
			}
			buf.WriteString("</div>")
			if image, ok := m["imageUrl"].(string); ok && image != "" {
				fmt.Fprintf(&buf, "<p>[gambar: %s]</p>", html.EscapeString(image))
			}
			if text, ok := m["text"].(string); ok && text != "" {
				fmt.Fprintf(&buf, "<pre>%s</pre>", html.EscapeString(text))
			}
			if errorText, ok := m["error"].(string); ok && errorText != "" {
				fmt.Fprintf(&buf, "<p class=\"err\">Error: %s</p>", html.EscapeString(errorText))
			}
			buf.WriteString("</div>")
		}
		buf.WriteString("</body></html>")
		w.Write(buf.Bytes())
		return
	}

	w.Header().Set("Content-Disposition", "attachment; filename=\""+filenamePrefix+".md\"")
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "# Riwayat %s — Ruang %s\n\n", groupName, roomName)
	fmt.Fprintf(&buf, "Pemilik: %s\n\nDiekspor: %s\n\n---\n\n", ownerName, time.Now().Format("2006-01-02 15:04"))
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
