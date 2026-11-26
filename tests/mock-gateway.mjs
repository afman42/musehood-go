import { once } from 'node:events';
import { createServer } from 'node:http';
import { pathToFileURL } from 'node:url';

export const pixel = Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR4nGMQyQ75DwADPQHTHNReowAAAABJRU5ErkJggg==', 'base64');
export const fixtureKey = 'browser-fixture-only-9876';
export const original401 = 'auth asli';
const models = ['sse', 'array', 'plain', 'delta', 'image', 'denied', 'key-echo', 'toolcode'];
const anthropicModels = ['sse-anthropic', 'json-anthropic'];
const geminiModels = ['gemini-mock'];
const ollamaModels = ['ndjson-ollama', 'json-ollama'];
const responsesModels = ['sse-responses', 'json-responses'];
export const anthroKey = 'anthropic-mock-key';
export const geminiKey = 'gemini-mock-key';

async function readBody(req) {
  const chunks = [];
  for await (const chunk of req) chunks.push(chunk);
  try { return JSON.parse(Buffer.concat(chunks)); } catch { return undefined; }
}

function sse(res, events) {
  res.writeHead(200, { 'Content-Type': 'text/event-stream' });
  for (const event of events) res.write(`data: ${JSON.stringify(event)}\n\n`);
  res.end('data: [DONE]\n\n');
}

// Anthropic mock mounted under /anthropic. Auth: x-api-key + anthropic-version.
async function anthropicMock(req, res, json) {
  if (req.headers['x-api-key'] !== anthroKey || req.headers['anthropic-version'] !== '2023-06-01') return res.writeHead(401).end(original401);
  if (req.method === 'GET' && req.url === '/anthropic/v1/models') return json(200, { data: anthropicModels.map(id => ({ id })) });
  if (req.method !== 'POST' || req.url !== '/anthropic/v1/messages') return json(404, { error: 'Endpoint mock tidak ditemukan' });
  const body = await readBody(req);
  if (body?.model === 'sse-anthropic') {
    if (body.messages?.length && body.messages.some(m => m.content?.includes('sedangdiproses'))) return res.writeHead(429).end('sibuk asli');
    return sse(res, [
      { type: 'message_start', message: { content: [] } },
      { type: 'content_block_delta', delta: { type: 'text_delta', text: 'Balasan ' } },
      { type: 'content_block_delta', delta: { type: 'text_delta', text: 'anthropic' } },
      { type: 'message_stop' },
    ]);
  }
  if (body?.model === 'json-anthropic') {
    if (body.system !== 'Sistem khusus' || body.max_tokens !== 1024) return json(400, { error: 'system/max_tokens mock salah' });
    return json(200, { content: [{ type: 'text', text: 'Balasan anthropic JSON' }] });
  }
  return res.writeHead(401).end(original401);
}

// Gemini mock mounted under /gemini. Auth: ?key= query param.
async function geminiMock(req, res, json) {
  const parsed = new URL(req.url, 'http://mock');
  if (parsed.searchParams.get('key') !== geminiKey) return res.writeHead(401).end(original401);
  if (req.method === 'GET' && parsed.pathname === '/gemini/v1beta/models') {
    return json(200, { models: geminiModels.map(name => ({ name: `models/${name}` })) });
  }
  const match = parsed.pathname.match(/^\/gemini\/v1beta\/models\/([^:]+):generateContent$/);
  if (req.method !== 'POST' || !match) return json(404, { error: 'Endpoint mock tidak ditemukan' });
  const model = decodeURIComponent(match[1]);
  const body = await readBody(req);
  if (!geminiModels.includes(model)) return res.writeHead(401).end(original401);
  if (model === 'gemini-mock' && body.systemInstruction?.parts?.[0]?.text !== 'Sistem khusus') return json(400, { error: { message: 'systemInstruction mock salah' } });
  const candidate = { candidates: [{ content: { parts: [{ text: 'Balasan ' }, { text: 'gemini' }] } }] };
  if (body.contents?.length === 2 && body.contents[1].parts[0].text === 'mintaSSE') {
    return sse(res, [{ candidates: [{ content: { parts: [{ text: 'Balasan SSE gemini' }] } }] }]);
  }
  return json(200, candidate);
}

// Ollama mock mounted under /ollama. Auth: Bearer opsional.
async function ollamaMock(req, res, json) {
  if (req.method === 'GET' && req.url === '/ollama/api/tags') return json(200, { models: ollamaModels.map(name => ({ name })) });
  if (req.method !== 'POST' || req.url !== '/ollama/api/chat') return json(404, { error: 'Endpoint mock tidak ditemukan' });
  const body = await readBody(req);
  if (!ollamaModels.includes(body?.model)) return res.writeHead(401).end(original401);
  if (body.model === 'ndjson-ollama') {
    res.writeHead(200, { 'Content-Type': 'application/x-ndjson' });
    res.write(JSON.stringify({ message: { content: 'Balasan ' }, done: false }) + '\n');
    res.write(JSON.stringify({ message: { content: 'ollama NDJSON' }, done: true }) + '\n');
    return res.end();
  }
  if (body.stream !== false) return json(400, { error: 'mock minta stream false' });
  return json(200, { message: { content: 'Balasan ollama JSON' }, done: true });
}

