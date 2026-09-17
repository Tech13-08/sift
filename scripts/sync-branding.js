#!/usr/bin/env node
/**
 * branding/branding.yaml → CSS/JSON/TS/SVG/email PNG.
 * Edit the yaml only; run `make sync-branding`.
 */
const fs = require('fs');
const path = require('path');

const ROOT = path.resolve(__dirname, '..');
const YAML_PATH = path.join(ROOT, 'branding', 'branding.yaml');
const CSS_OUT = path.join(ROOT, 'services/web-service/src/styles/branding.generated.css');
const JSON_OUT = path.join(ROOT, 'services/auth-service/lib/branding.generated.json');
const TS_OUT = path.join(ROOT, 'services/web-service/src/lib/branding.generated.ts');
const SVG_OUT = path.join(ROOT, 'services/web-service/public/sift-logo.svg');
const EMAIL_PNG_AUTH = path.join(ROOT, 'services/auth-service/lib/sift-logo-email.png');
const EMAIL_PNG_WEB = path.join(ROOT, 'services/web-service/public/sift-logo-email.png');
const SOURCE_PNG = path.join(ROOT, 'services/web-service/public/sift-logo.png');

const CSS_KEY_MAP = {
  background: 'background',
  foreground: 'foreground',
  card: 'card',
  card_foreground: 'card-foreground',
  popover: 'popover',
  popover_foreground: 'popover-foreground',
  primary: 'primary',
  primary_foreground: 'primary-foreground',
  secondary: 'secondary',
  secondary_foreground: 'secondary-foreground',
  muted: 'muted',
  muted_foreground: 'muted-foreground',
  accent: 'accent',
  accent_foreground: 'accent-foreground',
  destructive: 'destructive',
  border: 'border',
  input: 'input',
  ring: 'ring',
  amber: 'amber',
  teal: 'teal',
  coral: 'coral',
  chart_1: 'chart-1',
  chart_2: 'chart-2',
  chart_3: 'chart-3',
  chart_4: 'chart-4',
  chart_5: 'chart-5',
  sidebar: 'sidebar',
  sidebar_foreground: 'sidebar-foreground',
  sidebar_primary: 'sidebar-primary',
  sidebar_primary_foreground: 'sidebar-primary-foreground',
  sidebar_accent: 'sidebar-accent',
  sidebar_accent_foreground: 'sidebar-accent-foreground',
  sidebar_border: 'sidebar-border',
  sidebar_ring: 'sidebar-ring',
};

function stripInlineComment(value) {
  let inSingle = false;
  let inDouble = false;
  for (let i = 0; i < value.length; i++) {
    const ch = value[i];
    if (ch === "'" && !inDouble) inSingle = !inSingle;
    else if (ch === '"' && !inSingle) inDouble = !inDouble;
    else if (ch === '#' && !inSingle && !inDouble) {
      return value.slice(0, i).trimEnd();
    }
  }
  return value.trim();
}

