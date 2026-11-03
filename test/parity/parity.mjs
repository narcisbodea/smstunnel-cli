// Parity check: the same scenarios through the Node.js version (npm "smstunnel") and this Go version, compared
// on exit code, stdout, stderr and the requests SMSTunnel received. Ids, times, ports and temporary paths are
// normalised; nothing else is.
//
//   SMSTUNNEL_NODE_CLI=../smstunnel-cli SMSTUNNEL_GO_CLI=build/smstunnel.exe node test/parity/parity.mjs
//
// It runs against the Node version's own mock API (test/e2e/mock-server.mjs, one fresh instance per CLI), and,
// when SMSTUNNEL_E2E_KEY_FILE (+ SMSTUNNEL_E2E_KEY_NAME) points at a local SMSTunnel API, against that too
// (never smstunnel.io). It writes test/parity/results/report.md and exits 1 on any difference.
import { spawn } from 'node:child_process';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { createRequire } from 'node:module';
import { fileURLToPath, pathToFileURL } from 'node:url';

const here = path.dirname(fileURLToPath(import.meta.url));
const repo = path.resolve(here, '..', '..');
const nodeCli = path.resolve(process.env.SMSTUNNEL_NODE_CLI || path.join(repo, '..', 'smstunnel-cli'));
const goCli = path.resolve(process.env.SMSTUNNEL_GO_CLI || path.join(repo, 'build', process.platform === 'win32' ? 'smstunnel.exe' : 'smstunnel'));
const { startMockServer, KEYS, PHONES, SPECIAL, IDEMPOTENCY } = await import(pathToFileURL(path.join(nodeCli, 'test', 'e2e', 'mock-server.mjs')));

const CLIS = {
  node: { cmd: process.execPath, args: [path.join(nodeCli, 'dist', 'cli.js')] },
  go: { cmd: goCli, args: [] },
};

// Every run gets SMSTUNNEL_API_BASE pointing at a closed local port unless a scenario sets it or must read the
// address saved by login (guard: false): a scenario that loses its --api-base can never reach smstunnel.io.
const GUARD_API_BASE = 'http://127.0.0.1:9/api/v1';

function cleanEnv(extra = {}, guard = true) {
  const env = { ...process.env };
  for (const name of Object.keys(env)) if (name.toUpperCase().startsWith('SMSTUNNEL_')) delete env[name];
  if (guard) env.SMSTUNNEL_API_BASE = GUARD_API_BASE;
  for (const [name, value] of Object.entries(extra)) {
    if (value === undefined) delete env[name];
    else env[name] = value;
  }
  return env;
}

function run(cli, args, { env, input, timeoutMs = 60_000, onOutput } = {}) {
  return new Promise((resolve, reject) => {
    const child = spawn(CLIS[cli].cmd, [...CLIS[cli].args, ...args], { env, stdio: ['pipe', 'pipe', 'pipe'], windowsHide: true });
    let stdout = '';
    let stderr = '';
    child.stdout.setEncoding('utf8');
    child.stderr.setEncoding('utf8');
    child.stdout.on('data', (c) => {
      stdout += c;
      onOutput?.({ stdout, stderr }, child);
    });
    child.stderr.on('data', (c) => {
      stderr += c;
      onOutput?.({ stdout, stderr }, child);
    });
    const timer = setTimeout(() => child.kill(), timeoutMs);
    child.on('error', reject);
    child.on('close', (code, signal) => {
      clearTimeout(timer);
      resolve({ code, signal, stdout, stderr });
    });
    if (input !== undefined) child.stdin.write(input);
    child.stdin.end();
  });
}

function normalize(text, ctx) {
  let t = text;
  for (const [from, to] of ctx.replacements) {
    t = t.split(from).join(to);
    t = t.split(JSON.stringify(from).slice(1, -1)).join(to); // the same path inside JSON
  }
  if (ctx.counts) t = t.replace(/\b\d+ used\b/g, '<n> used').replace(/("(?:smsClean|smsBranded|twoFaClean|smsCleanMonth)": )\d+/g, '$1<n>');
  return t
    .replace(/\n {4}at [^\n]*/g, '') // Node.js stack traces
    .replace(/\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d+)?Z/g, '<iso>')
    .replace(/\d{4}-\d{2}-\d{2}T\d{2}%3A\d{2}%3A\d{2}\.\d{3}Z/g, '<iso>')
    .replace(/\d{4}-\d{2}-\d{2} \d{2}:\d{2}/g, '<local time>')
    .replace(/\b[0-9a-f]{24}\b/g, (id) => (ctx.keepIds.has(id) ? id : '<id>'))
    .replace(/127\.0\.0\.1:\d+/g, '127.0.0.1:<port>');
}

