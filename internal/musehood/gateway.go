package musehood

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
)

var eventLine = regexp.MustCompile(`(?m)^data[ \t]*: ?(.*)$`)
var eventBoundary = regexp.MustCompile(`\n[ \t]*\n`)

func contentText(v any) string {
	switch v := v.(type) {
	case string:
		return v
	case []any:
		var text strings.Builder
		for _, part := range v {
			text.WriteString(contentText(part))
		}
		return text.String()
	case map[string]any:
		if text, ok := v["text"].(string); ok {
			return text
		}
		if text, ok := field(v["text"], "value").(string); ok {
			return text
		}
		if text, ok := v["value"].(string); ok {
			return text
		}
	}
	return ""
}
func field(v any, key string) any { obj, _ := v.(map[string]any); return obj[key] }
func first(v any) any {
	if arr, ok := v.([]any); ok && len(arr) > 0 {
		return arr[0]
	}
	return nil
}
func eventText(value any) (string, error) {
	if e := field(value, "error"); e != nil && e != false && e != "" {
		raw, _ := json.Marshal(e)
		return "", errors.New(string(raw))
	}
	if s, ok := value.(string); ok {
		return s, nil
	}
	choice := first(field(value, "choices"))
	choiceDelta, delta := field(choice, "delta"), field(value, "delta")
	for _, item := range []any{field(choiceDelta, "content"), field(field(choice, "message"), "content"), field(choice, "text"), field(delta, "content"), field(value, "content"), field(value, "output_text"), choiceDelta, delta} {
		if item != nil {
			return contentText(item), nil
		}
	}
	return "", nil
}
func parseEvent(data string) (string, error) {
	switch strings.TrimSpace(data) {
	case "", "[DONE]":
		return "", nil
	}
	var value any
	if json.Unmarshal([]byte(data), &value) != nil {
		return data, nil
	}
	return eventText(value)
}
func parseBlock(lines [][]string) (string, error) {
	if len(lines) == 0 {
		return "", nil
	}
	if len(lines) == 1 {
		return parseEvent(lines[0][1])
	}
	payloads := make([]string, 0, len(lines))
	separate := true
	for _, line := range lines {
		s := line[1]
		payloads = append(payloads, s)
		if strings.TrimSpace(s) != "[DONE]" && !json.Valid([]byte(s)) {
			separate = false
		}
	}
	joined := strings.Join(payloads, "\n")
	if !separate || json.Valid([]byte(joined)) {
		return parseEvent(joined)
	}
	var text strings.Builder
	for _, payload := range payloads {
		part, err := parseEvent(payload)
		if err != nil {
			return "", err
		}
		text.WriteString(part)
	}
	return text.String(), nil
}
func parseCompletion(body string) (string, error) {
	source := strings.TrimSpace(strings.TrimPrefix(body, "\ufeff"))
	if source == "" {
		return "", errors.New("Respons provider kosong.")
	}
	sourceLF := strings.ReplaceAll(source, "\r\n", "\n")
	var text string
	if eventLine.MatchString(sourceLF) {
		var all strings.Builder
		for _, block := range eventBoundary.Split(sourceLF, -1) {
			part, err := parseBlock(eventLine.FindAllStringSubmatch(block, -1))
			if err != nil {
				return "", err
			}
			all.WriteString(part)
		}
		text = all.String()
	} else {
		var value any
		if json.Unmarshal([]byte(source), &value) != nil {
			return source, nil
		}
		var err error
		text, err = eventText(value)
		if err != nil {
			return "", err
		}
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return "", errors.New("Respons provider tidak berisi teks.")
	}
	return text, nil
}

const gatewayLimit = 20 * 1024 * 1024

