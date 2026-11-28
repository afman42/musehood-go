import { api, apiDelete, errorText } from './api';
import type { Attachment, Message, Notice, OllamaModel, PromptChain, RolePreset, Room, RoomDocument, ScheduledMessage, SearchFilters, SearchResult, State } from './types';

export const OUTBOX_KEY = 'musehood-outbox';

type OutboxItem = { text: string; roomId?: string; replyTo?: string; imageUrl?: string; attachment?: Attachment; templateId?: string; author?: string };

function loadOutbox(): OutboxItem[] {
  try {
    const raw = localStorage.getItem(OUTBOX_KEY);
    const parsed = raw ? JSON.parse(raw) : [];
    return Array.isArray(parsed) ? parsed : [];
  } catch { return []; }
}

function saveOutbox(items: OutboxItem[]) {
  try {
    if (items.length === 0) localStorage.removeItem(OUTBOX_KEY);
    else localStorage.setItem(OUTBOX_KEY, JSON.stringify(items));
  } catch { /* penyimpanan tidak tersedia */ }
}

export const NOTIFY_KEY = 'musehood-notify';

const SEEN_KEY = 'musehood-seencount-';

export function seenCount(roomId: string): number {
  try {
    return Number(localStorage.getItem(SEEN_KEY + roomId) ?? 0) || 0;
  } catch {
    return 0;
  }
}

function markSeen(result: State) {
  try {
    const room = result.rooms.find((r) => r.id === result.activeRoomId);
    if (room) localStorage.setItem(SEEN_KEY + room.id, String(room.messageCount ?? result.messages.length));
  } catch {
    // abaikan
  }
}

export function unreadCount(room: { id: string; messageCount?: number }): number {
  return Math.max(0, (room.messageCount ?? 0) - seenCount(room.id));
}

const FONT_SCALES: Record<string, string> = { S: '0.9', M: '1', L: '1.15' };

// Pilihan tema optimistis: tombol tema membalik kelas `dark` seketika,
// sebelum POST + refresh selesai. Selama pilihan ini belum tercermin di
// state server, applyTheme tidak boleh membalikkannya kembali — polling
// yang membawa settings lama akan membuat tema berkedip.
let pendingDark: boolean | null = null;

function effectiveDark(settings: State['settings']): boolean {
  const theme = settings.theme ?? (settings.darkMode ? 'dark' : 'light');
  return theme === 'dark' || (theme === 'auto' && typeof window !== 'undefined' && typeof window.matchMedia === 'function' && window.matchMedia('(prefers-color-scheme: dark)').matches);
}

function applyTheme(result: State) {
  const serverDark = effectiveDark(result.settings);
  if (pendingDark !== null && serverDark === pendingDark) pendingDark = null;
  const dark = pendingDark ?? serverDark;
  document.documentElement.classList.toggle('dark', dark);
  document.documentElement.classList.toggle('density-compact', result.settings.density === 'compact');
  document.documentElement.style?.setProperty('--font-scale', FONT_SCALES[result.settings.fontSize ?? 'M'] ?? '1');
}

export function notificationsEnabled(): boolean {
  try {
    return localStorage.getItem(NOTIFY_KEY) === '1' && typeof Notification !== 'undefined' && Notification.permission === 'granted';
  } catch {
    return false;
  }
}

export type ChatHooks = {
  // Dipanggil tepat sebelum state baru diterapkan (mis. untuk mengukur
  // posisi scroll), dan sesudahnya bila pesan/identitas berubah.
  beforeApply?: () => void;
  afterApply?: () => void | Promise<void>;
};

