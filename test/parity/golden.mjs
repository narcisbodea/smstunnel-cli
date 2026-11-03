// Writes internal/cli/testdata/golden/node-<zone>.json: what the Node.js version (npm "smstunnel", its built dist/)
// answers for a corpus of inputs, function by function. The Go unit tests (golden_test.go) check that this version
// answers the same. Run it from this repository with the Node.js version built:
//
//   SMSTUNNEL_NODE_CLI=../smstunnel-cli node test/parity/golden.mjs          (this computer's time zone)
//   TZ=UTC SMSTUNNEL_NODE_CLI=../smstunnel-cli node test/parity/golden.mjs
//
// Keys in this file are built from pieces ("sk_" + "live_" …) so the repository holds no key-like literal.
import fs from 'node:fs';
import path from 'node:path';
import { createRequire } from 'node:module';
import { fileURLToPath } from 'node:url';

const nodeCli = path.resolve(process.env.SMSTUNNEL_NODE_CLI || '../smstunnel-cli');
const require = createRequire(path.join(nodeCli, 'package.json'));
const dist = (name) => require(path.join(nodeCli, 'dist', name));
const { normalizeApiBase, maskKey, pollIntervalMs } = dist('context.js');
const { parseSince, formatLocal, formatAgo, parseSeconds } = dist('time.js');
const { checkRecipient, checkCountryCode } = dist('numbers.js');
const { containsSignInCode } = dist('sign-in-code.js');
const { smsParts } = dist('sms-parts.js');
const { checkIdempotencyKey } = dist('idempotency-key.js');
const { parseRetryAfter, errorMessageFrom, ApiError } = dist('api.js');
const { describeApiError } = dist('errors.js');
const { describeStatus } = dist('commands/status.js');
const { outcomeOf } = dist('commands/send.js');
const { usageLines } = dist('commands/usage.js');
const { receivedLine } = dist('commands/received.js');
const { phonesTable } = dist('commands/phones.js');

const NOW = new Date('2026-11-03T12:00:00Z');
const attempt = (fn) => {
  try {
    return { ok: fn() };
  } catch (error) {
    return { error: error.message, exitCode: error.exitCode ?? null };
  }
};
const time = (d) => (d === undefined ? 'undefined' : Number.isNaN(d.getTime()) ? 'Invalid Date' : d.toISOString());
const num = (n) => (Number.isNaN(n) ? 'NaN' : Object.is(n, -0) ? '-0' : n === Infinity ? 'Infinity' : n === -Infinity ? '-Infinity' : n);

