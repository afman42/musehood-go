package musehood

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// --- Workflow (fitur omp 12): review, commit workflow, goal/plan/vibe,
// autoresearch ---
//
// Batas jujur: review memakai alur diskusi ruang biasa (kursi meninjau
// diff, integrator merangkum); commit workflow mengusulkan grup commit
// konvensional dan hanya commit setelah pemilik menyetujui (push
// terpisah lewat tool github_push); goal mode menyuntikkan objektif
// persisten ke system prompt; plan mode ditegakkan di dispatch tool;
// vibe mode adalah flag + instruksi direktur untuk kursi integrator
// (memakai spawn_subagent/hub yang sudah ada — bukan runtime pekerja
// persisten baru); autoresearch menjalankan skrip benchmark N iterasi
// di sandbox pada branch autoresearch/* tanpa reset destruktif.

// roomModePrompt menyusun tambahan system prompt untuk goal/plan/vibe.
func roomModePrompt(r *room, s seat, cfg settings) string {
	var out strings.Builder
	if strings.TrimSpace(r.Goal) != "" {
		out.WriteString("\n\nObjektif ruang (goal mode, persisten): " + r.Goal + "\nArahkan semua jawaban dan tindakanmu untuk memajukan objektif ini.")
	}
	if r.PlanMode {
		out.WriteString("\n\nPlan mode AKTIF: kamu hanya boleh memakai tool kelas Read (membaca/memeriksa). Tool yang mengubah state akan ditolak sistem. Susun rencana langkah demi langkah dan minta pemilik menyetujuinya (mematikan plan mode) sebelum eksekusi.")
	}
	if r.VibeMode && s.Code == cfg.IntegratorSeat {
		out.WriteString("\n\nVibe mode AKTIF: kamu adalah direktur. Teruskan maksud pemilik menjadi tugas-tugas pekerja: gunakan spawn_subagent untuk pekerja persisten per tugas, hub_message untuk mengoordinasikan kursi lain, todo_add/todo_update untuk melacak pekerjaan, dan checkpoint sebelum langkah berisiko. Kamu sendiri fokus mengarahkan dan merangkum, bukan mengerjakan semuanya sendiri.")
	} else if r.VibeMode {
		out.WriteString("\n\nVibe mode AKTIF: kursi integrator bertindak sebagai direktur; ikuti arahan tugas darinya dan laporkan hasil lewat hub_message.")
	}
	return out.String()
}

// --- /review ---

type reviewInput struct {
	Project string `json:"project"`
	RoomID  string `json:"roomId"`
	Diff    string `json:"diff"`
}

// projectDiff mengembalikan diff proyek: perubahan belum commit, atau
// diff commit terakhir bila bersih.
func (a *App) projectDiff(dir string) (string, error) {
	run := func(args ...string) (string, error) {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		out, err := cmd.CombinedOutput()
		return string(out), err
	}
	diff, err := run("diff", "HEAD")
	if err != nil {
		return "", fmt.Errorf("git diff gagal (proyek belum repo git?)")
	}
	if strings.TrimSpace(diff) == "" {
		diff, err = run("show", "--format=", "HEAD")
		if err != nil {
			return "", fmt.Errorf("tidak ada perubahan untuk ditinjau")
		}
	}
	if strings.TrimSpace(diff) == "" {
		return "", fmt.Errorf("tidak ada perubahan untuk ditinjau")
	}
	if len(diff) > 12000 {
		diff = diff[:12000] + "\n... (diff dipotong)"
	}
	return diff, nil
}

// reviewLocked memulai pekerjaan review di ruang: tiap kursi meninjau
// diff, integrator merangkum (alur diskusi biasa).
func (a *App) reviewLocked(input reviewInput) (any, error) {
	diff := strings.TrimSpace(input.Diff)
	project := strings.TrimSpace(input.Project)
	if diff == "" {
		if project == "" {
			return nil, problem(400, "Isi project atau diff.")
		}
		dir, err := a.ensureProject(project)
		if err != nil {
			return nil, problem(400, err.Error())
		}
		d, err := a.projectDiff(dir)
		if err != nil {
			return nil, problem(400, err.Error())
		}
		diff = d
	}
	prompt := "Tinjau diff berikut sebagai code review. Untuk tiap temuan: sebutkan berkas/baris, tingkat keparahan (kritis/sedang/rendah), dan saran perbaikan konkret. Integrator: rangkum temuan terprioritas di akhir.\n\n```diff\n" + diff + "\n```"
	job, err := a.submitLocked(prompt, "", "", nil, input.RoomID, "", true)
	if err != nil {
		return nil, err
	}
	return map[string]any{"jobId": job.ID, "roomId": job.RoomID}, nil
}

// --- Commit workflow ---

type commitGroup struct {
	Message string   `json:"message"`
	Files   []string `json:"files"`
}

type commitWorkflowInput struct {
	Project string        `json:"project"`
	Approve bool          `json:"approve"`
	Groups  []commitGroup `json:"groups"`
}

