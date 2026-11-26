package musehood

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
)

type Options struct {
	DataDir      string
	Frontend     fs.FS
	TrustedHosts string
	PollInterval time.Duration
	MuseWait     time.Duration
}
type App struct {
	dir             string
	frontend        fs.FS
	index           []byte
	csp             string
	hosts           map[string]bool
	client          *http.Client
	ctx             context.Context
	stop            context.CancelFunc
	wg              sync.WaitGroup
	museWait        time.Duration
	mu              sync.Mutex
	skillsMu        sync.Mutex
	skillsCache     []skillDef
	skillsCacheAt   time.Time
	data            data
	secrets         []string
	redactor        *strings.Replacer
	mentionPatterns []seatMentionPattern
	running         map[*job]context.CancelFunc
	closed          bool
	approvalMu      sync.Mutex
	approvals       map[string]*approval
}
type apiError struct {
	status  int
	message string
}

func (e *apiError) Error() string              { return e.message }
func problem(status int, message string) error { return &apiError{status, message} }
func publicError(err error) string {
	var e *apiError
	if errors.As(err, &e) {
		return e.message
	}
	return "Kesalahan internal server. Periksa izin penyimpanan lokal."
}

var inlineScript = regexp.MustCompile(`(?is)<script(?:\s[^>]*)?>(.*?)</script>`)

