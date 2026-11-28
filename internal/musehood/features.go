package musehood

import (
	"encoding/json"
	"io"
	"net/http"
	"regexp"
	"slices"
	"strings"
)

// --- Ruang (multi-room) ---

type roomInput struct {
	Name          *string   `json:"name"`
	SeatCodes     *[]string `json:"seatCodes"`
	SeatOrder     *[]string `json:"seatOrder"`
	Pinned        *bool     `json:"pinned"`
	Icon          *string   `json:"icon"`
	Color         *string   `json:"color"`
	DisabledTools *[]string `json:"disabledTools"`
	Goal          *string   `json:"goal"`
	PlanMode      *bool     `json:"planMode"`
	VibeMode      *bool     `json:"vibeMode"`
}

var roomColorPattern = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

// validateRoomIconColor memvalidasi ikon (maks 8 rune) dan warna
// (#rrggbb) ruang (fitur 19).
func validateRoomIconColor(icon, color string) error {
	if len([]rune(strings.TrimSpace(icon))) > 8 {
		return problem(400, "Ikon ruang maksimal 8 karakter.")
	}
	if color != "" && !roomColorPattern.MatchString(color) {
		return problem(400, "Warna ruang harus berformat #rrggbb.")
	}
	return nil
}

// validateRoomDisabledTools memvalidasi daftar tool nonaktif ruang
// (fitur 25).
func validateRoomDisabledTools(tools []string) ([]string, error) {
	if len(tools) > 60 {
		return nil, problem(400, "Terlalu banyak tool dinonaktifkan.")
	}
	out := []string{}
	for _, name := range tools {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if !customToolNamePattern.MatchString(name) {
			return nil, problem(400, "Nama tool tidak valid.")
		}
		out = append(out, name)
	}
	return out, nil
}

func (a *App) roomViewsLocked() []map[string]any {
	views := []map[string]any{}
	for _, r := range a.data.Rooms {
		count := 0
		branchCounts := map[string]int{}
		branchOrder := []string{}
		for _, m := range a.data.Messages {
			if m.RoomID == r.ID {
				count++
				if branchCounts[m.branchID()] == 0 {
					branchOrder = append(branchOrder, m.branchID())
				}
				branchCounts[m.branchID()]++
			}
		}
		branches := make([]map[string]any, 0, len(branchOrder))
		for _, id := range branchOrder {
			branches = append(branches, map[string]any{"id": id, "messageCount": branchCounts[id]})
		}
		codes := r.SeatCodes
		if codes == nil {
			codes = []string{}
		}
		order := r.SeatOrder
		if order == nil {
			order = []string{}
		}
		participants := r.Participants
		if participants == nil {
			participants = []string{}
		}
		views = append(views, map[string]any{"id": r.ID, "name": r.Name, "createdAt": r.CreatedAt, "seatCodes": codes, "seatOrder": order, "messageCount": count, "active": r.ID == a.data.ActiveRoomID, "pinned": r.Pinned, "archived": r.Archived, "summary": r.Summary, "summaryCount": r.SummaryCount, "activeBranch": r.activeBranch(), "branches": branches, "icon": r.Icon, "color": r.Color, "disabledTools": r.DisabledTools, "participants": participants, "todos": roomTodosView(r), "checkpoints": roomCheckpointsView(r), "goal": r.Goal, "planMode": r.PlanMode, "vibeMode": r.VibeMode})
	}
	return views
}

func validateRoomName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || jsLength(name) > 80 || badName.MatchString(name) {
		return "", problem(400, "Nama ruang wajib diisi, maksimal 80 karakter, tanpa @ atau baris baru.")
	}
	return name, nil
}

func (a *App) validateSeatCodesLocked(codes []string) ([]string, error) {
	out := []string{}
	for _, code := range codes {
		if a.data.findSeat(code) == nil {
			return nil, problem(400, "Kode kursi ruang tidak dikenal: "+code)
		}
		if !slices.Contains(out, code) {
			out = append(out, code)
		}
	}
	return out, nil
}

