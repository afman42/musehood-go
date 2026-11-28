# Pengujian Musehood Go

Terakhir dijalankan 28 November 2026 dengan Go 1.27.4 (`linux/amd64`), Node.js 24.22.1, dan Chromium lokal melalui Playwright 1.65.1. Pengujian memakai gateway mock di loopback dan direktori data sementara.

## Audit 18 skill Go + 2 skill Svelte, gzip aset + skeleton (28 Nov 2026, v1.11.0)

**Audit backend** — 18 skill `golang-*` diterapkan satu per satu (benchmark 12 benchmark diukur; CLI: error `run()` dibungkus `%w`; code-style: 15 file gofmt; concurrency: sehat, subset `-race` bersih; context: timeout DNS webhook 5 dtk, `projectDiff` + `proposeCommitGroups` ber-`CommandContext`; database: SQLite `busy_timeout(5000)` + WAL, fsync sebelum rename; documentation: `doc.go` baru; error-handling: kegagalan `saveLocked` kini dicatat; gopls: 1 temuan diperbaiki; lint: staticcheck — 4 dead code dihapus; modernize: 28 `sort.*` → `slices.*`; performance: transport gateway `MaxIdleConnsPerHost=10`; security: **SSRF via redirect diperbaiki** (`checkRedirectPublic` di semua client HTTP tool), `govulncheck` 0 kerentanan). Tes baru: `TestSecurityRedirectGuard`, `TestValidateWebhookURL` (10 kasus), `TestGatewayTransport`.

**Audit frontend** — `svelte-code-writer` + `svelte-core-bestpractices`: key pada `each` SettingsToolLog, 15 direktif `class:` → clsx, guard `$state` read-own-write → `let` polos (3 panel), `serverResults` → `$state.raw`, listener polling kini dilepas saat cleanup, `aria-label` pada select kebijakan (axe `select-name`). Tiga kegagalan suite saat verifikasi terbukti pra-ada (race tes hapus pesan, artefak animasi pada axe, 11 baseline visual mendahului batch omp) — diperbaiki di sisi tes.

**Perbaikan layout Pengaturan** — aturan CSS `.settings-section textarea` ditambahkan (textarea sebelumnya inline-block 161 px menimpa label di Rantai prompt/Template diskusi/MCP); diverifikasi Playwright mobile 390×844 + 360×800 sebelum/sesudah; 7 baseline visual diperbarui.

**Gzip aset statis + skeleton loading** — 10 tes baru `static_compress_test.go` (gzip, `q=0`, berkas kecil/biner, Range, ETag 304 dua varian, header cache, HEAD, fallback index, hash CSP). Initial load 161.055 → **54.693 byte (-66%)**; skeleton terlihat 251–266 ms pada throttle 400 kbps (sebelumnya putih kosong ±2,9 dtk), 0 error konsol.

Total akhir v1.11.0: **389 fungsi tes Go** (`go test -list`) + **15 benchmark**, unit **76**, component **43**, `svelte-check` 0 error, `go vet` bersih, suite Go penuh hijau. Dirilis sebagai **v1.11.0** (28 Nov 2026).

## Kompresi gzip respons API JSON (28 Nov 2026)

`gzipJSONBody` di `internal/musehood/http.go` mengompresi badan JSON utuh >1 KB di `writeJSON` (semua endpoint API) dan unduhan cadangan (`/api/backup`), menghormati `Accept-Encoding` termasuk `q=0`, dengan `Content-Encoding: gzip` + `Vary: Accept-Encoding` + `Content-Length` eksplisit; hasil yang tidak lebih kecil dikembalikan polos. Streaming tidak tersentuh: tidak ada endpoint SSE ke browser (frontend polling `/api/state`), dan streaming gateway→server (`gatewayStream`) membaca baris demi baris — dibuktikan `TestGatewayStreamIncremental` (baris pertama tiba selagi server mock masih menahan baris kedua). 8 tes baru di `api_compress_test.go` (kompresi, q=0, di bawah ambang, tanpa Accept-Encoding, error kecil, `/api/state` ujung-ke-ujung, cadangan, streaming inkremental). Terukur di produksi: `/api/state` 33.218 → 9.435 byte (-72%), `/api/search?q=muse` 6.891 → 3.061 (-56%), `/api/backup` 44.870 → 10.282 (-77%), `/api/tools` 1.865 → 855 (-54%). Suite Go penuh hijau (242 dtk), `go vet` bersih, svelte-check 0 error, unit 76, component 43, e2e 21, perf 33, e2e-actions 10, buttons-perf 10. Deploy: binary dibangun ulang (cadangan `musehood-go.bak-20261128-api-gzip`), lokal + publik 200, balasan kursi M04 terverifikasi di produksi.

## Tes berat ronde 2 — backend + UI/UX (28 Nov 2026)

