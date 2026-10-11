"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
// pages/generatehistory/generatehistory.ts
const parameter_1 = require("../../utils/parameter");
const currentPageSelection_1 = require("./currentPageSelection");
// API基础地址
const API_BASE_URL = 'https://api.jiadilingguang.com';
const GENERATE_DRAFT_STORAGE_KEY = 'jdlg_ai_generate_local_draft_v1';
const MANAGE_VISIBLE_FALLBACK_COUNT = 9;
function normalizeUrlValue(value) {
    return typeof value === 'string' ? value.trim() : '';
}
function pickFirstUrl(values) {
    for (const value of values) {
        const url = normalizeUrlValue(value);
        if (url) {
            return url;
        }
    }
    return '';
}
Page({
    data: {
        list: [],
        loading: false,
        loadingMore: false,
        page: 1,
        pageSize: 10,
        total: 0,
        hasMore: true,
        availableTools: [],
        selectedToolId: '',
        hasAITools: false,
        // 批量管理模式
        isEditMode: false,
        selectedIds: [],
        selectedMap: {},
        selectAll: false,
        visibleIds: [],
        // 左滑相关
        touchStartX: 0,
        touchStartY: 0,
        swipedItemId: '',
        hasLocalDraft: false,
        localDraft: null,
    },
    manageVisibilityObserver: null,
    visibleIdSet: {},
    onLoad() {
        this.loadLocalDraft();
        this.loadAITools();
        this.loadHistory();
    },
    onShow() {
        this.loadLocalDraft();
        // 每次显示页面时刷新列表（可能有新的生成记录）
    },
    onUnload() {
        this.disconnectManageVisibilityObserver();
    },
    loadLocalDraft() {
        const rawDraft = wx.getStorageSync(GENERATE_DRAFT_STORAGE_KEY);
        if (!rawDraft || typeof rawDraft !== 'object') {
            this.setData({
                hasLocalDraft: false,
                localDraft: null,
            });
            return;
        }
        const prompt = String(rawDraft.promptText || '').trim();
        const imageCount = [
            ...(Array.isArray(rawDraft.originalImages) ? rawDraft.originalImages : []),
            ...(Array.isArray(rawDraft.referenceImages) ? rawDraft.referenceImages : []),
            ...(Array.isArray(rawDraft.uploadedImages) ? rawDraft.uploadedImages : []),
        ].filter((item, index, array) => {
            const value = String(item || '').trim();
            return !!value && array.findIndex((inner) => String(inner || '').trim() === value) === index;
        }).length;
        const selectedStyle = String(rawDraft.selectedStyle || '').trim();
        const generateCount = Number(rawDraft.selectedGenerateCount || 1);
        const hasContent = !!prompt || imageCount > 0 || !!selectedStyle || generateCount > 1;
        if (!hasContent) {
            wx.removeStorageSync(GENERATE_DRAFT_STORAGE_KEY);
            this.setData({
                hasLocalDraft: false,
                localDraft: null,
            });
            return;
        }
        const title = prompt
            ? (prompt.length > 22 ? `${prompt.slice(0, 22)}...` : prompt)
            : '未提交生成草稿';
        const metaParts = [
            imageCount > 0 ? `${imageCount} 张参考图` : '',
            selectedStyle ? selectedStyle : '',
            generateCount > 1 ? `计划生成 ${Math.floor(generateCount)} 张` : '',
        ].filter(Boolean);
        this.setData({
            hasLocalDraft: true,
            localDraft: {
                ...rawDraft,
                draftTitle: title,
                draftMeta: metaParts.length ? metaParts.join(' · ') : '可继续编辑后提交生成',
                draftTimeText: this.formatTime(String(rawDraft.updated_at || '')),
            },
        });
    },
    onContinueLocalDraft() {
        wx.navigateTo({
            url: '/pages/aigenerate/aigenerate?draft=1',
        });
    },
    onDiscardLocalDraft() {
        wx.showModal({
            title: '丢弃草稿',
            content: '确定丢弃这份未提交的生成草稿吗？',
            confirmText: '丢弃',
            confirmColor: '#c4543a',
            cancelText: '取消',
            success: (res) => {
                if (!res.confirm)
                    return;
                wx.removeStorageSync(GENERATE_DRAFT_STORAGE_KEY);
                this.setData({
                    hasLocalDraft: false,
                    localDraft: null,
                });
                wx.showToast({ title: '草稿已丢弃', icon: 'success' });
            },
        });
    },
    async loadAITools() {
        try {
            const token = wx.getStorageSync('token');
            if (!token) {
                return;
            }
            const apiPath = '/api/v1/miniprogram/ai-tools';
            const params = (0, parameter_1.generateRequestParams)(token, '', apiPath);
            const headers = (0, parameter_1.paramsToHeaders)(params);
            const res = await new Promise((resolve, reject) => {
                wx.request({
                    url: `${API_BASE_URL}${apiPath}`,
                    method: 'GET',
                    header: headers,
                    success: (r) => resolve(r.data),
                    fail: reject
                });
            });
            if (res && res.code === 0 && Array.isArray(res.data)) {
                const tools = res.data;
                this.setData({
                    availableTools: tools,
                    hasAITools: tools.length > 0,
                });
            }
        }
        catch (err) {
            console.error('加载 AI 工具列表失败:', err);
        }
    },
    onFilterByTool(e) {
        const toolId = e.currentTarget.dataset.id;
        this.setData({
            selectedToolId: toolId,
            page: 1,
            hasMore: true,
        });
        this.loadHistory();
    },
    onPullDownRefresh() {
        this.setData({ page: 1, hasMore: true });
        this.loadHistory().then(() => {
            wx.stopPullDownRefresh();
        });
    },
    onReachBottom() {
        if (this.data.hasMore && !this.data.loadingMore) {
            this.loadMore();
        }
    },
    goToGenerate() {
        wx.navigateTo({
            url: '/pages/aigenerate/aigenerate'
        });
    },
    async getAuthHeaders() {
        const token = wx.getStorageSync('token');
        if (!token) {
            throw new Error('未登录');
        }
        const requestBody = '';
        const apiPath = '/api/v1/miniprogram/user/tasks';
        const params = (0, parameter_1.generateRequestParams)(token, requestBody, apiPath);
        return (0, parameter_1.paramsToHeaders)(params);
    },
    async loadHistory() {
        if (this.data.loading)
            return;
        this.setData({ loading: true });
        try {
            const headers = await this.getAuthHeaders();
            const { page, pageSize, selectedToolId } = this.data;
            let url = `${API_BASE_URL}/api/v1/miniprogram/user/tasks?page=${page}&page_size=${pageSize}`;
            if (selectedToolId) {
                url += `&tool_id=${selectedToolId}`;
            }
            return new Promise((resolve, reject) => {
                wx.request({
                    url,
                    method: 'GET',
                    header: headers,
                    success: (res) => {
                        if (res.data.code === 0) {
                            const tasks = res.data.data.tasks || [];
                            const total = res.data.data.total || 0;
                            const formattedTasks = this.formatTasks(tasks);
                            this.setData({
                                list: page === 1 ? formattedTasks : [...this.data.list, ...formattedTasks],
                                total,
                                hasMore: tasks.length >= pageSize,
                            }, () => this.syncManageSelectionAfterListChange());
                            resolve();
                        }
                        else {
                            wx.showToast({ title: res.data.msg || '加载失败', icon: 'none' });
                            reject(new Error(res.data.msg));
                        }
                    },
                    fail: (err) => {
                        console.error('加载历史失败:', err);
                        wx.showToast({ title: '网络错误', icon: 'none' });
                        reject(err);
                    },
                    complete: () => {
                        this.setData({ loading: false });
                    }
                });
            });
        }
        catch (error) {
            console.error('加载历史失败:', error);
            this.setData({ loading: false });
            wx.showToast({ title: '加载失败', icon: 'none' });
        }
    },
    async loadMore() {
        if (this.data.loadingMore || !this.data.hasMore)
            return;
        this.setData({
            loadingMore: true,
            page: this.data.page + 1
        });
        try {
            const headers = await this.getAuthHeaders();
            const { page, pageSize, selectedToolId } = this.data;
            let url = `${API_BASE_URL}/api/v1/miniprogram/user/tasks?page=${page}&page_size=${pageSize}`;
            if (selectedToolId) {
                url += `&tool_id=${selectedToolId}`;
            }
            wx.request({
                url,
                method: 'GET',
                header: headers,
                success: (res) => {
                    if (res.data.code === 0) {
                        const tasks = res.data.data.tasks || [];
                        const formattedTasks = this.formatTasks(tasks);
                        this.setData({
                            list: [...this.data.list, ...formattedTasks],
                            hasMore: tasks.length >= pageSize,
                        }, () => this.syncManageSelectionAfterListChange());
                    }
                },
                complete: () => {
                    this.setData({ loadingMore: false });
                }
            });
        }
        catch (error) {
            console.error('加载更多失败:', error);
            this.setData({ loadingMore: false });
        }
    },
    formatTasks(tasks) {
        const sceneMap = {
            'ai_draw_single': 'AI绘画',
            'ai_draw_multi': 'AI绘画',
            'ai_chat_single': 'AI对话（单轮）',
            'ai_chat_multi': 'AI对话（多轮）',
            'ai_video': 'AI生成视频',
            'ai_cost_doc': 'AI造价(文档)',
        };
        return tasks.map(task => {
            const result = task.result || {};
            const imageCandidates = [
                result.url,
                result.image_url,
                ...(Array.isArray(result.images) ? result.images : []),
                ...(Array.isArray(result.image_urls) ? result.image_urls : []),
                ...(Array.isArray(result.urls) ? result.urls : []),
            ];
            const imageUrl = pickFirstUrl(imageCandidates);
            const displayThumbnailUrl = pickFirstUrl([
                result.thumbnail_url,
                result.thumbnail,
                result.thumb_url,
                result.list_thumb,
                result.cover_image,
                result.preview_url,
                task.thumbnail_url,
                task.thumbnail,
                imageUrl,
            ]);
            const excelUrl = (task.result && task.result.excel_url) ? task.result.excel_url : '';
            let prompt = '';
            if (task.user_prompt) {
                prompt = task.user_prompt;
            }
            else if (task.prompt) {
                prompt = task.prompt;
            }
            else {
                try {
                    if (task.request_payload) {
                        const payload = typeof task.request_payload === 'string'
                            ? JSON.parse(task.request_payload)
                            : task.request_payload;
                        prompt = payload.prompt || '';
                    }
                }
                catch (e) {
                    // ignore
                }
            }
            const resultImageCount = imageCandidates.filter((item, index, array) => {
                const value = typeof item === 'string' ? item.trim() : '';
                return !!value && array.findIndex((inner) => String(inner || '').trim() === value) === index;
            }).length;
            const referenceImages = [
                ...(Array.isArray(task.reference_image_urls) ? task.reference_image_urls : []),
                task.reference_image_url,
            ].filter((item, index, array) => {
                const value = typeof item === 'string' ? item.trim() : '';
                return !!value && array.findIndex((inner) => String(inner || '').trim() === value) === index;
            });
            return {
                ...task,
                sceneText: (task.scene === 'ai_draw_single' || task.scene === 'ai_draw_multi')
                    ? (resultImageCount > 1 ? `AI绘画（${resultImageCount}张）` : 'AI绘画')
                    : (sceneMap[task.scene] || task.scene),
                createdAtText: this.formatTime(task.created_at),
                imageUrl,
                displayThumbnailUrl,
                prompt,
                reference_image_url: referenceImages[0] || '',
                reference_image_urls: referenceImages,
                referencePreviewUrls: referenceImages.slice(0, 3),
                referenceOverflowCount: Math.max(referenceImages.length - 3, 0),
                excel_url: excelUrl,
            };
        });
    },
    formatTime(timeStr) {
        if (!timeStr)
            return '';
        const date = new Date(timeStr);
        if (Number.isNaN(date.getTime()))
            return '';
        const now = new Date();
        const year = date.getFullYear().toString();
        const month = (date.getMonth() + 1).toString().padStart(2, '0');
        const day = date.getDate().toString().padStart(2, '0');
        const hours = date.getHours().toString().padStart(2, '0');
        const minutes = date.getMinutes().toString().padStart(2, '0');
        if (date.getFullYear() === now.getFullYear()) {
            return `${month}-${day} ${hours}:${minutes}`;
        }
        return `${year}-${month}-${day} ${hours}:${minutes}`;
    },
    /** 复制 Excel 下载链接（ai_cost_doc 任务） */
    onCopyExcelLink(e) {
        const item = e.currentTarget.dataset.item;
        const url = item && item.excel_url;
        if (!url) {
            wx.showToast({ title: '暂无下载链接', icon: 'none' });
            return;
        }
        wx.setClipboardData({
            data: url,
            success: () => {
                wx.showToast({ title: '已复制，请在浏览器中打开下载', icon: 'success' });
            },
            fail: () => {
                wx.showToast({ title: '复制失败', icon: 'none' });
            }
        });
    },
    goToDetail(e) {
        const item = e.currentTarget.dataset.item;
        const isMakeSameTask = Array.isArray(item?.original_image_urls)
            && item.original_image_urls.some((image) => typeof image === 'string' && image.trim() !== '');
        const sourceQuery = isMakeSameTask ? '&source=make_same' : '';
        wx.navigateTo({
            url: `/pages/generatedetails/generatedetails?task_no=${item.task_no}${sourceQuery}`,
            success: (res) => {
                res.eventChannel.emit('taskData', item);
            }
        });
    },
    /** 切换编辑模式 */
    onToggleEditMode() {
        const isEditMode = !this.data.isEditMode;
        this.setData({
            isEditMode,
            selectedIds: [],
            selectedMap: {},
            selectAll: false,
            visibleIds: [],
            swipedItemId: '',
        }, () => {
            if (isEditMode) {
                this.startManageVisibilityObserver();
            }
            else {
                this.disconnectManageVisibilityObserver();
            }
        });
    },
    /** 左滑手势 - 开始 */
    onTouchStart(e) {
        const id = e.currentTarget.dataset.id;
        // 如果当前有打开的滑动项且不是当前项，先关闭
        if (this.data.swipedItemId && this.data.swipedItemId !== id) {
            this.setData({ swipedItemId: '' });
        }
        this.setData({
            touchStartX: e.touches[0].clientX,
            touchStartY: e.touches[0].clientY,
        });
    },
    /** 左滑手势 - 移动 */
    onTouchMove(e) {
        if (this.data.isEditMode)
            return;
        const deltaX = e.touches[0].clientX - this.data.touchStartX;
        const deltaY = Math.abs(e.touches[0].clientY - this.data.touchStartY);
        // 纵向滑动不处理
        if (deltaY > Math.abs(deltaX))
            return;
        // 向左滑超过阈值才打开
        if (deltaX < -60) {
            const id = e.currentTarget.dataset.id;
            if (this.data.swipedItemId !== id) {
                this.setData({ swipedItemId: id });
            }
        }
    },
    /** 左滑手势 - 结束 */
    onTouchEnd(e) {
        const deltaX = (e.changedTouches[0].clientX || 0) - this.data.touchStartX;
        // 向右滑时关闭
        if (deltaX > 60 && this.data.swipedItemId) {
            this.setData({ swipedItemId: '' });
        }
    },
    /** 点击列表项 */
    onItemClick(e) {
        // 如果有左滑打开的项，先关闭
        if (this.data.swipedItemId) {
            this.setData({ swipedItemId: '' });
            return;
        }
        // 编辑模式下点击切换选中
        if (this.data.isEditMode) {
            const id = e.currentTarget.dataset.id;
            if (id)
                this.toggleSelect(id);
            return;
        }
        // 正常模式跳转详情
        this.goToDetail(e);
    },
    /** 切换单项选中 */
    toggleSelect(id) {
        const selectedIds = [...this.data.selectedIds];
        const idx = selectedIds.indexOf(id);
        if (idx > -1) {
            selectedIds.splice(idx, 1);
        }
        else {
            selectedIds.push(id);
        }
        this.applySelectionState(selectedIds);
    },
    /** 复选框点击 */
    onToggleSelect(e) {
        const id = e.currentTarget.dataset.id;
        if (id)
            this.toggleSelect(id);
    },
    /** 全选/取消全选 */
    onToggleSelectAll() {
        const selectedIds = (0, currentPageSelection_1.toggleCurrentSelection)(this.data.selectedIds, this.getCurrentManageIds());
        this.applySelectionState(selectedIds);
    },
    applySelectionState(selectedIds) {
        const currentIds = this.getCurrentManageIds();
        this.setData({
            selectedIds,
            selectedMap: (0, currentPageSelection_1.buildSelectedMap)(selectedIds),
            selectAll: (0, currentPageSelection_1.isEveryCurrentSelected)(selectedIds, currentIds),
        });
    },
    getCurrentManageIds() {
        return (0, currentPageSelection_1.getCurrentSelectableIds)(this.data.list, (item) => item.task_no, this.data.visibleIds, MANAGE_VISIBLE_FALLBACK_COUNT);
    },
    syncManageSelectionAfterListChange() {
        const availableMap = (0, currentPageSelection_1.buildSelectedMap)(this.data.list.map((item) => item.task_no));
        const selectedIds = this.data.selectedIds.filter((id) => availableMap[id]);
        this.visibleIdSet = Object.keys(this.visibleIdSet || {}).reduce((acc, id) => {
            if (availableMap[id]) {
                acc[id] = true;
            }
            return acc;
        }, {});
        this.applySelectionState(selectedIds);
        if (this.data.isEditMode) {
            this.startManageVisibilityObserver();
        }
    },
    refreshManageVisibleIds() {
        const nextVisibleIds = this.data.list
            .map((item) => item.task_no)
            .filter((id) => this.visibleIdSet[id]);
        const currentVisibleIds = this.data.visibleIds || [];
        if (nextVisibleIds.join('|') === currentVisibleIds.join('|')) {
            this.refreshManageSelectAllState();
            return;
        }
        this.setData({ visibleIds: nextVisibleIds }, () => this.refreshManageSelectAllState());
    },
    refreshManageSelectAllState() {
        const nextSelectAll = (0, currentPageSelection_1.isEveryCurrentSelected)(this.data.selectedIds, this.getCurrentManageIds());
        if (nextSelectAll !== this.data.selectAll) {
            this.setData({ selectAll: nextSelectAll });
        }
    },
    startManageVisibilityObserver() {
        this.disconnectManageVisibilityObserver();
        this.visibleIdSet = {};
        this.setData({ visibleIds: [] });
        if (!this.data.isEditMode) {
            return;
        }
        wx.nextTick(() => {
            if (!this.data.isEditMode) {
                return;
            }
            const createObserver = this.createIntersectionObserver;
            if (typeof createObserver !== 'function') {
                return;
            }
            const observer = createObserver.call(this, { observeAll: true });
            this.manageVisibilityObserver = observer;
            observer.relativeToViewport().observe('.manage-card', (res) => {
                const id = String(res?.dataset?.id || '').trim();
                if (!id) {
                    return;
                }
                if (Number(res?.intersectionRatio || 0) > 0) {
                    this.visibleIdSet[id] = true;
                }
                else {
                    delete this.visibleIdSet[id];
                }
                this.refreshManageVisibleIds();
            });
        });
    },
    disconnectManageVisibilityObserver() {
        if (this.manageVisibilityObserver && typeof this.manageVisibilityObserver.disconnect === 'function') {
            this.manageVisibilityObserver.disconnect();
        }
        this.manageVisibilityObserver = null;
        this.visibleIdSet = {};
    },
    /** 删除单条记录（左滑触发，二次确认） */
    onDelete(e) {
        const item = e.currentTarget.dataset.item;
        if (!item || !item.task_no)
            return;
        wx.showModal({
            title: '确认删除',
            content: '删除后无法恢复，确定要删除这条记录吗？',
            confirmText: '删除',
            confirmColor: '#c4543a',
            cancelText: '取消',
            success: (res) => {
                if (!res.confirm)
                    return;
                this.doDeleteByTaskNo(item.task_no).then(() => {
                    this.setData({
                        list: this.data.list.filter((t) => t.task_no !== item.task_no),
                        total: Math.max(0, (this.data.total || 0) - 1),
                        swipedItemId: '',
                    });
                    wx.showToast({ title: '已删除', icon: 'success' });
                }).catch(() => {
                    wx.showToast({ title: '删除失败', icon: 'none' });
                });
            }
        });
    },
    /** 批量删除 */
    onBatchDelete() {
        const { selectedIds } = this.data;
        if (!selectedIds.length)
            return;
        wx.showModal({
            title: '确认删除',
            content: `确定要删除选中的 ${selectedIds.length} 条记录吗？删除后无法恢复。`,
            confirmText: '删除',
            confirmColor: '#c4543a',
            cancelText: '取消',
            success: async (res) => {
                if (!res.confirm)
                    return;
                wx.showLoading({ title: '删除中...' });
                let failCount = 0;
                for (const taskNo of selectedIds) {
                    try {
                        await this.doDeleteByTaskNo(taskNo);
                    }
                    catch {
                        failCount++;
                    }
                }
                wx.hideLoading();
                if (failCount > 0) {
                    wx.showToast({ title: `${failCount} 条删除失败`, icon: 'none' });
                }
                else {
                    wx.showToast({ title: '已删除', icon: 'success' });
                }
                // 重新加载列表
                this.setData({
                    isEditMode: false,
                    selectedIds: [],
                    selectedMap: {},
                    selectAll: false,
                    visibleIds: [],
                    page: 1,
                    hasMore: true,
                }, () => this.disconnectManageVisibilityObserver());
                this.loadHistory();
            }
        });
    },
    /** 按 task_no 删除（通用方法） */
    async doDeleteByTaskNo(taskNo) {
        const token = wx.getStorageSync('token');
        if (!token) {
            throw new Error('未登录');
        }
        const apiPath = `/api/v1/miniprogram/user/tasks/${taskNo}`;
        const params = (0, parameter_1.generateRequestParams)(token, '', apiPath);
        const headers = (0, parameter_1.paramsToHeaders)(params);
        const res = await new Promise((resolve, reject) => {
            wx.request({
                url: `${API_BASE_URL}${apiPath}`,
                method: 'DELETE',
                header: headers,
                success: (r) => resolve(r.data),
                fail: reject
            });
        });
        if (!res || res.code !== 0) {
            throw new Error((res && res.msg) || '删除失败');
        }
    },
    onShareAppMessage() {
        return {
            title: '甲第灵光 · 生成历史',
            path: '/pages/generatehistory/generatehistory',
        };
    },
});
