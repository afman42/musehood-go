package musehood

import (
	"strings"
)

// Uji A/B model (fitur 23): pertanyaan yang sama dijalankan ke dua model
// secara paralel (satu panggilan gateway non-streaming per model, tanpa
// tool). Hasil disimpan sebagai dua pesan asisten berpasangan
// (ABGroup + ABLabel "A"/"B" + Model); pemilik memilih pemenang lewat
// pickABWinnerLocked. Kegagalan satu sisi tidak menggagalkan sisi lain.

const abSystemPrompt = "Kamu asisten yang membantu. Jawab dalam Bahasa Indonesia yang jelas dan ringkas."

// startABTestLocked membuat pesan pertanyaan + dua pesan jawaban uji A/B
// dan menjalankan kedua model di goroutine. Pemanggil wajib memegang a.mu.
func (a *App) startABTestLocked(text, modelA, modelB, roomID string) (string, []string, error) {
	text = strings.TrimSpace(text)
	if text == "" || jsLength(text) > 20000 {
		return "", nil, problem(400, "Pertanyaan uji A/B wajib diisi, maksimal 20.000 karakter.")
	}
	modelA = strings.TrimSpace(modelA)
	modelB = strings.TrimSpace(modelB)
	if modelA == "" || modelB == "" {
		return "", nil, problem(400, "Dua model uji A/B wajib diisi.")
	}
	for _, model := range []string{modelA, modelB} {
		if jsLength(model) > 200 || badSetting.MatchString(model) {
			return "", nil, problem(400, "Nama model uji A/B tidak valid.")
		}
	}
	r := a.data.activeRoom()
	if roomID != "" {
		r = a.data.findRoom(roomID)
		if r == nil {
			return "", nil, problem(400, "Ruang tidak ditemukan.")
		}
	}
	if r == nil {
		return "", nil, problem(500, "Ruang bawaan tidak tersedia.")
	}
	text = a.redactLocked(text)
	group := uuid()
	previous := a.data
	a.data.Messages = append(a.data.Messages, &message{ID: uuid(), CreatedAt: now(), AuthorType: "owner", Text: text, Status: "completed", RoomID: r.ID, BranchID: r.activeBranch()})
	ids := make([]string, 0, 2)
	answers := make([]*message, 0, 2)
	for _, item := range []struct{ label, model string }{{"A", modelA}, {"B", modelB}} {
		m := &message{ID: uuid(), CreatedAt: now(), AuthorType: "seat", Text: "", Status: "typing", StatusText: "Model " + item.label + " sedang menjawab…", Model: item.model, ABGroup: group, ABLabel: item.label, RoomID: r.ID, BranchID: r.activeBranch()}
		a.data.Messages = append(a.data.Messages, m)
		answers = append(answers, m)
		ids = append(ids, m.ID)
	}
	if err := a.saveLocked(); err != nil {
		a.data = previous
		return "", nil, err
	}
	for _, m := range answers {
		m := m
		a.wg.Go(func() { a.runABAnswer(m, text) })
	}
	return group, ids, nil
}

// runABAnswer menjalankan satu sisi uji A/B: resolusi provider/model
// (termasuk awalan alias), satu panggilan gateway non-streaming tanpa
// tool, lalu finalisasi pesan di bawah mutex.
func (a *App) runABAnswer(m *message, text string) {
	a.mu.Lock()
	cfg, resolved := a.data.resolveModelProviderLocked(a.data.Settings, m.Model)
	a.mu.Unlock()
	probe := seat{identity: identity{Name: "Uji A/B"}, Code: "AB", Provider: "gateway", Model: &resolved}
	history := []gatewayMessage{
		{"system", abSystemPrompt},
		{"user", text},
	}
	reply, err := a.chat(a.ctx, cfg, probe, history)
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed {
		return
	}
	if err != nil {
		m.Error = a.redactLocked(publicError(err))
		m.Status = "error"
	} else {
		m.Text = a.redactLocked(strings.TrimSpace(reply))
		m.Status = "completed"
	}
	m.StatusText = ""
	a.saveJobLocked()
}

// pickABWinnerLocked menandai satu pesan uji A/B sebagai pemenang dan
// lawannya bukan pemenang. Pemanggil wajib memegang a.mu.
func (a *App) pickABWinnerLocked(group, messageID string) (any, error) {
	found := false
	var winner *message
	for _, m := range a.data.Messages {
		if m.ABGroup == group && m.ID == messageID {
			found = true
			winner = m
		}
	}
	if !found {
		return nil, problem(404, "Pesan uji A/B tidak ditemukan.")
	}
	previous := map[string]bool{}
	for _, m := range a.data.Messages {
		if m.ABGroup == group {
			previous[m.ID] = m.ABWinner
			m.ABWinner = m.ID == messageID
		}
	}
	if err := a.saveLocked(); err != nil {
		for _, m := range a.data.Messages {
			if m.ABGroup == group {
				m.ABWinner = previous[m.ID]
			}
		}
		return nil, err
	}
	return winner, nil
}
