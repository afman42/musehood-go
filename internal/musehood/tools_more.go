package musehood

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"html"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Tool tambahan (batch 27 Nov 2026): translate_text, ocr_image,
// image_caption, qr_code, rss_feed, youtube_transcript, currency_convert,
// text_to_speech, http_post, password_gen. URL API eksternal dibuat
// variabel agar tes dapat menggantinya dengan server lokal.

var (
	translateAPIURL = "https://api.mymemory.translated.net/get"
	currencyAPIURL  = "https://api.frankfurter.app/latest"
	qrAPIURL        = "https://api.qrserver.com/v1/create-qr-code/"
	youtubeBaseURL  = "https://www.youtube.com"
)

var langPairPattern = regexp.MustCompile(`^[a-z]{2,3}>[a-z]{2,3}$`)

// toolTranslateText menerjemahkan teks via MyMemory (gratis, tanpa kunci).
// Format argumen: baris pertama pasangan bahasa "id>en", sisanya teks.
func (a *App) toolTranslateText(ctx context.Context, arg string) string {
	pair, text, found := strings.Cut(arg, "\n")
	pair = strings.ToLower(strings.TrimSpace(pair))
	if !found || !langPairPattern.MatchString(pair) {
		return "Error: baris pertama argumen harus pasangan bahasa sumber>tujuan, mis. id>en; baris berikutnya teks yang diterjemahkan."
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return "Error: teks yang diterjemahkan kosong."
	}
	if len([]rune(text)) > 500 {
		return "Error: teks terlalu panjang (maks 500 karakter)."
	}
	from, to, _ := strings.Cut(pair, ">")
	target := translateAPIURL + "?q=" + url.QueryEscape(text) + "&langpair=" + url.QueryEscape(from+"|"+to)
	body, err := toolGet(ctx, target)
	if err != nil {
		return "Error: terjemahan gagal (" + err.Error() + ")."
	}
	var obj struct {
		ResponseData struct {
			TranslatedText string `json:"translatedText"`
		} `json:"responseData"`
		ResponseStatus any `json:"responseStatus"`
	}
	if json.Unmarshal([]byte(body), &obj) != nil || obj.ResponseData.TranslatedText == "" {
		return "Error: layanan terjemahan mengembalikan respons tidak dikenal."
	}
	return truncateToolResult(obj.ResponseData.TranslatedText)
}

// toolOCRImage membaca teks dari gambar sandbox dengan tesseract.
func (a *App) toolOCRImage(ctx context.Context, arg string) string {
	full, err := a.sandboxPath(arg)
	if err != nil {
		return "Error: " + err.Error()
	}
	raw, err := os.ReadFile(full)
	if err != nil {
		return "Error: file tidak ditemukan."
	}
	if imageExtension(raw) == "" {
		return "Error: file bukan gambar PNG/JPEG/GIF/WebP."
	}
	bin, err := exec.LookPath("tesseract")
	if err != nil {
		return "Error: tesseract tidak terpasang di server (apt install tesseract-ocr)."
	}
	run := func(lang string) (string, error) {
		cctx, cancel := context.WithTimeout(ctx, 60*time.Second)
		defer cancel()
		out, err := exec.CommandContext(cctx, bin, full, "stdout", "--psm", "6", "-l", lang).Output()
		return string(out), err
	}
	text, err := run("ind+eng")
	if err != nil {
		text, err = run("eng")
	}
	if err != nil {
		return "Error: OCR gagal (" + err.Error() + ")."
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return "Tidak ada teks yang terbaca pada gambar."
	}
	return truncateToolResult(text)
}