// Composable state chat: memegang state server, polling, notifikasi, dan
// semua aksi API. Halaman hanya mengurus DOM (scroll, fokus, dialog).
export function createChat(hooks: ChatHooks = {}) {
  let current = $state.raw<State>();
  let notice = $state<Notice>();
  let pinRequired = $state(false);
  let online = $state(typeof navigator === 'undefined' || typeof navigator.onLine !== 'boolean' ? true : navigator.onLine);
  let serverResults = $state<SearchResult[]>([]);
  let timer: ReturnType<typeof setTimeout>;
  let searchTimer: ReturnType<typeof setTimeout>;
  let requested = 0;
  let applied = 0;
  let messageKey = '';
  // Signature JSON state terakhir yang diterapkan. Polling yang tidak
  // membawa perubahan tidak mengganti `current`, sehingga Svelte tidak
  // me-render ulang seluruh daftar pesan tiap 700 ms (pada ruang besar
  // render ulang itu memblokir thread utama ratusan ms dan membuat
  // ketikan di composer terasa berat).
  let stateSignature = '';
  // Signature JSON daftar kursi terakhir; dipakai refresh() untuk
  // memutuskan apakah array pesan/kursi lama bisa dipakai ulang.
  let lastSeatKey = '';
  const notifiedIds = new Set<string>();
  let notificationsPrimed = false;

  // Notifikasi browser: balasan kursi yang selesai saat tab tersembunyi.
  function maybeNotify(result: State) {
    const completed = result.messages.filter(m => m.authorType === 'seat' && m.status === 'completed');
    if (!notificationsPrimed) {
      for (const m of completed) notifiedIds.add(m.id);
      notificationsPrimed = true;
      return;
    }
    for (const m of completed) {
      if (notifiedIds.has(m.id)) continue;
      notifiedIds.add(m.id);
      if (!document.hidden || !notificationsEnabled()) continue;
      const seat = result.seats.find(s => s.code === m.seat);
      const body = (m.text || 'Balasan baru').slice(0, 120);
      try {
        new Notification(`${seat?.name ?? m.seat ?? 'Kursi'} membalas`, { body, tag: m.id });
      } catch { /* notifikasi tidak tersedia */ }
    }
  }

  const activeRoom = $derived.by(() => current?.rooms?.find(r => r.id === current?.activeRoomId));
  const roomSeats = $derived.by(() => {
    if (!current) return [];
    const codes = activeRoom?.seatCodes;
    if (!codes || codes.length === 0) return current.seats;
    return current.seats.filter(s => codes.includes(s.code));
  });
  const pinnedMessages = $derived((current?.messages ?? []).filter(m => m.pinned));
  const connectionReady = $derived(Boolean(current) && Boolean(current!.settings.baseUrl) && (current!.settings.gatewayProtocol === 'ollama' || current!.settings.hasApiKey));

  function notify(text: string, error = false) {
    const target = document.querySelector<HTMLDialogElement>('dialog[open]')?.id ?? null;
    notice = { text, error, target };
    clearTimeout(timer);
    timer = setTimeout(() => { notice = undefined; }, error ? 6500 : 3500);
  }

  async function refresh() {
    const request = ++requested;
    let result: State;
    try {
      result = await api<State>('/api/state');
    } catch (error) {
      // PIN aktif dan sesi belum masuk: tampilkan layar login, bukan error.
      if (errorText(error) === 'PIN diperlukan') { pinRequired = true; return; }
      throw error;
    }
    pinRequired = false;
    if (request < applied) return;
    applied = request;
    const nextKey = JSON.stringify([result.messages, result.owner, result.group, result.seats.map(s => [s.name, s.avatar])]);
    const changed = nextKey !== messageKey;
    hooks.beforeApply?.();
    messageKey = nextKey;
    const signature = JSON.stringify(result);
    if (signature !== stateSignature) {
      stateSignature = signature;
      // Bila isi pesan & kursi tidak berubah (mis. hanya settings yang
      // berubah, seperti ganti tema), pakai ulang referensi array lama
      // agar Svelte tidak me-render ulang ratusan bubble — render ulang
      // itu memblokir thread utama ratusan ms pada ruang besar dan
      // membuat klik tombol terasa lambat.
      const seatKey = JSON.stringify(result.seats);
      if (current && !changed && seatKey === lastSeatKey) {
        result = { ...result, messages: current.messages, seats: current.seats };
      } else if (current) {
        // Pesan berubah sebagian (mis. satu rating/pin/reaksi, atau
        // satu balasan streaming): pakai ulang objek pesan lama yang
        // isinya identik agar bubble lain tidak ikut dirender ulang.
        const prevById = new Map(current.messages.map(m => [m.id, m]));
        result = {
          ...result,
          messages: result.messages.map(m => {
            const prev = prevById.get(m.id);
            return prev && JSON.stringify(prev) === JSON.stringify(m) ? prev : m;
          })
        };
      }
      lastSeatKey = seatKey;
      current = result;
    }
    maybeNotify(result);
    applyTheme(result);
    markSeen(result);
    if (changed) await hooks.afterApply?.();
  }

  async function mutate(route: string, body: object) {
    await api(route, body);
    await refresh();
  }

  // Pembaruan optimistis untuk aksi toggle kecil (rating, pin pesan,
  // reaksi, pin ruang, hapus pesan, pilih pemenang A/B): UI berubah
  // SEKETIKA, server menyusul di latar belakang. Tanpa ini klik
  // terasa lambat karena harus menunggu POST + GET /api/state (di
  // tunnel publik bisa >1 detik). Bila server menolak, keadaan
  // dipaksa dimuat ulang dari server agar tidak menyimpang.
  function patchMessageLocal(id: string, patch: Partial<Message>) {
    if (!current) return;
    current = { ...current, messages: current.messages.map(m => m.id === id ? { ...m, ...patch } : m) };
  }

  function patchRoomLocal(id: string, patch: Partial<Room>) {
    if (!current?.rooms) return;
    current = { ...current, rooms: current.rooms.map(r => r.id === id ? { ...r, ...patch } : r) };
  }

  async function optimistic(route: string, body: object, apply: () => void) {
    apply();
    try {
      await api(route, body);
      await refresh();
    } catch (error) {
      stateSignature = '';
      await refresh().catch(() => { /* keadaan server tetap dimuat polling berikutnya */ });
      throw error;
    }
  }

  // Ganti tema secara optimistis: kelas `dark` dibalik SEKETIKA agar
  // terasa instan, lalu disimpan ke server di latar belakang. Tanpa
  // ini, tema baru berubah setelah POST + GET /api/state selesai
  // (di tunnel publik bisa >1 detik) dan halaman membeku selama
  // render ulang. darkMode dan theme ditulis bersama agar tombol
  // selalu berpengaruh walau tema pernah diatur lewat Pengaturan.
  async function toggleTheme() {
    if (!current) return;
    const target = !(pendingDark ?? effectiveDark(current.settings));
    pendingDark = target;
    document.documentElement.classList.toggle('dark', target);
    try {
      await mutate('/api/settings', { settings: { darkMode: target, theme: target ? 'dark' : 'light' } });
    } catch (error) {
      pendingDark = null;
      if (current) applyTheme(current);
      throw error;
    }
  }

  async function login(pin: string) {
    await api('/api/login', { pin });
    pinRequired = false;
    await refresh();
  }

  async function logout() {
    await api('/api/logout', {});
    current = undefined;
    stateSignature = '';
    messageKey = '';
    pinRequired = true;
  }

  function startPolling() {
    if (typeof window !== 'undefined' && typeof window.matchMedia === 'function') {
      window.matchMedia('(prefers-color-scheme: dark)').addEventListener('change', () => {
        if (current?.settings.theme === 'auto') applyTheme(current);
      });
    }
    // Deteksi offline (fitur 28): banner + antrean pesan keluar yang
    // otomatis terkirim saat koneksi kembali.
    if (typeof window !== 'undefined') {
      window.addEventListener('online', () => { online = true; void flushOutbox(); });
      window.addEventListener('offline', () => { online = false; });
    }
    void flushOutbox();
    let stopped = false;
    let pollTimer: ReturnType<typeof setTimeout>;
    let pollError = false;
    async function poll() {
      try { await refresh(); pollError = false; }
      catch (error) {
        if (!stopped && !pollError) notify(`Koneksi server terputus: ${errorText(error)}`, true);
        pollError = true;
      }
      if (!stopped) pollTimer = setTimeout(poll, document.hidden ? 3000 : 700);
    }
    void poll();
    return () => { stopped = true; clearTimeout(pollTimer); clearTimeout(timer); };
  }

  async function sendMessage(payload: { text: string; replyTo?: string; imageUrl?: string; attachment?: Attachment; templateId?: string; author?: string }) {
    const item: OutboxItem = {
      text: payload.text,
      ...(current?.activeRoomId ? { roomId: current.activeRoomId } : {}),
      ...(payload.replyTo ? { replyTo: payload.replyTo } : {}),
      ...(payload.imageUrl ? { imageUrl: payload.imageUrl } : {}),
      ...(payload.attachment ? { attachment: payload.attachment } : {}),
      ...(payload.templateId ? { templateId: payload.templateId } : {}),
      ...(payload.author ? { author: payload.author } : {}),
    };
    if (!online) {
      saveOutbox([...loadOutbox(), item]);
      notify('Offline — pesan masuk antrean dan akan terkirim saat online kembali.');
      return;
    }
    try {
      await api('/api/message', item);
    } catch (error) {
      // Kegagalan jaringan (fetch TypeError) → antre; error server (4xx/5xx
      // dengan pesan) diteruskan ke pemanggil.
      if (error instanceof TypeError) {
        online = false;
        saveOutbox([...loadOutbox(), item]);
        notify('Koneksi terputus — pesan masuk antrean dan akan terkirim saat online kembali.');
        return;
      }
      throw error;
    }
    await refresh();
  }

  let flushing = false;
  async function flushOutbox() {
    if (flushing || !online) return;
    const items = loadOutbox();
    if (items.length === 0) return;
    flushing = true;
    try {
      const remaining: OutboxItem[] = [];
      for (let i = 0; i < items.length; i++) {
        try {
          await api('/api/message', items[i]);
        } catch (error) {
          if (error instanceof TypeError) {
            online = false;
            remaining.push(...items.slice(i));
            break;
          }
          // Error server untuk item ini: buang agar tidak menyumbat antrean.
        }
      }
      saveOutbox(remaining);
      if (remaining.length === 0) await refresh();
    } finally {
      flushing = false;
    }
  }

  async function summarizeRoom() {
    if (!current?.activeRoomId) return;
    await api(`/api/rooms/${current.activeRoomId}/summarize`, {});
    notify('Ringkasan diskusi sedang dibuat oleh kursi integrator.');
    await refresh();
  }

  async function progress() {
    await mutate('/api/progress', {});
  }

  async function startVote() {
    await api('/api/vote', { roomId: current?.activeRoomId ?? '' });
    notify('Voting dimulai — kursi gateway sedang menilai jawaban terakhir.');
    await refresh();
  }

  async function activateRoom(id: string) {
    await api(`/api/rooms/${id}/activate`, {});
    await refresh();
  }

  async function createRoom(name: string, seatCodes: string[]) {
    await api('/api/rooms', { name, seatCodes });
    await refresh();
  }

  async function togglePin(message: Message) {
    await optimistic(`/api/message/${message.id}/pin`, {}, () => patchMessageLocal(message.id, { pinned: !message.pinned }));
  }

  async function toggleReact(message: Message, emoji: string) {
    await optimistic(`/api/message/${message.id}/react`, { emoji }, () => {
      const list = [...(message.reactions ?? [])];
      const i = list.findIndex(r => r.emoji === emoji);
      if (i >= 0 && list[i].byOwner) {
        if (list[i].count <= 1) list.splice(i, 1);
        else list[i] = { ...list[i], count: list[i].count - 1, byOwner: false };
      } else if (i >= 0) {
        list[i] = { ...list[i], count: list[i].count + 1, byOwner: true };
      } else {
        list.push({ emoji, count: 1, byOwner: true });
      }
      patchMessageLocal(message.id, { reactions: list });
    });
  }

  async function removeMessage(message: Message) {
    await optimistic(`/api/message/${message.id}/delete`, {}, () => {
      if (!current) return;
      current = { ...current, messages: current.messages.filter(m => m.id !== message.id) };
    });
  }

  async function toggleRoomPin(room: Room) {
    await optimistic(`/api/rooms/${room.id}`, { pinned: !room.pinned }, () => patchRoomLocal(room.id, { pinned: !room.pinned }));
  }

  async function saveEdit(id: string, text: string) {
    await mutate(`/api/message/${id}/edit`, { text });
  }

  async function regenerateMessage(message: Message) {
    await mutate(`/api/message/${message.id}/regenerate`, {});
  }

  async function rerunMessage(message: Message) {
    await mutate(`/api/message/${message.id}/rerun`, {});
  }

  async function forkMessage(message: Message) {
    await mutate(`/api/message/${message.id}/fork`, {});
  }

  async function branchMessage(message: Message) {
    await mutate(`/api/message/${message.id}/branch`, {});
  }

  async function switchBranch(roomId: string, branchId: string) {
    await mutate(`/api/rooms/${roomId}/branch`, { branchId });
  }

  async function setMessageTags(message: Message, tags: string[]) {
    await mutate(`/api/message/${message.id}/tags`, { tags });
  }

  async function startDebate(seatA: string, seatB: string, topic: string) {
    await mutate('/api/debate', { seatA, seatB, topic, roomId: current?.activeRoomId ?? '' });
  }

  async function listScheduled(): Promise<ScheduledMessage[]> {
    const out = await api<{ scheduled: ScheduledMessage[] }>('/api/scheduled');
    return out.scheduled ?? [];
  }

  async function createScheduled(body: object): Promise<ScheduledMessage> {
    return api<ScheduledMessage>('/api/scheduled', body);
  }

  async function updateScheduled(id: string, body: object): Promise<ScheduledMessage> {
    return api<ScheduledMessage>(`/api/scheduled/${id}`, body);
  }

  async function deleteScheduled(id: string) {
    await apiDelete(`/api/scheduled/${id}`);
  }

  // Unggah FormData dengan laporan kemajuan (0-100) lewat XHR; di
  // lingkungan tanpa XHR (tes Node) kembali ke fetch biasa.
  function uploadForm<T>(route: string, body: FormData, onProgress?: (pct: number) => void): Promise<T> {
    if (typeof XMLHttpRequest === 'undefined' || !onProgress) return api<T>(route, body);
    return new Promise<T>((resolve, reject) => {
      const xhr = new XMLHttpRequest();
      xhr.open('POST', route);
      xhr.upload.onprogress = event => {
        if (event.lengthComputable) onProgress(Math.round((event.loaded / event.total) * 100));
      };
      xhr.onload = () => {
        let result;
        try { result = JSON.parse(xhr.responseText); } catch { reject(new Error('Respons server tidak valid.')); return; }
        if (xhr.status < 200 || xhr.status >= 300) { reject(new Error(result.error || `HTTP ${xhr.status}`)); return; }
        onProgress(100);
        resolve(result as T);
      };
      xhr.onerror = () => reject(new Error('Unggahan gagal: koneksi terputus.'));
      xhr.send(body);
    });
  }

  async function uploadImage(file: File, onProgress?: (pct: number) => void): Promise<string> {
    const body = new FormData();
    body.set('image', file);
    const out = await uploadForm<{ imageUrl: string }>('/api/upload-image', body, onProgress);
    return out.imageUrl;
  }

  async function uploadFile(file: File, onProgress?: (pct: number) => void): Promise<Attachment> {
    const body = new FormData();
    body.set('file', file);
    return uploadForm<Attachment>('/api/upload-file', body, onProgress);
  }

  function scheduleSearch(q: string, filters?: SearchFilters) {
    clearTimeout(searchTimer);
    const hasFilters = Boolean(filters && (filters.seat || filters.room || filters.from || filters.to || filters.hasImage || filters.includeArchived || filters.tag));
    if (!q && !hasFilters) { serverResults = []; return; }
    searchTimer = setTimeout(async () => {
      try {
        const params = new URLSearchParams({ q });
        if (filters?.seat) params.set('seat', filters.seat);
        if (filters?.room) params.set('room', filters.room);
        if (filters?.from) params.set('from', filters.from);
        if (filters?.to) params.set('to', filters.to);
        if (filters?.hasImage) params.set('hasImage', '1');
        if (filters?.includeArchived) params.set('includeArchived', '1');
        if (filters?.tag) params.set('tag', filters.tag);
        const out = await api<{ results: SearchResult[] }>(`/api/search?${params.toString()}`);
        serverResults = out.results;
      } catch { /* pencarian lokal di ruang aktif tetap jalan */ }
    }, 300);
  }

  async function decideApproval(id: string, approved: boolean) {
    await api(`/api/approvals/${id}`, { approved });
    await refresh();
  }

  async function answerQuestion(id: string, answer: string) {
    await api(`/api/questions/${id}`, { answer });
    await refresh();
  }

  function clearSearchResults() {
    clearTimeout(searchTimer);
    serverResults = [];
  }

  // --- Fitur batch 3 ---

  async function rateMessage(message: Message, rating: number) {
    await optimistic(`/api/message/${message.id}/rate`, { rating }, () => patchMessageLocal(message.id, { rating }));
  }

  async function startABTest(text: string, modelA: string, modelB: string) {
    await mutate('/api/abtest', { text, modelA, modelB, roomId: current?.activeRoomId ?? '' });
  }

  async function pickABWinner(group: string, messageId: string) {
    await optimistic(`/api/abtest/${group}/pick`, { messageId }, () => {
      if (!current) return;
      current = { ...current, messages: current.messages.map(m => m.abGroup === group ? { ...m, abWinner: m.id === messageId } : m) };
    });
  }

  async function createChain(name: string, steps: Array<{ seatCode: string; instruction: string }>): Promise<PromptChain> {
    const out = await api<PromptChain>('/api/chains', { name, steps });
    await refresh();
    return out;
  }

  async function updateChain(id: string, body: object) {
    await mutate(`/api/chains/${id}`, body);
  }

  async function deleteChain(id: string) {
    await mutate(`/api/chains/${id}/delete`, {});
  }

  async function runChain(id: string, text: string) {
    await mutate(`/api/chains/${id}/run`, { text, roomId: current?.activeRoomId ?? '' });
  }

  async function addParticipant(roomId: string, name: string) {
    await mutate(`/api/rooms/${roomId}/participants`, { name });
  }

  async function removeParticipant(roomId: string, name: string) {
    await mutate(`/api/rooms/${roomId}/participants/remove`, { name });
  }

  async function listDocuments(roomId: string): Promise<RoomDocument[]> {
    const out = await api<{ documents: RoomDocument[] }>(`/api/rooms/${roomId}/documents`);
    return out.documents ?? [];
  }

  async function uploadDocument(roomId: string, file: File): Promise<RoomDocument> {
    const form = new FormData();
    form.set('file', file);
    const out = await api<RoomDocument>(`/api/rooms/${roomId}/documents`, form);
    await refresh();
    return out;
  }

  async function deleteDocument(roomId: string, docId: string) {
    await mutate(`/api/rooms/${roomId}/documents/${docId}/delete`, {});
  }

  let rolesCache: RolePreset[] | undefined;
  async function fetchRoles(): Promise<RolePreset[]> {
    if (!rolesCache) {
      const out = await api<{ roles: RolePreset[] }>('/api/roles');
      rolesCache = out.roles ?? [];
    }
    return rolesCache;
  }

  async function detectOllama(baseUrl: string): Promise<OllamaModel[]> {
    const out = await api<{ models: OllamaModel[] }>('/api/ollama/detect', { baseUrl });
    return out.models ?? [];
  }

  async function addOllama(baseUrl: string, model: string) {
    await api('/api/ollama/add', { baseUrl, model });
    await refresh();
  }

  return {
    get current() { return current; },
    get notice() { return notice; },
    get pinRequired() { return pinRequired; },
    get online() { return online; },
    login,
    logout,
    get serverResults() { return serverResults; },
    get activeRoom() { return activeRoom; },
    get roomSeats() { return roomSeats; },
    get pinnedMessages() { return pinnedMessages; },
    get connectionReady() { return connectionReady; },
    notify,
    refresh,
    mutate,
    toggleTheme,
    startPolling,
    sendMessage,
    summarizeRoom,
    progress,
    startVote,
    activateRoom,
    createRoom,
    togglePin,
    toggleReact,
    removeMessage,
    toggleRoomPin,
    saveEdit,
    regenerateMessage,
    rerunMessage,
    forkMessage,
    branchMessage,
    switchBranch,
    setMessageTags,
    startDebate,
    listScheduled,
    createScheduled,
    updateScheduled,
    deleteScheduled,
    uploadImage,
    uploadFile,
    scheduleSearch,
    clearSearchResults,
    decideApproval,
    answerQuestion,
    rateMessage,
    startABTest,
    pickABWinner,
    createChain,
    updateChain,
    deleteChain,
    runChain,
    addParticipant,
    removeParticipant,
    listDocuments,
    uploadDocument,
    deleteDocument,
    fetchRoles,
    detectOllama,
    addOllama,
  };
}

export type Chat = ReturnType<typeof createChat>;
