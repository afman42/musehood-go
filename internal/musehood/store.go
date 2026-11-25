package musehood

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf16"
)

type identity struct {
	Name   string `json:"name"`
	Avatar string `json:"avatar"`
}
type owner struct {
	identity
	Present bool `json:"present"`
}
type seat struct {
	identity
	Code          string  `json:"code"`
	Role          string  `json:"role"`
	Provider      string  `json:"provider"`
	Model         *string `json:"model,omitempty"`
	Prompt        string  `json:"prompt,omitempty"`
	DefaultPrompt string  `json:"defaultPrompt,omitempty"`
	Present       bool    `json:"present"`
}
type settings struct {
	BaseURL          string `json:"baseUrl"`
	APIKey           string `json:"apiKey"`
	ProviderName     string `json:"providerName"`
	GatewayProtocol  string `json:"gatewayProtocol"`
	ActiveProviderID string `json:"activeProviderId"`
	RoundRobin       bool   `json:"roundRobin"`
	ReviewLevel      string `json:"reviewLevel"`
	DemoFallback     bool   `json:"demoFallback"`
	DarkMode         bool   `json:"darkMode"`
	// IntegratorSeat adalah kode kursi yang menutup pipeline "Tugas:"
	// (Hasil Akhir - Integrasi) dan menjawab "Tanya Progres". Kosong = "MUSE".
	IntegratorSeat string `json:"integratorSeat,omitempty"`
}

// gatewayProvider adalah satu konfigurasi gateway kustom yang tersimpan.
// APIKey tidak pernah dikirim ke klien apa adanya; daftar yang dibaca klien
// memakai gatewayProviderView di bawah.
type gatewayProvider struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Alias    string `json:"alias,omitempty"`
	BaseURL  string `json:"baseUrl"`
	APIKey   string `json:"apiKey"`
	Protocol string `json:"protocol"`
}

// gatewayProviderView adalah tampilan aman satu provider untuk klien.
type gatewayProviderView struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Alias     string `json:"alias,omitempty"`
	BaseURL   string `json:"baseUrl"`
	Protocol  string `json:"protocol"`
	HasAPIKey bool   `json:"hasApiKey"`
	Active    bool   `json:"active"`
}
type message struct {
	ID         string      `json:"id"`
	CreatedAt  string      `json:"createdAt"`
	AuthorType string      `json:"authorType"`
	Seat       string      `json:"seat,omitempty"`
	JobID      string      `json:"jobId"`
	Text       string      `json:"text"`
	Status     string      `json:"status"`
	StatusText string      `json:"statusText,omitempty"`
	QueueID    string      `json:"queueId,omitempty"`
	Label      string      `json:"label,omitempty"`
	Error      string      `json:"error,omitempty"`
	ImageURL   string      `json:"imageUrl,omitempty"`
	Attachment *attachment `json:"attachment,omitempty"`
	Demo       bool        `json:"demo,omitempty"`
	ReplyTo    string      `json:"replyTo,omitempty"`
	Reactions  []reaction  `json:"reactions,omitempty"`
	EditedAt   string      `json:"editedAt,omitempty"`
}
type reaction struct {
	Emoji   string `json:"emoji"`
	Count   int    `json:"count"`
	ByOwner bool   `json:"byOwner"`
}
type attachment struct {
	URL  string `json:"url"`
	Name string `json:"name"`
	Size int64  `json:"size"`
	Mime string `json:"mime"`
}
type step struct {
	Seat      string `json:"seat"`
	Kind      string `json:"kind"`
	Status    string `json:"status"`
	MessageID string `json:"messageId,omitempty"`
	QueueID   string `json:"queueId,omitempty"`
	QueuedAt  string `json:"queuedAt,omitempty"`
}
type job struct {
	ID          string `json:"id"`
	Prompt      string `json:"prompt"`
	Kind        string `json:"kind"`
	CreatedAt   string `json:"createdAt"`
	CompletedAt string `json:"completedAt,omitempty"`
	Status      string `json:"status"`
	Index       int    `json:"index"`
	Steps       []step `json:"steps"`
}
type activeTask struct {
	JobID         string  `json:"jobId"`
	Title         string  `json:"title"`
	LastCompleted *string `json:"lastCompleted"`
	CurrentStep   *string `json:"currentStep"`
	Status        string  `json:"status"`
	CreatedAt     string  `json:"createdAt"`
	CompletedAt   string  `json:"completedAt,omitempty"`
}
type connection struct {
	Models   []string `json:"models"`
	TestedAt string   `json:"testedAt"`
}
type data struct {
	Version    int               `json:"version"`
	Group      identity          `json:"group"`
	Owner      owner             `json:"owner"`
	Seats      []seat            `json:"seats"`
	Settings   settings          `json:"settings"`
	Providers  []gatewayProvider `json:"providers"`
	Messages   []*message        `json:"messages"`
	Jobs       []*job            `json:"jobs"`
	Task       *activeTask       `json:"activeTask"`
	Connection *connection       `json:"connection"`
}

