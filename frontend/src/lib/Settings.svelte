<script lang="ts">
  import { api, dismissBackdrop, errorText } from './api';
  import { tick } from 'svelte';
  import Toast from './Toast.svelte';
  import AvatarButton from './AvatarButton.svelte';
  import SettingsProviders from './SettingsProviders.svelte';
  import SettingsSeats from './SettingsSeats.svelte';
  import SettingsDiscussion from './SettingsDiscussion.svelte';
  import SettingsRooms from './SettingsRooms.svelte';
  import SettingsPresets from './SettingsPresets.svelte';
  import SettingsTemplates from './SettingsTemplates.svelte';
  import SettingsStats from './SettingsStats.svelte';
  import SettingsToolLog from './SettingsToolLog.svelte';
  import SettingsSecurity from './SettingsSecurity.svelte';
  import SettingsBackup from './SettingsBackup.svelte';
  import type { Notice, ReviewLevel, RoomEdit, SeatEdit, State } from './types';

  let { current, refresh, mutate, notify, notice }: {
    current: State | undefined;
    refresh: () => Promise<void>;
    mutate: (route: string, body: object) => Promise<void>;
    notify: (message: string, error?: boolean) => void;
    notice: Notice | undefined;
  } = $props();

  let dialog: HTMLDialogElement;
  let form: HTMLFormElement;
  let fileInput: HTMLInputElement;
  let providersPanel = $state<ReturnType<typeof SettingsProviders>>();
  let toolLogPanel = $state<ReturnType<typeof SettingsToolLog>>();
  let uploadTarget = '';
  let groupName = $state('');
  let ownerName = $state('');
  let roundRobin = $state(true);
  let demoFallback = $state(false);
  let reviewLevel = $state<ReviewLevel>('Sedang');
  let seats = $state<SeatEdit[]>([]);
  let roomEdits = $state<RoomEdit[]>([]);
  let integratorSeat = $state('MUSE');
  let toolsEnabled = $state(true);
  let codeToolEnabled = $state(true);
  let skillsEnabled = $state(true);
  let streamingEnabled = $state(true);
  let responseCacheEnabled = $state(false);
  let chainReplies = $state(false);
  let density = $state('normal');
  let theme = $state('light');
  let fontSize = $state('M');
  let saveStatus = $state('');
  let saving = $state(false);

  function syncSeats() {
    if (!current) return;
    seats = current.seats.map((seat) => ({
      code: seat.code,
      name: seat.name,
      role: seat.role,
      provider: seat.provider,
      ...(seat.model === undefined ? {} : { model: seat.model }),
      prompt: seat.prompt || seat.defaultPrompt || '',
      fallbackModel: '',
      fallbackModels: [seat.fallbackModel, ...(seat.fallbackModels ?? [])].filter(Boolean).join(', '),
      disabledTools: [...(seat.disabledTools ?? [])],
      memory: seat.memory ?? '',
      skills: (seat.skills ?? []).join(', '),
      autoMemory: seat.autoMemory ?? false,
      dailyBudgetUsd: seat.dailyBudgetUsd ?? 0,
      modelChat: seat.modelChat ?? '',
      modelTools: seat.modelTools ?? '',
      modelSummary: seat.modelSummary ?? '',
    }));
    integratorSeat = current.settings.integratorSeat || 'MUSE';
    roomEdits = (current.rooms ?? []).map((room) => ({
      id: room.id,
      name: room.name,
      seatCodes: room.seatCodes && room.seatCodes.length ? [...room.seatCodes] : current!.seats.map((s) => s.code),
      seatOrder: [...(room.seatOrder ?? [])],
    }));
  }

  export async function open() {
    try {
      if (!current) { await refresh(); await tick(); }
      if (!current) return;
      ({ name: groupName } = current.group);
      ({ name: ownerName } = current.owner);
      ({ roundRobin, demoFallback, reviewLevel } = current.settings);
      toolsEnabled = current.settings.toolsEnabled ?? true;
      codeToolEnabled = current.settings.codeToolEnabled ?? true;
      skillsEnabled = current.settings.skillsEnabled ?? true;
      streamingEnabled = current.settings.streamingEnabled ?? true;
      responseCacheEnabled = current.settings.responseCacheEnabled ?? false;
      chainReplies = current.settings.chainReplies ?? false;
      density = current.settings.density ?? 'normal';
      theme = current.settings.theme ?? (current.settings.darkMode ? 'dark' : 'light');
      fontSize = current.settings.fontSize ?? 'M';
      syncSeats();
      providersPanel?.resetForm();
      toolLogPanel?.reload();
      saveStatus = '';
      if (!dialog.open) dialog.showModal();
    } catch (error) { notify(errorText(error), true); }
  }

  async function save() {
    if (!form.reportValidity()) throw new Error('Lengkapi pengaturan yang belum valid.');
    saving = true;
    try {
      await mutate('/api/settings', {
        owner: { name: ownerName }, group: { name: groupName },
        seats: seats.map((s) => ({ ...s, skills: s.skills.split(',').map((x) => x.trim()).filter(Boolean), fallbackModels: s.fallbackModels.split(',').map((x) => x.trim()).filter(Boolean) })),
        settings: { roundRobin, demoFallback, reviewLevel, integratorSeat, toolsEnabled, codeToolEnabled, skillsEnabled, streamingEnabled, responseCacheEnabled, chainReplies, density, theme, fontSize },
      });
      providersPanel?.clearConnectionError();
      saveStatus = 'Tersimpan';
    } finally { saving = false; }
  }

  async function submit(event: SubmitEvent) {
    event.preventDefault();
    if (saving) return;
    try { await save(); notify('Pengaturan tersimpan.'); }
    catch (error) { notify(errorText(error), true); }
  }

  async function clear() {
    if (!window.confirm('Bersihkan semua pesan dan batalkan antrean aktif? Pengaturan tetap tersimpan.')) return;
    try { await mutate('/api/clear', {}); notify('Chat dibersihkan.'); }
    catch (error) { notify(errorText(error), true); }
  }

  function chooseAvatar(target: string) {
    uploadTarget = target;
    fileInput.value = '';
    fileInput.click();
  }

  async function upload() {
    const file = fileInput.files?.[0];
    if (!file) return;
    if (file.size > 2 * 1024 * 1024) { notify('Ukuran avatar maksimal 2 MB.', true); return; }
    const body = new FormData();
    body.set('target', uploadTarget);
    body.set('avatar', file);
    try {
      await api('/api/upload-avatar', body);
      await refresh();
      notify('Foto berhasil diperbarui.');
    } catch (error) { notify(errorText(error), true); }
  }
