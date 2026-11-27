// Tes aksesibilitas untuk permukaan UI fitur pendalaman 27 Nov 2026:
// layar login PIN, dialog debat, dialog bantuan shortcut, dialog info
// kursi, bagian Keamanan + Log Audit Tool di pengaturan, navigasi
// keyboard (Esc + fokus kembali), aria-live, kontras mode kompak,
// target sentuh pemilih reaksi, dan label form baru.
// Jalankan: node tests/frontend-a11y-depth.mjs
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { api, configureMockProvider, eventually, launchBrowser, makeReporter, startApp, startGateway } from './frontend-harness.mjs';

const repo = dirname(dirname(fileURLToPath(import.meta.url)));
const axePath = join(repo, 'frontend', 'node_modules', 'axe-core', 'axe.min.js');
const report = makeReporter('FRONTEND A11Y DEPTH');
const app = await startApp();
const gateway = await startGateway();
const browser = await launchBrowser();

async function axeAudit(page, label) {
  // Stabilkan rendering sebelum audit: animasi/transisi/scroll halus yang
  // masih berjalan membuat pemeriksaan kontras axe tidak deterministik.
  await page.addStyleTag({ content: '*{animation:none!important;transition:none!important;scroll-behavior:auto!important}' }).catch(() => {});
  await page.waitForTimeout(250);
  await page.addScriptTag({ path: axePath });
  const results = await page.evaluate(async () => {
    const out = await window.axe.run(document, { resultTypes: ['violations'] });
    return out.violations.map(v => ({
      id: v.id, impact: v.impact, nodes: v.nodes.length, help: v.help,
      targets: v.nodes.slice(0, 8).map(n => n.target.join(' ')),
    }));
  });
  const serious = results.filter(v => v.impact === 'critical' || v.impact === 'serious');
  for (const v of results) console.log(`  axe [${label}] ${v.impact}: ${v.id} (${v.nodes} node) — ${v.help} :: ${v.targets.join(' | ')}`);
  report.check(`axe ${label}: tanpa pelanggaran critical/serious`, serious.length === 0,
    serious.map(v => v.id).join(', '));
  return results;
}

async function contrastRatio(page, selector) {
  return page.evaluate((sel) => {
    function luminance([r, g, b]) {
      const f = c => { c /= 255; return c <= 0.03928 ? c / 12.92 : Math.pow((c + 0.055) / 1.055, 2.4); };
      return 0.2126 * f(r) + 0.7152 * f(g) + 0.0722 * f(b);
    }
    function parse(color) {
      const m = color.match(/rgba?\((\d+), (\d+), (\d+)(?:, ([\d.]+))?\)/);
      return m ? [Number(m[1]), Number(m[2]), Number(m[3]), m[4] === undefined ? 1 : Number(m[4])] : null;
    }
    function bgOf(el) {
      let node = el;
      while (node) {
        const c = parse(getComputedStyle(node).backgroundColor);
        if (c && c[3] > 0) return c;
        node = node.parentElement;
      }
      return [255, 255, 255, 1];
    }
    const el = document.querySelector(sel);
    if (!el) return null;
    const [l1, l2] = [luminance(parse(getComputedStyle(el).color)), luminance(bgOf(el))].sort((a, b) => b - a);
    return Math.round(((l1 + 0.05) / (l2 + 0.05)) * 100) / 100;
  }, selector);
}

