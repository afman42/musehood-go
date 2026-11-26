<script lang="ts">
  import { dismissBackdrop } from './api';
  import Toast from './Toast.svelte';
  import type { Notice, Seat } from './types';

  let { seats, notice, onPick }: {
    seats: Seat[];
    notice: Notice | undefined;
    onPick: (seat: Seat) => void;
  } = $props();

  let dialog: HTMLDialogElement;

  export function open() {
    dialog.showModal();
  }
</script>

<dialog bind:this={dialog} id="mentionDialog" class="mention-dialog" aria-labelledby="mentionTitle" onclick={(event) => dismissBackdrop(event, dialog)}>
  <div class="mention-heading"><h2 id="mentionTitle">Pilih kursi</h2><button id="closeMention" class="icon-button" type="button" aria-label="Tutup daftar kursi" onclick={() => dialog.close()}>✕</button></div>
  <p>Hanya kursi yang disebut akan membalas.</p>
  <div id="mentionList">
    {#each seats as seat (seat.code)}
      <button class="mention-option" type="button" data-seat={seat.code} onclick={() => { dialog.close(); onPick(seat); }}>
        <img src={seat.avatar} alt={seat.name}><span><strong>{seat.name}</strong><small>{seat.role}</small></span><span>{seat.code}</span>
      </button>
    {/each}
  </div>
  <Toast {notice} target="mentionDialog" />
</dialog>