// gatewayVision mengirim satu gambar + prompt ke model kursi dalam bentuk
// yang diharapkan tiap protokol, lalu mengembalikan teks balasannya.
func (a *App) gatewayVision(ctx context.Context, cfg settings, model, mime, encoded, prompt string) (string, error) {
	if err := a.requireGatewayConfig(cfg); err != nil {
		return "", err
	}
	var raw []byte
	var err error
	switch proto := protocolOf(cfg); proto {
	case protoAnthropic:
		raw, err = a.gatewayDo(ctx, http.MethodPost, openAIBase(cfg)+"/v1/messages", map[string]any{
			"model": model, "max_tokens": 1024,
			"messages": []map[string]any{{"role": "user", "content": []map[string]any{
				{"type": "image", "source": map[string]any{"type": "base64", "media_type": mime, "data": encoded}},
				{"type": "text", "text": prompt},
			}}},
		}, map[string]string{"x-api-key": cfg.APIKey, "anthropic-version": "2023-06-01"})
	case protoGemini:
		target := geminiBase(cfg) + "/v1beta/models/" + url.PathEscape(model) + ":generateContent?key=" + url.QueryEscape(cfg.APIKey)
		raw, err = a.gatewayDo(ctx, http.MethodPost, target, map[string]any{
			"contents": []map[string]any{{"role": "user", "parts": []map[string]any{
				{"text": prompt},
				{"inlineData": map[string]any{"mimeType": mime, "data": encoded}},
			}}},
		}, nil)
	case protoResponses:
		raw, err = a.gatewayDo(ctx, http.MethodPost, openAIBase(cfg)+"/v1/responses", map[string]any{
			"model": model,
			"input": []map[string]any{{"role": "user", "content": []map[string]any{
				{"type": "input_text", "text": prompt},
				{"type": "input_image", "image_url": "data:" + mime + ";base64," + encoded},
			}}},
		}, map[string]string{"Authorization": "Bearer " + cfg.APIKey})
	case protoOllama:
		headers := map[string]string{}
		if cfg.APIKey != "" {
			headers["Authorization"] = "Bearer " + cfg.APIKey
		}
		raw, err = a.gatewayDo(ctx, http.MethodPost, ollamaBase(cfg)+"/api/chat", map[string]any{
			"model": model, "stream": false,
			"messages": []map[string]any{{"role": "user", "content": prompt, "images": []string{encoded}}},
		}, headers)
	default:
		raw, err = a.gatewayDo(ctx, http.MethodPost, openAIBase(cfg)+"/v1/chat/completions", map[string]any{
			"model": model, "stream": false,
			"messages": []map[string]any{{"role": "user", "content": []map[string]any{
				{"type": "text", "text": prompt},
				{"type": "image_url", "image_url": map[string]any{"url": "data:" + mime + ";base64," + encoded}},
			}}},
		}, map[string]string{"Authorization": "Bearer " + cfg.APIKey})
	}
	if err != nil {
		return "", err
	}
	return parseProtocolText(protocolOf(cfg), string(raw))
}

var imageMimeByExt = map[string]string{
	"png": "image/png", "jpg": "image/jpeg", "gif": "image/gif", "webp": "image/webp",
}

// toolImageCaption menjelaskan gambar sandbox memakai model vision kursi.
func (a *App) toolImageCaption(ctx context.Context, cfg settings, s seat, arg string) string {
	name, question, _ := strings.Cut(arg, "\n")
	full, err := a.sandboxPath(name)
	if err != nil {
		return "Error: " + err.Error()
	}
	raw, err := os.ReadFile(full)
	if err != nil {
		return "Error: file tidak ditemukan."
	}
	ext := imageExtension(raw)
	if ext == "" {
		return "Error: file bukan gambar PNG/JPEG/GIF/WebP."
	}
	if len(raw) > 5*1024*1024 {
		return "Error: gambar terlalu besar (maks 5 MB); perkecil dulu dengan resize_image."
	}
	if s.Model == nil || *s.Model == "" {
		return fmt.Sprintf("Error: model %s (%s) belum diisi di Pengaturan.", s.Name, s.Code)
	}
	prompt := "Jelaskan isi gambar ini secara ringkas dan jelas dalam Bahasa Indonesia."
	if q := strings.TrimSpace(question); q != "" {
		prompt = q
	}
	text, err := a.gatewayVision(ctx, cfg, *s.Model, imageMimeByExt[ext], base64.StdEncoding.EncodeToString(raw), prompt)
	if err != nil {
		return "Error: penjelasan gambar gagal: " + err.Error()
	}
	return truncateToolResult(text)
}