var defaultSeats = []struct{ code, name, role string }{
	{"MUSE", "Muse", "Admin dan Integrasi"}, {"M10", "Opus", "Perencana"}, {"M01", "Plamo", "Pelaksana"},
	{"M02", "GLM", "Peninjau"}, {"M03", "Argon", "Visual"}, {"M04", "Deepseek", "Penasihat"}, {"M05", "Fable", "Analis Data"},
}
var badName = regexp.MustCompile(`[@\x00-\x1f]`)
var badSetting = regexp.MustCompile(`[\r\n\x00]`)

func ptr[T any](value T) *T { return &value }
func now() string           { return time.Now().UTC().Format("2006-01-02T15:04:05.000Z") }
func jsLength(s string) int { return len(utf16.Encode([]rune(s))) }
func uuid() string {
	var b [16]byte
	rand.Read(b[:])
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[:4], b[4:6], b[6:8], b[8:10], b[10:])
}

func loadData(dir string) (data, error) {
	var d data
	raw, err := os.ReadFile(filepath.Join(dir, "data.json"))
	if errors.Is(err, os.ErrNotExist) {
		d = data{Version: 1, Group: identity{"Musehood", "/avatars/group.svg"}, Owner: owner{identity{"Arman", "/avatars/owner.svg"}, true}, Settings: settings{RoundRobin: true, ReviewLevel: "Sedang"}, Messages: []*message{}, Jobs: []*job{}}
		for _, def := range defaultSeats {
			s := seat{identity: identity{def.name, "/avatars/" + strings.ToLower(def.code) + ".svg"}, Code: def.code, Role: def.role, Provider: "gateway", Model: ptr(""), Present: true}
			if s.Code == "MUSE" {
				s.Provider = "muse"
				s.Model = nil
			}
			d.Seats = append(d.Seats, s)
		}
		d.migrateProviders()
		return d, nil
	}
	if err != nil {
		return d, err
	}
	var structure struct {
		Settings *settings `json:"settings"`
	}
	if json.Unmarshal(raw, &structure) != nil || structure.Settings == nil || json.Unmarshal(raw, &d) != nil || len(d.Seats) < 7 || d.Messages == nil {
		return d, errors.New("data.json tidak valid; pulihkan dari salinan sebelum menjalankan server")
	}
	for i, def := range defaultSeats {
		s := &d.Seats[i]
		if s.Code != def.code {
			return d, errors.New("urutan kursi data.json tidak valid")
		}
		if strings.TrimSpace(s.Role) == "" {
			s.Role = def.role
		}
		if s.Provider != "muse" && s.Provider != "gateway" {
			if s.Code == "MUSE" {
				s.Provider = "muse"
			} else {
				s.Provider = "gateway"
			}
		}
		if s.Provider == "gateway" && s.Model == nil {
			s.Model = ptr("")
		}
	}
	// Custom seats (index 7+) must have unique codes and names; codes start with "C".
	seenCodes := map[string]bool{}
	seenNames := map[string]bool{}
	for i := range d.Seats {
		s := &d.Seats[i]
		if seenCodes[s.Code] {
			return d, errors.New("kode kursi data.json tidak unik")
		}
		seenCodes[s.Code] = true
		name := strings.ToLower(strings.TrimSpace(s.Name))
		if name == "" || seenNames[name] {
			return d, errors.New("nama kursi data.json tidak valid atau tidak unik")
		}
		seenNames[name] = true
		if i >= len(defaultSeats) {
			if !strings.HasPrefix(s.Code, "C") {
				return d, errors.New("kode kursi custom data.json harus diawali C")
			}
			if s.Provider != "muse" && s.Provider != "gateway" {
				s.Provider = "gateway"
			}
			if s.Provider == "gateway" && s.Model == nil {
				s.Model = ptr("")
			}
		}
	}
	if d.Settings.IntegratorSeat != "" && d.findSeat(d.Settings.IntegratorSeat) == nil {
		d.Settings.IntegratorSeat = ""
	}
	if d.Jobs == nil {
		d.Jobs = []*job{}
	}
	for _, m := range d.Messages {
		if m == nil {
			return d, errors.New("pesan data.json tidak valid")
		}
	}
	for _, j := range d.Jobs {
		if j == nil || j.Index < 0 || j.Index > len(j.Steps) {
			return d, errors.New("pekerjaan data.json tidak valid")
		}
		for _, s := range j.Steps {
			if j.Status != "completed" && d.findSeat(s.Seat) == nil {
				return d, errors.New("kursi pekerjaan data.json tidak valid")
			}
		}
	}
	seenProviders := map[string]bool{}
	for i := range d.Providers {
		p := &d.Providers[i]
		if p.ID == "" || seenProviders[p.ID] {
			return d, errors.New("ID provider data.json tidak valid atau tidak unik")
		}
		seenProviders[p.ID] = true
		if strings.TrimSpace(p.Name) == "" || strings.TrimSpace(p.BaseURL) == "" {
			return d, errors.New("provider data.json tidak valid")
		}
	}
	d.migrateProviders()
	return d, nil
}

