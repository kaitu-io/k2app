import { createCanvas, GlobalFonts } from '@napi-rs/canvas';
import { writeFileSync } from 'fs';
import { resolve, dirname } from 'path';
import { fileURLToPath } from 'url';

// Register system Chinese font (macOS PingFang)
GlobalFonts.registerFromPath('/System/Library/Fonts/PingFang.ttc', 'PingFang SC');

const __dirname = dirname(fileURLToPath(import.meta.url));

const WIDTH = 1200;
const HEIGHT = 630;

const canvas = createCanvas(WIDTH, HEIGHT);
const ctx = canvas.getContext('2d');

// Background
ctx.fillStyle = '#050508';
ctx.fillRect(0, 0, WIDTH, HEIGHT);

// Subtle green glow line at 60% height
const glowY = HEIGHT * 0.6;
const gradient = ctx.createRadialGradient(WIDTH / 2, glowY, 0, WIDTH / 2, glowY, 300);
gradient.addColorStop(0, 'rgba(0, 255, 136, 0.08)');
gradient.addColorStop(1, 'rgba(0, 255, 136, 0)');
ctx.fillStyle = gradient;
ctx.fillRect(0, glowY - 300, WIDTH, 600);

// Thin green pulse line
ctx.strokeStyle = 'rgba(0, 255, 136, 0.3)';
ctx.lineWidth = 1.5;
ctx.beginPath();
ctx.moveTo(0, glowY);
ctx.lineTo(WIDTH, glowY);
ctx.stroke();

// Main title — Chinese slogan
ctx.fillStyle = '#ffffff';
ctx.font = 'bold 72px "PingFang SC", sans-serif';
ctx.textAlign = 'center';
ctx.textBaseline = 'middle';
ctx.fillText('别人断线，你满速。', WIDTH / 2, HEIGHT * 0.30);

// Secondary — English slogan
ctx.fillStyle = '#00ff88';
ctx.font = 'bold 40px monospace';
ctx.fillText("Others Drop. You Don't.", WIDTH / 2, HEIGHT * 0.44);

// Tagline
ctx.fillStyle = '#9ca3af';
ctx.font = '24px "PingFang SC", sans-serif';
ctx.fillText('k2cc — 越拥堵，越从容', WIDTH / 2, HEIGHT * 0.75);

// Domain
ctx.fillStyle = '#6b7280';
ctx.font = '20px monospace';
ctx.fillText('kaitu.io', WIDTH / 2, HEIGHT * 0.88);

// Save
const buffer = canvas.toBuffer('image/png');
const outputPath = resolve(__dirname, '../public/images/og-default.png');
writeFileSync(outputPath, buffer);
console.log(`OG image generated: ${outputPath} (${buffer.length} bytes)`);
