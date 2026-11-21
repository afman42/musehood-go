# Rencana Implementasi Musehood Go

**Tujuan** — Menjalankan grup chat Musehood dari satu binary lokal dengan kontrak Node versi 1.

**Arsitektur** — `internal/musehood` menangani state, gateway, media, antrean, dan HTTP. `frontend/src` merender antarmuka serta menangani polling. `main.go` memasang frontend statis ter-embed di listener lokal.

**Spesifikasi** — [Desain Musehood Go](../specs/2026-11-21-musehood-go-design.md)

## 1. Kontrak backend dan parser

- [x] Tulis `app_test.go` dengan publik API `New(Options)`, `ServeHTTP`, dan `Close`. Pakai `httptest` untuk model hilang, format chat, 401, pipeline, gambar, batas avatar, penolakan Host/Origin, dan clear.
- [x] Jalankan `go test ./internal/musehood` untuk melihat kegagalan awal.
- [x] Implementasikan `store.go`, `gateway.go`, `images.go`, `queue.go`, `prompts.go`, `engine.go`, dan `http.go`. Pertahankan schema versi 1, async 202, redaksi nilai string, job pending yang dapat dipulihkan, serta status langkah dan progres.
- [x] Jalankan pengujian backend dan race detector. Uji clear saat panggilan gateway tertahan, pending timeout dan respons terlambat, serta restart ketika langkah terakhir menunggu Muse.

## 2. Frontend dan embed

- [x] Tulis tes Chromium untuk shell, composer, menu pengaturan, gambar, dan batas lebar HP. Buat smoke test binary mandiri.
- [x] Port CSS dan SVG, lalu implementasikan `+page.svelte`, `Settings.svelte`, dan tipe API TypeScript. Pertahankan teks dan struktur tampilan referensi.
- [x] Tambah adapter-static dengan fallback `index.html`, `main.go` dengan `embed`, dan `build.sh` berurutan. Uji aturan CSP terhadap bootstrap SvelteKit di browser.

## 3. Verifikasi dan commit

- [x] Jalankan clean build, `go test ./...`, pemeriksaan tipe Svelte, dan tes Chromium lokal. Pastikan proses smoke berhenti setelah pengujian.
- [x] Tulis hasil dan batas verifikasi di `TESTS.md`. Dokumentasikan build, data, gateway, dan protokol pekerja di `README.md`.
- [ ] Periksa diff dan commit lokal.