// migrateProviders menaikkan konfigurasi gateway tunggal lama menjadi daftar
// provider. Dijalankan setiap kali data dimuat sehingga data.json lama tetap
// bisa dibaca; hasilnya menetap saat data berikutnya disimpan (New juga
// menyimpan saat startup).
func (d *data) migrateProviders() {
	if len(d.Providers) == 0 && strings.TrimSpace(d.Settings.BaseURL) != "" {
		name := strings.TrimSpace(d.Settings.ProviderName)
		if name == "" {
			if u, err := url.Parse(d.Settings.BaseURL); err == nil && u.Hostname() != "" {
				name = u.Hostname()
			} else {
				name = "Gateway Utama"
			}
		}
		proto := d.Settings.GatewayProtocol
		if proto == "" {
			proto = "openai"
		}
		if !validProtocols[proto] {
			proto = "openai"
		}
		d.Providers = []gatewayProvider{{ID: "prov-utama", Name: name, BaseURL: strings.TrimRight(d.Settings.BaseURL, "/"), APIKey: d.Settings.APIKey, Protocol: proto}}
		d.Settings.ActiveProviderID = "prov-utama"
	}
	for i := range d.Providers {
		if !validProtocols[d.Providers[i].Protocol] {
			d.Providers[i].Protocol = "openai"
		}
	}
	if len(d.Providers) > 0 {
		active := d.findProvider(d.Settings.ActiveProviderID)
		if active == nil {
			d.Settings.ActiveProviderID = d.Providers[0].ID
			active = &d.Providers[0]
		}
		d.syncSettingsFromProvider(*active)
	} else {
		d.Settings.ActiveProviderID = ""
		d.Settings.BaseURL = ""
		d.Settings.APIKey = ""
		d.Settings.ProviderName = ""
		d.Settings.GatewayProtocol = ""
	}
}

func (d *data) findProvider(id string) *gatewayProvider {
	for i := range d.Providers {
		if d.Providers[i].ID == id {
			return &d.Providers[i]
		}
	}
	return nil
}

// syncSettingsFromProvider menyalin konfigurasi provider aktif ke settings
// tunggal yang dipakai engine gateway, supaya jalur chat lama tidak berubah.
func (d *data) syncSettingsFromProvider(p gatewayProvider) {
	d.Settings.BaseURL = p.BaseURL
	d.Settings.APIKey = p.APIKey
	d.Settings.ProviderName = p.Name
	d.Settings.GatewayProtocol = p.Protocol
	d.Settings.ActiveProviderID = p.ID
}

// providerConfig membangun settings untuk panggilan gateway ke satu provider.
func (d *data) providerConfig(p gatewayProvider) settings {
	return settings{BaseURL: p.BaseURL, APIKey: p.APIKey, ProviderName: p.Name, GatewayProtocol: p.Protocol}
}

func (d *data) providerView(p gatewayProvider) gatewayProviderView {
	return gatewayProviderView{ID: p.ID, Name: p.Name, Alias: p.Alias, BaseURL: p.BaseURL, Protocol: p.Protocol, HasAPIKey: p.APIKey != "", Active: p.ID == d.Settings.ActiveProviderID}
}

func (d *data) providerViews() []gatewayProviderView {
	views := []gatewayProviderView{}
	for _, p := range d.Providers {
		views = append(views, d.providerView(p))
	}
	return views
}

func validateProviderName(name string) error {
	name = strings.TrimSpace(name)
	if name == "" || jsLength(name) > 80 || badName.MatchString(name) {
		return problem(400, "Nama provider wajib diisi, maksimal 80 karakter, tanpa @ atau baris baru.")
	}
	return nil
}

func normalizeProviderBaseURL(raw string) (string, error) {
	base := strings.TrimRight(strings.TrimSpace(raw), "/")
	if base == "" {
		return "", problem(400, "Base URL wajib diisi.")
	}
	u, err := url.Parse(base)
	if err != nil || u.Hostname() == "" {
		return "", problem(400, "Base URL harus URL HTTP atau HTTPS yang valid.")
	}
	if !validHTTPURL(u) || u.RawQuery != "" || u.ForceQuery || strings.Contains(base, "#") {
		return "", problem(400, "Base URL harus HTTP/HTTPS tanpa kredensial, query, atau fragmen.")
	}
	u.Host = strings.ToLower(u.Host)
	return strings.TrimRight(u.String(), "/"), nil
}

