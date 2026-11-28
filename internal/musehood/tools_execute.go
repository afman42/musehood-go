package musehood

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// --- Tool execute (Code Mode, gaya OpenCode "execute") ---
//
// Kursi menulis JavaScript yang berjalan di sandbox node yang sama
// dengan run_code (tanpa jaringan, tanpa akses filesystem di luar
// folder kerja). Kode JS memanggil `await tool("nama", "argumen")`;
// panggilan diteruskan ke host lewat jembatan RPC JSON-lines di
// stdin/stdout dengan awalan acak per eksekusi, dan host menjalankannya
// lewat jalur tool biasa (cek allowlist, persetujuan, log audit yang
// sama). Batas: 30 detik, maks 20 panggilan tool, execute tidak dapat
// memanggil execute. Nilai return terakhir / console.log menjadi hasil.

const (
	executeTimeout   = 30 * time.Second
	maxExecuteCalls  = 20
	maxRPCLineBytes  = 1024 * 1024
	maxExecuteOutput = 64 * 1024
)

// executePrelude adalah prelude JS yang menyediakan fungsi tool() dan
// membungkus kode pengguna dalam fungsi async. %s adalah awalan RPC.
const executePrelude = `const __PREFIX = %q;
const __pending = new Map();
let __seq = 0;
let __buf = "";
process.stdin.setEncoding("utf8");
process.stdin.on("data", (chunk) => {
  __buf += chunk;
  let idx;
  while ((idx = __buf.indexOf("\n")) >= 0) {
    const line = __buf.slice(0, idx);
    __buf = __buf.slice(idx + 1);
    if (!line.startsWith(__PREFIX)) continue;
    try {
      const msg = JSON.parse(line.slice(__PREFIX.length));
      const resolve = __pending.get(msg.id);
      if (resolve) { __pending.delete(msg.id); resolve(String(msg.result ?? "")); }
    } catch (e) {}
  }
});
function tool(name, arg) {
  return new Promise((resolve) => {
    const id = ++__seq;
    __pending.set(id, resolve);
    process.stdout.write(__PREFIX + JSON.stringify({ id, tool: String(name), arg: String(arg ?? "") }) + "\n");
  });
}
(async () => {
%s
})().then((v) => {
  if (v !== undefined) console.log(typeof v === "string" ? v : JSON.stringify(v));
  process.stdout.write("", () => process.exit(0));
}).catch((e) => {
  console.error("Error: " + (e && e.message ? e.message : String(e)));
  process.stdout.write("", () => process.exit(1));
});
`

type rpcRequest struct {
	ID   int    `json:"id"`
	Tool string `json:"tool"`
	Arg  string `json:"arg"`
}

type rpcResponse struct {
	ID     int    `json:"id"`
	Result string `json:"result"`
}

func (a *App) toolExecute(ctx context.Context, cfg settings, s seat, arg string, code bool) string {
	jsCode := strings.TrimSpace(arg)
	if jsCode == "" {
		return "Error: argumen execute adalah kode JavaScript."
	}
	if len(jsCode) > maxSandboxFileBytes {
		return "Error: kode terlalu besar (maks 100 KB)."
	}
	if match, dangerous := dangerousCode(jsCode); dangerous {
		if !a.requestApproval(ctx, s, "code", "javascript", jsCode) {
			return fmt.Sprintf("Kode TIDAK dijalankan: terdeteksi pola berbahaya (%q) dan pemilik menolak atau tidak menyetujui dalam %d detik.", match, int(approvalTimeout.Seconds()))
		}
	}
	calls := 0
	handler := func(name, toolArg string) string {
		calls++
		if calls > maxExecuteCalls {
			return fmt.Sprintf("Error: batas %d panggilan tool per execute tercapai.", maxExecuteCalls)
		}
		if name == "execute" {
			return "Error: execute tidak dapat memanggil execute."
		}
		result, _ := a.runTool(ctx, cfg, s, name, toolArg, code)
		return truncateToolResult(result)
	}
	return a.executeCodeRPC(ctx, jsCode, handler)
}

