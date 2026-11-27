//go:build linux || darwin || freebsd || netbsd || openbsd || dragonfly || android || ios

package musehood

import (
	"os/exec"
	"syscall"
)

// killProcessGroup mengirim SIGTERM (force=false) atau SIGKILL
// (force=true) ke seluruh grup proses server proyek.
func killProcessGroup(pid int, force bool) error {
	sig := syscall.SIGTERM
	if force {
		sig = syscall.SIGKILL
	}
	return syscall.Kill(-pid, sig)
}

// setProcessGroup menaruh proses server di grup prosesnya sendiri
// agar bisa dihentikan sebagai satu grup.
func setProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}
