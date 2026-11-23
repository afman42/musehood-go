package musehood

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var taskPrefix = regexp.MustCompile(`(?i)^tugas\s*:\s*`)
var imagePrefix = regexp.MustCompile(`(?i)^gambar\s*:\s*`)
var jobPrefix = regexp.MustCompile(`(?i)^(gambar|tugas)\s*:\s*`)

const finalLabel = "Hasil Akhir - Integrasi"

func (a *App) routeLocked(text string) (string, []step) {
	if taskPrefix.MatchString(text) {
		return "task", []step{{Seat: "M10", Kind: "chat"}, {Seat: "M01", Kind: "chat"}, {Seat: "M04", Kind: "chat"}, {Seat: "M03", Kind: "image"}, {Seat: "M02", Kind: "chat"}, {Seat: "MUSE", Kind: "summary"}}
	}
	if imagePrefix.MatchString(text) {
		return "image", []step{{Seat: "M03", Kind: "image"}}
	}
	mentioned := []step{}
	for _, s := range a.data.Seats {
		pattern := regexp.MustCompile(`(?i)@(?:` + regexp.QuoteMeta(s.Name) + `|` + s.Code + `)(?:$|[^\p{L}\p{N}_-])`)
		if pattern.MatchString(text) {
			mentioned = append(mentioned, step{Seat: s.Code, Kind: "chat"})
		}
	}
	if len(mentioned) > 0 {
		return "chat", mentioned
	}
	for _, s := range a.data.Seats {
		mentioned = append(mentioned, step{Seat: s.Code, Kind: "chat"})
		if !a.data.Settings.RoundRobin {
			break
		}
	}
	return "chat", mentioned
}
func (a *App) submitLocked(text, replyTo, imageURL string, att *attachment, progress bool) (*job, error) {
	text = strings.TrimSpace(text)
	if (text == "" && imageURL == "" && att == nil) || jsLength(text) > 20000 {
		return nil, problem(400, "Pesan wajib berisi teks, gambar, atau lampiran, maksimal 20.000 karakter.")
	}
	if err := a.validateAttachment(att); err != nil {
		return nil, err
	}
	if replyTo != "" && a.findMessageLocked(replyTo) == nil {
		return nil, problem(400, "Pesan yang dibalas tidak ditemukan atau sudah dihapus.")
	}
	if imageURL != "" {
		if !localImage.MatchString(imageURL) {
			return nil, problem(400, "URL gambar harus berupa file lokal hasil unggahan (/images/…).")
		}
		if _, err := os.Stat(filepath.Join(a.dir, strings.TrimPrefix(imageURL, "/"))); err != nil {
			return nil, problem(400, "File gambar tidak ditemukan di penyimpanan lokal.")
		}
	} else if replyTo == "" && progress {
		replyTo, imageURL = "", ""
	}
	text = a.redactLocked(text)
	kind, steps := "progress", []step{{Seat: "MUSE", Kind: "progress"}}
	if !progress {
		kind, steps = a.routeLocked(text)
	}
	for i := range steps {
		steps[i].Status = "pending"
	}
	j := &job{ID: uuid(), Prompt: text, Kind: kind, CreatedAt: now(), Status: "processing", Steps: steps}
	previous := a.data
	a.data.Jobs = append(a.data.Jobs, j)
	a.data.Messages = append(a.data.Messages, &message{ID: uuid(), CreatedAt: now(), AuthorType: "owner", JobID: j.ID, Text: text, Status: "completed", ReplyTo: replyTo, ImageURL: imageURL, Attachment: att})
	if kind == "task" {
		a.data.Task = &activeTask{JobID: j.ID, Title: taskPrefix.ReplaceAllString(text, ""), CurrentStep: ptr(steps[0].Seat), Status: "processing", CreatedAt: now()}
	}
	if err := a.saveLocked(); err != nil {
		a.data = previous
		return nil, err
	}
	a.advanceLocked(j)
	return j, nil
}
func (a *App) progressLocked() (*job, error) {
	task := a.data.Task
	if task == nil {
		return nil, problem(409, "Belum ada tugas aktif. Kirim pesan berawalan Tugas: terlebih dahulu.")
	}
	last := "belum ada"
	if task.LastCompleted != nil {
		last = *task.LastCompleted
	}
	return a.submitLocked(fmt.Sprintf("Tanya Progres: %s. Langkah terakhir selesai: %s. Status: %s.", task.Title, last, task.Status), "", "", nil, true)
}
func (a *App) saveJobLocked() {
	if err := a.saveLocked(); err != nil {
		log.Print("Gagal menyimpan pekerjaan Musehood. Periksa penyimpanan lokal.")
	}
}
func (a *App) tickLocked() {
	for _, j := range a.data.Jobs {
		a.advanceLocked(j)
	}
}
func (a *App) messageLocked(j *job, s *step, member seat) *message {
	for i := len(a.data.Messages) - 1; i >= 0; i-- {
		if a.data.Messages[i].ID == s.MessageID {
			return a.data.Messages[i]
		}
	}
	m := &message{ID: uuid(), CreatedAt: now(), AuthorType: "seat", Seat: member.Code, JobID: j.ID, Status: "typing", StatusText: member.Name + " sedang mengetik..."}
	if s.Kind == "summary" {
		m.Label = finalLabel
	}
	s.MessageID = m.ID
	s.Status = "running"
	a.data.Messages = append(a.data.Messages, m)
	a.saveJobLocked()
	return m
}
func (a *App) enqueueLocked(j *job, s *step, member seat, m *message) error {
	q := queueRequest{ID: uuid(), Seat: member.Code, Kind: s.Kind, Prompt: systemPrompt(member, a.data) + "\n\n", Context: contextFor(a.data, j), CreatedAt: now()}
	if s.Kind == "summary" {
		q.Prompt += "Buat Hasil Akhir - Integrasi berdasarkan semua hasil tahap sebelumnya.\n"
	}
	q.Prompt += j.Prompt
	if err := writeJSONFile(a.queuePath("requests", q.ID), a.safeLocked(q)); err != nil {
		return err
	}
	s.QueueID = q.ID
	s.QueuedAt = q.CreatedAt
	m.QueueID = q.ID
	m.Status = "typing"
	if s.Kind == "image" {
		m.StatusText = "Menunggu pekerja Muse membuat gambar…"
	}
	j.Status = "waiting"
	a.saveJobLocked()
	return nil
}
func (a *App) finishStepLocked(j *job, s *step, m *message) {
	s.Status = "completed"
	m.Status = "completed"
	if m.Error != "" && m.Text == "" && m.ImageURL == "" {
		m.Status = "error"
	}
	m.StatusText = ""
	j.Index++
	j.Status = "processing"
	if task := a.data.Task; task != nil && task.JobID == j.ID {
		task.LastCompleted = ptr(s.Seat)
		task.CurrentStep = nil
		if j.Index < len(j.Steps) {
			task.CurrentStep = ptr(j.Steps[j.Index].Seat)
		}
	}
	if j.Index == len(j.Steps) {
		a.completeJobLocked(j)
	} else {
		a.saveJobLocked()
	}
}
func (a *App) completeJobLocked(j *job) {
	j.Status = "completed"
	j.CompletedAt = now()
	if task := a.data.Task; task != nil && task.JobID == j.ID {
		task.Status = "completed"
		task.CurrentStep = nil
		task.CompletedAt = j.CompletedAt
	}
	a.saveJobLocked()
}
func appendError(existing, extra string) string {
	if existing == "" {
		return extra
	}
	return existing + "\n" + extra
}
func (a *App) checkWaitingLocked(s *step, m *message) {
	since, err := time.Parse(time.RFC3339Nano, s.QueuedAt)
	if err != nil || time.Since(since) < a.museWait || m.Status == "waiting" {
		return
	}
	m.Status = "waiting"
	m.StatusText = "Muse belum menjawab — antrean masih menunggu"
	if s.Kind == "image" {
		m.StatusText = "Muse belum menjawab — antrean gambar masih menunggu"
	}
	a.saveJobLocked()
}

