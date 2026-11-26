package musehood

import (
	"strings"
)

// discussionTemplate adalah preset pipeline diskusi: urutan kursi yang
// menjawab plus instruksi tambahan yang disisipkan ke prompt.
type discussionTemplate struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Seats       []string `json:"seats"`
	Instruction string   `json:"instruction,omitempty"`
	CreatedAt   string   `json:"createdAt"`
}

type templateInput struct {
	Name        *string   `json:"name"`
	Seats       *[]string `json:"seats"`
	Instruction *string   `json:"instruction"`
}

const summaryLabel = "Ringkasan diskusi"
const summaryPrompt = "Ringkas seluruh diskusi di ruang ini sejauh ini untuk pemilik: keputusan yang diambil, fakta penting, dan hal yang masih terbuka. Tulis ringkas dan jelas dalam Bahasa Indonesia. Ringkasan ini akan dipakai sebagai konteks percakapan berikutnya."

func (a *App) validateTemplateLocked(t *discussionTemplate) error {
	t.Name = strings.TrimSpace(t.Name)
	if t.Name == "" || jsLength(t.Name) > 80 || badName.MatchString(t.Name) {
		return problem(400, "Nama template wajib diisi, maksimal 80 karakter, tanpa @ atau baris baru.")
	}
	if len(t.Seats) == 0 || len(t.Seats) > 12 {
		return problem(400, "Template harus berisi 1 sampai 12 kursi.")
	}
	for _, code := range t.Seats {
		if a.data.findSeat(code) == nil {
			return problem(400, "Kode kursi template tidak dikenal: "+code)
		}
	}
	t.Instruction = strings.TrimSpace(t.Instruction)
	if jsLength(t.Instruction) > 2000 || strings.Contains(t.Instruction, "\x00") {
		return problem(400, "Instruksi template maksimal 2000 karakter.")
	}
	return nil
}

func (a *App) createTemplateLocked(input templateInput) (discussionTemplate, error) {
	if len(a.data.Templates) >= 30 {
		return discussionTemplate{}, problem(400, "Maksimal 30 template diskusi.")
	}
	t := discussionTemplate{ID: uuid(), CreatedAt: now()}
	if input.Name != nil {
		t.Name = *input.Name
	}
	if input.Seats != nil {
		t.Seats = *input.Seats
	}
	if input.Instruction != nil {
		t.Instruction = *input.Instruction
	}
	if err := a.validateTemplateLocked(&t); err != nil {
		return discussionTemplate{}, err
	}
	t.Instruction = a.redactLocked(t.Instruction)
	previous := a.data.Templates
	a.data.Templates = append(a.data.Templates, t)
	if err := a.saveLocked(); err != nil {
		a.data.Templates = previous
		return discussionTemplate{}, err
	}
	return t, nil
}

func (a *App) updateTemplateLocked(id string, input templateInput) (discussionTemplate, error) {
	for i := range a.data.Templates {
		if a.data.Templates[i].ID != id {
			continue
		}
		t := a.data.Templates[i]
		if input.Name != nil {
			t.Name = *input.Name
		}
		if input.Seats != nil {
			t.Seats = *input.Seats
		}
		if input.Instruction != nil {
			t.Instruction = *input.Instruction
		}
		if err := a.validateTemplateLocked(&t); err != nil {
			return discussionTemplate{}, err
		}
		t.Instruction = a.redactLocked(t.Instruction)
		previous := a.data.Templates[i]
		a.data.Templates[i] = t
		if err := a.saveLocked(); err != nil {
			a.data.Templates[i] = previous
			return discussionTemplate{}, err
		}
		return t, nil
	}
	return discussionTemplate{}, problem(404, "Template tidak ditemukan.")
}

func (a *App) deleteTemplateLocked(id string) error {
	for i := range a.data.Templates {
		if a.data.Templates[i].ID == id {
			previous := a.data.Templates
			a.data.Templates = append(a.data.Templates[:i], a.data.Templates[i+1:]...)
			if err := a.saveLocked(); err != nil {
				a.data.Templates = previous
				return err
			}
			return nil
		}
	}
	return problem(404, "Template tidak ditemukan.")
}

func (a *App) findTemplateLocked(id string) *discussionTemplate {
	for i := range a.data.Templates {
		if a.data.Templates[i].ID == id {
			return &a.data.Templates[i]
		}
	}
	return nil
}

// submitTemplateLocked mengirim pesan pemilik dengan pipeline kursi template.
func (a *App) submitTemplateLocked(text, roomID, templateID string) (*job, error) {
	text = strings.TrimSpace(text)
	if text == "" || jsLength(text) > 20000 {
		return nil, problem(400, "Pesan wajib berisi teks, maksimal 20.000 karakter.")
	}
	t := a.findTemplateLocked(templateID)
	if t == nil {
		return nil, problem(404, "Template tidak ditemukan.")
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
	steps := []step{}
	for _, code := range t.Seats {
		if a.data.roomHasSeat(r, code) {
			steps = append(steps, step{Seat: code, Kind: "chat", Status: "pending"})
		}
	}
	if len(steps) == 0 {
		return nil, problem(400, "Tidak ada kursi template yang ikut di ruang ini.")
	}
	text = a.redactLocked(text)
	prompt := text
	if t.Instruction != "" {
		prompt = t.Instruction + "\n\n" + text
	}
	j := &job{ID: uuid(), Prompt: prompt, Kind: "template", CreatedAt: now(), Status: "processing", Steps: steps, RoomID: r.ID}
	previous := a.data
	a.data.Jobs = append(a.data.Jobs, j)
	a.data.Messages = append(a.data.Messages, &message{ID: uuid(), CreatedAt: now(), AuthorType: "owner", JobID: j.ID, Text: text, Status: "completed", RoomID: r.ID})
	if err := a.saveLocked(); err != nil {
		a.data = previous
		return nil, err
	}
	a.advanceLocked(j)
	return j, nil
}

// summarizeRoomLocked membuat pekerjaan ringkasan ruang oleh kursi integrator.
func (a *App) summarizeRoomLocked(roomID string) (*job, error) {
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
	for _, j := range a.data.Jobs {
		if j.RoomID == r.ID && j.Kind == "roomsum" && j.Status != "completed" {
			return nil, problem(409, "Ringkasan ruang ini sedang dibuat.")
		}
	}
	j := &job{ID: uuid(), Prompt: summaryPrompt, Kind: "roomsum", CreatedAt: now(), Status: "processing",
		Steps: []step{{Seat: a.integratorSeat(), Kind: "roomsum", Status: "pending"}}, RoomID: r.ID}
	previous := a.data
	a.data.Jobs = append(a.data.Jobs, j)
	if err := a.saveLocked(); err != nil {
		a.data = previous
		return nil, err
	}
	a.advanceLocked(j)
	return j, nil
}

// autoCompactLocked memicu ringkasan otomatis bila ruang menumpuk 80 pesan
// baru sejak ringkasan terakhir (kompaksi konteks).
func (a *App) autoCompactLocked() {
	for i := range a.data.Rooms {
		r := &a.data.Rooms[i]
		count := 0
		for _, m := range a.data.Messages {
			if m.RoomID == r.ID && m.Status != "typing" && m.Status != "waiting" {
				count++
			}
		}
		if count-r.SummaryCount >= 80 {
			_, _ = a.summarizeRoomLocked(r.ID)
		}
	}
}
