<script lang="ts">
  import { unreadCount } from './chat.svelte';
  import { t } from './i18n.svelte';
  import type { Room } from './types';

  let { rooms, activeRoomId, onSwitch, onAdd, onSummarize, onTogglePin }: {
    rooms: Room[];
    activeRoomId: string;
    onSwitch: (id: string) => void;
    onAdd: () => void;
    onSummarize: () => void;
    onTogglePin: (room: Room) => void;
  } = $props();

  const ordered = $derived(rooms.filter((room) => !room.archived).sort((a, b) => Number(b.pinned ?? false) - Number(a.pinned ?? false)));
</script>

<nav class="room-bar" aria-label={t('rooms')}>
  {#each ordered as room (room.id)}
    {@const unread = room.id === activeRoomId ? 0 : unreadCount(room)}
    <span class="room-chip-wrap">
      <button class="room-chip" class:active={room.id === activeRoomId} style={room.color ? `border-color: ${room.color}; box-shadow: inset 0 0 0 1px ${room.color}` : undefined} type="button" title={`Ruang ${room.name}`} onclick={() => onSwitch(room.id)}>{room.pinned ? '📌 ' : ''}{room.icon ? `${room.icon} ` : ''}{room.name}{#if unread > 0}<span class="room-badge" aria-label={`${unread} ${t('unread')}`}>{unread > 99 ? '99+' : unread}</span>{/if}</button>
      <button class="room-pin" type="button" title={room.pinned ? t('unpinRoom') : t('pinRoom')} aria-label={room.pinned ? t('unpinRoom') : t('pinRoom')} onclick={() => onTogglePin(room)}>{room.pinned ? '📌' : '📍'}</button>
    </span>
  {/each}
  <button class="room-chip room-add" type="button" title="Buat ruang baru" aria-label="Buat ruang baru" onclick={onAdd}>{t('newRoom')}</button>
  <button class="room-chip" type="button" title="Ringkas diskusi ruang ini" aria-label="Ringkas diskusi" onclick={onSummarize}>{t('summarize')}</button>
</nav>
