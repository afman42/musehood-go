package musehood

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// PIN aplikasi: bila settings.PINHash terisi, semua endpoint /api/*
// memerlukan cookie sesi (kecuali POST /api/login), dan endpoint antrean
// pekerja Muse memerlukan header X-Worker-Token. PIN disimpan sebagai
// hash SHA-256 bergaram ("salt:hash", hex). Cookie sesi berisi token
// HMAC-SHA256 dari hash PIN + waktu kedaluwarsa (7 hari), sehingga
// mengganti PIN otomatis membatalkan semua sesi lama.

const sessionCookieName = "musehood_session"
const sessionLifetime = 7 * 24 * time.Hour

func hashPIN(pin string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	sum := sha256.Sum256(append(append([]byte{}, salt...), []byte(":"+pin)...))
	return hex.EncodeToString(salt) + ":" + hex.EncodeToString(sum[:]), nil
}

func verifyPIN(pin, stored string) bool {
	saltHex, hashHex, ok := strings.Cut(stored, ":")
	if !ok {
		return false
	}
	salt, err := hex.DecodeString(saltHex)
	if err != nil {
		return false
	}
	sum := sha256.Sum256(append(append([]byte{}, salt...), []byte(":"+pin)...))
	return subtle.ConstantTimeCompare([]byte(hex.EncodeToString(sum[:])), []byte(hashHex)) == 1
}

func makeSessionToken(pinHash string, expiry time.Time) string {
	exp := strconv.FormatInt(expiry.Unix(), 10)
	mac := hmac.New(sha256.New, []byte(pinHash))
	mac.Write([]byte("musehood-session:" + exp))
	return exp + "." + hex.EncodeToString(mac.Sum(nil))
}

func validSessionToken(token, pinHash string) bool {
	exp, sig, ok := strings.Cut(token, ".")
	if !ok || pinHash == "" {
		return false
	}
	unix, err := strconv.ParseInt(exp, 10, 64)
	if err != nil || time.Now().Unix() > unix {
		return false
	}
	mac := hmac.New(sha256.New, []byte(pinHash))
	mac.Write([]byte("musehood-session:" + exp))
	return hmac.Equal([]byte(hex.EncodeToString(mac.Sum(nil))), []byte(sig))
}

// authorizeAPI memeriksa PIN untuk permintaan API. Tanpa PIN aktif,
// semua permintaan bebas (perilaku lama). Endpoint antrean pekerja
// memakai token pekerja, bukan cookie.
func (a *App) authorizeAPI(r *http.Request) error {
	a.mu.Lock()
	pinHash := a.data.Settings.PINHash
	workerToken := a.data.WorkerToken
	a.mu.Unlock()
	if pinHash == "" {
		return nil
	}
	path := r.URL.Path
	if path == "/api/login" {
		return nil
	}
	// API eksternal (fitur 42) memakai token Bearer sendiri sebagai
	// kredensial, bukan cookie PIN.
	if path == "/api/external/message" {
		return nil
	}
	if path == "/api/queue/muse" || strings.HasPrefix(path, "/api/queue/muse/") {
		given := r.Header.Get("X-Worker-Token")
		if workerToken == "" || subtle.ConstantTimeCompare([]byte(given), []byte(workerToken)) != 1 {
			return problem(401, "Token pekerja tidak valid.")
		}
		return nil
	}
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil || !validSessionToken(cookie.Value, pinHash) {
		return problem(401, "PIN diperlukan")
	}
	return nil
}

// clientIP mengambil IP klien: hop pertama X-Forwarded-For bila ada,
// selain itu alamat remote (fitur 28).
func clientIP(r *http.Request) string {
	if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
		if first, _, _ := strings.Cut(forwarded, ","); strings.TrimSpace(first) != "" {
			return strings.TrimSpace(first)
		}
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}

// loginAttempt melacak kegagalan masuk per IP (fitur 28).
type loginAttempt struct {
	failures     []time.Time
	blockedUntil time.Time
}

const (
	loginMaxFailures  = 5
	loginWindow       = 5 * time.Minute
	loginBlockFor     = 5 * time.Minute
	loginLogMaxEvents = 200
)

// loginBlocked memeriksa apakah IP sedang diblokir rate limit.
func (a *App) loginBlocked(ip string) bool {
	a.loginMu.Lock()
	defer a.loginMu.Unlock()
	attempt := a.loginFails[ip]
	return attempt != nil && time.Now().Before(attempt.blockedUntil)
}

// recordLoginFailure mencatat kegagalan dan memblokir IP bila ambang
// tercapai.
func (a *App) recordLoginFailure(ip string) {
	a.loginMu.Lock()
	defer a.loginMu.Unlock()
	if a.loginFails == nil {
		a.loginFails = map[string]*loginAttempt{}
	}
	attempt := a.loginFails[ip]
	if attempt == nil {
		attempt = &loginAttempt{}
		a.loginFails[ip] = attempt
	}
	now := time.Now()
	kept := attempt.failures[:0]
	for _, at := range attempt.failures {
		if now.Sub(at) < loginWindow {
			kept = append(kept, at)
		}
	}
	attempt.failures = append(kept, now)
	if len(attempt.failures) >= loginMaxFailures {
		attempt.blockedUntil = now.Add(loginBlockFor)
		attempt.failures = nil
	}
}

