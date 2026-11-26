# Musehood Go

Grup chat lokal berbahasa Indonesia untuk Arman dan tujuh kursi AI. Backend Go memakai standard library. Frontend SvelteKit 3, Svelte 5, dan TypeScript dibangun sebagai SPA dan disertakan di binary Go. Rilis terakhir: **v1.4.0** (40 binary lintas platform di GitHub Releases).

## Mulai

Gunakan Go **1.27.4** dan Node.js **24** untuk build pada VM ini.

```sh
cd ~/workspace/musehood-go
./build.sh
./musehood-go
```

Buka **http://127.0.0.1:3001**. Skrip memasang dependensi npm, membuat `frontend/build/`, lalu menjalankan `go build` dengan `GOPROXY=off`, `GOFLAGS=-mod=mod`, dan `GOTOOLCHAIN=local`. Node.js dipakai untuk build dan tes browser. Saat dijalankan, binary membawa HTML, JavaScript, CSS, dan avatar bawaannya sendiri.

### Flag CLI

```sh
./musehood-go -host 127.0.0.1 -port 3001 -data-dir /path/data -trusted-hosts chat.example.org
./musehood-go -version   # tampilkan versi
./musehood-go -h         # bantuan
```

| Flag | Bawaan | Env | Keterangan |
| --- | --- | --- | --- |
| `-host` | `127.0.0.1` | `HOST` | Host untuk mendengarkan; kosongkan (`-host ""`) untuk semua antarmuka |
| `-port` | `3001` | `PORT` | Port untuk mendengarkan (1–65535) |
| `-data-dir` | direktori kerja | `DATA_DIR` | Direktori data runtime |
| `-trusted-hosts` | — | `TRUSTED_HOSTS` | Host tambahan yang dipercaya, dipisahkan koma |
| `-version` | — | — | Tampilkan versi lalu keluar |

Flag mengalahkan env, env mengalahkan bawaan. Proxy lokal perlu meneruskan Host yang sama dengan host pada Origin browser. Host dan Origin diperiksa pada request, termasuk endpoint pekerja.

### Instalasi sebagai service (opsional)

Untuk menjalankan Musehood Go sebagai service latar di Linux, gunakan `install.sh`:

```sh
./install.sh                      # binary saja (ke /usr/local/bin atau ~/.local/bin)
sudo ./install.sh --service       # binary + service latar
sudo ./install.sh --uninstall     # lepas service + binary (data dipertahankan)
```

`install.sh` mengunduh binary rilis yang sesuai arsitektur mesin (atau pakai `--binary PATH` untuk binary lokal), lalu — bila `--service` — mendeteksi init system dan memasang service yang cocok:

| Init system | Template | Contoh distro |
| --- | --- | --- |
| systemd | `packaging/systemd/musehood-go.service` | Ubuntu, Debian, Fedora, Arch, RHEL, openSUSE |
| OpenRC | `packaging/openrc/musehood-go` | Alpine, Gentoo, Artix |
| SysV init | `packaging/sysv/musehood-go` | distro lama |

Service berjalan sebagai user sistem khusus (`musehood`, dibuat otomatis), data di `/var/lib/musehood-go`, dan memakai flag CLI (`-host`, `-port`, `-data-dir`). Semua bisa diubah lewat opsi: `--user`, `--host`, `--port`, `--data-dir`, `--service-name`, `--version`. Lihat `./install.sh --help`. Repo GitHub bersifat privat — set `GITHUB_TOKEN` bila unduhan butuh autentikasi.

Catatan: unit systemd menyertakan pengerasan dasar (`NoNewPrivileges`, `ProtectSystem=full`, dll.). Tool `run_code`/`run_shell` memakai bubblewrap, jadi pastikan user namespace tidak dimatikan di kernel.

## Build release (semua platform)

`build-release.sh` membangun frontend sekali, lalu cross-compile binary Go untuk semua pasangan `GOOS/GOARCH` dari `go tool dist list` (`CGO_ENABLED=0`, `GOPROXY=off`, `GOFLAGS=-mod=mod`). Hasilnya di `dist/<os>-<arch>/musehood-go` (`.exe` untuk windows).

```sh
cd ~/workspace/musehood-go
./build-release.sh
```

