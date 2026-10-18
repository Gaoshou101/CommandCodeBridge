const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const {test} = require('node:test');
const source = fs.readFileSync(require('node:path').join(__dirname, '../internal/bridge/ui/console.js'), 'utf8');
const escape = value => String(value ?? '').replace(/[&<>"']/g, c => `&#${c.charCodeAt(0)};`);
function functions(start, end, context) {
 vm.createContext(context);
 vm.runInContext(source.slice(source.indexOf(start), source.indexOf(end)), context);
 return context;
}
test('model filters combine evidence with case-insensitive aliases and retain original action indices', () => {
 const c = functions('function filteredModels(', 'function renderModels()', {state:{modelEvidence:{items:[{model:'second',status:'permission_unknown'},{model:'third',status:'expired'}]}}});
 const models = [{id:'first',upstream_id:'provider/one'},{id:'second',name:'Claude alias',upstream_id:'provider/two'},{id:'third',upstream_id:'provider/three'}];
 assert.equal(c.filteredModels(models,' CLAUDE ', 'attention')[0].index, 1);
 assert.equal(c.filteredModels(models,'provider', 'unknown').map(x=>x.index).join(','),'0,2');
 assert.equal(c.filteredModels(models,'first', 'available').length,0);
 assert.equal(c.filteredModels(models,'', 'all').length,3);
});
test('request drawer escapes long errors and only renders approved metadata fields', () => {
 const c = functions('function logPresentation(', 'async function loadLogs()', {esc:escape,fmt:String,when:String,latencyHTML:()=>''});
 const html = c.logDetailHTML({id:'<request>',model:'alias',status:403,error:'<script>'+ 'long-error '.repeat(1000),error_kind:'<bad>',usage_reported:false,prompt_tokens:100,messages:'PRIVATE_BODY',api_key:'PRIVATE_KEY',thinking_signature:'PRIVATE_SIGNATURE'});
 for(const hidden of ['<script>','<request>','<bad>','PRIVATE_BODY','PRIVATE_KEY','PRIVATE_SIGNATURE']) assert.ok(!html.includes(hidden));
 for(const visible of ['路由与协议','时延与用量','权限拒绝','long-error','缓存写入 tokens']) assert.ok(html.includes(visible));
 assert.ok(!html.includes('<dd>100</dd>'));
});
function logs(api) {
 const nodes = new Map();
 const c = {state:{authenticated:true,key:'one',offset:0,logs:[{id:'cached'}]},api,URLSearchParams,fmt:String,diagnosticIdentity:()=>c.state.key,logHTML:x=>x.id,
 $:id=>{if(!nodes.has(id))nodes.set(id,{value:id==='logStatus'?'all':'',innerHTML:id==='logRows'?'cached':'',setAttribute(){},removeAttribute(){}});return nodes.get(id)}};
 return functions('async function loadLogs()', 'async function refresh()', c);
}
test('delayed logs from another management identity cannot restore private rows',async()=>{
 let resolve; const c=logs(()=>new Promise(done=>resolve=done));const p=c.loadLogs();
 c.state.key='two'; c.$('logRows').innerHTML=''; resolve({items:[{id:'private'}],total:1});await p;
 assert.equal(c.$('logRows').innerHTML,'');
});
test('latest log refresh wins and failed refresh preserves cached rows with a visible status',async()=>{
 const pending=[];const c=logs(()=>new Promise((resolve,reject)=>pending.push({resolve,reject})));
 const first=c.loadLogs(),second=c.loadLogs();pending[1].resolve({items:[{id:'new'}],total:1});await second;
 pending[0].resolve({items:[{id:'old'}],total:1});await first;assert.equal(c.$('logRows').innerHTML,'new');
 const failure=c.loadLogs();pending[2].reject(Error('offline'));await assert.rejects(failure,/offline/);
 assert.equal(c.$('logRows').innerHTML,'new');assert.equal(c.$('logLoadStatus').hidden,false);assert.match(c.$('logLoadStatus').textContent,/保留上次结果/);
});
test('closing a drawer after list refresh restores focus to its replacement trigger',()=>{
 let focused='';const old={isConnected:false,dataset:{logDetail:'request'}},replacement={dataset:{logDetail:'request'},focus(){focused='replacement'}};
 const dialog={open:false,querySelectorAll:()=>[],showModal(){this.open=true}};
 const document={activeElement:old,querySelector:()=>null,querySelectorAll:()=>[replacement]};
 const c=functions('const dialogOrigins=', 'function apiURL(',{state:{view:'logs'},document,$:()=>dialog});
 c.openDialog('detailDialog');c.restoreDialogFocus('detailDialog');assert.equal(focused,'replacement');
});
test('closing an older dialog never steals focus from the next modal',()=>{
 let focused=false;const dialog={open:false,querySelectorAll:()=>[],showModal(){this.open=true}};
 const document={activeElement:{isConnected:true,dataset:{},focus(){focused=true}},querySelector:()=>({open:true})};
 const c=functions('const dialogOrigins=', 'function apiURL(',{state:{view:'models'},document,$:()=>dialog});
 c.openDialog('detailDialog');c.restoreDialogFocus('detailDialog');assert.equal(focused,false);
});

test('focus restoration skips the hidden desktop trigger after a mobile layout change',()=>{
 let focused='';const old={isConnected:false,dataset:{modelDetail:'2'}};
 const hidden={dataset:{modelDetail:'2'},getClientRects:()=>[],focus(){focused='hidden'}};
 const visible={dataset:{modelDetail:'2'},getClientRects:()=>[{}],focus(){focused='visible'}};
 const dialog={open:false,querySelectorAll:()=>[],showModal(){this.open=true}};
 const document={activeElement:old,querySelector:()=>null,querySelectorAll:()=>[hidden,visible]};
 const c=functions('const dialogOrigins=', 'function apiURL(',{state:{view:'models'},document,$:()=>dialog});
 c.openDialog('detailDialog');c.restoreDialogFocus('detailDialog');assert.equal(focused,'visible');
});

test('pending storage and recovery rows escape filenames and preserve original observation times',()=>{
 const nodes=new Map();const c={$:id=>{if(!nodes.has(id))nodes.set(id,{});return nodes.get(id)},esc:escape,fmt:String,when:x=>x||'未知'};
 functions('function statusRows(', 'function apiURL(',c);
 vm.runInContext(source.slice(source.indexOf('function renderStorage('),source.indexOf('async function loadStorage(')),c);
 c.renderStorage({precision:'minute/day',last_success_at:'original time',pending_files:['usage.json'],pending_since:'pending time',statistics_buckets:7,recovery_warnings:[{file:'<file>',message:'<script>history gap</script>',checked_at:'recovery time'}]});
 const html=c.$('storageStatus').innerHTML;
 for(const value of ['original time','pending time','usage.json','history gap','recovery time','恢复后的历史可能不完整'])assert.ok(html.includes(value));
 assert.ok(!html.includes('<file>')&&!html.includes('<script>'));
});

test('catalog and test dialogs cannot reopen after management logout during loading',async()=>{
 let resolve;const state={authenticated:true,key:'old',catalog:[]};const c={state,diagnosticIdentity:()=>state.key,api:()=>new Promise(done=>resolve=done),loadCredentials:()=>new Promise(done=>resolve=done),openDialog:()=>assert.fail('stale dialog opened'),globalError:()=>assert.fail('stale error rendered')};
 functions('async function showCatalog(', 'function catalogFiltered(',c);
 const catalog=c.showCatalog();state.authenticated=false;state.key='';resolve({models:[{id:'private'}]});await catalog;assert.equal(state.catalog.length,0);
 vm.runInContext(source.slice(source.indexOf('async function showTest('),source.indexOf("document.querySelectorAll('dialog')")),c);
 state.authenticated=true;state.key='new';const test=c.showTest(0);state.authenticated=false;resolve();await test;
});

test('late direct-test responses never restore output or evidence after logout',async()=>{
 let resolve;const nodes=new Map(),state={authenticated:true,key:'one'},button={};
 const c={state,diagnosticIdentity:()=>state.key,$:id=>{if(!nodes.has(id))nodes.set(id,{});return nodes.get(id)},api:()=>new Promise(done=>resolve=done),FormData:class{*[Symbol.iterator](){yield ['model','mock']}},toast:()=>assert.fail('stale success'),loadModelEvidence:()=>assert.fail('stale refresh'),dialogError:()=>assert.fail('stale error')};
 vm.createContext(c);vm.runInContext(source.slice(source.indexOf("$('testForm').onsubmit="),source.indexOf("$('settingsForm').onsubmit=")),c);
 const request=c.$('testForm').onsubmit({preventDefault(){},currentTarget:{querySelector:()=>button}});
 state.authenticated=false;state.key='two';c.$('testResult').textContent='';resolve({response:'private'});await request;
 assert.equal(c.$('testResult').textContent,'');assert.equal(button.disabled,false);
});

test('authentication reset closing a confirmation resolves it as canceled',async()=>{
 const nodes=new Map();const dialog=new EventTarget();dialog.open=true;dialog.close=()=>{dialog.open=false;dialog.dispatchEvent(new Event('close'))};
 const c={$:id=>{if(id==='confirmDialog')return dialog;if(!nodes.has(id))nodes.set(id,{});return nodes.get(id)},openDialog:()=>{dialog.open=true}};
 functions('async function confirmed(', 'function period(',c);
 const pending=c.confirmed('Confirm','Change');dialog.close();assert.equal(await pending,false);
 const next=c.confirmed('Confirm','Change');c.$('confirmYes').onclick();assert.equal(await next,true);
});
