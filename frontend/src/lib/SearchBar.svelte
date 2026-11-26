<script lang="ts">
  import { t } from './i18n.svelte';
  import { emptySearchFilters, type Room, type SearchFilters, type SearchResult, type Seat } from './types';

  let { query = $bindable(''), filters = $bindable(emptySearchFilters()), seats = [], rooms = [], matchCount, matchIndex, serverResults, onInput, onPrev, onNext, onClose, onJumpResult }: {
    query?: string;
    filters?: SearchFilters;
    seats?: Seat[];
    rooms?: Room[];
    matchCount: number;
    matchIndex: number;
    serverResults: SearchResult[];
    onInput: () => void;
    onPrev: () => void;
    onNext: () => void;
    onClose: () => void;
    onJumpResult: (result: SearchResult) => void;
  } = $props();

  const filterActive = $derived(Boolean(filters.seat || filters.room || filters.from || filters.to || filters.hasImage));

  function clearFilters() {
    filters = emptySearchFilters();
    onInput();
  }

  let searchInput = $state<HTMLInputElement>();

  export function focus() {
    searchInput?.focus();
  }
</script>

<div class="search-bar" role="search">
  <input bind:this={searchInput} bind:value={query} type="search" placeholder={t('search')} aria-label="Cari pesan" oninput={onInput}>
  <span class="search-count" aria-live="polite">{query.trim() ? `${matchCount ? matchIndex + 1 : 0}/${matchCount}` : ''}</span>
  <button class="icon-button" type="button" aria-label="Hasil sebelumnya" title="Sebelumnya" disabled={!matchCount} onclick={onPrev}>↑</button>
  <button class="icon-button" type="button" aria-label="Hasil berikutnya" title="Berikutnya" disabled={!matchCount} onclick={onNext}>↓</button>
  <button class="icon-button" type="button" aria-label="Tutup pencarian" title="Tutup" onclick={onClose}>✕</button>
</div>
<details class="search-filters" open={filterActive}>
  <summary>{t('filters')}{filterActive ? ' (aktif)' : ''}</summary>
  <div class="search-filter-grid">
    <label>Pengirim
      <select aria-label="Filter pengirim" bind:value={filters.seat} onchange={onInput}>
        <option value="">Semua</option>
        <option value="owner">Pemilik</option>
        {#each seats as seat (seat.code)}<option value={seat.code}>{seat.name}</option>{/each}
      </select>
    </label>
    <label>Ruang
      <select aria-label="Filter ruang" bind:value={filters.room} onchange={onInput}>
        <option value="">{t('allRooms')}</option>
        {#each rooms as room (room.id)}<option value={room.id}>{room.name}</option>{/each}
      </select>
    </label>
    <label>Dari tanggal<input aria-label="Dari tanggal" type="date" bind:value={filters.from} onchange={onInput}></label>
    <label>Sampai tanggal<input aria-label="Sampai tanggal" type="date" bind:value={filters.to} onchange={onInput}></label>
    <label class="check-label"><input type="checkbox" bind:checked={filters.hasImage} onchange={onInput}> {t('hasImage')}</label>
    {#if filterActive}<button class="text-button" type="button" onclick={clearFilters}>{t('clearFilters')}</button>{/if}
  </div>
</details>
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
