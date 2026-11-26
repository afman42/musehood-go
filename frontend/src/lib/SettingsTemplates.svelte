<script lang="ts">
  import { api, apiDelete, errorText } from './api';
  import type { State } from './types';

  let { current, refresh, notify }: {
    current: State | undefined;
    refresh: () => Promise<void>;
    notify: (message: string, error?: boolean) => void;
  } = $props();

  let name = $state('');
  let instruction = $state('');
  let sequence = $state<string[]>([]);
  let saving = $state(false);

  const templates = $derived(current?.templates ?? []);
  const seats = $derived(current?.seats ?? []);

  function seatName(code: string): string {
    return seats.find((s) => s.code === code)?.name ?? code;
  }

  function addToSequence(code: string) {
    if (sequence.length >= 12) { notify('Maksimal 12 kursi per template.', true); return; }
    sequence = [...sequence, code];
  }

  function removeFromSequence(index: number) {
    sequence = sequence.filter((_, i) => i !== index);
  }

  async function create() {
    const trimmed = name.trim();
    if (!trimmed) { notify('Nama template wajib diisi.', true); return; }
    if (sequence.length === 0) { notify('Pilih minimal satu kursi untuk urutan template.', true); return; }
    saving = true;
    try {
      await api('/api/templates', { name: trimmed, seats: sequence, instruction: instruction.trim() });
      name = '';
      instruction = '';
      sequence = [];
      await refresh();
      notify('Template diskusi tersimpan.');
    } catch (error) { notify(errorText(error), true); }
    finally { saving = false; }
  }

  async function remove(id: string, templateName: string) {
    if (!window.confirm(`Hapus template ${templateName}?`)) return;
    try {
      await apiDelete(`/api/templates/${id}`);
      await refresh();
      notify('Template dihapus.');
    } catch (error) { notify(errorText(error), true); }
  }
</script>

<section class="settings-section">
  <h3>Template diskusi</h3>
  <p class="section-hint">Preset pipeline: kursi menjawab berurutan sesuai urutan template, dengan instruksi tambahan. Pilih template di pemilih dekat tombol kirim sebelum mengirim pesan.</p>
  {#if templates.length}
    <ul class="template-list">
      {#each templates as template (template.id)}
        <li class="template-item">
          <div>
            <strong>{template.name}</strong>
            <small>{template.seats.map(seatName).join(' → ')}{template.instruction ? ` · “${template.instruction.slice(0, 60)}”` : ''}</small>
          </div>
          <button class="text-button danger-text" type="button" onclick={() => remove(template.id, template.name)}>Hapus</button>
        </li>
      {/each}
    </ul>
  {/if}
  <label>Nama template<input aria-label="Nama template" maxlength="80" placeholder="Contoh: Brainstorm ide" bind:value={name}></label>
  <span class="model-label">Urutan kursi (tekan untuk menambahkan)</span>
  <div class="template-seat-picker">
    {#each seats as seat (seat.code)}
      <button class="action-chip" type="button" onclick={() => addToSequence(seat.code)}>{seat.name}</button>
    {/each}
  </div>
  {#if sequence.length}
    <p class="field-hint">Urutan: {#each sequence as code, i (i)}<button class="template-step" type="button" title="Hapus langkah ini" onclick={() => removeFromSequence(i)}>{i + 1}. {seatName(code)} ✕</button>{/each}</p>
  {/if}
  <label>Instruksi tambahan (opsional)<textarea aria-label="Instruksi template" rows="2" maxlength="2000" placeholder="Mis. Setiap kursi beri sudut pandang berbeda, jangan mengulang." bind:value={instruction}></textarea></label>
  <button class="secondary-button" type="button" onclick={create} disabled={saving}>{saving ? 'Menyimpan…' : 'Simpan template'}</button>
</section>
