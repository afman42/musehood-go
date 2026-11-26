import type { Attachment, Message } from './types';

export function humanSize(size: number): string {
  if (size < 1024) return `${size} B`;
  if (size < 1024 * 1024) return `${(size / 1024).toFixed(1)} KB`;
  return `${(size / (1024 * 1024)).toFixed(1)} MB`;
}

export function isImageAttachment(att: Attachment): boolean {
  return att.mime.startsWith('image/');
}

export function attachmentLabel(message: Message): string {
  if (message.text) return message.text;
  if (message.imageUrl) return '[gambar]';
  if (message.attachment) return `[lampiran: ${message.attachment.name}]`;
  return '';
}
