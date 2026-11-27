package musehood

import (
	"slices"
	"strings"
)

// Rantai prompt (fitur 24): template langkah otomatis. Setiap langkah
// adalah satu panggilan gateway ke kursi pilihan (non-streaming, tanpa
// tool); output langkah sebelumnya menjadi input langkah berikutnya
// (instruction + "\n\n" + output sebelumnya). Hasil tiap langkah disimpan
// sebagai pesan asisten berlabel rantai (ChainID + ChainStep).

type chainStep struct {
	SeatCode    string `json:"seatCode"`
	Instruction string `json:"instruction"`
}

type promptChain struct {
	ID        string      `json:"id"`
	Name      string      `json:"name"`
	Steps     []chainStep `json:"steps"`
	CreatedAt string      `json:"createdAt"`
}

type chainInput struct {
	Name  *string      `json:"name"`
	Steps *[]chainStep `json:"steps"`
}

const maxChains = 20
const maxChainSteps = 6

func (a *App) validateChainLocked(c *promptChain) error {
	c.Name = strings.TrimSpace(c.Name)
	if c.Name == "" || jsLength(c.Name) > 80 || badName.MatchString(c.Name) {
		return problem(400, "Nama rantai wajib diisi, maksimal 80 karakter, tanpa @ atau baris baru.")
	}
	if len(c.Steps) == 0 || len(c.Steps) > maxChainSteps {
		return problem(400, "Rantai harus berisi 1 sampai 6 langkah.")
	}
	for i := range c.Steps {
		step := &c.Steps[i]
		step.SeatCode = strings.TrimSpace(step.SeatCode)
		if a.data.findSeat(step.SeatCode) == nil {
			return problem(400, "Kode kursi langkah rantai tidak dikenal: "+step.SeatCode)
		}
		step.Instruction = strings.TrimSpace(step.Instruction)
		if step.Instruction == "" || jsLength(step.Instruction) > 2000 || strings.Contains(step.Instruction, "\x00") {
			return problem(400, "Instruksi langkah rantai wajib diisi, maksimal 2000 karakter.")
		}
	}
	return nil
}

func (a *App) createChainLocked(input chainInput) (promptChain, error) {
	if len(a.data.Chains) >= maxChains {
		return promptChain{}, problem(400, "Maksimal 20 rantai prompt.")
	}
	c := promptChain{ID: uuid(), CreatedAt: now()}
	if input.Name != nil {
		c.Name = *input.Name
	}
	if input.Steps != nil {
		c.Steps = *input.Steps
	}
	if err := a.validateChainLocked(&c); err != nil {
		return promptChain{}, err
	}
	for i := range c.Steps {
		c.Steps[i].Instruction = a.redactLocked(c.Steps[i].Instruction)
	}
	previous := a.data.Chains
	a.data.Chains = append(a.data.Chains, c)
	if err := a.saveLocked(); err != nil {
		a.data.Chains = previous
		return promptChain{}, err
	}
	return c, nil
}

func (a *App) updateChainLocked(id string, input chainInput) (promptChain, error) {
	for i := range a.data.Chains {
		if a.data.Chains[i].ID != id {
			continue
		}
		c := a.data.Chains[i]
		if input.Name != nil {
			c.Name = *input.Name
		}
		if input.Steps != nil {
			c.Steps = *input.Steps
		}
		if err := a.validateChainLocked(&c); err != nil {
			return promptChain{}, err
		}
		for j := range c.Steps {
			c.Steps[j].Instruction = a.redactLocked(c.Steps[j].Instruction)
		}
		previous := a.data.Chains[i]
		a.data.Chains[i] = c
		if err := a.saveLocked(); err != nil {
			a.data.Chains[i] = previous
			return promptChain{}, err
		}
		return c, nil
	}
	return promptChain{}, problem(404, "Rantai tidak ditemukan.")
}

func (a *App) deleteChainLocked(id string) error {
	for i := range a.data.Chains {
		if a.data.Chains[i].ID == id {
			previous := a.data.Chains
			a.data.Chains = slices.Delete(slices.Clone(a.data.Chains), i, i+1)
			if err := a.saveLocked(); err != nil {
				a.data.Chains = previous
				return err
			}
			return nil
		}
	}
	return problem(404, "Rantai tidak ditemukan.")
}

func (a *App) findChainLocked(id string) *promptChain {
	for i := range a.data.Chains {
		if a.data.Chains[i].ID == id {
			return &a.data.Chains[i]
		}
	}
	return nil
}

