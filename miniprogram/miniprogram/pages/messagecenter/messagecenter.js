"use strict";
Page({
    data: {
        loading: false,
        messages: [],
    },
    onShareAppMessage() {
        return {
            title: '消息中心',
            path: '/pages/messagecenter/messagecenter',
        };
    },
});
