package musehood

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Skill adalah instruksi tambahan per kursi dalam format SKILL.md yang sama
// dengan skill pi: frontmatter YAML (name, description) + badan Markdown.
// Skill dibaca dari <dataDir>/skills/: subfolder berisi SKILL.md atau file
// .md langsung. Kursi mengaktifkan skill lewat field Skills (daftar nama);
// badan skill disisipkan ke system prompt kursi tersebut.

const maxSkillBodyChars = 6000
const maxSkillsPromptChars = 12000

type skillDef struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	// Source adalah asal skill: "lokal" (<dataDir>/skills), "skills.sh"
	// (~/.agents/skills, direktori universal skills.sh), atau "claude"
	// (~/.claude/skills). Kosong untuk skill yang dibuat di tes.
	Source string `json:"source,omitempty"`
	Body   string `json:"-"`
}

func (a *App) skillsDir() string {
	return filepath.Join(a.dir, "skills")
}

type skillDirSource struct {
	dir    string
	source string
}

// skillDirs adalah direktori skill yang dipindai, urut prioritas:
// folder lokal aplikasi dulu (menang bila nama sama), lalu direktori
// standar ekosistem skills.sh (~/.agents/skills adalah target universal
// `npx skills add -g`; ~/.claude/skills biasanya symlink ke sana).
func (a *App) skillDirs() []skillDirSource {
	dirs := []skillDirSource{{a.skillsDir(), "lokal"}}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		dirs = append(dirs,
			skillDirSource{filepath.Join(home, ".agents", "skills"), "skills.sh"},
			skillDirSource{filepath.Join(home, ".claude", "skills"), "claude"},
		)
	}
	return dirs
}

// parseSkill membaca frontmatter sederhana (name:/description:) dan badan.
func parseSkill(fallbackName string, content string) skillDef {
	skill := skillDef{Name: fallbackName}
	body := content
	if strings.HasPrefix(content, "---") {
		if end := strings.Index(content[3:], "\n---"); end >= 0 {
			front := content[3 : 3+end]
			body = content[3+end+len("\n---"):]
			for _, line := range strings.Split(front, "\n") {
				key, value, ok := strings.Cut(line, ":")
				if !ok {
					continue
				}
				value = strings.Trim(strings.TrimSpace(value), `"'`)
				switch strings.TrimSpace(key) {
				case "name":
					if value != "" {
						skill.Name = value
					}
				case "description":
					skill.Description = value
				}
			}
		}
	}
	body = strings.TrimSpace(body)
	if len(body) > maxSkillBodyChars {
		body = body[:maxSkillBodyChars] + "\n…(dipotong)"
	}
	skill.Body = body
	return skill
}

// skillsCacheTTL membatasi pemindaian ulang direktori skill: dengan
// ratusan skill terpasang via skills.sh, membaca semua SKILL.md pada
// setiap pesan terlalu boros.
const skillsCacheTTL = 30 * time.Second

func (a *App) loadSkills() []skillDef {
	a.skillsMu.Lock()
	defer a.skillsMu.Unlock()
	if a.skillsCache != nil && time.Since(a.skillsCacheAt) < skillsCacheTTL {
		return a.skillsCache
	}
	skills := a.scanSkills()
	a.skillsCache = skills
	a.skillsCacheAt = time.Now()
	return skills
}

func (a *App) scanSkills() []skillDef {
	var skills []skillDef
	seen := map[string]bool{}
	for _, source := range a.skillDirs() {
		entries, err := os.ReadDir(source.dir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			// Coba <entri>/SKILL.md dulu (folder skill standar skills.sh,
			// termasuk symlink antar-direktori agen); bila gagal dan
			// entrinya file .md, baca file itu langsung.
			path := filepath.Join(source.dir, entry.Name(), "SKILL.md")
			fallback := entry.Name()
			data, err := os.ReadFile(path)
			if err != nil {
				if !strings.HasSuffix(strings.ToLower(entry.Name()), ".md") {
					continue
				}
				path = filepath.Join(source.dir, entry.Name())
				fallback = strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name()))
				data, err = os.ReadFile(path)
				if err != nil {
					continue
				}
			}
			skill := parseSkill(fallback, string(data))
			if skill.Name == "" || skill.Body == "" || seen[strings.ToLower(skill.Name)] {
				continue
			}
			seen[strings.ToLower(skill.Name)] = true
			skill.Source = source.source
			skills = append(skills, skill)
		}
	}
	sort.Slice(skills, func(i, j int) bool { return skills[i].Name < skills[j].Name })
	return skills
}

// skillsPrompt menyusun teks skill aktif kursi untuk system prompt.
func (a *App) skillsPrompt(s seat) string {
	if len(s.Skills) == 0 {
		return ""
	}
	byName := map[string]skillDef{}
	for _, skill := range a.loadSkills() {
		byName[strings.ToLower(skill.Name)] = skill
	}
	var builder strings.Builder
	for _, name := range s.Skills {
		skill, ok := byName[strings.ToLower(strings.TrimSpace(name))]
		if !ok {
			continue
		}
		builder.WriteString("\n\n## Skill: " + skill.Name + "\n" + skill.Body)
		if builder.Len() > maxSkillsPromptChars {
			break
		}
	}
	text := builder.String()
	if len(text) > maxSkillsPromptChars {
		text = text[:maxSkillsPromptChars] + "\n…(dipotong)"
	}
	if text == "" {
		return ""
	}
	return "\n\nSkill aktif yang harus kamu ikuti:" + text
}