const urls = [
  'https://smstunnel.io', 'https://smstunnel.io/api/v1/', 'http://127.0.0.1:3121/api/v1', 'http://localhost:3121',
  'http://example.com/api/v1', 'ftp://smstunnel.io', 'https://user:pass@smstunnel.io', 'https://smstunnel.io/api/v1?x=1',
  'not a url', 'https://SMSTUNNEL.io:443/', 'http://127.1:3141', 'http://0x7f.0.0.1:3141/x', 'http://[0:0:0:0:0:0:0:1]:80/a',
  'http://localhost.:3141', 'localhost:3141', '127.0.0.1:3141', 'https:smstunnel.io', 'https:\\\\smstunnel.io\\api\\v1',
  'https:/smstunnel.io', 'https:///smstunnel.io', 'https://@smstunnel.io', 'https://:@smstunnel.io', 'https://a@smstunnel.io',
  'https://:x@smstunnel.io', 'https://::@smstunnel.io', 'https://a@b@smstunnel.io', 'https://@', 'https://user@:80',
  'https://smstunnel.io/a b/^/{x}/|/`/"/<>', 'https://smstunnel.io/a/./b/../c/%2e%2E/d', 'https://smstunnel.io/a/..',
  'https://smstunnel.io/a/.', 'https://smstunnel.io/./', 'https://smstunnel.io/%2e', 'https://smstunnel.io?', 'https://smstunnel.io#',
  'https://smstunnel.io/?x', 'https://smstunnel.io/#x', 'https://smstunnel.io/?#', 'https://bücher.de', 'https://xn--bcher-kva.de',
  'https://xn--abc.de', 'https://xn--.de', 'https://ex%41mple.com', 'http://%6Cocalhost:3141', 'https://ex%zzmple.com',
  'https://smstunnel.io:99999', 'https://smstunnel.io:', 'https://smstunnel.io:0443', 'https://smstunnel.io:65535',
  'https://smstunnel.io:65536', 'https://smstunnel.io:12a', 'http://localhost:80/x', 'https://exa mple.com', 'mailto:x',
  'file:///c:/x', 'file://exa mple/', 'file://localhost/x', 'foo://exa mple', 'foo://ok/path', 'foo:/x', 'foo:', 'ws://x',
  'wss://x', '  https://smstunnel.io  ', '\u0001https://smstunnel.io\u0002', 'https://smst\tunnel.io', 'https://smst\nunnel.io',
  'https://smstunnel.io/api/v1///', '', ':', '1http://x', 'h:', 'http://[::1]:3141', 'http://[::1', 'http://[::1]x',
  'http://[1:2:3:4:5:6:7:8]', 'http://[1::]', 'http://[::ffff:127.0.0.1]', 'http://[0::0:1]:3141/api/v1', 'http://[1:2::3:4]',
  'http://[1:0:0:2:0:0:0:3]', 'http://[::1:2:3:4:5:6:7:8]', 'http://[g::1]', 'https://example.123', 'https://1.2.3.4.5',
  'https://0x.0x', 'https://999999999999', 'https://4294967295', 'https://4294967296', 'https://0300.0250.1.1',
  'https://1.2.3.256', 'https://1.2.65536', 'https://09.1.1.1', 'https://1.2.3.4.', 'https://a..b', 'https://.',
  'https://smstunnel.io/é', 'https://ÉXAMPLE.com', 'https://ａｂｃ.com', 'https://ab\u3002cd', 'https://a\u00adb.com',
  'https://smstunnel.io\\api', 'https://smstunnel.io/a\\b', 'http://localhost/a?', 'HTTPS://SMSTUNNEL.IO/API/V1',
  'https://smstunnel.io/%7e/~', 'https://[::1]/api/v1', 'https://localhost:3121', 'https://smstunnel.io//api',
  'https://smstunnel.io/%2e%2e/x', 'https://smstunnel.io/a/%2E./b', 'https://smstunnel.io/a/.%2e', 'https://exa^mple.com',
  'https://exa|mple.com', 'https://exa%mple.com', 'https://ex\u0000ample.com', 'https://[::1]:3141', 'https://[::]',
];

