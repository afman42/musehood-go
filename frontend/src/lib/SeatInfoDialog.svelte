<script lang="ts">
  import { dismissBackdrop } from './api';
  import type { Seat } from './types';

  let { onMention }: {
    onMention: (seat: Seat) => void;
  } = $props();

  let dialog: HTMLDialogElement;
  let seat = $state<Seat | undefined>(undefined);

  export function open(next: Seat) {
    seat = next;
    dialog.showModal();
  }

  function providerLabel(s: Seat) {
    return s.provider === 'muse' ? 'Muse (antrean pekerja)' : 'Gateway';
  }

  function mentionCurrent() {
    if (!seat) return;
    dialog.close();
    onMention(seat);
  }
</script>

<dialog bind:this={dialog} id="seatInfoDialog" class="mention-dialog seat-info-dialog" aria-labelledby="seatInfoTitle" onclick={(event) => dismissBackdrop(event, dialog)}>
  {#if seat}
    <div class="mention-heading"><h2 id="seatInfoTitle">Info kursi</h2><button class="icon-button" type="button" aria-label="Tutup info kursi" onclick={() => dialog.close()}>✕</button></div>
    <div class="seat-info-head">
      <span class="seat-avatar-wrap"><img src={seat.avatar} alt={seat.name}><span class={['presence-dot', { offline: !seat.present }]} title={seat.present ? 'Hadir' : 'Tidak hadir'}></span></span>
      <div><strong>{seat.name}</strong><small>{seat.code} · {seat.role}</small></div>
    </div>
    <dl class="seat-info-list">
      <div><dt>Provider</dt><dd>{providerLabel(seat)}</dd></div>
      {#if seat.provider === 'gateway'}<div><dt>Model</dt><dd>{seat.model || '—'}</dd></div>{/if}
      {#if seat.fallbackModel || seat.fallbackModels?.length}<div><dt>Rantai fallback</dt><dd>{[seat.fallbackModel, ...(seat.fallbackModels ?? [])].filter(Boolean).join(' → ')}</dd></div>{/if}
      <div><dt>Status</dt><dd>{seat.present ? 'Hadir' : 'Tidak hadir'}</dd></div>
    </dl>
    {#if seat.skills?.length}
      <div class="seat-info-block"><span>Skill aktif</span><div class="seat-info-chips">{#each seat.skills as skill (skill)}<span class="skill-chip-static">{skill}</span>{/each}</div></div>
    {/if}
    {#if seat.memory}
      <div class="seat-info-block"><span>Memori</span><p>{seat.memory}</p></div>
    {/if}
    <div class="seat-info-actions">
      <button class="primary-button" type="button" onclick={mentionCurrent}>Mention @{seat.name}</button>
      <button class="secondary-button" type="button" onclick={() => dialog.close()}>Tutup</button>
    </div>
  {/if}
</dialog>
