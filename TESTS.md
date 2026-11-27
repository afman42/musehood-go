# Pengujian Musehood Go

Terakhir dijalankan 27 November 2026 dengan Go 1.27.4 (`linux/amd64`), Node.js 24.22.1, dan Chromium lokal melalui Playwright 1.65.1. Pengujian memakai gateway mock di loopback dan direktori data sementara.

## Pendalaman tes backend 8 kategori (27 Nov 2026)

Untuk 19 fitur pendalaman (PIN/auth, log audit tool, cache respons, health check, uji latency, pasang skill, persona, urutan bicara ruang, arsip ruang, rerun, fork, ekspor diskusi, reaksi, balasan berantai, debat, mode kompak, tool kustom http_post + parameter ber-tipe) ditambahkan **48 fungsi tes + 6 benchmark baru** di 8 file `depth_*_test.go`. Total kini **238 fungsi tes** (233 `internal/musehood` + 5 `main`) dan **12 benchmark** di 44 file `*_test.go`; cakupan pernyataan `internal/musehood` **82,8%** (`go test ./... -timeout 600s -count=1 -cover`, semua hijau).

| Kategori | File | Isi |
| --- | --- | --- |
| Security | `depth_security_test.go` (6) | Bypass PIN (cookie dimodifikasi/kedaluwarsa/asing, token pekerja salah), PINHash + WorkerToken tidak bocor di state/backup/ekspor, restore backup asing tidak menimpa PIN lokal, penolakan skill install (SSRF, traversal, >200 KB), isolasi lintas ruang, truncasi argumen log. |
| Unit | `depth_unit_test.go` (8) | Hash+verify PIN, token sesi (expiry/tamper), kunci cache + TTL + cap 200, validasi parameter ber-tipe, 6 persona valid, filter seatOrder, reaksi toggle. |
| Database | `depth_store_test.go` (6) | Roundtrip field baru, default aman data lama, cap log sesudah reload, data korup parsial tanpa panic, save atomik konkuren, PIN/token bertahan sesudah restart. |
| Contract | `depth_contract_test.go` (8) | Bentuk respons + status endpoint baru (login/PIN, tool-log, persona, debat, skill install, health/latency terurut, rerun/fork/arsip, header ekspor), semua error `{error}`. |
| API endpoint | `depth_api_test.go` (6) | Matriks metode 405/404, body rusak 400, ID tak dikenal 404, konflik 409/400/403, header cookie (HttpOnly/SameSite), gerbang PIN persis `{"error":"PIN diperlukan"}`. |
| E2E service | `depth_e2e_test.go` (4) | Alur PIN penuh, debat 5 tahap dengan integrator antrean Muse, skill terpasang memengaruhi prompt, cache pada alur pesan nyata. |
| Integration | `depth_integration_test.go` (7) | Arsip + pencarian, rerun membatalkan job berjalan, fork me-remap replyTo, rantai pada kursi antrean, health + CRUD provider, log audit dari chat nyata, persona dipakai diskusi. |
| Performance/load/stress | `depth_perf_test.go` (3 + 6 benchmark) | Benchmark PIN/sesi/log/cache/instruksi tool; state 3000 pesan + log penuh (state 46 ms, tool-log 6,5 ms); konkurensi rerun+fork+arsip (juga bersih di `-race`); 50 login salah tidak mengunci server. |

Dua perbaikan produksi ditemukan oleh tes ini: (1) restore backup kini selalu mempertahankan PINHash + WorkerToken lokal (sebelumnya backup rekayasa bisa menimpa PIN dan mengunci pemilik); (2) peta health in-memory dibersihkan saat provider dihapus.

## E2E semua fitur + test live (26 Nov 2026)

Dua lapis pengujian end-to-end yang memetakan **semua** fitur aplikasi sebagai alur pengguna:

| Perintah | Hasil |
| --- | --- |
| `go test -run TestE2EAllFeatures -count=1 -v ./internal/musehood/` | **16/16 subtest LULUS** (`e2e_test.go`): halaman utama + fallback SPA, state awal, pengaturan (level review/mode gelap/demo/bergilir), kustomisasi kursi + integrator, CRUD provider + alias + Fetch/Test Models, Tes Koneksi + Tes Model, diskusi bergilir, mention, antrean Muse via API, pipeline `Tugas:` 6 tahap + progres, `Gambar:`, operasi pesan (edit/hapus/balas/reaksi), unggah avatar/gambar/file, kursi kustom, ekspor JSON/MD, keamanan (Host/metode/JSON rusak), bersihkan chat. |
| `go test -race -run TestE2EAllFeatures -count=1 ./internal/musehood/` | Lulus, race detector bersih. |
| `go test -count=1 -cover ./...` | Lulus. Cakupan pernyataan `internal/musehood` **84,4%** (140 fungsi tes). |
| `node tests/live-e2e.mjs` | **49 PASS, 0 FAIL** (~1,2 dtk). Menjalankan binary asli pada port uji 3198 dengan direktori data sementara + mock gateway 3199, lalu menguji semua fitur lewat HTTP sungguhan (termasuk file gambar/unggahan diambil kembali, Host asing ditolak 403 via HTTP mentah, dan cek read-only terhadap server produksi lokal 127.0.0.1:3001 tanpa mengubah data). Keluar dengan kode 1 bila ada yang gagal; port uji dibersihkan setelah selesai. |

Catatan perilaku yang terverifikasi e2e: dengan Diskusi Bergilir aktif, pesan biasa dijawab semua kursi berurutan (MUSE via antrean pekerja lebih dulu); error provider pada Tes Model diteruskan apa adanya (mis. 401 dari gateway); field file unggahan adalah `avatar` (avatar), `image` (gambar), dan `file` (file umum).

| Perintah | Hasil |
| --- | --- |
| `./build.sh` setelah menghapus `frontend/build/`, `frontend/.svelte-kit/`, dan binary | Lulus. Adapter-static membuat `frontend/build/index.html`. Go menghasilkan binary dengan `GOPROXY=off` dan `GOFLAGS=-mod=mod`. |
| `(cd frontend && npm run check)` | 0 error dan 0 peringatan. |
| `go test -count=1 -cover ./...` | Lulus. Cakupan pernyataan `internal/musehood` 83,0%. |
| `go test -race -count=1 ./...` | Lulus. Race detector tidak melaporkan masalah. |
| `go vet ./...` | Lulus. |
| `MUSEHOOD_BROWSER=1 node --test tests/binary.test.mjs` | 2 tes lulus dengan `frontend/build/` disisihkan. Keduanya menjalankan salinan binary dari direktori kosong. |
| `(cd frontend && npm run test:unit)` | **45 lulus**: helper format/API, parser Markdown, lazy-load syntax highlighting, composable `chat.svelte.ts`. |
| `(cd frontend && npm run test:component)` | **21 lulus**: render SSR komponen Svelte (termasuk Markdown, blok kode, modal info kursi). |
| `node tests/frontend-e2e.mjs` | **21 PASS**: chat, mention + modal info kursi, render Markdown + syntax highlighting, pin, ruang, pencarian, voting, persetujuan tool. |
| `node tests/frontend-e2e-settings.mjs` | **20 PASS**: CRUD provider, fetch/test models, toggle, kursi custom, ruang, preset, statistik, backup/restore, ekspor. |
| `node tests/frontend-e2e-actions.mjs` | **10 PASS**: edit/hapus pesan, reaksi, balas/kutip, unggah gambar & file. |
| `node tests/frontend-a11y.mjs` | **20 PASS**: audit axe-core terang + gelap, nama aksesibel, keyboard/fokus, kontras WCAG, target sentuh. |
| `node tests/frontend-visual.mjs` | **5 PASS**: screenshot 5 skenario vs baseline piksel. |
| `node tests/frontend-perf.mjs` | **14 PASS**: viewport 320–1920px, emulasi sentuh iPhone, anggaran bundle <350 KB (aktual 248 KB). |

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