const dates = [
  '2026-11-03', '2026-11-03T08:00', '2026-11-03T08:00:00', '2026-11-03 08:00', '2026-11-03 8:00', '2026-11-03T8:00',
  '2026-11-03 08:00Z', '2026-11-03T08:00:00+0200', '2026-11-03T08:00:00+02:00', '2026-11-03 08:00 GMT+2',
  '2026-11-03 08:00 PM', '2026-11-03 8 PM', '2026-11-03T24:00', '2026-11-03T24:00:01', '2026-02-30', '2026-13-45',
  '2026-11-03junk', '2026-11-03 junk', '2026-11-03 (comment) 10:00', '2026-11-03 EST', '2026-11-03 10:00:00.123456',
  '2026-11-03T10:00:00.1Z', '2026-11-03T10:00:00.Z', '2026-11-03t10:00', '2026-11-03T10:00z', '2026-11-03 1:2:3',
  '2026-11-03 10.30', '2026-11-03 10::', '2026-11-03 Nov', '2026-11-030', '20261-11-03', '2026-11-03T10:00:00-00:00',
  '2026-11-03 10:00 +5', '2026-11-03 10:00 -0530', '2026-11-03 10:00 UTC+0530', '2026-11-03 10:00:60',
  '2026-11-03\u00a010:00', '2026-11-03\u2028 10:00', '2026-11-03 ２', '2026-11-03 10:00 am', '2026-11-03 12:00 am',
  '2026-11-03 13:00 pm', '2026-11-03,10:00', '2026-11-03/10:00', '2026-11-03 10:00:00 Z', '2026-11-03T10:00:00Z ',
  ' 2026-11-03 ', '2026-11-03T10', '9999-12-31T23:59:59Z', '2026-11-03 99999999999:00', '2026-03-29T03:30',
  '2026-03-29T03:00', '2026-03-29T04:00', '2026-03-29T02:59', '2026-10-25T03:30', '2026-10-25T03:00', '2026-10-25T04:00',
  '2026-10-25T02:59', '+002026-11-03T08:00:00Z', '-000001-01-01T00:00:00Z', '-000000-01-01T00:00:00Z',
  '+275760-09-13T00:00:00Z', '+275760-09-13T00:00:00.001Z', '-271821-04-20T00:00:00Z', '-271821-04-19T23:59:59Z',
  '2026-11-03T08:00:00.000000000001Z', '2026-11-03T08:00:00.9999Z', '2026-11-03T23:59:59.999+14:00', '2026-11-03T00:00-24:00',
  '2026-11-03T00:00+23:59', '2026-11-03T00:00+2400', 'Wed, 21 Oct 2015 07:28:00 GMT', 'Tue, 03 Nov 2026 12:00:30 GMT',
  'Sunday, 06-Nov-94 08:49:37 GMT', 'Sun Nov  6 08:49:37 1994', 'Nov 5', '11/5', '11/5/26', '5 Nov 2026', 'November 5, 2026 10:00',
  '2026-11-03 Mon', 'Mon 2026-11-03', '2026', '2026-11', '2026-11-03T10:00:00.123+01:30', '12:00', 'soon', '', 'T', 'Z',
  '2026-11-03 (unclosed', '2026-11-03 ((a)b) 10:00', '2026-11-03 10:00 (EST)', '2026-11-03 10:00 PST', '2026-11-03 10:00 pdt',
  '2026-11-03 10:00 utc', '2026-11-03 10:00 ut', '2026-11-03 10:00 gmtx', '2026-11-03 10:00 octx', '2026-11-03 10:00 -',
  '2026-11-03 10:00 +', '2026-11-03 10:00 +12345', '2026-11-03 10:00 +1:30', '2026-11-03 10:00:00.5', '2026-11-03 10:00:00.',
  '2026-11-03 24:00', '2026-11-03 24:01', '2026-11-03T08:00:00\u0000junk', '2026-11-03 \t10:00', '2026-11-03\n10:00',
  '2026-11-03 10:00 PM PM', '2026-11-03 0:00 pm', '2026-11-03 12:30 pm', '2026-11-03 Z', '2026-11-03 GMT', '2026-11-03 +0100',
  '2026-11-03 10:00 GMT-0800 (Pacific)', '1970-01-01T00:00:00Z', '1969-12-31T23:59:59.999Z', '1900-01-01T00:00',
  '1890-06-01T12:00', '2100-07-01T12:00', '2400-02-29', '2100-02-29', '0000-01-01', '0001-02-03T04:05:06Z', '0049-01-01',
  '0050-01-01T00:00', '0099-12-31 10:00',
];

const spans = ['90s', '30m', '2h', '1d', '1w', ' 2H ', '0s', '5 m', '5\u00a0h', '5ſ', '1000000d', '999999999999999d', '99999999w',
  '100000w', '1e3s', '-1h', '2h ago', 'yesterday', '12', '', '007m', '3M', '1W', 'h', '1hh', '10000000000000000000000s'];

const phones = ['+40712345678', '0712 345 678', '(0721) 123-456', '0040712345678', '+1 415.555.0123', ' +40712345678 ',
  '+40712345678,+40722333444', '0712345678; 0722333444', '+40712345678 +40722333444', '0712345678|0722333444',
  '0712345678 & 0722333444', '0712345678\n0722333444', '0712345678 0722333444', '0712345678/0722333444',
  '0712345678 or 0722333444', '0721-CALL-ME', '12345', '0721+123456', '', '0151/2345678', '+1415555\u00a00123', '+1415555\u20280123',
  '+14155550123\u0085', '\ufeff+14155550123\ufeff', '+١٤١٥٥٥٥٠١٢٣', '123456', '1234567', '12345678901234567', '123456789012345678',
  '+123456789012345678901234', '+1234567890123456789012345', '(((((((((((((((((((((((((', '1-2-3-4-5-6', '+ 1 2 3 4 5 6', 'ab',
  '+40 712 345 678 ', '+4o712345678', '+40712345678\t', '\t+40712345678'];

