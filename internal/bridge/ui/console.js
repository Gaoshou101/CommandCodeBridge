'use strict';
const $=id=>document.getElementById(id), esc=v=>String(v??'').replace(/[&<>"']/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
const state={view:'overview',config:null,credentials:[],activeCredential:null,catalog:[],selected:new Set(),offset:0,total:0,key:'',authType:'bearer',authenticated:false,blocked:false,busy:0};
const API_BASE=document.querySelector('meta[name="cpa-api-base"]').content;
const compactNumber=v=>v==null?'—':Number(v).toLocaleString('en',{notation:'compact',maximumFractionDigits:1});
const shortWhen=v=>v?new Date(v).toLocaleString('zh-CN',{month:'2-digit',day:'2-digit',hour:'2-digit',minute:'2-digit',hour12:false}):'未知';
const fmt=(v,d=0)=>v===null||v===undefined?'—':Number(v).toLocaleString('zh-CN',{maximumFractionDigits:d}), when=v=>v?new Date(v).toLocaleString('zh-CN'):'未知', percent=(a,b)=>a!=null&&b>0?Math.max(0,Math.min(100,a/b*100)):null;
function toast(text){$('toast').textContent=text;$('toast').hidden=false;clearTimeout(toast.timer);toast.timer=setTimeout(()=>$('toast').hidden=true,3500)}
function globalError(e){$('globalError').textContent=e.message||String(e);$('globalError').hidden=false}
function dialogError(id,e){$(id).querySelector('.form-error').textContent=e.message||String(e)}
function openDialog(id){$(id).querySelectorAll('.form-error').forEach(x=>x.textContent='');if(!$(id).open)$(id).showModal()}
function apiURL(path){return new URL(API_BASE+path,location.origin).href}
async function api(path,options={}){if(state.blocked&&!options.verify)throw new Error('管理访问已暂停，请重新验证登录');const headers={...(window.CommandCodeBridgeAuthHeaders?.()||{}),...options.headers};if(state.key){delete headers.Authorization;delete headers['X-Management-Key'];headers[state.authType==='x-management-key'?'X-Management-Key':'Authorization']=state.authType==='x-management-key'?state.key:'Bearer '+state.key}if(options.body!==undefined)headers['Content-Type']='application/json';state.busy++;$('busy').textContent='正在更新';try{const response=await fetch(apiURL(path),{method:options.method||'GET',headers,body:options.body===undefined?undefined:JSON.stringify(options.body),signal:AbortSignal.timeout(options.timeout||650000)});const text=await response.text();let data;try{data=JSON.parse(text)}catch{data={error:text.slice(0,200)}}if(!response.ok){if(response.status===401||response.status===403){state.authenticated=false;state.blocked=true;if(response.status===401){window.CommandCodeBridgeClearAuth?.(headers);state.key=''}openDialog('authDialog')}throw new Error(data.error||`HTTP ${response.status}`)}return data}finally{state.busy--;$('busy').textContent=state.busy?'正在更新':''}}
async function confirmed(title,text){$('confirmTitle').textContent=title;$('confirmText').textContent=text;openDialog('confirmDialog');return new Promise(resolve=>{let settled=false;const end=value=>{if(settled)return;settled=true;$('confirmDialog').close();resolve(value)};$('confirmCancel').onclick=()=>end(false);$('confirmYes').onclick=()=>end(true);$('confirmDialog').oncancel=()=>end(false)})}
function period(){const range=$('statsRange').value;let from='',to='';if(range==='today'){const date=new Date();date.setHours(0,0,0,0);from=date.toISOString()}else if(range==='24h')from=new Date(Date.now()-86400000).toISOString();else if(range==='custom'){if($('statsFrom').value)from=new Date($('statsFrom').value).toISOString();if($('statsTo').value)to=new Date($('statsTo').value).toISOString()}const q=new URLSearchParams();if(from)q.set('from',from);if(to)q.set('to',to);return q.size?'?'+q.toString():''}
async function loadConfig(){state.config=await api('/config');renderModels();const f=$('settingsForm');for(const k of ['timeout_seconds','log_retention','usage_refresh_seconds'])f.elements[k].value=state.config[k];f.elements.max_response_mb.value=state.config.max_response_bytes/1048576;f.elements.zdr.checked=state.config.zdr;$('dataDir').textContent=state.config.data_dir;const model=state.config.models[0]?.id||'你的模型名称';$('apiExample').textContent=JSON.stringify({model,messages:[{role:'user',content:'你好'}],stream:true},null,2)}
async function loadCredentials(){state.credentials=(await api('/credentials')).credentials;renderCredentials();renderHealth()}
async function loadStats(){const d=await api('/statistics'+period()),t=d.totals;const metrics=[['请求数',fmt(t.requests),`${fmt(t.success)} 成功 · ${fmt(t.failed)} 失败`],['输入 / 输出 tokens',compactNumber(t.input_tokens)+' / '+compactNumber(t.output_tokens),`${fmt(t.with_usage)} 次有用量记录`],['缓存命中',d.cache_hit_percent==null?'—':fmt(d.cache_hit_percent,1)+'%',`${fmt(t.cached_tokens)} tokens 读取`],['输出速率',d.output_tokens_per_second==null?'—':fmt(d.output_tokens_per_second,1)+' tok/s',d.average_ttft_ms==null?'首字时间未知':'首字 '+fmt(d.average_ttft_ms/1000,2)+'s']];$('summary').innerHTML=metrics.map(([name,value,hint],i)=>`<div><span class="muted">${name}</span><b ${i===1?`class="token-total" title="${fmt(t.input_tokens)} 输入 / ${fmt(t.output_tokens)} 输出 tokens"`:""}>${value}</b><small>${hint}</small></div>`).join('');const rows=Object.values(d.models).sort((a,b)=>b.requests-a.requests);$('modelStats').innerHTML=rows.length?`<table><thead><tr><th>模型</th><th>请求</th><th>成功</th><th>输入 tokens</th><th>输出 tokens</th><th>缓存 tokens</th></tr></thead><tbody>${rows.map(x=>`<tr><td class="code">${esc(x.model)}</td><td>${fmt(x.requests)}</td><td>${fmt(x.success)}</td><td>${fmt(x.input_tokens)}</td><td>${fmt(x.output_tokens)}</td><td>${fmt(x.cached_tokens)}</td></tr>`).join('')}</tbody></table>`:'<div class="empty"><strong>暂无请求</strong>发起请求后显示用量</div>';if(d.storage_error)globalError(new Error(d.storage_error))}
const healthLabels={healthy:['正常','good'],warning:['需关注','warn'],invalid:['鉴权异常','bad'],cooldown:['冷却中','warn'],disabled:['已停用','neutral'],unknown:['待验证','neutral']};
function healthBadge(h){const [label,tone]=healthLabels[h?.status]||healthLabels.unknown;return `<span class="badge ${tone}" title="${esc(h?.reason||'等待查询')}"><i class="status-dot" aria-hidden="true"></i>${label}</span>`}
function renderHealth(){
  const rows=state.credentials;
  if(!rows.length){$('credentialHealth').innerHTML='<div class="empty">暂无凭据</div>';return}
  const healthy=rows.filter(c=>c.health?.status==='healthy').length,enabled=rows.filter(c=>!c.disabled).length;
  const counts=Object.fromEntries(Object.keys(healthLabels).map(k=>[k,rows.filter(c=>(c.health?.status||'unknown')===k).length]));
  const bar=Object.entries(counts).filter(([,n])=>n).map(([k,n])=>`<span class="${k}" style="width:${n/rows.length*100}%" title="${healthLabels[k][0]} ${n}"></span>`).join('');
  const legend=Object.entries(counts).filter(([,n])=>n).map(([k,n])=>`<span>${healthLabels[k][0]} ${n}</span>`).join('');
  $('credentialHealth').innerHTML=`<div class="health-layout"><div class="health-score"><span>正常 / 已启用</span><div class="health-ratio">${healthy}<small> / ${enabled}</small></div><div class="health-bar" aria-hidden="true">${bar}</div><div class="health-legend">${legend}</div></div><div class="health-list">${rows.map(c=>{
    const h=c.health||{},recent=h.recent_requests||0;
    const detail=h.cooldown_until?'重试 '+shortWhen(h.cooldown_until):['invalid','warning'].includes(h.status)?h.reason||'查询异常':h.checked_at?'验证于 '+shortWhen(h.checked_at):h.status==='disabled'?'不参与调度':'尚未验证';
    const request=h.last_request_at?`最近请求 ${h.last_status} · ${shortWhen(h.last_request_at)}`:'暂无近期请求';
    return `<div class="health-row"><div><div class="health-name">${esc(c.label)}</div><div class="small" title="${esc(h.reason||'')}">${esc(detail)}</div></div><div class="health-meta"><div>${esc(request)}</div>${recent?`<div class="small">${fmt(recent)} 次请求 · ${fmt(h.recent_failed)} 次失败</div>`:''}</div>${healthBadge(h)}</div>`
  }).join('')}</div></div>`;
}
function windowHTML(w,name){
  const p=percent(w?.used,w?.cap),unknown=p==null;
  return `<div class="quota-window"><div class="meter-label"><span>${name}</span><span class="muted">${unknown?'未知':fmt(p,1)+'%'}</span></div><div class="quota-value">${fmt(w?.used,2)} <small>/ ${fmt(w?.cap,2)} credits</small></div><div class="meter ${unknown?'unknown':p>=100?'over':''}" ${unknown?'aria-hidden="true"':`role="meter" aria-label="${name}使用率" aria-valuemin="0" aria-valuemax="100" aria-valuenow="${p}"`}><span style="width:${p||0}%"></span></div><div class="quota-note" title="重置 ${esc(when(w?.reset_at))}">${w?'':'窗口未知 · '}重置 ${esc(shortWhen(w?.reset_at))}</div></div>`;
}
function monthlyHTML(u){
  const w=(u.windows||[]).find(w=>w.name==='monthly');
  if(w)return windowHTML(w,'月额度窗口');
  return `<div class="quota-window"><div class="meter-label"><span>月额度窗口</span><span class="muted">剩余</span></div><div class="quota-value">${fmt(u.monthly_credits,3)} <small>credits</small></div><div class="meter unknown" aria-hidden="true"></div><div class="quota-note" title="重置 ${esc(when(u.subscription_end))}">已用 ${fmt(u.monthly_used_credits,3)} · 上限未知<br>重置 ${esc(shortWhen(u.subscription_end))}</div></div>`;
}
function renderCredentials(){
  const rows=state.credentials;
  if(!rows.length){state.activeCredential=null;$('credentialRows').innerHTML='<div class="panel empty"><strong>暂无凭据</strong><button class="primary" data-add-account>添加凭据</button></div>';return}
  if(!rows.some(c=>c.id===state.activeCredential))state.activeCredential=(rows.find(c=>!c.disabled)||rows[0]).id;
  const c=rows.find(c=>c.id===state.activeCredential),u=c.usage||{},windows=u.windows||[],plan=u.plan||'套餐未知';
  const unavailable=(u.unavailable||[]).map(x=>({credits:'余额',subscription:'订阅',summary:'本期用量'}[x]||x)).join('、');
  const selector=rows.map(c=>`<button class="account-choice ${c.id===state.activeCredential?'selected':''}" data-account-select="${esc(c.id)}" aria-pressed="${c.id===state.activeCredential}"><i class="status-dot ${esc(c.health?.status||'unknown')}" aria-hidden="true"></i><span class="account-choice-copy"><strong>${esc(c.label)}</strong><small>${esc(c.usage?.plan||'套餐未知')} · ${(healthLabels[c.health?.status]||healthLabels.unknown)[0]}</small></span></button>`).join('');
  $('credentialRows').innerHTML=`<div class="credential-workspace"><div class="account-selector" aria-label="选择凭据"><div class="selector-heading"><span>凭据</span><span>${rows.length}</span></div>${selector}</div><article class="account"><div class="account-top"><div><div class="account-title"><strong>${esc(c.label)}</strong>${healthBadge(c.health)}</div><div class="small">${esc(plan)} · 优先级 ${c.priority}${c.shared_credentials>1?' · '+c.shared_credentials+' 把凭据共享额度':''}</div></div><div class="actions"><button class="compact" data-account-refresh="${esc(c.id)}" ${c.disabled?'disabled':''}>刷新</button><button class="quiet compact" data-account-edit="${esc(c.id)}">编辑</button><button class="quiet compact danger" data-account-delete="${esc(c.id)}">删除</button></div></div><div class="account-grid"><div class="balance-cell"><div><div class="small">剩余额度</div><div class="balance">${fmt(u.remaining_credits,3)}<small>credits</small></div></div><div class="balance-parts"><div><span>月度</span><b>${fmt(u.monthly_credits,3)}</b></div><div><span>购买</span><b>${fmt(u.purchased_credits,3)}</b></div><div><span>免费</span><b>${fmt(u.free_credits,3)}</b></div></div></div>${monthlyHTML(u)}${windowHTML(windows.find(w=>w.name==='fiveHour'),'5 小时窗口')}${windowHTML(windows.find(w=>w.name==='weekly'),'每周窗口')}</div><div class="account-foot"><span>更新 ${esc(shortWhen(u.updated_at))}${u.account_requests==null?'':' · '+fmt(u.account_requests)+' 次请求'}</span><span>订阅到期 ${esc(shortWhen(u.subscription_end))}</span>${u.status==='error'?`<div class="error-text">更新失败：${esc(u.error)}</div>`:''}${u.status==='partial'?`<div class="usage-warning">${esc(unavailable)}暂不可用，显示上次数据</div>`:''}</div></article></div>`;
}
function renderModels(){const models=state.config?.models||[];$('modelRows').innerHTML=models.length?`<table><thead><tr><th>客户端名称 / 上游模型</th><th>协议</th><th>思考映射</th><th>操作</th></tr></thead><tbody>${models.map((m,i)=>`<tr><td><div class="code">${esc(m.id)}</div><div class="small code">${esc(m.upstream_id)}</div></td><td>${m.supported_endpoints.includes('/chat/completions')?'OpenAI':'Anthropic'}<div class="small">${m.context_length?fmt(m.context_length)+' 上下文':''}</div></td><td>${m.reasoning_mapping.enabled?`<span class="badge">已启用</span><div class="small">${esc(Object.entries(m.reasoning_mapping.rules||{}).map(([a,b])=>a+' → '+b).join('，')||'全部透传')}</div>`:'<span class="muted">透传</span>'}</td><td><div class="actions"><button class="compact" data-model-test="${i}">测试</button><button class="compact" data-model-edit="${i}">编辑</button><button class="compact danger" data-model-delete="${i}">移除</button></div></td></tr>`).join('')}</tbody></table>`:'<div class="empty"><strong>暂无模型</strong><button class="primary" data-fetch-catalog>获取模型</button></div>'}
function latencyHTML(label,ms,local,threshold){
  const tone=ms==null||local?'':ms>=threshold*3?'bad':ms>=threshold?'warn':'good';
  return `<div class="latency-line ${tone}"><span>${label}</span><strong>${ms==null||local?'—':fmt(ms/1000,2)+'s'}</strong></div>`;
}
function logHTML(x){
  const local=x.upstream_skipped,success=x.status>=200&&x.status<400;
  const status=local?'本地退避':x.status===429?'限流':x.status===401?'鉴权失败':x.status===403?'权限拒绝':success?'成功':'失败';
  const tone=local||x.status===429?'warn':success?'good':'bad';
  const date=x.time?new Date(x.time):null;
  const day=date?date.toLocaleDateString('zh-CN',{month:'2-digit',day:'2-digit'}):'未知';
  const time=date?date.toLocaleTimeString('zh-CN',{hour12:false}):'—';
  const reasoning=x.reasoning_from===x.reasoning_to?(x.reasoning_from||'unset'):(x.reasoning_from||'unset')+' → '+(x.reasoning_to||'unset');
  const provider=local?'未调用上游':!x.provider||x.provider==='unknown'?'上游未报告':x.provider;
  const usage=x.usage_reported&&!local;
  const speed=x.output_tokens_per_second==null||local?'—':fmt(x.output_tokens_per_second,1)+' tok/s';
  const details=[['请求 ID',x.id],['上游模型',x.upstream_model],['实际凭据',x.credential||x.credential_id],['实际上游',provider],['思考映射',reasoning],['响应状态',x.status],['流终态',x.stream_end||'—'],['重试时间',x.retry_at?when(x.retry_at):'—'],['输入 tokens',usage?fmt(x.prompt_tokens):'—'],['输出 tokens',usage?fmt(x.completion_tokens):'—'],['缓存读取 tokens',usage?fmt(x.cached_tokens):'—'],['缓存写入 tokens',usage?fmt(x.cache_write_tokens):'—'],['思考 tokens',usage?fmt(x.reasoning_tokens):'—'],['首字耗时',x.ttft_ms==null||local?'—':fmt(x.ttft_ms/1000,2)+'s'],['总耗时',local?'—':fmt(x.duration_ms/1000,2)+'s'],['输出速率',speed]];
  return `<details class="log-entry" data-id="${esc(x.id)}"><summary class="log-main"><div class="log-cell"><span class="cell-label">时间</span><div>${esc(time)}</div><div class="small">${esc(day)}</div></div><div class="log-cell"><span class="cell-label">模型</span><div class="code log-model">${esc(x.model)}</div><div class="small code">${esc(x.upstream_model||'—')}</div></div><div class="log-cell"><span class="cell-label">思考强度</span><span class="badge neutral">${esc(reasoning)}</span></div><div class="log-cell"><span class="cell-label">结果</span><span class="badge ${tone}">${status}</span><div class="small">${x.status} · ${x.stream?'流式':'非流式'}</div></div><div class="log-cell"><span class="cell-label">凭据 / 渠道</span><div>${esc(x.credential||'未知凭据')}</div><div class="small">${esc(provider)}</div></div><div class="log-cell"><span class="cell-label">首字 / 耗时</span><div class="log-latency">${latencyHTML('首字',x.ttft_ms,local,1500)}${latencyHTML('耗时',x.duration_ms,local,10000)}</div></div><div class="log-cell"><span class="cell-label">用量 / 速率</span><div>${usage?fmt(x.prompt_tokens)+' / '+fmt(x.completion_tokens):'—'}</div><div class="small">缓存 ${usage?fmt(x.cached_tokens):'—'} · ${speed}</div></div><span class="chevron" aria-hidden="true">›</span></summary><div class="log-detail"><h3>请求详情 · ${esc(when(x.time))}</h3><dl class="detail-grid">${details.map(([k,v])=>`<div><dt>${k}</dt><dd>${esc(v??'—')}</dd></div>`).join('')}</dl>${x.error?`<div class="log-error"><strong>${esc(x.error_kind||'请求错误')}</strong><pre>${esc(x.error)}</pre></div>`:''}${local?'<p class="small" style="margin-top:14px">本地冷却，未调用上游。</p>':''}</div></details>`;
}
async function loadLogs(){
  const revision=state.logRevision=(state.logRevision||0)+1;
  const q=new URLSearchParams({offset:state.offset,limit:50,search:$('logSearch').value.trim(),status:$('logStatus').value});
  $('logRows').setAttribute('aria-busy','true');
  try{
    const d=await api('/logs?'+q);if(revision!==state.logRevision)return;
    const rows=d.items;state.total=d.total;
    if(state.offset>=d.total&&state.offset>0){state.offset=Math.max(0,Math.ceil(d.total/50)-1)*50;return loadLogs()}
    $('logCount').textContent=`${fmt(d.total)} 条记录`;
    $('logPage').textContent=`第 ${Math.floor(state.offset/50)+1} / ${Math.max(1,Math.ceil(d.total/50))} 页`;
    $('prevLogs').disabled=state.offset===0;$('nextLogs').disabled=state.offset+50>=d.total;
    $('logRows').innerHTML=rows.length?rows.map(logHTML).join(''):'<div class="panel empty"><strong>暂无匹配记录</strong>调整筛选条件后重试</div>';
  }catch(e){if(revision===state.logRevision){$('logRows').innerHTML='<div class="panel empty"><strong>日志读取失败</strong>重新登录后刷新</div>';throw e}}
  finally{if(revision===state.logRevision)$('logRows').removeAttribute('aria-busy')}
}
async function refresh(){
  if(!state.authenticated)return;
  try{
    $('globalError').hidden=true;
    if(state.view==='overview')await Promise.all([loadStats(),loadCredentials()]);
    if(state.view==='credentials')await loadCredentials();
    if(state.view==='models'||state.view==='settings')await loadConfig();
    if(state.view==='logs')await loadLogs();
  }catch(e){globalError(e)}
}
const viewNames={overview:'运行概览',credentials:'凭据与用量',models:'模型映射',logs:'请求日志',settings:'设置'};
const mobileNav=matchMedia('(max-width: 767px)');
function syncSidebar(){
  const opened=mobileNav.matches?document.body.classList.contains('sidebar-open'):!document.body.classList.contains('sidebar-collapsed');
  $('sidebarToggle').setAttribute('aria-expanded',String(opened));
  $('sidebarToggle').setAttribute('aria-label',opened?'收起导航':'展开导航');
  $('sidebar').inert=mobileNav.matches&&!opened;
  $('sidebarScrim').hidden=!mobileNav.matches||!opened;
}
function closeSidebar(){document.body.classList.remove('sidebar-open');syncSidebar()}
$('sidebarToggle').onclick=()=>{
  document.body.classList.toggle(mobileNav.matches?'sidebar-open':'sidebar-collapsed');syncSidebar();
  if(mobileNav.matches&&document.body.classList.contains('sidebar-open'))$('sidebar').querySelector('button.active').focus();
};
$('sidebarScrim').onclick=()=>{closeSidebar();$('sidebarToggle').focus()};
mobileNav.addEventListener('change',()=>{closeSidebar()});
document.addEventListener('keydown',e=>{
  if(!mobileNav.matches||!document.body.classList.contains('sidebar-open'))return;
  if(e.key==='Escape'){e.preventDefault();closeSidebar();$('sidebarToggle').focus()}
  if(e.key==='Tab'){
    const focusable=[...$('sidebar').querySelectorAll('a,button')],first=focusable[0],last=focusable.at(-1);
    if(e.shiftKey&&document.activeElement===first){e.preventDefault();last.focus()}
    else if(!e.shiftKey&&document.activeElement===last){e.preventDefault();first.focus()}
  }
});
function navigate(view){
  if(!viewNames[view])return;
  state.view=view;document.querySelectorAll('.view').forEach(x=>x.hidden=x.id!==view);
  document.querySelectorAll('nav button').forEach(x=>{x.classList.toggle('active',x.dataset.view===view);if(x.dataset.view===view)x.setAttribute('aria-current','page');else x.removeAttribute('aria-current')});
  $('currentView').textContent=viewNames[view];
  if(mobileNav.matches&&document.body.classList.contains('sidebar-open')){closeSidebar();$('sidebarToggle').focus()}
  refresh();
}
$('sidebar').querySelector('.brand').onclick=e=>{e.preventDefault();navigate('overview')};
document.querySelectorAll('nav button').forEach(b=>{b.title=viewNames[b.dataset.view];b.setAttribute('aria-label',viewNames[b.dataset.view])});
syncSidebar();
function editAccount(cid){const f=$('credentialForm');f.reset();const c=state.credentials.find(c=>c.id===cid);f.elements.id.value=cid||'';f.elements.api_key.required=!c;if(c){f.elements.label.value=c.label;f.elements.priority.value=c.priority;f.elements.disabled.checked=c.disabled}$('credentialTitle').textContent=c?'编辑凭据':'添加凭据';openDialog('credentialDialog')}
const defaultAlias=upstream=>upstream.trim().split('/').pop();
let lastSuggestedAlias='';
$('modelForm').elements.upstream_id.oninput=e=>{const f=$('modelForm');if(f.elements.index.value!==''||f.elements.id.value&&f.elements.id.value!==lastSuggestedAlias)return;lastSuggestedAlias=defaultAlias(e.target.value);f.elements.id.value=lastSuggestedAlias};
function editModel(index){const f=$('modelForm');f.reset();lastSuggestedAlias='';const m=state.config?.models[index];f.elements.index.value=m?index:'';if(m){f.elements.id.value=m.id;f.elements.upstream_id.value=m.upstream_id;f.elements.name.value=m.name||'';f.elements.endpoint.value=m.supported_endpoints.includes('/chat/completions')?'/chat/completions':'/messages';f.elements.thinking_mode.value=m.thinking_mode||"adaptive";f.elements.reasoning_enabled.checked=m.reasoning_mapping.enabled;f.elements.rules.value=JSON.stringify(m.reasoning_mapping.rules||{},null,2)}$('modelTitle').textContent=m?'编辑模型':'添加模型';openDialog('modelDialog')}
async function saveModels(models){const aliases=new Set();for(const m of models){if(aliases.has(m.id))throw new Error('客户端名称重复：'+m.id+'。请减少所选模型，或手动添加并设置不同名称。');aliases.add(m.id)}await api('/models',{method:'PUT',body:{models}});await loadConfig()}
async function showCatalog(){try{state.catalog=(await api('/models/refresh',{method:'POST',body:{},timeout:20000})).models;state.selected.clear();$('catalogSearch').value='';renderCatalog();openDialog('catalogDialog')}catch(e){globalError(e)}}
function catalogFiltered(){const text=$('catalogSearch').value.toLowerCase();const existing=new Set((state.config?.models||[]).map(m=>m.upstream_id));return state.catalog.filter(m=>!existing.has(m.upstream_id)&&(!text||(m.name+' '+m.upstream_id).toLowerCase().includes(text)))}
function renderCatalog(){const rows=catalogFiltered();$('catalogRows').innerHTML=rows.length?rows.map(m=>`<label><input type="checkbox" data-catalog-id="${esc(m.upstream_id)}" ${state.selected.has(m.upstream_id)?'checked':''}><span><span class="code">${esc(m.id)}</span><span class="small" style="display:block">${esc(m.name||'')} · ${esc(m.upstream_id)}</span></span><span class="small">${m.supported_endpoints.includes('/messages')?'Anthropic':'OpenAI'}</span></label>`).join(''):'<div class="empty">没有可导入的模型。</div>';$('importCatalog').textContent=`导入所选${state.selected.size?'（'+state.selected.size+'）':''}`}
async function showTest(index){await loadCredentials();const m=state.config.models[index],f=$('testForm');f.reset();f.elements.model.value=m.id;$('testModelName').textContent=m.id;f.elements.credential_id.innerHTML=state.credentials.filter(c=>!c.disabled).map(c=>`<option value="${esc(c.id)}">${esc(c.label)}</option>`).join('');$('testResult').hidden=true;openDialog('testDialog')}
document.querySelectorAll('[data-view]').forEach(b=>b.onclick=()=>navigate(b.dataset.view));document.querySelectorAll('[data-jump]').forEach(b=>b.onclick=()=>navigate(b.dataset.jump));document.querySelectorAll('[data-close]').forEach(b=>b.onclick=()=>$(b.dataset.close).close());document.querySelectorAll('[data-action="refresh"]').forEach(b=>b.onclick=refresh);
$('authButton').onclick=()=>{ $('rememberAuth').checked=window.CommandCodeBridgeRememberAuth?.()||false;openDialog('authDialog')};$('clearAuth').onclick=()=>{window.CommandCodeBridgeClearAuth?.();state.key='';state.authenticated=false;state.blocked=true;$('managementKey').value='';toast('已退出登录')};
$('authForm').onsubmit=async e=>{e.preventDefault();state.key=$('managementKey').value.trim();state.authType=$('authType').value;try{const status=await api('/status',{verify:true,timeout:15000});window.CommandCodeBridgeSaveAuth?.(state.key,state.authType,$('rememberAuth').checked);state.authenticated=true;state.blocked=false;$('managementKey').value='';$('authDialog').close();$('version').textContent='v'+status.version;await loadConfig();await loadCredentials();await refresh();toast('已登录')}catch(e){dialogError('authDialog',e)}};
$('credentialForm').onsubmit=async e=>{e.preventDefault();const f=e.currentTarget,cid=f.elements.id.value,body={label:f.elements.label.value,priority:Number(f.elements.priority.value),disabled:f.elements.disabled.checked};if(f.elements.api_key.value.trim())body.api_key=f.elements.api_key.value.trim();try{await api('/credentials'+(cid?'?id='+encodeURIComponent(cid):''),{method:cid?'PUT':'POST',body});f.elements.api_key.value='';$('credentialDialog').close();await loadCredentials();toast('凭据已保存')}catch(e){dialogError('credentialDialog',e)}};
$('modelForm').onsubmit=async e=>{e.preventDefault();const f=e.currentTarget;try{const rules=JSON.parse(f.elements.rules.value);if(!rules||Array.isArray(rules)||typeof rules!=='object')throw new Error('映射规则必须是 JSON 对象');const index=f.elements.index.value,old=index===''?{}:state.config.models[Number(index)],m={...old,id:f.elements.id.value.trim(),upstream_id:f.elements.upstream_id.value.trim(),name:f.elements.name.value.trim(),supported_endpoints:[f.elements.endpoint.value],thinking_mode:f.elements.endpoint.value==="/messages"?f.elements.thinking_mode.value:"",reasoning_mapping:{enabled:f.elements.reasoning_enabled.checked,rules}};const models=structuredClone(state.config.models);if(index==='')models.push(m);else models[Number(index)]=m;await saveModels(models);$('modelDialog').close();toast('模型已保存')}catch(e){dialogError('modelDialog',e)}};
$('testForm').onsubmit=async e=>{e.preventDefault();const f=e.currentTarget,b=f.querySelector('[type=submit]');b.disabled=true;$('testResult').hidden=false;$('testResult').textContent='等待模型回复…';try{const result=await api('/models/test',{method:'POST',body:Object.fromEntries(new FormData(f))});$('testResult').textContent=JSON.stringify(result.response,null,2);toast('测试完成')}catch(e){$('testResult').textContent='';dialogError('testDialog',e)}finally{b.disabled=false}};
$('settingsForm').onsubmit=async e=>{e.preventDefault();const f=e.currentTarget;try{await api('/config',{method:'PUT',body:{timeout_seconds:Number(f.elements.timeout_seconds.value),log_retention:Number(f.elements.log_retention.value),usage_refresh_seconds:Number(f.elements.usage_refresh_seconds.value),max_response_bytes:Number(f.elements.max_response_mb.value)*1048576,zdr:f.elements.zdr.checked}});await loadConfig();toast('设置已保存')}catch(e){globalError(e)}};
$('addCredential').onclick=()=>editAccount();$('addModel').onclick=()=>editModel();$('fetchModels').onclick=showCatalog;$('catalogSearch').oninput=renderCatalog;$('selectCatalog').onclick=()=>{catalogFiltered().forEach(m=>state.selected.add(m.upstream_id));renderCatalog()};$('catalogRows').onchange=e=>{const id=e.target.dataset.catalogId;if(!id)return;if(e.target.checked)state.selected.add(id);else state.selected.delete(id);$('importCatalog').textContent='导入所选（'+state.selected.size+'）'};
$('importCatalog').onclick=async()=>{try{if(!state.selected.size)throw new Error('请先选择模型');await saveModels([...state.config.models,...state.catalog.filter(m=>state.selected.has(m.upstream_id))]);$('catalogDialog').close();toast('模型已导入')}catch(e){dialogError('catalogDialog',e)}};
$('credentialRows').onclick=async e=>{const b=e.target.closest('button');if(!b)return;if(b.hasAttribute('data-add-account'))return editAccount();if(b.dataset.accountSelect){state.activeCredential=b.dataset.accountSelect;renderCredentials();[...$('credentialRows').querySelectorAll('[data-account-select]')].find(x=>x.dataset.accountSelect===state.activeCredential)?.focus();return}if(b.dataset.accountEdit)return editAccount(b.dataset.accountEdit);try{if(b.dataset.accountRefresh){b.disabled=true;await api('/credentials/usage?id='+encodeURIComponent(b.dataset.accountRefresh)+'&refresh=1',{timeout:20000});await loadCredentials()}if(b.dataset.accountDelete&&await confirmed('删除凭据','此凭据会从 CPA 移除，后续请求将使用其他可用账号。')){await api('/credentials?id='+encodeURIComponent(b.dataset.accountDelete),{method:'DELETE'});await loadCredentials();toast('凭据已删除')}}catch(e){globalError(e)}finally{b.disabled=false}};
$('modelRows').onclick=async e=>{const b=e.target.closest('button');if(!b)return;if(b.hasAttribute('data-fetch-catalog'))return showCatalog();if(b.dataset.modelEdit!==undefined)return editModel(Number(b.dataset.modelEdit));try{if(b.dataset.modelTest!==undefined)return await showTest(Number(b.dataset.modelTest));if(b.dataset.modelDelete!==undefined&&await confirmed('移除模型映射','客户端将无法再通过此名称调用模型。')){await saveModels(state.config.models.filter((_,i)=>i!==Number(b.dataset.modelDelete)));toast('模型映射已移除')}}catch(e){globalError(e)}};
$('refreshUsage').onclick=async()=>{const b=$('refreshUsage');b.disabled=true;try{for(const c of state.credentials.filter(c=>!c.disabled))await api('/credentials/usage?id='+encodeURIComponent(c.id)+'&refresh=1',{timeout:20000});await loadCredentials();toast('用量已刷新')}catch(e){globalError(e)}finally{b.disabled=false}};
$('clearAffinity').onclick=async()=>{if(!await confirmed('重新分配会话','清除本插件账号的旧会话绑定。下一条请求按当前优先级重新选账号，正在生成的回复继续完成。'))return;try{await api('/credentials/clear-affinity',{method:'POST',body:{},timeout:30000});toast('会话已重新分配')}catch(e){globalError(e)}};
$('resetStats').onclick=async()=>{if(!await confirmed('清空统计','统计将从零开始，请求日志保留。插件会保存原统计的恢复副本。'))return;try{await api('/statistics/reset',{method:'POST',body:{}});await loadStats();toast('统计已清空')}catch(e){globalError(e)}};
$('logStatus').onchange=()=>{state.offset=0;loadLogs().catch(globalError)};
$('searchLogs').onclick=()=>{state.offset=0;loadLogs().catch(globalError)};$('logSearch').onkeydown=e=>{if(e.key==='Enter')$('searchLogs').click()};$('prevLogs').onclick=()=>{state.offset=Math.max(0,state.offset-50);loadLogs().catch(globalError)};$('nextLogs').onclick=()=>{state.offset+=50;loadLogs().catch(globalError)};$('applyStats').onclick=()=>loadStats().catch(globalError);$('statsRange').onchange=()=>{$('statsFrom').hidden=$('statsTo').hidden=$('statsRange').value!=='custom'};
function setTheme(theme,save=false){document.documentElement.dataset.theme=theme;if(save)localStorage.setItem('commandcodebridge:theme',theme)}try{let theme=localStorage.getItem('commandcodebridge:theme');if(!theme&&parent!==window&&parent.location.origin===location.origin){const root=parent.document.documentElement;theme=root.classList.contains('dark')||root.dataset.theme==='dark'?'dark':'light'}setTheme(theme||(matchMedia('(prefers-color-scheme: dark)').matches?'dark':'light'))}catch{setTheme('light')}$('themeButton').onclick=()=>setTheme(document.documentElement.dataset.theme==='dark'?'light':'dark',true);
setInterval(()=>{if(state.authenticated&&!state.blocked&&!state.busy&&!document.hidden&&['credentials','overview'].includes(state.view))loadCredentials().catch(globalError)},15000);
(async()=>{try{const h=window.CommandCodeBridgeAuthHeaders?.()||{};if(!Object.keys(h).length){openDialog('authDialog');return}const status=await api('/status',{verify:true,timeout:15000});state.authenticated=true;$('version').textContent='v'+status.version;await loadConfig();await loadCredentials();await refresh()}catch(e){globalError(e)}})();
