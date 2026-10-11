const RETURN_KEY = 'report_login_return';
export function validReportPath(path: unknown): path is string {
    return typeof path === 'string' && /^\/pages\/report\/(landing|questionnaire|preview|delivery)(\?id=[A-Za-z0-9_-]{1,80})?$/.test(path);
}
export function requireReportLogin(path: string): boolean {
    if (wx.getStorageSync('token'))
        return true;
    if (!validReportPath(path))
        return false;
    wx.setStorageSync(RETURN_KEY, { path, expires: Date.now() + 30 * 60 * 1000 });
    wx.navigateTo({ url: '/pages/login/login', fail: () => wx.showToast({ title: '暂时无法打开登录，请重试', icon: 'none' }) });
    return false;
}
export function resumeReportAfterLogin(): boolean {
    const pending = wx.getStorageSync(RETURN_KEY);
    if (!pending)
        return false;
    wx.removeStorageSync(RETURN_KEY);
    if (!wx.getStorageSync('token') || pending.expires < Date.now() || !validReportPath(pending.path))
        return false;
    wx.redirectTo({ url: pending.path, fail: () => wx.reLaunch({ url: pending.path }) });
    return true;
}
