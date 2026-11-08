const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const path = require('node:path');
const root = path.join(__dirname, '../miniprogram');
function harness() {
  let page; const navigations = [];
  const wx = { getSystemInfoSync: () => ({}), showToast() {}, navigateTo: o => navigations.push(o.url), request: o => o.success({statusCode:200,data:{code:0,data:{list:[{designer_user_id:7,designer_name:'真实设计师',cert_status:'pending_review',work_previews:{popular:[{id:2,title:'高赞',likes:99},{id:1,title:'旧作',likes:3}],latest:[{id:3,title:'新作',likes:1},{id:2,title:'高赞',likes:99}]}}],total:1}}}) };
  vm.runInNewContext(fs.readFileSync(path.join(root,'pages/distribution/distribution.js'),'utf8'), {exports:{},wx,console,Page:p=>page=p, require});
  page.data = JSON.parse(JSON.stringify(page.data)); page.setData = v => Object.assign(page.data,v);
  return {page,navigations};
}
test('真实数据映射双作品，默认高赞优先，不伪造认证', async () => {
  const {page} = harness(); await page.loadDesigners(true);
  assert.equal(page.data.designerCards[0].works[0].id,2);
  assert.equal(page.data.designerCards[0].certified,false);
  page.onWorkSortTap({currentTarget:{dataset:{userId:7,sort:'latest'}}});
  assert.equal(page.data.designerCards[0].works[0].id,3);
  assert.equal(page.data.designerCards[0].workSort,'latest');
});
test('咨询保留设计师ID并打开现有主页咨询流程', () => {
  const {page,navigations} = harness(); page.onConsultDesigner({currentTarget:{dataset:{userId:7}}});
  assert.equal(navigations[0],'/pages/designerhome/designerhome?userId=7&consult=1');
});

test('排序只改变目标设计师的作品，不影响其他卡片，非法排序被忽略', async () => {
  const {page} = harness(); await page.loadDesigners(true);
  page.data.designerCards.push({...page.data.designerCards[0],designerUserId:8});
  page.onWorkSortTap({currentTarget:{dataset:{userId:7,sort:'latest'}}});
  assert.equal(page.data.designerCards[1].works[0].id,2);
  page.onWorkSortTap({currentTarget:{dataset:{userId:7,sort:'invalid'}}});
  assert.equal(page.data.designerCards[0].workSort,'latest');
});

test('仅主页作品继续进入对应设计师主页，广场作品进入详情', () => {
  const {page,navigations} = harness();
  page.onWorkTap({currentTarget:{dataset:{id:12,userId:7,publishScope:'homepage'}}});
  page.onWorkTap({currentTarget:{dataset:{id:13,userId:7,publishScope:'square'}}});
  assert.equal(navigations[0],'/pages/designerhome/designerhome?userId=7');
  assert.equal(navigations[1],'/pages/templatesquaredetails/templatesquaredetails?id=13');
});

test('原生WXML编译后确实展示双作品和独立排序按钮', () => {
  const {execFileSync} = require('node:child_process');
  const compiler = '/Applications/wechatwebdevtools.app/Contents/Resources/app.asar.unpacked/node_modules/wcc-exec/wcc';
  if (!fs.existsSync(compiler)) return;
  const source = execFileSync(compiler,['miniprogram/pages/distribution/distribution.wxml'],{cwd:path.join(__dirname,'..'),encoding:'utf8'});
  const context = {window:{},console}; vm.createContext(context);vm.runInContext(source,context);
  const tree = context.$gwx('miniprogram/pages/distribution/distribution.wxml')({__profileAccessMounted:true,tabs:[],traitFilters:[],designerCards:[{designerUserId:7,designerName:'测试设计师',works:[{id:1,title:'作品甲',likesText:'99'},{id:2,title:'作品乙',likesText:'12'}],workSort:'popular'}]});
  const rendered = JSON.stringify(tree);
  assert.ok(rendered.includes('作品甲') && rendered.includes('作品乙'));
  assert.ok(rendered.includes('点赞最多') && rendered.includes('最新发布'));
});

test('公开灵感入口在分类加载后选择案例精选并清除上次搜索', () => {
  let page; const app = {globalData:{designerTemplateEntry:'inspiration'}};
  vm.runInNewContext(fs.readFileSync(path.join(root,'pages/template/template.js'),'utf8'),{exports:{},console,Page:p=>page=p,getApp:()=>app,require:()=>({}),wx:{getSystemInfoSync:()=>({}),showToast(){}}});
  page.setData = value => Object.assign(page.data,value);
  Object.assign(page.data,{loading:false,mainTabs:[{label:'场景',value:'scene'},{label:'灵感',value:'inspiration'}],allSubTabs:[{label:'趋势',value:'rural_trend',parent:'inspiration'},{label:'案例精选',value:'selected_cases',parent:'inspiration'}],allThirdTabs:[],searchKeyword:'旧搜索'});
  let loaded = 0; page.loadTemplates = async () => {loaded++};
  assert.equal(page.consumeDesignerEntry(),true);
  assert.equal(page.data.currentMainTabIndex,1);
  assert.equal(page.data.subTabs[page.data.currentSubTabIndex].value,'selected_cases');
  assert.equal(page.data.searchKeyword,'');
  assert.equal(app.globalData.designerTemplateEntry,undefined);
  assert.equal(loaded,1);
});
