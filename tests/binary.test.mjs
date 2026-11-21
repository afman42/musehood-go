import assert from 'node:assert/strict';
import { spawn } from 'node:child_process';
import { once } from 'node:events';
import { cp, mkdir, mkdtemp, readFile, rm, writeFile } from 'node:fs/promises';
import { createServer } from 'node:http';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { test } from 'node:test';
import { fixtureKey as secret, mockGateway, original401, pixel } from './mock-gateway.mjs';
const binary = fileURLToPath(new URL('../musehood-go', import.meta.url));

async function eventually(fn, deadline = Date.now() + 8000) {
  let last;
  while (Date.now() < deadline) {
    try { const result = await fn(); if (result) return result; } catch (error) { last = error; }
    await new Promise(resolve => setTimeout(resolve, 50));
  }
  throw new Error('Kondisi pengujian tidak tercapai', { cause: last });
}

async function launch(t, browserTest = false) {
  const dir = await mkdtemp(join(tmpdir(), 'musehood-go-'));
  t.after(() => rm(dir, { recursive: true, force: true }));
  const executable = join(dir, 'app');
  await cp(binary, executable);
  const env = { ...process.env, DATA_DIR: join(dir, 'runtime'), TRUSTED_HOSTS: 'muse.test' };
  if (browserTest) {
    const socket = createServer().listen(0, '127.0.0.1');
    await once(socket, 'listening');
    env.PORT = String(socket.address().port);
    await new Promise(resolve => socket.close(resolve));
  } else delete env.PORT;
  const child = spawn(executable, [], { cwd: dir, env });
  let log = '';
  child.stdout.on('data', chunk => { log += chunk; });
  child.stderr.on('data', chunk => { log += chunk; });
  const exited = once(child, 'exit');
  t.after(async () => {
    child.kill('SIGTERM');
    const [code] = await exited;
    assert.equal(code, 0, log);
    assert.ok(!log.includes(secret), 'process log leaked the fixture credential');
  });
  const url = await eventually(() => {
    if (child.exitCode !== null) throw new Error(log);
    return log.match(/http:\/\/127\.0\.0\.1:\d+/)?.[0];
  });
  return { url, dir };
}