const countries = ['+40', '40', '0040', '1', '+1', '+353', ' +44 ', 'RO', 'DE', '+40a', '0', '+', '+12345', '', '001', '00',
  '0001', '+0', '1234', '12345', '+ 4 0', '٤٠', '+\u00a040', '0044', '00444', '004444', '0044444'];

const codes = ['123456', 'Code: 1234', 'Code:1234', 'G-123456', '12-345678', '123-456', 'Your sign-in code is 482913. It expires in 5 minutes.',
  '123456 is your code', 'Your login code is 12-345678.', '2026', 'hello there', 'A1B2C3', 'ORD12345 has shipped', '12345678901',
  'Call +40712345678', 'Pi is 3.14159', 'See you on 01/11/2026', 'Meet at 10:30', '123', '', '1234567890', '12345678901 1234',
  'x 1234.', '1234.5', '1.1234', '1234,', '1234,5', '12:1234', '1234:', '_1234', '1234_', '-1234', '+1234', '123-4567', '1234-567',
  '123-456-789', '-123-456', '123-456-', '123-456-7', '123-456.7', 'a123-456', '123-456a', '1.123-456', '١٢٣٤', '😀1234😀', '1234😀',
  '12 34 56', '0000', '9999999999', '00000000000', 'code\n482913', '482913\n', '(4829)', '[123-456]', '1/1234', '1234/1', '1234/a'];

const texts = ['', 'Hello', 'a'.repeat(160), 'a'.repeat(161), 'a'.repeat(306), 'a'.repeat(307), '€'.repeat(80), '€'.repeat(81),
  '{}[]~^|\\', 'Mulțumim, ș și ț', 'ș'.repeat(70), 'ș'.repeat(71), 'ș'.repeat(134), 'ș'.repeat(135), '😀', 'abc', '😀😀',
  'ăîâșț', '€'.repeat(76) + 'a', '\f', '\n\r', 'é'.repeat(161), 'Ç'.repeat(160), '😀'.repeat(35), '😀'.repeat(36), 'x😀'];

const idems = ['a', 'order-1042-ready', 'b7d1c0e2-8f7a-4c47-9a39-3f5f2c3d9e10', 'with spaces inside', '"quoted"', 'x'.repeat(255),
  '  padded  ', '', '   ', 'x'.repeat(256), 'commande-é', 'tab\there', 'emoji-😀', 'line\nbreak', '\u00a0k\u00a0', '~', '\u007f'];

const seconds = ['60', '5s', '1.5', '0', '-1', 'abc', '', '5S', '5ss', '.5', '5.', '1e3', '0.0001', '007', '1.50', ' 2 ', '2 s',
  '99999999999999999999', '3', '2.999', '0s', '1_000'];

const polls = ['100', '50', '49.9', '1e2', '0x64', ' 100 ', '', 'abc', 'Infinity', '-100', '1_000', '0b1111111', '1.', '.5e3', '+70'];

const lowers = ['İstanbul', 'ΟΔΟΣ', 'Σ', 'ΑΣ ΣΑ', 'ẞ', 'Ǆ', 'K', 'ﬁ', 'AΣ.', 'AΣ\u0301', 'A\u0301Σ', 'ʼΣ', '1Σ', 'DESK PHONE',
  'Ⅻ', 'Ａ', 'ΣΣ', 'ΑΣ\u200dΒ', 'ΑΣ1', 'Σa'];

const trims = [' a ', '\u00a0a\u00a0', '\ufeffa\ufeff', '\u0085a\u0085', '\u2028a\u2029', '\u180ea\u180e', '\u3000a', '\u200ba'];

