import assert from 'node:assert/strict';
import test from 'node:test';
import { environment, CRMError, baseSession, account, lead, deferred, nodes, textOf, settle } from './helpers/crm-harness.mjs';
const event = { preventDefault() {} };
const customer = (id='A', overrides={}) => ({id,name:`Customer ${id}`,phone:'13800000000',wechat:'wx-contact',address:'Sample address',buildingArea:120,buildingType:'新建',expectedBuildTime:'2027-03',remark:'Existing note',version:1,stage:'following',lifecycle:'protected',category:'建房咨询',intent:'high',ownerId:'wx1',ownerUsername:'crm1',nextFollowupAt:'2027-01-01T00:00:00Z',protectionExpiresAt:'2026-10-12T00:00:00Z',poolAt:null,customerNumber:`KH${id}`,demand:'Existing demand',...overrides});
const detail = c => ({customer:c,relations:[],followups:[],attachments:[],sources:[],cases:[]});
const form = h => h.find(n=>n.type==='form');
const submit = h => form(h).props.onSubmit(event);
const area = h => h.find(n=>n.type==='textarea');
const input = (h,label) => h.find(n=>n.type==='input'&&n.props['aria-label']===label);
const editName = (h,value) => { input(h,'客户姓名').props.onChange({target:{value}});h.render(); };
const click = (h,label) => {const result=h.button(label).props.onClick();h.render();return result;};
function crmFixture(env,c=customer()) {
 const calls=[];const base=env.crm;
 env.crm=async(url,options={})=>{if(options.method){calls.push({url,...options});return c;}if(url==='/customers/'+c.id)return detail(c);if(url==='/employees'||url==='/owners')return [account(1)];return base(url,options)};
 return calls;
}

test('report edit: 409 retains draft and original version until explicit latest-version review',async()=>{
 const env=environment();let c=customer();const writes=[];let fail=true;
 env.crm=async(url,o={})=>{if(o.method){writes.push(o);if(fail)throw new CRMError(409,'该联系电话已存在客户档案');return c;}return detail(c)};
 const h=await env.mount('customerReportForm.tsx','default',{customer:c,cancel(){},saved(){}});editName(h,'My draft');
 const oldSubmit=form(h).props.onSubmit;await oldSubmit(event);h.render();assert.equal(h.button('保存资料').props.disabled,true);assert.equal(input(h,'客户姓名').props.value,'My draft');
 await oldSubmit(event);assert.equal(writes.length,1);
 c=customer('A',{version:2,name:'Latest name',phone:'13900000000'});await click(h,'读取最新报备资料');h.render();
 assert.match(h.text(),/13900000000/);assert.equal(h.button('保存资料').props.disabled,true);assert.equal(writes[0].body.version,1);
 click(h,'已核对，保留输入继续编辑');fail=false;await submit(h);h.render();assert.equal(writes[1].body.version,2);assert.equal(writes[1].body.report.name,'My draft');
 await submit(h);assert.equal(writes.length,2);
});

test('report edit: explicit reload replaces input; failed or redirected latest detail never grants save',async()=>{
 const env=environment();let latest=detail(customer('A',{version:4,name:'Latest'}));env.crm=async(url,o={})=>{if(o.method)throw new CRMError(409,'conflict');return latest};
 const h=await env.mount('customerReportForm.tsx','default',{customer:customer(),cancel(){},saved(){}});editName(h,'Draft');await submit(h);h.render();
 latest=detail(customer('B'));await click(h,'读取最新报备资料');h.render();assert.match(h.text(),/档案已合并或目标已变化/);assert.equal(h.button('保存资料').props.disabled,true);
 latest=detail(customer('A',{version:4,name:'Latest'}));await click(h,'读取最新报备资料');h.render();click(h,'重新载入最新资料');assert.equal(input(h,'客户姓名').props.value,'Latest');
});

test('report: duplicate same-frame create submits once and a failing completion callback cannot imply an unsaved write',async()=>{
 const env=environment();const pending=deferred();let writes=0;env.crm=()=>{writes++;return pending.promise};
 const h=await env.mount('customerReportForm.tsx','default',{cancel(){},saved(){throw new Error('render failed')}});
 const handler=form(h).props.onSubmit;const first=handler(event);await handler(event);assert.equal(writes,1);pending.resolve(customer());await first;h.render();
 assert.match(h.text(),/资料已保存，但界面更新失败/);await handler(event);assert.equal(writes,1);assert.equal(h.button('提交报备').props.disabled,true);
});

test('report: late A write and latest-detail reads cannot populate a later B form or call its saved callback',async()=>{
 const env=environment();const pending=deferred();let saved=0;env.crm=()=>pending.promise;
 const h=await env.mount('customerReportForm.tsx','default',{customer:customer(),cancel(){},saved(){saved++}});const first=submit(h);
 h.props={...h.props,customer:customer('B')};await h.flush();pending.resolve(customer());await first;h.render();assert.equal(saved,0);assert.equal(input(h,'客户姓名').props.value,'Customer B');
});

