const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const { test } = require('node:test');

const source = fs.readFileSync(process.env.COMMANDCODEBRIDGE_AUTH_SOURCE || path.join(__dirname, '../internal/bridge/ui/cpa-auth.js'), 'utf8');
const origin = 'https://cpa.example';
const userAgent = 'CommandCodeBridge auth regression fixture';
const key = 'fixture-management-key';

test('reuse the saved panel login on the native login page behind a proxy prefix', () => {
  const context = loadAuth({ login: savedLogin(origin + '/cpa'), consoleURL: origin + '/cpa/v0/resource/plugins/commandcodebridge/login?state=fixture' });
  assert.equal(context.CommandCodeBridgeAuthHeaders().Authorization, 'Bearer ' + key);
});

function savedLogin(apiBase = origin, rememberPassword = true) {
  return { state: { apiBase, managementKey: key, rememberPassword }, version: 0 };
}

function encrypted(login, version = 'v1') {
  const bytes = new TextEncoder().encode(JSON.stringify(login));
  const mask = new TextEncoder().encode(version === 'v2'
    ? 'cli-proxy-api-webui::secure-storage|v2|cpa.example'
    : `cli-proxy-api-webui::secure-storage|cpa.example|${userAgent}`);
  return `enc::${version}::` + Buffer.from(bytes.map((byte, i) => byte ^ mask[i % mask.length])).toString('base64');
}

function loadAuth({ login = savedLogin(), apiBase = '/v0/management/commandcodebridge', consoleURL = origin + apiBase.replace(/\/v(?:0|8)\/management\/commandcodebridge\/?$/, '/v0/resource/plugins/commandcodebridge/console'), encoded = true, version = 'v1', userAgent: currentUserAgent = userAgent, loggedIn = 'true', session = new Map(), persistent = new Map(), blocked = false } = {}) {
  for (const [name, value] of [['isLoggedIn', loggedIn], ['cli-proxy-auth', encoded ? encrypted(login, version) : JSON.stringify(login)]]) {
    if (value !== null && !persistent.has(name)) persistent.set(name, value);
  }
  const storage = {
    getItem: name => persistent.get(name) ?? null,
    setItem: (name, value) => persistent.set(name, value),
    removeItem: name => persistent.delete(name),
  };
  const apiMeta = { content: apiBase };
  const context = vm.createContext({
    location: new URL(consoleURL),
    navigator: { userAgent: currentUserAgent },
    document: { querySelector: () => apiMeta },
    localStorage: storage,
    sessionStorage: {
      getItem: name => { if (blocked) throw new Error('storage blocked'); return session.get(name) ?? null; },
      setItem: (name, value) => { if (blocked) throw new Error('storage blocked'); session.set(name, value); },
      removeItem: name => { if (blocked) throw new Error('storage blocked'); session.delete(name); },
    },
    TextEncoder, TextDecoder, Uint8Array, URL, atob,
  });
  context.window = context;
  vm.runInContext(source, context);
  return context;
}

async function headers(options) {
  return JSON.parse(JSON.stringify(await loadAuth(options).CommandCodeBridgeAuthHeaders()));
}

test('reuse existing remembered login, encrypted or plain', async () => {
  for (const encoded of [true, false]) assert.deepEqual(await headers({ encoded }), { Authorization: `Bearer ${key}` });
});

test('reuse CPAMP v2 login even after the user agent changes', async () => {
  const persistent = new Map();
  for (const agent of [userAgent, 'Changed browser user agent']) {
    assert.deepEqual(await headers({ persistent, version: 'v2', userAgent: agent }), { Authorization: `Bearer ${key}` });
  }
});

test('recognize root, v0 and v8 endpoint forms including reverse-proxy prefixes', async () => {
  for (const prefix of ['', '/proxy']) {
    for (const version of ['v0', 'v8']) {
      for (const suffix of ['', '/v0/management/', '/v8/management/']) {
        assert.deepEqual(await headers({ login: savedLogin(origin + prefix + suffix), apiBase: `${prefix}/${version}/management/commandcodebridge` }), { Authorization: `Bearer ${key}` }, `${prefix} ${version} ${suffix}`);
      }
    }
  }
});

test('reject saved keys for another origin or another CPA on the same origin', async () => {
  for (const base of ['https://other.example', origin + '/other', 'http://cpa.example', 'https://user:pass@cpa.example', origin + '?server=other', origin + '#other']) {
    assert.deepEqual(await headers({ login: savedLogin(base) }), {}, base);
  }
});

test('do not reuse logged-out or unremembered persistent credentials', async () => {
  assert.deepEqual(await headers({ loggedIn: 'false' }), {});
  assert.deepEqual(await headers({ login: savedLogin(origin, false) }), {});
});

