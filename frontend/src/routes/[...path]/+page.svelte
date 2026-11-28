<script lang="ts">
  import { onMount, tick } from 'svelte';
  import Toast from '../../lib/Toast.svelte';
  import RoomBar from '../../lib/RoomBar.svelte';
  import RoomWorkPanel from '../../lib/RoomWorkPanel.svelte';
  import SearchBar from '../../lib/SearchBar.svelte';
  import SeatStrip from '../../lib/SeatStrip.svelte';
  import PinnedBar from '../../lib/PinnedBar.svelte';
  import MessageBubble from '../../lib/MessageBubble.svelte';
  import Composer from '../../lib/Composer.svelte';
  import { shortcutAction } from '../../lib/shortcuts';
  import { parseSlashCommand, slashMessageText, SLASH_HELP_TEXT } from '../../lib/slash';
  import { nextRating } from '../../lib/rating';
  import ApprovalBanner from '../../lib/ApprovalBanner.svelte';
  import QuestionBanner from '../../lib/QuestionBanner.svelte';
  import { createChat } from '../../lib/chat.svelte';
  import { errorText } from '../../lib/api';
  import { emptySearchFilters, type Attachment, type Message, type SearchFilters, type SearchResult, type Seat } from '../../lib/types';
  // Komponen berat (panel Pengaturan + dialog) dimuat malas lewat dynamic
  // import agar tidak masuk muatan awal (code splitting). Impor tipe saja
  // di sini — tidak menghasilkan kode runtime.
  import type SettingsType from '../../lib/Settings.svelte';
  import type MentionDialogType from '../../lib/MentionDialog.svelte';
  import type SeatInfoDialogType from '../../lib/SeatInfoDialog.svelte';
  import type ScheduleDialogType from '../../lib/ScheduleDialog.svelte';
  import type DebateDialogType from '../../lib/DebateDialog.svelte';
  import type ABTestDialogType from '../../lib/ABTestDialog.svelte';
  import type ShortcutHelpDialogType from '../../lib/ShortcutHelpDialog.svelte';
  import type RoomDialogType from '../../lib/RoomDialog.svelte';

  // Referensi DOM.
  let messages: HTMLElement;
  let settingsPanel = $state<ReturnType<typeof SettingsType>>();
  let mentionDialog = $state<ReturnType<typeof MentionDialogType>>();
  let seatInfoDialog = $state<ReturnType<typeof SeatInfoDialogType>>();
  let scheduleDialog = $state<ReturnType<typeof ScheduleDialogType>>();
  let debateDialog = $state<ReturnType<typeof DebateDialogType>>();
  let abDialog = $state<ReturnType<typeof ABTestDialogType>>();
  let shortcutHelp = $state<ReturnType<typeof ShortcutHelpDialogType>>();
  let roomDialog = $state<ReturnType<typeof RoomDialogType>>();
  let composer = $state<ReturnType<typeof Composer>>();
  let searchBar = $state<ReturnType<typeof SearchBar>>();

  // Pemegang komponen malas: undefined sampai pertama kali dibutuhkan.
  let Settings = $state.raw<typeof SettingsType | undefined>(undefined);
  let MentionDialog = $state.raw<typeof MentionDialogType | undefined>(undefined);
  let SeatInfoDialog = $state.raw<typeof SeatInfoDialogType | undefined>(undefined);
  let ScheduleDialog = $state.raw<typeof ScheduleDialogType | undefined>(undefined);
  let DebateDialog = $state.raw<typeof DebateDialogType | undefined>(undefined);
  let ABTestDialog = $state.raw<typeof ABTestDialogType | undefined>(undefined);
  let ShortcutHelpDialog = $state.raw<typeof ShortcutHelpDialogType | undefined>(undefined);
  let RoomDialog = $state.raw<typeof RoomDialogType | undefined>(undefined);

  // Indikator muatan dialog malas: chunk dialog (terutama Pengaturan
  // 82 KB) diunduh saat pertama dibuka; lewat tunnel publik itu bisa
  // >1 detik tanpa umpan balik. Spinner ditampilkan hanya bila muatan
  // melewati 150 ms agar buka-ulang yang sudah hangat tidak berkedip.
  let loadingDialog = $state<string | null>(null);
  let loadingTimer: ReturnType<typeof setTimeout> | undefined;

  async function withDialogLoading<T>(name: string, load: () => Promise<T>): Promise<T> {
    loadingTimer = setTimeout(() => { loadingDialog = name; }, 150);
    try {
      return await load();
    } finally {
      clearTimeout(loadingTimer);
      loadingDialog = null;
    }
  }

  async function openSettings() {
    await withDialogLoading('Pengaturan', async () => {
      Settings ??= (await import('../../lib/Settings.svelte')).default;
      await tick();
      await settingsPanel?.open();
    });
  }

  async function openMention() {
    await withDialogLoading('Mention', async () => {
      MentionDialog ??= (await import('../../lib/MentionDialog.svelte')).default;
      await tick();
      mentionDialog?.open();
    });
  }

  async function openSeatInfo(seat: Seat) {
    await withDialogLoading('Info kursi', async () => {
      SeatInfoDialog ??= (await import('../../lib/SeatInfoDialog.svelte')).default;
      await tick();
      seatInfoDialog?.open(seat);
    });
  }

  async function openSchedule() {
    await withDialogLoading('Jadwal', async () => {
      ScheduleDialog ??= (await import('../../lib/ScheduleDialog.svelte')).default;
      await tick();
      scheduleDialog?.open(text);
    });
  }

  async function openDebate() {
    await withDialogLoading('Debat', async () => {
      DebateDialog ??= (await import('../../lib/DebateDialog.svelte')).default;
      await tick();
      debateDialog?.open();
    });
  }

  async function openABTest() {
    await withDialogLoading('Uji A/B', async () => {
      ABTestDialog ??= (await import('../../lib/ABTestDialog.svelte')).default;
      await tick();
      abDialog?.open();
    });
  }

  async function openShortcutHelp() {
    await withDialogLoading('Bantuan shortcut', async () => {
      ShortcutHelpDialog ??= (await import('../../lib/ShortcutHelpDialog.svelte')).default;
      await tick();
      shortcutHelp?.open();
    });
  }

  async function openRoomDialog() {
    await withDialogLoading('Ruang', async () => {
      RoomDialog ??= (await import('../../lib/RoomDialog.svelte')).default;
      await tick();
      roomDialog?.open();
    });
  }
  let imageInput: HTMLInputElement;
  let fileInput: HTMLInputElement;

  // State scroll.
  let forceScroll = false;
  let scrolledOnce = false;
  let shouldScroll = false;

  const chat = createChat({
    beforeApply: () => {
      shouldScroll = Boolean(messages) && (forceScroll || messages.scrollHeight - messages.scrollTop - messages.clientHeight < 120);
    },
    afterApply: async () => {
      forceScroll = false;
      if (shouldScroll) {
        await tick();
        messages.scrollTo({ top: messages.scrollHeight, behavior: scrolledOnce ? 'smooth' : 'auto' });
        scrolledOnce = true;
      }
    },
  });
  const current = $derived(chat.current);

  // State UI lokal.
  const emojis = ['👍', '❤️', '😂', '😮', '😢', '🙏', '🔥', '👏'];
  let text = $state('');
  let submitting = $state(false);
  let asking = $state(false);
  let voting = $state(false);
  let replyTo = $state<Message>();
  let editingId = $state('');
  let editText = $state('');
  let reactFor = $state('');
  let pendingImage = $state('');
  let uploadingImage = $state(false);
  let pendingFile = $state<Attachment | null>(null);
  let uploadingFile = $state(false);
  let uploadProgress = $state<number | null>(null);
  let searchOpen = $state(false);
  let searchFilters = $state<SearchFilters>(emptySearchFilters());
  let selectedTemplate = $state('');
  let query = $state('');
  let matchIndex = $state(0);
  let pinInput = $state('');
  let pinBusy = $state(false);
  // Nama pengirim terpilih (fitur 21): '' = pemilik.
  let author = $state('');
  // Pesan lokal (hasil /bantu) yang tidak dikirim ke server (fitur 34).
  let localMessages = $state<Array<{ id: string; text: string }>>([]);

  const day = new Intl.DateTimeFormat('id-ID');
  const fullDay = new Intl.DateTimeFormat('id-ID', { day: 'numeric', month: 'long', year: 'numeric' });

  const matches = $derived(
    query.trim()
      ? (current?.messages ?? []).filter(m => m.text.toLowerCase().includes(query.trim().toLowerCase()))
      : []
  );

  // Draft otomatis per ruang.
  const DRAFT_KEY = 'musehood-draft-';
  let lastDraftRoom = '';
  $effect(() => {
    const roomId = current?.activeRoomId ?? '';
    if (roomId && roomId !== lastDraftRoom) {
      lastDraftRoom = roomId;
      try { text = localStorage.getItem(DRAFT_KEY + roomId) ?? ''; } catch { text = ''; }
    }
  });
  $effect(() => {
    const roomId = current?.activeRoomId;
    if (!roomId) return;
    try {
      if (text) localStorage.setItem(DRAFT_KEY + roomId, text);
      else localStorage.removeItem(DRAFT_KEY + roomId);
    } catch { /* abaikan */ }
  });

  // Pengumuman pembaca layar untuk balasan kursi terbaru.
  const liveAnnouncement = $derived.by(() => {
    const msgs = current?.messages ?? [];
    const last = [...msgs].reverse().find((m) => m.authorType === 'seat' && m.status === 'completed' && m.text);
    return last ? `${authorName(last)}: ${last.text.slice(0, 120)}` : '';
  });

  onMount(() => {
    chat.startPolling();
    if ('serviceWorker' in navigator) {
      navigator.serviceWorker.register('/sw.js').catch(() => { /* PWA opsional */ });
    }
    // Prefetch chunk dialog malas saat browser idle agar klik pertama
    // instan, tanpa membebani muatan awal (bukan preload di index.html).
    // Hanya modulnya yang diunduh — komponen tidak di-instantiate, jadi
    // tidak ada fetch/API yang terpicu sebelum dialog benar-benar dibuka.
    const prefetchDialogs = () => {
      const conn = (navigator as { connection?: { saveData?: boolean } }).connection;
      if (conn?.saveData) return;
      void import('../../lib/Settings.svelte').catch(() => {});
      void import('../../lib/MentionDialog.svelte').catch(() => {});
      void import('../../lib/SeatInfoDialog.svelte').catch(() => {});
      void import('../../lib/ScheduleDialog.svelte').catch(() => {});
      void import('../../lib/DebateDialog.svelte').catch(() => {});
      void import('../../lib/ABTestDialog.svelte').catch(() => {});
      void import('../../lib/ShortcutHelpDialog.svelte').catch(() => {});
      void import('../../lib/RoomDialog.svelte').catch(() => {});
    };
    const ric = (window as { requestIdleCallback?: (cb: () => void, opts?: { timeout: number }) => void }).requestIdleCallback;
    if (ric) ric(prefetchDialogs, { timeout: 5000 });
    else setTimeout(prefetchDialogs, 2500);
  });

  function handleShortcut(event: KeyboardEvent) {
    const target = event.target as HTMLElement | null;
    const inInput = Boolean(target && (target.tagName === 'INPUT' || target.tagName === 'TEXTAREA' || target.tagName === 'SELECT' || target.isContentEditable));
    const action = shortcutAction(event, inInput);
    if (!action) return;
    if (action === 'search') {
      event.preventDefault();
      if (!searchOpen) toggleSearch();
      else searchBar?.focus();
    } else if (action === 'send') {
      event.preventDefault();
      composer?.submit();
    } else if (action === 'focusComposer') {
      event.preventDefault();
      composer?.focus();
    } else if (action === 'closeDialog') {
      document.querySelector<HTMLDialogElement>('dialog[open]')?.close();
    } else if (action === 'help') {
      event.preventDefault();
      void openShortcutHelp();
    } else if (action.startsWith('room')) {
      const rooms = (current?.rooms ?? []).filter((room) => !room.archived).sort((a, b) => Number(b.pinned ?? false) - Number(a.pinned ?? false));
      const room = rooms[Number(action.slice(4)) - 1];
      if (room) {
        event.preventDefault();
        switchRoom(room.id);
      }
    }
  }

  async function submitLogin(event: SubmitEvent) {
    event.preventDefault();
    if (pinBusy) return;
    pinBusy = true;
    try {
      await chat.login(pinInput);
      pinInput = '';
    } catch (error) {
      chat.notify(errorText(error), true);
    } finally {
      pinBusy = false;
    }
  }

  function scrollForImage() {
    if (messages.scrollHeight - messages.scrollTop - messages.clientHeight < 450) messages.scrollTo({ top: messages.scrollHeight, behavior: 'smooth' });
  }

  async function send(event: SubmitEvent) {
    event.preventDefault();
    if ((!text.trim() && !pendingImage && !pendingFile) || submitting) return;
    // Perintah /slash (fitur 34): ubah menjadi pesan pemilik biasa.
    let outgoing = text.trim();
    if (!pendingImage && !pendingFile) {
      const parsed = parseSlashCommand(outgoing);
      if (parsed) {
        if (parsed.command === 'bantu') {
          localMessages = [...localMessages, { id: `local-${Date.now()}`, text: SLASH_HELP_TEXT }];
          await composer?.setText('');
          await tick();
          messages?.scrollTo({ top: messages.scrollHeight, behavior: 'smooth' });
          composer?.focus();
          return;
        }
        const integrator = current?.seats.find((s) => s.code === (current.settings.integratorSeat || 'MUSE')) ?? current?.seats[0];
        const coder = current?.seats.find((s) => s.rolePreset === 'coder') ?? integrator;
        const transformed = slashMessageText(parsed, integrator?.name ?? 'Muse', coder?.name ?? integrator?.name ?? 'Muse');
        if (transformed === null) {
          chat.notify(`Perintah /${parsed.command} butuh teks sesudahnya. Ketik /bantu untuk daftar perintah.`, true);
          return;
        }
        outgoing = transformed;
      }
    }
    submitting = true;
    try {
      forceScroll = true;
      await chat.sendMessage({
        text: outgoing,
        ...(replyTo ? { replyTo: replyTo.id } : {}),
        ...(pendingImage ? { imageUrl: pendingImage } : {}),
        ...(pendingFile ? { attachment: pendingFile } : {}),
        ...(selectedTemplate ? { templateId: selectedTemplate } : {}),
        ...(author ? { author } : {}),
      });
      await composer?.setText('');
      replyTo = undefined;
      pendingImage = '';
      pendingFile = null;
    } catch (error) { chat.notify(errorText(error), true); }
    finally { submitting = false; composer?.focus(); }
  }

  async function progress() {
    asking = true;
    try { forceScroll = true; await chat.progress(); }
    catch (error) { chat.notify(errorText(error), true); }
    finally { asking = false; }
  }

  async function startVote() {
    voting = true;
    try { forceScroll = true; await chat.startVote(); }
    catch (error) { chat.notify(errorText(error), true); }
    finally { voting = false; }
  }

  async function switchRoom(id: string) {
    if (!current || id === current.activeRoomId) return;
    try {
      forceScroll = true;
      // Indikator "Memuat ruang…" bila perpindahan >150 ms (jaringan
      // tunnel + render ruang besar bisa terasa membeku tanpa umpan
      // balik); tunggu dua frame agar overlay tidak hilang sebelum
      // ruang baru sempat terlukis.
      await withDialogLoading('ruang', async () => {
        await chat.activateRoom(id);
        await new Promise<void>((resolve) => requestAnimationFrame(() => requestAnimationFrame(() => resolve())));
      });
      replyTo = undefined;
    } catch (error) { chat.notify(errorText(error), true); }
  }

  async function createRoom(name: string, seatCodes: string[]) {
    const trimmed = name.trim();
    if (!trimmed) { chat.notify('Nama ruang wajib diisi.', true); return; }
    try {
      forceScroll = true;
      await withDialogLoading('ruang', async () => {
        await chat.createRoom(trimmed, seatCodes);
        await new Promise<void>((resolve) => requestAnimationFrame(() => requestAnimationFrame(() => resolve())));
      });
      roomDialog?.close();
      chat.notify(`Ruang ${trimmed} dibuat.`);
    } catch (error) { chat.notify(errorText(error), true); }
  }

  function startReply(message: Message) {
    replyTo = message;
    reactFor = '';
    composer?.focus();
  }

  function startEdit(message: Message) {
    editingId = message.id;
    editText = message.text;
    composer?.focus();
  }

  async function saveEdit(event: SubmitEvent) {
    event.preventDefault();
    const target = editingId;
    const value = editText.trim();
    if (!value) { chat.notify('Pesan tidak boleh kosong.', true); return; }
    try {
      await chat.saveEdit(target, value);
      editingId = '';
      editText = '';
    } catch (error) { chat.notify(errorText(error), true); }
  }

  async function removeMessage(message: Message) {
    if (!window.confirm('Hapus pesan ini secara permanen?')) return;
    if (editingId === message.id) { editingId = ''; editText = ''; }
    try { await chat.removeMessage(message); }
    catch (error) { chat.notify(errorText(error), true); }
  }

  async function regenerateMessage(message: Message) {
    try { await chat.regenerateMessage(message); }
    catch (error) { chat.notify(errorText(error), true); }
  }

  async function rerunMessage(message: Message) {
    if (!window.confirm('Jalankan ulang dari pesan ini? Semua pesan sesudahnya di ruang ini akan dihapus dan diskusi dibuat ulang.')) return;
    try { await chat.rerunMessage(message); chat.notify('Diskusi dijalankan ulang dari pesan ini.'); }
    catch (error) { chat.notify(errorText(error), true); }
  }

  async function forkMessage(message: Message) {
    try {
      await withDialogLoading('ruang', async () => {
        await chat.forkMessage(message);
        await new Promise<void>((resolve) => requestAnimationFrame(() => requestAnimationFrame(() => resolve())));
      });
      chat.notify('Ruang fork dibuat dan dibuka.');
    }
    catch (error) { chat.notify(errorText(error), true); }
  }

  async function branchMessage(message: Message) {
    try { await chat.branchMessage(message); chat.notify('Branch baru dibuat dan dibuka.'); }
    catch (error) { chat.notify(errorText(error), true); }
  }

  async function setTags(message: Message, tags: string[]) {
    try { await chat.setMessageTags(message, tags); }
    catch (error) { chat.notify(errorText(error), true); }
  }

  function exportDiscussion(message: Message) {
    window.open(`/api/export?message=${message.id}&format=html`, '_blank');
  }

  async function startDebate(seatA: string, seatB: string, topic: string) {
    await chat.startDebate(seatA, seatB, topic);
    chat.notify('Debat dimulai.');
  }

  async function startABTest(question: string, modelA: string, modelB: string) {
    try {
      forceScroll = true;
      await chat.startABTest(question, modelA, modelB);
      chat.notify('Uji A/B dimulai — dua jawaban sedang dibuat.');
    } catch (error) {
      chat.notify(errorText(error), true);
      throw error;
    }
  }

  async function rateMessage(message: Message, clicked: number) {
    try { await chat.rateMessage(message, nextRating(message.rating ?? 0, clicked)); }
    catch (error) { chat.notify(errorText(error), true); }
  }

  async function pickAB(message: Message) {
    if (!message.abGroup) return;
    try { await chat.pickABWinner(message.abGroup, message.id); }
    catch (error) { chat.notify(errorText(error), true); }
  }

  async function toggleReact(message: Message, emoji: string) {
    reactFor = '';
    try { await chat.toggleReact(message, emoji); }
    catch (error) { chat.notify(errorText(error), true); }
  }

  async function togglePin(message: Message) {
    try { await chat.togglePin(message); }
    catch (error) { chat.notify(errorText(error), true); }
  }

  async function chooseImage() {
    const file = imageInput.files?.[0];
    imageInput.value = '';
    if (!file) return;
    if (file.size > 2 * 1024 * 1024) { chat.notify('Ukuran gambar maksimal 2 MB.', true); return; }
    uploadingImage = true;
    uploadProgress = 0;
    try {
      pendingImage = await chat.uploadImage(file, pct => { uploadProgress = pct; });
      composer?.focus();
    } catch (error) { chat.notify(errorText(error), true); }
    finally { uploadingImage = false; uploadProgress = null; }
  }

  async function chooseFile() {
    const file = fileInput.files?.[0];
    fileInput.value = '';
    if (!file) return;
    if (file.size > 10 * 1024 * 1024) { chat.notify('Ukuran file maksimal 10 MB.', true); return; }
    uploadingFile = true;
    uploadProgress = 0;
    try {
      pendingFile = await chat.uploadFile(file, pct => { uploadProgress = pct; });
      composer?.focus();
    } catch (error) { chat.notify(errorText(error), true); }
    finally { uploadingFile = false; uploadProgress = null; }
  }

  async function toggleSearch() {
    searchOpen = !searchOpen;
    query = '';
    matchIndex = 0;
    chat.clearSearchResults();
    if (searchOpen) {
      await tick();
      searchBar?.focus();
    }
  }

  async function jumpTo(direction: 1 | -1) {
    if (!matches.length) return;
    matchIndex = (matchIndex + direction + matches.length) % matches.length;
    await tick();
    const target = messages.querySelector(`[data-message-id="${matches[matchIndex].id}"]`);
    target?.scrollIntoView({ block: 'center', behavior: 'smooth' });
    target?.classList.add('search-hit');
    setTimeout(() => target?.classList.remove('search-hit'), 1600);
  }

  async function jumpToQuote(id: string) {
    await tick();
    const target = messages.querySelector(`[data-message-id="${id}"]`);
    if (!target) { chat.notify('Pesan yang dibalas sudah dihapus.', true); return; }
    target.scrollIntoView({ block: 'center', behavior: 'smooth' });
    target.classList.add('search-hit');
    setTimeout(() => target.classList.remove('search-hit'), 1600);
  }

  async function jumpToResult(result: SearchResult) {
    try {
      if (current && result.roomId !== current.activeRoomId) {
        await chat.activateRoom(result.roomId);
      }
      await jumpToQuote(result.id);
    } catch (error) { chat.notify(errorText(error), true); }
  }

  function authorName(message: Message): string {
    if (message.authorType === 'owner') return message.author || current?.owner.name || '';
    return current?.seats.find(s => s.code === message.seat)?.name ?? message.seat ?? '';
  }
