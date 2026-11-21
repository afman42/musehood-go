<script lang="ts">
  import { api, dismissBackdrop, errorText, type State, type ReviewLevel } from './api';
  import { tick } from 'svelte';

  let { current, refresh, mutate, notify }: {
    current: State | undefined;
    refresh: () => Promise<void>;
    mutate: (route: string, body: object) => Promise<void>;
    notify: (message: string, error?: boolean) => void;
  } = $props();

  let dialog: HTMLDialogElement;
  let form: HTMLFormElement;
  let fileInput: HTMLInputElement;
  let uploadTarget = '';
  let groupName = $state('');
  let ownerName = $state('');
  let baseUrl = $state('');
  let apiKey = $state('');
  let roundRobin = $state(true);
  let demoFallback = $state(false);
  let reviewLevel = $state<ReviewLevel>('Sedang');
  let seats = $state<Array<{ code: string; name: string; model?: string }>>([]);
  let saveStatus = $state('');
  let connectionError = $state('');
  let saving = $state(false);
  let testing = $state(false);
  const levels: ReviewLevel[] = ['Longgar', 'Santai', 'Sedang', 'Ketat'];

  export async function open() {
    try {
      if (!current) { await refresh(); await tick(); }
      if (!current) return;
      ({ name: groupName } = current.group);
      ({ name: ownerName } = current.owner);
      ({ baseUrl, roundRobin, demoFallback, reviewLevel } = current.settings);
      seats = current.seats.map(({ code, name, model }) => model === undefined ? { code, name } : { code, name, model });
      apiKey = '';
      connectionError = '';
      saveStatus = '';
      if (!dialog.open) dialog.showModal();
    } catch (error) { notify(errorText(error), true); }
  }

  async function save() {
    if (!form.reportValidity()) throw new Error('Lengkapi pengaturan yang belum valid.');
    saving = true;
    try {
      await mutate('/api/settings', {
        owner: { name: ownerName }, group: { name: groupName }, seats,
        settings: { baseUrl, ...(apiKey ? { apiKey } : {}), roundRobin, demoFallback, reviewLevel },
      });
      apiKey = '';
      connectionError = '';
      saveStatus = 'Tersimpan';
    } finally { saving = false; }
  }

  async function submit(event: SubmitEvent) {
    event.preventDefault();
    if (saving || testing) return;
    try { await save(); notify('Pengaturan tersimpan.'); }
    catch (error) { notify(errorText(error), true); }
  }

  async function testConnection() {
    testing = true;
    try {
      await save();
      await mutate('/api/test-connection', {});
      notify('Koneksi gateway berhasil diuji.');
    } catch (error) {
      connectionError = errorText(error);
      notify(connectionError, true);
    } finally { testing = false; }
  }

  async function removeKey() {
    try {
      await mutate('/api/settings', { settings: { clearApiKey: true } });
      apiKey = '';
      connectionError = '';
      notify('API key dihapus.');
    } catch (error) { notify(errorText(error), true); }
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

{#snippet avatar(target: string, src: string, label: string)}
  <button class="avatar-upload" type="button" data-upload={target} aria-label={label} onclick={() => chooseAvatar(target)}>
    <img data-avatar-target={target} {src} alt="" width="48" height="48"><span>Ganti</span>
  </button>
{/snippet}

<dialog bind:this={dialog} id="settingsDialog" class="settings-dialog" aria-labelledby="settingsTitle"
  onclick={(event) => dismissBackdrop(event, dialog)}>
  <header class="panel-header">
    <div><p class="eyebrow">MUSEHOOD</p><h2 id="settingsTitle">Pengaturan grup</h2></div>
    <button id="closeSettings" class="icon-button" type="button" aria-label="Tutup pengaturan" onclick={() => dialog.close()}>✕</button>
  </header>
  <form bind:this={form} id="settingsForm" class="settings-form" onsubmit={submit}>
    <section class="settings-section">
      <h3>Identitas grup</h3><p class="section-hint">Satu pemilik, tujuh kursi. Semua berada di ruang yang sama.</p>
      <div class="identity-row">
        {@render avatar('group', current?.group.avatar ?? '/avatars/group.svg', 'Ganti foto grup')}
        <label>Nama grup<input id="groupNameInput" maxlength="80" required bind:value={groupName}></label>
      </div>
      <div class="identity-row">
        {@render avatar('owner', current?.owner.avatar ?? '/avatars/owner.svg', 'Ganti foto pemilik')}
        <label>Nama pemilik<input id="ownerName" maxlength="80" required bind:value={ownerName}></label>
      </div>
      <p class="field-hint">Foto: PNG, JPEG, GIF, atau WebP. Maksimal 2 MB.</p>
    </section>
    <section class="settings-section">
      <h3>Koneksi gateway</h3><p class="section-hint">Untuk enam kursi AI. Muse selalu memakai pekerja bawaan melalui antrean file.</p>
      <label>Base URL<input id="baseUrl" type="url" placeholder="http://127.0.0.1:20128" autocomplete="off" spellcheck="false" bind:value={baseUrl}></label>
      <label>API key<input id="apiKey" type="password" placeholder={current?.settings.hasApiKey ? 'Biarkan kosong untuk mempertahankan key' : 'Masukkan API key gateway'} autocomplete="new-password" spellcheck="false" bind:value={apiKey}></label>
      <div class="key-row">
        <span id="savedKey" class="field-hint">{current?.settings.hasApiKey ? `Key tersimpan: ${current.settings.apiKey}` : 'Belum ada key tersimpan.'}</span>
        {#if current?.settings.hasApiKey}<button id="deleteKey" class="text-button danger-text" type="button" onclick={removeKey} disabled={saving || testing}>Hapus key</button>{/if}
      </div>
      <button id="testConnection" class="secondary-button" type="button" onclick={testConnection} disabled={testing || saving}>{testing ? 'Menguji…' : 'Tes Koneksi'}</button>
      {#if connectionError}
        <div id="connectionResult" class="connection-result error" role="status">{connectionError}</div>
      {:else if current?.connection}
        <div id="connectionResult" class="connection-result" role="status">Terhubung · {current.connection.models.length} model ditemukan. Periksa status setiap kursi di bawah.</div>
        <div id="modelList" class="model-list">{#each current.connection.models as model}<span class="model-tag">{model}</span>{/each}</div>
      {/if}
    </section>
    <section class="settings-section">
      <h3>Tujuh kursi AI</h3><p class="section-hint">Nama tampil dan model dapat diganti. Kode serta peran tetap.</p>
      <div id="seatSettings">
        {#each seats as item (item.code)}
          {@const member = current?.seats.find(seat => seat.code === item.code)}
          {@const model = item.model?.trim()}
          {@const available = model && current?.connection ? current.connection.models.includes(model) : undefined}
          <article class="seat-settings">
            <div class="seat-settings-heading">
              {@render avatar(item.code, member?.avatar ?? '/avatars/group.svg', `Ganti avatar ${member?.name ?? item.name}`)}
              <div>
                <p><span class="seat-code">{item.code}</span><span class="seat-fixed-role">{member?.role}</span></p>
                <label>Nama tampil<input aria-label={`Nama tampil ${item.code}`} maxlength="40" required data-seat-name={item.code} bind:value={item.name}></label>
              </div>
            </div>
            {#if item.code === 'MUSE'}
              <p class="builtin-note">Bawaan aplikasi · Antrean file · Tanpa model gateway</p>
            {:else}
              <label class="model-label">{item.code === 'M03' ? 'Nama model (chat / gambar)' : 'Nama model gateway'}
                <input aria-label={`Model ${item.code}`} maxlength="200" placeholder="Sesuai nama model di gateway" autocomplete="off" spellcheck="false" data-model={item.code} bind:value={item.model}>
              </label>
              <span class="model-status" data-model-status={item.code} data-available={available === undefined ? undefined : String(available)}>
                {!model ? 'Nama model belum diisi.' : available === undefined ? 'Tes Koneksi untuk memeriksa nama model ini.' : available ? '✓ Model tersedia di gateway.' : '⚠ Model tidak ditemukan di daftar gateway.'}
              </span>
            {/if}
          </article>
        {/each}
      </div>
    </section>
    <section class="settings-section">
      <h3>Cara berdiskusi</h3>
      <label class="toggle-row"><span><strong>Diskusi Bergilir</strong><small>Semua kursi membalas bergiliran. Jika mati, hanya Muse tanpa @mention.</small></span><input id="roundRobin" type="checkbox" role="switch" bind:checked={roundRobin}></label>
      <label>Level Review<select id="reviewLevel" bind:value={reviewLevel}>{#each levels as level}<option value={level}>{level}</option>{/each}</select></label>
      <p class="field-hint">Mengatur ketajaman pemeriksaan kursi Peninjau.</p>
      <label class="toggle-row"><span><strong>Fallback demo</strong><small>Simulasi berlabel DEMO jika chat gateway gagal. Tidak berlaku untuk Muse atau gambar.</small></span><input id="demoFallback" type="checkbox" role="switch" bind:checked={demoFallback}></label>
    </section>
    <section class="settings-section danger-section">
      <h3>Riwayat percakapan</h3><p class="section-hint">Hapus pesan dan batalkan antrean aktif. Pengaturan tetap tersimpan.</p>
      <button id="clearChat" class="danger-button" type="button" onclick={clear}>Bersihkan chat</button>
    </section>
    <div class="settings-save"><span id="saveStatus" role="status">{saveStatus}</span><button id="saveSettings" class="primary-button" type="submit" disabled={saving || testing}>Simpan Pengaturan</button></div>
  </form>
</dialog>
<input id="avatarInput" type="file" accept="image/png,image/jpeg,image/gif,image/webp" hidden bind:this={fileInput} onchange={upload}>
