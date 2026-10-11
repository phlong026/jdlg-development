type Transfer = {
  sequence: number;
  disposed: boolean;
  task?: WechatMiniprogram.DownloadTask;
};

const transfers = new WeakMap<object, Transfer>();

Component({
  properties: {
    status: { type: String, value: 'loading' },
    mode: { type: String, value: 'preview' },
    documentUrl: { type: String, value: '' },
    title: { type: String, value: '你的家庭建房报告' },
  },
  data: { busy: false, error: '' },
  lifetimes: {
    attached() {
      transfers.set(this, { sequence: 0, disposed: false });
    },
    detached() {
      const transfer = transfers.get(this);
      if (transfer) {
        transfer.disposed = true;
        transfer.sequence++;
        transfer.task?.abort();
        transfers.delete(this);
      }
    },
  },
  observers: {
    'documentUrl,status'() {
      const transfer = transfers.get(this);
      if (transfer) {
        transfer.sequence++;
        transfer.task?.abort();
        transfer.task = undefined;
        this.setData({ busy: false, error: '' });
      }
    },
  },
  methods: {
    retry() {
      if (!this.data.busy) this.triggerEvent('refresh');
    },
    openReport() {
      const transfer = transfers.get(this);
      if (!transfer || transfer.disposed || this.data.busy || this.properties.status !== 'ready') return;
      // Only the authenticated server may provide this short-lived URL.
      // Displaying this component never grants a paid entitlement.
      const url = this.properties.documentUrl;
      if (!url.startsWith('https://') || url.length <= 8 || url.trim() !== url) {
        this.setData({ error: '报告链接暂不可用，请刷新后重试。' });
        return;
      }
      const sequence = ++transfer.sequence;
      const isCurrent = () => !transfer.disposed && sequence === transfer.sequence && url === this.properties.documentUrl;
      const fail = (message: string) => {
        if (isCurrent()) {
          transfer.task = undefined;
          this.setData({ busy: false, error: message });
        }
      };
      this.setData({ busy: true, error: '' });
      transfer.task = wx.downloadFile({
        url,
        timeout: 60000,
        success: (response) => {
          if (!isCurrent()) return;
          transfer.task = undefined;
          if (response.statusCode !== 200 || !response.tempFilePath) {
            fail('报告链接可能已过期，请刷新后重试。');
            return;
          }
          wx.openDocument({
            filePath: response.tempFilePath,
            fileType: 'pdf',
            showMenu: true,
            success: () => {
              if (isCurrent()) this.setData({ busy: false });
            },
            fail: () => fail('暂时无法打开报告，请重试。'),
          });
        },
        fail: () => fail('报告下载失败，请检查网络后重试。'),
      });
    },
  },
});