test('report review: same-frame duplicate review is locked and acknowledged review stays terminal',async()=>{
 const env=environment();const pending=deferred();let reviews=0;
 env.crm=async(url)=>{if(url==='/customers')throw new CRMError(409,'duplicate');reviews++;return pending.promise};
 const h=await env.mount('customerReportForm.tsx','default',{cancel(){},saved(){}});await submit(h);h.render();
 h.find(n=>n.type==='textarea'&&n.props.placeholder?.includes('共用电话')).props.onChange({target:{value:'Verified distinct people'}});h.render();
 const handler=h.button('提交核实申请').props.onClick;const first=handler();await handler();assert.equal(reviews,1);pending.resolve();await first;h.render();assert.match(h.text(),/无需重复提交/);await handler();assert.equal(reviews,1);assert.equal(h.button('提交报备').props.disabled,true);
});

test('customer form: explicit review uses latest version, offers reset, and locks duplicate writes',async()=>{
 const env=environment();const writes=[];let fail=true;const latest=customer('A',{version:3,demand:'Latest demand',ownerId:'wx2'});
 env.crm=async(url,o={})=>{if(o.method){writes.push(o);if(fail)throw new CRMError(409,'conflict');return latest}return detail(latest)};
 const h=await env.mount('customerManagement.tsx','CustomerForm',{customer:customer(),mode:'edit',files:[],cancel(){},saved(){}});
 area(h).props.onChange({target:{value:'My demand'}});h.render();await submit(h);h.render();assert.equal(h.button('保存资料').props.disabled,true);
 await click(h,'读取最新版本');h.render();click(h,'重新载入最新资料，清空当前输入');assert.equal(area(h).props.value,'Latest demand');
 fail=false;const handler=form(h).props.onSubmit;await handler(event);await handler(event);assert.equal(writes.length,2);assert.equal(writes[1].body.version,3);assert.equal(writes[1].body.ownerId,'wx2');
});

test('workflow: transfer keeps the editor snapshot after prop refresh and 409 requires explicit review',async()=>{
 const env=environment();const writes=[];let current=customer();let fail=true;
 env.crm=async(url,o={})=>{if(url==='/owners')return [account(1)];if(o.method){writes.push(o);if(fail)throw new CRMError(409,'归属已变化');return current;}return detail(current)};
 const h=await env.mount('customerWorkflow.tsx','WorkflowDetails',{detail:detail(current),supervisor:true,saved(){}});click(h,'分配 / 交接');area(h).props.onChange({target:{value:'My reason'}});h.render();
 current=customer('A',{version:5,ownerUsername:'crm2'});h.props={...h.props,detail:detail(current)};h.render();await submit(h);h.render();assert.equal(writes[0].body.version,1);assert.equal(area(h).props.value,'My reason');
 await submit(h);assert.equal(writes.length,1);await click(h,'读取最新档案核对');h.render();assert.match(h.text(),/crm2/);click(h,'已核对，保留操作输入');fail=false;await submit(h);h.render();assert.equal(writes[1].body.version,5);assert.equal(writes[1].body.reason,'My reason');
});

test('workflow merge: source and target versions are both reviewed; target identity mismatch blocks submission',async()=>{
 const env=environment();const writes=[];let source=customer(),target=customer('T',{version:7});let redirect=false,fail=true;
 env.crm=async(url,o={})=>{if(url==='/owners')return [];if(o.method){writes.push(o);if(fail)throw new CRMError(409,'version');return target;}if(url.endsWith('/T'))return detail(redirect?customer('OTHER'):target);return detail(source)};
 const h=await env.mount('customerWorkflow.tsx','WorkflowDetails',{detail:detail(source),supervisor:true,saved(){}});click(h,'关联同一客户');h.field('保留的主档 ID').props.onChange({target:{value:'T'}});h.render();
 redirect=true;await click(h,'读取目标主档');h.render();await submit(h);assert.equal(writes.length,0);
 redirect=false;await click(h,'读取目标主档');h.render();area(h).props.onChange({target:{value:'Verified same customer'}});h.render();await submit(h);h.render();
 source=customer('A',{version:2});target=customer('T',{version:8});await click(h,'读取最新档案核对');h.render();assert.match(h.text(),/目标主档 T；版本 8/);click(h,'已核对，保留操作输入');fail=false;await submit(h);assert.equal(writes[1].body.version,2);assert.equal(writes[1].body.targetVersion,8);
});