// toolQRCode membuat QR code PNG dari teks/URL via api.qrserver.com.
func (a *App) toolQRCode(ctx context.Context, arg string) (string, string) {
	text := strings.TrimSpace(arg)
	if text == "" {
		return "Error: teks QR kosong.", ""
	}
	if len([]rune(text)) > 500 {
		return "Error: teks QR terlalu panjang (maks 500 karakter).", ""
	}
	body, err := toolGet(ctx, qrAPIURL+"?size=512x512&margin=16&data="+url.QueryEscape(text))
	if err != nil {
		return "Error: pembuatan QR gagal (" + err.Error() + ").", ""
	}
	image, err := a.saveImage([]byte(body), "images")
	if err != nil {
		return "Error: hasil QR bukan gambar valid.", ""
	}
	return "QR code berhasil dibuat dan terlampir pada balasanmu. Sampaikan kepada pengguna bahwa QR-nya sudah jadi.", image
}

type rssDocument struct {
	XMLName xml.Name `xml:"rss"`
	Channel struct {
		Title string `xml:"title"`
		Items []struct {
			Title       string `xml:"title"`
			Link        string `xml:"link"`
			PubDate     string `xml:"pubDate"`
			Description string `xml:"description"`
		} `xml:"item"`
	} `xml:"channel"`
}

type atomDocument struct {
	XMLName xml.Name `xml:"feed"`
	Title   string   `xml:"title"`
	Entries []struct {
		Title string `xml:"title"`
		Link  struct {
			Href string `xml:"href,attr"`
		} `xml:"link"`
		Updated string `xml:"updated"`
		Summary string `xml:"summary"`
	} `xml:"entry"`
}

// toolRSSFeed membaca feed RSS 2.0 atau Atom dan mengembalikan item terbaru.
func (a *App) toolRSSFeed(ctx context.Context, arg string) string {
	rawURL, countLine, _ := strings.Cut(arg, "\n")
	rawURL = strings.TrimSpace(rawURL)
	if err := checkPublicURL(ctx, rawURL); err != nil {
		return "Error: " + err.Error()
	}
	count := 5
	if n, err := strconv.Atoi(strings.TrimSpace(countLine)); err == nil && n >= 1 && n <= 10 {
		count = n
	}
	body, err := toolGet(ctx, rawURL)
	if err != nil {
		return "Error: gagal mengambil feed (" + err.Error() + ")."
	}
	var lines []string
	var rss rssDocument
	if xml.Unmarshal([]byte(body), &rss) == nil && len(rss.Channel.Items) > 0 {
		for _, item := range rss.Channel.Items {
			if len(lines) >= count {
				break
			}
			line := "- " + strings.TrimSpace(htmlToText(item.Title))
			if d := strings.TrimSpace(item.PubDate); d != "" {
				line += " (" + d + ")"
			}
			if l := strings.TrimSpace(item.Link); l != "" {
				line += "\n  " + l
			}
			lines = append(lines, line)
		}
		return truncateToolResult(strings.Join(lines, "\n"))
	}
	var atom atomDocument
	if xml.Unmarshal([]byte(body), &atom) == nil && len(atom.Entries) > 0 {
		for _, entry := range atom.Entries {
			if len(lines) >= count {
				break
			}
			line := "- " + strings.TrimSpace(htmlToText(entry.Title))
			if d := strings.TrimSpace(entry.Updated); d != "" {
				line += " (" + d + ")"
			}
			if l := strings.TrimSpace(entry.Link.Href); l != "" {
				line += "\n  " + l
			}
			lines = append(lines, line)
		}
		return truncateToolResult(strings.Join(lines, "\n"))
	}
	return "Error: URL itu bukan feed RSS/Atom yang dapat dibaca."
}

var youtubeIDPattern = regexp.MustCompile(`(?:v=|youtu\.be/|shorts/|embed/|^)([A-Za-z0-9_-]{11})(?:[&?#\s]|$)`)

// extractCaptionTracks mengambil array JSON captionTracks dari halaman
// tontonan YouTube dengan pemindaian kurung seimbang.
func extractCaptionTracks(page string) []struct {
	BaseURL      string `json:"baseUrl"`
	LanguageCode string `json:"languageCode"`
} {
	idx := strings.Index(page, `"captionTracks":`)
	if idx < 0 {
		return nil
	}
	start := strings.Index(page[idx:], "[")
	if start < 0 {
		return nil
	}
	start += idx
	depth := 0
	inString := false
	escaped := false
	for i := start; i < len(page); i++ {
		c := page[i]
		if inString {
			if escaped {
				escaped = false
			} else if c == '\\' {
				escaped = true
			} else if c == '"' {
				inString = false
			}
			continue
		}
		switch c {
		case '"':
			inString = true
		case '[':
			depth++
		case ']':
			depth--
			if depth == 0 {
				var tracks []struct {
					BaseURL      string `json:"baseUrl"`
					LanguageCode string `json:"languageCode"`
				}
				if json.Unmarshal([]byte(page[start:i+1]), &tracks) == nil {
					return tracks
				}
				return nil
			}
		}
	}
	return nil
}

