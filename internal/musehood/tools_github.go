package musehood

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// --- Tool GitHub (fitur omp 6): REST API GitHub ---
//
// github_read/github_search/github_actions membaca lewat REST API
// (token opsional di settings.GitHubToken — tanpa token hanya repo
// publik, dengan rate limit lebih ketat). github_push mendorong proyek
// lokal ke remote GitHub-nya (approval kind "git", token wajib).
// Token tidak pernah ditampilkan dan dikosongkan di cadangan.

const githubAPITimeout = 30 * time.Second

func githubAPIBaseURL() string {
	if base := strings.TrimSpace(os.Getenv("MUSEHOOD_GITHUB_API")); base != "" {
		return strings.TrimRight(base, "/")
	}
	return githubAPIBase
}

var githubRepoPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)

func (a *App) githubGet(ctx context.Context, cfg settings, path, accept string) (json.RawMessage, error) {
	runCtx, cancel := context.WithTimeout(ctx, githubAPITimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(runCtx, http.MethodGet, githubAPIBaseURL()+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", accept)
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "musehood-go/1.0")
	if cfg.GitHubToken != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.GitHubToken)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 8*1024*1024))
	switch resp.StatusCode {
	case http.StatusOK:
		return body, nil
	case http.StatusNotFound:
		return nil, fmt.Errorf("tidak ditemukan di GitHub (atau repo privat tanpa token)")
	case http.StatusUnauthorized:
		return nil, fmt.Errorf("token GitHub tidak valid (401); periksa token di Pengaturan")
	case http.StatusForbidden:
		if resp.Header.Get("X-RateLimit-Remaining") == "0" {
			return nil, fmt.Errorf("rate limit GitHub API tercapai; isi token GitHub di Pengaturan untuk kuota lebih besar")
		}
		return nil, fmt.Errorf("akses ditolak GitHub (403)")
	default:
		return nil, fmt.Errorf("GitHub API menjawab %d", resp.StatusCode)
	}
}

func parseGitHubRepo(raw string) (string, string, error) {
	raw = strings.TrimSpace(raw)
	if !githubRepoPattern.MatchString(raw) {
		return "", "", fmt.Errorf("format repo harus <owner>/<repo>")
	}
	owner, repo, _ := strings.Cut(raw, "/")
	return owner, repo, nil
}

func (a *App) toolGitHubRead(ctx context.Context, cfg settings, arg string) string {
	fields := strings.Fields(arg)
	if len(fields) < 2 {
		return "Error: format github_read — \"repo <owner>/<repo>\", \"file <owner>/<repo> <path> [ref]\", \"issue <owner>/<repo> <nomor>\", \"pr <owner>/<repo> <nomor>\", atau \"diff <owner>/<repo> <nomor-pr>\"."
	}
	kind, repoArg := fields[0], fields[1]
	owner, repo, err := parseGitHubRepo(repoArg)
	if err != nil {
		return "Error: " + err.Error()
	}
	switch kind {
	case "repo":
		raw, err := a.githubGet(ctx, cfg, "/repos/"+owner+"/"+repo, "application/vnd.github+json")
		if err != nil {
			return "Error: " + err.Error()
		}
		var info struct {
			FullName      string `json:"full_name"`
			Description   string `json:"description"`
			Private       bool   `json:"private"`
			DefaultBranch string `json:"default_branch"`
			Stargazers    int    `json:"stargazers_count"`
			Forks         int    `json:"forks_count"`
			OpenIssues    int    `json:"open_issues_count"`
			Language      string `json:"language"`
			HTMLURL       string `json:"html_url"`
			PushedAt      string `json:"pushed_at"`
		}
		if json.Unmarshal(raw, &info) != nil {
			return truncateToolResult(string(raw))
		}
		visibility := "publik"
		if info.Private {
			visibility = "privat"
		}
		return fmt.Sprintf("Repo %s (%s)\n%s\nBahasa: %s, branch bawaan: %s, bintang: %d, fork: %d, issue terbuka: %d\nTerakhir push: %s\n%s",
			info.FullName, visibility, info.Description, info.Language, info.DefaultBranch, info.Stargazers, info.Forks, info.OpenIssues, info.PushedAt, info.HTMLURL)
	case "file":
		if len(fields) < 3 {
			return "Error: format github_read file <owner>/<repo> <path> [ref]."
		}
		path := "/repos/" + owner + "/" + repo + "/contents/" + strings.TrimPrefix(fields[2], "/")
		if len(fields) > 3 {
			path += "?ref=" + url.QueryEscape(fields[3])
		}
		raw, err := a.githubGet(ctx, cfg, path, "application/vnd.github+json")
		if err != nil {
			return "Error: " + err.Error()
		}
		var file struct {
			Content  string `json:"content"`
			Encoding string `json:"encoding"`
			Size     int    `json:"size"`
		}
		if json.Unmarshal(raw, &file) != nil {
			return truncateToolResult(string(raw))
		}
		if file.Encoding == "base64" {
			decoded, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(file.Content, "\n", ""))
			if err == nil {
				return truncateToolResult(string(decoded))
			}
		}
		return truncateToolResult(file.Content)
	case "issue", "pr":
		if len(fields) < 3 {
			return "Error: format github_read " + kind + " <owner>/<repo> <nomor>."
		}
		number := fields[2]
		if _, err := strconv.Atoi(number); err != nil {
			return "Error: nomor harus angka."
		}
		endpoint := "issues"
		if kind == "pr" {
			endpoint = "pulls"
		}
		raw, err := a.githubGet(ctx, cfg, "/repos/"+owner+"/"+repo+"/"+endpoint+"/"+number, "application/vnd.github+json")
		if err != nil {
			return "Error: " + err.Error()
		}
		var item struct {
			Number int    `json:"number"`
			Title  string `json:"title"`
			State  string `json:"state"`
			Body   string `json:"body"`
			User   struct {
				Login string `json:"login"`
			} `json:"user"`
			HTMLURL string `json:"html_url"`
		}
		if json.Unmarshal(raw, &item) != nil {
			return truncateToolResult(string(raw))
		}
		return truncateToolResult(fmt.Sprintf("#%d %s [%s] oleh @%s\n%s\n\n%s", item.Number, item.Title, item.State, item.User.Login, item.HTMLURL, item.Body))
	case "diff":
		if len(fields) < 3 {
			return "Error: format github_read diff <owner>/<repo> <nomor-pr>."
		}
		if _, err := strconv.Atoi(fields[2]); err != nil {
			return "Error: nomor PR harus angka."
		}
		raw, err := a.githubGet(ctx, cfg, "/repos/"+owner+"/"+repo+"/pulls/"+fields[2], "application/vnd.github.v3.diff")
		if err != nil {
			return "Error: " + err.Error()
		}
		return truncateToolResult(string(raw))
	default:
		return "Error: jenis github_read tidak dikenal: " + kind
	}
}