func New(opts Options) (*App, error) {
	if opts.DataDir == "" {
		opts.DataDir = "."
	}
	if opts.PollInterval <= 0 {
		opts.PollInterval = 750 * time.Millisecond
	}
	if opts.MuseWait <= 0 {
		opts.MuseWait = 10 * time.Minute
	}
	for _, dir := range []string{"avatars", "images", "uploads", "queue/requests", "queue/responses"} {
		if err := os.MkdirAll(filepath.Join(opts.DataDir, dir), 0700); err != nil {
			return nil, err
		}
	}
	d, err := loadData(opts.DataDir)
	if err != nil {
		return nil, err
	}
	a := &App{dir: opts.DataDir, frontend: opts.Frontend, data: d, running: make(map[*job]context.CancelFunc), museWait: opts.MuseWait,
		hosts: map[string]bool{"127.0.0.1": true, "localhost": true, "::1": true},
		client: &http.Client{Timeout: 60 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 || !validHTTPURL(req.URL) {
				return http.ErrUseLastResponse
			}
			if req.URL.Host != via[0].URL.Host || req.URL.Scheme != via[0].URL.Scheme {
				req.Header.Del("Authorization")
			}
			return nil
		}},
	}
	for _, host := range strings.Split(opts.TrustedHosts, ",") {
		if host = hostName(strings.TrimSpace(host)); host != "" {
			a.hosts[host] = true
		}
	}
	if opts.Frontend != nil {
		a.index, err = fs.ReadFile(opts.Frontend, "index.html")
		if err != nil {
			return nil, err
		}
	}
	scripts := "script-src 'self'"
	for _, match := range inlineScript.FindAllSubmatch(a.index, -1) {
		if len(match[1]) == 0 {
			continue
		}
		// HTML parsing normalizes CRLF before the browser calculates its hash.
		normalized := bytes.ReplaceAll(bytes.ReplaceAll(match[1], []byte("\r\n"), []byte("\n")), []byte("\r"), []byte("\n"))
		sum := sha256.Sum256(normalized)
		scripts += " 'sha256-" + base64.StdEncoding.EncodeToString(sum[:]) + "'"
	}
	// Permit only SvelteKit 3.4's static accessibility-announcer style attribute.
	const announcerStyle = "style-src-attr 'unsafe-hashes' 'sha256-S8qMpvofolR8Mpjy4kQvEm7m1q8clzU4dfDH0AmvZjo='"
	a.csp = "default-src 'self'; " + scripts + "; style-src 'self'; " + announcerStyle + "; img-src 'self' data:; connect-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'; form-action 'self'"
	a.rememberKeyLocked(d.Settings.APIKey)
	for _, p := range d.Providers {
		a.rememberKeyLocked(p.APIKey)
	}
	if err := a.saveLocked(); err != nil {
		return nil, err
	}
	a.ctx, a.stop = context.WithCancel(context.Background())
	a.mu.Lock()
	a.tickLocked()
	a.mu.Unlock()
	a.wg.Go(func() {
		ticker := time.NewTicker(opts.PollInterval)
		defer ticker.Stop()
		for {
			select {
			case <-a.ctx.Done():
				return
			case <-ticker.C:
				a.mu.Lock()
				a.tickLocked()
				a.mu.Unlock()
			}
		}
	})
	return a, nil
}
func (a *App) Close() {
	a.mu.Lock()
	a.closed = true
	a.stop()
	a.mu.Unlock()
	a.wg.Wait()
	a.client.CloseIdleConnections()
}
func hostName(host string) string {
	if ip := net.ParseIP(host); ip != nil {
		return strings.ToLower(host)
	}
	if h, _, err := net.SplitHostPort(host); err == nil {
		return strings.ToLower(h)
	}
	if strings.HasPrefix(host, "[") && strings.HasSuffix(host, "]") {
		return strings.ToLower(host[1 : len(host)-1])
	}
	if strings.ContainsAny(host, ":[]") {
		return ""
	}
	return strings.ToLower(host)
}
func (a *App) validateHost(r *http.Request) error {
	if !a.hosts[hostName(r.Host)] {
		return problem(403, "Host tidak diizinkan. Gunakan alamat localhost.")
	}
	if strings.EqualFold(r.Header.Get("Sec-Fetch-Site"), "cross-site") {
		// Navigasi GET tingkat atas dari situs lain (mis. setelah klik
		// "lanjutkan" di halaman warning tunnl.gg) hanya memuat halaman dan
		// tidak mengubah state — izinkan. Permintaan API dan method yang
		// mengubah state tetap ditolak; API juga dilindungi cek Origin.
		isAPI := r.URL.Path == "/api" || strings.HasPrefix(r.URL.Path, "/api/")
		if isAPI || (r.Method != http.MethodGet && r.Method != http.MethodHead) {
			return problem(403, "Permintaan dari situs lain tidak diizinkan.")
		}
	}
	if origin := r.Header.Get("Origin"); origin != "" {
		u, err := url.Parse(origin)
		if err != nil || !validHTTPURL(u) || u.Path != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
			return problem(403, "Origin tidak valid.")
		}
		if !strings.EqualFold(u.Host, r.Host) {
			return problem(403, "Origin permintaan tidak diizinkan.")
		}
	}
	return nil
}
func (a *App) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Content-Security-Policy", a.csp)
	isAPI := r.URL.Path == "/api" || strings.HasPrefix(r.URL.Path, "/api/")
	if isAPI {
		w.Header().Set("Cache-Control", "no-store")
	}
	if err := a.validateHost(r); err != nil {
		a.writeJSON(w, 0, nil, err)
		return
	}
	if r.URL.Path == "/api/export" {
		a.exportChat(w, r)
		return
	}
	if r.URL.Path == "/api/backup" {
		a.backup(w, r)
		return
	}
	if r.URL.Path == "/api/restore" {
		a.restore(w, r)
		return
	}
	if isAPI {
		status, value, err := a.api(w, r)
		a.writeJSON(w, status, value, err)
		return
	}
	a.serveStatic(w, r)
}
func (a *App) writeJSON(w http.ResponseWriter, status int, value json.RawMessage, err error) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if err != nil {
		status = 500
		var e *apiError
		if errors.As(err, &e) {
			status = e.status
		}
		a.mu.Lock()
		value, _ = json.Marshal(map[string]string{"error": a.redactLocked(publicError(err))})
		a.mu.Unlock()
	}
	w.WriteHeader(status)
	_, _ = w.Write(append(value, '\n'))
}
func (a *App) jsonLocked(v any) json.RawMessage {
	raw, err := json.Marshal(a.safeLocked(v))
	if err != nil {
		return json.RawMessage("null")
	}
	return raw
}
func (a *App) respondLocked(want int, fn func() (any, error)) (int, json.RawMessage, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed {
		return 0, nil, problem(503, "Server sedang berhenti.")
	}
	v, err := fn()
	if err != nil {
		return 0, nil, err
	}
	return want, a.jsonLocked(v), nil
}
func readObject(w http.ResponseWriter, r *http.Request, dest any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 256*1024)
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		var size *http.MaxBytesError
		if errors.As(err, &size) {
			return problem(413, "Isi permintaan terlalu besar.")
		}
		return problem(400, "Isi permintaan tidak dapat dibaca.")
	}
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || raw[0] != '{' || json.Unmarshal(raw, dest) != nil {
		return problem(400, "JSON permintaan tidak valid.")
	}
	return nil
}
func method(w http.ResponseWriter, r *http.Request, want string) error {
	if r.Method != want {
		w.Header().Set("Allow", want)
		return problem(405, "Metode tidak diizinkan untuk endpoint ini.")
	}
	return nil
}
func (a *App) api(w http.ResponseWriter, r *http.Request) (int, json.RawMessage, error) {
	path := r.URL.Path
	var want string
	switch path {
	case "/api/state", "/api/queue/muse", "/api/stats", "/api/search", "/api/skills", "/api/tools":
		want = http.MethodGet
	case "/api/providers", "/api/rooms", "/api/presets":
		if r.Method != http.MethodGet && r.Method != http.MethodPost {
			w.Header().Set("Allow", "GET, POST")
			return 0, nil, problem(405, "Metode tidak diizinkan untuk endpoint ini.")
		}
		want = r.Method
	case "/api/settings", "/api/message", "/api/test-connection", "/api/test-model", "/api/progress", "/api/clear", "/api/upload-avatar", "/api/upload-image", "/api/upload-file", "/api/seats", "/api/vote":
		want = http.MethodPost
	default:
		if strings.HasPrefix(path, "/api/queue/muse/") && !strings.Contains(strings.TrimPrefix(path, "/api/queue/muse/"), "/") {
			want = http.MethodPost
		} else if strings.HasPrefix(path, "/api/seats/") && !strings.Contains(strings.TrimPrefix(path, "/api/seats/"), "/") {
			want = http.MethodDelete
		} else if strings.HasPrefix(path, "/api/rooms/") {
			rest := strings.TrimPrefix(path, "/api/rooms/")
			parts := strings.Split(rest, "/")
			if len(parts) == 1 && parts[0] != "" {
				if r.Method == http.MethodDelete {
					want = http.MethodDelete
				} else if r.Method == http.MethodPost {
					want = http.MethodPost
				} else {
					w.Header().Set("Allow", "POST, DELETE")
					return 0, nil, problem(405, "Metode tidak diizinkan untuk endpoint ini.")
				}
			} else if len(parts) == 2 && parts[1] == "activate" && r.Method == http.MethodPost {
				want = http.MethodPost
			} else {
				return 0, nil, problem(404, "Endpoint API tidak ditemukan.")
			}
		} else if strings.HasPrefix(path, "/api/presets/") {
			rest := strings.TrimPrefix(path, "/api/presets/")
			parts := strings.Split(rest, "/")
			if len(parts) == 1 && parts[0] != "" && r.Method == http.MethodDelete {
				want = http.MethodDelete
			} else if len(parts) == 2 && parts[1] == "apply" && r.Method == http.MethodPost {
				want = http.MethodPost
			} else {
				return 0, nil, problem(404, "Endpoint API tidak ditemukan.")
			}
		} else if strings.HasPrefix(path, "/api/providers/") {
			rest := strings.TrimPrefix(path, "/api/providers/")
			parts := strings.Split(rest, "/")
			if len(parts) == 1 && parts[0] != "" {
				if r.Method == http.MethodDelete {
					want = http.MethodDelete
				} else if r.Method == http.MethodPost {
					want = http.MethodPost
				} else {
					w.Header().Set("Allow", "POST, DELETE")
					return 0, nil, problem(405, "Metode tidak diizinkan untuk endpoint ini.")
				}
			} else if len(parts) == 2 && parts[1] == "activate" && r.Method == http.MethodPost {
				want = http.MethodPost
			} else if len(parts) == 2 && parts[1] == "models" && r.Method == http.MethodGet {
				want = http.MethodGet
			} else if len(parts) == 2 && parts[1] == "test-models" && r.Method == http.MethodPost {
				want = http.MethodPost
			} else {
				return 0, nil, problem(404, "Endpoint API tidak ditemukan.")
			}
		} else if strings.HasPrefix(path, "/api/approvals/") && !strings.Contains(strings.TrimPrefix(path, "/api/approvals/"), "/") {
			want = http.MethodPost
		} else if _, _, ok := messageAction(path); ok {
			want = http.MethodPost
		} else {
			return 0, nil, problem(404, "Endpoint API tidak ditemukan.")
		}
	}
	if err := method(w, r, want); err != nil {
		return 0, nil, err
	}
	switch path {
	case "/api/state":
		return a.respondLocked(200, func() (any, error) { return a.stateLocked(), nil })
	case "/api/rooms":
		if r.Method == http.MethodGet {
			return a.respondLocked(200, func() (any, error) {
				return map[string]any{"rooms": a.roomViewsLocked(), "activeRoomId": a.data.ActiveRoomID}, nil
			})
		}
		var input roomInput
		if err := readObject(w, r, &input); err != nil {
			return 0, nil, err
		}
		return a.respondLocked(201, func() (any, error) { return a.createRoomLocked(input) })
	case "/api/presets":
		if r.Method == http.MethodGet {
			return a.respondLocked(200, func() (any, error) { return map[string]any{"presets": a.data.Presets}, nil })
		}
		var body struct {
			Name string `json:"name"`
		}
		if err := readObject(w, r, &body); err != nil {
			return 0, nil, err
		}
		return a.respondLocked(201, func() (any, error) { return a.createPresetLocked(body.Name) })
	case "/api/skills":
		return a.respondLocked(200, func() (any, error) { return map[string]any{"skills": a.loadSkills()}, nil })
	case "/api/tools":
		return a.respondLocked(200, func() (any, error) { return map[string]any{"tools": a.loadCustomTools()}, nil })
	case "/api/stats":
		return a.respondLocked(200, func() (any, error) { return map[string]any{"stats": a.data.Stats}, nil })
	case "/api/search":
		return a.respondLocked(200, func() (any, error) { return a.searchLocked(r.URL.Query().Get("q")), nil })
	case "/api/vote":
		var body struct {
			RoomID string `json:"roomId"`
		}
		if r.ContentLength > 0 {
			if err := readObject(w, r, &body); err != nil {
				return 0, nil, err
			}
		}
		return a.respondLocked(202, func() (any, error) {
			j, err := a.submitVoteLocked(body.RoomID)
			if err != nil {
				return nil, err
			}
			return a.jobResponseLocked(j), nil
		})
	case "/api/providers":
		if r.Method == http.MethodGet {
			return a.respondLocked(200, func() (any, error) { return map[string]any{"providers": a.data.providerViews()}, nil })
		}
		var input providerInput
		if err := readObject(w, r, &input); err != nil {
			return 0, nil, err
		}
		return a.respondLocked(201, func() (any, error) { return a.createProviderLocked(input) })
	case "/api/settings":
		var patch settingsUpdate
		if err := readObject(w, r, &patch); err != nil {
			return 0, nil, err
		}
		return a.respondLocked(200, func() (any, error) {
			if err := a.updateSettingsLocked(patch); err != nil {
				return nil, err
			}
			return a.stateLocked(), nil
		})
	case "/api/message":
		var body struct {
			Text       string      `json:"text"`
			ReplyTo    string      `json:"replyTo"`
			ImageURL   string      `json:"imageUrl"`
			Attachment *attachment `json:"attachment"`
			RoomID     string      `json:"roomId"`
		}
		if err := readObject(w, r, &body); err != nil {
			return 0, nil, err
		}
		return a.respondLocked(202, func() (any, error) {
			j, err := a.submitLocked(body.Text, body.ReplyTo, body.ImageURL, body.Attachment, body.RoomID, false)
			if err != nil {
				return nil, err
			}
			return a.jobResponseLocked(j), nil
		})
	case "/api/progress":
		return a.respondLocked(202, func() (any, error) {
			j, err := a.progressLocked()
			if err != nil {
				return nil, err
			}
			return a.jobResponseLocked(j), nil
		})
	case "/api/clear":
		var body struct {
			RoomID string `json:"roomId"`
		}
		if r.ContentLength > 0 {
			if err := readObject(w, r, &body); err != nil {
				return 0, nil, err
			}
		}
		return a.respondLocked(200, func() (any, error) { return map[string]bool{"ok": true}, a.clearLocked(body.RoomID) })
	case "/api/queue/muse":
		return a.respondLocked(200, func() (any, error) { return map[string]any{"requests": a.pendingLocked()}, nil })
	case "/api/test-connection":
		return a.testConnection(r.Context())
	case "/api/test-model":
		var body struct {
			Model string `json:"model"`
		}
		if err := readObject(w, r, &body); err != nil {
			return 0, nil, err
		}
		return a.testModel(r.Context(), body.Model)
	case "/api/upload-avatar":
		return a.uploadAvatar(w, r)
	case "/api/upload-image":
		return a.uploadImage(w, r)
	case "/api/upload-file":
		return a.uploadFile(w, r)
	case "/api/seats":
		var body struct {
			Name  string `json:"name"`
			Role  string `json:"role"`
			Model string `json:"model"`
		}
		if err := readObject(w, r, &body); err != nil {
			return 0, nil, err
		}
		return a.respondLocked(201, func() (any, error) {
			created, err := a.createSeatLocked(body.Name, body.Role, body.Model)
			if err != nil {
				return nil, err
			}
			return created, nil
		})
	default:
		if strings.HasPrefix(path, "/api/approvals/") {
			id := strings.TrimPrefix(path, "/api/approvals/")
			var body struct {
				Approved bool `json:"approved"`
			}
			if err := readObject(w, r, &body); err != nil {
				return 0, nil, err
			}
			if !a.decideApproval(id, body.Approved) {
				return 0, nil, problem(404, "Permintaan persetujuan tidak ditemukan atau sudah kedaluwarsa.")
			}
			return a.respondLocked(200, func() (any, error) { return a.stateLocked(), nil })
		}
		if strings.HasPrefix(path, "/api/rooms/") {
			rest := strings.TrimPrefix(path, "/api/rooms/")
			parts := strings.Split(rest, "/")
			if len(parts) == 2 && parts[1] == "activate" {
				return a.respondLocked(200, func() (any, error) { return a.activateRoomLocked(parts[0]) })
			}
			if len(parts) == 1 {
				if r.Method == http.MethodDelete {
					return a.respondLocked(200, func() (any, error) { return a.deleteRoomLocked(parts[0]) })
				}
				var input roomInput
				if err := readObject(w, r, &input); err != nil {
					return 0, nil, err
				}
				return a.respondLocked(200, func() (any, error) { return a.updateRoomLocked(parts[0], input) })
			}
		}
		if strings.HasPrefix(path, "/api/presets/") {
			rest := strings.TrimPrefix(path, "/api/presets/")
			parts := strings.Split(rest, "/")
			if len(parts) == 2 && parts[1] == "apply" {
				return a.respondLocked(200, func() (any, error) { return a.applyPresetLocked(parts[0]) })
			}
			if len(parts) == 1 {
				return a.respondLocked(200, func() (any, error) { return a.deletePresetLocked(parts[0]) })
			}
		}
		if strings.HasPrefix(path, "/api/providers/") {
			rest := strings.TrimPrefix(path, "/api/providers/")
			parts := strings.Split(rest, "/")
			if len(parts) == 2 && parts[1] == "activate" {
				return a.respondLocked(200, func() (any, error) {
					if err := a.activateProviderLocked(parts[0]); err != nil {
						return nil, err
					}
					return a.stateLocked(), nil
				})
			}
			if len(parts) == 2 && parts[1] == "models" {
				return a.fetchProviderModels(r.Context(), parts[0])
			}
			if len(parts) == 2 && parts[1] == "test-models" {
				var body struct {
					Models []string `json:"models"`
				}
				if err := readObject(w, r, &body); err != nil {
					return 0, nil, err
				}
				return a.testProviderModels(r.Context(), parts[0], body.Models)
			}
			if len(parts) == 1 {
				if r.Method == http.MethodDelete {
					return a.respondLocked(200, func() (any, error) {
						if err := a.deleteProviderLocked(parts[0]); err != nil {
							return nil, err
						}
						return map[string]bool{"ok": true}, nil
					})
				}
				var input providerInput
				if err := readObject(w, r, &input); err != nil {
					return 0, nil, err
				}
				return a.respondLocked(200, func() (any, error) { return a.updateProviderLocked(parts[0], input) })
			}
		}
		if strings.HasPrefix(path, "/api/seats/") && !strings.Contains(strings.TrimPrefix(path, "/api/seats/"), "/") {
			code := strings.TrimPrefix(path, "/api/seats/")
			return a.respondLocked(200, func() (any, error) {
				if err := a.deleteSeatLocked(code); err != nil {
					return nil, err
				}
				return map[string]bool{"ok": true}, nil
			})
		}
		if id, action, ok := messageAction(r.URL.Path); ok {
			return a.messageAction(w, r, id, action)
		}
		var reply workerReply
		if err := readObject(w, r, &reply); err != nil {
			return 0, nil, err
		}
		return a.workerRespond(r.Context(), strings.TrimPrefix(path, "/api/queue/muse/"), reply)
	}
}
func (a *App) jobResponseLocked(j *job) any {
	messages := []*message{}
	for _, m := range a.data.Messages {
		if m.JobID == j.ID {
			messages = append(messages, m)
		}
	}
	queue := []map[string]string{}
	for _, s := range j.Steps {
		if s.QueueID != "" {
			queue = append(queue, map[string]string{"id": s.QueueID, "seat": s.Seat, "kind": s.Kind})
		}
	}
	return map[string]any{"jobId": j.ID, "status": j.Status, "messages": messages, "queue": queue}
}
func (a *App) testConnection(ctx context.Context) (int, json.RawMessage, error) {
	a.mu.Lock()
	cfg := a.data.Settings
	a.mu.Unlock()
	models, err := a.models(ctx, cfg)
	if err != nil {
		return 0, nil, err
	}
	return a.respondLocked(200, func() (any, error) {
		if a.data.Settings.BaseURL != cfg.BaseURL || a.data.Settings.APIKey != cfg.APIKey {
			return nil, problem(409, "Gateway berubah saat diuji. Jalankan Tes Koneksi lagi.")
		}
		c := &connection{Models: models, TestedAt: now()}
		previous := a.data.Connection
		a.data.Connection = c
		if err := a.saveLocked(); err != nil {
			a.data.Connection = previous
			return nil, err
		}
		seats := []map[string]any{}
		for _, s := range a.data.Seats {
			out := map[string]any{"code": s.Code, "name": s.Name, "provider": s.Provider, "available": true}
			if s.Model != nil {
				out["model"] = *s.Model
				out["available"] = slices.Contains(models, *s.Model)
			}
			seats = append(seats, out)
		}
		return map[string]any{"models": models, "seats": seats, "testedAt": c.TestedAt}, nil
	})
}

