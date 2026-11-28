# Panduan Penggunaan Musehood Go

Panduan praktis memakai Musehood Go: grup chat lokal untuk Arman dan tujuh kursi AI (Muse, Opus, Plamo, GLM, Argon, Deepseek, Fable). Rilis terakhir: **v1.11.0** (28 Nov 2026) — [GitHub Releases](https://github.com/afman42/musehood-go/releases/tag/v1.11.0). Lihat juga [README.md](README.md) untuk arsitektur dan [TESTS.md](TESTS.md) untuk cakupan pengujian.

## 1. Cara dasar

Kamu tidak perlu memanggil tool satu per satu. Alurnya selalu sama:

1. **Sebut kursi** dengan `@Nama` atau kode, misalnya `@Plamo` atau `@M01`. Gunakan tombol **@ Kursi** atau tekan avatar kursi untuk menyisipkan mention. Beberapa mention ditangani sesuai urutan kursi.
2. **Beri tugas** dalam bahasa biasa. Kursi gateway memilih sendiri tool yang dipakai (maksimal 3 putaran tool per balasan) dan menjalankannya di server.
3. **Pantau aktivitas**: selama tool berjalan, status mengetik menampilkan aktivitasnya, misalnya "Plamo menjalankan web_search…". Hasil tiap tool tampil sebagai **kartu tool** di bawah balasan — klik untuk melihat argumen dan hasil lengkapnya.

Pola percakapan lain yang berguna:

- **Chat biasa** mengikuti urutan kursi saat **Diskusi Bergilir** aktif; saat mati, pesan menuju kursi integrator saja (bawaan Muse, bisa diganti di Pengaturan).
- `Tugas: …` menjalankan pipeline Opus → Plamo → Deepseek → Argon (gambar) → GLM → integrator dengan label **Hasil Akhir - Integrasi**. **Tanya Progres** menanyakan status tugas ke integrator.
- `Gambar: …` membuat gambar lewat gateway gambar.
- **Perintah /slash** di composer: `/ringkas`, `/terjemah`, `/kode`, `/gambar`, `/bantu`.
- Balasan kursi dirender sebagai Markdown (termasuk syntax highlighting blok kode), di-streaming bertahap, dan dapat diberi rating 👍/👎, disematkan (pin), diedit, atau dijalankan ulang (regenerate ↻).

**Pertanyaan dari kursi**: kursi dapat berhenti dan bertanya kepadamu lewat tool `ask_question` — muncul banner pertanyaan dengan pilihan jawaban (maks 6) plus kolom jawaban bebas. Pekerjaan kursi menunggu jawabanmu (timeout 180 detik, lalu kursi lanjut dengan catatan "tidak menjawab").

## 2. Pengaturan yang perlu diisi dulu

Semua ada di **Pengaturan grup** (ikon gir). Panel Pengaturan dimuat saat pertama dibuka; spinner "Memuat…" muncul bila butuh >150 ms.

| Pengaturan | Di mana | Keterangan |
| --- | --- | --- |
| **Provider gateway + model kursi** | Pengaturan → Provider Gateway | Tambah provider (Base URL + API key + protokol), klik **Fetch Models** dan **Tes Model**, lalu isi model tiap kursi. Model boleh ditulis `alias/model` untuk mengarahkan kursi ke provider beralias itu. Kursi dapat punya rantai fallback (1 utama + maks 5 cadangan). |
| **Token GitHub** | Pengaturan → panel Tool & memori (`settings.githubToken`) | Dibutuhkan agar tool `github_push` bisa push dan tool baca GitHub tidak kena rate limit publik. Tanpa token, tool GitHub hanya membaca data publik. Token dikosongkan di file backup dan tidak pernah ditampilkan kembali. |
| **Server LSP per bahasa** | Pengaturan → panel Tool & memori (`settings.lspServers`) | Go sudah siap dengan `gopls` (bawaan). Bahasa lain: isi perintah language server-nya, mis. `python` → `pylsp`, `typescript` → `typescript-language-server`, `rust` → `rust-analyzer`. Tanpa server terpasang, tool LSP memberi pesan instalasi yang jelas. |
| **Server MCP** | Pengaturan → Server MCP | Maks **5 server** (nama, perintah, argumen satu per baris, env `KUNCI=nilai` per baris). Tool-nya muncul sebagai `mcp_<server>_<tool>` (maks 50 per server) dan **setiap panggilan butuh persetujuanmu**. Nilai env tidak pernah ditampilkan kembali dan dikosongkan di backup. Daftar tool di-cache 60 detik, jadi server baru bisa butuh ±60 detik untuk muncul. Hanya transport stdio; server berjalan sebagai user aplikasi (tanpa sandbox) — hanya pasang server yang kamu percaya. |
| **Kebijakan per tool** | Pengaturan → panel Tool & memori (`settings.toolPolicies`) | Tiap tool dapat diset `allow` (langsung jalan), `prompt` (minta persetujuanmu, kind `tool`), atau `deny` (ditolak dengan pesan jelas). Ditegakkan di dispatch, jadi berlaku juga untuk panggilan dari Code Mode `execute`. Selain itu tiap kursi dan tiap ruang dapat menonaktifkan tool tertentu (allowlist). |
| **Katalog on-demand** | Pengaturan → panel Tool & memori (`settings.onDemandTools`) | Bila aktif, system prompt kursi hanya memuat deskripsi penuh tool inti + daftar nama tool lain; kursi memakai tool `tool_info` untuk membaca deskripsi tool yang dibutuhkan. Hemat token untuk katalog 111 tool. |
| **Memori durable** | Pengaturan → panel Tool & memori (`settings.memoryEnabled`) | Toggle memori bersama antar-kursi (`memory_recall`/`memory_retain`/`memory_reflect`/`memory_edit`, maks 500 entri). Daftar memori dapat dilihat dan dihapus dari panel yang sama. |
| **Tool untuk kursi gateway** | Pengaturan (saklar utama) | Mengendalikan semua tool kursi (bawaan aktif). Toggle terpisah **Tool coding (run_code & run_shell)** mengendalikan eksekusi kode/shell di sandbox. |
| **Skill per kursi** | Pengaturan → Skill aktif | Pilih skill (`SKILL.md`) dari folder lokal, `~/.agents/skills/` (skills.sh), atau `~/.claude/skills/`; badan skill disisipkan ke system prompt kursi. Saklar utama **Skill kursi** mengaktifkan/menonaktifkan semuanya. |

## 3. Panel kerja ruang

Panel kerja di dalam ruang mengatur cara kursi bekerja di ruang itu:

- **Goal mode** (`room.goal`): tulis tujuan tetap ruang; tujuan disuntikkan ke system prompt semua kursi di setiap balasan dan tampil di UI. Kosongkan untuk menonaktifkan.
- **Plan mode** (`room.planMode`): selama aktif, hanya tool kelas **baca** yang boleh jalan — tool tulis/eksekusi ditolak di dispatch. Kursi menyusun rencana dulu; kamu menyetujui rencana untuk keluar dari plan mode (API/UI).
- **Vibe mode** (`room.vibeMode`): kursi integrator bertindak sebagai direktur yang membagi kerja ke kursi lain memakai `spawn_subagent` dan pesan hub. Batas jujur: ini flag + instruksi direktur, bukan runtime pekerja persisten baru.
- **Todo**: checklist per ruang (status `pending`/`active`/`done`/`blocked`, maks 100). Kamu dapat mengelola dari panel; kursi juga dapat menambah/mengubahnya lewat tool `todo_add`/`todo_update`/`todo_list`.
- **Checkpoint / rewind**: tombol checkpoint menandai titik di ruang (maks 50 per ruang); **rewind** memotong semua pesan sesudah checkpoint itu. Rewind dari UI/API adalah konfirmasimu sendiri; rewind lewat tool kursi butuh persetujuanmu.

Mode ruang lain yang sudah ada: ikon + warna aksen per ruang, arsip ruang, urutan bicara per ruang, daftar kursi per ruang, dan izin tool per ruang (digabung dengan izin per kursi).

## 4. Persetujuan pemilik

Tindakan berisiko tidak langsung jalan. Muncul **banner Setujui / Tolak** di chat; bila tidak dijawab dalam **180 detik**, otomatis ditolak. API-nya `POST /api/approvals/{id}` dengan `{approved: true|false}`; permintaan yang menunggu tampil sebagai `pendingApprovals` di `/api/state`.

Aksi yang meminta persetujuan:

| Aksi | Kind | Catatan |
| --- | --- | --- |
| `run_code` / `eval` dengan pola berbahaya terdeteksi (`os.system`, subprocess, akses jaringan, baca path absolut, `rm -rf`, loop tak berujung, dll.) | `code` | Deteksi berbasis pola, bukan jaminan. Kode yang tidak terdeteksi pola berjalan langsung di sandbox. |
| Semua `run_shell` | `shell` | Selalu persetujuan, di sandbox yang sama dengan `run_code`. |
| `delete_file`, `worktree_remove` dengan `force`, restore snapshot proyek | `delete` | |
| `http_post` (tool bawaan maupun tool kustom tipe `http_post`) | `http` | URL + isi JSON ditampilkan pada banner. |
| `git_commit`, `github_push`, `git_stash pop`, `git_branch checkout` saat working tree kotor, impor proyek yang menimpa proyek lama | `git` | `github_push` memakai token dari Pengaturan (via env git, tidak pernah di argv/URL). |
| `ast_edit` dengan argumen `apply`, `lsp_rename` | `edit` | `ast_edit` tanpa `apply` hanya pratinjau diff; sesudah disetujui, snapshot proyek dibuat otomatis sebelum perubahan. |
| Semua panggilan tool MCP (`mcp_<server>_<tool>`) | `mcp` | Tool eksternal dapat melakukan apa saja; server tidak ter-sandbox. |
| Tool dengan kebijakan `prompt` | `tool` | Dari `settings.toolPolicies`. |
| `rewind` lewat tool kursi | — | Rewind dari UI/API tidak butuh banner (itu konfirmasimu sendiri). |
| Tool kustom tipe `shell` dan `javascript` | `shell`/`code` | Mengikuti aturan sandbox yang sama. |

Sandbox kode: `run_code`, `run_shell`, `eval`, dan `execute` berjalan di bubblewrap sebagai user `nobody` — filesystem di luar folder `sandbox/` tidak terlihat, tanpa jaringan, batas CPU/memori/proses via cgroup, timeout 10 detik untuk `run_code`/`run_shell` (30 detik untuk `eval`/`execute`), output dibatasi 4.000 karakter.

## 5. Katalog tool (111 tool bawaan)

Semua tool tunduk pada saklar tool utama, allowlist per kursi dan per ruang, serta kebijakan per tool. Tool proyek bekerja pada proyek bernama di `projects/<nama>/` — awali argumen dengan baris `project:<nama>` (tanpa itu dipakai proyek `default`); path selalu relatif dan tidak boleh keluar dari folder proyek.

### Coding & proyek

| Tool | Kegunaan | Contoh argumen |
| --- | --- | --- |
| `create_project` | Buat proyek baru dari template `go`/`node`/`python`/`web` | `latihan go` |
| `list_tree` | Pohon folder proyek (kedalaman maks 6, 500 entri) | `project:latihan` |
| `read_file_lines` | Baca rentang baris file (1-based, maks 500 baris) | `main.go 1 40` |
| `move_file` / `copy_file` / `mkdir` | Pindah/salin file (tidak menimpa), buat folder bersarang | `a.txt b.txt` |
| `search_code` | Grep regex RE2 lintas proyek dengan 2 baris konteks (maks 100 hasil) | `func Tambah` |
| `find_files` | Cari file dengan glob termasuk `**` (maks 200 hasil) | `**/*_test.go` |
| `apply_patch` | Terapkan unified diff format git secara atomik (snapshot otomatis dulu) | isi diff |
| `edit_many` | Banyak edit `<<<OLD>>>`/`<<<NEW>>>` per file dalam satu panggilan (atomik per file, snapshot otomatis) | blok edit per file |
| `project_snapshot` | Snapshot folder proyek: `create [label]`/`list`/`restore <id>`/`delete <id>` (restore = persetujuan) | `create sebelum-refactor` |
| `run_build` | Build proyek di sandbox tanpa jaringan (timeout 120 dtk) | `project:latihan` |
| `run_tests` | Jalankan tes proyek di sandbox (timeout 180 dtk) | `project:latihan` |
| `run_lint` | Lint proyek (go vet + gofmt -l; py_compile + ruff bila ada; eslint bila terpasang) | `project:latihan` |
| `format_code` | Format kode proyek (gofmt; black/prettier bila tersedia) | `project:latihan` |
| `run_server` | Dev server proyek sebagai proses latar di 127.0.0.1 (port 4100–4199, satu per proyek) | `start` / `status` / `stop` |
| `allowlist_shell` | Perintah aman tanpa persetujuan (ls/cat/grep/find tanpa -exec, versi tool, git baca-saja); metakarakter shell ditolak | `ls -la` |
| `outline` | Daftar fungsi/kelas per file (Go/Python/JS/TS) dengan nomor baris | `project:latihan` |
| `find_references` | Semua pemakaian simbol (word-boundary) lintas proyek (maks 100) | `Tambah` |
| `dependency_graph` | Peta import proyek (internal ditandai, eksternal diringkas) | `project:latihan` |
| `todo_scan` | TODO/FIXME/HACK/XXX dengan file:baris (maks 100) | `project:latihan` |
| `complexity_report` | LOC per file, 10 file terbesar, fungsi terpanjang | `project:latihan` |
| `generate_tests` | File tes kerangka dari file sumber (Go `_test.go`, Python `test_*.py`, JS `*.test.js`) | `main.go` |
| `export_project` | Zip proyek ke `exports/` + URL unduhan `/exports/<nama>.zip` | `project:latihan` |
| `import_project` | Impor zip dari `uploads/` menjadi proyek (penjaga zip-slip; menimpa = persetujuan) | `latihan.zip` |
| `env_file` | Kelola `.env` proyek: `list`/`get` (nilai ter-mask)/`set`/`delete`; nilai tidak masuk log audit | `set KUNCI=nilai` |
| `list_files` / `read_file` / `write_file` | Folder kerja bersama `sandbox/` (maks 100 KB per file) | `catatan.txt` |
| `append_file` / `edit_file` | Tambah teks ke akhir file; ganti sebagian isi file (`<<<OLD>>>`/`<<<NEW>>>`) | blok edit |
| `delete_file` | Hapus file di sandbox — **persetujuan pemilik** | `catatan.txt` |
| `download_file` | Unduh URL ke sandbox (maks 10 MB, penjaga SSRF) | URL + nama file |
| `zip_files` / `unzip_file` | Kompres file sandbox ke zip (maks 50 MB); ekstrak zip rata anti zip-slip (maks 100 entri/50 MB) | daftar file / nama zip |
| `file_hash` | SHA-256 sebuah file | `catatan.txt` |
| `csv_json` | Konversi CSV ↔ JSON (baris pertama CSV = nama kolom) | isi CSV/JSON |

### Git & worktree

| Tool | Kegunaan | Contoh argumen |
| --- | --- | --- |
| `git` | Perintah git baca-saja di sandbox (status/diff/log/show/branch/remote) | `status` |
| `git_commit` | `git add -A` + commit di folder proyek — **persetujuan pemilik** | `project:latihan` + pesan |
| `git_branch` | Branch proyek: `list`/`create <nama>`/`checkout <nama>` (checkout kotor = persetujuan) | `create fitur-x` |
| `git_stash` | Stash proyek: `push`/`pop`/`list` (pop = persetujuan) | `push` |
| `worktree_create` | `git worktree add` di `projects/<proyek>-wt/<nama>`, branch `wt-<nama>` (proyek harus punya ≥1 commit) | `project:latihan` + nama |
| `worktree_list` | Daftar worktree proyek (path + branch) | `project:latihan` |
| `worktree_remove` | Hapus worktree proyek; `force` = persetujuan pemilik | nama worktree |

### Eksekusi kode

| Tool | Kegunaan | Contoh argumen |
| --- | --- | --- |
| `run_code` | Jalankan Python/JavaScript di sandbox terisolasi (timeout 10 dtk) | kode + bahasa |
| `run_shell` | Perintah shell di sandbox yang sama — **selalu persetujuan pemilik** | perintah |
| `eval` | Kernel persisten Python/JavaScript per (ruang+kursi+bahasa): state bertahan antar-panggilan, timeout 30 dtk/panggilan, maks 8 sesi, idle >10 menit dimatikan, argumen `reset` untuk sesi baru | `python` + kode |
| `execute` | Code Mode: JavaScript di sandbox dengan jembatan RPC — kode memanggil `await tool("nama", "argumen")` dan host menjalankan tool itu dengan cek izin/allowlist/audit/persetujuan yang sama (maks 20 panggilan, timeout 30 dtk; `execute` bersarang dilarang) | skrip JS |

### Browser

| Tool | Kegunaan | Contoh argumen |
| --- | --- | --- |
| `browser_open` | Render halaman dengan Chromium headless lalu ekstrak teks DOM hasil render (penjaga SSRF, timeout 45 dtk) | URL |
| `browser_screenshot` | Screenshot PNG halaman ter-render; gambar terlampir pada balasan | URL |
| `browser_links` | Daftar tautan dari DOM ter-render (maks 50, tanpa duplikat) | URL |
| `screenshot_url` | Screenshot halaman web (Chromium headless); gambar terlampir pada balasan | URL |
| `browser_click` | Klik elemen via CDP (selector CSS) | `#tombol-kirim` |
| `browser_type` | Isi elemen via CDP lalu tekan Enter | `#cari` + teks |
| `browser_snapshot` | Teks DOM ringkas halaman aktif (judul + teks terlihat) via CDP | — |
| `browser_tabs` | Daftar / buka (`new <url>`) / tutup (`close <id>`) tab CDP; satu browser per aplikasi, penjaga SSRF tetap berlaku | `new https://contoh.id` |

### Web & riset

| Tool | Kegunaan | Contoh argumen |
| --- | --- | --- |
| `web_search` | Pencarian web (DuckDuckGo), 5 hasil teratas | kata kunci |
| `fetch_url` | Ambil isi teks halaman web (alamat privat/localhost diblokir) | URL |
| `summarize_url` | Ambil isi halaman web untuk diringkas kursi | URL |
| `rss_feed` | Baca feed RSS 2.0 / Atom (item terbaru) | URL feed |
| `youtube_transcript` | Transkrip/caption video YouTube tanpa kunci API | URL video |
| `translate_text` | Terjemahan antar bahasa via MyMemory (baris pertama: pasangan `id>en`) | `id>en` + teks |
| `currency_convert` | Konversi mata uang, kurs ECB via frankfurter.app | `100 USD IDR` |
| `current_time` | Tanggal dan waktu sekarang (Asia/Jakarta) | — |

### GitHub

| Tool | Kegunaan | Contoh argumen |
| --- | --- | --- |
| `github_read` | Baca repo/file/issue/PR/diff via REST API (`repo`, `file`, `issue`, `pr`, `diff`); tanpa token hanya publik | `repo afman42/musehood-go` |
| `github_search` | Pencarian kode/issue GitHub via REST API | kata kunci |
| `github_actions` | Status run GitHub Actions sebuah repo | `afman42/musehood-go` |
| `github_push` | `git push` proyek lokal ke remote GitHub — **persetujuan pemilik**; token dari Pengaturan | `project:latihan` |

### LSP, debug, AST

| Tool | Kegunaan | Contoh argumen |
| --- | --- | --- |
| `ast_grep` | Cari pola sintaks (AST) di proyek via ast-grep (opsional `lang:<bahasa>`) | `project:latihan` + pola |
| `ast_edit` | Codemod struktural ast-grep: tanpa `apply` hanya pratinjau diff; `apply` mengubah file setelah **persetujuan** + snapshot otomatis | pola → pengganti |
| `lsp_definition` / `lsp_references` | Definisi/rujukan simbol via language server stdio; server per bahasa dari Pengaturan, bawaan Go → `gopls` | file + posisi |
| `lsp_diagnostics` | Diagnostik file dari language server (`publishDiagnostics`) | file |
| `lsp_rename` | Ganti nama simbol via LSP `rename` — menerapkan WorkspaceEdit setelah **persetujuan** + snapshot | file + posisi + nama baru |
| `debug_launch` | Launch program Go proyek di debugger DAP (`dlv dap`, satu sesi per ruang) | `project:latihan` |
| `debug_breakpoint` | Pasang breakpoint file:baris | `main.go:12` |
| `debug_step` | `continue` / `next` / `stepIn` / `stepOut` | `next` |
| `debug_inspect` | StackTrace + variabel sesi debug | — |
| `debug_stop` | Terminate sesi debug | — |

### Memori, learn & skill

| Tool | Kegunaan | Contoh argumen |
| --- | --- | --- |
| `memory_recall` | Cari memori durable bersama dengan kata kunci | kata kunci |
| `memory_retain` | Simpan memori durable (tag opsional) | teks + tag |
| `memory_reflect` | Kumpulkan memori terkait untuk disintesis kursi | topik |
| `memory_edit` | Ubah/hapus entri memori durable | id + teks baru |
| `memory_save` | Simpan catatan ke memori per kursi (maks 500 char/catatan, total 4000 char) | catatan |
| `learn` | Simpan pelajaran ke memori durable; opsional `skill:<nama>` menjadikannya skill terkelola di `skills/<nama>/SKILL.md` | pelajaran |
| `manage_skill` | Daftar/buat/ubah/pensiunkan skill lokal terkelola (skill skills.sh/claude hanya dihitung) | `list` / `create <nama>` |

### Todo, checkpoint, hub & subagen

| Tool | Kegunaan | Contoh argumen |
| --- | --- | --- |
| `todo_add` / `todo_update` / `todo_list` | Checklist per ruang (pending/active/done/blocked, maks 100) | teks todo / id + status |
| `checkpoint` / `rewind` | Tandai titik di ruang / potong pesan sesudah checkpoint (rewind via tool = **persetujuan**) | label / id checkpoint |
| `hub_message` | Kirim pesan ke kursi lain di ruang yang sama (tampil sebagai pesan sistem `[hub → …]`) | `@Nama` + pesan |
| `hub_processes` / `hub_stop` | Daftar / hentikan proses latar terpusat (dev server, kernel eval, sesi debug, browser CDP) | id proses |
| `spawn_subagent` | Mulai sesi anak di ruang baru "Sub: …" (baris pertama opsional `seat:<kode>`, lalu prompt); kursi di ruang sub tidak boleh spawn lagi (kedalaman maks 1); ringkasan anak diposting ke ruang induk | `seat:M01` + prompt |
| `check_subagent` | Baca balasan terakhir sesi anak (atau status masih berjalan) | id sesi anak |
| `ask_question` | Kursi bertanya ke pemilik (baris pertama = pertanyaan, baris berikutnya = pilihan maks 6); menunggu jawaban (timeout 180 dtk) | pertanyaan + pilihan |

### Keamanan

| Tool | Kegunaan | Contoh argumen |
| --- | --- | --- |
| `security_scan` | Pindai keamanan proyek: Go → `govulncheck` bila ada, selain itu `go vet` di sandbox; Node → `npm audit --json`; semua → pola secret (nilai tidak pernah ditampilkan). Pemindai yang hilang dilaporkan jujur | `project:latihan` |
| `password_gen` | Password acak kuat (8–128 karakter, opsi `nosymbol`) | `16` |

### Media

| Tool | Kegunaan | Contoh argumen |
| --- | --- | --- |
| `generate_image` | Buat gambar lewat endpoint `images/generations` provider; gambar terlampir pada balasan | prompt gambar |
| `image_info` | Format, dimensi, dan ukuran gambar (PNG/JPEG/GIF) | nama file |
| `resize_image` | Perkecil gambar ke lebar maksimum (bilinear, rasio dipertahankan) | file + lebar |
| `convert_image` | Ubah format gambar PNG ↔ JPEG | file + format |
| `image_caption` | Jelaskan isi gambar sandbox memakai model vision kursi | nama file |
| `ocr_image` | Baca teks dari gambar sandbox (memakai `tesseract` bila tersedia) | nama file |
| `text_to_speech` | Teks menjadi WAV di sandbox (memakai `espeak-ng` bila tersedia; `lang=id`/`lang=en`) | teks |
| `qr_code` | QR code PNG dari teks/URL; gambar terlampir pada balasan | teks/URL |
| `pdf_text` | Ekstrak teks PDF (memakai `pdftotext` bila tersedia) | nama file |
| `pdf_from_markdown` | Buat PDF A4 dari teks Markdown ke sandbox | Markdown |

### Lainnya

| Tool | Kegunaan | Contoh argumen |
| --- | --- | --- |
| `http_post` | POST JSON ke URL publik — **persetujuan pemilik** | URL + JSON |
| `tool_info` | Deskripsi satu tool (dipakai katalog on-demand) | nama tool |
| `mcp_<server>_<tool>` | Tool dari server MCP stdio yang terdaftar — **setiap panggilan persetujuan pemilik** | JSON objek argumen |

Selain tool bawaan, kamu dapat menambah **tool kustom** lewat definisi JSON di folder `tools/` (dimuat ulang otomatis, tanpa restart) — tipe `http_get`, `http_post` (berpersetujuan), `shell`, `javascript`, dan `pipeline` (maks 5 langkah berantai). Contoh siap pakai ada di `tools/cuaca.json`; daftar lengkap tool (bawaan + kustom + MCP) dapat dilihat lewat `GET /api/tools`.

## 6. Contoh alur kerja lengkap

Prompt berikut siap disalin ke composer (ganti nama proyek/tugas sesuai kebutuhan):

**(a) Coding end-to-end**

> @Plamo buat proyek Go bernama "latihan", tulis fungsi `Tambah(a, b int) int` beserta tesnya, jalankan tesnya, perbaiki bila gagal, lalu commit dengan pesan "feat: fungsi Tambah".

Kursi akan menjalankan rantai `create_project` → `write_file`/`edit_file` → `run_tests` → (perbaiki) → `git_commit` (menunggu persetujuanmu di banner).

**(b) Riset web → memori**

> @Deepseek cari berita teknologi terbaru minggu ini, baca dua sumber teratas, ringkas dalam 5 poin, lalu simpan ringkasannya ke memori dengan tag "riset".

Rantai: `web_search` → `fetch_url`/`browser_open` → `memory_retain`. Di lain waktu: "@Deepseek apa isi memori bertag riset?" → `memory_recall`.

**(c) Orkestrasi subagen + todo + checkpoint**

> @Muse buat todo untuk tugas ini, tandai checkpoint "mulai", lalu spawn subagen ke @Plamo untuk mengerjakan bagian A dan @GLM untuk meninjau hasilnya; kabari saya lewat hub bila selesai.

Rantai: `todo_add` → `checkpoint` → `spawn_subagent` → `hub_message` → `check_subagent`. Bila hasilnya tidak bagus, gunakan **rewind** ke checkpoint "mulai".

**(d) /review + commit workflow**

1. Panggil `POST /api/review` dengan `{"project":"latihan"}` — kursi meninjau diff proyek (perubahan belum commit atau commit terakhir) sebagai pekerjaan biasa di ruang aktif.
2. Panggil `POST /api/commit-workflow` dengan `{"project":"latihan"}` — aplikasi mengusulkan grup commit konvensional per scope.
3. Bila usulannya cocok, kirim ulang dengan `{"project":"latihan","approve":true}` untuk menjalankan commit. Push tetap terpisah: minta kursi memakai `github_push` (berpersetujuan).

**(e) Autoresearch**

Panggil `POST /api/autoresearch` dengan `{"project":"latihan","script":"bench.sh","iterations":5}` — skrip benchmark dijalankan di sandbox proyek 5 iterasi (maks 10) pada branch `autoresearch/*`; metrik tiap iterasi = angka terakhir di output, tercatat dan dilaporkan. Tidak ada reset destruktif otomatis dan kode tidak diubah otomatis.

## 7. Workflow API

| Metode | Endpoint | Payload | Keterangan |
| --- | --- | --- | --- |
| POST | `/api/review` | `{"project":"<nama>"}` | Buat job tinjauan diff proyek; hasil tampil sebagai pesan kursi di ruang aktif |
| POST | `/api/commit-workflow` | `{"project":"<nama>"}` lalu `{"project":"<nama>","approve":true}` | Usulkan grup commit konvensional; kirim ulang dengan `approve:true` untuk commit (push terpisah via `github_push`) |
| POST | `/api/autoresearch` | `{"project":"<nama>","script":"<skrip>","iterations":<1-10>}` | Benchmark sandbox N iterasi di branch `autoresearch/*`, metrik tercatat per iterasi |
| POST | `/api/rooms/{id}` | `{"goal":"…"}` / `{"planMode":true}` / `{"vibeMode":true}` | Atur goal/plan/vibe mode ruang (juga tersedia di panel kerja ruang) |
| POST | `/api/rooms/{id}/checkpoint` + `/rewind` | `{"label":"…"}` / `{"checkpointId":"…"}` | Buat checkpoint; potong pesan sesudah checkpoint |
| POST | `/api/rooms/{id}/todos` (+ `/{todoId}/status`, DELETE `/{todoId}`) | `{"text":"…"}` / `{"status":"done"}` | Kelola todo ruang dari pemilik |
| GET / DELETE | `/api/memories` + `/api/memories/{id}` | — | Daftar/hapus memori durable |
| POST | `/api/approvals/{id}` | `{"approved":true}` | Setujui/tolak aksi berisiko yang menunggu |
| POST | `/api/questions/{id}` | `{"answer":"…"}` | Jawab pertanyaan kursi (`ask_question`) |
| GET/POST | `/api/mcp-servers` (+ `/{name}/delete`) | `{name, command, args, env}` | Kelola server MCP (maks 5) |
| GET | `/api/tools` | — | `{tools, builtin, mcp}` — semua tool yang tersedia |
| POST | `/api/message` | `{"text":"@Plamo …"}` | Kirim pesan (202); jalan yang sama dengan composer |

## 8. Batas jujur

- **Browser**: `browser_snapshot` adalah teks DOM ringkas (judul + teks terlihat), bukan pohon aksesibilitas penuh. Tidak ada Lighthouse. Tool browser butuh Chromium terpasang (dicari dari env `MUSEHOOD_CHROMIUM`/`CHROME_BIN`, PATH, lalu cache Playwright).
- **LSP/debug**: nyata untuk Go (`gopls`/`dlv`). Bahasa lain butuh language server/debug adapter terpasang + diatur di `settings.lspServers` — tanpa itu tool memberi pesan instalasi, tidak mengarang hasil. Debug: satu sesi per ruang.
- **Vibe mode**: flag + instruksi direktur untuk kursi integrator, bukan runtime pekerja persisten baru.
- **Autoresearch**: mencatat metrik per iterasi; tidak mengubah kode otomatis dan tanpa reset destruktif.
- **MCP**: hanya transport stdio (bukan HTTP/SSE), tanpa sampling/roots, dan server **tidak ter-sandbox** — berjalan sebagai user aplikasi. Daftar tool di-cache 60 detik.
- **Eval**: state per (ruang+kursi+bahasa); mati bila service restart (sesi baru otomatis dibuat).
- **Binary eksternal tidak ikut rilis**: `ast-grep`, `gopls`, `dlv`, `govulncheck`, `tesseract`, `espeak-ng`, `pdftotext`, dan Chromium tidak termasuk dalam binary rilis — pasang di mesin yang menjalankan aplikasi (mis. `apt install tesseract-ocr espeak-ng poppler-utils`). Tanpa binary itu, tool terkait memberi pesan instalasi bahasa Indonesia. Di VM pengembangan, binary-binary itu terpasang di `/home` dan **hilang bila VM diganti** — degradasi anggun dengan pesan instalasi yang jelas.
- **GitHub tanpa token**: hanya baca publik (kena rate limit); `github_push` butuh token di Pengaturan.
- **Deteksi pola berbahaya** pada `run_code`/`eval` berbasis pola, bukan jaminan keamanan; sandbox bubblewrap tetap menjadi batas utamanya.

## 9. Tautan

- Rilis terakhir: **v1.11.0** — https://github.com/afman42/musehood-go/releases/tag/v1.11.0
- [README.md](README.md) — arsitektur, konfigurasi gateway, API lengkap, penyimpanan, Docker
- [TESTS.md](TESTS.md) — cakupan pengujian (338 fungsi tes Go, termasuk 8 skenario agen gabungan)
