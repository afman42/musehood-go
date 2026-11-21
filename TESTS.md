# Pengujian Musehood Go

Dijalankan 21 November 2026 dengan Go 1.27.4 (`linux/amd64`), Node.js 24.22.1, dan Chromium lokal melalui Playwright 1.65.1. Pengujian memakai gateway mock di loopback dan direktori data sementara.

| Perintah | Hasil |
| --- | --- |
| `./build.sh` setelah menghapus `frontend/build/`, `frontend/.svelte-kit/`, dan binary | Lulus. Adapter-static membuat `frontend/build/index.html`. Go menghasilkan binary dengan `GOPROXY=off` dan `GOFLAGS=-mod=mod`. |
| `(cd frontend && npm run check)` | 0 error dan 0 peringatan. |
| `go test -count=1 -cover ./...` | Lulus. Cakupan pernyataan `internal/musehood` 82,8%. |
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

## Binary dan browser

Tes menjalankan binary dari folder sementara yang terpisah dari repo. Tes pertama memastikan port default 3001 melayani halaman utama, fallback SPA, dan SVG ter-embed. Tes Chromium memakai override port lokal dan memuat aplikasi dari rute SPA. Tes memeriksa strip kursi horizontal, composer, dan lebar halaman pada viewport 320, 390, serta 1440 piksel.

Chromium juga menjalankan alur pengaturan, daftar model, mask key, unggah avatar, mention, SSE, error 401 asli, redaksi key, gambar inline, pipeline, respons file Muse, progres via API pekerja, serta konfirmasi clear. Tes memverifikasi halaman bebas error JavaScript dan CSP. Log proses diperiksa terhadap key fixture, lalu binary dihentikan dengan SIGTERM.

## Kompatibilitas data Node

Verifikasi manual dengan modul `Store`, `Engine`, dan `MuseQueue` dari repo Node referensi. Node membuat state dan request `@Muse` yang tertunda dalam direktori sementara. Binary Go membuka data itu pada port lokal 3011 dan menerima jawaban dari file respons. Setelah Go berhenti, `Store` Node membaca kembali nama pemilik, pesan, status job `completed`, dan ID antrean yang sama. Tes Go juga memeriksa pemulihan state yang tersimpan di antara tahap terakhir dan penandaan job selesai.

Model gateway pemilik dan pekerja Muse nyata berada di luar cakupan pengujian. Gateway lokal dan penulisan file respons menguji kontrak integrasi yang dipakai masing-masing.

## Mock gateway mandiri

Jalankan `node tests/mock-gateway.mjs` untuk melayani loopback pada port 3101 atau atur `MOCK_PORT`. Masukkan `http://127.0.0.1:3101` dan key fixture `browser-fixture-only-9876` pada pengaturan Musehood. Model fixture tersedia dengan nama `sse`, `array`, `plain`, `delta`, `image`, `denied`, dan `key-echo`.
