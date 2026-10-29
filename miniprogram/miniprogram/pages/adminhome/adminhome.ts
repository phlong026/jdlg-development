const ADMIN_HOME_WEBVIEW_URL = 'https://api.jiadilingguang.com/admin/';
const CUSTOMER_MANAGEMENT_WEBVIEW_URL = `${ADMIN_HOME_WEBVIEW_URL}customer-management`;

function resolveAdminWebviewURL(target = '') {
  return target === 'customer-management'
    ? CUSTOMER_MANAGEMENT_WEBVIEW_URL
    : ADMIN_HOME_WEBVIEW_URL;
}

Page({
  data: {
    webUrl: ADMIN_HOME_WEBVIEW_URL,
  },

  onLoad(options: { target?: string } = {}) {
    this.setData({
      webUrl: resolveAdminWebviewURL(String(options.target || '')),
    });
  },

  onShareAppMessage() {
    return {
      title: '后台管理',
      path: '/pages/adminlogin/adminlogin',
    };
  },
});
