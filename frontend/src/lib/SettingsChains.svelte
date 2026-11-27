<script lang="ts">
  import { api, errorText } from './api';
  import type { ChainStep, State } from './types';

  let { current, refresh, notify }: {
    current: State | undefined;
    refresh: () => Promise<void>;
    notify: (message: string, error?: boolean) => void;
  } = $props();

  let name = $state('');
  let steps = $state<ChainStep[]>([{ seatCode: '', instruction: '' }]);
  let saving = $state(false);
  let runInputs = $state<Record<string, string>>({});
  let running = $state('');

  const chains = $derived(current?.chains ?? []);
  const seats = $derived((current?.seats ?? []).filter((seat) => seat.provider === 'gateway'));

  function seatName(code: string): string {
    return current?.seats.find((s) => s.code === code)?.name ?? code;
  }

  function addStep() {
    if (steps.length >= 6) { notify('Maksimal 6 langkah per rantai.', true); return; }
    steps = [...steps, { seatCode: seats[0]?.code ?? '', instruction: '' }];
  }

  function removeStep(index: number) {
    steps = steps.filter((_, i) => i !== index);
  }

  async function create() {
    const trimmed = name.trim();
    if (!trimmed) { notify('Nama rantai wajib diisi.', true); return; }
    if (steps.length === 0 || steps.some((step) => !step.seatCode)) { notify('Setiap langkah butuh kursi gateway.', true); return; }
    saving = true;
    try {
      await api('/api/chains', { name: trimmed, steps });
      name = '';
      steps = [{ seatCode: seats[0]?.code ?? '', instruction: '' }];
      await refresh();
      notify('Rantai prompt tersimpan.');
    } catch (error) { notify(errorText(error), true); }
    finally { saving = false; }
  }

  async function remove(id: string, chainName: string) {
    if (!window.confirm(`Hapus rantai ${chainName}?`)) return;
    try {
      await api(`/api/chains/${id}/delete`, {});
      await refresh();
      notify('Rantai dihapus.');
    } catch (error) { notify(errorText(error), true); }
  }

  async function run(id: string) {
    const text = (runInputs[id] ?? '').trim();
    if (!text) { notify('Isi teks awal untuk menjalankan rantai.', true); return; }
    if (running) return;
    running = id;
    try {
      await api(`/api/chains/${id}/run`, { text, roomId: current?.activeRoomId ?? '' });
      runInputs = { ...runInputs, [id]: '' };
      await refresh();
      notify('Rantai dijalankan — hasil tiap langkah muncul di ruang aktif.');
    } catch (error) { notify(errorText(error), true); }
    finally { running = ''; }
  }
</script>

<section class="settings-section">
  <h3>Rantai prompt</h3>
  <p class="section-hint">Langkah otomatis berurutan: output langkah sebelumnya menjadi input langkah berikutnya. Tiap langkah dijalankan satu kursi gateway; hasilnya tampil sebagai pesan di ruang aktif.</p>
  {#if chains.length}
    <ul class="template-list">
      {#each chains as chain (chain.id)}
        <li class="template-item">
          <div>
            <strong>{chain.name}</strong>
            <small>{chain.steps.map((step, i) => `${i + 1}. ${seatName(step.seatCode)}`).join(' → ')}</small>
            <div class="chain-run">
              <input aria-label={`Teks awal rantai ${chain.name}`} maxlength="20000" placeholder="Teks awal…" bind:value={runInputs[chain.id]}>
              <button class="secondary-button" type="button" onclick={() => run(chain.id)} disabled={running !== ''}>{running === chain.id ? 'Menjalankan…' : 'Jalankan'}</button>
            </div>
          </div>
          <button class="text-button danger-text" type="button" onclick={() => remove(chain.id, chain.name)}>Hapus</button>
        </li>
      {/each}
    </ul>
  {/if}
  <label>Nama rantai<input aria-label="Nama rantai" maxlength="80" placeholder="Contoh: Riset lalu ringkas" bind:value={name}></label>
  {#each steps as step, index (index)}
    <div class="chain-step">
      <label>Langkah {index + 1} — kursi<select aria-label={`Kursi langkah ${index + 1}`} bind:value={step.seatCode}>
        {#each seats as seat (seat.code)}<option value={seat.code}>{seat.name} ({seat.code})</option>{/each}
      </select></label>
      <label>Instruksi<textarea aria-label={`Instruksi langkah ${index + 1}`} rows="2" maxlength="2000" placeholder="Mis. Teliti topik berikut" bind:value={step.instruction}></textarea></label>
      {#if steps.length > 1}<button class="text-button danger-text" type="button" onclick={() => removeStep(index)}>Hapus langkah</button>{/if}
    </div>
  {/each}
  <div class="provider-form-actions">
    <button class="secondary-button" type="button" onclick={addStep} disabled={steps.length >= 6}>+ Langkah</button>
    <button class="secondary-button" type="button" onclick={create} disabled={saving}>{saving ? 'Menyimpan…' : 'Simpan rantai'}</button>
  </div>
</section>