function loadSimpleYaml(text) {
  const root = {};
  const stack = [{ indent: -1, obj: root }];
  for (const raw of text.split(/\r?\n/)) {
    const line = raw.replace(/\t/g, '  ');
    if (!line.trim() || line.trim().startsWith('#')) continue;
    const m = line.match(/^(\s*)([^:#]+):\s*(.*?)\s*$/);
    if (!m) throw new Error(`unsupported yaml line: ${raw}`);
    const indent = m[1].length;
    const key = m[2].trim();
    let value = stripInlineComment(m[3]);
    while (stack.length > 1 && indent <= stack[stack.length - 1].indent) {
      stack.pop();
    }
    const parent = stack[stack.length - 1].obj;
    if (value === '') {
      const child = {};
      parent[key] = child;
      stack.push({ indent, obj: child });
      continue;
    }
    if (
      (value.startsWith('"') && value.endsWith('"')) ||
      (value.startsWith("'") && value.endsWith("'"))
    ) {
      const quote = value[0];
      value = value.slice(1, -1);
      if (quote === '"') {
        value = value
          .replace(/\\n/g, '\n')
          .replace(/\\"/g, '"')
          .replace(/\\\\/g, '\\');
      } else {
        value = value.replace(/''/g, "'");
      }
    }
    parent[key] = value;
  }
  return root;
}

function logoSvg(markColor, maskId = 'bowl') {
  const c = markColor;
  return `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 128 100" fill="none">
  <defs>
    <mask id="${maskId}" maskUnits="userSpaceOnUse">
      <path d="M30 58 A34 34 0 0 0 98 58 Z" fill="#fff"/>
      <circle cx="48" cy="70" r="2.4" fill="#000"/>
      <circle cx="58" cy="68" r="1.8" fill="#000"/>
      <circle cx="68" cy="71" r="2.6" fill="#000"/>
      <circle cx="78" cy="69" r="2" fill="#000"/>
      <circle cx="54" cy="78" r="2.2" fill="#000"/>
      <circle cx="64" cy="80" r="1.7" fill="#000"/>
      <circle cx="74" cy="77" r="2.4" fill="#000"/>
      <circle cx="44" cy="82" r="1.6" fill="#000"/>
      <circle cx="84" cy="78" r="1.9" fill="#000"/>
      <circle cx="59" cy="88" r="2" fill="#000"/>
      <circle cx="70" cy="87" r="1.8" fill="#000"/>
      <circle cx="50" cy="90" r="1.5" fill="#000"/>
      <circle cx="79" cy="86" r="1.6" fill="#000"/>
      <circle cx="64" cy="74" r="1.4" fill="#000"/>
      <circle cx="72" cy="73" r="1.5" fill="#000"/>
      <circle cx="56" cy="73" r="1.3" fill="#000"/>
      <circle cx="46" cy="76" r="1.4" fill="#000"/>
      <circle cx="82" cy="73" r="1.5" fill="#000"/>
    </mask>
  </defs>
  <rect x="36" y="6" width="56" height="46" rx="2.5" stroke="${c}" stroke-width="5"/>
  <path d="M38 12 L64 34 L90 12" stroke="${c}" stroke-width="5" stroke-linecap="round" stroke-linejoin="round"/>
  <line x1="6" y1="58" x2="18" y2="58" stroke="${c}" stroke-width="5" stroke-linecap="round"/>
  <line x1="26" y1="58" x2="102" y2="58" stroke="${c}" stroke-width="5" stroke-linecap="round"/>
  <line x1="110" y1="58" x2="122" y2="58" stroke="${c}" stroke-width="5" stroke-linecap="round"/>
  <path d="M30 58 A34 34 0 0 0 98 58 Z" fill="${c}" mask="url(#${maskId})"/>
</svg>
`;
}

function ensureDir(filePath) {
  fs.mkdirSync(path.dirname(filePath), { recursive: true });
}

function writeCss(brand) {
  const colors = brand.colors;
  const lines = [
    '/* GENERATED by scripts/sync-branding.js from branding/branding.yaml - do not edit */',
    ':root {',
  ];
  for (const [key, cssName] of Object.entries(CSS_KEY_MAP)) {
    if (!(key in colors)) throw new Error(`branding.yaml colors missing: ${key}`);
    lines.push(`  --${cssName}: ${colors[key]};`);
  }
  lines.push(`  --radius: ${brand.radius || '0.5rem'};`);
  const atmosphere = brand.atmosphere || {};
  for (const [key, val] of Object.entries(atmosphere)) {
    const cssName = key.replace(/_/g, '-');
    lines.push(`  --atmosphere-${cssName}: ${val};`);
  }
  lines.push('}');
  lines.push('');
  ensureDir(CSS_OUT);
  fs.writeFileSync(CSS_OUT, `${lines.join('\n')}\n`);
}

function writeJson(brand) {
  const mark = brand.email.mark;
  const payload = {
    product: {
      name: brand.product.name,
      tagline: brand.product.tagline || '',
      url_fallback: brand.product.url_fallback || '',
    },
    email: brand.email,
    logo: {
      file_stroke: (brand.logo && brand.logo.file_stroke) || mark,
      inline_svg: logoSvg(mark, 'siftBowlMail').trim(),
      width: 40,
      height: 31,
    },
  };
  ensureDir(JSON_OUT);
  fs.writeFileSync(JSON_OUT, `${JSON.stringify(payload, null, 2)}\n`);
}

function writeTs(brand) {
  const email = brand.email;
  const lines = [
    '/* GENERATED by scripts/sync-branding.js from branding/branding.yaml - do not edit */',
    'export const brand = {',
    `  name: ${JSON.stringify(brand.product.name)} as const,`,
    `  tagline: ${JSON.stringify(brand.product.tagline || '')} as const,`,
    `  urlFallback: ${JSON.stringify(brand.product.url_fallback || '')} as const,`,
    '  email: {',
  ];
  for (const [k, v] of Object.entries(email)) {
    const camel = k.replace(/_([a-z])/g, (_, c) => c.toUpperCase());
    lines.push(`    ${camel}: ${JSON.stringify(v)} as const,`);
  }
  lines.push('  },');
  lines.push('} as const;');
  lines.push('');
  ensureDir(TS_OUT);
  fs.writeFileSync(TS_OUT, `${lines.join('\n')}\n`);
}

function writePublicSvg(brand) {
  const stroke = (brand.logo && brand.logo.file_stroke) || brand.email.mark;
  fs.writeFileSync(SVG_OUT, logoSvg(stroke, 'bowl'));
}

const PNG_CRC_TABLE = (() => {
  const t = new Uint32Array(256);
  for (let n = 0; n < 256; n++) {
    let c = n;
    for (let k = 0; k < 8; k++) c = c & 1 ? 0xedb88320 ^ (c >>> 1) : c >>> 1;
    t[n] = c >>> 0;
  }
  return t;
})();

function pngCrc32(buf) {
  let c = 0xffffffff;
  for (let i = 0; i < buf.length; i++) c = PNG_CRC_TABLE[(c ^ buf[i]) & 0xff] ^ (c >>> 8);
  return (c ^ 0xffffffff) >>> 0;
}

function readPngRgba(buf) {
  if (buf.toString('binary', 0, 8) !== '\x89PNG\r\n\x1a\n') throw new Error('not png');
  let o = 8;
  let w = 0;
  let h = 0;
  let depth = 8;
  let color = 6;
  const idats = [];
  while (o < buf.length) {
    const len = buf.readUInt32BE(o);
    o += 4;
    const type = buf.toString('ascii', o, o + 4);
    o += 4;
    const data = buf.subarray(o, o + len);
    o += len + 4;
    if (type === 'IHDR') {
      w = data.readUInt32BE(0);
      h = data.readUInt32BE(4);
      depth = data[8];
      color = data[9];
    } else if (type === 'IDAT') idats.push(data);
    else if (type === 'IEND') break;
  }
  if (depth !== 8 || color !== 6) {
    throw new Error(`email logo source must be 8-bit RGBA PNG (got depth=${depth} color=${color})`);
  }
  const zlib = require('zlib');
  const inflated = zlib.inflateSync(Buffer.concat(idats));
  const stride = w * 4;
  const rgba = Buffer.alloc(w * h * 4);
  let ip = 0;
  let op = 0;
  const prior = Buffer.alloc(stride);
  for (let y = 0; y < h; y++) {
    const filter = inflated[ip++];
    const row = inflated.subarray(ip, ip + stride);
    ip += stride;
    const out = Buffer.alloc(stride);
    for (let i = 0; i < stride; i++) {
      const x = row[i];
      const a = i >= 4 ? out[i - 4] : 0;
      const b = prior[i];
      const c = i >= 4 ? prior[i - 4] : 0;
      let v;
      if (filter === 0) v = x;
      else if (filter === 1) v = (x + a) & 255;
      else if (filter === 2) v = (x + b) & 255;
      else if (filter === 3) v = (x + Math.floor((a + b) / 2)) & 255;
      else if (filter === 4) {
        const p = a + b - c;
        const pa = Math.abs(p - a);
        const pb = Math.abs(p - b);
        const pc = Math.abs(p - c);
        v = (x + (pa <= pb && pa <= pc ? a : pb <= pc ? b : c)) & 255;
      } else throw new Error(`unsupported png filter ${filter}`);
      out[i] = v;
    }
    out.copy(rgba, op);
    op += stride;
    out.copy(prior, 0);
  }
  return { w, h, rgba };
}

function writePngRgba(w, h, rgba) {
  const zlib = require('zlib');
  const stride = w * 4;
  const raw = Buffer.alloc((stride + 1) * h);
  for (let y = 0; y < h; y++) {
    raw[y * (stride + 1)] = 0;
    rgba.copy(raw, y * (stride + 1) + 1, y * stride, (y + 1) * stride);
  }
  const compressed = zlib.deflateSync(raw, { level: 9 });
  function chunk(type, data) {
    const len = Buffer.alloc(4);
    len.writeUInt32BE(data.length);
    const t = Buffer.from(type);
    const crc = Buffer.alloc(4);
    crc.writeUInt32BE(pngCrc32(Buffer.concat([t, data])));
    return Buffer.concat([len, t, data, crc]);
  }
  const ihdr = Buffer.alloc(13);
  ihdr.writeUInt32BE(w, 0);
  ihdr.writeUInt32BE(h, 4);
  ihdr[8] = 8;
  ihdr[9] = 6;
  return Buffer.concat([
    Buffer.from([137, 80, 78, 71, 13, 10, 26, 10]),
    chunk('IHDR', ihdr),
    chunk('IDAT', compressed),
    chunk('IEND', Buffer.alloc(0)),
  ]);
}

function parseHexRgb(hex, fallback = [0xf7, 0xf3, 0xea]) {
  const m = String(hex || '').trim().match(/^#?([0-9a-fA-F]{6})$/);
  if (!m) return fallback;
  const n = parseInt(m[1], 16);
  return [(n >> 16) & 255, (n >> 8) & 255, n & 255];
}


function writeEmailPng(brand) {
  if (!fs.existsSync(SOURCE_PNG)) {
    console.warn('skip email png: missing', path.relative(ROOT, SOURCE_PNG));
    return;
  }
  const src = readPngRgba(fs.readFileSync(SOURCE_PNG));
  const mark = parseHexRgb(brand.email.mark);
  const recolored = Buffer.alloc(src.rgba.length);
  for (let i = 0; i < src.rgba.length; i += 4) {
    const lum = (src.rgba[i] + src.rgba[i + 1] + src.rgba[i + 2]) / 3;
    const ink = Math.max(0, Math.min(1, (255 - lum) / 255));
    recolored[i] = mark[0];
    recolored[i + 1] = mark[1];
    recolored[i + 2] = mark[2];
    recolored[i + 3] = Math.round(src.rgba[i + 3] * ink);
  }
  const tw = 160;
  const th = Math.max(1, Math.round((tw / src.w) * src.h));
  const scaled = Buffer.alloc(tw * th * 4);
  for (let y = 0; y < th; y++) {
    for (let x = 0; x < tw; x++) {
      const sx = Math.min(src.w - 1, Math.floor(((x + 0.5) * src.w) / tw));
      const sy = Math.min(src.h - 1, Math.floor(((y + 0.5) * src.h) / th));
      const si = (sy * src.w + sx) * 4;
      const di = (y * tw + x) * 4;
      scaled[di] = recolored[si];
      scaled[di + 1] = recolored[si + 1];
      scaled[di + 2] = recolored[si + 2];
      scaled[di + 3] = recolored[si + 3];
    }
  }
  const png = writePngRgba(tw, th, scaled);
  ensureDir(EMAIL_PNG_AUTH);
  ensureDir(EMAIL_PNG_WEB);
  fs.writeFileSync(EMAIL_PNG_AUTH, png);
  fs.writeFileSync(EMAIL_PNG_WEB, png);
}

function main() {
  if (!fs.existsSync(YAML_PATH)) throw new Error(`missing ${YAML_PATH}`);
  const brand = loadSimpleYaml(fs.readFileSync(YAML_PATH, 'utf8'));
  for (const section of ['product', 'colors', 'email']) {
    if (!brand[section]) throw new Error(`branding.yaml missing section: ${section}`);
  }
  writeCss(brand);
  writeJson(brand);
  writeTs(brand);
  writePublicSvg(brand);
  writeEmailPng(brand);
  console.log('Wrote', path.relative(ROOT, CSS_OUT));
  console.log('Wrote', path.relative(ROOT, JSON_OUT));
  console.log('Wrote', path.relative(ROOT, TS_OUT));
  console.log('Wrote', path.relative(ROOT, SVG_OUT));
  if (fs.existsSync(EMAIL_PNG_AUTH)) {
    console.log('Wrote', path.relative(ROOT, EMAIL_PNG_AUTH));
    console.log('Wrote', path.relative(ROOT, EMAIL_PNG_WEB));
  }
}

main();
