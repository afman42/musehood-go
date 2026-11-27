// Pemetaan shortcut keyboard (fitur 33). Logika murni agar bisa diuji
// unit tanpa DOM; halaman yang menjalankan aksinya.

export type ShortcutAction = 'search' | 'send' | 'focusComposer' | 'closeDialog' | 'help' | `room${number}`;

export type ShortcutEvent = {
  key: string;
  ctrlKey?: boolean;
  metaKey?: boolean;
  altKey?: boolean;
  shiftKey?: boolean;
};

// shortcutAction menentukan aksi untuk satu peristiwa keyboard.
// inInput=true bila fokus sedang di input/textarea/select/editor.
export function shortcutAction(event: ShortcutEvent, inInput: boolean): ShortcutAction | null {
  const mod = Boolean(event.ctrlKey || event.metaKey);
  if (mod && (event.key === 'k' || event.key === 'K')) return 'search';
  if (mod && event.key === 'Enter') return 'send';
  if (mod && event.key === '/') return 'focusComposer';
  if (event.key === 'Escape') return 'closeDialog';
  if (event.altKey && !mod && /^[1-9]$/.test(event.key)) return `room${event.key}` as ShortcutAction;
  if (event.key === '?' && !inInput && !mod && !event.altKey) return 'help';
  return null;
}
