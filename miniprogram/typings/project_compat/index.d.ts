declare interface CSSStyleDeclaration {
  [key: string]: string | null | undefined;
}

declare namespace WechatMiniprogram {
  interface CompatClientRect {
    bottom: number;
    height: number;
    left: number;
    right: number;
    top: number;
    width: number;
  }
  // Official miniprogram-api-typings 5.3.1 system-info interfaces.
  // Keep this compatibility subset local instead of upgrading all vendored types.
  interface WindowInfo {
    pixelRatio: number;
    safeArea: SafeArea;
    screenHeight: number;
    screenTop: number;
    screenWidth: number;
    statusBarHeight: number;
    windowHeight: number;
    windowWidth: number;
  }
  interface AppBaseInfo {
    PCKernelVersion: string;
    SDKVersion: string;
    enableDebug: boolean;
    fontSizeScaleFactor: number;
    fontSizeSetting: number;
    host: AppBaseInfoHost;
    language: string;
    version: string;
    theme?: 'dark' | 'light';
  }
  interface AppBaseInfoHost {
    appId: string;
  }
  interface DeviceInfo {
    abi: string;
    benchmarkLevel: number;
    brand: string;
    cpuType: string;
    deviceAbi: string;
    memorySize: string;
    model: string;
    platform: 'ios' | 'android' | 'ohos' | 'ohos_pc' | 'windows' | 'mac' | 'devtools';
    system: string;
  }
  interface Wx {
    /** https://developers.weixin.qq.com/miniprogram/dev/api/base/system/wx.getWindowInfo.html */
    getWindowInfo(): WindowInfo;
    /** https://developers.weixin.qq.com/miniprogram/dev/api/base/system/wx.getDeviceInfo.html */
    getDeviceInfo(): DeviceInfo;
    /** https://developers.weixin.qq.com/miniprogram/dev/api/base/system/wx.getAppBaseInfo.html */
    getAppBaseInfo(): AppBaseInfo;
  }
  interface OnMenuButtonBoundingClientRectWeightChangeListenerResult extends CompatClientRect {}
}

declare module 'dayjs' {
  export interface Dayjs {
    [key: string]: any;
  }
  const dayjs: any;
  export default dayjs;
}

declare module 'dayjs/locale/en' {
  const enLocale: any;
  export = enLocale;
}
