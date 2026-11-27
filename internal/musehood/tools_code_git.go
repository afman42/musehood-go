package musehood

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// --- Grup D: git dalam (di folder proyek) ---

var gitBranchNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]{0,80}$`)

// runProjectGit menjalankan git di folder proyek (langsung, seperti
// toolGit pada sandbox) dengan identitas musehood-go.
func (a *App) runProjectGit(ctx context.Context, dir string, args ...string) (string, error) {
	runCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	full := append([]string{"-C", dir, "-c", "user.name=musehood-go", "-c", "user.email=musehood@local", "-c", "safe.directory=" + dir}, args...)
	cmd := exec.CommandContext(runCtx, "git", full...)
	cmd.Env = append(os.Environ(), "HOME="+dir, "GIT_TERMINAL_PROMPT=0", "GIT_CONFIG_NOSYSTEM=1")
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func (a *App) ensureProjectGit(ctx context.Context, dir string) error {
	if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
		return nil
	}
	_, err := a.runProjectGit(ctx, dir, "init", "-q")
	return err
}

func (a *App) toolGitCommit(ctx context.Context, s seat, arg string) string {
	project, rest := parseProjectArg(arg)
	message := strings.TrimSpace(rest)
	if message == "" {
		return "Error: pesan commit kosong."
	}
	dir, err := a.ensureProject(project)
	if err != nil {
		return "Error: " + err.Error()
	}
	if err := a.ensureProjectGit(ctx, dir); err != nil {
		return "Error: gagal menginisialisasi git."
	}
	status, _ := a.runProjectGit(ctx, dir, "status", "--porcelain")
	if strings.TrimSpace(status) == "" {
		return fmt.Sprintf("Tidak ada perubahan untuk di-commit di proyek %q.", project)
	}
	detail := fmt.Sprintf("git commit di proyek %s: %q (%d berkas berubah)", project, message, len(strings.Split(strings.TrimSpace(status), "\n")))
	if !a.requestApproval(ctx, s, "git", "", detail) {
		return fmt.Sprintf("Commit TIDAK dilakukan: pemilik menolak atau tidak menyetujui dalam %d detik.", int(approvalTimeout.Seconds()))
	}
	if out, err := a.runProjectGit(ctx, dir, "add", "-A"); err != nil {
		return truncateToolResult("Error: git add gagal: " + strings.TrimSpace(out))
	}
	out, err := a.runProjectGit(ctx, dir, "commit", "-q", "-m", message)
	if err != nil {
		return truncateToolResult("Error: git commit gagal: " + strings.TrimSpace(out))
	}
	hash, _ := a.runProjectGit(ctx, dir, "rev-parse", "--short", "HEAD")
	return fmt.Sprintf("Commit %s dibuat di proyek %q: %q.", strings.TrimSpace(hash), project, message)
}

func (a *App) toolGitBranch(ctx context.Context, s seat, arg string) string {
	project, rest := parseProjectArg(arg)
	fields := strings.Fields(strings.TrimSpace(rest))
	if len(fields) == 0 {
		return "Error: format git_branch — list, create <nama>, atau checkout <nama>."
	}
	dir, err := a.ensureProject(project)
	if err != nil {
		return "Error: " + err.Error()
	}
	if err := a.ensureProjectGit(ctx, dir); err != nil {
		return "Error: gagal menginisialisasi git."
	}
	switch fields[0] {
	case "list":
		out, err := a.runProjectGit(ctx, dir, "branch", "--list")
		if err != nil {
			return truncateToolResult("Error: git branch gagal: " + strings.TrimSpace(out))
		}
		if strings.TrimSpace(out) == "" {
			return fmt.Sprintf("Belum ada branch di proyek %q (belum ada commit).", project)
		}
		return truncateToolResult("Branch proyek " + project + ":\n" + strings.TrimSpace(out))
	case "create":
		if len(fields) < 2 || !gitBranchNamePattern.MatchString(fields[1]) {
			return "Error: nama branch tidak valid."
		}
		out, err := a.runProjectGit(ctx, dir, "branch", fields[1])
		if err != nil {
			return truncateToolResult("Error: git branch gagal: " + strings.TrimSpace(out))
		}
		return fmt.Sprintf("Branch %q dibuat di proyek %q.", fields[1], project)
	case "checkout":
		if len(fields) < 2 || !gitBranchNamePattern.MatchString(fields[1]) {
			return "Error: nama branch tidak valid."
		}
		status, _ := a.runProjectGit(ctx, dir, "status", "--porcelain")
		if strings.TrimSpace(status) != "" {
			detail := fmt.Sprintf("git checkout %s di proyek %s dengan perubahan belum ter-commit", fields[1], project)
			if !a.requestApproval(ctx, s, "git", "", detail) {
				return fmt.Sprintf("Checkout TIDAK dilakukan: pemilik menolak atau tidak menyetujui dalam %d detik.", int(approvalTimeout.Seconds()))
			}
		}
		out, err := a.runProjectGit(ctx, dir, "checkout", "-q", fields[1])
		if err != nil {
			return truncateToolResult("Error: git checkout gagal: " + strings.TrimSpace(out))
		}
		return fmt.Sprintf("Branch aktif proyek %q kini %q.", project, fields[1])
	default:
		return "Error: subperintah harus list, create, atau checkout."
	}
}

func (a *App) toolGitStash(ctx context.Context, s seat, arg string) string {
	project, rest := parseProjectArg(arg)
	fields := strings.Fields(strings.TrimSpace(rest))
	if len(fields) == 0 {
		return "Error: format git_stash — push, pop, atau list."
	}
	dir, err := a.ensureProject(project)
	if err != nil {
		return "Error: " + err.Error()
	}
	if err := a.ensureProjectGit(ctx, dir); err != nil {
		return "Error: gagal menginisialisasi git."
	}
	switch fields[0] {
	case "push":
		out, err := a.runProjectGit(ctx, dir, "stash", "push", "-m", "musehood")
		if err != nil {
			return truncateToolResult("Error: git stash push gagal: " + strings.TrimSpace(out))
		}
		return truncateToolResult("Stash proyek " + project + ": " + strings.TrimSpace(out))
	case "pop":
		detail := fmt.Sprintf("git stash pop di proyek %s (menerapkan perubahan tersimpan)", project)
		if !a.requestApproval(ctx, s, "git", "", detail) {
			return fmt.Sprintf("Stash pop TIDAK dilakukan: pemilik menolak atau tidak menyetujui dalam %d detik.", int(approvalTimeout.Seconds()))
		}
		out, err := a.runProjectGit(ctx, dir, "stash", "pop")
		if err != nil {
			return truncateToolResult("Error: git stash pop gagal: " + strings.TrimSpace(out))
		}
		return truncateToolResult("Stash diterapkan di proyek " + project + ":\n" + tailLines(out, 20))
	case "list":
		out, err := a.runProjectGit(ctx, dir, "stash", "list")
		if err != nil {
			return truncateToolResult("Error: git stash list gagal: " + strings.TrimSpace(out))
		}
		if strings.TrimSpace(out) == "" {
			return fmt.Sprintf("Tidak ada stash di proyek %q.", project)
		}
		return truncateToolResult("Stash proyek " + project + ":\n" + strings.TrimSpace(out))
	default:
		return "Error: subperintah harus push, pop, atau list."
	}
}