func (a *App) createRoomLocked(input roomInput) (any, error) {
	if input.Name == nil {
		return nil, problem(400, "Nama ruang wajib diisi.")
	}
	name, err := validateRoomName(*input.Name)
	if err != nil {
		return nil, err
	}
	codes := []string{}
	if input.SeatCodes != nil {
		codes, err = a.validateSeatCodesLocked(*input.SeatCodes)
		if err != nil {
			return nil, err
		}
	}
	order := []string{}
	if input.SeatOrder != nil {
		order, err = a.validateSeatCodesLocked(*input.SeatOrder)
		if err != nil {
			return nil, err
		}
	}
	icon, color := "", ""
	if input.Icon != nil {
		icon = strings.TrimSpace(*input.Icon)
	}
	if input.Color != nil {
		color = strings.TrimSpace(*input.Color)
	}
	if err := validateRoomIconColor(icon, color); err != nil {
		return nil, err
	}
	disabledTools := []string{}
	if input.DisabledTools != nil {
		disabledTools, err = validateRoomDisabledTools(*input.DisabledTools)
		if err != nil {
			return nil, err
		}
	}
	r := room{ID: newRoomID(), Name: name, CreatedAt: now(), SeatCodes: codes, SeatOrder: order, Icon: icon, Color: color, DisabledTools: disabledTools}
	previous := a.data
	a.data.Rooms = append(a.data.Rooms, r)
	a.data.ActiveRoomID = r.ID
	if err := a.saveLocked(); err != nil {
		a.data = previous
		return nil, err
	}
	return map[string]any{"rooms": a.roomViewsLocked(), "activeRoomId": a.data.ActiveRoomID}, nil
}

func (a *App) updateRoomLocked(id string, input roomInput) (any, error) {
	r := a.data.findRoom(id)
	if r == nil {
		return nil, problem(404, "Ruang tidak ditemukan.")
	}
	previous := a.data
	next := *r
	if input.Name != nil {
		name, err := validateRoomName(*input.Name)
		if err != nil {
			return nil, err
		}
		next.Name = name
	}
	if input.SeatCodes != nil {
		codes, err := a.validateSeatCodesLocked(*input.SeatCodes)
		if err != nil {
			return nil, err
		}
		next.SeatCodes = codes
	}
	if input.SeatOrder != nil {
		order, err := a.validateSeatCodesLocked(*input.SeatOrder)
		if err != nil {
			return nil, err
		}
		next.SeatOrder = order
	}
	if input.Pinned != nil {
		next.Pinned = *input.Pinned
	}
	if input.Icon != nil {
		next.Icon = strings.TrimSpace(*input.Icon)
	}
	if input.Color != nil {
		next.Color = strings.TrimSpace(*input.Color)
	}
	if err := validateRoomIconColor(next.Icon, next.Color); err != nil {
		return nil, err
	}
	if input.DisabledTools != nil {
		tools, err := validateRoomDisabledTools(*input.DisabledTools)
		if err != nil {
			return nil, err
		}
		next.DisabledTools = tools
	}
	if input.Goal != nil {
		goal := strings.TrimSpace(*input.Goal)
		if jsLength(goal) > 2000 || strings.Contains(goal, "\x00") {
			return nil, problem(400, "Objektif ruang maksimal 2000 karakter.")
		}
		next.Goal = goal
	}
	if input.PlanMode != nil {
		next.PlanMode = *input.PlanMode
	}
	if input.VibeMode != nil {
		next.VibeMode = *input.VibeMode
	}
	*r = next
	if err := a.saveLocked(); err != nil {
		a.data = previous
		return nil, err
	}
	return map[string]any{"rooms": a.roomViewsLocked(), "activeRoomId": a.data.ActiveRoomID}, nil
}

func (a *App) activateRoomLocked(id string) (any, error) {
	r := a.data.findRoom(id)
	if r == nil {
		return nil, problem(404, "Ruang tidak ditemukan.")
	}
	previous := a.data
	a.data.ActiveRoomID = id
	// Mengaktifkan ruang arsip memulihkannya agar tetap terlihat di bilah ruang.
	r.Archived = false
	if err := a.saveLocked(); err != nil {
		a.data = previous
		return nil, err
	}
	return a.stateLocked(), nil
}

