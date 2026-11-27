<script lang="ts">
  import { dismissBackdrop } from './api';

  let dialog: HTMLDialogElement;

  export function open() {
    if (!dialog.open) dialog.showModal();
  }

  const rows = [
    ['Ctrl/Cmd + K', 'Fokus pencarian pesan'],
    ['Ctrl/Cmd + Enter', 'Kirim pesan'],
    ['Ctrl/Cmd + /', 'Fokus kotak pesan'],
    ['Alt + 1…9', 'Pindah ruang (urutan bilah ruang)'],
    ['Esc', 'Tutup dialog yang terbuka'],
    ['?', 'Buka bantuan shortcut ini'],
  ];
</script>

<dialog bind:this={dialog} id="shortcutHelpDialog" class="settings-dialog" aria-labelledby="shortcutHelpTitle" onclick={(event) => dismissBackdrop(event, dialog)}>
  <header class="panel-header">
    <div><p class="eyebrow">MUSEHOOD</p><h2 id="shortcutHelpTitle">Shortcut Keyboard</h2></div>
    <button class="icon-button" type="button" aria-label="Tutup bantuan shortcut" onclick={() => dialog.close()}>✕</button>
  </header>
  <div class="settings-form">
    <div class="stats-table" role="table" aria-label="Daftar shortcut keyboard">
      {#each rows as [keys, label] (keys)}
        <div class="stats-row" role="row"><span role="cell"><kbd>{keys}</kbd></span><span role="cell">{label}</span></div>
      {/each}
    </div>
  </div>
</dialog>
