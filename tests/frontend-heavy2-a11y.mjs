// Tes aksesibilitas berat ronde 2 (28 Nov 2026): axe pada panel baru
// SettingsOmp (111 pemilih kebijakan), RoomWorkPanel, QuestionBanner,
// SettingsMCP dalam tema terang DAN gelap; navigasi keyboard pada panel
// baru (Tab mencapai kontrol, Escape menutup + fokus kembali, Enter
// mengaktifkan pilihan pertanyaan).
// Jalankan: node tests/frontend-heavy2-a11y.mjs
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { api, configureMockProvider, launchBrowser, makeReporter, startApp, startGateway } from './frontend-harness.mjs';

const repo = dirname(dirname(fileURLToPath(import.meta.url)));
const axePath = join(repo, 'frontend', 'node_modules', 'axe-core', 'axe.min.js');
const report = makeReporter('FRONTEND HEAVY2 A11Y');
const app = await startApp();
const gateway = await startGateway();
const browser = await launchBrowser();

async function axeAudit(page, label, scopeSelector) {
  await page.addStyleTag({ content: '*{animation:none!important;transition:none!important}' }).catch(() => {});
  await page.waitForTimeout(250);
  await page.addScriptTag({ path: axePath });
  const results = await page.evaluate(async (scope) => {
    const root = scope ? document.querySelector(scope) : document;
    if (!root) return [{ id: 'scope-tidak-ditemukan', impact: 'critical', nodes: 1, help: scope }];
    const out = await window.axe.run(root, { resultTypes: ['violations'] });
    return out.violations.map(v => ({ id: v.id, impact: v.impact, nodes: v.nodes.length, help: v.help }));
  }, scopeSelector);
  const serious = results.filter(v => v.impact === 'critical' || v.impact === 'serious');
  for (const v of results) console.log(`  axe [${label}] ${v.impact}: ${v.id} (${v.nodes} node) — ${v.help}`);
  report.check(`axe ${label}: tanpa pelanggaran critical/serious`, serious.length === 0, serious.map(v => v.id).join(', '));
}

