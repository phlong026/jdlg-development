'use strict';
// Build a network-free native fixture from the exact production WXML/WXSS.
const fs = require('node:fs'), path = require('node:path'), os = require('node:os');
const source = path.resolve(__dirname, '../miniprogram');
const output = fs.mkdtempSync(path.join(os.tmpdir(), 'jdlg-note-layout-'));
function write(file, value) { const target = path.join(output, file); fs.mkdirSync(path.dirname(target), { recursive: true }); fs.writeFileSync(target, value); }
for (const file of ['pages/contentreader/contentreader.wxml', 'pages/contentreader/contentreader.wxss', 'pages/contentreader/contentreader.json', 'utils/note-reader-actions.js', 'utils/content-presentation.js', 'utils/article-document.js', 'components/content-presentation/content-presentation.wxss', 'components/mini-icon/mini-icon.js', 'components/mini-icon/mini-icon.json', 'components/mini-icon/mini-icon.wxml', 'components/mini-icon/mini-icon.wxss', 'components/navigation-bar/navigation-bar.wxml', 'components/navigation-bar/navigation-bar.js', 'components/navigation-bar/navigation-bar.json', 'assets/welcome/courtyard.jpg']) write(file, fs.readFileSync(path.join(source, file)));
// This component SCSS contains plain CSS; the fixture deliberately omits compiler plugins.
write('components/navigation-bar/navigation-bar.wxss', fs.readFileSync(path.join(source, 'components/navigation-bar/navigation-bar.scss')));
write('components/login-guard/login-guard.json', JSON.stringify({ component: true }));
write('components/login-guard/login-guard.js', 'Component({});');
write('components/login-guard/login-guard.wxml', '');
write('app.json', JSON.stringify({ pages: ['pages/contentreader/contentreader'], window: { navigationStyle: 'custom', backgroundColor: '#f7f5f1' }, style: 'v2' }));
write('app.js', 'App({});');
write('app.wxss', 'page{background:#f7f5f1}button{font:inherit}button::after{border:0}');
write('project.config.json', JSON.stringify({ appid: 'wx94fdc48861913ede', compileType: 'miniprogram', projectname: '甲第笔记视觉验收-示例数据', setting: { es6: true, minified: false }, libVersion: '3.16.3' }, null, 2));
write('pages/contentreader/contentreader.js', `
const actions=require('../../utils/note-reader-actions');
const {buildContentPresentation}=require('../../utils/content-presentation');
Page({
 ...actions.methods,
 data:{...actions.data,__profileAccessMounted:true,loading:false,contentReady:true,mediaErrors:{},videoStarted:{},interactionAvailable:true,comments:[{comment_id:'fixture-comment',author:{nickname:'示例用户'},text:'视觉验收示例，不会写入线上评论。',created_label:'11月9日'}],commentCount:1,likeCount:0,favoriteCount:0,liked:false,favorited:false,commentText:'',consentAvailable:false,guideVisible:false,posterAvailable:true},
 onLoad(){this.pageVisible=true;this.loadSequence=1;this.options={content_id:'fixture-note',content_version:1};const media=[{media_id:'fixture-image-one',kind:'image',url:'/assets/welcome/courtyard.jpg',width:500,height:750},{media_id:'fixture-image-two',kind:'image',url:'/assets/welcome/courtyard.jpg',width:500,height:750}];const content={title:'庭院日记｜把自然住进家里',body:'阳光穿过庭院，木色与绿意在客厅相遇。\\n\\n开放式客餐厅，让一家人的日常自然连在一起。\\n\\n保留材质的温度，也留出生活的松弛。',body_format:'plain',published_at:'2026-11-09',media,tags:['庭院设计','住宅设计']};this.setData({content,media,presentationView:buildContentPresentation(content,media,wx.getWindowInfo(),{prefix:'reader'})});},
 posterAccessCurrent(){return true;},
 copyNote(){wx.showToast({title:'离线示例不创建副本',icon:'none'});},exportNoteWord(){wx.showToast({title:'离线示例不导出文件',icon:'none'});},saveNoteImages(){wx.showToast({title:'离线示例不写相册',icon:'none'});},deleteCurrentNote(){wx.showToast({title:'离线示例不删除内容',icon:'none'});},copyWebLink(){wx.showToast({title:'离线示例不生成链接',icon:'none'});},openTrackingSettings(){wx.showToast({title:'离线示例不改设置',icon:'none'});},generatePoster(){wx.showToast({title:'离线示例不生成海报',icon:'none'});},
 onContentGalleryChange(event){this.setData({'presentationView.galleryIndex':event.detail.current});},
 mediaError(){},openImage(){wx.previewImage({urls:['/assets/welcome/courtyard.jpg'],current:'/assets/welcome/courtyard.jpg'});},
 loadNoteInteractions(){wx.showToast({title:'当前为离线视觉示例',icon:'none'});},
 scrollToNoteComments(){wx.pageScrollTo({selector:'#note-comments',duration:200});},
 openCommentComposer(){this.setData({commentComposerOpen:true});},closeCommentComposer(){this.setData({commentComposerOpen:false});},setCommentText(event){this.setData({commentText:event.detail.value});},
 submitNoteComment(){wx.showToast({title:'视觉示例不发送评论',icon:'none'});},toggleNoteLike(){this.setData({liked:!this.data.liked});},toggleNoteFavorite(){this.setData({favorited:!this.data.favorited});}
});
`);
process.stdout.write(output + '\n');