**Backend** (`internal/musehood/heavy2_*_test.go`, 30 tes + 3 benchmark baru): konkurensi tool omp (eval paralel 8 kernel terisolasi, hub/todo paralel 30+40, checkpoint/rewind saat job berjalan, spawn_subagent paralel kedalaman maks 1, CDP 6 tab paralel); keamanan (plan mode menyapu 111 tool, kebijakan deny/prompt di jalur langsung + `execute` + pipeline, MCP deny, `ast_edit`/`lsp_rename` tanpa approval ditolak, guard `github_push`, SSRF redirect GitHub + CDP, `security_scan` tanpa bocor 6 jenis secret, gate eval berbahaya, rewind via tool butuh approval); skala (memori FIFO 500, todo 100/ruang, checkpoint 50/ruang, 200 ruang dengan goal/todo/checkpoint — state 35 ms, search 3 ms, autoresearch 10 iterasi, review diff besar, `execute` 20 RPC ~95 ms, MCP berulang dengan cache 60 dtk); ketahanan (kernel eval dibunuh di tengah panggilan → pulih, LSP mati/hang tanpa hang, debug tanpa dlv, browser tanpa Chromium, CDP dibunuh → pulih). Benchmark baru: dispatch kebijakan 585 ns/op, `memory_recall` atas 500 memori 91 µs/op, `todo_list` 100 item 73 µs/op. Subset `-race` bersih.

Bug produk ditemukan + diperbaiki (semua dites ulang hijau): (1) data race `lastUsed` kernel eval → `atomic.Int64`; (2) kernel eval mati tidak dievakuasi → panggilan berikutnya gagal sekali; (3) LSP `drainDiagnostics` balapan membaca `bufio.Reader` yang sama dengan `request()` → respons shutdown tertelan → hang; kini satu readLoop + antrean pending; (4) rewind checkpoint mempertahankan checkpoint lebih baru bertimestamp ms sama → kini memotong berdasarkan urutan slice; (5) `githubGet` memakai `http.DefaultClient` → redirect ke alamat privat diikuti (SSRF) → `CheckRedirect` publik; (6) CDP `browser_tabs list` membaca `activeTab` tanpa kunci + `debug stop()` balapan dengan operasi aktif.

**Frontend** (`tests/frontend-heavy2-stress.mjs` 18 cek, `frontend-heavy2-a11y.mjs` 11 cek, `frontend-heavy2-visual.mjs` 8 baseline baru): SettingsOmp 111 pemilih kebijakan diubah beruntun, RoomWorkPanel goal/plan/vibe/20 todo/3 checkpoint lewat UI, QuestionBanner jawaban pilihan + bebas, SettingsMCP tambah/hapus; initial load 249,3 KB (< 350 KB), buka Pengaturan pertama 252–334 ms, render 100 todo + 50 checkpoint 457 ms tanpa long task > 2 dtk; axe bersih di panel baru × 2 tema; navigasi keyboard (Enter/Tab/Escape + fokus kembali). Bug UI ditemukan + diperbaiki: perubahan kebijakan cepat beruntun di SettingsOmp saling menimpa (lost update) → salinan lokal optimistis; select/status todo di RoomWorkPanel tanpa nama aksesibel (axe critical `select-name`) → `aria-label`. Baseline visual-heavy ronde 1 diperbarui (bilah "Kerja ruang" baru + timestamp server nyata; tes itu sensitif tanggal) dan skripnya dibuat deterministik (scroll dikunci ke bawah).

Total sesudah ronde ini: **371 fungsi tes Go** (341 → +30) + **15 benchmark** (12 → +3), unit **76**, component **43**, `svelte-check` 0 error, `go vet` bersih, suite Go penuh hijau (194 dtk), semua skrip frontend lama + baru hijau (satu flake `frontend-buttons-perf` lulus pada rerun). Deploy: binary dibangun ulang, unit systemd dipulihkan dari salinan repo (VM terganti), lokal + publik 200, `/api/tools` = 111, 3 tool nyata via kursi M04 (`memory_recall`, `eval`, `todo_add`) terverifikasi di log tool. Batas jujur: Firefox/WebKit tidak teruji (unduhan 403).

## 13 item gaya omp + skenario agen (28 Nov 2026, v1.10.0)

36 tool bawaan baru (total **111**) dari pesanan "implementasi semuanya" gaya omp, dengan **14 fungsi tes Go baru** di `internal/musehood/tools_omp_test.go` + **8 skenario agen** di `internal/musehood/agent_scenarios_test.go` + **36 kasus baru** di `heavy_tools_test.go` (semua 111 tool bawaan diuji argumen valid + invalid):

