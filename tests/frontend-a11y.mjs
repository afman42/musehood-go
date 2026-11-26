// Tes aksesibilitas (a11y) frontend: audit axe-core pada halaman utama dan
// dialog pengaturan, ditambah cek manual (nama aksesibel, alt gambar,
// navigasi keyboard, indikator fokus, kontras warna, target sentuh).
// Jalankan: node tests/frontend-a11y.mjs
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { api, configureMockProvider, launchBrowser, makeReporter, startApp, startGateway } from './frontend-harness.mjs';

const repo = dirname(dirname(fileURLToPath(import.meta.url)));
const axePath = join(repo, 'frontend', 'node_modules', 'axe-core', 'axe.min.js');
const report = makeReporter('FRONTEND A11Y');
const app = await startApp();
const gateway = await startGateway();
const browser = await launchBrowser();

async function axeAudit(page, label) {
  await page.addScriptTag({ path: axePath });
  const results = await page.evaluate(async () => {
    const out = await window.axe.run(document, { resultTypes: ['violations'] });
    return out.violations.map(v => ({ id: v.id, impact: v.impact, nodes: v.nodes.length, help: v.help }));
  });
  const serious = results.filter(v => v.impact === 'critical' || v.impact === 'serious');
  for (const v of results) console.log(`  axe [${label}] ${v.impact}: ${v.id} (${v.nodes} node) — ${v.help}`);
  report.check(`axe ${label}: tanpa pelanggaran critical/serious`, serious.length === 0,
    serious.map(v => v.id).join(', '));
  return results;
}

