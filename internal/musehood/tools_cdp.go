package musehood

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

// --- Tool browser CDP interaktif (fitur omp 5) ---
//
// Satu Chromium headless per aplikasi dengan --remote-debugging-port.
// Tab dikelola lewat endpoint HTTP /json/*; perintah halaman dikirim
// lewat WebSocket per-target (client RFC6455 minimal di bawah, tanpa
// dependensi baru): Page.navigate + Runtime.evaluate untuk klik,
// ketik, dan snapshot teks DOM. Penjaga SSRF checkPublicURL tetap
// berlaku untuk setiap URL. Tanpa Chromium: degradasi seperti tool
// browser lama.

const cdpTimeout = 45 * time.Second

type cdpBrowser struct {
	cmd       *exec.Cmd
	port      int
	userDir   string
	mu        sync.Mutex
	activeTab string
}

var (
	cdpMu      sync.Mutex
	cdpCurrent *cdpBrowser
)

type cdpTarget struct {
	ID                   string `json:"id"`
	Type                 string `json:"type"`
	Title                string `json:"title"`
	URL                  string `json:"url"`
	WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
}

// startCDPBrowser menjalankan Chromium bila belum berjalan.
func startCDPBrowser() (*cdpBrowser, error) {
	cdpMu.Lock()
	defer cdpMu.Unlock()
	if cdpCurrent != nil && cdpCurrent.cmd != nil && cdpCurrent.cmd.Process != nil {
		if _, err := cdpCurrent.listTargets(); err == nil {
			return cdpCurrent, nil
		}
		cdpCurrent = nil
	}
	chrome := findChromium()
	if chrome == "" {
		return nil, fmt.Errorf("chromium tidak ditemukan")
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()
	userDir, err := os.MkdirTemp("", "musehood-cdp-")
	if err != nil {
		return nil, err
	}
	args := append(chromiumBaseArgs(),
		"--remote-debugging-port="+strconv.Itoa(port),
		"--remote-debugging-address=127.0.0.1",
		"--remote-allow-origins=*",
		"--user-data-dir="+userDir,
		"about:blank",
	)
	cmd := exec.Command(chrome, args...)
	cmd.Env = os.Environ()
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	browser := &cdpBrowser{cmd: cmd, port: port, userDir: userDir}
	for i := 0; i < 50; i++ {
		if _, err := browser.listTargets(); err == nil {
			cdpCurrent = browser
			return browser, nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	_ = cmd.Process.Kill()
	return nil, fmt.Errorf("Chromium CDP tidak merespons")
}

func (b *cdpBrowser) httpDo(method, path string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, "http://127.0.0.1:"+strconv.Itoa(b.port)+path, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(io.LimitReader(resp.Body, 4*1024*1024))
}

func (b *cdpBrowser) httpGet(path string) ([]byte, error) {
	return b.httpDo(http.MethodGet, path)
}

func (b *cdpBrowser) listTargets() ([]cdpTarget, error) {
	raw, err := b.httpGet("/json/list")
	if err != nil {
		return nil, err
	}
	var targets []cdpTarget
	if err := json.Unmarshal(raw, &targets); err != nil {
		return nil, err
	}
	pages := targets[:0]
	for _, t := range targets {
		if t.Type == "page" {
			pages = append(pages, t)
		}
	}
	return pages, nil
}

func (b *cdpBrowser) newTarget(rawURL string) (cdpTarget, error) {
	// Chromium modern mewajibkan PUT untuk /json/new.
	raw, err := b.httpDo(http.MethodPut, "/json/new?"+url.QueryEscape(rawURL))
	if err != nil {
		return cdpTarget{}, err
	}
	var target cdpTarget
	if err := json.Unmarshal(raw, &target); err != nil {
		return cdpTarget{}, err
	}
	return target, nil
}

// tabForURL mengembalikan tab yang sudah memuat URL itu, atau membuka
// tab baru. Tab menjadi tab aktif.
func (b *cdpBrowser) tabForURL(rawURL string) (cdpTarget, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	targets, err := b.listTargets()
	if err != nil {
		return cdpTarget{}, err
	}
	for _, t := range targets {
		if t.URL == rawURL {
			b.activeTab = t.ID
			return t, nil
		}
	}
	target, err := b.newTarget(rawURL)
	if err != nil {
		return cdpTarget{}, err
	}
	b.activeTab = target.ID
	return target, nil
}

func (b *cdpBrowser) activeTarget() (cdpTarget, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	targets, err := b.listTargets()
	if err != nil {
		return cdpTarget{}, err
	}
	for _, t := range targets {
		if t.ID == b.activeTab {
			return t, nil
		}
	}
	if len(targets) > 0 {
		b.activeTab = targets[0].ID
		return targets[0], nil
	}
	return cdpTarget{}, fmt.Errorf("tidak ada tab terbuka")
}

// evaluate menjalankan JavaScript di tab dan mengembalikan hasilnya
// (returnByValue). Bila navigateTo tidak kosong, tab dinavigasi dulu.
// Target bisa hancur/dibuat ulang oleh navigasi (dial berikutnya
// menjawab 500); dalam kasus itu target diresolusi ulang sekali.
func (b *cdpBrowser) evaluate(target cdpTarget, navigateTo, expression string) (any, error) {
	value, err := b.evaluateOnce(target, navigateTo, expression)
	if err == nil {
		return value, nil
	}
	if !strings.Contains(err.Error(), "handshake WebSocket gagal") && !strings.Contains(err.Error(), "EOF") {
		return nil, err
	}
	fresh, resolveErr := b.resolveTarget(target)
	if resolveErr != nil {
		return nil, err
	}
	return b.evaluateOnce(fresh, navigateTo, expression)
}

// resolveTarget mencari target segar dengan id/URL yang sama.
func (b *cdpBrowser) resolveTarget(old cdpTarget) (cdpTarget, error) {
	targets, err := b.listTargets()
	if err != nil {
		return cdpTarget{}, err
	}
	for _, t := range targets {
		if t.ID == old.ID {
			return t, nil
		}
	}
	for _, t := range targets {
		if t.URL == old.URL {
			return t, nil
		}
	}
	if old.URL != "" && old.URL != "about:blank" {
		return b.newTarget(old.URL)
	}
	return cdpTarget{}, fmt.Errorf("target browser tidak ditemukan lagi")
}

func (b *cdpBrowser) evaluateOnce(target cdpTarget, navigateTo, expression string) (any, error) {
	conn, err := wsDial(target.WebSocketDebuggerURL)
	if err != nil {
		return nil, err
	}
	defer conn.close()
	client := &cdpClient{conn: conn}
	if _, err := client.call("Page.enable", nil); err != nil {
		return nil, err
	}
	if navigateTo != "" && target.URL != navigateTo {
		if _, err := client.call("Page.navigate", map[string]any{"url": navigateTo}); err != nil {
			return nil, err
		}
		// Tunggu pemuatan selesai (event Page.loadEventFired) maks 15 dtk.
		client.waitEvent("Page.loadEventFired", 15*time.Second)
	}
	result, err := client.call("Runtime.evaluate", map[string]any{
		"expression": expression, "returnByValue": true, "awaitPromise": true,
	})
	if err != nil {
		return nil, err
	}
	if exception, ok := result["exceptionDetails"].(map[string]any); ok {
		text, _ := exception["text"].(string)
		return nil, fmt.Errorf("JavaScript error: %s", text)
	}
	inner, _ := result["result"].(map[string]any)
	return inner["value"], nil
}

// --- Client CDP di atas WebSocket ---

type cdpClient struct {
	conn   *wsConn
	nextID int
}

func (c *cdpClient) call(method string, params map[string]any) (map[string]any, error) {
	c.nextID++
	id := c.nextID
	payload := map[string]any{"id": id, "method": method}
	if params != nil {
		payload["params"] = params
	}
	raw, _ := json.Marshal(payload)
	if err := c.conn.writeText(string(raw)); err != nil {
		return nil, err
	}
	for {
		msg, err := c.conn.readMessage()
		if err != nil {
			return nil, err
		}
		var parsed map[string]any
		if err := json.Unmarshal([]byte(msg), &parsed); err != nil {
			continue
		}
		if msgID, ok := parsed["id"].(float64); ok && int(msgID) == id {
			if rpcErr, ok := parsed["error"].(map[string]any); ok {
				return nil, fmt.Errorf("CDP %s: %v", method, rpcErr["message"])
			}
			result, _ := parsed["result"].(map[string]any)
			return result, nil
		}
	}
}

func (c *cdpClient) waitEvent(method string, timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		_ = c.conn.conn.SetReadDeadline(deadline)
		msg, err := c.conn.readMessage()
		if err != nil {
			return
		}
		var parsed map[string]any
		if json.Unmarshal([]byte(msg), &parsed) == nil && parsed["method"] == method {
			return
		}
	}
}

// --- WebSocket RFC6455 minimal (client) ---

type wsConn struct {
	conn   net.Conn
	reader *bufio.Reader
}

func wsDial(rawURL string) (*wsConn, error) {
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme != "ws" {
		return nil, fmt.Errorf("URL WebSocket tidak valid")
	}
	conn, err := net.DialTimeout("tcp", u.Host, 10*time.Second)
	if err != nil {
		return nil, err
	}
	keyBytes := make([]byte, 16)
	_, _ = rand.Read(keyBytes)
	key := base64.StdEncoding.EncodeToString(keyBytes)
	path := u.Path
	if u.RawQuery != "" {
		path += "?" + u.RawQuery
	}
	handshake := "GET " + path + " HTTP/1.1\r\n" +
		"Host: " + u.Host + "\r\n" +
		"Upgrade: websocket\r\n" +
		"Connection: Upgrade\r\n" +
		"Sec-WebSocket-Key: " + key + "\r\n" +
		"Sec-WebSocket-Version: 13\r\n\r\n"
	if _, err := conn.Write([]byte(handshake)); err != nil {
		_ = conn.Close()
		return nil, err
	}
	reader := bufio.NewReader(conn)
	statusLine, err := reader.ReadString('\n')
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	if !strings.Contains(statusLine, "101") {
		_ = conn.Close()
		return nil, fmt.Errorf("handshake WebSocket gagal: %s", strings.TrimSpace(statusLine))
	}
	// Baca header sampai baris kosong; verifikasi accept key.
	expected := wsAcceptKey(key)
	accepted := false
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			_ = conn.Close()
			return nil, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		if strings.HasPrefix(strings.ToLower(line), "sec-websocket-accept:") && strings.Contains(line, expected) {
			accepted = true
		}
	}
	if !accepted {
		_ = conn.Close()
		return nil, fmt.Errorf("Sec-WebSocket-Accept tidak cocok")
	}
	return &wsConn{conn: conn, reader: reader}, nil
}

func wsAcceptKey(key string) string {
	h := sha1.New()
	_, _ = h.Write([]byte(key + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"))
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}

func (w *wsConn) writeText(text string) error {
	payload := []byte(text)
	var header []byte
	header = append(header, 0x81) // FIN + text
	maskKey := make([]byte, 4)
	_, _ = rand.Read(maskKey)
	switch {
	case len(payload) < 126:
		header = append(header, 0x80|byte(len(payload)))
	case len(payload) < 65536:
		header = append(header, 0x80|126, byte(len(payload)>>8), byte(len(payload)))
	default:
		header = append(header, 0x80|127)
		var lenBuf [8]byte
		binary.BigEndian.PutUint64(lenBuf[:], uint64(len(payload)))
		header = append(header, lenBuf[:]...)
	}
	header = append(header, maskKey...)
	masked := make([]byte, len(payload))
	for i, b := range payload {
		masked[i] = b ^ maskKey[i%4]
	}
	_, err := w.conn.Write(append(header, masked...))
	return err
}

// readMessage membaca satu pesan utuh (menangani fragmentasi + ping).
func (w *wsConn) readMessage() (string, error) {
	var assembled []byte
	for {
		fin, opcode, payload, err := w.readFrame()
		if err != nil {
			return "", err
		}
		switch opcode {
		case 0x9: // ping -> pong
			_ = w.writeControl(0xA, payload)
			continue
		case 0x8:
			return "", fmt.Errorf("WebSocket ditutup server")
		case 0x1, 0x2, 0x0:
			assembled = append(assembled, payload...)
			if fin {
				return string(assembled), nil
			}
		}
	}
}

func (w *wsConn) writeControl(opcode byte, payload []byte) error {
	maskKey := make([]byte, 4)
	_, _ = rand.Read(maskKey)
	header := []byte{0x80 | opcode, 0x80 | byte(len(payload))}
	header = append(header, maskKey...)
	masked := make([]byte, len(payload))
	for i, b := range payload {
		masked[i] = b ^ maskKey[i%4]
	}
	_, err := w.conn.Write(append(header, masked...))
	return err
}

func (w *wsConn) readFrame() (bool, byte, []byte, error) {
	first := make([]byte, 2)
	if _, err := io.ReadFull(w.reader, first); err != nil {
		return false, 0, nil, err
	}
	fin := first[0]&0x80 != 0
	opcode := first[0] & 0x0F
	masked := first[1]&0x80 != 0
	length := uint64(first[1] & 0x7F)
	if length == 126 {
		buf := make([]byte, 2)
		if _, err := io.ReadFull(w.reader, buf); err != nil {
			return false, 0, nil, err
		}
		length = uint64(binary.BigEndian.Uint16(buf))
	} else if length == 127 {
		buf := make([]byte, 8)
		if _, err := io.ReadFull(w.reader, buf); err != nil {
			return false, 0, nil, err
		}
		length = binary.BigEndian.Uint64(buf)
	}
	if length > 64*1024*1024 {
		return false, 0, nil, fmt.Errorf("frame WebSocket terlalu besar")
	}
	var maskKey []byte
	if masked {
		maskKey = make([]byte, 4)
		if _, err := io.ReadFull(w.reader, maskKey); err != nil {
			return false, 0, nil, err
		}
	}
	payload := make([]byte, length)
	if _, err := io.ReadFull(w.reader, payload); err != nil {
		return false, 0, nil, err
	}
	if masked {
		for i := range payload {
			payload[i] ^= maskKey[i%4]
		}
	}
	return fin, opcode, payload, nil
}

func (w *wsConn) close() {
	_ = w.writeControl(0x8, nil)
	_ = w.conn.Close()
}

// --- Tool ---

const cdpMissingError = "Error: Chromium tidak ditemukan di server, browser interaktif (CDP) tidak bisa dipakai. Atur env MUSEHOOD_CHROMIUM ke binary Chromium untuk mengaktifkannya."

func (a *App) toolBrowserClick(ctx context.Context, arg string) string {
	lines := strings.Split(arg, "\n")
	if len(lines) < 2 {
		return "Error: format browser_click — baris pertama URL, baris kedua selector CSS."
	}
	rawURL := strings.TrimSpace(lines[0])
	selector := strings.TrimSpace(lines[1])
	if selector == "" {
		return "Error: selector kosong."
	}
	if err := checkPublicURL(ctx, rawURL); err != nil {
		return "Error: " + err.Error()
	}
	browser, err := startCDPBrowser()
	if err != nil {
		return cdpMissingError
	}
	target, err := browser.tabForURL(rawURL)
	if err != nil {
		return "Error: " + err.Error()
	}
	selJSON, _ := json.Marshal(selector)
	expr := fmt.Sprintf(`(() => { const el = document.querySelector(%s); if (!el) return "tidak-ditemukan"; el.scrollIntoView(); el.click(); return "diklik:" + (el.textContent || "").trim().slice(0, 80); })()`, selJSON)
	value, err := browser.evaluate(target, rawURL, expr)
	if err != nil {
		return "Error: " + err.Error()
	}
	text, _ := value.(string)
	if text == "tidak-ditemukan" {
		return fmt.Sprintf("Elemen %q tidak ditemukan di halaman.", selector)
	}
	return "Klik berhasil: " + strings.TrimPrefix(text, "diklik:")
}

func (a *App) toolBrowserType(ctx context.Context, arg string) string {
	lines := strings.Split(arg, "\n")
	if len(lines) < 3 {
		return "Error: format browser_type — baris pertama URL, baris kedua selector CSS, baris ketiga teks."
	}
	rawURL := strings.TrimSpace(lines[0])
	selector := strings.TrimSpace(lines[1])
	text := lines[2]
	if selector == "" {
		return "Error: selector kosong."
	}
	if err := checkPublicURL(ctx, rawURL); err != nil {
		return "Error: " + err.Error()
	}
	browser, err := startCDPBrowser()
	if err != nil {
		return cdpMissingError
	}
	target, err := browser.tabForURL(rawURL)
	if err != nil {
		return "Error: " + err.Error()
	}
	selJSON, _ := json.Marshal(selector)
	textJSON, _ := json.Marshal(text)
	expr := fmt.Sprintf(`(() => { const el = document.querySelector(%s); if (!el) return "tidak-ditemukan"; el.focus(); const proto = el.tagName === "TEXTAREA" ? HTMLTextAreaElement.prototype : HTMLInputElement.prototype; const setter = Object.getOwnPropertyDescriptor(proto, "value"); if (setter && setter.set) { setter.set.call(el, %s); } else { el.value = %s; } el.dispatchEvent(new Event("input", { bubbles: true })); el.dispatchEvent(new Event("change", { bubbles: true })); return "terisi:" + el.value.slice(0, 80); })()`, selJSON, textJSON, textJSON)
	value, err := browser.evaluate(target, rawURL, expr)
	if err != nil {
		return "Error: " + err.Error()
	}
	result, _ := value.(string)
	if result == "tidak-ditemukan" {
		return fmt.Sprintf("Elemen %q tidak ditemukan di halaman.", selector)
	}
	return "Teks diketik ke elemen: " + strings.TrimPrefix(result, "terisi:")
}

func (a *App) toolBrowserSnapshot(ctx context.Context, arg string) string {
	rawURL := strings.TrimSpace(arg)
	browser, err := startCDPBrowser()
	if err != nil {
		return cdpMissingError
	}
	var target cdpTarget
	if rawURL != "" {
		if err := checkPublicURL(ctx, rawURL); err != nil {
			return "Error: " + err.Error()
		}
		target, err = browser.tabForURL(rawURL)
	} else {
		target, err = browser.activeTarget()
	}
	if err != nil {
		return "Error: " + err.Error()
	}
	expr := `(() => {
		const text = (document.body ? document.body.innerText : "").slice(0, 4000);
		const els = [];
		document.querySelectorAll("a, button, input, select, textarea").forEach((el, i) => {
			if (i >= 40) return;
			let sel = el.tagName.toLowerCase();
			if (el.id) sel += "#" + el.id;
			else if (el.className && typeof el.className === "string") sel += "." + el.className.trim().split(/\s+/)[0];
			const label = (el.textContent || el.value || el.placeholder || "").trim().slice(0, 60);
			els.push(sel + (label ? " [" + label + "]" : ""));
		});
		return JSON.stringify({ title: document.title, url: location.href, text, elements: els });
	})()`
	value, err := browser.evaluate(target, rawURL, expr)
	if err != nil {
		return "Error: " + err.Error()
	}
	raw, _ := value.(string)
	var snap struct {
		Title    string   `json:"title"`
		URL      string   `json:"url"`
		Text     string   `json:"text"`
		Elements []string `json:"elements"`
	}
	if json.Unmarshal([]byte(raw), &snap) != nil {
		return truncateToolResult(raw)
	}
	var out strings.Builder
	out.WriteString("Judul: " + snap.Title + "\nURL: " + snap.URL + "\n\n")
	out.WriteString(snap.Text)
	if len(snap.Elements) > 0 {
		out.WriteString("\n\nElemen interaktif:\n")
		for _, el := range snap.Elements {
			out.WriteString("- " + el + "\n")
		}
	}
	return truncateToolResult(strings.TrimSpace(out.String()))
}

func (a *App) toolBrowserTabs(ctx context.Context, arg string) string {
	browser, err := startCDPBrowser()
	if err != nil {
		return cdpMissingError
	}
	command, rest, _ := strings.Cut(strings.TrimSpace(arg), " ")
	rest = strings.TrimSpace(rest)
	switch command {
	case "", "list":
		targets, err := browser.listTargets()
		if err != nil {
			return "Error: " + err.Error()
		}
		if len(targets) == 0 {
			return "Tidak ada tab terbuka."
		}
		var out strings.Builder
		for _, t := range targets {
			marker := ""
			if t.ID == browser.activeTab {
				marker = " (aktif)"
			}
			out.WriteString(fmt.Sprintf("- %s%s: %s <%s>\n", t.ID, marker, t.Title, t.URL))
		}
		return strings.TrimSpace(out.String())
	case "open":
		if err := checkPublicURL(ctx, rest); err != nil {
			return "Error: " + err.Error()
		}
		target, err := browser.newTarget(rest)
		if err != nil {
			return "Error: " + err.Error()
		}
		browser.mu.Lock()
		browser.activeTab = target.ID
		browser.mu.Unlock()
		return "Tab baru dibuka: " + target.ID + " <" + target.URL + ">"
	case "close":
		if rest == "" {
			return "Error: format browser_tabs close <id>."
		}
		if _, err := browser.httpGet("/json/close/" + url.PathEscape(rest)); err != nil {
			return "Error: " + err.Error()
		}
		return "Tab ditutup: " + rest
	case "activate":
		if rest == "" {
			return "Error: format browser_tabs activate <id>."
		}
		if _, err := browser.httpGet("/json/activate/" + url.PathEscape(rest)); err != nil {
			return "Error: " + err.Error()
		}
		browser.mu.Lock()
		browser.activeTab = rest
		browser.mu.Unlock()
		return "Tab diaktifkan: " + rest
	default:
		return "Error: format browser_tabs — list, open <url>, close <id>, atau activate <id>."
	}
}

// cdpBrowserAlive melaporkan apakah browser CDP sedang berjalan (hub).
func cdpBrowserAlive() bool {
	cdpMu.Lock()
	defer cdpMu.Unlock()
	if cdpCurrent == nil {
		return false
	}
	_, err := cdpCurrent.listTargets()
	return err == nil
}

// stopCDPBrowser menghentikan browser CDP (hub_stop "browser").
func stopCDPBrowser() bool {
	cdpMu.Lock()
	defer cdpMu.Unlock()
	if cdpCurrent == nil {
		return false
	}
	cmd := cdpCurrent.cmd
	cdpCurrent = nil
	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
		go func() { _ = cmd.Wait() }()
	}
	return true
}
