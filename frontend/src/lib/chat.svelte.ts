import { api, apiDelete, errorText } from './api';
import type { Attachment, Message, Notice, ScheduledMessage, SearchFilters, SearchResult, State } from './types';

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

function applyTheme(result: State) {
  const theme = result.settings.theme ?? (result.settings.darkMode ? 'dark' : 'light');
  const dark = theme === 'dark' || (theme === 'auto' && typeof window !== 'undefined' && typeof window.matchMedia === 'function' && window.matchMedia('(prefers-color-scheme: dark)').matches);
  document.documentElement.classList.toggle('dark', dark);
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
  let serverResults = $state<SearchResult[]>([]);
  let timer: ReturnType<typeof setTimeout>;
  let searchTimer: ReturnType<typeof setTimeout>;
  let requested = 0;
  let applied = 0;
  let messageKey = '';
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
    const result = await api<State>('/api/state');
    if (request < applied) return;
    applied = request;
    const nextKey = JSON.stringify([result.messages, result.owner, result.group, result.seats.map(s => [s.name, s.avatar])]);
    const changed = nextKey !== messageKey;
    hooks.beforeApply?.();
    messageKey = nextKey;
    current = result;
    maybeNotify(result);
    applyTheme(result);
    markSeen(result);
    if (changed) await hooks.afterApply?.();
  }

  async function mutate(route: string, body: object) {
    await api(route, body);
    await refresh();
  }

  function startPolling() {
    if (typeof window !== 'undefined' && typeof window.matchMedia === 'function') {
      window.matchMedia('(prefers-color-scheme: dark)').addEventListener('change', () => {
        if (current?.settings.theme === 'auto') applyTheme(current);
      });
    }
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

  async function sendMessage(payload: { text: string; replyTo?: string; imageUrl?: string; attachment?: Attachment; templateId?: string }) {
    await api('/api/message', {
      text: payload.text,
      ...(current?.activeRoomId ? { roomId: current.activeRoomId } : {}),
      ...(payload.replyTo ? { replyTo: payload.replyTo } : {}),
      ...(payload.imageUrl ? { imageUrl: payload.imageUrl } : {}),
      ...(payload.attachment ? { attachment: payload.attachment } : {}),
      ...(payload.templateId ? { templateId: payload.templateId } : {}),
    });
    await refresh();
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
    await mutate(`/api/message/${message.id}/pin`, {});
  }

  async function toggleReact(message: Message, emoji: string) {
    await mutate(`/api/message/${message.id}/react`, { emoji });
  }

  async function removeMessage(message: Message) {
    await mutate(`/api/message/${message.id}/delete`, {});
  }

  async function saveEdit(id: string, text: string) {
    await mutate(`/api/message/${id}/edit`, { text });
  }

  async function regenerateMessage(message: Message) {
    await mutate(`/api/message/${message.id}/regenerate`, {});
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
    const hasFilters = Boolean(filters && (filters.seat || filters.room || filters.from || filters.to || filters.hasImage));
    if (!q && !hasFilters) { serverResults = []; return; }
    searchTimer = setTimeout(async () => {
      try {
        const params = new URLSearchParams({ q });
        if (filters?.seat) params.set('seat', filters.seat);
        if (filters?.room) params.set('room', filters.room);
        if (filters?.from) params.set('from', filters.from);
        if (filters?.to) params.set('to', filters.to);
        if (filters?.hasImage) params.set('hasImage', '1');
        const out = await api<{ results: SearchResult[] }>(`/api/search?${params.toString()}`);
        serverResults = out.results;
      } catch { /* pencarian lokal di ruang aktif tetap jalan */ }
    }, 300);
  }

  async function decideApproval(id: string, approved: boolean) {
    await api(`/api/approvals/${id}`, { approved });
    await refresh();
  }

  function clearSearchResults() {
    clearTimeout(searchTimer);
    serverResults = [];
  }

  return {
    get current() { return current; },
    get notice() { return notice; },
    get serverResults() { return serverResults; },
    get activeRoom() { return activeRoom; },
    get roomSeats() { return roomSeats; },
    get pinnedMessages() { return pinnedMessages; },
    get connectionReady() { return connectionReady; },
    notify,
    refresh,
    mutate,
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
    saveEdit,
    regenerateMessage,
    listScheduled,
    createScheduled,
    updateScheduled,
    deleteScheduled,
    uploadImage,
    uploadFile,
    scheduleSearch,
    clearSearchResults,
    decideApproval,
  };
}

export type Chat = ReturnType<typeof createChat>;
