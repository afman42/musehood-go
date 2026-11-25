# Audit 4 Skill Go — Musehood Go (25 Nov 2026)

Repo: `~/workspace/musehood-go` (commit awal `c20da9b`, aplikasi web multi-AI, live di https://crisp-star-bdba159c.tunnl.gg).
Tanggal: 25 Nov 2026, ~12:18–13:00 WIB. Metode: satu sesi pi FRESH per skill, `--no-skills`, brief Inggris singkat, mode Review/Audit dari tiap SKILL.md.
Status akhir: **tidak ada commit, tidak ada push, tidak ada deploy**. Semua perubahan di working tree, Arman yang memutuskan.

## Ringkasan eksekutif

| Skill | Temuan konkret (diperbaiki) | Temuan lain (dilaporkan saja) | Perubahan kode |
|---|---|---|---|
| 1. golang-design-patterns | **4 bug High** (race export, export bocor secret, hapus kursi rusak job, hapus kursi cegah restart) | 5 observasi desain (Medium/Low) | messages.go, store.go + design_review_test.go (4 tes) |
| 2. golang-error-handling | **4 bug** (redaksi key ter-encode, mutasi gagal tetap teraplikasi, stream gagal dilaporkan sukses, panic regexp dari kode kursi) | 6 gap diagnostik/konvensi (Low) | engine.go, gateway.go, messages.go, store.go + error_handling_test.go (4 tes) |
| 3. golang-performance | **1 bottleneck terukur** (regex di-compile ulang per pesan: 806 µs/op → 1,08 µs/op, ~750x) | 4 jalur lain diukur, tidak diubah | engine.go, http.go (cache pola) + engine_test.go |
| 4. golang-testing | **3 gap diperbaiki** (subtes tergantung urutan, SVG tanpa assertion, boundary input tak teruji) | 4 gap tersisa (F4–F7) | app_test.go, chatops_test.go + http_test.go, queue_test.go, http_integration_test.go |

Verifikasi akhir (koordinator, independen): `go vet ./...` PASS, `go test ./...` PASS (1,36 dtk), `go test -race ./...` PASS (6,5 dtk).
Total diff: 7 file diubah, 105 insertions, 49 deletions + 6 file tes baru. Coverage repo: 82,8% → 83,7% (internal/musehood 84,5% → 85,4%).

## Insiden selama audit (provider & VM)

- **VM restart ~12:18 WIB** di tengah skill 1: ketiga service (agentrouter-proxy, musehood-go, tunnlgg-tunnel) mati. Dipulihkan dari salinan persisten (`~/workspace/musehood-go/*.service`, `~/workspace/agentrouter-proxy/*.service`) → semua aktif, musehood lokal 200.
- **agentrouter/gpt-5.6-sol gagal 2x pada skill 1**: sesi pertama jalan 234 dtk (44 pesan, audit dikerjakan) tapi berhenti dengan text kosong dan tanpa file findings; resume sesi yang sama juga kosong. Lalu pindah fallback.
- **hc/DeepSeek-V4-Flash: 503** "No available channel" (masih down). **rs/glm-5.3-flash: gagal** ("unknown error" berbahasa Rusia).
- **xingchi/gpt-6.1-sol menjadi provider yang bekerja** untuk keempat skill (sesi 4–13 menit, semua selesai dengan file findings). Skill 1 dijalankan ulang penuh via xingchi; skill 2–4 langsung via xingchi.

---

## 1. golang-design-patterns (Review mode)

Sumber detail: `/tmp/audit-design-patterns.md`. Provider: xingchi/gpt-6.1-sol (~7 mnt).

### Bug konkret — semua direproduksi dulu, lalu diperbaiki

1. **High — export balapan dengan state aplikasi.** `internal/musehood/messages.go:215–218` (awal). `stateLocked()` menyalin nilai luar tapi tetap menunjuk slice/pesan/jobs/reactions yang bisa berubah; export melepas mutex sebelum JSON marshaling. Edit/reaction/clear bersamaan bisa race dengan serialisasi.
2. **High — export melewati redaksi secret.** `internal/musehood/messages.go:216–230`. Komentar export menjanjikan perilaku redaksi `/api/state`, tapi export hanya memanggil `stateLocked()`, bukan `safeLocked()`. API key yang tersimpan di nama/prompt/pesan/metadata lampiran bisa bocor ke export JSON dan Markdown.
3. **High — hapus kursi yang dipakai job belum selesai dibolehkan.** `internal/musehood/store.go:314–323`. Guard memeriksa status `running` (dipakai untuk step, bukan job); job berstatus `processing`/`waiting`. Engine bisa dereference kursi yang hilang saat step berikutnya jalan → potensi crash proses.
4. **High — hapus kursi setelah job selesai mencegah restart.** `internal/musehood/store.go:215–219,326–329`. Loading mengharuskan setiap step historis punya kursi; job selesai yang merujuk kursi custom yang dihapus membuat `data.json` gagal validasi saat restart.

### Perbaikan

- `messages.go:215–217` (akhir): marshal `safeLocked(stateLocked())` sambil memegang mutex — satu snapshot terlindungi + redaksi kredensial sebelum output dibuat. Mutex dilepas sebelum menulis respons HTTP.
- `store.go:315` (akhir): hapus kursi diblokir untuk semua job yang statusnya bukan `completed` (tetap HTTP 409).
- `store.go:215` (akhir): referensi kursi yang dihapus diizinkan dalam riwayat job yang sudah selesai; tetap ditegakkan untuk job belum selesai.
- Tes baru `internal/musehood/design_review_test.go`: `TestExportRedactsStoredSecrets`, `TestExportConcurrentMessageUpdates`, `TestDeleteSeatRejectsUnfinishedJobs`, `TestRestartAfterDeletingCompletedJobSeat`. Semua gagal sebelum perbaikan (race detector menemukan race di `messages.go:218`; export membocorkan kredensial; hapus mengembalikan nil bukan 409; reload gagal `kursi pekerjaan data.json tidak valid`) dan lolos sesudahnya.

### Observasi desain (tidak diubah — butuh kebijakan/restruktur, bukan bug)

5. Medium — job aktif dan konkurensi outbound tanpa batas (`engine.go:46–86`, `http.go:85–95`). Tiap pesan bisa menambah goroutine/panggilan upstream; timeout 60 dtk membatasi tiap panggilan, bukan jumlah bersamaan.
6. Medium — riwayat dan file queue selesai tanpa batas otomatis; tiap save menulis ulang seluruh file data; export mem-buffer seluruh state di memori (`messages.go:218–224,242–249,255–311`).
7. Medium — response HTTP tanpa `WriteTimeout` (`main.go:53`); header/read/idle timeout ada, tapi write tidak dibatasi.
8. Low — transport HTTP implisit bersama antar instance `App` (`http.go:87–95,151`); `App.Close()` menutup koneksi idle pool bersama.
9. Low — riwayat secret yang diingat tumbuh tanpa batas (`store.go:332–355`) — disengaja untuk redaksi kredensial lama; tidak di-cap.

### Dicek dan bukan defect

Tidak ada `func init()`; regex dikompilasi sekali di package level; outbound HTTP memakai timeout 60 dtk + bounded reads; body request/upload/download ada size cap; Muse menunggu tanpa batas itu disengaja (README + `TestMuseTimeoutAndRoundRobin`).

---

## 2. golang-error-handling (Audit mode, 5 kategori)

Sumber detail: `/tmp/audit-error-handling.md`. Provider: xingchi/gpt-6.1-sol (~5 mnt).

### Bug konkret — semua direproduksi dulu, lalu diperbaiki

1. **High — API key ter-URL-encode bisa lolos redaksi di connection error.** `protocols.go:143` (Gemini menaruh key ter-escape di URL), `protocols.go:93`, `store.go:339`, `http.go:226`. Transport failure menyertakan URL request di error; redaksi hanya key mentah + JSON escape, bukan bentuk query-encoded. **Perbaikan:** tambah `url.QueryEscape(key)` ke varian redaksi. Tes: `TestGeminiConnectionErrorRedactsEncodedKey` (gagal sebelum, lolos sesudah).
2. **Medium — mutasi pesan yang ditolak tetap teraplikasi setelah save gagal.** `messages.go:90` (edit), `:108,129` (delete), `:158,165` (reaction). State diubah dulu, lalu save error dikembalikan tanpa restore — klien dapat HTTP 500 padahal perubahan tetap terlihat; save berikutnya bisa menyimpan perubahan yang ditolak. **Perbaikan:** restore field/slice/task saat save gagal (clone reactions sebelum diubah). Tes: `TestMessageActionsPreserveStateWhenSaveFails` (5 subkasus).
3. **Medium — kegagalan terminal stream Responses dilaporkan sebagai jawaban sukses.** `gateway.go:313`. `response.failed` (termasuk nested `response.error`) diabaikan; event `type:"error"` juga diabaikan — teks parsial dikembalikan dengan error nil. **Perbaikan:** kembalikan nested provider error untuk `response.failed`; kembalikan error berisi payload failure untuk event `error`. Tes: `TestResponsesStreamPropagatesTerminalErrors`.
4. **Medium — kode kursi tersimpan bisa memicu panic regexp.** `store.go:193` (loader hanya mensyaratkan prefix `C`), `engine.go:30` (`regexp.MustCompile` dinamis). Kode seperti `C(` lolos loader lalu panic saat routing pesan. **Perbaikan:** `regexp.QuoteMeta` pada kode (seperti sudah dilakukan untuk nama). Tes: `TestRoutingLoadedSeatCodeDoesNotPanic`.

### Gap lain (dilaporkan saja, Low)

5. String error tidak konsisten lowercase/tanpa titik (`main.go:31`, `gateway.go:110,137,201,279`); pesan UI Indonesia di `problem(...)` memang kalimat lengkap.
6. Penyebab/konteks hilang saat error berjalan internal — `main.go:35,39,47,60`, `protocols.go:82,93,101`, `gateway.go:168,177,185`, `images.go:87,95` meratakan cause jadi string; `apiError` (`http.go:49`) tanpa `Unwrap`. Tidak ada caller yang salah klasifikasi karenanya — refactor model kausal tidak dilakukan.
7. Beberapa error queue/shutdown kehilangan visibilitas diagnostik (`queue.go:64,67`, `http.go:483`, `engine.go:214,234,245,299`, `main.go:65,66`). **Tidak ditemukan pelanggaran log-and-return.**
8. Helper JSON generik menekan error serialisasi (`store.go:367,371`, `http.go:235`, `messages.go:206`) — laten, tidak ada failure yang terjangkau.
9. Error cleanup/output best-effort diabaikan (`store.go:232`, `http.go:567,537`, `messages.go:182`, `uploads.go:101`, dll) — tidak ada korupsi yang terbukti.
10. Logging produksi tidak terstruktur (`log.Printf`, tanpa `slog`); tidak ada request logging terstruktur method/path/status/durasi. Tidak ditemukan logging kredensial/PII.

### Cakupan panic/recover

Tidak ada `panic(...)`/`recover()` eksplisit di produksi. Goroutine polling (`http.go:129`) dan worker (`engine.go:275`) tanpa recovery boundary — observasi, bukan bukti panic lain; recovery blanket tidak ditambahkan.

---

## 3. golang-performance (Review mode, arsitektur)

Sumber detail: `/tmp/audit-performance.md`. Provider: xingchi/gpt-6.1-sol (~13 mnt). Artefak benchmark/profil di `/tmp/musehood-performance-*.txt`, `/tmp/musehood-route*.cpu/.mem`.

### Temuan dan perbaikan (terukur)

**Routing mention meng-compile ulang satu regexp per kursi di setiap pesan chat.** Benchmark 7 kursi `routeLocked("@Opus please review this")`, baseline: **806.115 ns/op, 369.525 B/op, 416 allocs/op**. Profil CPU: 29,8% di `regexp/syntax.ranges.Less`; profil alokasi: 97,4% dari kompilasi regex.

**Perbaikan:** pola mention di-cache di `App`, dibangun ulang hanya bila jumlah/nama/kode kursi berubah. Matching case-insensitive dan aturan boundary dipertahankan; rename kursi me-refresh pola.

Sesudah perbaikan (5 sampel 1 detik): median **1.075 ns/op, 96 B/op, 1 alloc/op** — ~**750x lebih cepat**, 99,97% lebih sedikit byte alokasi. Tes baru: cache invalidation setelah rename kursi (`internal/musehood/engine_test.go`).

### Jalur lain yang diukur — tidak diubah

- `contextFor` (batas 80 pesan): 10,5 µs/op @100 pesan; 17,0 µs/op @10.000 pesan; 24.176 B/op, 8 allocs/op.
- Polling job selesai linear: 78 µs/op @10.000 job; 812 µs/op @100.000 job — kecil relatif terhadap interval poll 750 ms; tidak diubah.
- `stateLocked` + JSON: 144 µs/op, 29 KB/op @100 pesan vs 14,3 ms/op, 3,13 MB/op @10.000 pesan — skala riwayat untuk dipantau; file data lokal hanya 4 pesan/2 job (6.036 byte), bukan bottleneck saat ini.
- Lookup pesan linear: 46 µs/op @10.000 pesan — tidak ditambah index tanpa bukti bottleneck.

---

## 4. golang-testing (Audit mode, 3 concern)

Sumber detail: `/tmp/audit-testing.md`. Provider: xingchi/gpt-6.1-sol (~10 mnt).

### Coverage

| Scope | Baseline | Akhir (default) | Akhir (`-tags=integration`) |
|---|---:|---:|---:|
| `internal/musehood` | 84,5% (1.403/1.660) | 85,4% (1.418/1.660) | 85,5% (1.419/1.660) |
| Root/main | 0,0% (0/35) | 0,0% | 0,0% |
| Total repo | 82,8% (1.403/1.695) | 83,7% (1.418/1.695) | 83,7% (1.419/1.695) |

Tes top-level: 38 → 41 (default), 42 (tagged).

### Temuan yang diperbaiki

- **F1 — subtes mention tergantung urutan + parent `*testing.T`.** `app_test.go:229` (baseline). Satu fixture untuk semua baris tabel; baris mengharapkan `2*(i+1)` pesan sehingga memilih baris belakangan tanpa pendahulu timeout. `Fatal` di child dilaporkan ke parent. Direproduksi: `TestMentionFormatsAndProviderError/M011` FAIL (`state condition timed out`, `subtest may have called FailNow on a parent test`). **Perbaikan:** tiap subtes membuat fixture sendiri; ketujuh subtes dijalankan terpisah dan lolos.
- **F2 — penolakan SVG tanpa assertion.** `chatops_test.go:215`. Tes membuang kedua return value `upload("nakal.svg", ...)` — menerima SVG pun tidak gagal. **Perbaikan:** assert HTTP 400 untuk SVG + request multipart tanpa field image → HTTP 400 (cabang `messages.go:184` tadinya 0 hit).
- **F3 — boundary request/worker tidak teruji.** `http.go:256,263`, `queue.go:74,86,91,98,103,110,117`. Tes baru: `TestReadObject` (9 kasus: batas 256 KiB, null/array/malformed/trailing/wrong-type, 1 byte di atas batas, reader error) — `readObject` 54,5% → 100%. `TestReadWorkerReply` (6 kasus: ID invalid, file hilang, JSON tak lengkap, reply >16 MiB, read error) — 71,4% → 92,9%. `TestValidateWorkerLimits` (5 kasus: batas 100.000 unit UTF-16, over-limit, whitespace-only, image URL ditolak, truncasi error 30.000 rune) — 77,8% → 100%. `uploadImage` 68,2% → 72,7%.

### Temuan tersisa (dilaporkan saja)

- **F4 — perilaku image/startup belum teruji:** `resolveImage` 37,8% → 45,9% (download remote sukses/gagal, oversize, penolakan file lokal belum ter-cover); `generateImage` 63,2% (JSON malformed, Base64 invalid, tanpa data gambar); `parseAnthropicText` 57,9% (`content[].text`, `content_block_start`); `New` 70,6% (jalur gagal init storage/frontend); `run`/`main` 0% (tes binary terpisah `tests/binary.test.mjs`, tidak dijalankan di audit ini).
- **F5 — tes integrasi HTTP/aplikasi tanpa build tag:** `app_test.go:35`, `protocols_test.go:16`, `restart_test.go:16`. 10 file tes lama tanpa `//go:build integration`. Tes baru `http_integration_test.go` memakai tag; file lama tidak dirombak (helper bercampur dengan unit test).
- **F6 — helper polling tanpa deadline HTTP:** `app_test.go:124,149,187`, `chatops_test.go:28` memakai `http.DefaultClient` tanpa timeout; deadline 4 detik hanya dicek setelah request kembali. Tidak ada hang teramati. Rekomendasi: client/context ber-timeout milik fixture.
- **F7 — tidak ada detektor goroutine-leak suite-wide; tidak ada race terdeteksi.** Tes baru `TestCloseCancelsInFlightGateway` (tag integration): shutdown saat request gateway aktif, `Close` kembali dalam batas waktu, konteks dibatalkan, state sesudahnya 503, hasil telat tidak tersimpan. `go test -race ./...` dan `go test -tags=integration -race ./...` PASS. `go test ./... -shuffle=on -count=3` PASS.

---

## Verifikasi akhir koordinator (independen, 25 Nov ~13:00 WIB)

```text
$ go vet ./...        → PASS, no diagnostics
$ go test ./...       → ok  musehood-go/internal/musehood  1.363s
$ go test -race ./... → ok  musehood-go/internal/musehood  6.518s
$ git diff --check    → PASS (dilaporkan tiap skill)
$ git status          → 7 file diubah + 6 file tes baru, tidak ada commit
```

## File-file terkait

- Laporan ini: `~/workspace/musehood-go/skill-audit-20261125.md` (tidak di-commit)
- Per-skill: `/tmp/audit-design-patterns.md`, `/tmp/audit-error-handling.md`, `/tmp/audit-performance.md`, `/tmp/audit-testing.md`
- Log pi: `/tmp/pi-audit-*.log`; log reproduksi/benchmark: `/tmp/audit-*-*.log`, `/tmp/musehood-performance-*.txt`, `/tmp/musehood-route*.cpu/.mem`, `/tmp/audit-testing-*.cover`
