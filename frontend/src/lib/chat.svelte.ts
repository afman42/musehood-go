import { api, errorText } from './api';
import type { Attachment, Message, Notice, SearchResult, State } from './types';

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
    document.documentElement.classList.toggle('dark', result.settings.darkMode);
    if (changed) await hooks.afterApply?.();
  }

  async function mutate(route: string, body: object) {
    await api(route, body);
    await refresh();
  }

  function startPolling() {
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

  async function sendMessage(payload: { text: string; replyTo?: string; imageUrl?: string; attachment?: Attachment }) {
    await api('/api/message', {
      text: payload.text,
      ...(current?.activeRoomId ? { roomId: current.activeRoomId } : {}),
      ...(payload.replyTo ? { replyTo: payload.replyTo } : {}),
      ...(payload.imageUrl ? { imageUrl: payload.imageUrl } : {}),
      ...(payload.attachment ? { attachment: payload.attachment } : {}),
    });
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

  async function uploadImage(file: File): Promise<string> {
    const body = new FormData();
    body.set('image', file);
    const out = await api<{ imageUrl: string }>('/api/upload-image', body);
    return out.imageUrl;
  }

  async function uploadFile(file: File): Promise<Attachment> {
    const body = new FormData();
    body.set('file', file);
    return api<Attachment>('/api/upload-file', body);
  }

  function scheduleSearch(q: string) {
    clearTimeout(searchTimer);
    if (!q) { serverResults = []; return; }
    searchTimer = setTimeout(async () => {
      try {
        const out = await api<{ results: SearchResult[] }>(`/api/search?q=${encodeURIComponent(q)}`);
        serverResults = out.results;
      } catch { /* pencarian lokal di ruang aktif tetap jalan */ }
    }, 300);
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
    progress,
    startVote,
    activateRoom,
    createRoom,
    togglePin,
    toggleReact,
    removeMessage,
    saveEdit,
    uploadImage,
    uploadFile,
    scheduleSearch,
    clearSearchResults,
  };
}

export type Chat = ReturnType<typeof createChat>;