const jsons = ['{}', '[]', 'null', '0', '-0', '1.0', '1e2', '1E21', '1e-7', '0.000001', '123456789012345678901', '1.5e300', '1e400',
  '-1e400', '0.1', '5e-324', '2.5e-324', '9007199254740993', '-12.34e+5', '1e21', '123e-20', '"\\u2028\\u2029<>&"',
  '"\\u0000\\u001f\\u007f"', '"\\ud800x\\udc00"', '"\\ud83d\\ude00 \\ud800 \\udfff"', '"\\ud83d\\ud83d\\ude00"',
  '{"b":1,"2":2,"a":3,"1":4,"b":5,"-1":6,"01":7,"4294967294":8,"4294967295":9}', '{"a":[],"b":{},"c":[1,{"d":[]}]}',
  '{"__proto__":{"x":1},"constructor":2}', ' {"a" : [ 1 , 2 ] } ', '{"a":1,}', '[1,]', '01', '1.', '.1', '+1', 'tru', 'NaN',
  '"\\x"', '"a\tb"', '"\\/"', '{"a":"\\"\\\\\\b\\f\\n\\r\\t"}', '\ufeff{}', '{"a":1}x', '{"":1,"0":2,"00":3}', '[[[]]]',
  '{"k":-0.0}', '{"k":100000000000000000000.5}', '"é😀"'];

const retries = ['7', '12', '0', '00', ' 7 ', '7.5', '-1', 'soon', '', new Date(NOW.getTime() + 30_000).toUTCString(),
  new Date(NOW.getTime() - 30_000).toUTCString(), 'Tue, 03 Nov 2026 12:00:00 GMT', '2026-11-03T12:01:00Z', '2026-11-03',
  '99999999999999999999', '1e3', '0x10'];

const errorBodies = [
  [{ message: ['a', 'b'] }, '', 400], [{ message: 'Invalid phone number' }, '', 400], [{ error: 'Bad Request' }, '', 400],
  [undefined, 'Service Unavailable', 503], [undefined, '<html>…</html>', 502], [{ message: ['', ' ', 'x'] }, '', 400],
  [{ message: [' a ', 5, 'b'] }, '', 400], [{ message: '  m  ' }, '{"message":"  m  "}', 400], [{ message: '  ' }, '{"message":"  "}', 400],
  [{ statusCode: 500 }, '{"statusCode":500}', 500], [[1, 2], '[1,2]', 500], [undefined, 'x'.repeat(300), 500],
  [undefined, 'x'.repeat(301), 500], [undefined, '   ', 500], [{ message: [] , error: 'E' }, '', 400], [null, 'null', 400],
  [{ error: 5 }, '{"error":5}', 418], [undefined, ' <p>x</p>', 500], [undefined, 'é'.repeat(300), 500], [undefined, '😀'.repeat(150), 500],
  [undefined, '😀'.repeat(151), 500],
];

const statuses = [
  { status: 'sent', sentAt: '2026-11-03T08:00:00Z' }, { status: 'sent' }, { status: 'sent', sentAt: '' }, { status: 'sent', sentAt: 'bad' },
  { status: 'delivered', deliveredAt: '2026-11-03T09:30:00.000Z' }, { status: 'delivered' }, { status: 'failed', errorMessage: 'No signal' },
  { status: 'failed' }, { status: 'failed', errorMessage: '' }, { status: 'failed', unconfirmed: true, deliveryStatus: 'unconfirmed' },
  { status: 'failed', deliveryStatus: 'unconfirmed' }, { status: 'sent', unconfirmed: 'yes' }, { status: 'pending' }, { status: 'queued' },
  {}, { status: 5 }, { status: null }, { status: 'sent', sentAt: 1762156800000 }, { status: 'sent', sentAt: true },
];

