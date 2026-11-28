<script lang="ts">
  import { attachmentLabel, humanSize, isImageAttachment } from './format';
  import MarkdownText from './MarkdownText.svelte';
  import type { Message, Seat } from './types';

  let {
    message, seat, seats, own, name, avatar, quoted, quotedAuthorName,
    editing, editText = $bindable(''), reactOpen, emojis, query,
    onReply, onToggleReactPicker, onReact, onPin, onEditStart, onEditSave, onEditCancel, onDelete, onJumpQuote, onImageLoad, onMentionClick, onRegenerate, onRerun, onFork, onExport, onBranch, onTags, onRate, onPickAB,
  }: {
    message: Message;
    seat: Seat | undefined;
    seats: Seat[];
    own: boolean;
    name: string;
    avatar: string;
    quoted: Message | undefined;
    quotedAuthorName: string;
    editing: boolean;
    editText?: string;
    reactOpen: boolean;
    emojis: string[];
    query: string;
    onReply: () => void;
    onToggleReactPicker: () => void;
    onReact: (emoji: string) => void;
    onPin: () => void;
    onEditStart: () => void;
    onEditSave: (event: SubmitEvent) => void;
    onEditCancel: () => void;
    onDelete: () => void;
    onJumpQuote: (id: string) => void;
    onImageLoad: () => void;
    onMentionClick: (seat: Seat) => void;
    onRegenerate: () => void;
    onRerun: () => void;
    onFork: () => void;
    onExport: () => void;
    onBranch: () => void;
    onTags: (tags: string[]) => void;
    onRate: (rating: number) => void;
    onPickAB: () => void;
  } = $props();

  function editTags() {
    const current = (message.tags ?? []).join(', ');
    const input = window.prompt('Tag pesan (pisahkan koma, maks 5):', current);
    if (input === null) return;
    onTags(input.split(',').map((tag) => tag.trim()).filter(Boolean));
  }

  const clock = new Intl.DateTimeFormat('id-ID', { hour: '2-digit', minute: '2-digit' });
  const date = $derived(new Date(message.createdAt));
</script>

