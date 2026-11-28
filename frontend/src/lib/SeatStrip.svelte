<script lang="ts">
  import type { Seat } from './types';

  let { seats, onMention }: {
    seats: Seat[];
    onMention: (seat: Seat) => void;
  } = $props();
</script>

<nav id="seatStrip" class="seat-strip" aria-label="Kursi AI ruang ini">
  {#each seats as seat (seat.code)}
    <button class="seat-card" type="button" title={`@${seat.name} · ${seat.code} · ${seat.role}`} aria-label={`Sebut ${seat.name} ${seat.role}`} onclick={() => onMention(seat)}>
      <span class="seat-avatar-wrap"><img src={seat.avatar} alt="" width="40" height="40"><span class={['presence-dot', { offline: !seat.present }]} title={seat.present ? 'Hadir' : 'Tidak hadir'}></span></span>
      <span class="seat-name">{seat.name}</span> <span class="seat-role">{seat.role}</span>
    </button>
  {/each}
</nav>