test('workflow: same-frame duplicate writes are locked and success is not relabeled after a callback failure',async()=>{
 const env=environment();const pending=deferred();let writes=0;env.crm=(url,o={})=>o.method?(writes++,pending.promise):Promise.resolve([]);
 const h=await env.mount('customerWorkflow.tsx','WorkflowDetails',{detail:detail(customer()),supervisor:true,saved(){throw new Error('refresh failed')}});click(h,'分配 / 交接');
 const handler=form(h).props.onSubmit;const first=handler(event);await handler(event);assert.equal(writes,1);pending.resolve();await first;h.render();assert.match(h.text(),/已保存，但界面更新失败/);await handler(event);assert.equal(writes,1);assert.equal(h.all(n=>n.type==='form').length,0);
});

test('legacy leads: write succeeds then detail read fails, leaving a saved notice and no resubmittable dialog',async()=>{
 const env=environment();const h=await env.mount('customerLeads.tsx');await click(h,'详情');await h.flush();click(h,'记录跟进');area(h).props.onChange({target:{value:'Done'}});h.render();
 const handler=h.button('保存跟进').props.onClick;env.leadApi.getCustomerLeadDetail=async()=>{throw new Error('detail unavailable')};handler();handler();await h.flush();
 assert.equal(env.leadWrites.length,1);assert.equal(h.all(n=>n.type==='textarea').length,0);assert.match(h.text(),/跟进已保存，但最新数据读取失败/);assert.equal(env.alerts.length,0);handler();await h.flush();assert.equal(env.leadWrites.length,1);
});

test('legacy leads: list refresh failure after acknowledged write never restores the completed editor',async()=>{
 const env=environment();const h=await env.mount('customerLeads.tsx');click(h,'跟进');area(h).props.onChange({target:{value:'Done'}});h.render();
 env.leadApi.getCustomerLeadList=async()=>{throw new Error('list unavailable')};h.button('保存跟进').props.onClick();await h.flush();assert.equal(env.leadWrites.length,1);assert.match(h.text(),/无需重复提交/);assert.equal(h.all(n=>n.type==='textarea').length,0);
 env.leadApi.getCustomerLeadList=async()=>({list:[lead],total:1});await click(h,'重新读取');await h.flush();assert.equal(env.leadWrites.length,1);
});

test('customer drawer: acknowledged claim survives detail refresh failure and retry only reads',async()=>{
 const env=environment();const c=customer('A',{poolAt:'2026-10-01T00:00:00Z',lifecycle:'pool'});let reads=0,writes=0;env.crm=async(url,o={})=>{if(url==='/employees')return [];if(o.method){writes++;return c}reads++;if(reads>1)throw new Error('detail unavailable');return detail(c)};
 const h=await env.mount('customerManagement.tsx','CustomerDrawer',{id:'A',supervisor:true,close(){},changed(){},checkPool(){}});await click(h,'重新报备并认领');await h.flush();assert.equal(writes,1);assert.match(h.text(),/本次操作已保存。最新档案读取失败/);
 const feedback=h.find(n=>typeof n.type==='function'&&n.type.name==='Feedback'&&n.props.error==='detail unavailable');feedback.props.retry();await h.flush();assert.equal(writes,1);
});

for(const now of ['2025-01-01','2030-01-01'])test(`pool capability: machine clock ${now} never grants an unverified claim`,async()=>{
 const env=environment();env.now=Date.parse(now);crmFixture(env);const h=await env.mount('customerManagement.tsx','CustomerDrawer',{id:'A',supervisor:true,close(){},changed(){},checkPool(){}});
 assert.equal(h.all(n=>n.type==='button'&&textOf(n)==='重新报备并认领').length,0);h.button('查看客户公海核对状态');h.button('记录跟进');
 const w=await env.mount('customerWorkflow.tsx','WorkflowDetails',{detail:detail(customer()),supervisor:false,saved(){}});assert.equal(w.all(n=>n.type==='input'&&n.props.type==='file').length,1);
});

test('pool capability: server pool-query evidence is tied to the exact customer and cannot pass through a merged redirect',async()=>{
 const env=environment();crmFixture(env);let h=await env.mount('customerManagement.tsx','CustomerDrawer',{id:'A',poolEvidence:{customerId:'A',version:1},supervisor:true,close(){},changed(){},checkPool(){}});h.button('重新报备并认领');
 h=await env.mount('customerManagement.tsx','CustomerDrawer',{id:'A',poolEvidence:{customerId:'B',version:1},supervisor:true,close(){},changed(){},checkPool(){}});assert.equal(h.all(n=>n.type==='button'&&textOf(n)==='重新报备并认领').length,0);
 env.crm=async url=>url==='/employees'?[]:detail(customer('B'));h=await env.mount('customerManagement.tsx','CustomerDrawer',{id:'A',poolEvidence:{customerId:'A',version:1},supervisor:true,close(){},changed(){},checkPool(){}});assert.equal(h.all(n=>n.type==='button'&&textOf(n)==='重新报备并认领').length,0);
});

