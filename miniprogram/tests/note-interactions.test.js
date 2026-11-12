'use strict';
const test=require('node:test'),assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path'),vm=require('node:vm');
const source=fs.readFileSync(path.join(__dirname,'../miniprogram/utils/note-interactions.js'),'utf8');
function deferred(){let resolve,reject;const promise=new Promise((yes,no)=>{resolve=yes;reject=no;});return{promise,resolve,reject};}
function runtime(token='account-a'){
 const storage={token},requests=[],navigations=[],toasts=[];
 const api={request:(suffix,method,body,credential,authenticated)=>{const pending=deferred();requests.push({suffix,method,body,credential,authenticated,pending});return pending.promise;}};
 const wx={getStorageSync:key=>storage[key],navigateTo:value=>navigations.push(value),showToast:value=>toasts.push(value)};
 const sandbox={module:{exports:{}},require:name=>{assert.equal(name,'./tracking/api');return api;},wx,Promise,Error,Object,String,Number,Array,Boolean,Math,Date,encodeURIComponent};
 vm.runInNewContext(source,sandbox,{filename:'note-interactions.js'});
 const mixin=sandbox.module.exports;
 const page={...mixin.methods,options:{content_id:'content_one',content_version:2},loadSequence:7,pageVisible:true,data:{...mixin.data,contentReady:true},setData(value){Object.assign(this.data,value);}};
 return{page,storage,requests,navigations,toasts,ok(index,data){requests[index].pending.resolve(data);},fail(index,statusCode=503){requests[index].pending.reject(Object.assign(new Error('failed'),{statusCode}));}};
}
const first={comment_id:'comment-one',text:'第一条',created_at:'2026-11-09T10:00:00Z',mine:false,author:{nickname:'访客'}};

test('loads the published fixed-version interaction snapshot with authenticated signing enabled',async()=>{
 const t=runtime();const loading=t.page.loadNoteInteractions();assert.equal(t.requests.length,1);assert.equal(t.requests[0].suffix,'/contents/content_one/versions/2/interactions?limit=20');assert.equal(t.requests[0].method,'GET');assert.equal(t.requests[0].authenticated,true);
 t.ok(0,{like_count:3,favorite_count:2,comment_count:1,liked:true,favorited:false,comments:[first],next_cursor:'next-page'});await loading;
 assert.equal(t.page.data.interactionAvailable,true);assert.equal(t.page.data.likeCount,3);assert.equal(t.page.data.comments[0].comment_id,'comment-one');assert.equal(t.page.data.commentCursor,'next-page');assert.equal(t.page.data.commentHasMore,true);
});

test('a WXML tap event starts a fresh load instead of being mistaken for pagination',async()=>{
 const t=runtime();Object.assign(t.page.data,{interactionAvailable:true,comments:[first],commentCursor:'cursor-one',commentHasMore:true});
 const loading=t.page.loadNoteInteractions({type:'tap',currentTarget:{dataset:{}}});
 assert.equal(t.requests[0].suffix,'/contents/content_one/versions/2/interactions?limit=20');
 t.ok(0,{like_count:0,favorite_count:0,comment_count:0,comments:[]});await loading;
 assert.equal(t.page.data.comments.length,0);assert.equal(t.page.data.commentHasMore,false);
});

for(const boundary of ['account','version','sequence','hidden'])test('discards a late interaction response after '+boundary,async()=>{
 const t=runtime();const loading=t.page.loadNoteInteractions();
 if(boundary==='account')t.storage.token='account-b';
 if(boundary==='version')t.page.options.content_version=3;
 if(boundary==='sequence')t.page.loadSequence=8;
 if(boundary==='hidden')t.page.pageVisible=false;
 t.ok(0,{like_count:99,favorite_count:99,comment_count:1,liked:true,favorited:true,comments:[first]});await loading;
 assert.equal(t.page.data.interactionAvailable,false);assert.equal(t.page.data.likeCount,null);assert.equal(t.page.data.comments.length,0);
});

