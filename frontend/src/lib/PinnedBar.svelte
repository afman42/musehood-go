<script lang="ts">
  import { attachmentLabel } from './format';
  import type { Message, Seat } from './types';

  let { pinned, ownerName, seats, onJump }: {
    pinned: Message[];
    ownerName: string | undefined;
    seats: Seat[];
    onJump: (id: string) => void;
  } = $props();
</script>

<div class="pinned-bar" aria-label="Pesan tersemat">
  <span class="pinned-title">📌 Tersemat</span>
  {#each pinned as message (message.id)}
    <button class="pinned-item" type="button" title="Lompat ke pesan tersemat" onclick={() => onJump(message.id)}>{message.authorType === 'owner' ? ownerName : seats.find(s => s.code === message.seat)?.name ?? message.seat}: {attachmentLabel(message).slice(0, 60)}</button>
  {/each}
</div>
