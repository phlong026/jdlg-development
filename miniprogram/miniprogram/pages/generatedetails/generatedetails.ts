// pages/generatedetails/generatedetails.ts
import { generateRequestParams, paramsToHeaders } from '../../utils/parameter';
import { resolveAssetPath } from '../../utils/asset';
import { sanitizeAIGenerationErrorMessage } from '../../utils/aiError';
import { prefetchImages } from '../../utils/perf';
import { prepareShareCardImage, prefetchShareCardImage } from '../../utils/shareImage';

// API基础地址
const API_BASE_URL = 'https://api.jiadilingguang.com';

interface TaskData {
  id: number;
  task_no: string;
  scene: string;
  status: string;
  stones_used: number;
  requested_count?: number;
  generated_count?: number;
  refunded_stones?: number;
  refunded_image_count?: number;
  result: any;
  error_message: string;
  created_at: string;
  updated_at: string;
  sceneText?: string;
  createdAtText?: string;
  imageUrl?: string;
  prompt?: string;
  reference_image_url?: string;
  reference_image_urls?: string[];
  original_image_urls?: string[];
  ordered_image_urls?: string[];
  user_prompt?: string;
  task_type?: 'ai_draw' | 'ai_video' | 'ai_chat' | 'ai_cost_doc';
  excel_url?: string;
  watermarkedImageUrl?: string;
  originalImageUrl?: string;
  /** 用于详情页展示区分：image | video | cost_doc */
  taskKind?: 'image' | 'video' | 'cost_doc';
  resultImages?: string[];
}

interface TaskImageInfo {
  title: string;
  subtitle: string;
}

type PreviewImageMode = 'result' | 'reference' | 'qrcode';

type PendingDownloadType = '' | 'image' | 'video';

function getTaskStatusPollDelay(attempt: number): number {
  if (attempt < 2) {
    return 3000;
  }
  if (attempt < 6) {
    return 4000;
  }
  return 5000;
}

interface PublishTabItem {
  label: string;
  value: string;
  parent?: string;
}

function base64Encode(input: string): string {
  const chars = 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/=';
  const str = encodeURIComponent(input).replace(/%([0-9A-F]{2})/g, (_, p1) =>
    String.fromCharCode(parseInt(p1, 16)),
  );
  let output = '';
  let i = 0;
  while (i < str.length) {
    const chr1 = str.charCodeAt(i++);
    const chr2 = str.charCodeAt(i++);
    const chr3 = str.charCodeAt(i++);

    const enc1 = chr1 >> 2;
    const enc2 = ((chr1 & 3) << 4) | (chr2 >> 4);
    let enc3 = ((chr2 & 15) << 2) | (chr3 >> 6);
    let enc4 = chr3 & 63;

    if (isNaN(chr2)) {
      enc3 = enc4 = 64;
    } else if (isNaN(chr3)) {
      enc4 = 64;
    }

    output +=
      chars.charAt(enc1) +
      chars.charAt(enc2) +
      chars.charAt(enc3) +
      chars.charAt(enc4);
  }
  return output;
}

function normalizeReferenceImageUrls(values: any[]): string[] {
  return values
    .map((item) => (typeof item === 'string' ? item.trim() : ''))
    .filter((item, index, array) => {
      if (!item) {
        return false;
      }
      if (!item.startsWith('http://') && !item.startsWith('https://')) {
        return false;
      }
      return array.indexOf(item) === index;
    });
}

function parseIncomingReferenceImageUrls(options: Record<string, any>): string[] {
  const values: string[] = [];
  if (options.reference_image_urls) {
    try {
      const parsed = JSON.parse(decodeURIComponent(options.reference_image_urls));
      if (Array.isArray(parsed)) {
        values.push(...parsed);
      }
    } catch (error) {
      console.warn('解析详情页 reference_image_urls 失败:', error);
    }
  }
  if (options.reference_image_url) {
    try {
      values.push(decodeURIComponent(options.reference_image_url));
    } catch (error) {
      console.warn('解析详情页 reference_image_url 失败:', error);
    }
  }
  return normalizeReferenceImageUrls(values);
}

function hasStructuredOriginalImages(data?: Partial<TaskData> | null): boolean {
  return Array.isArray(data?.original_image_urls)
    && data.original_image_urls.some((item) => typeof item === 'string' && item.trim() !== '');
}

function resolveDetailEntrySource(currentSource: any, data?: Partial<TaskData> | null): string {
  const resolvedSource = String(currentSource || '').trim();
  if (resolvedSource) {
    return resolvedSource;
  }
  if (hasStructuredOriginalImages(data)) {
    return 'make_same';
  }
  return '';
}

const PUBLISH_TEMPLATE_TOPICS: PublishTabItem[] = [
  { label: '场景', value: 'scene' },
  { label: '风格', value: 'style' },
  { label: '灵感', value: 'inspiration' },
];

const PUBLISH_TEMPLATE_SUB_TOPICS: PublishTabItem[] = [
  { label: '乡墅外观', value: 'villa_exterior', parent: 'scene' },
  { label: '室内空间', value: 'interior_space', parent: 'scene' },
  { label: '花园庭院', value: 'garden_courtyard', parent: 'scene' },
  { label: '改造翻新', value: 'renovation', parent: 'scene' },
  { label: '商业空间', value: 'commercial_space', parent: 'scene' },
  { label: '设计辅助', value: 'design_assist', parent: 'scene' },
  { label: '新闽派', value: 'new_minnan', parent: 'style' },
  { label: '新中式', value: 'new_chinese', parent: 'style' },
  { label: '现代风格', value: 'modern', parent: 'style' },
  { label: '经典欧式', value: 'classic_european', parent: 'style' },
  { label: '地域特色', value: 'regional', parent: 'style' },
  { label: '乡建趋势', value: 'rural_trend', parent: 'inspiration' },
  { label: '生活方式', value: 'lifestyle', parent: 'inspiration' },
  { label: '地域文化', value: 'regional_culture', parent: 'inspiration' },
  { label: '功能创新', value: 'function_innovation', parent: 'inspiration' },
  { label: '案例精选', value: 'selected_cases', parent: 'inspiration' },
];

void PUBLISH_TEMPLATE_TOPICS;
void PUBLISH_TEMPLATE_SUB_TOPICS;

function normalizePublishMainTabs(list: any): PublishTabItem[] {
  const normalized = (Array.isArray(list) ? list : [])
    .map((item) => ({
      label: String(item?.label || '').trim(),
      value: String(item?.value || '').trim(),
    }))
    .filter((item) => item.label && item.value);
  return normalized;
}

function normalizePublishSubTabs(list: any, mainTabs: PublishTabItem[]): PublishTabItem[] {
  const mainValues = new Set(mainTabs.map((item) => item.value));
  const normalized = (Array.isArray(list) ? list : [])
    .map((item) => ({
      label: String(item?.label || '').trim(),
      value: String(item?.value || '').trim(),
      parent: String(item?.parent || '').trim(),
    }))
    .filter((item) => item.label && item.value && item.parent && mainValues.has(item.parent));
  return normalized;
}

function getPublishSubTabsByParent(allSubTabs: PublishTabItem[], parentValue: string): PublishTabItem[] {
  const currentParent = String(parentValue || '').trim();
  return allSubTabs.filter((item) => String(item.parent || '').trim() === currentParent);
}

function normalizePublishThirdTabs(list: any, subTabs: PublishTabItem[]): PublishTabItem[] {
  const subValues = new Set(subTabs.map((item) => item.value));
  return (Array.isArray(list) ? list : [])
    .map((item) => ({
      label: String(item?.label || '').trim(),
      value: String(item?.value || '').trim(),
      parent: String(item?.parent || '').trim(),
    }))
    .filter((item) => item.label && item.value && item.parent && subValues.has(item.parent));
}

function getPublishThirdTabsByParent(allThirdTabs: PublishTabItem[], parentValue: string): PublishTabItem[] {
  const currentParent = String(parentValue || '').trim();
  return allThirdTabs.filter((item) => String(item.parent || '').trim() === currentParent);
}

const LOCAL_ENTERPRISE_WECHAT_QRCODE = resolveAssetPath('/assets/企业微信二维码.png');
const GENERATED_TASK_PROGRESS_STORAGE_PREFIX = 'generated_task_progress_';

