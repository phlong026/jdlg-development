// pages/templatepreview/templatepreview.ts

import { normalizeCosUrl } from '../../utils/asset';

function base64Decode(input: string): string {
  const chars = 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/=';
  let str = String(input).replace(/=+$/, '');
  if (str.length % 4 === 1) {
    throw new Error('Invalid base64 string');
  }
  let output = '';
  let bc = 0;
  let bs: number | undefined;
  let buffer: number | undefined;
  let idx = 0;
  while ((buffer = str.charCodeAt(idx++))) {
    const charIndex = chars.indexOf(String.fromCharCode(buffer));
    if (~charIndex) {
      bs = bc % 4 ? (bs as number) * 64 + charIndex : charIndex;
      if (bc++ % 4) {
        output += String.fromCharCode(255 & ((bs as number) >> ((-2 * bc) & 6)));
      }
    }
  }
  try {
    return decodeURIComponent(escape(output));
  } catch {
    return output;
  }
}

const PREVIEW_HORIZONTAL_MARGIN_RPX = 48;
const DEFAULT_PREVIEW_IMAGE_HEIGHT_PX = 500;

function normalizePositiveNumber(value: unknown): number {
  const num = Number(value);
  if (!Number.isFinite(num) || num <= 0) {
    return 0;
  }
  return Math.round(num);
}

Page({
  data: {
    title: '',
    description: '',
    imageUrl: '',
    userName: '',
    userAvatar: '',
    createdAt: '',
    readOnly: false,
    imageHeight: DEFAULT_PREVIEW_IMAGE_HEIGHT_PX,
  },

  onLoad(options: any) {
    const payload = options && options.data;
    const readOnly = options && (options.readonly === '1' || options.readonly === 'true');
    this.setData({
      readOnly,
    });
    if (payload) {
      try {
        const jsonStr = base64Decode(decodeURIComponent(payload));
        const data = JSON.parse(jsonStr);
        const imageUrl = normalizeCosUrl(String(data.imageUrl || ''));
        this.setData({
          title: data.title || '',
          description: data.description || '',
          imageUrl,
          userName: data.userName || '',
          userAvatar: data.userAvatar || '',
          createdAt: data.createdAt || '',
        });
        this.updatePreviewImageHeight(imageUrl, Number(data.imageWidth || 0), Number(data.imageHeight || 0));
      } catch (e) {
        console.error('预览数据解析失败:', e);
      }
    }
  },

  getPreviewContainerWidthPx(): number {
    try {
      const systemInfo = wx.getSystemInfoSync();
      const windowWidth = Number(systemInfo.windowWidth || 375);
      return Math.max(1, Math.round(windowWidth * (750 - PREVIEW_HORIZONTAL_MARGIN_RPX) / 750));
    } catch (error) {
      return 351;
    }
  },

  computePreviewImageHeight(width?: number, height?: number): number {
    const normalizedWidth = normalizePositiveNumber(width);
    const normalizedHeight = normalizePositiveNumber(height);
    if (!normalizedWidth || !normalizedHeight) {
      return DEFAULT_PREVIEW_IMAGE_HEIGHT_PX;
    }
    return Math.max(1, Math.round(this.getPreviewContainerWidthPx() * normalizedHeight / normalizedWidth));
  },

  updatePreviewImageHeight(url: string, width?: number, height?: number) {
    this.setData({
      imageHeight: this.computePreviewImageHeight(width, height),
    });

    const imageUrl = normalizeCosUrl(String(url || '').trim());
    if (!imageUrl) {
      return;
    }

    wx.getImageInfo({
      src: imageUrl,
      success: (res) => {
        this.setData({
          imageHeight: this.computePreviewImageHeight(res?.width, res?.height),
        });
      },
    });
  },

  onBack() {
    wx.navigateBack({ delta: 1 });
  },

  onConfirm() {
    if (this.data.readOnly) {
      wx.navigateBack({ delta: 1 });
      return;
    }
    const eventChannel = this.getOpenerEventChannel && this.getOpenerEventChannel();
    if (eventChannel) {
      eventChannel.emit('previewConfirm');
    }
    wx.navigateBack({ delta: 1 });
  },

  onPreviewImage() {
    const url = normalizeCosUrl(String(this.data.imageUrl || ''));
    if (!url) return;
    wx.previewImage({
      urls: [url],
      current: url,
      showmenu: false,
    });
  },

  onShareAppMessage() {
    return {
      title: this.data.title || '甲第灵光 · 模板预览',
      path: '/pages/template/template',
    };
  },
});

