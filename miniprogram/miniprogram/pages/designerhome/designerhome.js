"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
const asset_1 = require("../../utils/asset");
const enterpriseWechat_1 = require("../../utils/enterpriseWechat");
const favoriteApi_1 = require("../../utils/favoriteApi");
const API_BASE_URL = 'https://api.jiadilingguang.com';
const DEFAULT_CONSULT_WECHAT_TIP = '扫码添加企业微信，备注“设计咨询”，即可继续沟通服务内容、合作方式和报价。';
const DEFAULT_DESIGNER_WORK_IMAGE = (0, asset_1.resolveAssetPath)('/assets/images/home.jpg');
const INITIAL_VISIBLE_WORK_COUNT = 24;
const WORK_COUNT_STEP = 24;
function normalizeImageUrl(url, fallback = '') {
    const cleanUrl = String(url || '').trim();
    if (!cleanUrl) {
        return fallback;
    }
    if (/^(https?:\/\/|wxfile:\/\/|file:\/\/|data:)/i.test(cleanUrl)) {
        return /^https?:\/\//i.test(cleanUrl) ? (0, asset_1.normalizeCosUrl)(cleanUrl) : cleanUrl;
    }
    if (cleanUrl.startsWith('//')) {
        return (0, asset_1.normalizeCosUrl)(`https:${cleanUrl}`);
    }
    if (cleanUrl.startsWith('/')) {
        return `${API_BASE_URL}${cleanUrl}`;
    }
    return `${API_BASE_URL}/${cleanUrl}`;
}
function formatCompactCount(value) {
    const num = Number(value || 0);
    if (num >= 10000)
        return `${(num / 10000).toFixed(1).replace(/\.0$/, '')}W`;
    if (num >= 1000) {
        return `${(num / 1000).toFixed(1).replace(/\.0$/, '')}K`;
    }
    return String(num);
}
function formatDateText(value) {
    if (!value)
        return '--';
    const date = new Date(value);
    if (Number.isNaN(date.getTime())) {
        return String(value).slice(0, 10);
    }
    return `${date.getFullYear()}-${String(date.getMonth() + 1).padStart(2, '0')}-${String(date.getDate()).padStart(2, '0')}`;
}
function getAvatarText(name) {
    const text = String(name || '').trim();
    return text ? text.slice(0, 1) : '设';
}
function mapCertStatusLabel(status) {
    const value = String(status || '').trim();
    if (value === 'approved')
        return '已认证';
    if (value === 'pending_review')
        return '审核中';
    if (value === 'pending_payment')
        return '待支付';
    if (value === 'rejected')
        return '未通过';
    return '';
}
function buildSpecialtyTags(value, fallbackTitle) {
    const cleanValue = String(value || '').replace(/^专业领域[:：]?/, '').trim();
    const source = cleanValue || fallbackTitle;
    return source
        .split(/[，,、\s/]+/)
        .map((item) => String(item || '').trim())
        .filter((item, index, list) => Boolean(item) && list.indexOf(item) === index)
        .slice(0, 4);
}
function buildServiceIntroBrief(value) {
    const cleanValue = String(value || '').replace(/\s+/g, ' ').trim();
    if (!cleanValue) {
        return '适合先沟通需求方向、空间风格和服务边界，再决定是否继续深入合作。';
    }
    return cleanValue.length > 34 ? `${cleanValue.slice(0, 34)}...` : cleanValue;
}
function buildCompactServiceType(value) {
    const cleanValue = String(value || '').trim();
    if (!cleanValue) {
        return '设计服务';
    }
    const normalized = cleanValue.replace(/(设计服务|设计师服务|设计师|服务)$/g, '').trim() || cleanValue;
    if (/(乡墅|乡村别墅)/.test(normalized) && /定制/.test(normalized)) {
        return '乡墅定制';
    }
    if (/别墅/.test(normalized) && /定制/.test(normalized)) {
        return '别墅定制';
    }
    if (/庭院/.test(normalized) && /(规划|设计|营造)/.test(normalized)) {
        return '庭院设计';
    }
    if (/室内/.test(normalized) && /软装/.test(normalized)) {
        return '室内软装';
    }
    if (/室内/.test(normalized) && /设计/.test(normalized)) {
        return '室内设计';
    }
    if (/建筑/.test(normalized) && /改造/.test(normalized)) {
        return '建筑改造';
    }
    return normalized.length > 6 ? `${normalized.slice(0, 6)}…` : normalized;
}
function buildAvatarManageHint(isSelfHomepage, expanded) {
    if (!isSelfHomepage) {
        return '';
    }
    return expanded ? '再次点击头像可收起主页资料管理区' : '点击头像可展开主页资料管理区';
}
Page({
    data: {
        userId: 0,
        isSelfHomepage: false,
        loading: true,
        isFollowed: false,
        isFavorited: false,
        favoriteLoading: false,
        contactPopupVisible: false,
        supplierWechatQr: enterpriseWechat_1.DEFAULT_ENTERPRISE_WECHAT_QRCODE,
        supplierWechatQrAvailable: true,
        enterpriseWechatTip: DEFAULT_CONSULT_WECHAT_TIP,
        enterpriseWechatContact: '',
        supplierPhone: enterpriseWechat_1.DEFAULT_ENTERPRISE_SERVICE_PHONE,
        enterpriseWechatCustomerServiceCorpId: enterpriseWechat_1.DEFAULT_CUSTOMER_SERVICE_CORP_ID,
        enterpriseWechatCustomerServiceUrl: enterpriseWechat_1.DEFAULT_CUSTOMER_SERVICE_URL,
        enterpriseWechatConfigLoading: false,
        navSafeTop: 0,
        navBarHeight: 96,
        navContentHeight: 44,
        navSideWidth: 88,
        currentTopTab: 'works',
        currentReviewTab: 'positive',
        profile: {
            name: '设计师主页',
            avatar: '',
            avatarText: '设',
            title: '室内设计师',
            experienceText: '一年经验',
            bioText: '个人信息：室内设计师',
            specialtiesText: '专业领域：室内设计，室外设计，庭院设计',
            certStatusLabel: '',
        },
        stats: {
            totalWorks: 0,
            totalOrders: 0,
            totalEarningsText: '0',
            monthOrders: 0,
        },
        serviceConfig: {
            serviceType: '设计服务',
            serviceTypeCompact: '设计服务',
            createdAtText: '--',
            totalEarningsText: '0 灵石',
            monthEarningsText: '0 灵石',
            totalOrders: 0,
            feeText: '免费',
            serviceIntro: '',
            serviceIntroBrief: '适合先沟通需求方向、空间风格和服务边界，再决定是否继续深入合作。',
            serviceEnabled: false,
        },
        reviewSummary: {
            positiveCount: 0,
            negativeCount: 0,
        },
        showProfileOverviewSections: false,
        avatarManageHint: '',
        specialtyTags: [],
        allWorks: [],
        visibleWorkCount: INITIAL_VISIBLE_WORK_COUNT,
        worksHasMore: false,
        worksLeft: [],
        worksRight: [],
        allReviews: [],
        filteredReviews: [],
    },
    onLoad(options) {
        this.initNavLayout();
        const userId = Number(options?.userId || options?.id || 0);
        const currentUserId = this.getCurrentUserId();
        if (!userId) {
            wx.showToast({
                title: '缺少设计师信息',
                icon: 'none',
            });
            this.setData({ loading: false });
            return;
        }
        this.setData({
            userId,
            isSelfHomepage: currentUserId > 0 && currentUserId === userId,
            isFollowed: false,
            showProfileOverviewSections: false,
            avatarManageHint: buildAvatarManageHint(currentUserId > 0 && currentUserId === userId, false),
        });
        this.loadDesignerHomepage();
        this.loadFollowState();
        this.loadFavoriteState();
    },
    onPullDownRefresh() {
        this.loadDesignerHomepage();
        this.loadFollowState();
        this.loadFavoriteState();
    },
    splitWorks(works) {
        const left = [];
        const right = [];
        works.forEach((item, index) => {
            if (index % 2 === 0) {
                left.push(item);
            }
            else {
                right.push(item);
            }
        });
        return { left, right };
    },
    syncVisibleWorks(allWorks, visibleWorkCount) {
        const nextVisibleWorkCount = Math.min(Math.max(visibleWorkCount ?? this.data.visibleWorkCount ?? INITIAL_VISIBLE_WORK_COUNT, 0), allWorks.length);
        const visibleWorks = allWorks.slice(0, nextVisibleWorkCount);
        const columns = this.splitWorks(visibleWorks);
        this.setData({
            allWorks,
            visibleWorkCount: nextVisibleWorkCount,
            worksHasMore: nextVisibleWorkCount < allWorks.length,
            worksLeft: columns.left,
            worksRight: columns.right,
        });
    },
    applyReviewFilter(tab) {
        const activeTab = tab || this.data.currentReviewTab;
        const filteredReviews = (this.data.allReviews || []).filter((item) => item.sentiment === activeTab);
        this.setData({
            currentReviewTab: activeTab,
            filteredReviews,
        });
    },
    initNavLayout() {
        try {
            const systemInfo = wx.getSystemInfoSync();
            const menuRect = typeof wx.getMenuButtonBoundingClientRect === 'function'
                ? wx.getMenuButtonBoundingClientRect()
                : null;
            const safeTop = Number(systemInfo.statusBarHeight || systemInfo.safeArea?.top || 0);
            if (menuRect) {
                const navBarHeight = Number(menuRect.bottom + menuRect.top - safeTop);
                const navContentHeight = Number(menuRect.height);
                const navSideWidth = Number(systemInfo.windowWidth - menuRect.left);
                this.setData({
                    navSafeTop: safeTop,
                    navBarHeight,
                    navContentHeight,
                    navSideWidth,
                });
                return;
            }
            this.setData({
                navSafeTop: safeTop,
                navBarHeight: safeTop + 44,
                navContentHeight: 44,
                navSideWidth: 96,
            });
        }
        catch (error) {
            this.setData({
                navSafeTop: 20,
                navBarHeight: 64,
                navContentHeight: 44,
                navSideWidth: 96,
            });
        }
    },
    getAuthToken() {
        return String(wx.getStorageSync('token') || '').trim();
    },
    getCurrentUserId() {
        const userInfo = wx.getStorageSync('userInfo') || {};
        return Number(userInfo.id || userInfo.userId || 0);
    },
    async loadFollowState() {
        const userId = Number(this.data.userId || 0);
        const token = this.getAuthToken();
        if (!userId || !token) {
            this.setData({ isFollowed: false });
            return;
        }
        try {
            const data = await new Promise((resolve, reject) => {
                wx.request({
                    url: `${API_BASE_URL}/api/v1/miniprogram/user/designers/${userId}/follow`,
                    method: 'GET',
                    header: { token },
                    success: (res) => {
                        const body = (res.data || {});
                        if (res.statusCode === 200 && body.code === 0) {
                            resolve(body.data || {});
                            return;
                        }
                        reject(new Error(body.msg || `请求失败: ${res.statusCode}`));
                    },
                    fail: reject,
                });
            });
            this.setData({
                isFollowed: data?.followed === true,
            });
        }
        catch (error) {
            console.error('加载关注状态失败:', error);
            this.setData({ isFollowed: false });
        }
    },
    async loadFavoriteState() {
        const userId = Number(this.data.userId || 0);
        if (!userId || this.data.isSelfHomepage) {
            this.setData({ isFavorited: false });
            return;
        }
        try {
            const data = await (0, favoriteApi_1.fetchFavoriteStatus)('designer', userId);
            this.setData({ isFavorited: data.favorited === true });
        }
        catch (error) {
            this.setData({ isFavorited: false });
        }
    },
    async loadDesignerHomepage() {
        const userId = Number(this.data.userId || 0);
        if (!userId) {
            wx.stopPullDownRefresh();
            return;
        }
        this.setData({ loading: true });
        try {
            const data = await new Promise((resolve, reject) => {
                wx.request({
                    url: `${API_BASE_URL}/api/v1/miniprogram/designers/${userId}/homepage`,
                    method: 'GET',
                    success: (res) => {
                        const body = (res.data || {});
                        if (res.statusCode === 200 && body.code === 0) {
                            resolve(body.data || {});
                            return;
                        }
                        reject(new Error(body.msg || `请求失败: ${res.statusCode}`));
                    },
                    fail: reject,
                });
            });
            const works = Array.isArray(data.works) ? data.works : [];
            const mappedWorks = works.map((item) => ({
                id: Number(item?.id) || 0,
                title: String(item?.title || '未命名作品'),
                image: normalizeImageUrl(String(item?.image || ''), DEFAULT_DESIGNER_WORK_IMAGE),
                publishScope: String(item?.publish_scope || 'square'),
                tags: Array.isArray(item?.tags) ? item.tags.slice(0, 3).map((tag) => String(tag || '')) : [],
                viewsText: formatCompactCount(Number(item?.views) || 0),
                likesText: formatCompactCount(Number(item?.likes) || 0),
            }));
            const workTitleIdMap = mappedWorks.reduce((map, item) => {
                const title = String(item?.title || '').trim();
                if (title && !map[title]) {
                    map[title] = Number(item.id) || 0;
                }
                return map;
            }, {});
            const reviews = Array.isArray(data.reviews) ? data.reviews : [];
            const mappedReviews = reviews.map((item) => {
                const score = Math.max(0, Math.min(5, Number(item?.score) || 0));
                const relatedTitle = String(item?.related_title || '').trim();
                const relatedTemplateId = Number(item?.related_template_id) || 0;
                return {
                    id: Number(item?.id) || 0,
                    name: String(item?.name || '匿名用户'),
                    avatar: normalizeImageUrl(String(item?.avatar || '')),
                    avatarText: getAvatarText(String(item?.name || '')),
                    content: String(item?.content || ''),
                    dateText: formatDateText(String(item?.created_at || '')),
                    sentiment: String(item?.sentiment || 'positive') === 'negative' ? 'negative' : 'positive',
                    stars: [1, 2, 3, 4, 5].map((star) => star <= score),
                    relatedLabel: String(item?.related_label || ''),
                    relatedTitle,
                    relatedWorkId: relatedTemplateId || workTitleIdMap[relatedTitle] || 0,
                };
            });
            this.setData({
                loading: false,
                profile: {
                    name: String(data?.profile?.name || '设计师主页'),
                    avatar: normalizeImageUrl(String(data?.profile?.avatar || '')),
                    avatarText: getAvatarText(String(data?.profile?.name || '设计师主页')),
                    title: String(data?.profile?.title || '室内设计师'),
                    experienceText: String(data?.profile?.experience_text || '一年经验'),
                    bioText: String(data?.profile?.bio_text || '个人信息：室内设计师'),
                    specialtiesText: String(data?.profile?.specialties_text || '专业领域：室内设计，室外设计，庭院设计'),
                    certStatusLabel: mapCertStatusLabel(String(data?.profile?.cert_status || '')),
                },
                stats: {
                    totalWorks: Number(data?.stats?.total_works) || 0,
                    totalOrders: Number(data?.stats?.total_orders) || 0,
                    totalEarningsText: formatCompactCount(Number(data?.stats?.total_earnings) || 0),
                    monthOrders: Number(data?.stats?.month_orders) || 0,
                },
                serviceConfig: {
                    serviceType: String(data?.service_config?.service_type || '设计服务'),
                    serviceTypeCompact: buildCompactServiceType(String(data?.service_config?.service_type || '设计服务')),
                    createdAtText: String(data?.service_config?.created_at_text || '--'),
                    totalEarningsText: `${Number(data?.service_config?.total_earnings) || 0} 灵石`,
                    monthEarningsText: `${Number(data?.service_config?.month_earnings) || 0} 灵石`,
                    totalOrders: Number(data?.service_config?.total_orders) || 0,
                    feeText: Number(data?.service_config?.fee) > 0 ? `${Number(data?.service_config?.fee)} 灵石` : '免费',
                    serviceIntro: String(data?.service_config?.service_intro || ''),
                    serviceIntroBrief: buildServiceIntroBrief(String(data?.service_config?.service_intro || '')),
                    serviceEnabled: data?.service_config?.service_enabled === true,
                },
                reviewSummary: {
                    positiveCount: Number(data?.review_summary?.positive_count) || 0,
                    negativeCount: Number(data?.review_summary?.negative_count) || 0,
                },
                specialtyTags: buildSpecialtyTags(String(data?.profile?.specialties_text || ''), String(data?.profile?.title || '设计师')),
                allReviews: mappedReviews,
            });
            this.syncVisibleWorks(mappedWorks, Math.min(INITIAL_VISIBLE_WORK_COUNT, mappedWorks.length));
            this.applyReviewFilter(this.data.currentReviewTab);
        }
        catch (error) {
            this.setData({
                loading: false,
                allWorks: [],
                visibleWorkCount: INITIAL_VISIBLE_WORK_COUNT,
                worksHasMore: false,
                worksLeft: [],
                worksRight: [],
                allReviews: [],
                filteredReviews: [],
            });
            wx.showToast({
                title: String(error?.message || '加载设计师主页失败，请下拉重试'),
                icon: 'none',
            });
        }
        finally {
            wx.stopPullDownRefresh();
        }
    },
    onTopTabTap(e) {
        const tab = String(e.currentTarget.dataset.tab || 'works');
        if (tab === this.data.currentTopTab) {
            return;
        }
        this.setData({ currentTopTab: tab });
    },
    onViewWorksTab() {
        if (this.data.currentTopTab !== 'works') {
            this.setData({ currentTopTab: 'works' });
        }
    },
    onViewServiceTab() {
        if (this.data.currentTopTab !== 'service') {
            this.setData({ currentTopTab: 'service' });
        }
    },
    onLoadMoreWorks() {
        const allWorks = Array.isArray(this.data.allWorks) ? this.data.allWorks : [];
        if (!allWorks.length || !this.data.worksHasMore) {
            return;
        }
        this.syncVisibleWorks(allWorks, this.data.visibleWorkCount + WORK_COUNT_STEP);
    },
    onGoToOrderReview() {
        wx.navigateTo({
            url: '/pages/ordermanagement/ordermanagement',
            fail: () => {
                wx.showToast({
                    title: '页面跳转失败',
                    icon: 'none',
                });
            },
        });
    },
    onReviewTabTap(e) {
        const tab = String(e.currentTarget.dataset.tab || 'positive');
        if (tab === this.data.currentReviewTab) {
            return;
        }
        this.applyReviewFilter(tab);
    },
    onWorkTap(e) {
        const id = Number(e.currentTarget.dataset.id);
        const publishScope = String(e.currentTarget.dataset.publishScope || 'square');
        const image = String(e.currentTarget.dataset.image || '');
        if (publishScope !== 'square') {
            if (image) {
                wx.previewImage({
                    current: image,
                    urls: [image],
                });
                return;
            }
            wx.showToast({
                title: '该作品仅在主页展示',
                icon: 'none',
            });
            return;
        }
        if (!id) {
            return;
        }
        wx.navigateTo({
            url: `/pages/templatesquaredetails/templatesquaredetails?id=${id}`,
            fail: () => {
                wx.showToast({
                    title: '页面跳转失败',
                    icon: 'none',
                });
            },
        });
    },
    onReviewRelatedTap(e) {
        const id = Number(e.currentTarget.dataset.id || 0);
        const label = String(e.currentTarget.dataset.label || '');
        if (!id) {
            wx.showToast({
                title: label === '关联服务' ? '当前为服务评价，暂无详情页' : '暂未匹配到关联作品详情',
                icon: 'none',
            });
            return;
        }
        wx.navigateTo({
            url: `/pages/templatesquaredetails/templatesquaredetails?id=${id}`,
            fail: () => {
                wx.showToast({
                    title: '页面跳转失败',
                    icon: 'none',
                });
            },
        });
    },
    onPreviewText(e) {
        const title = String(e.currentTarget.dataset.title || '完整内容').trim() || '完整内容';
        const content = String(e.currentTarget.dataset.content || '').trim();
        if (!content) {
            return;
        }
        wx.showModal({
            title,
            content,
            showCancel: false,
            confirmText: '知道了',
        });
    },
    onAvatarTap() {
        if (this.data.isSelfHomepage) {
            const nextExpanded = !this.data.showProfileOverviewSections;
            this.setData({
                showProfileOverviewSections: nextExpanded,
                avatarManageHint: buildAvatarManageHint(true, nextExpanded),
            });
            return;
        }
        if (this.data.profile.avatar) {
            wx.previewImage({
                current: this.data.profile.avatar,
                urls: [this.data.profile.avatar],
            });
        }
    },
    async onToggleFollowDesigner() {
        const userId = Number(this.data.userId || 0);
        if (!userId) {
            return;
        }
        const token = this.getAuthToken();
        if (!token) {
            wx.showToast({
                title: '请先登录后再关注',
                icon: 'none',
            });
            return;
        }
        try {
            const nextFollowed = !this.data.isFollowed;
            const requestMethod = nextFollowed ? 'POST' : 'DELETE';
            const data = await new Promise((resolve, reject) => {
                wx.request({
                    url: `${API_BASE_URL}/api/v1/miniprogram/user/designers/${userId}/follow`,
                    method: requestMethod,
                    header: { token },
                    success: (res) => {
                        const body = (res.data || {});
                        if (res.statusCode === 200 && body.code === 0) {
                            resolve(body.data || {});
                            return;
                        }
                        reject(new Error(body.msg || `请求失败: ${res.statusCode}`));
                    },
                    fail: reject,
                });
            });
            this.setData({
                isFollowed: data?.followed === true,
            });
            wx.showToast({
                title: data?.followed === true ? '已关注设计师' : '已取消关注',
                icon: 'none',
            });
        }
        catch (error) {
            wx.showToast({
                title: error?.message || '操作失败',
                icon: 'none',
            });
        }
    },
    async toggleFavorite() {
        const userId = Number(this.data.userId || 0);
        if (!userId || this.data.isSelfHomepage || this.data.favoriteLoading) {
            return;
        }
        try {
            this.setData({ favoriteLoading: true });
            const data = await (0, favoriteApi_1.toggleFavorite)('designer', userId, this.data.isFavorited);
            this.setData({
                isFavorited: data.favorited === true,
                favoriteLoading: false,
            });
            wx.showToast({
                title: data.favorited === true ? '已收藏设计师' : '已取消收藏',
                icon: 'none',
            });
        }
        catch (error) {
            this.setData({ favoriteLoading: false });
            wx.showToast({
                title: error?.message || '收藏操作失败',
                icon: 'none',
            });
        }
    },
    async onConsultDesigner() {
        // 优先直接拉起企微客服，失败时再显示咨询弹窗
        const opened = await (0, enterpriseWechat_1.tryOpenCustomerServiceDirect)({
            customerServiceCorpId: this.data.enterpriseWechatCustomerServiceCorpId,
            customerServiceUrl: this.data.enterpriseWechatCustomerServiceUrl,
        });
        if (opened) {
            return;
        }
        this.setData({
            contactPopupVisible: true,
        });
        this.loadEnterpriseWechatServiceConfig();
    },
    onEditDesignerProfile() {
        wx.navigateTo({
            url: '/pages/myInformationmodification/myInformationmodification?section=designer&source=designerhome',
            fail: () => {
                wx.showToast({
                    title: '页面跳转失败',
                    icon: 'none',
                });
            },
        });
    },
    onEditDesignerService() {
        wx.navigateTo({
            url: '/pages/myInformationmodification/myInformationmodification?section=service&source=designerhome',
            fail: () => {
                wx.showToast({
                    title: '页面跳转失败',
                    icon: 'none',
                });
            },
        });
    },
    onOpenDesignerCertification() {
        wx.navigateTo({
            url: '/pages/Identityauthen/Identityauthen',
            fail: () => {
                wx.showToast({
                    title: '页面跳转失败',
                    icon: 'none',
                });
            },
        });
    },
    closeContactPopup() {
        this.setData({
            contactPopupVisible: false,
        });
    },
    previewWechatQrcode() {
        if (!this.data.supplierWechatQrAvailable || !this.data.supplierWechatQr) {
            wx.showToast({
                title: '暂未配置企业微信二维码',
                icon: 'none',
            });
            return;
        }
        wx.previewImage({
            urls: [this.data.supplierWechatQr],
            current: this.data.supplierWechatQr,
        });
    },
    saveImageToAlbum(url, loadingTitle, successTitle) {
        wx.showLoading({ title: loadingTitle });
        wx.downloadFile({
            url,
            success: (res) => {
                if (res.statusCode !== 200) {
                    wx.hideLoading();
                    wx.showToast({ title: '保存失败', icon: 'none' });
                    return;
                }
                wx.saveImageToPhotosAlbum({
                    filePath: res.tempFilePath,
                    success: () => {
                        wx.hideLoading();
                        wx.showToast({ title: successTitle, icon: 'success' });
                    },
                    fail: (err) => {
                        wx.hideLoading();
                        if (err?.errMsg && err.errMsg.includes('auth deny')) {
                            wx.showModal({
                                title: '提示',
                                content: '需要授权相册权限才能保存图片',
                                confirmText: '去设置',
                                success: (modalRes) => {
                                    if (modalRes.confirm) {
                                        wx.openSetting();
                                    }
                                }
                            });
                            return;
                        }
                        wx.showToast({ title: '保存失败', icon: 'none' });
                    }
                });
            },
            fail: () => {
                wx.hideLoading();
                wx.showToast({ title: '保存失败', icon: 'none' });
            }
        });
    },
    saveSupplierWechatQrcode() {
        if (!this.data.supplierWechatQrAvailable || !this.data.supplierWechatQr) {
            wx.showToast({
                title: '暂未配置企业微信二维码',
                icon: 'none',
            });
            return;
        }
        this.saveImageToAlbum(this.data.supplierWechatQr, '保存二维码中...', '二维码已保存');
    },
    onManualServiceContact(_e) {
        this.setData({
            contactPopupVisible: false,
        });
    },
    async loadEnterpriseWechatServiceConfig(force = false) {
        if (this.data.enterpriseWechatConfigLoading && !force) {
            return;
        }
        const token = wx.getStorageSync('token');
        if (!token) {
            // 未登录时不阻断，使用默认 corpId/url 已在 data 中初始化
            this.setData({ enterpriseWechatConfigLoading: false });
            return;
        }
        this.setData({
            enterpriseWechatConfigLoading: true,
        });
        try {
            const configData = await new Promise((resolve, reject) => {
                wx.request({
                    url: `${API_BASE_URL}/api/v1/miniprogram/user/ai/download-config`,
                    method: 'GET',
                    header: {
                        token,
                    },
                    success: (res) => {
                        if (res.statusCode === 200 && res.data) {
                            const data = res.data;
                            if (data.code === 0) {
                                resolve(data.data || {});
                                return;
                            }
                            reject(new Error(data.msg || '获取企业微信配置失败'));
                            return;
                        }
                        reject(new Error(`请求失败: ${res.statusCode}`));
                    },
                    fail: reject,
                });
            });
            const serviceConfig = (0, enterpriseWechat_1.resolveEnterpriseWechatServiceConfig)(configData, {
                qrcodeUrl: this.data.supplierWechatQr,
                tip: DEFAULT_CONSULT_WECHAT_TIP,
                contact: this.data.enterpriseWechatContact,
                servicePhone: this.data.supplierPhone,
                customerServiceCorpId: this.data.enterpriseWechatCustomerServiceCorpId,
                customerServiceUrl: this.data.enterpriseWechatCustomerServiceUrl,
            });
            this.setData({
                supplierWechatQr: serviceConfig.qrcodeUrl,
                supplierWechatQrAvailable: !!serviceConfig.qrcodeUrl,
                enterpriseWechatTip: serviceConfig.tip || DEFAULT_CONSULT_WECHAT_TIP,
                enterpriseWechatContact: serviceConfig.contact,
                supplierPhone: serviceConfig.servicePhone || enterpriseWechat_1.DEFAULT_ENTERPRISE_SERVICE_PHONE,
                enterpriseWechatCustomerServiceCorpId: serviceConfig.customerServiceCorpId,
                enterpriseWechatCustomerServiceUrl: serviceConfig.customerServiceUrl,
                enterpriseWechatConfigLoading: false,
            });
        }
        catch (error) {
            console.error('加载企业微信人工客服配置失败:', error);
            this.setData({
                enterpriseWechatConfigLoading: false,
            });
        }
    },
    getEnterpriseWechatServiceConfig() {
        return {
            qrcodeUrl: this.data.supplierWechatQr,
            tip: this.data.enterpriseWechatTip,
            contact: this.data.enterpriseWechatContact,
            servicePhone: this.data.supplierPhone,
            customerServiceCorpId: this.data.enterpriseWechatCustomerServiceCorpId,
            customerServiceUrl: this.data.enterpriseWechatCustomerServiceUrl,
        };
    },
    async onOpenEnterpriseCustomerService() {
        await this.loadEnterpriseWechatServiceConfig(true);
        const openResult = await (0, enterpriseWechat_1.openEnterpriseCustomerServiceChat)(this.getEnterpriseWechatServiceConfig());
        if (openResult.opened) {
            this.closeContactPopup();
            return;
        }
        wx.showToast({
            title: '未拉起微信客服，请使用二维码备用',
            icon: 'none',
        });
    },
    onCallSupplier() {
        if (!this.data.supplierPhone) {
            wx.showToast({ title: '暂未配置联系电话', icon: 'none' });
            return;
        }
        wx.makePhoneCall({
            phoneNumber: this.data.supplierPhone,
            fail: () => {
                wx.showToast({ title: '拨号失败，请稍后重试', icon: 'none' });
            },
        });
    },
    // 小程序原生客服按钮 bindcontact 回调
    onNativeContactSuccess() {
        this.setData({ contactPopupVisible: false });
    },
    // 智能客服：尝试拉起企微客服，失败弹二维码
    async onSmartServiceTap() {
        const config = this.getEnterpriseWechatServiceConfig();
        const result = await (0, enterpriseWechat_1.openEnterpriseCustomerServiceChat)(config);
        if (!result.opened) {
            // 企微客服拉起失败，关闭弹窗，弹出二维码预览
            this.setData({ contactPopupVisible: false });
            if (this.data.supplierWechatQrAvailable && this.data.supplierWechatQr) {
                wx.previewImage({
                    urls: [this.data.supplierWechatQr],
                    current: this.data.supplierWechatQr,
                });
            }
            else {
                wx.showToast({ title: '暂未配置企业微信二维码', icon: 'none' });
            }
        }
    },
    onTabSwitch() {
    },
    onShareAppMessage() {
        return {
            title: `${this.data.profile.name || '设计师'}的主页`,
            path: `/pages/designerhome/designerhome?userId=${this.data.userId || 0}`,
        };
    },
});