func limitedRead(r io.Reader, limit int64) ([]byte, bool, error) {
	raw, err := io.ReadAll(io.LimitReader(r, limit+1))
	return raw, len(raw) > int(limit), err
}
func (a *App) gateway(ctx context.Context, cfg settings, route string, body any) ([]byte, error) {
	if cfg.BaseURL == "" {
		return nil, problem(400, "Base URL gateway belum diisi. Buka Pengaturan untuk menyimpan koneksi.")
	}
	if cfg.APIKey == "" {
		return nil, problem(400, "API key gateway belum diisi. Simpan melalui Pengaturan.")
	}
	base := strings.TrimSuffix(strings.TrimRight(cfg.BaseURL, "/"), "/v1")
	method := http.MethodGet
	var input io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		method = http.MethodPost
		input = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, base+"/v1/"+route, input)
	if err != nil {
		return nil, problem(502, "Koneksi gateway gagal: "+err.Error())
	}
	req.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	req.Header.Set("Accept", "application/json, text/event-stream, text/plain")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := a.client.Do(req)
	if err != nil {
		return nil, problem(502, "Koneksi gateway gagal: "+err.Error())
	}
	defer res.Body.Close()
	raw, large, err := limitedRead(res.Body, gatewayLimit)
	if large {
		return nil, problem(502, fmt.Sprintf("HTTP %d: Respons gateway melebihi 20 MB.", res.StatusCode))
	}
	if err != nil {
		return nil, problem(502, fmt.Sprintf("HTTP %d: Gagal membaca respons gateway: %v", res.StatusCode, err))
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		status := res.StatusCode
		if status < 400 || status > 599 {
			status = 502
		}
		return nil, problem(status, fmt.Sprintf("HTTP %d: %s", res.StatusCode, raw))
	}
	return raw, nil
}
func (a *App) models(ctx context.Context, cfg settings) ([]string, error) {
	return a.protocolModels(ctx, cfg)
}
func (a *App) chat(ctx context.Context, cfg settings, s seat, history []gatewayMessage) (string, error) {
	if s.Code == "MUSE" || s.Provider == "muse" {
		return "", errors.New("Muse wajib memakai antrean file.")
	}
	if s.Model == nil || *s.Model == "" {
		return "", problem(400, fmt.Sprintf("Nama model %s (%s) belum diisi di Pengaturan.", s.Name, s.Code))
	}
	raw, err := a.protocolChat(ctx, cfg, *s.Model, history)
	if err != nil {
		return "", err
	}
	text, err := parseProtocolText(protocolOf(cfg), string(raw))
	if err != nil {
		return "", problem(502, fmt.Sprintf("HTTP 200: %v\n%s", err, raw))
	}
	return text, nil
}
func (a *App) generateImage(ctx context.Context, cfg settings, s seat, prompt string) (string, error) {
	if protocolOf(cfg) != protoOpenAI {
		return "", problem(400, "Pembuatan gambar gateway hanya didukung protokol openai. Gunakan jalur gambar pekerja Muse.")
	}
	if s.Model == nil || *s.Model == "" {
		return "", problem(400, fmt.Sprintf("Model gambar %s (%s) belum diisi di Pengaturan.", s.Name, s.Code))
	}
	raw, err := a.gateway(ctx, cfg, "images/generations", map[string]any{"model": *s.Model, "prompt": prompt, "n": 1, "size": "1024x1024"})
	if err != nil {
		return "", err
	}
	var obj any
	if json.Unmarshal(raw, &obj) != nil {
		return "", problem(502, "HTTP 200: Respons gambar bukan JSON. "+string(raw))
	}
	image := first(field(obj, "data"))
	if data, ok := field(image, "b64_json").(string); ok && data != "" {
		decoded, err := base64.StdEncoding.DecodeString(data)
		if err != nil {
			return "", problem(502, "HTTP 200: Gambar Base64 gateway tidak valid.")
		}
		return a.saveImage(decoded, "images")
	}
	if url, ok := field(image, "url").(string); ok && url != "" {
		return a.resolveImage(ctx, url)
	}
	return "", problem(502, "HTTP 200: Respons gateway tidak memuat gambar. "+string(raw))
}

// payloadChunks splits a raw body into per-event payloads when the provider
// replied with SSE despite stream:false; otherwise the whole body is one
// payload. [DONE] sentinels are kept for callers to skip.
func payloadChunks(body string) []string {
	source := strings.TrimSpace(strings.TrimPrefix(body, "\ufeff"))
	if source == "" {
		return nil
	}
	sourceLF := strings.ReplaceAll(source, "\r\n", "\n")
	if eventLine.MatchString(sourceLF) {
		out := []string{}
		for _, block := range eventBoundary.Split(sourceLF, -1) {
			for _, match := range eventLine.FindAllStringSubmatch(block, -1) {
				out = append(out, match[1])
			}
		}
		if len(out) > 0 {
			return out
		}
	}
	return []string{sourceLF}
}

func providerError(value any) error {
	if e := field(value, "error"); e != nil && e != false && e != "" {
		raw, _ := json.Marshal(e)
		return errors.New(string(raw))
	}
	return nil
}

func finishParsed(text, raw string) (string, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return "", errors.New("Respons provider tidak berisi teks.")
	}
	return text, nil
}

func decodePayload(payload string, value any) (ok bool, plain string) {
	if strings.TrimSpace(payload) == "" || strings.TrimSpace(payload) == "[DONE]" {
		return true, ""
	}
	if json.Unmarshal([]byte(payload), value) != nil {
		return true, payload
	}
	return false, ""
}

