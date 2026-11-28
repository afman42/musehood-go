package musehood

import (
	"cmp"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// --- Memori durable + learn (fitur omp 10) ---
//
// data.Memories adalah store bersama semua kursi (id, teks, tag,
// waktu, kursi sumber; maks 500, FIFO). Tool: memory_recall (cari kata
// kunci), memory_retain (simpan), memory_reflect (ambil yang terkait
// untuk disintesis kursi), memory_edit (ubah/hapus), learn (simpan
// pelajaran; opsional jadikan skill di <dataDir>/skills/<nama>/SKILL.md),
// manage_skill (daftar/buat/ubah/pensiunkan skill lokal). Semua tool
// memori digerbang settings.MemoryEnabled.

const maxDurableMemories = 500

func memoryDisabledError() string {
	return "Error: memori durable dimatikan di Pengaturan (MemoryEnabled)."
}

// searchMemories mencari memori yang memuat kata kunci (teks/tag).
func searchMemories(memories []durableMemory, query string, limit int) []durableMemory {
	words := strings.Fields(strings.ToLower(query))
	var hits []durableMemory
	for _, m := range memories {
		haystack := strings.ToLower(m.Text + " " + strings.Join(m.Tags, " "))
		matched := len(words) == 0
		for _, word := range words {
			if strings.Contains(haystack, word) {
				matched = true
				break
			}
		}
		if matched {
			hits = append(hits, m)
		}
	}
	// Terbaru dulu.
	slices.SortFunc(hits, func(a, b durableMemory) int { return cmp.Compare(b.CreatedAt, a.CreatedAt) })
	if len(hits) > limit {
		hits = hits[:limit]
	}
	return hits
}

func formatMemories(memories []durableMemory) string {
	var out strings.Builder
	for _, m := range memories {
		tags := ""
		if len(m.Tags) > 0 {
			tags = " [" + strings.Join(m.Tags, ", ") + "]"
		}
		out.WriteString(fmt.Sprintf("- (%s, %s)%s %s\n", m.ID, m.CreatedAt, tags, m.Text))
	}
	return strings.TrimSpace(out.String())
}

func (a *App) toolMemoryRecall(cfg settings, arg string) string {
	if !cfg.memoryOn() {
		return memoryDisabledError()
	}
	query := strings.TrimSpace(arg)
	if query == "" {
		return "Error: format memory_recall — kata kunci."
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	hits := searchMemories(a.data.Memories, query, 10)
	if len(hits) == 0 {
		return "Tidak ada memori yang cocok."
	}
	return truncateToolResult(formatMemories(hits))
}

func (a *App) toolMemoryRetain(cfg settings, s seat, arg string) string {
	if !cfg.memoryOn() {
		return memoryDisabledError()
	}
	text, tagsLine, _ := strings.Cut(arg, "\n")
	text = strings.TrimSpace(text)
	if text == "" {
		return "Error: format memory_retain — baris pertama teks, baris kedua (opsional) tag dipisah koma."
	}
	if len(text) > 2000 {
		text = text[:2000]
	}
	var tags []string
	for _, tag := range strings.Split(tagsLine, ",") {
		if tag = strings.TrimSpace(tag); tag != "" {
			tags = append(tags, tag)
		}
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	mem := durableMemory{ID: uuid(), Text: text, Tags: tags, SeatCode: s.Code, CreatedAt: now()}
	a.data.Memories = append(a.data.Memories, mem)
	if len(a.data.Memories) > maxDurableMemories {
		a.data.Memories = a.data.Memories[len(a.data.Memories)-maxDurableMemories:]
	}
	if err := a.saveLocked(); err != nil {
		return "Error: gagal menyimpan memori."
	}
	return "Memori tersimpan (id " + mem.ID + ")."
}

func (a *App) toolMemoryReflect(cfg settings, arg string) string {
	if !cfg.memoryOn() {
		return memoryDisabledError()
	}
	topic := strings.TrimSpace(arg)
	if topic == "" {
		return "Error: format memory_reflect — topik."
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	hits := searchMemories(a.data.Memories, topic, 15)
	if len(hits) == 0 {
		return "Belum ada memori terkait topik itu."
	}
	return truncateToolResult("Memori terkait untuk kamu sintesis:\n" + formatMemories(hits))
}

func (a *App) toolMemoryEdit(cfg settings, arg string) string {
	if !cfg.memoryOn() {
		return memoryDisabledError()
	}
	command, rest, _ := strings.Cut(strings.TrimSpace(arg), " ")
	rest = strings.TrimSpace(rest)
	a.mu.Lock()
	defer a.mu.Unlock()
	switch command {
	case "delete":
		for i := range a.data.Memories {
			if a.data.Memories[i].ID == rest {
				a.data.Memories = append(a.data.Memories[:i], a.data.Memories[i+1:]...)
				if err := a.saveLocked(); err != nil {
					return "Error: gagal menyimpan."
				}
				return "Memori dihapus."
			}
		}
		return fmt.Sprintf("Error: memori %q tidak ditemukan.", rest)
	case "update":
		id, text, found := strings.Cut(rest, "\n")
		id = strings.TrimSpace(id)
		text = strings.TrimSpace(text)
		if !found || text == "" {
			return "Error: format memory_edit update <id> lalu baris baru teks baru."
		}
		for i := range a.data.Memories {
			if a.data.Memories[i].ID == id {
				a.data.Memories[i].Text = text
				if err := a.saveLocked(); err != nil {
					return "Error: gagal menyimpan."
				}
				return "Memori diperbarui."
			}
		}
		return fmt.Sprintf("Error: memori %q tidak ditemukan.", id)
	default:
		return "Error: format memory_edit — \"delete <id>\" atau \"update <id>\\n<teks baru>\"."
	}
}

// writeManagedSkill menulis SKILL.md ke <dataDir>/skills/<nama>/.
func (a *App) writeManagedSkill(name, description, body string) error {
	if !customToolNamePattern.MatchString(name) {
		return fmt.Errorf("nama skill harus huruf kecil/angka/underscore")
	}
	dir := filepath.Join(a.skillsDir(), name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	content := "---\nname: " + name + "\ndescription: " + description + "\n---\n\n" + body + "\n"
	return os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644)
}

func (a *App) toolLearn(cfg settings, s seat, arg string) string {
	if !cfg.memoryOn() {
		return memoryDisabledError()
	}
	first, body, _ := strings.Cut(arg, "\n")
	first = strings.TrimSpace(first)
	body = strings.TrimSpace(body)
	if first == "" || body == "" {
		return "Error: format learn — baris pertama judul pelajaran (atau \"skill:<nama>\"), baris berikutnya isi pelajaran."
	}
	skillName := ""
	title := first
	if strings.HasPrefix(first, "skill:") {
		skillName = strings.TrimSpace(strings.TrimPrefix(first, "skill:"))
		title = skillName
		if !customToolNamePattern.MatchString(skillName) {
			return "Error: nama skill harus huruf kecil/angka/underscore (mis. skill:retry_policy)."
		}
	}
	// Simpan sebagai memori durable bertag "learned".
	a.mu.Lock()
	mem := durableMemory{ID: uuid(), Text: title + ": " + body, Tags: []string{"learned"}, SeatCode: s.Code, CreatedAt: now()}
	if len(mem.Text) > 2000 {
		mem.Text = mem.Text[:2000]
	}
	a.data.Memories = append(a.data.Memories, mem)
	if len(a.data.Memories) > maxDurableMemories {
		a.data.Memories = a.data.Memories[len(a.data.Memories)-maxDurableMemories:]
	}
	saveErr := a.saveLocked()
	a.mu.Unlock()
	if saveErr != nil {
		return "Error: gagal menyimpan pelajaran."
	}
	if skillName != "" {
		if err := a.writeManagedSkill(skillName, "Pelajaran: "+title, body); err != nil {
			return "Pelajaran tersimpan sebagai memori, tetapi gagal membuat skill: " + err.Error()
		}
		return fmt.Sprintf("Pelajaran tersimpan dan skill %q dibuat (id memori %s).", skillName, mem.ID)
	}
	return "Pelajaran tersimpan (id memori " + mem.ID + ")."
}

func (a *App) toolManageSkill(arg string) string {
	command, rest, _ := strings.Cut(strings.TrimSpace(arg), " ")
	rest = strings.TrimSpace(rest)
	switch command {
	case "", "list":
		skills := a.loadSkills()
		var local []skillDef
		other := 0
		for _, sk := range skills {
			if sk.Source == "lokal" {
				local = append(local, sk)
			} else {
				other++
			}
		}
		if len(local) == 0 {
			return fmt.Sprintf("Belum ada skill lokal terkelola (%d skill dari sumber lain terpasang).", other)
		}
		var out strings.Builder
		out.WriteString("Skill lokal terkelola:\n")
		for _, sk := range local {
			out.WriteString(fmt.Sprintf("- %s: %s\n", sk.Name, sk.Description))
		}
		out.WriteString(fmt.Sprintf("(%d skill lain dari skills.sh/claude tidak dikelola di sini.)", other))
		return truncateToolResult(strings.TrimSpace(out.String()))
	case "create":
		lines := strings.Split(rest, "\n")
		if len(lines) < 3 {
			return "Error: format manage_skill create <nama> lalu baris baru deskripsi, baris berikutnya isi skill."
		}
		name := strings.TrimSpace(lines[0])
		description := strings.TrimSpace(lines[1])
		body := strings.TrimSpace(strings.Join(lines[2:], "\n"))
		if err := a.writeManagedSkill(name, description, body); err != nil {
			return "Error: " + err.Error()
		}
		return fmt.Sprintf("Skill %q dibuat.", name)
	case "update":
		lines := strings.Split(rest, "\n")
		if len(lines) < 2 {
			return "Error: format manage_skill update <nama> lalu baris baru isi skill baru."
		}
		name := strings.TrimSpace(lines[0])
		body := strings.TrimSpace(strings.Join(lines[1:], "\n"))
		path := filepath.Join(a.skillsDir(), name, "SKILL.md")
		existing, err := os.ReadFile(path)
		if err != nil {
			return fmt.Sprintf("Error: skill lokal %q tidak ditemukan.", name)
		}
		parsed := parseSkill(name, string(existing))
		if err := a.writeManagedSkill(name, parsed.Description, body); err != nil {
			return "Error: " + err.Error()
		}
		return fmt.Sprintf("Skill %q diperbarui.", name)
	case "retire":
		name := strings.TrimSpace(rest)
		if !customToolNamePattern.MatchString(name) {
			return "Error: nama skill tidak valid."
		}
		dir := filepath.Join(a.skillsDir(), name)
		if _, err := os.Stat(dir); err != nil {
			return fmt.Sprintf("Error: skill lokal %q tidak ditemukan.", name)
		}
		if err := os.Rename(dir, dir+".retired"); err != nil {
			return "Error: gagal memensiunkan skill: " + err.Error()
		}
		return fmt.Sprintf("Skill %q dipensiunkan (folder diganti nama .retired).", name)
	default:
		return "Error: format manage_skill — list, create <nama>, update <nama>, atau retire <nama>."
	}
}
