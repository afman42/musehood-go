# Musehood Go

Grup chat lokal berbahasa Indonesia untuk Arman dan tujuh kursi AI. Backend Go (standard library + satu dependensi: driver SQLite pure-Go `modernc.org/sqlite` untuk backend penyimpanan opsional). Frontend SvelteKit 3, Svelte 5, dan TypeScript dibangun sebagai SPA dan disertakan di binary Go. Rilis terakhir: **v1.10.0** (28 Nov 2026, 40 binary lintas platform di [GitHub Releases](https://github.com/afman42/musehood-go/releases/tag/v1.10.0)).

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

Semua kursi — termasuk MUSE — dapat diubah dari Pengaturan: nama, peran, **prompt kustom peran** (editor system prompt per kursi), provider ("muse" untuk antrean pekerja atau "gateway"), model, **rantai fallback bertingkat** (satu model fallback utama + sampai 5 model cadangan yang dicoba berurutan bila model utama gagal), **tool yang dinonaktifkan** untuk kursi itu (allowlist per kursi), memori per kursi, skill aktif, dan avatar. Pengaturan `integratorSeat` memilih kursi yang menutup pipeline "Tugas:" dan menjawab Tanya Progres (bawaan MUSE).

Selain tujuh kursi bawaan, pemilik dapat menambah kursi AI sendiri dari Pengaturan (bagian "Tambah kursi"): isi nama, peran, dan nama model gateway. Kursi custom mendapat kode otomatis `C01`, `C02`, … dan langsung muncul di daftar kursi, bisa di-mention (`@Nama`), ikut Diskusi Bergilir, serta bisa diganti avatar dan modelnya seperti kursi lain. Kursi custom dapat dihapus dari Pengaturan (kursi bawaan tidak dapat dihapus, dan kursi yang sedang dipakai tugas berjalan tidak dapat dihapus sampai tugas selesai).

**Preset kursi** menyimpan susunan kursi saat ini (nama, peran, provider, model, fallback, memori) sebagai snapshot bernama yang dapat diterapkan kembali atau dihapus.

API: `POST /api/seats` (`{name, role, model}` → 201 kursi baru), `DELETE /api/seats/{code}` (→ `{ok:true}`), `GET/POST /api/presets`, `POST /api/presets/{id}/apply`, `DELETE /api/presets/{id}`. Nama tiap kursi harus unik agar @mention tidak ambigu.

## Konfigurasi gateway

Aplikasi mendukung **banyak provider gateway** sekaligus. Setiap provider punya nama, alias opsional, Base URL, API key, dan protokol (`openai`, `openai-responses`, `anthropic`, `gemini`, `ollama`). Satu provider aktif dipakai sebagai bawaan; Base URL boleh berakhiran `/v1` (dipotong otomatis). API key tidak pernah dikirim ke klien — tampilan hanya memuat `hasApiKey` dan status aktif.

1. Buka ikon gir **Pengaturan grup**, bagian Provider Gateway. Tambah provider, isi Base URL dan API key melalui formulir password.
2. Klik **Fetch Models** untuk melihat daftar model asli provider, dan **Tes Model** untuk menguji model terpilih (maksimal 10 model sekali tes).
3. Isi nama model untuk tiap kursi gateway. Model dapat ditulis `alias/model` untuk mengarahkan kursi ke provider beralias itu tanpa mengganti provider aktif (awalan alias dibuang sebelum dikirim).
4. Klik **Tes Koneksi** untuk menyimpan pengaturan dan melihat status tiap kursi. Input key kosong mempertahankan key yang tersimpan; **Hapus key** menghapusnya.

Gateway memakai `GET /v1/models`, `POST /v1/chat/completions` (atau endpoint sesuai protokol), dan `POST /v1/images/generations`. Balasan kursi gateway **di-streaming** (SSE per protokol: openai, openai-responses, anthropic, gemini, ollama) sehingga teks muncul bertahap; bila streaming gagal atau kosong, otomatis jatuh ke permintaan biasa. Saklarnya ada di Pengaturan (`streamingEnabled`, bawaan aktif). Error provider ditampilkan bersama status HTTP dan isi asli, dengan key disamarkan. Fallback demo dapat diaktifkan dari pengaturan untuk chat gateway dan ditandai **DEMO** pada pesan. Pengaturan awalnya mati. Bila model utama kursi gagal, **rantai fallback per kursi** dicoba berurutan secara otomatis. Tiap provider dapat diisi **harga input/output per 1M token** untuk estimasi biaya di statistik.

API: `GET/POST /api/providers`, `POST/DELETE /api/providers/{id}`, `POST /api/providers/{id}/activate`, `GET /api/providers/{id}/models`, `POST /api/providers/{id}/test-models`, `POST /api/test-model`, `POST /api/test-connection`.

## Percakapan

- Chat biasa mengikuti urutan tabel saat **Diskusi Bergilir** aktif, atau menuju kursi integrator saja saat toggle mati.
- Sebut kursi dengan `@Opus` atau kode seperti `@M10`. Beberapa mention akan ditangani sesuai urutan kursi. Gunakan **@ Kursi** atau tekan avatar untuk menyisipkan mention. Mention di dalam bubble pesan tampil sebagai tombol — tekan untuk membuka modal **Info kursi** (avatar, peran, provider/model, status hadir, skill aktif, memori) lengkap dengan tombol **Mention @Nama** untuk langsung menyebut kursi itu.
- `Tugas: …` menjalankan M10 → M01 → M04 → M03 (gambar) → M02 → integrator dengan label **Hasil Akhir - Integrasi**. **Tanya Progres** mengirim judul, tahap terakhir, dan status tugas ke integrator.
- `Gambar: …` memakai gateway gambar. Jika gagal, Argon menampilkan error asli dan menunggu gambar dari pekerja Muse.
- **Markdown di bubble**: teks pesan dirender sebagai Markdown (paragraf, heading, **bold**, *italic*, ~~coret~~, `kode`, blok kode, blockquote, daftar, garis horizontal, tautan) oleh parser subset aman di frontend — HTML mentah tidak pernah diteruskan, dan mention di dalam teks tetap bisa ditekan. Blok kode ber-bahasa diberi **syntax highlighting** (@speed-highlight/core, 34 bahasa) yang dimuat malas per bahasa dan warnanya mengikuti tema terang/gelap aplikasi.
- **Status kirim & progres unggah**: selama pesan dikirim, tombol kirim menampilkan "Mengirim… N dtk" dengan penghitung detik berjalan. Unggahan gambar/file menampilkan bar progres + persen, dan lampiran yang selesai diunggah ditandai "✓ siap dikirim" sebelum pesan dikirim.
- **Voting jawaban terbaik**: tombol Vote meminta kursi gateway yang menjawab diskusi terakhir memilih jawaban terbaik; pemenang ditandai dan integrator menerbitkan ringkasan hasil voting.
- **Regenerate**: tombol ↻ pada balasan kursi menjalankan ulang jawaban kursi itu (pesan lama diganti; langkah gambar menghasilkan gambar baru).
- **Pesan terjadwal**: tombol ⏰ **Jadwalkan** di composer mengirim pesan sekali pada waktu tertentu, atau berulang harian/mingguan/tiap N jam (zona waktu mengikuti browser). Jadwal dikelola dari dialog yang sama.
- **Draft otomatis**: teks composer yang belum terkirim tersimpan per ruang di browser dan pulih saat kembali ke ruang itu.
- **Notifikasi browser**: bila diaktifkan di Pengaturan, notifikasi muncul saat kursi selesai membalas dan tab tidak aktif.
- **Bersihkan chat** menghapus riwayat, status tugas, dan antrean aktif setelah konfirmasi. Pengaturan tetap tersimpan.

## Ruang, pencarian, dan ekspor

- **Multi-ruang**: setiap ruang punya riwayat dan susunan kursi sendiri. Ruang bawaan "Utama" menampung data lama. Pengalih ruang ada di header; ruang dikelola dari Pengaturan. Daftar kursi kosong pada ruang berarti semua kursi ikut. Ruang dapat **disematkan** (📌, naik ke urutan teratas) dan menampilkan **badge belum dibaca** berdasarkan jumlah pesan yang sudah dilihat di browser.
- **Pencarian lanjutan** mencari teks di semua ruang (`GET /api/search?q=…`) dengan filter pengirim (pemilik/kursi), ruang, rentang tanggal, dan ada gambar; kata kunci boleh kosong bila filter aktif.
- **Pesan tersemat (pin)**: pesan dapat disematkan dan tampil di bar tersemat di atas chat.
- **Ekspor**: chat dapat diekspor sebagai JSON atau HTML siap cetak/PDF, sadar ruang aktif.
- **Cadangan & pulihkan**: `GET /api/backup` mengunduh seluruh data dengan semua API key dikosongkan; `POST /api/restore` memulihkan cadangan sambil mempertahankan API key lama per ID provider.

## Template diskusi, ringkasan, dan kompaksi konteks

- **Template diskusi**: preset bernama berisi urutan kursi + instruksi (dibuat di Pengaturan → Template; klik chip kursi untuk menambah langkah, klik langkah untuk menghapus). Pilih template di composer sebelum mengirim — pesan dijalankan sebagai pipeline urutan kursi template dengan instruksi sebagai konteks, sementara pesan pemilik tetap menampilkan teks asli. Maksimal 30 template, 1–12 langkah.
- **Ringkasan diskusi**: tombol 📝 **Ringkas** di baris ruang meminta kursi integrator merangkum ruang aktif; hasil tersemat sebagai pesan berlabel "Ringkasan diskusi".
- **Kompaksi konteks otomatis**: ruang yang bertambah 80 pesan sejak ringkasan terakhir diringkas otomatis. Konteks yang dikirim ke kursi menjadi ringkasan + 40 pesan terakhir, sehingga diskusi panjang tetap ringan.

## Lampiran file

Tombol **Lampirkan File** di composer mengunggah satu file (maksimal 10 MB) ke server (`POST /api/upload-file`, multipart field `file`). File disimpan di direktori `uploads/` dengan nama `<uuid>-<nama-asli-disani>` dan izin `0600`. Pesan dapat berisi teks, lampiran, atau keduanya. Gambar (PNG/JPEG/GIF/WebP) tampil inline di bubble chat; file lain tampil sebagai tautan unduh dengan nama dan ukuran file. Pesan lampiran disimpan sebagai `attachment: {url, name, size, mime}` dan ikut diekspor bersama chat. File lampiran TIDAK dihapus saat "Bersihkan chat" (sama seperti gambar di `images/` dan avatar di `avatars/` — yang dihapus hanya pesan dan antreannya).

API: `POST /api/upload-file` (→ `{url, name, size, mime}`), `GET /uploads/<file>` (penyajian file tersimpan), `POST /api/message` menerima `attachment` opsional.

## Tool untuk kursi gateway

Kursi gateway dapat bertindak memakai tool saat menjawab (maksimal 3 putaran tool per balasan). Protokolnya berbasis teks — model menulis blok `<<TOOL:nama>>argumen<</TOOL>>`, server menjalankan tool-nya, lalu hasil tool dikirim kembali sebagai pesan lanjutan — sehingga bekerja di semua provider/protokol, termasuk jembatan CLI yang hanya mengembalikan teks. Selama tool berjalan, status mengetik menampilkan aktivitasnya ("Plamo menjalankan web_search…"); hasilnya tampil sebagai **kartu tool** di bawah balasan (klik untuk melihat argumen + hasil). Toggle **"Tool untuk kursi gateway"** di Pengaturan mengendalikan semuanya (bawaan aktif), dan tiap kursi dapat menonaktifkan tool tertentu (allowlist per kursi). Total **111 tool bawaan** (36 tool umum + 29 tool coding proyek + 10 tool gaya OpenCode + 36 tool gaya omp):

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
| `git` | Perintah git baca-saja di sandbox (status/diff/log/show/branch/remote) |
| `screenshot_url` | Screenshot halaman web (Chromium headless); gambar terlampir pada balasan |
| `summarize_url` | Ambil isi halaman web untuk diringkas kursi |
| `pdf_from_markdown` | Buat PDF A4 dari teks Markdown ke sandbox |
| `translate_text` | Terjemahan antar bahasa via MyMemory (baris pertama: pasangan `id>en`) |
| `ocr_image` | Baca teks dari gambar sandbox (memakai `tesseract` bila tersedia) |
| `image_caption` | Jelaskan isi gambar sandbox memakai model vision kursi (semua protokol) |
| `qr_code` | QR code PNG dari teks/URL; gambar terlampir pada balasan |
| `rss_feed` | Baca feed RSS 2.0 / Atom (item terbaru) |
| `youtube_transcript` | Transkrip/caption video YouTube tanpa kunci API |
| `currency_convert` | Konversi mata uang, kurs ECB via frankfurter.app (mis. `100 USD IDR`) |
| `text_to_speech` | Teks menjadi WAV di sandbox (memakai `espeak-ng` bila tersedia; `lang=id`/`lang=en`) |
| `http_post` | POST JSON ke URL publik — **memerlukan persetujuan pemilik** |
| `password_gen` | Password acak kuat (8–128 karakter, opsi `nosymbol`) |
| `run_code` | Jalankan Python/JavaScript di sandbox terisolasi |
| `run_shell` | Jalankan perintah shell di sandbox yang sama |
| `ask_question` | Kursi bertanya ke pemilik (baris pertama = pertanyaan, baris berikutnya = pilihan jawaban maks 6); pekerjaan menunggu jawaban (timeout 180 dtk), jawaban bebas selalu bisa lewat banner pertanyaan di chat atau `POST /api/questions/{id}` |
| `spawn_subagent` | Mulai sesi anak di ruang baru "Sub: …" (baris pertama opsional `seat:<kode>`, lalu prompt); kursi di ruang sub tidak boleh spawn lagi (kedalaman maks 1); ringkasan anak diposting ke ruang induk saat selesai |
| `check_subagent` | Baca balasan terakhir sesi anak dari id yang diberikan `spawn_subagent` (atau status masih berjalan) |
| `execute` | Code Mode: jalankan JavaScript di sandbox dengan jembatan RPC — kode memanggil `await tool("nama", "argumen")` dan host menjalankan tool itu dengan cek izin/allowlist/audit/persetujuan yang sama (maks 20 panggilan, timeout 30 dtk; `execute` bersarang dilarang) |
| `browser_open` | Render halaman web dengan Chromium headless lalu ekstrak teks DOM hasil render (penjaga SSRF, timeout 45 dtk) |
| `browser_screenshot` | Screenshot PNG halaman ter-render; gambar terlampir pada balasan |
| `browser_links` | Daftar tautan dari DOM ter-render (maks 50, tanpa duplikat) |

### Tool gaya omp

36 tool tambahan meniru katalog [omp](https://omp.sh/docs/tools). Tool yang butuh binary eksternal (ast-grep, gopls/dlv/govulncheck, Chromium) **degradasi anggun** dengan pesan instalasi bahasa Indonesia bila binary tidak ada — tidak pernah mengarang hasil.

| Tool | Fungsi |
| --- | --- |
| `ast_grep` | Cari pola sintaks (AST) di proyek via ast-grep (`project:<nama>` lalu pola, opsional `lang:<bahasa>`) |
| `ast_edit` | Codemod struktural ast-grep: tanpa `apply` hanya pratinjau diff; `apply` mengubah file setelah **persetujuan pemilik** (kind `edit`) + snapshot proyek otomatis |
| `lsp_definition` / `lsp_references` | Definisi/rujukan simbol via language server stdio (JSON-RPC); server per bahasa dari `settings.lspServers`, bawaan Go → `gopls` |
| `lsp_diagnostics` | Diagnostik file dari language server (`publishDiagnostics`) |
| `lsp_rename` | Ganti nama simbol via LSP `rename` — menerapkan WorkspaceEdit setelah **persetujuan pemilik** + snapshot |
| `eval` | Kernel persisten Python/JavaScript per (ruang+kursi+bahasa) di sandbox yang sama dengan `run_code`: state bertahan antar-panggilan, timeout 30 dtk/panggilan, maks 8 sesi, sesi idle >10 menit dimatikan, argumen `reset` untuk sesi baru; pola berbahaya = persetujuan |
| `debug_launch` / `debug_breakpoint` / `debug_step` / `debug_inspect` / `debug_stop` | Debugger DAP untuk Go via `dlv dap` (satu sesi per ruang): launch program proyek, breakpoint file:baris, continue/next/stepIn/stepOut, stackTrace+variabel, terminate |
| `browser_click` / `browser_type` | Klik / isi elemen via Chromium CDP (selector CSS); `browser_type` mengisi lalu menekan Enter |
| `browser_snapshot` | Teks DOM ringkas halaman aktif (judul + teks terlihat) via CDP |
| `browser_tabs` | Daftar / buka (`new <url>`) / tutup (`close <id>`) tab Chromium CDP; satu browser per aplikasi, penjaga SSRF tetap berlaku |
| `github_read` | Baca repo/file/issue/PR/diff GitHub via REST API (`repo`, `file`, `issue`, `pr`, `diff`); tanpa token hanya publik |
| `github_search` | Pencarian kode/issue GitHub via REST API |
| `github_actions` | Status run GitHub Actions sebuah repo |
| `github_push` | `git push` proyek lokal ke remote GitHub — **persetujuan pemilik** (kind `git`); token dari `settings.githubToken` (dikosongkan di backup) |
| `security_scan` | Pindai keamanan proyek: Go → `govulncheck` bila ada, selain itu `go vet` di sandbox; Node → `npm audit --json`; semua → pola secret (nilai tidak pernah ditampilkan). Pemindai yang hilang dilaporkan jujur |
| `hub_message` | Kirim pesan ke kursi lain di ruang yang sama (tampil sebagai pesan sistem `[hub → …]`) |
| `hub_processes` / `hub_stop` | Daftar / hentikan proses latar terpusat (dev server proyek, kernel eval, sesi debug, browser CDP) |
| `checkpoint` / `rewind` | Tandai titik di ruang / potong pesan sesudah checkpoint (rewind via tool = **persetujuan pemilik**; API/UI pemilik adalah konfirmasinya sendiri) |
| `memory_recall` / `memory_retain` / `memory_reflect` / `memory_edit` | Memori durable bersama (`data.memories`, maks 500): cari kata kunci, simpan (tag opsional), kumpulkan memori terkait untuk disintesis kursi, ubah/hapus; toggle `settings.memoryEnabled` |
| `learn` | Simpan pelajaran ke memori durable; opsional `skill:<nama>` menjadikannya skill terkelola di `<dataDir>/skills/<nama>/SKILL.md` |
| `manage_skill` | Daftar/buat/ubah/pensiunkan skill lokal terkelola (skill skills.sh/claude hanya dihitung, tidak diubah) |
| `todo_add` / `todo_update` / `todo_list` | Checklist per ruang (pending/active/done/blocked, maks 100); terlihat di panel ruang + API pemilik |
| `tool_info` | Deskripsi satu tool (dipakai katalog on-demand: `settings.onDemandTools` membuat system prompt hanya memuat deskripsi penuh tool inti + daftar nama) |

**Kebijakan per tool**: `settings.toolPolicies` memetakan nama tool → `allow`/`prompt`/`deny`, ditegakkan di dispatch (`deny` = ditolak dengan pesan jelas, `prompt` = persetujuan pemilik kind `tool`). Diatur lewat panel "Tool omp & memori" di Pengaturan, bersama token GitHub, server LSP per bahasa, toggle memori/on-demand, dan daftar memori durable.

**Workflow**: `POST /api/review` {project} menyuruh kursi meninjau diff proyek (perubahan belum commit atau commit terakhir) sebagai pekerjaan biasa; `POST /api/commit-workflow` {project} mengusulkan grup commit konvensional per scope — kirim ulang dengan `approve:true` untuk menjalankan commit (push terpisah via `github_push`); `POST /api/autoresearch` {project, script, iterations} menjalankan skrip benchmark di sandbox proyek N iterasi (maks 10) pada branch `autoresearch/*`, metrik = angka terakhir di output, **tanpa reset destruktif otomatis**. Mode ruang: **goal** (`room.goal` disuntik ke system prompt kursi), **plan** (`room.planMode`: hanya tool kelas baca yang boleh sampai pemilik menyetujui rencana), **vibe** (`room.vibeMode`: kursi integrator menjadi direktur yang memakai `spawn_subagent` + hub — batas jujur: bukan runtime pekerja persisten baru). Panel "Panel kerja ruang" di UI mengatur goal/plan/vibe, todo, dan checkpoint/rewind. API pendukung: `POST /api/rooms/{id}/checkpoint` + `/rewind`, `POST /api/rooms/{id}/todos` (+ `/{todoId}/status`, `DELETE /{todoId}`), `GET /api/memories` + `DELETE /api/memories/{id}`.

### Tool coding proyek

Tool coding bekerja pada **proyek bernama** di folder `projects/<nama>/` di direktori data. Awali argumen dengan baris `project:<nama>` untuk memilih proyek (tanpa itu dipakai proyek `default`); path selalu relatif terhadap folder proyek dan tidak boleh keluar dari folder itu. Semua tool tunduk pada saklar tool utama serta allowlist per kursi dan per ruang; tool eksekusi (`run_build`, `run_tests`, `run_lint`, `format_code`, `run_server`, `allowlist_shell`) juga tunduk pada toggle tool coding.

| Tool | Fungsi |
| --- | --- |
| `create_project` | Buat proyek baru dari template `go`/`node`/`python`/`web` (menolak bila sudah ada) |
| `list_tree` | Pohon folder proyek (kedalaman maks 6, 500 entri; `node_modules`/`.git`/`vendor`/`dist` diabaikan) |
| `read_file_lines` | Baca rentang baris file (1-based, maks 500 baris) |
| `move_file` / `copy_file` / `mkdir` | Pindah/salin file (tidak menimpa), buat folder bersarang |
| `search_code` | Grep regex RE2 lintas proyek dengan 2 baris konteks (maks 100 hasil) |
| `find_files` | Cari file dengan pola glob termasuk `**` (maks 200 hasil) |
| `apply_patch` | Terapkan unified diff format git secara atomik (snapshot otomatis dulu) |
| `edit_many` | Banyak edit `<<<OLD>>>`/`<<<NEW>>>` per file dalam satu panggilan (atomik per file, snapshot otomatis) |
| `project_snapshot` | Snapshot folder proyek: `create [label]`/`list`/`restore <id>`/`delete <id>` (maks 10, FIFO; restore = persetujuan pemilik) |
| `run_build` | Build proyek di sandbox tanpa jaringan (go build / npm run build / compileall; timeout 120 dtk) |
| `run_tests` | Jalankan tes proyek di sandbox (go test / npm test / pytest; timeout 180 dtk) |
| `run_lint` | Lint proyek (go vet + gofmt -l; py_compile + ruff bila ada; eslint hanya bila terpasang di proyek) |
| `format_code` | Format kode proyek (gofmt; black/prettier bila tersedia) |
| `run_server` | Dev server proyek sebagai proses latar di 127.0.0.1 (port 4100–4199, satu per proyek; `status`/`stop`) |
| `allowlist_shell` | Perintah aman tanpa persetujuan (ls/cat/grep/find tanpa -exec, versi tool, git baca-saja); metakarakter shell ditolak |
| `git_commit` | `git add -A` + commit di folder proyek — **persetujuan pemilik** (kind `git`) |
| `git_branch` | Branch proyek: `list`/`create <nama>`/`checkout <nama>` (checkout kotor = persetujuan) |
| `git_stash` | Stash proyek: `push`/`pop`/`list` (pop = persetujuan) |
| `outline` | Daftar fungsi/kelas per file (Go/Python/JS/TS) dengan nomor baris |
| `find_references` | Semua pemakaian simbol (word-boundary) lintas proyek (maks 100) |
| `dependency_graph` | Peta import proyek (internal ditandai, eksternal diringkas) |
| `todo_scan` | TODO/FIXME/HACK/XXX dengan file:baris (maks 100) |
| `complexity_report` | LOC per file, 10 file terbesar, fungsi terpanjang |
| `generate_tests` | File tes kerangka dari file sumber (Go `_test.go`, Python `test_*.py`, JS `*.test.js`) |
| `export_project` | Zip proyek ke `exports/` dan kembalikan URL unduhan `/exports/<nama>.zip` |
| `import_project` | Impor zip dari `uploads/` menjadi proyek (penjaga zip-slip; menimpa proyek lama = persetujuan) |
| `env_file` | Kelola `.env` proyek: `list`/`get` (nilai ter-mask)/`set`/`delete`; nilai tidak masuk log audit tool |
| `worktree_create` | `git worktree add` di `projects/<proyek>-wt/<nama>` dengan branch `wt-<nama>` (proyek harus punya minimal 1 commit) |
| `worktree_list` | Daftar worktree proyek (path + branch) |
| `worktree_remove` | Hapus worktree proyek; `force` = persetujuan pemilik (kind `delete`) |

Tool eksekusi proyek memakai sandbox bubblewrap yang sama dengan `run_code` (user `nobody`, tanpa jaringan, folder proyek ter-bind rw, toolchain Go ter-bind read-only di `/goroot`), dengan batas cgroup lebih longgar (1 GB memori) dan timeout 120–180 detik. `run_server` adalah pengecualian: namespace jaringan tidak diputus agar server bisa dijangkau di 127.0.0.1, tetapi filesystem tetap terisolasi dan `HOST=127.0.0.1` dipaksakan. Tool git menulis menjalankan `git` langsung di folder proyek dengan identitas `musehood-go <musehood@local>`.

Beberapa tool memakai binary sistem bila tersedia dan gagal dengan pesan jelas bila tidak ada: `ocr_image` → `tesseract-ocr`, `text_to_speech` → `espeak-ng`, `pdf_text` → `pdftotext` (poppler-utils), `screenshot_url` dan tool `browser_*` → Chromium. Di Debian/Ubuntu: `apt install tesseract-ocr espeak-ng poppler-utils`. Binary Chromium dicari dari env `MUSEHOOD_CHROMIUM`/`CHROME_BIN`, PATH, lalu cache Playwright (`~/.cache/ms-playwright/chromium-*/`); pasang dengan `npx playwright install chromium` bila belum ada. Tool `browser_open`/`screenshot`/`links` memakai CLI Chromium sekali jalan; tool CDP (`browser_click`/`type`/`snapshot`/`tabs`) memakai Chromium headless persisten dengan `--remote-debugging-port` + client WebSocket RFC6455 bawaan (tanpa dependensi baru). Batasan jujur: tidak ada Lighthouse, dan snapshot adalah teks DOM ringkas (bukan pohon aksesibilitas penuh).

### Sandbox kode dan persetujuan pemilik

`run_code` dan `run_shell` berjalan di sandbox bubblewrap sebagai user `nobody`: filesystem di luar folder `sandbox/` tidak terlihat sama sekali, tanpa jaringan, dengan batas CPU/memori/proses (cgroup lewat `systemd-run --scope`), timeout 10 detik, dan output dibatasi 4.000 karakter. Keduanya dikendalikan toggle terpisah **"Tool coding (run_code & run_shell)"** (bawaan aktif).

Seperti mode approval pada agent terminal (mis. Hermes), tindakan berisiko menunggu persetujuan pemilik lewat banner **Setujui / Tolak** di chat (timeout 180 detik, lalu otomatis ditolak):

- Kode yang terdeteksi pola berbahaya (`os.system`, subprocess, akses jaringan, baca path absolut, `rm -rf`, loop tak berujung, dll.) — deteksi berbasis pola, bukan jaminan.
- Semua perintah `run_shell`.
- Semua `delete_file`.
- Semua `http_post` (URL + isi JSON ditampilkan pada banner).

API: `POST /api/approvals/{id}` dengan `{approved: bool}`; permintaan yang menunggu muncul sebagai `pendingApprovals` di `/api/state` (kind `code`/`shell`/`delete`/`http`/`git`/`mcp`). Pertanyaan kursi yang menunggu jawaban muncul sebagai `pendingQuestions` di `/api/state` dan dijawab lewat `POST /api/questions/{id}` dengan `{answer}`.

### Docker

Image multi-stage tersedia lewat `Dockerfile` + `docker-compose.yml` (build lokal saja, tidak di-push ke registry mana pun):

```sh
docker compose up -d --build
```

Tahap pertama membangun frontend dengan Node, tahap kedua membangun binary Go (`CGO_ENABLED=0`, versi dari build arg `VERSION`), tahap runtime memakai Alpine berisi `git`, `nodejs`, `python3`, `tesseract-ocr`, `espeak-ng`, `poppler-utils`, `bubblewrap`, dan `ca-certificates`. Container berjalan sebagai user non-root, data di volume `/data`, port 3001. Catatan jujur: sandbox bubblewrap untuk tool kode butuh `--privileged` di dalam container (ada komentar opsional di `docker-compose.yml`); tanpa itu tool kode jatuh ke mode terbatas sedangkan fitur chat tetap berjalan.

### Skill per kursi

Kursi dapat diberi **skill**: instruksi tambahan dalam format `SKILL.md` yang sama dengan skill pi (frontmatter `name`/`description` + badan Markdown). Kompatibel dengan ekosistem **skills.sh**: selain folder lokal `skills/<nama>/SKILL.md` (atau file `skills/<nama>.md`) di direktori data, aplikasi juga memindai direktori standar skills.sh — `~/.agents/skills/` (target universal `npx skills add -g`) dan `~/.claude/skills/` (biasanya symlink ke sana) — jadi skill yang terpasang via `npx skills` langsung bisa dipilih tanpa disalin. Bila nama sama, skill lokal menang. Pilih skill untuk kursi lewat Pengaturan: pada **Skill aktif** cari nama skill lalu tekan untuk menempelkannya sebagai chip (bisa banyak; nama manual juga bisa; label sumber `lokal`/`skills.sh`/`claude` ditampilkan). Saklar utama **Skill kursi** di bagian paling atas Pengaturan mengaktifkan/menonaktifkan semua skill sekaligus (`settings.skillsEnabled`, bawaan aktif). Badan skill disisipkan ke system prompt kursi setiap kali menjawab (maks 6.000 karakter per skill, 12.000 total; daftar dipindai ulang maks tiap 30 detik). Daftar skill yang tersedia: `GET /api/skills`. Contoh ada di `skills/contoh/SKILL.md`.

### Tool kustom

Selain tool bawaan, tambahkan tool sendiri lewat definisi JSON di `tools/<nama>.json` (dimuat ulang otomatis, tanpa restart):

```json
{
  "name": "cuaca",
  "description": "Cuaca saat ini sebuah kota (wttr.in)",
  "argHint": "nama kota, contoh: Pontianak",
  "type": "http_get",
  "url": "https://wttr.in/{{kota}}?format=3&m",
  "params": ["kota"]
}
```

Tipe yang tersedia: `http_get` (GET ke URL template; `{{param}}` diganti dari baris argumen berurutan, `{{arg}}` = seluruh argumen; melewati penjaga SSRF; field opsional `userAgent` dan `headers` template), `http_post` (POST JSON dari template body — **persetujuan pemilik**), `shell` (perintah template dijalankan di sandbox yang sama dengan `run_shell` — selalu memerlukan persetujuan pemilik dan toggle tool coding aktif), `javascript` (field `code` dijalankan via sandbox node, argumen JSON via stdin, stdout = hasil), dan `pipeline` (field `steps`, maks 5 tool berantai, tanpa pipeline bersarang). Parameter boleh berupa string (legacy) atau objek `{name, type: string|number|bool, required, hint}` yang divalidasi sebelum jalan. Nama tool harus huruf kecil/angka/underscore dan tidak boleh menabrak tool bawaan. Daftar tool kustom: `GET /api/tools`. Contoh siap pakai ada di `tools/cuaca.json`.

### Server MCP (Model Context Protocol)

Kursi dapat memakai tool dari server MCP eksternal (transport stdio). Kelola server lewat Pengaturan → **Server MCP** (maks 5 server: nama, perintah, argumen satu per baris, env `KUNCI=nilai` per baris) atau API `GET/POST /api/mcp-servers` + `POST /api/mcp-servers/{name}/delete`. Nilai env tidak pernah ditampilkan kembali (hanya nama kuncinya) dan dikosongkan di file backup, sama seperti API key provider.

Setiap panggilan menjalankan proses server sekali (initialize → tools/list atau tools/call → proses dimatikan; timeout daftar 15 dtk, panggilan 60 dtk) agar sederhana dan aman — tidak ada proses server yang dibiarkan hidup. Daftar tool di-cache 60 detik dan di-refresh di latar belakang, jadi tool server yang baru ditambahkan bisa butuh sampai ±60 detik untuk muncul (jalur panggilan tool selalu mengambil daftar segar bila cache kosong). Tool terekspos ke kursi sebagai `mcp_<server>_<tool>` (maks 50 tool per server, nama disanitasi), muncul di `GET /api/tools` dengan sumber `mcp`, tercatat di log audit, dan **setiap panggilan memerlukan persetujuan pemilik** (kind `mcp`) karena tool eksternal dapat melakukan apa saja. Argumen tool = JSON objek argumen tool; teks bebas dibungkus sebagai `{"arg": …}`.

Batasan jujur: hanya transport stdio (bukan HTTP/SSE), tanpa sampling/roots MCP, dan server MCP tidak terisolasi sandbox — perintah server berjalan sebagai user yang menjalankan aplikasi, jadi hanya pasang server yang kamu percaya.

## Statistik pemakaian

`GET /api/stats` mengembalikan pemakaian per kursi dan per model (jumlah request, sukses/gagal, latensi, token) untuk chat dan gambar. Bila harga provider diisi (per 1M token input/output), statistik menyertakan **estimasi biaya** (`estimatedCostUsd`) per model. Tabel statistik tampil di Pengaturan.

## Pendalaman fitur

Selain fitur inti di atas, aplikasi punya lapisan pendalaman (semua dapat diatur dari Pengaturan):

- **Keamanan**: PIN aplikasi (hash + sesi cookie HMAC 7 hari; endpoint pekerja Muse memakai token pekerja terpisah; `/api/pin` menerbitkan cookie sesi seperti login sehingga sesi tidak langsung terkunci sesudah PIN dipasang), audit login (maks 200, FIFO) + rate limit PIN (5 gagal/IP/5 menit → 429 selama 5 menit), log audit tool (maks 500, FIFO — argumen `env_file set` di-redact).
- **Kursi**: persona preset (6 bawaan: reviewer ketat, penulis santai, analis data, programmer, penerjemah, tutor sabar), kloning kursi, memori otomatis dari ringkasan ruang, budget harian per kursi (kursi dilewati + catatan sekali per job bila habis), model per jenis tugas (chat/tools/summary), urutan bicara per ruang.
- **Gateway**: circuit breaker per provider (5 gagal berurutan → buka 60 detik → half-open), retry otomatis 429/5xx (maks 2×, backoff 1 dtk/3 dtk, `Retry-After` dihormati maks 10 dtk), cache respons (TTL 10 menit, non-streaming tanpa tool), health check provider berkala (5 menit) + uji latency berperingkat, load balancing antar provider bermodel sama, tool-calling native OpenAI (fallback protokol teks).
- **Percakapan**: edit pesan + regenerasi dari pesan pemilik, branching dalam ruang (cabang aktif dipilih via chip), mode paralel (kursi menjawab bersamaan, integrator terakhir), tag pesan (maks 5) + filter pencarian, jadwal berulang mini-cron (5 field), balasan berantai antar kursi, mode debat (pro/kontra/rebuttal/kesimpulan), fork pesan ke ruang baru, ekspor satu diskusi, statistik reaksi emoji.
- **Ruang**: ikon + warna aksen per ruang, arsip ruang (tersembunyi dari baris ruang + pencarian), izin tool per ruang.
- **Tool kustom lanjutan**: tipe `http_post` (berpersetujuan), parameter ber-tipe (string/number/bool + wajib), header auth template `{{param}}` (tidak masuk log audit), tipe `javascript` (sandbox node, argumen JSON via stdin), tipe `pipeline` (maks 5 langkah berantai).
- **Skill**: pasang skill dari URL SKILL.md atau repo git (`POST /api/skills/install`), statistik pemakaian skill.
- **Integrasi**: webhook keluar `message.completed`, API eksternal ber-token (`POST /api/external/message`, bebas gerbang PIN), jembatan Telegram (long-poll; butuh token bot + chat ID dari pemilik), auto-update dari rilis GitHub (`/api/update/check` + `/api/update/apply` dengan `.bak`).
- **Pencarian**: indeks full-text in-memory (AND multi-kata + fallback substring).
- **Penyimpanan**: backend SQLite opsional (kv-blob `data.db`, migrasi otomatis dari `data.json` yang dipertahankan sebagai cadangan) via env `MUSEHOOD_STORAGE=sqlite|json`; tanpa env, SQLite hanya dipakai bila `data.db` sudah ada — produksi bawaan tetap JSON.
- **UI**: shortcut keyboard (Ctrl/Cmd+K cari, Ctrl/Cmd+Enter kirim, Alt+1–9 ruang, `?` bantuan), mode kompak.

### Pendalaman fitur gelombang 3 (v1.9.0)

- **Ruang multi-pengguna**: tiap ruang punya daftar peserta (`room.Participants`); pesan mencatat pengirimnya (`message.Author`) dan composer menyediakan pemilih pengirim. API: `POST /api/rooms/{id}/participants` (+ `/remove`).
- **Preset peran kursi**: 6 preset peran siap pakai (`seat.RolePreset`, `GET /api/roles`) yang mengisi peran + prompt kursi sekali klik.
- **Uji A/B model**: satu prompt dijalankan ke dua model/kursi berdampingan (`POST /api/abtest`), pemilik memilih pemenang (`/pick`); hasil tercatat di statistik.
- **Rantai prompt**: pipeline langkah bernama yang dapat dipakai ulang (CRUD `/api/chains` + `/run`, maks 20 rantai, 6 langkah per rantai); tiap langkah adalah pesan pemilik/kursi berurutan.
- **Koreksi otomatis balasan**: bila diaktifkan (`settings.AutoReview` + `settings.ReviewerSeat`), balasan kursi ditinjau kursi peninjau sebelum tampil; teks asli tersimpan (`message.OriginalText`).
- **Lampiran dokumen (RAG)**: unggah dokumen per ruang (multipart, maks 5 MB; PDF diekstrak via `pdftotext` bila tersedia), dipecah menjadi chunk 1200/200 karakter, dan 3 chunk paling relevan disisipkan ke prompt kursi. API: `POST/GET/DELETE /api/rooms/{id}/documents`.
- **Ringkasan harian terjadwal**: ringkasan ruang dikirim tiap hari pada jam yang diatur (`DailySummaryEnabled/Time/Channel`, zona WIB) ke inbox aplikasi, webhook, atau Telegram.
- **Offline PWA penuh**: service worker `musehood-v2` menyimpan snapshot `/api/state`; pesan yang ditulis saat offline masuk antrean lokal dan terkirim otomatis saat koneksi kembali.
- **Ekspor Markdown**: `GET /api/export?format=markdown` menghasilkan Markdown ber-frontmatter YAML yang siap dibuka di Notion/Obsidian.
- **Rating balasan**: pemilik memberi 👍/👎 pada balasan kursi (`POST /api/message/{id}/rate`); statistik per kursi/model tampil di Pengaturan.
- **Deteksi Ollama lokal**: aplikasi mendeteksi server Ollama di localhost/LAN (`GET /api/ollama/detect`) dan dapat menambahkannya sebagai provider sekali klik (`POST /api/ollama/add`).
- **Perintah /slash**: `/ringkas`, `/terjemah`, `/kode`, `/gambar`, `/bantu` di composer (terakhir dijawab lokal tanpa gateway).

Responsivitas tombol juga diperbaiki di v1.9.0: ganti tema kini optimistis (tampilan berubah seketika, penyimpanan menyusul — terukur 5–23 ms, sebelumnya 1,2–1,9 detik), dialog yang dimuat malas (Pengaturan + 7 dialog) menampilkan spinner "Memuat…" bila butuh >150 ms dan chunk-nya di-prefetch saat aplikasi idle, aksi rating/pin/reaksi/hapus bersifat optimistis, dan ganti ruang menampilkan indikator "Memuat ruang…" bila lambat. Code splitting membuat muatan awal turun menjadi **229 KB** (JS+CSS yang benar-benar diunduh); panel Pengaturan (82 KB) baru diunduh saat pertama dibuka.

### Tool gaya OpenCode + omp (v1.10.0)

Rilis v1.10.0 (28 Nov 2026) menambahkan 10 tool gaya OpenCode (`ask_question`, `spawn_subagent`/`check_subagent`, Code Mode `execute`, worktree git, browser headless, server MCP stdio) dan 36 tool gaya omp (AST, LSP, kernel `eval` persisten, debugger DAP, browser CDP, GitHub, `security_scan`, hub, checkpoint/rewind, memori durable + `learn`, todo, workflow `/review`/commit/goal/plan/vibe/autoresearch, katalog on-demand + kebijakan per tool) — total **111 tool bawaan**, dijelaskan di bagian Tool di atas. Batas jujur: snapshot browser adalah teks DOM ringkas (bukan pohon aksesibilitas penuh) dan tidak ada Lighthouse; LSP/debug nyata untuk Go (`gopls`/`dlv`), bahasa lain butuh language server/debug adapter terpasang + diatur di `settings.lspServers`; vibe mode adalah flag + instruksi direktur untuk kursi integrator, bukan runtime pekerja persisten baru; server MCP hanya transport stdio dan berjalan tanpa sandbox sebagai user aplikasi. Binary eksternal yang dipakai beberapa tool (`ast-grep`, `gopls`, `dlv`, `govulncheck`, `tesseract`, `espeak-ng`, `pdftotext`, Chromium) **tidak ikut dalam binary rilis** — pasang di mesin yang menjalankan aplikasi; tanpa binary itu tool terkait memberi pesan instalasi bahasa Indonesia dan tidak mengarang hasil.

## PWA, tema, dan bahasa

- **PWA**: aplikasi dapat di-install (manifest + ikon 192/512 + service worker `musehood-v2`). Service worker meng-cache shell aplikasi, menyimpan snapshot `/api/state` untuk mode offline, dan memakai network-first untuk `/api/` sehingga data selalu segar; pesan yang ditulis saat offline masuk antrean lokal dan terkirim saat koneksi kembali.
- **Tema**: Terang / Gelap / **Otomatis** (mengikuti sistem) dan ukuran font Kecil/Sedang/Besar — diatur di Pengaturan → Diskusi, tersimpan sebagai `settings.theme`/`settings.fontSize`.
- **Bahasa antarmuka**: Indonesia / English untuk chrome utama (composer, baris ruang, pencarian, dialog jadwal, label pengaturan); balasan kursi tidak diterjemahkan. Pilihan tersimpan di browser.
- **Aksesibilitas**: menghormati `prefers-reduced-motion` dan mengumumkan balasan kursi baru lewat region `aria-live` untuk pembaca layar.

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
| POST | `/api/message/{id}/regenerate` | Jalankan ulang balasan kursi (202 `{jobId}`; pesan pemilik: 403) |
| GET/POST | `/api/scheduled` | Daftar pesan terjadwal; buat jadwal (sekali/harian/mingguan/interval) |
| POST/DELETE | `/api/scheduled/{id}` | Ubah/hapus jadwal |
| GET/POST | `/api/templates` | Daftar template diskusi; buat template `{name,seats,instruction}` |
| POST/DELETE | `/api/templates/{id}` | Ubah/hapus template |
| POST | `/api/rooms/{id}/summarize` | Ringkas ruang (202; 409 bila ringkasan sedang berjalan) |
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
| POST | `/api/questions/{id}` | `{answer}` untuk menjawab pertanyaan kursi (`ask_question`) |
| GET/POST | `/api/mcp-servers` | Daftar server MCP (nilai env tidak ditampilkan) / tambah-ubah server |
| POST | `/api/mcp-servers/{name}/delete` | Hapus server MCP |
| POST | `/api/vote` | Voting jawaban terbaik diskusi terakhir |
| GET | `/api/stats` | Statistik pemakaian per kursi/model |
| GET | `/api/search?q=…` | Pencarian lintas ruang; filter `seat`, `room`, `from`, `to`, `hasImage=1` |
| GET | `/api/skills` | Daftar skill yang tersedia (nama, deskripsi, sumber: lokal/skills.sh/claude) |
| GET | `/api/tools` | `{tools, builtin, mcp}` — tool kustom dari folder `tools/`, daftar tool bawaan, dan tool MCP terekspos |
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
| POST | `/api/login` / `/api/logout` / `/api/pin` | Login PIN, keluar, atur/ubah PIN |
| GET | `/api/personas` | Daftar persona preset kursi |
| POST | `/api/seats/{code}/persona` / `/clone` | Terapkan persona; kloning kursi |
| POST | `/api/rooms/{id}/archive` | Arsipkan/pulihkan ruang |
| POST | `/api/message/{id}/rerun` / `/fork` / `/branch` / `/tags` | Ulangi dari pesan pemilik; fork ke ruang baru; cabang; tag pesan |
| POST | `/api/message/{id}/rate` | Beri rating 👍/👎 pada balasan kursi |
| POST/DELETE | `/api/rooms/{id}/participants` (+ `/remove`) | Kelola peserta ruang multi-pengguna |
| POST/GET/DELETE | `/api/rooms/{id}/documents` | Unggah/daftar/hapus dokumen RAG per ruang (multipart, maks 5 MB) |
| GET | `/api/roles` | Daftar preset peran kursi |
| POST | `/api/abtest` (+ `/pick`) | Uji A/B dua model/kursi; pilih pemenang |
| GET/POST/DELETE | `/api/chains` (+ `/run`) | Rantai prompt: daftar/buat/hapus; jalankan rantai |
| GET/POST | `/api/ollama/detect` / `/api/ollama/add` | Deteksi server Ollama lokal/LAN; tambahkan sebagai provider |
| POST | `/api/debate` | Debat dua kursi (pro/kontra/rebuttal/kesimpulan) |
| GET | `/api/export?message=…` | Ekspor satu diskusi (HTML/JSON) |
| GET | `/api/tool-log` | Log audit pemakaian tool |
| POST | `/api/skills/install` | Pasang skill dari URL SKILL.md atau repo git |
| POST | `/api/providers/{id}/health` / `/latency-test` | Cek kesehatan provider; uji latency berperingkat |
| POST | `/api/external/message` | Pesan masuk via API eksternal (header `Authorization: Bearer <token>`) |
| POST | `/api/external-token/regenerate` | Buat ulang token API eksternal (tampil sekali) |
| GET/POST | `/api/update/check` / `/api/update/apply` | Cek rilis GitHub terbaru; terapkan update (dengan `.bak`) |
| GET | `/exports/<nama>.zip` | Unduh hasil `export_project` |

Browser mengambil hasil job dari `/api/state`. Semua error API memakai `{error}`. Pengaturan mendukung `owner.name`, `group.name`, `seats[].{code,name,role,prompt,provider,model,fallbackModel,fallbackModels,disabledTools,memory,skills}`, dan `settings.{baseUrl,apiKey,clearApiKey,roundRobin,reviewLevel,demoFallback,integratorSeat,toolsEnabled,codeToolEnabled,skillsEnabled,streamingEnabled,theme,fontSize}`; provider mendukung `priceInputPer1M`/`priceOutputPer1M`. `POST /api/message` menerima `templateId` opsional. Avatar dapat diunggah untuk `group`, `owner`, atau kode kursi.

## Penyimpanan

`DATA_DIR` memakai direktori kerja sebagai default. Isinya `data.json` dengan skema Musehood versi 1 (atau `data.db` bila backend SQLite diaktifkan via env `MUSEHOOD_STORAGE=sqlite` — migrasi otomatis dari `data.json`, JSON dipertahankan sebagai cadangan), folder `queue/`, `avatars/` untuk unggahan, `images/` untuk gambar, `uploads/` untuk lampiran file, `sandbox/` sebagai folder kerja tool kursi, `projects/` untuk proyek coding bernama, `exports/` untuk zip hasil ekspor proyek, `snapshots/` untuk snapshot proyek, `skills/` untuk skill lokal per kursi, dan `tools/` untuk definisi tool kustom JSON. `data.json` ditulis atomik dengan izin `0600` dan memuat key gateway konfigurasi. Untuk memindahkan data Node, salin `data.json`, `queue/`, isi unggahan dari `public/avatars/`, serta `public/images/` ke struktur runtime Go. Jalankan satu proses per `DATA_DIR`.

## Performa

Terukur di VM pengembangan (RAM tersedia ~400 MB, GOMAXPROCS=2):

- `/api/state` lokal: median **2 ms** per request; proses server memakai **~10 MB** RAM; binary ~21 MB (termasuk driver SQLite pure-Go).
- Muatan awal frontend (JS+CSS yang benar-benar diunduh): **229 KB** sesudah code splitting (panel Pengaturan 82 KB + 7 dialog dimuat malas saat pertama dibuka, lalu di-prefetch saat idle).
- Ganti tema: **5–23 ms** (optimistis, tanpa render ulang daftar pesan); render awal 1000 pesan ±2,9 detik; rating optimistis 12–86 ms klik→aktif.
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
(cd frontend && npm run test:unit && npm run test:component)
node tests/frontend-e2e.mjs
node tests/frontend-e2e-settings.mjs
node tests/frontend-e2e-actions.mjs
node tests/frontend-a11y.mjs
node tests/frontend-visual.mjs        # --update untuk memperbarui baseline
node tests/frontend-perf.mjs
```

`tests/live-e2e.mjs` menjalankan binary asli terhadap gateway mock dan menguji 49 skenario end-to-end. Uji Chromium menggunakan instalasi Playwright lokal. Isi `PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH` bila menggunakan executable Chromium dari lokasi lain. Lihat hasil dan cakupan pengujian di [TESTS.md](TESTS.md).

Suite backend (`internal/musehood/*_test.go` + `main_test.go`, 338 fungsi tes + 12 benchmark) mencakup: unit (parsing, tool, gambar, ruang), API/endpoint + kontrak respons (status, bentuk JSON, redaksi kredensial, validasi), database/penyimpanan (roundtrip `data.json`, data rusak, migrasi, konkurensi, restore termasuk penolakan file >32MB), keamanan (matriks SSRF, traversal sandbox, zip-slip, pola kode berbahaya, gerbang persetujuan), integrasi/E2E layanan (injeksi skill, fallback model + rantai fallback, persetujuan via API, tool kustom, voting, isolasi ruang, streaming SSE + fallback, regenerate, pesan terjadwal + jadwal berulang, template diskusi, ringkasan + kompaksi konteks, pencarian berfilter, sematan ruang + tema), jalur sukses tool jaringan (`fetch_url`, `web_search`, `http_get` kustom, terjemahan, kurs, QR, RSS, transkrip YouTube, `http_post` berpersetujuan — resolver DNS di-stub ke IP publik dan URL API diganti server lokal dalam tes, penjaga SSRF produksi tidak dilemahkan), pengetesan berat (konkurensi 100 POST bersamaan, fuzz 31 endpoint × 23 payload, 5000 pesan, semua 111 tool bawaan dengan argumen valid + invalid, SQLite 1000 pesan), tool gaya OpenCode (alur tanya-jawab + timeout, spawn/check subagent + penolakan kedalaman 2, Code Mode RPC + batas panggilan, worktree git nyata, degradasi + render Chromium nyata, server MCP palsu stdio + persetujuan + redaksi env), tool gaya omp (ast-grep nyata + pratinjau/apply, LSP gopls nyata + server LSP palsu, kernel eval persisten, DAP delve nyata + server DAP palsu, CDP Chromium nyata, API GitHub palsu lokal, pemindaian keamanan, hub/checkpoint/todo/memori, kebijakan deny/prompt, katalog on-demand) dan **skenario agen gabungan** (8 alur multi-tool lewat dispatch penuh: coding, riset, orkestrasi, Code Mode, plan mode, MCP dalam rantai, eval persisten, kebijakan), dan performa/beban/stres (300 request konkuren, 24 pesan konkuren, state 3.000 pesan, throughput `run_code` — dilewati pada `go test -short`).

Suite frontend mencakup: unit (76 tes: `frontend/tests/unit/`, helper format/API, parser Markdown, lazy-load syntax highlighting + composable `chat.svelte.ts` yang dikompilasi dan dijalankan di Node dengan fetch/document stub, via `node --test`), komponen (43 tes: `frontend/tests/component/`, render SSR komponen Svelte), E2E UI (`tests/frontend-e2e.mjs` 21 + `frontend-e2e-depth.mjs` 23 + `frontend-e2e-depth-settings.mjs` 10: chat, mention + modal info kursi, render Markdown + syntax highlighting, pengaturan, pin, ruang, pencarian, voting, persetujuan tool, alur PIN, debat, fork, rerun, reaksi, arsip, ekspor, urutan kursi, shortcut; `tests/frontend-e2e-settings.mjs` 20: CRUD provider + fetch/test models, toggle, kursi custom, kelola ruang, preset, statistik, backup/restore, ekspor; `tests/frontend-e2e-actions.mjs` 10: edit/hapus pesan, reaksi, balas/kutip, unggah gambar & file), aksesibilitas (`tests/frontend-a11y.mjs` 20 + `frontend-a11y-depth.mjs` 18: audit axe-core mode terang + gelap, nama aksesibel, alt, keyboard/fokus, kontras WCAG, target sentuh), visual regression (`tests/frontend-visual.mjs`: screenshot 12 skenario vs baseline piksel di `tests/visual-baselines/`), cross-browser/performance (`tests/frontend-perf.mjs` 33: viewport 320–1920px, emulasi sentuh iPhone, anggaran timing/bundle/polling — Chromium saja karena unduhan browser lain diblokir di VM pengembangan), pengetesan berat UI (`frontend-e2e-heavy.mjs` 26 + `frontend-heavy-stress.mjs` 16 + `frontend-a11y-heavy.mjs` 16 + `frontend-visual-heavy.mjs` 8 baseline), dan performa tombol (`tests/frontend-buttons-perf.mjs` 10: tema <300 ms tanpa longtask, rating optimistis, spinner dialog malas, prefetch tanpa fetch tool-log, spinner ganti ruang).
