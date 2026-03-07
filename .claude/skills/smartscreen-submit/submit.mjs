#!/usr/bin/env node
/**
 * SmartScreen submission script — automates everything except CAPTCHA.
 * Usage: node .claude/skills/smartscreen-submit/submit.mjs [version]
 *
 * If version is omitted, reads from package.json.
 * Requires: npx playwright install chromium (first time only)
 *
 * Exit codes:
 *   0 = form filled, paused at CAPTCHA (browser stays open)
 *   1 = error
 */
import { chromium } from 'playwright';
import { readFileSync, statSync, unlinkSync } from 'fs';
import { execFileSync } from 'child_process';
import { resolve, dirname } from 'path';
import { fileURLToPath } from 'url';

const __dirname = dirname(fileURLToPath(import.meta.url));
const PROJECT_ROOT = resolve(__dirname, '../../..');
const USER_DATA_DIR = process.env.PLAYWRIGHT_USER_DATA_DIR || `${process.env.HOME}/.playwright-chrome-profile`;

// Determine version
let version = process.argv[2];
if (!version) {
  const pkg = JSON.parse(readFileSync(resolve(PROJECT_ROOT, 'package.json'), 'utf8'));
  version = pkg.version;
}

const CDN_URL = `https://d0.all7.cc/kaitu/desktop/${version}/Kaitu_${version}_x64-setup.exe`;
const EXE_PATH = resolve(PROJECT_ROOT, `Kaitu_${version}_x64-setup.exe`);

const ADDITIONAL_INFO = `This is our officially signed Windows desktop installer for Kaitu VPN (version ${version}).

- Publisher: Kaitu (https://kaitu.io)
- Signed with: EV/OV code signing certificate
- Download: ${CDN_URL}
- Built via GitHub Actions CI (https://github.com/kaitu-io/k2app)

This is a legitimate VPN application. We are submitting for SmartScreen reputation review as a software developer to ensure our users don't receive false-positive warnings during installation.`;

async function main() {
  // Step 1: Download exe
  console.log(`Downloading ${CDN_URL} ...`);
  try {
    execFileSync('curl', ['-L', '-o', EXE_PATH, CDN_URL], { stdio: 'inherit' });
    const stat = statSync(EXE_PATH);
    if (stat.size === 0) throw new Error('Downloaded file is empty');
    console.log(`Downloaded: ${(stat.size / 1024 / 1024).toFixed(1)} MB`);
  } catch (e) {
    console.error('Download failed:', e.message);
    process.exit(1);
  }

  // Step 2: Launch persistent browser
  console.log('Launching browser...');
  const context = await chromium.launchPersistentContext(USER_DATA_DIR, {
    headless: false,
    viewport: { width: 1280, height: 900 },
    args: ['--disable-blink-features=AutomationControlled'],
  });
  const page = context.pages()[0] || await context.newPage();

  try {
    // Step 3: Navigate to submission page
    console.log('Navigating to SmartScreen submission page...');
    await page.goto('https://www.microsoft.com/en-us/wdsi/filesubmission', { waitUntil: 'networkidle' });
    await page.waitForTimeout(2000);

    // Check login status
    const signInLink = await page.locator('text="Sign in"').count();
    if (signInLink > 0) {
      console.log('\n⚠ Not logged in. Please log in manually in the browser window.');
      console.log('Press Enter here after login is complete...');
      await new Promise(r => process.stdin.once('data', r));
      await page.goto('https://www.microsoft.com/en-us/wdsi/filesubmission', { waitUntil: 'networkidle' });
      await page.waitForTimeout(2000);
    }

    // Step 4: Select "Software developer" and Continue
    console.log('Selecting Software developer...');
    await page.getByText('Software developer', { exact: false }).first().click();
    await page.waitForTimeout(500);
    await page.getByRole('button', { name: 'Continue' }).click();
    await page.waitForTimeout(3000);

    // Check if redirected to login
    if (page.url().includes('login') || page.url().includes('oauth')) {
      console.log('\n⚠ Redirected to login. Please log in manually.');
      console.log('Press Enter here after login is complete...');
      await new Promise(r => process.stdin.once('data', r));
      await page.goto('https://www.microsoft.com/en-us/wdsi/filesubmission', { waitUntil: 'networkidle' });
      await page.waitForTimeout(2000);
      await page.getByText('Software developer', { exact: false }).first().click();
      await page.waitForTimeout(500);
      await page.getByRole('button', { name: 'Continue' }).click();
      await page.waitForTimeout(3000);
    }

    // Step 5: Fill form
    console.log('Filling form...');

    // Security product dropdown (custom combobox)
    const dropdownTrigger = page.locator('button:has-text("Select")').first();
    await dropdownTrigger.click();
    await page.waitForTimeout(1000);
    await page.getByText('Microsoft Defender Smartscreen', { exact: false }).click();
    await page.waitForTimeout(500);

    // Company Name
    await page.getByLabel('Company Name').fill('Kaitu');

    // File upload
    console.log('Uploading exe...');
    const fileInput = page.locator('input[type="file"]');
    await fileInput.setInputFiles(EXE_PATH);
    await page.waitForTimeout(1000);

    // Radio: "Incorrectly detected as malware/malicious"
    await page.getByText('Incorrectly detected as malware/malicious', { exact: false }).click();
    await page.waitForTimeout(500);

    // Detection name
    await page.getByLabel('Detection name').fill('SmartScreen');

    // Additional information
    await page.getByLabel('Additional information').fill(ADDITIONAL_INFO);

    // Click Continue
    console.log('Clicking Continue...');
    await page.getByRole('button', { name: 'Continue' }).last().click();
    await page.waitForTimeout(3000);

    // Step 6: CAPTCHA — pause here
    console.log('\n========================================');
    console.log('FORM FILLED. CAPTCHA PAGE REACHED.');
    console.log('Solve CAPTCHA in browser, then click Submit.');
    console.log('Press Enter here after submission is complete...');
    console.log('========================================\n');

    await new Promise(r => process.stdin.once('data', r));

    console.log('Done!');
  } catch (e) {
    console.error('Error:', e.message);
    console.log('\nBrowser left open for debugging. Press Enter to close...');
    await new Promise(r => process.stdin.once('data', r));
  } finally {
    // Cleanup
    try { unlinkSync(EXE_PATH); } catch {}
    await context.close();
  }
}

main().catch(e => { console.error(e); process.exit(1); });
