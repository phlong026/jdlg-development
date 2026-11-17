import { getCostEstimateShare, CostEstimateShareResult } from '../../utils/costEstimateShare';
import { prepareCostEstimateShareImage, prepareCostEstimateTimelineImage } from '../../utils/costEstimateShareImage';
import { prepareRenovationShareImage, prepareRenovationTimelineImage } from '../../utils/renovationCostShareImage';

const REPORT_ID = /^[A-Za-z0-9_-]{32}$/;

function renovationTotalText(result: CostEstimateShareResult): string {
  return `${(Number(result.renovation?.totalAmount || 0) / 10000).toFixed(2)}万元`;
}

function renovationAmountText(value: number): string {
  return `${Math.round(Number(value || 0)).toLocaleString('zh-CN')}元`;
}

Page({
  pageReady: false,
  pageDestroyed: false,
  requestRevision: 0,
  data: {
    reportId: '', loading: true, error: '', contentReady: false, shareReady: false,
    result: null as CostEstimateShareResult | null, isRenovation: false, pageTitle: '公开造价报告', startLabel: '我也测算自己的方案',
    shareTitle: '', chatImage: '', timelineImage: '',
  },
  onLoad(options: Record<string, string | undefined>) {
    this.pageDestroyed = false;
    const reportId = String(options.report_id || '');
    this.setData({ reportId });
    if (!REPORT_ID.test(reportId)) {
      this.setData({ loading: false, error: '分享报告链接无效，请向分享人获取新链接。' });
      return;
    }
    void this.loadPublicSnapshot();
  },
  onReady() { this.pageReady = true; void this.prepareCovers(); },
  onShow() { if (this.pageReady && !this.data.loading) void this.loadPublicSnapshot(); },
  onUnload() { this.pageDestroyed = true; this.requestRevision++; },
  async loadPublicSnapshot() {
    if (!REPORT_ID.test(this.data.reportId)) return;
    const revision = ++this.requestRevision;
    this.setData({ loading: true, error: '', contentReady: false, shareReady: false });
    try {
      // This API can only read an intentionally published immutable snapshot.
      // Never load local estimates, questionnaire answers or private AI tasks.
      const snapshot = await getCostEstimateShare(this.data.reportId);
      if (this.pageDestroyed || revision !== this.requestRevision) return;
      const result = snapshot.result;
      const isRenovation = result.version === 5 && result.quoteType === 'renovation';
      const fallback = isRenovation ? '装修' : '建房';
      const region = result.basicInfo.find(item => item.label === '所在地区')?.value || fallback;
      const city = region.split('/').pop()?.trim() || fallback;
      const area = result.basicInfo.find(item => item.label === (isRenovation ? '装修面积' : '总建筑面积'))?.value || '';
      const totalText = isRenovation ? renovationTotalText(result) : result.estimatedTotalCost;
      this.setData({ result, isRenovation, loading: false, contentReady: true,
        pageTitle: isRenovation ? '公开装修报价' : '公开造价报告', startLabel: isRenovation ? '测算我的装修报价' : '我也测算自己的方案',
        shareTitle: isRenovation
          ? `${city}${area}装修参考总价${totalText}，查看装修报价明细`
          : `${city}${area}${result.totalLabel}${result.estimatedTotalCost}，查看公开测算报告` });
      // 报告就绪即可被转发：已登录但缺昵称的访客在此补全，分享链路上的两端都能显示可读名称。
      this.promptNicknameIfMissing();
      void this.prepareCovers();
    } catch (error: any) {
      if (this.pageDestroyed || revision !== this.requestRevision) return;
      this.setData({ result: null, loading: false, contentReady: false, shareReady: false,
        error: error.message || '这份公开报告已过期或撤销，请联系分享人。' });
    }
  },
  async prepareCovers() {
    if (!this.pageReady || !this.data.result || !this.data.contentReady || this.data.shareReady) return;
    const revision = this.requestRevision, result = this.data.result;
    const value = (label: string) => result.basicInfo.find(item => item.label === label)?.value || '';
    if (this.data.isRenovation) {
      const amount = (name: string) => renovationAmountText(result.costBreakdown.find(item => item.name === name)?.amount || 0);
      const card = { city: value('所在地区').split('/').pop()?.trim() || '装修', area: value('装修面积'), totalCost: renovationTotalText(result),
        materialCost: amount('主材费'), auxiliaryCost: amount('辅材费'), laborCost: amount('人工费') };
      try {
        const [chatImage, timelineImage] = await Promise.all([prepareRenovationShareImage(card), prepareRenovationTimelineImage(card)]);
        if (this.pageDestroyed || revision !== this.requestRevision) return;
        this.setData({ chatImage, timelineImage, shareReady: !!chatImage && !!timelineImage });
      } catch (error) { console.warn('装修报价分享封面暂不可用:', error); }
      return;
    }
    const card = { city: value('所在地区').split('/').pop()?.trim() || '建房', buildingArea: value('总建筑面积'),
      structureType: value('结构形式'), windowStandard: value('门窗标准'), totalCost: result.estimatedTotalCost, totalLabel: result.totalLabel };
    try {
      const [chatImage, timelineImage] = await Promise.all([prepareCostEstimateShareImage(card), prepareCostEstimateTimelineImage(card)]);
      if (this.pageDestroyed || revision !== this.requestRevision) return;
      this.setData({ chatImage, timelineImage, shareReady: !!chatImage && !!timelineImage });
    } catch (_) { /* Reading remains available when an optional share cover fails. */ }
  },
  getTimelineSharePayload() {
    if (!this.data.contentReady || !this.data.shareReady) return {};
    return { title: this.data.shareTitle, query: `report_id=${encodeURIComponent(this.data.reportId)}`, imageUrl: this.data.timelineImage };
  },
  onShareTimeline() { return this.getTimelineSharePayload(); },
  onShareAppMessage() {
    if (!this.data.contentReady) return {};
    return { title: this.data.shareTitle, path: `/pages/aicostshare/aicostshare?report_id=${encodeURIComponent(this.data.reportId)}`,
      imageUrl: this.data.chatImage || undefined };
  },
  retry() { if (!this.data.loading) void this.loadPublicSnapshot(); },
  startOwnEstimate() { wx.navigateTo({ url: this.data.isRenovation ? '/pages/renovationbudget/index' : '/pages/aicost/aicost' }); },
  promptNicknameIfMissing() {
    const select = (this as any).selectComponent;
    if (typeof select !== 'function') return;
    const guard = select.call(this, '#nicknameGuard') as any;
    if (!guard || typeof guard.checkAndShow !== 'function') return;
    void guard.checkAndShow({ required: true });
  },
});
