<script lang="ts">
  import type { Room } from './types';

  let { rooms, activeRoomId, onSwitch, onAdd, onSummarize }: {
    rooms: Room[];
    activeRoomId: string;
    onSwitch: (id: string) => void;
    onAdd: () => void;
    onSummarize: () => void;
  } = $props();
</script>

<nav class="room-bar" aria-label="Ruang grup">
  {#each rooms as room (room.id)}
    <button class="room-chip" class:active={room.id === activeRoomId} type="button" title={`Ruang ${room.name}`} onclick={() => onSwitch(room.id)}>{room.name}</button>
  {/each}
  <button class="room-chip room-add" type="button" title="Buat ruang baru" aria-label="Buat ruang baru" onclick={onAdd}>＋ Ruang</button>
  <button class="room-chip" type="button" title="Ringkas diskusi ruang ini" aria-label="Ringkas diskusi" onclick={onSummarize}>📝 Ringkas</button>
</nav>
