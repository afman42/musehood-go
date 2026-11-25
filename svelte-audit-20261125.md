# Audit 2 Skill Svelte — Musehood Go Frontend (25 Nov 2026)

Repo: `~/workspace/musehood-go` (commit awal `be3a246`, working tree bersih).
Target: `frontend/` (fokus `frontend/src`), Svelte 5.59.1, SvelteKit 3.4.0, runes mode.
Metode: satu sesi pi FRESH per skill, `--no-skills`, brief Inggris singkat, mode Review/Audit dari tiap SKILL.md.
Baseline sebelum audit: `npm run check` 0 errors, 0 warnings.
Status akhir: **tidak ada commit, tidak ada push, tidak ada deploy**. Semua perubahan di working tree, Arman yang memutuskan.

## Ringkasan eksekutif

| Skill | Temuan konkret (diperbaiki) | Dilaporkan saja (tidak diubah) | Perubahan kode |
|---|---|---|---|
| 1. svelte-code-writer | **7 perbaikan** (6 `#each` tanpa key + 1 manipulasi DOM toast) | 2 saran advisory (ganti `bind:this`, ganti `class:`) | Settings.svelte, +page.svelte |
| 2. svelte-core-bestpractices | **1 perbaikan** (`$state` → `$state.raw` untuk respons API besar) | 11 area diperiksa, tidak ada pelanggaran | +page.svelte:6 |

Total diff kumulatif: 2 file diubah, 16 insertions, 18 deletions. Hanya `frontend/` yang berubah.

## 1. svelte-code-writer (provider: agentrouter/gpt-5.6-sol, ~7 mnt)

Alat skill: `npx @sveltejs/mcp svelte-autofixer <file> --svelte-version 5.59.1` pada tiap file `.svelte` di `frontend/src`.

### Perbaikan

- `frontend/src/lib/Settings.svelte:221` — `#each` opsi protokol gateway tanpa key. Tambah key `(value)`.
- `frontend/src/lib/Settings.svelte:234` — `#each` tag model provider tanpa key. Tambah key posisi `(index)`; nama model upstream tidak dijamin unik.
- `frontend/src/lib/Settings.svelte:294` — `#each` opsi level review tanpa key. Tambah key `(level)`.
- `frontend/src/routes/[...path]/+page.svelte:45-47` — manipulasi DOM langsung (`append`/`after`) memindahkan toast antar lokasi DOM milik Svelte. Diganti render deklaratif: `notify()` kini hanya mencatat `target` (id dialog yang terbuka atau `null`), dan tiga toast deklaratif dirender di tempatnya (utama, dialog Settings, dialog mention); state `notice` dioper ke `Settings.svelte` sebagai prop agar modalnya memiliki toast sendiri.
- `frontend/src/routes/[...path]/+page.svelte:373` — `#each` fragmen highlight pesan tanpa key. Tambah key posisi `(partIndex)`; nilai fragmen bisa berulang.
- `frontend/src/routes/[...path]/+page.svelte:393` — `#each` reaksi tanpa key. Tambah key `(reaction.emoji)`; backend menyimpan maksimal satu reaksi pemilik per emoji.
- `frontend/src/routes/[...path]/+page.svelte:405` — `#each` emoji picker tanpa key. Tambah key `(emoji)`.

### Dilaporkan saja (tidak diubah)

- Saran autofixer mengganti `bind:this` dengan actions/attachments dan mengganti directive `class:` — advisory saja, tidak ada cacat terverifikasi; audit melarang rewrite spekulatif/gaya saja.
- Tidak ada cacat konkret di `frontend/src/lib/api.ts`, `frontend/src/routes/[...path]/+page.ts`, atau `frontend/src/app.html`; dibiarkan.

### Verifikasi skill 1

