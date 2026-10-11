const ADMIN_HOME_WEBVIEW_URL = 'https://api.jiadilingguang.com/admin/';

Page({
  data: {
    webUrl: ADMIN_HOME_WEBVIEW_URL,
  },

  onLoad() {
    this.setData({
      webUrl: ADMIN_HOME_WEBVIEW_URL,
    });
  },

  onShareAppMessage() {
    return {
      title: '后台管理',
      path: '/pages/adminlogin/adminlogin',
    };
  },
});