test('manual bearer and X-Management-Key survive refresh and iframe recreation within one tab', () => {
  for (const type of ['bearer', 'x-management-key']) {
    const session = new Map();
    const options = { session, login: savedLogin(origin, false), loggedIn: null };
    const first = loadAuth(options);
    first.CommandCodeBridgeSaveAuth(key, type);
    const expected = type === 'bearer' ? { Authorization: `Bearer ${key}` } : { 'X-Management-Key': key };
    assert.deepEqual(JSON.parse(JSON.stringify(loadAuth(options).CommandCodeBridgeAuthHeaders())), expected);
    assert.deepEqual(JSON.parse(JSON.stringify(loadAuth({ ...options, session: new Map() }).CommandCodeBridgeAuthHeaders())), {});
    assert.deepEqual(JSON.parse(JSON.stringify(loadAuth({ ...options, apiBase: '/other/v8/management/commandcodebridge' }).CommandCodeBridgeAuthHeaders())), {});
    first.CommandCodeBridgeClearAuth();
    assert.equal(session.size, 0);
  }
});

test('remembered manual keys survive a new session, stay endpoint scoped, and preserve panel storage on clear', async () => {
  for (const type of ['bearer', 'x-management-key']) {
    const session = new Map(), persistent = new Map();
    const options = { session, persistent };
    const context = loadAuth(options);
    const panelStorage = new Map(persistent);
    const expected = type === 'bearer' ? { Authorization: 'Bearer remembered-fixture-key' } : { 'X-Management-Key': 'remembered-fixture-key' };
    context.CommandCodeBridgeSaveAuth('remembered-fixture-key', type, true);
    assert.equal(session.size, 1);
    assert.equal(persistent.size, panelStorage.size + 1);
    assert.deepEqual(await headers({ ...options, session: new Map() }), expected);
    assert.deepEqual(await headers({ ...options, session: new Map(), apiBase: '/other/v8/management/commandcodebridge' }), {});

    const sessionOnly = new Map();
    loadAuth({ session: sessionOnly }).CommandCodeBridgeSaveAuth('session-fixture-key', type);
    const sessionExpected = type === 'bearer' ? { Authorization: 'Bearer session-fixture-key' } : { 'X-Management-Key': 'session-fixture-key' };
    assert.deepEqual(await headers({ ...options, session: sessionOnly }), sessionExpected);

    context.CommandCodeBridgeClearAuth();
    assert.equal(session.size, 0);
    assert.deepEqual(persistent, panelStorage);
    assert.deepEqual(await headers({ ...options, session: new Map() }), { Authorization: `Bearer ${key}` });
  }
});

test('default and explicit session-only saves remove an old remembered key for this endpoint', async () => {
  for (const remember of [undefined, false]) {
    const session = new Map(), persistent = new Map();
    const options = { session, persistent, login: savedLogin(origin, false), loggedIn: null };
    const context = loadAuth(options);
    const otherOptions = { ...options, session: new Map(), apiBase: '/other/v8/management/commandcodebridge' };
    loadAuth(otherOptions).CommandCodeBridgeSaveAuth('other-fixture-key', 'bearer', true);
    const savedPersistent = new Map(persistent);
    context.CommandCodeBridgeSaveAuth('old-fixture-key', 'bearer', true);
    assert.equal(persistent.size, savedPersistent.size + 1);
    context.CommandCodeBridgeSaveAuth(key, 'bearer', remember);
    assert.deepEqual(persistent, savedPersistent);
    assert.deepEqual(JSON.parse(JSON.stringify(context.CommandCodeBridgeAuthHeaders())), { Authorization: `Bearer ${key}` });
    assert.deepEqual(await headers({ ...options, session: new Map() }), {});
    assert.deepEqual(await headers({ ...otherOptions, session: new Map() }), { Authorization: 'Bearer other-fixture-key' });
  }
});

test('invalid storage and blocked storage leave the page usable', () => {
  const context = loadAuth({ blocked: true });
  assert.doesNotThrow(() => context.CommandCodeBridgeSaveAuth(key, 'bearer'));
  assert.doesNotThrow(() => context.CommandCodeBridgeClearAuth());
  assert.equal(context.CommandCodeBridgeAuthHeaders().Authorization, `Bearer ${key}`);
  const session = new Map();
  const corrupt = loadAuth({ session });
  corrupt.CommandCodeBridgeSaveAuth(key, 'bearer');
  session.set([...session.keys()][0], '{broken');
  assert.equal(corrupt.CommandCodeBridgeAuthHeaders().Authorization, `Bearer ${key}`);
});

test('a delayed rejection does not clear a newer manual key', () => {
  const session = new Map(), persistent = new Map();
  const context = loadAuth({ session, persistent });
  const panelStorage = new Map(persistent);
  context.CommandCodeBridgeSaveAuth(key, 'bearer', true);
  context.CommandCodeBridgeSaveAuth('new-fixture-key', 'bearer', true);
  const savedSession = new Map(session), savedPersistent = new Map(persistent);
  context.CommandCodeBridgeClearAuth({ Authorization: `Bearer ${key}` });
  assert.deepEqual(session, savedSession);
  assert.deepEqual(persistent, savedPersistent);
  assert.equal(context.CommandCodeBridgeAuthHeaders().Authorization, 'Bearer new-fixture-key');
  assert.equal(loadAuth({ persistent }).CommandCodeBridgeAuthHeaders().Authorization, 'Bearer new-fixture-key');
  context.CommandCodeBridgeClearAuth({ Authorization: 'Bearer new-fixture-key' });
  assert.equal(session.size, 0);
  assert.deepEqual(persistent, panelStorage);
});

