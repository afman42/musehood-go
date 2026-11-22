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

## Mock gateway mandiri

Jalankan `node tests/mock-gateway.mjs` untuk melayani loopback pada port 3101 atau atur `MOCK_PORT`. Masukkan `http://127.0.0.1:3101` dan key fixture `browser-fixture-only-9876` pada pengaturan Musehood. Model fixture tersedia dengan nama `sse`, `array`, `plain`, `delta`, `image`, `denied`, dan `key-echo`. Mock juga melayani jalur per protokol: `/v1/messages` (Anthropic, key `anthropic-mock-key`), `/v1beta/models/…:generateContent` (Gemini, key `gemini-mock-key`), `/v1/responses` (OpenAI Responses), dan `/api/chat` (Ollama, tanpa key).
