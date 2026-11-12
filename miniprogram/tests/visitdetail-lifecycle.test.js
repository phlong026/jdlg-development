'use strict';
const test=require('node:test'),assert=require('node:assert/strict'),fs=require('node:fs'),vm=require('node:vm'),path=require('node:path');
const root=path.join(__dirname,'../miniprogram/pages/visitdetail');
const source=()=>fs.readFileSync(path.join(root,'visitdetail.js'),'utf8');

// codex P1-4 回归：onLoad 曾引用未定义变量 visitor，两种入口都在初始化时抛 ReferenceError。
function loadPage(options,apiStub){
 let definition;
 const api=Object.assign({listMyShareVisits:async()=>({items:[]}),listMyVisitorEntries:async()=>({items:[]})},apiStub);
 vm.runInNewContext(source(),{Page:o=>definition=o,wx:{getStorageSync:()=>'login-token',hideShareMenu(){},navigateTo(){},stopPullDownRefresh(){}},require:()=>api,Promise,Date,Number,String,encodeURIComponent,console,setTimeout,clearTimeout});
 const page={...definition,data:structuredClone(definition.data),setData(value){Object.assign(this.data,value)}};
 page.onLoad(options);
 return page;
}

test('客户入口初始化不抛错且进入 customer 模式',()=>{
 const page=loadPage({visitor:'u:77',name:'陈丽芬',intent:'high'});
 assert.equal(page.data.mode,'customer');
 assert.equal(page.data.visitor,'u:77');
 assert.equal(page.data.intent_level,'high');
 assert.equal(page.data.visitor_name,'陈丽芬');
});

test('分享入口初始化不抛错且进入 share 模式',()=>{
 const page=loadPage({id:'share-0001',title:'庭院方案'});
 assert.equal(page.data.mode,'share');
 assert.equal(page.data.visitor,'');
 assert.equal(page.data.id,'share-0001');
});

test('分享入口缺 id 时给出错误而非白屏',()=>{
 const page=loadPage({});
 assert.equal(page.data.error,'缺少分享记录标识');
 assert.equal(page.data.loading,false);
});
