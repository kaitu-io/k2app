#!/usr/bin/env node
/**
 * Generate a solid-color square PNG without external deps.
 * Usage: node gen-placeholder-icon.mjs <size> <hexcolor> <outfile>
 * e.g.:  node gen-placeholder-icon.mjs 512 673AB7 ../brand-assets/overleap/icon-512x512.png
 */
import fs from 'node:fs';
import path from 'node:path';
import zlib from 'node:zlib';

const [size, hex, out] = process.argv.slice(2);
const n = parseInt(size, 10);
const r = parseInt(hex.slice(0, 2), 16);
const g = parseInt(hex.slice(2, 4), 16);
const b = parseInt(hex.slice(4, 6), 16);

const crcTable = Array.from({ length: 256 }, (_, k) => {
  let c = k;
  for (let i = 0; i < 8; i++) c = c & 1 ? 0xedb88320 ^ (c >>> 1) : c >>> 1;
  return c >>> 0;
});
const crc32 = (buf) => {
  let c = 0xffffffff;
  for (const byte of buf) c = crcTable[(c ^ byte) & 0xff] ^ (c >>> 8);
  return (c ^ 0xffffffff) >>> 0;
};
const chunk = (type, data) => {
  const len = Buffer.alloc(4);
  len.writeUInt32BE(data.length);
  const body = Buffer.concat([Buffer.from(type, 'ascii'), data]);
  const crc = Buffer.alloc(4);
  crc.writeUInt32BE(crc32(body));
  return Buffer.concat([len, body, crc]);
};

const ihdr = Buffer.alloc(13);
ihdr.writeUInt32BE(n, 0);
ihdr.writeUInt32BE(n, 4);
ihdr[8] = 8;  // bit depth
ihdr[9] = 2;  // color type: truecolor
const row = Buffer.concat([Buffer.from([0]), Buffer.alloc(n * 3)]);
for (let x = 0; x < n; x++) {
  row[1 + x * 3] = r;
  row[2 + x * 3] = g;
  row[3 + x * 3] = b;
}
const raw = Buffer.concat(Array.from({ length: n }, () => row));
const png = Buffer.concat([
  Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]),
  chunk('IHDR', ihdr),
  chunk('IDAT', zlib.deflateSync(raw)),
  chunk('IEND', Buffer.alloc(0)),
]);
fs.mkdirSync(path.dirname(out), { recursive: true });
fs.writeFileSync(out, png);
console.log(`wrote ${out} (${n}x${n} #${hex}, ${png.length} bytes)`);
