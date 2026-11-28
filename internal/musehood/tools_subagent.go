package musehood

import (
	"context"
	"fmt"
	"strings"
)

// --- Tool subagent (gaya OpenCode "subagent") ---
//
// Kursi dapat memulai sesi anak: ruang baru "Sub: <judul>" dengan pesan
// pemilik berisi prompt dan satu pekerjaan untuk kursi tujuan. Sesi anak
// berjalan di latar belakang; kursi induk membaca hasilnya lewat
// check_subagent. Kedalaman maksimum 1: ruang sub ditandai lewat
// room.SubagentOf (ID ruang induk) dan spawn dari dalamnya ditolak.
// Saat pekerjaan anak selesai, ringkasan hasil diposting sebagai pesan
// kursi di ruang induk (lihat completeJobLocked).

// toolRunInfo membawa konteks pekerjaan/ruang yang sedang menjalankan
// tool, disisipkan ke context oleh runLocked.
type toolRunInfo struct {
	JobID  string
	RoomID string
}

type toolRunInfoKey struct{}

func toolRunInfoFrom(ctx context.Context) toolRunInfo {
	if info, ok := ctx.Value(toolRunInfoKey{}).(toolRunInfo); ok {
		return info
	}
	return toolRunInfo{}
}

const maxSubagentPromptChars = 4000

// parseSubagentArg mengurai argumen spawn_subagent: baris pertama
// opsional "seat:<kode>" memilih kursi tujuan (bawaan kursi pemanggil),
// sisanya adalah prompt sesi anak.
func parseSubagentArg(arg string) (seatCode, prompt string) {
	first, rest, found := strings.Cut(arg, "\n")
	trimmed := strings.TrimSpace(first)
	if strings.HasPrefix(trimmed, "seat:") {
		seatCode = strings.TrimSpace(strings.TrimPrefix(trimmed, "seat:"))
		if !found {
			return seatCode, ""
		}
		return seatCode, strings.TrimSpace(rest)
	}
	return "", strings.TrimSpace(arg)
}

// spawnSubagentLocked membuat ruang sub + pesan pemilik + pekerjaan satu
// langkah untuk kursi tujuan. Harus dipanggil dengan a.mu terkunci.
func (a *App) spawnSubagentLocked(parentRoomID, targetSeatCode, prompt string) (*room, *job, error) {
	target := a.data.findSeat(targetSeatCode)
	if target == nil {
		return nil, nil, problem(400, "Kode kursi tujuan tidak dikenal: "+targetSeatCode)
	}
	if target.Provider != "gateway" {
		return nil, nil, problem(400, "Kursi tujuan subagent harus kursi gateway.")
	}
	title := "Sub: " + truncateRunes(strings.Join(strings.Fields(prompt), " "), 40)
	r := room{
		ID:         newRoomID(),
		Name:       title,
		CreatedAt:  now(),
		SeatCodes:  []string{targetSeatCode},
		SubagentOf: parentRoomID,
	}
	steps := []step{{Seat: targetSeatCode, Kind: "chat", Status: "pending", Label: "Subagent"}}
	j := &job{
		ID: uuid(), Prompt: prompt, Kind: "subagent", CreatedAt: now(),
		Status: "processing", Steps: steps, RoomID: r.ID, BranchID: "main",
	}
	previous := a.data
	a.data.Rooms = append(a.data.Rooms, r)
	a.data.Jobs = append(a.data.Jobs, j)
	a.data.Messages = append(a.data.Messages, &message{
		ID: uuid(), CreatedAt: now(), AuthorType: "owner", JobID: j.ID,
		Text: prompt, Status: "completed", RoomID: r.ID, BranchID: "main",
	})
	if err := a.saveLocked(); err != nil {
		a.data = previous
		return nil, nil, err
	}
	created := a.data.findRoom(r.ID)
	a.advanceLocked(j)
	return created, j, nil
}

