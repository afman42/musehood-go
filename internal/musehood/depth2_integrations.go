package musehood

// Integrasi keluar (grup F): webhook pesan selesai (41), API eksternal
// ber-token (42), jembatan Telegram (43), dan auto-update dari rilis
// GitHub (45).

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// notifyCompletedLocked dipanggil saat pesan kursi selesai dengan teks
// (fitur 41 & 43). Pemanggil wajib memegang a.mu.
func (a *App) notifyCompletedLocked(m *message) {
	if m.Status != "completed" || m.Text == "" || m.AuthorType != "seat" {
		return
	}
	cfg := a.data.Settings
	roomName := ""
	if r := a.data.findRoom(m.RoomID); r != nil {
		roomName = r.Name
	}
	if cfg.WebhookEnabled != nil && *cfg.WebhookEnabled && cfg.WebhookURL != "" {
		payload := map[string]any{
			"event": "message.completed",
			"room":  roomName,
			"seat":  m.Seat,
			"text":  m.Text,
			"time":  now(),
		}
		url := cfg.WebhookURL
		a.wg.Go(func() { a.fireWebhook(url, payload) })
	}
	if cfg.TelegramEnabled != nil && *cfg.TelegramEnabled && cfg.TelegramBotToken != "" && cfg.TelegramChatID != "" {
		token, chatID, text := cfg.TelegramBotToken, cfg.TelegramChatID, truncateRunes(m.Text, 4096)
		a.wg.Go(func() { a.telegramSend(token, chatID, text) })
	}
}

// fireWebhook mengirim payload webhook (fire-and-forget, timeout 10 dtk);
// kegagalan hanya dicatat ke log server (fitur 41).
func (a *App) fireWebhook(url string, payload map[string]any) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(a.ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		log.Printf("webhook: %v", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := a.client.Do(req)
	if err != nil {
		log.Printf("webhook ke %s gagal: %v", url, err)
		return
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		log.Printf("webhook ke %s: status %d", url, resp.StatusCode)
	}
}

// --- API eksternal ber-token (fitur 42) ---

// regenerateExternalTokenLocked membuat token API eksternal baru.
// Pemanggil wajib memegang a.mu.
func (a *App) regenerateExternalTokenLocked() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	token := hex.EncodeToString(raw)
	previous := a.data.Settings.APIToken
	a.data.Settings.APIToken = token
	if err := a.saveLocked(); err != nil {
		a.data.Settings.APIToken = previous
		return "", err
	}
	return token, nil
}

// externalMessage menerima pesan dari API eksternal dengan header
// Authorization: Bearer <token> (fitur 42). Endpoint ini bebas gerbang
// PIN cookie; token adalah kredensialnya.
func (a *App) externalMessage(w http.ResponseWriter, r *http.Request) (int, json.RawMessage, error) {
	var body struct {
		Text string `json:"text"`
	}
	if err := readObject(w, r, &body); err != nil {
		return 0, nil, err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	stored := a.data.Settings.APIToken
	given := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if stored == "" || given == "" || subtle.ConstantTimeCompare([]byte(given), []byte(stored)) != 1 {
		return 0, nil, problem(401, "Token API eksternal tidak valid.")
	}
	j, err := a.submitLocked(body.Text, "", "", nil, a.data.ActiveRoomID, false)
	if err != nil {
		return 0, nil, err
	}
	return 202, mustJSON(map[string]any{"jobId": j.ID}), nil
}

// --- Jembatan Telegram (fitur 43) ---

var (
	telegramBaseURL     = "https://api.telegram.org"
	telegramPollTimeout = 30 // detik
)

type telegramUpdate struct {
	UpdateID int64 `json:"update_id"`
	Message  *struct {
		Text string `json:"text"`
		Chat struct {
			ID int64 `json:"id"`
		} `json:"chat"`
	} `json:"message"`
}

// telegramLoop adalah long-poll getUpdates. Berhenti saat konteks App
// dibatalkan (Close).
func (a *App) telegramLoop() {
	for {
		select {
		case <-a.ctx.Done():
			return
		default:
		}
		a.mu.Lock()
		cfg := a.data.Settings
		offset := a.data.TelegramOffset
		a.mu.Unlock()
		if cfg.TelegramEnabled == nil || !*cfg.TelegramEnabled || cfg.TelegramBotToken == "" || cfg.TelegramChatID == "" {
			select {
			case <-a.ctx.Done():
				return
			case <-time.After(time.Second):
			}
			continue
		}
		a.telegramPollOnce(cfg, offset)
	}
}

func (a *App) telegramPollOnce(cfg settings, offset int64) {
	url := fmt.Sprintf("%s/bot%s/getUpdates?offset=%d&timeout=%d", telegramBaseURL, cfg.TelegramBotToken, offset, telegramPollTimeout)
	ctx, cancel := context.WithTimeout(a.ctx, time.Duration(telegramPollTimeout+15)*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return
	}
	resp, err := a.client.Do(req)
	if err != nil {
		select {
		case <-a.ctx.Done():
		case <-time.After(5 * time.Second):
		}
		return
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil || resp.StatusCode != 200 {
		return
	}
	var parsed struct {
		OK     bool             `json:"ok"`
		Result []telegramUpdate `json:"result"`
	}
	if json.Unmarshal(raw, &parsed) != nil || !parsed.OK {
		return
	}
	for _, update := range parsed.Result {
		a.mu.Lock()
		if update.UpdateID >= a.data.TelegramOffset {
			a.data.TelegramOffset = update.UpdateID + 1
		}
		if update.Message != nil && update.Message.Text != "" &&
			strconv.FormatInt(update.Message.Chat.ID, 10) == a.data.Settings.TelegramChatID {
			_, _ = a.submitLocked(update.Message.Text, "", "", nil, a.data.ActiveRoomID, false)
		}
		_ = a.saveLocked()
		a.mu.Unlock()
	}
}

func (a *App) telegramSend(token, chatID, text string) {
	payload, err := json.Marshal(map[string]string{"chat_id": chatID, "text": text})
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(a.ctx, 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, telegramBaseURL+"/bot"+token+"/sendMessage", bytes.NewReader(payload))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := a.client.Do(req)
	if err != nil {
		log.Printf("telegram sendMessage gagal: %v", err)
		return
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
}

// --- Auto-update (fitur 45) ---

var (
	githubAPIBase  = "https://api.github.com"
	executablePath = os.Executable
	restartProcess = defaultRestartProcess
)

type githubRelease struct {
	TagName string `json:"tag_name"`
	Assets  []struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
	} `json:"assets"`
}

func updateAssetName() string {
	name := "musehood-go-" + runtime.GOOS + "-" + runtime.GOARCH
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return name
}

func (a *App) fetchLatestRelease(ctx context.Context) (*githubRelease, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, githubAPIBase+"/repos/afman42/musehood-go/releases/latest", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "musehood-go")
	resp, err := a.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("GitHub API mengembalikan status %d", resp.StatusCode)
	}
	var release githubRelease
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&release); err != nil {
		return nil, err
	}
	return &release, nil
}

