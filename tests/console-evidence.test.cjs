const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const {test} = require('node:test');
const script = fs.readFileSync(require('node:path').join(__dirname,'../internal/bridge/ui/console.js'),'utf8');
function fixture(api) {
 const nodes = new Map();
 const context = {state:{authenticated:true,key:'fixture',credentials:[{id:'one',label:'one'},{id:'two',label:'two'}]},
  window:{},api,renderModels:()=>{},encodeURIComponent,
  diagnosticSources:{plugin_test:'插件直测',cpa_request:'CPA 请求'}, diagnosticIdentity:()=>context.state.key,
  esc:x=>String(x??'').replace(/</g,'&lt;'),shortWhen:x=>x,when:x=>x,fmt:x=>x,
  $:id=>{if(!nodes.has(id))nodes.set(id,{});return nodes.get(id)}};
 vm.createContext(context);
 vm.runInContext(script.slice(script.indexOf('const modelEvidenceLabels='),script.indexOf('function renderModels()')),context);
 return context;
}
test('evidence selector discards delayed results from another credential',async()=>{
 const pending=[];const c=fixture(()=>new Promise(resolve=>pending.push(resolve)));
 const first=c.loadModelEvidence();c.state.modelCredential='two';const second=c.loadModelEvidence();
 pending[1]({checked_at:'now',items:[{model:'second'}]});await second;
 pending[0]({checked_at:'old',items:[{model:'first'}]});await first;
 assert.equal(c.state.modelEvidence.items[0].model,'second');
});
test('late evidence after logout or login identity change is discarded',async()=>{
 let resolve;const c=fixture(()=>new Promise(done=>{resolve=done}));const p=c.loadModelEvidence();
 c.state.key='changed';resolve({items:[{model:'private'}]});await p;
 assert.equal(c.state.modelEvidence,null);
});
test('evidence escapes protocol and distinguishes source, expiry and generic denial',()=>{
 const c=fixture();c.state.modelEvidence={items:[{model:'alias',status:'permission_unknown',evidence:[
  {source:'plugin_test',status:'expired',client_protocol:'<img>',upstream_protocol:'messages'},
  {source:'cpa_request',status:'unknown'}]}]};
 const html=c.modelEvidenceDetailHTML('alias');
 for(const text of ['权限拒绝，原因未知','证据已过期','插件直测','CPA 请求'])assert.ok(html.includes(text));
 assert.ok(!html.includes('<img>'));
});
test('model permission failures do not log out management access',async()=>{
 const c=fixture();Object.assign(c,{AbortSignal,apiURL:x=>x,openDialog:()=>{},resetDiagnosticViews:()=>{throw Error('unexpected logout')},
 fetch:async()=>({ok:false,status:403,text:async()=>' {"error":"model permission denied"}'})});
 vm.runInContext(script.slice(script.indexOf('async function api('),script.indexOf('async function confirmed(')),c);
 await assert.rejects(c.api('/models/test'),/model permission denied/);
 assert.equal(c.state.authenticated,true);
});
