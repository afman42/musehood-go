<script lang="ts">
  import { api, apiDelete, errorText } from './api';
  import type { Room, RoomEdit, State } from './types';

  let { roomEdits = $bindable([]), current, refresh, notify, onSync }: {
    roomEdits?: RoomEdit[];
    current: State | undefined;
    refresh: () => Promise<void>;
    notify: (message: string, error?: boolean) => void;
    onSync: () => void;
  } = $props();

  let newRoomName = $state('');

  function toggleRoomSeatEdit(room: RoomEdit, code: string, checked: boolean) {
    room.seatCodes = checked ? [...room.seatCodes, code] : room.seatCodes.filter((c) => c !== code);
  }

  async function saveRoom(room: RoomEdit) {
    try {
      await api(`/api/rooms/${room.id}`, { name: room.name, seatCodes: room.seatCodes.length === (current?.seats.length ?? 0) ? [] : room.seatCodes });
      await refresh();
      onSync();
      notify('Ruang diperbarui.');
    } catch (error) { notify(errorText(error), true); }
  }

  async function addRoom() {
    const name = newRoomName.trim();
    if (!name) { notify('Nama ruang wajib diisi.', true); return; }
    try {
      await api('/api/rooms', { name, seatCodes: [] });
      newRoomName = '';
      await refresh();
      onSync();
      notify('Ruang ditambahkan.');
    } catch (error) { notify(errorText(error), true); }
  }

  async function removeRoom(room: Room) {
    if (!window.confirm(`Hapus ruang ${room.name} beserta semua pesannya?`)) return;
    try {
      await apiDelete(`/api/rooms/${room.id}`);
      await refresh();
      onSync();
      notify('Ruang dihapus.');
    } catch (error) { notify(errorText(error), true); }
  }
</script>

<section class="settings-section">
  <h3>Ruang grup</h3><p class="section-hint">Setiap ruang punya riwayat pesan dan susunan kursi sendiri. Centang kursi peserta ruang; semua tercentang berarti semua kursi ikut.</p>
  {#each roomEdits as room (room.id)}
    <article class="room-settings">
      <label>Nama ruang<input aria-label={`Nama ruang ${room.id}`} maxlength="80" bind:value={room.name}></label>
      <div class="room-seat-list">
        {#each current?.seats ?? [] as seat (seat.code)}
          <label class="room-seat-option"><input type="checkbox" checked={room.seatCodes.includes(seat.code)} onchange={(e) => toggleRoomSeatEdit(room, seat.code, e.currentTarget.checked)}> {seat.name} <small>({seat.code})</small></label>
        {/each}
      </div>
      <div class="provider-form-actions">
        <button class="secondary-button" type="button" onclick={() => saveRoom(room)}>Simpan ruang</button>
        {#if (current?.rooms.length ?? 0) > 1}<button class="text-button danger-text" type="button" onclick={() => { const target = current?.rooms.find((r) => r.id === room.id); if (target) removeRoom(target); }}>Hapus ruang</button>{/if}
      </div>
    </article>
  {/each}
  <div class="add-seat">
    <h4>Tambah ruang</h4>
    <label>Nama ruang<input aria-label="Nama ruang baru" maxlength="80" placeholder="Contoh: Riset" autocomplete="off" bind:value={newRoomName}></label>
    <button class="secondary-button" type="button" onclick={addRoom}>Tambah ruang</button>
  </div>
</section>