const usages = [
  { plan: 'free', usage: { smsClean: 3, smsBranded: 0, twoFaClean: 1, smsCleanMonth: 40 },
    limits: { smsClean: 25, twoFaClean: 20, smsCleanPerMonth: 500, smsBrandedPerDay: 25 },
    remaining: { smsClean: 22, twoFaClean: 19, smsCleanMonth: 460, smsBranded: 25 } },
  { plan: 'pro', usage: { smsClean: 0, smsBranded: 0, twoFaClean: 0 }, limits: { smsClean: 'unlimited', twoFaClean: 'unlimited' },
    remaining: { smsClean: 'unlimited', twoFaClean: 'unlimited' } },
  {}, { plan: null }, { plan: 'enterprise', enterprise: { id: 'x', name: 'ACME' } }, { enterprise: {} },
  { usage: { smsClean: null }, remaining: {}, limits: { smsClean: null, smsCleanPerMonth: null, smsBrandedPerDay: null } },
  { limits: { smsCleanPerMonth: 'unlimited', smsBrandedPerDay: 0 }, usage: { smsCleanMonth: 1, smsBranded: 2 } },
  { limits: { smsBrandedPerDay: '0' }, remaining: { smsBranded: 1 } }, { usage: 5, limits: 'x', remaining: [] },
  { usage: { smsClean: 1.5e21, twoFaClean: -0 }, remaining: { smsClean: [1, 2], twoFaClean: { a: 1 } }, limits: { smsClean: true } },
];

const receivedTexts = [
  { id: '1', sender: '+40712345678', message: 'See you\nat 10', receivedAt: '2026-11-03T08:05:00Z', deviceName: 'Store phone' },
  { id: '2', sender: '+1', message: null, isEncrypted: true, receivedAt: '2026-11-03T08:05:00Z' },
  { id: '3', sender: '+1', message: null, receivedAt: null }, { id: '4', sender: '+1', message: 'x', isEncrypted: 'no', receivedAt: 'bad' },
  { id: '5', sender: '+1', message: 'a \r\n \n b\r c\t\n', receivedAt: '2026-11-03T08:05:00.000Z', deviceName: '' },
  { id: '6', sender: null, message: '\u00a0\n\u2028x', receivedAt: 0 }, { id: '7', sender: 5, message: 'a\u2028b', receivedAt: 1 },
  { id: '8', message: '', receivedAt: '2026-11-03T08:05:00Z', deviceName: 'X' },
];

const deviceLists = [
  [{ id: 'a'.repeat(24), name: 'Store phone', isOnline: true, lastSeenAt: '2026-11-03T11:59:00Z' },
    { id: 'b'.repeat(24), name: 'Old Samsung', isOnline: false, lastSeenAt: '2026-10-30T12:00:00Z' }],
  [{ id: 'c'.repeat(24), name: 'Ünïcødé 😀 phone', isOnline: false },
    { id: 'd'.repeat(24), name: 'x', isOnline: 0, lastSeenAt: '2026-11-03T11:59:30Z' },
    { id: 'e'.repeat(24), name: 'y', isOnline: 'yes', lastSeenAt: null },
    { id: 'f'.repeat(24), name: 'z', isOnline: false, lastSeenAt: '2026-11-03T11:55:00Z' },
    { id: '1'.repeat(24), name: 'w', isOnline: false, lastSeenAt: '2026-11-03T09:00:00Z' },
    { id: '2'.repeat(24), name: 'v', isOnline: false, lastSeenAt: '2026-11-05T09:00:00Z' },
    { id: '3'.repeat(24), name: 'u', isOnline: false, lastSeenAt: 'not a date' },
    { id: '4'.repeat(24), name: 't', isOnline: false, lastSeenAt: '2026-11-01T12:30:00Z' },
    { id: '5'.repeat(24), name: 's', isOnline: false, lastSeenAt: '2026-11-03T11:59:29.500Z' }],
];

const locals = ['2026-11-03T08:05:00Z', null, '', 'not a date', 0, 1, 1762156800000, -1, true, false, '2026-03-29T00:59:59.999Z',
  '2026-03-29T01:00:00Z', '2026-10-25T00:59:59Z', '2026-10-25T01:00:00Z', '+010000-01-01T00:00:00Z', '-000050-06-15T12:34:00Z',
  '0033-01-01T00:00:00Z', [], ['2026-11-03'], {}];