func validateProviderProtocol(proto string) error {
	if !validProtocols[proto] {
		return problem(400, "Protokol gateway harus openai, openai-responses, anthropic, gemini, atau ollama.")
	}
	return nil
}

func validateProviderAPIKey(key string) error {
	if jsLength(key) > 4096 || badSetting.MatchString(key) {
		return problem(400, "API key tidak valid.")
	}
	return nil
}

func newProviderID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return "prov-" + hex.EncodeToString(b[:])
}

type providerInput struct {
	Name        *string `json:"name"`
	Alias       *string `json:"alias"`
	BaseURL     *string `json:"baseUrl"`
	APIKey      *string `json:"apiKey"`
	Protocol    *string `json:"protocol"`
	ClearAPIKey bool    `json:"clearApiKey"`
}

// validateProviderAlias memeriksa alias provider: label pembeda singkat yang
// ditampilkan sebagai awalan nama model (mis. "xc/gpt-6.1-sol"). Kosong
// berarti tanpa alias.
func validateProviderAlias(alias string) error {
	if alias == "" {
		return nil
	}
	if jsLength(alias) > 40 || strings.ContainsAny(alias, "/@ \t\r\n\x00") || badName.MatchString(alias) {
		return problem(400, "Alias provider maksimal 40 karakter, tanpa spasi, @, atau /.")
	}
	return nil
}

func (a *App) createProviderLocked(input providerInput) (gatewayProviderView, error) {
	if input.Name == nil || input.BaseURL == nil || input.Protocol == nil {
		return gatewayProviderView{}, problem(400, "Nama, Base URL, dan protokol provider wajib diisi.")
	}
	name := strings.TrimSpace(*input.Name)
	if err := validateProviderName(name); err != nil {
		return gatewayProviderView{}, err
	}
	alias := ""
	if input.Alias != nil {
		alias = strings.TrimSpace(*input.Alias)
		if err := validateProviderAlias(alias); err != nil {
			return gatewayProviderView{}, err
		}
	}
	base, err := normalizeProviderBaseURL(*input.BaseURL)
	if err != nil {
		return gatewayProviderView{}, err
	}
	proto := strings.TrimSpace(*input.Protocol)
	if proto == "" {
		proto = "openai"
	}
	if err := validateProviderProtocol(proto); err != nil {
		return gatewayProviderView{}, err
	}
	key := ""
	if input.APIKey != nil {
		key = strings.TrimSpace(*input.APIKey)
		if err := validateProviderAPIKey(key); err != nil {
			return gatewayProviderView{}, err
		}
	}
	if key == "" && proto != "ollama" {
		return gatewayProviderView{}, problem(400, "API key wajib diisi kecuali protokol Ollama.")
	}
	p := gatewayProvider{ID: newProviderID(), Name: name, Alias: alias, BaseURL: base, APIKey: key, Protocol: proto}
	next := a.data
	next.Providers = append(slices.Clone(next.Providers), p)
	if len(next.Providers) == 1 {
		next.syncSettingsFromProvider(p)
		next.Connection = nil
	}
	a.rememberKeyLocked(key)
	if err := writeJSONFile(filepath.Join(a.dir, "data.json"), next); err != nil {
		return gatewayProviderView{}, err
	}
	a.data = next
	return next.providerView(p), nil
}

func (a *App) updateProviderLocked(id string, input providerInput) (gatewayProviderView, error) {
	next := a.data
	next.Providers = slices.Clone(next.Providers)
	p := next.findProvider(id)
	if p == nil {
		return gatewayProviderView{}, problem(404, "Provider tidak ditemukan.")
	}
	if input.Name != nil {
		name := strings.TrimSpace(*input.Name)
		if err := validateProviderName(name); err != nil {
			return gatewayProviderView{}, err
		}
		p.Name = name
	}
	if input.Alias != nil {
		alias := strings.TrimSpace(*input.Alias)
		if err := validateProviderAlias(alias); err != nil {
			return gatewayProviderView{}, err
		}
		p.Alias = alias
	}
	if input.BaseURL != nil {
		base, err := normalizeProviderBaseURL(*input.BaseURL)
		if err != nil {
			return gatewayProviderView{}, err
		}
		p.BaseURL = base
	}
	if input.Protocol != nil {
		proto := strings.TrimSpace(*input.Protocol)
		if proto == "" {
			proto = "openai"
		}
		if err := validateProviderProtocol(proto); err != nil {
			return gatewayProviderView{}, err
		}
		p.Protocol = proto
	}
	if input.ClearAPIKey {
		p.APIKey = ""
	} else if input.APIKey != nil && *input.APIKey != "" && !strings.HasPrefix(*input.APIKey, "••••") {
		key := strings.TrimSpace(*input.APIKey)
		if err := validateProviderAPIKey(key); err != nil {
			return gatewayProviderView{}, err
		}
		p.APIKey = key
	}
	if p.APIKey == "" && p.Protocol != "ollama" {
		return gatewayProviderView{}, problem(400, "API key wajib diisi kecuali protokol Ollama.")
	}
	changed := false
	if p.ID == next.Settings.ActiveProviderID {
		if next.Settings.BaseURL != p.BaseURL || next.Settings.APIKey != p.APIKey || next.Settings.ProviderName != p.Name || next.Settings.GatewayProtocol != p.Protocol {
			changed = true
		}
		next.syncSettingsFromProvider(*p)
	}
	if changed {
		next.Connection = nil
	}
	a.rememberKeyLocked(p.APIKey)
	if err := writeJSONFile(filepath.Join(a.dir, "data.json"), next); err != nil {
		return gatewayProviderView{}, err
	}
	a.data = next
	return next.providerView(*p), nil
}

