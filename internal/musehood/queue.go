package musehood

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var queueID = regexp.MustCompile(`^[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$`)

type queueRequest struct {
	ID        string      `json:"id"`
	Seat      string      `json:"seat"`
	Kind      string      `json:"kind"`
	Prompt    string      `json:"prompt"`
	Context   chatContext `json:"context"`
	CreatedAt string      `json:"createdAt"`
}
type workerReply struct {
	Text     string `json:"text"`
	ImageURL string `json:"imageUrl,omitempty"`
	Error    string `json:"error,omitempty"`
}

func (a *App) queuePath(folder, id string) string {
	return filepath.Join(a.dir, "queue", folder, id+".json")
}
func (a *App) activeQueueLocked(id string) *step {
	if !queueID.MatchString(id) {
		return nil
	}
	for _, j := range a.data.Jobs {
		if j.Status != "completed" && j.Index < len(j.Steps) {
			s := &j.Steps[j.Index]
			if s.QueueID == id {
				return s
			}
		}
	}
	return nil
}
func (a *App) readQueueRequest(id string) (queueRequest, error) {
	var q queueRequest
	raw, err := os.ReadFile(a.queuePath("requests", id))
	if err == nil {
		err = json.Unmarshal(raw, &q)
	}
	return q, err
}
func (a *App) pendingLocked() []queueRequest {
	pending := []queueRequest{}
	for _, j := range a.data.Jobs {
		if j.Status == "completed" || j.Index >= len(j.Steps) {
			continue
		}
		id := j.Steps[j.Index].QueueID
		if !queueID.MatchString(id) {
			continue
		}
		if _, err := os.Stat(a.queuePath("responses", id)); !errors.Is(err, os.ErrNotExist) {
			continue
		}
		if q, err := a.readQueueRequest(id); err == nil {
			pending = append(pending, q)
		}
	}
	return pending
}
func (a *App) readWorkerReply(id string) ([]byte, error) {
	if !queueID.MatchString(id) {
		return nil, problem(400, "ID antrean tidak valid.")
	}
	f, err := os.Open(a.queuePath("responses", id))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	raw, large, err := limitedRead(f, 16*1024*1024)
	if large {
		return nil, problem(413, "Respons pekerja Muse terlalu besar.")
	}
	// A worker may still be writing its file. Wait for a complete JSON value.
	if err == nil && !json.Valid(raw) {
		return nil, nil
	}
	return raw, err
}
func (a *App) validateWorker(ctx context.Context, kind string, reply workerReply) (workerReply, error) {
	if reply.Error != "" {
		text := []rune(reply.Error)
		if len(text) > 30000 {
			text = text[:30000]
		}
		return workerReply{Error: string(text)}, nil
	}
	if jsLength(reply.Text) > 100000 {
		return workerReply{}, problem(400, "Teks pekerja Muse tidak valid (maksimal 100.000 karakter).")
	}
	if kind == "image" && reply.ImageURL == "" {
		return workerReply{}, problem(400, "Respons pembuatan gambar wajib memuat imageUrl gambar asli.")
	}
	if reply.ImageURL != "" {
		path, err := a.resolveImage(ctx, reply.ImageURL)
		if err != nil {
			return workerReply{}, err
		}
		reply.ImageURL = path
	}
	reply.Text = strings.TrimSpace(reply.Text)
	if reply.Text == "" && reply.ImageURL == "" {
		return workerReply{}, problem(400, "Respons Muse wajib memuat text atau imageUrl.")
	}
	return reply, nil
}
func (a *App) cancelQueueLocked() error {
	for _, folder := range []string{"requests", "responses"} {
		dir := filepath.Join(a.dir, "queue", folder)
		entries, err := os.ReadDir(dir)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			name := strings.TrimSuffix(strings.TrimSuffix(entry.Name(), ".tmp"), ".json")
			if !entry.IsDir() && queueID.MatchString(name) {
				if err := os.Remove(filepath.Join(dir, entry.Name())); err != nil && !errors.Is(err, os.ErrNotExist) {
					return err
				}
			}
		}
	}
	return nil
}