try {
  await configureMockProvider(app.url, gateway.url, { M01: 'plain', M02: 'sse', M10: 'array' });
  await api(app.url, 'POST', '/api/settings', { settings: { integratorSeat: 'M10' } }, 200);
  const context = await browser.newContext({ viewport: { width: 1280, height: 900 }, bypassCSP: true, reducedMotion: 'reduce' });
  const page = await context.newPage();
  await page.goto(app.url);
  await page.getByText('Pemilik Arman - Admin Muse - Hadir 8/8').waitFor();
  const composer = page.getByRole('textbox', { name: 'Pesan untuk grup' });

  // Pesan untuk dialog info kursi + pemilih reaksi + aria-live.
  await composer.fill('@Plamo uji a11y depth');
  await composer.press('Enter');
  await page.getByText('Balasan polos', { exact: true }).waitFor();

  // aria-live mengumumkan balasan kursi terbaru.
  const liveText = await page.locator('.visually-hidden[aria-live="polite"]').textContent();
  report.check('aria-live mengumumkan balasan kursi', liveText.includes('Plamo') && liveText.includes('Balasan polos'), liveText);

  // Target sentuh pemilih reaksi >= 24px.
  const seatMessage = page.locator('.message').filter({ hasText: 'Balasan polos' }).first();
  await seatMessage.hover();
  await seatMessage.getByRole('button', { name: 'Beri reaksi' }).click();
  await page.locator('.emoji-picker').waitFor({ state: 'visible' });
  const smallEmoji = await page.evaluate(() => {
    const out = [];
    for (const el of document.querySelectorAll('.emoji-picker button, .reaction-pill')) {
      const r = el.getBoundingClientRect();
      if (r.width > 0 && (r.width < 24 || r.height < 24)) out.push(`${Math.round(r.width)}x${Math.round(r.height)}`);
    }
    return out;
  });
  report.check('target sentuh pemilih reaksi >= 24px', smallEmoji.length === 0, smallEmoji.slice(0, 3).join(', '));
  await page.keyboard.press('Escape');

  // Dialog debat: axe + label select + Esc mengembalikan fokus ke pemicu.
  const debateButton = page.locator('#debateButton');
  await debateButton.focus();
  await page.keyboard.press('Enter');
  await page.locator('#debateDialog').waitFor({ state: 'visible' });
  report.check('dialog debat punya aria-labelledby',
    (await page.locator('#debateDialog').getAttribute('aria-labelledby')) === 'debateTitle');
  const proLabel = await page.locator('#debateDialog').getByLabel('Pihak pro').count();
  const topicLabel = await page.locator('#debateDialog').getByLabel('Topik debat').count();
  report.check('select + textarea debat terlabel', proLabel === 1 && topicLabel === 1);
  await axeAudit(page, 'dialog debat');
  await page.keyboard.press('Escape');
  await eventually(async () => (await page.locator('#debateDialog[open]').count()) === 0);
  const focusAfterDebate = await page.evaluate(() => document.activeElement?.id);
  report.check('Esc menutup debat + fokus kembali ke tombol Debat', focusAfterDebate === 'debateButton', String(focusAfterDebate));

  // Dialog bantuan shortcut: axe + Esc.
  await page.locator('header.group-header').click();
  await page.keyboard.press('?');
  await page.locator('#shortcutHelpDialog').waitFor({ state: 'visible' });
  await axeAudit(page, 'dialog bantuan shortcut');
  await page.keyboard.press('Escape');
  await eventually(async () => (await page.locator('#shortcutHelpDialog[open]').count()) === 0);
  report.check('Esc menutup dialog bantuan shortcut', true);

  // Dialog info kursi: axe + Esc mengembalikan fokus ke mention.
  const mention = page.locator('.message-owner .mention-link', { hasText: '@Plamo' }).first();
  await mention.click();
  await page.locator('#seatInfoDialog').waitFor({ state: 'visible' });
  await axeAudit(page, 'dialog info kursi');
  await page.keyboard.press('Escape');
  await eventually(async () => (await page.locator('#seatInfoDialog[open]').count()) === 0);
  const focusAfterInfo = await page.evaluate(() => document.activeElement?.classList?.contains('mention-link'));
  report.check('Esc menutup info kursi + fokus kembali ke mention', focusAfterInfo === true);

  // Pengaturan: bagian Keamanan + Log Audit Tool + label form baru + axe.
  await page.getByRole('button', { name: 'Pengaturan grup', exact: true }).click();
  await page.locator('#settingsForm').waitFor({ state: 'visible' });
  const skillLabel = await page.getByLabel('URL skill').count();
  const personaLabel = await page.getByLabel('Persona M01').count();
  report.check('label form pasang skill + dropdown persona terasosiasi', skillLabel === 1 && personaLabel === 1);
  const securityText = await page.locator('#settingsForm').textContent();
  report.check('bagian Keamanan + Log Audit Tool tampil di pengaturan',
    securityText.includes('Keamanan') && securityText.includes('Log Audit Tool'));
  await axeAudit(page, 'pengaturan (keamanan + log tool)');
  await page.keyboard.press('Escape');

  // Mode kompak: kontras teks pesan tetap >= 4.5.
  await api(app.url, 'POST', '/api/settings', { settings: { density: 'compact' } }, 200);
  await page.reload();
  await page.getByText('Pemilik Arman - Admin Muse - Hadir 8/8').waitFor();
  await eventually(async () => page.evaluate(() => document.documentElement.classList.contains('density-compact')));
  const compactRatio = await contrastRatio(page, '.message-text');
  report.check(`kontras mode kompak >= 4.5 (${compactRatio})`, compactRatio !== null && compactRatio >= 4.5);
  await axeAudit(page, 'mode kompak');
  await api(app.url, 'POST', '/api/settings', { settings: { density: 'normal' } }, 200);

  // Debat: aria-live mengumumkan kesimpulan.
  await page.locator('#debateButton').click();
  const debate = page.locator('#debateDialog');
  await debate.waitFor({ state: 'visible' });
  await debate.locator('select').nth(0).selectOption('M01');
  await debate.locator('select').nth(1).selectOption('M02');
  await debate.locator('textarea').fill('Topik a11y');
  await debate.getByRole('button', { name: 'Mulai Debat' }).click();
  await page.locator('.final-label', { hasText: 'Debat — Kesimpulan' }).waitFor({ timeout: 30000 });
  await eventually(async () => {
    const text = await page.locator('.visually-hidden[aria-live="polite"]').textContent();
    return text.includes('Balasan array');
  });
  report.check('aria-live mengumumkan kesimpulan debat', true);
  await context.close();
} finally {
  await browser.close();
  await gateway.stop();
  await app.stop();
}

// Layar login PIN: axe + label input (app terpisah dengan PIN aktif).
const pinApp = await startApp();
const pinBrowser = await launchBrowser();
try {
  await api(pinApp.url, 'POST', '/api/pin', { pin: '1234', oldPin: '' }, 200);
  const context = await pinBrowser.newContext({ viewport: { width: 1280, height: 900 }, bypassCSP: true });
  const page = await context.newPage();
  await page.goto(pinApp.url);
  await page.locator('.login-card').waitFor({ state: 'visible' });
  const pinLabel = await page.locator('.login-card').getByLabel('PIN', { exact: true }).count();
  report.check('input PIN terlabel', pinLabel === 1);
  await axeAudit(page, 'layar login PIN');
  await context.close();
} finally {
  await pinBrowser.close();
  await pinApp.stop();
}
process.exit(report.summary() > 0 ? 1 : 0);
