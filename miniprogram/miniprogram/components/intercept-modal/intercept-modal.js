"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
function getAppOption() {
    return getApp();
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
            getAppOption().registerInterceptModal?.(this);
        },
        detached() {
            getAppOption().unregisterInterceptModal?.(this);
        },
    },
    pageLifetimes: {
        show() {
            getAppOption().registerInterceptModal?.(this);
        },
    },
    methods: {
        show(payload = {}) {
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
        stopPropagation() { },
    },
});
