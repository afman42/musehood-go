package musehood

import (
	"fmt"
	"strings"
	"time"
)

// scheduledMessage adalah pesan pemilik yang dikirim otomatis pada waktu
// tertentu (sekali) atau berulang (harian/mingguan/interval jam).
type scheduledMessage struct {
	ID              string `json:"id"`
	RoomID          string `json:"roomId"`
	Text            string `json:"text"`
	SendAt          string `json:"sendAt,omitempty"` // sekali kirim (UTC RFC3339)
	Recur           string `json:"recur,omitempty"`  // "" | daily | weekly | interval
	TimeOfDay       string `json:"timeOfDay,omitempty"`
	Weekday         int    `json:"weekday,omitempty"` // 0=Minggu..6=Sabtu (weekly)
	IntervalHours   int    `json:"intervalHours,omitempty"`
	TzOffsetMinutes int    `json:"tzOffsetMinutes,omitempty"` // offset zona pengguna (menit dari UTC, +420 = WIB)
	Enabled         bool   `json:"enabled"`
	NextAt          string `json:"nextAt,omitempty"`
	LastSentAt      string `json:"lastSentAt,omitempty"`
	CreatedAt       string `json:"createdAt"`
}

type scheduledInput struct {
	RoomID          *string `json:"roomId"`
	Text            *string `json:"text"`
	SendAt          *string `json:"sendAt"`
	Recur           *string `json:"recur"`
	TimeOfDay       *string `json:"timeOfDay"`
	Weekday         *int    `json:"weekday"`
	IntervalHours   *int    `json:"intervalHours"`
	TzOffsetMinutes *int    `json:"tzOffsetMinutes"`
	Enabled         *bool   `json:"enabled"`
}

func parseScheduleTime(value string) (time.Time, error) {
	return time.Parse(time.RFC3339Nano, strings.TrimSpace(value))
}

func formatScheduleTime(t time.Time) string {
	return t.UTC().Format("2006-01-02T15:04:05.000Z")
}

// computeNextAt menghitung waktu kirim berikutnya (UTC) untuk jadwal berulang.
func computeNextAt(s scheduledMessage, from time.Time) (time.Time, error) {
	zone := time.FixedZone("", s.TzOffsetMinutes*60)
	local := from.In(zone)
	switch s.Recur {
	case "daily", "weekly":
		parts := strings.Split(s.TimeOfDay, ":")
		if len(parts) != 2 {
			return time.Time{}, problem(400, "Jam jadwal harus berformat HH:MM.")
		}
		var hour, minute int
		if _, err := fmt.Sscanf(parts[0], "%d", &hour); err != nil || hour < 0 || hour > 23 {
			return time.Time{}, problem(400, "Jam jadwal tidak valid.")
		}
		if _, err := fmt.Sscanf(parts[1], "%d", &minute); err != nil || minute < 0 || minute > 59 {
			return time.Time{}, problem(400, "Menit jadwal tidak valid.")
		}
		candidate := time.Date(local.Year(), local.Month(), local.Day(), hour, minute, 0, 0, zone)
		if s.Recur == "daily" {
			if !candidate.After(local) {
				candidate = candidate.AddDate(0, 0, 1)
			}
			return candidate, nil
		}
		// weekly
		if s.Weekday < 0 || s.Weekday > 6 {
			return time.Time{}, problem(400, "Hari jadwal harus 0 (Minggu) sampai 6 (Sabtu).")
		}
		daysAhead := (s.Weekday - int(local.Weekday()) + 7) % 7
		candidate = candidate.AddDate(0, 0, daysAhead)
		if !candidate.After(local) {
			candidate = candidate.AddDate(0, 0, 7)
		}
		return candidate, nil
	case "interval":
		if s.IntervalHours < 1 || s.IntervalHours > 24*30 {
			return time.Time{}, problem(400, "Interval harus 1 sampai 720 jam.")
		}
		return from.Add(time.Duration(s.IntervalHours) * time.Hour), nil
	}
	return time.Time{}, problem(400, "Jenis pengulangan tidak dikenal.")
}

func (a *App) validateScheduledLocked(s *scheduledMessage) error {
	s.Text = strings.TrimSpace(s.Text)
	if s.Text == "" || jsLength(s.Text) > 20000 {
		return problem(400, "Teks pesan terjadwal wajib diisi, maksimal 20.000 karakter.")
	}
	if a.data.findRoom(s.RoomID) == nil {
		return problem(400, "Ruang tidak ditemukan.")
	}
	if s.Recur == "" {
		if s.SendAt == "" {
			return problem(400, "Waktu kirim wajib diisi untuk pesan sekali kirim.")
		}
		at, err := parseScheduleTime(s.SendAt)
		if err != nil {
			return problem(400, "Waktu kirim tidak valid.")
		}
		s.NextAt = formatScheduleTime(at)
		return nil
	}
	next, err := computeNextAt(*s, time.Now())
	if err != nil {
		return err
	}
	s.NextAt = formatScheduleTime(next)
	return nil
}

