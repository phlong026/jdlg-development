// Diagnostic only. Executes repository handlers; does NOT implement WeChat native components.
const fs = require('node:fs');
const vm = require('node:vm');
const path = require('node:path');
const assert = require('node:assert/strict');
const { test } = require('node:test');
const ts = require('./report-runtime')('typescript');
const root = path.join(__dirname, '../miniprogram');
function load(file, ext, {storage={token:'diagnostic-token',userInfo:{id:101}}, request, component}={}) {
  let definition;
  const requests=[], navigation=[], events=[], toasts=[];
  const wx = {
    getStorageSync:k=>storage[k], setStorageSync:(k,v)=>storage[k]=v,
    removeStorageSync:k=>delete storage[k],
    request:o=>{requests.push(o); if(request) return request(o); o.success({statusCode:200,data:{code:0,data:{id:storage.userInfo?.id || 101,nickname:'',avatar:''}}});},
    login:o=>o.success({code:'diagnostic-code'}),
    switchTab:o=>navigation.push(o.url), reLaunch:o=>navigation.push(o.url),
    showToast:o=>toasts.push(o.title), showModal:()=>{},
  };
  const deps={generateRequestParams:()=>({}),paramsToHeaders:()=>({}),
    getCachedDeviceFingerprint:()=> 'diagnostic-device',generateDeviceFingerprint:async()=> 'diagnostic-device',cacheDeviceFingerprint:()=>{},
    blockUnsafeText:()=>false,resolveAssetPath:x=>x,HOME_ASSETS:{},
    resumeReportAfterLogin:()=>false,cityOptionsMap:{福建:['福州']}};
  const source=fs.readFileSync(path.join(root,`${file}.${ext}`),'utf8');
  const code=ext==='ts'?ts.transpileModule(source,{compilerOptions:{module:ts.ModuleKind.CommonJS,target:ts.ScriptTarget.ES2020}}).outputText:source;
  vm.runInNewContext(code,{exports:{},require:name=>name.endsWith('profile-validation')?require(path.join(root,'components/nickname-guard/profile-validation.js')):deps,Page:d=>definition=d,Component:d=>definition=d,
    wx,getApp:()=>({}),console:{error(){},warn(){},log(){}},setTimeout:f=>{f();return 1;},clearTimeout(){},setInterval(){},clearInterval(){},Date,Promise});
  const instance={...definition,...definition.methods,data:{...definition.data},
    setData:(v,done)=>{Object.assign(instance.data,v);if(done)done.call(instance);},triggerEvent:(name,detail)=>events.push({name,detail}),selectComponent:()=>component};
  return {instance,storage,requests,navigation,events,toasts};
}
const guard='components/nickname-guard/nickname-guard';
const form=nickname=>({detail:{value:{nickname}}});
for (const ext of ['ts','js']) {
  test(`${ext}: persisted login resumes required profile setup and cannot skip into content`, async()=>{
    let shows=0;
    const f=load('pages/login/login',ext,{component:{show(){shows++;}}});
    f.instance.initNavLayout=()=>{};
    f.instance.onLoad({});
    f.instance.onReady();
    await new Promise(setImmediate);
    assert.equal(shows,1); assert.equal(f.navigation.length,0);
    await f.instance.onNicknameSkipped();
    assert.deepEqual(f.navigation,[]);
    assert.equal(f.storage.token,'diagnostic-token');
  });
  test(`${ext}: welcome popup close resumes nickname check in the same visit`,async()=>{
    let checks=0;
    const f=load('pages/index/index',ext,{component:{checkAndShow:async()=>{checks++;}}});
    f.instance.syncTabBar=()=>{};f.instance.loadFeaturedGroups=()=>{};
    f.instance.data.homeIntroPopupVisible=true;
    f.instance.onShow(); await Promise.resolve();
    assert.equal(checks,0);
    f.instance.closeHomeIntroPopup();await new Promise(setImmediate);
    assert.equal(checks,1);
  });
  test(`${ext}: interrupted reminder reappears, explicit postponement is account scoped`,async()=>{
    const storage={token:'account-A',userInfo:{id:101}};
    const f=load(guard,ext,{storage});assert.equal(await f.instance.checkAndShow(),true);
    const remount=load(guard,ext,{storage});assert.equal(await remount.instance.checkAndShow(),true);
    remount.instance.onSkip();
    const later=load(guard,ext,{storage});assert.equal(await later.instance.checkAndShow(),false);
    storage.token='account-B';storage.userInfo={id:102};
    const other=load(guard,ext,{storage});assert.equal(await other.instance.checkAndShow(),true);
  });
}