const agos = ['2026-11-03T11:59:30Z', '2026-11-03T11:55:00Z', '2026-11-03T09:00:00Z', '2026-10-30T12:00:00Z', null, '',
  '2026-11-03T11:59:00.500Z', '2026-11-03T11:59:00.499Z', '2026-11-03T11:30:30Z', '2026-11-03T11:30:29Z', '2026-11-01T12:30:00Z',
  '2026-11-01T12:29:59Z', '2026-12-01T00:00:00Z', 'garbage', 1762171200000];

const keys = ['sk_' + 'live_' + '0123456789abcdef'.repeat(2), 'sk_short', 'abcdefghijklmn', 'abcdefghijklmno', 'a'];

const forms = ['+40722222222', 'a b', '*-._~', 'é', '😀', '&=?#', '2026-11-03T08:00:00.000Z', "'!()"];

const kinds = [
  { kind: 'network', message: 'connect ECONNREFUSED', networkCode: 'ECONNREFUSED', mayHaveArrived: false },
  { kind: 'network', message: 'socket hang up', networkCode: 'ECONNRESET', mayHaveArrived: true },
  { kind: 'timeout', message: 'No answer in time', networkCode: 'ETIMEDOUT', mayHaveArrived: true },
  { kind: 'timeout', message: 'No answer in time', networkCode: 'ETIMEDOUT', mayHaveArrived: false },
  { kind: 'redirect', status: 302, message: 'Redirect to x', mayHaveArrived: true },
  { kind: 'invalid-response', status: 200, message: 'The answer was not JSON', mayHaveArrived: true },
  ...[400, 401, 402, 404, 409, 410, 418, 422, 429, 500, 502, 503, 504, 599, 200, 301, 600, 199].map((status) => ({
    kind: 'http', status, message: `said ${status}`, mayHaveArrived: true })),
  { kind: 'http', status: 403, message: 'plan', mayHaveArrived: true },
  ...['sms:inbox', 'sms:send', 'sms:read', 'x:y', undefined].map((permission) => ({
    kind: 'http', status: 403, code: 'PERMISSION_REQUIRED', permission, message: 'perm', mayHaveArrived: true })),
  { kind: 'http', status: 403, code: 'OTHER', message: 'other code', mayHaveArrived: true },
  { kind: 'http', status: 422, code: 'IDEMPOTENCY_KEY_REUSED', message: 'reused', mayHaveArrived: true },
  { kind: 'http', status: 409, code: 'IDEMPOTENCY_KEY_IN_PROGRESS', retryAfter: 1, message: 'busy', mayHaveArrived: true },
  { kind: 'http', status: 409, code: 'IDEMPOTENCY_KEY_IN_PROGRESS', retryAfter: 3, message: 'busy', mayHaveArrived: true },
  { kind: 'http', status: 409, code: 'IDEMPOTENCY_KEY_IN_PROGRESS', message: 'busy', mayHaveArrived: true },
  { kind: 'http', status: 409, code: 'IDEMPOTENCY_KEY_IN_PROGRESS', retryAfter: 0, message: 'busy', mayHaveArrived: true },
  { kind: 'http', status: 429, retryAfter: 42, message: 'ThrottlerException', mayHaveArrived: true },
  { kind: 'http', status: 429, retryAfter: 1, message: 'x', mayHaveArrived: true },
  { kind: 'http', status: 429, retryAfter: 0, message: 'x', mayHaveArrived: true },
  { kind: 'http', status: 429, retryAfter: 1e21, message: 'x', mayHaveArrived: true },
  { kind: 'http', status: 429, message: 'HTTP 429', mayHaveArrived: true },
  { kind: 'http', status: 429, message: "You've sent today's 25 texts on the Free plan.", mayHaveArrived: true },
  { kind: 'http', status: 500, message: 'x', replayed: true, mayHaveArrived: true },
  { kind: 'http', status: 404, message: 'Device not found', mayHaveArrived: true },
];
const contexts = [];
for (const operation of ['login', 'phones', 'send', 'code', 'status', 'received', 'usage']) {
  for (const keySource of ['env', 'stored', 'typed']) {
    for (const idempotencyKey of [false, true]) contexts.push({ operation, keySource, idempotencyKey });
  }
}