// archiveRoomLocked mengarsipkan atau memulihkan ruang (fitur 18).
// Ruang aktif tidak dapat diarsipkan.
func (a *App) archiveRoomLocked(id string, archived bool) (any, error) {
	r := a.data.findRoom(id)
	if r == nil {
		return nil, problem(404, "Ruang tidak ditemukan.")
	}
	if archived && id == a.data.ActiveRoomID {
		return nil, problem(400, "Ruang aktif tidak dapat diarsipkan.")
	}
	previous := a.data
	r.Archived = archived
	if err := a.saveLocked(); err != nil {
		a.data = previous
		return nil, err
	}
	return map[string]any{"rooms": a.roomViewsLocked(), "activeRoomId": a.data.ActiveRoomID}, nil
}

func (a *App) deleteRoomLocked(id string) (any, error) {
	if a.data.findRoom(id) == nil {
		return nil, problem(404, "Ruang tidak ditemukan.")
	}
	if len(a.data.Rooms) <= 1 {
		return nil, problem(400, "Ruang terakhir tidak dapat dihapus.")
	}
	previous := a.data
	queueIDs := map[string]bool{}
	for _, j := range a.data.Jobs {
		if j.RoomID != id {
			continue
		}
		if cancel, ok := a.running[j]; ok {
			cancel()
			delete(a.running, j)
		}
		for _, s := range j.Steps {
			if s.QueueID != "" {
				queueIDs[s.QueueID] = true
			}
		}
	}
	rooms := make([]room, 0, len(a.data.Rooms)-1)
	for _, other := range a.data.Rooms {
		if other.ID != id {
			rooms = append(rooms, other)
		}
	}
	messages := make([]*message, 0, len(a.data.Messages))
	for _, m := range a.data.Messages {
		if m.RoomID != id {
			messages = append(messages, m)
		}
	}
	jobs := make([]*job, 0, len(a.data.Jobs))
	for _, j := range a.data.Jobs {
		if j.RoomID != id {
			jobs = append(jobs, j)
		}
	}
	a.data.Rooms = rooms
	a.data.Messages = messages
	a.data.Jobs = jobs
	if a.data.Task != nil && a.data.Task.RoomID == id {
		a.data.Task = nil
	}
	if a.data.ActiveRoomID == id {
		a.data.ActiveRoomID = rooms[0].ID
	}
	if err := a.saveLocked(); err != nil {
		a.data = previous
		return nil, err
	}
	if err := a.cancelQueueIDsLocked(queueIDs); err != nil {
		return nil, err
	}
	return map[string]any{"rooms": a.roomViewsLocked(), "activeRoomId": a.data.ActiveRoomID}, nil
}

// --- Preset kursi ---

func (a *App) createPresetLocked(name string) (any, error) {
	name = strings.TrimSpace(name)
	if name == "" || jsLength(name) > 80 || badName.MatchString(name) {
		return nil, problem(400, "Nama preset wajib diisi, maksimal 80 karakter, tanpa @ atau baris baru.")
	}
	p := seatPreset{ID: newPresetID(), Name: name, CreatedAt: now()}
	for _, s := range a.data.Seats {
		ps := presetSeat{Code: s.Code, Name: s.Name, Role: s.Role, Provider: s.Provider, Prompt: s.Prompt, FallbackModel: s.FallbackModel, FallbackModels: s.FallbackModels, DisabledTools: s.DisabledTools, Memory: s.Memory, Skills: s.Skills, RolePreset: s.RolePreset}
		if s.Model != nil {
			model := *s.Model
			ps.Model = &model
		}
		p.Seats = append(p.Seats, ps)
	}
	previous := a.data
	a.data.Presets = append(a.data.Presets, p)
	if err := a.saveLocked(); err != nil {
		a.data = previous
		return nil, err
	}
	return p, nil
}

