const ROUTES = { building: '/pages/aicost/aicost', renovation: '/pages/renovationbudget/index' };
const LABELS = { building: '建房报价', renovation: '装修报价' };
function stackLimit(error) { return /(page\s*stack|stack\s*limit|page.*limit|页面栈)/i.test(String(error && error.errMsg || error || '')); }

Component({
  properties: { kind: { type: String, value: 'building' } },
  data: { open: false, labels: LABELS },
  methods: {
    toggle() { this.setData({ open: !this.data.open }); },
    close() { this.setData({ open: false }); },
    select(e) {
      const kind = String(e.currentTarget.dataset.kind || '');
      this.setData({ open: false });
      if (!ROUTES[kind] || kind === this.data.kind) return;
      const targetRoute = ROUTES[kind].replace(/^\//, '');
      const pages = typeof getCurrentPages === 'function' ? getCurrentPages() : [];
      let targetIndex = -1;
      for (let index = pages.length - 2; index >= 0; index -= 1) {
        if (String(pages[index] && pages[index].route || '').replace(/^\//, '') === targetRoute) { targetIndex = index; break; }
      }
      if (targetIndex >= 0) {
        wx.navigateBack({ delta: pages.length - 1 - targetIndex, fail: () => wx.showToast({ title: '报价页切换失败，请重试', icon: 'none' }) });
        return;
      }
      wx.navigateTo({
        url: ROUTES[kind],
        fail: (error) => {
          if (stackLimit(error)) { wx.redirectTo({ url: ROUTES[kind] }); return; }
          wx.showToast({ title: '报价页打开失败，请重试', icon: 'none' });
        },
      });
    },
  },
});
