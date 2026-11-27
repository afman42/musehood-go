<script lang="ts">
  import { api, errorText } from './api';
  import type { State } from './types';

  let { current, notify, refresh }: { current: State | undefined; notify: (message: string, error?: boolean) => void; refresh: () => Promise<void> } = $props();

  let newPin = $state('');
  let oldPin = $state('');
  let busy = $state(false);

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
</fieldset>