func (a *App) applyPresetLocked(id string) (any, error) {
	var preset *seatPreset
	for i := range a.data.Presets {
		if a.data.Presets[i].ID == id {
			preset = &a.data.Presets[i]
			break
		}
	}
	if preset == nil {
		return nil, problem(404, "Preset tidak ditemukan.")
	}
	next := a.data
	next.Seats = slices.Clone(next.Seats)
	applied := 0
	for _, ps := range preset.Seats {
		s := next.findSeat(ps.Code)
		if s == nil {
			continue
		}
		s.Name = ps.Name
		s.Role = ps.Role
		if ps.Provider == "muse" || ps.Provider == "gateway" {
			s.Provider = ps.Provider
		}
		if ps.Model != nil {
			model := *ps.Model
			s.Model = &model
		} else if s.Provider == "gateway" && s.Model == nil {
			s.Model = ptr("")
		}
		s.Prompt = ps.Prompt
		s.FallbackModel = ps.FallbackModel
		s.FallbackModels = ps.FallbackModels
		s.DisabledTools = ps.DisabledTools
		s.Memory = ps.Memory
		s.Skills = ps.Skills
		s.RolePreset = ps.RolePreset
		applied++
	}
	seen := map[string]bool{}
	for _, s := range next.Seats {
		name := strings.ToLower(s.Name)
		if seen[name] {
			return nil, problem(400, "Preset membuat nama kursi tidak unik; preset tidak diterapkan.")
		}
		seen[name] = true
	}
	if err := a.persistLocked(next); err != nil {
		return nil, err
	}
	a.data = next
	return map[string]any{"applied": applied, "state": a.stateLocked()}, nil
}

func (a *App) deletePresetLocked(id string) (any, error) {
	found := false
	kept := make([]seatPreset, 0, len(a.data.Presets))
	for _, p := range a.data.Presets {
		if p.ID == id {
			found = true
			continue
		}
		kept = append(kept, p)
	}
	if !found {
		return nil, problem(404, "Preset tidak ditemukan.")
	}
	previous := a.data
	a.data.Presets = kept
	if err := a.saveLocked(); err != nil {
		a.data = previous
		return nil, err
	}
	return map[string]bool{"ok": true}, nil
}

// --- Pencarian lintas ruang ---

type searchFilters struct {
	Seat            string // "" semua, "owner", atau kode kursi
	Room            string // "" semua ruang
	From            string // YYYY-MM-DD inklusif
	To              string // YYYY-MM-DD inklusif
	HasImage        bool
	IncludeArchived bool   // sertakan ruang arsip (fitur 18)
	Tag             string // filter tag pesan (fitur 17)
}

func (f searchFilters) active() bool {
	return f.Seat != "" || f.Room != "" || f.From != "" || f.To != "" || f.HasImage || f.IncludeArchived || f.Tag != ""
}

