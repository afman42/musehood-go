//go:build !((linux && (386 || amd64 || arm || arm64 || loong64 || ppc64le || riscv64 || s390x)) || (darwin && (amd64 || arm64)) || (windows && (386 || amd64 || arm64)) || (freebsd && (386 || amd64 || arm || arm64)) || (netbsd && amd64) || (openbsd && (amd64 || arm64)) || (android && arm64) || (illumos && amd64))

package musehood

import "errors"

// openSQLiteStore di platform tanpa dukungan driver SQLite:
// backend JSON tetap tersedia; meminta SQLite secara eksplisit
// menghasilkan error yang jelas.
func openSQLiteStore(dir string) (dataStore, error) {
	return nil, errors.New("backend SQLite tidak tersedia di platform ini; gunakan MUSEHOOD_STORAGE=json")
}
