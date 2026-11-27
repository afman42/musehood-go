package musehood

import (
	"fmt"
	"strings"
	"time"
)

// Ringkasan harian terjadwal (fitur 27): pada jam yang diatur (zona
// Asia/Jakarta, sekali per hari), ringkasan diskusi hari ini dari semua
// ruang dibuat oleh kursi integrator (satu panggilan gateway; fallback
// daftar ruang + jumlah pesan bila gateway gagal), disimpan sebagai
// pesan di ruang utama, dan dikirim ke webhook/Telegram sesuai saluran.

const dailySummaryLabel = "Ringkasan harian"

// jakartaLocation mengembalikan zona Asia/Jakarta (fallback zona tetap
// UTC+7 bila basis data zona waktu tidak tersedia).
func jakartaLocation() *time.Location {
	if loc, err := time.LoadLocation("Asia/Jakarta"); err == nil {
		return loc
	}
	return time.FixedZone("WIB", 7*60*60)
}

// dailySummaryDueLocked melaporkan apakah ringkasan harian jatuh tempo
// pada waktu now (sudah dikonversi ke Asia/Jakarta oleh pemanggil atau
// di sini). Pemanggil wajib memegang a.mu.
func (a *App) dailySummaryDueLocked(now time.Time) bool {
	if !a.data.Settings.dailySummaryOn() {
		return false
	}
	local := now.In(jakartaLocation())
	if local.Format("15:04") != a.data.Settings.dailySummaryTime() {
		return false
	}
	return a.data.LastDailySummary != local.Format("2006-01-02")
}

// checkDailySummaryLocked dipanggil dari tickLocked: bila jatuh tempo,
// tandai tanggal hari ini (agar tidak dobel) lalu jalankan pembuatan
// ringkasan di goroutine. Pemanggil wajib memegang a.mu.
func (a *App) checkDailySummaryLocked() {
	if a.closed || !a.dailySummaryDueLocked(time.Now()) {
		return
	}
	date := time.Now().In(jakartaLocation()).Format("2006-01-02")
	a.data.LastDailySummary = date
	a.saveJobLocked()
	a.wg.Go(func() { a.runDailySummary(date) })
}

// dailySummaryMaterialLocked menyusun bahan ringkasan: daftar ruang
// dengan jumlah pesan hari ini + kutipan pesan terakhir tiap ruang,
// beserta teks fallback bila gateway tidak tersedia/gagal.
// Pemanggil wajib memegang a.mu.
func (a *App) dailySummaryMaterialLocked(date string) (material, fallback string) {
	loc := jakartaLocation()
	var b strings.Builder
	var fb strings.Builder
	fmt.Fprintf(&fb, "Ringkasan harian %s:\n", date)
	total := 0
	for _, r := range a.data.Rooms {
		var todays []*message
		for _, m := range a.data.Messages {
			if m.RoomID != r.ID || m.Status == "typing" || m.Status == "waiting" {
				continue
			}
			stamp, err := time.Parse(time.RFC3339Nano, m.CreatedAt)
			if err != nil || stamp.In(loc).Format("2006-01-02") != date {
				continue
			}
			todays = append(todays, m)
		}
		if len(todays) == 0 {
			continue
		}
		total += len(todays)
		fmt.Fprintf(&fb, "- Ruang %s: %d pesan\n", r.Name, len(todays))
		fmt.Fprintf(&b, "Ruang %s (%d pesan hari ini):\n", r.Name, len(todays))
		start := 0
		if len(todays) > 12 {
			start = len(todays) - 12
		}
		for _, m := range todays[start:] {
			name := a.data.Owner.Name
			if m.AuthorType == "owner" {
				if m.Author != "" {
					name = m.Author
				}
			} else if s := a.data.findSeat(m.Seat); s != nil {
				name = s.Name
			} else if m.Seat != "" {
				name = m.Seat
			}
			text := strings.TrimSpace(m.Text)
			if text == "" {
				continue
			}
			fmt.Fprintf(&b, "- %s: %s\n", name, truncateRunes(text, 300))
		}
		b.WriteString("\n")
	}
	if total == 0 {
		fb.WriteString("- Tidak ada pesan hari ini.\n")
	}
	return b.String(), strings.TrimSpace(fb.String())
}

// runDailySummary membuat ringkasan harian untuk tanggal date (format
// "2006-01-02" Asia/Jakarta) dan menyimpannya sebagai pesan kursi
// integrator di ruang utama, lalu mengirimkannya sesuai saluran.
func (a *App) runDailySummary(date string) {
	a.mu.Lock()
	material, fallback := a.dailySummaryMaterialLocked(date)
	integratorCode := a.integratorSeat()
	integrator := a.data.findSeat(integratorCode)
	var integratorCopy seat
	var cfg settings
	gatewayReady := false
	if integrator != nil && integrator.Provider == "gateway" {
		integratorCopy = *integrator
		cfg = a.data.Settings
		if model := integratorCopy.modelForTask("summary"); model != "" {
			var resolved string
			cfg, resolved = a.data.resolveModelProviderLocked(cfg, model)
			integratorCopy.Model = &resolved
			gatewayReady = true
		}
	}
	dataCopy := a.data
	channel := a.data.Settings.dailySummaryChannel()
	webhookURL := a.data.Settings.WebhookURL
	telegramToken := a.data.Settings.TelegramBotToken
	telegramChatID := a.data.Settings.TelegramChatID
	a.mu.Unlock()

	summary := fallback
	if gatewayReady && strings.TrimSpace(material) != "" {
		history := []gatewayMessage{
			{"system", a.systemPrompt(integratorCopy, dataCopy)},
			{"user", fmt.Sprintf("Buat ringkasan harian diskusi grup untuk tanggal %s berdasarkan bahan berikut. Sebutkan topik utama, keputusan, dan hal yang masih terbuka per ruang secara ringkas dalam Bahasa Indonesia.\n\n%s", date, material)},
		}
		if reply, err := a.chat(a.ctx, cfg, integratorCopy, history); err == nil && strings.TrimSpace(reply) != "" {
			summary = strings.TrimSpace(reply)
		}
	}

	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		return
	}
	room := a.data.findRoom(defaultRoomID)
	if room == nil && len(a.data.Rooms) > 0 {
		room = &a.data.Rooms[0]
	}
	if room == nil {
		a.mu.Unlock()
		return
	}
	m := &message{ID: uuid(), CreatedAt: now(), AuthorType: "seat", Seat: integratorCode, Text: a.redactLocked(summary), Status: "completed", Label: dailySummaryLabel, RoomID: room.ID, BranchID: room.activeBranch()}
	a.data.Messages = append(a.data.Messages, m)
	a.saveJobLocked()
	a.mu.Unlock()

	switch channel {
	case "webhook":
		if webhookURL != "" {
			a.fireWebhook(webhookURL, map[string]any{"event": "daily.summary", "date": date, "text": summary, "time": now()})
		}
	case "telegram":
		if telegramToken != "" && telegramChatID != "" {
			a.telegramSend(telegramToken, telegramChatID, truncateRunes(summary, 4096))
		}
	}
}