Yang wajib berhasil: `linux/amd64`, `linux/arm64`, `darwin/amd64`, `darwin/arm64`, `windows/amd64`, `windows/arm64`. Platform yang gagal (Android 32-bit/amd64 dan iOS — butuh external linking cgo) dan yang bukan executable mandiri (`js/wasm`, `wasip1/wasm`) dilewati dan dicatat di `dist/build-release.log`. Direktori `dist/` tidak masuk git (lihat `.gitignore`); binary release diunggah sebagai asset GitHub Release (tag `v1.1.0` ke atas). Lihat hasil build tiap platform di [TESTS.md](TESTS.md).

## Kursi

| Kode | Nama awal | Peran |
| --- | --- | --- |
| MUSE | Muse | Admin dan Integrasi |
| M10 | Opus | Perencana |
| M01 | Plamo | Pelaksana |
| M02 | GLM | Peninjau |
| M03 | Argon | Visual |
| M04 | Deepseek | Penasihat |
| M05 | Fable | Analis Data |

Semua kursi — termasuk MUSE — dapat diubah dari Pengaturan: nama, peran, provider ("muse" untuk antrean pekerja atau "gateway"), model, model fallback, memori per kursi, dan avatar. Pengaturan `integratorSeat` memilih kursi yang menutup pipeline "Tugas:" dan menjawab Tanya Progres (bawaan MUSE).

Selain tujuh kursi bawaan, pemilik dapat menambah kursi AI sendiri dari Pengaturan (bagian "Tambah kursi"): isi nama, peran, dan nama model gateway. Kursi custom mendapat kode otomatis `C01`, `C02`, … dan langsung muncul di daftar kursi, bisa di-mention (`@Nama`), ikut Diskusi Bergilir, serta bisa diganti avatar dan modelnya seperti kursi lain. Kursi custom dapat dihapus dari Pengaturan (kursi bawaan tidak dapat dihapus, dan kursi yang sedang dipakai tugas berjalan tidak dapat dihapus sampai tugas selesai).

**Preset kursi** menyimpan susunan kursi saat ini (nama, peran, provider, model, fallback, memori) sebagai snapshot bernama yang dapat diterapkan kembali atau dihapus.

API: `POST /api/seats` (`{name, role, model}` → 201 kursi baru), `DELETE /api/seats/{code}` (→ `{ok:true}`), `GET/POST /api/presets`, `POST /api/presets/{id}/apply`, `DELETE /api/presets/{id}`. Nama tiap kursi harus unik agar @mention tidak ambigu.

## Konfigurasi gateway

Aplikasi mendukung **banyak provider gateway** sekaligus. Setiap provider punya nama, alias opsional, Base URL, API key, dan protokol (`openai`, `openai-responses`, `anthropic`, `gemini`, `ollama`). Satu provider aktif dipakai sebagai bawaan; Base URL boleh berakhiran `/v1` (dipotong otomatis). API key tidak pernah dikirim ke klien — tampilan hanya memuat `hasApiKey` dan status aktif.

1. Buka ikon gir **Pengaturan grup**, bagian Provider Gateway. Tambah provider, isi Base URL dan API key melalui formulir password.
2. Klik **Fetch Models** untuk melihat daftar model asli provider, dan **Tes Model** untuk menguji model terpilih (maksimal 10 model sekali tes).
3. Isi nama model untuk tiap kursi gateway. Model dapat ditulis `alias/model` untuk mengarahkan kursi ke provider beralias itu tanpa mengganti provider aktif (awalan alias dibuang sebelum dikirim).
4. Klik **Tes Koneksi** untuk menyimpan pengaturan dan melihat status tiap kursi. Input key kosong mempertahankan key yang tersimpan; **Hapus key** menghapusnya.

Gateway memakai `GET /v1/models`, `POST /v1/chat/completions` (atau endpoint sesuai protokol), dan `POST /v1/images/generations`. Error provider ditampilkan bersama status HTTP dan isi asli, dengan key disamarkan. Fallback demo dapat diaktifkan dari pengaturan untuk chat gateway dan ditandai **DEMO** pada pesan. Pengaturan awalnya mati. Bila model utama kursi gagal, **model fallback per kursi** dicoba sekali secara otomatis.

API: `GET/POST /api/providers`, `POST/DELETE /api/providers/{id}`, `POST /api/providers/{id}/activate`, `GET /api/providers/{id}/models`, `POST /api/providers/{id}/test-models`, `POST /api/test-model`, `POST /api/test-connection`.

