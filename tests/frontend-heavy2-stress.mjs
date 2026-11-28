// Tes berat ronde 2 — stress panel baru + performa (28 Nov 2026):
// SettingsOmp (111 pemilih kebijakan), RoomWorkPanel (goal/plan/vibe/
// todo/checkpoint), QuestionBanner (jawaban pilihan + bebas),
// SettingsMCP, initial load <350 KB, buka panel omp pertama kali,
// render ruang 100 todo + 50 checkpoint, dan long task terukur.
// Jalankan: node tests/frontend-heavy2-stress.mjs
import { api, configureMockProvider, eventually, launchBrowser, makeReporter, startApp, startGateway } from './frontend-harness.mjs';

const report = makeReporter('FRONTEND HEAVY2 STRESS');
const app = await startApp();
const gateway = await startGateway();
const browser = await launchBrowser();

async function installLongTaskObserver(page) {
  await page.evaluate(() => {
    window.__longtasks = [];
    try {
      const observer = new PerformanceObserver(list => {
        for (const entry of list.getEntries()) window.__longtasks.push(entry.duration);
      });
      observer.observe({ entryTypes: ['longtask'] });
      window.__longtaskObserver = observer;
    } catch { /* longtask tidak didukung */ }
  });
}

async function longTaskStats(page) {
  return page.evaluate(() => {
    const tasks = window.__longtasks || [];
    return {
      count: tasks.length,
      max: tasks.length ? Math.max(...tasks) : 0,
      total: tasks.reduce((a, b) => a + b, 0),
    };
  }).catch(() => ({ count: 0, max: 0, total: 0 }));
}