<article class={['message', { 'message-owner': own, 'message-final': message.label }]} data-message-id={message.id}>
  <img class="message-avatar" src={avatar} alt={name}>
  <div class="bubble">
    <div class="message-meta"><span class="author-name">{name}</span><span class="author-role">{own ? (message.author ? 'Peserta' : 'Pemilik') : seat?.role ?? ''}</span><time datetime={message.createdAt} title={date.toLocaleString('id-ID')}>{clock.format(date)}</time>{#if message.editedAt}<span class="edited-label" title="Diedit {new Date(message.editedAt).toLocaleString('id-ID')}">(diedit)</span>{/if}{#if message.pinned}<span class="pin-label" title="Pesan tersemat">📌</span>{/if}{#if message.abGroup}<span class="ab-label" title="Uji A/B">Uji A/B · Model {message.abLabel}{#if message.model} · {message.model}{/if}</span>{/if}{#if message.chainId}<span class="chain-label" title="Langkah rantai prompt">⛓ Langkah {message.chainStep}</span>{/if}</div>
    {#if message.label}<div class="final-label">{message.label}</div>{/if}
    {#if message.bestAnswer}<div class="best-label">★ Jawaban Terbaik{#if message.votes} · {message.votes} suara{/if}</div>{/if}
    {#if message.abWinner}<div class="best-label">🏆 Pemenang uji A/B</div>{/if}
    {#if message.demo}<span class="demo-label">DEMO</span>{/if}
    {#if quoted}
      <button class="quote-block" type="button" onclick={() => onJumpQuote(quoted.id)}>
        <strong>{quotedAuthorName}</strong>
        <span>{attachmentLabel(quoted)}</span>
      </button>
    {/if}
    {#if editing}
      <form class="edit-area" onsubmit={onEditSave}>
        <textarea bind:value={editText} maxlength="20000" aria-label="Ubah pesan" rows="2"></textarea>
        <div class="edit-actions">
          <button class="text-button" type="button" onclick={onEditCancel}>Batal</button>
          <button class="primary-button" type="submit">Simpan</button>
        </div>
      </form>
    {:else}
      {#if message.text}
        <div class="message-text"><MarkdownText text={message.text} {seats} {query} {onMentionClick} /></div>
      {/if}
    {/if}
    {#if message.originalText}
      <details class="original-text">
        <summary>✎ Dikoreksi peninjau — lihat teks asli</summary>
        <div class="message-text"><MarkdownText text={message.originalText} {seats} {query} {onMentionClick} /></div>
      </details>
    {/if}
    {#if message.imageUrl}
      <a class="image-link" href={message.imageUrl} target="_blank" rel="noopener" title="Buka gambar ukuran penuh"><img class="chat-image" src={message.imageUrl} alt={message.text || `Gambar dari ${name}`} loading="lazy" onload={onImageLoad}></a>
    {/if}
    {#if message.attachment}
      {#if isImageAttachment(message.attachment)}
        <a class="image-link" href={message.attachment.url} target="_blank" rel="noopener" title="Buka gambar ukuran penuh"><img class="chat-image" src={message.attachment.url} alt={message.attachment.name} loading="lazy" onload={onImageLoad}></a>
      {:else}
        <a class="attachment-link" href={message.attachment.url} download={message.attachment.name} title="Unduh {message.attachment.name}">
          <svg viewBox="0 0 20 20" aria-hidden="true"><path d="M10 3v10m0 0-3.5-3.5M10 13l3.5-3.5M4 16.5h12"/></svg>
          <span><strong>{message.attachment.name}</strong><small>{humanSize(message.attachment.size)}</small></span>
        </a>
      {/if}
    {/if}
    {#if message.status === 'typing' || message.status === 'waiting'}<p class={['typing-status', { waiting: message.status === 'waiting' }]}><span class="typing-dots" aria-hidden="true"><i></i><i></i><i></i></span>{message.statusText || `${name} sedang mengetik...`}</p>{/if}
    {#if message.error}<pre class="provider-error" aria-label="Error asli provider">{message.error}</pre>{/if}
    {#if message.toolCalls?.length}
      <div class="tool-cards" aria-label="Hasil tool">
        {#each message.toolCalls as call, callIndex (callIndex)}
          <details class={['tool-card', { 'tool-fail': !call.ok }]}>
            <summary>🔧 {call.name}{call.ok ? '' : ' · gagal'}</summary>
            {#if call.arg}<p class="tool-arg"><strong>Argumen:</strong> {call.arg}</p>{/if}
            {#if call.result}<pre class="tool-result">{call.result}</pre>{/if}
          </details>
        {/each}
      </div>
    {:else if message.toolsUsed?.length}
      <div class="tools-used" aria-label="Tool yang dipakai">{#each message.toolsUsed as tool, toolIndex (toolIndex)}<span class="tool-chip">🔧 {tool}</span>{/each}</div>
    {/if}
    {#if message.tags?.length}
      <div class="tag-bar" aria-label="Tag pesan">{#each message.tags as tag (tag)}<span class="tool-chip">🏷 {tag}</span>{/each}</div>
    {/if}
    {#if message.reactions?.length}
      <div class="reaction-bar">
        {#each message.reactions as reaction (reaction.emoji)}
          <button class={['reaction-pill', { mine: reaction.byOwner }]} type="button" title="Reaksi pemilik" onclick={() => onReact(reaction.emoji)}>{reaction.emoji} {reaction.count}</button>
        {/each}
      </div>
    {/if}
    {#if message.status === 'completed' && !editing}
      <div class="msg-actions">
        <button class="msg-action" type="button" aria-label="Balas pesan" title="Balas" onclick={onReply}>↩︎</button>
        <div class="react-wrap">
          <button class="msg-action" type="button" aria-label="Beri reaksi" title="Reaksi" onclick={onToggleReactPicker}>😊</button>
          {#if reactOpen}
            <div class="emoji-picker" role="menu">
              {#each emojis as emoji (emoji)}
                <button type="button" role="menuitem" onclick={() => onReact(emoji)}>{emoji}</button>
              {/each}
            </div>
          {/if}
        </div>
        <button class={['msg-action', { 'pin-active': message.pinned }]} type="button" aria-label={message.pinned ? 'Lepas sematan' : 'Sematkan pesan'} title={message.pinned ? 'Lepas sematan' : 'Sematkan'} onclick={onPin}>📌</button>
        {#if own}<button class="msg-action" type="button" aria-label="Ubah pesan" title="Ubah" onclick={onEditStart}>✎</button>{/if}
        {#if own}<button class="msg-action" type="button" aria-label="Jalankan ulang dari sini" title="Jalankan ulang dari sini" onclick={onRerun}>↻</button>{/if}
        {#if !own}<button class="msg-action" type="button" aria-label="Buat ulang balasan" title="Buat ulang balasan" onclick={onRegenerate}>↻</button>{/if}
        <button class="msg-action" type="button" aria-label="Fork ke ruang baru" title="Fork ke ruang baru" onclick={onFork}>⑂</button>
        <button class="msg-action" type="button" aria-label="Branch dari sini" title="Branch dari sini" onclick={onBranch}>⎇</button>
        <button class="msg-action" type="button" aria-label="Ubah tag" title="Ubah tag" onclick={editTags}>🏷</button>
        <button class="msg-action" type="button" aria-label="Ekspor diskusi ini" title="Ekspor diskusi ini" onclick={onExport}>⬇</button>
        <button class="msg-action" type="button" aria-label="Hapus pesan" title="Hapus" onclick={onDelete}>🗑</button>
      </div>
    {/if}
    {#if !own && message.status === 'completed'}
      <div class="rating-bar" aria-label="Nilai balasan ini">
        <button class={['rating-button', { active: message.rating === 1 }]} type="button" aria-label="Balasan bagus" title="Bagus" onclick={() => onRate(1)}>👍</button>
        <button class={['rating-button', { active: message.rating === -1 }]} type="button" aria-label="Balasan kurang" title="Kurang" onclick={() => onRate(-1)}>👎</button>
        {#if message.abGroup && !message.abWinner}<button class="secondary-button ab-pick" type="button" onclick={onPickAB}>Pilih ini</button>{/if}
      </div>
    {/if}
  </div>
</article>