- `ast_grep`/`ast_edit`: ast-grep 0.51.3 nyata — pola ditemukan, pratinjau diff tidak mengubah file, `apply` mengubah file (persetujuan `edit` + snapshot), argumen invalid ditolak.
- LSP: gopls nyata (definition/references/diagnostics pada proyek Go sungguhan; `workspaceFolders` wajib — ditemukan lewat debug manual) + server LSP palsu Python (rename menerapkan WorkspaceEdit `changes` + `documentChanges`); tanpa server = pesan instalasi.
- `eval`: kernel Python persisten — variabel panggilan pertama terbaca di kedua, `reset` mengosongkan state, pola berbahaya = persetujuan.
- Debug DAP: delve nyata (launch program Go, breakpoint, inspect variabel, stop) + server DAP palsu Python (handshake request_seq); tanpa dlv = pesan jelas.
- Browser CDP: Chromium nyata terhadap halaman lokal (plumbing diuji langsung karena tool menolak loopback via SSRF; navigasi/klik/isi/snapshot/tab) + degradasi tanpa Chromium.
- GitHub: server HTTP palsu lokal (`MUSEHOOD_GITHUB_API`) — baca repo/file/issue/PR/diff, search, actions; token dikosongkan di backup + dipulihkan saat restore; tanpa token = baca publik + pesan jelas.
- `security_scan`: govulncheck nyata pada proyek Go bersih, pola secret terdeteksi tanpa menampilkan nilai, pemindai hilang dilaporkan jujur.
- Hub/checkpoint/rewind/todo/memori/learn/manage_skill: pesan antar-kursi masuk ruang, registri proses latar, checkpoint + rewind memotong pesan (persetujuan), todo API+tool, memori durable retain/recall/reflect/edit + toggle, skill terkelola dibuat/dipensiunkan.
- Workflow: `/api/review` membuat job tinjauan, `/api/commit-workflow` mengusulkan grup lalu commit sesudah `approve:true`, goal/plan/vibe mode (plan menolak tool tulis di dispatch), `/api/autoresearch` mencatat metrik per iterasi tanpa reset destruktif.
- Kebijakan per tool + katalog on-demand: `deny` menolak dengan pesan jelas, `prompt` memicu persetujuan kind `tool`, `tool_info` mengembalikan deskripsi.
- **Skenario agen gabungan** (rantai multi-tool lewat dispatch penuh, bukan fungsi internal): (a) coding create→edit→ast_grep→run_tests gagal→perbaiki→lulus→git_commit berpersetujuan; (b) riset web_search→fetch_url→browser_open→memory_retain→recall; (c) orkestrasi todo→spawn_subagent→hub_message→check_subagent→checkpoint→rewind berpersetujuan; (d) Code Mode `execute` memanggil 3 tool berbeda dalam satu skrip; (e) plan mode menolak tulis lalu lolos sesudah persetujuan; (f) MCP palsu dipanggil langsung + dari dalam `execute`; (g) eval persisten berbagi state; (h) kebijakan deny/prompt. **8/8 lulus.**
- **Skenario LIVE** via kursi gateway sungguhan (M04 Deepseek): `todo_add` → `eval` python `print(6*7)` = 42 → `memory_retain` — terverifikasi ujung-ke-ujung di produksi, ruang uji dibersihkan.

Bug ditemukan + diperbaiki saat pengetesan: (1) kernel eval mati di produksi karena path bind bwrap relatif (`a.dir` = direktori kerja) — dijadikan absolut; (2) `systemd-run --scope` gagal dari dalam service ("Failed to connect to bus") — eval kini jatuh ke bwrap langsung seperti `run_code`; (3) handshake DAP menelan respons `launch` — digabung ke satu loop request_seq; (4) target CDP hancur saat navigasi — `evaluate` mencoba ulang sekali dengan target baru; (5) parsing `git status --porcelain` tidak boleh TrimSpace (spasi depan signifikan).

Total sesudah batch ini: **338 fungsi tes Go** (`go test -list`) + 12 benchmark, unit **76**, component **43**, `svelte-check` 0 error, `go vet` bersih, suite Go penuh hijau.

Dirilis sebagai **v1.10.0** (28 Nov 2026, tag `cf87afb9` di remote main `0128947b`, **40/40 asset** binary lintas platform ter-upload dan terverifikasi via API; build release OK=40, FAIL=5 cgo android/ios, SKIP=2 wasm — sama seperti rilis sebelumnya). Rilis: https://github.com/afman42/musehood-go/releases/tag/v1.10.0

## 6 tool gaya OpenCode (28 Nov 2026, v1.10.0)

Sepuluh tool bawaan baru (total 75) dari pesanan "implementasi semuanya" gaya OpenCode, dengan **9 fungsi tes Go baru** di `internal/musehood/tools_opencode_test.go` + **10 kasus baru** di `heavy_tools_test.go` (semua 75 tool bawaan kini diuji argumen valid + invalid) + **5 tes component baru** (`tests/component/opencode-components.test.mjs`):