Page({
  data: {
    taskData: {} as TaskData,
    loading: true,
    statusText: '',
    durationText: '',
    resultSummary: '我已为你生成装修图。',
    visualProgressPercent: 6,
    loadingStageText: '任务已提交，正在准备生成环境...',
    resultRevealActive: false,
    entrySource: '',
    showSceneTabs: false,
    activeTab: 'exterior',
    navSafeTop: 0,
    navBarHeight: 96,
    navContentHeight: 44,
    navSideWidth: 88,
    showPublishModal: false,
    showImagePreviewModal: false,
    currentResultImageIndex: 0,
    resultImageInfos: [] as TaskImageInfo[],
    currentResultImageInfo: {
      title: '效果图',
      subtitle: '左右滑动可切换不同图片。',
    } as TaskImageInfo,
    previewImageUrls: [] as string[],
    previewImageIndex: 0,
    currentPreviewImageInfo: {
      title: '效果图',
      subtitle: '左右滑动可切换不同图片。',
    } as TaskImageInfo,
    previewImageMode: 'result' as PreviewImageMode,
    shareImageUrl: '',
    shareImageSourceUrl: '',
    downloadTargetImageIndex: 0,
    showEnterpriseWechatModal: false,
    enterpriseWechatQRCodeUrl: '',
    enterpriseWechatTip: '添加企业微信并留下电话号码后，可下载保存生成图片。',
    enterpriseWechatVerified: false,
    enterpriseWechatVerifiedAtText: '',
    enterpriseWechatContact: '',
    enterpriseWechatBindTicket: '',
    enterpriseWechatBindStatus: 'pending',
    enterpriseWechatConfigLoaded: false,
    enterpriseWechatConfigLoading: false,
    enterpriseWechatAutoChecking: false,
    enterpriseWechatPhoneSubmitting: false,
    pendingDownloadType: '' as PendingDownloadType,
    publishForm: {
      name: '',
      description: '',
      isFree: true,
      price: 0,
      mainTab: '',
      subTab: '',
      thirdTab: '',
    },
    // 模板广场一级/二级话题配置（用于发布时选择所属板块）
    mainTabs: [] as PublishTabItem[],
    allSubTabs: [] as PublishTabItem[],
    subTabs: [] as PublishTabItem[],
    allThirdTabs: [] as PublishTabItem[],
    thirdTabs: [] as PublishTabItem[],
    mainTabIndex: -1,
    subTabIndex: -1,
    thirdTabIndex: -1,
  },

  pollingTimer: null as any,
  pollingAttempt: 0,
  progressTimer: null as any,

  resultRevealTimer: null as any,

  enterpriseWechatAutoCheckTimer: null as any,

  enterpriseWechatAutoResumeInProgress: false,

  onLoad(options) {
    this.initNavLayout();
    this.loadTemplateTabConfig();
    const decodedTaskNo = options?.task_no ? decodeURIComponent(options.task_no) : '';
    const source = String(options?.source || '');
    const showSceneTabs = source === 'rural_villa'
      ? true
      : source === 'index' || source === 'urban_renewal'
        ? false
        : options?.showSceneTabs === '1';
    const activeTab = options?.tab === 'interior' ? 'interior' : 'exterior';
    this.setData({
      entrySource: resolveDetailEntrySource(source),
      showSceneTabs,
      activeTab,
    });

    const eventChannel = this.getOpenerEventChannel();
    const incomingReferenceImages = parseIncomingReferenceImageUrls(options || {});

    // 接收传递的任务数据
    eventChannel.on('taskData', (data: TaskData) => {
      this.setTaskData(data);
    });

    // 如果有task_no参数但没有通过eventChannel接收到数据，则从API获取
    if (options.task_no) {
      this.setTaskData({
        id: 0,
        task_no: decodedTaskNo,
        scene: options.task_type === 'ai_video'
          ? 'ai_video'
          : (options.task_type === 'ai_cost_doc' ? 'ai_cost_doc' : 'ai_draw_single'),
        status: 'pending',
        stones_used: 0,
        result: {},
        error_message: '',
        created_at: '',
        updated_at: '',
        task_type: options.task_type === 'ai_video'
          ? 'ai_video'
          : (options.task_type === 'ai_cost_doc' ? 'ai_cost_doc' : 'ai_draw'),
        prompt: options.prompt ? decodeURIComponent(options.prompt) : '',
        user_prompt: options.prompt ? decodeURIComponent(options.prompt) : '',
        reference_image_url: incomingReferenceImages[0] || '',
        reference_image_urls: incomingReferenceImages,
      });
      void this.loadTaskDetail(decodedTaskNo, true);
    }
  },

  initNavLayout() {
    try {
      const systemInfo = wx.getSystemInfoSync();
      const menuRect = typeof wx.getMenuButtonBoundingClientRect === 'function'
        ? wx.getMenuButtonBoundingClientRect()
        : null;
      const safeTop = Number(systemInfo.statusBarHeight || systemInfo.safeArea?.top || 0);

      if (menuRect) {
        const navBarHeight = Number(menuRect.height + Math.max((menuRect.top - safeTop) * 2, 0));
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
        navBarHeight: 44,
        navContentHeight: 44,
        navSideWidth: 96,
      });
    } catch (error) {
      this.setData({
        navSafeTop: 20,
        navBarHeight: 44,
        navContentHeight: 44,
        navSideWidth: 96,
      });
    }
  },

  async loadTemplateTabConfig() {
    try {
      const res = await new Promise<any>((resolve, reject) => {
        wx.request({
          url: `${API_BASE_URL}/api/v1/miniprogram/templates/tab-config`,
          method: 'GET',
          success: (response) => {
            if (response.statusCode === 200 && (response.data as any)?.code === 0) {
              resolve((response.data as any).data);
              return;
            }
            reject(new Error('获取模板广场分类配置失败'));
          },
          fail: reject,
        });
      });
      const mainTabs = normalizePublishMainTabs(res?.main_tabs);
      if (!mainTabs.length) {
        this.setData({
          mainTabs: [],
          allSubTabs: [],
          allThirdTabs: [],
          subTabs: [],
          thirdTabs: [],
          mainTabIndex: -1,
          subTabIndex: -1,
          thirdTabIndex: -1,
          'publishForm.mainTab': '',
          'publishForm.subTab': '',
          'publishForm.thirdTab': '',
        });
        return;
      }
      const allSubTabs = normalizePublishSubTabs(res?.sub_tabs, mainTabs);
      const allThirdTabs = normalizePublishThirdTabs(res?.third_tabs, allSubTabs);
      const firstMain = mainTabs[0];
      const subTabs = getPublishSubTabsByParent(allSubTabs, firstMain?.value || '');
      const firstSub = subTabs[0];
      const thirdTabs = getPublishThirdTabsByParent(allThirdTabs, firstSub?.value || '');
      const firstThird = thirdTabs[0];
      this.setData({
        mainTabs,
        allSubTabs,
        allThirdTabs,
        subTabs,
        thirdTabs,
        mainTabIndex: firstMain ? 0 : -1,
        subTabIndex: firstSub ? 0 : -1,
        thirdTabIndex: firstThird ? 0 : -1,
        'publishForm.mainTab': firstMain?.value || '',
        'publishForm.subTab': firstSub?.value || '',
        'publishForm.thirdTab': firstThird?.value || '',
      });
    } catch (e) {
      console.error('加载模板广场分类配置失败:', e);
      this.setData({
        mainTabs: [],
        allSubTabs: [],
        allThirdTabs: [],
        subTabs: [],
        thirdTabs: [],
        mainTabIndex: -1,
        subTabIndex: -1,
        thirdTabIndex: -1,
        'publishForm.mainTab': '',
        'publishForm.subTab': '',
        'publishForm.thirdTab': '',
      });
    }
  },

  setTaskData(data: TaskData) {
    const previousStatus = String(this.data.taskData?.status || '');
    const restoredProgress = this.getStoredProgressState(data.task_no);
    const nextEntrySource = resolveDetailEntrySource(this.data.entrySource, data);
    const sceneMap: Record<string, string> = {
      'ai_draw_single': 'AI绘画',
      'ai_draw_multi': 'AI绘画',
      'new_minnan_tool_text_to_image': '新闽派工具',
      'new_minnan_tool_image_to_image': '新闽派工具',
      'ai_chat_single': 'AI对话（单轮）',
      'ai_chat_multi': 'AI对话（多轮）',
      'ai_video': 'AI生成视频',
      'ai_cost_doc': 'AI造价(文档)',
    };

    let taskKind: 'image' | 'video' | 'cost_doc' = 'image';
    if (data.scene === 'ai_cost_doc') {
      taskKind = 'cost_doc';
    } else if (data.task_type === 'ai_video' || data.scene === 'ai_video') {
      taskKind = 'video';
    } else {
      taskKind = 'image';
    }
    data.taskKind = taskKind;

    const statusMap: Record<string, string> = {
      'pending': '等待中',
      'processing': '生成中',
      'success': '已完成',
      'failed': '已失败',
    };

    const createdAtText = this.formatTime(data.created_at);
    data.createdAtText = createdAtText || data.createdAtText || '';
    const watermarkedImageUrl = this.getWatermarkedImageUrl(data);
    const originalImageUrl = this.getOriginalImageUrl(data);
    const resultImages = this.extractResultImages(data);
    data.imageUrl = watermarkedImageUrl || data.imageUrl || '';
    data.watermarkedImageUrl = watermarkedImageUrl;
    data.originalImageUrl = originalImageUrl;
    if (!data.excel_url && data.result && data.result.excel_url) {
      data.excel_url = data.result.excel_url;
    }
    const referenceImages = this.extractReferenceImages(data);
    data.reference_image_urls = referenceImages;
    if (!data.reference_image_url && referenceImages.length > 0) {
      data.reference_image_url = referenceImages[0];
    }
    if (!Array.isArray(data.original_image_urls)) {
      data.original_image_urls = [];
    }
    if (!Array.isArray(data.ordered_image_urls) || data.ordered_image_urls.length === 0) {
      data.ordered_image_urls = [
        ...(Array.isArray(data.original_image_urls) ? data.original_image_urls : []),
        ...referenceImages,
      ].filter((item, index, array) => {
        const value = typeof item === 'string' ? item.trim() : '';
        return !!value && array.findIndex((inner) => String(inner || '').trim() === value) === index;
      }) as string[];
    }
    data.resultImages = resultImages.length ? resultImages : (watermarkedImageUrl ? [watermarkedImageUrl] : []);
    data.requested_count = this.getRequestedCount(data);
    data.generated_count = this.getGeneratedCount(data);
    data.refunded_stones = this.getRefundedStones(data);
    data.refunded_image_count = this.getRefundedImageCount(data);
    const resolvedSceneText = this.getSceneText(data, sceneMap);
    data.sceneText = resolvedSceneText;

    if (data.error_message) {
      data.error_message = sanitizeAIGenerationErrorMessage(data.error_message);
    } else if (data.status === 'failed') {
      data.error_message = sanitizeAIGenerationErrorMessage('');
    }

    let durationText = '';
    if (data.result && data.result.duration) {
      const seconds = (data.result.duration / 1000).toFixed(1);
      durationText = `${seconds}秒`;
    }

    const resultImageInfos = this.buildResultImageInfos(data);
    const safeResultImageIndex = resultImageInfos.length && this.data.currentResultImageIndex < resultImageInfos.length
      ? this.data.currentResultImageIndex
      : 0;
    const shouldRevealResult = (previousStatus === 'pending' || previousStatus === 'processing') && data.status === 'success';
    const nextVisualProgressPercent = (data.status === 'pending' || data.status === 'processing')
      ? Math.max(Number(this.data.visualProgressPercent || 0), restoredProgress.percent)
      : Number(this.data.visualProgressPercent || 0);
    const nextLoadingStageText = (data.status === 'pending' || data.status === 'processing')
      ? (restoredProgress.loadingStageText || this.data.loadingStageText)
      : this.data.loadingStageText;
    if (data.status !== 'success' && this.resultRevealTimer) {
      clearTimeout(this.resultRevealTimer);
      this.resultRevealTimer = null;
    }

    this.setData({
      entrySource: nextEntrySource,
      taskData: data,
      statusText: statusMap[data.status] || data.status,
      durationText: durationText,
      resultSummary: this.buildResultSummary(data),
      currentResultImageIndex: safeResultImageIndex,
      resultImageInfos,
      currentResultImageInfo: this.getSafeImageInfo(resultImageInfos, safeResultImageIndex),
      currentPreviewImageInfo: this.getSafeImageInfo(resultImageInfos, this.data.previewImageIndex),
      resultRevealActive: shouldRevealResult,
      visualProgressPercent: nextVisualProgressPercent,
      loadingStageText: nextLoadingStageText,
      loading: false,
    });
    void this.prepareCurrentShareImage(this.getCurrentShareSourceUrl(safeResultImageIndex));

    if (shouldRevealResult) {
      if (this.resultRevealTimer) {
        clearTimeout(this.resultRevealTimer);
      }
      this.resultRevealTimer = setTimeout(() => {
        this.setData({ resultRevealActive: false });
        this.resultRevealTimer = null;
      }, 560);
    }

    if (data.status === 'pending' || data.status === 'processing') {
      this.startVisualProgress(data.status);
    } else if (data.status === 'success') {
      this.completeVisualProgress();
    } else {
      this.stopVisualProgress();
    }

    if ((data.taskKind === 'image' || data.taskKind === 'video') && data.status === 'success' && wx.getStorageSync('token')) {
      this.loadEnterpriseWechatConfig();
    }

    if (data.status === 'success') {
      void prefetchImages([
        ...(Array.isArray(data.resultImages) ? data.resultImages : []),
        ...(Array.isArray(data.reference_image_urls) ? data.reference_image_urls : []),
      ], 3);
      this.prefetchResultShareImages(Array.isArray(data.resultImages) ? data.resultImages : []);
    }
  },

  onShow() {
    const taskNo = String(this.data.taskData?.task_no || '').trim();
    const taskStatus = String(this.data.taskData?.status || '');
    if (this.data.showEnterpriseWechatModal && this.data.pendingDownloadType) {
      this.startEnterpriseWechatAutoCheck();
    }
    if (!taskNo) {
      return;
    }
    if (taskStatus === 'pending' || taskStatus === 'processing') {
      const restoredProgress = this.getStoredProgressState(taskNo);
      if (restoredProgress.percent > 0 || restoredProgress.loadingStageText) {
        this.setData({
          visualProgressPercent: Math.max(Number(this.data.visualProgressPercent || 0), restoredProgress.percent),
          loadingStageText: restoredProgress.loadingStageText || this.data.loadingStageText,
        });
      }
      this.startVisualProgress(taskStatus);
      this.startPolling(taskNo);
    }
  },

  buildProcessingStageText(status: string, progress: number): string {
    if (status === 'pending') {
      if (progress < 20) {
        return '任务已提交，正在准备生成环境...';
      }
      if (progress < 40) {
        return '当前排队中，马上开始为你出图...';
      }
      return '已经进入生成队列，请稍后查看结果...';
    }

    if (progress < 35) {
      return '正在分析你的描述和参考图...';
    }
    if (progress < 65) {
      return '正在生成主体画面和空间结构...';
    }
    if (progress < 90) {
      return '正在细化材质、光影和整体效果...';
    }
    return '即将完成，正在整理最终结果...';
  },

  getProgressStorageKey(taskNo?: string): string {
    const safeTaskNo = String(taskNo || '').trim();
    return safeTaskNo ? `${GENERATED_TASK_PROGRESS_STORAGE_PREFIX}${safeTaskNo}` : '';
  },

  getStoredProgressState(taskNo?: string): { percent: number; loadingStageText: string } {
    const storageKey = this.getProgressStorageKey(taskNo);
    if (!storageKey) {
      return { percent: 0, loadingStageText: '' };
    }
    try {
      const cache = wx.getStorageSync(storageKey);
      const percent = Number(cache?.percent || 0);
      return {
        percent: Number.isFinite(percent) && percent > 0 ? Math.min(percent, 92) : 0,
        loadingStageText: String(cache?.loadingStageText || ''),
      };
    } catch (error) {
      return { percent: 0, loadingStageText: '' };
    }
  },

  saveProgressState(percent: number, loadingStageText: string) {
    const storageKey = this.getProgressStorageKey(this.data.taskData?.task_no);
    if (!storageKey) {
      return;
    }
    try {
      wx.setStorageSync(storageKey, {
        percent,
        loadingStageText,
        updatedAt: Date.now(),
      });
    } catch (error) {
    }
  },

  clearProgressState(taskNo?: string) {
    const storageKey = this.getProgressStorageKey(taskNo || this.data.taskData?.task_no);
    if (!storageKey) {
      return;
    }
    try {
      wx.removeStorageSync(storageKey);
    } catch (error) {
    }
  },

  startVisualProgress(status: string) {
    const current = Number(this.data.visualProgressPercent || 0);
    const initial = current > 0 ? current : status === 'pending' ? 8 : 18;
    this.setData({
      visualProgressPercent: initial,
      loadingStageText: this.buildProcessingStageText(status, initial),
    });
    this.saveProgressState(initial, this.buildProcessingStageText(status, initial));

    if (this.progressTimer) {
      return;
    }

    this.progressTimer = setInterval(() => {
      const taskStatus = String(this.data.taskData?.status || 'pending');
      const progress = Number(this.data.visualProgressPercent || 0);
      const ceiling = taskStatus === 'pending' ? 48 : 92;
      if (progress >= ceiling) {
        return;
      }

      const step = progress < 24 ? 4 : progress < 56 ? 3 : progress < 80 ? 2 : 1;
      const next = Math.min(ceiling, progress + step);
      const nextStageText = this.buildProcessingStageText(taskStatus, next);
      this.setData({
        visualProgressPercent: next,
        loadingStageText: nextStageText,
      });
      this.saveProgressState(next, nextStageText);
    }, 900);
  },

  stopVisualProgress() {
    if (this.progressTimer) {
      clearInterval(this.progressTimer);
      this.progressTimer = null;
    }
  },

  completeVisualProgress() {
    this.stopVisualProgress();
    this.setData({
      visualProgressPercent: 100,
      loadingStageText: '已生成完成，正在展示效果图...',
    });
    this.clearProgressState();
  },

  async getAuthHeaders(body: string = '', path: string = '/api/v1/miniprogram/ai/task/status') {
    const token = wx.getStorageSync('token');
    if (!token) {
      throw new Error('未登录');
    }

    const params = generateRequestParams(token, body, path);
    return paramsToHeaders(params);
  },

  async loadTaskDetail(taskNo: string, silent: boolean = false) {
    const taskData = this.data.taskData;
    let taskType = 'ai_draw';
    if (taskData?.scene === 'ai_video' || taskData?.task_type === 'ai_video' || taskNo.startsWith('v')) {
      taskType = 'ai_video';
    } else if (taskData?.scene === 'ai_cost_doc' || taskNo.startsWith('cost_')) {
      taskType = 'ai_cost_doc';
    }
    try {
      const body = JSON.stringify({
        task_no: taskNo,
        task_type: taskType
      });
      const headers = await this.getAuthHeaders(body, '/api/v1/miniprogram/ai/task/status');

      wx.request({
        url: `${API_BASE_URL}/api/v1/miniprogram/ai/task/status`,
        method: 'POST',
        header: {
          ...headers,
          'Content-Type': 'application/json'
        },
        data: {
          task_no: taskNo,
          task_type: taskType
        },
        success: (res: any) => {
          if (res.data.code === 0) {
            const data = res.data.data;

            // 合并现有数据
            const mergedData: TaskData = {
              ...this.data.taskData,
              ...data,
              id: data.id || this.data.taskData.id || 0,
              created_at: data.created_at || this.data.taskData.created_at || '',
              updated_at: data.updated_at || this.data.taskData.updated_at || '',
              createdAtText: '',
              requested_count: data.requested_count ?? data.result?.requested_count,
              generated_count: data.generated_count ?? data.result?.generated_count,
              refunded_stones: data.refunded_stones ?? data.result?.refunded_stones,
              refunded_image_count: data.refunded_image_count ?? data.result?.refunded_image_count,
              stones_used: data.stones_used ?? data.result?.stones_used ?? this.data.taskData.stones_used,
              imageUrl: data.result?.url || data.result?.image_url || this.data.taskData.imageUrl,
              watermarkedImageUrl: data.result?.url || data.result?.image_url || this.data.taskData.watermarkedImageUrl,
              originalImageUrl: data.result?.url_raw ?? this.data.taskData.originalImageUrl,
              reference_image_url: data.reference_image_url ?? this.data.taskData.reference_image_url,
              reference_image_urls: Array.isArray(data.reference_image_urls) ? data.reference_image_urls : this.data.taskData.reference_image_urls,
              original_image_urls: Array.isArray(data.original_image_urls) ? data.original_image_urls : this.data.taskData.original_image_urls,
              ordered_image_urls: Array.isArray(data.ordered_image_urls) ? data.ordered_image_urls : this.data.taskData.ordered_image_urls,
              excel_url: data.result?.excel_url ?? this.data.taskData.excel_url,
            };

            this.setTaskData(mergedData);

            // 如果任务完成或失败，停止轮询
            if (data.status === 'success' || data.status === 'failed') {
              this.stopPolling();
            } else {
              this.scheduleNextPoll(taskNo);
            }
          } else if (silent) {
            this.scheduleNextPoll(taskNo);
          }
        },
        fail: (err) => {
          if (!silent) {
            console.error('加载任务详情失败:', err);
          } else {
            this.scheduleNextPoll(taskNo);
          }
        }
      });
    } catch (error) {
      if (!silent) {
        console.error('加载任务详情失败:', error);
        this.setData({ loading: false });
      } else {
        this.scheduleNextPoll(taskNo);
      }
    }
  },

  scheduleNextPoll(taskNo: string) {
    this.stopPolling();
    const delay = getTaskStatusPollDelay(this.pollingAttempt);
    this.pollingAttempt += 1;
    this.pollingTimer = setTimeout(() => {
      this.loadTaskDetail(taskNo, true);
    }, delay);
  },

  startPolling(taskNo: string) {
    this.stopPolling();
    this.pollingAttempt = 0;
    this.loadTaskDetail(taskNo, true);
  },

  stopPolling() {
    if (this.pollingTimer) {
      clearTimeout(this.pollingTimer);
      this.pollingTimer = null;
    }
  },

  persistCurrentProgressState() {
    const taskStatus = String(this.data.taskData?.status || '');
    if (taskStatus === 'success' || taskStatus === 'failed') {
      this.clearProgressState();
    } else if (taskStatus === 'pending' || taskStatus === 'processing') {
      this.saveProgressState(Number(this.data.visualProgressPercent || 0), String(this.data.loadingStageText || ''));
    }
  },

  onHide() {
    this.persistCurrentProgressState();
    this.stopPolling();
    this.stopVisualProgress();
    this.stopEnterpriseWechatAutoCheck();
  },

  onUnload() {
    this.persistCurrentProgressState();
    this.stopPolling();
    this.stopVisualProgress();
    this.stopEnterpriseWechatAutoCheck();
    if (this.resultRevealTimer) {
      clearTimeout(this.resultRevealTimer);
      this.resultRevealTimer = null;
    }
  },

  formatTime(timeStr: string): string {
    if (!timeStr) return '';
    const date = new Date(timeStr);
    if (Number.isNaN(date.getTime())) return '';
    const year = date.getFullYear();
    const month = (date.getMonth() + 1).toString().padStart(2, '0');
    const day = date.getDate().toString().padStart(2, '0');
    const hours = date.getHours().toString().padStart(2, '0');
    const minutes = date.getMinutes().toString().padStart(2, '0');
    return `${year}-${month}-${day} ${hours}:${minutes}`;
  },

  extractResultImages(data: TaskData): string[] {
    const result = data?.result || {};
    const originalImageUrl = this.getOriginalImageUrl(data);
    const values = [
      result.url,
      result.image_url,
      ...(Array.isArray(result.images) ? result.images : []),
      ...(Array.isArray(result.image_urls) ? result.image_urls : []),
      ...(Array.isArray(result.urls) ? result.urls : []),
      data.watermarkedImageUrl,
      data.imageUrl,
    ];

    return values.filter((item, index, array) => {
      const value = typeof item === 'string' ? item.trim() : '';
      return !!value && value !== originalImageUrl && array.findIndex((inner) => String(inner || '').trim() === value) === index;
    }) as string[];
  },

  extractReferenceImages(data: TaskData): string[] {
    const values = [
      ...(Array.isArray(data.reference_image_urls) ? data.reference_image_urls : []),
      data.reference_image_url,
    ];

    return values.filter((item, index, array) => {
      const value = typeof item === 'string' ? item.trim() : '';
      return !!value && array.findIndex((inner) => String(inner || '').trim() === value) === index;
    }) as string[];
  },

  getRequestedCount(data: TaskData): number {
    const resultValue = Number(data?.result?.requested_count);
    if (Number.isFinite(resultValue) && resultValue > 0) {
      return Math.floor(resultValue);
    }
    const directValue = Number(data.requested_count);
    if (Number.isFinite(directValue) && directValue > 0) {
      return Math.floor(directValue);
    }
    const imageCount = Array.isArray(data.resultImages) ? data.resultImages.length : 0;
    if (imageCount > 0) {
      return imageCount;
    }
    return 1;
  },

  getGeneratedCount(data: TaskData): number {
    const resultValue = Number(data?.result?.generated_count);
    if (Number.isFinite(resultValue) && resultValue >= 0) {
      return Math.floor(resultValue);
    }
    const directValue = Number(data.generated_count);
    if (Number.isFinite(directValue) && directValue >= 0) {
      return Math.floor(directValue);
    }
    const imageCount = Array.isArray(data.resultImages) ? data.resultImages.length : 0;
    if (imageCount > 0) {
      return imageCount;
    }
    return data.status === 'success' && data.imageUrl ? 1 : 0;
  },

  getRefundedStones(data: TaskData): number {
    const directValue = Number(data.refunded_stones);
    if (Number.isFinite(directValue) && directValue > 0) {
      return Math.floor(directValue);
    }
    const resultValue = Number(data?.result?.refunded_stones);
    if (Number.isFinite(resultValue) && resultValue > 0) {
      return Math.floor(resultValue);
    }
    return 0;
  },

  getRefundedImageCount(data: TaskData): number {
    const directValue = Number(data.refunded_image_count);
    if (Number.isFinite(directValue) && directValue > 0) {
      return Math.floor(directValue);
    }
    const resultValue = Number(data?.result?.refunded_image_count);
    if (Number.isFinite(resultValue) && resultValue > 0) {
      return Math.floor(resultValue);
    }
    return 0;
  },

  buildResultSummary(data: TaskData): string {
    const requestedCount = this.getRequestedCount(data);
    const generatedCount = this.getGeneratedCount(data);
    const refundedStones = this.getRefundedStones(data);
    const resultSettledStones = Number(data?.result?.stones_used);
    const directSettledStones = Number(data.stones_used);
    const settledStones = Number.isFinite(resultSettledStones)
      ? resultSettledStones
      : Number.isFinite(directSettledStones)
        ? directSettledStones
        : 0;
    if (data.status === 'pending' || data.status === 'processing') {
      return `本次计划生成${requestedCount}张效果图，请稍等片刻。`;
    }
    if (data.status === 'failed') {
      return refundedStones > 0
        ? `这次生成没有成功，已退回${refundedStones}灵石，你可以重新调整后再试一次。`
        : '这次生成没有成功，你可以重新调整描述再试一次。';
    }
    if (requestedCount > generatedCount && refundedStones > 0) {
      return `本次计划生成${requestedCount}张，成功生成${generatedCount}张，已退回${refundedStones}灵石，实际扣费${settledStones}灵石。`;
    }
    return `本次已成功生成${generatedCount}张效果图，实际扣费${settledStones}灵石。`;
  },

  buildResultImageInfos(data: TaskData): TaskImageInfo[] {
    const images = data.resultImages || [];
    const total = images.length;
    const sceneText = data.sceneText || 'AI效果图';
    return images.map((_, index) => ({
      title: total > 1 ? `效果图 ${index + 1}` : '效果图',
      subtitle: `${sceneText} · 当前查看第 ${index + 1} 张，共 ${total} 张，左右滑动可切换并下载当前图片。`,
    }));
  },

  getSafeImageInfo(imageInfos: TaskImageInfo[], index: number): TaskImageInfo {
    return imageInfos[index] || imageInfos[0] || {
      title: '效果图',
      subtitle: '左右滑动可切换不同图片。',
    };
  },

  onSelectResultTab(e: any) {
    const tab = e.currentTarget.dataset.tab === 'interior' ? 'interior' : 'exterior';
    this.setData({
      activeTab: tab,
    });
  },

  goBack() {
    wx.navigateBack({
      fail: () => {
        wx.reLaunch({
          url: '/pages/index/index'
        });
      }
    });
  },

  previewImage() {
    const { resultImages = [], imageUrl } = this.data.taskData;
    const urls = resultImages.length ? resultImages : (imageUrl ? [imageUrl] : []);
    if (urls.length) {
      this.openImagePreview(urls, this.data.currentResultImageIndex || 0);
    }
  },

  previewResultImage(e: any) {
    const { resultImages = [] } = this.data.taskData;
    const index = Number(e.currentTarget.dataset.index || 0);
    if (!resultImages.length) {
      return;
    }
    this.openImagePreview(resultImages, index);
  },

  onResultSwiperChange(e: any) {
    const current = Number(e.detail.current || 0);
    this.setData({
      currentResultImageIndex: current,
      currentResultImageInfo: this.getSafeImageInfo(this.data.resultImageInfos, current),
    });
    void this.prepareCurrentShareImage(this.getCurrentShareSourceUrl(current));
  },

  openImagePreview(urls: string[], index: number = 0, mode: PreviewImageMode = 'result') {
    if (!urls.length) {
      return;
    }
    const safeIndex = index >= 0 && index < urls.length ? index : 0;
    if (mode !== 'qrcode') {
      wx.previewImage({
        current: urls[safeIndex] || urls[0],
        urls,
        showmenu: false,
      });
      return;
    }
    this.setData({
      previewImageUrls: urls,
      previewImageIndex: safeIndex,
      currentPreviewImageInfo: mode === 'qrcode'
        ? {
            title: '企业微信二维码',
            subtitle: '长按二维码可直接识别，添加企业微信并留下手机号后会自动继续下载。',
          }
        : mode === 'reference'
        ? {
            title: `参考图 ${safeIndex + 1}`,
            subtitle: `当前查看第 ${safeIndex + 1} 张参考图，共 ${urls.length} 张，仅支持预览。`,
          }
        : this.getSafeImageInfo(this.data.resultImageInfos, safeIndex),
      previewImageMode: mode,
      downloadTargetImageIndex: safeIndex,
      showImagePreviewModal: true,
    });
  },

  closeImagePreviewModal() {
    this.setData({
      showImagePreviewModal: false,
      previewImageMode: 'result',
    });
  },

  onImagePreviewContentTap() {
  },

  onPreviewSwiperChange(e: any) {
    const current = Number(e.detail.current || 0);
    this.setData({
      previewImageIndex: current,
      currentPreviewImageInfo: this.data.previewImageMode === 'qrcode'
        ? {
            title: '企业微信二维码',
            subtitle: '长按二维码可直接识别，添加企业微信并留下手机号后会自动继续下载。',
          }
        : this.data.previewImageMode === 'reference'
        ? {
            title: `参考图 ${current + 1}`,
            subtitle: `当前查看第 ${current + 1} 张参考图，共 ${this.data.previewImageUrls.length} 张，仅支持预览。`,
          }
        : this.getSafeImageInfo(this.data.resultImageInfos, current),
    });
  },

  getCurrentPreviewImageUrl(): string {
    const { previewImageUrls = [], previewImageIndex = 0 } = this.data;
    return previewImageUrls[previewImageIndex] || previewImageUrls[0] || '';
  },

  getSceneText(data: TaskData, sceneMap: Record<string, string>): string {
    if (data.scene === 'ai_draw_single' || data.scene === 'ai_draw_multi') {
      const imageCount = Array.isArray(data.resultImages) ? data.resultImages.length : 0;
      return imageCount > 1 ? `AI绘画（${imageCount}张）` : 'AI绘画';
    }

    return sceneMap[data.scene] || data.scene;
  },

  saveCurrentPreviewImage() {
    this.setData({
      downloadTargetImageIndex: this.data.previewImageIndex,
      showImagePreviewModal: false,
    });
    this.onDownloadVerifiedImage(this.data.previewImageIndex);
  },

  openEnterpriseWechatFromPreview() {
    this.setData({
      showImagePreviewModal: false,
      downloadTargetImageIndex: this.data.previewImageIndex,
    });
    this.onDownloadVerifiedImage();
  },

  previewReferenceImage(e: any) {
    const urls = this.extractReferenceImages(this.data.taskData);
    const currentIndex = Number(e?.currentTarget?.dataset?.index || 0);
    if (urls.length) {
      this.openImagePreview(urls, currentIndex, 'reference');
    }
  },

  copyPrompt() {
    const { prompt, user_prompt } = this.data.taskData;
    const text = user_prompt || prompt;
    if (!text) {
      wx.showToast({ title: '没有可复制的提示词', icon: 'none' });
      return;
    }
    wx.setClipboardData({
      data: text,
      success: () => {
        wx.showToast({ title: '已复制', icon: 'success' });
      }
    });
  },

  /** AI造价(文档)：复制 Excel 下载链接 */
  copyExcelLink() {
    const url = this.data.taskData.excel_url;
    if (!url) {
      wx.showToast({ title: '暂无下载链接', icon: 'none' });
      return;
    }
    wx.setClipboardData({
      data: url,
      success: () => {
        wx.showToast({ title: '已复制，请在浏览器中打开下载', icon: 'success' });
      }
    });
  },

  getSimpleToken(): string {
    const token = wx.getStorageSync('token');
    if (!token) {
      throw new Error('请先登录');
    }
    return token;
  },

  async readDownloadErrorMessage(tempFilePath: string, fallback: string): Promise<string> {
    if (!tempFilePath) {
      return fallback;
    }
    try {
      const fileContent = await new Promise<string>((resolve, reject) => {
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
    } catch (error) {
      return fallback;
    }
  },

  async normalizeVideoFilePath(tempFilePath: string): Promise<string> {
    const originalPath = String(tempFilePath || '').trim();
    if (!originalPath) {
      throw new Error('视频文件不存在');
    }
    if (/\.(mp4|mov|m4v)$/i.test(originalPath)) {
      return originalPath;
    }
    const userDataPath = String(wx.env?.USER_DATA_PATH || '').trim();
    if (!userDataPath) {
      return originalPath;
    }
    const targetPath = `${userDataPath}/generated_video_${Date.now()}.mp4`;
    await new Promise<void>((resolve, reject) => {
      wx.getFileSystemManager().copyFile({
        srcPath: originalPath,
        destPath: targetPath,
        success: () => resolve(),
        fail: reject,
      });
    });
    return targetPath;
  },

  saveImageToAlbum(url: string, loadingTitle: string, successTitle: string, downloadHeader: Record<string, string> = {}) {
    if (!url) {
      wx.showToast({ title: '暂无可保存图片', icon: 'none' });
      return;
    }

    wx.showLoading({ title: loadingTitle });

    const saveResolvedFile = (filePath: string) => {
      wx.saveImageToPhotosAlbum({
        filePath,
        success: () => {
          wx.hideLoading();
          wx.showToast({ title: successTitle, icon: 'success' });
        },
        fail: (err) => {
          wx.hideLoading();
          console.error('保存失败:', err);
          if (err.errMsg.includes('auth deny')) {
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
          } else {
            wx.showToast({ title: '保存失败', icon: 'none' });
          }
        }
      });
    };

    if (!/^https?:\/\//i.test(url)) {
      wx.getImageInfo({
        src: url,
        success: (res) => {
          saveResolvedFile(res.path);
        },
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
        this.readDownloadErrorMessage(res.tempFilePath, '下载失败').then((message: string) => {
          wx.hideLoading();
          wx.showToast({ title: message, icon: 'none' });
        });
      },
      fail: (err: any) => {
        wx.hideLoading();
        console.error('下载失败:', err);
        wx.showToast({ title: String(err?.errMsg || '下载失败').includes('domain list') ? '下载域名未配置' : '下载失败', icon: 'none' });
      },
    });
  },

  saveImage() {
    this.setData({
      downloadTargetImageIndex: this.data.currentResultImageIndex,
    });
    this.onDownloadVerifiedImage(this.data.currentResultImageIndex);
  },

  hasEnterpriseWechatDownloadAccess(): boolean {
    return !!this.data.enterpriseWechatVerified && !!String(this.data.enterpriseWechatContact || '').trim();
  },

  queuePendingEnterpriseWechatDownload(type: PendingDownloadType, imageIndex?: number) {
    const nextData: Record<string, any> = {
      pendingDownloadType: type,
    };
    if (typeof imageIndex === 'number') {
      nextData.downloadTargetImageIndex = imageIndex;
    }
    this.setData(nextData);
  },

  clearPendingEnterpriseWechatDownload() {
    this.stopEnterpriseWechatAutoCheck();
    this.setData({
      pendingDownloadType: '',
      enterpriseWechatAutoChecking: false,
    });
  },

  startEnterpriseWechatAutoCheck() {
    if (!this.data.showEnterpriseWechatModal || !this.data.pendingDownloadType) {
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
        await this.resumePendingEnterpriseWechatDownload();
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

  resolveEnterpriseWechatQRCodeUrl(url?: string): string {
    const qrCodeUrl = String(url || '').trim();
    return qrCodeUrl || LOCAL_ENTERPRISE_WECHAT_QRCODE;
  },

  applyEnterpriseWechatConfig(configData: any = {}) {
    const verifiedAt = String(configData.enterprise_wechat_verified_at || '').trim();
    const qrcodeUrl = String(configData.enterprise_wechat_qrcode_url || '').trim() || String(this.data.enterpriseWechatQRCodeUrl || '').trim();
    const tipText = String(configData.enterprise_wechat_tip || '').trim() || this.data.enterpriseWechatTip;
    const contact = String(configData.enterprise_wechat_contact || '').trim() || String(this.data.enterpriseWechatContact || '').trim();
    const bindTicket = String(configData.enterprise_wechat_bind_ticket || '').trim() || String(this.data.enterpriseWechatBindTicket || '').trim();
    const bindStatus = String(configData.enterprise_wechat_bind_status || '').trim() || String(this.data.enterpriseWechatBindStatus || '').trim() || 'pending';
    this.setData({
      enterpriseWechatQRCodeUrl: this.resolveEnterpriseWechatQRCodeUrl(qrcodeUrl),
      enterpriseWechatTip: tipText,
      enterpriseWechatVerified: !!configData.enterprise_wechat_verified,
      enterpriseWechatVerifiedAtText: verifiedAt,
      enterpriseWechatContact: contact,
      enterpriseWechatBindTicket: bindTicket,
      enterpriseWechatBindStatus: bindStatus,
    });
  },

  async loadEnterpriseWechatConfig(force: boolean = false) {
    if ((this.data.enterpriseWechatConfigLoaded && !force) || this.data.enterpriseWechatConfigLoading) {
      return;
    }

    let token = '';
    try {
      token = this.getSimpleToken();
    } catch (error: any) {
      wx.showToast({ title: error.message || '请先登录', icon: 'none' });
      return;
    }

    this.setData({ enterpriseWechatConfigLoading: true });

    try {
      const res = await new Promise<any>((resolve, reject) => {
        wx.request({
          url: `${API_BASE_URL}/api/v1/miniprogram/user/ai/download-config`,
          method: 'GET',
          header: {
            token,
          },
          success: (requestRes) => {
            if (requestRes.statusCode === 200 && requestRes.data) {
              const data = requestRes.data as any;
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
    } catch (error) {
      console.error('加载企业微信二维码配置失败:', error);
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
    this.openImagePreview([url], 0, 'qrcode');
  },

  async refreshEnterpriseWechatVerification() {
    await this.loadEnterpriseWechatConfig(true);
    if (this.hasEnterpriseWechatDownloadAccess()) {
      wx.showToast({ title: '验证已完成，正在继续下载', icon: 'success' });
      await this.resumePendingEnterpriseWechatDownload();
      return;
    }
    wx.showToast({ title: '暂未检测到验证完成', icon: 'none' });
    this.startEnterpriseWechatAutoCheck();
  },

  async continuePendingEnterpriseWechatDownload() {
    if (!this.data.pendingDownloadType) {
      return;
    }
    await this.loadEnterpriseWechatConfig(true);
    if (!this.hasEnterpriseWechatDownloadAccess()) {
      this.setData({ showEnterpriseWechatModal: true });
      this.startEnterpriseWechatAutoCheck();
      return;
    }
    await this.resumePendingEnterpriseWechatDownload();
  },

  async verifyEnterpriseWechatPhoneCode(code: string) {
    const token = this.getSimpleToken();
    const result = await new Promise<any>((resolve, reject) => {
      wx.request({
        url: `${API_BASE_URL}/api/v1/miniprogram/user/ai/wecom/verify-phone`,
        method: 'POST',
        header: {
          token,
          'Content-Type': 'application/json',
        },
        data: {
          code,
          ticket: String(this.data.enterpriseWechatBindTicket || '').trim(),
        },
        success: (res) => {
          if (res.statusCode === 200 && res.data) {
            const data = res.data as any;
            if (data.code === 0) {
              resolve(data.data || {});
              return;
            }
            reject(new Error(data.msg || '手机号验证失败'));
            return;
          }
          reject(new Error(`请求失败: ${res.statusCode}`));
        },
        fail: reject,
      });
    });
    this.applyEnterpriseWechatConfig(result);
    return result;
  },

  async onEnterpriseWechatPhoneAuth(e: any) {
    const phoneCode = String(e?.detail?.code || '').trim();
    const errorMessage = String(e?.detail?.errMsg || '').trim();
    if (!phoneCode) {
      if (errorMessage.includes('deny') || errorMessage.includes('cancel')) {
        wx.showToast({ title: '你已取消手机号授权', icon: 'none' });
        return;
      }
      wx.showToast({ title: '未获取到手机号授权', icon: 'none' });
      return;
    }
    if (this.data.enterpriseWechatPhoneSubmitting) {
      return;
    }
    this.setData({ enterpriseWechatPhoneSubmitting: true });
    wx.showLoading({ title: '验证中...' });
    try {
      await this.verifyEnterpriseWechatPhoneCode(phoneCode);
      wx.hideLoading();
      this.stopEnterpriseWechatAutoCheck();
      wx.showToast({ title: '验证成功，正在继续下载', icon: 'success' });
      await this.resumePendingEnterpriseWechatDownload();
    } catch (error: any) {
      wx.hideLoading();
      wx.showToast({ title: String(error?.message || '手机号验证失败'), icon: 'none' });
    } finally {
      this.setData({ enterpriseWechatPhoneSubmitting: false });
    }
  },

  async resumePendingEnterpriseWechatDownload() {
    if (this.enterpriseWechatAutoResumeInProgress || !this.data.pendingDownloadType) {
      return;
    }
    this.enterpriseWechatAutoResumeInProgress = true;
    try {
      if (this.data.pendingDownloadType === 'video') {
        this.performAuthorizedVideoDownload();
        return;
      }
      await this.performAuthorizedImageDownload(Number(this.data.downloadTargetImageIndex || 0));
    } finally {
      this.enterpriseWechatAutoResumeInProgress = false;
    }
  },

  async requestOriginalDownloadUrl(imageIndex: number = 0): Promise<string> {
    const token = this.getSimpleToken();
    const taskNo = String(this.data.taskData.task_no || '').trim();
    if (!taskNo) {
      throw new Error('任务信息缺失');
    }

    const result = await new Promise<any>((resolve, reject) => {
      wx.request({
        url: `${API_BASE_URL}/api/v1/miniprogram/user/ai/task/download-original`,
        method: 'POST',
        header: {
          token,
          'Content-Type': 'application/json',
        },
        data: {
          task_no: taskNo,
          image_index: imageIndex,
        },
        success: (res) => {
          if (res.statusCode === 200 && res.data) {
            const data = res.data as any;
            if (data.code === 0) {
              resolve(data.data || {});
              return;
            }
            reject(new Error(data.msg || '获取下载地址失败'));
            return;
          }
          reject(new Error(`请求失败: ${res.statusCode}`));
        },
        fail: reject,
      });
    });

    const url = String(result.url || '').trim();
    if (!url) {
      throw new Error('未获取到下载地址');
    }
    return url;
  },

  async requestVideoDownloadUrl(): Promise<string> {
    const token = this.getSimpleToken();
    const taskNo = String(this.data.taskData.task_no || '').trim();
    if (!taskNo) {
      throw new Error('任务信息缺失');
    }

    const result = await new Promise<any>((resolve, reject) => {
      wx.request({
        url: `${API_BASE_URL}/api/v1/miniprogram/user/ai/video/download-original`,
        method: 'POST',
        header: {
          token,
          'Content-Type': 'application/json',
        },
        data: {
          task_no: taskNo,
        },
        success: (res) => {
          if (res.statusCode === 200 && res.data) {
            const data = res.data as any;
            if (data.code === 0) {
              resolve(data.data || {});
              return;
            }
            reject(new Error(data.msg || '获取视频下载地址失败'));
            return;
          }
          reject(new Error(`请求失败: ${res.statusCode}`));
        },
        fail: reject,
      });
    });

    const url = String(result.url || '').trim();
    if (!url) {
      throw new Error('未获取到视频下载地址');
    }
    return url;
  },

  buildImageDownloadProxyUrl(imageIndex: number = 0): string {
    const taskNo = encodeURIComponent(String(this.data.taskData.task_no || '').trim());
    const safeImageIndex = Number.isFinite(imageIndex) ? Math.max(0, Number(imageIndex)) : 0;
    return `${API_BASE_URL}/api/v1/miniprogram/user/ai/task/download-original-file?task_no=${taskNo}&image_index=${safeImageIndex}`;
  },

  buildVideoDownloadProxyUrl(): string {
    const taskNo = encodeURIComponent(String(this.data.taskData.task_no || '').trim());
    return `${API_BASE_URL}/api/v1/miniprogram/user/ai/video/download-original-file?task_no=${taskNo}`;
  },

  async performAuthorizedImageDownload(imageIndex: number = 0) {
    try {
      this.setData({ showEnterpriseWechatModal: false });
      wx.showLoading({ title: '校验下载权限中...' });
      const token = this.getSimpleToken();
      await this.requestOriginalDownloadUrl(imageIndex);
      const url = this.buildImageDownloadProxyUrl(imageIndex);
      wx.hideLoading();
      this.clearPendingEnterpriseWechatDownload();
      this.saveImageToAlbum(url, '保存图片中...', '图片已保存到相册', { token });
    } catch (error: any) {
      wx.hideLoading();
      const message = String(error?.message || '图片下载失败');
      wx.showToast({ title: message, icon: 'none' });
      if (message.includes('企业微信') || message.includes('手机号') || message.includes('联系方式')) {
        this.setData({ showEnterpriseWechatModal: true });
        this.startEnterpriseWechatAutoCheck();
        return;
      }
      this.clearPendingEnterpriseWechatDownload();
    }
  },

  async onDownloadVerifiedImage(imageIndex?: number) {
    const targetIndex = typeof imageIndex === 'number'
      ? imageIndex
      : Number(this.data.downloadTargetImageIndex || 0);
    this.queuePendingEnterpriseWechatDownload('image', targetIndex);
    await this.loadEnterpriseWechatConfig(true);
    if (!this.hasEnterpriseWechatDownloadAccess()) {
      this.setData({ showEnterpriseWechatModal: true });
      this.startEnterpriseWechatAutoCheck();
      return;
    }
    await this.performAuthorizedImageDownload(targetIndex);
  },

  saveEnterpriseWechatQRCode() {
    const url = this.data.enterpriseWechatQRCodeUrl;
    if (!url) {
      wx.showToast({ title: '暂未配置二维码', icon: 'none' });
      return;
    }
    this.saveImageToAlbum(url, '保存二维码中...', '二维码已保存');
  },

  getWatermarkedImageUrl(data: TaskData): string {
    const result = data?.result || {};
    const originalImageUrl = this.getOriginalImageUrl(data);
    const values = [
      result.url,
      result.image_url,
      ...(Array.isArray(result.images) ? result.images : []),
      ...(Array.isArray(result.image_urls) ? result.image_urls : []),
      ...(Array.isArray(result.urls) ? result.urls : []),
      data.watermarkedImageUrl,
      data.imageUrl,
    ];

    for (let index = 0; index < values.length; index += 1) {
      const value = typeof values[index] === 'string' ? values[index].trim() : '';
      if (value && value !== originalImageUrl) {
        return value;
      }
    }

    return '';
  },

  getOriginalImageUrl(data: TaskData): string {
    const result = data?.result || {};
    const value = typeof result.url_raw === 'string' ? result.url_raw.trim() : '';
    return value || (data.originalImageUrl || '');
  },

  getCurrentVideoUrl(): string {
    return String(this.data.taskData.imageUrl || this.data.taskData.result?.url || '').trim();
  },

  canDownloadByEnterpriseWechat(): boolean {
    return !!this.data.enterpriseWechatVerified && !!String(this.data.enterpriseWechatContact || '').trim();
  },

  getCurrentShareImageUrl(): string {
    return this.data.shareImageUrl || this.getCurrentShareSourceUrl();
  },

  getCurrentShareSourceUrl(index?: number): string {
    const { taskData } = this.data;
    const currentResultImageIndex = typeof index === 'number' ? Math.max(0, Number(index)) : Math.max(0, Number(this.data.currentResultImageIndex || 0));
    const resultImages = Array.isArray(taskData.resultImages) ? taskData.resultImages : [];
    const currentImage = String(resultImages[currentResultImageIndex] || '').trim();
    if (currentImage) {
      return currentImage;
    }
    return String(taskData.watermarkedImageUrl || taskData.imageUrl || resultImages[0] || '').trim();
  },

  async prepareCurrentShareImage(sourceUrl?: string) {
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
    const shareImageUrl = await prepareShareCardImage(shareSourceUrl);
    if (this.data.shareImageSourceUrl !== shareSourceUrl) {
      return;
    }
    if (shareImageUrl) {
      this.setData({ shareImageUrl });
    }
  },

  prefetchResultShareImages(urls: string[]) {
    const uniqueUrls = (Array.isArray(urls) ? urls : [])
      .map((item) => String(item || '').trim())
      .filter((item, index, array) => item && array.indexOf(item) === index)
      .slice(0, 6);
    uniqueUrls.forEach((item) => {
      prefetchShareCardImage(item);
    });
  },

  buildShareTitle(): string {
    const sourceText = String(this.data.taskData.user_prompt || this.data.taskData.prompt || '').replace(/\s+/g, ' ').trim();
    if (!sourceText) {
      return '我用甲第灵光生成了一张效果图，邀你一起试试';
    }
    const shortText = sourceText.length > 18 ? `${sourceText.slice(0, 18)}...` : sourceText;
    return `我用甲第灵光生成了「${shortText}」效果图`;
  },

  buildSharePath(): string {
    const prompt = String(this.data.taskData.user_prompt || this.data.taskData.prompt || '').trim();
    if (!prompt) {
      return '/pages/aigenerate/aigenerate';
    }
    return `/pages/aigenerate/aigenerate?prompt=${encodeURIComponent(prompt)}`;
  },

  onShareAppMessage() {
    return {
      title: this.buildShareTitle(),
      imageUrl: this.getCurrentShareImageUrl(),
      path: this.buildSharePath(),
    };
  },

  onShareTimeline() {
    return {
      title: this.buildShareTitle(),
      imageUrl: this.getCurrentShareImageUrl(),
    };
  },

  /** 视频类：下载视频到相册 */
  downloadVideo() {
    const { taskKind } = this.data.taskData;
    const videoUrl = this.getCurrentVideoUrl();
    if (taskKind !== 'video' || !videoUrl) {
      wx.showToast({ title: '暂无视频地址', icon: 'none' });
      return;
    }
    this.queuePendingEnterpriseWechatDownload('video');
    this.loadEnterpriseWechatConfig(true).then(() => {
      if (!this.hasEnterpriseWechatDownloadAccess()) {
        this.setData({ showEnterpriseWechatModal: true });
        this.startEnterpriseWechatAutoCheck();
        return;
      }
      this.performAuthorizedVideoDownload();
    });
  },

  performAuthorizedVideoDownload() {
    const currentVideoUrl = this.getCurrentVideoUrl();
    if (!currentVideoUrl) {
      this.clearPendingEnterpriseWechatDownload();
      wx.showToast({ title: '暂无视频地址', icon: 'none' });
      return;
    }
    this.setData({ showEnterpriseWechatModal: false });
    wx.showLoading({ title: '下载中...' });
    this.requestVideoDownloadUrl()
      .then(() => {
        const token = this.getSimpleToken();
        const videoUrl = this.buildVideoDownloadProxyUrl();
        wx.downloadFile({
          url: videoUrl,
          header: { token },
          success: (res) => {
            if (res.statusCode !== 200) {
              this.readDownloadErrorMessage(res.tempFilePath, '下载失败').then((message: string) => {
                wx.hideLoading();
                this.clearPendingEnterpriseWechatDownload();
                wx.showToast({ title: message, icon: 'none' });
              });
              return;
            }
            this.normalizeVideoFilePath(res.tempFilePath)
              .then((normalizedPath: string) => {
                wx.saveVideoToPhotosAlbum({
                  filePath: normalizedPath,
                  success: () => {
                    wx.hideLoading();
                    this.clearPendingEnterpriseWechatDownload();
                    wx.showToast({ title: '已保存到相册', icon: 'success' });
                  },
                  fail: (err: any) => {
                    wx.hideLoading();
                    this.clearPendingEnterpriseWechatDownload();
                    if (err.errMsg && err.errMsg.includes('auth deny')) {
                      wx.showModal({
                        title: '提示',
                        content: '需要授权相册权限才能保存视频',
                        confirmText: '去设置',
                        success: (r) => {
                          if (r.confirm) wx.openSetting();
                        },
                      });
                    } else {
                      wx.showToast({ title: String(err?.errMsg || '保存失败'), icon: 'none' });
                    }
                  },
                });
              })
              .catch((error: any) => {
                wx.hideLoading();
                this.clearPendingEnterpriseWechatDownload();
                wx.showToast({ title: String(error?.message || '处理视频文件失败'), icon: 'none' });
              });
          },
          fail: (err: any) => {
            wx.hideLoading();
            this.clearPendingEnterpriseWechatDownload();
            console.error('下载视频失败:', err);
            wx.showToast({ title: String(err?.errMsg || '下载失败').includes('domain list') ? '下载域名未配置' : '下载失败', icon: 'none' });
          },
        });
      })
      .catch((error: any) => {
        wx.hideLoading();
        const message = String(error?.message || '视频下载失败');
        wx.showToast({ title: message, icon: 'none' });
        if (message.includes('企业微信') || message.includes('手机号') || message.includes('联系方式')) {
          this.setData({ showEnterpriseWechatModal: true });
          this.startEnterpriseWechatAutoCheck();
          return;
        }
        this.clearPendingEnterpriseWechatDownload();
      });
  },

  onOpenCustomerServiceForResult(event?: WechatMiniprogram.TouchEvent) {
    const { taskData } = this.data
    const taskNo = String(taskData.task_no || taskData.id || '')
    const prompt = String(taskData.user_prompt || taskData.prompt || '').trim()
    const action = String(event?.currentTarget?.dataset?.action || 'consult').trim()
    const actionTextMap: Record<string, string> = {
      consult: '咨询建房顾问',
      layout: '调整户型布局',
      facade: '优化建筑外观',
      cost: '精准测算造价',
      wechat: '添加微信获取完整方案',
    }
    const actionText = actionTextMap[action] || actionTextMap.consult
    const resultImages = Array.isArray(taskData.resultImages) ? taskData.resultImages : []
    const imageUrl = String(
      resultImages[this.data.currentResultImageIndex] ||
      taskData.imageUrl ||
      taskData.watermarkedImageUrl ||
      taskData.originalImageUrl ||
      ''
    ).trim()
    const query = [
      'source=generatedetails',
      taskNo ? `task_no=${encodeURIComponent(taskNo)}` : '',
      prompt ? `prompt=${encodeURIComponent(prompt)}` : '',
      imageUrl ? `image_url=${encodeURIComponent(imageUrl)}` : '',
      action ? `action=${encodeURIComponent(action)}` : '',
      actionText ? `action_text=${encodeURIComponent(actionText)}` : '',
    ].filter(Boolean).join('&')
    wx.navigateTo({
      url: `/pages/customerservice/customerservice?${query}`,
      fail: () => wx.showToast({ title: '客服页面打开失败', icon: 'none' }),
    })
  },

  regenerate() {
    const { taskKind, id, prompt } = this.data.taskData;
    const effectiveEntrySource = resolveDetailEntrySource(this.data.entrySource, this.data.taskData);
    const sourceQuery = effectiveEntrySource
      ? `&source=${encodeURIComponent(effectiveEntrySource)}`
      : '';
    const extraQuery = `tab=${this.data.activeTab}&showSceneTabs=${this.data.showSceneTabs ? 1 : 0}${sourceQuery}`;
    const prefillPrompt = String(this.data.taskData.user_prompt || prompt || '').trim();
    const referenceImages = this.extractReferenceImages(this.data.taskData);
    const originalImages = Array.isArray(this.data.taskData.original_image_urls) ? this.data.taskData.original_image_urls : [];
    const orderedImages = Array.isArray(this.data.taskData.ordered_image_urls) && this.data.taskData.ordered_image_urls.length > 0
      ? this.data.taskData.ordered_image_urls
      : [...originalImages, ...referenceImages];
    const prefillData = {
      prompt: prefillPrompt,
      reference_image_url: referenceImages[0] || '',
      reference_image_urls: referenceImages,
      original_image_urls: originalImages,
      ordered_image_urls: orderedImages,
    };
    // 视频类跳转到 AI 视频页
    if (taskKind === 'video') {
      const q = id ? `taskId=${id}` : (prompt ? `prompt=${encodeURIComponent(prompt)}` : '');
      wx.navigateTo({
        url: `/pages/aivideo/aivideo${q ? '?' + q : ''}`,
      });
      return;
    }
    // 图片类跳转到生成页面
    if (id) {
      wx.navigateTo({
        url: `/pages/aigenerate/aigenerate?taskId=${id}&${extraQuery}`,
        success: (navRes) => {
          navRes.eventChannel.emit('prefillGenerateData', prefillData);
        },
      });
    } else {
      wx.navigateTo({
        url: `/pages/aigenerate/aigenerate?${extraQuery}${referenceImages[0] ? `&reference_image_url=${encodeURIComponent(referenceImages[0])}` : ''}`,
        success: (navRes) => {
          navRes.eventChannel.emit('prefillGenerateData', prefillData);
        },
      });
    }
  },

  publishToSquare() {
    const mainTabs = this.data.mainTabs || [];
    const allSubTabs = this.data.allSubTabs || [];
    const allThirdTabs = this.data.allThirdTabs || [];
    const isNewMinnanTool = this.data.taskData?.scene === 'new_minnan_tool_text_to_image' || this.data.taskData?.scene === 'new_minnan_tool_image_to_image';
    const preferredMainTab = isNewMinnanTool && mainTabs.some((item) => item.value === 'style') ? 'style' : '';
    const mainTabValue = preferredMainTab || this.data.publishForm.mainTab || mainTabs[0]?.value || '';
    const subTabs = getPublishSubTabsByParent(allSubTabs, mainTabValue);
    const preferredSubTab = isNewMinnanTool && subTabs.some((item) => item.value === 'new_minnan') ? 'new_minnan' : '';
    const currentSubIndex = subTabs.findIndex((item) => item.value === (preferredSubTab || this.data.publishForm.subTab));
    const nextSubIndex = currentSubIndex >= 0 ? currentSubIndex : (subTabs[0] ? 0 : -1);
    const nextSub = nextSubIndex >= 0 ? subTabs[nextSubIndex] : undefined;
    const thirdTabs = getPublishThirdTabsByParent(allThirdTabs, nextSub?.value || '');
    const currentThirdIndex = thirdTabs.findIndex((item) => item.value === this.data.publishForm.thirdTab);
    const nextThirdIndex = currentThirdIndex >= 0 ? currentThirdIndex : (thirdTabs[0] ? 0 : -1);
    const nextThird = nextThirdIndex >= 0 ? thirdTabs[nextThirdIndex] : undefined;
    // 预填充表单
    this.setData({
      showPublishModal: true,
      subTabs,
      thirdTabs,
      mainTabIndex: Math.max(mainTabs.findIndex((item) => item.value === mainTabValue), 0),
      subTabIndex: nextSubIndex,
      thirdTabIndex: nextThirdIndex,
      publishForm: {
        name: '',
        description: '',
        isFree: true,
        price: 0,
        mainTab: mainTabValue,
        subTab: nextSub?.value || '',
        thirdTab: nextThird?.value || '',
      }
    });
  },

  // 仅前端预览：将当前表单内容通过 Base64 编码传给预览页展示
  previewPublish() {
    const { publishForm, taskData } = this.data;
    const userInfo = wx.getStorageSync('userInfo') || {};
    const payload = {
      title: publishForm.name || '未命名模板',
      description: publishForm.description || '',
      imageUrl: taskData.imageUrl || '',
      userName: userInfo.username || userInfo.name || '预览用户',
      userAvatar: userInfo.avatar || '',
      createdAt: taskData.created_at || '',
    };
    try {
      const json = JSON.stringify(payload);
      const b64 = base64Encode(json);
      wx.navigateTo({
        url: `/pages/templatepreview/templatepreview?data=${encodeURIComponent(b64)}`,
        events: {
          previewConfirm: () => {
            // 预览页确认后回调，直接触发正式发布
            this.submitPublish();
          },
        },
      });
    } catch (e) {
      console.error('构造预览数据失败:', e);
      wx.showToast({
        title: '预览失败',
        icon: 'none',
      });
    }
  },

  closePublishModal() {
    this.setData({ showPublishModal: false });
  },

  /** 点击遮罩：不关闭弹窗，避免还没输入就误触消失；仅通过关闭按钮/取消关闭 */
  onPublishModalMaskTap() { },

  /** 点击弹窗内容区：阻止冒泡到遮罩 */
  onPublishContentTap() { },

  onPublishInput(e: any) {
    const { field } = e.currentTarget.dataset;
    const { value } = e.detail;
    this.setData({
      [`publishForm.${field}`]: value
    });
  },

  onMainTabChange(e: any) {
    const index = parseInt(e.detail.value, 10);
    const mainTabs = this.data.mainTabs || [];
    const main = mainTabs[index];
    const subTabs = getPublishSubTabsByParent(this.data.allSubTabs || [], main?.value || '');
    const firstSub = subTabs[0];
    const thirdTabs = getPublishThirdTabsByParent(this.data.allThirdTabs || [], firstSub?.value || '');
    const firstThird = thirdTabs[0];
    this.setData({
      mainTabIndex: index,
      'publishForm.mainTab': main ? main.value : '',
      subTabs,
      thirdTabs,
      subTabIndex: firstSub ? 0 : -1,
      thirdTabIndex: firstThird ? 0 : -1,
      'publishForm.subTab': firstSub?.value || '',
      'publishForm.thirdTab': firstThird?.value || '',
    });
  },

  onSubTabChange(e: any) {
    const index = parseInt(e.detail.value, 10);
    const subTabs = this.data.subTabs || [];
    const sub = subTabs[index];
    const thirdTabs = getPublishThirdTabsByParent(this.data.allThirdTabs || [], sub?.value || '');
    const firstThird = thirdTabs[0];
    this.setData({
      subTabIndex: index,
      thirdTabs,
      thirdTabIndex: firstThird ? 0 : -1,
      'publishForm.subTab': sub ? sub.value : '',
      'publishForm.thirdTab': firstThird?.value || '',
    });
  },

  onThirdTabChange(e: any) {
    const index = parseInt(e.detail.value, 10);
    const thirdTabs = this.data.thirdTabs || [];
    const third = thirdTabs[index];
    this.setData({
      thirdTabIndex: index,
      'publishForm.thirdTab': third ? third.value : '',
    });
  },

  onChargeTypeChange(e: any) {
    const free = e.currentTarget.dataset.free === 'true';
    this.setData({
      'publishForm.isFree': free,
      'publishForm.price': free ? 0 : (this.data.publishForm.price || 1),
    });
  },

  onPriceInput(e: any) {
    const v = parseInt(e.detail.value, 10);
    const price = isNaN(v) || v < 0 ? 0 : v;
    this.setData({
      'publishForm.price': price,
    });
  },

  async submitPublish() {
    const { publishForm, taskData } = this.data;

    if (!publishForm.name.trim()) {
      wx.showToast({ title: '请输入模板名称', icon: 'none' });
      return;
    }

    if (!publishForm.mainTab) {
      wx.showToast({ title: '请选择一级分类', icon: 'none' });
      return;
    }

    if ((this.data.subTabs || []).length > 0 && !publishForm.subTab) {
      wx.showToast({ title: '请选择二级分类', icon: 'none' });
      return;
    }

    if ((this.data.thirdTabs || []).length > 0 && !publishForm.thirdTab) {
      wx.showToast({ title: '请选择三级分类', icon: 'none' });
      return;
    }

    wx.showLoading({ title: '发布中...' });

    try {
      const isFree = !!publishForm.isFree;
      const price = isFree ? 0 : (publishForm.price > 0 ? publishForm.price : 1);
      const mappedCategory = publishForm.thirdTab || publishForm.subTab || publishForm.mainTab || 'scene';
      const hiddenPrompt = String(taskData.user_prompt || taskData.prompt || '').trim();
      const body = JSON.stringify({
        name: publishForm.name,
        description: publishForm.description,
        category: mappedCategory,
        main_tab: publishForm.mainTab || undefined,
        sub_tab: publishForm.subTab || undefined,
        third_tab: publishForm.thirdTab || undefined,
        image_url: taskData.imageUrl,
        prompt: hiddenPrompt,
        is_free: isFree,
        price,
        original_task_id: taskData.id || null, // 传递原始任务ID
      });
      const headers = await this.getAuthHeaders(body, '/api/v1/miniprogram/templates');

      wx.request({
        url: `${API_BASE_URL}/api/v1/miniprogram/templates`,
        method: 'POST',
        header: {
          ...headers,
          'Content-Type': 'application/json'
        },
        data: {
          name: publishForm.name,
          description: publishForm.description,
          category: mappedCategory,
          main_tab: publishForm.mainTab || undefined,
          sub_tab: publishForm.subTab || undefined,
          third_tab: publishForm.thirdTab || undefined,
          image_url: taskData.imageUrl,
          prompt: hiddenPrompt,
          is_free: isFree,
          price,
          original_task_id: taskData.id || null, // 传递原始任务ID
        },
        success: (res: any) => {
          if (res.data.code === 0) {
            this.closePublishModal();
            wx.showModal({
              title: '提示',
              content: '模板已发成功，等待管理员审核',
              showCancel: false,
              confirmText: '确定',
            });
          } else {
            wx.showToast({ title: res.data.msg || '发布失败', icon: 'none' });
          }
        },
        fail: () => {
          wx.showToast({ title: '网络错误', icon: 'none' });
        },
        complete: () => {
          wx.hideLoading();
        }
      });
    } catch (error) {
      wx.hideLoading();
      wx.showToast({ title: '发布失败', icon: 'none' });
    }
  },
});
