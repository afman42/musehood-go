package musehood

import (
	"context"
	"fmt"
	"slices"
	"strings"
)

// --- Kebijakan per tool + katalog on-demand (fitur omp 13) ---
//
// settings.ToolPolicies memetakan nama tool ke "allow" (bawaan),
// "prompt" (persetujuan pemilik kind "tool" sebelum dijalankan), atau
// "deny" (ditolak dengan pesan jelas). Penegakan terjadi di awal
// runToolInner, sebelum switch dispatch, sehingga berlaku untuk tool
// bawaan, tool kustom, dan tool MCP.
//
// settings.OnDemandTools membuat system prompt hanya memuat deskripsi
// penuh tool inti; tool lain hanya disebut namanya dan deskripsinya
// diambil lewat tool tool_info.
//
// Plan mode (fitur omp 12): selama room.PlanMode aktif, hanya tool
// kelas Read (readOnlyTools) yang boleh dipakai kursi di ruang itu.

// readOnlyTools adalah tool kelas Read: memeriksa state tanpa mengubah
// berkas/data durable. Dipakai penegakan plan mode.
var readOnlyTools = map[string]bool{
	"web_search": true, "fetch_url": true, "list_files": true, "read_file": true,
	"file_hash": true, "image_info": true, "pdf_text": true, "current_time": true,
	"git": true, "summarize_url": true, "rss_feed": true, "youtube_transcript": true,
	"currency_convert": true, "password_gen": true, "check_subagent": true,
	"browser_open": true, "browser_links": true, "browser_snapshot": true,
	"list_tree": true, "read_file_lines": true, "search_code": true, "find_files": true,
	"outline": true, "find_references": true, "dependency_graph": true,
	"todo_scan": true, "complexity_report": true, "env_file": true,
	"worktree_list": true, "ast_grep": true,
	"lsp_definition": true, "lsp_references": true, "lsp_diagnostics": true,
	"github_read": true, "github_search": true, "github_actions": true,
	"hub_processes": true, "memory_recall": true, "memory_reflect": true,
	"todo_list": true, "tool_info": true,
	// env_file bisa "set"/"delete"; ia tetap kelas Read di sini karena
	// argumen baca adalah pemakaian utamanya, tetapi plan mode tetap
	// memblokir mutasinya lewat pemeriksaan argumen di enforcePlanMode.
}

// coreToolNames adalah tool inti yang deskripsinya selalu dimuat penuh
// pada mode on-demand.
var coreToolNames = map[string]bool{
	"web_search": true, "fetch_url": true, "read_file": true, "write_file": true,
	"edit_file": true, "list_files": true, "current_time": true, "ask_question": true,
	"run_code": true, "run_shell": true, "search_code": true, "read_file_lines": true,
	"list_tree": true, "run_tests": true, "run_build": true, "tool_info": true,
}

// enforceToolPolicy menegakkan settings.ToolPolicies. Mengembalikan
// pesan penolakan (non-kosong) bila tool tidak boleh dijalankan.
func (a *App) enforceToolPolicy(ctx context.Context, cfg settings, s seat, name, arg string) string {
	policy := cfg.ToolPolicies[name]
	switch policy {
	case "deny":
		return fmt.Sprintf("Error: tool %s dilarang oleh kebijakan pemilik (deny) di Pengaturan.", name)
	case "prompt":
		if !a.requestApproval(ctx, s, "tool", "", name+"\n"+truncateRunes(arg, 500)) {
			return fmt.Sprintf("Tool %s TIDAK dijalankan: kebijakan pemilik meminta persetujuan dan pemilik menolak atau tidak menyetujui dalam %d detik.", name, int(approvalTimeout.Seconds()))
		}
	}
	return ""
}

