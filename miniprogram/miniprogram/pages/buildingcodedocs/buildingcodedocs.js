"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
const parameter_1 = require("../../utils/parameter");
const deviceFingerprint_1 = require("../../utils/deviceFingerprint");
const API_BASE_URL = 'https://api.jiadilingguang.com';
const DOCS_API_PATH = '/api/v1/miniprogram/building-code-documents';
const UPLOAD_API_PATH = '/api/v1/miniprogram/building-code-documents/upload';
function formatSize(size) {
    if (!size)
        return '0KB';
    if (size >= 1024 * 1024)
        return `${(size / 1024 / 1024).toFixed(1)}MB`;
    return `${Math.max(1, Math.round(size / 1024))}KB`;
}
function statusText(status) {
    const map = {
        uploaded: '已上传',
        indexing: '知识库导入中',
        ready: '已进入全站问答',
        failed: '导入失败',
    };
    return map[status] || '处理中';
}
Page({
    data: {
        docs: [],
        loading: false,
        uploading: false,
        deviceId: '',
        initialized: false,
    },
    // 轮询定时器（不放在 data 里，避免触发 setData）
    pollTimer: null,
    async onLoad() {
        await this.initDeviceId();
        this.loadDocs();
    },
    onShow() {
        if (this.data.initialized) {
            this.loadDocs();
        }
    },
    onHide() {
        this.stopPolling();
    },
    onUnload() {
        this.stopPolling();
    },
    // 启动轮询：5 秒刷新一次列表，仅当存在 indexing/uploaded 状态时
    startPolling() {
        if (this.pollTimer)
            return;
        this.pollTimer = setInterval(() => {
            this.loadDocs();
        }, 5000);
    },
    stopPolling() {
        if (this.pollTimer) {
            clearInterval(this.pollTimer);
            this.pollTimer = null;
        }
    },
    getAuthHeaders(apiPath, body = {}) {
        const token = wx.getStorageSync('token');
        if (!token)
            return null;
        const params = (0, parameter_1.generateRequestParams)(token, body, apiPath, this.data.deviceId);
        return {
            ...(0, parameter_1.paramsToHeaders)(params),
            'Content-Type': 'application/json',
        };
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
                deviceId = '';
            }
        }
        this.setData({ deviceId: deviceId || '', initialized: true });
    },
    async loadDocs() {
        const headers = this.getAuthHeaders(DOCS_API_PATH);
        if (!headers) {
            wx.showToast({ title: '请先登录', icon: 'none' });
            return;
        }
        this.setData({ loading: true });
        wx.request({
            url: `${API_BASE_URL}${DOCS_API_PATH}`,
            method: 'GET',
            header: headers,
            success: (res) => {
                const body = res.data || {};
                if (res.statusCode === 200 && body.code === 0) {
                    const list = ((body.data || {}).list || []).map((item) => ({
                        ...item,
                        size_text: formatSize(Number(item.file_size || 0)),
                        status_text: statusText(String(item.index_status || '')),
                    }));
                    this.setData({ docs: list });
                    // 存在未完成索引的文档则启动轮询，全部完成则停止
                    const hasPending = list.some((item) => item.index_status === 'uploaded' || item.index_status === 'indexing');
                    if (hasPending) {
                        this.startPolling();
                    }
                    else {
                        this.stopPolling();
                    }
                    return;
                }
                wx.showToast({ title: body.msg || '加载失败', icon: 'none' });
            },
            fail: () => wx.showToast({ title: '加载失败', icon: 'none' }),
            complete: () => this.setData({ loading: false }),
        });
    },
    onChooseFile() {
        if (this.data.uploading)
            return;
        const token = wx.getStorageSync('token');
        if (!token) {
            wx.showToast({ title: '请先登录', icon: 'none' });
            return;
        }
        wx.chooseMessageFile({
            count: 1,
            type: 'file',
            extension: ['pdf', 'docx', 'txt', 'md'],
            success: (res) => {
                const file = (res.tempFiles || [])[0];
                if (!file)
                    return;
                this.uploadFile(file);
            },
        });
    },
    uploadFile(file) {
        const token = wx.getStorageSync('token');
        const params = (0, parameter_1.generateRequestParams)(token, '{}', UPLOAD_API_PATH, this.data.deviceId);
        this.setData({ uploading: true });
        wx.uploadFile({
            url: `${API_BASE_URL}${UPLOAD_API_PATH}`,
            filePath: file.path,
            name: 'file',
            formData: {
                title: file.name || '',
            },
            header: (0, parameter_1.paramsToHeaders)(params),
            success: (res) => {
                let body = {};
                try {
                    body = JSON.parse(res.data || '{}');
                }
                catch (e) {
                    body = {};
                }
                if (res.statusCode === 200 && body.code === 0) {
                    wx.showToast({ title: '上传成功', icon: 'success' });
                    this.loadDocs();
                    return;
                }
                wx.showToast({ title: body.msg || '上传失败', icon: 'none' });
            },
            fail: () => wx.showToast({ title: '上传失败', icon: 'none' }),
            complete: () => this.setData({ uploading: false }),
        });
    },
    onDeleteTap(e) {
        const id = Number(e.currentTarget.dataset.id || 0);
        if (!id)
            return;
        wx.showModal({
            title: '删除资料',
            content: '删除后该资料不再显示，已进入知识库的内容需要后台同步处理。',
            success: (res) => {
                if (!res.confirm)
                    return;
                this.deleteDoc(id);
            },
        });
    },
    deleteDoc(id) {
        const apiPath = `${DOCS_API_PATH}/${id}`;
        const headers = this.getAuthHeaders(apiPath);
        if (!headers)
            return;
        wx.request({
            url: `${API_BASE_URL}${apiPath}`,
            method: 'DELETE',
            header: headers,
            success: (res) => {
                const body = res.data || {};
                if (res.statusCode === 200 && body.code === 0) {
                    wx.showToast({ title: '已删除', icon: 'none' });
                    this.loadDocs();
                    return;
                }
                wx.showToast({ title: body.msg || '删除失败', icon: 'none' });
            },
            fail: () => wx.showToast({ title: '删除失败', icon: 'none' }),
        });
    },
});
