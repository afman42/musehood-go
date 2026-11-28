package musehood

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// --- Tool ast-grep (fitur omp 1): cari pola sintaks + codemod struktural ---
//
// ast_grep menjalankan `ast-grep run -p <pola>` di folder proyek;
// ast_edit memakai `-r <pola-baru>` untuk pratinjau diff, dan hanya
// menerapkan (flag -U) bila argumen memuat "apply" DAN pemilik
// menyetujui (approval kind "edit"); snapshot proyek dibuat dulu.
// Bila binary ast-grep tidak ditemukan, tool mengembalikan pesan
// instalasi bahasa Indonesia (degradasi anggun).

const astGrepTimeout = 60 * time.Second

const astGrepMissingError = "Error: ast-grep tidak ditemukan di server. Pasang dengan: npm install --prefix ~/.local @ast-grep/cli (atau unduh binary ast-grep ke ~/.local/bin), lalu coba lagi."

// findAstGrep mencari binary ast-grep: env MUSEHOOD_AST_GREP, PATH
// (nama "ast-grep" saja — "sg" di /usr/bin adalah tool group
// shadow-utils, bukan ast-grep), lalu lokasi ~/.local yang umum.
func findAstGrep() string {
	if path := strings.TrimSpace(os.Getenv("MUSEHOOD_AST_GREP")); path != "" {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	if path, err := exec.LookPath("ast-grep"); err == nil {
		return path
	}
	home, _ := os.UserHomeDir()
	for _, candidate := range []string{
		filepath.Join(home, ".local", "node_modules", ".bin", "ast-grep"),
		filepath.Join(home, ".local", "bin", "ast-grep"),
	} {
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return ""
}

// validAstLang memvalidasi nama bahasa ast-grep (huruf/angka/-/_).
func validAstLang(lang string) bool {
	if lang == "" || len(lang) > 30 {
		return false
	}
	for _, r := range lang {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '+') {
			return false
		}
	}
	return true
}

// runAstGrep menjalankan ast-grep di folder proyek. Exit code 1 berarti
// "tidak ada kecocokan" (bukan error); keluaran gabungan dikembalikan.
func (a *App) runAstGrep(ctx context.Context, projectDir string, args []string) (string, error) {
	bin := findAstGrep()
	if bin == "" {
		return "", fmt.Errorf("ast-grep tidak ditemukan")
	}
	runCtx, cancel := context.WithTimeout(ctx, astGrepTimeout)
	defer cancel()
	cmd := exec.CommandContext(runCtx, bin, append([]string{"run", "--threads", "2"}, args...)...)
	cmd.Dir = projectDir
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=/tmp", "NO_COLOR=1"}
	out, err := cmd.CombinedOutput()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 && len(out) == 0 {
			return "", nil // tidak ada kecocokan
		}
		return string(out), err
	}
	return string(out), nil
}

func (a *App) toolASTGrep(ctx context.Context, arg string) string {
	project, rest := parseProjectArg(arg)
	lines := strings.Split(rest, "\n")
	pattern := strings.TrimSpace(lines[0])
	if pattern == "" {
		return "Error: format ast_grep — baris pertama pola AST (mis. \"func $NAME() { $$$ }\"), baris kedua (opsional) bahasa, baris ketiga (opsional) glob file."
	}
	if findAstGrep() == "" {
		return astGrepMissingError
	}
	dir, err := a.ensureProject(project)
	if err != nil {
		return "Error: " + err.Error()
	}
	args := []string{"-p", pattern}
	if len(lines) > 1 {
		if lang := strings.TrimSpace(lines[1]); lang != "" {
			if !validAstLang(lang) {
				return "Error: nama bahasa tidak valid."
			}
			args = append(args, "-l", lang)
		}
	}
	if len(lines) > 2 {
		if glob := strings.TrimSpace(lines[2]); glob != "" {
			if len(glob) > 200 || strings.Contains(glob, "\x00") {
				return "Error: glob tidak valid."
			}
			args = append(args, "--globs", glob)
		}
	}
	args = append(args, ".")
	out, err := a.runAstGrep(ctx, dir, args)
	if err != nil {
		return truncateToolResult("Error: ast-grep gagal: " + strings.TrimSpace(out))
	}
	if strings.TrimSpace(out) == "" {
		return "Tidak ada kecocokan pola di proyek."
	}
	return truncateToolResult(out)
}

func (a *App) toolASTEdit(ctx context.Context, s seat, arg string) string {
	project, rest := parseProjectArg(arg)
	lines := strings.Split(rest, "\n")
	if len(lines) < 2 {
		return "Error: format ast_edit — baris pertama pola lama, baris kedua pola baru, baris ketiga (opsional) bahasa, baris keempat (opsional) \"apply\"."
	}
	pattern := strings.TrimSpace(lines[0])
	rewrite := strings.TrimSpace(lines[1])
	if pattern == "" || rewrite == "" {
		return "Error: pola lama dan pola baru wajib diisi."
	}
	if findAstGrep() == "" {
		return astGrepMissingError
	}
	dir, err := a.ensureProject(project)
	if err != nil {
		return "Error: " + err.Error()
	}
	args := []string{"-p", pattern, "-r", rewrite}
	apply := false
	for _, line := range lines[2:] {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if line == "apply" {
			apply = true
			continue
		}
		if !validAstLang(line) {
			return "Error: nama bahasa tidak valid."
		}
		args = append(args, "-l", line)
	}
	args = append(args, ".")
	if !apply {
		out, err := a.runAstGrep(ctx, dir, args)
		if err != nil {
			return truncateToolResult("Error: ast-grep gagal: " + strings.TrimSpace(out))
		}
		if strings.TrimSpace(out) == "" {
			return "Tidak ada kecocokan pola; tidak ada perubahan untuk diterapkan."
		}
		return truncateToolResult("Pratinjau perubahan (belum diterapkan; tambahkan baris \"apply\" untuk menerapkan):\n" + out)
	}
	detail := fmt.Sprintf("ast_edit proyek %s: %q -> %q", project, pattern, rewrite)
	if !a.requestApproval(ctx, s, "edit", "", detail) {
		return fmt.Sprintf("Perubahan TIDAK diterapkan: pemilik menolak atau tidak menyetujui dalam %d detik.", int(approvalTimeout.Seconds()))
	}
	if _, err := a.createSnapshot(project, "sebelum ast_edit"); err != nil {
		return "Error: gagal membuat snapshot proyek sebelum ast_edit."
	}
	out, err := a.runAstGrep(ctx, dir, append([]string{"-U"}, args...))
	if err != nil {
		return truncateToolResult("Error: ast-grep gagal menerapkan: " + strings.TrimSpace(out))
	}
	if strings.TrimSpace(out) == "" {
		return "Tidak ada kecocokan pola; tidak ada file berubah."
	}
	return truncateToolResult("Perubahan diterapkan (snapshot dibuat):\n" + out)
}
