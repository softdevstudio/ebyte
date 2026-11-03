// Builds the Kaitai oracle fixtures: a sample file for each imported gallery
// format, and the values the official Kaitai Struct compiler and JavaScript
// runtime read from it. The Go tests require EByte to read the same values.
//
//   npm install kaitai-struct-compiler@0.11.0 kaitai-struct@0.11.0 js-yaml@4.1.0
//   node generate.cjs
//
// Samples are built here byte by byte from each format's description, not by
// EByte, so the Go test compares two independent readings of the same bytes.
'use strict';
const fs = require('fs');
const path = require('path');
const yaml = require('js-yaml');
const KSC = require('kaitai-struct-compiler');
const KaitaiStream = require('kaitai-struct/KaitaiStream');

class W {
  constructor() { this.b = []; }
  u1(v) { this.b.push(v & 0xff); return this; }
  u2le(v) { return this.u1(v).u1(v >> 8); }
  u2be(v) { return this.u1(v >> 8).u1(v); }
  u4le(v) { return this.u2le(v & 0xffff).u2le(v >>> 16); }
  u4be(v) { return this.u2be(v >>> 16).u2be(v & 0xffff); }
  u8le(v) { const x = BigInt(v); for (let i = 0n; i < 8n; i++) this.u1(Number((x >> (8n * i)) & 0xffn)); return this; }
  u8be(v) { const x = BigInt(v); for (let i = 7n; i >= 0n; i--) this.u1(Number((x >> (8n * i)) & 0xffn)); return this; }
  s2be(v) { return this.u2be(v & 0xffff); }
  s4be(v) { return this.u4be(v >>> 0); }
  s4le(v) { return this.u4le(v >>> 0); }
  s8be(v) { return this.u8be(BigInt.asUintN(64, BigInt(v))); }
  f4le(v) { const d = new DataView(new ArrayBuffer(4)); d.setFloat32(0, v, true); return this.raw(new Uint8Array(d.buffer)); }
  f8be(v) { const d = new DataView(new ArrayBuffer(8)); d.setFloat64(0, v, false); return this.raw(new Uint8Array(d.buffer)); }
  raw(a) { for (const x of a) this.u1(x); return this; }
  text(s, n) { const a = Buffer.from(s, 'utf8'); if (a.length > n) throw new Error('long'); this.raw(a); for (let i = a.length; i < n; i++) this.u1(0); return this; }
  fill(n, f) { for (let i = 0; i < n; i++) this.u1(f(i)); return this; }
  bytes() { return Buffer.from(this.b); }
}