// proposeCommitGroups mengelompokkan berkas berubah per direktori atas
// menjadi usulan commit konvensional.
func proposeCommitGroups(dir string) ([]commitGroup, error) {
	cmd := exec.Command("git", "-C", dir, "status", "--porcelain")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("git status gagal (proyek belum repo git?)")
	}
	byScope := map[string][]string{}
	var order []string
	// Jangan TrimSpace seluruh keluaran: baris pertama porcelain bisa
	// diawali spasi (" M file") yang signifikan untuk offset path.
	for _, line := range strings.Split(string(out), "\n") {
		if len(line) < 4 {
			continue
		}
		path := strings.TrimSpace(line[3:])
		if idx := strings.Index(path, " -> "); idx >= 0 {
			path = path[idx+4:]
		}
		scope := "chore"
		parts := strings.Split(path, "/")
		switch {
		case strings.HasSuffix(path, "_test.go") || strings.Contains(path, ".test.") || strings.Contains(path, "/test"):
			scope = "test"
		case strings.HasSuffix(path, ".md"):
			scope = "docs"
		case len(parts) > 1:
			scope = "feat(" + parts[0] + ")"
		default:
			scope = "feat"
		}
		if _, ok := byScope[scope]; !ok {
			order = append(order, scope)
		}
		byScope[scope] = append(byScope[scope], path)
	}
	sort.Strings(order)
	groups := make([]commitGroup, 0, len(order))
	for _, scope := range order {
		files := byScope[scope]
		groups = append(groups, commitGroup{
			Message: fmt.Sprintf("%s: update %d file", scope, len(files)),
			Files:   files,
		})
	}
	return groups, nil
}

// executeCommitGroups menjalankan git add + commit per grup.
func executeCommitGroups(ctx context.Context, dir string, groups []commitGroup) ([]string, error) {
	var done []string
	for _, group := range groups {
		if len(group.Files) == 0 || strings.TrimSpace(group.Message) == "" {
			continue
		}
		addArgs := append([]string{"-C", dir, "add", "--"}, group.Files...)
		if out, err := exec.CommandContext(ctx, "git", addArgs...).CombinedOutput(); err != nil {
			return done, fmt.Errorf("git add gagal: %s", strings.TrimSpace(string(out)))
		}
		commitArgs := []string{"-C", dir, "-c", "user.name=Musehood Go", "-c", "user.email=musehood@localhost", "commit", "-m", group.Message}
		if out, err := exec.CommandContext(ctx, "git", commitArgs...).CombinedOutput(); err != nil {
			return done, fmt.Errorf("git commit gagal: %s", strings.TrimSpace(string(out)))
		}
		done = append(done, group.Message)
	}
	return done, nil
}

// --- Autoresearch ---

type autoresearchInput struct {
	Project    string `json:"project"`
	Script     string `json:"script"`
	Iterations int    `json:"iterations"`
}

type autoresearchResult struct {
	Iteration int     `json:"iteration"`
	OK        bool    `json:"ok"`
	Duration  float64 `json:"durationSec"`
	Metric    string  `json:"metric,omitempty"`
	Output    string  `json:"output"`
}

var autoresearchNumber = regexp.MustCompile(`-?\d+(\.\d+)?`)

// runAutoresearch menjalankan skrip benchmark di sandbox proyek pada
// branch autoresearch/* dan melaporkan tiap iterasi + yang terbaik
// (metrik = angka terakhir di keluaran; tanpa angka: sukses tercepat).
// Tidak ada reset destruktif otomatis.
func (a *App) runAutoresearch(ctx context.Context, input autoresearchInput) (any, error) {
	project := strings.TrimSpace(input.Project)
	script := strings.TrimSpace(input.Script)
	if project == "" || script == "" {
		return nil, problem(400, "Isi project dan script.")
	}
	iterations := input.Iterations
	if iterations < 1 {
		iterations = 3
	}
	if iterations > 10 {
		iterations = 10
	}
	dir, err := a.ensureProject(project)
	if err != nil {
		return nil, problem(400, err.Error())
	}
	branch := "autoresearch/" + time.Now().Format("20060102-150405")
	branchNote := ""
	if out, err := exec.CommandContext(ctx, "git", "-C", dir, "checkout", "-b", branch).CombinedOutput(); err != nil {
		branchNote = "branch tidak dibuat: " + strings.TrimSpace(string(out))
		branch = ""
	}
	results := make([]autoresearchResult, 0, iterations)
	for i := 1; i <= iterations; i++ {
		start := time.Now()
		out, runErr := a.execInProject(ctx, dir, []string{"sh", "-c", script}, 120, nil)
		duration := time.Since(start).Seconds()
		metric := ""
		if numbers := autoresearchNumber.FindAllString(out, -1); len(numbers) > 0 {
			metric = numbers[len(numbers)-1]
		}
		results = append(results, autoresearchResult{
			Iteration: i, OK: runErr == nil, Duration: duration,
			Metric: metric, Output: tailLines(strings.TrimSpace(out), 10),
		})
	}
	best := -1
	bestMetric := 0.0
	for i, res := range results {
		if !res.OK {
			continue
		}
		if res.Metric != "" {
			if value, err := strconv.ParseFloat(res.Metric, 64); err == nil {
				if best < 0 || value > bestMetric {
					best, bestMetric = i, value
				}
				continue
			}
		}
		if best < 0 || res.Duration < results[best].Duration {
			best = i
		}
	}
	return map[string]any{
		"branch": branch, "branchNote": branchNote,
		"results": results, "bestIteration": best + 1,
		"note": "Tanpa reset destruktif: branch dan perubahan dibiarkan apa adanya untuk ditinjau pemilik.",
	}, nil
}

// ensureProjectGitignore memastikan folder cache sandbox tidak ikut
// status git (dipakai autoresearch/commit workflow).
func ensureProjectGitignore(dir string) {
	path := filepath.Join(dir, ".gitignore")
	existing, _ := os.ReadFile(path)
	text := string(existing)
	for _, entry := range []string{".gocache/", ".gomodcache/"} {
		if !strings.Contains(text, entry) {
			text += entry + "\n"
		}
	}
	_ = os.WriteFile(path, []byte(text), 0o644)
}
