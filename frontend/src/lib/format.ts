import type { Attachment, Message, Seat } from './types';

export function humanSize(size: number): string {
  if (size < 1024) return `${size} B`;
  if (size < 1024 * 1024) return `${(size / 1024).toFixed(1)} KB`;
  return `${(size / (1024 * 1024)).toFixed(1)} MB`;
}

export function isImageAttachment(att: Attachment): boolean {
  return att.mime.startsWith('image/');
}

export function attachmentLabel(message: Message): string {
  if (message.text) return message.text;
  if (message.imageUrl) return '[gambar]';
  if (message.attachment) return `[lampiran: ${message.attachment.name}]`;
  return '';
}

// Sorot kemunculan kata kunci pencarian di dalam teks (untuk <mark>).
export function highlightParts(value: string, query: string): Array<string | { hit: string }> {
  const q = query.trim().toLowerCase();
  if (!q || !value.toLowerCase().includes(q)) return [value];
  const parts: Array<string | { hit: string }> = [];
  let rest = value;
  let lower = rest.toLowerCase();
  while (true) {
    const at = lower.indexOf(q);
    if (at < 0) { parts.push(rest); break; }
    parts.push(rest.slice(0, at), { hit: rest.slice(at, at + q.length) });
    rest = rest.slice(at + q.length);
    lower = rest.toLowerCase();
  }
  return parts;
}

export type MentionSegment = { text: string; seat?: Seat };

// Pecah teks pesan menjadi segmen biasa dan segmen mention (@Nama/@KODE
// kursi yang dikenal). "@" hanya dihitung bila tidak menempel pada kata
// sebelumnya (mis. email) dan nama tidak diikuti karakter kata lain.
export function splitMentions(text: string, seats: Seat[]): MentionSegment[] {
  if (!text || seats.length === 0) return [{ text }];
  const candidates: Array<{ label: string; seat: Seat }> = [];
  for (const seat of seats) {
    if (seat.name) candidates.push({ label: seat.name, seat });
    if (seat.code && seat.code !== seat.name) candidates.push({ label: seat.code, seat });
  }
  candidates.sort((a, b) => b.label.length - a.label.length);
  const isWord = (ch: string | undefined) => ch !== undefined && /[\p{L}\p{N}_-]/u.test(ch);
  const segments: MentionSegment[] = [];
  let plain = '';
  let i = 0;
  while (i < text.length) {
    if (text[i] === '@' && !isWord(text[i - 1])) {
      const rest = text.slice(i + 1);
      const lower = rest.toLowerCase();
      const hit = candidates.find(c => lower.startsWith(c.label.toLowerCase()) && !isWord(rest[c.label.length]));
      if (hit) {
        if (plain) { segments.push({ text: plain }); plain = ''; }
        segments.push({ text: text.slice(i, i + 1 + hit.label.length), seat: hit.seat });
        i += 1 + hit.label.length;
        continue;
      }
    }
    plain += text[i];
    i += 1;
  }
  if (plain) segments.push({ text: plain });
  return segments.length ? segments : [{ text }];
}