const samples = {
  windows_systemtime: () => new W().u2le(2026).u2le(11).u2le(2).u2le(3).u2le(10).u2le(42).u2le(7).u2le(250),
  android_nanoapp_header: () => new W().u4le(1).raw(Buffer.from('NANO')).u8le(0x1f6f6f676c0123n).u4le(0x10203).u4le(5)
    .u8le(0x0e616e6f417070n).u1(1).u1(4).raw([0, 0, 0, 0, 0, 0]),
  mbr_partition_table: () => {
    const w = new W().fill(0x1be, i => (i * 7) & 0xff);
    const parts = [[0x80, 0x0c, 2048, 1048576], [0, 0x83, 1050624, 4194304], [0, 0x82, 5244928, 524288], [0, 0, 0, 0]];
    for (const [status, type, lba, n] of parts) {
      w.u1(status).u1(1).u1(0x41).u1(0x02).u1(type).u1(0xfe).u1(0xff).u1(0xff).u4le(lba).u4le(n);
    }
    return w.raw([0x55, 0xaa]);
  },
  stl: () => {
    const w = new W().text('EByte oracle sample: two triangles', 80).u4le(2);
    const tri = [[0, 0, 1, 0, 0, 0, 1, 0, 0, 0, 1, 0], [0, 0, -1, 0.5, 0.25, 0, 1.5, 0.1, 0, -2.75, 3, 0]];
    tri.forEach((t, k) => { for (const v of t) w.f4le(v); w.u2le(k * 0x7c00); });
    return w;
  },
  vp8_duck_ivf: () => {
    const w = new W().raw(Buffer.from('DKIF')).u2le(0).u2le(32).raw(Buffer.from('VP80')).u2le(320).u2le(240).u4le(30).u4le(1).u4le(3).u4le(0);
    [[5, 0], [3, 1], [0, 2]].forEach(([n, ts]) => { w.u4le(n).u8le(ts).fill(n, i => 0x9d + i + ts); });
    return w;
  },
  shapefile_index: () => {
    const w = new W().raw([0, 0, 0x27, 0x0a]).fill(20, () => 0).s4be(50 + 4 * 3).raw([0xe8, 3, 0, 0]).s4le(5);
    for (const v of [-10.5, 20.25, 40.125, 60.0625, 0, 0, 0, 0]) w.f8be(v);
    for (const [off, len] of [[50, 20], [74, 36], [114, 4]]) w.s4be(off).s4be(len);
    return w;
  },
  aix_utmp: () => {
    const w = new W();
    [['root', 'co', 'console', 4001, 7, 1793000000, 0, 0, 'localhost'], ['LOGIN', 'l1', 'tty1', 4002, 6, 1793000100, 1, 2, '']].forEach(r => {
      w.text(r[0], 256).text(r[1], 14).text(r[2], 64).u8be(r[3]).s2be(r[4]).s8be(r[5]).s2be(r[6]).s2be(r[7]).text(r[8], 256).s4be(0).fill(8, () => 0).fill(24, () => 0);
    });
    return w;
  },
  glibc_utmp: () => {
    const w = new W();
    [[2, 0, '~', '~~', 'reboot', '6.18.0', 0, 0, 1793000000, 0], [7, 812, 'pts/0', 'ts/0', 'yk', 'éclair.local', 0, 3, 1793000050, 250000]].forEach(r => {
      w.s4le(r[0]).s4le(r[1]).text(r[2], 32).text(r[3], 4).text(r[4], 32).text(r[5], 256).u4le(r[6]).s4le(r[7]).u4le(r[8]).s4le(r[9]).fill(16, i => i === 0 ? 10 : 0).fill(20, () => 0);
    });
    return w;
  },
  monomakh_sapr_chg: () => {
    const w = new W().text('MONOMAKH01', 10);
    [['PROJECT.CFG', 'size=3'], ['SCHEME.DAT', 'abc\n']].forEach(([n, body]) => { w.text(n, 13).u8le(body.length).raw(Buffer.from(body)); });
    return w;
  },
};

// leaves flattens a parsed Kaitai object into its stored values in seq order:
// numbers, booleans and strings as strings, byte arrays as hex.
function leaves(v, out) {
  if (v instanceof Uint8Array) { out.push(Buffer.from(v).toString('hex')); return out; }
  if (Array.isArray(v)) { for (const x of v) leaves(x, out); return out; }
  if (typeof v === 'object' && v !== null) {
    for (const k of Object.keys(v)) if (!k.startsWith('_')) leaves(v[k], out);
    return out;
  }
  out.push(typeof v === 'boolean' ? (v ? '1' : '0') : String(v));
  return out;
}

async function main() {
  const here = __dirname;
  fs.mkdirSync(path.join(here, 'samples'), { recursive: true });
  const expected = {};
  for (const file of fs.readdirSync(path.join(here, 'gallery')).sort()) {
    const id = file.replace(/\.ksy$/, '');
    const ksy = yaml.load(fs.readFileSync(path.join(here, 'gallery', file), 'utf8'));
    const built = await KSC.compile('javascript', ksy, null, false);
    const code = Object.values(built)[0];
    const mod = { exports: {} };
    const req = name => { if (/KaitaiStream$/.test(name)) return KaitaiStream; throw new Error('require ' + name); };
    new Function('module', 'exports', 'require', 'define', code)(mod, mod.exports, req, undefined);
    const Parser = Object.values(mod.exports)[0] || mod.exports;
    const build = samples[id];
    if (!build) throw new Error('no sample for ' + id);
    const w = build();
    const bytes = w instanceof W ? w.bytes() : w;
    fs.writeFileSync(path.join(here, 'samples', id + '.bin'), bytes);
    const parsed = new Parser(new KaitaiStream(new Uint8Array(bytes).buffer));
    if (!parsed._io.isEof()) throw new Error(id + ': Kaitai stopped before the end');
    expected[id] = leaves(parsed, []);
  }
  fs.writeFileSync(path.join(here, 'expected.json'), JSON.stringify(expected, null, 1) + '\n');
}
main().catch(e => { console.error(e); process.exit(1); });
