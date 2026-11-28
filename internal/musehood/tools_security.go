package musehood

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// --- Tool security_scan (fitur omp 7) ---
//
// Memindai proyek per tipe: Go -> govulncheck (bila terpasang; butuh
// jaringan untuk basis data kerentanan sehingga dijalankan di luar
// sandbox dengan CGO_ENABLED=0 — hanya kompilasi+analisis, tidak
// menjalankan kode proyek) atau `go vet` di sandbox bila tidak ada;
// Node -> `npm audit --json` (juga butuh jaringan registry); semua
// proyek -> pencarian pola secret (regex). Pemindai yang hilang
// dilaporkan dengan jujur, tidak dikarang hasilnya.

const securityScanTimeout = 120 * time.Second

var secretPatterns = []struct {
	name string
	re   *regexp.Regexp
}{
	{"kunci privat PEM", regexp.MustCompile(`-----BEGIN (RSA |EC |OPENSSH )?PRIVATE KEY-----`)},
	{"kunci AWS", regexp.MustCompile(`AKIA[0-9A-Z]{16}`)},
	{"token GitHub", regexp.MustCompile(`gh[pousr]_[A-Za-z0-9_]{30,}`)},
	{"token GitHub fine-grained", regexp.MustCompile(`github_pat_[A-Za-z0-9_]{30,}`)},
	{"kunci API bergaya sk-", regexp.MustCompile(`sk-[A-Za-z0-9_-]{20,}`)},
	{"token Slack", regexp.MustCompile(`xox[baprs]-[A-Za-z0-9-]{10,}`)},
	{"penugasan password", regexp.MustCompile(`(?i)(password|passwd|secret|api[_-]?key)\s*[:=]\s*["'][^"'\s]{8,}["']`)},
}

func (a *App) toolSecurityScan(ctx context.Context, arg string) string {
	project, _ := parseProjectArg(arg)
	dir, err := a.ensureProject(project)
	if err != nil {
		return "Error: " + err.Error()
	}
	var sections []string

	if projectHasFile(dir, "go.mod") {
		if govulncheck := resolveLocalBinary("govulncheck"); govulncheck != "" {
			sections = append(sections, runGovulncheck(ctx, govulncheck, dir))
		} else {
			sections = append(sections, "govulncheck: tidak terpasang (pasang: go install golang.org/x/vuln/cmd/govulncheck@latest). Fallback go vet:")
			out, vetErr := a.execInProject(ctx, dir, []string{"go", "vet", "./..."}, 120, nil)
			if vetErr != nil {
				sections = append(sections, "go vet menemukan masalah:\n"+tailLines(out, 40))
			} else {
				sections = append(sections, "go vet: bersih.")
			}
		}
	}

	if projectHasFile(dir, "package.json") {
		sections = append(sections, runNpmAudit(ctx, dir))
	}

	// Pola secret untuk semua proyek.
	findings := scanSecretPatterns(dir)
	if len(findings) == 0 {
		sections = append(sections, "Pola secret: tidak ditemukan.")
	} else {
		sections = append(sections, "Pola secret ditemukan:\n"+strings.Join(findings, "\n"))
	}

	if len(sections) == 1 && strings.HasPrefix(sections[0], "Pola secret") {
		sections = append([]string{"Tidak ada pemindai per-bahasa yang berlaku (bukan proyek Go/Node)."}, sections...)
	}
	return truncateToolResult(strings.Join(sections, "\n\n"))
}

func runGovulncheck(ctx context.Context, bin, dir string) string {
	runCtx, cancel := context.WithTimeout(ctx, securityScanTimeout)
	defer cancel()
	cmd := exec.CommandContext(runCtx, bin, "./...")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOFLAGS=-mod=mod", "GOTOOLCHAIN=local")
	out, err := cmd.CombinedOutput()
	text := strings.TrimSpace(string(out))
	if err != nil {
		if text == "" {
			text = err.Error()
		}
		// govulncheck exit 3 = kerentanan ditemukan (bukan kegagalan alat).
		if strings.Contains(text, "Vulnerability #") || strings.Contains(text, "Your code is affected") {
			return "govulncheck menemukan kerentanan:\n" + tailLines(text, 60)
		}
		return "govulncheck gagal berjalan:\n" + tailLines(text, 20)
	}
	if strings.Contains(text, "No vulnerabilities found") || text == "" {
		return "govulncheck: tidak ada kerentanan yang memengaruhi kode."
	}
	return "govulncheck:\n" + tailLines(text, 60)
}

func runNpmAudit(ctx context.Context, dir string) string {
	if !projectHasFile(dir, "package-lock.json") && !projectHasFile(dir, "npm-shrinkwrap.json") {
		return "npm audit: dilewati — tidak ada package-lock.json (npm audit membutuhkan lockfile)."
	}
	npm := resolveLocalBinary("npm")
	if npm == "" {
		return "npm audit: npm tidak ditemukan di server."
	}
	runCtx, cancel := context.WithTimeout(ctx, securityScanTimeout)
	defer cancel()
	cmd := exec.CommandContext(runCtx, npm, "audit", "--json")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "npm_config_cache=/tmp/.npm-audit-cache")
	out, _ := cmd.CombinedOutput()
	var report struct {
		Metadata struct {
			Vulnerabilities struct {
				Info     int `json:"info"`
				Low      int `json:"low"`
				Moderate int `json:"moderate"`
				High     int `json:"high"`
				Critical int `json:"critical"`
				Total    int `json:"total"`
			} `json:"vulnerabilities"`
		} `json:"metadata"`
	}
	if json.Unmarshal(out, &report) != nil {
		return "npm audit gagal (jaringan registry?):\n" + tailLines(strings.TrimSpace(string(out)), 10)
	}
	v := report.Metadata.Vulnerabilities
	if v.Total == 0 {
		return "npm audit: tidak ada kerentanan."
	}
	return fmt.Sprintf("npm audit: %d kerentanan (critical %d, high %d, moderate %d, low %d, info %d). Jalankan `npm audit` untuk detail.", v.Total, v.Critical, v.High, v.Moderate, v.Low, v.Info)
}

// scanSecretPatterns mencari pola secret di berkas teks proyek.
func scanSecretPatterns(dir string) []string {
	var findings []string
	walkProject(dir, 2000, func(rel, full string, info os.FileInfo) bool {
		if len(findings) >= 50 {
			return false
		}
		if info.Size() > 512*1024 {
			return true
		}
		ext := strings.ToLower(filepath.Ext(rel))
		switch ext {
		case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".zip", ".gz", ".pdf", ".ico", ".woff", ".woff2", ".mp3", ".mp4", ".bin", ".exe", ".dll", ".so", ".dylib", ".class", ".jar":
			return true
		}
		raw, err := os.ReadFile(full)
		if err != nil {
			return true
		}
		text := string(raw)
		for _, pattern := range secretPatterns {
			if loc := pattern.re.FindStringIndex(text); loc != nil {
				line := 1 + strings.Count(text[:loc[0]], "\n")
				findings = append(findings, fmt.Sprintf("- %s:%d [%s] (nilai disembunyikan)", rel, line, pattern.name))
			}
		}
		return true
	})
	return findings
}
