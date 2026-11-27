<script lang="ts">
  import { api, dismissBackdrop, errorText } from './api';
  import type { Notice, State } from './types';
  import Toast from './Toast.svelte';

  let { current, notice, onStart }: {
    current: State | undefined;
    notice: Notice | undefined;
    onStart: (text: string, modelA: string, modelB: string) => Promise<void>;
  } = $props();

  let dialog: HTMLDialogElement;
  let text = $state('');
  let modelA = $state('');
  let modelB = $state('');
  let options = $state<string[]>([]);
  let loading = $state(false);
  let starting = $state(false);

  export async function open() {
    text = '';
    if (!dialog.open) dialog.showModal();
    if (options.length || loading) return;
    loading = true;
    try {
      const found = new Set<string>();
      // Model provider aktif dari koneksi terakhir.
      const active = current?.providers.find((provider) => provider.active);
      for (const model of current?.connection?.models ?? []) {
        found.add(active?.alias ? `${active.alias}/${model}` : model);
      }
      // Model kursi yang sudah terkonfigurasi.
      for (const seat of current?.seats ?? []) {
        if (seat.model) found.add(seat.model);
      }
      // Daftar model tiap provider terdaftar.
      for (const provider of current?.providers ?? []) {
        try {
          const out = await api<{ models: string[] }>(`/api/providers/${provider.id}/models`);
          for (const model of out.models ?? []) {
            found.add(provider.alias ? `${provider.alias}/${model}` : model);
          }
        } catch { /* provider tidak terjangkau: lewati */ }
      }
      options = [...found];
      if (options.length >= 2) {
        modelA = options[0];
        modelB = options[1];
      } else if (options.length === 1) {
        modelA = options[0];
        modelB = options[0];
      }
    } finally {
      loading = false;
    }
  }

  async function start(event: SubmitEvent) {
    event.preventDefault();
    if (starting) return;
    starting = true;
    try {
      await onStart(text.trim(), modelA, modelB);
      dialog.close();
    } catch (error) {
      void errorText(error);
    } finally {
      starting = false;
    }
  }
</script>

<dialog bind:this={dialog} id="abTestDialog" class="settings-dialog" aria-labelledby="abTestTitle" onclick={(event) => dismissBackdrop(event, dialog)}>
  <header class="panel-header">
    <div><p class="eyebrow">MUSEHOOD</p><h2 id="abTestTitle">Uji A/B Model</h2></div>
    <button class="icon-button" type="button" aria-label="Tutup uji A/B" onclick={() => dialog.close()}>✕</button>
  </header>
  <form class="settings-form" onsubmit={start}>
    <p class="section-hint">Pertanyaan yang sama dikirim ke dua model sekaligus. Kedua jawaban tampil berdampingan di ruang aktif; tekan "Pilih ini" pada jawaban yang lebih baik.</p>
    <label>Model A
      <select bind:value={modelA} required>
        {#each options as option (option)}<option value={option}>{option}</option>{/each}
      </select>
    </label>
    <label>Model B
      <select bind:value={modelB} required>
        {#each options as option (option)}<option value={option}>{option}</option>{/each}
      </select>
    </label>
    <label>Pertanyaan
      <textarea bind:value={text} maxlength="20000" rows="3" required placeholder="Contoh: Jelaskan fotosintesis dalam dua kalimat"></textarea>
    </label>
    {#if loading}<p class="field-hint">Memuat daftar model…</p>{/if}
    {#if !loading && options.length === 0}<p class="field-hint">Tidak ada model ditemukan. Tambahkan provider dan Fetch Models di Pengaturan → Provider.</p>{/if}
    <div class="settings-save">
      <button class="primary-button" type="submit" disabled={starting || loading || !modelA || !modelB || !text.trim()}>{starting ? 'Memulai…' : 'Mulai Uji A/B'}</button>
    </div>
  </form>
  <Toast {notice} target="abTestDialog" />
</dialog>
