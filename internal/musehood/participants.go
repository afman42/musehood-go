package musehood

import (
	"slices"
	"strings"
)

// Peserta ruang (fitur 21): ruang bersama multi-pengguna. Nama tamu
// terdaftar per ruang; pesan pemilik bisa dikirim atas nama salah satu
// peserta (field Author pada pesan).

const maxParticipants = 10

// validateParticipantName memeriksa nama peserta: wajib diisi, maks 40
// karakter, tanpa @ atau baris baru.
func validateParticipantName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || jsLength(name) > 40 || badName.MatchString(name) {
		return "", problem(400, "Nama peserta wajib diisi, maksimal 40 karakter, tanpa @ atau baris baru.")
	}
	return name, nil
}

// addParticipantLocked menambahkan satu nama peserta ke ruang.
// Pemanggil wajib memegang a.mu.
func (a *App) addParticipantLocked(roomID, name string) (any, error) {
	r := a.data.findRoom(roomID)
	if r == nil {
		return nil, problem(404, "Ruang tidak ditemukan.")
	}
	name, err := validateParticipantName(name)
	if err != nil {
		return nil, err
	}
	if strings.EqualFold(name, a.data.Owner.Name) {
		return nil, problem(400, "Nama peserta sama dengan nama pemilik.")
	}
	for _, existing := range r.Participants {
		if strings.EqualFold(existing, name) {
			return nil, problem(400, "Nama peserta sudah terdaftar di ruang ini.")
		}
	}
	if len(r.Participants) >= maxParticipants {
		return nil, problem(400, "Maksimal 10 peserta per ruang.")
	}
	previous := a.data
	r.Participants = append(r.Participants, name)
	if err := a.saveLocked(); err != nil {
		a.data = previous
		return nil, err
	}
	return map[string]any{"rooms": a.roomViewsLocked(), "activeRoomId": a.data.ActiveRoomID}, nil
}

// removeParticipantLocked menghapus satu nama peserta dari ruang.
// Pemanggil wajib memegang a.mu.
func (a *App) removeParticipantLocked(roomID, name string) (any, error) {
	r := a.data.findRoom(roomID)
	if r == nil {
		return nil, problem(404, "Ruang tidak ditemukan.")
	}
	name = strings.TrimSpace(name)
	index := -1
	for i, existing := range r.Participants {
		if strings.EqualFold(existing, name) {
			index = i
			break
		}
	}
	if index < 0 {
		return nil, problem(404, "Peserta tidak ditemukan di ruang ini.")
	}
	previous := a.data
	r.Participants = slices.Delete(slices.Clone(r.Participants), index, index+1)
	if err := a.saveLocked(); err != nil {
		a.data = previous
		return nil, err
	}
	return map[string]any{"rooms": a.roomViewsLocked(), "activeRoomId": a.data.ActiveRoomID}, nil
}

// resolveAuthorLocked memvalidasi nama pengirim pesan pemilik untuk satu
// ruang: kosong berarti pemilik utama; selain itu harus nama peserta
// terdaftar (atau nama pemilik sendiri). Mengembalikan nama kanonis
// yang disimpan pada pesan (kosong untuk pemilik utama).
func (a *App) resolveAuthorLocked(r *room, author string) (string, error) {
	author = strings.TrimSpace(author)
	if author == "" || strings.EqualFold(author, a.data.Owner.Name) {
		return "", nil
	}
	if r != nil {
		for _, name := range r.Participants {
			if strings.EqualFold(name, author) {
				return name, nil
			}
		}
	}
	return "", problem(400, "Nama pengirim bukan peserta terdaftar di ruang ini.")
}
