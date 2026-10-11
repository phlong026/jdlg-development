<script setup lang="ts">
import {ref,reactive,computed,onMounted,onUnmounted,watch} from 'vue'
import {ElMessage} from 'element-plus'
import {User,Calendar,Refresh,Search,Plus,ArrowRight,SwitchButton,Setting,Connection,EditPen,Download,Check,Clock} from '@element-plus/icons-vue'
import {api,download,type Account,type Customer,type Employee,type Detail} from './api'
const user=ref<Account|null>(null),booting=ref(true),demo=ref(false),busy=ref(false),saving=ref(false)
const login=reactive({username:'',password:''}),loginError=ref('')
const view=ref('customers'),queue=ref('all'),page=ref(1),size=20,total=ref(0),rows=ref<Customer[]>([]),staff=ref<Employee[]>([])
const overview=ref<Record<string,number>>({}),detail=ref<Detail|null>(null),drawer=ref(false),drawerLoading=ref(false),tab=ref('history')
const filters=reactive({keyword:'',stage:'',intent:'',category:'',ownerId:''}),loadError=ref('')
const syncData=ref<any>({runs:[],events:[]}),accounts=ref<Account[]>([])
const followOpen=ref(false),editOpen=ref(false),accountOpen=ref(false)
const formError=ref(''),editingAccount=ref(false),expandedFilters=ref(false)
const follow=reactive({version:0,content:'',stage:'following',nextFollowupAt:null as string|null})
const edit=reactive({version:0,ownerId:null as string|null,stage:'new',category:'待确认',intent:'medium',phone:'',demand:'',nextFollowupAt:null as string|null})
const accountForm=reactive({username:'',displayName:'',employeeId:'',role:'staff',enabled:true,password:''})
const stages:Record<string,string>={new:'新客户',following:'跟进中',won:'已成交',paused:'暂缓',invalid:'无效'}
const intents:Record<string,string>={high:'高意向',medium:'中意向',low:'低意向'}
const categories=['待确认','建房咨询','设计咨询','造价咨询','合作咨询']
const title=computed(()=>({customers:'客户管理',tasks:'我的待办',sync:'企微同步',team:'员工与权限'}[view.value]||'客户管理'))
const isAdmin=computed(()=>user.value?.role==='admin')
const employeeName=(id:string|null)=>staff.value.find(e=>e.id===id)?.name||id||'未分配'
const stamp=(s?:string|null)=>s?new Date(s).toLocaleString('zh-CN',{month:'2-digit',day:'2-digit',hour:'2-digit',minute:'2-digit',hour12:false}):'未安排'
const dateOnly=(s:string)=>new Date(s).toLocaleDateString('zh-CN')
const isDue=(c:Customer)=>['new','following'].includes(c.stage)&&!!c.nextFollowupAt&&new Date(c.nextFollowupAt).getTime()<Date.now()
function query() {return new URLSearchParams({...filters,queue:queue.value,page:String(page.value),size:String(size)}).toString()}
let listRequest=0,detailRequest=0,timer:ReturnType<typeof setInterval>
function resetSession() {
 ++listRequest;++detailRequest;user.value=null;drawer.value=false;detail.value=null;followOpen.value=false;editOpen.value=false;accountOpen.value=false;rows.value=[]
 view.value='customers';queue.value='all';page.value=1;overview.value={};staff.value=[];accounts.value=[];syncData.value={runs:[],events:[]};loadError.value='';busy.value=false
 Object.assign(filters,{keyword:'',stage:'',intent:'',category:'',ownerId:''})
}
async function initialize() {
 try {const me=await api<{account:Account;demo:boolean}>('/auth/me');user.value=me.account;demo.value=me.demo;await load();}
 catch {user.value=null} finally {booting.value=false}
}
async function signIn() {
 busy.value=true;loginError.value=''
 try {await api('/auth/login','POST',login);login.password='';await initialize();}
 catch(e:any) {loginError.value=e.message}
 finally {busy.value=false}
}
async function signOut() {try{await api('/auth/logout','POST');resetSession()}catch{ElMessage.error('退出未成功，请恢复连接后重试')}}
async function load(silent=false) {
 const request=++listRequest;if(!silent)busy.value=true;loadError.value=''
 try {
  const [o,e]=await Promise.all([api<Record<string,number>>('/overview'),api<Employee[]>('/employees')])
  if(request!==listRequest)return;overview.value=o;staff.value=e
  if(view.value==='sync') syncData.value=await api('/sync')
  else if(view.value==='team') accounts.value=await api<Account[]>('/accounts')
  else {const result=await api<{content:Customer[];totalElements:number}>('/customers?'+query());if(request===listRequest){rows.value=result.content;total.value=result.totalElements}}
 }catch(e:any){if(request===listRequest)loadError.value=e.message}
 finally {if(request===listRequest)busy.value=false}
}
function selectView(v:string) {view.value=v;queue.value=v==='tasks'?'today':'all';page.value=1;load()}
function selectQueue(v:string) {queue.value=v;page.value=1;load()}
function search() {page.value=1;load()}
async function openDetail(c:Customer) {
 const request=++detailRequest;drawer.value=true;drawerLoading.value=true;detail.value=null;tab.value='history'
 try {const d=await api<Detail>('/customers/'+encodeURIComponent(c.id));if(request===detailRequest)detail.value=d}
 catch(e:any){ElMessage.error(e.message);drawer.value=false}
 finally {if(request===detailRequest)drawerLoading.value=false}
}
async function reloadDetail() {
 if(!detail.value)return
 try{detail.value=await api<Detail>('/customers/'+encodeURIComponent(detail.value.customer.id))}
 catch(e:any){ElMessage.error(e.message);drawer.value=false;detail.value=null}
}
function startFollow() {
 if(!detail.value)return
 const c=detail.value.customer;Object.assign(follow,{version:c.version,content:'',stage:['won','invalid','paused'].includes(c.stage)?c.stage:'following',nextFollowupAt:null})
 formError.value='';followOpen.value=true
}
function startEdit() {
 if(!detail.value)return
 const c=detail.value.customer;Object.assign(edit,{version:c.version,ownerId:c.ownerId,stage:c.stage,category:c.category,intent:c.intent,phone:c.phone,demand:c.demand,nextFollowupAt:c.nextFollowupAt})
 formError.value='';editOpen.value=true
}
async function saveFollow() {
 if(!detail.value)return
 saving.value=true;formError.value=''
 try {
  await api('/customers/'+encodeURIComponent(detail.value.customer.id)+'/followups','POST',{...follow,nextFollowupAt:follow.nextFollowupAt?new Date(follow.nextFollowupAt).toISOString():null})
  followOpen.value=false;await reloadDetail();await load(true);ElMessage.success('跟进已记录，待办已更新')
 }catch(e:any){formError.value=e.message}finally {saving.value=false}
}
async function saveEdit() {
 if(!detail.value)return
 saving.value=true;formError.value=''
 try {
  await api('/customers/'+encodeURIComponent(detail.value.customer.id),'PATCH',{...edit,nextFollowupAt:edit.nextFollowupAt?new Date(edit.nextFollowupAt).toISOString():null})
  editOpen.value=false;await reloadDetail();await load(true);ElMessage.success('客户资料已更新')
 }catch(e:any){formError.value=e.message}finally {saving.value=false}
}
async function exportFile(kind:string) {try {await download('/export/'+kind+'?'+query(),kind+'.csv');ElMessage.success('导出完成')}catch(e:any){ElMessage.error(e.message)}}
async function synchronize() {saving.value=true;try{await api('/sync','POST');await load(true);ElMessage.success('同步任务已创建')}catch(e:any){ElMessage.error(e.message)}finally{saving.value=false}}
async function retry(id:string) {try{await api('/sync/events/'+id+'/retry','POST');await load(true);ElMessage.success('已加入重试队列')}catch(e:any){ElMessage.error(e.message)}}
function openAccount(a?:Account) {editingAccount.value=!!a;Object.assign(accountForm,{username:'',displayName:'',employeeId:'',role:'staff',enabled:true,password:''},a||{});formError.value='';accountOpen.value=true}
async function saveAccount() {saving.value=true;formError.value='';try{await api('/accounts','POST',accountForm);accountOpen.value=false;await load(true);ElMessage.success('账号已保存；该账号的旧会话已退出')}catch(e:any){formError.value=e.message}finally{saving.value=false}}
const syncLabels:Record<string,string>={pending:'排队中',running:'同步中',done:'已完成',failed:'失败'}
onMounted(()=>{initialize();window.addEventListener('crm-unauthorized',resetSession);timer=setInterval(()=>{if(user.value&&!busy.value)load(true)},30000)})
onUnmounted(()=>{clearInterval(timer);window.removeEventListener('crm-unauthorized',resetSession)})
watch(()=>[follow.stage,edit.stage],()=>{if(!['new','following'].includes(follow.stage))follow.nextFollowupAt=null;if(!['new','following'].includes(edit.stage))edit.nextFollowupAt=null})
</script>

