export type Identity = { name: string; avatar: string };
export type Seat = Identity & { code: string; role: string; provider: 'muse' | 'gateway'; model?: string; prompt?: string; defaultPrompt?: string; present: boolean };
export type ReviewLevel = 'Longgar' | 'Santai' | 'Sedang' | 'Ketat';
export type GatewayProtocol = 'openai' | 'openai-responses' | 'anthropic' | 'gemini' | 'ollama';
export type Settings = {
  baseUrl: string; apiKey: string; hasApiKey: boolean; providerName: string; roundRobin: boolean;
  reviewLevel: ReviewLevel; demoFallback: boolean; gatewayProtocol: GatewayProtocol; darkMode: boolean;
};
export type Reaction = { emoji: string; count: number; byOwner: boolean };
export type Attachment = { url: string; name: string; size: number; mime: string };
export type Message = {
  id: string; createdAt: string; authorType: 'owner' | 'seat'; seat?: string;
  text: string; status: string; label?: string; demo?: boolean; statusText?: string; error?: string; imageUrl?: string;
  attachment?: Attachment; replyTo?: string; editedAt?: string; reactions?: Reaction[] | null;
};
export type Connection = { models: string[]; testedAt: string };
export type State = {
  group: Identity; owner: Identity; seats: Seat[]; settings: Settings; messages: Message[];
  presence: { present: number; total: number }; connection: Connection | null;
};

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
