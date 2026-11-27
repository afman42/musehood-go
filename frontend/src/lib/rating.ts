// Penilaian kualitas balasan (fitur 30): logika toggle rating.
// Klik 👍 saat sudah 👍 → netral (0); klik 👍 saat 👎/netral → 1; dst.

export function nextRating(current: number, clicked: number): number {
  if (clicked !== 1 && clicked !== -1) return 0;
  return current === clicked ? 0 : clicked;
}
