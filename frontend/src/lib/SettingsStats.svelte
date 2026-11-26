<script lang="ts">
  import type { State } from './types';

  let { current }: { current: State | undefined } = $props();

  const rows = $derived.by(() => {
    const stats = current?.stats ?? {};
    return Object.entries(stats).map(([key, st]) => {
      const isSeat = key.startsWith('seat:');
      const label = isSeat
        ? (current?.seats.find((s) => s.code === key.slice(5))?.name ?? key.slice(5)) + ` (${key.slice(5)})`
        : key.slice(6);
      return { key, label, isSeat, ...st, avgMs: st.requests ? Math.round(st.totalLatencyMs / st.requests) : 0 };
    }).sort((a, b) => Number(b.isSeat) - Number(a.isSeat) || b.requests - a.requests);
  });
</script>

<section class="settings-section">
  <h3>Statistik pemakaian</h3><p class="section-hint">Jumlah panggilan gateway per kursi dan per model sejak statistik mulai dicatat.</p>
  {#if rows.length === 0}
    <p class="field-hint">Belum ada panggilan gateway yang tercatat.</p>
  {:else}
    <div class="stats-table" role="table" aria-label="Statistik pemakaian">
      <div class="stats-row stats-head" role="row"><span>Kursi / Model</span><span>Request</span><span>Berhasil</span><span>Gagal</span><span>Rata²</span><span>Token</span></div>
      {#each rows as row (row.key)}
        <div class="stats-row" role="row"><span>{row.label}</span><span>{row.requests}</span><span>{row.success}</span><span>{row.failed}</span><span>{row.avgMs} mdtk</span><span>{row.promptTokens + row.completionTokens}</span></div>
      {/each}
    </div>
  {/if}
</section>
