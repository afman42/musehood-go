<script lang="ts">
  import { api, apiDelete, errorText, protocolLabels } from './api';
  import type { GatewayProvider, GatewayProtocol, LatencyResult, ModelTestResult, OllamaModel, State } from './types';

  let { current, refresh, mutate, notify, onSave, saving }: {
    current: State | undefined;
    refresh: () => Promise<void>;
    mutate: (route: string, body: object) => Promise<void>;
    notify: (message: string, error?: boolean) => void;
    onSave: () => Promise<void>;
    saving: boolean;
  } = $props();

  let provName = $state('');
  let provAlias = $state('');
  let provBaseUrl = $state('');
  let provProtocol = $state<GatewayProtocol>('openai');
  let provApiKey = $state('');
  let provPriceIn = $state('');
  let provPriceOut = $state('');
  let editingProviderId = $state<string | null>(null);
  let fetchedModels = $state<Record<string, { models: string[]; testedAt: string }>>({});
  let providerTests = $state<Record<string, Record<string, ModelTestResult>>>({});
  let fetchingProvider = $state<string | null>(null);
  let testingProvider = $state<string | null>(null);
  let checkingHealth = $state<string | null>(null);
  let latencyProvider = $state<string | null>(null);
  let latencyResults = $state<Record<string, LatencyResult[]>>({});
  let savingProvider = $state(false);
  let testing = $state(false);
  let connectionError = $state('');

  function activeProvider(): GatewayProvider | undefined {
    return current?.providers.find((provider) => provider.active);
  }

  // Label model dengan awalan alias provider (mis. "xc/gpt-6.1-sol") agar
  // model bernama sama dari provider lain mudah dibedakan. Hanya tampilan;
  // nama model yang dikirim ke gateway tetap apa adanya.
  function modelLabel(provider: GatewayProvider | undefined, model: string): string {
    return provider?.alias ? `${provider.alias}/${model}` : model;
  }

  export function clearConnectionError() {
    connectionError = '';
  }

  export function resetForm() {
    provName = '';
    provAlias = '';
    provBaseUrl = '';
    provProtocol = 'openai';
    provApiKey = '';
    provPriceIn = '';
    provPriceOut = '';
    editingProviderId = null;
    connectionError = '';
  }

  function editProvider(provider: GatewayProvider) {
    editingProviderId = provider.id;
    provName = provider.name;
    provAlias = provider.alias ?? '';
    provBaseUrl = provider.baseUrl;
    provProtocol = provider.protocol;
    provApiKey = '';
    provPriceIn = provider.priceInputPer1M ? String(provider.priceInputPer1M) : '';
    provPriceOut = provider.priceOutputPer1M ? String(provider.priceOutputPer1M) : '';
  }

  async function saveProvider() {
    const name = provName.trim();
    const baseUrl = provBaseUrl.trim();
    if (!name || !baseUrl) { notify('Nama dan Base URL provider wajib diisi.', true); return; }
    savingProvider = true;
    try {
      const body: Record<string, unknown> = { name, alias: provAlias.trim(), baseUrl, protocol: provProtocol };
      if (provApiKey) body.apiKey = provApiKey;
      const priceIn = Number.parseFloat(provPriceIn);
      const priceOut = Number.parseFloat(provPriceOut);
      body.priceInputPer1M = Number.isFinite(priceIn) && priceIn >= 0 ? priceIn : 0;
      body.priceOutputPer1M = Number.isFinite(priceOut) && priceOut >= 0 ? priceOut : 0;
      if (editingProviderId) {
        await api(`/api/providers/${editingProviderId}`, body);
        notify('Provider diperbarui.');
      } else {
        await api('/api/providers', body);
        notify('Provider ditambahkan.');
      }
      resetForm();
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
      const out = await api<{ providerId: string; results: Array<{ model: string } & ModelTestResult> }>(`/api/providers/${provider.id}/test-models`, { models: models.slice(0, 10) });
      const perModel: Record<string, ModelTestResult> = {};
      for (const result of out.results) perModel[result.model] = result;
      providerTests = { ...providerTests, [provider.id]: perModel };
      const passed = out.results.filter((result) => result.ok).length;
      notify(`${passed} dari ${out.results.length} model berfungsi di ${provider.name}.`, passed < out.results.length);
    } catch (error) { notify(errorText(error), true); }
    finally { testingProvider = null; }
  }

  async function checkHealth(provider: GatewayProvider) {
    if (checkingHealth !== null) return;
    checkingHealth = provider.id;
    try {
      const out = await api<{ providerId: string; health: { ok: boolean; latencyMs: number; error?: string } }>(`/api/providers/${provider.id}/health`, {});
      await refresh();
      notify(out.health.ok ? `${provider.name} sehat (${out.health.latencyMs} mdtk).` : `${provider.name} bermasalah: ${out.health.error ?? 'tidak terhubung'}`, !out.health.ok);
    } catch (error) { notify(errorText(error), true); }
    finally { checkingHealth = null; }
  }

  async function runLatencyTest(provider: GatewayProvider) {
    if (latencyProvider !== null) return;
    latencyProvider = provider.id;
    try {
      const out = await api<{ providerId: string; results: LatencyResult[] }>(`/api/providers/${provider.id}/latency-test`, {});
      latencyResults = { ...latencyResults, [provider.id]: out.results ?? [] };
      notify(`Uji latency selesai untuk ${provider.name}.`);
    } catch (error) { notify(errorText(error), true); }
    finally { latencyProvider = null; }
  }

  async function testConnection() {
    testing = true;
    try {
      await onSave();
      await mutate('/api/test-connection', {});
      notify('Koneksi gateway berhasil diuji.');
    } catch (error) {
      connectionError = errorText(error);
      notify(connectionError, true);
    } finally { testing = false; }
  }

  // Model lokal Ollama (fitur 32).
  let ollamaBaseUrl = $state('http://127.0.0.1:11434');
  let ollamaModels = $state<OllamaModel[]>([]);
  let ollamaDetecting = $state(false);
  let ollamaAdding = $state('');

  async function detectOllama() {
    if (ollamaDetecting) return;
    ollamaDetecting = true;
    try {
      const out = await api<{ models: OllamaModel[] }>('/api/ollama/detect', { baseUrl: ollamaBaseUrl.trim() });
      ollamaModels = out.models ?? [];
      notify(ollamaModels.length ? `${ollamaModels.length} model Ollama ditemukan.` : 'Tidak ada model di Ollama itu.');
    } catch (error) { notify(errorText(error), true); }
    finally { ollamaDetecting = false; }
  }

  async function addOllamaModel(model: string) {
    if (ollamaAdding) return;
    ollamaAdding = model;
    try {
      await api('/api/ollama/add', { baseUrl: ollamaBaseUrl.trim(), model });
      await refresh();
      notify(`Provider Ollama ditambahkan (${model}). Aktifkan di daftar provider bila ingin dipakai.`);
    } catch (error) { notify(errorText(error), true); }
    finally { ollamaAdding = ''; }
  }
