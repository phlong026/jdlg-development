Page({
  data: {
    loading: false,
    messages: [] as any[],
  },

  onShareAppMessage() {
    return {
      title: '消息中心',
      path: '/pages/messagecenter/messagecenter',
    };
  },
});
