const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const { test } = require('node:test');
const script = fs.readFileSync(path.join(__dirname, '../internal/bridge/ui/console.js'), 'utf8');
function fixture(api = async () => ({})) {
  const nodes = new Map();
  const context = {
    state: { authenticated: true, key: 'fixture-key', authType: 'bearer' },
    window: {}, api, when: value => value,
    esc: value => String(value ?? '').replace(/[&<>"']/g, character => `&#${character.charCodeAt(0)};`),
    $: id => { if (!nodes.has(id)) nodes.set(id, {}); return nodes.get(id); },
  };
  vm.createContext(context);
  vm.runInContext(script.slice(script.indexOf('const diagnosticStatus='), script.indexOf("$('runDiagnostics').onclick")), context);
  return context;
}
function report(label) {
  return { checked_at: '2026-10-18', models: 0, host_message: 'host checked', credentials: [
    { label, status: 'missing', message: 'not loaded', action: 'retry', evidence: [
      { source: 'account_query', status: 'ok' }, { source: 'plugin_test', status: 'unknown' }, { source: 'cpa_request', status: 'unknown' },
    ] },
  ] };
}
test('diagnostic output escapes labels and distinguishes account, direct test and CPA evidence', () => {
  const context = fixture();
  const html = context.renderDiagnostics(report('<img onerror=alert(1)>'));
  assert.ok(!html.includes('<img'));
  for (const text of ['账户查询', '插件直测', 'CPA 请求', '暂无可确认记录', '尚未配置模型映射']) assert.ok(html.includes(text));
  const compatibility = context.renderCompatibility({checked_at: 'today', version: '0.2.10', checks: [{name: 'snapshot_backend', status: 'unknown', message: '<script>secret</script>', action: 'verify'}]});
  assert.ok(compatibility.includes('未确认') && !compatibility.includes('<script>'));
});
test('late diagnostics cannot overwrite a newer report', async () => {
  const pending = [];
  const context = fixture(() => new Promise(resolve => pending.push(resolve)));
  const first = context.loadDiagnostics(), second = context.loadDiagnostics();
  pending[1](report('new account')); await second;
  pending[0](report('old account')); await first;
  assert.ok(context.$('diagnosticResults').innerHTML.includes('new account'));
  assert.ok(!context.$('diagnosticResults').innerHTML.includes('old account'));
});
test('logout and changed management identity discard in-flight results', async () => {
  let resolve;
  const context = fixture(() => new Promise(done => { resolve = done; }));
  const request = context.loadDiagnostics();
  context.resetDiagnosticViews(); context.state.key = 'new-key';
  resolve(report('previous account')); await request;
  assert.equal(context.$('diagnosticResults').textContent, '登录后重新诊断');
  assert.equal(context.$('diagnosticResults').innerHTML, undefined);
});

test('rejected management access immediately clears rendered diagnostics', async () => {
  const context = fixture();
  Object.assign(context, {
    fetch: async () => ({ok: false, status: 401, text: async () => '{"error":"unauthorized"}'}),
    apiURL: value => value, AbortSignal, openDialog: () => {},
  });
  vm.runInContext(script.slice(script.indexOf('async function api('), script.indexOf('async function confirmed(')), context);
  await assert.rejects(context.api('/diagnostics'), /unauthorized/);
  assert.equal(context.state.authenticated, false);
  assert.equal(context.$('diagnosticResults').textContent, '登录后重新诊断');
  assert.equal(context.$('compatibilityResults').textContent, '登录后重新检查');
});
