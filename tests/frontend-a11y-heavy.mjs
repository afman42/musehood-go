// Tes aksesibilitas berat (27 Nov 2026): axe pada semua dialog/panel baru
// (Uji A/B, Rantai Prompt, Dokumen, Statistik, Log Tool, Debat, banner
// persetujuan) dalam tema terang DAN gelap; navigasi keyboard penuh;
// fokus terlihat; aria-live mengumumkan pesan baru.
// Jalankan: node tests/frontend-a11y-heavy.mjs
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { api, configureMockProvider, eventually, launchBrowser, makeReporter, startApp, startGateway } from './frontend-harness.mjs';

const repo = dirname(dirname(fileURLToPath(import.meta.url)));
const axePath = join(repo, 'frontend', 'node_modules', 'axe-core', 'axe.min.js');
const report = makeReporter('FRONTEND A11Y HEAVY');
const app = await startApp();
const gateway = await startGateway();
const browser = await launchBrowser();

async function axeAudit(page, label) {
  // Nonaktifkan animasi/transisi agar pengukuran kontras stabil (elemen
  // yang sedang fade-in memberi hasil berubah-ubah antar-run).
  await page.addStyleTag({ content: '*{animation:none!important;transition:none!important}' }).catch(() => {});
  await page.waitForTimeout(250);
  await page.addScriptTag({ path: axePath });
  const results = await page.evaluate(async () => {
    const out = await window.axe.run(document, { resultTypes: ['violations'] });
    return out.violations.map(v => ({ id: v.id, impact: v.impact, nodes: v.nodes.length, help: v.help }));
  });
  const serious = results.filter(v => v.impact === 'critical' || v.impact === 'serious');
  for (const v of results) console.log(`  axe [${label}] ${v.impact}: ${v.id} (${v.nodes} node) — ${v.help}`);
  report.check(`axe ${label}: tanpa pelanggaran critical/serious`, serious.length === 0, serious.map(v => v.id).join(', '));
}