func (a *App) toolGitHubSearch(ctx context.Context, cfg settings, arg string) string {
	kind, query, found := strings.Cut(arg, "\n")
	kind = strings.ToLower(strings.TrimSpace(kind))
	query = strings.TrimSpace(query)
	if !found || query == "" {
		return "Error: format github_search — baris pertama jenis (repos/code/issues), baris kedua kata kunci."
	}
	var endpoint string
	switch kind {
	case "repos":
		endpoint = "/search/repositories"
	case "code":
		endpoint = "/search/code"
	case "issues":
		endpoint = "/search/issues"
	default:
		return "Error: jenis pencarian harus repos, code, atau issues."
	}
	raw, err := a.githubGet(ctx, cfg, endpoint+"?q="+url.QueryEscape(query)+"&per_page=10", "application/vnd.github+json")
	if err != nil {
		return "Error: " + err.Error()
	}
	var result struct {
		TotalCount int `json:"total_count"`
		Items      []struct {
			FullName    string `json:"full_name"`
			Name        string `json:"name"`
			Title       string `json:"title"`
			Path        string `json:"path"`
			HTMLURL     string `json:"html_url"`
			Description string `json:"description"`
			Repository  struct {
				FullName string `json:"full_name"`
			} `json:"repository"`
		} `json:"items"`
	}
	if json.Unmarshal(raw, &result) != nil {
		return truncateToolResult(string(raw))
	}
	var out strings.Builder
	out.WriteString(fmt.Sprintf("Total hasil: %d\n", result.TotalCount))
	for _, item := range result.Items {
		switch kind {
		case "repos":
			out.WriteString(fmt.Sprintf("- %s — %s <%s>\n", item.FullName, item.Description, item.HTMLURL))
		case "code":
			out.WriteString(fmt.Sprintf("- %s/%s <%s>\n", item.Repository.FullName, item.Path, item.HTMLURL))
		case "issues":
			out.WriteString(fmt.Sprintf("- %s <%s>\n", item.Title, item.HTMLURL))
		}
	}
	return truncateToolResult(strings.TrimSpace(out.String()))
}