try {
  await configureMockProvider(app.url, gateway.url);
  // bypassCSP hanya untuk injeksi axe-core pada pengujian.
  const context = await browser.newContext({ viewport: { width: 1280, height: 800 }, bypassCSP: true });
  const page = await context.newPage();
  await page.goto(app.url);
  await page.getByText('Pemilik Arman - Admin Muse - Hadir 8/8').waitFor();

  // Struktur dasar dokumen.
  report.check('atribut lang="id"', (await page.locator('html').getAttribute('lang')) === 'id');
  report.check('judul halaman terisi', (await page.title()).length > 0);

  // Nama aksesibel untuk semua kontrol interaktif yang terlihat.
  const unnamed = await page.evaluate(() => {
    const out = [];
    const els = document.querySelectorAll('button, input, textarea, select, a[href]');
    for (const el of els) {
      const rect = el.getBoundingClientRect();
      if (rect.width === 0 || rect.height === 0) continue;
      const name = (el.getAttribute('aria-label') || el.innerText || el.value || el.placeholder || '').trim();
      const labelled = el.id && document.querySelector(`label[for="${el.id}"]`);
      const wrapped = el.closest('label');
      if (!name && !labelled && !wrapped) out.push(el.tagName + (el.className ? '.' + String(el.className).split(' ')[0] : ''));
    }
    return out;
  });
  report.check('semua kontrol terlihat punya nama aksesibel', unnamed.length === 0, unnamed.slice(0, 5).join(', '));

  // Gambar wajib alt.
  const noAlt = await page.evaluate(() =>
    [...document.querySelectorAll('img')].filter(img => img.getAttribute('alt') === null).length);
  report.check('semua <img> punya atribut alt', noAlt === 0, `${noAlt} tanpa alt`);

  // Navigasi keyboard: Tab mencapai composer; fokus terlihat.
  await page.locator('body').click();
  let reachedComposer = false;
  for (let i = 0; i < 40 && !reachedComposer; i++) {
    await page.keyboard.press('Tab');
    reachedComposer = await page.evaluate(() =>
      document.activeElement?.getAttribute('aria-label') === 'Pesan untuk grup');
  }
  report.check('Tab mencapai textarea pesan', reachedComposer);
  const focusStyle = await page.evaluate(() => {
    const el = document.activeElement;
    const cs = getComputedStyle(el);
    return { outline: cs.outlineStyle, shadow: cs.boxShadow };
  });
  report.check('indikator fokus terlihat (outline/box-shadow)',
    focusStyle.outline !== 'none' || focusStyle.shadow !== 'none', JSON.stringify(focusStyle));

  // Target sentuh tombol aksi pesan >= 24px (WCAG 2.2 AA minimum).
  const composer = page.getByRole('textbox', { name: 'Pesan untuk grup' });
  await composer.fill('@Plamo uji a11y');
  await composer.press('Enter');
  await page.getByText('Balasan polos', { exact: true }).waitFor();
  await page.locator('.message').first().hover();
  const smallTargets = await page.evaluate(() => {
    const out = [];
    for (const el of document.querySelectorAll('.msg-action, .icon-button')) {
      const r = el.getBoundingClientRect();
      if (r.width > 0 && (r.width < 24 || r.height < 24)) out.push(`${Math.round(r.width)}x${Math.round(r.height)}`);
    }
    return out;
  });
  report.check('target sentuh tombol aksi >= 24px', smallTargets.length === 0, smallTargets.slice(0, 3).join(', '));

  // Kontras warna teks utama (rasio WCAG >= 4.5).
  const contrasts = await page.evaluate(() => {
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
    function ratio(el) {
      const fg = parse(getComputedStyle(el).color);
      const bg = bgOf(el);
      const [l1, l2] = [luminance(fg), luminance(bg)].sort((a, b) => b - a);
      return (l1 + 0.05) / (l2 + 0.05);
    }
    const targets = {
      'teks pesan': document.querySelector('.message-text'),
      'nama penulis': document.querySelector('.author-name'),
      'judul grup': document.querySelector('.group-name, h1'),
      'tombol utama': document.querySelector('.primary-button'),
    };
    const out = {};
    for (const [name, el] of Object.entries(targets)) if (el) out[name] = Math.round(ratio(el) * 100) / 100;
    return out;
  });
  for (const [name, ratio] of Object.entries(contrasts)) {
    report.check(`kontras ${name} >= 4.5 (${ratio})`, ratio >= 4.5);
  }

  // Audit axe: halaman utama + dialog pengaturan terbuka.
  await axeAudit(page, 'halaman utama');
  await page.getByRole('button', { name: 'Pengaturan grup', exact: true }).click();
  await page.locator('#settingsForm').waitFor({ state: 'visible' });
  report.check('dialog pengaturan punya label aksesibel',
    (await page.locator('dialog[open]').first().getAttribute('aria-labelledby')) !== null ||
    (await page.locator('dialog[open]').first().getAttribute('aria-label')) !== null);
  await axeAudit(page, 'dialog pengaturan');
  await page.keyboard.press('Escape');
  report.check('Escape menutup dialog', (await page.locator('dialog[open]').count()) === 0);

  // Mode gelap: audit axe + kontras teks utama.
  await api(app.url, 'POST', '/api/settings', { settings: { darkMode: true } }, 200);
  await page.reload();
  await page.getByText('Pemilik Arman - Admin Muse - Hadir 8/8').waitFor();
  report.check('mode gelap aktif (html.dark)', await page.evaluate(() => document.documentElement.classList.contains('dark')));
  await axeAudit(page, 'mode gelap');
  const darkContrasts = await page.evaluate(() => {
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
      return [0, 0, 0, 1];
    }
    function ratio(el) {
      const [l1, l2] = [luminance(parse(getComputedStyle(el).color)), luminance(bgOf(el))].sort((a, b) => b - a);
      return (l1 + 0.05) / (l2 + 0.05);
    }
    const targets = {
      'teks pesan gelap': document.querySelector('.message-text'),
      'judul grup gelap': document.querySelector('.group-name, h1'),
      'subjudul gelap': document.querySelector('#groupSubtitle'),
    };
    const out = {};
    for (const [name, el] of Object.entries(targets)) if (el) out[name] = Math.round(ratio(el) * 100) / 100;
    return out;
  });
  for (const [name, ratio] of Object.entries(darkContrasts)) {
    report.check(`kontras ${name} >= 4.5 (${ratio})`, ratio >= 4.5);
  }
  await api(app.url, 'POST', '/api/settings', { settings: { darkMode: false } }, 200);
} finally {
  await browser.close();
  await gateway.stop();
  await app.stop();
}
process.exit(report.summary() > 0 ? 1 : 0);