test('pool capability: late A detail cannot reopen claim or transfer pool evidence after selecting B',async()=>{
 const env=environment();const a=deferred();env.crm=url=>url==='/employees'?Promise.resolve([]):url.endsWith('/A')?a.promise:Promise.resolve(detail(customer('B')));
 const h=await env.mount('customerManagement.tsx','CustomerDrawer',{id:'A',poolEvidence:{customerId:'A',version:1},supervisor:true,close(){},changed(){},checkPool(){}});h.props={...h.props,id:'B'};await h.flush();a.resolve(detail(customer()));await h.flush();
 assert.match(h.text(),/Customer B/);assert.equal(h.all(n=>n.type==='button'&&textOf(n)==='重新报备并认领').length,0);
});

test('pool rows attach server-query evidence only while their own query result is current',async()=>{
 const env=environment();const opened=[];let pending=null;env.crm=async(url,o={})=>{if(url==='/employees')return [];if(url==='/customers')return pending?pending.promise:{list:[customer()],total:1};throw new Error(url)};
 const h=await env.mount('customerManagement.tsx','Customers',{supervisor:true,tasks:false,initialQueue:'pool',open:(...args)=>opened.push(args),report(){}});click(h,'查看 / 认领');assert.deepEqual(JSON.parse(JSON.stringify(opened[0])),['A',{customerId:'A',version:1}]);
 pending=deferred();h.props={...h.props,initialQueue:'all'};await h.flush();assert.equal(h.all(n=>n.type==='button'&&textOf(n)==='查看 / 认领').length,0);pending.resolve({list:[customer('B')],total:1});await h.flush();click(h,'查看 / 跟进');assert.deepEqual(opened[1],['B',undefined]);
});

test('session cache: reset clears badges and rejects late publication, including the same user logging in again',()=>{
 const env=environment(),store=env.store;const a=store.getLeadOverviewSession();store.publishLeadOverview({todo_count:7},a);assert.equal(store.useLeadOverviewCounts().todo_count,7);
 store.resetLeadOverviewSession();assert.deepEqual({...store.useLeadOverviewCounts()},{});store.publishLeadOverview({todo_count:99},a);assert.deepEqual({...store.useLeadOverviewCounts()},{});
 store.resetLeadOverviewSession(7);const b=store.getLeadOverviewSession();store.publishLeadOverview({todo_count:5},b);store.publishLeadOverview({todo_count:99},a);assert.equal(store.useLeadOverviewCounts().todo_count,5);
});

test('session provider: logout, refresh and failed me checks invalidate overview counts immediately',async()=>{
 const env=environment();const h=await env.mount('component/ManagementSession.tsx','ManagementSessionProvider',{children:null});let ticket=env.store.getLeadOverviewSession();env.store.publishLeadOverview({todo_count:8},ticket);
 env.dispatch('management-session-expired');assert.deepEqual({...env.store.useLeadOverviewCounts()},{});env.store.publishLeadOverview({todo_count:9},ticket);assert.deepEqual({...env.store.useLeadOverviewCounts()},{});
 env.getMe=async()=>{throw new Error('session failed')};env.dispatch('management-session-changed');await h.flush();assert.equal(h.tree.props.value.user,null);assert.deepEqual({...env.store.useLeadOverviewCounts()},{});
});

test('session cache: a pending A overview cannot populate B page data or navigation badges',async()=>{
 const env=environment();const old=deferred();env.leadApi.getCustomerLeadOverview=()=>old.promise;const h=await env.mount('customerLeads.tsx');env.store.resetLeadOverviewSession(8);env.session={...baseSession(),user:{...baseSession().user,id:8}};
 env.leadApi.getCustomerLeadOverview=async()=>({todo_count:2,total_count:2});await h.flush();assert.equal(env.store.useLeadOverviewCounts().todo_count,2);old.resolve({todo_count:99,total_count:99});await h.flush();assert.equal(env.store.useLeadOverviewCounts().todo_count,2);
});

test('push retry: same-frame duplicate clicks submit once; acknowledged retry stays removed after read failure',async()=>{
 const env=environment();const pending=deferred();let writes=0,reads=0;env.crm=async(url,o={})=>{if(o.method){writes++;return pending.promise}reads++;if(reads>1)throw new Error('read failed');return {configured:true,failures:[{id:'N1',pushError:'failed'}],uncertain:[]}};
 const h=await env.mount('customerWorkflow.tsx','PushStatus');const handler=h.button('核实后重试').props.onClick;const first=handler();await handler();assert.equal(writes,1);pending.resolve();await first;await h.flush();assert.match(h.text(),/重试已提交/);assert.match(h.text(),/read failed/);assert.equal(h.all(n=>n.type==='button'&&textOf(n)==='核实后重试').length,0);await handler();assert.equal(writes,1);
});