test('pagination is single-flight, de-duplicates comments and remains retryable after failure',async()=>{
 const t=runtime();let loading=t.page.loadNoteInteractions();t.ok(0,{like_count:1,favorite_count:0,comment_count:3,comments:[first],next_cursor:'cursor-one'});await loading;
 loading=t.page.loadNoteInteractions(true);t.page.loadNoteInteractions(true);assert.equal(t.requests.length,2,'duplicate load-more is ignored while pending');assert.match(t.requests[1].suffix,/cursor=cursor-one/);
 t.fail(1);await loading;assert.equal(t.page.data.interactionLoading,false);assert.equal(t.page.data.commentHasMore,true);assert.match(t.page.data.interactionError,/重试/);
 loading=t.page.loadNoteInteractions(true);assert.equal(t.requests.length,3);t.ok(2,{like_count:1,favorite_count:0,comment_count:2,comments:[first,{...first,comment_id:'comment-two',text:'第二条'}]});await loading;
 assert.equal(t.page.data.comments.map(item=>item.comment_id).join(','),'comment-one,comment-two');assert.equal(t.page.data.commentHasMore,false);assert.equal(t.page.data.interactionError,'');
});

test('a missing interaction service hides the capability without changing content readiness',async()=>{
 const t=runtime();const loading=t.page.loadNoteInteractions();t.fail(0,404);await loading;
 assert.equal(t.page.data.contentReady,true);assert.equal(t.page.data.interactionAvailable,false);assert.match(t.page.data.interactionError,/暂不可用/);
});

test('a new account request clears prior personal state and wins over the older response',async()=>{
 const t=runtime();let loading=t.page.loadNoteInteractions();t.ok(0,{like_count:8,favorite_count:4,comment_count:1,liked:true,favorited:true,comments:[first]});await loading;
 t.storage.token='account-b';const old=t.page.loadNoteInteractions();assert.equal(t.page.data.liked,false);assert.equal(t.page.data.likeCount,null);assert.equal(t.page.data.comments.length,0);
 t.storage.token='account-c';const current=t.page.loadNoteInteractions();t.ok(2,{like_count:1,favorite_count:0,comment_count:0,liked:false,favorited:false,comments:[]});await current;
 t.ok(1,{like_count:99,favorite_count:99,comment_count:1,liked:true,favorited:true,comments:[first]});await old;
 assert.equal(t.page.data.likeCount,1);assert.equal(t.page.data.liked,false);assert.equal(t.page.data.comments.length,0);
});

test('anonymous writes navigate to login and never replay the intended write',async()=>{
 const t=runtime('');t.page.data.commentText='不会自动提交';
 await t.page.submitNoteComment();await t.page.toggleNoteLike();await t.page.toggleNoteFavorite();
 assert.equal(t.requests.length,0);assert.equal(t.navigations.length,3);assert.ok(t.navigations.every(item=>item.url==='/pages/login/login'));
});

test('comment submission trims to 500 characters and appends the authoritative server comment',async()=>{
 const t=runtime();Object.assign(t.page.data,{interactionAvailable:true,commentCount:0});t.page.setCommentText({detail:{value:'字'.repeat(510)}});assert.equal(t.page.data.commentText.length,500);
 const writing=t.page.submitNoteComment();assert.equal(t.requests[0].suffix,'/contents/content_one/versions/2/interactions/comments');assert.equal(t.requests[0].body.text.length,500);assert.equal(t.requests[0].authenticated,true);
 const mine={...first,comment_id:'mine-one',text:t.requests[0].body.text,mine:true};t.ok(0,mine);await writing;
 assert.equal(t.page.data.comments.at(-1).comment_id,'mine-one');assert.equal(t.page.data.commentCount,1);assert.equal(t.page.data.commentText,'');assert.equal(t.page.data.commentComposerOpen,false);
});

