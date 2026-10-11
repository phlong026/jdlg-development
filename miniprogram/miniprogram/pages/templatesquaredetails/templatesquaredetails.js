"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
// pages/templatesquaredetails/templatesquaredetails.ts
const parameter_1 = require("../../utils/parameter");
const deviceFingerprint_1 = require("../../utils/deviceFingerprint");
const asset_1 = require("../../utils/asset");
const perf_1 = require("../../utils/perf");
const shareImage_1 = require("../../utils/shareImage");
// API基础地址
const API_BASE_URL = 'https://api.jiadilingguang.com'; // 根据实际情况修改
const LOCAL_ENTERPRISE_WECHAT_QRCODE = (0, asset_1.resolveAssetPath)('/assets/企业微信二维码.png');
const PAGE_BACKGROUND_TOP = '#e6daca';
const PAGE_BACKGROUND_BOTTOM = '#ece4d9';
const TEMPLATE_DETAIL_CACHE_TTL = 3 * 60 * 1000;
const DEFAULT_HERO_HEIGHT_PX = 420;
function buildPublicTemplateDetailCacheKey(templateId) {
    return `template-detail:public:${Number(templateId || 0)}`;
}
function buildTokenCacheScope(token) {
    const value = String(token || '').trim();
    let hash = 0;
    for (let index = 0; index < value.length; index += 1) {
        hash = ((hash << 5) - hash + value.charCodeAt(index)) | 0;
    }
    return Math.abs(hash).toString(36) || 'anonymous';
}
function getCachedLoginUserId() {
    try {
        const userInfo = wx.getStorageSync('userInfo') || wx.getStorageSync('user') || {};
        return Number(userInfo.id || userInfo.user_id || userInfo.userId || 0) || 0;
    }
    catch (error) {
        return 0;
    }
}
function buildTemplateDetailCacheKey(templateId, token = '') {
    const safeTemplateId = Number(templateId || 0);
    const safeToken = String(token || '').trim();
    if (!safeToken) {
        return buildPublicTemplateDetailCacheKey(safeTemplateId);
    }
    const userId = getCachedLoginUserId();
    const authScope = userId > 0 ? `user:${userId}` : `token:${buildTokenCacheScope(safeToken)}`;
    return `template-detail:auth:${authScope}:${safeTemplateId}`;
}
function mapDesignerCertStatusLabel(status) {
    const value = String(status || '').trim();
    if (value === 'approved')
        return '平台认证';
    if (value === 'pending_review')
        return '审核中';
    if (value === 'pending_payment')
        return '待支付';
    if (value === 'rejected')
        return '未通过';
    return '';
}
Page({
    /**
     * 页面的初始数据
     */
    data: {
        templateId: 0,
        template: null,
        unlocked: false,
        prompt: '',
        token: '',
        loading: false,
        // 模板名称、描述、图片（与后端 detail 返回的 name/description/thumbnail/preview_url 对应）
        templateName: '',
        templateDesc: '',
        mainImage: '', // 主要内容区域的主图
        imageList: [], // 多图列表，用于滑动预览
        // UI相关数据
        userInfo: {
            name: '匿名',
            avatar: '',
            title: '设计师',
            userId: 0,
            avatarText: '设',
            certStatusLabel: '',
        },
        noteContent: '没有描述',
        postDate: '',
        commentCount: 0,
        likeCount: 0,
        shareCount: 0,
        isLiked: false,
        usageCount: 0, // 使用人数
        // 是否有原始 AI 任务（管理员创建的展示模板一般没有）
        hasOriginalTask: true,
        isExhibitionTemplate: false,
        imageHeight: 500,
        imageHeights: [],
        currentImageIndex: 0,
        shareImageUrl: '',
        shareImageSourceUrl: '',
        navTop: 0,
        navBarHeight: 72,
        heroHeight: DEFAULT_HERO_HEIGHT_PX,
        heroDefaultHeight: DEFAULT_HERO_HEIGHT_PX,
        price: 0,
        isFree: true,
        selectedAction: '',
        primaryActionText: '做同款',
        primaryActionHint: '直接按当前效果图快速生成相似风格',
        statusBadgeText: '免费模板',
        localComments: [],
        showCommentModal: false,
        commentDraft: '',
        commentsLoading: false,
        previewShowMenu: false,
        canDownloadImages: false,
        userPhoneVerified: false,
        userRechargeMember: false,
        downloadActionText: '登录后下载',
        downloadActionHint: '登录后可继续验证并下载当前模板图片',
        showEnterpriseWechatModal: false,
        enterpriseWechatQRCodeUrl: '',
        enterpriseWechatTip: '完成手机号授权验证后，可下载保存模板图片。',
        enterpriseWechatVerified: false,
        enterpriseWechatVerifiedAtText: '',
        enterpriseWechatContact: '',
        enterpriseWechatBindTicket: '',
        enterpriseWechatBindStatus: 'pending',
        enterpriseWechatConfigLoaded: false,
        enterpriseWechatConfigLoading: false,
        enterpriseWechatAutoChecking: false,
        enterpriseWechatAutoResumeInProgress: false,
        enterpriseWechatPhoneSubmitting: false,
        pendingDownloadImageIndex: -1,
    },
    enterpriseWechatAutoCheckTimer: null,
    detailLoadedToken: '',
    detailNeedsRefreshOnShow: false,
    /**
     * 生命周期函数--监听页面加载
     */
    onLoad(options) {
        const token = wx.getStorageSync('token') || '';
        this.setData({ token, previewShowMenu: false });
        this.syncWindowBackground();
        this.initLayoutMetrics();
        this.initUIData();
        const id = options.id ? parseInt(options.id) : 0;
        if (id > 0) {
            this.setData({ templateId: id });
            const hasCachedDetail = Boolean((0, perf_1.getPageCache)(buildTemplateDetailCacheKey(id, token)));
            void this.loadTemplateDetail(hasCachedDetail);
        }
    },
    /**
     * 生命周期函数--监听页面显示
     */
    onShow() {
        this.syncWindowBackground();
        const token = wx.getStorageSync('token') || '';
        const tokenChanged = token !== this.detailLoadedToken;
        if (token !== this.data.token) {
            this.setData({ token });
        }
        if (this.data.showEnterpriseWechatModal && Number(this.data.pendingDownloadImageIndex) >= 0) {
            this.startEnterpriseWechatAutoCheck();
        }
        if (this.data.templateId > 0 && (this.detailNeedsRefreshOnShow || tokenChanged || !this.data.template)) {
            this.detailNeedsRefreshOnShow = false;
            void this.loadTemplateDetail(!!this.data.template);
        }
    },
    /**
     * 生命周期函数--监听页面隐藏
     */
    onHide() {
        this.stopEnterpriseWechatAutoCheck();
    },
    onUnload() {
        this.stopEnterpriseWechatAutoCheck();
    },
    async loadCreatorCertificationStatus(userId) {
        const safeUserId = Number(userId || 0);
        if (!safeUserId) {
            return;
        }
        try {
            const data = await new Promise((resolve, reject) => {
                wx.request({
                    url: `${API_BASE_URL}/api/v1/miniprogram/designers/${safeUserId}/homepage`,
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
            const certStatusLabel = mapDesignerCertStatusLabel(String(data?.profile?.cert_status || ''));
            this.setData({
                userInfo: {
                    ...(this.data.userInfo || {}),
                    certStatusLabel,
                },
            });
        }
        catch (error) {
            this.setData({
                userInfo: {
                    ...(this.data.userInfo || {}),
                    certStatusLabel: '',
                },
            });
        }
    },
    /**
     * 页面相关事件处理函数--监听用户下拉动作
     */
    onPullDownRefresh() {
        if (this.data.templateId > 0) {
            this.loadTemplateDetail().finally(() => wx.stopPullDownRefresh());
            return;
        }
        wx.stopPullDownRefresh();
    },
    /**
     * 页面上拉触底事件的处理函数
     */
    onReachBottom() {
    },
    initUIData() {
        this.setData({
            enterpriseWechatQRCodeUrl: this.resolveEnterpriseWechatQRCodeUrl(),
            enterpriseWechatVerifiedAtText: '',
            enterpriseWechatContact: '',
            enterpriseWechatBindTicket: '',
            enterpriseWechatBindStatus: 'pending',
            enterpriseWechatConfigLoaded: false,
            enterpriseWechatConfigLoading: false,
            enterpriseWechatAutoChecking: false,
            enterpriseWechatPhoneSubmitting: false,
            showEnterpriseWechatModal: false,
            pendingDownloadImageIndex: -1,
        });
        this.updatePrimaryActionState();
    },
    /**
     * 生命周期函数--监听页面显示
     */
    onShareAppMessage() {
        const shareSourceUrl = this.getCurrentShareSourceUrl();
        return {
            title: this.data.templateName || '甲第灵光 · 模板详情',
            path: `/pages/templatesquaredetails/templatesquaredetails?id=${this.data.templateId || 0}`,
            imageUrl: this.data.shareImageUrl || shareSourceUrl,
            success: () => {
                this.handleShareSuccess();
            },
        };
    },
    onShareTimeline() {
        const shareSourceUrl = this.getCurrentShareSourceUrl();
        return {
            title: this.data.templateName || '甲第灵光 · 模板详情',
            query: `id=${this.data.templateId || 0}`,
            imageUrl: this.data.shareImageUrl || shareSourceUrl,
            success: () => {
                this.handleShareSuccess();
            },
        };
    },
    noop() {
    },
    /**
     * 点击评论
     */
    onComment() {
        const token = wx.getStorageSync('token');
        if (!token) {
            wx.showToast({
                title: '请先登录后评论',
                icon: 'none',
            });
            return;
        }
        this.setData({
            showCommentModal: true,
            commentDraft: '',
        });
    },
    closeCommentModal() {
        this.setData({
            showCommentModal: false,
            commentDraft: '',
        });
    },
    onCommentInput(e) {
        this.setData({
            commentDraft: String(e.detail.value || ''),
        });
    },
    async submitComment() {
        const content = String(this.data.commentDraft || '').trim();
        if (!content) {
            wx.showToast({
                title: '请输入评论内容',
                icon: 'none',
            });
            return;
        }
        const token = wx.getStorageSync('token');
        if (!token) {
            wx.showToast({
                title: '请先登录后评论',
                icon: 'none',
            });
            return;
        }
        try {
            const deviceID = (0, deviceFingerprint_1.getCachedDeviceFingerprint)() || '';
            const apiPath = `/api/v1/miniprogram/templates/${this.data.templateId}/comments`;
            const requestBody = { content };
            const params = (0, parameter_1.generateRequestParams)(token, requestBody, apiPath, deviceID);
            const headers = {
                ...(0, parameter_1.paramsToHeaders)(params),
                'Content-Type': 'application/json',
            };
            const response = await new Promise((resolve, reject) => {
                wx.request({
                    url: `${API_BASE_URL}${apiPath}`,
                    method: 'POST',
                    header: headers,
                    data: requestBody,
                    success: (res) => {
                        if (res.statusCode === 200 && res.data) {
                            const data = res.data;
                            if (data.code === 0) {
                                resolve(data.data || {});
                                return;
                            }
                            reject(new Error(data.msg || '评论发布失败'));
                            return;
                        }
                        reject(new Error(`请求失败: ${res.statusCode}`));
                    },
                    fail: reject,
                });
            });
            const nextComment = this.normalizeCommentItem(response);
            const nextComments = [nextComment, ...(this.data.localComments || [])];
            this.setData({
                localComments: nextComments,
                commentCount: Number(response.comment_count || nextComments.length),
                showCommentModal: false,
                commentDraft: '',
            });
            wx.showToast({
                title: '评论已发布',
                icon: 'success',
            });
        }
        catch (error) {
            wx.showToast({
                title: error?.message || '评论发布失败',
                icon: 'none',
            });
        }
    },
    async onMakeSame() {
        this.setData({ selectedAction: 'make_same' });
        await this.startUseTemplateFlow('make_same');
    },
    clearActionSelection() {
        this.setData({ selectedAction: '' });
    },
    getSimpleToken() {
        const token = String(wx.getStorageSync('token') || '').trim();
        if (!token) {
            throw new Error('请先登录');
        }
        return token;
    },
    async readDownloadErrorMessage(tempFilePath, fallback) {
        if (!tempFilePath) {
            return fallback;
        }
        try {
            const fileContent = await new Promise((resolve, reject) => {
                wx.getFileSystemManager().readFile({
                    filePath: tempFilePath,
                    encoding: 'utf8',
                    success: (res) => resolve(String(res.data || '')),
                    fail: reject,
                });
            });
            const parsed = JSON.parse(fileContent || '{}');
            const message = String(parsed?.msg || parsed?.message || '').trim();
            return message || fallback;
        }
        catch (error) {
            return fallback;
        }
    },
    saveImageToAlbum(url, loadingTitle, successTitle, downloadHeader = {}) {
        if (!url) {
            wx.showToast({ title: '暂无可保存图片', icon: 'none' });
            return;
        }
        wx.showLoading({ title: loadingTitle });
        const saveResolvedFile = (filePath) => {
            wx.saveImageToPhotosAlbum({
                filePath,
                success: () => {
                    wx.hideLoading();
                    wx.showToast({ title: successTitle, icon: 'success' });
                },
                fail: (err) => {
                    wx.hideLoading();
                    if (String(err?.errMsg || '').includes('auth deny')) {
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
        };
        if (!/^https?:\/\//i.test(url)) {
            wx.getImageInfo({
                src: url,
                success: (res) => saveResolvedFile(res.path),
                fail: () => {
                    wx.hideLoading();
                    wx.showToast({ title: '读取图片失败', icon: 'none' });
                }
            });
            return;
        }
        wx.downloadFile({
            url,
            header: downloadHeader,
            success: (res) => {
                if (res.statusCode === 200) {
                    saveResolvedFile(res.tempFilePath);
                    return;
                }
                this.readDownloadErrorMessage(res.tempFilePath, '下载失败').then((message) => {
                    wx.hideLoading();
                    wx.showToast({ title: message, icon: 'none' });
                });
            },
            fail: (err) => {
                wx.hideLoading();
                wx.showToast({ title: String(err?.errMsg || '下载失败').includes('domain list') ? '下载域名未配置' : '下载失败', icon: 'none' });
            },
        });
    },
    hasEnterpriseWechatDownloadAccess() {
        return !!this.data.enterpriseWechatVerified && !!String(this.data.enterpriseWechatContact || '').trim();
    },
    queuePendingTemplateDownload(imageIndex) {
        this.setData({
            pendingDownloadImageIndex: typeof imageIndex === 'number' ? Math.max(0, Number(imageIndex)) : Number(this.data.currentImageIndex || 0),
        });
    },
    clearPendingTemplateDownload() {
        this.stopEnterpriseWechatAutoCheck();
        this.setData({
            pendingDownloadImageIndex: -1,
            enterpriseWechatAutoChecking: false,
        });
    },
    startEnterpriseWechatAutoCheck() {
        if (!this.data.showEnterpriseWechatModal || Number(this.data.pendingDownloadImageIndex) < 0) {
            return;
        }
        if (this.enterpriseWechatAutoCheckTimer) {
            if (!this.data.enterpriseWechatAutoChecking) {
                this.setData({ enterpriseWechatAutoChecking: true });
            }
            return;
        }
        const tick = async () => {
            await this.loadEnterpriseWechatConfig(true);
            if (this.hasEnterpriseWechatDownloadAccess()) {
                this.stopEnterpriseWechatAutoCheck();
                await this.resumePendingTemplateDownload();
            }
        };
        this.setData({ enterpriseWechatAutoChecking: true });
        void tick();
        this.enterpriseWechatAutoCheckTimer = setInterval(() => {
            void tick();
        }, 2500);
    },
    stopEnterpriseWechatAutoCheck() {
        if (this.enterpriseWechatAutoCheckTimer) {
            clearInterval(this.enterpriseWechatAutoCheckTimer);
            this.enterpriseWechatAutoCheckTimer = null;
        }
        if (this.data.enterpriseWechatAutoChecking) {
            this.setData({ enterpriseWechatAutoChecking: false });
        }
    },
    resolveEnterpriseWechatQRCodeUrl(url) {
        const qrCodeUrl = String(url || '').trim();
        return qrCodeUrl || LOCAL_ENTERPRISE_WECHAT_QRCODE;
    },
    applyEnterpriseWechatConfig(configData = {}) {
        const verifiedAt = String(configData.enterprise_wechat_verified_at || '').trim();
        const qrcodeUrl = String(configData.enterprise_wechat_qrcode_url || '').trim() || String(this.data.enterpriseWechatQRCodeUrl || '').trim();
        const tipText = String(configData.enterprise_wechat_tip || '').trim() || this.data.enterpriseWechatTip;
        const contact = String(configData.enterprise_wechat_contact || '').trim() || String(this.data.enterpriseWechatContact || '').trim();
        const bindTicket = String(configData.enterprise_wechat_bind_ticket || '').trim() || String(this.data.enterpriseWechatBindTicket || '').trim();
        const bindStatus = String(configData.enterprise_wechat_bind_status || '').trim() || String(this.data.enterpriseWechatBindStatus || '').trim() || 'pending';
        const verified = !!configData.enterprise_wechat_verified && !!contact;
        this.setData({
            enterpriseWechatQRCodeUrl: this.resolveEnterpriseWechatQRCodeUrl(qrcodeUrl),
            enterpriseWechatTip: tipText,
            enterpriseWechatVerified: verified,
            enterpriseWechatVerifiedAtText: verifiedAt,
            enterpriseWechatContact: contact,
            enterpriseWechatBindTicket: bindTicket,
            enterpriseWechatBindStatus: bindStatus,
            userPhoneVerified: verified,
        });
    },
    async loadEnterpriseWechatConfig(force = false) {
        if ((this.data.enterpriseWechatConfigLoaded && !force) || this.data.enterpriseWechatConfigLoading) {
            return;
        }
        let token = '';
        try {
            token = this.getSimpleToken();
        }
        catch (error) {
            wx.showToast({ title: error.message || '请先登录', icon: 'none' });
            return;
        }
        this.setData({ enterpriseWechatConfigLoading: true });
        try {
            const res = await new Promise((resolve, reject) => {
                wx.request({
                    url: `${API_BASE_URL}/api/v1/miniprogram/user/ai/download-config`,
                    method: 'GET',
                    header: { token },
                    success: (requestRes) => {
                        if (requestRes.statusCode === 200 && requestRes.data) {
                            const data = requestRes.data;
                            if (data.code === 0) {
                                resolve(data.data || {});
                                return;
                            }
                            reject(new Error(data.msg || '获取下载配置失败'));
                            return;
                        }
                        reject(new Error(`请求失败: ${requestRes.statusCode}`));
                    },
                    fail: reject,
                });
            });
            this.applyEnterpriseWechatConfig(res);
            this.setData({
                enterpriseWechatConfigLoaded: true,
                enterpriseWechatConfigLoading: false,
            });
        }
        catch (error) {
            this.setData({
                enterpriseWechatConfigLoaded: true,
                enterpriseWechatConfigLoading: false,
                enterpriseWechatQRCodeUrl: this.resolveEnterpriseWechatQRCodeUrl(),
                enterpriseWechatVerified: false,
                enterpriseWechatVerifiedAtText: '',
                enterpriseWechatContact: '',
                enterpriseWechatBindTicket: '',
                enterpriseWechatBindStatus: 'pending',
                enterpriseWechatTip: '添加企业微信并留下电话号码后即可下载保存，二维码已为你预置。',
            });
        }
    },
    async openEnterpriseWechatModal() {
        this.setData({ showEnterpriseWechatModal: true });
        await this.loadEnterpriseWechatConfig();
        if (!this.hasEnterpriseWechatDownloadAccess()) {
            this.startEnterpriseWechatAutoCheck();
        }
    },
    closeEnterpriseWechatModal() {
        this.stopEnterpriseWechatAutoCheck();
        this.setData({ showEnterpriseWechatModal: false });
    },
    onEnterpriseWechatContentTap() {
    },
    previewEnterpriseWechatQRCode() {
        const url = String(this.data.enterpriseWechatQRCodeUrl || '').trim();
        if (!url) {
            wx.showToast({ title: '暂未配置二维码', icon: 'none' });
            return;
        }
        wx.previewImage({
            current: url,
            urls: [url],
            showmenu: true,
        });
    },
    async refreshEnterpriseWechatVerification() {
        await this.loadEnterpriseWechatConfig(true);
        if (this.hasEnterpriseWechatDownloadAccess()) {
            wx.showToast({ title: '验证已完成，正在继续下载', icon: 'success' });
            await this.resumePendingTemplateDownload();
            return;
        }
        wx.showToast({ title: '暂未检测到验证完成', icon: 'none' });
        this.startEnterpriseWechatAutoCheck();
    },
    async continuePendingTemplateDownload() {
        if (Number(this.data.pendingDownloadImageIndex) < 0) {
            return;
        }
        await this.loadEnterpriseWechatConfig(true);
        if (!this.hasEnterpriseWechatDownloadAccess()) {
            this.setData({ showEnterpriseWechatModal: true });
            this.startEnterpriseWechatAutoCheck();
            return;
        }
        await this.resumePendingTemplateDownload();
    },
    async requestTemplateDownloadPayload() {
        const token = this.getSimpleToken();
        const deviceID = (0, deviceFingerprint_1.getCachedDeviceFingerprint)() || '';
        const apiPath = `/api/v1/miniprogram/templates/${this.data.templateId}/download`;
        const requestBody = {};
        const params = (0, parameter_1.generateRequestParams)(token, requestBody, apiPath, deviceID);
        const headers = {
            ...(0, parameter_1.paramsToHeaders)(params),
            'Content-Type': 'application/json',
        };
        const result = await new Promise((resolve, reject) => {
            wx.request({
                url: `${API_BASE_URL}${apiPath}`,
                method: 'POST',
                header: headers,
                data: requestBody,
                success: (requestRes) => {
                    if (requestRes.statusCode === 200 && requestRes.data) {
                        const data = requestRes.data;
                        if (data.code === 0) {
                            resolve(data.data || {});
                            return;
                        }
                        reject(new Error(data.msg || '获取下载地址失败'));
                        return;
                    }
                    reject(new Error(`请求失败: ${requestRes.statusCode}`));
                },
                fail: reject,
            });
        });
        return {
            imageUrls: Array.isArray(result.image_urls) ? result.image_urls.map((item) => String(item || '').trim()).filter(Boolean) : [],
            downloadUrls: Array.isArray(result.download_urls) ? result.download_urls.map((item) => String(item || '').trim()).filter(Boolean) : [],
        };
    },
    buildTemplateDownloadProxyRequest(imageIndex, token) {
        const safeIndex = Math.max(0, Number(imageIndex || 0));
        const apiPath = `/api/v1/miniprogram/templates/${this.data.templateId}/download-file?image_index=${safeIndex}`;
        const params = (0, parameter_1.generateRequestParams)(token, {}, apiPath, (0, deviceFingerprint_1.getCachedDeviceFingerprint)() || '');
        return {
            url: `${API_BASE_URL}${apiPath}`,
            headers: (0, parameter_1.paramsToHeaders)(params),
        };
    },
    async performAuthorizedTemplateDownload(imageIndex = 0) {
        try {
            this.setData({ showEnterpriseWechatModal: false });
            wx.showLoading({ title: '校验下载权限中...' });
            const token = this.getSimpleToken();
            const { imageUrls, downloadUrls } = await this.requestTemplateDownloadPayload();
            const currentImages = Array.isArray(this.data.imageList) ? this.data.imageList : [];
            const safeIndex = Math.max(0, Number(imageIndex || 0));
            const currentUrl = String(currentImages[safeIndex] || this.data.mainImage || '').trim();
            const matchedRawIndex = imageUrls.findIndex((item) => item === currentUrl);
            const targetIndex = matchedRawIndex >= 0 ? matchedRawIndex : safeIndex;
            const proxyRequest = this.buildTemplateDownloadProxyRequest(targetIndex, token);
            const proxyUrl = downloadUrls[targetIndex] || downloadUrls[0] || proxyRequest.url;
            const targetUrl = proxyUrl.startsWith('http://') || proxyUrl.startsWith('https://') ? proxyUrl : `${API_BASE_URL}${proxyUrl}`;
            wx.hideLoading();
            if (!targetUrl) {
                throw new Error('未获取到可下载图片');
            }
            this.clearPendingTemplateDownload();
            this.setData({
                canDownloadImages: true,
                userRechargeMember: true,
                userPhoneVerified: true,
                usageCount: Number(this.data.usageCount || 0) + 1,
                downloadActionText: '下载当前图片',
                downloadActionHint: '已开通下载资格，可保存当前模板图片到相册',
            });
            this.saveImageToAlbum(targetUrl, '保存图片中...', '图片已保存到相册', proxyRequest.headers);
        }
        catch (error) {
            wx.hideLoading();
            const message = String(error?.message || '图片下载失败');
            if (message.includes('手机号') || message.includes('企业微信') || message.includes('联系方式')) {
                this.setData({ showEnterpriseWechatModal: true });
                this.startEnterpriseWechatAutoCheck();
                return;
            }
            if (message.includes('充值')) {
                this.clearPendingTemplateDownload();
                this.promptRechargeAndGo();
                return;
            }
            this.clearPendingTemplateDownload();
            wx.showToast({ title: message, icon: 'none' });
        }
    },
    promptRechargeAndGo() {
        wx.showModal({
            title: '充值后下载',
            content: '当前模板图片下载仅对已完成标准充值的用户开放。现在标准充值成功后默认即可下载；如果后续某个金额配置了具体会员时长，则按对应档位规则生效。',
            confirmText: '去充值',
            success: (res) => {
                if (res.confirm) {
                    this.detailNeedsRefreshOnShow = true;
                    wx.navigateTo({ url: '/pages/topupcenter/topupcenter' });
                }
            }
        });
    },
    onDownloadTemplateImage() {
        this.setData({ selectedAction: '' });
        const targetIndex = Number(this.data.currentImageIndex || 0);
        if (!(this.data.imageList || []).length && !this.data.mainImage) {
            wx.showToast({ title: '暂无可下载图片', icon: 'none' });
            return;
        }
        const token = String(wx.getStorageSync('token') || '').trim();
        if (!token) {
            wx.showModal({
                title: '登录后下载',
                content: '模板图片下载属于专属权益，请先登录后继续操作。',
                confirmText: '去登录',
                success: (res) => {
                    if (res.confirm) {
                        this.detailNeedsRefreshOnShow = true;
                        wx.navigateTo({ url: '/pages/login/login' });
                    }
                }
            });
            return;
        }
        this.queuePendingTemplateDownload(targetIndex);
        this.loadEnterpriseWechatConfig(true).then(async () => {
            if (!this.hasEnterpriseWechatDownloadAccess()) {
                this.setData({ showEnterpriseWechatModal: true });
                this.startEnterpriseWechatAutoCheck();
                return;
            }
            if (!this.data.userRechargeMember) {
                this.promptRechargeAndGo();
                return;
            }
            await this.performAuthorizedTemplateDownload(targetIndex);
        }).catch(() => {
            wx.showToast({ title: '获取下载资格失败', icon: 'none' });
        });
    },
    saveEnterpriseWechatQRCode() {
        const url = String(this.data.enterpriseWechatQRCodeUrl || '').trim();
        if (!url) {
            wx.showToast({ title: '暂未配置二维码', icon: 'none' });
            return;
        }
        this.saveImageToAlbum(url, '保存二维码中...', '二维码已保存');
    },
    async loadTemplateDetail(silent = false) {
        if (this.data.loading && !silent)
            return;
        if (!silent) {
            this.setData({ loading: true });
        }
        const token = this.data.token;
        const url = token
            ? `${API_BASE_URL}/api/v1/miniprogram/templates/${this.data.templateId}/detail`
            : `${API_BASE_URL}/api/v1/miniprogram/templates/${this.data.templateId}`;
        try {
            const cacheKey = buildTemplateDetailCacheKey(this.data.templateId, token);
            const cachedDetail = (0, perf_1.getPageCache)(cacheKey);
            const res = cachedDetail || await new Promise((resolve, reject) => {
                const deviceID = (0, deviceFingerprint_1.getCachedDeviceFingerprint)() || '';
                const body = {};
                const apiPath = token
                    ? `/api/v1/miniprogram/templates/${this.data.templateId}/detail`
                    : `/api/v1/miniprogram/templates/${this.data.templateId}`;
                const headers = token
                    ? { ...(0, parameter_1.paramsToHeaders)((0, parameter_1.generateRequestParams)(token, body, apiPath, deviceID)), 'Content-Type': 'application/json' }
                    : {};
                wx.request({
                    url,
                    method: 'GET',
                    header: headers,
                    data: {},
                    success: (requestRes) => {
                        if (requestRes.statusCode === 200 && requestRes.data) {
                            const data = requestRes.data;
                            if (data.code === 0) {
                                resolve(data.data);
                                return;
                            }
                            reject(new Error(data.msg || '获取模板详情失败'));
                            return;
                        }
                        reject(new Error(`请求失败: ${requestRes.statusCode}`));
                    },
                    fail: reject,
                });
            });
            (0, perf_1.setPageCache)(cacheKey, res, TEMPLATE_DETAIL_CACHE_TTL);
            let imageList = [];
            if (res.images) {
                try {
                    const images = typeof res.images === 'string' ? JSON.parse(res.images) : res.images;
                    if (Array.isArray(images)) {
                        images.forEach((img) => {
                            if (typeof img === 'string' && img) {
                                imageList.push(img);
                                return;
                            }
                            if (img && typeof img === 'object') {
                                const imageUrl = (0, asset_1.normalizeCosUrl)(img.image || img.url || img.preview_url || '');
                                if (typeof imageUrl === 'string' && imageUrl) {
                                    imageList.push(imageUrl);
                                }
                            }
                        });
                    }
                }
                catch (error) {
                    console.error('解析图片列表失败:', error);
                }
            }
            let mainImage = (0, asset_1.normalizeCosUrl)(res.preview_url || res.thumbnail || '');
            if (!mainImage && imageList.length > 0) {
                mainImage = imageList[0];
            }
            if (mainImage) {
                if (!imageList.includes(mainImage)) {
                    imageList.unshift(mainImage);
                }
                else {
                    imageList = [mainImage, ...imageList.filter((item) => item !== mainImage)];
                }
            }
            let userInfo = this.data.userInfo;
            if (res.creator_info) {
                userInfo = {
                    name: res.creator_info.name || '匿名',
                    avatar: res.creator_info.avatar || '',
                    title: res.creator_info.title || '设计师',
                    userId: Number(res.creator_info.user_id) || Number(res.creator_user_id) || 0,
                    avatarText: res.creator_info.name ? String(res.creator_info.name).slice(0, 1) : '设',
                    certStatusLabel: '',
                };
            }
            const hasOriginalTask = typeof res.has_original_task === 'boolean' ? res.has_original_task : false;
            const isAdminTemplate = res.creator === 'admin';
            this.setData({
                template: res,
                unlocked: !!res.unlocked,
                prompt: res.prompt || '',
                loading: false,
                templateName: res.name || '',
                templateDesc: res.description || '',
                noteContent: res.description || this.data.noteContent,
                mainImage: mainImage || '',
                imageList,
                currentImageIndex: 0,
                likeCount: Number(res.like_count || 0),
                commentCount: Number(res.comment_count || 0),
                shareCount: Number(res.share_count || 0),
                userInfo,
                usageCount: Number(res.download_count || res.usage_count || 0),
                hasOriginalTask,
                isExhibitionTemplate: !hasOriginalTask || isAdminTemplate,
                postDate: this.formatDateText(res.created_at),
                price: Number(res.price || 0),
                isFree: !!res.is_free,
                canDownloadImages: !!res.can_download_images,
                userPhoneVerified: !!res.user_phone_verified,
                userRechargeMember: !!res.user_recharge_member,
                downloadActionText: String(res.download_action_text || '登录后下载'),
                downloadActionHint: String(res.download_action_hint || '登录后可继续验证并下载当前模板图片'),
                enterpriseWechatVerified: !!res.user_phone_verified,
            });
            this.prepareHeroImageHeights(imageList, Number(res.image_width || 0), Number(res.image_height || 0));
            this.detailLoadedToken = String(token || '').trim();
            void (0, perf_1.prefetchImages)([mainImage, ...imageList], 2);
            void this.prepareCurrentShareImage(mainImage || imageList[0] || '');
            if (Number(userInfo.userId) > 0) {
                void this.loadCreatorCertificationStatus(Number(userInfo.userId));
            }
            this.updatePrimaryActionState();
            void Promise.allSettled([this.loadLikedState(), this.loadComments()]);
        }
        catch (error) {
            console.error('加载模板详情失败:', error);
            if (!silent) {
                wx.showToast({ title: error.message || '加载失败', icon: 'none' });
            }
            this.setData({ loading: false });
        }
    },
    /**
     * 收藏模板
     */
    async onLike() {
        const token = wx.getStorageSync('token');
        if (!token) {
            wx.showToast({
                title: '请先登录',
                icon: 'none'
            });
            return;
        }
        if (!this.data.templateId) {
            return;
        }
        try {
            const deviceID = (0, deviceFingerprint_1.getCachedDeviceFingerprint)() || '';
            const apiPath = `/api/v1/miniprogram/templates/${this.data.templateId}/like`;
            const requestBody = {};
            const params = (0, parameter_1.generateRequestParams)(token, requestBody, apiPath, deviceID);
            const headers = {
                ...(0, parameter_1.paramsToHeaders)(params),
                'Content-Type': 'application/json',
            };
            const res = await new Promise((resolve, reject) => {
                wx.request({
                    url: `${API_BASE_URL}${apiPath}`,
                    method: 'POST',
                    header: headers,
                    data: requestBody,
                    success: (res) => {
                        if (res.statusCode === 200 && res.data) {
                            const data = res.data;
                            if (data.code === 0) {
                                resolve(data.data);
                            }
                            else {
                                reject(new Error(data.msg || '收藏失败'));
                            }
                        }
                        else {
                            reject(new Error(`请求失败: ${res.statusCode}`));
                        }
                    },
                    fail: (err) => {
                        reject(err);
                    }
                });
            });
            const nextLiked = !!res.liked;
            const nextLikeCount = typeof res.like_count === 'number'
                ? res.like_count
                : Math.max(0, this.data.likeCount + (nextLiked ? 1 : -1));
            this.setData({
                'template.like_count': nextLikeCount,
                likeCount: nextLikeCount,
                isLiked: nextLiked,
            });
            wx.showToast({
                title: nextLiked ? '已收藏' : '已取消收藏',
                icon: 'none'
            });
        }
        catch (error) {
            console.error('收藏失败:', error);
            wx.showToast({
                title: error.message || '收藏失败',
                icon: 'none'
            });
        }
    },
    onDesignerTap() {
        const userId = Number(this.data.userInfo.userId || 0);
        if (!userId) {
            wx.showToast({
                title: '暂未获取到设计师信息',
                icon: 'none',
            });
            return;
        }
        wx.navigateTo({
            url: `/pages/designerhome/designerhome?userId=${userId}`,
            fail: () => {
                wx.showToast({
                    title: '页面跳转失败',
                    icon: 'none',
                });
            },
        });
    },
    /**
     * 预览当前图片（全屏查看）
     */
    onPreviewImage(e) {
        const index = e.currentTarget.dataset.index || 0;
        const urls = this.data.imageList || [];
        if (!urls.length)
            return;
        wx.previewImage({
            current: urls[index] || urls[0],
            urls,
            showmenu: this.data.previewShowMenu,
        });
    },
    initLayoutMetrics() {
        try {
            const menuRect = wx.getMenuButtonBoundingClientRect();
            const systemInfo = wx.getSystemInfoSync();
            const safeTop = Number(systemInfo.statusBarHeight || systemInfo.safeArea?.top || 20);
            const heroHeight = Math.round((systemInfo.windowWidth || 375) * 0.72);
            this.setData({
                navTop: Math.max(safeTop + 4, Number(menuRect?.top || safeTop + 8) - 2),
                navBarHeight: Math.max(64, Number(menuRect?.height || 32) + 18),
                heroHeight,
                heroDefaultHeight: heroHeight,
            });
        }
        catch (error) {
            this.setData({
                navTop: 26,
                navBarHeight: 68,
                heroHeight: DEFAULT_HERO_HEIGHT_PX,
                heroDefaultHeight: DEFAULT_HERO_HEIGHT_PX,
            });
        }
    },
    refreshHeroHeightByIndex(index) {
        void index;
        const nextHeroHeight = Number(this.data.heroDefaultHeight || DEFAULT_HERO_HEIGHT_PX);
        this.setData({
            heroHeight: nextHeroHeight,
        });
    },
    prepareHeroImageHeights(urls, firstWidth, firstHeight) {
        const nextUrls = Array.isArray(urls) ? urls.filter((item) => String(item || '').trim()) : [];
        if (!nextUrls.length) {
            this.setData({
                imageHeights: [],
                heroHeight: Number(this.data.heroDefaultHeight || DEFAULT_HERO_HEIGHT_PX),
            });
            return;
        }
        const defaultHeight = Number(this.data.heroDefaultHeight || DEFAULT_HERO_HEIGHT_PX);
        const nextHeights = nextUrls.map(() => defaultHeight);
        void firstWidth;
        void firstHeight;
        this.setData({
            imageHeights: nextHeights,
            heroHeight: defaultHeight,
        });
    },
    syncWindowBackground() {
        if (typeof wx.setBackgroundColor !== 'function') {
            return;
        }
        wx.setBackgroundColor({
            backgroundColor: PAGE_BACKGROUND_BOTTOM,
            backgroundColorTop: PAGE_BACKGROUND_TOP,
            backgroundColorBottom: PAGE_BACKGROUND_BOTTOM,
        });
    },
    buildDisplayPrompt() {
        return String(this.data.prompt || this.data.templateDesc || this.data.noteContent || '').trim();
    },
    buildDisplayReferenceImages() {
        return [this.data.mainImage, ...(Array.isArray(this.data.imageList) ? this.data.imageList : [])]
            .map((item) => String(item || '').trim())
            .filter((item, index, array) => item && array.indexOf(item) === index)
            .slice(0, 6);
    },
    async navigateToDisplayGenerate(source) {
        const referenceImages = this.buildDisplayReferenceImages();
        const referenceImage = referenceImages[0] || '';
        const prefillData = {
            reference_image_url: referenceImage,
            reference_image_urls: referenceImages,
            original_image_urls: [],
            ordered_image_urls: referenceImages,
        };
        const query = [
            `source=${encodeURIComponent(source || 'make_same')}`,
        ];
        if (this.data.hasOriginalTask) {
            query.push(`templateId=${this.data.templateId || 0}`);
        }
        if (referenceImage) {
            query.push(`reference_image_url=${encodeURIComponent(referenceImage)}`);
        }
        wx.navigateTo({
            url: `/pages/aigenerate/aigenerate?${query.join('&')}`,
            success: (navRes) => {
                navRes.eventChannel.emit('prefillGenerateData', prefillData);
            },
            fail: () => {
                wx.showToast({
                    title: '页面跳转失败',
                    icon: 'none',
                });
            },
        });
    },
    async startUseTemplateFlow(source) {
        if (!this.data.isExhibitionTemplate) {
            const ready = await this.ensureTemplateUsable();
            if (!ready) {
                return;
            }
        }
        await this.recordTemplateUse();
        await this.navigateToDisplayGenerate(source || 'make_same');
        return;
        /*
        wx.navigateTo({
          url: `/pages/aigenerate/aigenerate?templateId=${this.data.templateId}&source=${source}${referenceImage ? `&reference_image_url=${encodeURIComponent(referenceImage)}` : ''}`,
          success: (navRes) => {
            navRes.eventChannel.emit('prefillGenerateData', prefillData);
          },
          fail: () => {
            wx.showToast({
              title: '页面跳转失败',
              icon: 'none',
            });
          },
        });
        */
    },
    async ensureTemplateUsable() {
        if (this.data.isFree || this.data.unlocked) {
            return true;
        }
        const token = wx.getStorageSync('token');
        if (!token) {
            wx.showToast({
                title: '请先登录后解锁模板',
                icon: 'none',
            });
            return false;
        }
        const confirmed = await new Promise((resolve) => {
            wx.showModal({
                title: '解锁模板',
                content: `该模板需要 ${this.data.price || 0} 灵石，确认解锁后立即使用吗？`,
                confirmText: '确认解锁',
                success: (res) => resolve(!!res.confirm),
                fail: () => resolve(false),
            });
        });
        if (!confirmed) {
            return false;
        }
        try {
            wx.showLoading({ title: '解锁中...' });
            const deviceID = (0, deviceFingerprint_1.getCachedDeviceFingerprint)() || '';
            const apiPath = `/api/v1/miniprogram/templates/${this.data.templateId}/unlock`;
            const requestBody = {};
            const params = (0, parameter_1.generateRequestParams)(token, requestBody, apiPath, deviceID);
            const headers = {
                ...(0, parameter_1.paramsToHeaders)(params),
                'Content-Type': 'application/json',
            };
            await new Promise((resolve, reject) => {
                wx.request({
                    url: `${API_BASE_URL}${apiPath}`,
                    method: 'POST',
                    header: headers,
                    data: requestBody,
                    success: (res) => {
                        if (res.statusCode === 200 && res.data) {
                            const responseData = res.data;
                            if (responseData.code === 0) {
                                resolve(responseData.data || {});
                                return;
                            }
                            reject(new Error(responseData.msg || '解锁失败'));
                            return;
                        }
                        reject(new Error(`请求失败: ${res.statusCode}`));
                    },
                    fail: reject,
                });
            });
            wx.hideLoading();
            this.setData({ unlocked: true });
            this.updatePrimaryActionState();
            wx.showToast({
                title: '解锁成功',
                icon: 'success',
            });
            return true;
        }
        catch (error) {
            wx.hideLoading();
            wx.showToast({
                title: error?.message || '解锁失败',
                icon: 'none',
            });
            return false;
        }
    },
    async recordTemplateUse() {
        if (!this.data.templateId) {
            return;
        }
        try {
            await new Promise((resolve, reject) => {
                wx.request({
                    url: `${API_BASE_URL}/api/v1/miniprogram/templates/${this.data.templateId}/use`,
                    method: 'POST',
                    success: (res) => {
                        if (res.statusCode === 200 && res.data) {
                            const responseData = res.data;
                            if (responseData.code === 0) {
                                resolve(responseData.data || {});
                                return;
                            }
                            reject(new Error(responseData.msg || '更新使用次数失败'));
                            return;
                        }
                        reject(new Error(`请求失败: ${res.statusCode}`));
                    },
                    fail: reject,
                });
            });
            this.setData({
                usageCount: Number(this.data.usageCount || 0) + 1,
            });
        }
        catch (error) {
            console.error('更新使用次数失败:', error);
        }
    },
    onSwiperChange(e) {
        const current = e.detail?.current ?? 0;
        this.setData({ currentImageIndex: current });
        this.refreshHeroHeightByIndex(current);
        void this.prepareCurrentShareImage(this.getCurrentShareSourceUrl(current));
    },
    getCurrentShareSourceUrl(index) {
        const imageList = Array.isArray(this.data.imageList) ? this.data.imageList : [];
        const safeIndex = typeof index === 'number' ? Math.max(0, Number(index)) : Math.max(0, Number(this.data.currentImageIndex || 0));
        return String(imageList[safeIndex] || this.data.mainImage || imageList[0] || '').trim();
    },
    async prepareCurrentShareImage(sourceUrl) {
        const shareSourceUrl = String(sourceUrl || this.getCurrentShareSourceUrl()).trim();
        if (!shareSourceUrl) {
            this.setData({
                shareImageUrl: '',
                shareImageSourceUrl: '',
            });
            return;
        }
        this.setData({
            shareImageUrl: '',
            shareImageSourceUrl: shareSourceUrl,
        });
        const shareImageUrl = await (0, shareImage_1.prepareShareCardImage)(shareSourceUrl);
        if (this.data.shareImageSourceUrl !== shareSourceUrl) {
            return;
        }
        if (shareImageUrl) {
            this.setData({ shareImageUrl });
        }
    },
    formatDateText(value, fallback = '刚刚发布') {
        if (!value) {
            return fallback;
        }
        const date = new Date(value);
        if (Number.isNaN(date.getTime())) {
            return String(value);
        }
        const month = String(date.getMonth() + 1).padStart(2, '0');
        const day = String(date.getDate()).padStart(2, '0');
        return `${month}-${day}`;
    },
    normalizeCommentItem(item) {
        const userName = String(item?.author_name || item?.userName || '用户').trim() || '用户';
        return {
            id: String(item?.id || Date.now()),
            userName,
            userAvatar: String(item?.author_avatar || item?.userAvatar || '').trim(),
            userAvatarText: userName.slice(0, 1) || '用',
            content: String(item?.content || '').trim(),
            createdAt: this.formatDateText(item?.created_at, '刚刚'),
        };
    },
    async loadComments() {
        if (!this.data.templateId) {
            return;
        }
        this.setData({ commentsLoading: true });
        try {
            const response = await new Promise((resolve, reject) => {
                wx.request({
                    url: `${API_BASE_URL}/api/v1/miniprogram/templates/${this.data.templateId}/comments?page=1&page_size=20`,
                    method: 'GET',
                    success: (res) => {
                        if (res.statusCode === 200 && res.data) {
                            const data = res.data;
                            if (data.code === 0) {
                                resolve(data.data || {});
                                return;
                            }
                            reject(new Error(data.msg || '获取评论失败'));
                            return;
                        }
                        reject(new Error(`请求失败: ${res.statusCode}`));
                    },
                    fail: reject,
                });
            });
            const list = Array.isArray(response.list) ? response.list.map((item) => this.normalizeCommentItem(item)) : [];
            this.setData({
                localComments: list,
                commentCount: Number(response.total || list.length || this.data.commentCount || 0),
                commentsLoading: false,
            });
        }
        catch (error) {
            console.error('加载评论失败:', error);
            this.setData({ commentsLoading: false });
        }
    },
    async handleShareSuccess() {
        if (!this.data.templateId) {
            return;
        }
        try {
            const response = await new Promise((resolve, reject) => {
                wx.request({
                    url: `${API_BASE_URL}/api/v1/miniprogram/templates/${this.data.templateId}/share`,
                    method: 'POST',
                    header: {
                        'Content-Type': 'application/json',
                    },
                    data: {
                        channel: 'miniprogram_share',
                    },
                    success: (res) => {
                        if (res.statusCode === 200 && res.data) {
                            const data = res.data;
                            if (data.code === 0) {
                                resolve(data.data || {});
                                return;
                            }
                            reject(new Error(data.msg || '记录分享失败'));
                            return;
                        }
                        reject(new Error(`请求失败: ${res.statusCode}`));
                    },
                    fail: reject,
                });
            });
            this.setData({
                shareCount: Number(response.share_count || this.data.shareCount || 0),
            });
        }
        catch (error) {
            console.error('记录分享失败:', error);
        }
    },
    updatePrimaryActionState() {
        if (this.data.isExhibitionTemplate) {
            this.setData({
                statusBadgeText: '仅展示',
                primaryActionText: '做同款',
                primaryActionHint: '基于当前展示图与说明，继续生成相似风格方案',
            });
            return;
        }
        if (this.data.isFree || this.data.unlocked) {
            this.setData({
                statusBadgeText: this.data.isFree ? '免费模板' : '已解锁',
                primaryActionText: '做同款',
                primaryActionHint: '自动带入模板信息，直接开始生成你的同款效果图',
            });
            return;
        }
        this.setData({
            statusBadgeText: `${this.data.price || 0} 灵石`,
            primaryActionText: '解锁后做同款',
            primaryActionHint: '解锁后即可按当前效果图直接生成同款',
        });
    },
    async loadLikedState() {
        const token = this.data.token;
        if (!token || !this.data.templateId) {
            this.setData({ isLiked: false });
            return;
        }
        try {
            const deviceID = (0, deviceFingerprint_1.getCachedDeviceFingerprint)() || '';
            const apiPath = '/api/v1/miniprogram/templates/liked-ids';
            const params = (0, parameter_1.generateRequestParams)(token, {}, apiPath, deviceID);
            const headers = {
                ...(0, parameter_1.paramsToHeaders)(params),
                'Content-Type': 'application/json',
            };
            const data = await new Promise((resolve, reject) => {
                wx.request({
                    url: `${API_BASE_URL}${apiPath}`,
                    method: 'GET',
                    header: headers,
                    success: (res) => {
                        if (res.statusCode === 200 && res.data) {
                            const responseData = res.data;
                            if (responseData.code === 0) {
                                resolve(responseData.data || {});
                                return;
                            }
                            reject(new Error(responseData.msg || '获取收藏状态失败'));
                            return;
                        }
                        reject(new Error(`请求失败: ${res.statusCode}`));
                    },
                    fail: reject,
                });
            });
            const ids = Array.isArray(data.ids) ? data.ids.map((item) => Number(item)) : [];
            this.setData({
                isLiked: ids.includes(Number(this.data.templateId)),
            });
        }
        catch (error) {
            console.error('获取收藏状态失败:', error);
        }
    },
});
