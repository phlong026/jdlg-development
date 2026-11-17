// @ts-nocheck
'use strict';
const api=require('../../utils/ai-video-studio-api');
const mergePrompts=(current,rows)=>{const seen=new Set();return(current||[]).concat(rows||[]).filter(item=>{const id=String(item.prompt_id||'');if(!id||seen.has(id))return false;seen.add(id);return true})};
Page({
  data:{name:'',prompt:'',items:[],page:1,hasMore:false,loading:true,loadingMore:false,saving:false,error:'',selectMode:false,mode:''},
  onLoad(o){this._sessionEpoch=(this._sessionEpoch||0)+1;this._unloaded=false;this._token=String(wx.getStorageSync('token')||'');this.setData({selectMode:o.select==='1',mode:o.mode||'',saving:false});this.load()},
  onUnload(){this._unloaded=true;this._epoch=(this._epoch||0)+1},
  current(epoch){return !this._unloaded&&this._token===String(wx.getStorageSync('token')||'')&&(epoch===undefined||epoch===this._epoch)},
  session(){return{epoch:this._sessionEpoch,token:this._token}},
  ownsSession(session){return!!session&&session.epoch===this._sessionEpoch&&session.token===this._token&&this.current()},
  inputName(e){this.setData({name:e.detail.value})},
  inputPrompt(e){this.setData({prompt:String(e.detail.value||'').slice(0,7000)})},
  openTutorial(){wx.navigateTo({url:'/pages/aivideostudio/tutorial?mode='+encodeURIComponent(this.data.mode||'text')})},
  async load(append){
    const appendPage=append===true,epoch=this._epoch=(this._epoch||0)+1,page=appendPage?this.data.page+1:1,pageSize=50;
    this.setData(appendPage?{loadingMore:true,error:''}:{loading:true,error:'',items:[]});
    try{
      const value=await api.listPrompts({page,page_size:pageSize,mode:this.data.mode});
      if(!this.current(epoch))return;
      const items=value.items||[],hasMore=value.has_more!==undefined?!!value.has_more:page*pageSize<Number(value.total||0);
      this.setData({loading:false,loadingMore:false,page,hasMore,items:appendPage?mergePrompts(this.data.items,items):items});
    }catch(e){if(this.current(epoch))this.setData({loading:false,loadingMore:false,error:e.message||'提示词加载失败'})}
  },
  loadMore(){if(!this.data.loadingMore&&this.data.hasMore)return this.load(true)},
  async save(){const session=this.session(),name=String(this.data.name||'').trim(),prompt=String(this.data.prompt||'').trim();if(!name||!prompt){this.setData({error:'请输入名称和提示词内容'});return}this.setData({saving:true,error:''});try{await api.createPrompt({name,mode:this.data.mode||'all',prompt});if(!this.ownsSession(session))return;this.setData({saving:false,name:'',prompt:''});await this.load()}catch(e){if(this.ownsSession(session))this.setData({saving:false,error:e.message||'保存失败'})}},
  choose(e){const session=this.session(),item=this.data.items[Number(e.currentTarget.dataset.index)],channel=this.getOpenerEventChannel&&this.getOpenerEventChannel();wx.showModal({title:'导入提示词',content:'将覆盖当前输入，是否继续？',success:r=>{if(r.confirm&&this.ownsSession(session)){if(channel&&channel.emit)channel.emit('selected',item);wx.navigateBack()}}})},
  remove(e){const session=this.session(),item=this.data.items[Number(e.currentTarget.dataset.index)];wx.showModal({title:'删除提示词',content:'已提交任务中的输入快照不会被删除。',success:async r=>{if(!r.confirm||!this.ownsSession(session))return;try{await api.deletePrompt(item.prompt_id,item.revision);if(this.ownsSession(session))this.load()}catch(error){if(this.ownsSession(session))this.setData({error:error.message||'删除失败'})}}})}
});