try {
  await configureMockProvider(app.url, gateway.url, { M01: 'plain', M02: 'sse', M10: 'array', M04: 'toolquestion' });
  await api(app.url, 'POST', '/api/settings', { settings: { integratorSeat: 'M10' } }, 200);

  const context = await browser.newContext({ viewport: { width: 1280, height: 900 }, bypassCSP: true });
  const page = await context.newPage();
  page.on('dialog', d => d.accept());
  await page.goto(app.url);
  await page.getByText('Pemilik Arman - Admin Muse - Hadir 8/8').waitFor();

  // --- Perf: initial load < 350 KB (transfer JS+CSS yang benar-benar dimuat) ---
  const initialBytes = await page.evaluate(() => {
    const resources = performance.getEntriesByType('resource')
      .filter(r => /\.(js|css)(\?|$)/.test(r.name));
    return resources.reduce((sum, r) => sum + (r.transferSize || r.encodedBodySize || 0), 0);
  });
  report.check(`initial load JS+CSS < 350 KB (${(initialBytes / 1024).toFixed(1)} KB)`, initialBytes < 350 * 1024);

  await installLongTaskObserver(page);

  // --- SettingsOmp: 111 pemilih kebijakan + ubah beruntun ---
  const settingsStart = Date.now();
  await page.getByRole('button', { name: 'Pengaturan grup', exact: true }).click();
  await page.locator('#settingsForm').waitFor({ state: 'visible' });
  const firstOpenMs = Date.now() - settingsStart;
  report.check(`buka Pengaturan pertama kali (lazy chunk) terukur: ${firstOpenMs} ms`, firstOpenMs < 8000);

  const policySelects = page.locator('select[aria-label^="Kebijakan tool"]');
  await eventually(async () => (await policySelects.count()) === 111, 15000);
  report.check('SettingsOmp memuat 111 pemilih kebijakan', true);

  const toolsToChange = ['write_file', 'run_shell', 'eval', 'github_push', 'browser_click', 'memory_retain', 'todo_add', 'checkpoint', 'ast_edit', 'lsp_rename'];
  for (const tool of toolsToChange) {
    await page.locator(`select[aria-label="Kebijakan tool ${tool}"]`).selectOption('deny');
  }
  await eventually(async () => {
    const state = await api(app.url, 'GET', '/api/state', undefined, 200);
    const policies = state.settings?.toolPolicies ?? {};
    return toolsToChange.every(t => policies[t] === 'deny');
  }, 15000);
  report.check('10 kebijakan deny tersimpan lewat UI', true);
  // Kembalikan ke allow agar tidak mengganggu bagian lain.
  for (const tool of toolsToChange) {
    await page.locator(`select[aria-label="Kebijakan tool ${tool}"]`).selectOption('allow');
  }

  // Toggle memori durable + on-demand beruntun.
  const memoryToggle = page.locator('label.toggle-row input[type="checkbox"]').first();
  await memoryToggle.click();
  await memoryToggle.click();

  // Buka/tutup Pengaturan berulang: tidak boleh makin lambat / error.
  for (let i = 0; i < 3; i++) {
    await page.keyboard.press('Escape');
    await page.locator('#settingsForm').waitFor({ state: 'hidden' });
    await page.getByRole('button', { name: 'Pengaturan grup', exact: true }).click();
    await page.locator('#settingsForm').waitFor({ state: 'visible' });
  }
  report.check('buka/tutup Pengaturan 3x stabil', true);
  await page.keyboard.press('Escape');
  await page.locator('#settingsForm').waitFor({ state: 'hidden' });

  // --- SettingsMCP: tambah + hapus server lewat UI ---
  await page.getByRole('button', { name: 'Pengaturan grup', exact: true }).click();
  await page.locator('#settingsForm').waitFor({ state: 'visible' });
  await page.getByLabel('Nama server MCP').fill('berkas_uji');
  await page.getByLabel('Perintah server MCP').fill('python3');
  await page.getByLabel('Argumen server MCP').fill('-c\nprint(1)');
  await page.getByRole('button', { name: 'Simpan server MCP' }).click();
  await eventually(async () => {
    const out = await api(app.url, 'GET', '/api/mcp-servers', undefined, 200);
    return JSON.stringify(out).includes('berkas_uji');
  }, 10000);
  report.check('SettingsMCP menambah server lewat UI', true);
  await page.locator('section', { hasText: 'Server MCP' }).getByRole('button', { name: 'Hapus' }).first().click();
  await eventually(async () => {
    const out = await api(app.url, 'GET', '/api/mcp-servers', undefined, 200);
    return !JSON.stringify(out).includes('berkas_uji');
  }, 10000);
  report.check('SettingsMCP menghapus server lewat UI', true);
  await page.keyboard.press('Escape');
  await page.locator('#settingsForm').waitFor({ state: 'hidden' });

  // --- RoomWorkPanel: goal, mode, todo, checkpoint lewat UI ---
  await page.locator('.work-toggle').click();
  await page.locator('.work-body').waitFor({ state: 'visible' });
  await page.getByRole('button', { name: 'Ubah', exact: true }).click();
  await page.getByPlaceholder('Objektif persisten ruang…').fill('Selesaikan tes berat ronde 2');
  await page.getByRole('button', { name: 'Simpan', exact: true }).click();
  await eventually(async () => {
    const state = await api(app.url, 'GET', '/api/state', undefined, 200);
    const room = (state.rooms ?? []).find(r => r.active);
    return room?.goal === 'Selesaikan tes berat ronde 2';
  }, 10000);
  report.check('RoomWorkPanel menyimpan goal', true);

  await page.getByLabel(/Plan mode/).click();
  await page.getByLabel(/Vibe mode/).click();
  await eventually(async () => {
    const state = await api(app.url, 'GET', '/api/state', undefined, 200);
    const room = (state.rooms ?? []).find(r => r.active);
    return room?.planMode === true && room?.vibeMode === true;
  }, 10000);
  report.check('RoomWorkPanel mengaktifkan plan+vibe mode', true);
  await page.getByLabel(/Plan mode/).click();
  await page.getByLabel(/Vibe mode/).click();

  // Tambah 20 todo lewat UI beruntun.
  const todoInput = page.getByPlaceholder('Tambah todo…');
  for (let i = 0; i < 20; i++) {
    await todoInput.fill(`todo stress ${i}`);
    await page.getByRole('button', { name: 'Tambah', exact: true }).click();
  }
  await eventually(async () => (await page.locator('.todo-row').count()) >= 20, 15000);
  report.check('20 todo ditambahkan lewat UI', true);
  // Ubah status 5 todo pertama lewat select.
  const todoSelects = page.locator('.todo-select');
  for (let i = 0; i < 5; i++) await todoSelects.nth(i).selectOption('done');
  await eventually(async () => {
    const state = await api(app.url, 'GET', '/api/state', undefined, 200);
    const room = (state.rooms ?? []).find(r => r.active);
    return (room?.todos ?? []).filter(t => t.status === 'done').length === 5;
  }, 10000);
  report.check('5 todo diubah statusnya lewat UI', true);

  // Checkpoint lewat UI (butuh pesan di ruang: kirim satu pesan dulu).
  const composer = page.getByRole('textbox', { name: 'Pesan untuk grup' });
  await composer.fill('@Plamo pesan acuan checkpoint');
  await composer.press('Enter');
  await page.getByText('Balasan polos', { exact: true }).first().waitFor({ timeout: 15000 });
  for (let i = 0; i < 3; i++) {
    await page.getByRole('button', { name: '+ Checkpoint sekarang' }).click();
    await page.waitForTimeout(200);
  }
  await eventually(async () => {
    const state = await api(app.url, 'GET', '/api/state', undefined, 200);
    const room = (state.rooms ?? []).find(r => r.active);
    return (room?.checkpoints ?? []).length >= 3;
  }, 10000);
  report.check('3 checkpoint dibuat lewat UI', true);

  // --- QuestionBanner: jawaban pilihan lalu jawaban bebas ---
  await composer.fill('@Deepseek tanya warna');
  await composer.press('Enter');
  await page.locator('.question-banner').waitFor({ state: 'visible', timeout: 20000 });
  report.check('QuestionBanner muncul dengan pilihan', await page.locator('.question-options button').count() === 3);
  await page.locator('.question-options button', { hasText: 'Biru' }).click();
  await page.locator('.question-banner').waitFor({ state: 'hidden', timeout: 10000 });
  await page.getByText('Siap, jawabanmu tercatat.', { exact: true }).first().waitFor({ timeout: 15000 });
  report.check('jawaban pilihan terkirim dan kursi melanjutkan', true);

  await composer.fill('@Deepseek tanya warna lagi');
  await composer.press('Enter');
  await page.locator('.question-banner').waitFor({ state: 'visible', timeout: 20000 });
  await page.getByLabel('Jawaban bebas').fill('Ungu metalik');
  await page.getByRole('button', { name: 'Kirim jawaban' }).click();
  await page.locator('.question-banner').waitFor({ state: 'hidden', timeout: 10000 });
  report.check('jawaban bebas terkirim', true);

  // --- Perf: render ruang 100 todo + 50 checkpoint ---
  const state0 = await api(app.url, 'GET', '/api/state', undefined, 200);
  const roomID = (state0.rooms ?? []).find(r => r.active)?.id ?? 'room-utama';
  const currentTodos = ((state0.rooms ?? []).find(r => r.active)?.todos ?? []).length;
  for (let i = currentTodos; i < 100; i++) {
    await api(app.url, 'POST', `/api/rooms/${roomID}/todos`, { text: `todo skala ${i}` }, 201);
  }
  const currentCP = ((state0.rooms ?? []).find(r => r.active)?.checkpoints ?? []).length;
  for (let i = currentCP; i < 50; i++) {
    await api(app.url, 'POST', `/api/rooms/${roomID}/checkpoint`, { label: `cp skala ${i}` }, 201);
  }
  await page.evaluate(() => { window.__longtasks = []; });
  const renderStart = Date.now();
  await page.reload();
  await page.getByText('Pemilik Arman - Admin Muse - Hadir 8/8').waitFor();
  await installLongTaskObserver(page);
  await page.evaluate(() => { window.__longtasks = []; });
  await page.locator('.work-toggle').click();
  await eventually(async () => (await page.locator('.todo-row').count()) >= 150, 20000);
  const renderMs = Date.now() - renderStart;
  const todoRows = await page.locator('.todo-row').count();
  report.check(`render 100 todo + 50 checkpoint (${todoRows} baris) dalam ${renderMs} ms`, todoRows >= 150 && renderMs < 15000);

  const stats = await longTaskStats(page);
  report.check(`long task saat render panel besar: ${stats.count} task, maks ${stats.max.toFixed(0)} ms, total ${stats.total.toFixed(0)} ms`,
    stats.max < 2000);

  // Scroll panel besar berulang tidak error.
  await page.locator('.work-body').evaluate(el => { el.scrollTop = el.scrollHeight; });
  await page.waitForTimeout(300);
  await page.locator('.work-body').evaluate(el => { el.scrollTop = 0; });
  report.check('scroll panel 150 baris stabil', true);

  await context.close();
} catch (error) {
  report.check('stress selesai tanpa error fatal', false, String(error).slice(0, 300));
} finally {
  await browser.close().catch(() => {});
  await gateway.stop().catch(() => {});
  await app.stop().catch(() => {});
}

process.exit(report.summary() > 0 ? 1 : 0);
