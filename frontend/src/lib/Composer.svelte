<script lang="ts">
  import { tick } from 'svelte';
  import { attachmentLabel, humanSize } from './format';
  import { t } from './i18n.svelte';
  import { SLASH_COMMANDS } from './slash';
  import type { Attachment, DiscussionTemplate, Message } from './types';

  let {
    text = $bindable(''), submitting, asking, voting, uploadingImage, uploadingFile, uploadProgress,
    replyTo, replyAuthorName, pendingImage, pendingFile,
    templates = [], selectedTemplate = $bindable(''),
    participants = [], ownerName = '', author = $bindable(''),
    onSend, onProgress, onVote, onOpenMention, onImagePrompt, onUploadImage, onUploadFile,
    onCancelReply, onCancelImage, onCancelFile, onSchedule, onDebate, onABTest,
  }: {
    text?: string;
    submitting: boolean;
    asking: boolean;
    voting: boolean;
    uploadingImage: boolean;
    uploadingFile: boolean;
    uploadProgress: number | null;
    replyTo: Message | undefined;
    replyAuthorName: string;
    pendingImage: string;
    pendingFile: Attachment | null;
    templates?: DiscussionTemplate[];
    selectedTemplate?: string;
    participants?: string[];
    ownerName?: string;
    author?: string;
    onSend: (event: SubmitEvent) => void;
    onProgress: () => void;
    onVote: () => void;
    onOpenMention: () => void;
    onImagePrompt: () => void;
    onUploadImage: () => void;
    onUploadFile: () => void;
    onCancelReply: () => void;
    onCancelImage: () => void;
    onCancelFile: () => void;
    onSchedule: () => void;
    onDebate: () => void;
    onABTest: () => void;
  } = $props();

  let input: HTMLTextAreaElement;
  let composer: HTMLFormElement;

  // Hitungan detik selama pesan dikirim, agar tombol kirim menjelaskan
  // bahwa proses sedang berjalan (bukan diam tanpa keterangan).
  let sendSeconds = $state(0);
  $effect(() => {
    if (!submitting) { sendSeconds = 0; return; }
    const started = Date.now();
    const timer = setInterval(() => { sendSeconds = Math.floor((Date.now() - started) / 1000); }, 250);
    return () => clearInterval(timer);
  });

  // Tinggi textarea saat terakhir diterapkan + nilai teksnya. Resize
  // hanya dilakukan bila tinggi memang perlu berubah: men-toggle
  // height auto->px pada tiap ketikan memaksa layout ulang kontainer
  // pesan (mahal pada ruang berisi ribuan pesan).
  let appliedHeight = 0;
  let appliedValue = '';
  function resizeInput(force = false) {
    const value = input.value;
    const lineCount = (s: string) => s.split('\n').length;
    const needsResize = force || appliedHeight === 0
      || lineCount(value) !== lineCount(appliedValue)
      || value.length < appliedValue.length
      || input.scrollHeight > input.clientHeight + 1;
    if (!needsResize) return;
    input.style.height = 'auto';
    appliedHeight = Math.min(input.scrollHeight, 130);
    input.style.height = `${appliedHeight}px`;
    appliedValue = value;
  }

  export function focus() {
    input?.focus();
  }

  export function submit() {
    composer?.requestSubmit();
  }

  export async function setText(value: string) {
    text = value;
    await tick();
    resizeInput(true);
    input.focus();
  }

  export async function insertMention(name: string) {
    const { selectionStart, selectionEnd, value } = input;
    const prefix = selectionStart > 0 && !/\s$/.test(value.slice(0, selectionStart)) ? ' ' : '';
    const insertion = `${prefix}@${name} `;
    await setText(value.slice(0, selectionStart) + insertion + value.slice(selectionEnd));
    input.setSelectionRange(selectionStart + insertion.length, selectionStart + insertion.length);
  }

  function handleEnter(event: KeyboardEvent) {
    if (event.key === 'Enter' && !event.shiftKey && !event.isComposing) {
      event.preventDefault();
      composer.requestSubmit();
    }
  }

  // Petunjuk perintah /slash (fitur 34): tampil saat teks diawali "/" dan
  // perintah belum selesai diketik (belum ada spasi).
  const slashHints = $derived.by(() => {
    const trimmed = text.trimStart();
    if (!trimmed.startsWith('/') || trimmed.includes(' ') || trimmed.includes('\n')) return [];
    const prefix = trimmed.slice(1).toLowerCase();
    return SLASH_COMMANDS.filter((item) => item.name.startsWith(prefix));
  });

  function pickSlash(name: string) {
    void setText(`/${name} `);
  }
</script>