- `ask_question`: alur tanya → pemilik menjawab via API (pilihan + jawaban bebas) → hasil tool; timeout = hasil informatif "tidak menjawab"; argumen tidak valid ditolak.
- `spawn_subagent`/`check_subagent`: spawn membuat ruang sub + job, check membaca balasan, ringkasan diposting ke ruang induk, spawn dari ruang sub ditolak (kedalaman 2), kursi tujuan harus gateway.
- `execute` (Code Mode): RPC JSON-lines memanggil tool lain (`current_time`) dari JS sandbox, nilai return, batas 20 panggilan, `execute` bersarang diblokir, `throw` → teks error.
- `worktree_create/list/remove`: proyek tanpa commit ditolak dengan pesan jelas; commit nyata via git, create/list/remove, duplikat + nama tidak valid ditolak.
- `browser_open/screenshot/links`: degradasi tanpa Chromium (pesan instalasi bahasa Indonesia), penjaga SSRF loopback, ekstraksi tautan; render nyata Chromium pada URL `data:` dengan konten yang disuntikkan JS (dilewati bila Chromium tidak ada). Klik/isi form kini tersedia lewat tool CDP gaya omp (lihat bagian di atas); Lighthouse tetap tidak diimplementasikan.
- MCP stdio: server MCP palsu (script Python JSON-RPC) → initialize + tools/list + tools/call; nilai env tidak bocor ke respons API/state; tool muncul di `GET /api/tools`; panggilan butuh persetujuan kind `mcp` (timeout = ditolak); tool tak dikenal ditolak; batas 5 server; hapus server + 404.

Bug ditemukan + diperbaiki saat pengetesan: map cache MCP `mcpRefreshing` tidak pernah diinisialisasi → panic "assignment to entry in nil map" saat memegang mutex cache → deadlock permanen pada `GET /api/tools`. Diperbaiki dengan inisialisasi map di bawah lock + `defer Unlock` di ketiga jalur cache.

Total sesudah batch ini: **316 fungsi tes Go** (`go test -list`) + 12 benchmark, unit **76**, component **43**, `svelte-check` 0 error, `go vet` bersih, suite Go penuh hijau (123 dtk).

## Pendalaman fitur gelombang 3 + code splitting + perbaikan tombol (27 Nov 2026, v1.9.0)

12 fitur gelombang 3 (ruang multi-pengguna, preset peran kursi, uji A/B model, rantai prompt, koreksi otomatis, dokumen RAG, ringkasan harian, offline PWA penuh, ekspor Markdown Notion/Obsidian, rating balasan, deteksi Ollama, perintah /slash) ditambah **10 fungsi tes baru** di `depth3_test.go` → total **286 fungsi tes** saat itu; unit frontend naik ke **76**, component **38**, `svelte-check` 0 error. Terverifikasi live: rantai prompt dijalankan ujung-ke-ujung lewat gateway asli, peserta ruang/rating/ekspor/deteksi Ollama dicek lewat API.

**Code splitting**: panel Pengaturan (82 KB) + 7 dialog (Mention, Info Kursi, Jadwal, Debat, A/B, Shortcut, Ruang) dimuat malas via dynamic import. Muatan awal (JS+CSS yang benar-benar diunduh) turun dari ±328 KB menjadi **229 KB**; `frontend-perf.mjs` kini menerapkan ambang <350 KB pada muatan awal dan **lulus** (margin 121 KB). Efek samping baik: baseline visual `login-pin` diperbarui — toast error "PIN diperlukan" di layar login hilang (penyebabnya: panel Log Tool yang selalu ter-mount memanggil `/api/tool-log` saat muat halaman → 401).

**Perbaikan tombol lambat** (keluhan pemilik: tombol tema + Pengaturan lama): terukur sebelumnya tema 1.224 ms lokal / 1.902 ms lewat tunnel (menunggu server + render ulang semua bubble), Pengaturan 1.695 ms klik pertama (chunk 82 KB tanpa indikator). Sesudah perbaikan: tema optimistis **5–23 ms** tanpa longtask, Pengaturan dingin 705 ms / hangat 262 ms dengan spinner bila >150 ms + prefetch chunk saat idle, rating optimistis 12–86 ms, render awal 1000 pesan 4.201 → ±2.900 ms. Audit 45 tombol (ruang 300 pesan, Playwright) — semua aman; yang memang butuh waktu (debat, voting, A/B) bersifat job-based dan sudah berindikator. Tes baru `tests/frontend-buttons-perf.mjs` **10/10** (tema <300 ms tanpa longtask, rating optimistis, spinner saat chunk ditunda, prefetch tanpa fetch `/api/tool-log`, spinner ganti ruang). Bug produk ditemukan + diperbaiki: `/api/pin` kini menerbitkan cookie sesi (sebelumnya sesi langsung 401 sesudah PIN pertama dipasang, tombol Keluar tidak pernah muncul) — ada tes regresinya.

