// @ts-nocheck
'use strict';
const api=require('./api');
const READER='pages/contentreader/contentreader';
const data={shareMaterials:[],shareMaterialsState:'hidden',shareMaterialsCursor:'',shareMaterialsLoading:false,shareMaterialOpening:''};
function account(){return String(wx.getStorageSync('token')||'');}
function current(page,sequence,token){return page.__materialsSequence===sequence&&page.__materialsAccount===token&&account()===token&&page.__materialsHidden!==true;}
const methods={
 resetShareMaterials(){
  this.__materialsSequence=(this.__materialsSequence||0)+1;
  this.__materialsAccount=account();
  this.setData({...data,shareMaterials:[]});
 },
 hideShareMaterials(){this.__materialsHidden=true;this.resetShareMaterials();},
 async refreshShareMaterials(event){
  const more=!!(event&&event.currentTarget&&event.currentTarget.dataset.more);
  const token=account();
  if(this.__materialsAccount!==token)this.resetShareMaterials();
  this.__materialsHidden=false;
  if(!token){this.resetShareMaterials();return;}
  if(this.data.shareMaterialsLoading)return;
  const cursor=more?this.data.shareMaterialsCursor:'';
  if(more&&!cursor)return;
  const sequence=(this.__materialsSequence||0)+1;this.__materialsSequence=sequence;this.__materialsAccount=token;
  this.setData({shareMaterialsLoading:true,shareMaterialOpening:'',shareMaterialsState:'loading',...(!more?{shareMaterials:[],shareMaterialsCursor:''}:{})});
  try{
   const result=await api.request('/materials?limit=20'+(cursor?'&cursor='+encodeURIComponent(cursor):''),'GET',{},undefined,true);
   if(!current(this,sequence,token))return;
   const items=(result.items||[]).filter(item=>item.status==='published'&&item.route_key===READER&&/^[A-Za-z0-9_-]{8,128}$/.test(item.content_id)&&Number.isInteger(item.content_version)&&item.content_version>0).map(item=>({...item,coverURL:item.cover?api.mediaURL(item.cover.url):''}));
   const all=more?this.data.shareMaterials.concat(items):items;
   this.setData({shareMaterials:all,shareMaterialsCursor:result.next_cursor||'',shareMaterialsLoading:false,shareMaterialsState:all.length?'ready':'empty'});
  }catch(error){
   if(!current(this,sequence,token))return;
   const denied=error.statusCode===401||error.statusCode===403||error.statusCode===404;
   this.setData({shareMaterials:[],shareMaterialsCursor:'',shareMaterialsLoading:false,shareMaterialsState:denied?'hidden':'error'});
  }
 },
 async openShareMaterial(event){
  if(this.data.shareMaterialOpening||this.data.shareMaterialsLoading)return;
  const token=account();
  if(!token||token!==this.__materialsAccount){this.resetShareMaterials();return;}
  const id=String(event.currentTarget.dataset.id||'');
  const item=this.data.shareMaterials.find(value=>value.content_id===id);
  if(!item)return;
  const sequence=this.__materialsSequence;
  this.setData({shareMaterialOpening:id});
  try{
   const entry=await api.request('/materials/'+encodeURIComponent(id)+'/versions/'+item.content_version,'GET',{},undefined,true);
   if(!current(this,sequence,token))return;
   if(entry.route_key!==READER||entry.content_id!==id||entry.content_version!==item.content_version||entry.status!=='published')throw new Error('内容入口无效');
   wx.navigateTo({url:'/'+READER+'?content_id='+encodeURIComponent(entry.content_id)+'&content_version='+entry.content_version,
    fail:()=>{if(current(this,sequence,token))wx.showToast({title:'打开失败，请重试',icon:'none'});}});
  }catch(error){
   if(!current(this,sequence,token))return;
   wx.showToast({title:'素材不可访问，请刷新后重试',icon:'none'});
   this.setData({shareMaterialOpening:''});
   await this.refreshShareMaterials();
  }finally{if(current(this,sequence,token))this.setData({shareMaterialOpening:''});}
 }
};
module.exports={data,methods};
export {};
