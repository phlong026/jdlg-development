function record(page, event) {
  const detail=event.detail||{};
  const value=event.type==='submit'?detail.value?.nickname:detail.value;
  console.log('[nickname-probe]',{
    at:Date.now(),event:event.type,keys:Object.keys(detail),
    hasValue:typeof value==='string',utf16:typeof value==='string'?value.length:null,
    codePoints:typeof value==='string'?Array.from(value).length:null,
    equalsInputCache:typeof value==='string'?value===page.data.nickname:null,
    pass:detail.pass,timeout:detail.timeout,saving:page.data.saving,
  });
}
Page({
  data:{nickname:'',saving:false},
  onLoad(){console.log('[nickname-probe]',wx.getSystemInfoSync().SDKVersion);},
  onInput(e){record(this,e);this.setData({nickname:e.detail.value});},
  onObserved(e){record(this,e);},
  onSubmit(e){
    record(this,e);
    this.setData({saving:true});
    setTimeout(()=>this.setData({saving:false}),500);
  },
  openObserved(){wx.navigateTo({url:'/pages/observed'});},
});