Total akhir v1.9.0: **308 fungsi tes Go** (termasuk pengetesan berat di bawah) + 12 benchmark, unit **76**, component **38**, semua suite hijau.

## Tool coding proyek + Docker (27 Nov 2026)

29 tool coding baru (`tools_code*.go`, total 65 tool bawaan) diuji oleh 7 fungsi tes di `tools_code_test.go`:

- `TestProjectCreateAndFiles` — create_project (template Go, tolak duplikat/nama buruk), list_tree, read_file_lines, mkdir, copy/move (tolak menimpa), search_code, find_files glob `**`, penolakan traversal path (`../`, absolut).
- `TestProjectPatchEditManySnapshot` — apply_patch ubah+tambah file, kegagalan konteks = atomik (tidak ada file berubah), edit_many multi-file, snapshot otomatis (auto-patch/auto-edit), create/list/restore (persetujuan)/delete.
- `TestProjectBuildTestLintFormat` — proyek Go nyata di sandbox: run_build, run_tests, run_lint, format_code, allowlist_shell (perintah aman jalan; metakarakter `;` dan perintah di luar allowlist ditolak).
- `TestProjectRunServer` — python http.server di port eksplisit: start, HTTP GET 200 di 127.0.0.1, status, penolakan server kedua, stop.
- `TestProjectGit` — git_commit (persetujuan; commit bersih = tidak ada perubahan), git_branch create/list/checkout, git_stash push/list/pop (persetujuan).
- `TestProjectIntelligence` — outline, find_references, dependency_graph, todo_scan, complexity_report, generate_tests (kerangka Go + tolak menimpa).
- `TestProjectExportImportEnv` — export_project (zip + pola nama unduhan), import_project dari uploads, penolakan zip-slip, env_file set/get ter-mask/list/delete, redaksi argumen log audit (`redactToolArg`).

Hasil: `go test ./... -timeout 900s -count=1` hijau (internal 79,9 dtk). svelte-check 0 error. Demo live via kursi Plamo: create_project → run_build → run_tests semuanya ok. Docker: CLI docker tidak tersedia di VM ini — `docker-compose.yml` terverifikasi sebagai YAML valid, tetapi build image TIDAK teruji di sini.

## Pendalaman fitur putaran 2 — 26 fitur (27 Nov 2026)

Implementasi 26 fitur pendalaman (perintah Arman: nomor 3,4,5,6,7,8,9,10,12,13,14,15,17,18,19,22,23,24,25,27,28,35,34,41,42,43,45) ditambah **30 fungsi tes baru** di 6 file `depth2_*_test.go`. Total kini **268 fungsi tes** (263 `internal/musehood` + 5 `main`); `go test ./... -timeout 600s -count=1` semua hijau; frontend unit **71** + component **38** hijau; `svelte-check` 0 error.

| Grup | File | Isi |
| --- | --- | --- |
| A — Kursi (3,4,5,6) | `depth2_a_test.go` (4) | Kloning kursi menyalin konfigurasi; memori otomatis sesudah ringkasan ruang; budget harian melewati kursi; model per tugas (chat/tools/summary) tercatat di mock gateway. |
| B — Gateway (7,8,9,10,12) | `depth2_b_test.go` (7) | Retry 500→sukses & 400 tanpa retry; circuit breaker membuka/menutup + badge health; parser streaming anthropic/gemini/ollama + mock SSE/NDJSON; tool-calling native + fallback teks; load balancing ke provider kedua. |
| C — Percakapan (13,14,15,17,18) | `depth2_c_test.go` (5) | Edit+regenerasi (perilaku berubah: 202 + job baru); branching terisolasi; mode paralel konkuren; tag pesan + filter pencarian; cron mini (next-occurrence + penolakan range). |
| D — Ruang & tool (19,22,23,24,25,27) | `depth2_d_test.go` (7) | Ikon/warna ruang + validasi; header tool kustom hasil substitusi (tidak masuk log audit); tool javascript di sandbox node + approval kode berbahaya; pipeline 2 langkah + larangan bersarang; tool nonaktif per ruang; hitungan pemakaian skill. |
| E — Keamanan & data (28,35,34) | `depth2_e_test.go` (3) | Rate limit PIN (5 gagal → 429, IP lain bebas) + log audit; indeks full-text (AND multi-kata, substring sebagian, pesan terhapus hilang); backend SQLite roundtrip + migrasi dari data.json + pemilihan env. |
| F — Integrasi (41,42,43,45) | `depth2_f_test.go` (4) | Webhook terkirim saat balasan selesai + URL privat ditolak; token API eksternal (benar 202, salah/kosong 401, bebas gerbang PIN, tidak bocor di state); jembatan Telegram via mock (update masuk jadi pesan, balasan terkirim, token tidak bocor); cek + terapkan update via mock GitHub (binary terganti, `.bak` tersimpan, restart di-stub). |