const results = [];
const differences = [];

function compare(group, name, a, b, ctxA, ctxB, extra = {}) {
  const fields = {
    exit: [String(a.code), String(b.code)],
    stdout: [normalize(a.stdout, ctxA), normalize(b.stdout, ctxB)],
    stderr: [normalize(a.stderr, ctxA), normalize(b.stderr, ctxB)],
    ...extra,
  };
  if (extra.skipExit) delete fields.exit;
  delete fields.skipExit;
  const diff = Object.entries(fields).filter(([, [x, y]]) => x !== y);
  results.push({ group, name, same: diff.length === 0 });
  if (diff.length) differences.push({ group, name, diff });
}

// ---- against the Node version's mock API ----

async function mockScenarios(cli) {
  const mock = await startMockServer();
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), `smstunnel-parity-${cli}-`));
  const configDir = path.join(dir, 'config');
  const base = { SMSTUNNEL_CONFIG_DIR: configDir, SMSTUNNEL_KEY_STORE: 'file', SMSTUNNEL_POLL_INTERVAL_MS: '100' };
  const out = [];
  const record = async (name, args, { key = KEYS.full, env = {}, input, apiBase = true, watch, guard = true } = {}) => {
    const before = mock.state.requests.length;
    const allArgs = apiBase ? ['--api-base', mock.apiBase, ...args] : args;
    const result = await run(cli, allArgs, {
      env: cleanEnv({ ...base, SMSTUNNEL_API_KEY: key === null ? undefined : key, ...env }, guard),
      input,
      ...(watch ? watch(mock) : {}),
    });
    const requests = mock.state.requests.slice(before).map((r) => ({
      method: r.method, url: r.url, body: r.body === undefined ? undefined : JSON.stringify(r.body),
      idempotencyKey: r.headers['idempotency-key'], accept: r.headers.accept, contentType: r.headers['content-type'],
      apiKey: r.headers['x-api-key'] === undefined ? undefined : `${r.headers['x-api-key'].slice(0, 8)}…`,
      authorization: r.headers.authorization, encoding: r.headers['accept-encoding'],
      ua: (r.headers['user-agent'] || '').replace(/\(.*$/, '('),
    }));
    out.push({ name, result, requests, replacements: [[mock.apiBase, '<api>'], [configDir, '<config>'], [dir, '<dir>']] });
  };

  // Arguments, help and dispatch.
  for (const args of [['--version'], ['-v'], ['--help'], ['-h'], ['send', '--help'], ['help', 'code'], ['help', 'nope'], ['help'], [],
    ['blast'], ['blast', '--help'], ['phones', '--loud'], ['send', '--wait=yes'], ['--json=x'], ['phones', '--wait', '--help'],
    ['send', '+40712345678', '-v'], ['phones', '--since', '1h'], ['phones', 'extra'], ['', 'x'], ['phones', '--', '--json']]) {
    await record(`args ${JSON.stringify(args)}`, args, { apiBase: false });
  }
  await record('no key', ['phones'], { key: null });
  await record('no key --json', ['phones', '--json'], { key: null });
  await record('login refused', ['login'], { key: null, input: `${KEYS.refused}\n` });
  await record('login with an argument', ['login', KEYS.full], { key: null });
  await record('login empty stdin', ['login'], { key: null, input: '' });
  await record('login blank lines', ['login'], { key: null, input: '\n  \n' });
  await record('login bad key', ['login'], { key: null, input: 'short\n' });
  await record('login bad key spaces', ['login'], { key: null, input: '  sk live x  \n' });
  await record('login', ['login'], { key: null, input: `\n   ${KEYS.full}   \nignored\n` });
  out.push({ name: 'config.json after login', file: fs.readFileSync(path.join(configDir, 'config.json'), 'utf8') });
  await record('phones with the saved key and address', ['phones'], { key: null, apiBase: false, guard: false });
  await record('SMSTUNNEL_API_KEY wins', ['phones'], { key: KEYS.refused, apiBase: false, guard: false });
  await record('login again --json, with SMSTUNNEL_API_KEY set', ['login', '--json'], { key: KEYS.sendOnly, input: `${KEYS.full}\n` });
  await record('logout --json, with SMSTUNNEL_API_KEY set', ['logout', '--json'], { key: KEYS.sendOnly, apiBase: false });
  await record('logout again', ['logout'], { key: null, apiBase: false });
  await record('phones after logout', ['phones'], { key: null, apiBase: false });

  await record('phones', ['phones']);
  await record('phones --json', ['phones', '--json']);
  await record('send', ['send', '+40712345678', 'Your', 'order', 'is', 'ready']);
  await record('send --json', ['send', '+40712345678', 'JSON text', '--json']);
  await record('send by name and country', ['send', '0712345678', 'Hi', '--phone', 'desk PHONE', '--country', '+40']);
  await record('send by id', ['send', '+40712345678', 'Hi', '--phone', PHONES[2].id]);
  await record('send country with spaces', ['send', '0712345678', 'Hi', '--country', ' 0 0 4 0 ']);
  await record('send long text (parts)', ['send', '+40712345678', 'x'.repeat(161)]);
  await record('send UCS-2 parts', ['send', '+40712345678', 'ș'.repeat(71)]);
  await record('send 1600 code points of emoji', ['send', '+40712345678', '😀'.repeat(1600)]);
  await record('send branded', ['send', '+40712345678', 'BRANDED text']);
  await record('send branded --wait', ['send', '+40712345678', 'BRANDED wait', '--wait']);
  await record('send words with dashes', ['send', '+40712345678', '-20%', 'today', '-', 'only']);
  await record('send after --', ['send', '+40712345678', '--', '--wait', 'is', 'text']);
  await record('send quotes and unicode', ['send', '+40712345678', 'say "hi" — ăîșț \\ / <b>&amp;</b>   end']);
  for (const [name, args] of [
    ['list comma', ['send', '+40712345678,+40722333444', 'Hi']], ['list semicolon', ['send', '+40712345678;+40722333444', 'Hi']],
    ['two plus', ['send', '+40712345678 +40722333444', 'Hi']], ['two numbers', ['send', '0712345678 0722333444', 'Hi']],
    ['letters', ['send', 'mom', 'Hi']], ['twin', ['send', '+40712345678', 'Hi', '--phone', 'Twin']],
    ['kitchen', ['send', '+40712345678', 'Hi', '--phone', 'Kitchen']], ['country RO', ['send', '+40712345678', 'Hi', '--country', 'RO']],
    ['1601', ['send', '+40712345678', 'x'.repeat(1601)]], ['no text', ['send', '+40712345678']],
    ['timeout without wait', ['send', '+40712345678', 'Hi', '--timeout', '5']], ['bad timeout', ['send', '+40712345678', 'Hi', '--timeout', '0', '--wait']],
    ['no number', ['send']], ['plus inside', ['send', '0721+123456', 'Hi']], ['short number', ['send', '12345', 'Hi']],
    ['phones --wait', ['phones', '--wait']], ['blank text', ['send', '+40712345678', '   ']],
  ]) {
    await record(`refused: ${name}`, args);
  }
  await record('stdin text', ['send', '+40712345678'], { input: 'Backup finished\n' });
  await record('stdin dash', ['send', '+40712345678', '-'], { input: 'Line one\r\nLine two\n\n' });
  await record('stdin too long', ['send', '+40712345678', '-'], { input: 'x'.repeat(64 * 1024 + 1) });
  await record('stdin exactly 64 KiB of text (too long a text)', ['send', '+40712345678', '-'], { input: 'x'.repeat(64 * 1024) });
  await record('stdin invalid UTF-8', ['send', '+40712345678', '-'], { input: Buffer.from([0x48, 0xe2, 0x82, 0x41, 0xff, 0x0a]) });
  await record('wait sent', ['send', '+40712345678', 'See you at 10', '--wait']);
  await record('wait --json', ['send', '+40712345678', 'JSON please', '--wait', '--json']);
  await record('wait failed', ['send', '+40712345678', 'FAILME please', '--wait']);
  await record('wait failed --json', ['send', '+40712345678', 'FAILME json', '--wait', '--json']);
  await record('wait unconfirmed', ['send', '+40712345678', 'UNCONFIRMED text', '--wait']);
  await record('wait 429 once', ['send', '+40712345678', 'STATUS429ONCE text', '--wait']);
  await record('wait 403', ['send', '+40712345678', 'STATUS403 text', '--wait']);
  await record('wait 403 --json', ['send', '+40712345678', 'STATUS403 json', '--wait', '--json']);
  await record('queued', ['send', '+40712345678', 'Hi', '--phone', 'Spare phone']);
  await record('queued --json', ['send', '+40712345678', 'Hi', '--phone', 'Spare phone', '--json']);
  await record('queued --wait --timeout 1', ['send', '+40712345678', 'Hi', '--phone', 'Spare phone', '--wait', '--timeout', '1']);
  await record('queued --wait --timeout 0.5s --json', ['send', '+40712345678', 'Hi', '--phone', 'Spare phone', '--wait', '--timeout', '0.5s', '--json']);
  for (const [name, number] of Object.entries(SPECIAL)) {
    await record(`special ${name}`, ['send', number, 'Hi']);
    await record(`special ${name} --json`, ['send', number, 'Hi', '--json']);
  }
  await record('refused key --json', ['phones', '--json'], { key: KEYS.refused });
  await record('code without a code', ['code', '+40712345678', 'Hello there']);
  await record('code too long', ['code', '+40712345678', `Code 482913 ${'x'.repeat(320)}`]);
  await record('code --wait', ['code', '+40712345678', 'Your', 'sign-in', 'code', 'is', '482913', '--wait']);
  await record('code 123-456', ['code', '+40712345678', 'Code: 123-456']);
  await record('code blank', ['code', '+40712345678', ' ']);
  await record('status', ['status', '000000000000000000000001']);
  await record('status --json', ['status', '000000000000000000000001', '--json']);
  await record('status unknown', ['status', 'ffffffffffffffffffffffff']);
  await record('status malformed', ['status', 'abc']);
  await record('status two ids', ['status', 'a', 'b']);
  await record('status uppercase padded', ['status', '  000000000000000000000001  ']);
  await record('received', ['received']);
  await record('received --json', ['received', '--json']);
  await record('received --since --from --limit', ['received', '--since', '1h', '--from', ' +40722222222 ', '--limit', '5']);
  await record('received --since date', ['received', '--since', '2026-11-03']);
  await record('received --since local time', ['received', '--since', '2099-11-03 08:00']);
  await record('received --phone', ['received', '--phone', 'Desk phone', '--limit', '007']);
  await record('received bad since', ['received', '--since', 'yesterday']);
  await record('received limit 0', ['received', '--limit', '0']);
  await record('received interval without watch', ['received', '--interval', '5']);
  await record('received interval 2', ['received', '--watch', '--interval', '2']);
  await record('received argument', ['received', 'x']);
  await record('received huge span', ['received', '--since', '999999999999999d']);
  await record('received huge span --json', ['received', '--since', '999999999999999d', '--json']);
  await record('received send-only key', ['received'], { key: KEYS.sendOnly });
  await record('received --watch', ['received', '--watch', '--interval', '3'], {
    watch: (m) => {
      let added = false;
      return {
        timeoutMs: 20_000,
        onOutput: ({ stdout }, child) => {
          if (!added && stdout.includes('See you / at 10')) {
            added = true;
            m.addReceived({ id: 'abababababababababababab', sender: '+40733333333', message: 'New reply while watching' });
          }
          if (stdout.includes('New reply while watching')) setTimeout(() => child.kill(), 500);
        },
      };
    },
  });
  await record('received --watch --json', ['received', '--watch', '--interval', '3', '--json'], {
    watch: () => ({ timeoutMs: 20_000, onOutput: ({ stdout }, child) => stdout.includes('New reply') && setTimeout(() => child.kill(), 300) }),
  });
  await record('usage', ['usage']);
  await record('usage --json', ['usage', '--json']);
  await record('http to another host', ['phones', '--api-base', 'http://example.com/api/v1'], { apiBase: false });
  await record('redirect base', ['phones', '--api-base', mock.apiBase.replace('/api/v1', '/redirect-base/api/v1')], { apiBase: false });
  await record('html base', ['phones', '--api-base', mock.apiBase.replace('/api/v1', '/html-base/api/v1')], { apiBase: false });
  await record('nothing listening', ['phones', '--api-base', 'http://127.0.0.1:9/api/v1'], { apiBase: false });
  await record('nothing listening, a send', ['send', '+40712345678', 'Hi', '--api-base', 'http://127.0.0.1:9/api/v1'], { apiBase: false });
  await record('unknown host', ['phones', '--api-base', 'https://smstunnel-parity.invalid/api/v1'], { apiBase: false });
  await record('SMSTUNNEL_API_BASE', ['phones'], { apiBase: false, env: { SMSTUNNEL_API_BASE: mock.apiBase.replace('/api/v1', '') } });
  await record('SMSTUNNEL_API_BASE bad', ['phones'], { apiBase: false, env: { SMSTUNNEL_API_BASE: 'ftp://x' } });
  for (const bad of ['not a url', 'https://user:pass@smstunnel.io', 'https://smstunnel.io/api/v1?x=1', 'localhost:3141']) {
    await record(`api base ${bad}`, ['phones', '--api-base', bad], { apiBase: false });
  }
  await record('idempotency first', ['send', '+40712345678', 'Order 1042 is ready', '--idempotency-key', 'order-1042-ready']);
  await record('idempotency again', ['send', '+40712345678', 'Order 1042 is ready', '--idempotency-key', 'order-1042-ready']);
  await record('idempotency --json', ['send', '+40712345678', 'Order 1042 is ready', '--idempotency-key', 'order-1042-ready', '--json']);
  await record('idempotency --wait', ['send', '+40712345678', 'Order 1042 is ready', '--idempotency-key', 'order-1042-ready', '--wait']);
  await record('idempotency other text', ['send', '+40712345678', 'A different text', '--idempotency-key', 'order-1042-ready']);
  await record('idempotency code --json', ['code', '+40712345678', 'Order 1042 is ready 482913', '--idempotency-key', 'order-1042-ready', '--json']);
  await record('idempotency in progress', ['send', '+40712345678', 'Hi', '--idempotency-key', IDEMPOTENCY.inProgress]);
  for (const v of ['x'.repeat(256), 'commande-é', '   ', '  padded  ']) await record(`idempotency key ${JSON.stringify(v.slice(0, 12))}`, ['send', '+40712345678', 'Hi', '--idempotency-key', v]);
  await record('idempotency on phones', ['phones', '--idempotency-key', 'k']);
  await record('code with a key', ['code', '+40712345678', 'Your sign-in code is 552211', '--idempotency-key', 'login-552211']);
  await record('500 with a key', ['send', SPECIAL.serverProblem, 'Hi', '--idempotency-key', 'server-problem-1']);
  await record('500 with a key, again', ['send', SPECIAL.serverProblem, 'Hi', '--idempotency-key', 'server-problem-1']);
  await record('dropped with a key', ['send', SPECIAL.connectionDrops, 'Hi', '--idempotency-key', 'dropped-1']);
  await record('dropped with a key, again', ['send', SPECIAL.connectionDrops, 'Hi', '--idempotency-key', 'dropped-1']);
  await record('SMSTUNNEL_KEY_STORE=cloud login', ['login'], { key: null, input: `${KEYS.full}\n`, env: { SMSTUNNEL_KEY_STORE: 'cloud' } });
  fs.mkdirSync(configDir, { recursive: true });
  fs.writeFileSync(path.join(configDir, 'config.json'), '{ not json');
  await record('broken config.json', ['phones'], { key: null, apiBase: false });
  await record('broken config.json, logout', ['logout'], { key: null, apiBase: false });
  fs.writeFileSync(path.join(configDir, 'config.json'), '{"apiKey":5}');
  await record('a number as the saved key', ['phones'], { key: null, apiBase: true });
  fs.rmSync(path.join(configDir, 'config.json'));
  await mock.close();
  fs.rmSync(dir, { recursive: true, force: true });
  return out;
}

