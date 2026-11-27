<script lang="ts">
  import { onMount, tick } from 'svelte';
  import Settings from '../../lib/Settings.svelte';
  import Toast from '../../lib/Toast.svelte';
  import RoomBar from '../../lib/RoomBar.svelte';
  import SearchBar from '../../lib/SearchBar.svelte';
  import SeatStrip from '../../lib/SeatStrip.svelte';
  import PinnedBar from '../../lib/PinnedBar.svelte';
  import MessageBubble from '../../lib/MessageBubble.svelte';
  import Composer from '../../lib/Composer.svelte';
  import MentionDialog from '../../lib/MentionDialog.svelte';
  import SeatInfoDialog from '../../lib/SeatInfoDialog.svelte';
  import ScheduleDialog from '../../lib/ScheduleDialog.svelte';
  import DebateDialog from '../../lib/DebateDialog.svelte';
  import ShortcutHelpDialog from '../../lib/ShortcutHelpDialog.svelte';
  import { shortcutAction } from '../../lib/shortcuts';
  import ApprovalBanner from '../../lib/ApprovalBanner.svelte';
  import RoomDialog from '../../lib/RoomDialog.svelte';
  import { createChat } from '../../lib/chat.svelte';
  import { errorText } from '../../lib/api';
  import { emptySearchFilters, type Attachment, type Message, type SearchFilters, type SearchResult } from '../../lib/types';

  // Referensi DOM.
  let messages: HTMLElement;
  let settingsPanel = $state<ReturnType<typeof Settings>>();
  let mentionDialog = $state<ReturnType<typeof MentionDialog>>();
  let seatInfoDialog = $state<ReturnType<typeof SeatInfoDialog>>();
  let scheduleDialog = $state<ReturnType<typeof ScheduleDialog>>();
  let debateDialog = $state<ReturnType<typeof DebateDialog>>();
  let shortcutHelp = $state<ReturnType<typeof ShortcutHelpDialog>>();
  let roomDialog = $state<ReturnType<typeof RoomDialog>>();
  let composer = $state<ReturnType<typeof Composer>>();
  let searchBar = $state<ReturnType<typeof SearchBar>>();
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
      shortcutHelp?.open();
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
    submitting = true;
    try {
      forceScroll = true;
      await chat.sendMessage({
        text: text.trim(),
        ...(replyTo ? { replyTo: replyTo.id } : {}),
        ...(pendingImage ? { imageUrl: pendingImage } : {}),
        ...(pendingFile ? { attachment: pendingFile } : {}),
        ...(selectedTemplate ? { templateId: selectedTemplate } : {}),
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
      await chat.activateRoom(id);
      replyTo = undefined;
    } catch (error) { chat.notify(errorText(error), true); }
  }

  async function createRoom(name: string, seatCodes: string[]) {
    const trimmed = name.trim();
    if (!trimmed) { chat.notify('Nama ruang wajib diisi.', true); return; }
    try {
      forceScroll = true;
      await chat.createRoom(trimmed, seatCodes);
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
    try { await chat.forkMessage(message); chat.notify('Ruang fork dibuat dan dibuka.'); }
    catch (error) { chat.notify(errorText(error), true); }
  }

  function exportDiscussion(message: Message) {
    window.open(`/api/export?message=${message.id}&format=html`, '_blank');
  }

  async function startDebate(seatA: string, seatB: string, topic: string) {
    await chat.startDebate(seatA, seatB, topic);
    chat.notify('Debat dimulai.');
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
    return message.authorType === 'owner' ? current?.owner.name ?? '' : current?.seats.find(s => s.code === message.seat)?.name ?? message.seat ?? '';
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
      <button id="darkToggle" class="icon-button" type="button" aria-label="Mode gelap" title="Mode gelap/terang" onclick={() => current && chat.mutate('/api/settings', { settings: { darkMode: !current.settings.darkMode } }).catch(e => chat.notify(errorText(e), true))}>
        <svg viewBox="0 0 24 24" aria-hidden="true"><path d="M20 13.5A8.5 8.5 0 0 1 10.5 4 8.5 8.5 0 1 0 20 13.5Z"/></svg>
      </button>
      <button id="openSettings" class="icon-button" type="button" aria-label="Pengaturan grup" title="Pengaturan grup" onclick={() => settingsPanel?.open()}>
        <svg viewBox="0 0 24 24" aria-hidden="true"><path d="m9.7 3-.6 2.2-2 .9L5 5.5 2.8 9.3l1.6 1.6v2.2l-1.6 1.6L5 18.5l2.1-.6 2 .9.6 2.2h4.6l.6-2.2 2-.9 2.1.6 2.2-3.8-1.6-1.6v-2.2l1.6-1.6L19 5.5l-2.1.6-2-.9L14.3 3Z"/><circle cx="12" cy="12" r="3.2"/></svg>
      </button>
    </div>
  </header>
  {#if current && current.rooms.length > 0}
    <RoomBar rooms={current.rooms} activeRoomId={current.activeRoomId} onSwitch={switchRoom} onAdd={() => roomDialog?.open()} onSummarize={() => chat.summarizeRoom().catch((e) => chat.notify(errorText(e), true))} onTogglePin={(room) => chat.mutate(`/api/rooms/${room.id}`, { pinned: !room.pinned }).catch((e) => chat.notify(errorText(e), true))} />
  {/if}
  {#if searchOpen}
    <SearchBar bind:this={searchBar} bind:query bind:filters={searchFilters} seats={current?.seats ?? []} rooms={current?.rooms ?? []} matchCount={matches.length} {matchIndex} serverResults={chat.serverResults}
      onInput={() => chat.scheduleSearch(query.trim(), searchFilters)} onPrev={() => jumpTo(-1)} onNext={() => jumpTo(1)} onClose={toggleSearch} onJumpResult={jumpToResult} />
  {/if}
  <div class="visually-hidden" aria-live="polite">{liveAnnouncement}</div>
  <SeatStrip seats={chat.roomSeats} onMention={(seat) => composer?.insertMention(seat.name)} />
  {#if current && !chat.connectionReady}
    <button id="connectionNotice" class="connection-notice" type="button" onclick={() => settingsPanel?.open()}><span class="notice-dot"></span><span>Hubungkan gateway melalui Pengaturan untuk mengaktifkan enam kursi AI.</span><span aria-hidden="true">›</span></button>
  {/if}
  {#if chat.pinnedMessages.length}
    <PinnedBar pinned={chat.pinnedMessages} ownerName={current?.owner.name} seats={current?.seats ?? []} onJump={jumpToQuote} />
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
      {#each current.messages as message, index (message.id)}
        {@const date = new Date(message.createdAt)}
        {@const own = message.authorType === 'owner'}
        {@const seat = current.seats.find(s => s.code === message.seat)}
        {@const author = own ? current.owner : seat}
        {@const quoted = message.replyTo ? current.messages.find(m => m.id === message.replyTo) : undefined}
        {#if index === 0 || day.format(date) !== day.format(new Date(current.messages[index - 1].createdAt))}
          <div class="date-divider"><span>{day.format(date) === day.format(new Date()) ? 'Hari ini' : fullDay.format(date)}</span></div>
        {/if}
        <MessageBubble
          {message} {seat} seats={chat.roomSeats} {own}
          name={author?.name ?? message.seat ?? ''}
          avatar={author?.avatar ?? '/avatars/group.svg'}
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
          onMentionClick={(mentioned) => seatInfoDialog?.open(mentioned)}
          onRegenerate={() => regenerateMessage(message)}
          onRerun={() => rerunMessage(message)}
          onFork={() => forkMessage(message)}
          onExport={() => exportDiscussion(message)}
        />
      {/each}
    {/if}
  </section>
  {#if current?.pendingApprovals?.length}
    <ApprovalBanner approvals={current.pendingApprovals} onDecide={(id, approved) => chat.decideApproval(id, approved).catch(e => chat.notify(errorText(e), true))} />
  {/if}
  <Composer bind:this={composer} bind:text {submitting} {asking} {voting} {uploadingImage} {uploadingFile} {uploadProgress}
    {replyTo} replyAuthorName={replyTo ? authorName(replyTo) : ''} {pendingImage} {pendingFile}
    templates={current?.templates ?? []} bind:selectedTemplate
    onSend={send} onProgress={progress} onVote={startVote}
    onOpenMention={() => { if (current) mentionDialog?.open(); }}
    onImagePrompt={() => composer?.setText(/^Gambar\s*:/i.test(text) ? text : `Gambar: ${text}`)}
    onUploadImage={() => imageInput.click()} onUploadFile={() => fileInput.click()}
    onCancelReply={() => replyTo = undefined} onCancelImage={() => pendingImage = ''} onCancelFile={() => pendingFile = null}
    onSchedule={() => scheduleDialog?.open(text)}
    onDebate={() => debateDialog?.open()} />
</main>
{/if}

<Settings bind:this={settingsPanel} current={chat.current} refresh={chat.refresh} mutate={chat.mutate} notify={chat.notify} notice={chat.notice} />
<MentionDialog bind:this={mentionDialog} seats={chat.roomSeats} notice={chat.notice} onPick={(seat) => composer?.insertMention(seat.name)} />
<SeatInfoDialog bind:this={seatInfoDialog} onMention={(seat) => composer?.insertMention(seat.name)} />
<ScheduleDialog bind:this={scheduleDialog} listScheduled={chat.listScheduled} createScheduled={chat.createScheduled} updateScheduled={chat.updateScheduled} deleteScheduled={chat.deleteScheduled} notify={chat.notify} roomId={current?.activeRoomId ?? ''} />
<RoomDialog bind:this={roomDialog} seats={current?.seats ?? []} notice={chat.notice} onCreate={createRoom} />
<DebateDialog bind:this={debateDialog} seats={current?.seats ?? []} notice={chat.notice} onStart={startDebate} />
<ShortcutHelpDialog bind:this={shortcutHelp} />
<Toast notice={chat.notice} target={null} />
<input id="chatImageInput" type="file" accept="image/png,image/jpeg,image/gif,image/webp" hidden bind:this={imageInput} onchange={chooseImage}>
<input id="chatFileInput" type="file" hidden bind:this={fileInput} onchange={chooseFile}>
