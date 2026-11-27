//go:build !(linux || darwin || freebsd || netbsd || openbsd || dragonfly || android || ios)

package musehood

import (
	"os"
	"os/exec"
)

// killProcessGroup di platform tanpa grup proses POSIX (Windows,
// plan9, solaris, illumos, aix, js, wasip1): proses server
// dihentikan langsung; SIGTERM tidak tersedia.
func killProcessGroup(pid int, force bool) error {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return proc.Kill()
}

// setProcessGroup di platform ini adalah no-op.
func setProcessGroup(cmd *exec.Cmd) {}
