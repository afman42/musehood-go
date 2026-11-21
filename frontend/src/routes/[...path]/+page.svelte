<script lang="ts">
  import { onMount, tick } from 'svelte';
  import Settings from '../../lib/Settings.svelte';
  import { api, dismissBackdrop, errorText, type Seat, type State } from '../../lib/api';

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

  async function send(event: SubmitEvent) {
    event.preventDefault();
    if (!text.trim() || submitting) return;
    submitting = true;
    try {
      forceScroll = true;
      await api('/api/message', { text: text.trim() });
      await put('');
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
</script>

<svelte:head><title>{current ? `${current.group.name} — Musehood` : 'Musehood — Grup multi-AI'}</title></svelte:head>

<main class="app">
  <header class="group-header">
    <img id="groupAvatar" class="group-avatar" src={current?.group.avatar ?? '/avatars/group.svg'} alt={current ? `Foto grup ${current.group.name}` : 'Foto grup'} width="42" height="42">
    <div class="group-heading">
      <h1 id="groupName" title={current?.group.name ?? 'Musehood'}>{current?.group.name ?? 'Musehood'}</h1>
      <p id="groupSubtitle">{current ? `Pemilik ${current.owner.name} - Admin ${current.seats[0].name} - Hadir ${current.presence.present}/${current.presence.total}` : 'Menghubungkan grup…'}</p>
    </div>
    <button id="openSettings" class="icon-button" type="button" aria-label="Pengaturan grup" title="Pengaturan grup" onclick={() => settingsPanel.open()}>
      <svg viewBox="0 0 24 24" aria-hidden="true"><path d="m9.7 3-.6 2.2-2 .9L5 5.5 2.8 9.3l1.6 1.6v2.2l-1.6 1.6L5 18.5l2.1-.6 2 .9.6 2.2h4.6l.6-2.2 2-.9 2.1.6 2.2-3.8-1.6-1.6v-2.2l1.6-1.6L19 5.5l-2.1.6-2-.9L14.3 3Z"/><circle cx="12" cy="12" r="3.2"/></svg>
    </button>
  </header>
  <nav id="seatStrip" class="seat-strip" aria-label="Tujuh kursi AI">
    {#each current?.seats ?? [] as seat (seat.code)}
      <button class="seat-card" type="button" title={`@${seat.name} · ${seat.code} · ${seat.role}`} aria-label={`Sebut ${seat.name}, ${seat.role}`} onclick={() => insertMention(seat)}>
        <span class="seat-avatar-wrap"><img src={seat.avatar} alt={seat.name} width="40" height="40"><span class="presence-dot" class:offline={!seat.present} title={seat.present ? 'Hadir' : 'Tidak hadir'}></span></span>
        <span class="seat-name">{seat.name}</span><span class="seat-role">{seat.role}</span>
      </button>
    {/each}
  </nav>
  {#if current && !(current.settings.baseUrl && current.settings.hasApiKey)}
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
        {#if index === 0 || day.format(date) !== day.format(new Date(current.messages[index - 1].createdAt))}
          <div class="date-divider"><span>{day.format(date) === day.format(new Date()) ? 'Hari ini' : fullDay.format(date)}</span></div>
        {/if}
        <article class="message" class:message-owner={own} class:message-final={message.label} data-message-id={message.id}>
          <img class="message-avatar" src={author?.avatar ?? '/avatars/group.svg'} alt={name}>
          <div class="bubble">
            <div class="message-meta"><span class="author-name">{name}</span><span class="author-role">{own ? 'Pemilik' : seat?.role ?? ''}</span><time datetime={message.createdAt} title={date.toLocaleString('id-ID')}>{clock.format(date)}</time></div>
            {#if message.label}<div class="final-label">{message.label}</div>{/if}
            {#if message.demo}<span class="demo-label">DEMO</span>{/if}
            {#if message.text}<div class="message-text">{message.text}</div>{/if}
            {#if message.imageUrl}
              <a class="image-link" href={message.imageUrl} target="_blank" rel="noopener" title="Buka gambar ukuran penuh"><img class="chat-image" src={message.imageUrl} alt={message.text || `Gambar dari ${name}`} loading="lazy" onload={scrollForImage}></a>
            {/if}
            {#if message.status === 'typing' || message.status === 'waiting'}<p class="typing-status" class:waiting={message.status === 'waiting'}>{message.statusText || `${name} sedang mengetik...`}</p>{/if}
            {#if message.error}<pre class="provider-error" aria-label="Error asli provider">{message.error}</pre>{/if}
          </div>
        </article>
      {/each}
    {/if}
  </section>
  <footer class="composer-area">
    <div class="action-chips" aria-label="Aksi cepat">
      <button id="mentionButton" class="action-chip" type="button" onclick={() => { if (current) mention.showModal(); }}>@ Kursi</button>
      <button id="progressButton" class="action-chip" type="button" onclick={progress} disabled={asking}><svg viewBox="0 0 20 20" aria-hidden="true"><path d="M3 10a7 7 0 1 0 2-5M3 3v4h4M10 6v4l3 2"/></svg>Tanya Progres</button>
      <button id="imageButton" class="action-chip" type="button" onclick={() => put(/^Gambar\s*:/i.test(text) ? text : `Gambar: ${text}`)}><svg viewBox="0 0 20 20" aria-hidden="true"><rect x="2.5" y="2.5" width="15" height="15" rx="3"/><circle cx="7" cy="7" r="1.4"/><path d="m3 15 4-4 3 3 3-5 4 6"/></svg>Buat Gambar</button>
    </div>
    <form bind:this={composer} id="composer" class="composer" onsubmit={send}>
      <textarea bind:this={input} bind:value={text} id="messageInput" rows="1" maxlength="20000" placeholder="Tulis pesan, @kursi, atau Tugas: …" aria-label="Pesan untuk grup" required onkeydown={handleEnter} oninput={resizeInput}></textarea>
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
