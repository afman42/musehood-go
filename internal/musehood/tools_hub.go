package musehood

import (
	"context"
	"fmt"
	"slices"
	"strings"
)

// --- Tool hub (fitur omp 8): pesan antar-kursi + proses latar ---
//
// hub_message menyisipkan pesan ke ruang saat ini yang ditujukan ke
// kursi lain (menjadi bagian konteks ruang, terlihat oleh kursi tujuan
// pada giliran berikutnya). hub_processes mendaftar proses latar yang
// hidup dari registri terpusat: dev server run_server, sesi eval, sesi
// debug, dan browser CDP. hub_stop menghentikan proses berdasarkan id
// ("server:<proyek>", "eval:<kunci>", "debug:<kunci>", "browser");
// semua panggilan tercatat di audit log tool seperti tool lain.

func (a *App) toolHubMessage(ctx context.Context, s seat, arg string) string {
	target, body, found := strings.Cut(arg, "\n")
	target = strings.TrimSpace(target)
	body = strings.TrimSpace(body)
	if !found || target == "" || body == "" {
		return "Error: format hub_message — baris pertama kode kursi tujuan, baris berikutnya isi pesan."
	}
	if len(body) > 4000 {
		body = body[:4000]
	}
	info := toolRunInfoFrom(ctx)
	if info.RoomID == "" {
		return "Error: hub_message hanya bisa dipakai di dalam ruang."
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	room := a.data.findRoom(info.RoomID)
	if room == nil {
		return "Error: ruang tidak ditemukan."
	}
	targetSeat := a.data.findSeat(target)
	if targetSeat == nil {
		return fmt.Sprintf("Error: kursi %q tidak ditemukan.", target)
	}
	if targetSeat.Code == s.Code {
		return "Error: tidak bisa mengirim pesan hub ke kursi sendiri."
	}
	text := fmt.Sprintf("[hub → %s (%s)] %s", targetSeat.Name, targetSeat.Code, body)
	a.data.Messages = append(a.data.Messages, &message{
		ID: uuid(), CreatedAt: now(), AuthorType: "seat", Seat: s.Code,
		JobID: info.JobID, Text: text, Status: "completed",
		RoomID: room.ID, BranchID: room.activeBranch(),
	})
	if err := a.saveLocked(); err != nil {
		return "Error: gagal menyimpan pesan hub."
	}
	return fmt.Sprintf("Pesan terkirim ke %s (%s) di ruang ini; kursi tujuan akan membacanya pada giliran berikutnya.", targetSeat.Name, targetSeat.Code)
}

// hubProcessIDs mengembalikan id semua proses latar yang hidup.
func (a *App) hubProcessIDs() []string {
	var ids []string
	a.serverMu.Lock()
	for project, server := range a.servers {
		if server.running() {
			ids = append(ids, fmt.Sprintf("server:%s (port %d, sejak %s)", project, server.port, server.startedAt.Format("15:04:05")))
		}
	}
	a.serverMu.Unlock()
	ids = append(ids, evalSessionList()...)
	ids = append(ids, debugSessionList()...)
	if cdpBrowserAlive() {
		ids = append(ids, "browser (Chromium CDP)")
	}
	slices.Sort(ids)
	return ids
}

func (a *App) toolHubProcesses() string {
	ids := a.hubProcessIDs()
	if len(ids) == 0 {
		return "Tidak ada proses latar yang hidup."
	}
	var out strings.Builder
	out.WriteString("Proses latar yang hidup:\n")
	for _, id := range ids {
		out.WriteString("- " + id + "\n")
	}
	return strings.TrimSpace(out.String())
}

func (a *App) toolHubStop(arg string) string {
	id := strings.TrimSpace(arg)
	// Buang keterangan dalam kurung dari hub_processes.
	if idx := strings.Index(id, " ("); idx >= 0 {
		id = id[:idx]
	}
	switch {
	case id == "browser":
		if stopCDPBrowser() {
			return "Browser CDP dihentikan."
		}
		return "Browser CDP tidak sedang berjalan."
	case strings.HasPrefix(id, "server:"):
		project := strings.TrimPrefix(id, "server:")
		a.serverMu.Lock()
		server, ok := a.servers[project]
		if ok {
			delete(a.servers, project)
		}
		a.serverMu.Unlock()
		if !ok {
			return fmt.Sprintf("Server proyek %q tidak ditemukan.", project)
		}
		server.stop()
		return fmt.Sprintf("Server proyek %q dihentikan.", project)
	case strings.HasPrefix(id, "eval:"):
		if stopEvalSession(strings.TrimPrefix(id, "eval:")) {
			return "Sesi eval dihentikan."
		}
		return "Sesi eval tidak ditemukan."
	case strings.HasPrefix(id, "debug:"):
		if stopDebugSession(strings.TrimPrefix(id, "debug:")) {
			return "Sesi debug dihentikan."
		}
		return "Sesi debug tidak ditemukan."
	default:
		return "Error: format hub_stop — id proses dari hub_processes (server:<proyek>, eval:<kunci>, debug:<kunci>, browser)."
	}
}