// parseResponsesText handles the OpenAI Responses protocol: non-stream bodies
// with output[].content[].text plus SSE events of type
// response.output_text.delta.
func parseResponsesText(body string) (string, error) {
	var text strings.Builder
	sawDelta := false
	for _, payload := range payloadChunks(body) {
		var value any
		ok, plain := decodePayload(payload, &value)
		if !ok {
			if err := providerError(value); err != nil {
				return "", err
			}
			switch field(value, "type") {
			case "response.output_text.delta":
				sawDelta = true
				if delta, isStr := field(value, "delta").(string); isStr {
					text.WriteString(delta)
				}
			case "error", "response.failed":
				if err := providerError(field(value, "response")); err != nil {
					return "", err
				}
				return "", fmt.Errorf("respons provider gagal: %s", payload)
			case "response.completed", "response.incomplete":
				// Full snapshots duplicate text already streamed as deltas.
			default:
				if !sawDelta {
					// Non-stream body: concatenate output[].content[].text.
					target := value
					if inner := field(value, "response"); inner != nil {
						target = inner
					}
					if output, list := field(target, "output").([]any); list {
						for _, item := range output {
							if content, parts := field(item, "content").([]any); parts {
								for _, part := range content {
									text.WriteString(contentText(part))
								}
							}
						}
					}
				}
			}
		} else {
			text.WriteString(plain)
		}
	}
	return finishParsed(text.String(), body)
}

// parseAnthropicText handles Anthropic messages: content[].text blocks or SSE
// content_block_delta events with delta.text.
func parseAnthropicText(body string) (string, error) {
	var text strings.Builder
	for _, payload := range payloadChunks(body) {
		var value any
		ok, plain := decodePayload(payload, &value)
		if !ok {
			if err := providerError(value); err != nil {
				return "", err
			}
			switch field(value, "type") {
			case "content_block_delta":
				if delta, isStr := field(field(value, "delta"), "text").(string); isStr {
					text.WriteString(delta)
				}
			case "content_block_start":
				if block, isStr := field(field(value, "content_block"), "text").(string); isStr {
					text.WriteString(block)
				}
			default:
				// Non-stream body: join text blocks of the full message.
				if content, list := field(value, "content").([]any); list {
					for _, block := range content {
						if field(block, "type") == "text" {
							if blockText, isStr := field(block, "text").(string); isStr {
								text.WriteString(blockText)
							}
						}
					}
				}
			}
		} else {
			text.WriteString(plain)
		}
	}
	return finishParsed(text.String(), body)
}

// parseGeminiText handles Gemini generateContent: candidates[0].content.parts
// both as one JSON body and as SSE data events.
func parseGeminiText(body string) (string, error) {
	var text strings.Builder
	for _, payload := range payloadChunks(body) {
		var value any
		ok, plain := decodePayload(payload, &value)
		if !ok {
			if err := providerError(value); err != nil {
				return "", err
			}
			if candidates, list := field(value, "candidates").([]any); list && len(candidates) > 0 {
				if parts, partsList := field(field(candidates[0], "content"), "parts").([]any); partsList {
					for _, part := range parts {
						if partText, isStr := field(part, "text").(string); isStr {
							text.WriteString(partText)
						}
					}
				}
			}
		} else {
			text.WriteString(plain)
		}
	}
	return finishParsed(text.String(), body)
}

// parseOllamaText handles Ollama chat: message.content either in one JSON body
// or as NDJSON lines with partial content per line.
func parseOllamaText(body string) (string, error) {
	var text strings.Builder
	for _, line := range payloadChunks(body) {
		for _, chunk := range strings.Split(line, "\n") {
			chunk = strings.TrimSpace(chunk)
			if chunk == "" {
				continue
			}
			var value any
			if json.Unmarshal([]byte(chunk), &value) != nil {
				text.WriteString(chunk)
				continue
			}
			if err := providerError(value); err != nil {
				return "", err
			}
			if message := field(value, "message"); message != nil {
				if content, isStr := field(message, "content").(string); isStr {
					text.WriteString(content)
				}
			}
		}
	}
	return finishParsed(text.String(), body)
}

func parseProtocolText(proto protocol, body string) (string, error) {
	switch proto {
	case protoResponses:
		return parseResponsesText(body)
	case protoAnthropic:
		return parseAnthropicText(body)
	case protoGemini:
		return parseGeminiText(body)
	case protoOllama:
		return parseOllamaText(body)
	default:
		return parseCompletion(body)
	}
}