test('like and favorite update only from successful authoritative responses',async()=>{
 const t=runtime();Object.assign(t.page.data,{interactionAvailable:true,liked:false,favorited:false,likeCount:4,favoriteCount:5});
 let writing=t.page.toggleNoteLike();assert.equal(t.requests[0].body.active,true);assert.equal(t.page.data.liked,false,'no optimistic mutation');t.fail(0);await writing;assert.equal(t.page.data.liked,false);assert.equal(t.page.data.likeCount,4);
 writing=t.page.toggleNoteLike();t.ok(1,{active:true,count:5});await writing;assert.equal(t.page.data.liked,true);assert.equal(t.page.data.likeCount,5);
 writing=t.page.toggleNoteFavorite();assert.equal(t.requests[2].body.active,true);t.ok(2,{active:true,count:6});await writing;assert.equal(t.page.data.favorited,true);assert.equal(t.page.data.favoriteCount,6);
});

test('an older request finally block cannot unlock a newer request',async()=>{
 const t=runtime();const old=t.page.loadNoteInteractions();t.page.invalidateNoteInteractions();const current=t.page.loadNoteInteractions();assert.equal(t.requests.length,2);t.ok(0,{comments:[]});await old;assert.equal(t.page.data.interactionLoading,true);t.ok(1,{like_count:0,favorite_count:0,comment_count:0,comments:[]});await current;assert.equal(t.page.data.interactionLoading,false);
});

test('an account change cancels an old reaction without its finally unlocking the new account action',async()=>{
 const t=runtime();let loading=t.page.loadNoteInteractions();t.ok(0,{like_count:4,favorite_count:2,comment_count:0,liked:false,favorited:false,comments:[]});await loading;
 const old=t.page.toggleNoteLike();assert.equal(t.page.data.interactionBusy,true);
 t.storage.token='account-b';loading=t.page.loadNoteInteractions();assert.equal(t.page.data.interactionBusy,false);t.ok(2,{like_count:1,favorite_count:3,comment_count:0,liked:false,favorited:false,comments:[]});await loading;
 const current=t.page.toggleNoteFavorite();assert.equal(t.page.data.interactionBusy,true);
 t.ok(1,{active:true,count:5});await old;assert.equal(t.page.data.interactionBusy,true);assert.equal(t.page.data.liked,false);assert.equal(t.page.data.likeCount,1);
 t.ok(3,{active:true,count:4});await current;assert.equal(t.page.data.interactionBusy,false);assert.equal(t.page.data.favorited,true);assert.equal(t.page.data.favoriteCount,4);
});

test('a late GET cannot overwrite a newer successful reaction',async()=>{
 const t=runtime();Object.assign(t.page.data,{interactionAvailable:true,likeCount:4,liked:false});
 const reading=t.page.loadNoteInteractions();const writing=t.page.toggleNoteLike();assert.equal(t.requests.length,2);assert.equal(t.page.data.interactionLoading,false);
 t.ok(1,{active:true,count:5});await writing;assert.equal(t.page.data.liked,true);assert.equal(t.page.data.likeCount,5);
 t.ok(0,{like_count:4,favorite_count:0,comment_count:0,liked:false,favorited:false,comments:[]});await reading;
 assert.equal(t.page.data.liked,true);assert.equal(t.page.data.likeCount,5);
});

test('reads and all mutations are mutually exclusive while a write is pending',async()=>{
 const t=runtime();Object.assign(t.page.data,{interactionAvailable:true,commentCount:0,commentText:'新评论'});
 const comment=t.page.submitNoteComment();t.page.loadNoteInteractions();t.page.toggleNoteLike();assert.equal(t.requests.length,1,'comment blocks reads and reactions');
 t.ok(0,{...first,comment_id:'mine-two',mine:true});await comment;
 const reaction=t.page.toggleNoteLike();t.page.loadNoteInteractions();t.page.submitNoteComment();assert.equal(t.requests.length,2,'reaction blocks reads and comments');
 t.ok(1,{active:true,count:1});await reaction;
 t.page.data.commentText='另一条';const deleting=t.page.deleteNoteComment({currentTarget:{dataset:{commentId:'mine-two'}}});t.page.loadNoteInteractions();t.page.submitNoteComment();assert.equal(t.requests.length,3,'delete blocks reads and comments');
 t.ok(2,{deleted:true});await deleting;assert.equal(t.page.data.comments.length,0);assert.equal(t.page.data.commentCount,0);
});
