// @ts-nocheck
'use strict';
const api=require('../../utils/ai-video-studio-api');
const ml=m=>({text:'文生视频',first_frame:'首帧图生',first_last_frame:'首尾帧',reference:'参考生成'}[m]||'视频创作');
const sl=s=>({draft:'草稿',queued:'等待处理',processing:'生成中',partial_success:'部分完成',completed:'已完成',failed:'失败',uncertain:'待核对',cancelled:'已取消'}[s]||'状态待确认');
const mergeRecords=(current,rows)=>{const seen=new Set();return(current||[]).concat(rows||[]).filter(item=>{const id=String(item.id||'');if(!id||seen.has(id))return false;seen.add(id);return true})};
Page({
  data:{tab:'batches',items:[],page:1,hasMore:false,loading:true,loadingMore:false,error:''},
  onLoad(o){this._unloaded=false;this._token=String(wx.getStorageSync('token')||'');this.setData({tab:o.tab==='drafts'?'drafts':'batches'});this.load()},
  onUnload(){this._unloaded=true;this._epoch=(this._epoch||0)+1},
  current(epoch){return!this._unloaded&&this._token===String(wx.getStorageSync('token')||'')&&(epoch===undefined||epoch===this._epoch)},
  onPullDownRefresh(){this.load().finally(()=>wx.stopPullDownRefresh())},
  switchTab(e){this.setData({tab:e.currentTarget.dataset.tab},()=>this.load())},
  async load(append){
    const appendPage=append===true,epoch=this._epoch=(this._epoch||0)+1,page=appendPage?this.data.page+1:1;
    this.setData(appendPage?{loadingMore:true,error:''}:{loading:true,error:'',items:[]});
    try{
      const value=this.data.tab==='drafts'?await api.listDrafts({page,page_size:20,status:'draft'}):await api.listBatches({page,page_size:20});
      if(!this.current(epoch))return;
      const rows=(value.items||[]).map(item=>({id:item.draft_id||item.batch_id,name:item.name||'未命名创作',mode:item.mode,mode_label:ml(item.mode),status:item.status||'draft',status_label:sl(item.status||'draft'),time_label:String(item.updated_at||item.created_at||'').replace('T',' ').slice(0,16)}));
      this.setData({loading:false,loadingMore:false,page,items:appendPage?mergeRecords(this.data.items,rows):rows,hasMore:value.has_more!==undefined?!!value.has_more:page*20<Number(value.total||0)});
    }catch(e){if(this.current(epoch))this.setData({loading:false,loadingMore:false,error:e.message||'记录加载失败'})}
  },
  loadMore(){if(!this.data.loadingMore)this.load(true)},
  open(e){const item=this.data.items[Number(e.currentTarget.dataset.index)];wx.navigateTo({url:this.data.tab==='drafts'?'/pages/aivideostudio/generate?draft_id='+encodeURIComponent(item.id):'/pages/aivideostudio/detail?batch_id='+encodeURIComponent(item.id)})}
});
