package musehood

// Fitur pendalaman grup B (Gateway): circuit breaker per provider (7),
// retry + backoff (8), dan tool-calling native openai (10). Streaming
// anthropic/gemini/ollama (9) sudah ada di stream.go; load balancing
// (12) ada di engine.go.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// --- Circuit breaker (fitur 7) ---

const breakerFailureThreshold = 5

// breakerOpenDuration adalah lama breaker terbuka sebelum percobaan
// half-open. Variabel paket agar tes bisa memperpendeknya.
var breakerOpenDuration = 60 * time.Second

type circuitBreaker struct {
	failures      int
	openUntil     time.Time
	halfOpenTrial bool
}

// breakerKeyForURL menurunkan kunci breaker (skema+host) dari URL target.
func breakerKeyForURL(target string) string {
	u, err := url.Parse(target)
	if err != nil || u.Host == "" {
		return target
	}
	return u.Scheme + "://" + u.Host
}

// countedGatewayFailure menentukan apakah error gateway dihitung sebagai
// kegagalan provider untuk breaker dan layak di-retry: error transport
// (502 dari lapisan gateway), 429, dan 5xx. 4xx lain (400/401/404…)
// adalah kesalahan permintaan, bukan kesehatan provider.
func countedGatewayFailure(err error) bool {
	var apiErr *apiError
	if !errors.As(err, &apiErr) {
		return false
	}
	return apiErr.status == http.StatusTooManyRequests || apiErr.status >= 500
}

// breakerAllow memeriksa apakah panggilan ke host boleh jalan. Bila
// breaker terbuka, panggilan langsung gagal cepat; setelah jeda, satu
// panggilan percobaan (half-open) diizinkan.
func (a *App) breakerAllow(key string) error {
	a.breakerMu.Lock()
	defer a.breakerMu.Unlock()
	b := a.breakers[key]
	if b == nil {
		return nil
	}
	if b.failures < breakerFailureThreshold {
		return nil
	}
	if time.Now().Before(b.openUntil) {
		return problem(503, "Circuit breaker provider terbuka karena kegagalan berurutan; coba lagi nanti.")
	}
	if b.halfOpenTrial {
		return problem(503, "Circuit breaker provider sedang percobaan half-open; coba lagi nanti.")
	}
	b.halfOpenTrial = true
	return nil
}

// breakerRecord mencatat hasil akhir satu panggilan logis (setelah retry).
func (a *App) breakerRecord(key string, err error) {
	a.breakerMu.Lock()
	defer a.breakerMu.Unlock()
	if a.breakers == nil {
		a.breakers = map[string]*circuitBreaker{}
	}
	b := a.breakers[key]
	if b == nil {
		b = &circuitBreaker{}
		a.breakers[key] = b
	}
	if err == nil {
		b.failures = 0
		b.openUntil = time.Time{}
		b.halfOpenTrial = false
		return
	}
	if !countedGatewayFailure(err) {
		// Error permintaan (4xx): bukan sinyal kesehatan; lepaskan slot
		// percobaan half-open agar panggilan berikutnya bisa mencoba.
		b.halfOpenTrial = false
		return
	}
	b.failures++
	b.halfOpenTrial = false
	if b.failures >= breakerFailureThreshold {
		b.openUntil = time.Now().Add(breakerOpenDuration)
	}
}

// breakerState mengembalikan status breaker untuk satu BaseURL provider:
// "closed", "open", atau "half-open" (fitur 7, badge di UI).
func (a *App) breakerState(baseURL string) string {
	key := breakerKeyForURL(baseURL)
	a.breakerMu.Lock()
	defer a.breakerMu.Unlock()
	b := a.breakers[key]
	if b == nil || b.failures < breakerFailureThreshold {
		return "closed"
	}
	if time.Now().Before(b.openUntil) {
		return "open"
	}
	return "half-open"
}

// --- Retry + backoff (fitur 8) ---

