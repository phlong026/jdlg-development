'use strict';
const api=require('./tracking/api');

const data={
 interactionAvailable:false,interactionLoading:false,interactionError:'',comments:[],commentCount:null,
 likeCount:null,favoriteCount:null,liked:false,favorited:false,commentText:'',commentSubmitting:false,
 interactionBusy:false,commentCursor:'',commentHasMore:false,commentComposerOpen:false
};

function sessionToken(){return String(wx.getStorageSync('token')||'');}
function contentRef(page){
 const id=String(page.options&&page.options.content_id||''),version=Number(page.options&&page.options.content_version);
 if(!/^[A-Za-z0-9_-]{8,128}$/.test(id)||!Number.isInteger(version)||version<1)return null;
 return{id,version,path:'/contents/'+encodeURIComponent(id)+'/versions/'+version+'/interactions'};
}
function capture(page){
 const ref=contentRef(page);if(!ref)return null;
 const token=sessionToken();
 return{...ref,token,identity:ref.id+'|'+ref.version+'|'+token,loadSequence:page.loadSequence,generation:page._noteInteractionGeneration||0};
}
function isCurrent(page,proof){
 const ref=contentRef(page);
 return !!proof&&page.pageVisible!==false&&page.data&&page.data.contentReady===true&&!!ref&&
  ref.id===proof.id&&ref.version===proof.version&&sessionToken()===proof.token&&
  page.loadSequence===proof.loadSequence&&(page._noteInteractionGeneration||0)===proof.generation;
}
function count(value){if(value===null||value===undefined||value==='')return null;const number=Number(value);return Number.isInteger(number)&&number>=0?number:null;}
function commentID(item){return String(item&&item.comment_id||'');}
function mergeComments(current,incoming){
 const result=[],seen=new Set();
 (current||[]).concat(incoming||[]).forEach(item=>{const id=commentID(item);if(!id||seen.has(id))return;seen.add(id);result.push(item);});
 return result;
}
function errorStatus(error){return Number(error&&error.statusCode)||Number(error&&error.code)||0;}
function requireSession(){
 if(sessionToken())return true;
 wx.navigateTo({url:'/pages/login/login'});return false;
}
function notifyFailure(message){if(wx.showToast)wx.showToast({title:message,icon:'none'});}
function clippedText(event){
 const value=String(event&&event.detail&&event.detail.value||'');
 return Array.from(value).slice(0,500).join('');
}

