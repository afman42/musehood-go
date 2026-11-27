package musehood

// Mode paralel (fitur 15): kursi-kursi dalam satu putaran diskusi chat
// berjalan konkuren. Berlaku untuk pekerjaan chat dengan >1 langkah
// gateway (tanpa kursi antrean Muse, tanpa balasan berantai); selain itu
// jalur sekuensial biasa dipakai. Urutan tampil pesan mengikuti urutan
// kursi karena pesan dibuat lebih dulu berurutan.

import (
	"context"
	"fmt"
)

// parallelEligibleLocked memeriksa apakah pekerjaan boleh dijalankan
// paralel. Pemanggil wajib memegang a.mu.
func (a *App) parallelEligibleLocked(j *job) bool {
	if !a.data.Settings.parallelOn() || j.Kind != "chat" || j.Index != 0 || len(j.Steps) < 2 {
		return false
	}
	if a.data.Settings.chainRepliesOn() {
		return false
	}
	for _, s := range j.Steps {
		if s.Kind != "chat" {
			return false
		}
		member := a.data.findSeat(s.Seat)
		if member == nil || member.Provider == "muse" {
			return false
		}
	}
	return true
}

// fallbackAttempt adalah satu percobaan model cadangan (konfigurasi +
// kursi dengan model ter-resolusi) yang diselesaikan di bawah mutex
// sebelum goroutine berjalan.
type fallbackAttempt struct {
	cfg    settings
	member seat
}

type parallelStep struct {
	s          *step
	member     seat
	m          *message
	cfg        settings
	history    []gatewayMessage
	fallbacks  []fallbackAttempt
	lbAttempts []fallbackAttempt
}

// advanceParallelLocked menjalankan semua langkah chat pekerjaan secara
// konkuren. Pemanggil wajib memegang a.mu.
func (a *App) advanceParallelLocked(j *job) {
	tools := a.data.Settings.toolsOn()
	code := a.data.Settings.codeToolOn()
	stream := a.data.Settings.streamingOn()
	var runnable []parallelStep
	for i := range j.Steps {
		s := &j.Steps[i]
		member := *a.data.findSeat(s.Seat)
		m := a.messageLocked(j, s, member)
		if a.budgetExceededLocked(member) {
			key := j.ID + "|" + member.Code
			if !a.budgetNoted[key] {
				a.budgetNoted[key] = true
				m.Error = fmt.Sprintf("Kursi %s dilewati: budget harian $%.2f sudah tercapai hari ini.", member.Name, member.DailyBudgetUSD)
			}
			s.Status = "completed"
			m.Status = "error"
			m.StatusText = ""
			continue
		}
		cfg := a.data.Settings
		task := "chat"
		if tools {
			task = "tools"
		}
		if model := member.modelForTask(task); model != "" {
			var resolved string
			cfg, resolved = a.data.resolveModelProviderLocked(cfg, model)
			member.Model = &resolved
		}
		history := a.gatewayMessages(member, a.data, j)
		for k := range history {
			history[k].Content = a.redactLocked(history[k].Content)
		}
		var fallbacks []fallbackAttempt
		for _, fb := range member.fallbackChain() {
			fbCfg, resolved := a.data.resolveModelProviderLocked(a.data.Settings, fb)
			fbMember := member
			fbMember.Model = &resolved
			fallbacks = append(fallbacks, fallbackAttempt{fbCfg, fbMember})
		}
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
		runnable = append(runnable, parallelStep{s, member, m, cfg, history, fallbacks, lbAttempts})
	}
	if len(runnable) == 0 {
		j.Index = len(j.Steps)
		a.completeJobLocked(j)
		return
	}
	a.saveJobLocked()
	ctx, cancel := context.WithCancel(a.ctx)
	a.running[j] = cancel
	if a.parallelRemaining == nil {
		a.parallelRemaining = map[*job]int{}
	}
	a.parallelRemaining[j] = len(runnable)
	for _, p := range runnable {
		p := p
		a.wg.Go(func() {
			onActivity := func(toolName string) {
				a.mu.Lock()
				p.m.StatusText = p.member.Name + " menjalankan " + toolName + "…"
				a.mu.Unlock()
			}
			onDelta := func(partial string) {
				a.mu.Lock()
				p.m.Text = a.redactLocked(partial)
				a.mu.Unlock()
			}
			reply, err := a.chatWithTools(ctx, p.cfg, p.member, p.history, tools, code, stream, onActivity, onDelta)
			for _, fb := range p.fallbacks {
				if err == nil {
					break
				}
				if fbReply, fbErr := a.chatWithTools(ctx, fb.cfg, fb.member, p.history, tools, code, stream, onActivity, onDelta); fbErr == nil {
					reply, err = fbReply, nil
				}
			}
			for _, lb := range p.lbAttempts {
				if err == nil {
					break
				}
				if lbReply, lbErr := a.chatWithTools(ctx, lb.cfg, lb.member, p.history, tools, code, stream, onActivity, onDelta); lbErr == nil {
					reply, err = lbReply, nil
				}
			}
			a.mu.Lock()
			defer a.mu.Unlock()
			if ctx.Err() != nil || a.closed {
				a.parallelRemaining[j]--
				if a.parallelRemaining[j] <= 0 {
					delete(a.parallelRemaining, j)
					delete(a.running, j)
				}
				return
			}
			if err == nil {
				if reply.Error != "" {
					p.m.Error = appendError(p.m.Error, a.redactLocked(reply.Error))
				} else {
					p.m.ImageURL = reply.ImageURL
					p.m.Text = a.redactLocked(reply.Text)
					p.m.ToolsUsed = reply.ToolsUsed
					p.m.ToolCalls = reply.ToolCalls
					for i := range p.m.ToolCalls {
						p.m.ToolCalls[i].Arg = a.redactLocked(p.m.ToolCalls[i].Arg)
						p.m.ToolCalls[i].Result = a.redactLocked(p.m.ToolCalls[i].Result)
					}
				}
			} else {
				p.m.Error = a.redactLocked(publicError(err))
				if a.data.Settings.DemoFallback {
					p.m.Demo = true
					p.m.Text = a.redactLocked(fmt.Sprintf("DEMO — %s (%s): simulasi lokal untuk permintaan “%s”. Belum ada hasil model nyata; periksa koneksi gateway.", p.member.Name, p.member.Role, j.Prompt))
				}
			}
			a.parallelFinishLocked(j, p.s, p.m)
		})
	}
}

// parallelFinishLocked menandai satu langkah paralel selesai; langkah
// terakhir menyelesaikan pekerjaan. Pemanggil wajib memegang a.mu.
func (a *App) parallelFinishLocked(j *job, s *step, m *message) {
	s.Status = "completed"
	m.Status = "completed"
	if m.Error != "" && m.Text == "" && m.ImageURL == "" {
		m.Status = "error"
	}
	m.StatusText = ""
	a.parallelRemaining[j]--
	if a.parallelRemaining[j] <= 0 {
		delete(a.parallelRemaining, j)
		delete(a.running, j)
		j.Index = len(j.Steps)
		a.completeJobLocked(j)
		return
	}
	a.saveJobLocked()
}