func (a *App) createScheduledLocked(input scheduledInput) (scheduledMessage, error) {
	if len(a.data.Scheduled) >= 50 {
		return scheduledMessage{}, problem(400, "Maksimal 50 pesan terjadwal.")
	}
	s := scheduledMessage{ID: uuid(), Enabled: true, CreatedAt: now()}
	if input.RoomID != nil {
		s.RoomID = *input.RoomID
	} else if room := a.data.activeRoom(); room != nil {
		s.RoomID = room.ID
	}
	if input.Text != nil {
		s.Text = *input.Text
	}
	if input.SendAt != nil {
		s.SendAt = *input.SendAt
	}
	if input.Recur != nil {
		s.Recur = strings.TrimSpace(*input.Recur)
	}
	if input.TimeOfDay != nil {
		s.TimeOfDay = strings.TrimSpace(*input.TimeOfDay)
	}
	if input.Weekday != nil {
		s.Weekday = *input.Weekday
	}
	if input.IntervalHours != nil {
		s.IntervalHours = *input.IntervalHours
	}
	if input.TzOffsetMinutes != nil {
		s.TzOffsetMinutes = *input.TzOffsetMinutes
	}
	if input.Enabled != nil {
		s.Enabled = *input.Enabled
	}
	if err := a.validateScheduledLocked(&s); err != nil {
		return scheduledMessage{}, err
	}
	s.Text = a.redactLocked(s.Text)
	previous := a.data.Scheduled
	a.data.Scheduled = append(a.data.Scheduled, s)
	if err := a.saveLocked(); err != nil {
		a.data.Scheduled = previous
		return scheduledMessage{}, err
	}
	return s, nil
}

func (a *App) updateScheduledLocked(id string, input scheduledInput) (scheduledMessage, error) {
	for i := range a.data.Scheduled {
		if a.data.Scheduled[i].ID != id {
			continue
		}
		s := a.data.Scheduled[i]
		if input.Text != nil {
			s.Text = *input.Text
		}
		if input.RoomID != nil {
			s.RoomID = *input.RoomID
		}
		if input.SendAt != nil {
			s.SendAt = *input.SendAt
		}
		if input.Recur != nil {
			s.Recur = strings.TrimSpace(*input.Recur)
		}
		if input.TimeOfDay != nil {
			s.TimeOfDay = strings.TrimSpace(*input.TimeOfDay)
		}
		if input.Weekday != nil {
			s.Weekday = *input.Weekday
		}
		if input.IntervalHours != nil {
			s.IntervalHours = *input.IntervalHours
		}
		if input.TzOffsetMinutes != nil {
			s.TzOffsetMinutes = *input.TzOffsetMinutes
		}
		if input.Enabled != nil {
			s.Enabled = *input.Enabled
		}
		if err := a.validateScheduledLocked(&s); err != nil {
			return scheduledMessage{}, err
		}
		s.Text = a.redactLocked(s.Text)
		previous := a.data.Scheduled[i]
		a.data.Scheduled[i] = s
		if err := a.saveLocked(); err != nil {
			a.data.Scheduled[i] = previous
			return scheduledMessage{}, err
		}
		return s, nil
	}
	return scheduledMessage{}, problem(404, "Pesan terjadwal tidak ditemukan.")
}

func (a *App) deleteScheduledLocked(id string) error {
	for i := range a.data.Scheduled {
		if a.data.Scheduled[i].ID == id {
			previous := a.data.Scheduled
			a.data.Scheduled = append(a.data.Scheduled[:i], a.data.Scheduled[i+1:]...)
			if err := a.saveLocked(); err != nil {
				a.data.Scheduled = previous
				return err
			}
			return nil
		}
	}
	return problem(404, "Pesan terjadwal tidak ditemukan.")
}

// runSchedulesLocked mengirim pesan terjadwal yang sudah jatuh tempo.
// Dipanggil dari tickLocked (mutex sudah terkunci).
func (a *App) runSchedulesLocked() {
	if len(a.data.Scheduled) == 0 {
		return
	}
	nowTime := time.Now()
	changed := false
	kept := a.data.Scheduled[:0]
	for _, s := range a.data.Scheduled {
		if !s.Enabled || s.NextAt == "" {
			kept = append(kept, s)
			continue
		}
		next, err := parseScheduleTime(s.NextAt)
		if err != nil || next.After(nowTime) {
			kept = append(kept, s)
			continue
		}
		// Jatuh tempo: kirim sebagai pesan pemilik di ruangnya.
		if _, err := a.submitLocked(s.Text, "", "", nil, s.RoomID, false); err != nil {
			kept = append(kept, s) // coba lagi pada tick berikutnya
			continue
		}
		changed = true
		s.LastSentAt = now()
		if s.Recur == "" {
			continue // sekali kirim: buang dari daftar
		}
		if nextAt, err := computeNextAt(s, nowTime); err == nil {
			s.NextAt = formatScheduleTime(nextAt)
		}
		kept = append(kept, s)
	}
	a.data.Scheduled = kept
	if changed {
		a.saveJobLocked()
	}
}