test('push retry: failures release only their own lock and a successful refresh clears the old error',async()=>{
 const env=environment();let fail=true,writes=0;env.crm=async(url,o={})=>{if(o.method){writes++;if(fail)throw new CRMError(409,'只能重试失败通知');return;}return {configured:true,failures:[{id:'N1',pushError:'failed'}],uncertain:[{id:'N2'}]}};
 const h=await env.mount('customerWorkflow.tsx','PushStatus');await click(h,'核实后重试');h.render();assert.equal(h.button('核实后重试').props.disabled,false);await click(h,'刷新提醒状态');h.render();assert.doesNotMatch(h.text(),/只能重试失败通知/);fail=false;await click(h,'核实后重试');h.render();assert.equal(writes,2);assert.equal(h.all(n=>n.type==='button'&&textOf(n)==='核实后重试').length,0);
});

test('notice seen and sync retry both reject same-frame duplicate handlers',async()=>{
 const env=environment();let writes=0;const p=deferred();env.crm=async(url,o={})=>o.method?(writes++,p.promise):[{id:'N1',customerId:'A',content:'Notice',seen:false,createdAt:'2026-10-11'}];
 const notices=await env.mount('customerWorkflow.tsx','NoticePanel',{open(){}});const seen=notices.button('已读').props.onClick;const first=seen();await seen();assert.equal(writes,1);p.resolve();await first;notices.render();await seen();assert.equal(writes,1);
 const s=deferred();env.crm=async(url,o={})=>o.method?(writes++,s.promise):{configured:true,callbackConfigured:true,pending:0,failed:1,runs:[],events:[{id:'E1',status:'failed'}]};const sync=await env.mount('customerManagement.tsx','SyncPanel');const retry=sync.button('重试').props.onClick;const second=retry();await retry();assert.equal(writes,2);s.resolve();await second;
});

test('statistics: embedded high-intent count uses the pending queue and global scope is explicit under a user filter',async()=>{
 const env=environment();env.params=new URLSearchParams('user_id=5');env.leadApi.getCustomerLeadOverview=async()=>({total_count:20,todo_count:6,high_intent_count:12,high_todo_count:3});
 const h=await env.mount('customerLeads.tsx','default',{embedded:true});const card=h.find(n=>n.props?.className==='customer-lead-stat'&&textOf(n).includes('待跟进高意向'));assert.equal(nodes(card).find(n=>n.type==='strong').props.children,3);assert.match(h.text(),/全平台小程序线索概览，不随下方队列、用户或搜索条件变化/);
});

test('statistics: initial overview failure is unavailable, never a confirmed zero',async()=>{
 const env=environment();env.leadApi.getCustomerLeadOverview=async()=>{throw new Error('unavailable')};const h=await env.mount('customerLeads.tsx','default',{embedded:true});assert.match(h.text(),/线索统计尚未确认/);assert.equal(h.all(n=>n.props?.className==='customer-lead-stat').length,0);
});

test('unmount: late report/workflow writes and push status reads cannot write state or invoke completion',async()=>{
 for(const kind of ['report','workflow','push']){
  const env=environment();const pending=deferred();let saved=0;env.crm=(url,o={})=>url==='/owners'?Promise.resolve([]):pending.promise;
  const h=kind==='report'?await env.mount('customerReportForm.tsx','default',{customer:customer(),cancel(){},saved(){saved++}}):kind==='workflow'?await env.mount('customerWorkflow.tsx','WorkflowDetails',{detail:detail(customer()),supervisor:true,saved(){saved++}}):await env.mount('customerWorkflow.tsx','PushStatus');
  let first;if(kind==='report')first=submit(h);if(kind==='workflow'){click(h,'分配 / 交接');first=submit(h)}h.unmount();pending.resolve(kind==='push'?{configured:true,failures:[],uncertain:[]}:customer());await first;await settle();assert.equal(saved,0,kind);assert.equal(env.updatesAfterUnmount,0,kind);
 }
});

test('pool evidence expires after a successful claim changes the customer version',async()=>{
 const env=environment();let c=customer();let writes=0;env.crm=async(url,o={})=>{if(url==='/employees')return [];if(o.method){writes++;c=customer('A',{version:2,protectionExpiresAt:'2027-01-01T00:00:00Z'});return c;}return detail(c)};
 const h=await env.mount('customerManagement.tsx','CustomerDrawer',{id:'A',poolEvidence:{customerId:'A',version:1},supervisor:true,close(){},changed(){},checkPool(){}});
 const oldClaim=h.button('重新报备并认领').props.onClick;await oldClaim();await h.flush();assert.equal(writes,1);assert.equal(h.all(n=>n.type==='button'&&textOf(n)==='重新报备并认领').length,0);h.button('记录跟进');await oldClaim();assert.equal(writes,1);
});

test('report target switching is blocked before the new target form initialization effect runs',async()=>{
 const env=environment();let writes=0;env.crm=async()=>{writes++;return customer('B')};const h=await env.mount('customerReportForm.tsx','default',{customer:customer(),cancel(){},saved(){}});
 editName(h,'A draft');h.props={...h.props,customer:customer('B')};h.render();await submit(h);assert.equal(writes,0);await h.flush();assert.equal(input(h,'客户姓名').props.value,'Customer B');
});

