package musehood

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
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
	if !verifyPIN(body.PIN, pinHash) {
		return 0, nil, problem(401, "PIN salah.")
	}
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
