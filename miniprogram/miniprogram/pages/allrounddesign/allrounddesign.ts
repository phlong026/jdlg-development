// pages/allrounddesign/allrounddesign.ts
import { generateRequestParams, paramsToHeaders } from '../../utils/parameter';
import { getCachedDeviceFingerprint } from '../../utils/deviceFingerprint';
import { sanitizeAIGenerationErrorMessage } from '../../utils/aiError';
import { blockUnsafeText } from '../../utils/promptReview';

const API_BASE_URL = 'https://api.jiadilingguang.com';
const DEFAULT_ALLROUND_COST = 20;
const DEFAULT_PARENT_COST = 20;
const MAX_REFERENCE_IMAGE_COUNT = 6;

type ReferenceImageField = 'posterReferenceImages' | 'culturalReferenceImages' | 'parentReferenceImages';

function normalizeImages(values: any[] = [], maxCount = MAX_REFERENCE_IMAGE_COUNT): string[] {
  return values
    .map((item) => (typeof item === 'string' ? item.trim() : ''))
    .filter((item, index, array) => !!item && array.indexOf(item) === index)
    .slice(0, maxCount);
}

function isRemoteImage(value: string): boolean {
  return value.startsWith('http://') || value.startsWith('https://');
}

Page({
  data: {
    activeTab: 'style',
    tabs: [
      { label: '风格变换', value: 'style' },
      { label: '海报设计', value: 'poster' },
      { label: '文创设计', value: 'cultural' },
      { label: '亲子工坊', value: 'parentWorkshop' },
    ],

    styleOriginalImage: '',
    targetStyle: 'modern',
    styleOptions: [
      { label: '现代简约', value: 'modern', subtitle: '利落克制' },
      { label: '新中式', value: 'chinese', subtitle: '东方雅致' },
      { label: '新闽派', value: 'min', subtitle: '地域表达' },
      { label: '传统古建', value: 'traditional', subtitle: '古建韵味' },
      { label: '海派', value: 'shanghai', subtitle: '都市融合' },
      { label: '田园风', value: 'countryside', subtitle: '自然松弛' },
      { label: '卡通风格', value: 'cartoon', subtitle: '轻松趣味' },
      { label: '水彩风格', value: 'watercolor', subtitle: '柔和手绘' },
      { label: '油画风格', value: 'oil', subtitle: '厚重艺术' },
      { label: '素描风格', value: 'sketch', subtitle: '线稿表达' },
    ],

    posterTheme: '',
    posterText: '',
    posterStyle: 'business',
    posterStyleOptions: [
      { label: '简约商务', value: 'business', subtitle: '清晰正式' },
      { label: '时尚潮流', value: 'fashion', subtitle: '年轻吸睛' },
      { label: '文艺清新', value: 'literary', subtitle: '轻柔耐看' },
      { label: '科技感', value: 'tech', subtitle: '理性未来' },
      { label: '复古风格', value: 'vintage', subtitle: '经典质感' },
      { label: '插画风格', value: 'illustration', subtitle: '图形表达' },
      { label: '手绘风格', value: 'handdrawn', subtitle: '自然亲和' },
      { label: '极简风格', value: 'minimalist', subtitle: '克制留白' },
    ],
    posterReferenceImages: [] as string[],

    productType: '',
    productRequirements: '',
    culturalStyle: 'guochao',
    culturalStyleOptions: [
      { label: '国潮风格', value: 'guochao', subtitle: '东方新潮' },
      { label: '简约现代', value: 'modern', subtitle: '日常好用' },
      { label: '传统元素', value: 'traditional', subtitle: '文化提取' },
      { label: '创意插画', value: 'illustration', subtitle: '图案鲜明' },
      { label: '几何图案', value: 'geometric', subtitle: '秩序构成' },
      { label: '文字设计', value: 'typography', subtitle: '字形为主' },
      { label: '抽象艺术', value: 'abstract', subtitle: '艺术表达' },
      { label: '自然元素', value: 'nature', subtitle: '植物自然' },
    ],
    culturalReferenceImages: [] as string[],

    parentDesignType: 'doll',
    parentDesignTypeLabel: '玩偶手作',
    parentDesignTypes: [
      { label: '玩偶手作', value: 'doll', subtitle: '形象共创' },
      { label: '亲子海报', value: 'poster', subtitle: '活动纪念' },
      { label: '文创周边', value: 'cultural', subtitle: '贴纸徽章' },
    ],
    parentPrototype: 'landmark',
    parentPrototypeLabel: '城市地标',
    parentPrototypes: [
      { label: '城市地标', value: 'landmark', subtitle: '适合亲子共创' },
      { label: '普通住宅', value: 'residence', subtitle: '日常空间' },
      { label: '建筑造型', value: 'building', subtitle: '建筑启发' },
    ],
    parentStyle: 'cute',
    parentStyleLabel: '可爱',
    parentStyleOptions: [
      { label: '可爱', value: 'cute', subtitle: '温馨亲子' },
      { label: '酷炫', value: 'cool', subtitle: '强视觉' },
      { label: '复古', value: 'retro', subtitle: '怀旧质感' },
      { label: '科幻', value: 'scifi', subtitle: '未来想象' },
    ],
    parentTheme: '',
    parentRequirements: '',
    parentReferenceImages: [] as string[],

    selectedQuality: 'uhd',
    selectedCanvas: '1:1',
    selectedGenerateCount: 1,
    qualityOptions: [
      { label: '高清', value: 'hd' },
      { label: '超清', value: 'uhd' },
    ],
    canvasOptions: [
      { label: '横图', value: '16:9', size: '16:9' },
      { label: '方图', value: '1:1', size: '1:1' },
      { label: '竖图', value: '9:16', size: '9:16' },
    ],
    generateCountOptions: [1, 2, 3],
    promptText: '',
    optimizingPrompt: false,

    allroundCost: DEFAULT_ALLROUND_COST,
    parentCost: DEFAULT_PARENT_COST,
    currentUnitCost: DEFAULT_ALLROUND_COST,
    currentCost: DEFAULT_ALLROUND_COST,
    pricingLoaded: false,
    generating: false,
  },

  onLoad() {
    this.loadAIPricing();
    this.syncCurrentCost();
  },

  onTabSwitch(e: any) {
    const value = String(e.currentTarget.dataset.value || 'style');
    this.setData({ activeTab: value }, () => this.syncCurrentCost());
  },

  getCurrentGenerateCount(): number {
    const value = Number(this.data.selectedGenerateCount || 1);
    if (!Number.isFinite(value) || value <= 0) {
      return 1;
    }
    return Math.min(3, Math.max(1, Math.floor(value)));
  },

  getCurrentUnitCost(): number {
    return this.data.activeTab === 'parentWorkshop'
      ? Number(this.data.parentCost || DEFAULT_PARENT_COST)
      : Number(this.data.allroundCost || DEFAULT_ALLROUND_COST);
  },

  syncCurrentCost() {
    const selectedGenerateCount = this.getCurrentGenerateCount();
    const currentUnitCost = this.getCurrentUnitCost();
    this.setData({
      selectedGenerateCount,
      currentUnitCost,
      currentCost: currentUnitCost * selectedGenerateCount,
    });
  },

  async loadAIPricing() {
    try {
      const res = await new Promise<any>((resolve, reject) => {
        wx.request({
          url: `${API_BASE_URL}/api/v1/miniprogram/ai/pricing?scenes=allround_design,parent_child_design`,
          method: 'GET',
          success: (requestRes) => {
            if (requestRes.statusCode === 200 && requestRes.data) {
              const data = requestRes.data as any;
              if (data.code === 0) {
                resolve(data.data || {});
              } else {
                reject(new Error(data.msg || '获取计费失败'));
              }
            } else {
              reject(new Error(`请求失败: ${requestRes.statusCode}`));
            }
          },
          fail: reject,
        });
      });
      const prices = res?.prices || {};
      const allroundCost = Number(prices.allround_design);
      const parentCost = Number(prices.parent_child_design);
      this.setData({
        allroundCost: Number.isFinite(allroundCost) && allroundCost > 0 ? allroundCost : DEFAULT_ALLROUND_COST,
        parentCost: Number.isFinite(parentCost) && parentCost > 0 ? parentCost : DEFAULT_PARENT_COST,
        pricingLoaded: true,
      }, () => this.syncCurrentCost());
    } catch (_error) {
      this.setData({ pricingLoaded: false }, () => this.syncCurrentCost());
    }
  },

  requestStoneBalance(token: string): Promise<number> {
    return new Promise((resolve, reject) => {
      wx.request({
        url: `${API_BASE_URL}/api/v1/miniprogram/user/stones`,
        method: 'GET',
        header: {
          token,
          'Content-Type': 'application/json',
        },
        success: (requestRes) => {
          if (requestRes.statusCode === 200 && requestRes.data) {
            const data = requestRes.data as any;
            if (data.code === 0) {
              resolve(Number(data.data?.stones || 0));
            } else {
              reject(new Error(data.msg || '获取余额失败'));
            }
          } else {
            reject(new Error(`请求失败: ${requestRes.statusCode}`));
          }
        },
        fail: reject,
      });
    });
  },

  onSelectTargetStyle(e: any) {
    this.setData({ targetStyle: String(e.currentTarget.dataset.value || '') });
  },

  onSelectPosterStyle(e: any) {
    this.setData({ posterStyle: String(e.currentTarget.dataset.value || '') });
  },

  onSelectCulturalStyle(e: any) {
    this.setData({ culturalStyle: String(e.currentTarget.dataset.value || '') });
  },

  onSelectParentType(e: any) {
    this.setData({
      parentDesignType: String(e.currentTarget.dataset.value || 'doll'),
      parentDesignTypeLabel: String(e.currentTarget.dataset.label || '玩偶手作'),
    });
  },

  onSelectParentPrototype(e: any) {
    this.setData({
      parentPrototype: String(e.currentTarget.dataset.value || 'landmark'),
      parentPrototypeLabel: String(e.currentTarget.dataset.label || '城市地标'),
    });
  },

  onSelectParentStyle(e: any) {
    this.setData({
      parentStyle: String(e.currentTarget.dataset.value || 'cute'),
      parentStyleLabel: String(e.currentTarget.dataset.label || '可爱'),
    });
  },

  onSelectQuality(e: any) {
    this.setData({ selectedQuality: String(e.currentTarget.dataset.value || 'uhd') });
  },

  onSelectCanvas(e: any) {
    this.setData({ selectedCanvas: String(e.currentTarget.dataset.value || '1:1') });
  },

  onSelectGenerateCount(e: any) {
    const value = Number(e.currentTarget.dataset.count);
    if (!Number.isFinite(value) || value <= 0) {
      return;
    }
    this.setData({ selectedGenerateCount: Math.min(3, Math.max(1, Math.floor(value))) }, () => this.syncCurrentCost());
  },

  onPromptInput(e: any) {
    this.setData({ promptText: String(e.detail.value || '') });
  },

  async onOptimizePrompt() {
    if (this.data.optimizingPrompt) return;
    const rawPrompt = (this.data.promptText || '').trim();
    if (!rawPrompt) {
      wx.showToast({ title: '请先输入提示词', icon: 'none' });
      return;
    }
    this.setData({ optimizingPrompt: true });
    const { optimizePrompt } = require('../../utils/promptOptimize');
    const result = await optimizePrompt(rawPrompt, 'architecture');
    this.setData({ optimizingPrompt: false });
    if (result.success && result.optimizedPrompt) {
      this.setData({ promptText: result.optimizedPrompt });
      wx.showToast({ title: '优化完成', icon: 'success' });
    } else {
      if (result.needMembership) {
        wx.showModal({ title: '会员专属', content: result.error || '请开通会员后使用AI优化', confirmText: '去开通', success: (res) => { if (res.confirm) wx.navigateTo({ url: '/pages/topupcenter/topupcenter' }); } });
      } else {
        wx.showToast({ title: result.error || '优化失败', icon: 'none' });
      }
    }
  },

  onPosterThemeInput(e: any) {
    this.setData({ posterTheme: String(e.detail.value || '') });
  },

  onPosterTextInput(e: any) {
    this.setData({ posterText: String(e.detail.value || '') });
  },

  onProductTypeInput(e: any) {
    this.setData({ productType: String(e.detail.value || '') });
  },

  onProductRequirementsInput(e: any) {
    this.setData({ productRequirements: String(e.detail.value || '') });
  },

  onParentThemeInput(e: any) {
    this.setData({ parentTheme: String(e.detail.value || '') });
  },

  onParentRequirementsInput(e: any) {
    this.setData({ parentRequirements: String(e.detail.value || '') });
  },

  chooseImages(count: number): Promise<string[]> {
    return new Promise((resolve, reject) => {
      wx.chooseMedia({
        count,
        mediaType: ['image'],
        sourceType: ['camera', 'album'],
        success: (res) => {
          resolve((res.tempFiles || []).map((file) => file.tempFilePath).filter(Boolean));
        },
        fail: (err) => {
          const message = String(err?.errMsg || '');
          if (message.indexOf('cancel') >= 0) {
            resolve([]);
            return;
          }
          reject(err);
        },
      });
    });
  },

  async onUploadStyleOriginal() {
    try {
      const images = await this.chooseImages(1);
      if (images[0]) {
        this.setData({ styleOriginalImage: images[0] });
      }
    } catch (_error) {
      wx.showToast({ title: '选择图片失败', icon: 'none' });
    }
  },

  onDeleteStyleOriginal() {
    this.setData({ styleOriginalImage: '' });
  },

  async onUploadPosterReference() {
    await this.appendReferenceImages('posterReferenceImages');
  },

  async onUploadCulturalReference() {
    await this.appendReferenceImages('culturalReferenceImages');
  },

  async onUploadParentReference() {
    await this.appendReferenceImages('parentReferenceImages');
  },

  async appendReferenceImages(field: ReferenceImageField) {
    try {
      const current = normalizeImages(this.data[field] || []);
      if (current.length >= MAX_REFERENCE_IMAGE_COUNT) {
        wx.showToast({ title: '最多上传6张参考图', icon: 'none' });
        return;
      }
      const images = await this.chooseImages(MAX_REFERENCE_IMAGE_COUNT - current.length);
      if (images.length > 0) {
        this.setData({ [field]: normalizeImages([...current, ...images]) });
      }
    } catch (_error) {
      wx.showToast({ title: '选择图片失败', icon: 'none' });
    }
  },

  onDeletePosterReferenceImage(e: any) {
    this.deleteReferenceImage('posterReferenceImages', Number(e.currentTarget.dataset.index));
  },

  onDeleteCulturalReferenceImage(e: any) {
    this.deleteReferenceImage('culturalReferenceImages', Number(e.currentTarget.dataset.index));
  },

  onDeleteParentReferenceImage(e: any) {
    this.deleteReferenceImage('parentReferenceImages', Number(e.currentTarget.dataset.index));
  },

  deleteReferenceImage(field: ReferenceImageField, index: number) {
    const images = normalizeImages(this.data[field] || []);
    if (index < 0 || index >= images.length) {
      return;
    }
    images.splice(index, 1);
    this.setData({ [field]: images });
  },

  onPreviewImage(e: any) {
    const url = String(e.currentTarget.dataset.url || '');
    const rawUrls = e.currentTarget.dataset.urls;
    const urls = Array.isArray(rawUrls) ? normalizeImages(rawUrls) : [url];
    if (!url || urls.length === 0) {
      return;
    }
    wx.previewImage({ current: url, urls });
  },

  uploadImageToOSS(imagePath: string, token: string): Promise<string> {
    if (isRemoteImage(imagePath)) {
      return Promise.resolve(imagePath);
    }
    const apiPath = '/api/v1/miniprogram/ai/upload-reference-image';
    const deviceID = getCachedDeviceFingerprint() || '';
    const params = generateRequestParams(token, '{}', apiPath, deviceID);
    const headers = paramsToHeaders(params);
    return new Promise((resolve, reject) => {
      wx.uploadFile({
        url: `${API_BASE_URL}${apiPath}`,
        filePath: imagePath,
        name: 'file',
        header: headers,
        success: (res) => {
          if (res.statusCode !== 200) {
            reject(new Error(`上传失败: ${res.statusCode}`));
            return;
          }
          try {
            const data = JSON.parse(res.data) as any;
            if (data.code === 0 && data.data?.url) {
              resolve(String(data.data.url));
            } else {
              reject(new Error(data.msg || '上传失败'));
            }
          } catch (_error) {
            reject(new Error('解析响应失败'));
          }
        },
        fail: reject,
      });
    });
  },

  async uploadImageList(images: string[], token: string): Promise<string[]> {
    const result: string[] = [];
    for (const image of normalizeImages(images)) {
      result.push(await this.uploadImageToOSS(image, token));
    }
    return result;
  },

  blockUserTexts(): boolean {
    const texts: string[] = [];
    const promptText = String(this.data.promptText || '').trim();
    if (promptText) {
      texts.push(promptText);
    }

    const activeTab = this.data.activeTab;
    if (activeTab === 'poster') {
      const theme = String(this.data.posterTheme || '').trim();
      const text = String(this.data.posterText || '').trim();
      if (theme) texts.push(theme);
      if (text) texts.push(text);
    } else if (activeTab === 'cultural') {
      const productType = String(this.data.productType || '').trim();
      const requirements = String(this.data.productRequirements || '').trim();
      if (productType) texts.push(productType);
      if (requirements) texts.push(requirements);
    } else if (activeTab === 'parentWorkshop') {
      const theme = String(this.data.parentTheme || '').trim();
      const requirements = String(this.data.parentRequirements || '').trim();
      if (theme) texts.push(theme);
      if (requirements) texts.push(requirements);
    }

    for (const text of texts) {
      if (blockUnsafeText(text, { scene: 'prompt' })) {
        return true;
      }
    }
    return false;
  },

  validateCurrentForm(): string {
    const activeTab = this.data.activeTab;
    if (activeTab === 'style') {
      if (!this.data.styleOriginalImage) {
        return '请先上传原图';
      }
      if (!this.data.targetStyle) {
        return '请选择目标风格';
      }
      return '';
    }
    if (activeTab === 'poster') {
      return (this.data.posterTheme || this.data.posterText) ? '' : '请填写海报主题或文字';
    }
    if (activeTab === 'cultural') {
      return (this.data.productType || this.data.productRequirements) ? '' : '请填写产品类型或设计要求';
    }
    return (this.data.parentTheme || this.data.parentRequirements) ? '' : '请填写创作主题或设计要求';
  },

  buildAllroundRequest(originalImageUrl: string, referenceImages: string[]) {
    const generateCount = this.getCurrentGenerateCount();
    const basePayload: Record<string, any> = {
      quality: this.data.selectedQuality,
      canvas: this.data.selectedCanvas,
      generate_count: generateCount,
      user_prompt: String(this.data.promptText || '').trim(),
    };
    if (this.data.activeTab === 'style') {
      return {
        ...basePayload,
        design_type: 'style_transform',
        original_image: originalImageUrl,
        target_style: this.data.targetStyle,
        original_image_urls: originalImageUrl ? [originalImageUrl] : [],
        reference_image_urls: originalImageUrl ? [originalImageUrl] : [],
        ordered_image_urls: originalImageUrl ? [originalImageUrl] : [],
      };
    }
    if (this.data.activeTab === 'poster') {
      return {
        ...basePayload,
        design_type: 'poster',
        theme: this.data.posterTheme,
        text: this.data.posterText,
        style: this.data.posterStyle,
        reference_images: referenceImages,
        reference_image_urls: referenceImages,
        ordered_image_urls: referenceImages,
      };
    }
    return {
      ...basePayload,
      design_type: 'cultural',
      product_type: this.data.productType,
      requirements: this.data.productRequirements,
      style: this.data.culturalStyle,
      reference_images: referenceImages,
      reference_image_urls: referenceImages,
      ordered_image_urls: referenceImages,
    };
  },

  buildParentRequest(referenceImages: string[]) {
    const generateCount = this.getCurrentGenerateCount();
    const requestData: Record<string, any> = {
      design_type: this.data.parentDesignType,
      prototype: this.data.parentPrototype,
      style: this.data.parentStyle,
      quality: this.data.selectedQuality,
      canvas: this.data.selectedCanvas,
      generate_count: generateCount,
      user_prompt: String(this.data.promptText || '').trim(),
      reference_images: referenceImages,
      reference_image_urls: referenceImages,
      ordered_image_urls: referenceImages,
    };
    if (this.data.parentDesignType === 'doll') {
      requestData.theme = this.data.parentTheme;
      requestData.requirements = this.data.parentRequirements;
    } else if (this.data.parentDesignType === 'poster') {
      requestData.theme = this.data.parentTheme;
      requestData.text = this.data.parentRequirements;
    } else {
      requestData.product_type = this.data.parentTheme;
      requestData.requirements = this.data.parentRequirements;
    }
    return requestData;
  },

  async onGenerateDesign() {
    if (this.data.generating) {
      return;
    }
    if (this.blockUserTexts()) {
      return;
    }
    const validationMessage = this.validateCurrentForm();
    if (validationMessage) {
      wx.showToast({ title: validationMessage, icon: 'none' });
      return;
    }
    const token = wx.getStorageSync('token');
    if (!token) {
      wx.showToast({ title: '请先登录', icon: 'none' });
      return;
    }

    try {
      const stones = await this.requestStoneBalance(token);
      if (stones < this.data.currentCost) {
        wx.showModal({
          title: '余额不足',
          content: `当前余额：${stones}灵石，需要：${this.data.currentCost}灵石`,
          confirmText: '去充值',
          success: (res) => {
            if (res.confirm) {
              wx.navigateTo({ url: '/pages/topupcenter/topupcenter' });
            }
          },
        });
        return;
      }
    } catch (_error) {
      wx.showToast({ title: '检查余额失败', icon: 'none' });
      return;
    }

    this.setData({ generating: true });
    wx.showLoading({ title: '正在提交...', mask: true });

    try {
      const activeTab = this.data.activeTab;
      let apiPath = '/api/v1/miniprogram/ai/allround-design';
      let scene = 'allround_design';
      let requestData: Record<string, any>;
      let referenceImages: string[] = [];
      let originalImageUrl = '';

      if (activeTab === 'style') {
        originalImageUrl = await this.uploadImageToOSS(this.data.styleOriginalImage, token);
        requestData = this.buildAllroundRequest(originalImageUrl, []);
      } else if (activeTab === 'poster') {
        referenceImages = await this.uploadImageList(this.data.posterReferenceImages, token);
        requestData = this.buildAllroundRequest('', referenceImages);
      } else if (activeTab === 'cultural') {
        referenceImages = await this.uploadImageList(this.data.culturalReferenceImages, token);
        requestData = this.buildAllroundRequest('', referenceImages);
      } else {
        apiPath = '/api/v1/miniprogram/ai/parent-child-design';
        scene = 'parent_child_design';
        referenceImages = await this.uploadImageList(this.data.parentReferenceImages, token);
        requestData = this.buildParentRequest(referenceImages);
      }

      const deviceID = getCachedDeviceFingerprint() || '';
      const params = generateRequestParams(token, requestData, apiPath, deviceID);
      const headers = paramsToHeaders(params);
      const res = await new Promise<any>((resolve, reject) => {
        wx.request({
          url: `${API_BASE_URL}${apiPath}`,
          method: 'POST',
          header: {
            ...headers,
            'Content-Type': 'application/json',
          },
          data: requestData,
          success: (requestRes) => {
            if (requestRes.statusCode === 200 && requestRes.data) {
              const data = requestRes.data as any;
              if (data.code === 0) {
                resolve(data.data || {});
              } else {
                reject(new Error(data.msg || '生成失败'));
              }
            } else {
              reject(new Error(`请求失败: ${requestRes.statusCode}`));
            }
          },
          fail: reject,
        });
      });

      if (!res?.task_no) {
        throw new Error('生成失败');
      }
      const taskNo = encodeURIComponent(String(res.task_no));
      const safePrompt = encodeURIComponent(String(requestData.user_prompt || requestData.theme || requestData.product_type || '综合设计'));
      wx.navigateTo({
        url: `/pages/generatedetails/generatedetails?task_no=${taskNo}&prompt=${safePrompt}`,
        success: (navRes) => {
          navRes.eventChannel.emit('taskData', {
            id: res.task_id || 0,
            task_no: res.task_no,
            scene,
            status: 'pending',
            requested_count: this.getCurrentGenerateCount(),
            generated_count: 0,
            stones_used: this.data.currentCost,
            result: {},
            error_message: '',
            created_at: new Date().toISOString(),
            updated_at: new Date().toISOString(),
            task_type: 'ai_draw',
            taskKind: 'image',
            prompt: requestData.user_prompt || '',
            user_prompt: requestData.user_prompt || '',
            reference_image_url: referenceImages[0] || originalImageUrl || '',
            reference_image_urls: referenceImages.length > 0 ? referenceImages : (originalImageUrl ? [originalImageUrl] : []),
            original_image_urls: originalImageUrl ? [originalImageUrl] : [],
            ordered_image_urls: referenceImages.length > 0 ? referenceImages : (originalImageUrl ? [originalImageUrl] : []),
            quality: this.data.selectedQuality,
            canvas: this.data.selectedCanvas,
          });
        },
        fail: () => wx.showToast({ title: '页面跳转失败', icon: 'none' }),
      });
    } catch (error: any) {
      wx.showToast({
        title: sanitizeAIGenerationErrorMessage(error?.message || '生成失败'),
        icon: 'none',
        duration: 2400,
      });
    } finally {
      wx.hideLoading();
      this.setData({ generating: false });
    }
  },

  onShareAppMessage() {
    return {
      title: '综合设计',
      path: '/pages/allrounddesign/allrounddesign',
    };
  },
});
