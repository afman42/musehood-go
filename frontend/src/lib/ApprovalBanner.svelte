<script lang="ts">
  import type { Approval } from './types';

  let { approvals, onDecide }: {
    approvals: Approval[];
    onDecide: (id: string, approved: boolean) => void;
  } = $props();

  function title(approval: Approval): string {
    const who = `${approval.seatName} (${approval.seatCode})`;
    if (approval.kind === 'shell') return `⚠ ${who} ingin menjalankan perintah shell:`;
    if (approval.kind === 'delete') return `⚠ ${who} ingin menghapus file:`;
    if (approval.kind === 'http') return `⚠ ${who} ingin mengirim POST HTTP:`;
    if (approval.kind === 'git') return `⚠ ${who} ingin menjalankan operasi git:`;
    if (approval.kind === 'mcp') return `⚠ ${who} ingin memanggil tool MCP eksternal:`;
    return `⚠ ${who} ingin menjalankan kode ${approval.language ?? ''} yang terdeteksi berbahaya:`;
  }
</script>

{#each approvals as approval (approval.id)}
  <div class="approval-banner" role="alertdialog" aria-label="Persetujuan tindakan kursi">
    <p class="approval-title">{title(approval)}</p>
    <pre class="approval-code">{approval.detail}</pre>
    <div class="approval-actions">
      <button class="danger-button" type="button" onclick={() => onDecide(approval.id, false)}>Tolak</button>
      <button class="primary-button" type="button" onclick={() => onDecide(approval.id, true)}>Setujui & jalankan</button>
    </div>
  </div>
{/each}
