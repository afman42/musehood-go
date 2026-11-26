<script lang="ts">
  import { dismissBackdrop } from './api';
  import Toast from './Toast.svelte';
  import type { Notice, Seat } from './types';

  let { seats, notice, onCreate }: {
    seats: Seat[];
    notice: Notice | undefined;
    onCreate: (name: string, seatCodes: string[]) => void;
  } = $props();

  let dialog: HTMLDialogElement;
  let newRoomName = $state('');
  let newRoomSeats = $state<string[]>([]);

  export function open() {
    newRoomName = '';
    newRoomSeats = seats.map(s => s.code);
    dialog.showModal();
  }

  export function close() {
    dialog.close();
  }

  function toggleRoomSeat(code: string, checked: boolean) {
    newRoomSeats = checked ? [...newRoomSeats, code] : newRoomSeats.filter(c => c !== code);
  }

  function submit(event: SubmitEvent) {
    event.preventDefault();
    onCreate(newRoomName, newRoomSeats.length === seats.length ? [] : newRoomSeats);
  }
</script>

<dialog bind:this={dialog} id="roomDialog" class="mention-dialog" aria-labelledby="roomTitle" onclick={(event) => dismissBackdrop(event, dialog)}>
  <div class="mention-heading"><h2 id="roomTitle">Ruang baru</h2><button class="icon-button" type="button" aria-label="Tutup" onclick={() => dialog.close()}>✕</button></div>
  <p>Ruang punya riwayat pesan dan susunan kursi sendiri. Kosongkan semua kursi untuk memakai semua kursi.</p>
  <form class="room-form" onsubmit={submit}>
    <label>Nama ruang<input bind:value={newRoomName} maxlength="80" required placeholder="Contoh: Coding" aria-label="Nama ruang baru"></label>
    <div class="room-seat-list">
      {#each seats as seat (seat.code)}
        <label class="room-seat-option"><input type="checkbox" checked={newRoomSeats.includes(seat.code)} onchange={(e) => toggleRoomSeat(seat.code, e.currentTarget.checked)}> {seat.name} <small>({seat.role})</small></label>
      {/each}
    </div>
    <div class="edit-actions">
      <button class="text-button" type="button" onclick={() => dialog.close()}>Batal</button>
      <button class="primary-button" type="submit">Buat ruang</button>
    </div>
  </form>
  <Toast {notice} target="roomDialog" />
</dialog>
