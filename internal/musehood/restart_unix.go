//go:build !windows

package musehood

import (
	"os"
	"syscall"
)

// defaultRestartProcess mengganti proses saat ini dengan binary baru
// (fitur 45).
func defaultRestartProcess(path string) error {
	return syscall.Exec(path, os.Args, os.Environ())
}
