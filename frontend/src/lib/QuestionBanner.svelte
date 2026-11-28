<script lang="ts">
  import type { Question } from './types';

  let { questions, onAnswer }: {
    questions: Question[];
    onAnswer: (id: string, answer: string) => void;
  } = $props();

  let drafts = $state<Record<string, string>>({});
  let sending = $state('');

  async function send(id: string, answer: string) {
    const trimmed = answer.trim();
    if (!trimmed || sending) return;
    sending = id;
    try {
      await onAnswer(id, trimmed);
      drafts = { ...drafts, [id]: '' };
    } finally {
      sending = '';
    }
  }
</script>

{#each questions as question (question.id)}
  <div class="approval-banner question-banner" role="alertdialog" aria-label="Pertanyaan kursi">
    <p class="approval-title">❓ {question.seatName} ({question.seatCode}) bertanya:</p>
    <p class="question-text">{question.text}</p>
    {#if question.options?.length}
      <div class="question-options">
        {#each question.options as option (option)}
          <button class="secondary-button" type="button" disabled={sending === question.id} onclick={() => send(question.id, option)}>{option}</button>
        {/each}
      </div>
    {/if}
    <div class="question-free">
      <input aria-label="Jawaban bebas" maxlength="2000" placeholder="Atau tulis jawaban bebas…" bind:value={drafts[question.id]}
        onkeydown={(event) => { if (event.key === 'Enter') { event.preventDefault(); void send(question.id, drafts[question.id] ?? ''); } }}>
      <button class="primary-button" type="button" disabled={sending === question.id || !(drafts[question.id] ?? '').trim()} onclick={() => send(question.id, drafts[question.id] ?? '')}>{sending === question.id ? 'Mengirim…' : 'Kirim jawaban'}</button>
    </div>
  </div>
{/each}
