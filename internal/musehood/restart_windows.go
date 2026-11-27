//go:build windows

package musehood

import (
	"os"
	"os/exec"
)

// defaultRestartProcess memulai binary baru lalu keluar (fitur 45).
func defaultRestartProcess(path string) error {
	cmd := exec.Command(path, os.Args[1:]...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return err
	}
	os.Exit(0)
	return nil
}
