// Pintasan perintah /slash di kotak pesan (fitur 34). Parser murni agar
// mudah diuji: teks diawali "/" dengan perintah dikenal diubah menjadi
// pesan pemilik biasa; perintah tidak dikenal dikirim apa adanya.

export type SlashCommandName = 'ringkas' | 'terjemah' | 'kode' | 'gambar' | 'bantu';

export type SlashCommand = { command: SlashCommandName; arg: string };

export const SLASH_COMMANDS: Array<{ name: SlashCommandName; usage: string; hint: string }> = [
  { name: 'ringkas', usage: '/ringkas', hint: 'Minta kursi integrator meringkas diskusi ruang ini' },
  { name: 'terjemah', usage: '/terjemah <teks>', hint: 'Terjemahkan teks ke Bahasa Indonesia' },
  { name: 'kode', usage: '/kode <permintaan>', hint: 'Minta kursi coder menulis kode' },
  { name: 'gambar', usage: '/gambar <deskripsi>', hint: 'Buat gambar dari deskripsi' },
  { name: 'bantu', usage: '/bantu', hint: 'Tampilkan daftar perintah ini' },
];

const KNOWN = new Set<string>(SLASH_COMMANDS.map((item) => item.name));

// parseSlashCommand membaca perintah dari teks. Mengembalikan null bila teks
// bukan perintah slash yang dikenal (termasuk "/" saja atau perintah asing).
export function parseSlashCommand(text: string): SlashCommand | null {
  const trimmed = text.trimStart();
  if (!trimmed.startsWith('/')) return null;
  const match = /^\/([a-zA-Z]+)(?:\s+([\s\S]*))?$/.exec(trimmed);
  if (!match) return null;
  const name = match[1].toLowerCase();
  if (!KNOWN.has(name)) return null;
  return { command: name as SlashCommandName, arg: (match[2] ?? '').trim() };
}

// slashNeedsArg: perintah yang tidak berguna tanpa argumen.
export function slashNeedsArg(command: SlashCommandName): boolean {
  return command === 'terjemah' || command === 'kode' || command === 'gambar';
}

// slashMessageText mengubah perintah menjadi teks pesan pemilik yang dikirim
// ke server. integratorName/coderName adalah nama kursi tujuan (untuk
// @mention). Mengembalikan null untuk /bantu (ditangani lokal) dan untuk
// perintah berargumen yang argumennya kosong.
export function slashMessageText(parsed: SlashCommand, integratorName: string, coderName: string): string | null {
  switch (parsed.command) {
    case 'bantu':
      return null;
    case 'ringkas':
      return `@${integratorName} Ringkas diskusi di ruang ini sejauh ini: poin utama, keputusan, dan hal yang masih terbuka.`;
    case 'terjemah':
      if (!parsed.arg) return null;
      return `@${integratorName} Terjemahkan ke Bahasa Indonesia: ${parsed.arg}`;
    case 'kode':
      if (!parsed.arg) return null;
      return `@${coderName} Tulis kode untuk permintaan berikut (sertakan penjelasan singkat): ${parsed.arg}`;
    case 'gambar':
      if (!parsed.arg) return null;
      return `Gambar: ${parsed.arg}`;
  }
}

export const SLASH_HELP_TEXT = [
  'Perintah cepat yang tersedia:',
  ...SLASH_COMMANDS.map((item) => `${item.usage} — ${item.hint}`),
].join('\n');