func (a *App) deleteProviderLocked(id string) error {
	next := a.data
	idx := -1
	for i := range next.Providers {
		if next.Providers[i].ID == id {
			idx = i
			break
		}
	}
	if idx < 0 {
		return problem(404, "Provider tidak ditemukan.")
	}
	wasActive := next.Providers[idx].ID == next.Settings.ActiveProviderID
	kept := slices.Clone(next.Providers[:idx])
	next.Providers = append(kept, next.Providers[idx+1:]...)
	if wasActive {
		next.Connection = nil
		if len(next.Providers) > 0 {
			next.syncSettingsFromProvider(next.Providers[0])
		} else {
			next.Settings.ActiveProviderID = ""
			next.Settings.BaseURL = ""
			next.Settings.APIKey = ""
			next.Settings.ProviderName = ""
			next.Settings.GatewayProtocol = ""
		}
	}
	if err := writeJSONFile(filepath.Join(a.dir, "data.json"), next); err != nil {
		return err
	}
	a.data = next
	return nil
}

func (a *App) activateProviderLocked(id string) error {
	next := a.data
	p := next.findProvider(id)
	if p == nil {
		return problem(404, "Provider tidak ditemukan.")
	}
	if next.Settings.ActiveProviderID != p.ID {
		next.syncSettingsFromProvider(*p)
		next.Connection = nil
	}
	if err := writeJSONFile(filepath.Join(a.dir, "data.json"), next); err != nil {
		return err
	}
	a.data = next
	return nil
}

func writeJSONFile(file string, value any) error {
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(file), ".musehood-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(temp.Name())
	_, writeErr := temp.Write(append(raw, '\n'))
	if err := errors.Join(writeErr, temp.Close()); err != nil {
		return err
	}
	return os.Rename(temp.Name(), file)
}
func (a *App) saveLocked() error { return writeJSONFile(filepath.Join(a.dir, "data.json"), a.data) }
func (d *data) findSeat(code string) *seat {
	for i := range d.Seats {
		if d.Seats[i].Code == code {
			return &d.Seats[i]
		}
	}
	return nil
}
func isBuiltinSeat(code string) bool {
	for _, def := range defaultSeats {
		if def.code == code {
			return true
		}
	}
	return false
}

// createSeatLocked adds a custom seat (code C01, C02, ...). Caller must hold a.mu.
func (a *App) createSeatLocked(name, role, model string) (*seat, error) {
	name = strings.TrimSpace(name)
	if name == "" || jsLength(name) > 40 || badName.MatchString(name) {
		return nil, problem(400, "Nama kursi wajib diisi, maksimal 40 karakter, tanpa @ atau baris baru.")
	}
	role = strings.TrimSpace(role)
	if role == "" || jsLength(role) > 80 || badName.MatchString(role) {
		return nil, problem(400, "Peran kursi wajib diisi, maksimal 80 karakter, tanpa @ atau baris baru.")
	}
	model = strings.TrimSpace(model)
	if jsLength(model) > 200 || badSetting.MatchString(model) {
		return nil, problem(400, "Nama model tidak valid.")
	}
	lower := strings.ToLower(name)
	for _, s := range a.data.Seats {
		if strings.ToLower(s.Name) == lower {
			return nil, problem(400, "Nama kursi sudah dipakai. Pilih nama lain agar @mention tidak ambigu.")
		}
	}
	code := ""
	for n := 1; n < 100; n++ {
		candidate := fmt.Sprintf("C%02d", n)
		if a.data.findSeat(candidate) == nil {
			code = candidate
			break
		}
	}
	if code == "" {
		return nil, problem(400, "Jumlah kursi custom sudah mencapai batas.")
	}
	next := a.data
	next.Seats = slices.Clone(next.Seats)
	created := seat{identity: identity{name, "/avatars/custom.svg"}, Code: code, Role: role, Provider: "gateway", Model: ptr(model), Present: true}
	next.Seats = append(next.Seats, created)
	if err := writeJSONFile(filepath.Join(a.dir, "data.json"), next); err != nil {
		return nil, err
	}
	a.data = next
	return a.data.findSeat(code), nil
}

