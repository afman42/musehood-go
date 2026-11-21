package musehood

import (
	"bytes"
	"crypto/rand"
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
	Code     string  `json:"code"`
	Role     string  `json:"role"`
	Provider string  `json:"provider"`
	Model    *string `json:"model,omitempty"`
	Present  bool    `json:"present"`
}
type settings struct {
	BaseURL      string `json:"baseUrl"`
	APIKey       string `json:"apiKey"`
	RoundRobin   bool   `json:"roundRobin"`
	ReviewLevel  string `json:"reviewLevel"`
	DemoFallback bool   `json:"demoFallback"`
}
type message struct {
	ID         string `json:"id"`
	CreatedAt  string `json:"createdAt"`
	AuthorType string `json:"authorType"`
	Seat       string `json:"seat,omitempty"`
	JobID      string `json:"jobId"`
	Text       string `json:"text"`
	Status     string `json:"status"`
	StatusText string `json:"statusText,omitempty"`
	QueueID    string `json:"queueId,omitempty"`
	Label      string `json:"label,omitempty"`
	Error      string `json:"error,omitempty"`
	ImageURL   string `json:"imageUrl,omitempty"`
	Demo       bool   `json:"demo,omitempty"`
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
	Version    int         `json:"version"`
	Group      identity    `json:"group"`
	Owner      owner       `json:"owner"`
	Seats      []seat      `json:"seats"`
	Settings   settings    `json:"settings"`
	Messages   []*message  `json:"messages"`
	Jobs       []*job      `json:"jobs"`
	Task       *activeTask `json:"activeTask"`
	Connection *connection `json:"connection"`
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
		return d, nil
	}
	if err != nil {
		return d, err
	}
	var structure struct {
		Settings *settings `json:"settings"`
	}
	if json.Unmarshal(raw, &structure) != nil || structure.Settings == nil || json.Unmarshal(raw, &d) != nil || len(d.Seats) != 7 || d.Messages == nil {
		return d, errors.New("data.json tidak valid; pulihkan dari salinan sebelum menjalankan server")
	}
	for i, def := range defaultSeats {
		s := &d.Seats[i]
		if s.Code != def.code {
			return d, errors.New("urutan kursi data.json tidak valid")
		}
		s.Role = def.role
		if s.Code == "MUSE" {
			s.Provider = "muse"
			s.Model = nil
		} else {
			s.Provider = "gateway"
			if s.Model == nil {
				s.Model = ptr("")
			}
		}
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
			if d.findSeat(s.Seat) == nil {
				return d, errors.New("kursi pekerjaan data.json tidak valid")
			}
		}
	}
	return d, nil
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
func (a *App) rememberKeyLocked(key string) {
	if key != "" && !slices.Contains(a.secrets, key) {
		a.secrets = append(a.secrets, key)
		slices.SortStableFunc(a.secrets, func(s, t string) int { return len(t) - len(s) })
		pairs := make([]string, 0, 4*len(a.secrets))
		const hidden = "[RAHASIA DISEMBUNYIKAN]"
		for _, s := range a.secrets {
			pairs = append(pairs, s, hidden)
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
		Settings struct {
			settings
			HasAPIKey bool `json:"hasApiKey"`
		} `json:"settings"`
		Presence struct {
			Present int `json:"present"`
			Total   int `json:"total"`
		} `json:"presence"`
	}{d, struct {
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
		Code  string  `json:"code"`
		Model *string `json:"model"`
	} `json:"seats"`
	Settings *struct {
		BaseURL      *string `json:"baseUrl"`
		APIKey       *string `json:"apiKey"`
		ReviewLevel  *string `json:"reviewLevel"`
		RoundRobin   *bool   `json:"roundRobin"`
		DemoFallback *bool   `json:"demoFallback"`
		ClearAPIKey  bool    `json:"clearApiKey"`
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
			if s.Code != "MUSE" && patch.Model != nil {
				if jsLength(*patch.Model) > 200 || badSetting.MatchString(*patch.Model) {
					return problem(400, "Nama model tidak valid.")
				}
				s.Model = ptr(strings.TrimSpace(*patch.Model))
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
	}
	if next.Settings.BaseURL != a.data.Settings.BaseURL || next.Settings.APIKey != a.data.Settings.APIKey {
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
