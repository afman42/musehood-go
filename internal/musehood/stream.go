package musehood

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// gatewayStream mengirim satu request POST ke gateway dan meneruskan isi
// respons baris demi baris ke onLine (untuk SSE/NDJSON). Error provider
// dikembalikan apa adanya seperti gatewayDo.
func (a *App) gatewayStream(ctx context.Context, target string, body any, headers map[string]string, onLine func(string)) error {
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, strings.NewReader(string(raw)))
	if err != nil {
		return problem(502, "Koneksi gateway gagal: "+err.Error())
	}
	req.Header.Set("Accept", "text/event-stream, application/json, text/plain")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36")
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := a.client.Do(req)
	if err != nil {
		return problem(502, "Koneksi gateway gagal: "+err.Error())
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		bodyRaw, _, _ := limitedRead(res.Body, gatewayLimit)
		status := res.StatusCode
		if status < 400 || status > 599 {
			status = 502
		}
		return problem(status, "HTTP "+strconv.Itoa(res.StatusCode)+": "+string(bodyRaw))
	}
	reader := bufio.NewReader(res.Body)
	var total int64
	for {
		line, err := reader.ReadString('\n')
		total += int64(len(line))
		if total > gatewayLimit {
			return problem(502, "Respons gateway melebihi 20 MB.")
		}
		if trimmed := strings.TrimRight(line, "\r\n"); trimmed != "" {
			onLine(trimmed)
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return problem(502, "Gagal membaca respons gateway: "+err.Error())
		}
	}
}

// streamDeltaText mengambil potongan teks dari satu payload stream sesuai
// protokol. Mengembalikan "" bila payload bukan potongan teks.
func streamDeltaText(proto protocol, payload string) string {
	trimmed := strings.TrimSpace(payload)
	if trimmed == "" || trimmed == "[DONE]" {
		return ""
	}
	var value any
	if json.Unmarshal([]byte(trimmed), &value) != nil {
		return ""
	}
	switch proto {
	case protoResponses:
		if field(value, "type") == "response.output_text.delta" {
			if delta, ok := field(value, "delta").(string); ok {
				return delta
			}
		}
		return ""
	case protoAnthropic:
		if field(value, "type") == "content_block_delta" {
			if text, ok := field(field(value, "delta"), "text").(string); ok {
				return text
			}
		}
		return ""
	case protoGemini:
		candidate := first(field(value, "candidates"))
		return contentText(field(field(candidate, "content"), "parts"))
	case protoOllama:
		if text, ok := field(field(value, "message"), "content").(string); ok {
			return text
		}
		return ""
	default:
		text, _ := parseEvent(trimmed)
		return text
	}
}

// protocolChatStream menjalankan chat dengan streaming: onDelta dipanggil
// dengan teks terakumulasi setiap potongan baru tiba. Mengembalikan error
// bila stream tidak menghasilkan teks sama sekali (pemanggil boleh jatuh
// kembali ke chat non-stream).
func (a *App) protocolChatStream(ctx context.Context, cfg settings, model string, history []gatewayMessage, onDelta func(partial string)) (string, error) {
	if err := a.requireGatewayConfig(cfg); err != nil {
		return "", err
	}
	proto := protocolOf(cfg)
	var target string
	var body map[string]any
	var headers map[string]string
	switch proto {
	case protoAnthropic:
		system, msgs := splitSystem(history)
		target = openAIBase(cfg) + "/v1/messages"
		body = map[string]any{"model": model, "max_tokens": 1024, "system": system, "messages": msgs, "stream": true}
		headers = map[string]string{"x-api-key": cfg.APIKey, "anthropic-version": "2023-06-01"}
	case protoGemini:
		system, msgs := splitSystem(history)
		contents := make([]map[string]any, 0, len(msgs))
		for _, m := range msgs {
			role := "model"
			if m.Role == "user" {
				role = "user"
			}
			contents = append(contents, map[string]any{"role": role, "parts": []map[string]any{{"text": m.Content}}})
		}
		target = geminiBase(cfg) + "/v1beta/models/" + url.PathEscape(model) + ":streamGenerateContent?alt=sse&key=" + url.QueryEscape(cfg.APIKey)
		body = map[string]any{"contents": contents, "systemInstruction": map[string]any{"parts": []map[string]any{{"text": system}}}}
	case protoResponses:
		target = openAIBase(cfg) + "/v1/responses"
		body = map[string]any{"model": model, "input": history, "stream": true}
		headers = map[string]string{"Authorization": "Bearer " + cfg.APIKey}
	case protoOllama:
		headers = map[string]string{}
		if cfg.APIKey != "" {
			headers["Authorization"] = "Bearer " + cfg.APIKey
		}
		target = ollamaBase(cfg) + "/api/chat"
		body = map[string]any{"model": model, "messages": history, "stream": true}
	default:
		target = openAIBase(cfg) + "/v1/chat/completions"
		body = map[string]any{"model": model, "messages": history, "stream": true}
		headers = map[string]string{"Authorization": "Bearer " + cfg.APIKey}
	}
	var accumulated strings.Builder
	gotAny := false
	err := a.gatewayStream(ctx, target, body, headers, func(line string) {
		payload := line
		if proto != protoOllama {
			if !strings.HasPrefix(line, "data:") {
				return
			}
			payload = strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		}
		chunk := streamDeltaText(proto, payload)
		if chunk == "" {
			return
		}
		gotAny = true
		accumulated.WriteString(chunk)
		if onDelta != nil {
			onDelta(accumulated.String())
		}
	})
	if err != nil {
		return "", err
	}
	if !gotAny || strings.TrimSpace(accumulated.String()) == "" {
		return "", errors.New("Stream gateway tidak menghasilkan teks.")
	}
	return accumulated.String(), nil
}
