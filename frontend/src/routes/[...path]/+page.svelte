<script lang="ts">
  import { onMount, tick } from 'svelte';
  import Settings from '../../lib/Settings.svelte';
  import { api, dismissBackdrop, errorText, type Seat, type State, type Message, type Attachment } from '../../lib/api';

  let current = $state<State>();
  let text = $state('');
  let submitting = $state(false);
  let asking = $state(false);
  let input: HTMLTextAreaElement;
  let composer: HTMLFormElement;
  let messages: HTMLElement;
  let mention: HTMLDialogElement;
  let settingsPanel: ReturnType<typeof Settings>;
  let mainToast: HTMLDivElement;
  let dialogToast: HTMLDivElement;
  let notice = $state<{ text: string; error: boolean; dialog: boolean }>();
  let timer: ReturnType<typeof setTimeout>;
  let requested = 0;
  let applied = 0;
  let messageKey = '';
  let forceScroll = false;
  const clock = new Intl.DateTimeFormat('id-ID', { hour: '2-digit', minute: '2-digit' });
  const day = new Intl.DateTimeFormat('id-ID');
  const fullDay = new Intl.DateTimeFormat('id-ID', { day: 'numeric', month: 'long', year: 'numeric' });

  // Fitur chat tambahan.
  const emojis = ['👍', '❤️', '😂', '😮', '😢', '🔥'];
  let replyTo = $state<Message>();
  let editingId = $state('');
  let editText = $state('');
  let reactFor = $state('');
  let imageInput: HTMLInputElement;
  let pendingImage = $state('');
  let uploadingImage = $state(false);
  let fileInput: HTMLInputElement;
  let pendingFile = $state<Attachment | null>(null);
  let uploadingFile = $state(false);
  let searchOpen = $state(false);
  let query = $state('');
  let matchIndex = $state(0);
  let searchInput = $state<HTMLInputElement>();

  function notify(text: string, error = false) {
    const open = document.querySelector('dialog[open]');
    if (open) open.append(dialogToast);
    else if (dialogToast.parentElement !== mainToast.parentElement) mainToast.after(dialogToast);
    notice = { text, error, dialog: Boolean(open) };
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
    const shouldScroll = messages && (forceScroll || messages.scrollHeight - messages.scrollTop - messages.clientHeight < 120);
    messageKey = nextKey;
    current = result;
    document.documentElement.classList.toggle('dark', result.settings.darkMode);
    if (changed) {
      forceScroll = false;
      if (shouldScroll) {
        await tick();
        messages.scrollTop = messages.scrollHeight;
      }
    }
  }

  async function mutate(route: string, body: object) {
    await api(route, body);
    await refresh();
  }

  onMount(() => {
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
  });

  function resizeInput() {
    input.style.height = 'auto';
    input.style.height = `${Math.min(input.scrollHeight, 130)}px`;
  }

  async function put(value: string) {
    text = value;
    await tick();
    resizeInput();
    input.focus();
  }

  async function insertMention(seat: Seat) {
    const { selectionStart, selectionEnd, value } = input;
    const prefix = selectionStart > 0 && !/\s$/.test(value.slice(0, selectionStart)) ? ' ' : '';
    const insertion = `${prefix}@${seat.name} `;
    await put(value.slice(0, selectionStart) + insertion + value.slice(selectionEnd));
    input.setSelectionRange(selectionStart + insertion.length, selectionStart + insertion.length);
  }

  function handleEnter(event: KeyboardEvent) {
    if (event.key === 'Enter' && !event.shiftKey && !event.isComposing) {
      event.preventDefault();
      composer.requestSubmit();
    }
  }

  function humanSize(size: number): string {
    if (size < 1024) return `${size} B`;
    if (size < 1024 * 1024) return `${(size / 1024).toFixed(1)} KB`;
    return `${(size / (1024 * 1024)).toFixed(1)} MB`;
  }

  function isImageAttachment(att: Attachment): boolean {
    return att.mime.startsWith('image/');
  }

  function attachmentLabel(message: Message): string {
    if (message.text) return message.text;
    if (message.imageUrl) return '[gambar]';
    if (message.attachment) return `[lampiran: ${message.attachment.name}]`;
    return '';
  }

  async function send(event: SubmitEvent) {
    event.preventDefault();
    if ((!text.trim() && !pendingImage && !pendingFile) || submitting) return;
    submitting = true;
    try {
      forceScroll = true;
      await api('/api/message', { text: text.trim(), ...(replyTo ? { replyTo: replyTo.id } : {}), ...(pendingImage ? { imageUrl: pendingImage } : {}), ...(pendingFile ? { attachment: pendingFile } : {}) });
      await put('');
      replyTo = undefined;
      pendingImage = '';
      pendingFile = null;
      await refresh();
    } catch (error) { notify(errorText(error), true); }
    finally { submitting = false; input.focus(); }
  }

  async function progress() {
    asking = true;
    try { forceScroll = true; await mutate('/api/progress', {}); }
    catch (error) { notify(errorText(error), true); }
    finally { asking = false; }
  }

  function scrollForImage() {
    if (messages.scrollHeight - messages.scrollTop - messages.clientHeight < 450) messages.scrollTop = messages.scrollHeight;
  }

  function startReply(message: Message) {
    replyTo = message;
    reactFor = '';
    input.focus();
  }

  function startEdit(message: Message) {
    editingId = message.id;
    editText = message.text;
    input.focus();
  }

  async function saveEdit(event: SubmitEvent) {
    event.preventDefault();
    const target = editingId;
    const value = editText.trim();
    if (!value) { notify('Pesan tidak boleh kosong.', true); return; }
    try {
      await mutate(`/api/message/${target}/edit`, { text: value });
      editingId = '';
      editText = '';
    } catch (error) { notify(errorText(error), true); }
  }

  async function removeMessage(message: Message) {
    if (!window.confirm('Hapus pesan ini secara permanen?')) return;
    if (editingId === message.id) { editingId = ''; editText = ''; }
    try { await mutate(`/api/message/${message.id}/delete`, {}); }
    catch (error) { notify(errorText(error), true); }
  }

  async function toggleReact(message: Message, emoji: string) {
    reactFor = '';
    try { await mutate(`/api/message/${message.id}/react`, { emoji }); }
    catch (error) { notify(errorText(error), true); }
  }

  async function chooseImage() {
    const file = imageInput.files?.[0];
    imageInput.value = '';
    if (!file) return;
    if (file.size > 2 * 1024 * 1024) { notify('Ukuran gambar maksimal 2 MB.', true); return; }
    uploadingImage = true;
    try {
      const body = new FormData();
      body.set('image', file);
      const out = await api<{ imageUrl: string }>('/api/upload-image', body);
      pendingImage = out.imageUrl;
      input.focus();
    } catch (error) { notify(errorText(error), true); }
    finally { uploadingImage = false; }
  }

  async function chooseFile() {
    const file = fileInput.files?.[0];
    fileInput.value = '';
    if (!file) return;
    if (file.size > 10 * 1024 * 1024) { notify('Ukuran file maksimal 10 MB.', true); return; }
    uploadingFile = true;
    try {
      const body = new FormData();
      body.set('file', file);
      const out = await api<Attachment>('/api/upload-file', body);
      pendingFile = out;
      input.focus();
    } catch (error) { notify(errorText(error), true); }
    finally { uploadingFile = false; }
  }

  const matches = $derived(
    query.trim()
      ? (current?.messages ?? []).filter(m => m.text.toLowerCase().includes(query.trim().toLowerCase()))
      : []
  );

  const connectionReady = $derived(Boolean(current) && Boolean(current!.settings.baseUrl) && (current!.settings.gatewayProtocol === 'ollama' || current!.settings.hasApiKey));

  async function toggleSearch() {
    searchOpen = !searchOpen;
    query = '';
    matchIndex = 0;
    if (searchOpen) {
      await tick();
      searchInput?.focus();
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
    if (!target) { notify('Pesan yang dibalas sudah dihapus.', true); return; }
    target.scrollIntoView({ block: 'center', behavior: 'smooth' });
    target.classList.add('search-hit');
    setTimeout(() => target?.classList.remove('search-hit'), 1600);
  }

  function highlight(message: Message) {
    const q = query.trim().toLowerCase();
    if (!q || !message.text.toLowerCase().includes(q)) return [message.text];
    const parts: Array<string | { hit: string }> = [];
    let rest = message.text;
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
</script>

<svelte:head><title>{current ? `${current.group.name} — Musehood` : 'Musehood — Grup multi-AI'}</title></svelte:head>

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
      <button id="darkToggle" class="icon-button" type="button" aria-label="Mode gelap" title="Mode gelap/terang" onclick={() => current && mutate('/api/settings', { settings: { darkMode: !current.settings.darkMode } }).catch(e => notify(errorText(e), true))}>
        <svg viewBox="0 0 24 24" aria-hidden="true"><path d="M20 13.5A8.5 8.5 0 0 1 10.5 4 8.5 8.5 0 1 0 20 13.5Z"/></svg>
      </button>
      <button id="openSettings" class="icon-button" type="button" aria-label="Pengaturan grup" title="Pengaturan grup" onclick={() => settingsPanel.open()}>
        <svg viewBox="0 0 24 24" aria-hidden="true"><path d="m9.7 3-.6 2.2-2 .9L5 5.5 2.8 9.3l1.6 1.6v2.2l-1.6 1.6L5 18.5l2.1-.6 2 .9.6 2.2h4.6l.6-2.2 2-.9 2.1.6 2.2-3.8-1.6-1.6v-2.2l1.6-1.6L19 5.5l-2.1.6-2-.9L14.3 3Z"/><circle cx="12" cy="12" r="3.2"/></svg>
      </button>
    </div>
  </header>
  {#if searchOpen}
    <div class="search-bar" role="search">
      <input bind:this={searchInput} bind:value={query} type="search" placeholder="Cari dalam percakapan…" aria-label="Cari pesan">
      <span class="search-count" aria-live="polite">{query.trim() ? `${matches.length ? matchIndex + 1 : 0}/${matches.length}` : ''}</span>
      <button class="icon-button" type="button" aria-label="Hasil sebelumnya" title="Sebelumnya" disabled={!matches.length} onclick={() => jumpTo(-1)}>↑</button>
      <button class="icon-button" type="button" aria-label="Hasil berikutnya" title="Berikutnya" disabled={!matches.length} onclick={() => jumpTo(1)}>↓</button>
      <button class="icon-button" type="button" aria-label="Tutup pencarian" title="Tutup" onclick={toggleSearch}>✕</button>
    </div>
  {/if}
  <nav id="seatStrip" class="seat-strip" aria-label="Tujuh kursi AI">
    {#each current?.seats ?? [] as seat (seat.code)}
      <button class="seat-card" type="button" title={`@${seat.name} · ${seat.code} · ${seat.role}`} aria-label={`Sebut ${seat.name}, ${seat.role}`} onclick={() => insertMention(seat)}>
        <span class="seat-avatar-wrap"><img src={seat.avatar} alt={seat.name} width="40" height="40"><span class="presence-dot" class:offline={!seat.present} title={seat.present ? 'Hadir' : 'Tidak hadir'}></span></span>
        <span class="seat-name">{seat.name}</span><span class="seat-role">{seat.role}</span>
      </button>
    {/each}
  </nav>
  {#if current && !connectionReady}
    <button id="connectionNotice" class="connection-notice" type="button" onclick={() => settingsPanel.open()}><span class="notice-dot"></span><span>Hubungkan gateway melalui Pengaturan untuk mengaktifkan enam kursi AI.</span><span aria-hidden="true">›</span></button>
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
          <button type="button" onclick={() => put('Tugas: ')}>Mulai sebuah tugas</button>
          <button type="button" onclick={() => put('Gambar: ')}>Buat sebuah gambar</button>
          <button type="button" onclick={() => put(`@${current?.seats[0].name ?? 'Muse'} `)}>Sapa {current.seats[0].name}</button>
        </div>
        <p class="empty-note">Muse terhubung melalui antrean pekerja lokal.</p>
      </div>
    {:else}
      {#each current.messages as message, index (message.id)}
        {@const date = new Date(message.createdAt)}
        {@const own = message.authorType === 'owner'}
        {@const seat = current.seats.find(s => s.code === message.seat)}
        {@const author = own ? current.owner : seat}
        {@const name = author?.name ?? message.seat ?? ''}
        {@const quoted = message.replyTo ? current.messages.find(m => m.id === message.replyTo) : undefined}
        {#if index === 0 || day.format(date) !== day.format(new Date(current.messages[index - 1].createdAt))}
          <div class="date-divider"><span>{day.format(date) === day.format(new Date()) ? 'Hari ini' : fullDay.format(date)}</span></div>
        {/if}
        <article class="message" class:message-owner={own} class:message-final={message.label} data-message-id={message.id}>
          <img class="message-avatar" src={author?.avatar ?? '/avatars/group.svg'} alt={name}>
          <div class="bubble">
            <div class="message-meta"><span class="author-name">{name}</span><span class="author-role">{own ? 'Pemilik' : seat?.role ?? ''}</span><time datetime={message.createdAt} title={date.toLocaleString('id-ID')}>{clock.format(date)}</time>{#if message.editedAt}<span class="edited-label" title="Diedit {new Date(message.editedAt).toLocaleString('id-ID')}">(diedit)</span>{/if}</div>
            {#if message.label}<div class="final-label">{message.label}</div>{/if}
            {#if message.demo}<span class="demo-label">DEMO</span>{/if}
            {#if quoted}
              <button class="quote-block" type="button" onclick={() => jumpToQuote(quoted.id)}>
                <strong>{quoted.authorType === 'owner' ? current.owner.name : current.seats.find(s => s.code === quoted.seat)?.name ?? quoted.seat}</strong>
                <span>{attachmentLabel(quoted)}</span>
              </button>
            {/if}
            {#if editingId === message.id}
              <form class="edit-area" onsubmit={saveEdit}>
                <textarea bind:value={editText} maxlength="20000" aria-label="Ubah pesan" rows="2"></textarea>
                <div class="edit-actions">
                  <button class="text-button" type="button" onclick={() => { editingId = ''; editText = ''; }}>Batal</button>
                  <button class="primary-button" type="submit">Simpan</button>
                </div>
              </form>
            {:else}
              {#if message.text}
                <div class="message-text">{#each highlight(message) as part}{#if typeof part === 'string'}{part}{:else}<mark class="search-mark">{part.hit}</mark>{/if}{/each}</div>
              {/if}
            {/if}
            {#if message.imageUrl}
              <a class="image-link" href={message.imageUrl} target="_blank" rel="noopener" title="Buka gambar ukuran penuh"><img class="chat-image" src={message.imageUrl} alt={message.text || `Gambar dari ${name}`} loading="lazy" onload={scrollForImage}></a>
            {/if}
            {#if message.attachment}
              {#if isImageAttachment(message.attachment)}
                <a class="image-link" href={message.attachment.url} target="_blank" rel="noopener" title="Buka gambar ukuran penuh"><img class="chat-image" src={message.attachment.url} alt={message.attachment.name} loading="lazy" onload={scrollForImage}></a>
              {:else}
                <a class="attachment-link" href={message.attachment.url} download={message.attachment.name} title="Unduh {message.attachment.name}">
                  <svg viewBox="0 0 20 20" aria-hidden="true"><path d="M10 3v10m0 0-3.5-3.5M10 13l3.5-3.5M4 16.5h12"/></svg>
                  <span><strong>{message.attachment.name}</strong><small>{humanSize(message.attachment.size)}</small></span>
                </a>
              {/if}
            {/if}
            {#if message.status === 'typing' || message.status === 'waiting'}<p class="typing-status" class:waiting={message.status === 'waiting'}>{message.statusText || `${name} sedang mengetik...`}</p>{/if}
            {#if message.error}<pre class="provider-error" aria-label="Error asli provider">{message.error}</pre>{/if}
            {#if message.reactions?.length}
              <div class="reaction-bar">
                {#each message.reactions as reaction}
                  <button class="reaction-pill" type="button" class:mine={reaction.byOwner} title="Reaksi pemilik" onclick={() => toggleReact(message, reaction.emoji)}>{reaction.emoji} {reaction.count}</button>
                {/each}
              </div>
            {/if}
            {#if message.status === 'completed' && editingId !== message.id}
              <div class="msg-actions">
                <button class="msg-action" type="button" aria-label="Balas pesan" title="Balas" onclick={() => startReply(message)}>↩︎</button>
                <div class="react-wrap">
                  <button class="msg-action" type="button" aria-label="Beri reaksi" title="Reaksi" onclick={() => reactFor = reactFor === message.id ? '' : message.id}>😊</button>
                  {#if reactFor === message.id}
                    <div class="emoji-picker" role="menu">
                      {#each emojis as emoji}
                        <button type="button" role="menuitem" onclick={() => toggleReact(message, emoji)}>{emoji}</button>
                      {/each}
                    </div>
                  {/if}
                </div>
                {#if own}<button class="msg-action" type="button" aria-label="Ubah pesan" title="Ubah" onclick={() => startEdit(message)}>✎</button>{/if}
                <button class="msg-action" type="button" aria-label="Hapus pesan" title="Hapus" onclick={() => removeMessage(message)}>🗑</button>
              </div>
            {/if}
          </div>
        </article>
      {/each}
    {/if}
  </section>
  <footer class="composer-area">
    {#if replyTo}
      {@const target = replyTo}
      <div class="reply-chip">
        <span>Membalas <strong>{target.authorType === 'owner' ? current?.owner.name : current?.seats.find(s => s.code === target.seat)?.name ?? target.seat}</strong>: {attachmentLabel(target)}</span>
        <button class="icon-button" type="button" aria-label="Batalkan balasan" onclick={() => replyTo = undefined}>✕</button>
      </div>
    {/if}
    {#if pendingImage}
      <div class="reply-chip image-chip">
        <img src={pendingImage} alt="Pratinjau gambar terlampir" width="34" height="34"><span>Gambar siap dikirim</span>
        <button class="icon-button" type="button" aria-label="Batalkan gambar" onclick={() => pendingImage = ''}>✕</button>
      </div>
    {/if}
    {#if pendingFile}
      <div class="reply-chip file-chip">
        <span><strong>{pendingFile.name}</strong> · {humanSize(pendingFile.size)} · siap dikirim</span>
        <button class="icon-button" type="button" aria-label="Batalkan lampiran" onclick={() => pendingFile = null}>✕</button>
      </div>
    {/if}
    <div class="action-chips" aria-label="Aksi cepat">
      <button id="mentionButton" class="action-chip" type="button" onclick={() => { if (current) mention.showModal(); }}>@ Kursi</button>
      <button id="progressButton" class="action-chip" type="button" onclick={progress} disabled={asking}><svg viewBox="0 0 20 20" aria-hidden="true"><path d="M3 10a7 7 0 1 0 2-5M3 3v4h4M10 6v4l3 2"/></svg>Tanya Progres</button>
      <button id="imageButton" class="action-chip" type="button" onclick={() => put(/^Gambar\s*:/i.test(text) ? text : `Gambar: ${text}`)}><svg viewBox="0 0 20 20" aria-hidden="true"><rect x="2.5" y="2.5" width="15" height="15" rx="3"/><circle cx="7" cy="7" r="1.4"/><path d="m3 15 4-4 3 3 3-5 4 6"/></svg>Buat Gambar</button>
      <button id="uploadImageButton" class="action-chip" type="button" onclick={() => imageInput.click()} disabled={uploadingImage}><svg viewBox="0 0 20 20" aria-hidden="true"><path d="M10 13V4m0 0L6.5 7.5M10 4l3.5 3.5M4 13v2.5h12V13"/></svg>{uploadingImage ? 'Mengunggah…' : 'Kirim Gambar'}</button>
      <button id="uploadFileButton" class="action-chip" type="button" onclick={() => fileInput.click()} disabled={uploadingFile}><svg viewBox="0 0 20 20" aria-hidden="true"><path d="M10 3v8m0 0-3-3m3 3 3-3M4 15v2.5h12V15"/></svg>{uploadingFile ? 'Mengunggah…' : 'Lampirkan File'}</button>
    </div>
    <form bind:this={composer} id="composer" class="composer" onsubmit={send}>
      <textarea bind:this={input} bind:value={text} id="messageInput" rows="1" maxlength="20000" placeholder="Tulis pesan, @kursi, atau Tugas: …" aria-label="Pesan untuk grup" required={!pendingImage && !pendingFile} onkeydown={handleEnter} oninput={resizeInput}></textarea>
      <button id="sendButton" class="send-button" type="submit" aria-label="Kirim pesan" title="Kirim pesan" disabled={submitting}><svg viewBox="0 0 24 24" aria-hidden="true"><path d="m4 4 17 8-17 8 3-8-3-8Z"/><path d="M7 12h13"/></svg></button>
    </form>
    <p class="composer-hint">Enter untuk kirim <span>·</span> Shift + Enter untuk baris baru</p>
  </footer>
</main>

<Settings bind:this={settingsPanel} {current} {refresh} {mutate} {notify} />
<dialog bind:this={mention} id="mentionDialog" class="mention-dialog" aria-labelledby="mentionTitle" onclick={(event) => dismissBackdrop(event, mention)}>
  <div class="mention-heading"><h2 id="mentionTitle">Pilih kursi</h2><button id="closeMention" class="icon-button" type="button" aria-label="Tutup daftar kursi" onclick={() => mention.close()}>✕</button></div>
  <p>Hanya kursi yang disebut akan membalas.</p>
  <div id="mentionList">
    {#each current?.seats ?? [] as seat (seat.code)}
      <button class="mention-option" type="button" data-seat={seat.code} onclick={() => { mention.close(); void insertMention(seat); }}>
        <img src={seat.avatar} alt={seat.name}><span><strong>{seat.name}</strong><small>{seat.role}</small></span><span>{seat.code}</span>
      </button>
    {/each}
  </div>
</dialog>
<div bind:this={mainToast} class="toast" role="status" class:error={notice?.error} hidden={!notice || notice.dialog}>{notice?.dialog ? '' : notice?.text}</div>
<div bind:this={dialogToast} class="toast" role="status" class:error={notice?.error} hidden={!notice?.dialog}>{notice?.dialog ? notice.text : ''}</div>
<input id="chatImageInput" type="file" accept="image/png,image/jpeg,image/gif,image/webp" hidden bind:this={imageInput} onchange={chooseImage}>
<input id="chatFileInput" type="file" hidden bind:this={fileInput} onchange={chooseFile}>