// retryDelays adalah jeda sebelum percobaan ulang ke-1 dan ke-2.
// Variabel paket agar tes bisa memperpendeknya.
var retryDelays = []time.Duration{time.Second, 3 * time.Second}

const retryAfterCap = 10 * time.Second

// gatewayAttempt adalah hasil satu percobaan HTTP gateway: raw body,
// status HTTP (0 bila error transport), jeda Retry-After dari header,
// dan error (nil bila sukses).
type gatewayAttempt struct {
	raw        []byte
	status     int
	retryAfter time.Duration
	err        error
}

// parseRetryAfter membaca header Retry-After (detik) dengan batas atas.
func parseRetryAfter(res *http.Response) time.Duration {
	if res == nil {
		return 0
	}
	value := strings.TrimSpace(res.Header.Get("Retry-After"))
	if value == "" {
		return 0
	}
	seconds, err := strconv.Atoi(value)
	if err != nil || seconds <= 0 {
		return 0
	}
	delay := time.Duration(seconds) * time.Second
	if delay > retryAfterCap {
		delay = retryAfterCap
	}
	return delay
}

// runGatewayAttempts menjalankan percobaan gateway dengan circuit breaker
// dan retry: kegagalan terhitung (transport/429/5xx) diulang maks 2x
// dengan jeda 1 dtk lalu 3 dtk (atau Retry-After bila ada). Breaker hanya
// mencatat hasil akhir.
func (a *App) runGatewayAttempts(ctx context.Context, target string, attempt func() gatewayAttempt) ([]byte, error) {
	key := breakerKeyForURL(target)
	if err := a.breakerAllow(key); err != nil {
		return nil, err
	}
	var last gatewayAttempt
	for try := 0; try <= len(retryDelays); try++ {
		if try > 0 {
			delay := retryDelays[try-1]
			if last.retryAfter > 0 {
				delay = last.retryAfter
			}
			select {
			case <-ctx.Done():
				a.breakerRecord(key, ctx.Err())
				return nil, ctx.Err()
			case <-time.After(delay):
			}
		}
		last = attempt()
		if last.err == nil || !countedGatewayFailure(last.err) {
			break
		}
	}
	a.breakerRecord(key, last.err)
	return last.raw, last.err
}

// validateWebhookURL memvalidasi URL webhook keluar (fitur 41): harus
// http/https; host privat hanya boleh loopback (localhost/127.0.0.1/::1),
// host publik lolos. Dipakai saat pengaturan disimpan.
func validateWebhookURL(raw string) error {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return problem(400, "URL webhook harus http/https yang valid.")
	}
	host := u.Hostname()
	if host == "" {
		return problem(400, "URL webhook harus punya host.")
	}
	if strings.EqualFold(host, "localhost") {
		return nil
	}
	ips, err := lookupIP(context.Background(), host)
	if err != nil || len(ips) == 0 {
		return problem(400, "Host webhook tidak dapat dijangkau.")
	}
	for _, ip := range ips {
		if ip.IsLoopback() {
			continue
		}
		if ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified() {
			return problem(400, "Host webhook privat hanya boleh localhost/127.0.0.1/::1.")
		}
	}
	return nil
}

// --- Tool-calling native (fitur 10) ---

// nativeToolsEnabled membaca saklar tool-calling native (nil = aktif).
func (a *App) nativeToolsEnabled() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.data.Settings.nativeToolsOn()
}

const nativeToolNote = "\n\nKamu dapat memakai tool yang disediakan lewat mekanisme tool-calling. Gunakan tool hanya bila benar-benar perlu (mis. butuh info terkini dari web atau isi file). Bila tidak perlu, jawab langsung."

