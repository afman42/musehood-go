<script lang="ts">
  import type { State } from './types';

  let { current }: { current: State | undefined } = $props();

  const reactionRows = $derived.by(() => {
    const reactions = current?.reactions;
    if (!reactions) return [];
    return Object.entries(reactions.total)
      .map(([emoji, count]) => ({
        emoji,
        count,
        seats: Object.entries(reactions.bySeat)
          .filter(([, perEmoji]) => perEmoji[emoji])
          .map(([code, perEmoji]) => `${current?.seats.find((s) => s.code === code)?.name ?? code}: ${perEmoji[emoji]}`)
          .join(' · '),
      }))
      .sort((a, b) => b.count - a.count);
  });

  const ratingRows = $derived.by(() => {
    const ratings = current?.ratings;
    if (!ratings) return { seats: [], models: [] };
    const seats = Object.entries(ratings.bySeat)
      .map(([code, bucket]) => ({ label: `${current?.seats.find((s) => s.code === code)?.name ?? code} (${code})`, up: bucket.up, down: bucket.down }))
      .sort((a, b) => b.up + b.down - (a.up + a.down));
    const models = Object.entries(ratings.byModel)
      .map(([model, bucket]) => ({ label: model, up: bucket.up, down: bucket.down }))
      .sort((a, b) => b.up + b.down - (a.up + a.down));
    return { seats, models };
  });

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
  {#if ratingRows.seats.length || ratingRows.models.length}
    <h4>Penilaian kualitas balasan</h4>
    <div class="stats-table" role="table" aria-label="Statistik penilaian">
      <div class="stats-row stats-head" role="row"><span role="columnheader">Kursi / Model</span><span role="columnheader">👍</span><span role="columnheader">👎</span></div>
      {#each ratingRows.seats as row (row.label)}
        <div class="stats-row" role="row"><span role="cell">{row.label}</span><span role="cell">{row.up}</span><span role="cell">{row.down}</span></div>
      {/each}
      {#each ratingRows.models as row (row.label)}
        <div class="stats-row" role="row"><span role="cell">Model {row.label}</span><span role="cell">{row.up}</span><span role="cell">{row.down}</span></div>
      {/each}
    </div>
  {/if}
  {#if reactionRows.length}
    <h4>Reaksi</h4>
    <div class="stats-table" role="table" aria-label="Statistik reaksi">
      <div class="stats-row stats-head" role="row"><span role="columnheader">Emoji</span><span role="columnheader">Total</span><span role="columnheader">Per kursi penerima</span></div>
      {#each reactionRows as row (row.emoji)}
        <div class="stats-row" role="row"><span role="cell">{row.emoji}</span><span role="cell">{row.count}</span><span role="cell">{row.seats || '—'}</span></div>
      {/each}
    </div>
  {/if}
</section>