<footer class="composer-area">
  {#if replyTo}
    <div class="reply-chip">
      <span>Membalas <strong>{replyAuthorName}</strong>: {attachmentLabel(replyTo)}</span>
      <button class="icon-button" type="button" aria-label="Batalkan balasan" onclick={onCancelReply}>✕</button>
    </div>
  {/if}
  {#if uploadingImage || uploadingFile}
    <div class="upload-progress" role="progressbar" aria-label="Kemajuan unggahan" aria-valuemin="0" aria-valuemax="100" aria-valuenow={uploadProgress ?? 0}>
      <div class="upload-progress-track"><div class="upload-progress-fill" style:width={`${uploadProgress ?? 0}%`}></div></div>
      <span>{uploadingImage ? 'Mengunggah gambar' : 'Mengunggah file'}… {uploadProgress ?? 0}%</span>
    </div>
  {/if}
  {#if pendingImage}
    <div class="reply-chip image-chip">
      <img src={pendingImage} alt="Pratinjau gambar terlampir" width="34" height="34"><span>✓ Gambar siap dikirim</span>
      <button class="icon-button" type="button" aria-label="Batalkan gambar" onclick={onCancelImage}>✕</button>
    </div>
  {/if}
  {#if pendingFile}
    <div class="reply-chip file-chip">
      <span><strong>{pendingFile.name}</strong> · {humanSize(pendingFile.size)} · ✓ siap dikirim</span>
      <button class="icon-button" type="button" aria-label="Batalkan lampiran" onclick={onCancelFile}>✕</button>
    </div>
  {/if}
  <div class="action-chips" aria-label="Aksi cepat">
    <button id="mentionButton" class="action-chip" type="button" onclick={onOpenMention}>@ Kursi</button>
    <button id="progressButton" class="action-chip" type="button" onclick={onProgress} disabled={asking}><svg viewBox="0 0 20 20" aria-hidden="true"><path d="M3 10a7 7 0 1 0 2-5M3 3v4h4M10 6v4l3 2"/></svg>{t('progress').replace('📊 ', '')}</button>
    <button id="voteButton" class="action-chip" type="button" onclick={onVote} disabled={voting}>★ {voting ? 'Memulai voting…' : t('vote').replace('⭐ ', '')}</button>
    <button id="scheduleButton" class="action-chip" type="button" onclick={onSchedule}>{t('schedule')}</button>
    <button id="debateButton" class="action-chip" type="button" onclick={onDebate}>⚖ Debat</button>
    <button id="abTestButton" class="action-chip" type="button" onclick={onABTest}>🅰🅱 Uji A/B</button>
    {#if participants.length}
      <select id="authorSelect" class="template-select" aria-label="Nama pengirim" bind:value={author}>
        <option value="">{ownerName}</option>
        {#each participants as participant (participant)}<option value={participant}>{participant}</option>{/each}
      </select>
    {/if}
    {#if templates.length}
      <select id="templateSelect" class="template-select" aria-label="Template diskusi" bind:value={selectedTemplate}>
        <option value="">{t('noTemplate')}</option>
        {#each templates as template (template.id)}<option value={template.id}>{template.name}</option>{/each}
      </select>
    {/if}
    <button id="imageButton" class="action-chip" type="button" onclick={onImagePrompt}><svg viewBox="0 0 20 20" aria-hidden="true"><rect x="2.5" y="2.5" width="15" height="15" rx="3"/><circle cx="7" cy="7" r="1.4"/><path d="m3 15 4-4 3 3 3-5 4 6"/></svg>Buat Gambar</button>
    <button id="uploadImageButton" class="action-chip" type="button" onclick={onUploadImage} disabled={uploadingImage}><svg viewBox="0 0 20 20" aria-hidden="true"><path d="M10 13V4m0 0L6.5 7.5M10 4l3.5 3.5M4 13v2.5h12V13"/></svg>{uploadingImage ? `Mengunggah… ${uploadProgress ?? 0}%` : 'Kirim Gambar'}</button>
    <button id="uploadFileButton" class="action-chip" type="button" onclick={onUploadFile} disabled={uploadingFile}><svg viewBox="0 0 20 20" aria-hidden="true"><path d="M10 3v8m0 0-3-3m3 3 3-3M4 15v2.5h12V15"/></svg>{uploadingFile ? `Mengunggah… ${uploadProgress ?? 0}%` : 'Lampirkan File'}</button>
  </div>
  {#if slashHints.length}
    <div class="slash-hints" role="listbox" aria-label="Perintah cepat">
      {#each slashHints as item (item.name)}
        <button class="slash-hint" type="button" role="option" aria-selected="false" onclick={() => pickSlash(item.name)}>
          <strong>{item.usage}</strong><span>{item.hint}</span>
        </button>
      {/each}
    </div>
  {/if}
  <form bind:this={composer} id="composer" class="composer" onsubmit={onSend}>
    <textarea bind:this={input} bind:value={text} id="messageInput" rows="1" maxlength="20000" placeholder={t('composerPlaceholder')} aria-label="Pesan untuk grup" required={!pendingImage && !pendingFile} onkeydown={handleEnter} oninput={() => resizeInput()}></textarea>
    <button id="sendButton" class={['send-button', { sending: submitting }]} type="submit" aria-label={t('send')} title={t('send')} disabled={submitting}><svg viewBox="0 0 24 24" aria-hidden="true"><path d="m4 4 17 8-17 8 3-8-3-8Z"/><path d="M7 12h13"/></svg>{#if submitting}<span class="send-label">{t('sending')} {sendSeconds} dtk</span>{/if}</button>
  </form>
  <p class="composer-hint">Enter untuk kirim <span>·</span> Shift + Enter untuk baris baru <span>·</span> Ketik / untuk perintah cepat</p>
</footer>
