"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
const parameter_1 = require("../../utils/parameter");
const deviceFingerprint_1 = require("../../utils/deviceFingerprint");
const API_BASE_URL = 'https://api.jiadilingguang.com';
const BUILDING_CODE_CHAT_API_PATH = '/api/v1/miniprogram/ai/building-code/chat';
const quickPrompts = [
    { iconSrc: '/assets/icons/specadvisor/icon-staircase.svg', text: '楼梯最小宽度怎么定？' },
    { iconSrc: '/assets/icons/specadvisor/icon-fire-escape.svg', text: '消防疏散距离怎么查？' },
    { iconSrc: '/assets/icons/specadvisor/icon-building-height.svg', text: '住宅层高有什么要求？' },
    { iconSrc: '/assets/icons/specadvisor/icon-house-rules.svg', text: '自建房要注意哪些规范？' },
];
const WELCOME_MESSAGE = {
    id: 'welcome',
    role: 'ai',
    content: '您好，我是建规快查。\n可以帮你查询条文依据和审图要点。',
};
Page({
    data: {
        quickPrompts,
        inputText: '',
        loading: false,
        scrollIntoView: '',
        deviceId: '',
        chatStarted: false, // 用户是否已发起首条对话，true 时收起 hero 区域
        messages: [WELCOME_MESSAGE],
        quotesPanelVisible: false,
        quotesPanelData: [],
        networkOk: true,
    },
    async onLoad() {
        await this.initDeviceId();
        this.bindNetworkListener();
    },
    // 监听网络状态变化，断网时顶部显示提示
    bindNetworkListener() {
        wx.getNetworkType({
            success: (res) => {
                this.setData({ networkOk: res.networkType !== 'none' });
            },
        });
        this.networkChangeHandler = (res) => {
            this.setData({ networkOk: !!res.isConnected });
        };
        wx.onNetworkStatusChange(this.networkChangeHandler);
    },
    networkChangeHandler: null,
    // 当前正在进行的流式请求任务，用于离开页面时主动中止
    currentRequestTask: null,
    onUnload() {
        if (this.currentRequestTask) {
            try {
                this.currentRequestTask.abort();
            }
            catch (e) {
                // 忽略中止异常
            }
            this.currentRequestTask = null;
        }
        if (this.networkChangeHandler) {
            wx.offNetworkStatusChange(this.networkChangeHandler);
            this.networkChangeHandler = null;
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
                deviceId = '';
            }
        }
        this.setData({ deviceId: deviceId || '' });
    },
    onInput(e) {
        this.setData({
            inputText: e.detail.value,
        });
    },
    onPromptTap(e) {
        const text = String(e.currentTarget.dataset.text || '');
        if (!text) {
            return;
        }
        this.setData({ inputText: text });
        this.onSend();
    },
    onSend() {
        const text = String(this.data.inputText || '').trim();
        if (!text || this.data.loading) {
            return;
        }
        const token = wx.getStorageSync('token');
        if (!token) {
            wx.showToast({ title: '请先登录', icon: 'none' });
            return;
        }
        const userMessage = {
            id: `u-${Date.now()}`,
            role: 'user',
            content: text,
            createdAt: Date.now(),
        };
        const nextMessages = [...this.data.messages, userMessage];
        this.setData({
            inputText: '',
            loading: true,
            chatStarted: true,
            messages: nextMessages,
            scrollIntoView: `msg-${userMessage.id}`,
        });
        this.requestBuildingCodeAnswer(nextMessages);
    },
    // 新建对话：清空消息，恢复欢迎语和快捷提问
    onNewChat() {
        if (this.data.loading) {
            wx.showToast({ title: '正在回答中，稍后再试', icon: 'none' });
            return;
        }
        this.setData({
            messages: [WELCOME_MESSAGE],
            chatStarted: false,
            inputText: '',
            scrollIntoView: '',
        });
    },
    // 长按 AI 消息复制内容
    onCopyMessage(e) {
        const id = String(e.currentTarget.dataset.id || '');
        const msg = this.data.messages.find((m) => m.id === id);
        if (!msg || !msg.content)
            return;
        wx.setClipboardData({
            data: msg.content,
            success: () => wx.showToast({ title: '已复制', icon: 'none' }),
        });
    },
    // 错误重试：用最后一条用户消息重新发起请求
    onRetry(e) {
        if (this.data.loading)
            return;
        const id = String(e.currentTarget.dataset.id || '');
        const msg = this.data.messages.find((m) => m.id === id);
        const question = msg && msg.lastUserQuestion ? msg.lastUserQuestion : '';
        if (!question) {
            wx.showToast({ title: '无法重试', icon: 'none' });
            return;
        }
        // 移除失败的 AI 消息，保留用户消息，重新发起
        const messages = this.data.messages.filter((m) => m.id !== id);
        this.setData({ messages, loading: true });
        this.requestBuildingCodeAnswer(messages);
    },
    // 重新生成：找到这条 AI 消息前面的用户消息，截断上下文后重新请求
    onRegenerate(e) {
        if (this.data.loading)
            return;
        const id = String(e.currentTarget.dataset.id || '');
        const messages = this.data.messages;
        const idx = messages.findIndex((m) => m.id === id);
        if (idx < 1)
            return;
        // 保留 idx 之前的所有消息（含触发本次回答的用户消息），移除 AI 回答本身
        const truncated = messages.slice(0, idx);
        // 必须以用户消息结尾
        if (truncated[truncated.length - 1].role !== 'user')
            return;
        this.setData({ messages: truncated, loading: true });
        this.requestBuildingCodeAnswer(truncated);
    },
    requestBuildingCodeAnswer(nextMessages) {
        const token = wx.getStorageSync('token');
        if (!token) {
            this.setData({ loading: false });
            wx.showToast({ title: '请先登录', icon: 'none' });
            return;
        }
        const chatMessages = nextMessages
            .filter((item) => item.role === 'user' || item.role === 'ai')
            .slice(-10)
            .map((item) => ({
            role: item.role === 'ai' ? 'assistant' : 'user',
            content: item.content,
        }));
        const requestBody = {
            knowledge_scope: 'public',
            messages: chatMessages,
        };
        const params = (0, parameter_1.generateRequestParams)(token, requestBody, BUILDING_CODE_CHAT_API_PATH, this.data.deviceId);
        // 先插入一条空的 AI 消息占位，sourceText 默认空，只有收到 quotes 或 notice 才显示
        const aiMsgId = `a-${Date.now()}`;
        const aiMessage = {
            id: aiMsgId,
            role: 'ai',
            content: '',
            sourceText: '',
            createdAt: Date.now(),
        };
        this.setData({
            messages: [...this.data.messages, aiMessage],
            scrollIntoView: `msg-${aiMsgId}`,
        });
        // 流式状态
        let contentBuffer = '';
        let sourceText = '';
        let byteBuffer = new Uint8Array(0);
        let lastFlushAt = 0;
        let pendingFlush = null;
        let firstContent = true;
        // setData 节流（80ms 一次），避免长回答时频繁 setData 卡顿
        const flushView = () => {
            pendingFlush = null;
            lastFlushAt = Date.now();
            this.updateAIMessage(aiMsgId, contentBuffer, sourceText);
        };
        const scheduleFlush = () => {
            const now = Date.now();
            if (now - lastFlushAt >= 80) {
                flushView();
            }
            else if (!pendingFlush) {
                pendingFlush = setTimeout(flushView, 80);
            }
        };
        const cancelPendingFlush = () => {
            if (pendingFlush) {
                clearTimeout(pendingFlush);
                pendingFlush = null;
            }
        };
        const requestTask = wx.request({
            url: `${API_BASE_URL}${BUILDING_CODE_CHAT_API_PATH}`,
            method: 'POST',
            data: requestBody,
            enableChunked: true,
            header: {
                ...(0, parameter_1.paramsToHeaders)(params),
                'Content-Type': 'application/json',
                'Accept': 'text/event-stream',
            },
            success: () => {
                cancelPendingFlush();
                if (!contentBuffer) {
                    contentBuffer = '暂时没有检索到明确答案，请换一种问法再试。';
                }
                this.updateAIMessage(aiMsgId, contentBuffer, sourceText);
                this.setData({ loading: false });
                this.currentRequestTask = null;
            },
            fail: () => {
                cancelPendingFlush();
                this.markAsErrorMessage(aiMsgId, '建规快查暂时不可用', '请稍后重试', nextMessages);
                this.setData({ loading: false });
                this.currentRequestTask = null;
            },
        });
        this.currentRequestTask = requestTask;
        requestTask.onChunkReceived((res) => {
            // 累积字节，按 \n\n 边界切分，确保不会切断 UTF-8 字符
            const incoming = new Uint8Array(res.data);
            const merged = new Uint8Array(byteBuffer.length + incoming.length);
            merged.set(byteBuffer, 0);
            merged.set(incoming, byteBuffer.length);
            byteBuffer = merged;
            // 找最后一个 \n\n 边界（0x0A 0x0A）
            let lastBoundary = -1;
            for (let i = byteBuffer.length - 1; i >= 1; i--) {
                if (byteBuffer[i] === 0x0A && byteBuffer[i - 1] === 0x0A) {
                    lastBoundary = i;
                    break;
                }
            }
            if (lastBoundary < 0)
                return;
            const completeText = this.utf8BytesToString(byteBuffer.subarray(0, lastBoundary + 1));
            byteBuffer = byteBuffer.slice(lastBoundary + 1);
            const events = completeText.split('\n\n');
            for (const part of events) {
                const line = part.trim();
                if (!line.startsWith('data:'))
                    continue;
                const jsonStr = line.slice(5).trim();
                if (!jsonStr)
                    continue;
                try {
                    const evt = JSON.parse(jsonStr);
                    switch (evt.type) {
                        case 'content':
                            contentBuffer += evt.content || '';
                            // 第一个 token 到达时立即关闭 loading 隐藏思考圆点
                            if (firstContent) {
                                firstContent = false;
                                this.setData({ loading: false });
                            }
                            scheduleFlush();
                            break;
                        case 'quotes':
                            if (evt.quotes && evt.quotes.length > 0) {
                                const first = evt.quotes[0] || {};
                                const name = first.sourceName || first.source_name || '';
                                sourceText = name
                                    ? `来自《${name}》`
                                    : `来自建规快查知识库 · 引用 ${evt.quotes.length} 条`;
                                this.attachQuotes(aiMsgId, evt.quotes);
                                scheduleFlush();
                            }
                            break;
                        case 'notice':
                            // 兜底提示：本次回答非来自知识库
                            sourceText = evt.notice || '内容仅供参考，请以正式规范为准';
                            scheduleFlush();
                            break;
                        case 'error':
                            cancelPendingFlush();
                            this.markAsErrorMessage(aiMsgId, evt.error || '建规快查暂时不可用', '请稍后重试', nextMessages);
                            this.setData({ loading: false });
                            break;
                        case 'done':
                            cancelPendingFlush();
                            this.updateAIMessage(aiMsgId, contentBuffer, sourceText);
                            this.setData({ loading: false });
                            break;
                    }
                }
                catch (e) {
                    // 解析失败忽略，等下一条
                }
            }
        });
    },
    // 更新指定 AI 消息的内容
    updateAIMessage(msgId, content, sourceText) {
        const messages = this.data.messages;
        const idx = messages.findIndex((m) => m.id === msgId);
        if (idx < 0)
            return;
        const key = `messages[${idx}].content`;
        const sourceKey = `messages[${idx}].sourceText`;
        this.setData({
            [key]: content,
            [sourceKey]: sourceText,
            scrollIntoView: `msg-${msgId}`,
        });
    },
    // 标记 AI 消息为错误状态，并记录用于重试的最后一条用户问题
    markAsErrorMessage(msgId, content, sourceText, sentMessages) {
        const messages = this.data.messages;
        const idx = messages.findIndex((m) => m.id === msgId);
        if (idx < 0)
            return;
        let lastUserQuestion = '';
        for (let i = sentMessages.length - 1; i >= 0; i--) {
            if (sentMessages[i].role === 'user') {
                lastUserQuestion = sentMessages[i].content;
                break;
            }
        }
        this.setData({
            [`messages[${idx}].content`]: content,
            [`messages[${idx}].sourceText`]: sourceText,
            [`messages[${idx}].isError`]: true,
            [`messages[${idx}].lastUserQuestion`]: lastUserQuestion,
            scrollIntoView: `msg-${msgId}`,
        });
    },
    // 把引用列表挂到对应 AI 消息上，供点击 source-chip 时展开查看
    attachQuotes(msgId, quotes) {
        const idx = this.data.messages.findIndex((m) => m.id === msgId);
        if (idx < 0)
            return;
        this.setData({
            [`messages[${idx}].quotes`]: quotes,
        });
    },
    // 点击 source-chip 展开引用面板
    onSourceChipTap(e) {
        const id = String(e.currentTarget.dataset.id || '');
        const msg = this.data.messages.find((m) => m.id === id);
        if (!msg || !msg.quotes || msg.quotes.length === 0)
            return;
        this.setData({
            quotesPanelVisible: true,
            quotesPanelData: msg.quotes,
        });
    },
    onCloseQuotesPanel() {
        this.setData({ quotesPanelVisible: false });
    },
    // UTF-8 字节数组解码为字符串。由于 \n\n (0x0A 0x0A) 是 ASCII 边界，
    // 在 \n\n 处切分不会切断中文等多字节 UTF-8 字符，因此 escape+decodeURIComponent 安全。
    utf8BytesToString(bytes) {
        let binary = '';
        for (let i = 0; i < bytes.length; i++) {
            binary += String.fromCharCode(bytes[i]);
        }
        try {
            return decodeURIComponent(escape(binary));
        }
        catch (e) {
            return binary;
        }
    },
    showRequestFailed(message) {
        const aiMessage = {
            id: `a-${Date.now()}`,
            role: 'ai',
            content: message,
            sourceText: '请稍后重试',
        };
        this.setData({
            loading: false,
            messages: [...this.data.messages, aiMessage],
            scrollIntoView: `msg-${aiMessage.id}`,
        });
    },
    onAddTap() {
        wx.navigateTo({
            url: '/pages/buildingcodedocs/buildingcodedocs',
            fail: () => wx.showToast({ title: '页面跳转失败', icon: 'none' }),
        });
    },
    onShareAppMessage() {
        return {
            title: '建规快查',
            path: '/pages/specadvisor/specadvisor',
        };
    },
});