test('legacy save detail completion after logout never refreshes the new session or writes after unmount',async()=>{
 const env=environment();const h=await env.mount('customerLeads.tsx');await click(h,'详情');await h.flush();click(h,'记录跟进');area(h).props.onChange({target:{value:'Done'}});h.render();const pending=deferred();let listReads=0;
 env.leadApi.getCustomerLeadDetail=()=>pending.promise;env.leadApi.getCustomerLeadList=async()=>{listReads++;return {list:[lead],total:1}};
 h.button('保存跟进').props.onClick();await settle();env.store.resetLeadOverviewSession(8);h.unmount();pending.resolve({lead,events:[],followups:[]});await settle();assert.equal(listReads,0);assert.equal(env.updatesAfterUnmount,0);
});

test('standalone overview labels distinguish full pending totals from today new records',async()=>{
 const env=environment();const h=await env.mount('customerLeadHub.tsx','default',{overview:{total_count:8,today_new_count:1,todo_count:6,high_intent_count:5,high_todo_count:2},loading:false,updatedAt:null,onRefresh(){}});
 assert.match(h.text(),/全量概览/);assert.match(h.text(),/今日新增1/);assert.match(h.text(),/待跟进高意向2/);
});

test('sync accepted writes cannot replay through an old handler while its refresh fails',async()=>{
 const env=environment();let writes=0,reads=0;env.crm=async(url,o={})=>{if(o.method){writes++;return;}reads++;if(reads>1)throw new Error('refresh failed');return {configured:true,callbackConfigured:true,pending:0,failed:1,runs:[],events:[{id:'E1',status:'failed'}]}};
 const h=await env.mount('customerManagement.tsx','SyncPanel');const retry=h.button('重试').props.onClick;await retry();await h.flush();await retry();assert.equal(writes,1);assert.match(h.text(),/任务已提交/);
});

for(const lifecycle of ['won','archived','pending'])test(`pool evidence cannot grant claim or release for terminal/excluded ${lifecycle} state even at the same version`,async()=>{
 const env=environment();const c=customer('A',{lifecycle,poolAt:'2026-10-01T00:00:00Z'});crmFixture(env,c);
 const evidence={customerId:'A',version:1};const h=await env.mount('customerManagement.tsx','CustomerDrawer',{id:'A',poolEvidence:evidence,supervisor:true,close(){},changed(){},checkPool(){}});
 assert.equal(h.all(n=>n.type==='button'&&textOf(n)==='重新报备并认领').length,0);
 const workflow=await env.mount('customerWorkflow.tsx','WorkflowDetails',{detail:detail(c),poolEvidence:evidence,supervisor:true,saved(){}});assert.equal(workflow.all(n=>n.type==='button'&&textOf(n)==='释放公海').length,0);
});

test('report edit: a server ownership rejection preserves the draft and requires a fresh review',async()=>{
 const env=environment();let writes=0;env.crm=async(url,o={})=>{if(o.method){writes++;throw new CRMError(403,'无权修改此客户')}throw new CRMError(403,'无权查看此客户')};
 const h=await env.mount('customerReportForm.tsx','default',{customer:customer(),cancel(){},saved(){}});editName(h,'Keep this draft');const handler=form(h).props.onSubmit;await handler(event);h.render();await handler(event);assert.equal(writes,1);assert.equal(input(h,'客户姓名').props.value,'Keep this draft');await click(h,'读取最新报备资料');h.render();assert.equal(h.button('保存资料').props.disabled,true);assert.match(h.text(),/无权查看此客户/);
});

test('legacy post-save refresh uses the newest query even before its replacement effect runs',async()=>{
 const env=environment(),pending=deferred();const queries=[];
 env.leadApi.getCustomerLeadList=async query=>{queries.push(query);return {list:[{...lead,id:query.status==='converted'?2:1,name:query.status==='converted'?'Converted result':'Original result',status:query.status||'new'}],total:1}};
 env.leadApi.updateCustomerLeadStatus=()=>pending.promise;
 const h=await env.mount('customerLeads.tsx');click(h,'跟进');area(h).props.onChange({target:{value:'Saved'}});h.render();h.button('保存跟进').props.onClick();
 h.find(n=>n.type==='select'&&nodes(n).some(o=>o.type==='option'&&o.props.children==='全部状态')).props.onChange({target:{value:'converted'}});h.render(); // Deliberately do not run useEffect yet.
 pending.resolve();await settle();h.render();assert.equal(queries.at(-1).status,'converted');assert.match(h.text(),/Converted result/);await h.flush();assert.equal(queries.at(-1).status,'converted');
});