// ---- against a local SMSTunnel API ----

function localAPI() {
  const file = process.env.SMSTUNNEL_E2E_KEY_FILE;
  if (!file) return null;
  const j = JSON.parse(fs.readFileSync(file, 'utf8'));
  const base = `${process.env.SMSTUNNEL_E2E_API_BASE || j.serverUrl}`.replace(/\/+$/, '');
  if (!/^http:\/\/(127\.0\.0\.1|localhost)(:\d+)?(\/|$)/.test(base)) throw new Error(`not a local API: ${base}`);
  const key = process.env.SMSTUNNEL_E2E_KEY_NAME ? j.keys[process.env.SMSTUNNEL_E2E_KEY_NAME] : j.apiKey;
  return { base: `${base}/api/v1`, key, deviceId: j.deviceId, number: process.env.SMSTUNNEL_E2E_NUMBER || '+14155550123' };
}

async function localScenarios(cli, api, stamp) {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), `smstunnel-parity-local-${cli}-`));
  const configDir = path.join(dir, 'config');
  const base = { SMSTUNNEL_CONFIG_DIR: configDir, SMSTUNNEL_KEY_STORE: 'file', SMSTUNNEL_POLL_INTERVAL_MS: '500' };
  const out = [];
  const idem = `parity-${cli}-${stamp}`;
  const record = async (name, args, { key = api.key, env = {}, input } = {}) => {
    const result = await run(cli, ['--api-base', api.base, ...args], { env: cleanEnv({ ...base, SMSTUNNEL_API_KEY: key === null ? undefined : key, ...env }), input });
    out.push({ name, result, replacements: [[configDir, '<config>'], [dir, '<dir>'], [idem, '<idempotency key>']] });
    return result;
  };
  await record('login', ['login'], { key: null, input: `${api.key}\n` });
  await record('phones (saved key)', ['phones'], { key: null });
  await record('phones --json', ['phones', '--json']);
  await record('usage', ['usage']);
  await record('usage --json', ['usage', '--json']);
  await record('received --since 1d', ['received', '--since', '1d']);
  await record('received --json', ['received', '--json', '--limit', '3']);
  await record('received --phone by name', ['received', '--phone', 'test PHONE']);
  await record('status unknown', ['status', 'ffffffffffffffffffffffff']);
  const sent = await record('send --wait --json', ['send', api.number, `smstunnel CLI parity (<cli>): sent and checked`, '--wait', '--timeout', '30', '--json']);
  let id = 'ffffffffffffffffffffffff';
  try {
    id = JSON.parse(sent.stdout).messageId;
  } catch {}
  await record('status of it', ['status', id]);
  await record('status of it --json', ['status', id, '--json']);
  await record('code --wait', ['code', api.number, 'Your smstunnel CLI parity (<cli>) sign-in code is 482913', '--wait', '--timeout', '30']);
  await record('idempotency first', ['send', api.number, `smstunnel CLI parity (<cli>): ${idem}`, '--idempotency-key', idem, '--json']);
  await record('idempotency again', ['send', api.number, `smstunnel CLI parity (<cli>): ${idem}`, '--idempotency-key', idem]);
  await record('idempotency other text', ['send', api.number, 'smstunnel CLI parity: another text, same key', '--idempotency-key', idem]);
  await record('send to a phone that is not there', ['send', api.number, 'Hi', '--phone', 'No such phone']);
  await record('refused key', ['phones'], { key: `sk_${'live_'}${'0'.repeat(32)}` });
  await record('logout', ['logout'], { key: null });
  await record('phones after logout', ['phones'], { key: null });
  fs.rmSync(dir, { recursive: true, force: true });
  return out;
}