</script>

<section class="settings-section">
  <h3>Provider Gateway</h3><p class="section-hint">Kelola gateway kustom untuk kursi AI. Provider aktif dipakai semua kursi. Muse selalu memakai pekerja bawaan melalui antrean file.</p>
  {#if (current?.providers ?? []).length === 0}
    <p class="field-hint">Belum ada provider. Tambahkan satu di bawah.</p>
  {:else}
    <div id="providerList">
      {#each current?.providers ?? [] as provider (provider.id)}
        <article class="provider-row">
          <div class="provider-info">
            <p><strong>{provider.name}</strong> {#if provider.alias}<span class="model-tag">{provider.alias}</span>{/if} {#if provider.active}<span class="provider-active">Aktif</span>{/if} {#if provider.health}<span class="health-badge {provider.health.ok ? 'ok' : 'fail'}" title={provider.health.ok ? `Dicek ${provider.health.checkedAt}` : (provider.health.error ?? '')}>{provider.health.ok ? `● Sehat ${provider.health.latencyMs} mdtk` : '● Bermasalah'}</span>{/if} {#if provider.health?.circuit && provider.health.circuit !== 'closed'}<span class="health-badge fail" title="Circuit breaker provider">⚡ Breaker {provider.health.circuit}</span>{/if}</p>
            <p class="field-hint">{provider.baseUrl} · {protocolLabels[provider.protocol]} · {provider.hasApiKey ? 'Key tersimpan' : 'Tanpa key'}</p>
          </div>
          <div class="provider-actions">
            {#if !provider.active}<button class="secondary-button" type="button" onclick={() => activateProvider(provider)}>Aktifkan</button>{/if}
            <button class="secondary-button" type="button" onclick={() => editProvider(provider)}>Edit</button>
            <button class="secondary-button" type="button" onclick={() => fetchModels(provider)} disabled={fetchingProvider !== null}>{fetchingProvider === provider.id ? 'Mengambil…' : 'Fetch Models'}</button>
            <button class="secondary-button" type="button" onclick={() => testModels(provider)} disabled={testingProvider !== null || (fetchedModels[provider.id]?.models.length ?? 0) === 0}>{testingProvider === provider.id ? 'Menguji…' : 'Tes Models'}</button>
            <button class="secondary-button" type="button" onclick={() => checkHealth(provider)} disabled={checkingHealth !== null}>{checkingHealth === provider.id ? 'Memeriksa…' : 'Cek Kesehatan'}</button>
            <button class="secondary-button" type="button" onclick={() => runLatencyTest(provider)} disabled={latencyProvider !== null}>{latencyProvider === provider.id ? 'Mengukur…' : 'Uji Latency'}</button>
            <button class="text-button danger-text" type="button" onclick={() => deleteProvider(provider)}>Hapus</button>
          </div>
          {#if latencyResults[provider.id]?.length}
            <div class="table-scroll">
              <table class="stats-table">
                <thead><tr><th>#</th><th>Model</th><th>Latency</th><th>Status</th></tr></thead>
                <tbody>
                  {#each latencyResults[provider.id] as result, rank (result.model)}
                    <tr><td>{rank + 1}</td><td>{modelLabel(provider, result.model)}</td><td>{result.ok ? `${result.latencyMs} mdtk` : '—'}</td><td>{result.ok ? 'OK' : (result.error ?? 'Gagal')}</td></tr>
                  {/each}
                </tbody>
              </table>
            </div>
          {/if}
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
    <label>Harga input (USD/1jt token)<input aria-label="Harga input provider" type="number" min="0" step="any" placeholder="Opsional, untuk estimasi biaya" bind:value={provPriceIn}></label>
    <label>Harga output (USD/1jt token)<input aria-label="Harga output provider" type="number" min="0" step="any" placeholder="Opsional, untuk estimasi biaya" bind:value={provPriceOut}></label>
    <div class="provider-form-actions">
      <button class="secondary-button" type="button" onclick={saveProvider} disabled={savingProvider}>{savingProvider ? 'Menyimpan…' : editingProviderId ? 'Simpan perubahan' : 'Tambah provider'}</button>
      {#if editingProviderId}
        <button class="text-button" type="button" onclick={resetForm}>Batal</button>
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
  <div class="add-provider">
    <h4>Model lokal Ollama</h4>
    <p class="field-hint">Deteksi model dari server Ollama di komputer ini atau jaringan lokal (localhost/LAN saja), lalu tambahkan sebagai provider protokol Ollama.</p>
    <label>Base URL Ollama<input aria-label="Base URL Ollama" type="url" placeholder="http://127.0.0.1:11434" autocomplete="off" spellcheck="false" bind:value={ollamaBaseUrl}></label>
    <div class="provider-form-actions">
      <button class="secondary-button" type="button" onclick={detectOllama} disabled={ollamaDetecting}>{ollamaDetecting ? 'Mendeteksi…' : 'Deteksi Ollama'}</button>
    </div>
    {#if ollamaModels.length}
      <div class="ollama-models">
        {#each ollamaModels as model (model.name)}
          <p class="ollama-model-row"><span><strong>{model.name}</strong>{#if model.size} <span class="field-hint">({(model.size / 1e9).toFixed(1)} GB)</span>{/if}</span><button class="secondary-button" type="button" onclick={() => addOllamaModel(model.name)} disabled={ollamaAdding !== ''}>{ollamaAdding === model.name ? 'Menambah…' : 'Tambah'}</button></p>
        {/each}
      </div>
    {/if}
  </div>
</section>
