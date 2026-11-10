#!/usr/bin/env node
// Regenerates src/i18n/client-messages.generated.ts. See client-messages-lib.mjs.
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { createAnalyzer, renderGenerated } from './client-messages-lib.mjs';

const webRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..', '..');
const out = path.join(webRoot, 'src', 'i18n', 'client-messages.generated.ts');
fs.writeFileSync(out, renderGenerated(createAnalyzer(webRoot).analyze()));
console.log('wrote', path.relative(webRoot, out));