// One login, read by the other tool: the same config.json (and, on Windows, the same Credential Manager entry).
async function crossLogin(api, base) {
  const out = [];
  for (const store of ['file', process.platform === 'win32' ? 'auto' : null].filter(Boolean)) {
    for (const [first, second] of [['node', 'go'], ['go', 'node']]) {
      const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'smstunnel-parity-cross-'));
      const env = cleanEnv({ SMSTUNNEL_CONFIG_DIR: path.join(dir, 'config'), SMSTUNNEL_KEY_STORE: store, SMSTUNNEL_KEYCHAIN_SERVICE: 'smstunnel-cli-parity-test' }, false);
      const login = await run(first, ['login', '--api-base', base.apiBase], { env, input: `${base.key}\n` });
      const phones = await run(second, ['phones'], { env });
      const logout = await run(second, ['logout'], { env });
      const after = await run(first, ['phones'], { env });
      out.push({ store, first, second, login: login.code, saved: login.stdout.replace(/\(.*\)\./, ''), phones: phones.code, logout: logout.code, logoutText: logout.stdout, after: after.code });
      fs.rmSync(dir, { recursive: true, force: true });
    }
  }
  return out;
}

const keepIds = new Set([...PHONES.map((p) => p.id), 'abababababababababababab', 'e5e5e5e5e5e5e5e5e5e5e5e5', 'f6f6f6f6f6f6f6f6f6f6f6f6', 'ffffffffffffffffffffffff', '000000000000000000000001']);