func (a *App) searchLocked(q string, f searchFilters) any {
	q = strings.ToLower(strings.TrimSpace(q))
	results := []map[string]any{}
	if q == "" && !f.active() {
		return map[string]any{"results": results}
	}
	// Indeks full-text (fitur 35): kandidat dari indeks digabung dengan
	// kecocokan frasa substring agar perilaku lama tidak hilang.
	var candidates map[string]bool
	indexApplies := false
	if q != "" {
		candidates, indexApplies = a.ftCandidatesLocked(q)
	}
	for _, m := range a.data.Messages {
		if q != "" {
			phrase := strings.Contains(strings.ToLower(m.Text), q)
			if indexApplies {
				if !candidates[m.ID] && !phrase {
					continue
				}
			} else if !phrase {
				continue
			}
		}
		if f.Seat == "owner" && m.AuthorType != "owner" {
			continue
		}
		if f.Seat != "" && f.Seat != "owner" && m.Seat != f.Seat {
			continue
		}
		if f.Room != "" && m.RoomID != f.Room {
			continue
		}
		date := m.CreatedAt
		if len(date) > 10 {
			date = date[:10]
		}
		if f.From != "" && date < f.From {
			continue
		}
		if f.To != "" && date > f.To {
			continue
		}
		if f.HasImage && m.ImageURL == "" && (m.Attachment == nil || !strings.HasPrefix(m.Attachment.Mime, "image/")) {
			continue
		}
		if f.Tag != "" {
			found := false
			for _, tag := range m.Tags {
				if strings.EqualFold(tag, f.Tag) {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}
		if !f.IncludeArchived {
			if r := a.data.findRoom(m.RoomID); r != nil && r.Archived {
				continue
			}
		}
		roomName := ""
		if r := a.data.findRoom(m.RoomID); r != nil {
			roomName = r.Name
		}
		author := m.Seat
		if m.AuthorType == "owner" {
			author = a.data.Owner.Name
		} else if s := a.data.findSeat(m.Seat); s != nil {
			author = s.Name
		}
		results = append(results, map[string]any{"id": m.ID, "roomId": m.RoomID, "roomName": roomName, "author": author, "text": m.Text, "createdAt": m.CreatedAt})
		if len(results) >= 100 {
			break
		}
	}
	return map[string]any{"results": results}
}

// --- Cadangkan & pulihkan ---

// backup mengunduh seluruh data (ruang, kursi, provider, pesan, preset,
// statistik) sebagai JSON. Kunci API tidak pernah ikut: semua key dikosongkan.
func (a *App) backup(w http.ResponseWriter, r *http.Request) {
	if err := method(w, r, http.MethodGet); err != nil {
		a.writeJSON(w, r, 0, nil, err)
		return
	}
	a.mu.Lock()
	d := a.data
	d.Providers = slices.Clone(d.Providers)
	for i := range d.Providers {
		d.Providers[i].APIKey = ""
	}
	d.Settings.APIKey = ""
	d.Settings.PINHash = ""
	d.Settings.APIToken = ""
	d.Settings.TelegramBotToken = ""
	d.Settings.GitHubToken = ""
	// Nilai env server MCP tidak ikut cadangan (hanya nama kuncinya).
	if len(d.Settings.MCPServers) > 0 {
		servers := slices.Clone(d.Settings.MCPServers)
		for i := range servers {
			if len(servers[i].Env) > 0 {
				env := make(map[string]string, len(servers[i].Env))
				for key := range servers[i].Env {
					env[key] = ""
				}
				servers[i].Env = env
			}
		}
		d.Settings.MCPServers = servers
	}
	d.WorkerToken = ""
	raw, err := json.MarshalIndent(a.safeLocked(d), "", "  ")
	a.mu.Unlock()
	if err != nil {
		a.writeJSON(w, r, 0, nil, problem(500, "Gagal menyusun cadangan."))
		return
	}
	w.Header().Set("Content-Disposition", `attachment; filename="cadangan-musehood.json"`)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	// Content-Encoding gzip transparan bagi browser: berkas yang
	// disimpan tetap JSON utuh, hanya transfernya yang lebih kecil.
	w.Write(gzipJSONBody(w, r, append(raw, '\n')))
}

// restore mengganti seluruh data dengan isi file cadangan. Kunci API yang
// sudah tersimpan dipertahankan untuk provider dengan ID yang sama, karena
// file cadangan tidak memuat kunci.
func (a *App) restore(w http.ResponseWriter, r *http.Request) {
	if err := method(w, r, http.MethodPost); err != nil {
		a.writeJSON(w, r, 0, nil, err)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 32*1024*1024)
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		a.writeJSON(w, r, 0, nil, problem(400, "Isi cadangan tidak dapat dibaca."))
		return
	}
	var incoming data
	if json.Unmarshal(raw, &incoming) != nil || len(incoming.Seats) < 7 || incoming.Messages == nil {
		a.writeJSON(w, r, 0, nil, problem(400, "File cadangan tidak valid."))
		return
	}
	for i, def := range defaultSeats {
		if incoming.Seats[i].Code != def.code {
			a.writeJSON(w, r, 0, nil, problem(400, "Urutan kursi cadangan tidak valid."))
			return
		}
	}
	for _, m := range incoming.Messages {
		if m == nil {
			a.writeJSON(w, r, 0, nil, problem(400, "Pesan cadangan tidak valid."))
			return
		}
	}
	for _, j := range incoming.Jobs {
		if j == nil || j.Index < 0 || j.Index > len(j.Steps) {
			a.writeJSON(w, r, 0, nil, problem(400, "Pekerjaan cadangan tidak valid."))
			return
		}
	}
	a.mu.Lock()
	for i := range incoming.Providers {
		if incoming.Providers[i].APIKey == "" {
			if old := a.data.findProvider(incoming.Providers[i].ID); old != nil {
				incoming.Providers[i].APIKey = old.APIKey
			}
		}
	}
	if incoming.Settings.APIKey == "" {
		if old := a.data.findProvider(incoming.Settings.ActiveProviderID); old != nil {
			incoming.Settings.APIKey = old.APIKey
		}
	}
	// Token GitHub tidak ikut cadangan; yang tersimpan di mesin ini
	// dipertahankan bila cadangan tidak membawanya.
	if incoming.Settings.GitHubToken == "" {
		incoming.Settings.GitHubToken = a.data.Settings.GitHubToken
	}
	// Nilai env server MCP yang kosong di cadangan diisi ulang dari
	// server bernama sama yang sudah tersimpan di mesin ini.
	for i := range incoming.Settings.MCPServers {
		for key, value := range incoming.Settings.MCPServers[i].Env {
			if value != "" {
				continue
			}
			for _, old := range a.data.Settings.MCPServers {
				if old.Name == incoming.Settings.MCPServers[i].Name {
					if oldValue, ok := old.Env[key]; ok {
						incoming.Settings.MCPServers[i].Env[key] = oldValue
					}
				}
			}
		}
	}
	// PIN dan token pekerja tidak ikut cadangan (backup selalu
	// mengosongkannya); yang aktif di mesin ini selalu dipertahankan agar
	// file restore yang disusun manual tidak bisa mengunci pemilik keluar
	// atau mengganti token pekerja.
	incoming.Settings.PINHash = a.data.Settings.PINHash
	incoming.WorkerToken = a.data.WorkerToken
	incoming.migrateRooms()
	incoming.migrateProviders()
	// Server MCP dari cadangan disaring: hanya konfigurasi valid dan
	// maksimal maxMCPServers yang dipertahankan.
	{
		valid := incoming.Settings.MCPServers[:0]
		seen := map[string]bool{}
		for _, server := range incoming.Settings.MCPServers {
			if validateMCPServer(server) != nil || seen[server.Name] {
				continue
			}
			seen[server.Name] = true
			valid = append(valid, server)
			if len(valid) >= maxMCPServers {
				break
			}
		}
		incoming.Settings.MCPServers = valid
	}
	// Pekerjaan yang belum selesai dari cadangan tidak punya file antrean di
	// mesin ini; reset langkah aktifnya agar dijalankan ulang, bukan macet.
	for _, j := range incoming.Jobs {
		if j == nil || j.Status == "completed" || j.Index >= len(j.Steps) {
			continue
		}
		s := &j.Steps[j.Index]
		if s.QueueID != "" {
			s.QueueID = ""
			s.QueuedAt = ""
			s.Status = "pending"
			for _, m := range incoming.Messages {
				if m.ID == s.MessageID {
					m.QueueID = ""
					m.Status = "typing"
					m.StatusText = "Menunggu dijalankan ulang setelah pemulihan…"
				}
			}
		}
	}
	for j, cancel := range a.running {
		cancel()
		delete(a.running, j)
	}
	previous := a.data
	a.data = incoming
	if err := a.saveLocked(); err != nil {
		a.data = previous
		a.mu.Unlock()
		a.writeJSON(w, r, 0, nil, err)
		return
	}
	a.rememberKeyLocked(a.data.Settings.APIKey)
	for _, p := range a.data.Providers {
		a.rememberKeyLocked(p.APIKey)
	}
	for _, server := range a.data.Settings.MCPServers {
		for _, value := range server.Env {
			a.rememberKeyLocked(value)
		}
	}
	_ = a.cancelQueueLocked()
	a.tickLocked()
	a.mu.Unlock()
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(200)
	w.Write([]byte("{\"ok\":true}\n"))
}
