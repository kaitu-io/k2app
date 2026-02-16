#!/usr/bin/env node
// antiblock-keygen.js — Generate AES-256 encryption key for antiblock config

'use strict';

const crypto = require('node:crypto');

function generateKey() {
  return crypto.randomBytes(32).toString('hex');
}

// Main
if (require.main === module) {
  const key = generateKey();
  process.stdout.write(key);
}

module.exports = { generateKey };