// enforcePlanMode menolak tool non-Read selama plan mode aktif di ruang
// pekerjaan ini. Ruang diketahui dari toolRunInfo di context.
func (a *App) enforcePlanMode(ctx context.Context, name, arg string) string {
	info := toolRunInfoFrom(ctx)
	if info.RoomID == "" {
		return ""
	}
	a.mu.Lock()
	r := a.data.findRoom(info.RoomID)
	plan := r != nil && r.PlanMode
	a.mu.Unlock()
	if !plan {
		return ""
	}
	if readOnlyTools[name] {
		// env_file bercampur: "list"/"get" membaca, "set"/"delete"
		// menulis. Mutasinya tetap ditolak di plan mode.
		if name == "env_file" {
			_, rest := parseProjectArg(arg)
			rest = strings.TrimSpace(rest)
			if strings.HasPrefix(rest, "set ") || strings.HasPrefix(rest, "delete ") {
				return "Error: plan mode aktif di ruang ini — env_file set/delete (kelas Write) ditolak. Minta pemilik mematikan plan mode sebelum mengubah state."
			}
		}
		return ""
	}
	return fmt.Sprintf("Error: plan mode aktif di ruang ini — tool %s (kelas Write/Exec) ditolak. Selesaikan rencana dan minta pemilik menyetujuinya (matikan plan mode) sebelum memakai tool yang mengubah state.", name)
}

// toolDescriptionLines mengurai baris "- nama: deskripsi" dari teks
// instruksi tool menjadi peta nama -> baris lengkap.
func toolDescriptionLines(text string) map[string]string {
	out := map[string]string{}
	for _, line := range strings.Split(text, "\n") {
		if !strings.HasPrefix(line, "- ") {
			continue
		}
		name, _, found := strings.Cut(strings.TrimPrefix(line, "- "), ":")
		if !found {
			continue
		}
		name = strings.TrimSpace(name)
		if customToolNamePattern.MatchString(name) {
			out[name] = line
		}
	}
	return out
}

// onDemandToolText mengubah teks instruksi penuh menjadi versi
// on-demand: baris tool inti tetap penuh, tool lain hanya disebut
// namanya dalam satu daftar.
func onDemandToolText(text string) string {
	lines := strings.Split(text, "\n")
	kept := make([]string, 0, len(lines))
	var others []string
	for _, line := range lines {
		if strings.HasPrefix(line, "- ") {
			name, _, found := strings.Cut(strings.TrimPrefix(line, "- "), ":")
			if found {
				name = strings.TrimSpace(name)
				if customToolNamePattern.MatchString(name) && !coreToolNames[name] {
					others = append(others, name)
					continue
				}
			}
		}
		kept = append(kept, line)
	}
	slices.Sort(others)
	kept = append(kept, "\nTool lain yang tersedia (ambil deskripsi + format argumennya dengan tool tool_info sebelum memakai): "+strings.Join(others, ", ")+".")
	return strings.Join(kept, "\n")
}

// toolToolInfo mengembalikan deskripsi lengkap satu tool (bawaan,
// kustom, atau MCP) untuk mode on-demand maupun pemakaian biasa.
func (a *App) toolToolInfo(cfg settings, s seat, arg string, code bool) string {
	name := strings.TrimSpace(arg)
	if name == "" {
		return "Error: format tool_info adalah nama tool."
	}
	text := toolInstructions
	if code {
		text += codeToolInstruction
	}
	if line, ok := toolDescriptionLines(text)[name]; ok {
		return strings.TrimPrefix(line, "- ")
	}
	for _, tool := range a.loadCustomTools() {
		if tool.Name == name {
			hint := tool.ArgHint
			if hint == "" {
				hint = "sesuai deskripsi"
			}
			return fmt.Sprintf("%s: %s. Argumen: %s.", tool.Name, tool.Description, hint)
		}
	}
	if strings.HasPrefix(name, "mcp_") {
		for _, line := range strings.Split(a.mcpInstructionLines(s, cfg.MCPServers), "\n") {
			if strings.HasPrefix(line, "- "+name+":") {
				return strings.TrimPrefix(line, "- ")
			}
		}
	}
	if builtinToolNames[name] {
		return name + ": tool bawaan (deskripsi tidak tersedia untuk mode saat ini)."
	}
	return fmt.Sprintf("Error: tool %q tidak dikenal.", name)
}