<template>
 <div v-if="booting" class="startup">正在加载客户工作台…</div>
 <div v-else-if="!user" class="login-page">
  <div class="login-intro"><div class="brand-mark">雀</div><span class="eyebrow">源雀 SCRM · 客户管理</span><h1>每一位客户，<br>都有下一步。</h1><p>从新增客户到持续跟进，<br>把客户、记录和待办放在一起。</p><div class="intro-note">第一期 · 客户工作台</div></div>
  <form class="login-card" @submit.prevent="signIn"><h2>登录工作台</h2><p>使用管理员为你开通的账号</p><label for="username">账号</label><el-input id="username" v-model="login.username" size="large" autocomplete="username" placeholder="输入登录账号"/><label for="password">密码</label><el-input id="password" v-model="login.password" type="password" show-password size="large" autocomplete="current-password" placeholder="输入密码"/><el-alert v-if="loginError" :title="loginError" type="error" :closable="false"/><el-button type="primary" size="large" native-type="submit" :loading="busy">登录</el-button><small>基于 <a href="https://github.com/IYque/Iyque-SCRM" target="_blank" rel="noopener noreferrer">源雀 Iyque-SCRM</a> 开发</small></form>
 </div>
 <div v-else class="workspace">
  <aside class="sidebar">
   <div class="brand"><div class="brand-mark small">雀</div><div><strong>客户工作台</strong><span>源雀 SCRM</span></div></div>
   <div class="nav-label">日常工作</div>
   <nav>
    <button :class="{active:view==='customers'}" @click="selectView('customers')"><el-icon><User/></el-icon><span>客户管理</span></button>
    <button :class="{active:view==='tasks'}" @click="selectView('tasks')"><el-icon><Calendar/></el-icon><span>我的待办</span><b v-if="overview.overdue">{{overview.overdue}}</b></button>
    <template v-if="isAdmin"><div class="nav-label">管理设置</div><button :class="{active:view==='sync'}" @click="selectView('sync')"><el-icon><Connection/></el-icon><span>企微同步</span></button><button :class="{active:view==='team'}" @click="selectView('team')"><el-icon><Setting/></el-icon><span>员工与权限</span></button></template>
   </nav>
   <div class="sidebar-bottom"><span class="live-dot"></span>第一期客户管理<br><small>记录需求 · 持续跟进</small></div>
  </aside>
  <div class="main-shell">
   <header class="topbar"><span>工作台 <span class="crumb">/</span> {{title}}</span><div class="topbar-user"><span class="user-avatar">{{user.displayName?.slice(0,1)}}</span><span>{{user.displayName}}<small>{{isAdmin?'管理员':'顾问'}}</small></span><el-button text :icon="SwitchButton" aria-label="退出登录" @click="signOut"/></div></header>
   <main>
    <div v-if="demo" class="demo-banner">演示环境 · 当前为示例客户，用于体验操作；不会连接真实企微或发送消息。</div>
    <section class="page-heading"><div><h1>{{title}}</h1><p>{{view==='customers'?'了解每位客户的需求，让每一次跟进都有记录。':view==='tasks'?'先处理到期事项，再安排下一次联系。':view==='sync'?'查看客户同步进度，及时处理未完成的同步。':'为顾问开通账号，明确客户的管理范围。'}}</p></div><el-button :icon="Refresh" :loading="busy" @click="load()">刷新</el-button></section>
    <el-alert v-if="loadError" :title="loadError" type="error" show-icon :closable="false" class="error-alert"/>
    <template v-if="view==='customers'||view==='tasks'">
     <section class="stats">
      <button class="stat" :class="{chosen:queue==='all'}" @click="selectQueue('all')"><span>客户总数</span><strong>{{overview.all||0}}</strong><small>{{isAdmin?'全部管理范围':'分配给我的客户'}}</small><el-icon><User/></el-icon></button>
      <button class="stat" :class="{chosen:queue==='new'}" @click="selectQueue('new')"><span>待首次跟进</span><strong>{{overview.new||0}}</strong><small>开始了解客户需求</small><el-icon><Plus/></el-icon></button>
      <button class="stat" :class="{chosen:queue==='today'}" @click="selectQueue('today')"><span>今日待办</span><strong>{{overview.today||0}}</strong><small>今天安排的联系</small><el-icon><Calendar/></el-icon></button>
      <button class="stat urgent" :class="{chosen:queue==='overdue'}" @click="selectQueue('overdue')"><span>逾期未跟进</span><strong>{{overview.overdue||0}}</strong><small>需要优先处理</small><el-icon><Clock/></el-icon></button>
     </section>
     <section class="customer-panel">
      <div class="panel-toolbar"><div class="queue-tabs"><button v-for="item in [{key:'all',label:'全部客户'},{key:'today',label:'今日待办'},{key:'overdue',label:'逾期待办'},{key:'unplanned',label:'未安排跟进'},...(isAdmin?[{key:'unassigned',label:'未分配'}]:[])]" :key="item.key" :class="{selected:queue===item.key}" @click="selectQueue(item.key)">{{item.label}}</button></div><el-dropdown @command="exportFile"><el-button :icon="Download">导出</el-button><template #dropdown><el-dropdown-menu><el-dropdown-item command="customers">导出筛选客户</el-dropdown-item><el-dropdown-item command="followups">导出筛选客户的跟进记录</el-dropdown-item></el-dropdown-menu></template></el-dropdown></div>
      <form class="filters" :class="{expanded:expandedFilters}" @submit.prevent="search"><el-input v-model="filters.keyword" :prefix-icon="Search" clearable placeholder="搜索客户、电话或需求" @clear="search"/><el-select v-model="filters.stage" clearable placeholder="客户状态" @change="search"><el-option v-for="(label,key) in stages" :key="key" :label="label" :value="key"/></el-select><el-select v-model="filters.category" clearable placeholder="咨询类型" @change="search"><el-option v-for="c in categories" :key="c" :label="c" :value="c"/></el-select><el-select v-model="filters.intent" clearable placeholder="意向程度" @change="search"><el-option v-for="(label,key) in intents" :key="key" :label="label" :value="key"/></el-select><el-select v-if="isAdmin" v-model="filters.ownerId" clearable placeholder="负责人" @change="search"><el-option v-for="s in staff" :key="s.id" :value="s.id" :label="s.name"/></el-select><el-button native-type="submit" :icon="Search">查询</el-button><el-button class="mobile-filter-toggle" @click="expandedFilters=!expandedFilters">{{expandedFilters?'收起筛选':'更多筛选'}}</el-button></form>
      <el-table v-loading="busy" :data="rows" row-key="id" class="customer-table" @row-click="openDetail" empty-text="当前没有符合条件的客户">
       <el-table-column label="客户" min-width="180"><template #default="{row}"><div class="customer-cell"><span class="customer-avatar">{{row.name?.slice(0,1)||'客'}}</span><div><strong>{{row.name}}</strong><small>{{row.phone||'尚未补充联系方式'}}</small></div></div></template></el-table-column>
       <el-table-column label="客户状态" width="110"><template #default="{row}"><span class="stage-tag" :class="row.stage">{{stages[row.stage]}}</span></template></el-table-column>
       <el-table-column label="咨询与意向" width="150"><template #default="{row}"><div class="category-cell">{{row.category}}<small :class="'intent-'+row.intent">{{intents[row.intent]}}</small></div></template></el-table-column>
       <el-table-column label="负责人" width="120"><template #default="{row}">{{employeeName(row.ownerId)}}</template></el-table-column>
       <el-table-column label="下次跟进" min-width="150"><template #default="{row}"><span :class="{'due':isDue(row)}">{{stamp(row.nextFollowupAt)}}</span><small v-if="isDue(row)" class="due-hint">已逾期</small></template></el-table-column>
       <el-table-column label="需求摘要" min-width="240" prop="demand" show-overflow-tooltip/>
       <el-table-column label="" width="75" fixed="right"><template #default><el-button link type="primary">查看 <el-icon><ArrowRight/></el-icon></el-button></template></el-table-column>
      </el-table>
      <div class="mobile-customers"><button v-for="c in rows" :key="c.id" class="mobile-customer" @click="openDetail(c)"><div><strong>{{c.name}}</strong><span class="stage-tag" :class="c.stage">{{stages[c.stage]}}</span></div><p>{{c.demand||'待补充客户需求'}}</p><footer><span>{{employeeName(c.ownerId)}}</span><span :class="{'due':isDue(c)}">{{stamp(c.nextFollowupAt)}} {{isDue(c)?'已逾期':''}}</span></footer></button><el-empty v-if="!rows.length" description="暂无符合条件的客户"/></div>
      <div class="pagination"><span>共 {{total}} 位客户</span><el-pagination v-model:current-page="page" :page-size="size" :total="total" layout="prev, pager, next" @current-change="load()"/></div>
     </section>
    </template>
    <template v-else-if="view==='sync'">
     <section class="settings-panel"><div class="section-title"><div><h2>客户同步</h2><p>同步企微可见范围内的客户和员工，不覆盖已经填写的跟进记录。</p></div><el-button type="primary" :icon="Refresh" :loading="saving" :disabled="!syncData.configured" @click="synchronize">立即同步</el-button></div><el-alert :title="syncData.configured?'客户读取凭据已配置；实际连接结果请查看同步任务。':'尚未连接真实企微。请按部署说明配置企业 ID、客户联系 Secret 和回调参数。'" :type="syncData.configured?'success':'info'" :closable="false"/><p class="subtle">{{syncData.callbackConfigured?'回调参数已配置，仍需在企微后台验证 URL 并做真实加好友测试。':'回调尚未配置完整，新增客户暂不能通过事件自动入库。'}}</p><div class="sync-numbers"><span>待处理事件 <b>{{syncData.pending||0}}</b></span><span>需重试事件 <b>{{syncData.failed||0}}</b></span></div><h3>最近同步任务</h3><el-table :data="syncData.runs" empty-text="暂无同步任务"><el-table-column label="开始时间"><template #default="{row}">{{stamp(row.startedAt)}}</template></el-table-column><el-table-column label="状态"><template #default="{row}">{{syncLabels[row.status]}}</template></el-table-column><el-table-column prop="processed" label="已同步客户"/><el-table-column prop="error" label="说明" min-width="240"/></el-table></section>
     <section class="settings-panel"><h2>待处理事件</h2><p>失败事件会自动重试；排查配置后，也可以手动重新加入队列。</p><el-table :data="syncData.events" empty-text="当前没有待处理事件"><el-table-column prop="customerId" label="客户标识"/><el-table-column prop="changeType" label="事件"/><el-table-column prop="attempts" label="重试次数" width="100"/><el-table-column prop="error" label="说明" min-width="220"/><el-table-column label="操作" width="100"><template #default="{row}"><el-button link type="primary" @click="retry(row.id)">重试</el-button></template></el-table-column></el-table></section>
    </template>
    <template v-else-if="view==='team'">
     <section class="settings-panel"><div class="section-title"><div><h2>登录账号</h2><p>顾问仅查看分配给自己的客户；管理员管理全部客户及账号。</p></div><el-button type="primary" :icon="Plus" @click="openAccount()">新增账号</el-button></div><el-table :data="accounts"><el-table-column prop="displayName" label="姓名"/><el-table-column prop="username" label="账号"/><el-table-column label="角色"><template #default="{row}">{{row.role==='admin'?'管理员':'顾问'}}</template></el-table-column><el-table-column label="关联企微员工"><template #default="{row}">{{employeeName(row.employeeId)}}</template></el-table-column><el-table-column label="状态"><template #default="{row}">{{row.enabled?'启用':'停用'}}</template></el-table-column><el-table-column label="操作" width="100"><template #default="{row}"><el-button link type="primary" @click="openAccount(row)">编辑</el-button></template></el-table-column></el-table></section>
     <section class="settings-panel"><h2>企微员工</h2><p>通过企微同步获取可接待客户的员工，创建顾问账号时选择对应人员。</p><el-table :data="staff" empty-text="请先完成企微同步"><el-table-column prop="name" label="名称"/><el-table-column prop="id" label="企微员工 ID"/><el-table-column label="状态"><template #default="{row}">{{row.active?'可接待':'不在当前接待范围'}}</template></el-table-column></el-table></section>
    </template>
   </main>
   <footer class="page-footer">源雀 SCRM · 第一期客户管理<span>客户关系与跟进记录，集中管理。</span></footer>
  </div>
  <el-drawer v-model="drawer" size="min(760px, 100vw)" title="客户详情" destroy-on-close>
   <div v-loading="drawerLoading">
    <template v-if="detail">
     <div class="detail-heading"><span class="customer-avatar large">{{detail.customer.name.slice(0,1)}}</span><div><h2>{{detail.customer.name}}</h2><p>{{detail.customer.category}} · {{employeeName(detail.customer.ownerId)}}</p></div><span class="stage-tag" :class="detail.customer.stage">{{stages[detail.customer.stage]}}</span></div>
     <div class="detail-actions"><el-button type="primary" :icon="EditPen" @click="startFollow">记录跟进</el-button><el-button :icon="Setting" @click="startEdit">编辑资料</el-button><el-button :icon="Refresh" @click="reloadDetail">刷新</el-button></div>
     <div class="next-step" :class="{late:isDue(detail.customer)}"><el-icon><Calendar/></el-icon><div><span>下次联系</span><strong>{{stamp(detail.customer.nextFollowupAt)}}</strong></div><span v-if="isDue(detail.customer)" class="due">已逾期</span></div>
     <el-tabs v-model="tab"><el-tab-pane label="跟进记录" name="history"><div v-if="!detail.followups.length" class="empty-history"><el-icon><EditPen/></el-icon><h3>还没有跟进记录</h3><p>记录第一次联系，给后续沟通一个起点。</p></div><div v-for="f in detail.followups" :key="f.id" class="history-item"><div class="history-dot" :class="{muted:f.kind==='profile'}"></div><div class="history-meta"><strong>{{f.kind==='followup'?'客户跟进':'资料调整'}}</strong><time>{{stamp(f.createdAt)}}</time></div><p>{{f.content}}</p><footer>{{f.operatorId}} <span>· {{stages[f.stage]}}</span><span v-if="f.nextFollowupAt"> · 下次 {{stamp(f.nextFollowupAt)}}</span></footer></div></el-tab-pane>
      <el-tab-pane label="客户资料" name="profile"><dl class="profile-fields"><dt>负责人</dt><dd>{{employeeName(detail.customer.ownerId)}}</dd><dt>咨询类型</dt><dd>{{detail.customer.category}}</dd><dt>意向程度</dt><dd>{{intents[detail.customer.intent]}}</dd><dt>联系方式</dt><dd>{{detail.customer.phone||'待补充'}}</dd><dt>添加时间</dt><dd>{{dateOnly(detail.customer.createdAt)}}</dd><dt>最近跟进</dt><dd>{{detail.customer.lastFollowupAt?stamp(detail.customer.lastFollowupAt):'尚未跟进'}}</dd><dt>客户需求</dt><dd class="demand-text">{{detail.customer.demand||'待补充'}}</dd></dl></el-tab-pane>
      <el-tab-pane label="企微关系" name="relations"><p class="subtle">业务负责人调整不会自动转移企微好友关系。</p><div v-for="r in detail.relations" :key="r.userId" class="relation-row"><div><strong>{{employeeName(r.userId)}}</strong><small>{{r.addTime||'时间待同步'}}</small></div><span>{{({0:'正常联系',1:'客户已删除员工',2:'员工已删除客户',3:'关系待核实'} as Record<number,string>)[r.status]||'待确认'}}</span></div></el-tab-pane></el-tabs>
    </template>
   </div>
  </el-drawer>
  <el-dialog v-model="followOpen" title="记录本次跟进" width="min(560px, 94vw)" :close-on-click-modal="false">
   <el-form label-position="top" @submit.prevent="saveFollow"><el-form-item label="本次沟通情况"><el-input v-model="follow.content" type="textarea" :rows="5" maxlength="2000" show-word-limit placeholder="记录客户需求、沟通结果和下一步安排"/></el-form-item><el-form-item label="客户状态"><el-select v-model="follow.stage"><el-option v-for="(label,key) in stages" :key="key" :label="label" :value="key"/></el-select></el-form-item><el-form-item v-if="['new','following'].includes(follow.stage)" label="下次跟进时间"><el-date-picker v-model="follow.nextFollowupAt" type="datetime" value-format="YYYY-MM-DDTHH:mm:ssZ" placeholder="选择下次联系时间"/></el-form-item><p v-else class="subtle">此状态将结束当前待办，可随时重新开始跟进。</p><el-alert v-if="formError" :title="formError" type="error" :closable="false"/></el-form><template #footer><el-button @click="followOpen=false">取消</el-button><el-button type="primary" :loading="saving" :icon="Check" @click="saveFollow">保存跟进</el-button></template>
  </el-dialog>
  <el-dialog v-model="editOpen" title="编辑客户资料" width="min(620px, 94vw)" :close-on-click-modal="false">
   <el-form label-position="top"><div class="form-grid"><el-form-item label="负责人"><el-select v-model="edit.ownerId" :disabled="!isAdmin" clearable><el-option v-for="s in staff.filter(x=>x.active)" :key="s.id" :label="s.name" :value="s.id"/></el-select></el-form-item><el-form-item label="客户状态"><el-select v-model="edit.stage"><el-option v-for="(label,key) in stages" :key="key" :label="label" :value="key"/></el-select></el-form-item><el-form-item label="咨询类型"><el-select v-model="edit.category"><el-option v-for="c in categories" :key="c" :label="c" :value="c"/></el-select></el-form-item><el-form-item label="意向程度"><el-select v-model="edit.intent"><el-option v-for="(label,key) in intents" :key="key" :label="label" :value="key"/></el-select></el-form-item></div><el-form-item label="联系方式"><el-input v-model="edit.phone" maxlength="80" placeholder="客户提供的电话或其他联系方式"/></el-form-item><el-form-item label="客户需求"><el-input v-model="edit.demand" type="textarea" :rows="4" maxlength="2000" show-word-limit/></el-form-item><el-form-item v-if="['new','following'].includes(edit.stage)" label="下次跟进时间"><el-date-picker v-model="edit.nextFollowupAt" type="datetime" value-format="YYYY-MM-DDTHH:mm:ssZ" placeholder="安排下次联系"/></el-form-item><el-alert v-if="formError" :title="formError" type="error" :closable="false"/></el-form><template #footer><el-button @click="editOpen=false">取消</el-button><el-button type="primary" :loading="saving" @click="saveEdit">保存资料</el-button></template>
  </el-dialog>
  <el-dialog v-model="accountOpen" title="员工登录账号" width="min(560px, 94vw)" :close-on-click-modal="false"><el-form label-position="top"><div class="form-grid"><el-form-item label="登录账号"><el-input v-model="accountForm.username" :disabled="editingAccount" placeholder="英文、数字或 ._-"/></el-form-item><el-form-item label="显示名称"><el-input v-model="accountForm.displayName"/></el-form-item></div><el-form-item label="角色"><el-select v-model="accountForm.role"><el-option label="顾问" value="staff"/><el-option label="管理员" value="admin"/></el-select></el-form-item><el-form-item label="关联企微员工"><el-select v-model="accountForm.employeeId" clearable><el-option v-for="s in staff.filter(x=>x.active)" :key="s.id" :label="s.name" :value="s.id"/></el-select></el-form-item><el-form-item label="密码（新账号必填；修改账号留空则保持）"><el-input v-model="accountForm.password" type="password" show-password autocomplete="new-password" placeholder="至少 12 个字符"/></el-form-item><el-form-item label="账号启用"><el-switch v-model="accountForm.enabled"/></el-form-item><el-alert v-if="formError" :title="formError" type="error" :closable="false"/></el-form><template #footer><el-button @click="accountOpen=false">取消</el-button><el-button type="primary" :loading="saving" @click="saveAccount">保存账号</el-button></template></el-dialog>
 </div>
</template>
