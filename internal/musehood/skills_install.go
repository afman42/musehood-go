package musehood

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// Pemasangan skill dari URL (fitur 23). Dua jalur:
//   - URL https ke file SKILL.md langsung (maks 200 KB, penjaga SSRF).
//   - URL repo git https: clone dangkal ke folder sementara, cari
//     SKILL.md di root atau satu subfolder, salin ke folder skill lokal.
//
// Kedua fungsi di bawah adalah variabel agar tes bisa mengarahkannya ke
// server lokal; jalur produksi selalu https + penjaga SSRF.

var skillFetchURL = func(ctx context.Context, rawURL string) ([]byte, error) {
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme != "https" {
		return nil, problem(400, "URL skill harus berupa URL HTTPS yang valid.")
	}
	if err := checkPublicURL(ctx, rawURL); err != nil {
		return nil, problem(400, "URL skill ditolak penjaga SSRF.")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "musehood-go/1.0")
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, problem(502, "Server skill menjawab HTTP "+resp.Status+".")
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 200*1024+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > 200*1024 {
		return nil, problem(400, "File SKILL.md melebihi batas 200 KB.")
	}
	return raw, nil
}

var skillGitClone = func(ctx context.Context, rawURL, dir string) error {
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme != "https" {
		return problem(400, "URL repo skill harus berupa URL HTTPS yang valid.")
	}
	ctx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "clone", "--depth", "1", rawURL, dir)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	if err := cmd.Run(); err != nil {
		return problem(502, "git clone repo skill gagal.")
	}
	return nil
}

var skillNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)

// installSkill memasang skill dari URL dan mengembalikan namanya.
func (a *App) installSkill(ctx context.Context, rawURL string, overwrite bool) (string, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return "", problem(400, "URL skill wajib diisi.")
	}
	u, err := url.Parse(rawURL)
	if err != nil || u.Hostname() == "" {
		return "", problem(400, "URL skill tidak valid.")
	}

	var content []byte
	fallback := ""
	if strings.HasSuffix(strings.ToLower(u.Path), ".md") {
		raw, err := skillFetchURL(ctx, rawURL)
		if err != nil {
			return "", err
		}
		content = raw
		fallback = strings.TrimSuffix(path.Base(u.Path), path.Ext(u.Path))
	} else {
		tmp, err := os.MkdirTemp(a.dir, "skill-clone-")
		if err != nil {
			return "", err
		}
		defer os.RemoveAll(tmp)
		if err := skillGitClone(ctx, rawURL, tmp); err != nil {
			return "", err
		}
		found := findSkillFile(tmp)
		if found == "" {
			return "", problem(400, "SKILL.md tidak ditemukan di repo tersebut.")
		}
		raw, err := os.ReadFile(found)
		if err != nil {
			return "", err
		}
		if len(raw) > 200*1024 {
			return "", problem(400, "File SKILL.md melebihi batas 200 KB.")
		}
		content = raw
		fallback = strings.TrimSuffix(path.Base(u.Path), ".git")
	}

	def := parseSkill(fallback, string(content))
	name := strings.ToLower(strings.TrimSpace(def.Name))
	if !skillNamePattern.MatchString(name) {
		return "", problem(400, "Nama skill tidak valid; gunakan huruf kecil, angka, tanda hubung, atau garis bawah.")
	}

	skillsDir := filepath.Join(a.dir, "skills")
	target := filepath.Join(skillsDir, name)
	_, dirErr := os.Stat(target)
	_, fileErr := os.Stat(filepath.Join(skillsDir, name+".md"))
	if (dirErr == nil || fileErr == nil) && !overwrite {
		return "", problem(409, "Skill "+name+" sudah ada. Aktifkan timpa untuk menggantinya.")
	}
	if err := os.MkdirAll(target, 0o700); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(target, "SKILL.md"), content, 0o600); err != nil {
		return "", err
	}
	a.skillsMu.Lock()
	a.skillsCacheAt = time.Time{}
	a.skillsMu.Unlock()
	return name, nil
}

// findSkillFile mencari SKILL.md di root folder clone atau satu tingkat
// subfolder.
func findSkillFile(root string) string {
	if candidate := filepath.Join(root, "SKILL.md"); fileExists(candidate) {
		return candidate
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return ""
	}
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		if candidate := filepath.Join(root, entry.Name(), "SKILL.md"); fileExists(candidate) {
			return candidate
		}
	}
	return ""
}

func fileExists(name string) bool {
	info, err := os.Stat(name)
	return err == nil && !info.IsDir()
}
