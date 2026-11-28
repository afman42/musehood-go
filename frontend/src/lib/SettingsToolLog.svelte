<script lang="ts">
  import { api, errorText } from './api';
  import type { ToolLogEntry } from './types';

  let { notify }: { notify: (message: string, error?: boolean) => void } = $props();

  let entries = $state<ToolLogEntry[]>([]);
  // Penjaga muatan sekali jalan: sengaja NON-reaktif (seperti
  // SettingsRooms/SettingsSeats) agar $effect di bawah tidak
  // berlangganan variabel yang ditulisnya sendiri — dengan $state,
  // reload() memicu effect sekali lagi dan fetch menjadi ganda.
  let loaded = false;

  async function load() {
    try {
      const result = await api<{ entries: ToolLogEntry[] }>('/api/tool-log?limit=100');
      entries = result.entries ?? [];
      loaded = true;
    } catch (error) {
      notify(errorText(error), true);
    }
  }

  $effect(() => {
    if (!loaded) void load();
  });

  // Dipanggil Settings.open() agar log selalu segar saat dialog dibuka
  // (komponen ini selalu terpasang, jadi muatan awal bisa sudah basi).
  export function reload() {
    loaded = false;
    void load();
  }

  function timeText(value: string) {
    const date = new Date(value);
    return Number.isNaN(date.getTime()) ? value : date.toLocaleString('id-ID');
  }
</script>

<fieldset>
  <legend>Log Audit Tool</legend>
  <p class="hint">100 pemakaian tool terakhir oleh kursi (server menyimpan maksimal 500 entri).</p>
  <div class="settings-actions">
    <button class="secondary-button" type="button" onclick={load}>Segarkan</button>
  </div>
  {#if entries.length}
    <div class="table-scroll">
      <table class="stats-table">
        <thead><tr><th>Waktu</th><th>Kursi</th><th>Tool</th><th>Argumen</th><th>Status</th><th>Durasi</th></tr></thead>
        <tbody>
          {#each entries as entry (entry.time + '|' + entry.seatCode + '|' + entry.tool + '|' + entry.durationMs + '|' + (entry.arg ?? ''))}
            <tr>
              <td>{timeText(entry.time)}</td>
              <td>{entry.seatName} ({entry.seatCode})</td>
              <td><code>{entry.tool}</code></td>
              <td class="tool-log-arg">{entry.arg ?? ''}</td>
              <td>{entry.ok ? 'OK' : 'Gagal'}</td>
              <td>{entry.durationMs} mdtk</td>
            </tr>
          {/each}
        </tbody>
      </table>
    </div>
  {:else}
    <p class="hint">Belum ada pemakaian tool tercatat.</p>
  {/if}
</fieldset>
