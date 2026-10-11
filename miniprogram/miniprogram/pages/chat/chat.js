"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
// pages/chat/chat.ts
const parameter_1 = require("../../utils/parameter");
const deviceFingerprint_1 = require("../../utils/deviceFingerprint");
const aiError_1 = require("../../utils/aiError");
const promptReview_1 = require("../../utils/promptReview");
// API基础地址
const API_BASE_URL = 'https://api.jiadilingguang.com'; // 根据实际情况修改
const DEFAULT_CHAT_COST = 10;
function getTaskStatusPollDelay(attempt) {
    if (attempt < 2) {
        return 3000;
    }
    if (attempt < 6) {
        return 4000;
    }
    return 5000;
}
Page({
    /**
     * 页面的初始数据
     */
    data: {
        quickActions: [
            { icon: 'brain', text: '深度思考' },
            { icon: 'edit', text: '帮我写作' },
            { icon: 'phone-locked', text: '打电话' },
            { icon: 'sparkle', text: 'AI 创作' }
        ],
        isRecording: false,
        showModeModal: false, // 是否显示模式选择弹窗
        mode: '', // 'direct' 或 'chat'
        source: '', // 'photo' 或 'voice'
        messages: [],
        inputText: '',
        conversationTitle: '新对话',
        loading: false,
        /** 正在等待豆包首字：显示提示文案与加载动画 */
        waitingFirstChunk: false,
        streamingMessageId: '',
        chatCost: DEFAULT_CHAT_COST,
    },
    /**
     * 生命周期函数--监听页面加载
     */
    onLoad(options) {
        this.loadChatPricing();
        const source = String(options.source || '');
        const mode = String(options.mode || '');
        const title = String(options.title || '').trim();
        const preset = String(options.preset || '').trim();
        this.setData({
            source,
            conversationTitle: title || this.data.conversationTitle,
        });
        if (mode === 'chat') {
            this.setData({
                mode: 'chat',
                showModeModal: false,
            });
            if (!this.data.messages.length) {
                this.addMessage('ai', preset || '你好！我是AI设计客服，可以先帮你判断这个设计师适不适合你的需求，再帮你整理更容易沟通的咨询内容。');
            }
            return;
        }
        if (source) {
            this.setData({
                showModeModal: true,
            });
        }
    },
    /**
     * 生命周期函数--监听页面显示
     */
    onShow() {
        // 如果还没有选择模式且有来源，显示弹窗
        if (!this.data.mode && this.data.source) {
            this.setData({
                showModeModal: true,
            });
        }
    },
    async loadChatPricing() {
        try {
            const res = await new Promise((resolve, reject) => {
                wx.request({
                    url: `${API_BASE_URL}/api/v1/miniprogram/ai/pricing?scenes=ai_chat_single`,
                    method: 'GET',
                    success: (requestRes) => {
                        if (requestRes.statusCode === 200 && requestRes.data) {
                            const data = requestRes.data;
                            if (data.code === 0) {
                                resolve(data.data || {});
                            }
                            else {
                                reject(new Error(data.msg || '获取 AI 聊天计费失败'));
                            }
                        }
                        else {
                            reject(new Error(`请求失败: ${requestRes.statusCode}`));
                        }
                    },
                    fail: reject,
                });
            });
            const nextChatCost = Number(res?.prices?.ai_chat_single);
            this.setData({
                chatCost: Number.isFinite(nextChatCost) && nextChatCost > 0 ? nextChatCost : DEFAULT_CHAT_COST,
            });
        }
        catch (error) {
            this.setData({
                chatCost: DEFAULT_CHAT_COST,
            });
        }
    },
    /**
     * 关闭模式选择弹窗
     */
    closeModeModal() {
        this.setData({
            showModeModal: false,
        });
    },
    /**
     * 阻止事件冒泡
     */
    stopPropagation() {
        // 阻止点击事件冒泡
    },
    /**
     * 选择模式
     */
    selectMode(e) {
        const mode = e.currentTarget.dataset.mode;
        if (mode === 'direct') {
            const prompt = String(this.data.inputText || '').trim();
            if (prompt && (0, promptReview_1.blockUnsafeText)(prompt, { scene: 'prompt' })) {
                return;
            }
            this.setData({
                showModeModal: false,
            });
            const params = {};
            // 如果有来源（拍照或语音），也可以传递
            if (this.data.source) {
                params.source = this.data.source;
            }
            const queryString = Object.keys(params)
                .map(key => `${key}=${encodeURIComponent(params[key])}`)
                .join('&');
            const url = queryString
                ? `/pages/aigenerate/aigenerate?${queryString}`
                : '/pages/aigenerate/aigenerate';
            wx.navigateTo({
                url,
                success: (navRes) => {
                    if (prompt) {
                        navRes.eventChannel.emit('prefillGenerateData', { prompt, source: this.data.source });
                    }
                },
                fail: (err) => {
                    console.error('跳转失败:', err);
                    wx.showToast({
                        title: '页面跳转失败',
                        icon: 'none',
                    });
                }
            });
        }
        else if (mode === 'chat') {
            // 聊天模式
            this.setData({
                mode: mode,
                showModeModal: false,
            });
            // 发送欢迎消息
            this.addMessage('ai', '你好！我是AI设计助手，可以帮你完善设计需求。请告诉我你的想法，我会根据我们的对话生成优化的提示词。');
        }
        else if (mode === 'video') {
            const prompt = String(this.data.inputText || '').trim();
            if (prompt && (0, promptReview_1.blockUnsafeText)(prompt, { scene: 'prompt' })) {
                return;
            }
            this.setData({
                showModeModal: false,
            });
            const params = {};
            if (prompt) {
                params.prompt = encodeURIComponent(prompt);
            }
            const queryString = Object.keys(params)
                .map(key => `${key}=${params[key]}`)
                .join('&');
            const url = queryString
                ? `/pages/aivideo/aivideo?${queryString}`
                : '/pages/aivideo/aivideo';
            wx.navigateTo({
                url,
                fail: (err) => {
                    console.error('跳转失败:', err);
                    wx.showToast({ title: '页面跳转失败', icon: 'none' });
                },
            });
        }
        else if (mode === 'cost') {
            // 生成造价：跳转到AI造价页面
            this.setData({
                showModeModal: false,
            });
            wx.navigateTo({
                url: '/pages/aicost/aicost',
                fail: (err) => {
                    console.error('跳转失败:', err);
                    wx.showToast({
                        title: '页面跳转失败',
                        icon: 'none',
                    });
                }
            });
        }
    },
    /**
     * 添加消息
     */
    addMessage(role, content) {
        const message = {
            id: Date.now().toString(),
            role: role,
            content: content,
            time: this.formatTime(new Date()),
        };
        this.setData({
            messages: [...this.data.messages, message],
        });
        // 滚动到底部
        setTimeout(() => {
            this.scrollToBottom();
        }, 100);
    },
    /**
     * 格式化时间
     */
    formatTime(date) {
        const hours = date.getHours().toString().padStart(2, '0');
        const minutes = date.getMinutes().toString().padStart(2, '0');
        return `${hours}:${minutes}`;
    },
    /**
     * 滚动到底部
     */
    scrollToBottom() {
        // 小程序中可以使用wx.pageScrollTo
        wx.pageScrollTo({
            scrollTop: 9999,
            duration: 300,
        });
    },
    /**
     * 输入框内容变化
     */
    onInputChange(e) {
        this.setData({
            inputText: e.detail.value,
        });
    },
    /**
     * 发送消息
     */
    onSendMessage() {
        const text = this.data.inputText.trim();
        if (!text) {
            return;
        }
        // 如果没有选择模式，先显示模式选择弹窗
        if (!this.data.mode) {
            this.setData({
                showModeModal: true,
            });
            return;
        }
        if ((0, promptReview_1.blockUnsafeText)(text, { scene: 'chat' })) {
            return;
        }
        // 清空输入框
        this.setData({
            inputText: '',
            loading: true,
        });
        // 添加用户消息
        this.addMessage('user', text);
        if (this.data.mode === 'chat') {
            void this.sendChatMessage(text);
        }
        else {
            void this.generateDirectly(text);
        }
        this.setData({
            loading: false,
        });
    },
    /**
     * 发送聊天消息（流式）
     */
    async sendChatMessage(text) {
        if ((0, promptReview_1.blockUnsafeText)(text, { scene: 'chat' })) {
            return;
        }
        try {
            const token = wx.getStorageSync('token');
            if (!token) {
                wx.showToast({
                    title: '请先登录',
                    icon: 'none',
                });
                return;
            }
            // 构建聊天历史
            const history = this.data.messages
                .filter(msg => msg.role === 'user' || msg.role === 'ai')
                .map(msg => ({
                role: msg.role === 'user' ? 'user' : 'assistant',
                content: msg.content,
            }));
            // 添加当前消息
            const messages = [...history.slice(-10), { role: 'user', content: text }];
            // 构建请求体
            const requestBody = {
                messages: messages,
                system_prompt: '', // 使用后端默认的系统提示词
            };
            // 获取设备ID
            const deviceID = (0, deviceFingerprint_1.getCachedDeviceFingerprint)() || '';
            // 生成请求参数
            const apiPath = '/api/v1/miniprogram/ai/chat/stream';
            const params = (0, parameter_1.generateRequestParams)(token, JSON.stringify(requestBody), apiPath, deviceID);
            const headers = (0, parameter_1.paramsToHeaders)(params);
            // 先添加一个空的AI消息，用于流式更新
            const aiMessageId = Date.now().toString();
            const aiMessage = {
                id: aiMessageId,
                role: 'ai',
                content: '',
                time: this.formatTime(new Date()),
            };
            this.setData({
                messages: [...this.data.messages, aiMessage],
                waitingFirstChunk: true,
                streamingMessageId: aiMessageId,
            });
            // 使用requestTask进行流式请求
            let fullContent = '';
            const requestTask = wx.request({
                url: `${API_BASE_URL}${apiPath}`,
                method: 'POST',
                header: {
                    ...headers,
                    'Content-Type': 'application/json',
                    'Accept': 'text/event-stream',
                },
                data: requestBody,
                enableChunked: true, // 启用分块传输
                success: (res) => {
                    this.setData({ waitingFirstChunk: false, streamingMessageId: '' });
                    if (res.statusCode === 402 && res.data) {
                        const data = res.data;
                        const required = Number(data?.data?.required || this.data.chatCost || 0);
                        const current = Number(data?.data?.current || 0);
                        this.updateAIMessage(aiMessageId, `当前对话需要 ${required} 灵石，你的余额为 ${current} 灵石，请先充值后再试。`);
                        return;
                    }
                    // 非流式响应处理（fallback）
                    if (res.statusCode === 200 && res.data) {
                        if (typeof res.data === 'object' && res.data.code !== undefined) {
                            // JSON响应，可能是错误
                            if (res.data.code !== 0) {
                                this.updateAIMessage(aiMessageId, `抱歉，${res.data.msg || '请求失败'}`);
                            }
                        }
                    }
                    else if (res.statusCode !== 200) {
                        const data = res.data;
                        this.updateAIMessage(aiMessageId, `抱歉，${(data && data.msg) || `请求失败：${res.statusCode}`}`);
                    }
                },
                fail: (err) => {
                    this.setData({ waitingFirstChunk: false, streamingMessageId: '' });
                    console.error('流式请求失败:', err);
                    this.updateAIMessage(aiMessageId, `抱歉，网络错误：${err.errMsg || '请稍后重试'}`);
                },
            });
            // 监听分块数据
            requestTask.onChunkReceived((res) => {
                try {
                    // 将ArrayBuffer转换为字符串
                    const text = this.arrayBufferToString(res.data);
                    // 解析SSE数据
                    const lines = text.split('\n');
                    for (const line of lines) {
                        if (line.startsWith('data: ')) {
                            const jsonStr = line.substring(6).trim();
                            if (jsonStr && jsonStr !== '[DONE]') {
                                try {
                                    const data = JSON.parse(jsonStr);
                                    if (data.type === 'content' && data.content) {
                                        if (this.data.waitingFirstChunk) {
                                            this.setData({ waitingFirstChunk: false, streamingMessageId: '' });
                                        }
                                        fullContent += data.content;
                                        this.updateAIMessage(aiMessageId, fullContent);
                                    }
                                    else if (data.type === 'done') {
                                        this.setData({ waitingFirstChunk: false, streamingMessageId: '' });
                                        console.log('流式聊天完成');
                                    }
                                    else if (data.type === 'error') {
                                        this.setData({ waitingFirstChunk: false, streamingMessageId: '' });
                                        this.updateAIMessage(aiMessageId, `抱歉，${data.error || '发生错误'}`);
                                    }
                                }
                                catch (e) {
                                    // 忽略解析错误
                                }
                            }
                        }
                    }
                }
                catch (e) {
                    console.error('解析分块数据失败:', e);
                }
            });
        }
        catch (error) {
            console.error('发送消息失败:', error);
            this.addMessage('ai', `抱歉，发送失败：${error.message || '网络错误'}`);
        }
    },
    /**
     * ArrayBuffer转字符串
     */
    arrayBufferToString(buffer) {
        const uint8Array = new Uint8Array(buffer);
        let result = '';
        for (let i = 0; i < uint8Array.length; i++) {
            result += String.fromCharCode(uint8Array[i]);
        }
        // 处理UTF-8编码
        try {
            return decodeURIComponent(escape(result));
        }
        catch (e) {
            return result;
        }
    },
    /**
     * 更新AI消息内容
     */
    updateAIMessage(messageId, content) {
        const messages = this.data.messages.map((msg) => {
            if (msg.id === messageId) {
                return { ...msg, content };
            }
            return msg;
        });
        this.setData({ messages });
        // 滚动到底部
        setTimeout(() => {
            this.scrollToBottom();
        }, 50);
    },
    /**
     * 轮询任务状态
     */
    async pollTaskStatus(taskNo, taskType) {
        const maxAttempts = 18;
        let attempts = 0;
        const poll = async () => {
            if (attempts >= maxAttempts) {
                this.addMessage('ai', '任务处理超时，请稍后重试');
                return;
            }
            try {
                const token = wx.getStorageSync('token');
                if (!token) {
                    return;
                }
                const requestBody = {
                    task_no: taskNo,
                    task_type: taskType,
                };
                const deviceID = (0, deviceFingerprint_1.getCachedDeviceFingerprint)() || '';
                const apiPath = '/api/v1/miniprogram/ai/task/status';
                const params = (0, parameter_1.generateRequestParams)(token, requestBody, apiPath, deviceID);
                const headers = (0, parameter_1.paramsToHeaders)(params);
                const res = await new Promise((resolve, reject) => {
                    wx.request({
                        url: `${API_BASE_URL}${apiPath}`,
                        method: 'POST',
                        header: {
                            ...headers,
                            'Content-Type': 'application/json',
                        },
                        data: requestBody,
                        success: (res) => {
                            if (res.statusCode === 200 && res.data) {
                                const data = res.data;
                                if (data.code === 0) {
                                    resolve(data.data);
                                }
                                else {
                                    reject(new Error(data.msg || '查询失败'));
                                }
                            }
                            else {
                                reject(new Error(`请求失败: ${res.statusCode}`));
                            }
                        },
                        fail: (err) => {
                            reject(err);
                        },
                    });
                });
                if (res.status === 'success') {
                    // 任务成功，显示结果
                    // 移除等待消息
                    const messages = this.data.messages.filter((msg) => msg.content !== '正在思考中...');
                    this.setData({ messages });
                    if (res.result) {
                        // 根据任务类型显示不同的结果
                        if (taskType === 'ai_chat') {
                            // 聊天任务：显示消息内容
                            if (res.result.message) {
                                this.addMessage('ai', res.result.message);
                            }
                            else if (res.result.content) {
                                this.addMessage('ai', res.result.content);
                            }
                            else {
                                this.addMessage('ai', '对话完成');
                            }
                        }
                        else if (taskType === 'ai_draw') {
                            // 绘画任务：显示图片或跳转
                            if (res.result.image_url) {
                                this.addMessage('ai', `方案已生成！图片地址：${res.result.image_url}`);
                                // 可以在这里添加图片预览或跳转到结果页面
                            }
                            else {
                                this.addMessage('ai', '方案生成完成');
                            }
                        }
                    }
                    else {
                        this.addMessage('ai', '任务已完成');
                    }
                }
                else if (res.status === 'failed') {
                    // 任务失败，移除等待消息
                    const messages = this.data.messages.filter((msg) => msg.content !== '正在思考中...');
                    this.setData({ messages });
                    this.addMessage('ai', (0, aiError_1.sanitizeAIGenerationErrorMessage)(res.error_message));
                }
                else {
                    // 任务进行中，继续轮询
                    const nextDelay = getTaskStatusPollDelay(attempts);
                    attempts++;
                    setTimeout(poll, nextDelay);
                }
            }
            catch (error) {
                console.error('轮询任务状态失败:', error);
                const nextDelay = getTaskStatusPollDelay(attempts);
                attempts++;
                if (attempts < maxAttempts) {
                    setTimeout(poll, nextDelay);
                }
            }
        };
        poll();
    },
    /**
     * 直接生成方案
     */
    async generateDirectly(text) {
        void text;
        try {
            const token = wx.getStorageSync('token');
            if (!token) {
                wx.showToast({
                    title: '请先登录',
                    icon: 'none',
                });
                return;
            }
            // 调用AI绘画接口生成方案
            // 这里需要根据实际需求调用相应的生成接口
            wx.showToast({
                title: '正在生成方案...',
                icon: 'loading',
                duration: 2000,
            });
            // 模拟生成过程
            setTimeout(() => {
                wx.showToast({
                    title: '方案生成成功',
                    icon: 'success',
                });
                // 可以跳转到结果页面
            }, 2000);
        }
        catch (error) {
            console.error('生成方案失败:', error);
            wx.showToast({
                title: (0, aiError_1.sanitizeAIGenerationErrorMessage)(error.message || '生成失败'),
                icon: 'none',
                duration: 2000,
            });
        }
    },
    /**
     * 根据聊天内容生成提示词
     */
    async generatePromptFromChat() {
        if (this.data.messages.length === 0) {
            wx.showToast({
                title: '暂无对话内容',
                icon: 'none',
            });
            return;
        }
        wx.showLoading({
            title: '生成提示词中...',
        });
        try {
            const token = wx.getStorageSync('token');
            if (!token) {
                wx.hideLoading();
                wx.showToast({
                    title: '请先登录',
                    icon: 'none',
                });
                return;
            }
            // 提取对话内容
            const conversationText = this.data.messages
                .map(msg => `${msg.role === 'user' ? '用户' : 'AI'}: ${msg.content}`)
                .join('\n');
            // 调用后端接口生成提示词
            const deviceID = (0, deviceFingerprint_1.getCachedDeviceFingerprint)() || '';
            const requestBody = {
                conversation: conversationText,
                messages: this.data.messages.map(msg => ({
                    role: msg.role === 'user' ? 'user' : 'assistant',
                    content: msg.content,
                })),
            };
            const apiPath = '/api/v1/miniprogram/ai/generate-prompt';
            const params = (0, parameter_1.generateRequestParams)(token, requestBody, apiPath, deviceID);
            const headers = (0, parameter_1.paramsToHeaders)(params);
            const res = await new Promise((resolve, reject) => {
                wx.request({
                    url: `${API_BASE_URL}${apiPath}`,
                    method: 'POST',
                    header: {
                        ...headers,
                        'Content-Type': 'application/json',
                    },
                    data: requestBody,
                    success: (res) => {
                        if (res.statusCode === 200 && res.data) {
                            const data = res.data;
                            if (data.code === 0) {
                                resolve(data.data);
                            }
                            else {
                                reject(new Error(data.msg || '生成提示词失败'));
                            }
                        }
                        else {
                            reject(new Error(`请求失败: ${res.statusCode}`));
                        }
                    },
                    fail: (err) => {
                        reject(err);
                    },
                });
            });
            wx.hideLoading();
            const prompt = res.prompt || this.extractPromptFromConversation(conversationText);
            // 显示生成的提示词
            wx.showModal({
                title: '生成的提示词',
                content: prompt,
                confirmText: '使用此提示词生成',
                cancelText: '取消',
                editable: true,
                placeholderText: '可以编辑提示词',
                success: (modalRes) => {
                    if (modalRes.confirm) {
                        // 使用生成的提示词（或编辑后的）直接生成方案
                        const finalPrompt = modalRes.content || prompt;
                        this.generateWithPrompt(finalPrompt);
                    }
                },
            });
        }
        catch (error) {
            wx.hideLoading();
            console.error('生成提示词失败:', error);
            // 如果API不存在，使用前端提取
            const conversationText = this.data.messages
                .map(msg => `${msg.role === 'user' ? '用户' : 'AI'}: ${msg.content}`)
                .join('\n');
            const prompt = this.extractPromptFromConversation(conversationText);
            wx.showModal({
                title: '生成的提示词',
                content: prompt,
                confirmText: '使用此提示词生成',
                cancelText: '取消',
                editable: true,
                placeholderText: '可以编辑提示词',
                success: (modalRes) => {
                    if (modalRes.confirm) {
                        const finalPrompt = modalRes.content || prompt;
                        this.generateWithPrompt(finalPrompt);
                    }
                },
            });
        }
    },
    /**
     * 从对话中提取提示词
     */
    extractPromptFromConversation(conversation) {
        // 简单的提示词提取逻辑
        // 实际应该调用AI接口进行智能提取
        const lines = conversation.split('\n');
        const userMessages = lines
            .filter(line => line.startsWith('用户:'))
            .map(line => line.replace('用户:', '').trim())
            .join('，');
        // 构建提示词
        let prompt = `根据以下需求生成设计方案：${userMessages}`;
        // 可以添加更多智能提取逻辑
        if (userMessages.includes('面积') || userMessages.includes('平米') || userMessages.includes('m²')) {
            prompt += '，包含面积信息';
        }
        if (userMessages.includes('风格') || userMessages.includes('样式')) {
            prompt += '，包含风格要求';
        }
        return prompt;
    },
    /**
     * 使用提示词生成方案
     */
    async generateWithPrompt(prompt) {
        if ((0, promptReview_1.blockUnsafeText)(prompt.trim(), { scene: 'prompt' })) {
            return;
        }
        try {
            const token = wx.getStorageSync('token');
            if (!token) {
                wx.showToast({
                    title: '请先登录',
                    icon: 'none',
                });
                return;
            }
            wx.showLoading({
                title: '正在生成方案...',
            });
            // 构建请求体
            const requestBody = {
                scene: 'ai_draw_single',
                payload: {
                    prompt: prompt,
                },
            };
            // 获取设备ID
            const deviceID = (0, deviceFingerprint_1.getCachedDeviceFingerprint)() || '';
            // 生成请求参数
            const apiPath = '/api/v1/miniprogram/ai/draw';
            const params = (0, parameter_1.generateRequestParams)(token, requestBody, apiPath, deviceID);
            const headers = (0, parameter_1.paramsToHeaders)(params);
            // 调用AI绘画接口
            const res = await new Promise((resolve, reject) => {
                wx.request({
                    url: `${API_BASE_URL}${apiPath}`,
                    method: 'POST',
                    header: {
                        ...headers,
                        'Content-Type': 'application/json',
                    },
                    data: requestBody,
                    success: (res) => {
                        if (res.statusCode === 200 && res.data) {
                            const data = res.data;
                            if (data.code === 0) {
                                resolve(data.data);
                            }
                            else {
                                reject(new Error(data.msg || '生成失败'));
                            }
                        }
                        else {
                            reject(new Error(`请求失败: ${res.statusCode}`));
                        }
                    },
                    fail: (err) => {
                        reject(err);
                    },
                });
            });
            wx.hideLoading();
            // 获取任务编号，开始轮询任务状态
            if (res.task_no) {
                wx.showToast({
                    title: '任务已提交，正在生成...',
                    icon: 'loading',
                    duration: 2000,
                });
                this.pollTaskStatus(res.task_no, 'ai_draw');
            }
            else {
                wx.showToast({
                    title: '方案生成成功',
                    icon: 'success',
                });
            }
        }
        catch (error) {
            wx.hideLoading();
            console.error('生成方案失败:', error);
            wx.showToast({
                title: (0, aiError_1.sanitizeAIGenerationErrorMessage)(error.message || '生成失败'),
                icon: 'none',
                duration: 2000,
            });
        }
    },
    /**
     * 点击标题
     */
    onTitleTap() {
        wx.showToast({
            title: '查看对话详情',
            icon: 'none'
        });
    },
    /**
     * 快速操作
     */
    onQuickAction(e) {
        const index = e.currentTarget.dataset.index;
        const action = this.data.quickActions[index];
        console.log('快速操作', action);
        // 如果是"打电话"，跳转到语音通话页面
        if (action.text === '打电话') {
            wx.navigateTo({
                url: '/pages/voicecall/voicecall',
                fail: () => {
                    wx.showToast({
                        title: '页面跳转失败',
                        icon: 'none'
                    });
                }
            });
        }
        else {
            wx.showToast({
                title: action.text,
                icon: 'none'
            });
        }
    },
    /**
     * 添加
     */
    onAdd() {
        wx.showActionSheet({
            itemList: ['图片', '文件', '位置'],
            success: (res) => {
                console.log('选择', res.tapIndex);
            }
        });
    },
    onShareAppMessage() {
        return {
            title: '甲第灵光 · AI聊天助手',
            path: '/pages/chat/chat',
        };
    },
});