test('legacy query identity rejects an older read between render and the next filter effect',async()=>{
 const env=environment(),old=deferred();let reads=0;env.leadApi.getCustomerLeadList=async query=>{reads++;if(reads===2)return old.promise;return {list:[{...lead,name:query.status==='converted'?'Converted result':'Original result'}],total:1}};
 const h=await env.mount('customerLeads.tsx','default',{embedded:true});click(h,'刷新');
 h.find(n=>n.type==='select'&&nodes(n).some(o=>o.type==='option'&&o.props.children==='全部状态')).props.onChange({target:{value:'converted'}});h.render();
 old.resolve({list:[{...lead,name:'Stale result must not land'}],total:1});await settle();h.render();assert.doesNotMatch(h.text(),/Stale result must not land/);await h.flush();assert.match(h.text(),/Converted result/);
});

async function drawerWithChild(kind,{fail=false,pool=false}={}) {
 const env=environment(),pending=deferred();let writes=0,closed=0,checked=0;const changes=[];const c=customer('A',pool?{lifecycle:'pool',poolAt:'2026-10-01'}:{});
 env.crm=async(url,o={})=>{if(o.method){writes++;return pending.promise}return url==='/employees'||url==='/owners'?[]:detail(c)};
 const drawer=await env.mount('customerManagement.tsx','CustomerDrawer',{id:'A',supervisor:true,close(){closed++},changed(){},checkPool(){checked++},pendingChanged(value){changes.push(value)}});
 const close=drawer.find(n=>n.type==='button'&&n.props['aria-label']==='关闭客户详情').props.onClick;
 const check=drawer.button('查看客户公海核对状态').props.onClick;
 let child;
 if(kind==='edit'||kind==='report'){
  click(drawer,kind==='edit'?'编辑资料':'编辑报备资料');const childNode=drawer.find(n=>typeof n.type==='function'&&n.type.name===(kind==='edit'?'CustomerForm':'CustomerReportForm'));
  child=await env.mount(kind==='edit'?'customerManagement.tsx':'customerReportForm.tsx',kind==='edit'?'CustomerForm':'default',childNode.props);
 } else if(kind==='workflow') {
  const childNode=drawer.find(n=>typeof n.type==='function'&&n.type.name==='WorkflowDetails');child=await env.mount('customerWorkflow.tsx','WorkflowDetails',childNode.props);click(child,'分配 / 交接');
 }
 const first=kind==='claim'?drawer.button('重新报备并认领').props.onClick():submit(child);
 return {env,drawer,child,pending,first,close,check,changes,writes:()=>writes,closed:()=>closed,checked:()=>checked};
}

for(const kind of ['edit','report'])test(`drawer shared gate: pending ${kind} cannot close, cancel or reopen to submit again`,async()=>{
 const x=await drawerWithChild(kind);x.close();x.child.props.cancel();x.drawer.render();
 assert.equal(x.closed(),0);assert.equal(x.writes(),1);assert.equal(x.drawer.find(n=>n.type==='button'&&n.props['aria-label']==='关闭客户详情').props.disabled,true);
 const expected=kind==='edit'?'CustomerForm':'CustomerReportForm';x.drawer.find(n=>typeof n.type==='function'&&n.type.name===expected);await submit(x.child);assert.equal(x.writes(),1);assert.deepEqual(x.changes,[true]);
 x.pending.reject(new CRMError(409,'Changed while editing'));await x.first;x.child.render();x.drawer.render();assert.deepEqual(x.changes,[true,false]);assert.equal(x.closed(),0);x.close();assert.equal(x.closed(),1);
});

test('drawer shared gate: workflow writes block detail Escape, header, mode switch and pool navigation',async()=>{
 const x=await drawerWithChild('workflow');const edit=x.drawer.button('编辑资料').props.onClick;
 x.close();x.check();edit();x.env.events.get('keydown').forEach(handler=>handler({key:'Escape'}));x.drawer.render();assert.equal(x.closed(),0);assert.equal(x.checked(),0);assert.equal(x.drawer.all(n=>typeof n.type==='function'&&n.type.name==='CustomerForm').length,0);assert.equal(x.writes(),1);
 x.pending.resolve(customer('A',{version:2}));await x.first;await x.drawer.flush();assert.deepEqual(x.changes,[true,false]);assert.equal(x.closed(),0);x.close();assert.equal(x.closed(),1);
});

test('drawer shared gate: claim owns the same pending lock through header, Escape and pool exits',async()=>{
 const x=await drawerWithChild('claim',{pool:true});x.close();x.check();x.env.events.get('keydown').forEach(handler=>handler({key:'Escape'}));assert.equal(x.closed(),0);assert.equal(x.checked(),0);
 const workflowNode=x.drawer.find(n=>typeof n.type==='function'&&n.type.name==='WorkflowDetails');const child=await x.env.mount('customerWorkflow.tsx','WorkflowDetails',workflowNode.props);click(child,'分配 / 交接');assert.equal(child.all(n=>n.type==='form').length,0);assert.equal(x.writes(),1);
 x.pending.reject(new CRMError(409,'Already claimed'));await x.first;await x.drawer.flush();assert.deepEqual(x.changes,[true,false]);x.close();assert.equal(x.closed(),1);
});