var timedTextPattern = regexp.MustCompile(`(?s)<text[^>]*>(.*?)</text>`)

// toolYouTubeTranscript mengambil caption video YouTube tanpa kunci API.
func (a *App) toolYouTubeTranscript(ctx context.Context, arg string) string {
	m := youtubeIDPattern.FindStringSubmatch(strings.TrimSpace(arg))
	if m == nil {
		return "Error: URL atau ID video YouTube tidak dikenali."
	}
	videoID := m[1]
	page, err := toolGet(ctx, youtubeBaseURL+"/watch?v="+videoID)
	if err != nil {
		return "Error: gagal membuka halaman video (" + err.Error() + ")."
	}
	tracks := extractCaptionTracks(page)
	if len(tracks) == 0 {
		return "Error: video ini tidak memiliki caption/transkrip."
	}
	track := tracks[0]
	for _, want := range []string{"id", "en"} {
		for _, t := range tracks {
			if t.LanguageCode == want {
				track = t
			}
		}
		if track.LanguageCode == want {
			break
		}
	}
	captionXML, err := toolGet(ctx, track.BaseURL)
	if err != nil {
		return "Error: gagal mengambil transkrip (" + err.Error() + ")."
	}
	var parts []string
	for _, seg := range timedTextPattern.FindAllStringSubmatch(captionXML, -1) {
		text := strings.TrimSpace(html.UnescapeString(tagPattern.ReplaceAllString(seg[1], "")))
		if text != "" {
			parts = append(parts, text)
		}
	}
	if len(parts) == 0 {
		return "Error: transkrip kosong."
	}
	return truncateToolResult(strings.Join(parts, " "))
}

var currencyCodePattern = regexp.MustCompile(`^[A-Z]{3}$`)

// toolCurrencyConvert mengonversi mata uang via frankfurter.app (ECB).
func (a *App) toolCurrencyConvert(ctx context.Context, arg string) string {
	fields := strings.Fields(strings.ToUpper(strings.TrimSpace(arg)))
	amount := 1.0
	if len(fields) == 3 {
		v, err := strconv.ParseFloat(fields[0], 64)
		if err != nil || v <= 0 {
			return "Error: jumlah tidak valid."
		}
		amount = v
		fields = fields[1:]
	}
	if len(fields) != 2 || !currencyCodePattern.MatchString(fields[0]) || !currencyCodePattern.MatchString(fields[1]) {
		return `Error: format argumen harus "100 USD IDR" (jumlah boleh dikosongkan).`
	}
	from, to := fields[0], fields[1]
	target := currencyAPIURL + "?amount=" + strconv.FormatFloat(amount, 'f', -1, 64) + "&from=" + from + "&to=" + to
	body, err := toolGet(ctx, target)
	if err != nil {
		return "Error: konversi gagal (" + err.Error() + ")."
	}
	var obj struct {
		Amount float64            `json:"amount"`
		Base   string             `json:"base"`
		Date   string             `json:"date"`
		Rates  map[string]float64 `json:"rates"`
	}
	if json.Unmarshal([]byte(body), &obj) != nil {
		return "Error: layanan kurs mengembalikan respons tidak dikenal."
	}
	rate, ok := obj.Rates[to]
	if !ok {
		return "Error: kode mata uang " + to + " tidak didukung layanan kurs."
	}
	return fmt.Sprintf("%s %s = %s %s (kurs %s)", trimFloat(obj.Amount), from, trimFloat(rate), to, obj.Date)
}

func trimFloat(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}

var ttsVoicePattern = regexp.MustCompile(`^[a-z]{2,3}(-[a-z]+)?$`)

