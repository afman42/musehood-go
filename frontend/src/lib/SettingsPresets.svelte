<script lang="ts">
  import { api, apiDelete, errorText } from './api';
  import type { State } from './types';

  let { current, refresh, notify, onSave, onSync }: {
    current: State | undefined;
    refresh: () => Promise<void>;
    notify: (message: string, error?: boolean) => void;
    onSave: () => Promise<void>;
    onSync: () => void;
  } = $props();

  let presetName = $state('');

  async function savePreset() {
    const name = presetName.trim();
    if (!name) { notify('Nama preset wajib diisi.', true); return; }
    try {
      await onSave();
      await api('/api/presets', { name });
      presetName = '';
      await refresh();
      notify('Preset kursi tersimpan.');
    } catch (error) { notify(errorText(error), true); }
  }

  async function applyPreset(id: string, name: string) {
    if (!window.confirm(`Terapkan preset ${name}? Konfigurasi kursi saat ini diganti.`)) return;
    try {
      await api(`/api/presets/${id}/apply`, {});
      await refresh();
      onSync();
      notify('Preset diterapkan.');
    } catch (error) { notify(errorText(error), true); }
  }

  async function removePreset(id: string, name: string) {
    if (!window.confirm(`Hapus preset ${name}?`)) return;
    try {
      await apiDelete(`/api/presets/${id}`);
      await refresh();
      notify('Preset dihapus.');
    } catch (error) { notify(errorText(error), true); }
  }
</script>

<section class="settings-section">
  <h3>Preset kursi</h3><p class="section-hint">Simpan susunan kursi saat ini (nama, peran, model, prompt, fallback, memori) sebagai templat dan muat ulang kapan saja.</p>
  {#each current?.presets ?? [] as preset (preset.id)}
    <article class="provider-row">
      <div class="provider-info"><p><strong>{preset.name}</strong></p><p class="field-hint">{preset.seats.length} kursi · {new Date(preset.createdAt).toLocaleString('id-ID')}</p></div>
      <div class="provider-actions">
        <button class="secondary-button" type="button" onclick={() => applyPreset(preset.id, preset.name)}>Terapkan</button>
        <button class="text-button danger-text" type="button" onclick={() => removePreset(preset.id, preset.name)}>Hapus</button>
      </div>
    </article>
  {/each}
  <label>Nama preset<input aria-label="Nama preset baru" maxlength="80" placeholder="Contoh: Susunan riset" autocomplete="off" bind:value={presetName}></label>
  <button class="secondary-button mt-10" type="button" onclick={savePreset}>Simpan preset dari susunan saat ini</button>
</section>