</script>

<svelte:head><title>{current ? `${current.group.name} — Musehood` : 'Musehood — Grup multi-AI'}</title></svelte:head>
<svelte:window onkeydown={handleShortcut} />

{#if chat.pinRequired}
  <main class="login-screen">
    <form class="login-card" onsubmit={submitLogin}>
      <p class="eyebrow">MUSEHOOD</p>
      <h1>Masukkan PIN</h1>
      <p>Grup ini dilindungi PIN aplikasi. Sesi masuk berlaku 7 hari.</p>
      <label>PIN<input bind:value={pinInput} type="password" autocomplete="current-password" required maxlength="64" placeholder="PIN"></label>
      <button class="primary-button" type="submit" disabled={pinBusy}>Masuk</button>
    </form>
  </main>
{:else}
<main class="app">
  <header class="group-header">
    <img id="groupAvatar" class="group-avatar" src={current?.group.avatar ?? '/avatars/group.svg'} alt={current ? `Foto grup ${current.group.name}` : 'Foto grup'} width="42" height="42">
    <div class="group-heading">
      <h1 id="groupName" title={current?.group.name ?? 'Musehood'}>{current?.group.name ?? 'Musehood'}</h1>
      <p id="groupSubtitle">{current ? `Pemilik ${current.owner.name} - Admin ${current.seats[0].name} - Hadir ${current.presence.present}/${current.presence.total}` : 'Menghubungkan grup…'}</p>
    </div>
    <div class="header-actions">
      <button id="searchToggle" class="icon-button" type="button" aria-label="Cari pesan" title="Cari pesan" onclick={toggleSearch}>
        <svg viewBox="0 0 24 24" aria-hidden="true"><circle cx="10.5" cy="10.5" r="6.5"/><path d="m15.5 15.5 5 5"/></svg>
      </button>
      <button id="darkToggle" class="icon-button" type="button" aria-label="Mode gelap" title="Mode gelap/terang" onclick={() => chat.toggleTheme().catch(e => chat.notify(errorText(e), true))}>
        <svg viewBox="0 0 24 24" aria-hidden="true"><path d="M20 13.5A8.5 8.5 0 0 1 10.5 4 8.5 8.5 0 1 0 20 13.5Z"/></svg>
      </button>
      <button id="openSettings" class="icon-button" type="button" aria-label="Pengaturan grup" title="Pengaturan grup" aria-busy={loadingDialog === 'Pengaturan'} onclick={() => void openSettings()}>
        <svg viewBox="0 0 24 24" aria-hidden="true"><path d="m9.7 3-.6 2.2-2 .9L5 5.5 2.8 9.3l1.6 1.6v2.2l-1.6 1.6L5 18.5l2.1-.6 2 .9.6 2.2h4.6l.6-2.2 2-.9 2.1.6 2.2-3.8-1.6-1.6v-2.2l1.6-1.6L19 5.5l-2.1.6-2-.9L14.3 3Z"/><circle cx="12" cy="12" r="3.2"/></svg>
      </button>
    </div>
  </header>
  {#if current && current.rooms.length > 0}
    <RoomBar rooms={current.rooms} activeRoomId={current.activeRoomId} onSwitch={switchRoom} onAdd={() => void openRoomDialog()} onSummarize={() => chat.summarizeRoom().catch((e) => chat.notify(errorText(e), true))} onTogglePin={(room) => chat.toggleRoomPin(room).catch((e) => chat.notify(errorText(e), true))} />
    {@const workRoom = current.rooms.find((room) => room.id === current.activeRoomId)}
    {#if workRoom}
      <RoomWorkPanel room={workRoom}
        onAddTodo={(text) => chat.addRoomTodo(workRoom.id, text).catch((e) => chat.notify(errorText(e), true))}
        onTodoStatus={(todoId, status) => chat.setRoomTodoStatus(workRoom.id, todoId, status).catch((e) => chat.notify(errorText(e), true))}
        onDeleteTodo={(todoId) => chat.deleteRoomTodo(workRoom.id, todoId).catch((e) => chat.notify(errorText(e), true))}
        onCheckpoint={(label) => chat.addRoomCheckpoint(workRoom.id, label).catch((e) => chat.notify(errorText(e), true))}
        onRewind={(checkpointId) => chat.rewindRoom(workRoom.id, checkpointId).catch((e) => chat.notify(errorText(e), true))}
        onModes={(patch) => chat.setRoomModes(workRoom.id, patch).catch((e) => chat.notify(errorText(e), true))} />
    {/if}
  {/if}
  {#if searchOpen}
    <SearchBar bind:this={searchBar} bind:query bind:filters={searchFilters} seats={current?.seats ?? []} rooms={current?.rooms ?? []} matchCount={matches.length} {matchIndex} serverResults={chat.serverResults}
      onInput={() => chat.scheduleSearch(query.trim(), searchFilters)} onPrev={() => jumpTo(-1)} onNext={() => jumpTo(1)} onClose={toggleSearch} onJumpResult={jumpToResult} />
  {/if}
  <div class="visually-hidden" aria-live="polite">{liveAnnouncement}</div>
  {#if !chat.online}
    <div class="offline-banner" role="status">Offline — menampilkan data terakhir. Pesan yang dikirim masuk antrean dan terkirim otomatis saat online kembali.</div>
  {/if}
  <SeatStrip seats={chat.roomSeats} onMention={(seat) => composer?.insertMention(seat.name)} />
  {#if current && !chat.connectionReady}
    <button id="connectionNotice" class="connection-notice" type="button" onclick={() => void openSettings()}><span class="notice-dot"></span><span>Hubungkan gateway melalui Pengaturan untuk mengaktifkan enam kursi AI.</span><span aria-hidden="true">›</span></button>
  {/if}
  {#if chat.pinnedMessages.length}
    <PinnedBar pinned={chat.pinnedMessages} ownerName={current?.owner.name} seats={current?.seats ?? []} onJump={jumpToQuote} />
  {/if}
  {#if current}
    {@const activeRoom = current.rooms.find((room) => room.id === current.activeRoomId)}
    {#if activeRoom?.branches && activeRoom.branches.length > 1}
      <nav class="room-bar branch-bar" aria-label="Branch percakapan">
        {#each activeRoom.branches as branch, index (branch.id)}
          <button class="room-chip" class:active={(activeRoom.activeBranch ?? 'main') === branch.id} type="button" title={`Branch ${index + 1} (${branch.messageCount} pesan)`} onclick={() => chat.switchBranch(activeRoom.id, branch.id).catch((e) => chat.notify(errorText(e), true))}>{branch.id === 'main' ? '⎇ Utama' : `⎇ Branch ${index + 1}`}</button>
        {/each}
      </nav>
    {/if}
  {/if}
  <section bind:this={messages} id="messages" class="messages" aria-label="Percakapan grup" aria-live="polite" aria-relevant="additions text">
    {#if !current}
      <div class="loading-state">Memuat percakapan…</div>
    {:else if !current.messages.length}
      <div class="empty-state">
        <img src={current.group.avatar} alt="Ikon grup"><p class="eyebrow">SELAMAT DATANG DI GRUP</p>
        <h2>Satu ruang.<br>Tujuh sudut pandang.</h2>
        <p>{current.owner.name} bersama tujuh kursi AI. Bagikan ide, selesaikan tugas, dan temukan jawaban dari beragam peran.</p>
        <div class="suggestions">
          <button type="button" onclick={() => composer?.setText('Tugas: ')}>Mulai sebuah tugas</button>
          <button type="button" onclick={() => composer?.setText('Gambar: ')}>Buat sebuah gambar</button>
          <button type="button" onclick={() => composer?.setText(`@${current?.seats[0].name ?? 'Muse'} `)}>Sapa {current.seats[0].name}</button>
        </div>
        <p class="empty-note">Muse terhubung melalui antrean pekerja lokal.</p>
      </div>
    {:else}
      {#snippet bubble(message: Message)}
        {@const own = message.authorType === 'owner'}
        {@const seat = current.seats.find(s => s.code === message.seat)}
        {@const authorIdentity = own ? current.owner : seat}
        {@const quoted = message.replyTo ? current.messages.find(m => m.id === message.replyTo) : undefined}
        <MessageBubble
          {message} {seat} seats={chat.roomSeats} {own}
          name={own ? (message.author || authorIdentity?.name || '') : (authorIdentity?.name ?? message.seat ?? '')}
          avatar={authorIdentity?.avatar ?? '/avatars/group.svg'}
          {quoted}
          quotedAuthorName={quoted ? authorName(quoted) : ''}
          editing={editingId === message.id}
          bind:editText
          reactOpen={reactFor === message.id}
          {emojis}
          {query}
          onReply={() => startReply(message)}
          onToggleReactPicker={() => reactFor = reactFor === message.id ? '' : message.id}
          onReact={(emoji) => toggleReact(message, emoji)}
          onPin={() => togglePin(message)}
          onEditStart={() => startEdit(message)}
          onEditSave={saveEdit}
          onEditCancel={() => { editingId = ''; editText = ''; }}
          onDelete={() => removeMessage(message)}
          onJumpQuote={jumpToQuote}
          onImageLoad={scrollForImage}
          onMentionClick={(mentioned) => void openSeatInfo(mentioned)}
          onRegenerate={() => regenerateMessage(message)}
          onRerun={() => rerunMessage(message)}
          onFork={() => forkMessage(message)}
          onBranch={() => branchMessage(message)}
          onTags={(tags) => setTags(message, tags)}
          onExport={() => exportDiscussion(message)}
          onRate={(clicked) => rateMessage(message, clicked)}
          onPickAB={() => pickAB(message)}
        />
      {/snippet}
      {#each current.messages as message, index (message.id)}
        {@const date = new Date(message.createdAt)}
        {@const prev = current.messages[index - 1]}
        {@const next = current.messages[index + 1]}
        {#if index === 0 || day.format(date) !== day.format(new Date(current.messages[index - 1].createdAt))}
          <div class="date-divider"><span>{day.format(date) === day.format(new Date()) ? 'Hari ini' : fullDay.format(date)}</span></div>
        {/if}
        {#if message.abLabel === 'B' && prev?.abGroup && prev.abGroup === message.abGroup}
          <!-- sudah dirender berdampingan dengan model A -->
        {:else if message.abLabel === 'A' && next?.abGroup === message.abGroup && next.abLabel === 'B'}
          <div class="ab-pair">{@render bubble(message)}{@render bubble(next)}</div>
        {:else}
          {@render bubble(message)}
        {/if}
      {/each}
      {#each localMessages as local (local.id)}
        <article class="message local-message" data-message-id={local.id}>
          <img class="message-avatar" src={current.group.avatar} alt="Musehood">
          <div class="bubble">
            <div class="message-meta"><span class="author-name">Musehood</span><span class="author-role">Bantuan lokal</span></div>
            <div class="message-text local-text">{local.text}</div>
          </div>
        </article>
      {/each}
    {/if}
  </section>
  {#if current?.pendingApprovals?.length}
    <ApprovalBanner approvals={current.pendingApprovals} onDecide={(id, approved) => chat.decideApproval(id, approved).catch(e => chat.notify(errorText(e), true))} />
  {/if}
  {#if current?.pendingQuestions?.length}
    <QuestionBanner questions={current.pendingQuestions} onAnswer={(id, answer) => chat.answerQuestion(id, answer).catch(e => chat.notify(errorText(e), true))} />
  {/if}
  <Composer bind:this={composer} bind:text {submitting} {asking} {voting} {uploadingImage} {uploadingFile} {uploadProgress}
    {replyTo} replyAuthorName={replyTo ? authorName(replyTo) : ''} {pendingImage} {pendingFile}
    templates={current?.templates ?? []} bind:selectedTemplate
    participants={current?.rooms.find((room) => room.id === current.activeRoomId)?.participants ?? []}
    ownerName={current?.owner.name ?? ''} bind:author
    onSend={send} onProgress={progress} onVote={startVote}
    onOpenMention={() => { if (current) void openMention(); }}
    onImagePrompt={() => composer?.setText(/^Gambar\s*:/i.test(text) ? text : `Gambar: ${text}`)}
    onUploadImage={() => imageInput.click()} onUploadFile={() => fileInput.click()}
    onCancelReply={() => replyTo = undefined} onCancelImage={() => pendingImage = ''} onCancelFile={() => pendingFile = null}
    onSchedule={() => void openSchedule()}
    onDebate={() => void openDebate()}
    onABTest={() => void openABTest()} />
</main>
{/if}

{#if loadingDialog}
  <div class="dialog-loading" role="status" aria-live="polite">
    <span class="dialog-loading-card">
      <span class="spinner" aria-hidden="true"></span>
      <span>Memuat {loadingDialog}…</span>
    </span>
  </div>
{/if}

{#if Settings}
  <Settings bind:this={settingsPanel} current={chat.current} refresh={chat.refresh} mutate={chat.mutate} notify={chat.notify} notice={chat.notice} />
{/if}
{#if MentionDialog}
  <MentionDialog bind:this={mentionDialog} seats={chat.roomSeats} notice={chat.notice} onPick={(seat) => composer?.insertMention(seat.name)} />
{/if}
{#if SeatInfoDialog}
  <SeatInfoDialog bind:this={seatInfoDialog} onMention={(seat) => composer?.insertMention(seat.name)} />
{/if}
{#if ScheduleDialog}
  <ScheduleDialog bind:this={scheduleDialog} listScheduled={chat.listScheduled} createScheduled={chat.createScheduled} updateScheduled={chat.updateScheduled} deleteScheduled={chat.deleteScheduled} notify={chat.notify} roomId={current?.activeRoomId ?? ''} />
{/if}
{#if RoomDialog}
  <RoomDialog bind:this={roomDialog} seats={current?.seats ?? []} notice={chat.notice} onCreate={createRoom} />
{/if}
{#if DebateDialog}
  <DebateDialog bind:this={debateDialog} seats={current?.seats ?? []} notice={chat.notice} onStart={startDebate} />
{/if}
{#if ABTestDialog}
  <ABTestDialog bind:this={abDialog} current={chat.current} notice={chat.notice} onStart={startABTest} />
{/if}
{#if ShortcutHelpDialog}
  <ShortcutHelpDialog bind:this={shortcutHelp} />
{/if}
<Toast notice={chat.notice} target={null} />
<input id="chatImageInput" type="file" accept="image/png,image/jpeg,image/gif,image/webp" hidden bind:this={imageInput} onchange={chooseImage}>
<input id="chatFileInput" type="file" hidden bind:this={fileInput} onchange={chooseFile}>
