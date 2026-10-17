import { readFile, writeFile } from 'node:fs/promises';
import { fileURLToPath } from 'node:url';
import { splitStatements } from '../../_web/src/statements.ts';

const base = (process.env.EMULATOR_URL || 'http://127.0.0.1:8080').replace(/\/$/, '');
const root = new URL('./', import.meta.url);
const login = await fetch(`${base}/session/v1/login-request`, {
  method: 'POST', headers: { 'Content-Type': 'application/json' },
  body: JSON.stringify({ data: { LOGIN_NAME: process.env.EMULATOR_USER || 'ADMIN', PASSWORD: process.env.EMULATOR_PASSWORD || 'admin' } }),
}).then(r => r.json());
if (!login.success || !login.data?.token) throw new Error(login.message || 'Sign-in failed');
const headers = { 'Content-Type': 'application/json', Authorization: `Bearer ${login.data.token}` };
const info = login.data.sessionInfo;
const context = { database: info.databaseName, schema: info.schemaName, role: info.roleName };
async function execute(statement) {
  let result = await fetch(`${base}/api/v2/statements`, { method: 'POST', headers, body: JSON.stringify({ ...context, statement }) }).then(r => r.json());
  const handle = result.statementHandle;
  for (let i = 0; result.code === '333334' && i < 240; i++) {
    await new Promise(resolve => setTimeout(resolve, 250));
    result = await fetch(`${base}/api/v2/statements/${handle}`, { headers }).then(r => r.json());
  }
  if (result.sqlState !== '00000') throw new Error(result.message || `Statement failed: ${result.code}`);
  return result;
}
const databases = await fetch(`${base}/api/v2/databases`, { headers }).then(r => r.json());
const resumeFrom = Number(process.argv.find(arg => arg.startsWith('--resume-from='))?.split('=')[1] || 1);
if (!Number.isInteger(resumeFrom) || resumeFrom < 1) throw new Error('Invalid --resume-from');
const exists = (Array.isArray(databases) ? databases : []).some(db => db.name === 'MALLARD_DEMO_DB');
if (resumeFrom > 1 && !exists) throw new Error('Cannot resume: demo database does not exist');
if (exists && resumeFrom === 1) {
  throw new Error('MALLARD_DEMO_DB already exists. Demo was not modified. Inspect it before choosing to remove or reuse it.');
}
const statements = splitStatements(await readFile(new URL('bootstrap.sql', root), 'utf8'));
if (resumeFrom > statements.length) throw new Error('Resume index exceeds statement count');
if (resumeFrom > 1) Object.assign(context, { database: 'MALLARD_DEMO_DB', schema: 'COMMERCE', warehouse: 'MALLARD_DEMO_WH' });
for (let i = resumeFrom - 1; i < statements.length; i++) {
  const sql = statements[i].text;
  try { await execute(sql); } catch (error) { throw new Error(`Statement ${i + 1}: ${error.message}\n${sql}`, { cause: error }); }
  if (/CREATE DATABASE MALLARD_DEMO_DB/i.test(sql)) context.database = 'MALLARD_DEMO_DB';
  if (/CREATE WAREHOUSE MALLARD_DEMO_WH/i.test(sql)) context.warehouse = 'MALLARD_DEMO_WH';
  if (/CREATE SCHEMA MALLARD_DEMO_DB.COMMERCE/i.test(sql)) context.schema = 'COMMERCE';
  console.log(`${String(i + 1).padStart(2, '0')}/${statements.length} OK`);
}
const hero = await execute(await readFile(new URL('revenue-overview.sql', root), 'utf8'));
const counts = await execute('SELECT (SELECT COUNT(*) FROM MALLARD_DEMO_DB.COMMERCE.ORDERS) AS source_orders, (SELECT COUNT(*) FROM MALLARD_DEMO_DB.COMMERCE.PROCESSED_ORDERS) AS processed_orders, (SELECT COUNT(*) FROM MALLARD_DEMO_DB.COMMERCE.ORDER_CHANGES) AS pending_changes;');
const functions = await execute('SHOW FUNCTIONS;');
const tasks = await execute('SHOW TASKS;');
if (JSON.stringify(counts.data) !== '[[12,12,0]]') throw new Error(`Unexpected pipeline counts: ${JSON.stringify(counts.data)}`);
const containsNames = (result, names) => names.every(name => result.data?.some(row => row.includes(name)));
if (hero.data?.length !== 6 || !containsNames(functions, ['NET_REVENUE', 'ORDER_SEGMENT', 'REVENUE_BAND']) || !containsNames(tasks, ['INGEST_ORDERS_TASK', 'DATA_QUALITY_TASK', 'PIPELINE_HEARTBEAT_TASK'])) throw new Error('Unexpected demo object or result counts');
const report = { database: context.database, schema: context.schema, warehouse: context.warehouse, statements: statements.length, pipelineCounts: counts.data, functions: functions.data.length, tasks: tasks.data.length, result: hero.data };
await writeFile(new URL('verification.json', root), JSON.stringify(report, null, 2) + '\n');
console.log(`Verified demo. Report: ${fileURLToPath(new URL('verification.json', root))}`);
