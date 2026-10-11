"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
// pages/Parentchildcreativity/Parentchildcreativity.ts
const parameter_1 = require("../../utils/parameter");
const deviceFingerprint_1 = require("../../utils/deviceFingerprint");
const aiError_1 = require("../../utils/aiError");
// API基础地址
const API_BASE_URL = 'https://api.jiadilingguang.com';
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
        selectedPrototype: 'landmark',
        selectedStyle: 'cute',
        prototypes: [
            { label: '城市地标', value: 'landmark' },
            { label: '普通住宅', value: 'residence' },
            { label: '叁种建筑', value: 'building' }
        ],
        styles: [
            { label: '可爱', value: 'cute' },
            { label: '酷炫', value: 'cool' },
            { label: '复古', value: 'retro' },
            { label: '科幻', value: 'scifi' }
        ],
        selectedType: 'doll', // doll, poster, cultural
        designTypes: [
            { label: '玩偶设计', value: 'doll', icon: 'heart' },
            { label: '海报设计', value: 'poster', icon: 'image' },
            { label: '文创设计', value: 'cultural', icon: 'gift' }
        ],
        // 玩偶设计相关
        dollTheme: '',
        dollRequirements: '',
        dollReferenceImages: [],
        // 海报设计相关
        posterTheme: '',
        posterText: '',
        posterReferenceImages: [],
        // 文创设计相关
        culturalProductType: '',
        culturalRequirements: '',
        culturalReferenceImages: [],
        // 生成结果
        generatedImages: [],
        generating: false,
        canGenerate: false
    },
    /**
     * 生命周期函数--监听页面加载
     */
    onLoad() {
    },
    /**
     * 生命周期函数--监听页面初次渲染完成
     */
    onReady() {
    },
    /**
     * 生命周期函数--监听页面显示
     */
    onShow() {
    },
    /**
     * 生命周期函数--监听页面隐藏
     */
    onHide() {
    },
    /**
     * 生命周期函数--监听页面卸载
     */
    onUnload() {
    },
    /**
     * 页面相关事件处理函数--监听用户下拉动作
     */
    onPullDownRefresh() {
    },
    /**
     * 页面上拉触底事件的处理函数
     */
    onReachBottom() {
    },
    /**
     * 用户点击右上角分享
     */
    onShareAppMessage() {
        return {
            title: '亲子创意工坊',
            path: '/pages/Parentchildcreativity/Parentchildcreativity'
        };
    },
    /**
     * 每日签到
     */
    onDailyCheckin() {
        wx.navigateTo({
            url: '/pages/checkin/checkin?source=index'
        });
    },
    /**
     * 选择原型
     */
    onSelectPrototype(e) {
        const value = e.currentTarget.dataset.value;
        this.setData({
            selectedPrototype: value
        });
    },
    /**
     * 选择风格
     */
    onSelectStyle(e) {
        const value = e.currentTarget.dataset.value;
        this.setData({
            selectedStyle: value
        });
    },
    /**
     * 选择生成类型
     */
    onSelectType(e) {
        const value = e.currentTarget.dataset.value;
        this.setData({
            selectedType: value
        });
        this.checkCanGenerate();
    },
    // ========== 玩偶设计相关 ==========
    /**
     * 玩偶主题输入
     */
    onDollThemeInput(e) {
        this.setData({
            dollTheme: e.detail.value
        });
        this.checkCanGenerate();
    },
    /**
     * 玩偶要求输入
     */
    onDollRequirementsInput(e) {
        this.setData({
            dollRequirements: e.detail.value
        });
        this.checkCanGenerate();
    },
    /**
     * 上传玩偶参考图
     */
    onUploadDollReference() {
        wx.chooseMedia({
            count: 9,
            mediaType: ['image'],
            sourceType: ['camera', 'album'],
            success: (res) => {
                const tempFiles = res.tempFiles.map(file => file.tempFilePath);
                const currentImages = this.data.dollReferenceImages || [];
                const newImages = [...currentImages, ...tempFiles].slice(0, 9);
                this.setData({
                    dollReferenceImages: newImages
                });
            },
            fail: (err) => {
                console.error('选择图片失败', err);
                wx.showToast({
                    title: '选择图片失败',
                    icon: 'none'
                });
            }
        });
    },
    /**
     * 删除玩偶参考图
     */
    onDeleteDollReferenceImage(e) {
        const index = e.currentTarget.dataset.index;
        const images = this.data.dollReferenceImages;
        images.splice(index, 1);
        this.setData({
            dollReferenceImages: images
        });
    },
    // ========== 海报设计相关 ==========
    /**
     * 海报主题输入
     */
    onPosterThemeInput(e) {
        this.setData({
            posterTheme: e.detail.value
        });
        this.checkCanGenerate();
    },
    /**
     * 海报文字输入
     */
    onPosterTextInput(e) {
        this.setData({
            posterText: e.detail.value
        });
        this.checkCanGenerate();
    },
    /**
     * 上传海报参考图
     */
    onUploadPosterReference() {
        wx.chooseMedia({
            count: 9,
            mediaType: ['image'],
            sourceType: ['camera', 'album'],
            success: (res) => {
                const tempFiles = res.tempFiles.map(file => file.tempFilePath);
                const currentImages = this.data.posterReferenceImages || [];
                const newImages = [...currentImages, ...tempFiles].slice(0, 9);
                this.setData({
                    posterReferenceImages: newImages
                });
            },
            fail: (err) => {
                console.error('选择图片失败', err);
                wx.showToast({
                    title: '选择图片失败',
                    icon: 'none'
                });
            }
        });
    },
    /**
     * 删除海报参考图
     */
    onDeletePosterReferenceImage(e) {
        const index = e.currentTarget.dataset.index;
        const images = this.data.posterReferenceImages;
        images.splice(index, 1);
        this.setData({
            posterReferenceImages: images
        });
    },
    // ========== 文创设计相关 ==========
    /**
     * 文创产品类型输入
     */
    onCulturalProductTypeInput(e) {
        this.setData({
            culturalProductType: e.detail.value
        });
        this.checkCanGenerate();
    },
    /**
     * 文创要求输入
     */
    onCulturalRequirementsInput(e) {
        this.setData({
            culturalRequirements: e.detail.value
        });
        this.checkCanGenerate();
    },
    /**
     * 上传文创参考图
     */
    onUploadCulturalReference() {
        wx.chooseMedia({
            count: 9,
            mediaType: ['image'],
            sourceType: ['camera', 'album'],
            success: (res) => {
                const tempFiles = res.tempFiles.map(file => file.tempFilePath);
                const currentImages = this.data.culturalReferenceImages || [];
                const newImages = [...currentImages, ...tempFiles].slice(0, 9);
                this.setData({
                    culturalReferenceImages: newImages
                });
            },
            fail: (err) => {
                console.error('选择图片失败', err);
                wx.showToast({
                    title: '选择图片失败',
                    icon: 'none'
                });
            }
        });
    },
    /**
     * 删除文创参考图
     */
    onDeleteCulturalReferenceImage(e) {
        const index = e.currentTarget.dataset.index;
        const images = this.data.culturalReferenceImages;
        images.splice(index, 1);
        this.setData({
            culturalReferenceImages: images
        });
    },
    /**
     * 检查是否可以生成
     */
    checkCanGenerate() {
        const selectedType = this.data.selectedType;
        let canGenerate = false;
        if (selectedType === 'doll') {
            // 玩偶设计：至少填写主题或要求
            canGenerate = !!(this.data.dollTheme || this.data.dollRequirements);
        }
        else if (selectedType === 'poster') {
            // 海报设计：至少填写主题或文字
            canGenerate = !!(this.data.posterTheme || this.data.posterText);
        }
        else if (selectedType === 'cultural') {
            // 文创设计：至少填写产品类型或要求
            canGenerate = !!(this.data.culturalProductType || this.data.culturalRequirements);
        }
        this.setData({
            canGenerate: canGenerate
        });
    },
    /**
     * AI生成创意设计
     */
    async onGenerate() {
        if (!this.data.canGenerate) {
            wx.showToast({
                title: '请填写必要信息',
                icon: 'none'
            });
            return;
        }
        const token = wx.getStorageSync('token');
        if (!token) {
            wx.showToast({
                title: '请先登录',
                icon: 'none'
            });
            return;
        }
        this.setData({ generating: true });
        wx.showLoading({
            title: '正在生成...',
        });
        try {
            const selectedType = this.data.selectedType;
            let requestData = {
                design_type: selectedType,
                prototype: this.data.selectedPrototype,
                style: this.data.selectedStyle
            };
            if (selectedType === 'doll') {
                // 玩偶设计
                const referenceUrls = [];
                for (const imagePath of this.data.dollReferenceImages) {
                    if (imagePath.startsWith('http://') || imagePath.startsWith('https://')) {
                        referenceUrls.push(imagePath);
                    }
                    else {
                        const url = await this.uploadImageToOSS(imagePath, token);
                        if (url) {
                            referenceUrls.push(url);
                        }
                    }
                }
                requestData = {
                    ...requestData,
                    theme: this.data.dollTheme,
                    requirements: this.data.dollRequirements,
                    reference_images: referenceUrls
                };
            }
            else if (selectedType === 'poster') {
                // 海报设计
                const referenceUrls = [];
                for (const imagePath of this.data.posterReferenceImages) {
                    if (imagePath.startsWith('http://') || imagePath.startsWith('https://')) {
                        referenceUrls.push(imagePath);
                    }
                    else {
                        const url = await this.uploadImageToOSS(imagePath, token);
                        if (url) {
                            referenceUrls.push(url);
                        }
                    }
                }
                requestData = {
                    ...requestData,
                    theme: this.data.posterTheme,
                    text: this.data.posterText,
                    reference_images: referenceUrls
                };
            }
            else if (selectedType === 'cultural') {
                // 文创设计
                const referenceUrls = [];
                for (const imagePath of this.data.culturalReferenceImages) {
                    if (imagePath.startsWith('http://') || imagePath.startsWith('https://')) {
                        referenceUrls.push(imagePath);
                    }
                    else {
                        const url = await this.uploadImageToOSS(imagePath, token);
                        if (url) {
                            referenceUrls.push(url);
                        }
                    }
                }
                requestData = {
                    ...requestData,
                    product_type: this.data.culturalProductType,
                    requirements: this.data.culturalRequirements,
                    reference_images: referenceUrls
                };
            }
            // 调用后端API
            const apiPath = '/api/v1/miniprogram/ai/parent-child-design';
            // 获取设备ID
            const deviceID = (0, deviceFingerprint_1.getCachedDeviceFingerprint)() || '';
            // 生成请求参数
            const params = (0, parameter_1.generateRequestParams)(token, requestData, apiPath, deviceID);
            const headers = (0, parameter_1.paramsToHeaders)(params);
            const res = await new Promise((resolve, reject) => {
                wx.request({
                    url: `${API_BASE_URL}${apiPath}`,
                    method: 'POST',
                    header: {
                        ...headers,
                        'Content-Type': 'application/json',
                    },
                    data: requestData,
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
            // 保存任务编号，用于后续轮询
            const taskNo = res.task_no;
            if (taskNo) {
                // 开始轮询任务状态
                this.pollTaskStatus(taskNo);
            }
            else {
                this.setData({ generating: false });
                wx.showToast({
                    title: '任务提交失败',
                    icon: 'none'
                });
            }
        }
        catch (error) {
            wx.hideLoading();
            this.setData({ generating: false });
            console.error('生成失败:', error);
            wx.showToast({
                title: (0, aiError_1.sanitizeAIGenerationErrorMessage)(error.message || '生成失败'),
                icon: 'none',
                duration: 2000
            });
        }
    },
    /**
     * 轮询任务状态
     */
    async pollTaskStatus(taskNo) {
        const token = wx.getStorageSync('token');
        let pollCount = 0;
        const maxPolls = 30;
        const poll = async () => {
            if (pollCount >= maxPolls) {
                this.setData({ generating: false });
                wx.showToast({
                    title: '生成超时，请稍后查看',
                    icon: 'none'
                });
                return;
            }
            try {
                const requestData = {
                    task_no: taskNo,
                    task_type: 'ai_draw'
                };
                // 获取设备ID
                const deviceID = (0, deviceFingerprint_1.getCachedDeviceFingerprint)() || '';
                // 生成请求参数（文件上传时使用空对象作为body）
                const apiPath = '/api/v1/miniprogram/ai/task/status';
                const params = (0, parameter_1.generateRequestParams)(token, requestData, apiPath, deviceID);
                const headers = (0, parameter_1.paramsToHeaders)(params);
                const res = await new Promise((resolve, reject) => {
                    wx.request({
                        url: `${API_BASE_URL}${apiPath}`,
                        method: 'POST',
                        header: {
                            'token': headers.token,
                            'token-signature': headers['token-signature'],
                            'sin': headers.sin,
                            'md5-signature': headers['md5-signature'],
                            'pass': headers.pass,
                            'tm': headers.tm,
                            'Content-Type': 'application/json',
                        },
                        data: requestData,
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
                if (res.status === 'success' && res.result) {
                    // 任务完成
                    this.setData({ generating: false });
                    wx.hideLoading();
                    // 解析结果
                    let resultUrl = '';
                    if (typeof res.result === 'string') {
                        try {
                            const resultData = JSON.parse(res.result);
                            resultUrl = resultData.url || resultData.url_raw || '';
                        }
                        catch (e) {
                            resultUrl = res.result;
                        }
                    }
                    else if (res.result.url) {
                        resultUrl = res.result.url;
                    }
                    else if (res.result.url_raw) {
                        resultUrl = res.result.url_raw;
                    }
                    if (resultUrl) {
                        // 添加到生成结果列表
                        const generatedImages = this.data.generatedImages || [];
                        generatedImages.push(resultUrl);
                        this.setData({
                            generatedImages: generatedImages
                        });
                        wx.showToast({
                            title: '生成成功',
                            icon: 'success'
                        });
                    }
                    else {
                        wx.showToast({
                            title: (0, aiError_1.sanitizeAIGenerationErrorMessage)('生成完成，但未获取到结果'),
                            icon: 'none'
                        });
                    }
                }
                else if (res.status === 'failed') {
                    // 任务失败
                    this.setData({ generating: false });
                    wx.hideLoading();
                    wx.showToast({
                        title: (0, aiError_1.sanitizeAIGenerationErrorMessage)(res.error_message || res.error || '生成失败'),
                        icon: 'none'
                    });
                }
                else {
                    // 继续轮询
                    const nextDelay = getTaskStatusPollDelay(pollCount);
                    pollCount++;
                    setTimeout(poll, nextDelay);
                }
            }
            catch (error) {
                // 继续轮询
                const nextDelay = getTaskStatusPollDelay(pollCount);
                pollCount++;
                setTimeout(poll, nextDelay);
            }
        };
        poll();
    },
    /**
     * 上传图片到OSS
     */
    async uploadImageToOSS(imagePath, token) {
        try {
            const apiPath = '/api/v1/miniprogram/ai/upload-reference-image';
            // 获取设备ID
            const deviceID = (0, deviceFingerprint_1.getCachedDeviceFingerprint)() || '';
            // 生成请求参数（文件上传时使用空对象作为body）
            const params = (0, parameter_1.generateRequestParams)(token, '{}', apiPath, deviceID);
            const headers = (0, parameter_1.paramsToHeaders)(params);
            return new Promise((resolve, reject) => {
                wx.uploadFile({
                    url: `${API_BASE_URL}${apiPath}`,
                    filePath: imagePath,
                    name: 'file',
                    header: headers,
                    success: (res) => {
                        try {
                            const data = JSON.parse(res.data);
                            if (data.code === 0 && data.data && data.data.url) {
                                resolve(data.data.url);
                            }
                            else {
                                reject(new Error(data.msg || '上传失败'));
                            }
                        }
                        catch (e) {
                            reject(new Error('解析响应失败'));
                        }
                    },
                    fail: (err) => {
                        reject(err);
                    },
                });
            });
        }
        catch (error) {
            console.error('上传图片失败:', error);
            return null;
        }
    },
    /**
     * 保存图片
     */
    onSaveImage(e) {
        const index = e.currentTarget.dataset.index;
        const imageUrl = this.data.generatedImages[index];
        if (!imageUrl)
            return;
        wx.showModal({
            title: '下载保存需验证',
            content: '生成结果默认仅支持查看，下载保存需先添加企业微信并留下电话号码。',
            showCancel: false
        });
    },
    /**
     * 分享图片
     */
    onShareImage() {
        wx.showToast({
            title: '分享功能待实现',
            icon: 'none'
        });
    },
    /**
     * 晒作品
     */
    onShowcase() {
        wx.showToast({
            title: '晒作品',
            icon: 'none'
        });
    },
    /**
     * 发布到模板广场
     */
    onPublish() {
        wx.switchTab({
            url: '/pages/template/template',
            fail: () => {
                wx.showToast({
                    title: '页面跳转失败',
                    icon: 'none'
                });
            }
        });
    },
    /**
     * tabbar 切换
     */
    onTabSwitch(e) {
        console.log('切换 tab', e.detail);
    }
});