## Percakapan

- Chat biasa mengikuti urutan tabel saat **Diskusi Bergilir** aktif, atau menuju kursi integrator saja saat toggle mati.
- Sebut kursi dengan `@Opus` atau kode seperti `@M10`. Beberapa mention akan ditangani sesuai urutan kursi. Gunakan **@ Kursi** atau tekan avatar untuk menyisipkan mention.
- `Tugas: …` menjalankan M10 → M01 → M04 → M03 (gambar) → M02 → integrator dengan label **Hasil Akhir - Integrasi**. **Tanya Progres** mengirim judul, tahap terakhir, dan status tugas ke integrator.
- `Gambar: …` memakai gateway gambar. Jika gagal, Argon menampilkan error asli dan menunggu gambar dari pekerja Muse.
- **Voting jawaban terbaik**: tombol Vote meminta kursi gateway yang menjawab diskusi terakhir memilih jawaban terbaik; pemenang ditandai dan integrator menerbitkan ringkasan hasil voting.
- **Bersihkan chat** menghapus riwayat, status tugas, dan antrean aktif setelah konfirmasi. Pengaturan tetap tersimpan.

## Ruang, pencarian, dan ekspor

- **Multi-ruang**: setiap ruang punya riwayat dan susunan kursi sendiri. Ruang bawaan "Utama" menampung data lama. Pengalih ruang ada di header; ruang dikelola dari Pengaturan. Daftar kursi kosong pada ruang berarti semua kursi ikut.
- **Pencarian** mencari teks di semua ruang (`GET /api/search?q=…`) dan menampilkan hasil dari server.
- **Pesan tersemat (pin)**: pesan dapat disematkan dan tampil di bar tersemat di atas chat.
- **Ekspor**: chat dapat diekspor sebagai JSON atau HTML siap cetak/PDF, sadar ruang aktif.
- **Cadangan & pulihkan**: `GET /api/backup` mengunduh seluruh data dengan semua API key dikosongkan; `POST /api/restore` memulihkan cadangan sambil mempertahankan API key lama per ID provider.

## Lampiran file

Tombol **Lampirkan File** di composer mengunggah satu file (maksimal 10 MB) ke server (`POST /api/upload-file`, multipart field `file`). File disimpan di direktori `uploads/` dengan nama `<uuid>-<nama-asli-disani>` dan izin `0600`. Pesan dapat berisi teks, lampiran, atau keduanya. Gambar (PNG/JPEG/GIF/WebP) tampil inline di bubble chat; file lain tampil sebagai tautan unduh dengan nama dan ukuran file. Pesan lampiran disimpan sebagai `attachment: {url, name, size, mime}` dan ikut diekspor bersama chat. File lampiran TIDAK dihapus saat "Bersihkan chat" (sama seperti gambar di `images/` dan avatar di `avatars/` — yang dihapus hanya pesan dan antreannya).

API: `POST /api/upload-file` (→ `{url, name, size, mime}`), `GET /uploads/<file>` (penyajian file tersimpan), `POST /api/message` menerima `attachment` opsional.

## Tool untuk kursi gateway

Kursi gateway dapat bertindak memakai tool saat menjawab (maksimal 3 putaran tool per balasan). Protokolnya berbasis teks — model menulis blok `<<TOOL:nama>>argumen<</TOOL>>`, server menjalankan tool-nya, lalu hasil tool dikirim kembali sebagai pesan lanjutan — sehingga bekerja di semua provider/protokol, termasuk jembatan CLI yang hanya mengembalikan teks. Pesan yang memakai tool menampilkan chip nama tool di UI. Toggle **"Tool untuk kursi gateway"** di Pengaturan mengendalikan semuanya (bawaan aktif).