// Advance synchronous steps while holding the mutex. The active-run map keeps a
// job from starting a second network operation while its current one is in flight.
func (a *App) advanceLocked(j *job) {
	if a.closed || a.running[j] != nil || j.Status == "completed" {
		return
	}
	// Node may have saved the final step before persisting the job's completion.
	if j.Index == len(j.Steps) {
		a.completeJobLocked(j)
		return
	}
	for j.Index < len(j.Steps) {
		s := &j.Steps[j.Index]
		member := *a.data.findSeat(s.Seat)
		m := a.messageLocked(j, s, member)
		if s.QueueID != "" {
			raw, err := a.readWorkerReply(s.QueueID)
			if err != nil {
				m.Error = appendError(m.Error, "Respons pekerja Muse: gagal membaca file respons.")
				a.finishStepLocked(j, s, m)
				continue
			}
			if raw == nil {
				a.checkWaitingLocked(s, m)
				return
			}
			kind := s.Kind
			a.runLocked(j, s, member, m, true, func(ctx context.Context) (workerReply, error) {
				var reply workerReply
				if err := json.Unmarshal(raw, &reply); err != nil {
					return workerReply{}, problem(400, "Respons pekerja Muse harus berupa objek JSON dengan nilai teks.")
				}
				return a.validateWorker(ctx, kind, reply)
			})
			return
		}
		if member.Code == "MUSE" {
			if err := a.enqueueLocked(j, s, member, m); err == nil {
				return
			}
			m.Error = "Gagal menulis antrean Muse. Periksa penyimpanan lokal."
			a.finishStepLocked(j, s, m)
			continue
		}
		cfg := a.data.Settings
		if s.Kind == "image" {
			if protocolOf(cfg) != protoOpenAI {
				// Protokol tanpa endpoint gambar: langsung pakai jalur gambar pekerja Muse.
				if err := a.enqueueLocked(j, s, member, m); err == nil {
					return
				}
				m.Error = "Gagal menulis antrean Muse. Periksa penyimpanan lokal."
				a.finishStepLocked(j, s, m)
				continue
			}
			subject := jobPrefix.ReplaceAllString(j.Prompt, "")
			raw, _ := json.Marshal(contextFor(a.data, j))
			prompt := a.redactLocked(systemPrompt(member, a.data) + "\nPermintaan gambar: " + subject + "\nKonteks tugas: " + string(raw))
			a.runLocked(j, s, member, m, false, func(ctx context.Context) (workerReply, error) {
				image, err := a.generateImage(ctx, cfg, member, prompt)
				return workerReply{ImageURL: image, Text: subject}, err
			})
		} else {
			history := gatewayMessages(member, a.data, j)
			for i := range history {
				history[i].Content = a.redactLocked(history[i].Content)
			}
			a.runLocked(j, s, member, m, false, func(ctx context.Context) (workerReply, error) {
				text, err := a.chat(ctx, cfg, member, history)
				return workerReply{Text: text}, err
			})
		}
		return
	}
}
func (a *App) runLocked(j *job, s *step, member seat, m *message, worker bool, run func(context.Context) (workerReply, error)) {
	ctx, cancel := context.WithCancel(a.ctx)
	a.running[j] = cancel
	a.wg.Go(func() {
		defer cancel()
		reply, err := run(ctx)
		a.mu.Lock()
		defer a.mu.Unlock()
		if ctx.Err() != nil || a.closed {
			return
		}
		delete(a.running, j)
		if err == nil {
			if reply.Error != "" {
				m.Error = appendError(m.Error, a.redactLocked(reply.Error))
			} else {
				m.ImageURL = reply.ImageURL
				m.Text = a.redactLocked(reply.Text)
				if !worker && s.Kind == "image" {
					m.Text = a.redactLocked("Gambar untuk " + a.data.Owner.Name + ": " + reply.Text)
				}
			}
		} else if worker {
			m.Error = appendError(m.Error, "Respons pekerja Muse: "+a.redactLocked(publicError(err)))
		} else {
			m.Error = a.redactLocked(publicError(err))
			if s.Kind == "image" {
				if err := a.enqueueLocked(j, s, member, m); err == nil {
					return
				}
				m.Error = appendError(m.Error, "Gagal menulis antrean Muse. Periksa penyimpanan lokal.")
			} else if a.data.Settings.DemoFallback {
				m.Demo = true
				m.Text = a.redactLocked(fmt.Sprintf("DEMO — %s (%s): simulasi lokal untuk permintaan “%s”. Belum ada hasil model nyata; periksa koneksi gateway.", member.Name, member.Role, j.Prompt))
			}
		}
		a.finishStepLocked(j, s, m)
		a.advanceLocked(j)
	})
}
func (a *App) clearLocked() error {
	previous := a.data
	a.data.Messages = []*message{}
	a.data.Jobs = []*job{}
	a.data.Task = nil
	if err := a.saveLocked(); err != nil {
		a.data = previous
		return err
	}
	for j, cancel := range a.running {
		cancel()
		delete(a.running, j)
	}
	return a.cancelQueueLocked()
}