test('drawer shared gate: forced unmount ignores late completion while the lease releases exactly once',async()=>{
 const x=await drawerWithChild('edit');x.child.unmount();x.drawer.unmount();x.pending.resolve(customer('A',{version:2}));await x.first;assert.deepEqual(x.changes,[true,false]);assert.equal(x.env.updatesAfterUnmount,0);assert.equal(x.closed(),0);
});

for(const lifecycle of ['protected','pool'])test(`drawer identity: returned ${lifecycle} B for requested A exposes no edit, workflow or claim entry`,async()=>{
 const env=environment(),writes=[];let closed=0;
 env.crm=async(url,o={})=>{if(o.method){writes.push(url);return}return url==='/employees'?[]:detail(customer('B',{lifecycle,poolAt:lifecycle==='pool'?'2026-10-01':null}))};
 const drawer=await env.mount('customerManagement.tsx','CustomerDrawer',{id:'A',poolEvidence:{customerId:'A',version:1},supervisor:true,close(){closed++},changed(){},checkPool(){}});
 assert.match(drawer.text(),/请求 A，返回 B/);assert.match(drawer.text(),/档案可能已合并/);
 for(const label of ['编辑资料','编辑报备资料','记录跟进','重新报备并认领'])assert.equal(drawer.all(n=>n.type==='button'&&textOf(n)===label).length,0);
 assert.equal(drawer.all(n=>typeof n.type==='function'&&['CustomerForm','CustomerReportForm','WorkflowDetails'].includes(n.type.name)).length,0);
 await click(drawer,'重新读取当前档案');await drawer.flush();assert.deepEqual(writes,[]);click(drawer,'关闭并核对客户列表');assert.equal(closed,1);
});

for(const kind of ['edit','report','workflow'])test(`drawer identity: cached real ${kind} child cannot write while reloading or after a merged redirect`,async()=>{
 const env=environment(),writes=[];let returned=customer();
 env.crm=async(url,o={})=>{if(o.method){writes.push(url);return}return url==='/employees'||url==='/owners'?[]:detail(returned)};
 const drawer=await env.mount('customerManagement.tsx','CustomerDrawer',{id:'A',supervisor:true,close(){},changed(){},checkPool(){}});
 if(kind!=='workflow')click(drawer,kind==='edit'?'编辑资料':'编辑报备资料');
 const name=kind==='edit'?'CustomerForm':kind==='report'?'CustomerReportForm':'WorkflowDetails';
 const node=drawer.find(n=>typeof n.type==='function'&&n.type.name===name);
 const child=await env.mount(kind==='edit'?'customerManagement.tsx':kind==='report'?'customerReportForm.tsx':'customerWorkflow.tsx',kind==='report'?'default':name,node.props);
 if(kind==='workflow')click(child,'分配 / 交接');
 const cachedSubmit=form(child).props.onSubmit;
 drawer.find(n=>typeof n.type==='function'&&n.type.name==='Feedback'&&n.props.retry).props.retry();drawer.render();
 await cachedSubmit(event);assert.deepEqual(writes,[]); // Query identity is already invalid before the replacement effect.
 returned=customer('B',{lifecycle:'pool',poolAt:'2026-10-01'});await drawer.flush();await cachedSubmit(event);
 assert.deepEqual(writes,[]);assert.match(drawer.text(),/请求 A，返回 B/);assert.equal(drawer.all(n=>typeof n.type==='function'&&n.type.name===name).length,0);
});

test('drawer identity: cached claim cannot follow a canonical redirect or a new selection',async()=>{
 const env=environment(),writes=[];let returned=customer('A',{lifecycle:'pool',poolAt:'2026-10-01'});
 env.crm=async(url,o={})=>{if(o.method){writes.push(url);return}return url==='/employees'?[]:detail(returned)};
 const drawer=await env.mount('customerManagement.tsx','CustomerDrawer',{id:'A',supervisor:true,close(){},changed(){},checkPool(){}});
 const claim=drawer.button('重新报备并认领').props.onClick;
 returned=customer('B',{lifecycle:'pool',poolAt:'2026-10-01'});drawer.find(n=>typeof n.type==='function'&&n.type.name==='Feedback'&&n.props.retry).props.retry();await drawer.flush();await claim();assert.deepEqual(writes,[]);
 drawer.props={...drawer.props,id:'B'};drawer.render();await claim();assert.deepEqual(writes,[]);await drawer.flush();await claim();assert.deepEqual(writes,[]);
 await click(drawer,'重新报备并认领');assert.deepEqual(writes,['/customers/B/claim']);
});
