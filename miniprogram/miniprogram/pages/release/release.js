"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
// pages/release/release.ts
const parameter_1 = require("../../utils/parameter");
const deviceFingerprint_1 = require("../../utils/deviceFingerprint");
const asset_1 = require("../../utils/asset");
const API_BASE_URL = 'https://api.jiadilingguang.com';
const DEFAULT_COVER_IMAGE = (0, asset_1.resolveAssetPath)('/assets/images/home.jpg');
function normalizeImageUrl(url) {
    const cleanUrl = String(url || '').trim();
    if (!cleanUrl) {
        return DEFAULT_COVER_IMAGE;
    }
    if (/^(https?:\/\/|wxfile:\/\/|file:\/\/|data:)/i.test(cleanUrl)) {
        return cleanUrl;
    }
    if (cleanUrl.startsWith('//')) {
        return `https:${cleanUrl}`;
    }
    if (cleanUrl.startsWith('/')) {
        return `${API_BASE_URL}${cleanUrl}`;
    }
    return `${API_BASE_URL}/${cleanUrl}`;
}
function formatDisplayTime(value) {
    const text = String(value || '').trim();
    if (!text) {
        return '--';
    }
    const matched = text.match(/^(\d{4})[-/](\d{1,2})[-/](\d{1,2})(?:[ T](\d{1,2}):(\d{1,2})(?::\d{1,2})?)?/);
    if (matched) {
        const [, year, month, day, hour = '00', minute = '00'] = matched;
        return `${year}-${month.padStart(2, '0')}-${day.padStart(2, '0')} ${hour.padStart(2, '0')}:${minute.padStart(2, '0')}`;
    }
    return text.replace('T', ' ').slice(0, 16);
}
Page({
    data: {
        token: '',
        deviceId: '',
        loading: true,
        // 统计
        totalWorks: 0,
        totalViews: 0,
        totalIncome: '0.00',
        monthIncome: '0.00',
        fansCount: 0,
        receivedLikes: 0,
        followingCount: 0,
        // 筛选
        currentTab: 'works',
        currentRange: '7d',
        // 分页
        page: 1,
        pageSize: 10,
        hasMore: true,
        // 数据
        works: [],
        incomeRecords: [],
        isManageMode: false,
        selectedIds: [],
        selectedMap: {},
        selectAll: false,
        swipedWorkId: 0,
        touchStartX: 0,
        touchStartY: 0,
        touchMoved: false,
    },
    async onLoad() {
        await this.initDeviceId();
        this.initToken();
        await this.loadSummary();
        await this.loadList(true);
    },
    onShow() {
        if (this.data.token) {
            this.loadSummary();
            this.loadList(true);
        }
    },
    async initDeviceId() {
        let deviceId = (0, deviceFingerprint_1.getCachedDeviceFingerprint)();
        if (!deviceId) {
            try {
                deviceId = await (0, deviceFingerprint_1.generateDeviceFingerprint)();
                if (deviceId) {
                    (0, deviceFingerprint_1.cacheDeviceFingerprint)(deviceId);
                }
            }
            catch (e) {
                console.error('获取设备ID失败:', e);
            }
        }
        this.setData({ deviceId: deviceId || '' });
    },
    initToken() {
        const token = wx.getStorageSync('token');
        this.setData({ token: token || '' });
        if (!token) {
            wx.showToast({
                title: '请先登录',
                icon: 'none',
            });
        }
    },
    getAuthHeaders(apiPath, body = {}) {
        const token = this.data.token;
        if (!token)
            return null;
        const params = (0, parameter_1.generateRequestParams)(token, body, apiPath, this.data.deviceId);
        return {
            ...(0, parameter_1.paramsToHeaders)(params),
            'Content-Type': 'application/json',
        };
    },
    // 顶部统计（作品数量/曝光/收益）- 接入 /user/templates/summary
    async loadSummary() {
        const token = this.data.token;
        if (!token)
            return;
        try {
            const apiPath = '/api/v1/miniprogram/user/templates/summary';
            const headers = this.getAuthHeaders(apiPath, {});
            if (!headers)
                return;
            const data = await new Promise((resolve, reject) => {
                wx.request({
                    url: `${API_BASE_URL}${apiPath}`,
                    method: 'GET',
                    header: headers,
                    success: (res) => {
                        if (res.statusCode === 200 && res.data) {
                            const d = res.data;
                            if (d.code === 0) {
                                resolve(d.data);
                            }
                            else {
                                reject(new Error(d.msg || '获取统计失败'));
                            }
                        }
                        else {
                            reject(new Error(`请求失败: ${res.statusCode}`));
                        }
                    },
                    fail: reject,
                });
            });
            const totalEarnings = data.total_earnings ?? data.total_income ?? 0;
            const monthEarnings = data.month_earnings ?? data.month_income ?? 0;
            this.setData({
                totalWorks: data.total_works ?? data.total_plans ?? 0,
                totalViews: data.total_views ?? 0,
                totalIncome: String(totalEarnings),
                monthIncome: String(monthEarnings),
                fansCount: Number(data.fans_count || 0),
                receivedLikes: Number(data.received_likes || 0),
                followingCount: Number(data.following_count || 0),
            });
        }
        catch (err) {
            console.error('获取发布统计失败:', err);
        }
    },
    async loadList(reset = false) {
        const token = this.data.token;
        if (!token) {
            this.setData({ loading: false });
            return;
        }
        if (reset) {
            this.setData({
                page: 1,
                hasMore: true,
            });
        }
        else {
            if (!this.data.hasMore || this.data.loading)
                return;
        }
        const page = reset ? 1 : this.data.page + 1;
        this.setData({ loading: true });
        const apiPath = this.data.currentTab === 'works'
            ? '/api/v1/miniprogram/user/templates'
            : '/api/v1/miniprogram/user/templates/income';
        const body = {
            page,
            page_size: this.data.pageSize,
        };
        if (this.data.currentTab === 'works') {
            body.category = '';
        }
        try {
            const headers = this.getAuthHeaders(apiPath, body);
            if (!headers)
                return;
            const resp = await new Promise((resolve, reject) => {
                wx.request({
                    url: `${API_BASE_URL}${apiPath}`,
                    method: 'POST',
                    header: headers,
                    data: body,
                    success: (res) => {
                        if (res.statusCode === 200 && res.data) {
                            const d = res.data;
                            if (d.code === 0) {
                                resolve(d.data);
                            }
                            else {
                                reject(new Error(d.msg || '获取列表失败'));
                            }
                        }
                        else {
                            reject(new Error(`请求失败: ${res.statusCode}`));
                        }
                    },
                    fail: reject,
                });
            });
            const list = resp.list || [];
            const total = resp.total || 0;
            if (this.data.currentTab === 'works') {
                const mapped = list.map((item, index) => ({
                    id: item.id || index,
                    title: item.title || item.name || '未命名作品',
                    cover: normalizeImageUrl(item.cover || item.thumbnail || item.preview_url || DEFAULT_COVER_IMAGE),
                    desc: item.desc || item.description || '',
                    status: this.getStatusClass(item.status),
                    statusText: this.getStatusText(item.status),
                    views: item.views || item.exposure || item.download_count || 0,
                    likes: item.likes || item.favorites || item.like_count || 0,
                    income: Number(item.income || item.earnings || 0).toFixed(2),
                    time: formatDisplayTime(item.created_at || ''),
                    templateId: item.id,
                    publishScope: item.publish_scope === 'homepage_only' ? 'homepage_only' : 'square',
                    publishScopeText: item.publish_scope === 'homepage_only' ? '仅主页展示' : '主页 + 模板广场',
                    rejectReason: String(item.reject_reason || ''),
                    sourceTypeText: this.getSourceTypeText(item.source_type),
                    canViewDetail: item.publish_scope === 'square' && item.status === 'published',
                    viewActionText: item.publish_scope === 'square' && item.status === 'published' ? '查看作品' : '预览作品',
                }));
                const newList = reset ? mapped : this.data.works.concat(mapped);
                const selectedIds = this.data.isManageMode
                    ? this.data.selectedIds.filter((id) => newList.some((work) => work.id === id))
                    : [];
                this.setData({
                    works: newList,
                    page,
                    hasMore: newList.length < total,
                    selectedIds,
                    selectedMap: selectedIds.reduce((acc, id) => {
                        acc[id] = true;
                        return acc;
                    }, {}),
                    selectAll: selectedIds.length === newList.length && newList.length > 0,
                });
            }
            else {
                const mapped = list.map((item, index) => ({
                    id: item.id || index,
                    title: item.title || item.remark || '模板付费',
                    source: item.source || item.scene_desc || '模板付费',
                    amount: String(item.amount || 0),
                    time: formatDisplayTime(item.created_at || ''),
                    remark: item.remark || '',
                }));
                const newList = reset ? mapped : this.data.incomeRecords.concat(mapped);
                this.setData({
                    incomeRecords: newList,
                    page,
                    hasMore: newList.length < total,
                });
            }
        }
        catch (err) {
            console.error('获取列表失败:', err);
            wx.showToast({
                title: '加载失败',
                icon: 'none',
            });
        }
        finally {
            this.setData({ loading: false });
            wx.stopPullDownRefresh();
        }
    },
    getStatusText(status) {
        if (status === 'published' || status === 'online')
            return '已上线';
        if (status === 'pending' || status === 'review')
            return '审核中';
        if (status === 'rejected')
            return '已拒绝';
        if (status === 'draft')
            return '草稿';
        if (status === 'archived' || status === 'offline')
            return '已下线';
        return '未知状态';
    },
    getStatusClass(status) {
        if (status === 'published' || status === 'online')
            return 'online';
        if (status === 'pending' || status === 'review')
            return 'review';
        if (status === 'rejected')
            return 'rejected';
        return 'offline';
    },
    getSourceTypeText(sourceType) {
        if (sourceType === 'album_upload')
            return '相册上传';
        if (sourceType === 'ai_generated')
            return 'AI 生成';
        return '后台创建';
    },
    onTabChange(e) {
        const tab = e.currentTarget.dataset.tab;
        if (tab === this.data.currentTab)
            return;
        this.setData({
            currentTab: tab,
            isManageMode: false,
            selectedIds: [],
            selectedMap: {},
            selectAll: false,
            swipedWorkId: 0,
        });
        this.loadList(true);
    },
    onRangeChange(e) {
        const range = e.currentTarget.dataset.range;
        if (range === this.data.currentRange)
            return;
        this.setData({
            currentRange: range,
        });
        this.loadSummary();
        this.loadList(true);
    },
    onPullDownRefresh() {
        this.loadSummary();
        this.loadList(true);
    },
    onReachBottom() {
        this.loadList(false);
    },
    onCreateWork() {
        if (this.data.isManageMode)
            return;
        wx.navigateTo({
            url: '/pages/release/designerworkpublish',
            fail: () => {
                wx.showToast({
                    title: '页面跳转失败',
                    icon: 'none',
                });
            },
        });
    },
    onViewWork(e) {
        if (this.data.isManageMode)
            return;
        const id = e.currentTarget.dataset.id;
        const canViewDetail = e.currentTarget.dataset.canViewDetail === true || e.currentTarget.dataset.canViewDetail === 'true';
        const cover = String(e.currentTarget.dataset.cover || '');
        if (id && canViewDetail) {
            wx.navigateTo({
                url: `/pages/templatesquaredetails/templatesquaredetails?id=${id}&source=release`,
            });
            return;
        }
        if (cover) {
            wx.previewImage({
                current: cover,
                urls: [cover],
                showmenu: false,
            });
        }
    },
    onEditWork(e) {
        if (this.data.isManageMode)
            return;
        const editId = Number(e.currentTarget.dataset.id || 0);
        if (!editId)
            return;
        wx.navigateTo({
            url: `/pages/release/designerworkpublish?id=${editId}&mode=edit`,
            fail: () => {
                wx.showToast({
                    title: '页面跳转失败',
                    icon: 'none',
                });
            },
        });
        return;
    },
    onToggleManageMode() {
        const nextMode = !this.data.isManageMode;
        this.setData({
            isManageMode: nextMode,
            selectedIds: [],
            selectedMap: {},
            selectAll: false,
            swipedWorkId: 0,
        });
    },
    onToggleSelect(e) {
        const id = Number(e.currentTarget.dataset.id || 0);
        if (!id)
            return;
        const selectedIds = [...this.data.selectedIds];
        const index = selectedIds.indexOf(id);
        if (index >= 0) {
            selectedIds.splice(index, 1);
        }
        else {
            selectedIds.push(id);
        }
        this.applySelectionState(selectedIds);
    },
    onToggleSelectAll() {
        const selectAll = !this.data.selectAll;
        const selectedIds = selectAll ? this.data.works.map((item) => item.id) : [];
        this.applySelectionState(selectedIds);
    },
    applySelectionState(selectedIds) {
        const selectedMap = selectedIds.reduce((acc, id) => {
            acc[id] = true;
            return acc;
        }, {});
        this.setData({
            selectedIds,
            selectedMap,
            selectAll: selectedIds.length === this.data.works.length && this.data.works.length > 0,
        });
    },
    onWorkTouchStart(e) {
        if (this.data.isManageMode)
            return;
        const touch = e.touches && e.touches[0];
        if (!touch)
            return;
        this.setData({
            touchStartX: touch.clientX,
            touchStartY: touch.clientY,
            touchMoved: false,
        });
    },
    onWorkTouchMove(e) {
        if (this.data.isManageMode)
            return;
        const touch = e.touches && e.touches[0];
        if (!touch)
            return;
        const deltaX = touch.clientX - this.data.touchStartX;
        const deltaY = touch.clientY - this.data.touchStartY;
        if (Math.abs(deltaX) > Math.abs(deltaY) && Math.abs(deltaX) > 18) {
            this.setData({ touchMoved: true });
        }
    },
    onWorkTouchEnd(e) {
        if (this.data.isManageMode)
            return;
        const touch = (e.changedTouches && e.changedTouches[0]) || null;
        if (!touch)
            return;
        const id = Number(e.currentTarget.dataset.id || 0);
        const deltaX = touch.clientX - this.data.touchStartX;
        const deltaY = touch.clientY - this.data.touchStartY;
        if (!id || Math.abs(deltaY) > Math.abs(deltaX)) {
            return;
        }
        if (deltaX < -42) {
            this.setData({ swipedWorkId: id, touchMoved: false });
            return;
        }
        if (deltaX > 24 && this.data.swipedWorkId === id) {
            this.setData({ swipedWorkId: 0, touchMoved: false });
        }
    },
    onDeleteWork(e) {
        const id = Number(e.currentTarget.dataset.id || 0);
        const title = String(e.currentTarget.dataset.title || '该作品');
        if (!id)
            return;
        wx.showModal({
            title: '确认删除',
            content: `确定要删除「${title}」吗？删除后无法恢复。`,
            confirmText: '删除',
            confirmColor: '#c4543a',
            cancelText: '取消',
            success: async (res) => {
                if (!res.confirm)
                    return;
                try {
                    await this.deleteWork(id);
                    const works = this.data.works.filter((item) => item.id !== id);
                    const selectedIds = this.data.selectedIds.filter((itemId) => itemId !== id);
                    this.setData({
                        works,
                        swipedWorkId: 0,
                    });
                    this.applySelectionState(selectedIds);
                    this.loadSummary();
                    wx.showToast({ title: '已删除', icon: 'success' });
                }
                catch (err) {
                    wx.showToast({
                        title: err?.message || '删除失败',
                        icon: 'none',
                    });
                }
            },
        });
    },
    async deleteWork(id) {
        const apiPath = `/api/v1/miniprogram/user/templates/${id}`;
        const headers = this.getAuthHeaders(apiPath, '');
        if (!headers) {
            throw new Error('请先登录');
        }
        const result = await new Promise((resolve, reject) => {
            wx.request({
                url: `${API_BASE_URL}${apiPath}`,
                method: 'DELETE',
                header: headers,
                success: (res) => resolve(res.data),
                fail: reject,
            });
        });
        if (!result || result.code !== 0) {
            throw new Error(result?.msg || '删除失败');
        }
    },
    onBatchDeleteWorks() {
        const selectedIds = this.data.selectedIds;
        if (!selectedIds.length)
            return;
        wx.showModal({
            title: '确认删除',
            content: `确定要删除选中的 ${selectedIds.length} 个作品吗？删除后无法恢复。`,
            confirmText: '删除',
            confirmColor: '#c4543a',
            cancelText: '取消',
            success: async (res) => {
                if (!res.confirm)
                    return;
                try {
                    wx.showLoading({ title: '删除中...', mask: true });
                    await this.batchDeleteWorks(selectedIds);
                    this.setData({
                        isManageMode: false,
                        selectedIds: [],
                        selectedMap: {},
                        selectAll: false,
                        swipedWorkId: 0,
                        page: 1,
                        hasMore: true,
                    });
                    await this.loadSummary();
                    await this.loadList(true);
                    wx.showToast({ title: '已删除', icon: 'success' });
                }
                catch (err) {
                    wx.showToast({
                        title: err?.message || '删除失败',
                        icon: 'none',
                    });
                }
                finally {
                    wx.hideLoading();
                }
            },
        });
    },
    async batchDeleteWorks(ids) {
        const apiPath = '/api/v1/miniprogram/user/templates/batch-delete';
        const body = { ids };
        const headers = this.getAuthHeaders(apiPath, body);
        if (!headers) {
            throw new Error('请先登录');
        }
        const result = await new Promise((resolve, reject) => {
            wx.request({
                url: `${API_BASE_URL}${apiPath}`,
                method: 'POST',
                header: headers,
                data: body,
                success: (res) => resolve(res.data),
                fail: reject,
            });
        });
        if (!result || result.code !== 0) {
            throw new Error(result?.msg || '批量删除失败');
        }
    },
    onShareWork(e) {
        const id = e.currentTarget.dataset.id;
        console.log('推广作品', id);
        wx.showToast({
            title: '推广功能待接入',
            icon: 'none',
        });
    },
    onShareAppMessage() {
        return {
            title: '我的发布',
            path: '/pages/release/release',
        };
    },
});
