// @ts-nocheck
'use strict';
const api = require('../../utils/ai-video-studio-api');
const typeLabel = value => ({ image: '图片', video: '视频', audio: '音频' }[value] || '素材');
const absoluteURL = value => { const url=String(value||'');return url.startsWith('/')?api.ORIGIN+url:url };
const normalizedItem = item => {
  const type=item.media_type||item.kind,url=absoluteURL(item.url),preview=absoluteURL(item.preview_url||item.poster_url||item.thumbnail_url);
  return Object.assign({},item,{media_type:type,duration:Number(item.duration_seconds||item.duration||0),preview_url:type==='image'?(preview||url):preview,media_url:url||((type==='video'||type==='audio')?preview:''),type_label:typeLabel(type)});
};
const mergeMedia = (current, rows) => { const seen=new Set();return (current||[]).concat(rows||[]).filter(item=>{const id=String(item.media_id||'');if(!id||seen.has(id))return false;seen.add(id);return true}) };
Page({
  data: { types: [{value:'',label:'全部'},{value:'image',label:'图片'},{value:'video',label:'视频'},{value:'audio',label:'音频'}], allowedType:'', mediaType:'', keyword:'', items:[], page:1, hasMore:false, loading:true, loadingMore:false, uploading:false, uploadProgress:0, imageUploadEnabled:false, mediaUploadEnabled:false, mediaDisabledReason:'', capabilitiesError:'', error:'', previewError:'', playingAudioID:'', loadingAudioID:'', selectMode:false },
  onLoad(options) {
    this._sessionEpoch=(this._sessionEpoch||0)+1;
    this._unloaded=false;
    this._uploadOperation=null;
    this._token=String(wx.getStorageSync('token')||'');
    this.options=options||{};
    const allowedType=['image','video','audio'].includes(options.allowed_type)?options.allowed_type:'';
    this.setData({selectMode:options.select==='1',allowedType,mediaType:allowedType||options.media_type||'',types:allowedType?[{value:allowedType,label:typeLabel(allowedType)}]:this.data.types});
    this.loadCapabilities();
    this.load();
  },
  onHide(){this._previewEpoch=(this._previewEpoch||0)+1;this.stopAudioPreview(true)},
  onShow(){if(this._token&&this._token!==String(wx.getStorageSync('token')||'')){this._previewEpoch=(this._previewEpoch||0)+1;this.stopAudioPreview(true)}},
  onUnload(){this._previewEpoch=(this._previewEpoch||0)+1;this.stopAudioPreview(true);this._unloaded=true;this._listEpoch=(this._listEpoch||0)+1;this._capEpoch=(this._capEpoch||0)+1;if(this._uploadOperation)this._uploadOperation.cancel()}, current(){return !this._unloaded&&this._token===String(wx.getStorageSync('token')||'')},
  session(){return{epoch:this._sessionEpoch,token:this._token}},
  ownsSession(session){return!!session&&session.epoch===this._sessionEpoch&&session.token===this._token&&this.current()},
  onPullDownRefresh(){this.load().finally(()=>wx.stopPullDownRefresh())}, onKeyword(e){this.setData({keyword:e.detail.value})}, search(){this.load()},
  selectType(e){const value=e.currentTarget.dataset.value;if(this.data.allowedType&&value!==this.data.allowedType)return;this.setData({mediaType:value},()=>this.load())},
  async loadCapabilities(){const epoch=this._capEpoch=(this._capEpoch||0)+1;this.setData({capabilitiesError:''});try{const value=await api.capabilities();if(this.current()&&epoch===this._capEpoch)this.setData({imageUploadEnabled:!!value.image_upload_enabled,mediaUploadEnabled:!!value.media_upload_enabled,mediaDisabledReason:value.media_disabled_reason||'',capabilitiesError:''})}catch(error){if(this.current()&&epoch===this._capEpoch)this.setData({capabilitiesError:error.message||'上传能力读取失败'})}},
  async load(append) {
    const appendPage=append===true,epoch=this._listEpoch=(this._listEpoch||0)+1,page=appendPage?this.data.page+1:1,pageSize=20;
    if(!appendPage)this.stopAudioPreview(true);
    this.setData(appendPage?{loadingMore:true,error:''}:{loading:true,error:'',items:[]});
    try{
      const value=await api.listMedia({page,page_size:pageSize,media_type:this.data.allowedType||this.data.mediaType,keyword:String(this.data.keyword||'').trim()});
      if(!this.current()||epoch!==this._listEpoch)return;
      const rows=(value.items||[]).map(normalizedItem),hasMore=value.has_more!==undefined?!!value.has_more:page*pageSize<Number(value.total||0);
      this.setData({loading:false,loadingMore:false,page,items:appendPage?mergeMedia(this.data.items,rows):rows,hasMore});
    }catch(error){if(this.current()&&epoch===this._listEpoch)this.setData({loading:false,loadingMore:false,error:error.message||'素材加载失败'})}
  },
  loadMore(){if(!this.data.loadingMore)this.load(true)},
  choose(e){if(!this.data.selectMode)return;const item=this.data.items[Number(e.currentTarget.dataset.index)];if(!item)return;if(this.data.allowedType&&item.media_type!==this.data.allowedType){wx.showToast({title:`请选择${typeLabel(this.data.allowedType)}`,icon:'none'});return}const channel=this.getOpenerEventChannel&&this.getOpenerEventChannel();if(channel&&channel.emit)channel.emit('selected',item);wx.navigateBack();},
  chooseUpload(e){const session=this.session(),kind=e.currentTarget.dataset.type;if((this.data.allowedType&&kind!==this.data.allowedType)||this.data.uploading||(kind==='image'?!this.data.imageUploadEnabled:!this.data.mediaUploadEnabled))return;wx.chooseMedia({count:1,mediaType:[kind],sourceType:['album','camera'],success:result=>{if(!this.ownsSession(session))return;const file=result.tempFiles&&result.tempFiles[0];if(file)this.upload(file,kind)},fail:error=>{if(this.ownsSession(session)&&!/cancel/i.test(String(error&&error.errMsg||'')))this.setData({error:'无法打开相册，请检查权限后重试'})}})},
  chooseAudio(){const session=this.session();if((this.data.allowedType&&this.data.allowedType!=='audio')||this.data.uploading||!this.data.mediaUploadEnabled)return;wx.chooseMessageFile({count:1,type:'file',extension:['mp3','wav','ogg','flac'],success:result=>{if(!this.ownsSession(session))return;const file=result.tempFiles&&result.tempFiles[0];if(file)this.upload(file,'audio')},fail:error=>{if(this.ownsSession(session)&&!/cancel/i.test(String(error&&error.errMsg||'')))this.setData({error:'无法读取音频文件，请检查权限后重试'})}})},
  async upload(file,kind){const session=this.session(),path=file.tempFilePath||file.path;if(!path)return;this.setData({uploading:true,uploadProgress:0,error:''});let operation;operation=api.uploadMedia(path,progress=>{if(this.ownsSession(session)&&this._uploadOperation===operation)this.setData({uploadProgress:Number(progress.progress||0)})});this._uploadOperation=operation;try{await operation.promise;if(!this.ownsSession(session)||this._uploadOperation!==operation)return;this._uploadOperation=null;this.setData({uploading:false,uploadProgress:100,mediaType:kind});wx.showToast({title:'素材已上传',icon:'success'});await this.load();}catch(error){if(this.ownsSession(session)&&this._uploadOperation===operation){this._uploadOperation=null;this.setData({uploading:false,error:error.message||'素材上传失败'})}}},
  cancelUpload(){if(this._uploadOperation)this._uploadOperation.cancel()},
  preview(e){
    const session=this.session();
    const epoch=this._previewEpoch=(this._previewEpoch||0)+1;
    const item=this.data.items[Number(e.currentTarget.dataset.index)];
    if(!item)return;
    this.setData({previewError:''});
    if(item.media_type==='image'){
      const images=this.data.items.filter(row=>row.media_type==='image').map(row=>row.preview_url||row.media_url).filter(Boolean),current=item.preview_url||item.media_url;
      if(current)wx.previewImage({current,urls:images,fail:()=>{if(this.current()&&epoch===this._previewEpoch)this.setData({previewError:'图片预览失败'})}});
      return;
    }
    if(item.media_type==='video'){
      this.stopAudioPreview(true);
      if(!item.media_url||typeof wx.previewMedia!=='function'){this.setData({previewError:'视频暂时无法预览'});return}
      wx.previewMedia({sources:[{url:item.media_url,type:'video',poster:item.preview_url||''}],current:0,fail:()=>{if(this.current()&&epoch===this._previewEpoch)this.setData({previewError:'视频预览失败'})}});
      return;
    }
    if(this._audioMediaID===item.media_id&&this._audioPlayer){this.stopAudioPreview(true);return}
    this.stopAudioPreview(true);
    if(!item.media_url||typeof wx.createInnerAudioContext!=='function'){this.setData({previewError:'音频暂时无法试听'});return}
    const player=wx.createInnerAudioContext();this._audioPlayer=player;this._audioMediaID=item.media_id;this.setData({playingAudioID:'',loadingAudioID:item.media_id,previewError:''});player.src=item.media_url;
    player.onPlay(()=>{if(this.ownsSession(session)&&this._audioPlayer===player){if(this._audioLoadTimer)clearTimeout(this._audioLoadTimer);this._audioLoadTimer=null;this.setData({playingAudioID:item.media_id,loadingAudioID:'',previewError:''})}});
    player.onPause(()=>{if(this.ownsSession(session)&&this._audioPlayer===player)this.setData({playingAudioID:'',loadingAudioID:''})});
    player.onEnded(()=>{if(this._audioPlayer===player)this.stopAudioPreview(true)});
    player.onError(()=>{if(this.ownsSession(session)&&this._audioPlayer===player){this.setData({previewError:'音频试听失败'});this.stopAudioPreview(true)}});
    this._audioLoadTimer=setTimeout(()=>{if(this.ownsSession(session)&&this._audioPlayer===player&&this.data.playingAudioID!==item.media_id){this.setData({previewError:'音频加载超时，请重试'});this.stopAudioPreview(true)}},8000);if(this._audioLoadTimer&&this._audioLoadTimer.unref)this._audioLoadTimer.unref();
    player.play();
  },
  stopAudioPreview(destroy){if(this._audioLoadTimer)clearTimeout(this._audioLoadTimer);this._audioLoadTimer=null;const player=this._audioPlayer;this._audioPlayer=null;this._audioMediaID='';this.setData({playingAudioID:'',loadingAudioID:''});if(!player)return;try{player.pause()}catch(_){}if(destroy){try{player.destroy()}catch(_){}}},
});
