package musehood

import (
	"fmt"
	"strings"
)

var review = map[string]string{
	"Longgar": "Cek ringan saja: cukup temukan masalah yang sangat jelas dan berisiko besar.",
	"Santai":  "Berikan masukan singkat, ramah, dan satu atau dua saran utama.",
	"Sedang":  "Lakukan review seimbang: jelaskan kekuatan, kelemahan, risiko, dan saran yang proporsional.",
	"Ketat":   "Lakukan kritik detail. Uji asumsi, ketepatan, dan kelengkapan. Akhiri dengan daftar perbaikan wajib yang konkret.",
}
var duties = map[string]string{
	"MUSE": "Koordinasikan grup sebagai admin dan integrator. Rangkum kontribusi tiap kursi, selesaikan pertentangan, bedakan hasil nyata dari rencana dan kegagalan. Jangan mengklaim pekerjaan yang belum dilakukan.",
	"M10":  "Susun rencana terstruktur dengan tujuan, urutan langkah, kebutuhan, risiko, dan kriteria selesai. Rencana harus bisa dijalankan Pelaksana.",
	"M01":  "Eksekusi rencana secara konkret: berikan hasil, langkah operasional, kode, atau dokumen yang diminta. Jelaskan batas kemampuan dan jangan mengklaim tindakan eksternal yang tidak dilakukan.",
	"M02":  "Tinjau hasil kursi sebelumnya untuk ketepatan, risiko, dan kesesuaian tujuan.",
	"M03":  "Deskripsikan visual yang akan dibuat: komposisi, gaya, warna, subjek, dan detail penting. Jika diminta gambar, gunakan jalur pembuatan gambar aplikasi; jangan mengklaim ada gambar tanpa hasil nyata.",
	"M04":  "Berbincang dengan hangat, beri masukan praktis, pertimbangkan dampak dan pilihan alternatif dengan bahasa sederhana.",
	"M05":  "Analisis menggunakan data dan angka. Cantumkan asumsi, metode perhitungan, ketidakpastian, dan sumber bila tersedia. Jangan mengarang data atau statistik.",
}

// defaultPromptFor adalah instruksi peran bawaan tiap kursi. Kursi custom
// tidak punya entri duties, jadi defaultnya mengikuti teks perannya. Kursi
// bawaan yang perannya diganti juga mengikuti teks peran barunya.
func defaultPromptFor(s seat) string {
	if text, ok := duties[s.Code]; ok {
		for _, def := range defaultSeats {
			if def.code == s.Code && strings.TrimSpace(s.Role) == def.role {
				return text
			}
		}
	}
	role := strings.TrimSpace(s.Role)
	if role == "" {
		return "Laksanakan permintaan sesuai peran kursi ini."
	}
	return "Laksanakan peran " + role + "."
}

func systemPrompt(s seat, d data) string {
	rolePrompt := strings.TrimSpace(s.Prompt)
	if rolePrompt == "" {
		rolePrompt = defaultPromptFor(s)
	}
	prompt := fmt.Sprintf("Nama kursi: %s (%s). Peran: %s. Grup: %s. Nama pemilik: %s. Selalu sebut pemilik dengan nama %s. Gunakan Bahasa Indonesia yang jelas dan ringkas. %s", s.Name, s.Code, s.Role, d.Group.Name, d.Owner.Name, d.Owner.Name, rolePrompt)
	if s.Code == "M02" {
		prompt += " Level Review " + d.Settings.ReviewLevel + ": " + review[d.Settings.ReviewLevel]
	}
	if memory := strings.TrimSpace(s.Memory); memory != "" {
		prompt += " Memori kursi (catatan persisten yang harus kamu ingat dan pakai): " + memory
	}
	return prompt
}

type contextMessage struct {
	Name     string `json:"name"`
	Role     string `json:"role"`
	Text     string `json:"text"`
	Error    string `json:"error,omitempty"`
	ImageURL string `json:"imageUrl,omitempty"`
}
type chatContext struct {
	Group    string           `json:"group"`
	Owner    string           `json:"owner"`
	TaskInfo *activeTask      `json:"activeTask"`
	Messages []contextMessage `json:"messages"`
	Task     string           `json:"task"`
}

func contextFor(d data, j *job) chatContext {
	history := []contextMessage{}
	for i := len(d.Messages) - 1; i >= 0 && len(history) < 80; i-- {
		m := d.Messages[i]
		if j.RoomID != "" && m.RoomID != j.RoomID {
			continue
		}
		if m.Status == "typing" || m.Status == "waiting" {
			continue
		}
		entry := contextMessage{Text: m.Text, Error: m.Error, ImageURL: m.ImageURL}
		if m.AuthorType == "owner" {
			entry.Name = d.Owner.Name
			entry.Role = "Pemilik"
		} else if s := d.findSeat(m.Seat); s != nil {
			entry.Name = s.Name
			entry.Role = s.Role
		}
		history = append(history, entry)
	}
	for i, k := 0, len(history)-1; i < k; i, k = i+1, k-1 {
		history[i], history[k] = history[k], history[i]
	}
	return chatContext{d.Group.Name, d.Owner.Name, d.Task, history, j.Prompt}
}

type gatewayMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

func gatewayMessages(s seat, d data, j *job) []gatewayMessage {
	system := systemPrompt(s, d)
	if d.Settings.toolsOn() {
		system += buildToolInstructions(d.Settings.codeToolOn())
	}
	history := []gatewayMessage{{"system", system}}
	for _, m := range contextFor(d, j).Messages {
		role := "assistant"
		if m.Role == "Pemilik" {
			role = "user"
		}
		var text strings.Builder
		fmt.Fprintf(&text, "%s (%s): %s", m.Name, m.Role, m.Text)
		if m.ImageURL != "" {
			text.WriteString("\nGambar: " + m.ImageURL)
		}
		if m.Error != "" {
			text.WriteString("\nKegagalan provider: " + m.Error)
		}
		history = append(history, gatewayMessage{role, text.String()})
	}
	return append(history, gatewayMessage{"user", fmt.Sprintf("Permintaan %s: %s\nKontribusikan bagian sesuai peran %s berdasarkan konteks di atas.", d.Owner.Name, j.Prompt, s.Role)})
}
