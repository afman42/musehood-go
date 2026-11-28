package musehood

import (
	"context"
	"fmt"
	"strings"
)

// --- Checkpoint/rewind (fitur omp 9) ---
//
// room.Checkpoints menandai titik balik percakapan (id, label,
// messageId pesan terakhir saat checkpoint dibuat). Rewind memotong
// pesan ruang yang dibuat SESUDAH checkpoint (berdasarkan urutan
// a.data.Messages). Tool kursi rewind memerlukan persetujuan pemilik;
// jalur API pemilik (POST /api/rooms/{id}/rewind) adalah konfirmasi
// pemilik itu sendiri dari UI.

const maxRoomCheckpoints = 50

func roomCheckpointsView(r room) []map[string]any {
	out := make([]map[string]any, 0, len(r.Checkpoints))
	for _, cp := range r.Checkpoints {
		out = append(out, map[string]any{
			"id": cp.ID, "label": cp.Label, "messageId": cp.MessageID, "createdAt": cp.CreatedAt,
		})
	}
	return out
}

// addCheckpointLocked membuat checkpoint pada pesan terakhir ruang.
func (a *App) addCheckpointLocked(r *room, label string) (roomCheckpoint, error) {
	label = strings.TrimSpace(label)
	if len(label) > 200 {
		label = label[:200]
	}
	lastID := ""
	for _, m := range a.data.Messages {
		if m.RoomID == r.ID {
			lastID = m.ID
		}
	}
	if lastID == "" {
		return roomCheckpoint{}, problem(400, "Belum ada pesan di ruang ini untuk ditandai.")
	}
	if label == "" {
		label = fmt.Sprintf("Checkpoint %d", len(r.Checkpoints)+1)
	}
	cp := roomCheckpoint{ID: uuid(), Label: label, MessageID: lastID, CreatedAt: now()}
	r.Checkpoints = append(r.Checkpoints, cp)
	if len(r.Checkpoints) > maxRoomCheckpoints {
		r.Checkpoints = r.Checkpoints[len(r.Checkpoints)-maxRoomCheckpoints:]
	}
	return cp, nil
}

// findCheckpointLocked mencari checkpoint berdasarkan id atau label.
func findCheckpointLocked(r *room, ref string) *roomCheckpoint {
	ref = strings.TrimSpace(ref)
	for i := range r.Checkpoints {
		if r.Checkpoints[i].ID == ref || r.Checkpoints[i].Label == ref {
			return &r.Checkpoints[i]
		}
	}
	return nil
}

// rewindRoomLocked memotong pesan ruang sesudah checkpoint dan
// membuang checkpoint yang lebih baru. Mengembalikan jumlah pesan
// yang dipotong.
func (a *App) rewindRoomLocked(r *room, cp *roomCheckpoint) int {
	cutIndex := -1
	for i, m := range a.data.Messages {
		if m.ID == cp.MessageID {
			cutIndex = i
			break
		}
	}
	if cutIndex < 0 {
		return 0
	}
	kept := a.data.Messages[:0]
	removed := 0
	for i, m := range a.data.Messages {
		if i > cutIndex && m.RoomID == r.ID {
			removed++
			continue
		}
		kept = append(kept, m)
	}
	a.data.Messages = kept
	// Buang checkpoint yang lebih baru dari titik rewind berdasarkan
	// urutan slice (bukan perbandingan CreatedAt: checkpoint yang
	// dibuat pada milidetik yang sama akan lolos perbandingan string
	// dan menunjuk pesan yang sudah dipotong — bug ronde 2).
	for i := range r.Checkpoints {
		if r.Checkpoints[i].ID == cp.ID {
			r.Checkpoints = r.Checkpoints[:i+1]
			break
		}
	}
	return removed
}

func (a *App) toolCheckpoint(ctx context.Context, arg string) string {
	info := toolRunInfoFrom(ctx)
	if info.RoomID == "" {
		return "Error: checkpoint hanya bisa dipakai di dalam ruang."
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	r := a.data.findRoom(info.RoomID)
	if r == nil {
		return "Error: ruang tidak ditemukan."
	}
	cp, err := a.addCheckpointLocked(r, arg)
	if err != nil {
		return "Error: " + err.Error()
	}
	if err := a.saveLocked(); err != nil {
		return "Error: gagal menyimpan checkpoint."
	}
	return fmt.Sprintf("Checkpoint dibuat: %q (id %s).", cp.Label, cp.ID)
}

func (a *App) toolRewind(ctx context.Context, s seat, arg string) string {
	ref := strings.TrimSpace(arg)
	if ref == "" {
		return "Error: format rewind — id atau label checkpoint."
	}
	info := toolRunInfoFrom(ctx)
	if info.RoomID == "" {
		return "Error: rewind hanya bisa dipakai di dalam ruang."
	}
	a.mu.Lock()
	r := a.data.findRoom(info.RoomID)
	if r == nil {
		a.mu.Unlock()
		return "Error: ruang tidak ditemukan."
	}
	cp := findCheckpointLocked(r, ref)
	if cp == nil {
		a.mu.Unlock()
		return fmt.Sprintf("Error: checkpoint %q tidak ditemukan.", ref)
	}
	label := cp.Label
	a.mu.Unlock()

	detail := fmt.Sprintf("rewind ruang ke checkpoint %q (memotong pesan sesudahnya)", label)
	if !a.requestApproval(ctx, s, "edit", "", detail) {
		return fmt.Sprintf("Rewind TIDAK dilakukan: pemilik menolak atau tidak menyetujui dalam %d detik.", int(approvalTimeout.Seconds()))
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	r = a.data.findRoom(info.RoomID)
	if r == nil {
		return "Error: ruang tidak ditemukan."
	}
	cp = findCheckpointLocked(r, ref)
	if cp == nil {
		return "Error: checkpoint sudah tidak ada."
	}
	removed := a.rewindRoomLocked(r, cp)
	if err := a.saveLocked(); err != nil {
		return "Error: gagal menyimpan rewind."
	}
	return fmt.Sprintf("Rewind ke checkpoint %q selesai: %d pesan sesudahnya dipotong.", label, removed)
}