Tiga regresi grup B ditemukan oleh suite penuh dan diperbaiki: (1) jalur tool native kini menghormati cache respons (sebelumnya pertanyaan identik selalu memukul gateway); (2) jalur native mengurai balasan SSE menjadi teks (sebelumnya payload mentah tampil sebagai balasan); (3) teks EN i18n menghindari apostrof ter-escape agar paritas tes i18n lulus. Tes lama yang disesuaikan dengan perilaku baru yang memang diperintahkan: edit pesan kini 202+regenerasi (3 tes), stres login memakai IP berbeda per percobaan agar tidak kena rate limit baru.

Catatan penyimpanan (34): driver pure-Go `modernc.org/sqlite` v1.63.0 berhasil diunduh; backend SQLite menyimpan blob JSON yang sama di tabel kv `data.db`. Pemilihan: env `MUSEHOOD_STORAGE=sqlite|json`; tanpa env, SQLite hanya bila `data.db` sudah ada — produksi tetap JSON. Migrasi otomatis dari `data.json` saat tabel kosong; berkas JSON dipertahankan sebagai cadangan.

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

## Pendalaman tes frontend 6 kategori (27 Nov 2026)

Untuk UI 19 fitur pendalaman (login PIN + Keamanan, Log Audit Tool, dialog debat, bantuan shortcut, mode kompak, editor urutan bicara, dropdown persona, arsip ruang, rerun/fork, pemilih reaksi, ekspor diskusi, form pasang skill, tabel latency, badge health, toggle cache respons) semua kategori tes frontend diperdalam. Semua suite hijau pada menjalankan terakhir (27 Nov 2026); `svelte-check` 0 error (1 peringatan lama di `+page.svelte`); `tests/binary.test.mjs` dilewati karena memakai port 3001 yang sedang dipakai service produksi.

| Kategori | Perintah | Hasil |
| --- | --- | --- |
| Unit | `(cd frontend && npm run test:unit)` | **71 lulus** (sebelumnya 48): batas `humanSize` (0/999/1024/MB/GB), `splitMentions` (nama mirip awalan, `@` tengah kata, banyak mention), `highlightParts`, fence Markdown belum ditutup, skema link berbahaya ditolak, heading/list bertingkat, escape HTML mentah, semua binding shortcut, paritas kunci i18n ID/EN + fallback, `maybeNotify` (hanya tab tersembunyi + izin granted), interval polling visible/hidden, alur PIN dan rute debat/rerun/fork di `chat.svelte.ts`. |
| Component & integration | `(cd frontend && npm run test:component)` | **38 lulus** (sebelumnya 22): render SSR DebateDialog (dua select, mulai nonaktif saat kursi sama), ShortcutHelpDialog, SettingsSecurity (PIN aktif/nonaktif), SettingsToolLog (kosong vs terisi), SeatInfoDialog + persona, MessageBubble reaksi (8 emoji + hitungan), PinnedBar ringkasan ruang, RoomBar (arsip tersembunyi, badge `99+`), editor urutan bicara, toggle cache/chain/density. Interaksi komponen↔API (persona diterapkan, density → kelas `density-compact`, penolakan pasang skill) diverifikasi di E2E karena SSR tidak bisa berinteraksi. |
| E2E | `node tests/frontend-e2e-depth.mjs`, `node tests/frontend-e2e-depth-settings.mjs` | **23 + 10 PASS**: alur PIN penuh (pasang → login salah ditolak → benar masuk → logout), debat 5 tahap berurutan dengan label, fork (riwayat tersalin sampai titik fork), rerun (balasan lama hilang, baru muncul), reaksi naik/turun, arsip ruang sembunyi/pulih, ekspor diskusi (nama file `diskusi-musehood.html`), urutan bicara diikuti round-robin, shortcut Ctrl+K/`?`/Alt+1-2/Ctrl+Enter, persona diterapkan ke server, penolakan pasang skill (non-HTTPS + SSRF; jalur sukses/409 tidak bisa di-E2E karena backend mengambil URL sebelum cek duplikat — dicakup tes backend), density+cache+chain tersimpan, badge health, tabel latency, log tool terisi dari chat nyata. |
| Accessibility | `node tests/frontend-a11y-depth.mjs` | **18 PASS**: axe pada login PIN, dialog debat, bantuan shortcut, info kursi, Keamanan, Log Audit Tool; Esc menutup dialog + fokus kembali ke pemicu; aria-live mengumumkan balasan debat/rerun; target reaksi ≥ 24px; label form pasang skill + dropdown persona; kontras mode kompak. |
| Visual regression | `node tests/frontend-visual.mjs` | **12 PASS** (sebelumnya 5): baseline baru `login-pin`, `dialog-debat`, `dialog-shortcut`, `chat-kompak`, `bubble-tool-reaksi` (kartu tool + pil reaksi), `pengaturan-log-tool` (log terisi + Keamanan), `bilah-ruang-badge` (badge belum dibaca + ruang arsip tersembunyi). Baseline lama disegarkan untuk perubahan warna kontras; stabil tanpa `--update` (beda maks 0,036%). |
| Cross-browser & perf | `node tests/frontend-perf.mjs` | **33 PASS** (sebelumnya 14): dialog debat/bantuan/pengaturan + login PIN di 1280/768/390px dan sentuh iPhone 13 tanpa overflow, login PIN via sentuh; ruang besar (seed `data.json`): render 300 pesan normal ~0,8–1,0 dtk vs kompak ~0,8–0,9 dtk, render 3000 pesan ~3,7 dtk, idle 3,5 dtk pada 3000 pesan ≤ 2 long task (total ≤ 132 ms), ketik 60 karakter pada 3000 pesan ~76–126 ms/karakter (dibatasi biaya frame browser untuk DOM besar di VM 1,5 CPU; pada 300 pesan ~15 ms/karakter), pencarian Ctrl+K pada 3000 pesan ~0,5–0,8 dtk, buka log tool 500 entri ~2,2–2,4 dtk. |

