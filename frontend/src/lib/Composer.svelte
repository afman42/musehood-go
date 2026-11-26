<script lang="ts">
  import { tick } from 'svelte';
  import { attachmentLabel, humanSize } from './format';
  import type { Attachment, Message } from './types';

  let {
    text = $bindable(''), submitting, asking, voting, uploadingImage, uploadingFile, uploadProgress,
    replyTo, replyAuthorName, pendingImage, pendingFile,
    onSend, onProgress, onVote, onOpenMention, onImagePrompt, onUploadImage, onUploadFile,
    onCancelReply, onCancelImage, onCancelFile, onSchedule,
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

  function resizeInput() {
    input.style.height = 'auto';
    input.style.height = `${Math.min(input.scrollHeight, 130)}px`;
  }

  export function focus() {
    input?.focus();
  }

  export async function setText(value: string) {
    text = value;
    await tick();
    resizeInput();
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
    <button id="progressButton" class="action-chip" type="button" onclick={onProgress} disabled={asking}><svg viewBox="0 0 20 20" aria-hidden="true"><path d="M3 10a7 7 0 1 0 2-5M3 3v4h4M10 6v4l3 2"/></svg>Tanya Progres</button>
    <button id="voteButton" class="action-chip" type="button" onclick={onVote} disabled={voting}>★ {voting ? 'Memulai voting…' : 'Voting Jawaban Terbaik'}</button>
    <button id="scheduleButton" class="action-chip" type="button" onclick={onSchedule}>⏰ Jadwalkan</button>
    <button id="imageButton" class="action-chip" type="button" onclick={onImagePrompt}><svg viewBox="0 0 20 20" aria-hidden="true"><rect x="2.5" y="2.5" width="15" height="15" rx="3"/><circle cx="7" cy="7" r="1.4"/><path d="m3 15 4-4 3 3 3-5 4 6"/></svg>Buat Gambar</button>
    <button id="uploadImageButton" class="action-chip" type="button" onclick={onUploadImage} disabled={uploadingImage}><svg viewBox="0 0 20 20" aria-hidden="true"><path d="M10 13V4m0 0L6.5 7.5M10 4l3.5 3.5M4 13v2.5h12V13"/></svg>{uploadingImage ? `Mengunggah… ${uploadProgress ?? 0}%` : 'Kirim Gambar'}</button>
    <button id="uploadFileButton" class="action-chip" type="button" onclick={onUploadFile} disabled={uploadingFile}><svg viewBox="0 0 20 20" aria-hidden="true"><path d="M10 3v8m0 0-3-3m3 3 3-3M4 15v2.5h12V15"/></svg>{uploadingFile ? `Mengunggah… ${uploadProgress ?? 0}%` : 'Lampirkan File'}</button>
  </div>
  <form bind:this={composer} id="composer" class="composer" onsubmit={onSend}>
    <textarea bind:this={input} bind:value={text} id="messageInput" rows="1" maxlength="20000" placeholder="Tulis pesan, @kursi, atau Tugas: …" aria-label="Pesan untuk grup" required={!pendingImage && !pendingFile} onkeydown={handleEnter} oninput={resizeInput}></textarea>
    <button id="sendButton" class="send-button" class:sending={submitting} type="submit" aria-label="Kirim pesan" title="Kirim pesan" disabled={submitting}><svg viewBox="0 0 24 24" aria-hidden="true"><path d="m4 4 17 8-17 8 3-8-3-8Z"/><path d="M7 12h13"/></svg>{#if submitting}<span class="send-label">Mengirim… {sendSeconds} dtk</span>{/if}</button>
  </form>
  <p class="composer-hint">Enter untuk kirim <span>·</span> Shift + Enter untuk baris baru</p>
</footer>
