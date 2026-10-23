const fs = require('node:fs'), path = require('node:path'), vm = require('node:vm');
const assert = require('node:assert/strict'), {test} = require('node:test');
const ts = require('./report-runtime')('typescript');
const root = path.join(__dirname, '../miniprogram');
for (const ext of ['ts', 'js']) test(`${ext}: clearing caches retains entry signing dependencies and privacy choices`, async () => {
  const storage={token:'session-token',userInfo:{id:42},device_fingerprint:'session-device',
    share_tracking_consent_choice_v1:{accepted:false,explicit:true},privacy_settings:{analytics:false},draft:{input:'private'},interception_pool_cache_v1:{temporary:true}};
  const expected=JSON.parse(JSON.stringify(storage));delete expected.interception_pool_cache_v1;
  let definition,cleared=0,signed;
  const wx={getStorageSync:k=>storage[k],setStorageSync:(k,v)=>storage[k]=v,removeStorageSync:k=>delete storage[k],
    clearStorage(){throw Error('must not clear all storage');},showModal:o=>o.success({confirm:true}),showToast(){},
    request:o=>o.success({statusCode:200,data:{code:0,data:{ok:true}}})};
  const deps={getCachedDeviceFingerprint:()=>storage.device_fingerprint,clearPageCaches:()=>cleared++};
  let code=fs.readFileSync(path.join(root,`pages/settings/settings.${ext}`),'utf8');
  if(ext==='ts')code=ts.transpileModule(code,{compilerOptions:{module:ts.ModuleKind.CommonJS,target:ts.ScriptTarget.ES2020}}).outputText;
  vm.runInNewContext(code,{exports:{},require:()=>deps,Page:d=>definition=d,wx,console});
  const page={...definition,initCacheSize(){}};
  page.onClearCache();assert.equal(cleared,1);assert.deepEqual(storage,expected);
  const module={exports:{}};
  vm.runInNewContext(fs.readFileSync(path.join(root,'utils/tracking/api.js'),'utf8'),{module,wx,Promise,Error,
    require:()=>({generateRequestParams:(...args)=>{signed=args;return{token:args[0]};},paramsToHeaders:x=>x})});
  await module.exports.request('/entry','POST',{route_key:'pages/index/index'},undefined,true);
  assert.equal(signed[0],'session-token');assert.equal(signed[3],'session-device');
});
