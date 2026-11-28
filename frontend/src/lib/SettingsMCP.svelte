<script lang="ts">
  import { api, errorText } from './api';
  import type { McpServerView } from './types';

  let { notify }: { notify: (message: string, error?: boolean) => void } = $props();

  let servers = $state<McpServerView[]>([]);
  // Penjaga muatan sekali jalan: sengaja NON-reaktif agar $effect
  // tidak berlangganan variabel yang ditulisnya sendiri (fetch ganda
  // saat reload()).
  let loaded = false;
  let name = $state('');
  let command = $state('');
  let argsText = $state('');
  let envText = $state('');
  let saving = $state(false);

  async function load() {
    try {
      const result = await api<{ servers: McpServerView[] }>('/api/mcp-servers');
      servers = result.servers ?? [];
      loaded = true;
    } catch (error) {
      notify(errorText(error), true);
    }
  }

  $effect(() => {
    if (!loaded) void load();
  });

  // Dipanggil Settings.open() agar daftar selalu segar saat dialog dibuka.
  export function reload() {
    loaded = false;
    void load();
  }

  function parseEnv(text: string): Record<string, string> | undefined {
    const trimmed = text.trim();
    if (!trimmed) return undefined;
    const env: Record<string, string> = {};
    for (const line of trimmed.split('\n')) {
      const entry = line.trim();
      if (!entry) continue;
      const eq = entry.indexOf('=');
      if (eq <= 0) throw new Error(`Baris env tidak valid: "${entry}" (format KUNCI=nilai).`);
      env[entry.slice(0, eq).trim()] = entry.slice(eq + 1);
    }
    return env;
  }

  async function save() {
    const trimmedName = name.trim();
    const trimmedCommand = command.trim();
    if (!trimmedName || !trimmedCommand) { notify('Nama dan perintah server MCP wajib diisi.', true); return; }
    let env: Record<string, string> | undefined;
    try {
      env = parseEnv(envText);
    } catch (error) { notify(errorText(error), true); return; }
    const args = argsText.split('\n').map((line) => line.trim()).filter(Boolean);
    saving = true;
    try {
      const result = await api<{ servers: McpServerView[] }>('/api/mcp-servers', {
        name: trimmedName,
        command: trimmedCommand,
        args,
        ...(env === undefined ? {} : { env }),
      });
      servers = result.servers ?? [];
      name = '';
      command = '';
      argsText = '';
      envText = '';
      notify('Server MCP tersimpan. Tool-nya tersedia sebagai mcp_' + trimmedName + '_* (perlu persetujuanmu per panggilan).');
    } catch (error) { notify(errorText(error), true); }
    finally { saving = false; }
  }

  async function remove(serverName: string) {
    if (!window.confirm(`Hapus server MCP ${serverName}?`)) return;
    try {
      const result = await api<{ servers: McpServerView[] }>(`/api/mcp-servers/${serverName}/delete`, {});
      servers = result.servers ?? [];
      notify('Server MCP dihapus.');
    } catch (error) { notify(errorText(error), true); }
  }
</script>

<section class="settings-section">
  <h3>Server MCP</h3>
  <p class="section-hint">Server Model Context Protocol (stdio) yang tool-nya dapat dipakai kursi sebagai <code>mcp_&lt;server&gt;_&lt;tool&gt;</code>. Maksimal 5 server. Setiap panggilan tool MCP memerlukan persetujuanmu. Nilai env tidak pernah ditampilkan kembali.</p>
  {#if servers.length}
    <ul class="template-list">
      {#each servers as server (server.name)}
        <li class="template-item">
          <div>
            <strong>{server.name}</strong>
            <small><code>{server.command}{server.args.length ? ' ' + server.args.join(' ') : ''}</code></small>
            {#if server.envKeys.length}<small>Env: {server.envKeys.join(', ')}</small>{/if}
          </div>
          <button class="text-button danger-text" type="button" onclick={() => remove(server.name)}>Hapus</button>
        </li>
      {/each}
    </ul>
  {/if}
  <label>Nama server<input aria-label="Nama server MCP" maxlength="31" placeholder="Contoh: berkas" bind:value={name}></label>
  <label>Perintah<input aria-label="Perintah server MCP" maxlength="500" placeholder="Contoh: npx" bind:value={command}></label>
  <label>Argumen (satu per baris)<textarea aria-label="Argumen server MCP" rows="2" maxlength="2000" placeholder="-y&#10;@modelcontextprotocol/server-filesystem&#10;/tmp" bind:value={argsText}></textarea></label>
  <label>Variabel env (KUNCI=nilai per baris, opsional)<textarea aria-label="Variabel env server MCP" rows="2" maxlength="4000" placeholder="API_KEY=rahasia" bind:value={envText}></textarea></label>
  <div class="provider-form-actions">
    <button class="secondary-button" type="button" onclick={save} disabled={saving}>{saving ? 'Menyimpan…' : 'Simpan server MCP'}</button>
  </div>
</section>