// testOneModel mengirim satu prompt minimal ke satu model dan melaporkan
// latensi serta cuplikan balasan. Dipakai Tes Model per kursi maupun
// Tes Models massal per provider.
func (a *App) testOneModel(ctx context.Context, cfg settings, model string) (int64, string, error) {
	start := time.Now()
	history := []gatewayMessage{
		{"system", "You are a helpful assistant. Reply concisely."},
		{"user", "Balas dengan satu kata: OK"},
	}
	raw, err := a.protocolChat(ctx, cfg, model, history)
	if err != nil {
		return 0, "", err
	}
	text, err := parseProtocolText(protocolOf(cfg), string(raw))
	if err != nil {
		return 0, "", err
	}
	reply := strings.TrimSpace(text)
	if runes := []rune(reply); len(runes) > 200 {
		reply = string(runes[:200]) + "…"
	}
	return time.Since(start).Milliseconds(), reply, nil
}

// testModel mengirim satu prompt minimal ke model gateway aktif dan
// melaporkan apakah model benar-benar menjawab, beserta latensi dan cuplikan
// balasan. Berbeda dengan Tes Koneksi (yang hanya mengambil daftar model),
// endpoint ini membuktikan model bisa dipakai.
func (a *App) testModel(ctx context.Context, model string) (int, json.RawMessage, error) {
	model = strings.TrimSpace(model)
	if model == "" {
		return 0, nil, problem(400, "Nama model wajib diisi untuk diuji.")
	}
	if jsLength(model) > 200 || badSetting.MatchString(model) {
		return 0, nil, problem(400, "Nama model tidak valid.")
	}
	a.mu.Lock()
	cfg := a.data.Settings
	cfg, testName := a.data.resolveModelProviderLocked(cfg, model)
	a.mu.Unlock()
	latency, reply, err := a.testOneModel(ctx, cfg, testName)
	if err != nil {
		return 0, nil, err
	}
	result := map[string]any{"ok": true, "model": model, "latencyMs": latency, "reply": reply, "testedAt": now()}
	return a.respondLocked(200, func() (any, error) { return result, nil })
}