for (const ext of ['ts','js']) {
  test(`${ext}: returning login trusts the same-account server nickname over stale cache`,async()=>{
    let shows=0;
    const storage={token:'verified-same-account',userInfo:{id:101,nickname:'旧缓存昵称'}};
    const f=load('pages/login/login',ext,{storage,component:{show(){shows++;}},request:o=>o.success({statusCode:200,data:{code:0,data:{id:101,nickname:'  当前服务端昵称  ',avatar:'https://cdn.example/portrait.png'}}})});
    await f.instance.enterAppAfterLogin();
    assert.equal(shows,0);assert.equal(storage.userInfo.id,101);
    assert.equal(storage.userInfo.nickname,'当前服务端昵称');
    assert.equal(f.requests.length,1);assert.equal(f.requests[0].method,'GET');
    assert.deepEqual(f.navigation,['/pages/index/index']);
  });
  test(`${ext}: blank or invalid server nickname resumes required completion without writing an empty name`,async()=>{
    for(const value of [null,'   ','名'.repeat(33)]){
      let shows=0;
      const storage={token:'verified-same-account',userInfo:{id:101,nickname:'缓存不能当作已保存证据'}};
      const f=load('pages/login/login',ext,{storage,component:{show(){shows++;}},request:o=>o.success({statusCode:200,data:{code:0,data:{id:101,nickname:value}}})});
      await f.instance.enterAppAfterLogin();assert.equal(shows,1);
      assert.equal(storage.userInfo.id,101);assert.equal(storage.userInfo.nickname,'缓存不能当作已保存证据');
      assert.equal(f.requests.length,1);assert.equal(f.requests[0].method,'GET');
      const g=load(guard,ext,{storage});assert.equal(g.instance.showIfMissing({id:101,nickname:value}),true);
    }
  });
  test(`${ext}: late profile from another token or mismatched account cannot replace the current nickname`,async()=>{
    for(const changedToken of [true,false]){
      const storage={token:'current-token',userInfo:{id:101,nickname:'当前账号昵称'}};
      const f=load('pages/login/login',ext,{storage,request:o=>{if(changedToken)storage.token='new-token';o.success({statusCode:200,data:{code:0,data:{id:changedToken?101:202,nickname:'不属于当前账号'}}});}});
      await assert.rejects(f.instance.fetchVerifiedProfile(),/账号/);
      assert.equal(storage.userInfo.id,101);assert.equal(storage.userInfo.nickname,'当前账号昵称');
    }
  });
}

for (const ext of ['ts','js']) {
  test(`${ext}: full login does not navigate when profile read fails or avatar is missing`, async()=>{
    for (const request of [o=>o.fail(new Error('offline')), o=>o.success({statusCode:500,data:{code:500}}), o=>o.success({statusCode:200,data:{code:0,data:{id:101,nickname:'已有昵称',avatar:''}}})]) {
      let shows=0;
      const f=load('pages/login/login',ext,{component:{show(){shows++;}},request});
      assert.equal(await f.instance.enterAppAfterLogin(),false); assert.equal(f.navigation.length,0);
      assert.ok(f.instance.data.profileError || shows===1);
    }
  });
}
