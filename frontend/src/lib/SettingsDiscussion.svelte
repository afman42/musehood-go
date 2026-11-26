<script lang="ts">
  import type { ReviewLevel, SeatEdit } from './types';

  let {
    roundRobin = $bindable(true), reviewLevel = $bindable<ReviewLevel>('Sedang'),
    integratorSeat = $bindable('MUSE'), demoFallback = $bindable(false), toolsEnabled = $bindable(true), seats,
  }: {
    roundRobin?: boolean;
    reviewLevel?: ReviewLevel;
    integratorSeat?: string;
    demoFallback?: boolean;
    toolsEnabled?: boolean;
    seats: SeatEdit[];
  } = $props();

  const levels: ReviewLevel[] = ['Longgar', 'Santai', 'Sedang', 'Ketat'];
</script>

<section class="settings-section">
  <h3>Cara berdiskusi</h3>
  <label class="toggle-row"><span><strong>Diskusi Bergilir</strong><small>Semua kursi membalas bergiliran. Jika mati, hanya Muse tanpa @mention.</small></span><input id="roundRobin" type="checkbox" role="switch" bind:checked={roundRobin}></label>
  <label>Level Review<select id="reviewLevel" bind:value={reviewLevel}>{#each levels as level (level)}<option value={level}>{level}</option>{/each}</select></label>
  <p class="field-hint">Mengatur ketajaman pemeriksaan kursi Peninjau.</p>
  <label>Kursi integrator<select id="integratorSeat" bind:value={integratorSeat}>{#each seats as item (item.code)}<option value={item.code}>{item.name} ({item.code})</option>{/each}</select></label>
  <p class="field-hint">Kursi yang menutup pipeline "Tugas:" (Hasil Akhir - Integrasi) dan menjawab "Tanya Progres". Bawaan: Muse (MUSE).</p>
  <label class="toggle-row"><span><strong>Fallback demo</strong><small>Simulasi berlabel DEMO jika chat gateway gagal. Tidak berlaku untuk Muse atau gambar.</small></span><input id="demoFallback" type="checkbox" role="switch" bind:checked={demoFallback}></label>
  <label class="toggle-row"><span><strong>Tool untuk kursi gateway</strong><small>Kursi gateway dapat memakai tool saat menjawab: pencarian web, mengambil halaman web, membaca/menulis file di folder kerja (sandbox), dan waktu sekarang. Maksimal 3 putaran tool per balasan.</small></span><input id="toolsEnabled" type="checkbox" role="switch" bind:checked={toolsEnabled}></label>
</section>
