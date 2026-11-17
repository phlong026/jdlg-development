const fs=require('fs'), path=require('path');
const root=process.argv[2];
const ts=require(path.join(process.env.REPORT_TEST_NODE_MODULES,'typescript'));
const sass=require(path.join(process.env.REPORT_TEST_NODE_MODULES,'sass'));
const src=path.join(root,'miniprogram/miniprogram');
const out='/tmp/jdlg-renovation-ui-preview-20261117';
function write(name,data){const p=path.join(out,'miniprogram',name);fs.mkdirSync(path.dirname(p),{recursive:true});fs.writeFileSync(p,data)}
function copy(name){const p=path.join(src,name);for(const file of fs.readdirSync(p)){const rel=path.join(name,file), full=path.join(src,rel);if(fs.statSync(full).isDirectory()){copy(rel);continue}const ext=path.extname(file);if(ext==='.ts')write(rel.slice(0,-3)+'.js',ts.transpileModule(fs.readFileSync(full,'utf8'),{compilerOptions:{module:ts.ModuleKind.CommonJS,target:ts.ScriptTarget.ES2020}}).outputText);else if(ext==='.scss')write(rel.slice(0,-5)+'.wxss',sass.compile(full,{silenceDeprecations:['import','legacy-js-api']}).css);else if(ext!=='.js' && ext!=='.wxss')write(rel,fs.readFileSync(full));else if(!fs.existsSync(full.replace(/\.(js|wxss)$/,ext==='.js'?'.ts':'.scss')))write(rel,fs.readFileSync(full));}}
for(const p of ['pages/renovationbudget','pages/aicostshare','pages/aicost','components/navigation-bar','components/quote-type-switch','components/renovation-report'])copy(p);
for(const name of ['renovation-pricing','renovation-report','costRegions','costEstimateShare','costEstimateShareImage','renovationCostShareImage']){
 const full=path.join(src,'utils',name+'.ts');write('utils/'+name+'.js',ts.transpileModule(fs.readFileSync(full,'utf8'),{compilerOptions:{module:ts.ModuleKind.CommonJS,target:ts.ScriptTarget.ES2020}}).outputText);
}
write('utils/deviceFingerprint.js','exports.getCachedDeviceFingerprint=()=>"offline-fixture";');
write('utils/parameter.js','exports.generateRequestParams=()=>({});exports.paramsToHeaders=()=>({});');
write('utils/asset.js','exports.resolveAssetPath=x=>x;');
write('utils/cloudImage.js','exports.prepareCloudImage=async()=>"";');
write('utils/enterpriseWechat.js','exports.DEFAULT_CUSTOMER_SERVICE_CORP_ID="";exports.DEFAULT_CUSTOMER_SERVICE_URL="";exports.tryOpenCustomerServiceDirect=async()=>false;');
for(const component of ['login-guard','nickname-guard']){
 write(`components/${component}/${component}.json`,JSON.stringify({component:true}));
 write(`components/${component}/${component}.js`,'Component({methods:{check(){return Promise.resolve()},checkAndShow(){return Promise.resolve()}}});');
 write(`components/${component}/${component}.wxml`,'');write(`components/${component}/${component}.wxss`,'');
}
write('app.json',JSON.stringify({pages:['pages/renovationbudget/index','pages/renovationbudget/result','pages/aicostshare/aicostshare','pages/aicost/aicost'],window:{navigationStyle:'custom',backgroundColor:'#e6daca'},sitemapLocation:'sitemap.json'}));
write('sitemap.json',JSON.stringify({rules:[]}));
write('app.wxss','page{font-family:-apple-system,BlinkMacSystemFont,"PingFang SC",sans-serif;box-sizing:border-box;}');
write('app.js',`const pricing=require('./utils/renovation-pricing');const helper=require('./utils/renovation-report');
App({onLaunch(){wx.setStorageSync('token','offline-fixture');wx.setStorageSync('userInfo',{id:987654321,token:'offline-fixture'});helper.writeRenovationReport(pricing.estimateRenovation(pricing.createDefaultRenovationInput()),{excludedItems:[],quantities:{},fees:{management:true,design:true,tax:false}});console.warn('装修离线UI：合成账户与示例数据，无真实后端写入');}});
wx.request=function(options){const id='AbcdEFghIJklMNopQRstUVwxYZ012345';const result=helper.readRenovationReport().report;options.success&&options.success({statusCode:200,data:{code:0,data:{report_id:id,expires_at:'2026-12-17T00:00:00Z',result}}});};`);
fs.writeFileSync(path.join(out,'project.config.json'),JSON.stringify({description:'装修离线原生UI验证，仅合成数据',miniprogramRoot:'miniprogram/',compileType:'miniprogram',appid:'wx94fdc48861913ede',projectname:'装修报价-离线UI验收',libVersion:'2.32.3',setting:{es6:true,enhance:true,minified:false,postcss:false,urlCheck:false},condition:{miniprogram:{list:[{name:'装修表单',pathName:'pages/renovationbudget/index'},{name:'装修报告',pathName:'pages/renovationbudget/result'},{name:'公开装修报告',pathName:'pages/aicostshare/aicostshare',query:'report_id=AbcdEFghIJklMNopQRstUVwxYZ012345'}]}}},null,2));
console.log(out);
