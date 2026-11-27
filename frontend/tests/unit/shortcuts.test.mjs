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
