package musehood

import (
	"fmt"
	"strconv"
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
	// Repeat adalah pola mini-cron 5 kolom (menit jam tgl bulan
	// hari-minggu) untuk pengulangan fleksibel (fitur 18). Bila terisi,
	// ia mengalahkan Recur.
	Repeat          string `json:"repeat,omitempty"`
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
	Repeat          *string `json:"repeat"`
	TzOffsetMinutes *int    `json:"tzOffsetMinutes"`
	Enabled         *bool   `json:"enabled"`
}

func parseScheduleTime(value string) (time.Time, error) {
	return time.Parse(time.RFC3339Nano, strings.TrimSpace(value))
}

func formatScheduleTime(t time.Time) string {
	return t.UTC().Format("2006-01-02T15:04:05.000Z")
}

// --- Mini-cron (fitur 18) ---

// cronField adalah satu kolom pola cron yang sudah diuraikan.
type cronField struct {
	star   bool
	values map[int]bool
}

func parseCronField(raw string, min, max int, sundaySeven bool) (cronField, error) {
	field := cronField{values: map[int]bool{}}
	for _, item := range strings.Split(raw, ",") {
		item = strings.TrimSpace(item)
		if item == "*" {
			field.star = true
			continue
		}
		if strings.HasPrefix(item, "*/") {
			step, err := strconv.Atoi(strings.TrimPrefix(item, "*/"))
			if err != nil || step < 1 {
				return field, problem(400, "Langkah cron tidak valid: "+item)
			}
			for v := min; v <= max; v += step {
				field.values[v] = true
			}
			continue
		}
		value, err := strconv.Atoi(item)
		if err != nil {
			return field, problem(400, "Nilai cron tidak valid: "+item)
		}
		if sundaySeven && value == 7 {
			value = 0
		}
		if value < min || value > max {
			return field, problem(400, fmt.Sprintf("Nilai cron %d di luar rentang %d-%d.", value, min, max))
		}
		field.values[value] = true
	}
	return field, nil
}

func (f cronField) matches(value int) bool {
	return f.star || f.values[value]
}

// cronSchedule adalah pola mini-cron 5 kolom yang sudah diuraikan.
type cronSchedule struct {
	minute, hour, dayOfMonth, month, dayOfWeek cronField
}

func parseCron(repeat string) (cronSchedule, error) {
	parts := strings.Fields(strings.TrimSpace(repeat))
	if len(parts) != 5 {
		return cronSchedule{}, problem(400, "Pola cron harus 5 kolom: menit jam tanggal bulan hari-minggu.")
	}
	var err error
	var sched cronSchedule
	if sched.minute, err = parseCronField(parts[0], 0, 59, false); err != nil {
		return sched, err
	}
	if sched.hour, err = parseCronField(parts[1], 0, 23, false); err != nil {
		return sched, err
	}
	if sched.dayOfMonth, err = parseCronField(parts[2], 1, 31, false); err != nil {
		return sched, err
	}
	if sched.month, err = parseCronField(parts[3], 1, 12, false); err != nil {
		return sched, err
	}
	if sched.dayOfWeek, err = parseCronField(parts[4], 0, 6, true); err != nil {
		return sched, err
	}
	return sched, nil
}

func (c cronSchedule) matches(t time.Time) bool {
	if !c.minute.matches(t.Minute()) || !c.hour.matches(t.Hour()) || !c.month.matches(int(t.Month())) {
		return false
	}
	domRestricted := !c.dayOfMonth.star
	dowRestricted := !c.dayOfWeek.star
	domOK := c.dayOfMonth.matches(t.Day())
	dowOK := c.dayOfWeek.matches(int(t.Weekday()))
	// Semantik cron standar: bila tanggal DAN hari-minggu sama-sama
	// dibatasi, salah satu yang cocok sudah cukup (OR).
	if domRestricted && dowRestricted {
		return domOK || dowOK
	}
	return domOK && dowOK
}

// nextCronOccurrence mencari kemunculan berikutnya setelah from (menit
// berikutnya ke atas) dalam zona jadwal, maks ~4 tahun pencarian.
func nextCronOccurrence(sched cronSchedule, from time.Time, zone *time.Location) (time.Time, error) {
	candidate := from.In(zone).Truncate(time.Minute).Add(time.Minute)
	limit := candidate.AddDate(4, 0, 0)
	for candidate.Before(limit) {
		if sched.matches(candidate) {
			return candidate, nil
		}
		candidate = candidate.Add(time.Minute)
	}
	return time.Time{}, problem(400, "Pola cron tidak punya kemunculan dalam 4 tahun ke depan.")
}

// computeNextAt menghitung waktu kirim berikutnya (UTC) untuk jadwal berulang.
func computeNextAt(s scheduledMessage, from time.Time) (time.Time, error) {
	zone := time.FixedZone("", s.TzOffsetMinutes*60)
	if strings.TrimSpace(s.Repeat) != "" {
		sched, err := parseCron(s.Repeat)
		if err != nil {
			return time.Time{}, err
		}
		return nextCronOccurrence(sched, from, zone)
	}
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
	if s.Recur == "" && strings.TrimSpace(s.Repeat) == "" {
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
	if input.Repeat != nil {
		s.Repeat = strings.TrimSpace(*input.Repeat)
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
		if input.Repeat != nil {
			s.Repeat = strings.TrimSpace(*input.Repeat)
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
		if _, err := a.submitLocked(s.Text, "", "", nil, s.RoomID, "", false); err != nil {
			kept = append(kept, s) // coba lagi pada tick berikutnya
			continue
		}
		changed = true
		s.LastSentAt = now()
		if s.Recur == "" && strings.TrimSpace(s.Repeat) == "" {
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