// fetchProviderModels mengambil daftar model dari satu provider tanpa harus
// menjadikannya aktif. Untuk provider aktif, hasilnya juga disimpan ke
// Connection seperti Tes Koneksi, supaya status ketersediaan model kursi ikut
// diperbarui.
func (a *App) fetchProviderModels(ctx context.Context, id string) (int, json.RawMessage, error) {
	a.mu.Lock()
	p := a.data.findProvider(id)
	if p == nil {
		a.mu.Unlock()
		return 0, nil, problem(404, "Provider tidak ditemukan.")
	}
	cfg := a.data.providerConfig(*p)
	active := p.ID == a.data.Settings.ActiveProviderID
	a.mu.Unlock()
	models, err := a.models(ctx, cfg)
	if err != nil {
		return 0, nil, err
	}
	if !active {
		return a.respondLocked(200, func() (any, error) {
			return map[string]any{"providerId": id, "models": models, "testedAt": now()}, nil
		})
	}
	return a.respondLocked(200, func() (any, error) {
		c := &connection{Models: models, TestedAt: now()}
		previous := a.data.Connection
		a.data.Connection = c
		if err := a.saveLocked(); err != nil {
			a.data.Connection = previous
			return nil, err
		}
		return map[string]any{"providerId": id, "models": models, "testedAt": c.TestedAt}, nil
	})
}

