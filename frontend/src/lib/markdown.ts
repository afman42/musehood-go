// Parser Markdown subset yang aman untuk teks chat: hasil parse dirender
// sebagai elemen Svelte (bukan HTML mentah), jadi tidak ada permukaan XSS.
// Mendukung paragraf, heading, bold/italic/strike, kode inline & blok,
// blockquote, daftar ul/ol, garis horizontal, dan tautan http/https/mailto.

export type Inline =
  | { type: 'text'; text: string }
  | { type: 'strong' | 'em' | 'del'; children: Inline[] }
  | { type: 'code'; text: string }
  | { type: 'link'; text: string; href: string };

export type Block =
  | { type: 'p'; children: Inline[] }
  | { type: 'h'; level: number; children: Inline[] }
  | { type: 'code'; text: string; lang: string }
  | { type: 'quote'; blocks: Block[] }
  | { type: 'ul' | 'ol'; items: Inline[][] }
  | { type: 'hr' };

const FENCE = /^```(\S*)\s*$/;
const FENCE_CLOSE = /^```\s*$/;
const HEADING = /^(#{1,6})\s+(.*)$/;
const HR = /^\s*(-{3,}|\*{3,}|_{3,})\s*$/;
const QUOTE = /^\s*>/;
const UL_ITEM = /^\s*[-*+]\s+/;
const OL_ITEM = /^\s*\d+[.)]\s+/;

function startsBlock(line: string): boolean {
  return FENCE.test(line) || HEADING.test(line) || HR.test(line) || QUOTE.test(line) || UL_ITEM.test(line) || OL_ITEM.test(line);
}

// Cache hasil parse per teks: bubble yang sama tidak di-parse ulang
// setiap render. Hasil parse diperlakukan sebagai immutable oleh
// perender, jadi aman dipakai bersama.
const parseCache = new Map<string, Block[]>();
const PARSE_CACHE_MAX = 1000;

export function parseMarkdown(text: string): Block[] {
  const hit = parseCache.get(text);
  if (hit) return hit;
  const blocks = parseMarkdownUncached(text);
  if (parseCache.size >= PARSE_CACHE_MAX) parseCache.clear();
  parseCache.set(text, blocks);
  return blocks;
}

function parseMarkdownUncached(text: string): Block[] {
  const lines = text.replace(/\r\n/g, '\n').split('\n');
  const blocks: Block[] = [];
  let i = 0;
  while (i < lines.length) {
    const line = lines[i];
    if (line.trim() === '') { i++; continue; }
    const fence = FENCE.exec(line);
    if (fence) {
      const buf: string[] = [];
      i++;
      while (i < lines.length && !FENCE_CLOSE.test(lines[i])) { buf.push(lines[i]); i++; }
      i++;
      blocks.push({ type: 'code', text: buf.join('\n'), lang: fence[1] });
      continue;
    }
    const heading = HEADING.exec(line);
    if (heading) {
      blocks.push({ type: 'h', level: heading[1].length, children: parseInline(heading[2].trim()) });
      i++;
      continue;
    }
    if (HR.test(line)) { blocks.push({ type: 'hr' }); i++; continue; }
    if (QUOTE.test(line)) {
      const buf: string[] = [];
      while (i < lines.length && QUOTE.test(lines[i])) { buf.push(lines[i].replace(/^\s*>\s?/, '')); i++; }
      blocks.push({ type: 'quote', blocks: parseMarkdown(buf.join('\n')) });
      continue;
    }
    if (UL_ITEM.test(line)) {
      const items: Inline[][] = [];
      while (i < lines.length && UL_ITEM.test(lines[i])) { items.push(parseInline(lines[i].replace(UL_ITEM, ''))); i++; }
      blocks.push({ type: 'ul', items });
      continue;
    }
    if (OL_ITEM.test(line)) {
      const items: Inline[][] = [];
      while (i < lines.length && OL_ITEM.test(lines[i])) { items.push(parseInline(lines[i].replace(OL_ITEM, ''))); i++; }
      blocks.push({ type: 'ol', items });
      continue;
    }
    const buf = [line];
    i++;
    while (i < lines.length && lines[i].trim() !== '' && !startsBlock(lines[i])) { buf.push(lines[i]); i++; }
    blocks.push({ type: 'p', children: parseInline(buf.join('\n')) });
  }
  return blocks;
}

function findEmClose(text: string, from: number): number {
  let idx = text.indexOf('*', from);
  while (idx >= 0) {
    if (text[idx + 1] === '*') { idx = text.indexOf('*', idx + 2); continue; }
    return idx;
  }
  return -1;
}

export function parseInline(text: string): Inline[] {
  const out: Inline[] = [];
  let plain = '';
  const flush = () => { if (plain) { out.push({ type: 'text', text: plain }); plain = ''; } };
  let i = 0;
  while (i < text.length) {
    const ch = text[i];
    if (ch === '\\' && i + 1 < text.length && /[!-/:-@\[-`{-~]/.test(text[i + 1])) {
      plain += text[i + 1];
      i += 2;
      continue;
    }
    if (ch === '`') {
      const end = text.indexOf('`', i + 1);
      if (end > i + 1) { flush(); out.push({ type: 'code', text: text.slice(i + 1, end) }); i = end + 1; continue; }
    }
    if (text.startsWith('***', i)) {
      const end = text.indexOf('***', i + 3);
      if (end > i + 3) {
        flush();
        out.push({ type: 'strong', children: [{ type: 'em', children: parseInline(text.slice(i + 3, end)) }] });
        i = end + 3;
        continue;
      }
    }
    if (text.startsWith('**', i)) {
      const end = text.indexOf('**', i + 2);
      if (end > i + 2) { flush(); out.push({ type: 'strong', children: parseInline(text.slice(i + 2, end)) }); i = end + 2; continue; }
    }
    if (text.startsWith('~~', i)) {
      const end = text.indexOf('~~', i + 2);
      if (end > i + 2) { flush(); out.push({ type: 'del', children: parseInline(text.slice(i + 2, end)) }); i = end + 2; continue; }
    }
    if (ch === '*') {
      const end = findEmClose(text, i + 1);
      if (end > i + 1) { flush(); out.push({ type: 'em', children: parseInline(text.slice(i + 1, end)) }); i = end + 1; continue; }
    }
    if (ch === '_' && (i === 0 || /[\s([{"']/.test(text[i - 1]))) {
      const end = text.indexOf('_', i + 1);
      if (end > i && (end + 1 >= text.length || /[\s.,;:!?)\]}"']/.test(text[end + 1]))) {
        flush();
        out.push({ type: 'em', children: parseInline(text.slice(i + 1, end)) });
        i = end + 1;
        continue;
      }
    }
    if (ch === '[') {
      const link = /^\[([^\]]+)\]\(([^)\s]+)\)/.exec(text.slice(i));
      if (link && /^(https?:\/\/|mailto:)/i.test(link[2])) {
        flush();
        out.push({ type: 'link', text: link[1], href: link[2] });
        i += link[0].length;
        continue;
      }
    }
    plain += ch;
    i++;
  }
  flush();
  return out;
}
