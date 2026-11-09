'use strict';
const test=require('node:test'),assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path'),vm=require('node:vm'),{execFileSync}=require('node:child_process');
const root=path.join(__dirname,'../miniprogram');
const compiler=process.env.WXML_COMPILER||'/Applications/wechatwebdevtools.app/Contents/Resources/app.asar.unpacked/node_modules/wcc-exec/wcc';
test('note renders unbranded gallery with anchored author actions and genuine interaction state',()=>{
 const file='./pages/contentreader/contentreader.wxml',source=execFileSync(compiler,[file,'./components/content-presentation/content-presentation.wxml'],{cwd:root,encoding:'utf8'});
 const runtime={window:{},console};vm.createContext(runtime);vm.runInContext(source,runtime);const render=runtime.$gwx(file);
 const base={__profileAccessMounted:true,contentReady:true,loading:false,content:{title:'原木住宅'},presentationView:{kind:'gallery',title:'原木住宅',bodyText:'完整正文',media:[{media_id:'image-one',url:'wxfile://one',width:340,height:300}],galleryHeight:300,galleryIndex:0},comments:[],interactionAvailable:true,commentCount:0,noteMenuOpen:false};
 const output=JSON.stringify(render(base));
 assert.match(output,/note-toolbar/);assert.match(output,/完整正文/);assert.match(output,/note-interaction-dock/);assert.doesNotMatch(output,/reader-brand|甲第内容/);
 const menu=JSON.stringify(render({...base,noteMenuOpen:true,noteMenu:[{key:'edit',label:'编辑笔记'},{key:'delete',label:'删除笔记'}]}));
 assert.match(menu,/编辑笔记/);assert.match(menu,/删除笔记/);
 const hidden=JSON.stringify(render({...base,__profileAccessMounted:false}));assert.doesNotMatch(hidden,/完整正文|编辑笔记/);
});

test('portrait note media stays complete while leaving room for its title',()=>{
 const {buildContentPresentation}=require('../miniprogram/utils/content-presentation');
 const media=[{media_id:'portrait-one',kind:'image',url:'wxfile://portrait',width:500,height:750}];
 const view=buildContentPresentation({title:'竖图笔记',body:'正文',media},media,{windowWidth:375,windowHeight:800},{edgeToEdge:true,galleryMaxHeightRatio:.52});
 assert.equal(view.frameWidth,375);assert.ok(view.galleryHeight<=416);
 assert.ok(Math.abs(view.media[0].width/view.media[0].height-500/750)<.01);
});
