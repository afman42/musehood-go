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

  let allToolNames = $state<string[]>([]);
  let toolsLoaded = false;
  $effect(() => {
    if (toolsLoaded) return;
    toolsLoaded = true;
    api<{ tools: Array<{ name: string }>; builtin: string[] }>('/api/tools')
      .then((out) => { allToolNames = [...(out.builtin ?? []), ...(out.tools ?? []).map((t) => t.name)]; })
      .catch(() => { /* daftar kosong: bagian tool disembunyikan */ });
  });

  function toggleRoomDisabledTool(room: RoomEdit, name: string, disabled: boolean) {
    room.disabledTools = disabled
      ? [...room.disabledTools, name]
      : room.disabledTools.filter((t) => t !== name);
  }

  function toggleRoomSeatEdit(room: RoomEdit, code: string, checked: boolean) {
    room.seatCodes = checked ? [...room.seatCodes, code] : room.seatCodes.filter((c) => c !== code);
  }

  async function saveRoom(room: RoomEdit) {
    try {
      await api(`/api/rooms/${room.id}`, { name: room.name, seatCodes: room.seatCodes.length === (current?.seats.length ?? 0) ? [] : room.seatCodes, seatOrder: room.seatOrder, icon: room.icon, color: room.color, disabledTools: room.disabledTools });
      await refresh();
      onSync();
      notify('Ruang diperbarui.');
    } catch (error) { notify(errorText(error), true); }
  }

  function toggleOrderSeat(room: RoomEdit, code: string) {
    room.seatOrder = room.seatOrder.includes(code)
      ? room.seatOrder.filter((c) => c !== code)
      : [...room.seatOrder, code];
  }

  function seatName(code: string): string {
    return current?.seats.find((seat) => seat.code === code)?.name ?? code;
  }

  async function toggleArchive(room: RoomEdit, archived: boolean) {
    try {
      await api(`/api/rooms/${room.id}/archive`, { archived });
      await refresh();
      onSync();
      notify(archived ? 'Ruang diarsipkan.' : 'Ruang dipulihkan.');
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
    {@const roomState = current?.rooms.find((r) => r.id === room.id)}
    <article class="room-settings">
      <label>Nama ruang<input aria-label={`Nama ruang ${room.id}`} maxlength="80" bind:value={room.name}></label>
      <div class="room-icon-color">
        <label>Ikon ruang<input aria-label={`Ikon ruang ${room.id}`} maxlength="8" placeholder="💬" bind:value={room.icon}></label>
        <label>Warna ruang<input aria-label={`Warna ruang ${room.id}`} type="color" value={room.color || '#7c5cff'} oninput={(e) => room.color = e.currentTarget.value}></label>
        {#if room.color}<button class="text-button" type="button" onclick={() => room.color = ''}>Tanpa warna</button>{/if}
      </div>
      {#if allToolNames.length}
        <details class="tools-details">
          <summary>Tool dinonaktifkan di ruang ini ({room.disabledTools.length})</summary>
          <div class="tools-checks">
            {#each allToolNames as toolName (toolName)}
              <label class="check-label"><input type="checkbox" checked={room.disabledTools.includes(toolName)} onchange={(e) => toggleRoomDisabledTool(room, toolName, e.currentTarget.checked)}> {toolName}</label>
            {/each}
          </div>
          <span class="field-hint">Tool yang dicentang tidak ditawarkan ke kursi mana pun di ruang ini.</span>
        </details>
      {/if}
      {#if roomState?.archived}<p class="field-hint">🗄 Ruang ini terarsip dan tidak tampil di bilah ruang.</p>{/if}
      <div class="room-seat-list">
        {#each current?.seats ?? [] as seat (seat.code)}
          <label class="room-seat-option"><input type="checkbox" checked={room.seatCodes.includes(seat.code)} onchange={(e) => toggleRoomSeatEdit(room, seat.code, e.currentTarget.checked)}> {seat.name} <small>({seat.code})</small></label>
        {/each}
      </div>
      <div class="seat-order-editor">
        <p class="field-hint">Urutan bicara: klik kursi untuk menambah ke urutan, klik chip untuk menghapus. Kosong berarti urutan bawaan.</p>
        <div class="seat-order-chips">
          {#each room.seatOrder as code, index (code)}
            <button class="model-tag order-chip" type="button" title="Hapus dari urutan" onclick={() => toggleOrderSeat(room, code)}>{index + 1}. {seatName(code)} ✕</button>
          {/each}
          {#if room.seatOrder.length === 0}<span class="field-hint">Urutan bawaan</span>{/if}
        </div>
        <div class="seat-order-add">
          {#each current?.seats ?? [] as seat (seat.code)}
            {#if !room.seatOrder.includes(seat.code)}
              <button class="secondary-button order-add" type="button" onclick={() => toggleOrderSeat(room, seat.code)}>+ {seat.name}</button>
            {/if}
          {/each}
          {#if room.seatOrder.length}<button class="text-button" type="button" onclick={() => room.seatOrder = []}>Reset urutan</button>{/if}
        </div>
      </div>
      <div class="provider-form-actions">
        <button class="secondary-button" type="button" onclick={() => saveRoom(room)}>Simpan ruang</button>
        {#if roomState?.archived}
          <button class="secondary-button" type="button" onclick={() => toggleArchive(room, false)}>Pulihkan</button>
        {:else if roomState && !roomState.active}
          <button class="secondary-button" type="button" onclick={() => toggleArchive(room, true)}>Arsipkan</button>
        {/if}
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