// updateCheck membandingkan versi saat ini dengan rilis terbaru.
func (a *App) updateCheck(ctx context.Context) (map[string]any, error) {
	release, err := a.fetchLatestRelease(ctx)
	if err != nil {
		return nil, err
	}
	current := a.version
	if current == "" {
		current = "dev"
	}
	latest := strings.TrimPrefix(release.TagName, "v")
	available := current == "dev" || strings.TrimPrefix(current, "v") != latest
	assetName := updateAssetName()
	downloadURL := ""
	for _, asset := range release.Assets {
		if asset.Name == assetName {
			downloadURL = asset.BrowserDownloadURL
			break
		}
	}
	return map[string]any{
		"current":         current,
		"latest":          release.TagName,
		"updateAvailable": available,
		"assetName":       assetName,
		"downloadUrl":     downloadURL,
	}, nil
}

// updateApply mengunduh aset rilis terbaru, mengganti binary (rename
// atomik dengan cadangan .bak), lalu me-restart proses.
func (a *App) updateApply(ctx context.Context) (map[string]any, error) {
	release, err := a.fetchLatestRelease(ctx)
	if err != nil {
		return nil, err
	}
	assetName := updateAssetName()
	downloadURL := ""
	for _, asset := range release.Assets {
		if asset.Name == assetName {
			downloadURL = asset.BrowserDownloadURL
			break
		}
	}
	if downloadURL == "" {
		return nil, problem(404, "Aset "+assetName+" tidak ditemukan pada rilis terbaru.")
	}
	exe, err := executablePath()
	if err != nil {
		return nil, err
	}
	dir := filepath.Dir(exe)
	tmp, err := os.CreateTemp(dir, "musehood-go-update-*")
	if err != nil {
		return nil, err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		tmp.Close()
		return nil, err
	}
	req.Header.Set("User-Agent", "musehood-go")
	resp, err := a.client.Do(req)
	if err != nil {
		tmp.Close()
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		tmp.Close()
		return nil, fmt.Errorf("unduhan update mengembalikan status %d", resp.StatusCode)
	}
	written, err := io.Copy(tmp, io.LimitReader(resp.Body, 512<<20))
	if err != nil {
		tmp.Close()
		return nil, err
	}
	if err := tmp.Close(); err != nil {
		return nil, err
	}
	if written == 0 {
		return nil, problem(502, "Unduhan update kosong.")
	}
	if err := os.Chmod(tmpName, 0o755); err != nil {
		return nil, err
	}
	backup := exe + ".bak"
	_ = os.Remove(backup)
	if err := os.Rename(exe, backup); err != nil {
		return nil, err
	}
	if err := os.Rename(tmpName, exe); err != nil {
		_ = os.Rename(backup, exe)
		return nil, err
	}
	if err := restartProcess(exe); err != nil {
		return nil, err
	}
	return map[string]any{"ok": true, "version": release.TagName}, nil
}
