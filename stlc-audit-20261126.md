# Audit STLC + Skill Gabungan — 10 Fitur Baru (26 Nov 2026)

Perintah Arman: "gabungkan skill lainnya supaya akhirnya projek nya bagus".
Skill yang digabungkan: `software-testing-lifecycle` (STLC 7 fase) +
`golang-error-handling` + `golang-concurrency` + `golang-code-style` +
`svelte-core-bestpractices`. STLC juga ditambahkan ke rantai
`skill-combos` untuk project code.

## Fase 1 — Requirement Analysis
Requirement = 10 fitur pilihan Arman (pencarian, ekspor, multi-ruang, pin,
memori kursi, fallback kursi, voting, statistik, backup/restore, preset).
Ambiguitas yang ditemukan dan diputuskan:
- Pemilih voting: hanya kursi gateway (kursi antrean Muse tidak bisa
  menilai lewat jalur antrean) → ditolak 409 bila tidak ada pemilih.
- Statistik: hanya panggilan chat/gambar nyata; panggilan "Tes Model"
  tidak dihitung.
- Ruang tanpa kursi tercentang = semua kursi ikut (konsisten dialog + backend).

## Fase 2 — Test Planning
Scope: kode baru (`features.go`, perubahan store/engine/http/messages/
prompts/gateway/queue) + UI baru (+page.svelte, Settings.svelte, api.ts).
Strategi: unit test Go (termasuk `-race`), coverage per fungsi, live e2e
49 skenario, smoke test API di produksi. Exit criteria: semua tes hijau,
coverage fungsi baru ≥ 70%, tidak ada defect terbuka dari audit.

## Fase 3–5 — Desain, Lingkungan, Eksekusi
10 tes unit fitur + 4 tes regresi baru dari audit (total suite lulus,
`-race` bersih). Coverage awal fitur: 83,4% → akhir 84,4%;
`updateRoomLocked` 0% → 90%.

## Fase 6 — Defect ditemukan & diperbaiki (semua sudah retest)
1. **Voting**: kursi yang berubah jalur ke antrean Muse setelah voting
   dibuat bisa menjawab voting dengan prompt chat biasa → ditambah guard
   di `advanceLocked` (langkah voting kursi antrean dilewati dengan pesan
   jelas). Regresi: `TestVoteOnlyQueueAnswersRejected`.
2. **Restore**: pekerjaan cadangan dengan `index` di luar rentang langkah
   bisa lolos dan macet selamanya → validasi ditambah (400). Regresi:
   `TestRestoreRejectsInvalidJob`.
3. **UI ruang**: menyimpan ruang dengan semua kursi tercentang menulis
   daftar eksplisit; kursi yang ditambah belakangan jadi terlewat →
   kini dikirim `[]` (= semua kursi) bila semua tercentang
   (dialog ruang baru + Pengaturan).
4. **UI pencarian**: hasil pencarian server lama masih tampil saat
   pencarian dibuka lagi → dibersihkan di `toggleSearch`.
5. **Style**: `gofmt` pada engine.go, gateway.go, queue.go, features.go.
6. Celah tes ditutup: `TestUpdateRoom`, `TestPresetApplyNameCollisionRejected`.

Diperiksa dan dinyatakan aman (tanpa perubahan): penguncian `recordStat`
(tidak ada jalur chat di bawah lock), restore saat pekerjaan berjalan
(goroutine lama menulis ke data yatim, tidak mengotori data baru),
`tallyVotesLocked` di bawah lock, ekspor HTML ter-escape, pencarian
case-insensitive Unicode.

## Fase 7 — Test Closure
- `go test -race ./...` lulus; `go vet` bersih; `gofmt` bersih.
- Live e2e: 49/49 PASS. Produksi terverifikasi HTTP 200 (lokal + publik).
- Belum dites (dicatat jujur): voting dengan pemilih nyata di produksi
  (baru teruji unit dengan mock + jalur 409), restore cadangan > 32 MB
  (ditolak MaxBytesReader, belum diuji dengan file sebesar itu).
- Pelajaran: fungsi yang dipakai UI (updateRoom) sempat 0% coverage —
  coverage per fungsi wajib dicek untuk setiap endpoint baru.