// nativeToolDefinitions membangun daftar tools[] format OpenAI dari
// instruksi tool teks (satu fungsi per tool, parameter tunggal "arg").
func nativeToolDefinitions(instructions string) []map[string]any {
	defs := []map[string]any{}
	for _, line := range strings.Split(instructions, "\n") {
		if !strings.HasPrefix(line, "- ") {
			continue
		}
		name, rest, found := strings.Cut(strings.TrimPrefix(line, "- "), ": ")
		if !found || !customToolNamePattern.MatchString(name) {
			continue
		}
		defs = append(defs, map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        name,
				"description": strings.TrimSpace(rest),
				"parameters": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"arg": map[string]any{"type": "string", "description": "Argumen tool sesuai deskripsi."},
					},
					"required": []string{"arg"},
				},
			},
		})
	}
	return defs
}

// stripTextToolInstructions membuang blok instruksi tool protokol teks
// dari konten system untuk jalur native.
func stripTextToolInstructions(system string) string {
	if idx := strings.Index(system, "\n\nKamu dapat memakai tool berikut."); idx >= 0 {
		return system[:idx]
	}
	return system
}

// nativeToolCall adalah satu panggilan tool dari respons native.
type nativeToolCall struct {
	id   string
	name string
	arg  string
}

// parseNativeToolCalls mengambil tool_calls dari respons chat completions.
// Respons non-JSON (teks polos dari gateway sederhana) diperlakukan
// sebagai teks biasa.
func parseNativeToolCalls(raw []byte) ([]nativeToolCall, string) {
	var obj map[string]any
	if json.Unmarshal(raw, &obj) != nil {
		// Provider membalas SSE walau diminta non-streaming: urai teksnya
		// seperti jalur chat biasa, jangan tampilkan payload mentah.
		if text, err := parseProtocolText(protoOpenAI, string(raw)); err == nil && text != "" {
			return nil, text
		}
		return nil, strings.TrimSpace(string(raw))
	}
	choice := first(field(obj, "choices"))
	message := field(choice, "message")
	content := contentText(field(message, "content"))
	var calls []nativeToolCall
	rawCalls, _ := field(message, "tool_calls").([]any)
	for _, item := range rawCalls {
		fn := field(item, "function")
		name, _ := field(fn, "name").(string)
		if name == "" {
			continue
		}
		id, _ := field(item, "id").(string)
		arg := ""
		switch args := field(fn, "arguments").(type) {
		case string:
			var parsed map[string]any
			if json.Unmarshal([]byte(args), &parsed) == nil {
				if v, ok := parsed["arg"].(string); ok {
					arg = v
				}
			}
			if arg == "" {
				arg = args
			}
		case map[string]any:
			if v, ok := args["arg"].(string); ok {
				arg = v
			}
		}
		calls = append(calls, nativeToolCall{id: id, name: name, arg: arg})
	}
	if content == "" && len(calls) == 0 {
		if text, err := eventText(obj); err == nil {
			content = text
		}
	}
	return calls, content
}