</script>

<dialog bind:this={dialog} id="settingsDialog" class="settings-dialog" aria-labelledby="settingsTitle"
  onclick={(event) => dismissBackdrop(event, dialog)}>
  <header class="panel-header">
    <div><p class="eyebrow">MUSEHOOD</p><h2 id="settingsTitle">Pengaturan grup</h2></div>
    <button id="closeSettings" class="icon-button" type="button" aria-label="Tutup pengaturan" onclick={() => dialog.close()}>✕</button>
  </header>
  <form bind:this={form} id="settingsForm" class="settings-form" onsubmit={submit}>
    <section class="settings-section">
      <h3>Skill kursi</h3><p class="section-hint">Saklar utama skill (SKILL.md). Nonaktif berarti skill yang dipilih per kursi tidak disisipkan ke instruksi kursi.</p>
      <label class="toggle-row"><span><strong>Skill kursi</strong><small>{skillsEnabled ? 'Aktif — skill per kursi dipakai saat menjawab.' : 'Nonaktif — pilihan skill tersimpan tapi tidak dipakai.'}</small></span><input id="skillsEnabled" type="checkbox" role="switch" bind:checked={skillsEnabled}></label>
    </section>
    <section class="settings-section">
      <h3>Identitas grup</h3><p class="section-hint">Satu pemilik, tujuh kursi. Semua berada di ruang yang sama.</p>
      <div class="identity-row">
        <AvatarButton target="group" src={current?.group.avatar ?? '/avatars/group.svg'} label="Ganti foto grup" onChoose={chooseAvatar} />
        <label>Nama grup<input id="groupNameInput" maxlength="80" required bind:value={groupName}></label>
      </div>
      <div class="identity-row">
        <AvatarButton target="owner" src={current?.owner.avatar ?? '/avatars/owner.svg'} label="Ganti foto pemilik" onChoose={chooseAvatar} />
        <label>Nama pemilik<input id="ownerName" maxlength="80" required bind:value={ownerName}></label>
      </div>
      <p class="field-hint">Foto: PNG, JPEG, GIF, atau WebP. Maksimal 2 MB.</p>
    </section>
    <SettingsProviders bind:this={providersPanel} {current} {refresh} {mutate} {notify} onSave={save} {saving} />
    <SettingsSeats bind:seats {current} {refresh} {notify} onSync={syncSeats} onSave={save} {saving} onChooseAvatar={chooseAvatar} {skillsEnabled} />
    <SettingsDiscussion bind:roundRobin bind:reviewLevel bind:integratorSeat bind:demoFallback bind:toolsEnabled bind:codeToolEnabled bind:streamingEnabled bind:responseCacheEnabled bind:chainReplies bind:density bind:theme bind:fontSize {seats} />
    <SettingsRooms bind:roomEdits {current} {refresh} {notify} onSync={syncSeats} />
    <SettingsPresets {current} {refresh} {notify} onSave={save} onSync={syncSeats} />
    <SettingsTemplates {current} {refresh} {notify} />
    <SettingsStats {current} />
    <SettingsToolLog bind:this={toolLogPanel} {notify} />
    <SettingsSecurity {current} {notify} {refresh} />
    <SettingsBackup {current} {refresh} {notify} onSync={syncSeats} />
    <section class="settings-section danger-section">
      <h3>Riwayat percakapan</h3><p class="section-hint">Hapus pesan dan batalkan antrean aktif di ruang yang sedang aktif saja. Pengaturan dan ruang lain tetap tersimpan.</p>
      <button id="clearChat" class="danger-button" type="button" onclick={clear}>Bersihkan chat ruang ini</button>
    </section>
    <div class="settings-save"><span id="saveStatus" role="status">{saveStatus}</span><button id="saveSettings" class="primary-button" type="submit" disabled={saving}>Simpan Pengaturan</button></div>
  </form>
  <Toast {notice} target="settingsDialog" />
</dialog>
<input id="avatarInput" type="file" accept="image/png,image/jpeg,image/gif,image/webp" hidden bind:this={fileInput} onchange={upload}>