try {
  await configureMockProvider(app.url, gateway.url, { M01: 'plain', M02: 'sse', M10: 'array', M04: 'toolcode' });
  await api(app.url, 'POST', '/api/settings', { settings: { integratorSeat: 'M10' } }, 200);
  // Rantai + dokumen agar panel terkait terisi saat diaudit.
  await api(app.url, 'POST', '/api/chains', { name: 'Rantai A11y', steps: [{ seatCode: 'M01', instruction: 'uji' }] }, 201);

  for (const theme of ['light', 'dark']) {
    await api(app.url, 'POST', '/api/settings', { settings: { darkMode: theme === 'dark' } }, 200);
    const context = await browser.newContext({ viewport: { width: 1280, height: 900 }, bypassCSP: true });
    const page = await context.newPage();
    page.on('dialog', d => d.accept());
    await page.goto(app.url);
    await page.getByText('Pemilik Arman - Admin Muse - Hadir 8/8').waitFor();
    const composer = page.getByRole('textbox', { name: 'Pesan untuk grup' });

    // Halaman utama + pesan dengan rating.
    await composer.fill('@Plamo pesan a11y');
    await composer.press('Enter');
    await page.getByText('Balasan polos', { exact: true }).waitFor({ timeout: 15000 });
    await axeAudit(page, `utama-${theme}`);

    // Dialog Uji A/B.
    await page.locator('#abTestButton').click();
    await page.locator('#abTestDialog').waitFor({ state: 'visible' });
    await axeAudit(page, `ab-${theme}`);
    await page.keyboard.press('Escape');
    await page.locator('#abTestDialog').waitFor({ state: 'hidden' });

    // Dialog Debat.
    await page.locator('#debateButton').click();
    await page.locator('#debateDialog').waitFor({ state: 'visible' });
    await axeAudit(page, `debat-${theme}`);
    await page.keyboard.press('Escape');

    // Banner persetujuan (tool berbahaya dari mock toolcode).
    await composer.fill('@Deepseek jalankan kode');
    await composer.press('Enter');
    await page.locator('.approval-banner').waitFor({ state: 'visible', timeout: 20000 });
    await axeAudit(page, `approval-${theme}`);
    await page.locator('.approval-banner').getByRole('button', { name: /Tolak|Batal/ }).first().click();

    // Pengaturan (semua bagian: rantai, dokumen, statistik, log tool, keamanan).
    await page.getByRole('button', { name: 'Pengaturan grup', exact: true }).click();
    await page.locator('#settingsForm').waitFor({ state: 'visible' });
    await page.waitForTimeout(600);
    await axeAudit(page, `pengaturan-${theme}`);
    await page.keyboard.press('Escape');

    await context.close();
  }

  // Navigasi keyboard penuh + fokus + aria-live (tema terang).
  await api(app.url, 'POST', '/api/settings', { settings: { darkMode: false } }, 200);
  const context = await browser.newContext({ viewport: { width: 1280, height: 900 }, bypassCSP: true });
  const page = await context.newPage();
  page.on('dialog', d => d.accept());
  await page.goto(app.url);
  await page.getByText('Pemilik Arman - Admin Muse - Hadir 8/8').waitFor();
  const composer = page.getByRole('textbox', { name: 'Pesan untuk grup' });

  // Tab dari composer bergerak ke kontrol berikutnya (tidak terjebak).
  await composer.focus();
  await page.keyboard.press('Tab');
  const afterTab = await page.evaluate(() => document.activeElement?.id || document.activeElement?.tagName);
  report.check('Tab dari composer berpindah fokus', afterTab !== 'messageInput' && afterTab !== 'BODY', `fokus: ${afterTab}`);

  // Indikator fokus terlihat (outline/box-shadow pada elemen terfokus).
  const focusStyle = await page.evaluate(() => {
    const el = document.activeElement;
    const cs = getComputedStyle(el);
    return { outline: cs.outlineStyle, shadow: cs.boxShadow };
  });
  report.check('indikator fokus terlihat', focusStyle.outline !== 'none' || focusStyle.shadow !== 'none',
    JSON.stringify(focusStyle));

  // Enter mengirim pesan dari keyboard.
  await composer.focus();
  await composer.fill('@Plamo pesan keyboard');
  await composer.press('Enter');
  await page.getByText('Balasan polos', { exact: true }).first().waitFor({ timeout: 15000 });
  report.check('Enter mengirim pesan', true);

  // aria-live mengumumkan balasan baru.
  const liveTexts = await page.locator('[aria-live="polite"]').allTextContents().catch(() => []);
  report.check('aria-live mengumumkan balasan baru', liveTexts.some(t => /Balasan polos/.test(t)), liveTexts.join(' | ').slice(0, 120));

  // Esc menutup dialog dan fokus kembali ke pemicu.
  await page.locator('#abTestButton').focus();
  await page.keyboard.press('Enter');
  await page.locator('#abTestDialog').waitFor({ state: 'visible' });
  await page.keyboard.press('Escape');
  await page.locator('#abTestDialog').waitFor({ state: 'hidden' });
  const focusBack = await page.evaluate(() => document.activeElement?.id);
  report.check('Esc menutup dialog A/B, fokus kembali ke pemicu', focusBack === 'abTestButton', `fokus: ${focusBack}`);

  // Semua tombol rating punya nama aksesibel.
  const ratingNames = await page.evaluate(() =>
    [...document.querySelectorAll('.rating-button')].map(b => b.getAttribute('aria-label') || ''));
  report.check('tombol rating berlabel aksesibel', ratingNames.length > 0 && ratingNames.every(n => n.length > 0));

  await context.close();
} finally {
  await browser.close();
  await gateway.stop();
  await app.stop();
}
process.exit(report.summary() ? 1 : 0);