func (a *App) toolGitHubActions(ctx context.Context, cfg settings, arg string) string {
	fields := strings.Fields(arg)
	if len(fields) < 2 {
		return "Error: format github_actions — \"runs <owner>/<repo>\" atau \"run <owner>/<repo> <id>\"."
	}
	owner, repo, err := parseGitHubRepo(fields[1])
	if err != nil {
		return "Error: " + err.Error()
	}
	switch fields[0] {
	case "runs":
		raw, err := a.githubGet(ctx, cfg, "/repos/"+owner+"/"+repo+"/actions/runs?per_page=5", "application/vnd.github+json")
		if err != nil {
			return "Error: " + err.Error()
		}
		var result struct {
			WorkflowRuns []struct {
				ID         int64  `json:"id"`
				Name       string `json:"name"`
				Status     string `json:"status"`
				Conclusion string `json:"conclusion"`
				Branch     string `json:"head_branch"`
				HTMLURL    string `json:"html_url"`
			} `json:"workflow_runs"`
		}
		if json.Unmarshal(raw, &result) != nil {
			return truncateToolResult(string(raw))
		}
		if len(result.WorkflowRuns) == 0 {
			return "Belum ada run GitHub Actions."
		}
		var out strings.Builder
		for _, run := range result.WorkflowRuns {
			out.WriteString(fmt.Sprintf("- #%d %s [%s/%s] branch %s <%s>\n", run.ID, run.Name, run.Status, run.Conclusion, run.Branch, run.HTMLURL))
		}
		return truncateToolResult(strings.TrimSpace(out.String()))
	case "run":
		if len(fields) < 3 {
			return "Error: format github_actions run <owner>/<repo> <id>."
		}
		raw, err := a.githubGet(ctx, cfg, "/repos/"+owner+"/"+repo+"/actions/runs/"+fields[2], "application/vnd.github+json")
		if err != nil {
			return "Error: " + err.Error()
		}
		var run struct {
			ID         int64  `json:"id"`
			Name       string `json:"name"`
			Status     string `json:"status"`
			Conclusion string `json:"conclusion"`
			Branch     string `json:"head_branch"`
			HTMLURL    string `json:"html_url"`
		}
		if json.Unmarshal(raw, &run) != nil {
			return truncateToolResult(string(raw))
		}
		return fmt.Sprintf("Run #%d %s [%s/%s] branch %s\n%s", run.ID, run.Name, run.Status, run.Conclusion, run.Branch, run.HTMLURL)
	default:
		return "Error: format github_actions — \"runs <owner>/<repo>\" atau \"run <owner>/<repo> <id>\"."
	}
}

func (a *App) toolGitHubPush(ctx context.Context, cfg settings, s seat, arg string) string {
	if cfg.GitHubToken == "" {
		return "Error: token GitHub belum diisi di Pengaturan; github_push membutuhkan token (github_read publik tetap bisa tanpa token)."
	}
	project, _ := parseProjectArg(arg)
	dir, err := a.ensureProject(project)
	if err != nil {
		return "Error: " + err.Error()
	}
	remoteCmd := exec.CommandContext(ctx, "git", "-C", dir, "remote", "get-url", "origin")
	remoteOut, err := remoteCmd.Output()
	if err != nil {
		return "Error: proyek tidak punya remote git \"origin\"; tambahkan dulu (git remote add origin <url>)."
	}
	remote := strings.TrimSpace(string(remoteOut))
	if !strings.Contains(remote, "github.com") {
		return "Error: remote origin bukan GitHub (" + remote + "); github_push hanya untuk remote GitHub."
	}
	branchCmd := exec.CommandContext(ctx, "git", "-C", dir, "branch", "--show-current")
	branchOut, _ := branchCmd.Output()
	branch := strings.TrimSpace(string(branchOut))
	if branch == "" {
		branch = "HEAD"
	}
	detail := fmt.Sprintf("github_push proyek %s branch %s ke %s", project, branch, remote)
	if !a.requestApproval(ctx, s, "git", "", detail) {
		return fmt.Sprintf("Push TIDAK dilakukan: pemilik menolak atau tidak menyetujui dalam %d detik.", int(approvalTimeout.Seconds()))
	}
	basic := base64.StdEncoding.EncodeToString([]byte("x-access-token:" + cfg.GitHubToken))
	pushCtx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()
	pushCmd := exec.CommandContext(pushCtx, "git", "-C", dir, "push", "origin", "HEAD")
	pushCmd.Env = append(os.Environ(),
		"GIT_CONFIG_COUNT=1",
		"GIT_CONFIG_KEY_0=http.https://github.com/.extraheader",
		"GIT_CONFIG_VALUE_0=AUTHORIZATION: basic "+basic,
		"GIT_TERMINAL_PROMPT=0",
	)
	out, err := pushCmd.CombinedOutput()
	text := strings.TrimSpace(string(out))
	if err != nil {
		if text == "" {
			text = err.Error()
		}
		return truncateToolResult("Error: git push gagal:\n" + text)
	}
	if text == "" {
		text = "(sudah mutakhir)"
	}
	return truncateToolResult("Push berhasil:\n" + text)
}
