<script lang="ts">
  import { api, apiDelete, errorText } from './api';
  import type { Room, RoomDocument, RoomEdit, State } from './types';

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

  // Peserta ruang (fitur 21).
  let participantInputs = $state<Record<string, string>>({});

  async function addParticipant(roomId: string) {
    const name = (participantInputs[roomId] ?? '').trim();
    if (!name) { notify('Nama peserta wajib diisi.', true); return; }
    try {
      await api(`/api/rooms/${roomId}/participants`, { name });
      participantInputs = { ...participantInputs, [roomId]: '' };
      await refresh();
      notify(`Peserta ${name} ditambahkan.`);
    } catch (error) { notify(errorText(error), true); }
  }

  async function removeParticipant(roomId: string, name: string) {
    try {
      await api(`/api/rooms/${roomId}/participants/remove`, { name });
      await refresh();
      notify(`Peserta ${name} dihapus.`);
    } catch (error) { notify(errorText(error), true); }
  }

  // Dokumen ruang (fitur 26).
  let documents = $state<Record<string, RoomDocument[]>>({});
  let uploadingDoc = $state('');

  async function loadDocuments(roomId: string) {
    try {
      const out = await api<{ documents: RoomDocument[] }>(`/api/rooms/${roomId}/documents`);
      documents = { ...documents, [roomId]: out.documents ?? [] };
    } catch (error) { notify(errorText(error), true); }
  }

  async function uploadDocument(roomId: string, event: Event) {
    const input = event.currentTarget as HTMLInputElement;
    const file = input.files?.[0];
    input.value = '';
    if (!file) return;
    if (file.size > 5 * 1024 * 1024) { notify('Dokumen maksimal 5 MB.', true); return; }
    uploadingDoc = roomId;
    try {
      const form = new FormData();
      form.set('file', file);
      await api(`/api/rooms/${roomId}/documents`, form);
      await loadDocuments(roomId);
      notify(`Dokumen ${file.name} terlampir.`);
    } catch (error) { notify(errorText(error), true); }
    finally { uploadingDoc = ''; }
  }

  async function deleteDocument(roomId: string, doc: RoomDocument) {
    if (!window.confirm(`Hapus dokumen ${doc.name} dari ruang ini?`)) return;
    try {
      await api(`/api/rooms/${roomId}/documents/${doc.id}/delete`, {});
      await loadDocuments(roomId);
      notify('Dokumen dihapus.');
    } catch (error) { notify(errorText(error), true); }
  }

  function exportMarkdown(roomId: string) {
    window.open(`/api/export?format=markdown&room=${encodeURIComponent(roomId)}`, '_blank');
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
      <div class="participants-editor">
        <p class="field-hint">Peserta ruang (ruang bersama): orang lain yang bisa chat dengan namanya sendiri lewat pemilih pengirim di kotak pesan.</p>
        <div class="seat-order-chips">
          {#each roomState?.participants ?? [] as participant (participant)}
            <button class="model-tag order-chip" type="button" title="Hapus peserta" onclick={() => removeParticipant(room.id, participant)}>{participant} ✕</button>
          {/each}
          {#if (roomState?.participants ?? []).length === 0}<span class="field-hint">Belum ada peserta</span>{/if}
        </div>
        <div class="participant-add">
          <input aria-label={`Nama peserta baru ${room.id}`} maxlength="40" placeholder="Nama peserta" autocomplete="off" bind:value={participantInputs[room.id]}>
          <button class="secondary-button" type="button" onclick={() => addParticipant(room.id)}>Tambah peserta</button>
        </div>
      </div>
      <div class="documents-editor">
        <p class="field-hint">Dokumen terlampir (maks 5 MB, teks/PDF/Markdown/CSV): kutipan relevan disisipkan otomatis ke konteks kursi di ruang ini.</p>
        {#if documents[room.id] === undefined}
          <button class="secondary-button" type="button" onclick={() => loadDocuments(room.id)}>Lihat dokumen ruang</button>
        {:else}
          {#each documents[room.id] as doc (doc.id)}
            <p class="document-row"><span>📄 {doc.name} <span class="field-hint">({doc.chunks} potongan)</span></span><button class="text-button danger-text" type="button" onclick={() => deleteDocument(room.id, doc)}>Hapus</button></p>
          {/each}
          {#if documents[room.id].length === 0}<p class="field-hint">Belum ada dokumen.</p>{/if}
        {/if}
        <label class="secondary-button file-button">{uploadingDoc === room.id ? 'Mengunggah…' : 'Lampirkan dokumen'}<input type="file" accept=".txt,.md,.markdown,.csv,.pdf" hidden onchange={(e) => uploadDocument(room.id, e)}></label>
      </div>
      <div class="provider-form-actions">
        <button class="secondary-button" type="button" onclick={() => saveRoom(room)}>Simpan ruang</button>
        <button class="secondary-button" type="button" onclick={() => exportMarkdown(room.id)}>Ekspor Markdown</button>
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
