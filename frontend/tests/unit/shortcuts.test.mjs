// Unit tests untuk pemetaan shortcut keyboard (fitur 33).
import assert from 'node:assert/strict';
import { test } from 'node:test';
import { shortcutAction } from '../../src/lib/shortcuts.ts';

test('shortcutAction: kombinasi Ctrl/Cmd', () => {
  assert.equal(shortcutAction({ key: 'k', ctrlKey: true }, false), 'search');
  assert.equal(shortcutAction({ key: 'K', metaKey: true }, true), 'search');
  assert.equal(shortcutAction({ key: 'Enter', ctrlKey: true }, true), 'send');
  assert.equal(shortcutAction({ key: '/', metaKey: true }, false), 'focusComposer');
  assert.equal(shortcutAction({ key: 'k' }, false), null);
  assert.equal(shortcutAction({ key: 'Enter' }, true), null);
});

test('shortcutAction: Escape dan Alt+angka', () => {
  assert.equal(shortcutAction({ key: 'Escape' }, false), 'closeDialog');
  assert.equal(shortcutAction({ key: 'Escape' }, true), 'closeDialog');
  assert.equal(shortcutAction({ key: '1', altKey: true }, false), 'room1');
  assert.equal(shortcutAction({ key: '9', altKey: true }, false), 'room9');
  assert.equal(shortcutAction({ key: '0', altKey: true }, false), null);
  assert.equal(shortcutAction({ key: '2', altKey: true, ctrlKey: true }, false), null);
});

test('shortcutAction: bantuan "?" hanya di luar input', () => {
  assert.equal(shortcutAction({ key: '?' }, false), 'help');
  assert.equal(shortcutAction({ key: '?' }, true), null);
  assert.equal(shortcutAction({ key: '?', ctrlKey: true }, false), null);
  assert.equal(shortcutAction({ key: 'a' }, false), null);
});

test('shortcutAction: semua binding lengkap + modifier campuran', () => {
  // Ctrl+K / Cmd+K bekerja juga saat fokus di input (pencarian global).
  assert.equal(shortcutAction({ key: 'k', ctrlKey: true }, true), 'search');
  assert.equal(shortcutAction({ key: 'K', ctrlKey: true, shiftKey: true }, false), 'search');
  // Ctrl+Enter = kirim; Enter polos dan Shift+Enter bukan.
  assert.equal(shortcutAction({ key: 'Enter', metaKey: true }, true), 'send');
  assert.equal(shortcutAction({ key: 'Enter', shiftKey: true }, true), null);
  // Ctrl+/ fokus composer.
  assert.equal(shortcutAction({ key: '/', ctrlKey: true }, true), 'focusComposer');
  assert.equal(shortcutAction({ key: '/' }, false), null);
  // Alt+1..9 batas ruang; Alt+0 dan Alt+huruf bukan.
  for (let n = 1; n <= 9; n++) {
    assert.equal(shortcutAction({ key: String(n), altKey: true }, true), `room${n}`);
  }
  assert.equal(shortcutAction({ key: 'a', altKey: true }, false), null);
  assert.equal(shortcutAction({ key: '3', altKey: true, metaKey: true }, false), null);
  // "?" dengan Alt bukan bantuan; Esc selalu menutup dialog.
  assert.equal(shortcutAction({ key: '?', altKey: true }, false), null);
  assert.equal(shortcutAction({ key: 'Escape', ctrlKey: true }, true), 'closeDialog');
  // Huruf polos dan kombinasi tak dikenal tidak beraksi.
  assert.equal(shortcutAction({ key: 's', ctrlKey: true }, false), null);
  assert.equal(shortcutAction({ key: 'Tab' }, false), null);
});