const [nodeMock, goMock] = [await mockScenarios('node'), await mockScenarios('go')];
for (let i = 0; i < nodeMock.length; i++) {
  const a = nodeMock[i];
  const b = goMock[i];
  const ctxA = { replacements: a.replacements ?? nodeMock.find((x) => x.replacements).replacements, keepIds };
  const ctxB = { replacements: b.replacements ?? goMock.find((x) => x.replacements).replacements, keepIds };
  if (a.file !== undefined) {
    const [x, y] = [normalize(a.file, ctxA), normalize(b.file, ctxB)];
    results.push({ group: 'mock', name: a.name, same: x === y });
    if (x !== y) differences.push({ group: 'mock', name: a.name, diff: [['file', [x, y]]] });
    continue;
  }
  const req = (list, ctx) => normalize(JSON.stringify(list, null, 1), ctx);
  const watch = a.name.startsWith('received --watch');
  compare('mock', a.name, a.result, b.result, ctxA, ctxB, {
    requests: watch ? ['', ''] : [req(a.requests, ctxA), req(b.requests, ctxB)],
    skipExit: watch, // a killed process has no exit code to compare
  });
}

const api = localAPI();
let cross = [];
if (api) {
  const stamp = Date.now().toString(36);
  const [nodeLocal, goLocal] = [await localScenarios('node', api, stamp), await localScenarios('go', api, stamp)];
  for (let i = 0; i < nodeLocal.length; i++) {
    const a = nodeLocal[i];
    const b = goLocal[i];
    // Other keys of the same test account send texts too: usage counts are not compared.
    const counts = a.name.startsWith('usage');
    compare('local API', a.name, a.result, b.result, { replacements: a.replacements, keepIds, counts }, { replacements: b.replacements, keepIds, counts });
  }
  const mock = await startMockServer();
  cross = await crossLogin(api, { apiBase: mock.apiBase, key: KEYS.full });
  await mock.close();
  for (const c of cross) {
    const ok = c.login === 0 && c.phones === 0 && c.logout === 0 && c.after === 3 && /removed from/.test(c.logoutText);
    results.push({ group: 'shared login', name: `${c.store} store: ${c.first} logs in, ${c.second} uses and removes it`, same: ok });
    if (!ok) differences.push({ group: 'shared login', name: `${c.store}: ${c.first} → ${c.second}`, diff: [['codes', [JSON.stringify(c), '']]] });
  }
}

