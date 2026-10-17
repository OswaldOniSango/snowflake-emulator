import { createRequire } from 'node:module';
import { readFile } from 'node:fs/promises';
import { fileURLToPath } from 'node:url';
const require = createRequire(new URL('../../_web/package.json', import.meta.url));
const { chromium, expect } = require('@playwright/test');
const sql = await readFile(new URL('revenue-overview.sql', import.meta.url), 'utf8');
const worksheets = [
  { id: 'revenue', name: 'Revenue overview.sql', sql },
  { id: 'pipeline', name: 'Streaming pipeline.sql', sql: 'SELECT * FROM MALLARD_DEMO_DB.COMMERCE.PIPELINE_AUDIT;' },
  { id: 'functions', name: 'SQL functions.sql', sql: 'SHOW FUNCTIONS;' },
  { id: 'tasks', name: 'Task orchestration.sql', sql: 'SHOW TASKS;' },
].map(w => ({ ...w, context: { database: 'MALLARD_DEMO_DB', schema: 'COMMERCE' } }));
const browser = await chromium.launch();
try {
  const context = await browser.newContext({ viewport: { width: 1920, height: 1200 }, colorScheme: 'dark', deviceScaleFactor: 1 });
  await context.addInitScript(({ worksheets }) => {
    if (!localStorage.getItem('mallard.workspace')) localStorage.setItem('mallard.workspace', JSON.stringify({ version: 1, worksheets, activeId: 'revenue', openWorksheetIds: worksheets.map(w => w.id) }));
    localStorage.setItem('mallard.theme', 'dark');
    localStorage.setItem('mallard.sidebar-width', '350');
    localStorage.setItem('mallard.dock-ratio', '.38');
  }, { worksheets });
  const page = await context.newPage();
  page.setDefaultTimeout(15000);
  page.on('pageerror', error => console.error(error.message));
  await page.goto(process.env.EMULATOR_URL || 'http://127.0.0.1:8080');
  await page.getByLabel('Username', { exact: true }).fill(process.env.EMULATOR_USER || 'ADMIN');
  await page.getByLabel('Password', { exact: true }).fill(process.env.EMULATOR_PASSWORD || 'admin');
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await expect(page.getByRole('button', { name: 'User menu', exact: true })).toBeVisible();
  await page.getByRole('button', { name: 'Choose role and warehouse' }).click();
  await page.locator('.compute-popover').getByRole('button', { name: /MALLARD_DEMO_WH/ }).click();
  await page.keyboard.press('Escape');
  console.log('Signed in');
  await page.locator('.explorer .node').filter({ hasText: 'MALLARD_DEMO_DB' }).click();
  await page.locator('.explorer .node').filter({ hasText: 'COMMERCE' }).click();
  await expect(page.locator('.object-group-heading').filter({ hasText: 'Tasks' })).toBeVisible();
  await page.locator('.object-group-heading').filter({ hasText: 'Stages' }).click();
  await page.getByRole('button', { name: 'Run current query', exact: true }).click();
  await expect(page.getByText('North America', { exact: true })).toBeVisible();
  await page.mouse.move(1500, 60);
  await page.screenshot({ path: fileURLToPath(new URL('mallard-showcase.png', import.meta.url)), animations: 'disabled' });
  console.log('Saved examples/social-demo/mallard-showcase.png');
} finally { await browser.close(); }