const methods={
 invalidateNoteInteractions(){
  this._noteInteractionGeneration=(this._noteInteractionGeneration||0)+1;
  this._noteInteractionLoadID=(this._noteInteractionLoadID||0)+1;
  this._noteInteractionActionID=(this._noteInteractionActionID||0)+1;
  this._noteInteractionCommentID=(this._noteInteractionCommentID||0)+1;
  if(this.data&&(this.data.interactionLoading||this.data.interactionBusy||this.data.commentSubmitting))this.setData({interactionLoading:false,interactionBusy:false,commentSubmitting:false});
 },
 async loadNoteInteractions(more=false){
  more=more===true;
  const proof=capture(this);if(!proof||!this.data||this.data.contentReady!==true)return;
  const changedIdentity=!more&&this._noteInteractionLoadedKey!==proof.identity;
  if((this.data.interactionBusy||this.data.commentSubmitting)&&!changedIdentity)return;
  if(more&&(!this.data.commentHasMore||this.data.interactionLoading))return;
  const requestID=(this._noteInteractionLoadID||0)+1;this._noteInteractionLoadID=requestID;
  const cursor=more?String(this.data.commentCursor||''):'';
  const suffix=proof.path+'?limit=20'+(cursor?'&cursor='+encodeURIComponent(cursor):'');
  if(changedIdentity){
   this._noteInteractionActionID=(this._noteInteractionActionID||0)+1;
   this._noteInteractionCommentID=(this._noteInteractionCommentID||0)+1;
  }
  this.setData(changedIdentity?{
   interactionLoading:true,interactionError:'',interactionAvailable:false,comments:[],commentCount:null,
   likeCount:null,favoriteCount:null,liked:false,favorited:false,commentCursor:'',commentHasMore:false,
   interactionBusy:false,commentSubmitting:false,commentText:'',commentComposerOpen:false
  }:{interactionLoading:true,interactionError:''});
  try{
   const result=await api.request(suffix,'GET',{},undefined,true);
   if(requestID!==this._noteInteractionLoadID||!isCurrent(this,proof))return;
   const nextCursor=String(result&&result.next_cursor||''),incoming=Array.isArray(result&&result.comments)?result.comments:[];
   this._noteInteractionLoadedKey=proof.identity;
   this.setData({
    interactionAvailable:true,interactionError:'',comments:mergeComments(more?this.data.comments:[],incoming),
    commentCount:count(result&&result.comment_count),likeCount:count(result&&result.like_count),favoriteCount:count(result&&result.favorite_count),
    liked:result&&result.liked===true,favorited:result&&result.favorited===true,
    commentCursor:nextCursor,commentHasMore:!!nextCursor
   });
  }catch(error){
   if(requestID!==this._noteInteractionLoadID||!isCurrent(this,proof))return;
   if(errorStatus(error)===404){
    this.setData({interactionAvailable:false,interactionError:'互动功能暂不可用',comments:[],commentCount:null,likeCount:null,favoriteCount:null,liked:false,favorited:false,commentCursor:'',commentHasMore:false});
   }else this.setData({interactionError:'互动加载失败，请重试'});
  }finally{
   if(requestID===this._noteInteractionLoadID)this.setData({interactionLoading:false});
  }
 },
 setCommentText(event){this.setData({commentText:clippedText(event)});},
 openCommentComposer(){if(!requireSession())return;this.setData({commentComposerOpen:true,interactionError:''});},
 closeCommentComposer(){if(this.data.commentSubmitting)return;this.setData({commentComposerOpen:false});},
 async submitNoteComment(){
  if(!requireSession())return;
  if(this.data.commentSubmitting||this.data.interactionBusy)return;
  const text=Array.from(String(this.data.commentText||'').trim()).slice(0,500).join('');
  if(!text){notifyFailure('请输入评论内容');return;}
  const proof=capture(this);if(!proof)return;
  const requestID=(this._noteInteractionCommentID||0)+1;this._noteInteractionCommentID=requestID;
  this._noteInteractionLoadedKey=proof.identity;
  this._noteInteractionLoadID=(this._noteInteractionLoadID||0)+1;
  this.setData({commentSubmitting:true,interactionLoading:false,interactionError:''});
  try{
   const comment=await api.request(proof.path+'/comments','POST',{text},undefined,true);
   if(requestID!==this._noteInteractionCommentID||!isCurrent(this,proof)||!commentID(comment))return;
   const knownCount=count(this.data.commentCount);
   this.setData({interactionAvailable:true,comments:mergeComments(this.data.comments,[comment]),commentCount:knownCount===null?null:knownCount+1,commentText:'',commentComposerOpen:false});
  }catch(error){
   if(requestID!==this._noteInteractionCommentID||!isCurrent(this,proof))return;
   this.setData({interactionError:'评论发布失败，请重试'});notifyFailure('评论发布失败，请重试');
  }finally{
   if(requestID===this._noteInteractionCommentID)this.setData({commentSubmitting:false});
  }
 },
 async toggleNoteLike(){return this._setNoteReaction('like','liked','likeCount');},
 async toggleNoteFavorite(){return this._setNoteReaction('favorite','favorited','favoriteCount');},
 async _setNoteReaction(resource,stateKey,countKey){
  if(!requireSession())return;
  const proof=capture(this);if(!proof||this.data.interactionBusy||this.data.commentSubmitting)return;
  const active=this.data[stateKey]!==true,requestID=(this._noteInteractionActionID||0)+1;this._noteInteractionActionID=requestID;
  this._noteInteractionLoadedKey=proof.identity;
  this._noteInteractionLoadID=(this._noteInteractionLoadID||0)+1;
  this.setData({interactionBusy:true,interactionLoading:false,interactionError:''});
  try{
   const result=await api.request(proof.path+'/'+resource,'POST',{active},undefined,true);
   if(requestID!==this._noteInteractionActionID||!isCurrent(this,proof))return;
   const authoritativeCount=count(result&&result.count);
   if(typeof(result&&result.active)!=='boolean'||authoritativeCount===null)throw new Error('互动响应无效');
   this.setData({interactionAvailable:true,[stateKey]:result.active,[countKey]:authoritativeCount});
  }catch(error){
   if(requestID!==this._noteInteractionActionID||!isCurrent(this,proof))return;
   this.setData({interactionError:'操作失败，请重试'});notifyFailure('操作失败，请重试');
  }finally{
   if(requestID===this._noteInteractionActionID)this.setData({interactionBusy:false});
  }
 },
 async deleteNoteComment(event){
  if(!requireSession())return;
  const id=String(event&&event.currentTarget&&event.currentTarget.dataset&&event.currentTarget.dataset.commentId||event&&event.comment_id||'');
  if(!id||this.data.interactionBusy||this.data.commentSubmitting)return;
  const proof=capture(this);if(!proof)return;
  const requestID=(this._noteInteractionActionID||0)+1;this._noteInteractionActionID=requestID;
  this._noteInteractionLoadedKey=proof.identity;
  this._noteInteractionLoadID=(this._noteInteractionLoadID||0)+1;
  this.setData({interactionBusy:true,interactionLoading:false,interactionError:''});
  try{
   const result=await api.request(proof.path+'/comments/'+encodeURIComponent(id),'DELETE',{},undefined,true);
   if(requestID!==this._noteInteractionActionID||!isCurrent(this,proof)||!result||result.deleted!==true)return;
   const comments=this.data.comments.filter(item=>commentID(item)!==id),knownCount=count(this.data.commentCount);
   this.setData({comments,commentCount:knownCount===null?null:Math.max(0,knownCount-1)});
  }catch(error){
   if(requestID!==this._noteInteractionActionID||!isCurrent(this,proof))return;
   this.setData({interactionError:'评论删除失败，请重试'});notifyFailure('评论删除失败，请重试');
  }finally{
   if(requestID===this._noteInteractionActionID)this.setData({interactionBusy:false});
  }
 }
};

module.exports={data,methods};
