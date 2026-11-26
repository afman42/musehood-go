<script lang="ts">
  import type { Room } from './types';

  let { rooms, activeRoomId, onSwitch, onAdd }: {
    rooms: Room[];
    activeRoomId: string;
    onSwitch: (id: string) => void;
    onAdd: () => void;
  } = $props();
</script>

<nav class="room-bar" aria-label="Ruang grup">
  {#each rooms as room (room.id)}
    <button class="room-chip" class:active={room.id === activeRoomId} type="button" title={`Ruang ${room.name}`} onclick={() => onSwitch(room.id)}>{room.name}</button>
  {/each}
  <button class="room-chip room-add" type="button" title="Buat ruang baru" aria-label="Buat ruang baru" onclick={onAdd}>＋ Ruang</button>
</nav>