Enam perbaikan produksi ditemukan oleh tes ini: (1) `humanSize` kini punya cabang GB (sebelumnya ≥ 1 GB tampil sebagai ribuan MB); (2) Log Audit Tool dimuat ulang setiap dialog pengaturan dibuka (sebelumnya basi/kosong karena komponen selalu ter-mount); (3) warna meta bubble pemilik + tautan mention digelapkan agar kontras axe stabil ≥ 4,5; (4) `/api/state` kini menyertakan `messageCount` per ruang — badge belum dibaca sebelumnya tidak pernah tampil (tes regresi Go `TestAPIStateRoomsIncludeMessageCount`); (5) form dialog debat + bagian fieldset Keamanan/Log Audit Tool kini ter-style seperti bagian pengaturan lain; (6) polling yang tidak membawa perubahan tidak lagi me-render ulang daftar pesan (signature state di `chat.svelte.ts` — sebelumnya long task 400–515 ms per poll pada ruang 3000 pesan) dan resize textarea composer hanya berjalan bila tinggi perlu berubah.

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
| `(cd frontend && npm run test:unit)` | **71 lulus**: helper format/API, parser Markdown, lazy-load syntax highlighting, composable `chat.svelte.ts`, i18n, shortcut. |
| `(cd frontend && npm run test:component)` | **38 lulus**: render SSR komponen Svelte (termasuk Markdown, blok kode, modal info kursi, dialog debat/bantuan, panel Keamanan/Log Tool). |
| `node tests/frontend-e2e.mjs` | **21 PASS**: chat, mention + modal info kursi, render Markdown + syntax highlighting, pin, ruang, pencarian, voting, persetujuan tool. |
| `node tests/frontend-e2e-settings.mjs` | **20 PASS**: CRUD provider, fetch/test models, toggle, kursi custom, ruang, preset, statistik, backup/restore, ekspor. |
| `node tests/frontend-e2e-actions.mjs` | **10 PASS**: edit/hapus pesan, reaksi, balas/kutip, unggah gambar & file. |
| `node tests/frontend-e2e-depth.mjs` | **23 PASS**: PIN, debat 5 tahap, fork, rerun, reaksi, arsip ruang, ekspor diskusi, urutan bicara, shortcut. |
| `node tests/frontend-e2e-depth-settings.mjs` | **10 PASS**: persona, pasang skill (penolakan), density/cache/chain, badge health, tabel latency, log tool. |
| `node tests/frontend-a11y.mjs` | **20 PASS**: audit axe-core terang + gelap, nama aksesibel, keyboard/fokus, kontras WCAG, target sentuh. |
| `node tests/frontend-a11y-depth.mjs` | **18 PASS**: axe permukaan baru, Esc + fokus kembali, aria-live, target reaksi, label form. |
| `node tests/frontend-visual.mjs` | **12 PASS**: screenshot 12 skenario vs baseline piksel. |
| `node tests/frontend-perf.mjs` | **33 PASS**: viewport 320–1920px + permukaan baru, emulasi sentuh iPhone, anggaran bundle <350 KB (aktual 303 KB), perf ruang 300/3000 pesan. |

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

## Pengetesan berat (27 Nov 2026)

