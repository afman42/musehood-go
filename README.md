# Musehood Go

Grup chat lokal berbahasa Indonesia untuk Arman dan tujuh kursi AI. Backend Go memakai standard library. Frontend SvelteKit 3, Svelte 5, dan TypeScript dibangun sebagai SPA dan disertakan di binary Go.

## Mulai

Gunakan Go **1.27.4** dan Node.js **24** untuk build pada VM ini.

```sh
cd ~/workspace/musehood-go
./build.sh
./musehood-go
```

Buka **http://127.0.0.1:3001**. Skrip memasang dependensi npm, membuat `frontend/build/`, lalu menjalankan `go build` dengan `GOPROXY=off`, `GOFLAGS=-mod=mod`, dan `GOTOOLCHAIN=local`. Node.js dipakai untuk build dan tes browser. Saat dijalankan, binary membawa HTML, JavaScript, CSS, dan avatar bawaannya sendiri.

Server mendengarkan `127.0.0.1`. Atur `PORT` untuk mengganti port dan `DATA_DIR` untuk mengganti direktori runtime. `TRUSTED_HOSTS` menerima nama host tambahan yang dipisahkan koma, misalnya `TRUSTED_HOSTS=chat.example.org`. Proxy lokal perlu meneruskan Host yang sama dengan host pada Origin browser. Host dan Origin diperiksa pada request, termasuk endpoint pekerja.

## Build release (semua platform)

`build-release.sh` membangun frontend sekali, lalu cross-compile binary Go untuk semua pasangan `GOOS/GOARCH` dari `go tool dist list` (`CGO_ENABLED=0`, `GOPROXY=off`, `GOFLAGS=-mod=mod`). Hasilnya di `dist/<os>-<arch>/musehood-go` (`.exe` untuk windows).

```sh
cd ~/workspace/musehood-go
./build-release.sh
```

Yang wajib berhasil: `linux/amd64`, `linux/arm64`, `darwin/amd64`, `darwin/arm64`, `windows/amd64`, `windows/arm64`. Platform yang gagal (Android 32-bit/amd64 dan iOS — butuh external linking cgo) dan yang bukan executable mandiri (`js/wasm`, `wasip1/wasm`) dilewati dan dicatat di `dist/build-release.log`. Direktori `dist/` tidak masuk git (lihat `.gitignore`); binary release diunggah sebagai asset GitHub Release (tag `v1.1.0` ke atas). Lihat hasil build tiap platform di [TESTS.md](TESTS.md).

## Konfigurasi gateway

1. Buka ikon gir **Pengaturan grup**. Masukkan Base URL gateway OpenAI-compatible dan API key melalui formulir password. Base URL dapat berakhiran `/v1`.
2. Isi nama model untuk enam kursi gateway. Argon memakai nama model yang sama untuk chat dan pembuatan gambar.
3. Klik **Tes Koneksi** untuk menyimpan pengaturan dan melihat daftar model asli beserta status tiap kursi.
4. Untuk pembaruan lain, klik **Simpan Pengaturan**. Input key kosong mempertahankan key yang tersimpan. **Hapus key** menghapusnya. Setelah disimpan, pengaturan hanya menampilkan mask dengan empat karakter terakhir untuk key yang lebih panjang dari empat karakter.

Gateway memakai `GET /v1/models`, `POST /v1/chat/completions`, dan `POST /v1/images/generations`. Error provider ditampilkan bersama status HTTP dan isi asli, dengan key disamarkan. Fallback demo dapat diaktifkan dari pengaturan untuk chat gateway dan ditandai **DEMO** pada pesan. Pengaturan awalnya mati.

## Percakapan

| Kode | Nama awal | Peran |
| --- | --- | --- |
| MUSE | Muse | Admin dan Integrasi |
| M10 | Opus | Perencana |
| M01 | Plamo | Pelaksana |
| M02 | GLM | Peninjau |
| M03 | Argon | Visual |
| M04 | Deepseek | Penasihat |
| M05 | Fable | Analis Data |

- Chat biasa mengikuti urutan tabel saat **Diskusi Bergilir** aktif, atau menuju Muse saja saat toggle mati.
- Sebut kursi dengan `@Opus` atau kode seperti `@M10`. Beberapa mention akan ditangani sesuai urutan kursi. Gunakan **@ Kursi** atau tekan avatar untuk menyisipkan mention.
- `Tugas: …` menjalankan M10 → M01 → M04 → M03 (gambar) → M02 → Muse dengan label **Hasil Akhir - Integrasi**. **Tanya Progres** mengirim judul, tahap terakhir, dan status tugas ke Muse.
- `Gambar: …` memakai gateway gambar. Jika gagal, Argon menampilkan error asli dan menunggu gambar dari pekerja Muse.
- **Bersihkan chat** menghapus riwayat, status tugas, dan antrean aktif setelah konfirmasi. Pengaturan tetap tersimpan.

