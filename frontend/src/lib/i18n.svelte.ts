// i18n sederhana untuk chrome antarmuka utama (ID/EN).
// Balasan kursi tidak diterjemahkan.

export type Locale = 'id' | 'en';

const LOCALE_KEY = 'musehood-locale';

const dict: Record<string, Record<Locale, string>> = {
  settings: { id: 'Pengaturan', en: 'Settings' },
  darkMode: { id: 'Mode gelap/terang', en: 'Dark/light mode' },
  search: { id: 'Cari pesan…', en: 'Search messages…' },
  composerPlaceholder: { id: 'Tulis pesan ke grup… (@nama untuk memanggil kursi, "Tugas:" untuk pipeline, "Gambar:" untuk gambar)', en: 'Write a message to the group… (@name to call a seat, "Tugas:" for a pipeline, "Gambar:" for an image)' },
  send: { id: 'Kirim', en: 'Send' },
  sending: { id: 'Mengirim…', en: 'Sending…' },
  progress: { id: '📊 Tanya Progres', en: '📊 Ask Progress' },
  vote: { id: '⭐ Voting Jawaban', en: '⭐ Vote Answers' },
  mention: { id: '@ Sebut', en: '@ Mention' },
  image: { id: '🖼 Gambar', en: '🖼 Image' },
  uploadImage: { id: '📷 Unggah Gambar', en: '📷 Upload Image' },
  uploadFile: { id: '📎 Unggah File', en: '📎 Upload File' },
  schedule: { id: '⏰ Jadwalkan', en: '⏰ Schedule' },
  noTemplate: { id: 'Tanpa template', en: 'No template' },
  newRoom: { id: '＋ Ruang', en: '＋ Room' },
  summarize: { id: '📝 Ringkas', en: '📝 Summarize' },
  rooms: { id: 'Ruang grup', en: 'Group rooms' },
  pinnedMessages: { id: 'Pesan tersemat', en: 'Pinned messages' },
  reply: { id: 'Balas', en: 'Reply' },
  edit: { id: 'Ubah', en: 'Edit' },
  delete: { id: 'Hapus', en: 'Delete' },
  pin: { id: 'Sematkan', en: 'Pin' },
  unpin: { id: 'Lepas sematan', en: 'Unpin' },
  react: { id: 'Reaksi', en: 'React' },
  regenerate: { id: 'Ulangi jawaban', en: 'Regenerate answer' },
  save: { id: 'Simpan', en: 'Save' },
  cancel: { id: 'Batal', en: 'Cancel' },
  close: { id: 'Tutup', en: 'Close' },
  language: { id: 'Bahasa antarmuka', en: 'Interface language' },
  theme: { id: 'Tema tampilan', en: 'Appearance theme' },
  themeLight: { id: 'Terang', en: 'Light' },
  themeDark: { id: 'Gelap', en: 'Dark' },
  themeAuto: { id: 'Otomatis (ikuti sistem)', en: 'Automatic (follow system)' },
  fontSize: { id: 'Ukuran font', en: 'Font size' },
  fontSmall: { id: 'Kecil', en: 'Small' },
  fontMedium: { id: 'Sedang', en: 'Medium' },
  fontLarge: { id: 'Besar', en: 'Large' },
  notifications: { id: 'Notifikasi browser', en: 'Browser notifications' },
  streaming: { id: 'Streaming balasan', en: 'Streaming replies' },
  scheduleTitle: { id: '⏰ Jadwalkan pesan', en: '⏰ Schedule message' },
  scheduleOnce: { id: 'Sekali kirim', en: 'Send once' },
  scheduleDaily: { id: 'Harian', en: 'Daily' },
  scheduleWeekly: { id: 'Mingguan', en: 'Weekly' },
  scheduleInterval: { id: 'Setiap N jam', en: 'Every N hours' },
  scheduleList: { id: 'Jadwal tersimpan', en: 'Saved schedules' },
  seatInfo: { id: 'Info kursi', en: 'Seat info' },
  mentionSeat: { id: 'Mention', en: 'Mention' },
  filters: { id: 'Filter lanjutan', en: 'Advanced filters' },
  clearFilters: { id: 'Bersihkan filter', en: 'Clear filters' },
  sender: { id: 'Pengirim', en: 'Sender' },
  allSenders: { id: 'Semua pengirim', en: 'All senders' },
  owner: { id: 'Pemilik', en: 'Owner' },
  allRooms: { id: 'Semua ruang', en: 'All rooms' },
  hasImage: { id: 'Ada gambar', en: 'Has image' },
  dateFrom: { id: 'Dari tanggal', en: 'From date' },
  dateTo: { id: 'Sampai tanggal', en: 'To date' },
  unread: { id: 'belum dibaca', en: 'unread' },
  pinRoom: { id: 'Sematkan ruang', en: 'Pin room' },
  unpinRoom: { id: 'Lepas sematan ruang', en: 'Unpin room' },
  autoMemory: { id: 'Memori otomatis', en: 'Auto memory' },
  autoMemoryHint: { id: 'Setiap ringkasan ruang dibuat, kursi ini menulis 1–3 poin memori singkat dari ringkasan itu ke memorinya sendiri.', en: 'Whenever a room summary is created, this seat writes 1–3 short memory points from it into its own memory.' },
  dailyBudget: { id: 'Budget harian (USD, 0 = tanpa batas)', en: 'Daily budget (USD, 0 = unlimited)' },
  dailyBudgetHint: { id: 'Bila biaya hari ini (dari harga token provider) mencapai budget, kursi dilewati sampai besok.', en: 'When today\'s cost (from provider token prices) reaches the budget, the seat is skipped until tomorrow.' },
  taskModels: { id: 'Model per jenis tugas (opsional)', en: 'Model per task type (optional)' },
  modelChatPh: { id: 'Model chat biasa (kosong = model utama)', en: 'Plain chat model (empty = main model)' },
  modelToolsPh: { id: 'Model putaran dengan tool (kosong = model utama)', en: 'Tool-round model (empty = main model)' },
  modelSummaryPh: { id: 'Model ringkasan & memori otomatis (kosong = model utama)', en: 'Summary & auto-memory model (empty = main model)' },
  taskModelsHint: { id: 'Boleh berformat alias/model. Model tugas dipakai lebih dulu; bila gagal, rantai fallback tetap dicoba.', en: 'May use alias/model format. The task model is tried first; on failure the fallback chain still applies.' },
  cloneSeat: { id: 'Kloning kursi ini', en: 'Clone this seat' },
};

function loadLocale(): Locale {
  try {
    const saved = localStorage.getItem(LOCALE_KEY);
    return saved === 'en' ? 'en' : 'id';
  } catch {
    return 'id';
  }
}

export const i18n = $state<{ locale: Locale }>({ locale: loadLocale() });

export function setLocale(locale: Locale) {
  i18n.locale = locale;
  try {
    localStorage.setItem(LOCALE_KEY, locale);
  } catch {
    // abaikan
  }
}

export function t(key: string): string {
  const entry = dict[key];
  if (!entry) return key;
  return entry[i18n.locale] ?? entry.id;
}
