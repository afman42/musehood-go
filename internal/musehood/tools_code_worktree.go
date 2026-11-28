package musehood

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// --- Tool worktree git proyek (gaya OpenCode worktree) ---
//
// Worktree adalah salinan kerja terhubung dari repo proyek di
// projects/<proyek>-wt/<nama>, masing-masing pada branch "wt-<nama>".
// worktree_remove tanpa force hanya menghapus worktree bersih; force
// memerlukan persetujuan pemilik (kind "delete").

var worktreeNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,40}$`)

func (a *App) worktreeBaseDir(project string) string {
	return filepath.Join(a.projectsDir(), project+"-wt")
}

func (a *App) worktreeDir(project, name string) string {
	return filepath.Join(a.worktreeBaseDir(project), name)
}

// parseWorktreeArg mengurai argumen worktree: baris pertama opsional
// "project:<nama>" (pola tool proyek), lalu nama worktree dan kata
// opsional "force" untuk worktree_remove.
func parseWorktreeArg(arg string) (project, name string, force bool) {
	project, rest := parseProjectArg(arg)
	fields := strings.Fields(strings.TrimSpace(rest))
	if len(fields) > 0 {
		name = fields[0]
	}
	for _, field := range fields[1:] {
		if field == "force" {
			force = true
		}
	}
	return project, name, force
}

func (a *App) toolWorktreeCreate(ctx context.Context, arg string) string {
	project, name, _ := parseWorktreeArg(arg)
	if !worktreeNamePattern.MatchString(name) {
		return "Error: nama worktree tidak valid (huruf kecil, angka, - dan _, maks 41 karakter)."
	}
	dir, err := a.ensureProject(project)
	if err != nil {
		return "Error: " + err.Error()
	}
	if err := a.ensureProjectGit(ctx, dir); err != nil {
		return "Error: gagal menginisialisasi git."
	}
	if out, err := a.runProjectGit(ctx, dir, "rev-parse", "--verify", "HEAD"); err != nil {
		_ = out
		return fmt.Sprintf("Error: proyek %q belum punya commit; buat commit dulu dengan git_commit sebelum worktree.", project)
	}
	wtDir := a.worktreeDir(project, name)
	if _, err := os.Stat(wtDir); err == nil {
		return fmt.Sprintf("Error: worktree %q sudah ada di proyek %q.", name, project)
	}
	if err := os.MkdirAll(a.worktreeBaseDir(project), 0o777); err != nil {
		return "Error: gagal membuat folder worktree."
	}
	branch := "wt-" + name
	_, err = a.runProjectGit(ctx, dir, "worktree", "add", "-b", branch, wtDir)
	if err != nil {
		// Branch mungkin sudah ada dari worktree sebelumnya yang terhapus.
		out, err := a.runProjectGit(ctx, dir, "worktree", "add", wtDir, branch)
		if err != nil {
			return truncateToolResult("Error: git worktree add gagal: " + strings.TrimSpace(out))
		}
	}
	return fmt.Sprintf("Worktree %q dibuat untuk proyek %q pada branch %q di %s.", name, project, branch, wtDir)
}

func (a *App) toolWorktreeList(ctx context.Context, arg string) string {
	project, _ := parseProjectArg(arg)
	dir, err := a.ensureProject(project)
	if err != nil {
		return "Error: " + err.Error()
	}
	if err := a.ensureProjectGit(ctx, dir); err != nil {
		return "Error: gagal menginisialisasi git."
	}
	out, err := a.runProjectGit(ctx, dir, "worktree", "list")
	if err != nil {
		return truncateToolResult("Error: git worktree list gagal: " + strings.TrimSpace(out))
	}
	return truncateToolResult("Worktree proyek " + project + ":\n" + strings.TrimSpace(out))
}

func (a *App) toolWorktreeRemove(ctx context.Context, s seat, arg string) string {
	project, name, force := parseWorktreeArg(arg)
	if !worktreeNamePattern.MatchString(name) {
		return "Error: nama worktree tidak valid (huruf kecil, angka, - dan _, maks 41 karakter)."
	}
	dir, err := a.ensureProject(project)
	if err != nil {
		return "Error: " + err.Error()
	}
	if err := a.ensureProjectGit(ctx, dir); err != nil {
		return "Error: gagal menginisialisasi git."
	}
	wtDir := a.worktreeDir(project, name)
	if _, err := os.Stat(wtDir); err != nil {
		return fmt.Sprintf("Error: worktree %q tidak ditemukan di proyek %q.", name, project)
	}
	args := []string{"worktree", "remove"}
	if force {
		detail := fmt.Sprintf("git worktree remove --force %s di proyek %s (perubahan belum ter-commit akan hilang)", name, project)
		if !a.requestApproval(ctx, s, "delete", "", detail) {
			return fmt.Sprintf("Worktree TIDAK dihapus: pemilik menolak atau tidak menyetujui dalam %d detik.", int(approvalTimeout.Seconds()))
		}
		args = append(args, "--force")
	}
	args = append(args, wtDir)
	out, err := a.runProjectGit(ctx, dir, args...)
	if err != nil {
		return truncateToolResult("Error: git worktree remove gagal: " + strings.TrimSpace(out) + " (tambahkan \"force\" bila worktree kotor, memerlukan persetujuan pemilik)")
	}
	_, _ = a.runProjectGit(ctx, dir, "worktree", "prune")
	return fmt.Sprintf("Worktree %q dihapus dari proyek %q.", name, project)
}