// toolTextToSpeech mengubah teks menjadi WAV dengan espeak-ng.
func (a *App) toolTextToSpeech(ctx context.Context, arg string) string {
	voice := "id"
	text := arg
	if first, rest, found := strings.Cut(arg, "\n"); found {
		if v, ok := strings.CutPrefix(strings.TrimSpace(first), "lang="); ok {
			voice = strings.ToLower(strings.TrimSpace(v))
			text = rest
		}
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return "Error: teks kosong."
	}
	if len([]rune(text)) > 1000 {
		return "Error: teks terlalu panjang (maks 1000 karakter)."
	}
	if !ttsVoicePattern.MatchString(voice) {
		return "Error: kode bahasa tidak valid (contoh: id, en)."
	}
	bin, err := exec.LookPath("espeak-ng")
	if err != nil {
		return "Error: espeak-ng tidak terpasang di server (apt install espeak-ng)."
	}
	if err := os.MkdirAll(a.sandboxDir(), 0o755); err != nil {
		return "Error: gagal membuat folder kerja."
	}
	name := "tts-" + uuid() + ".wav"
	outPath := filepath.Join(a.sandboxDir(), name)
	cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, bin, "-v", voice, "-w", outPath, "--stdin")
	cmd.Stdin = strings.NewReader(text)
	if out, err := cmd.CombinedOutput(); err != nil {
		return "Error: text-to-speech gagal (" + strings.TrimSpace(string(out)) + ")."
	}
	info, err := os.Stat(outPath)
	if err != nil {
		return "Error: file audio tidak terbentuk."
	}
	return fmt.Sprintf("Audio tersimpan sebagai %s (%d byte) di folder kerja.", name, info.Size())
}

// toolHTTPPost mengirim POST JSON ke URL publik setelah persetujuan pemilik.
func (a *App) toolHTTPPost(ctx context.Context, s seat, arg string) string {
	rawURL, body, _ := strings.Cut(arg, "\n")
	rawURL = strings.TrimSpace(rawURL)
	body = strings.TrimSpace(body)
	if err := checkPublicURL(ctx, rawURL); err != nil {
		return "Error: " + err.Error()
	}
	if body == "" || !json.Valid([]byte(body)) {
		return "Error: isi POST harus JSON yang valid (baris kedua dan seterusnya)."
	}
	if !a.requestApproval(ctx, s, "http", "", "POST "+rawURL+"\n"+truncateRunes(body, 500)) {
		return fmt.Sprintf("POST TIDAK dikirim: pemilik menolak atau tidak menyetujui dalam %d detik.", int(approvalTimeout.Seconds()))
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, rawURL, strings.NewReader(body))
	if err != nil {
		return "Error: URL tidak valid."
	}
	req.Header.Set("User-Agent", browserUserAgent)
	req.Header.Set("Content-Type", "application/json")
	resp, err := toolHTTPClient.Do(req)
	if err != nil {
		return "Error: POST gagal: " + err.Error()
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return "Error: gagal membaca respons."
	}
	return truncateToolResult(fmt.Sprintf("HTTP %d\n%s", resp.StatusCode, strings.TrimSpace(string(raw))))
}

// toolPasswordGen membuat password acak dengan crypto/rand.
func toolPasswordGen(arg string) string {
	length := 16
	noSymbol := false
	for _, f := range strings.Fields(strings.ToLower(arg)) {
		if f == "nosymbol" {
			noSymbol = true
			continue
		}
		if n, err := strconv.Atoi(f); err == nil {
			length = n
		}
	}
	if length < 8 {
		length = 8
	}
	if length > 128 {
		length = 128
	}
	const lower = "abcdefghijkmnopqrstuvwxyz"
	const upper = "ABCDEFGHJKLMNPQRSTUVWXYZ"
	const digits = "23456789"
	const symbols = "!@#$%^&*()-_=+[]{}<>?"
	charset := lower + upper + digits
	if !noSymbol {
		charset += symbols
	}
	pick := func(set string) byte {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(set))))
		if err != nil {
			return set[0]
		}
		return set[n.Int64()]
	}
	for attempt := 0; attempt < 100; attempt++ {
		buf := make([]byte, length)
		for i := range buf {
			buf[i] = pick(charset)
		}
		s := string(buf)
		hasDigit := strings.ContainsAny(s, digits)
		hasUpper := strings.ContainsAny(s, upper)
		hasLower := strings.ContainsAny(s, lower)
		if hasDigit && hasUpper && hasLower {
			return "Password: " + s
		}
	}
	return "Error: gagal membuat password."
}