// recordLoginSuccess mereset hitungan kegagalan IP.
func (a *App) recordLoginSuccess(ip string) {
	a.loginMu.Lock()
	defer a.loginMu.Unlock()
	delete(a.loginFails, ip)
}

// appendLoginLogLocked menambah entri audit masuk (maks 200 FIFO) dan
// menyimpannya. Pemanggil wajib memegang a.mu.
func (a *App) appendLoginLogLocked(ip string, success bool) {
	a.data.LoginLog = append(a.data.LoginLog, loginEvent{Time: now(), IP: ip, Success: success})
	if len(a.data.LoginLog) > loginLogMaxEvents {
		a.data.LoginLog = a.data.LoginLog[len(a.data.LoginLog)-loginLogMaxEvents:]
	}
	_ = a.saveLocked()
}

func (a *App) login(w http.ResponseWriter, r *http.Request) (int, json.RawMessage, error) {
	var body struct {
		PIN string `json:"pin"`
	}
	if err := readObject(w, r, &body); err != nil {
		return 0, nil, err
	}
	a.mu.Lock()
	pinHash := a.data.Settings.PINHash
	a.mu.Unlock()
	if pinHash == "" {
		return 0, nil, problem(400, "PIN tidak aktif.")
	}
	ip := clientIP(r)
	if a.loginBlocked(ip) {
		a.mu.Lock()
		a.appendLoginLogLocked(ip, false)
		a.mu.Unlock()
		return 0, nil, problem(429, "Terlalu banyak percobaan masuk yang gagal. Coba lagi nanti.")
	}
	if !verifyPIN(body.PIN, pinHash) {
		a.recordLoginFailure(ip)
		a.mu.Lock()
		a.appendLoginLogLocked(ip, false)
		a.mu.Unlock()
		return 0, nil, problem(401, "PIN salah.")
	}
	a.recordLoginSuccess(ip)
	a.mu.Lock()
	a.appendLoginLogLocked(ip, true)
	a.mu.Unlock()
	expiry := time.Now().Add(sessionLifetime)
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookieName, Value: makeSessionToken(pinHash, expiry),
		Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode,
		MaxAge: int(sessionLifetime.Seconds()), Expires: expiry,
	})
	return 200, mustJSON(map[string]bool{"ok": true}), nil
}

func (a *App) logout(w http.ResponseWriter, r *http.Request) (int, json.RawMessage, error) {
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookieName, Value: "", Path: "/", HttpOnly: true,
		SameSite: http.SameSiteLaxMode, MaxAge: -1, Expires: time.Unix(0, 0),
	})
	return 200, mustJSON(map[string]bool{"ok": true}), nil
}

// setPIN memasang/mengganti/menghapus PIN lalu menerbitkan cookie
// sesi baru untuk sesi ini (token sesi diturunkan dari hash PIN,
// jadi tanpa cookie baru pengguna langsung terkunci 401 sesudah
// memasang PIN pertamanya). Sesi lain tetap tidak berlaku karena
// hash berubah — itu memang disengaja.
func (a *App) setPIN(w http.ResponseWriter, r *http.Request) (int, json.RawMessage, error) {
	var body struct {
		PIN    string `json:"pin"`
		OldPIN string `json:"oldPin"`
	}
	if err := readObject(w, r, &body); err != nil {
		return 0, nil, err
	}
	a.mu.Lock()
	err := a.setPINLocked(body.PIN, body.OldPIN)
	pinHash := a.data.Settings.PINHash
	a.mu.Unlock()
	if err != nil {
		return 0, nil, err
	}
	if pinHash != "" {
		expiry := time.Now().Add(sessionLifetime)
		http.SetCookie(w, &http.Cookie{
			Name: sessionCookieName, Value: makeSessionToken(pinHash, expiry),
			Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode,
			MaxAge: int(sessionLifetime.Seconds()), Expires: expiry,
		})
	}
	return 200, mustJSON(map[string]any{"ok": true, "hasPin": pinHash != ""}), nil
}

// setPINLocked memasang, mengganti, atau menghapus PIN. Bila PIN sudah
// aktif, PIN lama wajib benar. PIN kosong berarti menghapus PIN.
// Pemanggil wajib memegang a.mu.
func (a *App) setPINLocked(pin, oldPin string) error {
	if a.data.Settings.PINHash != "" && !verifyPIN(oldPin, a.data.Settings.PINHash) {
		return problem(401, "PIN lama salah.")
	}
	pin = strings.TrimSpace(pin)
	previous := a.data.Settings.PINHash
	if pin == "" {
		a.data.Settings.PINHash = ""
	} else {
		if len([]rune(pin)) < 4 || len([]rune(pin)) > 64 {
			return problem(400, "PIN harus 4–64 karakter.")
		}
		hash, err := hashPIN(pin)
		if err != nil {
			return err
		}
		a.data.Settings.PINHash = hash
	}
	if err := a.saveLocked(); err != nil {
		a.data.Settings.PINHash = previous
		return err
	}
	a.rememberKeyLocked(a.data.Settings.PINHash)
	return nil
}
