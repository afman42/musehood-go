<script lang="ts">
  import { errorText } from './api';
  import type { State } from './types';

  let { current, refresh, notify, onSync }: {
    current: State | undefined;
    refresh: () => Promise<void>;
    notify: (message: string, error?: boolean) => void;
    onSync: () => void;
  } = $props();

  let restoring = $state(false);
  let restoreInput: HTMLInputElement;

  async function restoreBackup() {
    const file = restoreInput.files?.[0];
    restoreInput.value = '';
    if (!file) return;
    if (!window.confirm('Pulihkan cadangan ini? Semua data saat ini (ruang, pesan, kursi, provider) diganti. Kunci API yang tersimpan dipertahankan.')) return;
    restoring = true;
    try {
      const text = await file.text();
      const response = await fetch('/api/restore', { method: 'POST', body: text, headers: { 'Content-Type': 'application/json' } });
      const result = await response.json().catch(() => ({}));
      if (!response.ok) throw new Error(result.error || `HTTP ${response.status}`);
      await refresh();
      onSync();
      notify('Cadangan dipulihkan.');
    } catch (error) { notify(errorText(error), true); }
    finally { restoring = false; }
  }
</script>

<section class="settings-section">
  <h3>Cadangan & ekspor</h3><p class="section-hint">Cadangan memuat ruang, kursi, provider, pesan, preset, dan statistik — tanpa kunci API. Saat dipulihkan, kunci yang sudah tersimpan dipertahankan.</p>
  <div class="provider-form-actions">
    <a class="secondary-button" href="/api/backup" download="cadangan-musehood.json">Unduh cadangan</a>
    <button class="secondary-button" type="button" onclick={() => restoreInput.click()} disabled={restoring}>{restoring ? 'Memulihkan…' : 'Pulihkan cadangan'}</button>
  </div>
  <p class="section-hint" style="margin-top:14px">Ekspor riwayat ruang aktif:</p>
  <div class="provider-form-actions">
    <a class="secondary-button" href={`/api/export?format=md&room=${current?.activeRoomId ?? ''}`} download="riwayat-musehood.md">Markdown</a>
    <a class="secondary-button" href={`/api/export?format=json&room=${current?.activeRoomId ?? ''}`} download="riwayat-musehood.json">JSON</a>
    <a class="secondary-button" href={`/api/export?format=html&room=${current?.activeRoomId ?? ''}`} download="riwayat-musehood.html">HTML (cetak/PDF)</a>
  </div>
</section>
<input id="restoreInput" type="file" accept="application/json,.json" hidden bind:this={restoreInput} onchange={restoreBackup}>
