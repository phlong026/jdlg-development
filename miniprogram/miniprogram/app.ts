// app.ts
import { blockUnsafeText, initPromptReview } from './utils/promptReview';

const TABBAR_IMAGE_PATHS = [
  '/assets/tabbar/未选中态@2x.png',
  '/assets/tabbar/首页导航栏@2x.png',
  '/assets/tabbar/模板广场导航栏@2x.png',
  '/assets/tabbar/设计师导航栏@2x.png',
  '/assets/tabbar/我的导航栏@2x.png'
];

let interceptModalHost: WechatMiniprogram.Component.TrivialInstance | null = null;

App<IAppOption>({
  globalData: {
    blockUnsafeText,
  },

  registerInterceptModal(host: WechatMiniprogram.Component.TrivialInstance) {
    interceptModalHost = host;
  },

  unregisterInterceptModal(host: WechatMiniprogram.Component.TrivialInstance) {
    if (interceptModalHost === host) {
      interceptModalHost = null;
    }
  },

  showInterceptModal(payload: InterceptModalPayload) {
    const host = interceptModalHost as WechatMiniprogram.Component.TrivialInstance & {
      show?: (data: InterceptModalPayload) => void;
    };
    if (host && typeof host.show === 'function') {
      host.show(payload);
      return true;
    }
    return false;
  },

  onLaunch(options: any) {
    // 展示本地存储能力
    const logs = wx.getStorageSync('logs') || []
    logs.unshift(Date.now())
    wx.setStorageSync('logs', logs)
    // 扫码进入：scene 为邀请码；从分享链接进入：query.invite_code。登录时会读取并完成绑定
    const scene = options?.scene
    const query = options?.query || {}
    if (scene != null && scene !== '') {
      wx.setStorageSync('pending_invite_code', String(scene))
    } else if (query.invite_code) {
      wx.setStorageSync('pending_invite_code', query.invite_code)
    }

    TABBAR_IMAGE_PATHS.forEach((src) => {
      wx.getImageInfo({
        src,
        fail: () => {}
      })
    })

    // 本地审核无需初始化，保留空调用兼容旧缓存
    void initPromptReview()
  },
})