try {
  await configureMockProvider(app.url, gateway.url, { M01: 'plain', M02: 'sse', M10: 'array', M04: 'toolquestion' });
  await api(app.url, 'POST', '/api/settings', { settings: { integratorSeat: 'M10' } }, 200);
  // Isi ruang: todo + checkpoint agar RoomWorkPanel terisi saat diaudit.
  const state0 = await api(app.url, 'GET', '/api/state', undefined, 200);
  const roomID = (state0.rooms ?? []).find(r => r.active)?.id ?? 'room-utama';
  for (let i = 0; i < 3; i++) await api(app.url, 'POST', `/api/rooms/${roomID}/todos`, { text: `todo a11y ${i}` }, 201);

  for (const theme of ['light', 'dark']) {
    await api(app.url, 'POST', '/api/settings', { settings: { darkMode: theme === 'dark' } }, 200);
    const context = await browser.newContext({ viewport: { width: 1280, height: 900 }, bypassCSP: true });
    const page = await context.newPage();
    page.on('dialog', d => d.accept());
    await page.goto(app.url);
    await page.getByText('Pemilik Arman - Admin Muse - Hadir 8/8').waitFor();
    const composer = page.getByRole('textbox', { name: 'Pesan untuk grup' });

    // RoomWorkPanel terbuka.
    await page.locator('.work-toggle').click();
    await page.locator('.work-body').waitFor({ state: 'visible' });
    await axeAudit(page, `roomwork-${theme}`, '.work-panel');

    // QuestionBanner (jawaban pilihan).
    await composer.fill('@Deepseek tanya warna');
    await composer.press('Enter');
    await page.locator('.question-banner').waitFor({ state: 'visible', timeout: 20000 });
    await axeAudit(page, `question-${theme}`, '.question-banner');
    await page.locator('.question-options button').first().click();
    await page.locator('.question-banner').waitFor({ state: 'hidden', timeout: 10000 });

    // Pengaturan: SettingsOmp + SettingsMCP (seluruh form).
    await page.getByRole('button', { name: 'Pengaturan grup', exact: true }).click();
    await page.locator('#settingsForm').waitFor({ state: 'visible' });
    await page.locator('select[aria-label="Kebijakan tool write_file"]').waitFor({ state: 'attached', timeout: 15000 });
    await axeAudit(page, `settings-omp-mcp-${theme}`, '#settingsForm');
    await page.keyboard.press('Escape');
    await page.locator('#settingsForm').waitFor({ state: 'hidden' });

    await context.close();
  }

  // --- Navigasi keyboard panel baru (tema terang) ---
  await api(app.url, 'POST', '/api/settings', { settings: { darkMode: false } }, 200);
  const context = await browser.newContext({ viewport: { width: 1280, height: 900 }, bypassCSP: true });
  const page = await context.newPage();
  page.on('dialog', d => d.accept());
  await page.goto(app.url);
  await page.getByText('Pemilik Arman - Admin Muse - Hadir 8/8').waitFor();
  const composer = page.getByRole('textbox', { name: 'Pesan untuk grup' });

  // Toggle RoomWorkPanel via keyboard (Enter) + aria-expanded berubah.
  await page.locator('.work-toggle').focus();
  await page.keyboard.press('Enter');
  await page.locator('.work-body').waitFor({ state: 'visible' });
  const expanded = await page.locator('.work-toggle').getAttribute('aria-expanded');
  report.check('Enter membuka RoomWorkPanel + aria-expanded=true', expanded === 'true');

  // Tab dari toggle mencapai kontrol di dalam panel (tidak terjebak).
  let reachedPanelControl = false;
  for (let i = 0; i < 12; i++) {
    await page.keyboard.press('Tab');
    const inPanel = await page.evaluate(() => !!document.activeElement?.closest('.work-panel'));
    if (inPanel) { reachedPanelControl = true; break; }
  }
  report.check('Tab mencapai kontrol RoomWorkPanel', reachedPanelControl);

  // Indikator fokus terlihat pada kontrol panel.
  const focusStyle = await page.evaluate(() => {
    const el = document.activeElement;
    const cs = getComputedStyle(el);
    return { outline: cs.outlineStyle, shadow: cs.boxShadow };
  });
  report.check('indikator fokus terlihat di panel', focusStyle.outline !== 'none' || focusStyle.shadow !== 'none',
    JSON.stringify(focusStyle));

  // Escape menutup Pengaturan dan fokus kembali ke tombol pemicu.
  const settingsButton = page.getByRole('button', { name: 'Pengaturan grup', exact: true });
  await settingsButton.focus();
  await page.keyboard.press('Enter');
  await page.locator('#settingsForm').waitFor({ state: 'visible' });
  await page.locator('select[aria-label="Kebijakan tool write_file"]').focus();
  await page.keyboard.press('Escape');
  await page.locator('#settingsForm').waitFor({ state: 'hidden' });
  const focusBack = await page.evaluate(() => document.activeElement?.id ?? document.activeElement?.tagName ?? '');
  report.check('Escape menutup Pengaturan + fokus kembali ke pemicu', focusBack === 'openSettings', `fokus: ${focusBack}`);

  // QuestionBanner: pilihan bisa difokus + Enter mengaktifkan.
  await composer.fill('@Deepseek tanya lagi');
  await composer.press('Enter');
  await page.locator('.question-banner').waitFor({ state: 'visible', timeout: 20000 });
  await page.locator('.question-options button').nth(1).focus();
  await page.keyboard.press('Enter');
  await page.locator('.question-banner').waitFor({ state: 'hidden', timeout: 10000 });
  report.check('Enter pada pilihan QuestionBanner mengirim jawaban', true);

  await context.close();
} catch (error) {
  report.check('a11y selesai tanpa error fatal', false, String(error).slice(0, 300));
} finally {
  await browser.close().catch(() => {});
  await gateway.stop().catch(() => {});
  await app.stop().catch(() => {});
}

process.exit(report.summary() > 0 ? 1 : 0);