// deleteSeatLocked removes a custom seat. Caller must hold a.mu.
func (a *App) deleteSeatLocked(code string) error {
	if isBuiltinSeat(code) {
		return problem(400, "Kursi bawaan tidak dapat dihapus.")
	}
	index := -1
	for i := range a.data.Seats {
		if a.data.Seats[i].Code == code {
			index = i
			break
		}
	}
	if index < 0 {
		return problem(404, "Kursi tidak ditemukan.")
	}
	for _, j := range a.data.Jobs {
		if j.Status != "completed" {
			for _, s := range j.Steps {
				if s.Seat == code {
					return problem(409, "Kursi sedang dipakai tugas yang berjalan. Tunggu tugas selesai.")
				}
			}
		}
	}
	next := a.data
	next.Seats = slices.Clone(next.Seats)
	next.Seats = append(next.Seats[:index], next.Seats[index+1:]...)
	if next.Settings.IntegratorSeat == code {
		next.Settings.IntegratorSeat = ""
	}
	if err := writeJSONFile(filepath.Join(a.dir, "data.json"), next); err != nil {
		return err
	}
	a.data = next
	return nil
}
func (a *App) rememberKeyLocked(key string) {
	if key != "" && !slices.Contains(a.secrets, key) {
		a.secrets = append(a.secrets, key)
		slices.SortStableFunc(a.secrets, func(s, t string) int { return len(t) - len(s) })
		pairs := make([]string, 0, 4*len(a.secrets))
		const hidden = "[RAHASIA DISEMBUNYIKAN]"
		for _, s := range a.secrets {
			pairs = append(pairs, s, hidden, url.QueryEscape(s), hidden)
			for _, escapeHTML := range []bool{true, false} {
				var b bytes.Buffer
				encoder := json.NewEncoder(&b)
				encoder.SetEscapeHTML(escapeHTML)
				_ = encoder.Encode(s)
				escaped := b.String()[1 : b.Len()-2]
				if escaped != s {
					pairs = append(pairs, escaped, hidden)
				}
			}
		}
		a.redactor = strings.NewReplacer(pairs...)
	}
}
func (a *App) redactLocked(s string) string {
	if a.redactor == nil {
		return s
	}
	return a.redactor.Replace(s)
}

// Walk JSON string values so a key resembling a property name or a JSON literal
// cannot corrupt the wire format. Called with the app mutex held.
func (a *App) safeLocked(input any) any {
	if a.redactor == nil {
		return input
	}
	raw, _ := json.Marshal(input)
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	_ = decoder.Decode(&value)
	var visit func(any) any
	visit = func(v any) any {
		switch x := v.(type) {
		case string:
			return a.redactLocked(x)
		case []any:
			for i, item := range x {
				x[i] = visit(item)
			}
		case map[string]any:
			for k, item := range x {
				x[k] = visit(item)
			}
		}
		return v
	}
	return visit(value)
}
func (a *App) stateLocked() any {
	d := a.data
	// Salin kursi sebelum mengisi DefaultPrompt agar nilai turunan ini tidak
	// bocor ke data tersimpan (irisan Seats berbagi array dengan a.data).
	d.Seats = slices.Clone(d.Seats)
	for i := range d.Seats {
		d.Seats[i].DefaultPrompt = defaultPromptFor(d.Seats[i])
	}
	// Salin provider dan kosongkan key-nya: klien hanya menerima tampilan
	// aman (hasApiKey/active) lewat field Providers di bawah. Tampilan
	// dihitung SEBELUM key dikosongkan, supaya hasApiKey tetap benar.
	views := d.providerViews()
	d.Providers = slices.Clone(d.Providers)
	for i := range d.Providers {
		d.Providers[i].APIKey = ""
	}
	key := []rune(d.Settings.APIKey)
	masked := ""
	if len(key) > 0 {
		masked = "••••••••"
		if len(key) > 4 {
			masked += string(key[len(key)-4:])
		}
	}
	d.Settings.APIKey = masked
	present := 0
	if d.Owner.Present {
		present++
	}
	for _, s := range d.Seats {
		if s.Present {
			present++
		}
	}
	return struct {
		data
		Providers []gatewayProviderView `json:"providers"`
		Settings  struct {
			settings
			HasAPIKey bool `json:"hasApiKey"`
		} `json:"settings"`
		Presence struct {
			Present int `json:"present"`
			Total   int `json:"total"`
		} `json:"presence"`
	}{d, views, struct {
		settings
		HasAPIKey bool `json:"hasApiKey"`
	}{d.Settings, len(key) > 0}, struct {
		Present int `json:"present"`
		Total   int `json:"total"`
	}{present, 1 + len(d.Seats)}}
}

