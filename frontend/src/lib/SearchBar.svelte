<script lang="ts">
  import type { SearchResult } from './types';

  let { query = $bindable(''), matchCount, matchIndex, serverResults, onInput, onPrev, onNext, onClose, onJumpResult }: {
    query?: string;
    matchCount: number;
    matchIndex: number;
    serverResults: SearchResult[];
    onInput: () => void;
    onPrev: () => void;
    onNext: () => void;
    onClose: () => void;
    onJumpResult: (result: SearchResult) => void;
  } = $props();

  let searchInput = $state<HTMLInputElement>();

  export function focus() {
    searchInput?.focus();
  }
</script>

<div class="search-bar" role="search">
  <input bind:this={searchInput} bind:value={query} type="search" placeholder="Cari dalam percakapan…" aria-label="Cari pesan" oninput={onInput}>
  <span class="search-count" aria-live="polite">{query.trim() ? `${matchCount ? matchIndex + 1 : 0}/${matchCount}` : ''}</span>
  <button class="icon-button" type="button" aria-label="Hasil sebelumnya" title="Sebelumnya" disabled={!matchCount} onclick={onPrev}>↑</button>
  <button class="icon-button" type="button" aria-label="Hasil berikutnya" title="Berikutnya" disabled={!matchCount} onclick={onNext}>↓</button>
  <button class="icon-button" type="button" aria-label="Tutup pencarian" title="Tutup" onclick={onClose}>✕</button>
</div>
{#if serverResults.length}
  <div class="search-results" aria-label="Hasil pencarian semua ruang">
    {#each serverResults as result (result.id)}
      <button class="search-result" type="button" onclick={() => onJumpResult(result)}>
        <span class="search-result-room">{result.roomName}</span>
        <span class="search-result-text"><strong>{result.author}:</strong> {result.text.slice(0, 120)}{result.text.length > 120 ? '…' : ''}</span>
      </button>
    {/each}
  </div>
{/if}