// executeCodeRPC menjalankan JavaScript di sandbox dengan jembatan RPC
// dua arah lewat stdin/stdout. Baris stdout berawalan prefix RPC adalah
// permintaan tool; baris lain dikumpulkan sebagai output.
func (a *App) executeCodeRPC(ctx context.Context, jsCode string, handler func(name, arg string) string) string {
	dir := a.sandboxDir()
	if err := os.MkdirAll(dir, 0o777); err != nil {
		return "Error: gagal membuat folder kerja."
	}
	token := uuid()
	if len(token) > 8 {
		token = token[:8]
	}
	prefix := "\x01MHRPC" + token + " "
	script := fmt.Sprintf(executePrelude, prefix, jsCode)
	name := "exec-" + uuid() + ".js"
	full := filepath.Join(dir, name)
	if err := os.WriteFile(full, []byte(script), 0o644); err != nil {
		return "Error: gagal menulis berkas kode."
	}
	defer os.Remove(full)

	out, errText, runErr := a.runRPCOnce(ctx, dir, name, prefix, handler, true)
	if runErr != nil && strings.Contains(errText, "Failed to") && strings.TrimSpace(out) == "" {
		// systemd-run tidak tersedia: jalankan bwrap langsung.
		out, errText, runErr = a.runRPCOnce(ctx, dir, name, prefix, handler, false)
	}
	text := strings.TrimSpace(out)
	if strings.TrimSpace(errText) != "" {
		if text != "" {
			text += "\n"
		}
		text += strings.TrimSpace(errText)
	}
	if runErr != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return truncateToolResult("Error: execute melebihi batas waktu 30 detik.\n" + text)
		}
		if text == "" {
			text = runErr.Error()
		}
		return truncateToolResult("Kode selesai dengan error:\n" + text)
	}
	if text == "" {
		text = "(tidak ada output)"
	}
	return truncateToolResult(text)
}

// runRPCOnce menjalankan satu proses sandbox dengan pipa stdin/stdout
// dan melayani permintaan RPC sampai proses selesai.
func (a *App) runRPCOnce(ctx context.Context, dir, name, prefix string, handler func(name, arg string) string, useSystemd bool) (string, string, error) {
	bwrapArgs := sandboxBwrapArgs(dir, "node", name, executeTimeout)
	runCtx, cancel := context.WithTimeout(ctx, executeTimeout+15*time.Second)
	defer cancel()

	var cmd *exec.Cmd
	if useSystemd {
		args := append([]string{"--quiet", "--scope", "-p", "MemoryMax=256M", "-p", "TasksMax=64", "-p", "CPUQuota=50%", "bwrap"}, bwrapArgs...)
		cmd = exec.CommandContext(runCtx, "systemd-run", args...)
	} else {
		cmd = exec.CommandContext(runCtx, "bwrap", bwrapArgs...)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return "", "", err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", "", err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return "", "", err
	}
	if err := cmd.Start(); err != nil {
		return "", "", err
	}

	var errBuf strings.Builder
	var errMu sync.Mutex
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		scanner := bufio.NewScanner(stderr)
		scanner.Buffer(make([]byte, 0, 64*1024), maxRPCLineBytes)
		for scanner.Scan() {
			errMu.Lock()
			if errBuf.Len() < maxExecuteOutput {
				errBuf.WriteString(scanner.Text())
				errBuf.WriteString("\n")
			}
			errMu.Unlock()
		}
	}()

	var outBuf strings.Builder
	reader := bufio.NewReaderSize(stdout, 64*1024)
	for {
		line, readErr := readRPCLine(reader)
		if line != "" {
			if strings.HasPrefix(line, prefix) {
				var req rpcRequest
				if json.Unmarshal([]byte(strings.TrimPrefix(line, prefix)), &req) == nil && req.Tool != "" {
					result := handler(req.Tool, req.Arg)
					resp, _ := json.Marshal(rpcResponse{ID: req.ID, Result: result})
					_, _ = io.WriteString(stdin, prefix+string(resp)+"\n")
				}
			} else if outBuf.Len() < maxExecuteOutput {
				outBuf.WriteString(line)
			}
		}
		if readErr != nil {
			break
		}
	}
	_ = stdin.Close()
	waitErr := cmd.Wait()
	wg.Wait()
	errMu.Lock()
	errText := errBuf.String()
	errMu.Unlock()
	return outBuf.String(), errText, waitErr
}

// readRPCLine membaca satu baris (termasuk "\n") dengan batas ukuran;
// baris yang lebih panjang dipotong.
func readRPCLine(reader *bufio.Reader) (string, error) {
	var sb strings.Builder
	for {
		chunk, err := reader.ReadString('\n')
		if sb.Len() < maxRPCLineBytes {
			sb.WriteString(chunk)
		}
		if err != nil {
			return sb.String(), err
		}
		if strings.HasSuffix(chunk, "\n") {
			return sb.String(), nil
		}
	}
}
