'use strict';
const test=require('node:test'),assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path'),vm=require('node:vm'),{execFileSync}=require('node:child_process');
const root=path.join(__dirname,'../miniprogram');
const compiler=process.env.WXML_COMPILER||'/Applications/wechatwebdevtools.app/Contents/Resources/app.asar.unpacked/node_modules/wcc-exec/wcc';
test('current reader WXML compiles note actions without bypassing the access gate',()=>{
 const file='./pages/contentreader/contentreader.wxml',source=execFileSync(compiler,[file],{cwd:root,encoding:'utf8'});
 const runtime={window:{},console};vm.createContext(runtime);vm.runInContext(source,runtime);const render=runtime.$gwx(file);
 const base={__profileAccessMounted:true,contentReady:true,loading:false,content:{title:'原木住宅'},presentationView:{kind:'gallery',title:'原木住宅',bodyText:'完整正文',media:[{media_id:'image-one',url:'wxfile://one',width:340,height:300}],galleryHeight:300,galleryIndex:0},comments:[],interactionAvailable:true,commentCount:0,noteMenuOpen:false};
 const output=JSON.stringify(render(base));
 assert.match(output,/note-toolbar/);assert.match(output,/完整正文/);assert.match(output,/note-interaction-dock/);assert.match(output,/note-comments/);
 const menu=JSON.stringify(render({...base,noteMenuOpen:true,noteMenu:[{key:'edit',label:'编辑笔记'},{key:'delete',label:'删除笔记'}]}));
 assert.match(menu,/编辑笔记/);assert.match(menu,/删除笔记/);
 const hidden=JSON.stringify(render({...base,__profileAccessMounted:false}));assert.doesNotMatch(hidden,/完整正文|编辑笔记/);
});

test('note action CSS defines layers and every static native icon',()=>{
 const css=fs.readFileSync(path.join(root,'pages/contentreader/contentreader.wxss'),'utf8');
 assert.match(css,/\.note-interaction-dock\s*\{[^}]*position:\s*fixed[^}]*z-index:\s*25/s);
 assert.match(css,/\.note-modal-mask\s*\{[^}]*position:\s*fixed[^}]*z-index:\s*40/s);
 const iconCSS=fs.readFileSync(path.join(root,'components/mini-icon/mini-icon.wxss'),'utf8');
 const actions=fs.readFileSync(path.join(root,'utils/note-reader-actions.js'),'utf8');
 const wxml=fs.readFileSync(path.join(root,'pages/contentreader/contentreader.wxml'),'utf8');
 const names=new Set([...actions.matchAll(/icon:\s*'([^']+)'/g),...wxml.matchAll(/<mini-icon[^>]+name="([^"{]+)"/g)].map(match=>match[1]));
 for(const name of names)assert.ok(iconCSS.includes('.mi-'+name+' {'),name+' icon missing');
});