const maxTestModels = 10

// testProviderModels menguji tiap model dari daftar (maksimal 10) dengan satu
// prompt minimal dan melaporkan hasilnya per model. Model yang gagal tidak
// menghentikan pengujian model lain; total waktu dibatasi 180 detik.
func (a *App) testProviderModels(ctx context.Context, id string, models []string) (int, json.RawMessage, error) {
	if len(models) == 0 {
		return 0, nil, problem(400, "Daftar model untuk diuji wajib diisi.")
	}
	if len(models) > maxTestModels {
		return 0, nil, problem(400, "Maksimal 10 model dapat diuji sekaligus.")
	}
	a.mu.Lock()
	p := a.data.findProvider(id)
	if p == nil {
		a.mu.Unlock()
		return 0, nil, problem(404, "Provider tidak ditemukan.")
	}
	cfg := a.data.providerConfig(*p)
	a.mu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, 180*time.Second)
	defer cancel()
	results := []map[string]any{}
	for _, model := range models {
		model = strings.TrimSpace(model)
		if model == "" || jsLength(model) > 200 || badSetting.MatchString(model) {
			results = append(results, map[string]any{"model": model, "ok": false, "error": "Nama model tidak valid."})
			continue
		}
		latency, reply, err := a.testOneModel(ctx, cfg, model)
		if err != nil {
			results = append(results, map[string]any{"model": model, "ok": false, "error": publicError(err)})
			continue
		}
		results = append(results, map[string]any{"model": model, "ok": true, "latencyMs": latency, "reply": reply})
	}
	return a.respondLocked(200, func() (any, error) {
		return map[string]any{"providerId": id, "results": results}, nil
	})
}
func (a *App) workerRespond(ctx context.Context, id string, reply workerReply) (int, json.RawMessage, error) {
	a.mu.Lock()
	s := a.activeQueueLocked(id)
	if s == nil {
		a.mu.Unlock()
		return 0, nil, problem(404, "Request Muse tidak ditemukan atau sudah selesai.")
	}
	kind := s.Kind
	_, err := a.readQueueRequest(id)
	_, existsErr := os.Stat(a.queuePath("responses", id))
	a.mu.Unlock()
	if err != nil {
		return 0, nil, problem(404, "File request Muse tidak ditemukan.")
	}
	if existsErr == nil {
		return 0, nil, problem(409, "Request Muse sudah memiliki respons.")
	}
	if !errors.Is(existsErr, os.ErrNotExist) {
		return 0, nil, existsErr
	}
	validated, err := a.validateWorker(ctx, kind, reply)
	if err != nil {
		return 0, nil, err
	}
	return a.respondLocked(200, func() (any, error) {
		if a.activeQueueLocked(id) == nil {
			return nil, problem(404, "Request Muse tidak ditemukan atau sudah selesai.")
		}
		file := a.queuePath("responses", id)
		if _, err := os.Stat(file); err == nil {
			return nil, problem(409, "Request Muse sudah memiliki respons.")
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		validated.Text = a.redactLocked(validated.Text)
		validated.Error = a.redactLocked(validated.Error)
		if err := writeJSONFile(file, validated); err != nil {
			return nil, err
		}
		a.tickLocked()
		return map[string]any{"ok": true, "id": id}, nil
	})
}
func avatarFor(d *data, target string) *string {
	switch target {
	case "group":
		return &d.Group.Avatar
	case "owner":
		return &d.Owner.Avatar
	default:
		if s := d.findSeat(target); s != nil {
			return &s.Avatar
		}
		return nil
	}
}
func (a *App) uploadAvatar(w http.ResponseWriter, r *http.Request) (int, json.RawMessage, error) {
	r.Body = http.MaxBytesReader(w, r.Body, avatarLimit+64*1024)
	if err := r.ParseMultipartForm(avatarLimit + 64*1024); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return 0, nil, problem(413, "Ukuran avatar maksimal 2 MB.")
		}
		return 0, nil, problem(400, "Unggahan avatar harus berupa multipart yang valid.")
	}
	defer r.MultipartForm.RemoveAll()
	target := r.FormValue("target")
	if len(r.MultipartForm.File["avatar"]) != 1 {
		return 0, nil, problem(400, "Pilih satu file gambar untuk diunggah.")
	}
	file, _, err := r.FormFile("avatar")
	if err != nil {
		return 0, nil, problem(400, "Pilih file gambar untuk diunggah.")
	}
	defer file.Close()
	raw, large, err := limitedRead(file, avatarLimit)
	if large {
		return 0, nil, problem(413, "Ukuran avatar maksimal 2 MB.")
	}
	if err != nil {
		return 0, nil, err
	}
	return a.respondLocked(200, func() (any, error) {
		dest := avatarFor(&a.data, target)
		if dest == nil {
			return nil, problem(400, "Target avatar harus group, owner, atau kode kursi.")
		}
		path, err := a.saveImage(raw, "avatars")
		if err != nil {
			return nil, err
		}
		previous := *dest
		*dest = path
		if err := a.saveLocked(); err != nil {
			*dest = previous
			_ = os.Remove(filepath.Join(a.dir, path[1:]))
			return nil, err
		}
		return map[string]string{"avatar": path, "target": target}, nil
	})
}
func (a *App) serveStatic(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		a.writeJSON(w, 0, nil, problem(405, "Metode tidak diizinkan."))
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/")
	if fs.ValidPath(path) {
		folder, filename, _ := strings.Cut(path, "/")
		if (folder == "avatars" || folder == "images") && uploadName.MatchString(filename) {
			if f, err := os.OpenInRoot(filepath.Join(a.dir, folder), filename); err == nil {
				defer f.Close()
				if stat, err := f.Stat(); err == nil && stat.Mode().IsRegular() {
					w.Header().Set("Cache-Control", "no-cache")
					http.ServeContent(w, r, filename, stat.ModTime(), f)
					return
				}
			}
		}
		if folder == "uploads" && storedUpload.MatchString(filename) {
			if f, err := os.OpenInRoot(filepath.Join(a.dir, folder), filename); err == nil {
				defer f.Close()
				if stat, err := f.Stat(); err == nil && stat.Mode().IsRegular() {
					w.Header().Set("Cache-Control", "no-cache")
					http.ServeContent(w, r, filename, stat.ModTime(), f)
					return
				}
			}
		}
		if path != "index.html" && a.frontend != nil {
			if raw, err := fs.ReadFile(a.frontend, path); err == nil {
				if strings.HasPrefix(path, "_app/immutable/") {
					w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				}
				http.ServeContent(w, r, path, time.Time{}, bytes.NewReader(raw))
				return
			}
		}
	}
	if len(a.index) == 0 || strings.HasPrefix(path, "images/") || strings.HasPrefix(path, "avatars/") || strings.HasPrefix(path, "uploads/") || strings.HasPrefix(path, "_app/") {
		a.writeJSON(w, 0, nil, problem(404, "Halaman tidak ditemukan."))
		return
	}
	w.Header().Set("Cache-Control", "no-cache")
	http.ServeContent(w, r, "index.html", time.Time{}, bytes.NewReader(a.index))
}