func (a *App) toolSpawnSubagent(ctx context.Context, s seat, arg string) string {
	seatCode, prompt := parseSubagentArg(arg)
	if prompt == "" {
		return "Error: prompt subagent kosong. Format: baris pertama opsional \"seat:<kode>\", baris berikutnya prompt."
	}
	if len([]rune(prompt)) > maxSubagentPromptChars {
		return fmt.Sprintf("Error: prompt subagent terlalu panjang (maks %d karakter).", maxSubagentPromptChars)
	}
	if seatCode == "" {
		seatCode = s.Code
	}
	info := toolRunInfoFrom(ctx)
	a.mu.Lock()
	parent := a.data.findRoom(info.RoomID)
	if parent == nil {
		parent = a.data.activeRoom()
	}
	if parent != nil && parent.SubagentOf != "" {
		a.mu.Unlock()
		return "Error: spawn_subagent tidak boleh dari dalam ruang subagent (kedalaman maksimum 1)."
	}
	parentID := ""
	if parent != nil {
		parentID = parent.ID
	}
	r, j, err := a.spawnSubagentLocked(parentID, seatCode, a.redactLocked(prompt))
	a.mu.Unlock()
	if err != nil {
		return "Error: " + publicError(err)
	}
	targetName := seatCode
	a.mu.Lock()
	if t := a.data.findSeat(seatCode); t != nil {
		targetName = t.Name
	}
	a.mu.Unlock()
	return fmt.Sprintf("Sesi subagent dimulai di ruang %q (id ruang: %s, id pekerjaan: %s) untuk kursi %s (%s). Baca hasilnya nanti dengan check_subagent (argumen: id ruang).", r.Name, r.ID, j.ID, targetName, seatCode)
}

// subagentResultLocked membaca status/hasil sesi anak dari id ruang atau
// id pekerjaan. Harus dipanggil dengan a.mu terkunci.
func (a *App) subagentResultLocked(id string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return "Error: argumen check_subagent adalah id ruang atau id pekerjaan subagent."
	}
	var j *job
	for _, candidate := range a.data.Jobs {
		if candidate.ID == id || candidate.RoomID == id {
			j = candidate // pekerjaan terakhir di ruang itu
		}
	}
	if j == nil {
		return "Error: sesi subagent tidak ditemukan untuk id itu."
	}
	roomName := j.RoomID
	if r := a.data.findRoom(j.RoomID); r != nil {
		roomName = r.Name
	}
	if j.Status != "completed" {
		return fmt.Sprintf("Sesi subagent %q masih berjalan (status pekerjaan: %s). Coba lagi nanti.", roomName, j.Status)
	}
	var last *message
	for _, m := range a.data.Messages {
		if m.JobID == j.ID && m.AuthorType == "seat" && m.Status == "completed" {
			last = m
		}
	}
	if last == nil {
		return fmt.Sprintf("Sesi subagent %q selesai tanpa balasan kursi.", roomName)
	}
	text := strings.TrimSpace(last.Text)
	if text == "" && last.Error != "" {
		return fmt.Sprintf("Sesi subagent %q selesai dengan error: %s", roomName, last.Error)
	}
	return fmt.Sprintf("Hasil sesi subagent %q:\n%s", roomName, truncateToolResult(text))
}

func (a *App) toolCheckSubagent(arg string) string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.subagentResultLocked(arg)
}

// postSubagentSummaryLocked memposting ringkasan hasil sesi anak ke
// ruang induk saat pekerjaannya selesai. Harus dipanggil dengan a.mu
// terkunci (dari completeJobLocked).
func (a *App) postSubagentSummaryLocked(j *job) {
	r := a.data.findRoom(j.RoomID)
	if r == nil || r.SubagentOf == "" {
		return
	}
	parent := a.data.findRoom(r.SubagentOf)
	if parent == nil {
		return
	}
	var last *message
	for _, m := range a.data.Messages {
		if m.JobID == j.ID && m.AuthorType == "seat" && m.Status == "completed" {
			last = m
		}
	}
	if last == nil {
		return
	}
	text := strings.TrimSpace(last.Text)
	if text == "" {
		text = "(tanpa teks)"
	}
	a.data.Messages = append(a.data.Messages, &message{
		ID: uuid(), CreatedAt: now(), AuthorType: "seat", Seat: last.Seat,
		JobID: j.ID, Text: fmt.Sprintf("Hasil subagent %q:\n%s", r.Name, truncateRunes(text, 1500)),
		Status: "completed", Label: "Subagent", RoomID: parent.ID, BranchID: parent.activeBranch(),
	})
}