const out = {
  zone: Intl.DateTimeFormat().resolvedOptions().timeZone,
  now: NOW.toISOString(),
  urls: urls.map((input) => ({ input, ...attempt(() => normalizeApiBase(input)) })),
  dates: dates.map((input) => ({ input, value: num(Date.parse(input)) })),
  since: [...dates, ...spans].map((input) => ({ input, value: time(parseSince(input, NOW)) })),
  phones: phones.map((input) => ({ input, ...checkRecipient(input) })),
  countries: countries.map((input) => ({ input, ...checkCountryCode(input) })),
  codes: codes.map((input) => ({ input, value: containsSignInCode(input) })),
  texts: texts.map((input) => ({ input, ...smsParts(input) })),
  idems: idems.map((input) => ({ input, ...checkIdempotencyKey(input) })),
  seconds: seconds.map((input) => ({ input, value: parseSeconds(input) ?? null })),
  polls: polls.map((input) => ({ input, value: num(pollIntervalMs({ SMSTUNNEL_POLL_INTERVAL_MS: input }, 2000)) })),
  lowers: lowers.map((input) => ({ input, value: input.toLowerCase() })),
  trims: trims.map((input) => ({ input, value: input.trim() })),
  jsons: jsons.map((input) => {
    try {
      const v = JSON.parse(input);
      return { input, pretty: JSON.stringify(v, null, 2) ?? null, compact: JSON.stringify(v) ?? null, string: typeof v === 'object' && v !== null ? null : String(v) };
    } catch {
      return { input, error: true };
    }
  }),
  retries: retries.map((input) => ({ input, value: parseRetryAfter(input, NOW.getTime()) ?? null })),
  errorBodies: errorBodies.map(([body, text, status]) => ({ body: body === undefined ? '__undefined__' : body, text, status, value: errorMessageFrom(body, text, status) })),
  statuses: statuses.map((input) => ({ input, value: describeStatus(input), outcome: outcomeOf(input) ?? null })),
  usages: usages.map((input) => ({ input, value: usageLines(input) })),
  received: receivedTexts.map((input) => ({ input, value: receivedLine(input) })),
  tables: deviceLists.map((input) => ({ input, value: phonesTable(input, NOW) })),
  locals: locals.map((input) => ({ input, value: formatLocal(input) })),
  agos: agos.map((input) => ({ input, value: formatAgo(input, NOW) })),
  // Keys as lists of characters, so the file holds no key-like text.
  masks: keys.map((input) => ({ chars: [...input], valueChars: [...maskKey(input)] })),
  forms: forms.map((input) => ({ input, value: new URLSearchParams({ x: input }).toString().slice(2) })),
  apiErrors: (() => {
    // Each error × each context: the message (an index into messages) and the exit code; the details depend only on the error.
    const messages = [];
    const index = new Map();
    const results = kinds.map((error) => {
      let details;
      const row = contexts.map((context) => {
        const described = describeApiError(new ApiError(error), { ...context, apiBase: 'https://smstunnel.io/api/v1' });
        details = JSON.stringify(described.details);
        if (!index.has(described.message)) {
          index.set(described.message, messages.length);
          messages.push(described.message);
        }
        return [index.get(described.message), described.exitCode];
      });
      return { error, details, row };
    });
    return { contexts, messages, results };
  })(),
};

const here = path.dirname(fileURLToPath(import.meta.url));
const dir = path.join(here, '..', '..', 'internal', 'cli', 'testdata', 'golden');
fs.mkdirSync(dir, { recursive: true });
const file = path.join(dir, `node-${out.zone.replace(/[^A-Za-z0-9]+/g, '-')}.json`);
fs.writeFileSync(file, `${JSON.stringify(out)}\n`);
console.log(`wrote ${path.relative(process.cwd(), file)}: ${Object.entries(out).filter(([, v]) => Array.isArray(v)).map(([k, v]) => `${k} ${v.length}`).join(', ')}, apiErrors ${out.apiErrors.results.length * contexts.length}`);
