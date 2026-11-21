export type Identity = { name: string; avatar: string };
export type Seat = Identity & { code: string; role: string; provider: 'muse' | 'gateway'; model?: string; present: boolean };
export type ReviewLevel = 'Longgar' | 'Santai' | 'Sedang' | 'Ketat';
export type Settings = {
  baseUrl: string; apiKey: string; hasApiKey: boolean; roundRobin: boolean;
  reviewLevel: ReviewLevel; demoFallback: boolean;
};
export type Message = {
  id: string; createdAt: string; authorType: 'owner' | 'seat'; seat?: string;
  text: string; status: string; label?: string; demo?: boolean; statusText?: string; error?: string; imageUrl?: string;
};
export type Connection = { models: string[]; testedAt: string };
export type State = {
  group: Identity; owner: Identity; seats: Seat[]; settings: Settings; messages: Message[];
  presence: { present: number; total: number }; connection: Connection | null;
};

export async function api<T = unknown>(route: string, body?: object): Promise<T> {
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

export function errorText(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

export function dismissBackdrop(event: MouseEvent, dialog: HTMLDialogElement) {
  if (event.target !== dialog) return;
  const { left, right, top, bottom } = dialog.getBoundingClientRect();
  if (event.clientX < left || event.clientX > right || event.clientY < top || event.clientY > bottom) dialog.close();
}
