<script lang="ts">
  import { dismissBackdrop, errorText } from './api';
  import { t } from './i18n.svelte';
  import type { ScheduledMessage } from './types';

  let { listScheduled, createScheduled, updateScheduled, deleteScheduled, notify, roomId }: {
    listScheduled: () => Promise<ScheduledMessage[]>;
    createScheduled: (body: object) => Promise<ScheduledMessage>;
    updateScheduled: (id: string, body: object) => Promise<ScheduledMessage>;
    deleteScheduled: (id: string) => Promise<void>;
    notify: (message: string, error?: boolean) => void;
    roomId: string;
  } = $props();

  let dialog: HTMLDialogElement;
  let items = $state<ScheduledMessage[]>([]);
  let text = $state('');
  let mode = $state<'once' | 'daily' | 'weekly' | 'interval' | 'cron'>('once');
  let repeat = $state('0 7 * * *');
  let onceAt = $state('');
  let timeOfDay = $state('07:00');
  let weekday = $state(1);
  let intervalHours = $state(24);
  let saving = $state(false);

  const dayNames = ['Minggu', 'Senin', 'Selasa', 'Rabu', 'Kamis', 'Jumat', 'Sabtu'];

  export async function open(prefill = '') {
    text = prefill;
    try {
      items = await listScheduled();
    } catch (error) { notify(errorText(error), true); }
    if (!dialog.open) dialog.showModal();
  }

  function describe(item: ScheduledMessage): string {
    if (item.repeat) return `Cron ${item.repeat}`;
    if (!item.recur) return `Sekali · ${item.nextAt ? new Date(item.nextAt).toLocaleString('id-ID') : ''}`;
    if (item.recur === 'daily') return `Harian · ${item.timeOfDay}`;
    if (item.recur === 'weekly') return `Mingguan · ${dayNames[item.weekday ?? 0]} ${item.timeOfDay}`;
    return `Tiap ${item.intervalHours} jam`;
  }

  async function add() {
    if (saving) return;
    const trimmed = text.trim();
    if (!trimmed) { notify('Teks pesan terjadwal wajib diisi.', true); return; }
    const tzOffsetMinutes = -new Date().getTimezoneOffset();
    const body: Record<string, unknown> = { text: trimmed, roomId, tzOffsetMinutes };
    if (mode === 'once') {
      if (!onceAt) { notify('Pilih tanggal dan jam pengiriman.', true); return; }
      body.sendAt = new Date(onceAt).toISOString();
    } else if (mode === 'daily') {
      body.recur = 'daily'; body.timeOfDay = timeOfDay;
    } else if (mode === 'weekly') {
      body.recur = 'weekly'; body.timeOfDay = timeOfDay; body.weekday = weekday;
    } else if (mode === 'cron') {
      if (!repeat.trim()) { notify('Pola cron wajib diisi.', true); return; }
      body.repeat = repeat.trim();
    } else {
      body.recur = 'interval'; body.intervalHours = intervalHours;
    }
    saving = true;
    try {
      await createScheduled(body);
      items = await listScheduled();
      text = '';
      notify('Pesan terjadwal ditambahkan.');
    } catch (error) { notify(errorText(error), true); }
    finally { saving = false; }
  }

  async function toggle(item: ScheduledMessage) {
    try {
      await updateScheduled(item.id, { enabled: !item.enabled });
      items = await listScheduled();
    } catch (error) { notify(errorText(error), true); }
  }

  async function remove(item: ScheduledMessage) {
    if (!window.confirm('Hapus pesan terjadwal ini?')) return;
    try {
      await deleteScheduled(item.id);
      items = await listScheduled();
    } catch (error) { notify(errorText(error), true); }
  }
</script>

<dialog bind:this={dialog} id="scheduleDialog" class="mention-dialog schedule-dialog" aria-label="Pesan terjadwal" onclick={(event) => dismissBackdrop(event, dialog)}>
  <div class="mention-heading">
    <h2>Pesan Terjadwal</h2>
    <button class="icon-button" type="button" aria-label="Tutup" onclick={() => dialog.close()}>✕</button>
  </div>
  <p class="section-hint">Pesan dikirim sebagai pesanmu di ruang aktif pada waktu yang dipilih (zona waktu perangkatmu), lalu kursi menjawab seperti biasa.</p>
  <label>Teks pesan<textarea aria-label="Teks pesan terjadwal" rows="2" maxlength="20000" placeholder="Tulis pesan, @kursi, atau Tugas: …" bind:value={text}></textarea></label>
  <label>Pengulangan
    <select aria-label="Jenis pengulangan" bind:value={mode}>
      <option value="once">{t('scheduleOnce')}</option>
      <option value="daily">{t('scheduleDaily')}</option>
      <option value="weekly">{t('scheduleWeekly')}</option>
      <option value="interval">Tiap N jam</option>
      <option value="cron">Cron (5 kolom)</option>
    </select>
  </label>
  {#if mode === 'once'}
    <label>Waktu kirim<input aria-label="Waktu kirim" type="datetime-local" bind:value={onceAt}></label>
  {:else if mode === 'daily'}
    <label>Jam<input aria-label="Jam harian" type="time" bind:value={timeOfDay}></label>
  {:else if mode === 'weekly'}
    <label>Hari<select aria-label="Hari mingguan" bind:value={weekday}>{#each dayNames as day, i (i)}<option value={i}>{day}</option>{/each}</select></label>
    <label>Jam<input aria-label="Jam mingguan" type="time" bind:value={timeOfDay}></label>
  {:else if mode === 'cron'}
    <label>Pola cron<input aria-label="Pola cron" type="text" placeholder="menit jam tgl bulan hari (mis. 0 7 * * *)" autocomplete="off" spellcheck="false" bind:value={repeat}></label>
    <p class="section-hint">Format: menit jam tanggal bulan hari-minggu. Mendukung *, */n, angka, dan daftar koma. Contoh: "*/30 * * * *" tiap 30 menit, "0 7 * * 1" Senin 07.00.</p>
  {:else}
    <label>Interval (jam)<input aria-label="Interval jam" type="number" min="1" max="720" bind:value={intervalHours}></label>
  {/if}
  <button class="primary-button" type="button" onclick={add} disabled={saving}>{saving ? 'Menambahkan…' : 'Tambah jadwal'}</button>
  {#if items.length}
    <h3 class="schedule-list-title">Jadwal aktif ({items.length})</h3>
    <ul class="schedule-list">
      {#each items as item (item.id)}
        <li class={['schedule-item', { disabled: !item.enabled }]}>
          <div>
            <strong>{item.text.slice(0, 80)}</strong>
            <small>{describe(item)}{item.nextAt && item.recur ? ` · berikutnya ${new Date(item.nextAt).toLocaleString('id-ID')}` : ''}</small>
          </div>
          <button class="text-button" type="button" onclick={() => toggle(item)}>{item.enabled ? 'Nonaktifkan' : 'Aktifkan'}</button>
          <button class="text-button danger-text" type="button" onclick={() => remove(item)}>Hapus</button>
        </li>
      {/each}
    </ul>
  {/if}
</dialog>