// runChainLocked menjalankan satu rantai: membuat pesan pemilik (teks
// awal) + pesan asisten per langkah (status typing), lalu goroutine
// menjalankan langkah berurutan. Pemanggil wajib memegang a.mu.
func (a *App) runChainLocked(id, text, roomID string) (any, error) {
	c := a.findChainLocked(id)
	if c == nil {
		return nil, problem(404, "Rantai tidak ditemukan.")
	}
	text = strings.TrimSpace(text)
	if text == "" || jsLength(text) > 20000 {
		return nil, problem(400, "Teks awal rantai wajib diisi, maksimal 20.000 karakter.")
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
	// Salin rantai agar goroutine tidak membaca data yang berubah.
	chain := *c
	chain.Steps = slices.Clone(c.Steps)
	text = a.redactLocked(text)
	previous := a.data
	a.data.Messages = append(a.data.Messages, &message{ID: uuid(), CreatedAt: now(), AuthorType: "owner", Text: text, Status: "completed", RoomID: r.ID, BranchID: r.activeBranch()})
	ids := make([]string, 0, len(chain.Steps))
	answers := make([]*message, 0, len(chain.Steps))
	for i, step := range chain.Steps {
		seatName := step.SeatCode
		if s := a.data.findSeat(step.SeatCode); s != nil {
			seatName = s.Name
		}
		m := &message{ID: uuid(), CreatedAt: now(), AuthorType: "seat", Seat: step.SeatCode, Status: "typing", StatusText: seatName + " menjalankan langkah rantai…", Label: "Rantai: " + chain.Name, ChainID: chain.ID, ChainStep: i + 1, RoomID: r.ID, BranchID: r.activeBranch()}
		a.data.Messages = append(a.data.Messages, m)
		answers = append(answers, m)
		ids = append(ids, m.ID)
	}
	if err := a.saveLocked(); err != nil {
		a.data = previous
		return nil, err
	}
	a.wg.Go(func() { a.runChainSteps(chain, answers, text) })
	return map[string]any{"chainId": chain.ID, "messages": ids}, nil
}

// runChainSteps menjalankan langkah rantai berurutan di goroutine:
// input langkah = instruction + "\n\n" + output langkah sebelumnya
// (langkah pertama memakai teks awal pemilik). Bila satu langkah gagal,
// langkah itu ditandai error dan langkah berikutnya dibatalkan dengan
// catatan, karena inputnya bergantung pada output sebelumnya.
func (a *App) runChainSteps(chain promptChain, answers []*message, input string) {
	previousOutput := input
	for i, step := range chain.Steps {
		m := answers[i]
		a.mu.Lock()
		member := a.data.findSeat(step.SeatCode)
		var memberCopy seat
		var cfg settings
		if member != nil {
			memberCopy = *member
			cfg = a.data.Settings
			if model := memberCopy.modelForTask("chat"); model != "" {
				var resolved string
				cfg, resolved = a.data.resolveModelProviderLocked(cfg, model)
				memberCopy.Model = &resolved
			}
		}
		dataCopy := a.data
		a.mu.Unlock()
		if member == nil {
			a.finishChainMessage(m, "", "Kursi langkah rantai tidak ditemukan.")
			a.cancelRemainingChainSteps(answers, i+1)
			return
		}
		if memberCopy.Provider != "gateway" {
			a.finishChainMessage(m, "", "Kursi antrean Muse tidak dapat menjalankan langkah rantai.")
			a.cancelRemainingChainSteps(answers, i+1)
			return
		}
		history := []gatewayMessage{
			{"system", a.systemPrompt(memberCopy, dataCopy)},
			{"user", step.Instruction + "\n\n" + previousOutput},
		}
		for k := range history {
			a.mu.Lock()
			history[k].Content = a.redactLocked(history[k].Content)
			a.mu.Unlock()
		}
		reply, err := a.chat(a.ctx, cfg, memberCopy, history)
		if err != nil {
			a.mu.Lock()
			errText := a.redactLocked(publicError(err))
			a.mu.Unlock()
			a.finishChainMessage(m, "", errText)
			a.cancelRemainingChainSteps(answers, i+1)
			return
		}
		output := strings.TrimSpace(reply)
		a.finishChainMessage(m, output, "")
		previousOutput = output
	}
}

// finishChainMessage memfinalkan satu pesan langkah rantai.
func (a *App) finishChainMessage(m *message, text, errText string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed {
		return
	}
	if errText != "" {
		m.Error = errText
		m.Status = "error"
	} else {
		m.Text = a.redactLocked(text)
		m.Status = "completed"
	}
	m.StatusText = ""
	a.saveJobLocked()
}

// cancelRemainingChainSteps menandai langkah-langkah sesudah langkah
// yang gagal sebagai error (inputnya tidak tersedia).
func (a *App) cancelRemainingChainSteps(answers []*message, from int) {
	for _, m := range answers[from:] {
		a.finishChainMessage(m, "", "Langkah rantai dibatalkan: langkah sebelumnya gagal.")
	}
}
