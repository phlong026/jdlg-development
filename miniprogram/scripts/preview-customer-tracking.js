'use strict';

// Native layout fixture: production page files, local examples, no network API.
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const source = path.resolve(__dirname, '../miniprogram');
const output = fs.mkdtempSync(path.join(os.tmpdir(), 'jdlg-customer-tracking-preview-'));

function write(relative, content) {
  const target = path.join(output, relative);
  fs.mkdirSync(path.dirname(target), { recursive: true });
  fs.writeFileSync(target, content);
}

for (const extension of ['js', 'json', 'wxml', 'wxss']) {
  const file = `pages/sharevisits/sharevisits.${extension}`;
  const value = fs.readFileSync(path.join(source, file), 'utf8');
  // Keep the user's stored session untouched; only this copied fixture reads
  // a synthetic token. No credentials are copied into the preview project.
  write('miniprogram/' + file, extension === 'js'
    ? value.replaceAll("wx.getStorageSync('token')", "'offline-customer-ui-fixture'")
    : value);
}
for (const extension of ['js', 'json', 'wxml']) {
  const file = `components/navigation-bar/navigation-bar.${extension}`;
  write('miniprogram/' + file, fs.readFileSync(path.join(source, file)));
}
for (const extension of ['js', 'json', 'wxml', 'wxss']) {
  const file = `components/mini-icon/mini-icon.${extension}`;
  write('miniprogram/' + file, fs.readFileSync(path.join(source, file)));
}
write('miniprogram/components/navigation-bar/navigation-bar.wxss', fs.readFileSync(path.join(source, 'components/navigation-bar/navigation-bar.scss')));
write('miniprogram/assets/courtyard.jpg', fs.readFileSync(path.join(source, 'assets/welcome/courtyard.jpg')));
write('miniprogram/components/login-guard/login-guard.json', JSON.stringify({ component: true }));
write('miniprogram/components/login-guard/login-guard.js', 'Component({});\n');
write('miniprogram/components/login-guard/login-guard.wxml', '<view hidden="true" />\n');
write('miniprogram/utils/protected-media.js', 'module.exports={resolveProtectedMediaURL:async()=>"/assets/courtyard.jpg"};\n');

write('miniprogram/utils/phase3-api.js', `
'use strict';
const image = '/assets/courtyard.jpg';
const asOf = new Date().toISOString();
const instant = minutes => new Date(Date.now() - minutes * 60000).toISOString();
const visitor = (id, name, level) => ({visitor_key:id,nickname:name,avatar_url:image,identity_state:'identified',intent:{level}});
const cards = [
  {view_id:'fixture-1',occurred_at:instant(5),visitor:visitor('u:77','张先生','high'),content_id:'fixture-villa',content_version:1,title:'现代简约庭院别墅设计方案',type:'video',source:{kind:'share',channel:'app_message'},visible_ms:28000,video_watch_ms:24000,progress_pct:80,progress_available:true,content_page_views:3},
  {view_id:'fixture-2',occurred_at:instant(48),visitor:visitor('u:88','李女士','medium'),content_id:'fixture-interior',content_version:1,title:'原木奶油风客厅，温暖又通透',type:'note',source:{kind:'share',channel:'timeline'},visible_ms:18000,video_watch_ms:0,progress_available:false,content_page_views:2},
  {view_id:'fixture-3',occurred_at:instant(100),visitor:visitor('u:77','张先生','high'),content_id:'fixture-chinese-villa',content_version:1,title:'二层新中式别墅外观方案',type:'video',source:{kind:'share',channel:'app_message'},visible_ms:15000,video_watch_ms:12000,progress_pct:40,progress_available:true,content_page_views:1},
  {view_id:'fixture-4',occurred_at:instant(160),visitor:{visitor_key:'a:fixture',nickname:'',identity_state:'anonymous',intent:{level:'unknown'}},content_id:'fixture-layout',content_version:1,title:'180㎡现代乡墅空间布局',type:'note',source:{kind:'share'},visible_ms:9000,video_watch_ms:0,progress_available:false,content_page_views:1},
].map(item=>({...item,route_key:'pages/contentreader/contentreader',share_id:'fixture-share',cover:{url:image,kind:'image'},status:'published'}));
function reply(items){return {items,as_of:asOf,metric_version:'offline-fixture',share_window:{limit:400,truncated:false}};}
module.exports={
  ORIGIN:'https://api.jiadilingguang.com',
  listMyVisits:async(query={})=>reply(cards.filter(item=>(!query.q||item.visitor.nickname.includes(query.q))&&(!query.intent||item.visitor.intent.level===query.intent)&&(!query.channel||(query.channel==='unknown'?!item.source.channel:item.source.channel===query.channel)))),
  listMyVisitors:async()=>reply([visitor('u:77','张先生','high'),visitor('u:88','李女士','medium')].map(item=>({...item,last_visit:instant(5),page_views:3,visible_ms:42000,project_activity_count:1}))),
  listMyContentStats:async()=>reply([{route_key:'pages/contentreader/contentreader',content_id:'fixture-villa',content_version:1,title:'现代简约庭院别墅设计方案',cover:{url:image},visitor_count:2,page_views:4,forward_count:1}]),
  listMyShares:async()=>reply([{share_id:'fixture-share',title:'现代简约庭院别墅设计方案',channel:'app_message',status:'published',created_at:instant(180),visitor_count:2,page_views:4,last_visit:instant(5)}]),
};
`);
write('miniprogram/app.js', `
const nativePage = Page;
Page = definition => { definition.data = {...definition.data,__profileAccessMounted:true}; nativePage(definition); };
App({});
`);
write('miniprogram/app.json', JSON.stringify({
  pages: ['pages/sharevisits/sharevisits'],
  window: { navigationStyle: 'custom', backgroundColor: '#f7f3ee' },
  style: 'v2',
}));
write('miniprogram/app.wxss', 'page{background:#f7f3ee}button::after{border:0}\n');
write('project.config.json', JSON.stringify({
  appid: 'wx94fdc48861913ede', compileType: 'miniprogram', miniprogramRoot: 'miniprogram/',
  projectname: '客户跟踪离线视觉验收', libVersion: '3.16.3',
  setting: { es6: true, minified: false, urlCheck: false },
}, null, 2));
write('README-OFFLINE-PREVIEW.md', '原生页面布局示例。使用本地模拟数据及合成登录读取，无线上接口、登录存储修改、业务写入或真实账号验收。\n');
process.stdout.write(output + '\n');
