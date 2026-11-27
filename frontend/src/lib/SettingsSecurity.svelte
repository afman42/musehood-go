<script lang="ts">
  import { api, errorText } from './api';
  import { t } from './i18n.svelte';
  import type { State } from './types';

  let { current, notify, refresh }: { current: State | undefined; notify: (message: string, error?: boolean) => void; refresh: () => Promise<void> } = $props();

  let newPin = $state('');
  let oldPin = $state('');
  let busy = $state(false);
  let externalToken = $state('');
  let updateInfo = $state<{ current: string; latest: string; updateAvailable: boolean } | undefined>(undefined);
  let updateBusy = $state(false);

  async function regenerateToken() {
    try {
      const out = await api<{ token: string }>('/api/external-token/regenerate', {});
      externalToken = out.token;
      notify('Token API eksternal dibuat. Salin sekarang — hanya ditampilkan sekali.');
    } catch (error) { notify(errorText(error), true); }
  }

  async function checkUpdate() {
    updateBusy = true;
    try {
      updateInfo = await api<{ current: string; latest: string; updateAvailable: boolean }>('/api/update/check');
    } catch (error) { notify(errorText(error), true); }
    finally { updateBusy = false; }
  }

  async function applyUpdate() {
    if (!updateInfo || !window.confirm(`Terapkan pembaruan ke ${updateInfo.latest}? Aplikasi akan dimulai ulang.`)) return;
    updateBusy = true;
    try {
      await api('/api/update/apply', {});
      notify('Pembaruan diterapkan; aplikasi dimulai ulang…');
    } catch (error) { notify(errorText(error), true); }
    finally { updateBusy = false; }
  }

  async function changePin(pin: string) {
    if (busy) return;
    busy = true;
    try {
      await api('/api/pin', { pin, oldPin });
      newPin = '';
      oldPin = '';
      await refresh();
      notify(pin ? 'PIN tersimpan.' : 'PIN dihapus.');
    } catch (error) {
      notify(errorText(error), true);
    } finally {
      busy = false;
    }
  }

  async function logout() {
    if (busy) return;
    busy = true;
    try {
      await api('/api/logout', {});
      window.location.reload();
    } catch (error) {
      notify(errorText(error), true);
    } finally {
      busy = false;
    }
  }
</script>

<fieldset>
  <legend>Keamanan</legend>
  <p class="hint">PIN aplikasi melindungi akses ke Musehood. Bila PIN aktif, semua akses butuh masuk dengan PIN (sesi berlaku 7 hari). Status saat ini: <strong>{current?.settings.hasPin ? 'PIN aktif' : 'PIN tidak aktif'}</strong>.</p>
  {#if current?.settings.hasPin}
    <label>PIN lama <input bind:value={oldPin} type="password" autocomplete="current-password" maxlength="64" placeholder="PIN lama"></label>
  {/if}
  <label>{current?.settings.hasPin ? 'PIN baru' : 'PIN'} <input bind:value={newPin} type="password" autocomplete="new-password" maxlength="64" placeholder="4–64 karakter"></label>
  <div class="settings-actions">
    <button class="secondary-button" type="button" disabled={busy || newPin.trim().length < 4} onclick={() => changePin(newPin)}>{current?.settings.hasPin ? 'Ganti PIN' : 'Pasang PIN'}</button>
    {#if current?.settings.hasPin}
      <button class="secondary-button" type="button" disabled={busy} onclick={() => changePin('')}>Hapus PIN</button>
      <button class="secondary-button" type="button" disabled={busy} onclick={logout}>Keluar (logout)</button>
    {/if}
  </div>
  <h4>{t('externalToken')}</h4>
  <p class="hint">{t('externalTokenHint')}</p>
  <div class="settings-actions">
    <button class="secondary-button" type="button" onclick={regenerateToken}>{t('regenerateToken')}</button>
  </div>
  {#if externalToken}<p class="hint"><code>{externalToken}</code></p>{/if}
  <h4>{t('checkUpdate')}</h4>
  {#if updateInfo}
    <p class="hint">{t('currentVersion')}: {updateInfo.current} — {updateInfo.updateAvailable ? `${t('updateAvailable')}: ${updateInfo.latest}` : t('upToDate')}</p>
  {/if}
  <div class="settings-actions">
    <button class="secondary-button" type="button" disabled={updateBusy} onclick={checkUpdate}>{t('checkUpdate')}</button>
    {#if updateInfo?.updateAvailable}<button class="secondary-button" type="button" disabled={updateBusy} onclick={applyUpdate}>{t('applyUpdate')}</button>{/if}
  </div>
  {#if current?.loginLog?.length}
    <h4>Audit masuk terakhir</h4>
    <ul class="login-log">
      {#each [...current.loginLog].slice(-10).reverse() as entry (entry.time + entry.ip)}
        <li>{entry.success ? '✅' : '⛔'} {new Date(entry.time).toLocaleString()} — {entry.ip}</li>
      {/each}
    </ul>
    <p class="hint">5 kegagalan dari satu IP dalam 5 menit memblokir IP itu selama 5 menit.</p>
  {/if}
</fieldset>
