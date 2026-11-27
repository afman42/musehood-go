package musehood

import (
	"context"
	"strings"
)

// Koreksi otomatis balasan (fitur 25): bila settings.AutoReview aktif,
// balasan kursi gateway diperiksa kursi peninjau sebelum disimpan final.
// Peninjau diminta mengembalikan teks asli persis bila sudah benar, atau
// versi perbaikan bila ada kesalahan nyata. Teks asli disimpan di
// message.OriginalText bila koreksi mengubah teks. Balasan kursi
// peninjau sendiri, pesan gambar/voting, dan balasan pekerja Muse tidak
// ditinjau; tinjauan hanya satu kali per pesan (panggilan langsung,
// tidak rekursif).

const autoReviewInstruction = "Periksa balasan berikut yang ditulis kursi lain. Bila balasan itu sudah benar dan layak, balas PERSIS teks aslinya tanpa tambahan, pengantar, atau penjelasan apa pun. Bila ada kesalahan nyata (fakta, logika, bahasa, atau format), balas HANYA versi yang sudah diperbaiki, tanpa penjelasan tentang perubahanmu.\n\n---\n"

// autoReviewReply menjalankan satu tinjauan atas teks balasan. Mengembalikan
// (teks hasil tinjauan, true) bila tinjauan berhasil; (\"\", false) bila
// fitur mati, peninjau tidak tersedia, atau panggilan gagal — dalam hal
// itu teks asli dipakai apa adanya.
func (a *App) autoReviewReply(ctx context.Context, member seat, text string) (string, bool) {
	a.mu.Lock()
	if !a.data.Settings.autoReviewOn() {
		a.mu.Unlock()
		return "", false
	}
	reviewerCode := strings.TrimSpace(a.data.Settings.ReviewerSeat)
	if reviewerCode == "" {
		reviewerCode = a.integratorSeat()
	}
	if reviewerCode == member.Code {
		a.mu.Unlock()
		return "", false
	}
	reviewer := a.data.findSeat(reviewerCode)
	if reviewer == nil || reviewer.Provider != "gateway" {
		a.mu.Unlock()
		return "", false
	}
	reviewerCopy := *reviewer
	cfg := a.data.Settings
	if model := reviewerCopy.modelForTask("chat"); model != "" {
		var resolved string
		cfg, resolved = a.data.resolveModelProviderLocked(cfg, model)
		reviewerCopy.Model = &resolved
	}
	dataCopy := a.data
	a.mu.Unlock()
	history := []gatewayMessage{
		{"system", a.systemPrompt(reviewerCopy, dataCopy)},
		{"user", autoReviewInstruction + text},
	}
	reply, err := a.chat(ctx, cfg, reviewerCopy, history)
	if err != nil {
		return "", false
	}
	reply = strings.TrimSpace(reply)
	if reply == "" {
		return "", false
	}
	return reply, true
}