type nameUpdate struct {
	Name *string `json:"name"`
}
type settingsUpdate struct {
	Owner *nameUpdate `json:"owner"`
	Group *nameUpdate `json:"group"`
	Seats *[]struct {
		nameUpdate
		Code     string  `json:"code"`
		Role     *string `json:"role"`
		Provider *string `json:"provider"`
		Model    *string `json:"model"`
		Prompt   *string `json:"prompt"`
	} `json:"seats"`
	Settings *struct {
		BaseURL         *string `json:"baseUrl"`
		APIKey          *string `json:"apiKey"`
		ProviderName    *string `json:"providerName"`
		GatewayProtocol *string `json:"gatewayProtocol"`
		ReviewLevel     *string `json:"reviewLevel"`
		RoundRobin      *bool   `json:"roundRobin"`
		DemoFallback    *bool   `json:"demoFallback"`
		DarkMode        *bool   `json:"darkMode"`
		IntegratorSeat  *string `json:"integratorSeat"`
		ClearAPIKey     bool    `json:"clearApiKey"`
	} `json:"settings"`
}

func updateName(dest *string, value *string, label string, max int) error {
	if value == nil {
		return nil
	}
	name := strings.TrimSpace(*value)
	if name == "" || jsLength(name) > max || badName.MatchString(*value) {
		return problem(400, fmt.Sprintf("%s wajib diisi, maksimal %d karakter, tanpa @ atau baris baru.", label, max))
	}
	*dest = name
	return nil
}
func (a *App) updateSettingsLocked(input settingsUpdate) error {
	next := a.data
	next.Seats = slices.Clone(next.Seats)
	next.Providers = slices.Clone(next.Providers)
	if input.Owner != nil {
		if err := updateName(&next.Owner.Name, input.Owner.Name, "Nama pemilik", 80); err != nil {
			return err
		}
	}
	if input.Group != nil {
		if err := updateName(&next.Group.Name, input.Group.Name, "Nama grup", 80); err != nil {
			return err
		}
	}
	if input.Seats != nil {
		for _, patch := range *input.Seats {
			s := next.findSeat(patch.Code)
			if s == nil {
				return problem(400, "Kode kursi tidak dikenal.")
			}
			if err := updateName(&s.Name, patch.Name, "Nama kursi", 40); err != nil {
				return err
			}
			if patch.Role != nil {
				role := strings.TrimSpace(*patch.Role)
				if role == "" || jsLength(role) > 80 || badName.MatchString(role) {
					return problem(400, "Peran kursi wajib diisi, maksimal 80 karakter, tanpa @ atau baris baru.")
				}
				s.Role = role
			}
			if patch.Provider != nil {
				provider := strings.TrimSpace(*patch.Provider)
				if provider != "muse" && provider != "gateway" {
					return problem(400, "Provider kursi harus muse atau gateway.")
				}
				s.Provider = provider
				if provider == "gateway" && s.Model == nil {
					s.Model = ptr("")
				}
			}
			if patch.Model != nil {
				if jsLength(*patch.Model) > 200 || badSetting.MatchString(*patch.Model) {
					return problem(400, "Nama model tidak valid.")
				}
				s.Model = ptr(strings.TrimSpace(*patch.Model))
			}
			if patch.Prompt != nil {
				prompt := strings.TrimSpace(*patch.Prompt)
				if jsLength(prompt) > 4000 || strings.Contains(prompt, "\x00") || strings.Contains(prompt, "\r") {
					return problem(400, "Prompt kustom maksimal 4000 karakter, tanpa karakter kontrol.")
				}
				s.Prompt = prompt
			}
		}
		seen := map[string]bool{}
		for _, s := range next.Seats {
			name := strings.ToLower(s.Name)
			if seen[name] {
				return problem(400, "Nama tiap kursi harus berbeda agar @mention tidak ambigu.")
			}
			seen[name] = true
		}
	}
	if patch := input.Settings; patch != nil {
		if patch.ReviewLevel != nil {
			if _, ok := review[*patch.ReviewLevel]; !ok {
				return problem(400, "Level Review harus Longgar, Santai, Sedang, atau Ketat.")
			}
			next.Settings.ReviewLevel = *patch.ReviewLevel
		}
		if patch.GatewayProtocol != nil {
			proto := strings.TrimSpace(*patch.GatewayProtocol)
			if !validProtocols[proto] {
				return problem(400, "Protokol gateway harus openai, openai-responses, anthropic, gemini, atau ollama.")
			}
			next.Settings.GatewayProtocol = proto
		}
		if patch.ProviderName != nil {
			name := strings.TrimSpace(*patch.ProviderName)
			if jsLength(name) > 80 || badName.MatchString(name) {
				return problem(400, "Nama provider maksimal 80 karakter, tanpa @ atau baris baru.")
			}
			next.Settings.ProviderName = name
		}
		if patch.DarkMode != nil {
			next.Settings.DarkMode = *patch.DarkMode
		}
		if patch.IntegratorSeat != nil {
			code := strings.TrimSpace(*patch.IntegratorSeat)
			if code == "" || next.findSeat(code) == nil {
				return problem(400, "Kursi integrator tidak dikenal.")
			}
			next.Settings.IntegratorSeat = code
		}
		if patch.RoundRobin != nil {
			next.Settings.RoundRobin = *patch.RoundRobin
		}
		if patch.DemoFallback != nil {
			next.Settings.DemoFallback = *patch.DemoFallback
		}
		if patch.BaseURL != nil {
			base := strings.TrimRight(strings.TrimSpace(*patch.BaseURL), "/")
			if base != "" {
				u, err := url.Parse(base)
				if err != nil || u.Hostname() == "" {
					return problem(400, "Base URL harus URL HTTP atau HTTPS yang valid.")
				}
				if !validHTTPURL(u) || u.RawQuery != "" || u.ForceQuery || strings.Contains(base, "#") {
					return problem(400, "Base URL harus HTTP/HTTPS tanpa kredensial, query, atau fragmen.")
				}
				u.Host = strings.ToLower(u.Host)
				next.Settings.BaseURL = strings.TrimRight(u.String(), "/")
			} else {
				next.Settings.BaseURL = ""
			}
		}
		if patch.ClearAPIKey {
			next.Settings.APIKey = ""
		} else if patch.APIKey != nil && *patch.APIKey != "" {
			key := *patch.APIKey
			if jsLength(key) > 4096 || badSetting.MatchString(key) {
				return problem(400, "API key tidak valid.")
			}
			if !strings.HasPrefix(key, "••••") {
				next.Settings.APIKey = strings.TrimSpace(key)
			}
		}
		// Tulis balik field gateway ke provider aktif agar daftar provider
		// tetap konsisten dengan pengaturan tunggal lama. Hanya field yang
		// benar-benar dibawa patch yang disinkronkan, agar patch pengaturan
		// lain (mis. integratorSeat) tidak mengosongkan nama/BaseURL provider.
		if active := next.findProvider(next.Settings.ActiveProviderID); active != nil {
			if patch.BaseURL != nil {
				active.BaseURL = next.Settings.BaseURL
			}
			if patch.APIKey != nil || patch.ClearAPIKey {
				active.APIKey = next.Settings.APIKey
			}
			if patch.ProviderName != nil {
				active.Name = next.Settings.ProviderName
			}
			if patch.GatewayProtocol != nil {
				active.Protocol = next.Settings.GatewayProtocol
			}
		}
		// Bila belum ada provider aktif tetapi settings kini punya gateway
		// (konfigurasi lama lewat /api/settings), buat provider dari
		// settings agar daftar provider selalu mencerminkan gateway yang
		// dipakai.
		if next.findProvider(next.Settings.ActiveProviderID) == nil && strings.TrimSpace(next.Settings.BaseURL) != "" {
			name := strings.TrimSpace(next.Settings.ProviderName)
			if name == "" {
				if u, err := url.Parse(next.Settings.BaseURL); err == nil && u.Hostname() != "" {
					name = u.Hostname()
				} else {
					name = "Gateway Utama"
				}
			}
			proto := next.Settings.GatewayProtocol
			if proto == "" {
				proto = "openai"
			}
			id := "prov-utama"
			if next.findProvider(id) != nil {
				id = newProviderID()
			}
			next.Providers = append(next.Providers, gatewayProvider{ID: id, Name: name, BaseURL: next.Settings.BaseURL, APIKey: next.Settings.APIKey, Protocol: proto})
			next.Settings.ActiveProviderID = id
		}
	}
	if next.Settings.BaseURL != a.data.Settings.BaseURL || next.Settings.APIKey != a.data.Settings.APIKey || next.Settings.GatewayProtocol != a.data.Settings.GatewayProtocol {
		next.Connection = nil
	}
	a.rememberKeyLocked(next.Settings.APIKey)
	if err := writeJSONFile(filepath.Join(a.dir, "data.json"), next); err != nil {
		return err
	}
	a.data = next
	return nil
}

func validHTTPURL(u *url.URL) bool {
	return (u.Scheme == "http" || u.Scheme == "https") && u.Hostname() != "" && u.User == nil
}
