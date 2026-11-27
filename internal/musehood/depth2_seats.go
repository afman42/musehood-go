package musehood

// Fitur pendalaman grup A (Kursi): memori otomatis (3), kloning kursi (4),
// budget harian per kursi (5), dan model per jenis tugas (6).

import (
	"fmt"
	"log"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// dailyUsageKey adalah kunci tanggal pemakaian harian (waktu lokal server).
func dailyUsageKey() string {
	return time.Now().Format("2006-01-02")
}

// budgetExceededLocked memeriksa apakah kursi sudah mencapai budget
// hariannya hari ini (fitur 5). Pemanggil wajib memegang a.mu.
func (a *App) budgetExceededLocked(member seat) bool {
	if member.DailyBudgetUSD <= 0 {
		return false
	}
	day := a.data.DailyUsage[dailyUsageKey()]
	if day == nil {
		return false
	}
	usage := day[member.Code]
	return usage != nil && usage.CostUSD >= member.DailyBudgetUSD
}

// recordDailyUsageLocked menambahkan pemakaian token+biaya hari ini untuk
// satu kursi (fitur 5). Harga diambil dari provider yang cocok dengan
// cfg.ProviderName (nama atau alias). Pemanggil wajib memegang a.mu.
func (a *App) recordDailyUsageLocked(code string, cfg settings, promptTokens, completionTokens int64) {
	if a.data.DailyUsage == nil {
		a.data.DailyUsage = map[string]map[string]*dailyUsage{}
	}
	today := dailyUsageKey()
	day := a.data.DailyUsage[today]
	if day == nil {
		day = map[string]*dailyUsage{}
		a.data.DailyUsage[today] = day
	}
	usage := day[code]
	if usage == nil {
		usage = &dailyUsage{}
		day[code] = usage
	}
	usage.TokensIn += promptTokens
	usage.TokensOut += completionTokens
	for _, p := range a.data.Providers {
		if p.Name == cfg.ProviderName || (p.Alias != "" && p.Alias == cfg.ProviderName) {
			usage.CostUSD += float64(promptTokens)/1e6*p.PriceInputPer1M +
				float64(completionTokens)/1e6*p.PriceOutputPer1M
			break
		}
	}
	// Jaga ukuran peta: buang tanggal tertua bila sudah lebih dari 40 hari.
	for len(a.data.DailyUsage) > 40 {
		oldest := ""
		for date := range a.data.DailyUsage {
			if date != today && (oldest == "" || date < oldest) {
				oldest = date
			}
		}
		if oldest == "" {
			break
		}
		delete(a.data.DailyUsage, oldest)
	}
}

// cloneSeatLocked mengkloning kursi menjadi kursi custom baru (fitur 4):
// semua field konfigurasi (termasuk memori) disalin, kode C baru, dan nama
// diberi akhiran " (salinan)" (ditambah nomor bila nama itu sudah dipakai).
// Pemanggil wajib memegang a.mu.
func (a *App) cloneSeatLocked(code string) (*seat, error) {
	original := a.data.findSeat(code)
	if original == nil {
		return nil, problem(404, "Kursi tidak ditemukan.")
	}
	cloneCode := ""
	for n := 1; n < 100; n++ {
		candidate := fmt.Sprintf("C%02d", n)
		if a.data.findSeat(candidate) == nil {
			cloneCode = candidate
			break
		}
	}
	if cloneCode == "" {
		return nil, problem(400, "Jumlah kursi custom sudah mencapai batas.")
	}
	taken := map[string]bool{}
	for _, s := range a.data.Seats {
		taken[strings.ToLower(s.Name)] = true
	}
	base := original.Name + " (salinan)"
	if jsLength(base) > 40 {
		base = strings.TrimSpace(string([]rune(original.Name)[:40-len(" (salinan)")])) + " (salinan)"
	}
	name := base
	for n := 2; taken[strings.ToLower(name)]; n++ {
		name = fmt.Sprintf("%s %d", base, n)
		if jsLength(name) > 40 {
			name = strings.TrimSpace(string([]rune(base)[:40-len(fmt.Sprintf(" %d", n))])) + fmt.Sprintf(" %d", n)
		}
	}
	clone := *original
	clone.Code = cloneCode
	clone.Name = name
	clone.Present = true
	// Salinan dalam untuk field pointer/irisan agar klon tidak berbagi
	// penyimpanan dengan kursi asal.
	if clone.Model != nil {
		clone.Model = ptr(*clone.Model)
	}
	if clone.AutoMemory != nil {
		clone.AutoMemory = ptr(*clone.AutoMemory)
	}
	clone.FallbackModels = slices.Clone(clone.FallbackModels)
	clone.DisabledTools = slices.Clone(clone.DisabledTools)
	clone.Skills = slices.Clone(clone.Skills)
	next := a.data
	next.Seats = append(slices.Clone(next.Seats), clone)
	if err := writeJSONFile(filepath.Join(a.dir, "data.json"), next); err != nil {
		return nil, err
	}
	a.data = next
	return a.data.findSeat(cloneCode), nil
}

// trimSeatMemory memotong memori kursi ke 4000 karakter dengan membuang
// bagian tertua (FIFO dari depan) bila melebihi batas (fitur 3).
func trimSeatMemory(memory string) string {
	runes := []rune(memory)
	if len(runes) <= 4000 {
		return memory
	}
	return string(runes[len(runes)-4000:])
}

const autoMemoryPrompt = "Tulis 1-3 poin memori singkat dari ringkasan diskusi berikut untuk dirimu sendiri, agar bisa kamu ingat di percakapan berikutnya. Satu poin per baris, diawali tanda hubung. Tanpa pembuka atau penutup lain.\n\nRingkasan:\n"

// autoMemoryAfterSummaryLocked memicu penulisan memori otomatis (fitur 3)
// untuk kursi gateway peserta ruang yang mengaktifkan AutoMemory: tiap
// kursi dipanggil modelnya (model tugas "summary") dengan ringkasan ruang,
// lalu hasilnya ditambahkan ke Memory kursi itu. Berjalan di goroutine
// agar tidak memblokir penyelesaian langkah ringkasan. Pemanggil wajib
// memegang a.mu.
func (a *App) autoMemoryAfterSummaryLocked(roomID, summary string) {
	if a.closed {
		return
	}
	r := a.data.findRoom(roomID)
	if r == nil {
		return
	}
	type target struct {
		cfg    settings
		member seat
		system string
	}
	var targets []target
	for _, code := range a.data.roomSeatCodes(r) {
		s := a.data.findSeat(code)
		if s == nil || s.Provider != "gateway" || s.AutoMemory == nil || !*s.AutoMemory {
			continue
		}
		member := *s
		taskModel := member.modelForTask("summary")
		if taskModel == "" {
			continue
		}
		cfg, resolved := a.data.resolveModelProviderLocked(a.data.Settings, taskModel)
		member.Model = &resolved
		targets = append(targets, target{cfg, member, a.systemPrompt(member, a.data)})
	}
	if len(targets) == 0 {
		return
	}
	redactedSummary := a.redactLocked(summary)
	a.wg.Go(func() {
		for _, t := range targets {
			history := []gatewayMessage{
				{Role: "system", Content: t.system},
				{Role: "user", Content: autoMemoryPrompt + redactedSummary},
			}
			text, err := a.chat(a.ctx, t.cfg, t.member, history)
			if err != nil {
				log.Printf("Memori otomatis kursi %s gagal: %v", t.member.Code, err)
				continue
			}
			text = strings.TrimSpace(text)
			if text == "" {
				continue
			}
			a.mu.Lock()
			if s := a.data.findSeat(t.member.Code); s != nil && s.AutoMemory != nil && *s.AutoMemory {
				combined := strings.TrimSpace(s.Memory)
				if combined != "" {
					combined += "\n"
				}
				s.Memory = trimSeatMemory(combined + a.redactLocked(text))
				a.saveJobLocked()
			}
			a.mu.Unlock()
		}
	})
}
