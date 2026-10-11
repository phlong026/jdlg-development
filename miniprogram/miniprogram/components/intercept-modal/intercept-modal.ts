export type InterceptModalPayload = {
  title?: string;
  message?: string;
  hint?: string;
};

type InterceptModalHost = WechatMiniprogram.Component.TrivialInstance & {
  show?: (payload: InterceptModalPayload) => void;
};

function getAppOption() {
  return getApp<IAppOption>();
}

Component({
  data: {
    visible: false,
    title: '内容拦截',
    message: '内容包含违规信息，请修改后重试',
    hint: '请修改内容后再发送。',
  },

  lifetimes: {
    attached() {
      getAppOption().registerInterceptModal?.(this as InterceptModalHost);
    },
    detached() {
      getAppOption().unregisterInterceptModal?.(this as InterceptModalHost);
    },
  },

  pageLifetimes: {
    show() {
      getAppOption().registerInterceptModal?.(this as InterceptModalHost);
    },
  },

  methods: {
    show(payload: InterceptModalPayload = {}) {
      this.setData({
        visible: true,
        title: payload.title || '内容拦截',
        message: payload.message || '内容包含违规信息，请修改后重试',
        hint: payload.hint || '请修改内容后再发送。',
      });
    },

    onConfirm() {
      this.setData({ visible: false });
    },

    onMaskTap() {
      this.setData({ visible: false });
    },

    stopPropagation() {},
  },
});