| Tool | Fungsi |
| --- | --- |
| `web_search` | Pencarian web (DuckDuckGo), 5 hasil teratas |
| `fetch_url` | Ambil isi teks halaman web (alamat privat/localhost diblokir) |
| `list_files` / `read_file` / `write_file` | Folder kerja bersama `sandbox/` (maks 100 KB per file) |
| `append_file` / `edit_file` | Tambah teks ke akhir file; ganti sebagian isi file (penanda `<<<OLD>>>`/`<<<NEW>>>`) |
| `delete_file` | Hapus file di sandbox — **memerlukan persetujuan pemilik** |
| `download_file` | Unduh URL ke sandbox (maks 10 MB, penjaga SSRF) |
| `zip_files` | Kompres file sandbox ke zip (maks 50 MB) |
| `unzip_file` | Ekstrak zip di sandbox (rata, anti zip-slip, maks 100 entri/50 MB) |
| `file_hash` | SHA-256 sebuah file |
| `image_info` | Format, dimensi, dan ukuran gambar (PNG/JPEG/GIF) |
| `resize_image` | Perkecil gambar ke lebar maksimum (bilinear, rasio dipertahankan) |
| `convert_image` | Ubah format gambar PNG ↔ JPEG |
| `pdf_text` | Ekstrak teks PDF (memakai `pdftotext` bila tersedia) |
| `csv_json` | Konversi CSV ↔ JSON (baris pertama CSV = nama kolom) |
| `generate_image` | Buat gambar lewat endpoint `images/generations` provider; gambar terlampir pada balasan |
| `memory_save` | Simpan catatan ke memori per kursi (maks 500 char/catatan, total 4000 char) |
| `current_time` | Tanggal dan waktu sekarang (Asia/Jakarta) |
| `run_code` | Jalankan Python/JavaScript di sandbox terisolasi |
| `run_shell` | Jalankan perintah shell di sandbox yang sama |

### Sandbox kode dan persetujuan pemilik

`run_code` dan `run_shell` berjalan di sandbox bubblewrap sebagai user `nobody`: filesystem di luar folder `sandbox/` tidak terlihat sama sekali, tanpa jaringan, dengan batas CPU/memori/proses (cgroup lewat `systemd-run --scope`), timeout 10 detik, dan output dibatasi 4.000 karakter. Keduanya dikendalikan toggle terpisah **"Tool coding (run_code & run_shell)"** (bawaan aktif).

Seperti mode approval pada agent terminal (mis. Hermes), tindakan berisiko menunggu persetujuan pemilik lewat banner **Setujui / Tolak** di chat (timeout 180 detik, lalu otomatis ditolak):

- Kode yang terdeteksi pola berbahaya (`os.system`, subprocess, akses jaringan, baca path absolut, `rm -rf`, loop tak berujung, dll.) — deteksi berbasis pola, bukan jaminan.
- Semua perintah `run_shell`.
- Semua `delete_file`.

API: `POST /api/approvals/{id}` dengan `{approved: bool}`; permintaan yang menunggu muncul sebagai `pendingApprovals` di `/api/state` (kind `code`/`shell`/`delete`).

## Statistik pemakaian

