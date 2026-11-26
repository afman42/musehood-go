// Harness bersama untuk tes frontend berbasis browser (E2E, a11y, visual,
// performa): menjalankan binary asli + mock gateway di port acak dengan
// direktori data sementara, lalu membuka Chromium Playwright.
import { spawn } from 'node:child_process';
import { once } from 'node:events';
import { mkdtempSync, rmSync } from 'node:fs';
import { createServer } from 'node:http';
import { tmpdir } from 'node:os';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { fixtureKey, mockGateway } from './mock-gateway.mjs';

const repo = dirname(dirname(fileURLToPath(import.meta.url)));
export const binaryPath = join(repo, 'musehood-go');

export function makeReporter(title) {
  let passed = 0;
  let failed = 0;
  const failures = [];
  return {
    check(name, ok, detail = '') {
      if (ok) { passed++; console.log(`PASS  ${name}`); }
      else { failed++; failures.push(name); console.log(`FAIL  ${name}${detail ? ' — ' + detail : ''}`); }
    },
    summary() {
      console.log(`\n=== ${title}: ${passed} PASS, ${failed} FAIL ===`);
      if (failed > 0) console.log('Gagal:', failures.join('; '));
      return failed;
    },
  };
}

async function freePort() {
  const server = createServer().listen(0, '127.0.0.1');
  await once(server, 'listening');
  const port = server.address().port;
  await new Promise(resolve => server.close(resolve));
  return port;
}

export async function startApp() {
  const dir = mkdtempSync(join(tmpdir(), 'musehood-fe-'));
  const port = await freePort();
  const child = spawn(binaryPath, [], {
    cwd: dir,
    env: { ...process.env, DATA_DIR: join(dir, 'runtime'), PORT: String(port) },
  });
  let log = '';
  child.stdout.on('data', c => { log += c; });
  child.stderr.on('data', c => { log += c; });
  const url = `http://127.0.0.1:${port}`;
  const deadline = Date.now() + 15000;
  while (Date.now() < deadline) {
    if (child.exitCode !== null) throw new Error('binary berhenti: ' + log);
    try {
      const res = await fetch(url + '/api/state');
      if (res.ok) break;
    } catch { /* belum siap */ }
    await new Promise(r => setTimeout(r, 100));
  }
  return {
    url, dir,
    async stop() {
      child.kill('SIGTERM');
      await once(child, 'exit').catch(() => {});
      rmSync(dir, { recursive: true, force: true });
    },
  };
}

export async function api(url, method, path, body, expect = 200) {
  const res = await fetch(url + path, {
    method,
    headers: body === undefined ? undefined : { 'Content-Type': 'application/json' },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  const json = await res.json().catch(() => ({}));
  if (res.status !== expect) throw new Error(`${method} ${path}: mau ${expect}, dapat ${res.status} ${JSON.stringify(json)}`);
  return json;
}

// Konfigurasi provider mock + model kursi lewat API (tanpa klik UI).
export async function configureMockProvider(appUrl, gatewayUrl, seatModels = { M01: 'plain', M02: 'sse', M10: 'array' }) {
  const created = await api(appUrl, 'POST', '/api/providers', {
    name: 'Mock', baseUrl: gatewayUrl + '/v1', apiKey: fixtureKey, protocol: 'openai',
  }, 201);
  await api(appUrl, 'POST', `/api/providers/${created.id}/activate`, {}, 200);
  const seats = Object.entries(seatModels).map(([code, model]) => ({ code, model }));
  await api(appUrl, 'POST', '/api/settings', { seats }, 200);
  return created.id;
}

export async function startGateway() {
  const gateway = await mockGateway();
  return { url: gateway.url, stop: () => new Promise(r => gateway.server.close(r)) };
}

export async function launchBrowser() {
  const { chromium } = await import('../frontend/node_modules/playwright/index.mjs');
  return chromium.launch({
    executablePath: process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH || undefined,
    args: ['--no-sandbox'],
  });
}

export async function eventually(fn, timeoutMs = 10000) {
  const deadline = Date.now() + timeoutMs;
  let last;
  while (Date.now() < deadline) {
    try { const value = await fn(); if (value) return value; } catch (e) { last = e; }
    await new Promise(r => setTimeout(r, 100));
  }
  throw new Error('kondisi tidak tercapai', { cause: last });
}