test('binary mandiri menyajikan HTML, aset embed dan fallback SPA', async t => {
  const { url, dir } = await launch(t);
  assert.equal(url, 'http://127.0.0.1:3001');
  const page = await fetch(url);
  assert.equal(page.status, 200);
  assert.match(page.headers.get('content-type'), /text\/html/);
  assert.match(page.headers.get('content-security-policy'), /script-src 'self' 'sha256-/);
  const html = await page.text();
  assert.match(html, /lang="id"/);
  assert.match(html, /Musehood/);
  assert.equal(await (await fetch(url + '/rute/spa')).text(), html);
  const avatar = await fetch(url + '/avatars/group.svg');
  assert.equal(avatar.status, 200);
  assert.match(await avatar.text(), /<svg/);
  const state = await (await fetch(url + '/api/state')).json();
  assert.equal(state.owner.name, 'Arman');
  assert.deepEqual(state.presence, { present: 8, total: 8 });
  const data = await readFile(join(dir, 'runtime', 'data.json'), 'utf8');
  assert.equal(JSON.parse(data).version, 1);
});

test('Chromium memakai chat, pengaturan dan gambar dari binary yang sama', { skip: process.env.MUSEHOOD_BROWSER !== '1' }, async t => {
  const { chromium } = await import('../frontend/node_modules/playwright/index.mjs');
  const { url, dir } = await launch(t, true);
  const gateway = await mockGateway();
  t.after(() => new Promise(resolve => gateway.server.close(resolve)));
  const browser = await chromium.launch({ executablePath: process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH || undefined, args: ['--no-sandbox'] });
  t.after(() => browser.close());
  const page = await browser.newPage({ viewport: { width: 320, height: 740 } });
  const errors = [];
  page.on('pageerror', e => errors.push(e.message));
  page.on('console', e => { if (e.type() === 'error') errors.push(e.text()); });
  await page.goto(url + '/rute/spa');
  await page.getByText('Pemilik Arman - Admin Muse - Hadir 8/8').waitFor();
  assert.equal(await page.locator('.seat-card').count(), 7);
  assert.ok(await page.locator('.seat-strip').evaluate(el => el.scrollWidth > el.clientWidth));
  for (const width of [320, 390, 1440]) {
    await page.setViewportSize({ width, height: 740 });
    assert.equal(await page.evaluate(() => document.documentElement.scrollWidth), width);
    assert.ok((await page.locator('#composer').boundingBox()).y > 500);
  }
  await page.setViewportSize({ width: 390, height: 844 });
  await page.getByRole('button', { name: 'Pengaturan grup', exact: true }).click();
  await page.getByLabel('Nama pemilik', { exact: true }).fill('Bima');
  await page.getByLabel('Base URL').fill(`${gateway.url}/v1`);
  await page.getByLabel('API key', { exact: true }).fill(secret);
  assert.equal(await page.locator('#apiKey').getAttribute('type'), 'password');
  assert.equal(await page.getByLabel('Model MUSE', { exact: true }).count(), 0);
  await page.getByLabel('Model M10').fill('sse');
  await page.getByLabel('Model M01').fill('array');
  await page.getByLabel('Model M04').fill('plain');
  await page.getByLabel('Model M03').fill('image');
  await page.getByLabel('Model M02').fill('missing');
  await page.getByLabel('Model M05').fill('key-echo');
  await page.getByRole('button', { name: 'Tes Koneksi' }).click();
  await page.getByText('Terhubung · 7 model ditemukan.', { exact: false }).waitFor();
  assert.equal(await page.locator('[data-model-status=M02]').getAttribute('data-available'), 'false');
  assert.equal(await page.locator('[data-model-status=M10]').getAttribute('data-available'), 'true');
  assert.equal(await page.locator('#apiKey').inputValue(), '');
  await page.getByText('Key tersimpan: ••••••••9876').waitFor();
  await page.getByRole('button', { name: 'Ganti foto pemilik' }).click();
  await page.locator('#avatarInput').setInputFiles({ name: 'foto.png', mimeType: 'image/png', buffer: pixel });
  await page.getByText('Foto berhasil diperbarui.').waitFor();
  await page.getByRole('button', { name: 'Tutup pengaturan' }).click();
  await page.getByText('Pemilik Bima - Admin Muse - Hadir 8/8').waitFor();
  const composer = page.getByRole('textbox', { name: 'Pesan untuk grup' });
  await page.getByRole('button', { name: '@ Kursi', exact: true }).click();
  await page.locator('[data-seat=M10]').click();
  assert.equal(await composer.inputValue(), '@Opus ');
  await composer.fill('@Opus <img src=x onerror=alert(1)>');
  await composer.press('Enter');
  await page.getByText('Balasan SSE', { exact: true }).waitFor();
  assert.equal(await page.locator('.message-owner .author-name').textContent(), 'Bima');
  assert.equal(await page.locator('.message-text img').count(), 0);
  await composer.fill('@GLM cek'); await composer.press('Enter');
  await page.locator('.provider-error').getByText(`HTTP 401: ${original401}`).waitFor();
  await composer.fill('@Fable cek redaksi'); await composer.press('Enter');
  await page.getByText('HTTP 401: {"error":"[RAHASIA DISEMBUNYIKAN]"}', { exact: true }).waitFor();
  assert.ok(!(await (await fetch(url + '/api/state')).text()).includes(secret));
  await page.getByRole('button', { name: 'Buat Gambar' }).click();
  assert.equal(await composer.inputValue(), 'Gambar: ');
  await composer.fill('Gambar: Bunga'); await composer.press('Enter');
  await page.waitForFunction(() => document.querySelector('.chat-image')?.naturalWidth === 1);
  await composer.fill('Tugas: Susun taman'); await composer.press('Enter');
  await page.getByText('Muse sedang mengetik...', { exact: true }).waitFor();
  const pending = async kind => eventually(async () => (await (await fetch(url + '/api/queue/muse')).json()).requests.find(r => r.kind === kind));
  let req = await pending('summary');
  await writeFile(join(dir, 'runtime', 'queue', 'responses', `${req.id}.json`), JSON.stringify({ text: 'Integrasi taman selesai' }));
  await page.getByText('Integrasi taman selesai').waitFor();
  await page.getByText('Hasil Akhir - Integrasi').waitFor();
  await page.getByRole('button', { name: 'Tanya Progres' }).click();
  req = await pending('progress');
  assert.equal(req.context.activeTask.title, 'Susun taman');
  assert.equal((await fetch(url + '/api/queue/muse/' + req.id, { method: 'POST', body: '{"text":"Tugas selesai"}' })).status, 200);
  await page.getByText('Tugas selesai', { exact: true }).waitFor();
  await page.getByRole('button', { name: 'Pengaturan grup', exact: true }).click();
  const clear = page.getByRole('button', { name: 'Bersihkan chat' });
  page.once('dialog', d => d.dismiss()); await clear.click();
  assert.ok(await page.locator('.message').count() > 0);
  page.once('dialog', d => d.accept()); await clear.click();
  await page.waitForFunction(() => !document.querySelector('.message'));
  assert.equal(await page.getByLabel('Nama pemilik', { exact: true }).inputValue(), 'Bima');
  assert.deepEqual(errors, []);
  if (process.env.MUSEHOOD_ARTIFACTS) {
    await mkdir(process.env.MUSEHOOD_ARTIFACTS, { recursive: true });
    await page.screenshot({ path: join(process.env.MUSEHOOD_ARTIFACTS, 'mobile-settings.png') });
    await page.getByRole('button', { name: 'Tutup pengaturan' }).click();
    await page.screenshot({ path: join(process.env.MUSEHOOD_ARTIFACTS, 'mobile.png') });
  }
});
