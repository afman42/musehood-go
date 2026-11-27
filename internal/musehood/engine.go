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

type seatMentionPattern struct {
	name string
	code string
	re   *regexp.Regexp
}

var taskPrefix = regexp.MustCompile(`(?i)^tugas\s*:\s*`)
var imagePrefix = regexp.MustCompile(`(?i)^gambar\s*:\s*`)
var jobPrefix = regexp.MustCompile(`(?i)^(gambar|tugas)\s*:\s*`)

const finalLabel = "Hasil Akhir - Integrasi"
const voteLabel = "Voting Jawaban Terbaik"

// integratorSeat adalah kursi yang menutup pipeline "Tugas:" dan menjawab
// "Tanya Progres". Bawaan "MUSE"; dapat diganti lewat Pengaturan.
func (a *App) integratorSeat() string {
	if code := a.data.Settings.IntegratorSeat; code != "" && a.data.findSeat(code) != nil {
		return code
	}
	return "MUSE"
}

func (a *App) routeLocked(text string, r *room) (string, []step) {
	if taskPrefix.MatchString(text) {
		pipeline := []step{{Seat: "M10", Kind: "chat"}, {Seat: "M01", Kind: "chat"}, {Seat: "M04", Kind: "chat"}, {Seat: "M03", Kind: "image"}, {Seat: "M02", Kind: "chat"}, {Seat: a.integratorSeat(), Kind: "summary"}}
		steps := []step{}
		for _, s := range pipeline {
			// Kursi integrator selalu menutup pipeline; tahap lain hanya
			// berjalan bila kursinya peserta ruang ini.
			if s.Kind == "summary" || a.data.roomHasSeat(r, s.Seat) {
				steps = append(steps, s)
			}
		}
		return "task", steps
	}
	if imagePrefix.MatchString(text) {
		if a.data.roomHasSeat(r, "M03") {
			return "image", []step{{Seat: "M03", Kind: "image"}}
		}
		return "image", []step{{Seat: a.integratorSeat(), Kind: "chat"}}
	}
	mentioned := []step{}
	stalePatterns := len(a.mentionPatterns) != len(a.data.Seats)
	if !stalePatterns {
		for i, s := range a.data.Seats {
			cached := a.mentionPatterns[i]
			if cached.name != s.Name || cached.code != s.Code {
				stalePatterns = true
				break
			}
		}
	}
	if stalePatterns {
		a.mentionPatterns = make([]seatMentionPattern, len(a.data.Seats))
		for i, s := range a.data.Seats {
			a.mentionPatterns[i] = seatMentionPattern{
				name: s.Name,
				code: s.Code,
				re:   regexp.MustCompile(`(?i)@(?:` + regexp.QuoteMeta(s.Name) + `|` + regexp.QuoteMeta(s.Code) + `)(?:$|[^\p{L}\p{N}_-])`),
			}
		}
	}
	for _, cached := range a.mentionPatterns {
		if cached.re.MatchString(text) && a.data.roomHasSeat(r, cached.code) {
			mentioned = append(mentioned, step{Seat: cached.code, Kind: "chat"})
		}
	}
	if len(mentioned) > 0 {
		return "chat", mentioned
	}
	for _, code := range a.data.roomSeatCodes(r) {
		mentioned = append(mentioned, step{Seat: code, Kind: "chat"})
		if !a.data.Settings.RoundRobin {
			break
		}
	}
	return "chat", mentioned
}
func (a *App) submitLocked(text, replyTo, imageURL string, att *attachment, roomID, author string, progress bool) (*job, error) {
	text = strings.TrimSpace(text)
	if (text == "" && imageURL == "" && att == nil) || jsLength(text) > 20000 {
		return nil, problem(400, "Pesan wajib berisi teks, gambar, atau lampiran, maksimal 20.000 karakter.")
	}
	if err := a.validateAttachment(att); err != nil {
		return nil, err
	}
	r := a.data.activeRoom()
	if roomID != "" {
		r = a.data.findRoom(roomID)
		if r == nil {
			return nil, problem(400, "Ruang tidak ditemukan.")
		}
	}
	if r == nil {
		return nil, problem(500, "Ruang bawaan tidak tersedia.")
	}
	// Nama pengirim (fitur 21): kosong = pemilik utama, selain itu harus
	// peserta terdaftar di ruang ini.
	authorName, err := a.resolveAuthorLocked(r, author)
	if err != nil {
		return nil, err
	}
	if replyTo != "" {
		target := a.findMessageLocked(replyTo)
		if target == nil || target.RoomID != r.ID {
			return nil, problem(400, "Pesan yang dibalas tidak ditemukan atau sudah dihapus.")
		}
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
	kind, steps := "progress", []step{{Seat: a.integratorSeat(), Kind: "progress"}}
	if !progress {
		kind, steps = a.routeLocked(text, r)
	}
	for i := range steps {
		steps[i].Status = "pending"
	}
	j := &job{ID: uuid(), Prompt: text, Kind: kind, CreatedAt: now(), Status: "processing", Steps: steps, RoomID: r.ID, BranchID: r.activeBranch()}
	previous := a.data
	a.data.Jobs = append(a.data.Jobs, j)
	a.data.Messages = append(a.data.Messages, &message{ID: uuid(), CreatedAt: now(), AuthorType: "owner", Author: authorName, JobID: j.ID, Text: text, Status: "completed", ReplyTo: replyTo, ImageURL: imageURL, Attachment: att, RoomID: r.ID, BranchID: r.activeBranch()})
	if kind == "task" {
		a.data.Task = &activeTask{JobID: j.ID, Title: taskPrefix.ReplaceAllString(text, ""), CurrentStep: ptr(steps[0].Seat), Status: "processing", CreatedAt: now(), RoomID: r.ID}
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
	if task == nil || (a.data.activeRoom() != nil && task.RoomID != a.data.activeRoom().ID) {
		return nil, problem(409, "Belum ada tugas aktif. Kirim pesan berawalan Tugas: terlebih dahulu.")
	}
	last := "belum ada"
	if task.LastCompleted != nil {
		last = *task.LastCompleted
	}
	return a.submitLocked(fmt.Sprintf("Tanya Progres: %s. Langkah terakhir selesai: %s. Status: %s.", task.Title, last, task.Status), "", "", nil, task.RoomID, "", true)
}
func (a *App) saveJobLocked() {
	if err := a.saveLocked(); err != nil {
		log.Print("Gagal menyimpan pekerjaan Musehood. Periksa penyimpanan lokal.")
	}
}
// submitDebateLocked membuat pekerjaan debat dua kursi (fitur 15):
// A pro → B kontra → A sanggahan → B sanggahan → integrator kesimpulan.
func (a *App) submitDebateLocked(seatA, seatB, topic, roomID string) (*job, error) {
	topic = strings.TrimSpace(topic)
	if topic == "" || jsLength(topic) > 2000 {
		return nil, problem(400, "Topik debat wajib diisi, maksimal 2.000 karakter.")
	}
	if seatA == seatB {
		return nil, problem(400, "Dua kursi debat harus berbeda.")
	}
	aSeat := a.data.findSeat(seatA)
	bSeat := a.data.findSeat(seatB)
	if aSeat == nil || bSeat == nil {
		return nil, problem(404, "Kursi debat tidak ditemukan.")
	}
	if aSeat.Provider != "gateway" || bSeat.Provider != "gateway" {
		return nil, problem(400, "Kursi debat harus kursi gateway.")
	}
	r := a.data.activeRoom()
	if roomID != "" {
		r = a.data.findRoom(roomID)
		if r == nil {
			return nil, problem(400, "Ruang tidak ditemukan.")
		}
	}
	if r == nil {
		return nil, problem(500, "Ruang bawaan tidak tersedia.")
	}
	topic = a.redactLocked(topic)
	steps := []step{
		{Seat: seatA, Kind: "debate-pro", Label: "Debat — Pro"},
		{Seat: seatB, Kind: "debate-contra", Label: "Debat — Kontra"},
		{Seat: seatA, Kind: "debate-rebuttal", Label: "Debat — Sanggahan"},
		{Seat: seatB, Kind: "debate-rebuttal", Label: "Debat — Sanggahan"},
		{Seat: a.integratorSeat(), Kind: "debate-summary", Label: "Debat — Kesimpulan"},
	}
	for i := range steps {
		steps[i].Status = "pending"
	}
	j := &job{ID: uuid(), Prompt: "Debat: " + topic, Kind: "debate", CreatedAt: now(), Status: "processing", Steps: steps, RoomID: r.ID, BranchID: r.activeBranch()}
	ownerText := fmt.Sprintf("Debat: %s (@%s vs @%s)", topic, aSeat.Name, bSeat.Name)
	previous := a.data
	a.data.Jobs = append(a.data.Jobs, j)
	a.data.Messages = append(a.data.Messages, &message{ID: uuid(), CreatedAt: now(), AuthorType: "owner", JobID: j.ID, Text: ownerText, Status: "completed", RoomID: r.ID, BranchID: r.activeBranch()})
	if err := a.saveLocked(); err != nil {
		a.data = previous
		return nil, err
	}
	a.advanceLocked(j)
	return j, nil
}

func (a *App) tickLocked() {
	a.runSchedulesLocked()
	a.autoCompactLocked()
	a.checkDailySummaryLocked()
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
	m := &message{ID: uuid(), CreatedAt: now(), AuthorType: "seat", Seat: member.Code, JobID: j.ID, Status: "typing", StatusText: member.Name + " sedang mengetik...", RoomID: j.RoomID, BranchID: j.BranchID}
	if s.Kind == "summary" {
		m.Label = finalLabel
	}
	if s.Kind == "roomsum" {
		m.Label = summaryLabel
	}
	if s.Kind == "vote" {
		m.Label = voteLabel
	}
	if s.Label != "" {
		m.Label = s.Label
	}
	s.MessageID = m.ID
	s.Status = "running"
	a.data.Messages = append(a.data.Messages, m)
	a.saveJobLocked()
	return m
}
// chainPrevLocked mengembalikan pesan kursi sebelumnya pada diskusi
// bergilir bila balasan berantai aktif (fitur 14).
func (a *App) chainPrevLocked(j *job) (*message, string, bool) {
	if !a.data.Settings.chainRepliesOn() || j.Kind != "chat" || j.Index == 0 {
		return nil, "", false
	}
	prev := &j.Steps[j.Index-1]
	prevMsg := a.findMessageLocked(prev.MessageID)
	if prevMsg == nil || prevMsg.AuthorType != "seat" {
		return nil, "", false
	}
	name := prev.Seat
	if s := a.data.findSeat(prev.Seat); s != nil {
		name = s.Name
	}
	return prevMsg, name, true
}

func chainInstruction(prevName string) string {
	return "Instruksi tambahan: tanggapi secara eksplisit balasan " + prevName + " sebelumnya (sebut namanya), lalu lanjutkan kontribusimu."
}

// debateInstruction adalah instruksi peran untuk tahap debat (fitur 15).
func debateInstruction(kind, topic string) string {
	switch kind {
	case "debate-pro":
		return "Kamu pihak PRO dalam debat ini. Argumenkan posisi mendukung topik berikut dengan argumen terkuatmu: " + topic
	case "debate-contra":
		return "Kamu pihak KONTRA dalam debat ini. Bantah argumen pihak pro sebelumnya dan argumenkan posisi menolak topik berikut: " + topic
	case "debate-rebuttal":
		return "Sanggah argumen lawan sebelumnya dan perkuat posisimu dalam debat tentang: " + topic
	case "debate-summary":
		return "Buat kesimpulan debat tentang \"" + topic + "\": rangkum argumen kedua pihak, nilai argumen yang terkuat, dan beri penilaian akhir yang seimbang."
	}
	return ""
}

func debateTopic(j *job) string {
	return strings.TrimPrefix(j.Prompt, "Debat: ")
}

func (a *App) enqueueLocked(j *job, s *step, member seat, m *message) error {
	q := queueRequest{ID: uuid(), Seat: member.Code, Kind: s.Kind, Prompt: a.systemPrompt(member, a.data) + "\n\n", Context: contextFor(a.data, j), CreatedAt: now()}
	if s.Kind == "summary" {
		q.Prompt += "Buat Hasil Akhir - Integrasi berdasarkan semua hasil tahap sebelumnya.\n"
	}
	if _, prevName, ok := a.chainPrevLocked(j); ok {
		q.Prompt += chainInstruction(prevName) + "\n"
	}
	if strings.HasPrefix(s.Kind, "debate-") {
		q.Prompt += debateInstruction(s.Kind, debateTopic(j)) + "\n"
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
	if s.Kind == "roomsum" && m.Text != "" {
		// Simpan ringkasan untuk kompaksi konteks dan sematkan pesannya.
		m.Pinned = true
		if r := a.data.findRoom(j.RoomID); r != nil {
			r.Summary = m.Text
			count := 0
			for _, msg := range a.data.Messages {
				if msg.RoomID == r.ID && msg.Status != "typing" && msg.Status != "waiting" {
					count++
				}
			}
			r.SummaryCount = count
		}
		// Memori otomatis (fitur 3): kursi peserta yang mengaktifkan
		// AutoMemory menulis poin memori dari ringkasan ini.
		a.autoMemoryAfterSummaryLocked(j.RoomID, m.Text)
	}
	// Webhook + Telegram (fitur 41 & 43).
	a.notifyCompletedLocked(m)
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
	// Bersihkan catatan budget "sekali per pekerjaan" (fitur 5).
	for key := range a.budgetNoted {
		if strings.HasPrefix(key, j.ID+"|") {
			delete(a.budgetNoted, key)
		}
	}
	if task := a.data.Task; task != nil && task.JobID == j.ID {
		task.Status = "completed"
		task.CurrentStep = nil
		task.CompletedAt = j.CompletedAt
	}
	if j.Kind == "vote" {
		a.tallyVotesLocked(j)
	}
	a.saveJobLocked()
}

// voteChoice memilih kandidat yang disebut paling awal dalam teks suara
// (kode kursi atau nama tampil), selain jawaban pemilih sendiri.
func (a *App) voteChoiceLocked(text string, j *job, voter string) string {
	lower := strings.ToLower(text)
	bestID := ""
	bestAt := -1
	for _, id := range j.Candidates {
		cand := a.findMessageLocked(id)
		if cand == nil || cand.Seat == voter {
			continue
		}
		at := strings.Index(lower, strings.ToLower(cand.Seat))
		if s := a.data.findSeat(cand.Seat); s != nil {
			if nameAt := strings.Index(lower, strings.ToLower(s.Name)); nameAt >= 0 && (at < 0 || nameAt < at) {
				at = nameAt
			}
		}
		if at >= 0 && (bestAt < 0 || at < bestAt) {
			bestAt, bestID = at, id
		}
	}
	return bestID
}

func (a *App) tallyVotesLocked(j *job) {
	counts := map[string]int{}
	for _, s := range j.Steps {
		voteMsg := a.findMessageLocked(s.MessageID)
		if voteMsg == nil || voteMsg.Status != "completed" {
			continue
		}
		if choice := a.voteChoiceLocked(voteMsg.Text, j, s.Seat); choice != "" {
			counts[choice]++
		}
	}
	best := 0
	for _, id := range j.Candidates {
		if counts[id] > best {
			best = counts[id]
		}
	}
	lines := []string{}
	winners := []string{}
	for _, id := range j.Candidates {
		cand := a.findMessageLocked(id)
		if cand == nil {
			continue
		}
		cand.Votes = counts[id]
		cand.BestAnswer = best > 0 && counts[id] == best
		name := cand.Seat
		if s := a.data.findSeat(cand.Seat); s != nil {
			name = s.Name
		}
		lines = append(lines, fmt.Sprintf("%s (%s): %d suara", name, cand.Seat, counts[id]))
		if cand.BestAnswer {
			winners = append(winners, name)
		}
	}
	summary := "Hasil voting jawaban terbaik — " + strings.Join(lines, " · ") + "."
	if len(winners) > 0 {
		summary += " Jawaban terbaik: " + strings.Join(winners, ", ") + "."
	} else {
		summary += " Tidak ada suara yang terbaca."
	}
	a.data.Messages = append(a.data.Messages, &message{ID: uuid(), CreatedAt: now(), AuthorType: "seat", Seat: a.integratorSeat(), JobID: j.ID, Text: summary, Status: "completed", Label: voteLabel, RoomID: j.RoomID})
}

// voteMessagesLocked menyusun prompt penilaian untuk satu kursi pemilih.
func (a *App) voteMessagesLocked(voter seat, j *job) []gatewayMessage {
	var list strings.Builder
	for _, id := range j.Candidates {
		cand := a.findMessageLocked(id)
		if cand == nil {
			continue
		}
		name := cand.Seat
		if s := a.data.findSeat(cand.Seat); s != nil {
			name = s.Name
		}
		fmt.Fprintf(&list, "[%s] %s: %s\n\n", cand.Seat, name, cand.Text)
	}
	instruction := "Kamu menilai jawaban-jawaban kursi lain untuk permintaan pemilik. Pilih SATU jawaban terbaik (jangan pilih jawabanmu sendiri). Balas HANYA dengan kode kursi jawaban terbaik (mis. M01) diikuti satu kalimat alasan singkat."
	return []gatewayMessage{
		{"system", a.systemPrompt(voter, a.data) + " " + instruction},
		{"user", "Permintaan pemilik: " + j.Prompt + "\n\nJawaban kandidat:\n" + list.String() + "Kursimu adalah " + voter.Code + " (" + voter.Name + "). Jangan memilih jawabanmu sendiri."},
	}
}

// submitVoteLocked memulai voting jawaban terbaik untuk diskusi terakhir
// yang selesai di ruang ini. Pemilih adalah kursi gateway yang ikut menjawab.
func (a *App) submitVoteLocked(roomID string) (*job, error) {
	r := a.data.activeRoom()
	if roomID != "" {
		r = a.data.findRoom(roomID)
		if r == nil {
			return nil, problem(400, "Ruang tidak ditemukan.")
		}
	}
	if r == nil {
		return nil, problem(500, "Ruang bawaan tidak tersedia.")
	}
	var target *job
	var candidates []*message
	for i := len(a.data.Jobs) - 1; i >= 0; i-- {
		j := a.data.Jobs[i]
		if j.RoomID != r.ID || j.Status != "completed" || j.Kind == "vote" {
			continue
		}
		found := []*message{}
		for _, m := range a.data.Messages {
			if m.JobID == j.ID && m.AuthorType == "seat" && m.Status == "completed" && strings.TrimSpace(m.Text) != "" && m.Label != finalLabel {
				found = append(found, m)
			}
		}
		if len(found) >= 2 {
			target, candidates = j, found
			break
		}
	}
	if target == nil {
		return nil, problem(409, "Belum ada diskusi dengan minimal dua jawaban kursi untuk divoting di ruang ini.")
	}
	voters := []string{}
	seen := map[string]bool{}
	for _, m := range candidates {
		if seen[m.Seat] {
			continue
		}
		seen[m.Seat] = true
		if s := a.data.findSeat(m.Seat); s != nil && s.Provider == "gateway" {
			voters = append(voters, m.Seat)
		}
	}
	if len(voters) == 0 {
		return nil, problem(409, "Tidak ada kursi gateway yang bisa ikut voting.")
	}
	previous := a.data
	ids := []string{}
	for _, m := range candidates {
		m.Votes = 0
		m.BestAnswer = false
		ids = append(ids, m.ID)
	}
	steps := []step{}
	for _, code := range voters {
		steps = append(steps, step{Seat: code, Kind: "vote", Status: "pending"})
	}
	j := &job{ID: uuid(), Prompt: target.Prompt, Kind: "vote", CreatedAt: now(), Status: "processing", Steps: steps, RoomID: r.ID, BranchID: r.activeBranch(), Candidates: ids}
	a.data.Jobs = append(a.data.Jobs, j)
	if err := a.saveLocked(); err != nil {
		a.data = previous
		return nil, err
	}
	a.advanceLocked(j)
	return j, nil
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
	// Mode paralel (fitur 15): putaran chat multi-kursi gateway berjalan
	// konkuren; selain itu jalur sekuensial di bawah.
	if a.parallelEligibleLocked(j) {
		a.advanceParallelLocked(j)
		return
	}
	for j.Index < len(j.Steps) {
		s := &j.Steps[j.Index]
		member := *a.data.findSeat(s.Seat)
		a.applyRoomDisabledToolsLocked(j, &member)
		m := a.messageLocked(j, s, member)
		// Balasan berantai (fitur 14): tautkan pesan ini ke balasan kursi
		// sebelumnya pada diskusi bergilir.
		if prevMsg, _, ok := a.chainPrevLocked(j); ok && m.ReplyTo == "" {
			m.ReplyTo = prevMsg.ID
		}
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
		if s.Kind == "vote" && member.Provider == "muse" {
			// Pemilih voting hanya kursi gateway. Kursi yang berubah jalur
			// ke antrean setelah voting dibuat dilewati, agar pekerja antrean
			// tidak menjawab dengan prompt chat biasa.
			m.Error = "Kursi antrean Muse tidak ikut voting."
			a.finishStepLocked(j, s, m)
			continue
		}
		if member.Provider == "muse" {
			if err := a.enqueueLocked(j, s, member, m); err == nil {
				return
			}
			m.Error = "Gagal menulis antrean Muse. Periksa penyimpanan lokal."
			a.finishStepLocked(j, s, m)
			continue
		}
		// Budget harian (fitur 5): kursi yang sudah mencapai budget hari
		// ini dilewati; catatan singkat hanya sekali per pekerjaan.
		if a.budgetExceededLocked(member) {
			key := j.ID + "|" + member.Code
			if !a.budgetNoted[key] {
				a.budgetNoted[key] = true
				m.Error = fmt.Sprintf("Kursi %s dilewati: budget harian $%.2f sudah tercapai hari ini.", member.Name, member.DailyBudgetUSD)
			}
			a.finishStepLocked(j, s, m)
			continue
		}
		cfg := a.data.Settings
		// Model per jenis tugas (fitur 6): ringkasan memakai model
		// ringkasan, putaran dengan tool memakai model tools, sisanya
		// model chat; gambar tetap memakai Model kursi.
		task := "chat"
		switch {
		case s.Kind == "roomsum" || s.Kind == "summary":
			task = "summary"
		case s.Kind != "image" && s.Kind != "vote" && a.data.Settings.toolsOn():
			task = "tools"
		}
		taskModel := ""
		if s.Kind == "image" {
			if member.Model != nil {
				taskModel = *member.Model
			}
		} else {
			taskModel = member.modelForTask(task)
		}
		if taskModel != "" {
			var resolved string
			cfg, resolved = a.data.resolveModelProviderLocked(cfg, taskModel)
			member.Model = &resolved
		}
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
			prompt := a.redactLocked(a.systemPrompt(member, a.data) + "\nPermintaan gambar: " + subject + "\nKonteks tugas: " + string(raw))
			a.runLocked(j, s, member, m, false, func(ctx context.Context) (workerReply, error) {
				image, err := a.generateImage(ctx, cfg, member, prompt)
				return workerReply{ImageURL: image, Text: subject}, err
			})
		} else if s.Kind == "vote" {
			history := a.voteMessagesLocked(member, j)
			for i := range history {
				history[i].Content = a.redactLocked(history[i].Content)
			}
			a.runLocked(j, s, member, m, false, func(ctx context.Context) (workerReply, error) {
				text, err := a.chat(ctx, cfg, member, history)
				return workerReply{Text: text}, err
			})
		} else {
			history := a.gatewayMessages(member, a.data, j)
			if _, prevName, ok := a.chainPrevLocked(j); ok {
				history = append(history, gatewayMessage{Role: "user", Content: chainInstruction(prevName)})
			}
			if strings.HasPrefix(s.Kind, "debate-") {
				history = append(history, gatewayMessage{Role: "user", Content: debateInstruction(s.Kind, debateTopic(j))})
			}
			for i := range history {
				history[i].Content = a.redactLocked(history[i].Content)
			}
			// Rantai fallback per kursi: konfigurasi tiap model cadangan
			// diselesaikan sekarang (resolusi alias butuh daftar provider)
			// sebelum goroutine berjalan tanpa mutex.
			var fallbacks []fallbackAttempt
			for _, fb := range member.fallbackChain() {
				fbCfg, resolved := a.data.resolveModelProviderLocked(a.data.Settings, fb)
				fbMember := member
				fbMember.Model = &resolved
				fallbacks = append(fallbacks, fallbackAttempt{fbCfg, fbMember})
			}
			// Load balancing (fitur 12): bila aktif, setelah model utama
			// dan rantai fallback gagal total, coba provider lain dengan
			// model bernama sama (urutan daftar provider, melewati
			// provider yang sudah dicoba).
			var lbAttempts []fallbackAttempt
			if a.data.Settings.loadBalanceOn() && member.Model != nil {
				tried := map[string]bool{cfg.BaseURL: true}
				for _, fb := range fallbacks {
					tried[fb.cfg.BaseURL] = true
				}
				for _, p := range a.data.Providers {
					if tried[p.BaseURL] {
						continue
					}
					tried[p.BaseURL] = true
					lbMember := member
					model := *member.Model
					lbMember.Model = &model
					lbAttempts = append(lbAttempts, fallbackAttempt{a.data.providerConfig(p), lbMember})
				}
			}
			tools := a.data.Settings.toolsOn()
			code := a.data.Settings.codeToolOn()
			stream := a.data.Settings.streamingOn()
			a.runLocked(j, s, member, m, false, func(ctx context.Context) (workerReply, error) {
				onActivity := func(toolName string) {
					a.mu.Lock()
					m.StatusText = member.Name + " menjalankan " + toolName + "…"
					a.mu.Unlock()
				}
				onDelta := func(partial string) {
					if idx := strings.Index(partial, "<<TOOL:"); idx >= 0 {
						partial = partial[:idx]
					}
					a.mu.Lock()
					m.Text = a.redactLocked(partial)
					a.mu.Unlock()
				}
				reply, err := a.chatWithTools(ctx, cfg, member, history, tools, code, stream, onActivity, onDelta)
				for _, fb := range fallbacks {
					if err == nil {
						break
					}
					if fbReply, fbErr := a.chatWithTools(ctx, fb.cfg, fb.member, history, tools, code, stream, onActivity, onDelta); fbErr == nil {
						return fbReply, nil
					}
				}
				for _, lb := range lbAttempts {
					if err == nil {
						break
					}
					if lbReply, lbErr := a.chatWithTools(ctx, lb.cfg, lb.member, history, tools, code, stream, onActivity, onDelta); lbErr == nil {
						return lbReply, nil
					}
				}
				return reply, err
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
		// Koreksi otomatis (fitur 25): kursi peninjau memeriksa balasan
		// gateway sebelum disimpan final. Dilakukan di luar mutex
		// karena melibatkan panggilan gateway.
		corrected := ""
		if err == nil && !worker && reply.Error == "" && strings.TrimSpace(reply.Text) != "" && s.Kind != "image" && s.Kind != "vote" {
			if text, ok := a.autoReviewReply(ctx, member, reply.Text); ok {
				corrected = text
			}
		}
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
				// Hasil koreksi otomatis (fitur 25) menggantikan teks
				// balasan; teks asli disimpan bila berubah.
				if corrected != "" && corrected != reply.Text {
					m.OriginalText = a.redactLocked(reply.Text)
					m.Text = a.redactLocked(corrected)
				}
				// Catat model penghasil balasan untuk statistik rating
				// per model (fitur 30); jalur pekerja Muse tidak punya model.
				if !worker && member.Model != nil {
					m.Model = *member.Model
				}
				m.ToolsUsed = reply.ToolsUsed
				m.ToolCalls = reply.ToolCalls
				for i := range m.ToolCalls {
					m.ToolCalls[i].Arg = a.redactLocked(m.ToolCalls[i].Arg)
					m.ToolCalls[i].Result = a.redactLocked(m.ToolCalls[i].Result)
				}
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

// clearLocked membersihkan pesan dan pekerjaan satu ruang saja (bawaan:
// ruang aktif). Ruang lain dan pengaturan tetap utuh.
func (a *App) clearLocked(roomID string) error {
	r := a.data.activeRoom()
	if roomID != "" {
		r = a.data.findRoom(roomID)
		if r == nil {
			return problem(400, "Ruang tidak ditemukan.")
		}
	}
	if r == nil {
		return problem(500, "Ruang bawaan tidak tersedia.")
	}
	previous := a.data
	keptMessages := make([]*message, 0, len(a.data.Messages))
	for _, m := range a.data.Messages {
		if m.RoomID != r.ID {
			keptMessages = append(keptMessages, m)
		}
	}
	keptJobs := make([]*job, 0, len(a.data.Jobs))
	removed := []*job{}
	for _, j := range a.data.Jobs {
		if j.RoomID != r.ID {
			keptJobs = append(keptJobs, j)
		} else {
			removed = append(removed, j)
		}
	}
	a.data.Messages = keptMessages
	a.data.Jobs = keptJobs
	if a.data.Task != nil && a.data.Task.RoomID == r.ID {
		a.data.Task = nil
	}
	if err := a.saveLocked(); err != nil {
		a.data = previous
		return err
	}
	queueIDs := map[string]bool{}
	for _, j := range removed {
		if cancel, ok := a.running[j]; ok {
			cancel()
			delete(a.running, j)
		}
		for _, s := range j.Steps {
			if s.QueueID != "" {
				queueIDs[s.QueueID] = true
			}
		}
	}
	return a.cancelQueueIDsLocked(queueIDs)
}
