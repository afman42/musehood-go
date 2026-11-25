# Pengujian Musehood Go

Dijalankan 22 November 2026 dengan Go 1.27.4 (`linux/amd64`), Node.js 24.22.1, dan Chromium lokal melalui Playwright 1.65.1. Pengujian memakai gateway mock di loopback dan direktori data sementara.

| Perintah | Hasil |
| --- | --- |
| `./build.sh` setelah menghapus `frontend/build/`, `frontend/.svelte-kit/`, dan binary | Lulus. Adapter-static membuat `frontend/build/index.html`. Go menghasilkan binary dengan `GOPROXY=off` dan `GOFLAGS=-mod=mod`. |
| `(cd frontend && npm run check)` | 0 error dan 0 peringatan. |
| `go test -count=1 -cover ./...` | Lulus. Cakupan pernyataan `internal/musehood` 83,0%. |
| `go test -race -count=1 ./...` | Lulus. Race detector tidak melaporkan masalah. |
| `go vet ./...` | Lulus. |
| `MUSEHOOD_BROWSER=1 node --test tests/binary.test.mjs` | 2 tes lulus dengan `frontend/build/` disisihkan. Keduanya menjalankan salinan binary dari direktori kosong. |

## Backend dan API

Tes Go memakai `httptest` untuk menguji:

- Semua endpoint API, termasuk validasi Host, Origin, HTTP method, dan payload kosong.
- Tes Koneksi menampilkan daftar model asli, menandai model yang hilang, dan mempertahankan status serta body HTTP 401. Field konfigurasi direspons dengan key termask.
- Parser menerima SSE meski request memakai `stream:false`, termasuk format SSE multiline, event beruntun, delta langsung, array konten, dan plain text.
- Mention membatasi jumlah panggilan. Diskusi bergilir mengikuti urutan kursi. Saat toggle dimatikan, pesan biasa menuju Muse.
- Keenam tahap `Tugas:` diproses berurutan. Respons ringkasan berasal dari file pekerja dan berlabel **Hasil Akhir - Integrasi**. Progres menyertakan status tugas. Empat level review menghasilkan instruksi Peninjau yang berbeda.
- Gambar PNG yang dihasilkan bisa didekode. Jika gateway gambar gagal, antrean fallback meminta gambar asli dan tetap menampilkan error gateway.
- Muse menunggu jawaban pekerja. Pengujian memperpendek timeout melalui opsi internal, lalu memverifikasi jawaban terlambat. Pekerjaan yang tertunda dilanjutkan setelah restart dengan ID antrean yang sama.
- Unggahan avatar tersedia untuk grup, pemilik, dan seluruh kursi. File dengan tipe salah atau ukuran lebih dari 2 MB ditolak.
- Clear menghapus pesan dan pekerjaan sambil mempertahankan konfigurasi, termasuk saat panggilan gateway tertahan. Key fixture disamarkan ketika provider menggemakannya, termasuk bentuk JSON yang di-escape.

## Protokol gateway (Workstream A)

`gatewayProtocol` kini bagian dari `data.json` (`openai`, `openai-responses`, `anthropic`, `gemini`, `ollama`; nilai di luar daftar ditolak dan perubahan protokol mereset koneksi). Pengujian `protocols_test.go` memakai mock per protokol di `tests/mock-gateway.mjs`:

- **Anthropic**: header `x-api-key` + `anthropic-version`, system prompt pada field terpisah, `max_tokens: 1024`; HTTP 401 mock diteruskan apa adanya (`HTTP 401: auth anthropic asli`).
- **Gemini**: endpoint `:generateContent` pada `/v1beta` (base URL dengan akhiran `/v1beta` atau `/v1` dinormalisasi tanpa duplikasi), key lewat query `?key=`, SSE diekstrak dari `candidates[0].content.parts`.
- **Ollama**: `/api/chat` tanpa API key, format NDJSON.
- **OpenAI Responses**: `POST /v1/responses` dengan Bearer, snapshot `response.output_text` yang berulang dideduplikasi saat streaming.
- Pembuatan gambar hanya pada protokol `openai`; pada protokol lain pesan `Gambar:` otomatis dialihkan ke jalur gambar pekerja Muse.

## CRUD Provider Gateway

Beberapa provider gateway tersimpan di `data.json` (`providers[]`); satu provider aktif disinkronkan ke settings tunggal yang dipakai engine, jadi jalur chat lama tidak berubah. Pengujian `providers_test.go` mencakup:

- **CRUD** (`GET/POST /api/providers`, `POST/DELETE /api/providers/{id}`, `POST /api/providers/{id}/activate`): buat/ubah/hapus/aktifkan, validasi nama/URL/protokol, key wajib kecuali Ollama, key kosong saat update mempertahankan key lama, hapus provider aktif mengaktifkan provider pertama yang tersisa, hapus semua mengosongkan gateway. API key tidak pernah muncul di respons (hanya `hasApiKey`); rotasi key tidak membocorkan key lama lewat state.
- **Migrasi**: `data.json` lama (hanya settings gateway tunggal) otomatis menjadi satu provider `prov-utama` saat dimuat; migrasi idempotent. Pengaturan gateway lewat `/api/settings` juga menulis-balik ke provider aktif, atau membuat provider bila belum ada.
- **Fetch Models** (`GET /api/providers/{id}/models`): daftar model dari provider mana pun tanpa harus mengaktifkannya; untuk provider aktif hasilnya juga disimpan ke `Connection` seperti Tes Koneksi.
- **Tes Models** (`POST /api/providers/{id}/test-models`, maksimal 10 model): satu prompt minimal per model secara berurutan, hasil per model (latensi + cuplikan balasan atau error); model gagal tidak menghentikan yang lain; key yang digemakan provider pada error disamarkan `[RAHASIA DISEMBUNYIKAN]`.
- **UI**: panel Pengaturan → Provider Gateway (daftar + tambah/edit/hapus/aktifkan, Fetch Models, Tes Models). `npm run check` 0 error, `npm run build` lulus.

## Fitur chat (Workstream B)

Pengujian `chatops_test.go` mencakup:

- **Edit** (`POST /api/message/:id/edit`): hanya pesan pemilik (403 untuk pesan kursi), kosong ditolak 400, `editedAt` tersimpan dan tampil sebagai "(diedit)".
- **Hapus** (`POST /api/message/:id/delete`): dengan konfirmasi di UI; pesan yang sedang dikerjakan ditolak 409; menghapus semua pesan tugas membersihkan `activeTask`.
- **Balasan** (`replyTo`): tersimpan, ditampilkan sebagai kutipan yang bisa diklik untuk melompat ke pesan sumber; ID tak dikenal ditolak 400.
- **Cari**: sisi klien dengan sorotan, navigasi sebelumnya/berikutnya, dan jumlah hasil.
- **Ekspor** (`GET /api/export?format=json|md`): JSON berisi pesan + pengaturan tanpa API key; Markdown memuat label **Hasil Akhir - Integrasi** dan nama pemilik; format lain ditolak 400.
- **Reaksi** (`POST /api/message/:id/react`): enam emoji diizinkan, toggle menaikkan/menurunkan hitungan dengan penanda pemilik; emoji di luar daftar ditolak 400.
- **Mode gelap**: `darkMode` tersimpan di `data.json`, default terang, kelas `dark` pada `documentElement`.
- **Unggah gambar** (`POST /api/upload-image`): PNG/JPEG/GIF/WebP maksimal 2 MB (413 bila lebih), SVG ditolak, URL `/images/*` dilayani dan dirender inline dalam gelembung chat; `imageUrl` eksternal ditolak.

## Binary dan browser

Tes menjalankan binary dari folder sementara yang terpisah dari repo. Tes pertama memastikan port default 3001 melayani halaman utama, fallback SPA, dan SVG ter-embed. Tes Chromium memakai override port lokal dan memuat aplikasi dari rute SPA. Tes memeriksa strip kursi horizontal, composer, dan lebar halaman pada viewport 320, 390, serta 1440 piksel.

Chromium juga menjalankan alur pengaturan, daftar model, mask key, unggah avatar, mention, SSE, error 401 asli, redaksi key, gambar inline, pipeline, respons file Muse, progres via API pekerja, serta konfirmasi clear. Tes memverifikasi halaman bebas error JavaScript dan CSP. Log proses diperiksa terhadap key fixture, lalu binary dihentikan dengan SIGTERM.

## Kompatibilitas data Node

Verifikasi manual dengan modul `Store`, `Engine`, dan `MuseQueue` dari repo Node referensi. Node membuat state dan request `@Muse` yang tertunda dalam direktori sementara. Binary Go membuka data itu pada port lokal 3011 dan menerima jawaban dari file respons. Setelah Go berhenti, `Store` Node membaca kembali nama pemilik, pesan, status job `completed`, dan ID antrean yang sama. Tes Go juga memeriksa pemulihan state yang tersimpan di antara tahap terakhir dan penandaan job selesai.

Model gateway pemilik dan pekerja Muse nyata berada di luar cakupan pengujian. Gateway lokal dan penulisan file respons menguji kontrak integrasi yang dipakai masing-masing.

## Build release lintas platform (v1.1.0)

Dijalankan 22 November 2026 dengan Go 1.27.4 via `./build-release.sh` (frontend dibangun sekali, lalu cross-compile `CGO_ENABLED=0`, `GOPROXY=off`, `GOFLAGS=-mod=mod` untuk semua 47 pasangan `go tool dist list`). Hasil: **40 berhasil, 5 gagal, 2 dilewati**.

Keenam platform wajib semuanya berhasil:

| Platform | Ukuran binary |
| --- | --- |
| linux/amd64 | 11.649.619 byte |
| linux/arm64 | 10.803.775 byte |
| darwin/amd64 | 11.904.384 byte |
| darwin/arm64 | 11.139.282 byte |
| windows/amd64 | 11.821.568 byte (`musehood-go.exe`) |
| windows/arm64 | 10.813.952 byte (`musehood-go.exe`) |

34 platform lain juga berhasil: aix/ppc64, android/arm64, dragonfly/amd64, freebsd/386, freebsd/amd64, freebsd/arm, freebsd/arm64, illumos/amd64, linux/386, linux/arm, linux/loong64, linux/mips, linux/mips64, linux/mips64le, linux/mipsle, linux/ppc64, linux/ppc64le, linux/riscv64, linux/s390x, netbsd/386, netbsd/amd64, netbsd/arm, netbsd/arm64, openbsd/386, openbsd/amd64, openbsd/arm, openbsd/arm64, openbsd/ppc64, openbsd/riscv64, plan9/386, plan9/amd64, plan9/arm, solaris/amd64, windows/386.

Gagal (5) — semuanya butuh external linking cgo, yang tidak tersedia saat cross-compile dengan `CGO_ENABLED=0`: android/386, android/amd64, android/arm, ios/amd64, ios/arm64. Dilewati (2) — bukan executable mandiri: js/wasm dan wasip1/wasm (butuh runtime JS/WASI). Log lengkap: `dist/build-release.log` (tidak masuk git).

## Mock gateway mandiri

Jalankan `node tests/mock-gateway.mjs` untuk melayani loopback pada port 3101 atau atur `MOCK_PORT`. Masukkan `http://127.0.0.1:3101` dan key fixture `browser-fixture-only-9876` pada pengaturan Musehood. Model fixture tersedia dengan nama `sse`, `array`, `plain`, `delta`, `image`, `denied`, dan `key-echo`. Mock juga melayani jalur per protokol: `/v1/messages` (Anthropic, key `anthropic-mock-key`), `/v1beta/models/…:generateContent` (Gemini, key `gemini-mock-key`), `/v1/responses` (OpenAI Responses), dan `/api/chat` (Ollama, tanpa key).

## Kustomisasi kursi bawaan & kursi integrator (25 Nov 2026)

Peran, provider (jalur jawaban: `muse` antrean file / `gateway` model), dan model kini dapat diganti untuk **semua** kursi lewat `POST /api/settings` — termasuk kursi MUSE (Admin dan Integrasi), yang sebelumnya perannya dipaksa dari kode dan tidak punya model. Kursi bawaan tetap tidak dapat dihapus dan kodenya tetap. Nilai kustom bertahan setelah reload (`loadData` hanya mengisi bawaan bila kosong/tidak valid). Prompt bawaan kursi bawaan mengikuti teks peran barunya bila peran diganti.

Pengaturan baru `settings.integratorSeat`: kursi yang menutup pipeline "Tugas:" (Hasil Akhir - Integrasi) dan menjawab "Tanya Progres". Bawaan `MUSE`. Kode tidak dikenal ditolak (400); menghapus kursi custom yang sedang menjadi integrator mengembalikannya ke MUSE. Routing antrean kini berdasarkan `provider`, bukan kode kursi.

Bug terkait yang diperbaiki: write-through field gateway ke provider aktif kini hanya menyinkronkan field yang dibawa patch — patch pengaturan non-gateway (mis. `integratorSeat`) tidak lagi mengosongkan nama/BaseURL provider aktif yang membuat `data.json` tidak valid saat dimuat ulang.

Tes: `internal/musehood/seat_custom_test.go` (`TestSeatRoleProviderModelPatch`, `TestSettingsPatchKeepsProviderName`, `TestIntegratorSeat`). Verifikasi: `go vet` + `go test` + `go test -race` lulus, coverage 85,1%, `npm run check` 0 error, `npm run build` lulus; dites live lewat API (buat kursi custom → ganti peran/provider → jadikan integrator → kembalikan → hapus).

## Alias provider gateway (25 Nov 2026)

Tiap provider gateway kini punya field `alias` opsional (maks 40 karakter, tanpa spasi/`@`/`/`) sebagai label pembeda. Alias tampil di daftar provider dan menjadi awalan nama model di daftar model (Fetch Models, hasil Tes Models, dan tag model koneksi provider aktif), mis. `xc/gpt-6.1-sol` — agar model bernama sama dari provider lain mudah dibedakan. Alias hanya tampilan: nama model yang dikirim ke gateway tetap apa adanya, dan pencocokan model kursi dengan daftar gateway tetap memakai nama mentah. Alias dapat diisi saat tambah provider, diubah, atau dikosongkan lewat edit; tampil di view provider (`alias`, dihilangkan bila kosong) dan bertahan setelah reload.

Tes: `TestProviderAlias` di `internal/musehood/providers_test.go`.