`GET /api/stats` mengembalikan pemakaian per kursi dan per model (jumlah request, sukses/gagal, latensi, token) untuk chat dan gambar. Tabel statistik tampil di Pengaturan.

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
| GET | `/api/state` | State grup, kursi, pengaturan termask, pesan ruang aktif, kehadiran, jobs, tugas aktif, provider, ruang, preset, statistik, `pendingApprovals` |
| POST | `/api/message` | `{text}` dan respons 202 `{jobId,status,messages,queue}`; opsional `replyTo`, `imageUrl`, `attachment` |
| POST | `/api/message/{id}/edit` | Ubah teks pesan |
| POST | `/api/message/{id}/delete` | Hapus pesan |
| POST | `/api/message/{id}/react` | Beri reaksi pada pesan |
| POST | `/api/message/{id}/pin` | Sematkan/lepas sematan pesan |
| POST | `/api/upload-file` | Multipart field `file`, maksimal 10 MB, respons `{url,name,size,mime}` |
| POST | `/api/upload-image` | Multipart gambar untuk pesan |
| POST | `/api/seats` | `{name,role,model}` → 201 kursi custom baru |
| DELETE | `/api/seats/{code}` | Hapus kursi custom → `{ok:true}` (kursi bawaan: 400) |
| GET/POST | `/api/rooms` | Daftar ruang; buat ruang `{name, seatCodes?}` |
| POST | `/api/rooms/{id}` | Ubah ruang |
| DELETE | `/api/rooms/{id}` | Hapus ruang |
| POST | `/api/rooms/{id}/activate` | Jadikan ruang aktif |
| GET/POST | `/api/presets` | Daftar preset; simpan preset `{name}` |
| POST | `/api/presets/{id}/apply` | Terapkan preset |
| DELETE | `/api/presets/{id}` | Hapus preset |
| GET/POST | `/api/providers` | Daftar provider (tanpa key); tambah provider |
| POST/DELETE | `/api/providers/{id}` | Ubah/hapus provider |
| POST | `/api/providers/{id}/activate` | Jadikan provider aktif |
| GET | `/api/providers/{id}/models` | Daftar model provider |
| POST | `/api/providers/{id}/test-models` | Tes sampai 10 model |
| POST | `/api/approvals/{id}` | `{approved}` untuk persetujuan tool berisiko |
| POST | `/api/vote` | Voting jawaban terbaik diskusi terakhir |
| GET | `/api/stats` | Statistik pemakaian per kursi/model |
| GET | `/api/search?q=…` | Pencarian lintas ruang |
| GET | `/api/backup` | Unduh cadangan (API key dikosongkan) |
| POST | `/api/restore` | Pulihkan cadangan (maks 32 MB, key lama dipertahankan) |
| GET | `/api/export` | Ekspor chat (JSON/HTML, sadar ruang) |
| POST | `/api/progress` | Respons 202 seperti pesan. HTTP 409 bila belum ada tugas |
| POST | `/api/settings` | Patch `{owner,group,seats,settings}` dan state terbaru |
| POST | `/api/test-connection` | `{models,seats,testedAt}` dari gateway tersimpan |
| POST | `/api/test-model` | Tes satu model pada provider aktif |
| POST | `/api/clear` | `{ok:true}` |
| GET | `/api/queue/muse` | `{requests:[…]}` |
| POST | `/api/queue/muse/:id` | `{text,imageUrl?}` atau `{error}` |
| POST | `/api/upload-avatar` | Multipart dengan `target` dan `avatar`, raster maksimal 2 MB |

Browser mengambil hasil job dari `/api/state`. Semua error API memakai `{error}`. Pengaturan mendukung `owner.name`, `group.name`, `seats[].{code,name,role,provider,model,fallbackModel,memory}`, dan `settings.{baseUrl,apiKey,clearApiKey,roundRobin,reviewLevel,demoFallback,integratorSeat,toolsEnabled,codeToolEnabled}`. Avatar dapat diunggah untuk `group`, `owner`, atau kode kursi.

## Penyimpanan

`DATA_DIR` memakai direktori kerja sebagai default. Isinya `data.json` dengan skema Musehood versi 1, folder `queue/`, `avatars/` untuk unggahan, `images/` untuk gambar, `uploads/` untuk lampiran file, dan `sandbox/` sebagai folder kerja tool kursi. `data.json` ditulis atomik dengan izin `0600` dan memuat key gateway konfigurasi. Untuk memindahkan data Node, salin `data.json`, `queue/`, isi unggahan dari `public/avatars/`, serta `public/images/` ke struktur runtime Go. Jalankan satu proses per `DATA_DIR`.

## Performa

Terukur di VM pengembangan (RAM tersedia ~400 MB, GOMAXPROCS=2):

- `/api/state` lokal: median **2 ms** per request; proses server memakai **~10 MB** RAM; binary 12 MB.
- `run_code` Python: **~50 ms** per eksekusi (termasuk startup sandbox + interpreter); JavaScript **~93 ms**.
- Deteksi pola berbahaya: 47 µs; tulis+baca file sandbox: 22 µs.

Benchmark dapat dijalankan ulang dengan `go test -bench . ./internal/musehood/`.

## Pengujian

```sh
export PATH=/home/hatch/go-toolchain/go/bin:$PATH
export GOPROXY=off GOFLAGS=-mod=mod GOTOOLCHAIN=local
./build.sh
go test ./...
go test -race ./...
go test -bench . ./internal/musehood/
(cd frontend && npm run check)
node tests/live-e2e.mjs
node --test tests/binary.test.mjs
MUSEHOOD_BROWSER=1 node --test tests/binary.test.mjs
```

`tests/live-e2e.mjs` menjalankan binary asli terhadap gateway mock dan menguji 49 skenario end-to-end. Uji Chromium menggunakan instalasi Playwright lokal. Isi `PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH` bila menggunakan executable Chromium dari lokasi lain. Lihat hasil dan cakupan pengujian di [TESTS.md](TESTS.md).
