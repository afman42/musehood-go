package musehood

// Abstraksi penyimpanan data (fitur 34). Backend JSON (bawaan produksi)
// menyimpan seluruh data sebagai satu blob di data.json; backend SQLite
// menyimpan blob yang sama di tabel kv (kunci "data") pada data.db.
// Pemilihan backend: env MUSEHOOD_STORAGE=sqlite|json; bila tidak diset,
// SQLite dipakai hanya bila data.db sudah ada, selain itu JSON —
// produksi yang berjalan tetap JSON kecuali env diset. Saat backend
// SQLite dibuka dan tabelnya masih kosong, data dimigrasi dari data.json
// (berkas JSON dipertahankan sebagai cadangan).

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

type dataStore interface {
	load() (data, error)
	save(d data) error
}

type jsonStore struct{ dir string }

func (s jsonStore) load() (data, error) { return loadData(s.dir) }

func (s jsonStore) save(d data) error {
	return writeJSONFile(filepath.Join(s.dir, "data.json"), d)
}

type sqliteStore struct {
	dir string
	db  *sql.DB
}

func openSQLiteStore(dir string) (*sqliteStore, error) {
	db, err := sql.Open("sqlite", filepath.Join(dir, "data.db"))
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

// openStore memilih backend penyimpanan untuk direktori data.
func openStore(dir string) (dataStore, error) {
	choice := strings.ToLower(strings.TrimSpace(os.Getenv("MUSEHOOD_STORAGE")))
	useSQLite := false
	switch choice {
	case "sqlite":
		useSQLite = true
	case "json":
		useSQLite = false
	default:
		if _, err := os.Stat(filepath.Join(dir, "data.db")); err == nil {
			useSQLite = true
		}
	}
	if useSQLite {
		return openSQLiteStore(dir)
	}
	return jsonStore{dir: dir}, nil
}

// persistLocked menyimpan data lewat backend aktif. App tanpa store
// (dibuat langsung di tes) jatuh ke berkas JSON seperti perilaku lama.
// Pemanggil wajib memegang a.mu.
func (a *App) persistLocked(d data) error {
	if a.store != nil {
		return a.store.save(d)
	}
	return writeJSONFile(filepath.Join(a.dir, "data.json"), d)
}
