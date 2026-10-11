/// <reference path="./types/index.d.ts" />

type InterceptModalPayload = {
  title?: string;
  message?: string;
  hint?: string;
};

interface IAppOption {
  globalData: {
    userInfo?: WechatMiniprogram.UserInfo,
    blockUnsafeText?: (text: string, options?: Record<string, unknown>) => boolean,
  }
  userInfoReadyCallback?: WechatMiniprogram.GetUserInfoSuccessCallback,
  registerInterceptModal?: (host: WechatMiniprogram.Component.TrivialInstance) => void,
  unregisterInterceptModal?: (host: WechatMiniprogram.Component.TrivialInstance) => void,
  showInterceptModal?: (payload: InterceptModalPayload) => boolean,
}