## Pekerja Muse

Aplikasi terhubung ke model Muse melalui pekerja eksternal. Muse memakai antrean file untuk `chat`, `summary`, dan `progress`. Fallback gambar memakai antrean yang sama dengan `seat: "M03"` dan `kind: "image"`.

Permintaan ditulis ke `queue/requests/<id>.json`. Bentuknya sebagai berikut.

```json
{
  "id": "uuid",
  "seat": "MUSE",
  "kind": "chat",
  "prompt": "System prompt dan permintaan pemilik",
  "context": {"group": "Musehood", "owner": "Arman", "activeTask": null, "messages": [], "task": "Permintaan pemilik"},
  "createdAt": "2026-11-21T12:00:00.000Z"
}
```

Pekerja bisa menulis `queue/responses/<id>.json` berisi `{ "text": "jawaban model Muse" }`. Gunakan file sementara dan rename atomik. Pekerja juga bisa membaca `GET /api/queue/muse` dan mengirim hasil ke `POST /api/queue/muse/<id>`. Kirim `{ "error": "rincian kegagalan" }` jika model gagal. Untuk gambar, taruh PNG/JPEG/GIF/WebP di direktori `images/` runtime dan kirim `imageUrl` seperti `/images/hasil.png`. URL HTTP(S) atau data URL raster dapat disalin ke penyimpanan gambar lokal.

Backend memeriksa file setiap 750 ms. Selama menunggu, chat menampilkan indikator mengetik. Setelah 10 menit, status berubah menjadi **Muse belum menjawab — antrean masih menunggu**. Respons yang tiba kemudian tetap masuk ke chat. Job yang menunggu Muse memakai ID antrean yang sama setelah restart.

## API

| Metode | Endpoint | Kontrak |
| --- | --- | --- |
| GET | `/api/state` | State grup, kursi, pengaturan termask, pesan, kehadiran, jobs, tugas aktif, dan hasil koneksi |
| POST | `/api/message` | `{text}` dan respons 202 `{jobId,status,messages,queue}` |
| POST | `/api/progress` | Respons 202 seperti pesan. HTTP 409 bila belum ada tugas |
| POST | `/api/settings` | Patch `{owner,group,seats,settings}` dan state terbaru |
| POST | `/api/test-connection` | `{models,seats,testedAt}` dari gateway tersimpan |
| POST | `/api/clear` | `{ok:true}` |
| GET | `/api/queue/muse` | `{requests:[…]}` |
| POST | `/api/queue/muse/:id` | `{text,imageUrl?}` atau `{error}` |
| POST | `/api/upload-avatar` | Multipart dengan `target` dan `avatar`, raster maksimal 2 MB |

Browser mengambil hasil job dari `/api/state`. Semua error API memakai `{error}`. Pengaturan mendukung `owner.name`, `group.name`, `seats[].{code,name,model}`, dan `settings.{baseUrl,apiKey,clearApiKey,roundRobin,reviewLevel,demoFallback}`. Avatar dapat diunggah untuk `group`, `owner`, atau kode kursi.

## Penyimpanan

`DATA_DIR` memakai direktori kerja sebagai default. Isinya `data.json` dengan skema Musehood versi 1, folder `queue/`, `avatars/` untuk unggahan, dan `images/` untuk gambar. `data.json` ditulis atomik dengan izin `0600` dan memuat key gateway konfigurasi. Untuk memindahkan data Node, salin `data.json`, `queue/`, isi unggahan dari `public/avatars/`, serta `public/images/` ke struktur runtime Go. Jalankan satu proses per `DATA_DIR`.

## Pengujian

```sh
export PATH=/home/hatch/go-toolchain/go/bin:$PATH
export GOPROXY=off GOFLAGS=-mod=mod GOTOOLCHAIN=local
./build.sh
go test ./...
go test -race ./...
(cd frontend && npm run check)
node --test tests/binary.test.mjs
MUSEHOOD_BROWSER=1 node --test tests/binary.test.mjs
```

Uji Chromium menggunakan instalasi Playwright lokal. Isi `PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH` bila menggunakan executable Chromium dari lokasi lain. Lihat hasil dan cakupan pengujian di [TESTS.md](TESTS.md).
