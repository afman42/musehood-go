<script lang="ts">
  import { api, apiDelete, errorText } from './api';
  import AvatarButton from './AvatarButton.svelte';
  import type { ModelTestResult, SeatEdit, State } from './types';

  let { seats = $bindable([]), current, refresh, notify, onSync, onSave, saving, onChooseAvatar }: {
    seats?: SeatEdit[];
    current: State | undefined;
    refresh: () => Promise<void>;
    notify: (message: string, error?: boolean) => void;
    onSync: () => void;
    onSave: () => Promise<void>;
    saving: boolean;
    onChooseAvatar: (target: string) => void;
  } = $props();

  let newSeatName = $state('');
  let newSeatRole = $state('');
  let newSeatModel = $state('');
  let addingSeat = $state(false);
  let testingModel = $state<string | null>(null);
  let modelTests = $state<Record<string, ModelTestResult>>({});

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
      onSync();
      notify('Kursi baru ditambahkan.');
    } catch (error) { notify(errorText(error), true); }
    finally { addingSeat = false; }
  }

  async function removeSeat(code: string, name: string) {
    if (!window.confirm(`Hapus kursi ${name}? Kursi bawaan tidak dapat dihapus.`)) return;
    try {
      await apiDelete(`/api/seats/${code}`);
      await refresh();
      onSync();
      notify('Kursi dihapus.');
    } catch (error) { notify(errorText(error), true); }
  }

  async function testModel(code: string) {
    const item = seats.find((seat) => seat.code === code);
    const model = item?.model?.trim();
    if (!model) { notify('Isi nama model dulu sebelum dites.', true); return; }
    if (testingModel !== null) return;
    testingModel = code;
    try {
      await onSave();
      const out = await api<{ ok: boolean; model: string; latencyMs: number; reply: string; testedAt: string }>('/api/test-model', { model });
      modelTests = { ...modelTests, [code]: { ok: true, latencyMs: out.latencyMs, reply: out.reply } };
      notify(`Model ${model} berfungsi.`);
    } catch (error) {
      const message = errorText(error);
      modelTests = { ...modelTests, [code]: { ok: false, error: message } };
      notify(message, true);
    } finally { testingModel = null; }
  }
</script>

<section class="settings-section">
  <h3>Kursi AI</h3><p class="section-hint">Nama tampil, peran, jalur jawaban, dan model dapat diganti untuk semua kursi — termasuk kursi Admin dan Integrasi. Kode kursi bawaan tetap dan kursi bawaan tidak dapat dihapus. Kursi custom dapat ditambah dan dihapus di bawah.</p>
  <div id="seatSettings">
    {#each seats as item (item.code)}
      {@const member = current?.seats.find(seat => seat.code === item.code)}
      {@const model = item.model?.trim()}
      {@const available = model && current?.connection ? current.connection.models.includes(model) : undefined}
      <article class="seat-settings">
        <div class="seat-settings-heading">
          <AvatarButton target={item.code} src={member?.avatar ?? '/avatars/group.svg'} label={`Ganti avatar ${member?.name ?? item.name}`} onChoose={onChooseAvatar} />
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
        {#if item.provider === 'gateway'}
          <label class="model-label">Model fallback (cadangan)
            <input aria-label={`Model fallback ${item.code}`} maxlength="200" placeholder="Dicoba sekali bila model utama gagal" autocomplete="off" spellcheck="false" data-fallback={item.code} bind:value={item.fallbackModel}>
          </label>
          <span class="field-hint">Boleh berformat alias/model. Kosongkan untuk tanpa fallback.</span>
        {/if}
        <label class="prompt-label">Memori kursi
          <textarea aria-label={`Memori ${item.code}`} rows="2" maxlength="4000" placeholder="Catatan persisten yang selalu diingat kursi ini…" data-memory={item.code} bind:value={item.memory}></textarea>
        </label>
        <span class="field-hint">Disisipkan ke instruksi kursi setiap kali menjawab; tetap ada setelah chat dibersihkan.</span>
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
