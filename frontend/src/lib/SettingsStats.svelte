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
      <div class="stats-row stats-head" role="row"><span role="columnheader">Kursi / Model</span><span role="columnheader">Request</span><span role="columnheader">Berhasil</span><span role="columnheader">Gagal</span><span role="columnheader">Rata²</span><span role="columnheader">Token</span><span role="columnheader">Est. biaya</span></div>
      {#each rows as row (row.key)}
        <div class="stats-row" role="row"><span role="cell">{row.label}</span><span role="cell">{row.requests}</span><span role="cell">{row.success}</span><span role="cell">{row.failed}</span><span role="cell">{row.avgMs} mdtk</span><span role="cell">{row.promptTokens + row.completionTokens}</span><span role="cell">{row.estimatedCostUsd !== undefined ? `$${row.estimatedCostUsd.toFixed(4)}` : '—'}</span></div>
      {/each}
    </div>
  {/if}
</section>
