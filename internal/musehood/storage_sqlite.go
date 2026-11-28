//go:build (linux && (386 || amd64 || arm || arm64 || loong64 || ppc64le || riscv64 || s390x)) || (darwin && (amd64 || arm64)) || (windows && (386 || amd64 || arm64)) || (freebsd && (386 || amd64 || arm || arm64)) || (netbsd && amd64) || (openbsd && (amd64 || arm64)) || (android && arm64) || (illumos && amd64)

package musehood

// Backend SQLite (fitur 34) — hanya untuk platform yang didukung
// driver pure-Go modernc.org/sqlite (lewat modernc.org/libc).
// Platform lain memakai stub di storage_sqlite_stub.go dan tetap
// bisa memakai backend JSON.

import (
	"database/sql"
	"encoding/json"
	"path/filepath"

	_ "modernc.org/sqlite"
)

type sqliteStore struct {
	dir string
	db  *sql.DB
}

func openSQLiteStore(dir string) (*sqliteStore, error) {
	// busy_timeout: proses lain (mis. instans lama saat restart tumpang
	// tindih) tidak langsung gagal "database is locked". WAL: pembaca
	// tidak memblokir penulis. MaxOpenConns(1) tetap menyerialkan
	// akses dalam proses ini.
	dsn := "file:" + filepath.Join(dir, "data.db") + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS kv (key TEXT PRIMARY KEY, value TEXT NOT NULL)`); err != nil {
		db.Close()
		return nil, err
	}
	return &sqliteStore{dir: dir, db: db}, nil
}

func (s *sqliteStore) load() (data, error) {
	var raw string
	err := s.db.QueryRow(`SELECT value FROM kv WHERE key = 'data'`).Scan(&raw)
	if err == sql.ErrNoRows {
		// Migrasi dari data.json bila ada; berkas JSON tetap sebagai cadangan.
		d, err := loadData(s.dir)
		if err != nil {
			return d, err
		}
		if err := s.save(d); err != nil {
			return d, err
		}
		return d, nil
	}
	if err != nil {
		return data{}, err
	}
	return parseData([]byte(raw))
}

func (s *sqliteStore) save(d data) error {
	raw, err := json.Marshal(d)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`INSERT INTO kv (key, value) VALUES ('data', ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`, string(raw))
	return err
}
