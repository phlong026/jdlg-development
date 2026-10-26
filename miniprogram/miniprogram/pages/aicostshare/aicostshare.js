"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
const costEstimateShare_1 = require("../../utils/costEstimateShare");
const costEstimateShareImage_1 = require("../../utils/costEstimateShareImage");
const REPORT_ID = /^[A-Za-z0-9_-]{32}$/;
Page({
    pageReady: false,
    pageDestroyed: false,
    requestRevision: 0,
    data: {
        reportId: '', loading: true, error: '', contentReady: false, shareReady: false,
        result: null, shareTitle: '', chatImage: '', timelineImage: '',
    },
    onLoad(options) {
        const reportId = String(options.report_id || '');
        this.setData({ reportId });
        if (!REPORT_ID.test(reportId)) {
            this.setData({ loading: false, error: '分享报告链接无效，请向分享人获取新链接。' });
            return;
        }
        // 分享报告须登录后查看：未登录时 login-guard 挡住内容，登录成功后再加载快照。
        // 防御式调用：Node 测试环境无组件时跳过门禁，直接加载。
        const select = this.selectComponent;
        if (typeof select === 'function') {
            const guard = select.call(this, '#loginGuard');
            if (guard && typeof guard.check === 'function' && !guard.check())
                return;
        }
        void this.loadPublicSnapshot();
    },
    onLoginSucceeded() {
        // 登录（含一键建号）成功后加载报告内容；昵称引导由内容就绪后的 nickname-guard 接管。
        void this.loadPublicSnapshot();
    },
    onReady() { this.pageReady = true; void this.prepareCovers(); },
    onShow() { if (this.pageReady && !this.data.loading)
        void this.loadPublicSnapshot(); },
    onUnload() { this.pageDestroyed = true; this.requestRevision++; },
    async loadPublicSnapshot() {
        if (!REPORT_ID.test(this.data.reportId))
            return;
        const revision = ++this.requestRevision;
        this.setData({ loading: true, error: '', contentReady: false, shareReady: false });
        try {
            // This API can only read an intentionally published immutable snapshot.
            // Never load local estimates, questionnaire answers or private AI tasks.
            const snapshot = await (0, costEstimateShare_1.getCostEstimateShare)(this.data.reportId);
            if (this.pageDestroyed || revision !== this.requestRevision)
                return;
            const result = snapshot.result;
            const region = result.basicInfo.find(item => item.label === '所在地区')?.value || '建房';
            const city = region.split('/').pop()?.trim() || '建房';
            const area = result.basicInfo.find(item => item.label === '总建筑面积')?.value || '';
            this.setData({ result, loading: false, contentReady: true,
                shareTitle: `${city}${area}${result.totalLabel}${result.estimatedTotalCost}，查看公开测算报告` });
            // 报告就绪即可被转发：已登录但缺昵称的访客在此补全，分享链路上的两端都能显示可读名称。
            this.promptNicknameIfMissing();
            void this.prepareCovers();
        }
        catch (error) {
            if (this.pageDestroyed || revision !== this.requestRevision)
                return;
            this.setData({ result: null, loading: false, contentReady: false, shareReady: false,
                error: error.message || '这份公开报告已过期或撤销，请联系分享人。' });
        }
    },
    async prepareCovers() {
        if (!this.pageReady || !this.data.result || !this.data.contentReady || this.data.shareReady)
            return;
        const revision = this.requestRevision, result = this.data.result;
        const value = (label) => result.basicInfo.find(item => item.label === label)?.value || '';
        const card = { city: value('所在地区').split('/').pop()?.trim() || '建房', buildingArea: value('总建筑面积'),
            structureType: value('结构形式'), windowStandard: value('门窗标准'), totalCost: result.estimatedTotalCost, totalLabel: result.totalLabel };
        try {
            const [chatImage, timelineImage] = await Promise.all([(0, costEstimateShareImage_1.prepareCostEstimateShareImage)(card), (0, costEstimateShareImage_1.prepareCostEstimateTimelineImage)(card)]);
            if (this.pageDestroyed || revision !== this.requestRevision)
                return;
            this.setData({ chatImage, timelineImage, shareReady: !!chatImage && !!timelineImage });
        }
        catch (_) { /* Reading remains available when an optional share cover fails. */ }
    },
    getTimelineSharePayload() {
        if (!this.data.contentReady || !this.data.shareReady)
            return {};
        return { title: this.data.shareTitle, query: `report_id=${encodeURIComponent(this.data.reportId)}`, imageUrl: this.data.timelineImage };
    },
    onShareTimeline() { return this.getTimelineSharePayload(); },
    onShareAppMessage() {
        if (!this.data.contentReady)
            return {};
        return { title: this.data.shareTitle, path: `/pages/aicostshare/aicostshare?report_id=${encodeURIComponent(this.data.reportId)}`,
            imageUrl: this.data.chatImage || undefined };
    },
    retry() { if (!this.data.loading)
        void this.loadPublicSnapshot(); },
    startOwnEstimate() { wx.navigateTo({ url: '/pages/aicost/aicost' }); },
    promptNicknameIfMissing() {
        const select = this.selectComponent;
        if (typeof select !== 'function')
            return;
        const guard = select.call(this, '#nicknameGuard');
        if (!guard || typeof guard.checkAndShow !== 'function')
            return;
        void guard.checkAndShow({ required: true });
    },
});
