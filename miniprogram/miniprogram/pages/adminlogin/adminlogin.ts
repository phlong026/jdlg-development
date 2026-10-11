const ADMIN_LOGIN_WEBVIEW_URL = 'https://api.jiadilingguang.com/admin/login';

Page({
  data: {
    webUrl: ADMIN_LOGIN_WEBVIEW_URL,
  },

  onLoad() {
    this.setData({
      webUrl: ADMIN_LOGIN_WEBVIEW_URL,
    });
  },

  onShareAppMessage() {
    return {
      title: '后台管理登录',
      path: '/pages/adminlogin/adminlogin',
    };
  },
});
