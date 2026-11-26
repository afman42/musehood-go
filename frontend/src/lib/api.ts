import type { GatewayProtocol } from './types';

export const protocolLabels: Record<GatewayProtocol, string> = {
  'openai': 'OpenAI — Chat Completions',
  'openai-responses': 'OpenAI — Responses',
  'anthropic': 'Anthropic — Messages',
  'gemini': 'Google Gemini',
  'ollama': 'Ollama (lokal)',
};

export async function api<T = unknown>(route: string, body?: object | FormData): Promise<T> {
  const multipart = body instanceof FormData;
  const response = await fetch(route, body === undefined ? { cache: 'no-store' } : {
    method: 'POST', body: multipart ? body : JSON.stringify(body),
    headers: multipart ? undefined : { 'Content-Type': 'application/json' },
  });
  let result;
  try { result = await response.json(); }
  catch { throw new Error('Respons server tidak valid.'); }
  if (!response.ok) throw new Error(result.error || `HTTP ${response.status}`);
  return result as T;
}

export async function apiDelete<T = unknown>(route: string): Promise<T> {
  const response = await fetch(route, { method: 'DELETE', cache: 'no-store' });
  let result;
  try { result = await response.json(); }
  catch { throw new Error('Respons server tidak valid.'); }
  if (!response.ok) throw new Error(result.error || `HTTP ${response.status}`);
  return result as T;
}

export function errorText(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

export function dismissBackdrop(event: MouseEvent, dialog: HTMLDialogElement) {
  if (event.target !== dialog) return;
  const { left, right, top, bottom } = dialog.getBoundingClientRect();
  if (event.clientX < left || event.clientX > right || event.clientY < top || event.clientY > bottom) dialog.close();
}