Perintah pemilik: test berat UI/UX + backend menyeluruh. Semua tes baru hijau; bug yang ditemukan diperbaiki + diuji ulang.

### Backend (`internal/musehood/heavy_*_test.go`, +22 fungsi → total 308)

- `heavy_concurrency_test.go` (6): 100 POST pesan bersamaan (semua 202, tanpa hilang/duplikat), 50 operasi campuran bersamaan, 200 ruang, 5000 pesan (`/api/state` ±180 ms, `/api/search` ±86 ms), 50 dokumen RAG (batas 10/ruang ditegakkan), rantai 6 langkah + A/B dua sisi, pesan 20.000 karakter + emoji/RTL. Subset konkurensi lulus `-race` bersih.
- `heavy_security_test.go` (9): fuzz sweep 31 endpoint × 23 payload (JSON rusak, tipe salah, string 300 KB, null byte) — tidak pernah 500; path traversal ID → 4xx; SSRF (metadata IP, DNS rebinding, host campuran) ditolak; brute-force PIN → 429 + LoginLog; restore jahat (PIN/token asing diabaikan, 1000 kursi tidak 500); upload abuse (6 MB, PDF rusak, nama traversal); sandbox tool (metakarakter shell, traversal, 101 KB); redaksi secret di state/backup/export/tool-log/stats; token eksternal salah → 401; approval timeout (var diperpendek 300 ms) gagal rapi tanpa deadlock; 30 login salah bersamaan → 429 tanpa 500.
- `heavy_tools_test.go` (1 fungsi, 65 kasus = semua tool builtin): tiap tool dipanggil dengan argumen valid + invalid; jaringan dimock (RoundTripper palsu); tool ber-approval diuji sampai gerbang approval.
- `heavy_data_test.go` (5): SQLite 1000 pesan roundtrip (simpan 13,7 ms, muat 4,3 ms), migrasi JSON→SQLite data besar, `data.db` rusak → error jelas, restart di tengah job (job selesai, tidak menggantung), sapu method salah di 54 route → 404/405/401, tidak pernah 5xx.

Perubahan produksi (testability, perilaku sama): `approvalTimeout` const → var; klien HTTP unduhan jadi var `downloadHTTPClient`.

### Frontend (`tests/frontend-*-heavy.mjs`)

- `frontend-e2e-heavy.mjs` (26 PASS): alur penuh — mention + streaming, tool call → approval → kartu tool, rating + statistik, pin, voting, edit, branch, A/B dialog, rantai 3 langkah, dokumen upload/hapus, peserta ruang, /slash, offline → antrean → terkirim, ekspor Markdown, PIN + rate limit.
- `frontend-heavy-stress.mjs` (16 PASS): render 3000 pesan ±6–6,8 dtk, long task terburuk ±53–62 ms saat scroll+polling, ketik 15 karakter ±1,5–2,9 dtk, 50 ruang (ganti ruang ±165–192 ms), pesan 20.000 karakter, tabel Markdown tidak crash, kode >20.000 karakter tampil tanpa highlight, dialog A/B tanpa overflow 360/768/1440 px, emulasi iPhone 13.
- `frontend-a11y-heavy.mjs` (16 PASS): axe di 5 permukaan × 2 tema + keyboard (Tab, fokus terlihat, Enter, aria-live, Esc + fokus kembali), label rating.
- `frontend-visual-heavy.mjs` (8 baseline baru): dialog A/B, panel Rantai, banner offline, kartu rating — terang + gelap, stabil 0,000%.

### Bug ditemukan + diperbaiki

- Kontras warna (axe serious): `.eyebrow`/`.field-hint`/placeholder memakai warna tetap yang gagal di tema gelap (2,79:1) → `var(--muted)`; `.danger-text` → `var(--red)`; `.typing-status` terang 3,44:1 → `var(--muted)`; tombol utama tema gelap teks putih di atas hijau terang (2,12:1) → teks gelap. Baseline visual lama diperbarui mengikuti warna baru.
- `tests/live-e2e.mjs` masih mengharapkan edit pesan = 200; perilaku sekarang 202 + regenerasi — ekspektasi diperbarui (bukan bug produk).

### Tidak bisa diuji (jujur)

- Firefox/WebKit: unduhan browser diblokir runtime (403) — cross-browser hanya Chromium multi-viewport + emulasi iPhone 13.
- Timeout approval 180 detik asli diuji lewat var yang diperpendek (jalur kode sama).
- `frontend-perf.mjs`: ukuran bundle JS+CSS 349,79 KB membulat 350 → gagal ambang `< 350 KB` (sudah di ambang sejak fitur depth3, bukan dari perubahan ini). **Teratasi di v1.9.0**: code splitting menurunkan muatan awal menjadi 229 KB dan ambang kini diterapkan pada muatan awal — tes lulus (lihat bagian gelombang 3 di atas).
