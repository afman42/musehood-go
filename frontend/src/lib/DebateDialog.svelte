<script lang="ts">
  import { dismissBackdrop, errorText } from './api';
  import type { Notice, Seat } from './types';
  import Toast from './Toast.svelte';

  let { seats, notice, onStart }: {
    seats: Seat[];
    notice: Notice | undefined;
    onStart: (seatA: string, seatB: string, topic: string) => Promise<void>;
  } = $props();

  let dialog: HTMLDialogElement;
  let seatA = $state('');
  let seatB = $state('');
  let topic = $state('');
  let starting = $state(false);

  const gatewaySeats = $derived(seats.filter((seat) => seat.provider === 'gateway'));

  export function open() {
    if (gatewaySeats.length >= 2) {
      seatA = gatewaySeats[0].code;
      seatB = gatewaySeats[1].code;
    }
    topic = '';
    if (!dialog.open) dialog.showModal();
  }

  async function start(event: SubmitEvent) {
    event.preventDefault();
    if (starting) return;
    starting = true;
    try {
      await onStart(seatA, seatB, topic.trim());
      dialog.close();
    } catch (error) {
      // Notifikasi ditampilkan pemanggil; dialog tetap terbuka.
      void errorText(error);
    } finally {
      starting = false;
    }
  }
</script>

<dialog bind:this={dialog} id="debateDialog" class="settings-dialog" aria-labelledby="debateTitle" onclick={(event) => dismissBackdrop(event, dialog)}>
  <header class="panel-header">
    <div><p class="eyebrow">MUSEHOOD</p><h2 id="debateTitle">Mode Debat</h2></div>
    <button class="icon-button" type="button" aria-label="Tutup debat" onclick={() => dialog.close()}>✕</button>
  </header>
  <form class="settings-form" onsubmit={start}>
    <p class="section-hint">Dua kursi gateway berdebat: pihak pro membuka, pihak kontra membantah, keduanya menyanggah, lalu kursi integrator menyimpulkan.</p>
    <label>Pihak pro
      <select bind:value={seatA} required>
        {#each gatewaySeats as seat (seat.code)}<option value={seat.code}>{seat.name} ({seat.code})</option>{/each}
      </select>
    </label>
    <label>Pihak kontra
      <select bind:value={seatB} required>
        {#each gatewaySeats as seat (seat.code)}<option value={seat.code}>{seat.name} ({seat.code})</option>{/each}
      </select>
    </label>
    <label>Topik debat
      <textarea bind:value={topic} maxlength="2000" rows="3" required placeholder="Contoh: Kopi lebih baik dari teh"></textarea>
    </label>
    {#if gatewaySeats.length < 2}<p class="field-hint">Debat butuh minimal dua kursi gateway.</p>{/if}
    <div class="settings-save">
      <button class="primary-button" type="submit" disabled={starting || gatewaySeats.length < 2 || seatA === seatB || !topic.trim()}>{starting ? 'Memulai…' : 'Mulai Debat'}</button>
    </div>
  </form>
  <Toast {notice} target="debateDialog" />
</dialog>
