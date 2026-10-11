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
  interface WindowInfo extends SystemInfo {}
  interface AppBaseInfo {
    [key: string]: any;
  }
  interface DeviceInfo extends SystemInfo {}
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