// chatWithNativeTools menjalankan chat dengan tool-calling native OpenAI.
// Mengembalikan ok=false (tanpa error) bila jalur native tidak bisa dipakai
// (permintaan pertama gagal, mis. provider menolak tools[]) sehingga
// pemanggil jatuh ke protokol teks.
func (a *App) chatWithNativeTools(ctx context.Context, cfg settings, s seat, history []gatewayMessage, code bool, onActivity func(toolName string), onDelta func(partial string)) (workerReply, bool, error) {
	if s.Model == nil || *s.Model == "" {
		return workerReply{}, false, nil
	}
	defs := nativeToolDefinitions(a.buildToolInstructions(code, s))
	if len(defs) == 0 {
		return workerReply{}, false, nil
	}
	messages := make([]map[string]any, 0, len(history)+2)
	for i, m := range history {
		content := m.Content
		if i == 0 && m.Role == "system" {
			content = stripTextToolInstructions(content) + nativeToolNote
		}
		messages = append(messages, map[string]any{"role": m.Role, "content": content})
	}
	// Cache respons (fitur lama) tetap berlaku pada jalur native:
	// jawaban teks polos tanpa tool disimpan dan dipakai ulang.
	cacheKey := ""
	if a.responseCacheEnabled() {
		cacheKey = responseCacheKey(cfg, *s.Model, history)
		if text, ok := a.cacheGet(cacheKey); ok {
			return workerReply{Text: text}, true, nil
		}
	}
	target := openAIBase(cfg) + "/v1/chat/completions"
	headers := map[string]string{"Authorization": "Bearer " + cfg.APIKey}
	var used []string
	var calls []toolCallRecord
	var imageURL string
	for round := 0; round <= maxToolRounds; round++ {
		start := time.Now()
		raw, err := a.gatewayDo(ctx, http.MethodPost, target, map[string]any{
			"model": *s.Model, "messages": messages, "tools": defs, "tool_choice": "auto", "stream": false,
		}, headers)
		if err != nil {
			if round == 0 {
				// Provider menolak jalur native: jatuh ke protokol teks.
				return workerReply{}, false, nil
			}
			a.recordStat(s, cfg, *s.Model, false, time.Since(start), 0, 0)
			return workerReply{}, true, err
		}
		promptTokens, completionTokens := extractUsage(raw)
		a.recordStat(s, cfg, *s.Model, true, time.Since(start), promptTokens, completionTokens)
		toolCalls, content := parseNativeToolCalls(raw)
		if len(toolCalls) == 0 {
			// Model menjawab dengan blok tool protokol teks (provider
			// mengabaikan tools[]): jalankan tool-nya dan lanjutkan
			// dengan pesan gaya protokol teks.
			if name, arg, found := parseToolBlock(content); found && round < maxToolRounds {
				if onActivity != nil {
					onActivity(name)
				}
				result, image := a.runTool(ctx, cfg, s, name, arg, code)
				if image != "" {
					imageURL = image
				}
				used = append(used, name)
				calls = append(calls, toolCallRecord{
					Name:   name,
					Arg:    truncateRunes(redactToolArg(name, arg), 300),
					Result: truncateRunes(result, 600),
					OK:     toolResultOK(result),
				})
				messages = append(messages,
					map[string]any{"role": "assistant", "content": content},
					map[string]any{"role": "user", "content": fmt.Sprintf("Hasil tool %s:\n%s\n\nLanjutkan jawabanmu untuk pengguna berdasarkan hasil di atas.", name, result)},
				)
				continue
			}
			text := stripToolBlocks(content)
			if onDelta != nil && text != "" {
				onDelta(text)
			}
			if cacheKey != "" && text != "" && len(used) == 0 {
				a.cachePut(cacheKey, text)
			}
			return workerReply{Text: text, ImageURL: imageURL, ToolsUsed: used, ToolCalls: calls}, true, nil
		}
		if round == maxToolRounds {
			text := stripToolBlocks(content)
			if onDelta != nil && text != "" {
				onDelta(text)
			}
			return workerReply{Text: text, ImageURL: imageURL, ToolsUsed: used, ToolCalls: calls}, true, nil
		}
		assistantCalls := make([]map[string]any, 0, len(toolCalls))
		for _, call := range toolCalls {
			assistantCalls = append(assistantCalls, map[string]any{
				"id": call.id, "type": "function",
				"function": map[string]any{"name": call.name, "arguments": fmt.Sprintf(`{"arg":%s}`, strconv.Quote(call.arg))},
			})
		}
		messages = append(messages, map[string]any{"role": "assistant", "content": content, "tool_calls": assistantCalls})
		for _, call := range toolCalls {
			if onActivity != nil {
				onActivity(call.name)
			}
			result, image := a.runTool(ctx, cfg, s, call.name, call.arg, code)
			if image != "" {
				imageURL = image
			}
			used = append(used, call.name)
			calls = append(calls, toolCallRecord{
				Name:   call.name,
				Arg:    truncateRunes(redactToolArg(call.name, call.arg), 300),
				Result: truncateRunes(result, 600),
				OK:     toolResultOK(result),
			})
			messages = append(messages, map[string]any{"role": "tool", "tool_call_id": call.id, "content": result})
		}
	}
	return workerReply{}, false, nil
}
