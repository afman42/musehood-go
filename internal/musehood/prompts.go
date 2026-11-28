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

func (a *App) systemPrompt(s seat, d data) string {
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
	if d.Settings.skillsOn() {
		prompt += a.skillsPrompt(s)
	}
	// Preset peran kursi (fitur 22): prompt perilaku tambahan digabung
	// setelah Memory/Skills.
	if preset := findRolePreset(s.RolePreset); preset != nil {
		prompt += " Preset peran: " + preset.Prompt
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
	// Kompaksi konteks: bila ruang punya ringkasan dan riwayatnya panjang,
	// kirim ringkasan + 40 pesan terakhir saja.
	limit := 80
	var roomSummary string
	if j.RoomID != "" {
		if r := d.findRoom(j.RoomID); r != nil && r.Summary != "" {
			count := 0
			for _, m := range d.Messages {
				if m.RoomID == j.RoomID && m.Status != "typing" && m.Status != "waiting" {
					count++
				}
			}
			if count > 40 {
				limit = 40
				roomSummary = r.Summary
			}
		}
	}
	// Konteks hanya memakai pesan branch pekerjaan ini (fitur 14).
	branch := j.BranchID
	if branch == "" {
		if r := d.findRoom(j.RoomID); r != nil {
			branch = r.activeBranch()
		} else {
			branch = "main"
		}
	}
	history := []contextMessage{}
	for i := len(d.Messages) - 1; i >= 0 && len(history) < limit; i-- {
		m := d.Messages[i]
		if j.RoomID != "" && m.RoomID != j.RoomID {
			continue
		}
		if m.branchID() != branch {
			continue
		}
		if m.Status == "typing" || m.Status == "waiting" {
			continue
		}
		entry := contextMessage{Text: m.Text, Error: m.Error, ImageURL: m.ImageURL}
		if m.AuthorType == "owner" {
			// Pesan peserta ruang (fitur 21) memakai nama pengirimnya.
			entry.Name = d.Owner.Name
			if m.Author != "" {
				entry.Name = m.Author
			}
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
	if roomSummary != "" {
		history = append([]contextMessage{{Name: "Ringkasan", Role: "Ringkasan diskusi sebelumnya", Text: roomSummary}}, history...)
	}
	return chatContext{d.Group.Name, d.Owner.Name, d.Task, history, j.Prompt}
}

type gatewayMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

func (a *App) gatewayMessages(s seat, d data, j *job) []gatewayMessage {
	system := a.systemPrompt(s, d)
	// Konteks dokumen terlampir (fitur 26): chunk paling relevan dengan
	// pesan pemilik terakhir disisipkan ke system prompt.
	if j.RoomID != "" {
		if chunks := relevantDocumentChunks(d, j.RoomID, j.Prompt, 3); len(chunks) > 0 {
			system += "\n\nDokumen terlampir (kutipan yang relevan dengan permintaan; gunakan sebagai sumber bila membantu):\n" + strings.Join(chunks, "\n---\n")
		}
	}
	if d.Settings.toolsOn() {
		system += a.buildToolInstructions(d.Settings.codeToolOn(), s, d.Settings.MCPServers, d.Settings.onDemandOn())
	}
	// Goal/plan/vibe mode ruang (fitur omp 12): objektif persisten dan
	// mode kerja disuntikkan ke system prompt.
	if j.RoomID != "" {
		if r := d.findRoom(j.RoomID); r != nil {
			system += roomModePrompt(r, s, d.Settings)
		}
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
	// Nama peminta: peserta ruang (fitur 21) bila pesan pemilik pekerjaan
	// ini dikirim atas nama peserta, selain itu pemilik utama.
	requester := d.Owner.Name
	for _, m := range d.Messages {
		if m.JobID == j.ID && m.AuthorType == "owner" && m.Author != "" {
			requester = m.Author
			break
		}
	}
	return append(history, gatewayMessage{"user", fmt.Sprintf("Permintaan %s: %s\nKontribusikan bagian sesuai peran %s berdasarkan konteks di atas.", requester, j.Prompt, s.Role)})
}