// OpenAI Responses mock mounted under /openai-responses.
async function responsesMock(req, res, json) {
  if (req.headers.authorization !== `Bearer ${fixtureKey}`) return res.writeHead(401).end(original401);
  if (req.method === 'GET' && req.url === '/openai-responses/v1/models') return json(200, { data: responsesModels.map(id => ({ id })) });
  if (req.method !== 'POST' || req.url !== '/openai-responses/v1/responses') return json(404, { error: 'Endpoint mock tidak ditemukan' });
  const body = await readBody(req);
  if (!responsesModels.includes(body?.model)) return res.writeHead(401).end(original401);
  if (body.model === 'sse-responses') {
    const last = body.input?.[body.input.length - 1];
    if (last?.content !== 'mintaSSE') return res.writeHead(429).end('sibuk asli');
    return sse(res, [
      { type: 'response.output_text.delta', delta: 'Balasan ' },
      { type: 'response.output_text.delta', delta: 'responses SSE' },
    ]);
  }
  if (body.input?.length !== 2 || body.input[0].role !== 'system') return json(400, { error: 'input mock salah' });
  return json(200, { output: [{ type: 'message', content: [{ type: 'output_text', text: 'Balasan responses JSON' }] }] });
}

export async function mockGateway(port = 0) {
  const server = createServer(async (req, res) => {
    const json = (status, body) => res.writeHead(status, { 'Content-Type': 'application/json' }).end(JSON.stringify(body));
    if (req.url.startsWith('/anthropic')) return anthropicMock(req, res, json);
    if (req.url.startsWith('/gemini')) return geminiMock(req, res, json);
    if (req.url.startsWith('/ollama')) return ollamaMock(req, res, json);
    if (req.url.startsWith('/openai-responses')) return responsesMock(req, res, json);
    if (req.headers.authorization !== `Bearer ${fixtureKey}`) return res.writeHead(401).end(original401);
    if (req.method === 'GET' && req.url === '/v1/models') return json(200, { data: models.map(id => ({ id })) });
    if (req.method !== 'POST') return json(404, { error: 'Endpoint mock tidak ditemukan' });
    const chunks = [];
    for await (const chunk of req) chunks.push(chunk);
    let body;
    try { body = JSON.parse(Buffer.concat(chunks)); }
    catch { return json(400, { error: 'JSON mock tidak valid' }); }
    if (body?.model === 'key-echo') return json(401, { error: fixtureKey });
    if (body?.model === 'denied' || !models.includes(body?.model)) return res.writeHead(401).end(original401);
    if (req.url === '/v1/images/generations' && body.model === 'image') {
      return json(200, { data: [{ b64_json: pixel.toString('base64') }] });
    }
    if (req.url !== '/v1/chat/completions') return json(404, { error: 'Endpoint mock tidak ditemukan' });
    // Panggilan voting: balas kode kursi lain (saling memilih M01<->M02).
    const allText = (body.messages || []).map(m => typeof m.content === 'string' ? m.content : '').join('\n');
    if (allText.includes('menilai jawaban')) {
      const pick = allText.includes('Kursimu adalah M01') ? 'M02' : 'M01';
      return json(200, { choices: [{ message: { content: `${pick} karena paling jelas.` } }] });
    }
    switch (body.model) {
      case 'array': return json(200, { choices: [{ message: { content: [{ text: 'Balasan ' }, { text: 'array' }] } }] });
      case 'delta': return json(200, { delta: { content: 'Balasan delta' } });
      case 'plain': return res.writeHead(200, { 'Content-Type': 'text/plain' }).end('Balasan polos');
      case 'sse':
        res.writeHead(200, { 'Content-Type': 'text/event-stream' });
        res.write('data: {"choices":[{"delta":{"content":"Balasan "}}]}\n\n');
        return res.end('data: {"choices":[{"delta":{"content":"SSE"}}]}\n\ndata: [DONE]\n\n');
      case 'toolcode': {
        // Model yang meminta run_code berbahaya (memicu persetujuan pemilik),
        // lalu menjawab final setelah menerima hasil tool.
        const joined = (body.messages || []).map(m => typeof m.content === 'string' ? m.content : '').join('\n');
        if (joined.includes('Hasil tool')) return json(200, { choices: [{ message: { content: 'Kode selesai diproses.' } }] });
        return json(200, { choices: [{ message: { content: '<<TOOL:run_code>>python\nimport os; os.system("echo halo-dari-mock")<</TOOL>>' } }] });
      }
      default: return json(400, { error: 'Gunakan model chat mock' });
    }
  }).listen(port, '127.0.0.1');
  await once(server, 'listening');
  return { server, url: `http://127.0.0.1:${server.address().port}` };
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  const { url } = await mockGateway(Number(process.env.MOCK_PORT || '3101'));
  console.log(`Mock gateway lokal di ${url}`);
}
