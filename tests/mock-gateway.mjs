import { once } from 'node:events';
import { createServer } from 'node:http';
import { pathToFileURL } from 'node:url';

export const pixel = Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR4nGMQyQ75DwADPQHTHNReowAAAABJRU5ErkJggg==', 'base64');
export const fixtureKey = 'browser-fixture-only-9876';
export const original401 = 'auth asli';
const models = ['sse', 'array', 'plain', 'delta', 'image', 'denied', 'key-echo'];

export async function mockGateway(port = 0) {
  const server = createServer(async (req, res) => {
    const json = (status, body) => res.writeHead(status, { 'Content-Type': 'application/json' }).end(JSON.stringify(body));
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
    switch (body.model) {
      case 'array': return json(200, { choices: [{ message: { content: [{ text: 'Balasan ' }, { text: 'array' }] } }] });
      case 'delta': return json(200, { delta: { content: 'Balasan delta' } });
      case 'plain': return res.writeHead(200, { 'Content-Type': 'text/plain' }).end('Balasan polos');
      case 'sse':
        res.writeHead(200, { 'Content-Type': 'text/event-stream' });
        res.write('data: {"choices":[{"delta":{"content":"Balasan "}}]}\n\n');
        return res.end('data: {"choices":[{"delta":{"content":"SSE"}}]}\n\ndata: [DONE]\n\n');
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
