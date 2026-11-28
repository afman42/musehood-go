<script lang="ts">
  import { api, apiDelete, errorText } from './api';
  import type { DurableMemory, State } from './types';

  let { current, mutate, notify }: {
    current: State | undefined;
    mutate: (route: string, body: object) => Promise<void>;
    notify: (message: string, error?: boolean) => void;
  } = $props();

  let githubToken = $state('');
  let lspText = $state('');
  let lspLoaded = $state(false);
  let tools = $state<string[]>([]);
  let memories = $state<DurableMemory[]>([]);
  let loaded = $state(false);
  let saving = $state(false);

  const settings = $derived(current?.settings);
  const policies = $derived(settings?.toolPolicies ?? {});

  async function load() {
    try {
      const [toolsResult, memoriesResult] = await Promise.all([
        api<{ builtin: string[] }>('/api/tools'),
        api<{ memories: DurableMemory[] }>('/api/memories'),
      ]);
      tools = toolsResult.builtin ?? [];
      memories = memoriesResult.memories ?? [];
      loaded = true;
    } catch (error) {
      notify(errorText(error), true);
    }
  }

  $effect(() => {
    if (!loaded) void load();
  });

  $effect(() => {
    if (!lspLoaded && settings) {
      lspText = Object.entries(settings.lspServers ?? {}).map(([lang, cmd]) => `${lang}=${cmd}`).join('\n');
      lspLoaded = true;
    }
  });

  export function reload() {
    loaded = false;
    lspLoaded = false;
    void load();
  }

  async function saveToken() {
    const token = githubToken.trim();
    if (!token) { notify('Isi token GitHub dulu (kosong = token lama dipertahankan).', true); return; }
    saving = true;
    try {
      await mutate('/api/settings', { settings: { githubToken: token } });
      githubToken = '';
      notify('Token GitHub tersimpan.');
    } catch (error) { notify(errorText(error), true); }
    finally { saving = false; }
  }

  async function saveLSP() {
    const servers: Record<string, string> = {};
    for (const line of lspText.split('\n')) {
      const entry = line.trim();
      if (!entry) continue;
      const eq = entry.indexOf('=');
      if (eq <= 0) { notify(`Baris LSP tidak valid: "${entry}" (format bahasa=perintah).`, true); return; }
      servers[entry.slice(0, eq).trim()] = entry.slice(eq + 1).trim();
    }
    saving = true;
    try {
      await mutate('/api/settings', { settings: { lspServers: servers } });
      notify('Server LSP tersimpan.');
    } catch (error) { notify(errorText(error), true); }
    finally { saving = false; }
  }

  async function toggleSetting(key: 'memoryEnabled' | 'onDemandTools', value: boolean) {
    try {
      await mutate('/api/settings', { settings: { [key]: value } });
    } catch (error) { notify(errorText(error), true); }
  }

  async function setPolicy(tool: string, policy: string) {
    const next = { ...policies };
    if (policy === 'allow') delete next[tool];
    else next[tool] = policy;
    try {
      await mutate('/api/settings', { settings: { toolPolicies: next } });
    } catch (error) { notify(errorText(error), true); }
  }

  async function deleteMemory(id: string) {
    try {
      await apiDelete(`/api/memories/${id}`);
      memories = memories.filter((m) => m.id !== id);
      notify('Memori dihapus.');
    } catch (error) { notify(errorText(error), true); }
  }
</script>

<section class="settings-section">
  <h3>Tool agen (omp)</h3>
  <p class="section-hint">GitHub, language server (LSP), memori durable, katalog tool on-demand, dan kebijakan per tool untuk tool kursi gaya omp.</p>

  <label>Token GitHub (untuk tool github_*)
    <input type="password" bind:value={githubToken} placeholder={settings?.hasGithubToken ? 'Token tersimpan (isi untuk mengganti)' : 'ghp_… / github_pat_…'} autocomplete="off" />
  </label>
  <span class="field-hint">Tanpa token, github_read/search/actions hanya membaca repo publik. Token tidak pernah ditampilkan dan dikosongkan di cadangan.</span>
  <button class="secondary-button" type="button" disabled={saving} onclick={saveToken}>Simpan token</button>

  <label>Server LSP per bahasa (satu per baris: bahasa=perintah)
    <textarea class="omp-textarea" bind:value={lspText} placeholder={'go=gopls\npython=pylsp'} rows="3"></textarea>
  </label>
  <span class="field-hint">Kosong = bawaan (go→gopls, python→pylsp, typescript→typescript-language-server, rust→rust-analyzer) bila terpasang.</span>
  <button class="secondary-button" type="button" disabled={saving} onclick={saveLSP}>Simpan server LSP</button>

  <label class="toggle-row">
    <span><strong>Memori durable bersama</strong><small>Tool memory_*/learn menyimpan fakta lintas ruang (maks 500).</small></span>
    <input type="checkbox" checked={settings?.memoryEnabled ?? true} onchange={(e) => void toggleSetting('memoryEnabled', (e.currentTarget as HTMLInputElement).checked)} />
  </label>

  <label class="toggle-row">
    <span><strong>Katalog tool on-demand</strong><small>System prompt hanya memuat deskripsi tool inti; sisanya diambil lewat tool_info.</small></span>
    <input type="checkbox" checked={settings?.onDemandTools ?? false} onchange={(e) => void toggleSetting('onDemandTools', (e.currentTarget as HTMLInputElement).checked)} />
  </label>

  <h4 class="omp-subtitle">Kebijakan per tool</h4>
  <p class="section-hint">allow = langsung jalan, prompt = minta persetujuanmu dulu, deny = dilarang.</p>
  <div class="policy-list">
    {#each tools as tool (tool)}
      <div class="policy-row">
        <code>{tool}</code>
        <select value={policies[tool] ?? 'allow'} onchange={(e) => void setPolicy(tool, (e.currentTarget as HTMLSelectElement).value)}>
          <option value="allow">allow</option>
          <option value="prompt">prompt</option>
          <option value="deny">deny</option>
        </select>
      </div>
    {/each}
  </div>

  <h4 class="omp-subtitle">Memori durable ({memories.length})</h4>
  {#if memories.length === 0}
    <p class="section-hint">Belum ada memori tersimpan.</p>
  {:else}
    <div class="memory-list">
      {#each memories as memory (memory.id)}
        <div class="memory-row">
          <span class="memory-text">{memory.text}{#if memory.tags?.length}<small> [{memory.tags.join(', ')}]</small>{/if}</span>
          <button class="text-button danger-text" type="button" onclick={() => void deleteMemory(memory.id)}>Hapus</button>
        </div>
      {/each}
    </div>
  {/if}
</section>