const lines = [`# Parity: Node.js version vs Go version`, '', `- Node.js CLI: ${nodeCli} (${process.version})`, `- Go CLI: ${goCli}`,
  `- Platform: ${process.platform} ${os.release()}, time zone ${Intl.DateTimeFormat().resolvedOptions().timeZone}`,
  `- Local API: ${api ? api.base : 'not used'}`, '', `${results.filter((r) => r.same).length} of ${results.length} scenarios identical.`, ''];
for (const g of [...new Set(results.map((r) => r.group))]) {
  const list = results.filter((r) => r.group === g);
  lines.push(`## ${g}: ${list.filter((r) => r.same).length}/${list.length}`, '');
  for (const r of list) lines.push(`- ${r.same ? 'same' : '**DIFFERENT**'}: ${r.name}`);
  lines.push('');
}
if (differences.length) {
  lines.push('## Differences', '');
  for (const d of differences) {
    lines.push(`### ${d.group}: ${d.name}`, '');
    for (const [field, [x, y]] of d.diff) lines.push(`${field}, Node:`, '```', x, '```', `${field}, Go:`, '```', y, '```', '');
  }
}
const outDir = path.join(here, 'results');
fs.mkdirSync(outDir, { recursive: true });
fs.writeFileSync(path.join(outDir, 'report.md'), `${lines.join('\n')}\n`);
console.log(lines.slice(0, 8).join('\n'));
for (const d of differences) console.log(`DIFFERENT: ${d.group}: ${d.name} (${d.diff.map(([f]) => f).join(', ')})`);
process.exit(differences.length ? 1 : 0);