- Re-run `svelte-autofixer` pada kedua file `.svelte`: **0 issues** (hanya saran advisory).
- `npm run check`: **0 errors, 0 warnings**.
- `npm run build`: lolos.
- `git diff --check -- frontend`: lolos.
- Verifikasi independen koordinator: diff dibaca langsung; id dialog `settingsDialog` cocok dengan logika target toast; `npm run check` 0/0.

## 2. svelte-core-bestpractices (provider: agentrouter/gpt-5.6-sol, ~9 mnt)

### Perbaikan

- `frontend/src/routes/[...path]/+page.svelte:6` — respons `/api/state` lengkap (termasuk riwayat pesan yang bisa besar) dipakai sebagai `$state` dalam (deep proxy). Nilai ini hanya diganti utuh setelah polling/mutasi; tidak ada field nested yang dimutasi di komponen. **Perbaikan:** `let current = $state<State>()` → `let current = $state.raw<State>()`, menghindari proxy dalam yang tidak perlu.
  - Verifikasi independen koordinator: grep tidak menemukan mutasi nested `current` di mana pun (hanya penugasan utuh `current = result` di baris 58), jadi `$state.raw` aman.

### Diperiksa, tidak perlu diubah

- `$state` lain: state UI primitif sudah reaktif dengan tepat. `seats` sengaja tetap deep `$state` karena binding form memutasi field nested kursi. `modelTests` kecil, dibiarkan.
- Nilai terhitung: `matches` dan `connectionReady` sudah memakai `$derived` (`+page.svelte:230-236`); tidak ada state terhitung yang diimplementasi lewat effect.
- Effect/browser guard: tidak ada `$effect`. Siklus polling memakai `onMount` dengan cleanup (pekerjaan mount/unmount sungguhan, bukan sinkronisasi state turunan). Tidak ada guard `if (browser)` di effect.
- Props: `Settings.svelte` memakai `$props`; pembacaan prop tetap reaktif; tidak ada nilai tergantung prop yang ditangkap secara non-reaktif.
- `#key`: tidak ada — tidak ada biaya remount paksa.
- Event: handler DOM memakai properti event Svelte 5 (`onclick`, `onsubmit`, `oninput`, dst.); tidak ada handler legacy `on:`.
- Snippet/slot: markup avatar reusable adalah snippet yang dirender `{@render}`; tidak ada slot legacy.
- Each block: semua loop sudah berkey (key dari audit skill 1 dipertahankan, tidak dikerjakan ulang).
- Sintaks legacy/berbagi state: tidak ada `export let`, `$:`, store legacy, `<slot>`, `<svelte:component>`, `<svelte:self>`, atau action. `export async function open()` adalah API ekspor komponen, bukan bentuk prop legacy `export let`.
- Context: tidak ada state reaktif bersama di modul, jadi context tidak diperlukan. `api.ts` hanya berisi type/fungsi/konstanta stateless.
- `api.ts`, `+page.ts`, `app.html`: tidak ada pelanggaran konkret.
- Perubahan audit skill 1 (keyed loop, toast deklaratif) dipertahankan, tidak dikerjakan ulang.

### Verifikasi skill 2

- `npm run check`: **0 errors, 0 warnings**.
- `npm run build`: lolos (Vite production build, adapter-static menulis `frontend/build`).
- Smoke test Chromium (oleh pi, terhadap instance lokal sementara dari frontend hasil audit): lolos — SPA termuat tanpa page/console error; input nama kursi, model, dan prompt di Settings ter-update dengan benar.
- Verifikasi independen koordinator: `npm run check` 0/0, `npm run build` lolos, `git diff --check` lolos.

## Insiden selama audit

Tidak ada. Kedua sesi pi jalan dengan provider pertama (agentrouter/gpt-5.6-sol), tanpa empty response, tanpa rate limit, tanpa VM restart. Tidak ada fallback provider yang dipakai (rs, agentrouter/gpt-6.1-sol, xingchi tidak diperlukan). atmorouter tidak dipakai (banned).

## Detail per skill

- `/tmp/audit-svelte-code-writer.md`
- `/tmp/audit-svelte-bestpractices.md`
