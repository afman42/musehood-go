<script lang="ts">
  import type { CodeApproval } from './types';

  let { approvals, onDecide }: {
    approvals: CodeApproval[];
    onDecide: (id: string, approved: boolean) => void;
  } = $props();
</script>

{#each approvals as approval (approval.id)}
  <div class="approval-banner" role="alertdialog" aria-label="Persetujuan menjalankan kode">
    <p class="approval-title">⚠ {approval.seatName} ({approval.seatCode}) ingin menjalankan kode {approval.language} yang terdeteksi berbahaya:</p>
    <pre class="approval-code">{approval.code}</pre>
    <div class="approval-actions">
      <button class="danger-button" type="button" onclick={() => onDecide(approval.id, false)}>Tolak</button>
      <button class="primary-button" type="button" onclick={() => onDecide(approval.id, true)}>Setujui & jalankan</button>
    </div>
  </div>
{/each}
