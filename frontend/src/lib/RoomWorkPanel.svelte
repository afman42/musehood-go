<script lang="ts">
  import type { Room } from './types';

  let { room, onAddTodo, onTodoStatus, onDeleteTodo, onCheckpoint, onRewind, onModes }: {
    room: Room;
    onAddTodo: (text: string) => Promise<void>;
    onTodoStatus: (todoId: string, status: string) => Promise<void>;
    onDeleteTodo: (todoId: string) => Promise<void>;
    onCheckpoint: (label: string) => Promise<void>;
    onRewind: (checkpointId: string) => Promise<void>;
    onModes: (patch: { goal?: string; planMode?: boolean; vibeMode?: boolean }) => Promise<void>;
  } = $props();

  let open = $state(false);
  let newTodo = $state('');
  let goalDraft = $state('');
  let goalEditing = $state(false);

  const todos = $derived(room.todos ?? []);
  const checkpoints = $derived(room.checkpoints ?? []);
  const doneCount = $derived(todos.filter((t) => t.status === 'done').length);

  const statusMark: Record<string, string> = { pending: '○', active: '◐', done: '●', blocked: '⊘' };
  const nextStatus: Record<string, string> = { pending: 'active', active: 'done', done: 'pending', blocked: 'pending' };

  async function submitTodo(event: Event) {
    event.preventDefault();
    const text = newTodo.trim();
    if (!text) return;
    newTodo = '';
    await onAddTodo(text);
  }

  async function saveGoal() {
    await onModes({ goal: goalDraft.trim() });
    goalEditing = false;
  }

  function startGoalEdit() {
    goalDraft = room.goal ?? '';
    goalEditing = true;
  }

  async function confirmRewind(id: string, label: string) {
    if (!window.confirm(`Rewind ke checkpoint "${label}"? Pesan sesudah checkpoint akan dipotong.`)) return;
    await onRewind(id);
  }
</script>

<div class="work-panel">
  <button class="work-toggle" type="button" aria-expanded={open} onclick={() => (open = !open)}>
    🧰 Kerja ruang
    {#if todos.length > 0}<span class="work-badge">{doneCount}/{todos.length} todo</span>{/if}
    {#if room.planMode}<span class="work-badge plan">plan mode</span>{/if}
    {#if room.vibeMode}<span class="work-badge vibe">vibe</span>{/if}
    <span class="work-caret">{open ? '▾' : '▸'}</span>
  </button>

  {#if open}
    <div class="work-body">
      <div class="work-row">
        <span class="work-label">Objektif (goal)</span>
        {#if goalEditing}
          <input class="work-input" bind:value={goalDraft} placeholder="Objektif persisten ruang…" maxlength="2000" />
          <button class="work-btn" type="button" onclick={saveGoal}>Simpan</button>
          <button class="work-btn ghost" type="button" onclick={() => (goalEditing = false)}>Batal</button>
        {:else}
          <span class="work-goal">{room.goal || '(belum ada)'}</span>
          <button class="work-btn ghost" type="button" onclick={startGoalEdit}>Ubah</button>
        {/if}
      </div>

      <div class="work-row">
        <label class="work-check">
          <input type="checkbox" checked={room.planMode ?? false} onchange={(e) => void onModes({ planMode: (e.currentTarget as HTMLInputElement).checked })} />
          Plan mode (hanya tool Read)
        </label>
        <label class="work-check">
          <input type="checkbox" checked={room.vibeMode ?? false} onchange={(e) => void onModes({ vibeMode: (e.currentTarget as HTMLInputElement).checked })} />
          Vibe mode (integrator direktur)
        </label>
      </div>

      <div class="work-section">
        <span class="work-label">Checklist</span>
        {#each todos as todo (todo.id)}
          <div class="todo-row">
            <button class="todo-mark" type="button" title="Ubah status" onclick={() => void onTodoStatus(todo.id, nextStatus[todo.status] ?? 'pending')}>{statusMark[todo.status] ?? '○'}</button>
            <span class="todo-text" class:done={todo.status === 'done'}>{todo.text}</span>
            <select class="todo-select" value={todo.status} onchange={(e) => void onTodoStatus(todo.id, (e.currentTarget as HTMLSelectElement).value)}>
              <option value="pending">pending</option>
              <option value="active">active</option>
              <option value="done">done</option>
              <option value="blocked">blocked</option>
            </select>
            <button class="todo-del" type="button" title="Hapus todo" onclick={() => void onDeleteTodo(todo.id)}>✕</button>
          </div>
        {/each}
        <form class="todo-add" onsubmit={submitTodo}>
          <input class="work-input" bind:value={newTodo} placeholder="Tambah todo…" maxlength="500" />
          <button class="work-btn" type="submit">Tambah</button>
        </form>
      </div>

      <div class="work-section">
        <span class="work-label">Checkpoint</span>
        {#each checkpoints as cp (cp.id)}
          <div class="todo-row">
            <span class="todo-text">⏪ {cp.label}</span>
            <button class="work-btn ghost" type="button" onclick={() => void confirmRewind(cp.id, cp.label)}>Rewind</button>
          </div>
        {/each}
        <button class="work-btn" type="button" onclick={() => void onCheckpoint('')}>+ Checkpoint sekarang</button>
      </div>
    </div>
  {/if}
</div>
