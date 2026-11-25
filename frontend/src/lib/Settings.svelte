<script lang="ts">
  import { api, apiDelete, dismissBackdrop, errorText, protocolLabels, type GatewayProvider, type State, type ReviewLevel, type GatewayProtocol } from './api';
  import { tick } from 'svelte';

  let { current, refresh, mutate, notify, notice }: {
    current: State | undefined;
    refresh: () => Promise<void>;
    mutate: (route: string, body: object) => Promise<void>;
    notify: (message: string, error?: boolean) => void;
    notice: { text: string; error: boolean; target: string | null } | undefined;
  } = $props();

  let dialog: HTMLDialogElement;
  let form: HTMLFormElement;
  let fileInput: HTMLInputElement;
  let uploadTarget = '';
  let groupName = $state('');
  let ownerName = $state('');
  let roundRobin = $state(true);
  let demoFallback = $state(false);
  let reviewLevel = $state<ReviewLevel>('Sedang');
  let provName = $state('');
  let provAlias = $state('');
  let provBaseUrl = $state('');
  let provProtocol = $state<GatewayProtocol>('openai');
  let provApiKey = $state('');
  let editingProviderId = $state<string | null>(null);
  let fetchedModels = $state<Record<string, { models: string[]; testedAt: string }>>({});
  let providerTests = $state<Record<string, Record<string, { ok: boolean; latencyMs?: number; reply?: string; error?: string }>>>({});
  let fetchingProvider = $state<string | null>(null);
  let testingProvider = $state<string | null>(null);
  let savingProvider = $state(false);
  let seats = $state<Array<{ code: string; name: string; role: string; provider: 'muse' | 'gateway'; model?: string; prompt: string }>>([]);
  let integratorSeat = $state('MUSE');
  let newSeatName = $state('');
  let newSeatRole = $state('');
  let newSeatModel = $state('');
  let addingSeat = $state(false);
  let saveStatus = $state('');
  let connectionError = $state('');
  let saving = $state(false);
  let testing = $state(false);
  let testingModel = $state<string | null>(null);
  let modelTests = $state<Record<string, { ok: boolean; latencyMs?: number; reply?: string; error?: string }>>({});
  const levels: ReviewLevel[] = ['Longgar', 'Santai', 'Sedang', 'Ketat'];

  function syncSeats() {
    if (!current) return;
    seats = current.seats.map((seat) => ({
      code: seat.code,
      name: seat.name,
      role: seat.role,
      provider: seat.provider,
      ...(seat.model === undefined ? {} : { model: seat.model }),
      prompt: seat.prompt || seat.defaultPrompt || '',
    }));
    integratorSeat = current.settings.integratorSeat || 'MUSE';
  }

  function activeProvider(): GatewayProvider | undefined {
    return current?.providers.find((provider) => provider.active);
  }

  // Label model dengan awalan alias provider (mis. "xc/gpt-6.1-sol") agar
  // model bernama sama dari provider lain mudah dibedakan. Hanya tampilan;
  // nama model yang dikirim ke gateway tetap apa adanya.
  function modelLabel(provider: GatewayProvider | undefined, model: string): string {
    return provider?.alias ? `${provider.alias}/${model}` : model;
  }

  export async function open() {
    try {
      if (!current) { await refresh(); await tick(); }
      if (!current) return;
      ({ name: groupName } = current.group);
      ({ name: ownerName } = current.owner);
      ({ roundRobin, demoFallback, reviewLevel } = current.settings);
      syncSeats();
      resetProviderForm();
      connectionError = '';
      saveStatus = '';
      if (!dialog.open) dialog.showModal();
    } catch (error) { notify(errorText(error), true); }
  }

  function resetProviderForm() {
    provName = '';
    provAlias = '';
    provBaseUrl = '';
    provProtocol = 'openai';
    provApiKey = '';
    editingProviderId = null;
  }

  function editProvider(provider: GatewayProvider) {
    editingProviderId = provider.id;
    provName = provider.name;
    provAlias = provider.alias ?? '';
    provBaseUrl = provider.baseUrl;
    provProtocol = provider.protocol;
    provApiKey = '';
  }

  async function saveProvider() {
    const name = provName.trim();
    const baseUrl = provBaseUrl.trim();
    if (!name || !baseUrl) { notify('Nama dan Base URL provider wajib diisi.', true); return; }
    savingProvider = true;
    try {
      const body: Record<string, unknown> = { name, alias: provAlias.trim(), baseUrl, protocol: provProtocol };
      if (provApiKey) body.apiKey = provApiKey;
      if (editingProviderId) {
        await api(`/api/providers/${editingProviderId}`, body);
        notify('Provider diperbarui.');
      } else {
        await api('/api/providers', body);
        notify('Provider ditambahkan.');
      }
      resetProviderForm();
      await refresh();
    } catch (error) { notify(errorText(error), true); }
    finally { savingProvider = false; }
  }

  async function deleteProvider(provider: GatewayProvider) {
    if (!window.confirm(`Hapus provider ${provider.name}? Kursi akan memakai provider aktif berikutnya.`)) return;
    try {
      await apiDelete(`/api/providers/${provider.id}`);
      await refresh();
      notify('Provider dihapus.');
    } catch (error) { notify(errorText(error), true); }
  }

  async function activateProvider(provider: GatewayProvider) {
    try {
      await mutate(`/api/providers/${provider.id}/activate`, {});
      notify(`Provider aktif: ${provider.name}.`);
    } catch (error) { notify(errorText(error), true); }
  }

  async function clearProviderKey(provider: GatewayProvider) {
    if (!window.confirm(`Hapus API key provider ${provider.name}?`)) return;
    try {
      await api(`/api/providers/${provider.id}`, { clearApiKey: true });
      await refresh();
      notify('API key provider dihapus.');
    } catch (error) { notify(errorText(error), true); }
  }

  async function fetchModels(provider: GatewayProvider) {
    if (fetchingProvider !== null) return;
    fetchingProvider = provider.id;
    try {
      const out = await api<{ providerId: string; models: string[]; testedAt: string }>(`/api/providers/${provider.id}/models`);
      fetchedModels = { ...fetchedModels, [provider.id]: { models: out.models, testedAt: out.testedAt } };
      await refresh();
      notify(`${out.models.length} model ditemukan di ${provider.name}.`);
    } catch (error) { notify(errorText(error), true); }
    finally { fetchingProvider = null; }
  }

  async function testModels(provider: GatewayProvider) {
    const models = fetchedModels[provider.id]?.models ?? [];
    if (models.length === 0) { notify('Fetch Models dulu sebelum dites.', true); return; }
    if (testingProvider !== null) return;
    testingProvider = provider.id;
    try {
      const out = await api<{ providerId: string; results: Array<{ model: string; ok: boolean; latencyMs?: number; reply?: string; error?: string }> }>(`/api/providers/${provider.id}/test-models`, { models: models.slice(0, 10) });
      const perModel: Record<string, { ok: boolean; latencyMs?: number; reply?: string; error?: string }> = {};
      for (const result of out.results) perModel[result.model] = result;
      providerTests = { ...providerTests, [provider.id]: perModel };
      const passed = out.results.filter((result) => result.ok).length;
      notify(`${passed} dari ${out.results.length} model berfungsi di ${provider.name}.`, passed < out.results.length);
    } catch (error) { notify(errorText(error), true); }
    finally { testingProvider = null; }
  }

  async function addSeat() {
    if (addingSeat) return;
    const name = newSeatName.trim();
    const role = newSeatRole.trim();
    const model = newSeatModel.trim();
    if (!name || !role) { notify('Nama dan peran kursi baru wajib diisi.', true); return; }
    addingSeat = true;
    try {
      await api('/api/seats', { name, role, model });
      newSeatName = '';
      newSeatRole = '';
      newSeatModel = '';
      await refresh();
      syncSeats();
      notify('Kursi baru ditambahkan.');
    } catch (error) { notify(errorText(error), true); }
    finally { addingSeat = false; }
  }

  async function removeSeat(code: string, name: string) {
    if (!window.confirm(`Hapus kursi ${name}? Kursi bawaan tidak dapat dihapus.`)) return;
    try {
      await apiDelete(`/api/seats/${code}`);
      await refresh();
      syncSeats();
      notify('Kursi dihapus.');
    } catch (error) { notify(errorText(error), true); }
  }

  async function save() {
    if (!form.reportValidity()) throw new Error('Lengkapi pengaturan yang belum valid.');
    saving = true;
    try {
      await mutate('/api/settings', {
        owner: { name: ownerName }, group: { name: groupName }, seats,
        settings: { roundRobin, demoFallback, reviewLevel, integratorSeat },
      });
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

  async function testModel(code: string) {
    const item = seats.find((seat) => seat.code === code);
    const model = item?.model?.trim();
    if (!model) { notify('Isi nama model dulu sebelum dites.', true); return; }
    if (testingModel !== null) return;
    testingModel = code;
    try {
      await save();
      const out = await api<{ ok: boolean; model: string; latencyMs: number; reply: string; testedAt: string }>('/api/test-model', { model });
      modelTests = { ...modelTests, [code]: { ok: true, latencyMs: out.latencyMs, reply: out.reply } };
      notify(`Model ${model} berfungsi.`);
    } catch (error) {
      const message = errorText(error);
      modelTests = { ...modelTests, [code]: { ok: false, error: message } };
      notify(message, true);
    } finally { testingModel = null; }
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
      <h3>Provider Gateway</h3><p class="section-hint">Kelola gateway kustom untuk kursi AI. Provider aktif dipakai semua kursi. Muse selalu memakai pekerja bawaan melalui antrean file.</p>
      {#if (current?.providers ?? []).length === 0}
        <p class="field-hint">Belum ada provider. Tambahkan satu di bawah.</p>
      {:else}
        <div id="providerList">
          {#each current?.providers ?? [] as provider (provider.id)}
            <article class="provider-row">
              <div class="provider-info">
                <p><strong>{provider.name}</strong> {#if provider.alias}<span class="model-tag">{provider.alias}</span>{/if} {#if provider.active}<span class="provider-active">Aktif</span>{/if}</p>
                <p class="field-hint">{provider.baseUrl} · {protocolLabels[provider.protocol]} · {provider.hasApiKey ? 'Key tersimpan' : 'Tanpa key'}</p>
              </div>
              <div class="provider-actions">
                {#if !provider.active}<button class="secondary-button" type="button" onclick={() => activateProvider(provider)}>Aktifkan</button>{/if}
                <button class="secondary-button" type="button" onclick={() => editProvider(provider)}>Edit</button>
                <button class="secondary-button" type="button" onclick={() => fetchModels(provider)} disabled={fetchingProvider !== null}>{fetchingProvider === provider.id ? 'Mengambil…' : 'Fetch Models'}</button>
                <button class="secondary-button" type="button" onclick={() => testModels(provider)} disabled={testingProvider !== null || (fetchedModels[provider.id]?.models.length ?? 0) === 0}>{testingProvider === provider.id ? 'Menguji…' : 'Tes Models'}</button>
                <button class="text-button danger-text" type="button" onclick={() => deleteProvider(provider)}>Hapus</button>
              </div>
              {#if fetchedModels[provider.id]}
                <div class="model-list">{#each fetchedModels[provider.id].models as model (model)}<span class="model-tag">{modelLabel(provider, model)}</span>{/each}</div>
                {#if providerTests[provider.id]}
                  <div class="provider-test-results">
                    {#each Object.entries(providerTests[provider.id]) as [model, result] (model)}
                      <p class="model-status {result.ok ? 'test-ok' : 'test-fail'}" data-provider-test={provider.id + ':' + model}>{result.ok ? `✓ ${modelLabel(provider, model)} · ${result.latencyMs} mdtk · “${result.reply}”` : `✗ ${modelLabel(provider, model)} · ${result.error}`}</p>
                    {/each}
                  </div>
                {/if}
              {/if}
            </article>
          {/each}
        </div>
      {/if}
      <div class="add-provider">
        <h4>{editingProviderId ? 'Edit provider' : 'Tambah provider'}</h4>
        <label>Nama provider<input aria-label="Nama provider" maxlength="80" placeholder="Contoh: Xingchi" autocomplete="off" bind:value={provName}></label>
        <label>Alias (label pembeda model)<input aria-label="Alias provider" maxlength="40" placeholder="Contoh: xc" autocomplete="off" spellcheck="false" bind:value={provAlias}></label>
        <span class="field-hint">Ditampilkan sebagai awalan nama model (mis. xc/gpt-6.1-sol) agar model provider ini mudah dibedakan dari provider lain. Kosongkan bila tidak perlu.</span>
        <label>Base URL<input aria-label="Base URL provider" type="url" placeholder="http://127.0.0.1:20128" autocomplete="off" spellcheck="false" bind:value={provBaseUrl}></label>
        <label>Protokol gateway<select aria-label="Protokol provider" bind:value={provProtocol}>
          {#each Object.entries(protocolLabels) as [value, label] (value)}<option value={value}>{label}</option>{/each}
        </select></label>
        <p class="field-hint">Menentukan format permintaan dan endpoint Fetch Models. Pembuatan gambar hanya didukung protokol OpenAI — Chat Completions; protokol lain otomatis memakai jalur gambar pekerja Muse.</p>
        <label>API key<input aria-label="API key provider" type="password" placeholder={editingProviderId ? 'Biarkan kosong untuk mempertahankan key' : 'Masukkan API key gateway'} autocomplete="new-password" spellcheck="false" bind:value={provApiKey}></label>
        <div class="provider-form-actions">
          <button class="secondary-button" type="button" onclick={saveProvider} disabled={savingProvider}>{savingProvider ? 'Menyimpan…' : editingProviderId ? 'Simpan perubahan' : 'Tambah provider'}</button>
          {#if editingProviderId}
            <button class="text-button" type="button" onclick={resetProviderForm}>Batal</button>
            {@const editing = current?.providers.find((item) => item.id === editingProviderId)}
            {#if editing?.hasApiKey}<button class="text-button danger-text" type="button" onclick={() => editing && clearProviderKey(editing)}>Hapus key</button>{/if}
          {/if}
        </div>
      </div>
      <button id="testConnection" class="secondary-button" type="button" onclick={testConnection} disabled={testing || saving}>{testing ? 'Menguji…' : 'Tes Koneksi (provider aktif)'}</button>
      {#if connectionError}
        <div id="connectionResult" class="connection-result error" role="status">{connectionError}</div>
      {:else if current?.connection}
        <div id="connectionResult" class="connection-result" role="status">Provider {activeProvider()?.name ?? 'Kustom'} · Terhubung · {current.connection.models.length} model ditemukan. Periksa status setiap kursi di bawah.</div>
        <div id="modelList" class="model-list">{#each current.connection.models as model, index (index)}<span class="model-tag">{modelLabel(activeProvider(), model)}</span>{/each}</div>
      {/if}
    </section>
    <section class="settings-section">
      <h3>Kursi AI</h3><p class="section-hint">Nama tampil, peran, jalur jawaban, dan model dapat diganti untuk semua kursi — termasuk kursi Admin dan Integrasi. Kode kursi bawaan tetap dan kursi bawaan tidak dapat dihapus. Kursi custom dapat ditambah dan dihapus di bawah.</p>
      <div id="seatSettings">
        {#each seats as item (item.code)}
          {@const member = current?.seats.find(seat => seat.code === item.code)}
          {@const model = item.model?.trim()}
          {@const available = model && current?.connection ? current.connection.models.includes(model) : undefined}
          <article class="seat-settings">
            <div class="seat-settings-heading">
              {@render avatar(item.code, member?.avatar ?? '/avatars/group.svg', `Ganti avatar ${member?.name ?? item.name}`)}
              <div>
                <p><span class="seat-code">{item.code}</span></p>
                <label>Nama tampil<input aria-label={`Nama tampil ${item.code}`} maxlength="40" required data-seat-name={item.code} bind:value={item.name}></label>
                <label>Peran<input aria-label={`Peran ${item.code}`} maxlength="80" required data-seat-role={item.code} bind:value={item.role}></label>
                <label>Jalur jawaban<select aria-label={`Provider ${item.code}`} data-seat-provider={item.code} bind:value={item.provider}>
                  <option value="muse">Muse (antrean file)</option>
                  <option value="gateway">Gateway (model)</option>
                </select></label>
              </div>
            </div>
            {#if item.provider === 'muse'}
              <p class="builtin-note">Menjawab lewat antrean file Muse · Tanpa model gateway</p>
            {:else}
              <label class="model-label">{item.code === 'M03' ? 'Nama model (chat / gambar)' : 'Nama model gateway'}
                <input aria-label={`Model ${item.code}`} maxlength="200" placeholder="Sesuai nama model di gateway" autocomplete="off" spellcheck="false" data-model={item.code} bind:value={item.model}>
              </label>
              <span class="model-status" data-model-status={item.code} data-available={available === undefined ? undefined : String(available)}>
                {!model ? 'Nama model belum diisi.' : available === undefined ? 'Tes Koneksi untuk memeriksa nama model ini.' : available ? '✓ Model tersedia di gateway.' : '⚠ Model tidak ditemukan di daftar gateway.'}
              </span>
              <div class="model-test-row">
                <button class="secondary-button" type="button" onclick={() => testModel(item.code)} disabled={!model || testingModel !== null || saving}>{testingModel === item.code ? 'Menguji…' : 'Tes Model'}</button>
                {#if modelTests[item.code]}
                  {#if modelTests[item.code].ok}
                    <span class="model-status test-ok" data-model-test={item.code}>✓ Model berfungsi · {modelTests[item.code].latencyMs} mdtk · “{modelTests[item.code].reply}”</span>
                  {:else}
                    <span class="model-status test-fail" data-model-test={item.code}>✗ {modelTests[item.code].error}</span>
                  {/if}
                {/if}
              </div>
            {/if}
            <label class="prompt-label">Prompt kustom peran
              <textarea aria-label={`Prompt kustom ${item.code}`} rows="3" maxlength="4000" placeholder="Instruksi peran untuk kursi ini…" data-prompt={item.code} bind:value={item.prompt}></textarea>
            </label>
            <span class="field-hint">Menggantikan instruksi peran bawaan. Kosongkan untuk kembali ke prompt bawaan peran.</span>
            {#if item.code.startsWith('C')}
              <button class="text-button danger-text" type="button" onclick={() => removeSeat(item.code, item.name)}>Hapus kursi ini</button>
            {/if}
          </article>
        {/each}
      </div>
      <div class="add-seat">
        <h4>Tambah kursi custom</h4>
        <p class="section-hint">Kursi tambahan memakai gateway dan ikut Diskusi Bergilir serta @mention. Tujuh kursi bawaan tidak dapat dihapus.</p>
        <label>Nama kursi<input aria-label="Nama kursi baru" maxlength="40" placeholder="Contoh: Nova" autocomplete="off" bind:value={newSeatName}></label>
        <label>Peran<input aria-label="Peran kursi baru" maxlength="80" placeholder="Contoh: Pengamat" autocomplete="off" bind:value={newSeatRole}></label>
        <label>Nama model gateway<input aria-label="Model kursi baru" maxlength="200" placeholder="Sesuai nama model di gateway" autocomplete="off" spellcheck="false" bind:value={newSeatModel}></label>
        <button class="secondary-button" type="button" onclick={addSeat} disabled={addingSeat}>{addingSeat ? 'Menambahkan…' : 'Tambah kursi'}</button>
      </div>
    </section>
    <section class="settings-section">
      <h3>Cara berdiskusi</h3>
      <label class="toggle-row"><span><strong>Diskusi Bergilir</strong><small>Semua kursi membalas bergiliran. Jika mati, hanya Muse tanpa @mention.</small></span><input id="roundRobin" type="checkbox" role="switch" bind:checked={roundRobin}></label>
      <label>Level Review<select id="reviewLevel" bind:value={reviewLevel}>{#each levels as level (level)}<option value={level}>{level}</option>{/each}</select></label>
      <p class="field-hint">Mengatur ketajaman pemeriksaan kursi Peninjau.</p>
      <label>Kursi integrator<select id="integratorSeat" bind:value={integratorSeat}>{#each seats as item (item.code)}<option value={item.code}>{item.name} ({item.code})</option>{/each}</select></label>
      <p class="field-hint">Kursi yang menutup pipeline "Tugas:" (Hasil Akhir - Integrasi) dan menjawab "Tanya Progres". Bawaan: Muse (MUSE).</p>
      <label class="toggle-row"><span><strong>Fallback demo</strong><small>Simulasi berlabel DEMO jika chat gateway gagal. Tidak berlaku untuk Muse atau gambar.</small></span><input id="demoFallback" type="checkbox" role="switch" bind:checked={demoFallback}></label>
    </section>
    <section class="settings-section danger-section">
      <h3>Riwayat percakapan</h3><p class="section-hint">Hapus pesan dan batalkan antrean aktif. Pengaturan tetap tersimpan.</p>
      <button id="clearChat" class="danger-button" type="button" onclick={clear}>Bersihkan chat</button>
    </section>
    <div class="settings-save"><span id="saveStatus" role="status">{saveStatus}</span><button id="saveSettings" class="primary-button" type="submit" disabled={saving || testing}>Simpan Pengaturan</button></div>
  </form>
  <div class="toast" role="status" class:error={notice?.error} hidden={notice?.target !== 'settingsDialog'}>{notice?.target === 'settingsDialog' ? notice.text : ''}</div>
</dialog>
<input id="avatarInput" type="file" accept="image/png,image/jpeg,image/gif,image/webp" hidden bind:this={fileInput} onchange={upload}>
