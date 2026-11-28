package musehood

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
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
	"sort"
	"strconv"
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
	// Version adalah versi aplikasi (dari ldflags) untuk cek update.
	Version string
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
	// questionMu/questions adalah pertanyaan kursi yang menunggu
	// jawaban pemilik (tool ask_question), hanya di memori.
	questionMu sync.Mutex
	questions  map[string]*question
	// mcpMu/mcpCache/mcpRefreshing adalah cache daftar tool server
	// MCP (60 dtk), hanya di memori.
	mcpMu         sync.Mutex
	mcpCache      map[string]mcpCacheEntry
	mcpRefreshing map[string]bool
	cacheMu         sync.Mutex
	cache           map[string]cacheEntry
	cacheHits       int64
	healthMu        sync.Mutex
	health          map[string]providerHealth
	// budgetNoted menandai catatan budget "sekali per pekerjaan" yang
	// sudah ditampilkan (fitur 5), kunci "jobID|kodeKursi".
	budgetNoted map[string]bool
	// breakerMu/breakers adalah circuit breaker per host provider
	// (fitur 7), hanya di memori.
	breakerMu sync.Mutex
	breakers  map[string]*circuitBreaker
	// parallelRemaining menghitung langkah paralel yang belum selesai
	// per pekerjaan (fitur 15).
	parallelRemaining map[*job]int
	// loginMu/loginFails adalah rate limit masuk PIN per IP (fitur 28),
	// hanya di memori.
	loginMu    sync.Mutex
	loginFails map[string]*loginAttempt
	// ftMu/ftIndex/ftDirty adalah indeks full-text pesan (fitur 35),
	// hanya di memori; dibangun ulang malas saat kotor.
	ftMu    sync.Mutex
	ftIndex map[string]map[string]bool
	ftDirty bool
	// store adalah backend penyimpanan data (fitur 34); nil berarti
	// berkas JSON langsung (App yang dibuat di tes).
	store dataStore
	// version adalah versi aplikasi untuk cek update (fitur 45).
	version string
	// serverMu/servers adalah dev server proyek yang berjalan
	// (tool run_server), hanya di memori.
	serverMu sync.Mutex
	servers  map[string]*projectServer
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
	store, err := openStore(opts.DataDir)
	if err != nil {
		return nil, err
	}
	d, err := store.load()
	if err != nil {
		return nil, err
	}
	if d.WorkerToken == "" {
		raw := make([]byte, 32)
		if _, err := rand.Read(raw); err != nil {
			return nil, err
		}
		d.WorkerToken = hex.EncodeToString(raw)
	}
	a := &App{dir: opts.DataDir, frontend: opts.Frontend, data: d, store: store, version: opts.Version, running: make(map[*job]context.CancelFunc), museWait: opts.MuseWait,
		cache: make(map[string]cacheEntry), health: make(map[string]providerHealth), budgetNoted: map[string]bool{}, breakers: map[string]*circuitBreaker{}, loginFails: map[string]*loginAttempt{}, ftDirty: true,
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
	a.csp = "default-src 'self'; " + scripts + "; style-src 'self'; " + announcerStyle + "; img-src 'self' data:; connect-src 'self'; worker-src 'self'; manifest-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'; form-action 'self'"
	a.rememberKeyLocked(d.Settings.APIKey)
	a.rememberKeyLocked(d.Settings.PINHash)
	// APIToken sengaja TIDAK didaftarkan sebagai rahasia redaksi: token
	// harus bisa ditampilkan sekali oleh endpoint regenerate (fitur 42).
	// Bidang ini tetap dikosongkan di state dan cadangan.
	a.rememberKeyLocked(d.Settings.TelegramBotToken)
	a.rememberKeyLocked(d.WorkerToken)
	for _, p := range d.Providers {
		a.rememberKeyLocked(p.APIKey)
	}
	for _, server := range d.Settings.MCPServers {
		for _, value := range server.Env {
			a.rememberKeyLocked(value)
		}
	}
	if err := a.saveLocked(); err != nil {
		return nil, err
	}
	// Token pekerja ditulis ke file agar skrip pekerja Muse bisa
	// mengirimkannya sebagai header X-Worker-Token saat PIN aktif.
	tokenFile := filepath.Join(opts.DataDir, "worker-token.txt")
	if err := os.WriteFile(tokenFile, []byte(d.WorkerToken+"\n"), 0600); err != nil {
		return nil, err
	}
	_ = os.Chmod(tokenFile, 0600)
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
	// Pemeriksaan kesehatan provider berkala (fitur 6). Pemeriksaan pertama
	// menunggu satu interval agar start aplikasi tidak memukul gateway.
	a.wg.Go(a.healthLoop)
	// Jembatan Telegram (fitur 43): loop long-poll yang diam bila nonaktif.
	a.wg.Go(a.telegramLoop)
	return a, nil
}
func (a *App) Close() {
	a.mu.Lock()
	a.closed = true
	a.stop()
	a.mu.Unlock()
	a.stopAllProjectServers()
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
	if isAPI {
		if err := a.authorizeAPI(r); err != nil {
			a.writeJSON(w, 0, nil, err)
			return
		}
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
	case "/api/state", "/api/queue/muse", "/api/stats", "/api/search", "/api/skills", "/api/tools", "/api/tool-log", "/api/personas", "/api/update/check", "/api/roles", "/api/memories":
		want = http.MethodGet
	case "/api/providers", "/api/rooms", "/api/presets", "/api/scheduled", "/api/templates", "/api/chains", "/api/mcp-servers":
		if r.Method != http.MethodGet && r.Method != http.MethodPost {
			w.Header().Set("Allow", "GET, POST")
			return 0, nil, problem(405, "Metode tidak diizinkan untuk endpoint ini.")
		}
		want = r.Method
	case "/api/settings", "/api/message", "/api/test-connection", "/api/test-model", "/api/progress", "/api/clear", "/api/upload-avatar", "/api/upload-image", "/api/upload-file", "/api/seats", "/api/vote", "/api/login", "/api/logout", "/api/pin", "/api/debate", "/api/skills/install", "/api/update/apply", "/api/external-token/regenerate", "/api/external/message", "/api/abtest", "/api/ollama/detect", "/api/ollama/add", "/api/review", "/api/commit-workflow", "/api/autoresearch":
		want = http.MethodPost
	default:
		if strings.HasPrefix(path, "/api/queue/muse/") && !strings.Contains(strings.TrimPrefix(path, "/api/queue/muse/"), "/") {
			want = http.MethodPost
		} else if strings.HasPrefix(path, "/api/seats/") && strings.HasSuffix(path, "/persona") && r.Method == http.MethodPost {
			want = http.MethodPost
		} else if strings.HasPrefix(path, "/api/seats/") && strings.HasSuffix(path, "/clone") && r.Method == http.MethodPost {
			want = http.MethodPost
		} else if strings.HasPrefix(path, "/api/seats/") && !strings.Contains(strings.TrimPrefix(path, "/api/seats/"), "/") {
			want = http.MethodDelete
		} else if strings.HasPrefix(path, "/api/memories/") && !strings.Contains(strings.TrimPrefix(path, "/api/memories/"), "/") && r.Method == http.MethodDelete {
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
			} else if len(parts) == 2 && (parts[1] == "activate" || parts[1] == "summarize" || parts[1] == "archive" || parts[1] == "branch" || parts[1] == "participants" || parts[1] == "checkpoint" || parts[1] == "rewind" || parts[1] == "todos") && r.Method == http.MethodPost {
				want = http.MethodPost
			} else if len(parts) == 3 && parts[1] == "todos" && r.Method == http.MethodDelete {
				want = http.MethodDelete
			} else if len(parts) == 4 && parts[1] == "todos" && parts[3] == "status" && r.Method == http.MethodPost {
				want = http.MethodPost
			} else if len(parts) == 2 && parts[1] == "documents" && (r.Method == http.MethodGet || r.Method == http.MethodPost) {
				want = r.Method
			} else if len(parts) == 3 && parts[1] == "participants" && parts[2] == "remove" && r.Method == http.MethodPost {
				want = http.MethodPost
			} else if len(parts) == 4 && parts[1] == "documents" && parts[3] == "delete" && r.Method == http.MethodPost {
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
			} else if len(parts) == 2 && parts[1] == "health" && r.Method == http.MethodPost {
				want = http.MethodPost
			} else if len(parts) == 2 && parts[1] == "latency-test" && r.Method == http.MethodPost {
				want = http.MethodPost
			} else {
				return 0, nil, problem(404, "Endpoint API tidak ditemukan.")
			}
		} else if strings.HasPrefix(path, "/api/chains/") {
			rest := strings.TrimPrefix(path, "/api/chains/")
			parts := strings.Split(rest, "/")
			if len(parts) == 1 && parts[0] != "" && r.Method == http.MethodPost {
				want = http.MethodPost
			} else if len(parts) == 2 && (parts[1] == "delete" || parts[1] == "run") && r.Method == http.MethodPost {
				want = http.MethodPost
			} else {
				return 0, nil, problem(404, "Endpoint API tidak ditemukan.")
			}
		} else if strings.HasPrefix(path, "/api/abtest/") {
			rest := strings.TrimPrefix(path, "/api/abtest/")
			parts := strings.Split(rest, "/")
			if len(parts) == 2 && parts[1] == "pick" && r.Method == http.MethodPost {
				want = http.MethodPost
			} else {
				return 0, nil, problem(404, "Endpoint API tidak ditemukan.")
			}
		} else if strings.HasPrefix(path, "/api/approvals/") && !strings.Contains(strings.TrimPrefix(path, "/api/approvals/"), "/") {
			want = http.MethodPost
		} else if strings.HasPrefix(path, "/api/questions/") && !strings.Contains(strings.TrimPrefix(path, "/api/questions/"), "/") {
			want = http.MethodPost
		} else if strings.HasPrefix(path, "/api/mcp-servers/") {
			rest := strings.TrimPrefix(path, "/api/mcp-servers/")
			parts := strings.Split(rest, "/")
			if len(parts) == 2 && parts[1] == "delete" && r.Method == http.MethodPost {
				want = http.MethodPost
			} else {
				return 0, nil, problem(404, "Endpoint API tidak ditemukan.")
			}
		} else if strings.HasPrefix(path, "/api/templates/") && !strings.Contains(strings.TrimPrefix(path, "/api/templates/"), "/") {
			if r.Method == http.MethodDelete {
				want = http.MethodDelete
			} else if r.Method == http.MethodPost {
				want = http.MethodPost
			} else {
				w.Header().Set("Allow", "POST, DELETE")
				return 0, nil, problem(405, "Metode tidak diizinkan untuk endpoint ini.")
			}
		} else if strings.HasPrefix(path, "/api/scheduled/") && !strings.Contains(strings.TrimPrefix(path, "/api/scheduled/"), "/") {
			if r.Method == http.MethodDelete {
				want = http.MethodDelete
			} else if r.Method == http.MethodPost {
				want = http.MethodPost
			} else {
				w.Header().Set("Allow", "POST, DELETE")
				return 0, nil, problem(405, "Metode tidak diizinkan untuk endpoint ini.")
			}
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
	case "/api/login":
		return a.login(w, r)
	case "/api/logout":
		return a.logout(w, r)
	case "/api/external-token/regenerate":
		return a.respondLocked(200, func() (any, error) {
			token, err := a.regenerateExternalTokenLocked()
			if err != nil {
				return nil, err
			}
			return map[string]any{"token": token}, nil
		})
	case "/api/external/message":
		return a.externalMessage(w, r)
	case "/api/update/check":
		out, err := a.updateCheck(r.Context())
		if err != nil {
			return 0, nil, err
		}
		return 200, mustJSON(out), nil
	case "/api/update/apply":
		out, err := a.updateApply(r.Context())
		if err != nil {
			return 0, nil, err
		}
		return 200, mustJSON(out), nil
	case "/api/pin":
		return a.setPIN(w, r)
	case "/api/skills/install":
		var body struct {
			URL       string `json:"url"`
			Overwrite bool   `json:"overwrite"`
		}
		if err := readObject(w, r, &body); err != nil {
			return 0, nil, err
		}
		name, err := a.installSkill(r.Context(), body.URL, body.Overwrite)
		if err != nil {
			return 0, nil, err
		}
		return a.respondLocked(200, func() (any, error) {
			return map[string]any{"name": name, "source": "lokal"}, nil
		})
	case "/api/debate":
		var body struct {
			SeatA  string `json:"seatA"`
			SeatB  string `json:"seatB"`
			Topic  string `json:"topic"`
			RoomID string `json:"roomId"`
		}
		if err := readObject(w, r, &body); err != nil {
			return 0, nil, err
		}
		return a.respondLocked(202, func() (any, error) {
			j, err := a.submitDebateLocked(body.SeatA, body.SeatB, body.Topic, body.RoomID)
			if err != nil {
				return nil, err
			}
			return a.jobResponseLocked(j), nil
		})
	case "/api/personas":
		return a.respondLocked(200, func() (any, error) {
			return map[string]any{"personas": builtinPersonas}, nil
		})
	case "/api/roles":
		return a.respondLocked(200, func() (any, error) {
			return map[string]any{"roles": builtinRoles}, nil
		})
	case "/api/chains":
		if r.Method == http.MethodGet {
			return a.respondLocked(200, func() (any, error) {
				items := a.data.Chains
				if items == nil {
					items = []promptChain{}
				}
				return map[string]any{"chains": items}, nil
			})
		}
		var input chainInput
		if err := readObject(w, r, &input); err != nil {
			return 0, nil, err
		}
		return a.respondLocked(201, func() (any, error) { return a.createChainLocked(input) })
	case "/api/abtest":
		var body struct {
			Text   string `json:"text"`
			ModelA string `json:"modelA"`
			ModelB string `json:"modelB"`
			RoomID string `json:"roomId"`
		}
		if err := readObject(w, r, &body); err != nil {
			return 0, nil, err
		}
		return a.respondLocked(202, func() (any, error) {
			group, ids, err := a.startABTestLocked(body.Text, body.ModelA, body.ModelB, body.RoomID)
			if err != nil {
				return nil, err
			}
			return map[string]any{"group": group, "messages": ids}, nil
		})
	case "/api/ollama/detect":
		var body struct {
			BaseURL string `json:"baseUrl"`
		}
		if r.ContentLength > 0 {
			if err := readObject(w, r, &body); err != nil {
				return 0, nil, err
			}
		}
		base, err := validateOllamaBaseURL(body.BaseURL)
		if err != nil {
			return 0, nil, err
		}
		models, err := a.detectOllama(r.Context(), base)
		if err != nil {
			return 0, nil, err
		}
		return a.respondLocked(200, func() (any, error) {
			return map[string]any{"baseUrl": base, "models": models}, nil
		})
	case "/api/ollama/add":
		var body struct {
			BaseURL string `json:"baseUrl"`
			Model   string `json:"model"`
		}
		if err := readObject(w, r, &body); err != nil {
			return 0, nil, err
		}
		return a.respondLocked(200, func() (any, error) { return a.addOllamaProviderLocked(body.BaseURL, body.Model) })
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
	case "/api/templates":
		if r.Method == http.MethodGet {
			return a.respondLocked(200, func() (any, error) {
				items := a.data.Templates
				if items == nil {
					items = []discussionTemplate{}
				}
				return map[string]any{"templates": items}, nil
			})
		}
		var input templateInput
		if err := readObject(w, r, &input); err != nil {
			return 0, nil, err
		}
		return a.respondLocked(201, func() (any, error) { return a.createTemplateLocked(input) })
	case "/api/scheduled":
		if r.Method == http.MethodGet {
			return a.respondLocked(200, func() (any, error) {
				items := a.data.Scheduled
				if items == nil {
					items = []scheduledMessage{}
				}
				return map[string]any{"scheduled": items}, nil
			})
		}
		var input scheduledInput
		if err := readObject(w, r, &input); err != nil {
			return 0, nil, err
		}
		return a.respondLocked(201, func() (any, error) { return a.createScheduledLocked(input) })
	case "/api/skills":
		return a.respondLocked(200, func() (any, error) {
			skills := a.loadSkills()
			for i := range skills {
				skills[i].Usage = a.data.SkillUsage[skills[i].Name]
			}
			return map[string]any{"skills": skills}, nil
		})
	case "/api/tools":
		return a.respondLocked(200, func() (any, error) {
			builtin := make([]string, 0, len(builtinToolNames))
			for name := range builtinToolNames {
				builtin = append(builtin, name)
			}
			sort.Strings(builtin)
			return map[string]any{"tools": a.loadCustomTools(), "builtin": builtin, "mcp": a.mcpToolViews(a.data.Settings.MCPServers)}, nil
		})
	case "/api/review":
		var input reviewInput
		if err := readObject(w, r, &input); err != nil {
			return 0, nil, err
		}
		return a.respondLocked(202, func() (any, error) { return a.reviewLocked(input) })
	case "/api/commit-workflow":
		var input commitWorkflowInput
		if err := readObject(w, r, &input); err != nil {
			return 0, nil, err
		}
		dir, err := a.ensureProject(input.Project)
		if err != nil {
			return 0, nil, problem(400, err.Error())
		}
		ensureProjectGitignore(dir)
		if !input.Approve {
			groups, err := proposeCommitGroups(dir)
			if err != nil {
				return 0, nil, problem(400, err.Error())
			}
			return a.respondLocked(200, func() (any, error) {
				return map[string]any{"groups": groups, "note": "Kirim ulang dengan approve:true (boleh mengubah pesan grup) untuk menjalankan commit. Push terpisah lewat tool github_push."}, nil
			})
		}
		done, err := executeCommitGroups(r.Context(), dir, input.Groups)
		if err != nil {
			return 0, nil, problem(400, err.Error())
		}
		return a.respondLocked(200, func() (any, error) {
			return map[string]any{"committed": done}, nil
		})
	case "/api/autoresearch":
		var input autoresearchInput
		if err := readObject(w, r, &input); err != nil {
			return 0, nil, err
		}
		result, err := a.runAutoresearch(r.Context(), input)
		if err != nil {
			return 0, nil, err
		}
		return a.respondLocked(200, func() (any, error) { return result, nil })
	case "/api/memories":
		return a.respondLocked(200, func() (any, error) {
			memories := a.data.Memories
			if memories == nil {
				memories = []durableMemory{}
			}
			return map[string]any{"memories": memories}, nil
		})
	case "/api/mcp-servers":
		if r.Method == http.MethodGet {
			return a.respondLocked(200, func() (any, error) {
				return map[string]any{"servers": a.mcpServerViewsLocked()}, nil
			})
		}
		var input mcpServerConfig
		if err := readObject(w, r, &input); err != nil {
			return 0, nil, err
		}
		return a.respondLocked(200, func() (any, error) {
			if err := a.upsertMCPServerLocked(input); err != nil {
				return nil, err
			}
			return map[string]any{"servers": a.mcpServerViewsLocked()}, nil
		})
	case "/api/tool-log":
		limit := 100
		if raw := r.URL.Query().Get("limit"); raw != "" {
			if n, err := strconv.Atoi(raw); err == nil && n > 0 {
				limit = n
			}
		}
		if limit > maxToolLogEntries {
			limit = maxToolLogEntries
		}
		return a.respondLocked(200, func() (any, error) {
			entries := []toolLogEntry{}
			for i := len(a.data.ToolLog) - 1; i >= 0 && len(entries) < limit; i-- {
				entries = append(entries, a.data.ToolLog[i])
			}
			return map[string]any{"entries": entries}, nil
		})
	case "/api/stats":
		return a.respondLocked(200, func() (any, error) {
			return map[string]any{"stats": a.statsWithCostLocked(), "cacheHits": a.cacheHitCount(), "reactions": a.reactionsStatsLocked(), "ratings": a.ratingsStatsLocked()}, nil
		})
	case "/api/search":
		query := r.URL.Query()
		filters := searchFilters{
			Seat:            query.Get("seat"),
			Room:            query.Get("room"),
			From:            query.Get("from"),
			To:            query.Get("to"),
			HasImage:        query.Get("hasImage") == "1",
			IncludeArchived: query.Get("includeArchived") == "1",
			Tag:             query.Get("tag"),
		}
		return a.respondLocked(200, func() (any, error) { return a.searchLocked(query.Get("q"), filters), nil })
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
			return a.respondLocked(200, func() (any, error) {
				views := a.data.providerViews()
				a.overlayHealth(views)
				return map[string]any{"providers": views}, nil
			})
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
			TemplateID string      `json:"templateId"`
			Author     string      `json:"author"`
		}
		if err := readObject(w, r, &body); err != nil {
			return 0, nil, err
		}
		return a.respondLocked(202, func() (any, error) {
			if body.TemplateID != "" {
				j, err := a.submitTemplateLocked(body.Text, body.RoomID, body.TemplateID)
				if err != nil {
					return nil, err
				}
				return a.jobResponseLocked(j), nil
			}
			j, err := a.submitLocked(body.Text, body.ReplyTo, body.ImageURL, body.Attachment, body.RoomID, body.Author, false)
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
		if strings.HasPrefix(path, "/api/chains/") {
			rest := strings.TrimPrefix(path, "/api/chains/")
			parts := strings.Split(rest, "/")
			if len(parts) == 2 && parts[1] == "delete" {
				return a.respondLocked(200, func() (any, error) { return map[string]bool{"ok": true}, a.deleteChainLocked(parts[0]) })
			}
			if len(parts) == 2 && parts[1] == "run" {
				var body struct {
					Text   string `json:"text"`
					RoomID string `json:"roomId"`
				}
				if err := readObject(w, r, &body); err != nil {
					return 0, nil, err
				}
				return a.respondLocked(202, func() (any, error) { return a.runChainLocked(parts[0], body.Text, body.RoomID) })
			}
			if len(parts) == 1 {
				var input chainInput
				if err := readObject(w, r, &input); err != nil {
					return 0, nil, err
				}
				return a.respondLocked(200, func() (any, error) { return a.updateChainLocked(parts[0], input) })
			}
		}
		if strings.HasPrefix(path, "/api/abtest/") {
			rest := strings.TrimPrefix(path, "/api/abtest/")
			parts := strings.Split(rest, "/")
			if len(parts) == 2 && parts[1] == "pick" {
				var body struct {
					MessageID string `json:"messageId"`
				}
				if err := readObject(w, r, &body); err != nil {
					return 0, nil, err
				}
				return a.respondLocked(200, func() (any, error) { return a.pickABWinnerLocked(parts[0], body.MessageID) })
			}
		}
		if strings.HasPrefix(path, "/api/templates/") {
			id := strings.TrimPrefix(path, "/api/templates/")
			if r.Method == http.MethodDelete {
				return a.respondLocked(200, func() (any, error) { return map[string]bool{"ok": true}, a.deleteTemplateLocked(id) })
			}
			var input templateInput
			if err := readObject(w, r, &input); err != nil {
				return 0, nil, err
			}
			return a.respondLocked(200, func() (any, error) { return a.updateTemplateLocked(id, input) })
		}
		if strings.HasPrefix(path, "/api/scheduled/") {
			id := strings.TrimPrefix(path, "/api/scheduled/")
			if r.Method == http.MethodDelete {
				return a.respondLocked(200, func() (any, error) { return map[string]bool{"ok": true}, a.deleteScheduledLocked(id) })
			}
			var input scheduledInput
			if err := readObject(w, r, &input); err != nil {
				return 0, nil, err
			}
			return a.respondLocked(200, func() (any, error) { return a.updateScheduledLocked(id, input) })
		}
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
		if strings.HasPrefix(path, "/api/questions/") {
			id := strings.TrimPrefix(path, "/api/questions/")
			var body struct {
				Answer string `json:"answer"`
			}
			if err := readObject(w, r, &body); err != nil {
				return 0, nil, err
			}
			if len([]rune(body.Answer)) > maxAnswerChars {
				return 0, nil, problem(400, "Jawaban terlalu panjang.")
			}
			if !a.answerQuestion(id, body.Answer) {
				return 0, nil, problem(404, "Pertanyaan tidak ditemukan atau sudah kedaluwarsa.")
			}
			return a.respondLocked(200, func() (any, error) { return a.stateLocked(), nil })
		}
		if strings.HasPrefix(path, "/api/mcp-servers/") {
			rest := strings.TrimPrefix(path, "/api/mcp-servers/")
			parts := strings.Split(rest, "/")
			if len(parts) == 2 && parts[1] == "delete" {
				return a.respondLocked(200, func() (any, error) {
					if err := a.deleteMCPServerLocked(parts[0]); err != nil {
						return nil, err
					}
					return map[string]any{"servers": a.mcpServerViewsLocked()}, nil
				})
			}
			return 0, nil, problem(404, "Endpoint API tidak ditemukan.")
		}
		if strings.HasPrefix(path, "/api/memories/") {
			id := strings.TrimPrefix(path, "/api/memories/")
			return a.respondLocked(200, func() (any, error) {
				for i := range a.data.Memories {
					if a.data.Memories[i].ID == id {
						a.data.Memories = append(a.data.Memories[:i], a.data.Memories[i+1:]...)
						return map[string]bool{"ok": true}, a.saveLocked()
					}
				}
				return nil, problem(404, "Memori tidak ditemukan.")
			})
		}
		if strings.HasPrefix(path, "/api/rooms/") {
			rest := strings.TrimPrefix(path, "/api/rooms/")
			parts := strings.Split(rest, "/")
			if len(parts) == 2 && parts[1] == "activate" {
				return a.respondLocked(200, func() (any, error) { return a.activateRoomLocked(parts[0]) })
			}
			if len(parts) == 2 && parts[1] == "archive" {
				var body struct {
					Archived bool `json:"archived"`
				}
				if err := readObject(w, r, &body); err != nil {
					return 0, nil, err
				}
				return a.respondLocked(200, func() (any, error) { return a.archiveRoomLocked(parts[0], body.Archived) })
			}
			if len(parts) == 2 && parts[1] == "summarize" {
				return a.respondLocked(202, func() (any, error) {
					j, err := a.summarizeRoomLocked(parts[0])
					if err != nil {
						return nil, err
					}
					return a.jobResponseLocked(j), nil
				})
			}
			if len(parts) == 2 && parts[1] == "branch" {
				var body struct {
					BranchID string `json:"branchId"`
				}
				if err := readObject(w, r, &body); err != nil {
					return 0, nil, err
				}
				return a.respondLocked(200, func() (any, error) { return a.switchBranchLocked(parts[0], body.BranchID) })
			}
			if len(parts) == 2 && parts[1] == "participants" {
				var body struct {
					Name string `json:"name"`
				}
				if err := readObject(w, r, &body); err != nil {
					return 0, nil, err
				}
				return a.respondLocked(200, func() (any, error) { return a.addParticipantLocked(parts[0], body.Name) })
			}
			if len(parts) == 2 && parts[1] == "checkpoint" {
				var body struct {
					Label string `json:"label"`
				}
				if err := readObject(w, r, &body); err != nil {
					return 0, nil, err
				}
				return a.respondLocked(201, func() (any, error) {
					room := a.data.findRoom(parts[0])
					if room == nil {
						return nil, problem(404, "Ruang tidak ditemukan.")
					}
					cp, err := a.addCheckpointLocked(room, body.Label)
					if err != nil {
						return nil, err
					}
					return map[string]any{"checkpoint": cp}, a.saveLocked()
				})
			}
			if len(parts) == 2 && parts[1] == "rewind" {
				var body struct {
					CheckpointID string `json:"checkpointId"`
				}
				if err := readObject(w, r, &body); err != nil {
					return 0, nil, err
				}
				return a.respondLocked(200, func() (any, error) {
					room := a.data.findRoom(parts[0])
					if room == nil {
						return nil, problem(404, "Ruang tidak ditemukan.")
					}
					cp := findCheckpointLocked(room, body.CheckpointID)
					if cp == nil {
						return nil, problem(404, "Checkpoint tidak ditemukan.")
					}
					removed := a.rewindRoomLocked(room, cp)
					if err := a.saveLocked(); err != nil {
						return nil, err
					}
					return map[string]any{"removed": removed, "state": a.stateLocked()}, nil
				})
			}
			if len(parts) == 2 && parts[1] == "todos" {
				var body struct {
					Text string `json:"text"`
				}
				if err := readObject(w, r, &body); err != nil {
					return 0, nil, err
				}
				return a.respondLocked(201, func() (any, error) {
					room := a.data.findRoom(parts[0])
					if room == nil {
						return nil, problem(404, "Ruang tidak ditemukan.")
					}
					todo, err := a.addRoomTodoLocked(room, body.Text)
					if err != nil {
						return nil, err
					}
					return map[string]any{"todo": todo}, a.saveLocked()
				})
			}
			if len(parts) == 3 && parts[1] == "todos" {
				return a.respondLocked(200, func() (any, error) {
					room := a.data.findRoom(parts[0])
					if room == nil {
						return nil, problem(404, "Ruang tidak ditemukan.")
					}
					if err := a.deleteRoomTodoLocked(room, parts[2]); err != nil {
						return nil, err
					}
					return map[string]bool{"ok": true}, a.saveLocked()
				})
			}
			if len(parts) == 4 && parts[1] == "todos" && parts[3] == "status" {
				var body struct {
					Status string `json:"status"`
				}
				if err := readObject(w, r, &body); err != nil {
					return 0, nil, err
				}
				return a.respondLocked(200, func() (any, error) {
					room := a.data.findRoom(parts[0])
					if room == nil {
						return nil, problem(404, "Ruang tidak ditemukan.")
					}
					if err := a.updateRoomTodoLocked(room, parts[2], body.Status); err != nil {
						return nil, err
					}
					return map[string]bool{"ok": true}, a.saveLocked()
				})
			}
			if len(parts) == 3 && parts[1] == "participants" && parts[2] == "remove" {
				var body struct {
					Name string `json:"name"`
				}
				if err := readObject(w, r, &body); err != nil {
					return 0, nil, err
				}
				return a.respondLocked(200, func() (any, error) { return a.removeParticipantLocked(parts[0], body.Name) })
			}
			if len(parts) == 2 && parts[1] == "documents" {
				if r.Method == http.MethodGet {
					return a.respondLocked(200, func() (any, error) { return a.listDocumentsLocked(parts[0]) })
				}
				return a.uploadDocument(w, r, parts[0])
			}
			if len(parts) == 4 && parts[1] == "documents" && parts[3] == "delete" {
				return a.respondLocked(200, func() (any, error) { return map[string]bool{"ok": true}, a.deleteDocumentLocked(parts[0], parts[2]) })
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
			if len(parts) == 2 && parts[1] == "health" {
				a.mu.Lock()
				p := a.data.findProvider(parts[0])
				if p == nil {
					a.mu.Unlock()
					return 0, nil, problem(404, "Provider tidak ditemukan.")
				}
				provider := *p
				a.mu.Unlock()
				health := a.checkProviderHealth(r.Context(), provider)
				return a.respondLocked(200, func() (any, error) {
					return map[string]any{"providerId": parts[0], "health": health}, nil
				})
			}
			if len(parts) == 2 && parts[1] == "latency-test" {
				return a.latencyTest(r.Context(), parts[0])
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
		if strings.HasPrefix(path, "/api/seats/") && strings.HasSuffix(path, "/persona") {
			code := strings.TrimSuffix(strings.TrimPrefix(path, "/api/seats/"), "/persona")
			var body struct {
				PersonaID string `json:"personaId"`
			}
			if err := readObject(w, r, &body); err != nil {
				return 0, nil, err
			}
			return a.respondLocked(200, func() (any, error) { return a.applyPersonaLocked(code, body.PersonaID) })
		}
		if strings.HasPrefix(path, "/api/seats/") && strings.HasSuffix(path, "/clone") {
			code := strings.TrimSuffix(strings.TrimPrefix(path, "/api/seats/"), "/clone")
			return a.respondLocked(201, func() (any, error) {
				clone, err := a.cloneSeatLocked(code)
				if err != nil {
					return nil, err
				}
				return map[string]any{"seat": clone}, nil
			})
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
		if folder == "exports" && exportNamePattern.MatchString(filename) {
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
				if path == "manifest.webmanifest" {
					w.Header().Set("Content-Type", "application/manifest+json")
				}
				if path == "sw.js" {
					w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
					w.Header().Set("Cache-Control", "no-cache")
					w.Header().Set("Service-Worker-Allowed", "/")
				}
				http.ServeContent(w, r, path, time.Time{}, bytes.NewReader(raw))
				return
			}
		}
	}
	if len(a.index) == 0 || strings.HasPrefix(path, "images/") || strings.HasPrefix(path, "avatars/") || strings.HasPrefix(path, "uploads/") || strings.HasPrefix(path, "exports/") || strings.HasPrefix(path, "_app/") {
		a.writeJSON(w, 0, nil, problem(404, "Halaman tidak ditemukan."))
		return
	}
	w.Header().Set("Cache-Control", "no-cache")
	http.ServeContent(w, r, "index.html", time.Time{}, bytes.NewReader(a.index))
}
