// pages/Invitefriends/Invitefriends.ts
import { generateRequestParams, paramsToHeaders } from '../../utils/parameter';
import {
  generateDeviceFingerprint,
  getCachedDeviceFingerprint,
  cacheDeviceFingerprint,
} from '../../utils/deviceFingerprint';
import { resolveAssetPath } from '../../utils/asset';
import { prepareShareCardImage } from '../../utils/shareImage';

const API_BASE_URL = 'https://api.jiadilingguang.com';
const INVITE_POSTER_LOGO = '/pages/Invitefriends/intro-logo.png';
const DEFAULT_INVITE_AVATAR = resolveAssetPath('/assets/images/home.jpg');

interface InviteRecord {
  id: number;
  name: string;
  avatar: string;
  desc: string;
  reward: number;
  time: string;
}

Page({
  data: {
    token: '',
    deviceId: '',
    userInfo: null as any,
    returnPage: '/pages/index/index',

    // 邀请数据（邀请码由后端返回 6 位唯一码）
    inviteCode: '',
    inviteCount: 0,
    totalReward: 0,
    monthReward: 0,

    // 核销：输入他人邀请码
    redeemCodeInput: '',

    // 邀请记录
    records: [] as InviteRecord[],

    // 海报相关
    showPosterModal: false,
    posterImageUrl: '',
    shareImageUrl: '',
    posterLogoUrl: INVITE_POSTER_LOGO,
    posterLogoOverlayVisible: false,
    canvasWidth: 750,
    canvasHeight: 1000,
  },

  async onLoad(options: Record<string, string>) {
    this.setData({
      returnPage: this.resolveReturnPage(options?.source),
    });
    await this.initDeviceId();
    this.initToken();
    this.initUserInfo();
    await this.loadInviteData();
    await this.loadInviteRecords();
  },

  resolveReturnPage(source?: string) {
    const pageMap: Record<string, string> = {
      index: '/pages/index/index',
      my: '/pages/my/my',
    };

    return pageMap[source || ''] || '/pages/index/index';
  },

  async initDeviceId() {
    let deviceId = getCachedDeviceFingerprint();
    if (!deviceId) {
      try {
        deviceId = await generateDeviceFingerprint();
        if (deviceId) {
          cacheDeviceFingerprint(deviceId);
        }
      } catch (e) {
        console.error('获取设备ID失败:', e);
      }
    }
    this.setData({ deviceId: deviceId || '' });
  },

  initToken() {
    const token = wx.getStorageSync('token');
    this.setData({ token: token || '' });
  },

  initUserInfo() {
    const userInfo = wx.getStorageSync('userInfo');
    this.setData({ userInfo: userInfo || null });
  },

  getAuthHeaders(apiPath: string, body: any = {}) {
    const token = this.data.token;
    if (!token) return null;
    const params = generateRequestParams(token, body, apiPath, this.data.deviceId);
    return {
      ...paramsToHeaders(params),
      'Content-Type': 'application/json',
    };
  },

  // 加载邀请数据
  async loadInviteData() {
    const token = this.data.token;
    if (!token) {
      this.setData({ inviteCode: '', inviteCount: 0, totalReward: 0, monthReward: 0 });
      return;
    }

    try {
      const apiPath = '/api/v1/miniprogram/user/invite/info';
      const headers = this.getAuthHeaders(apiPath, {});
      if (!headers) return;

      const data = await new Promise<any>((resolve, reject) => {
        wx.request({
          url: `${API_BASE_URL}${apiPath}`,
          method: 'GET',
          header: headers,
          success: (res) => {
            if (res.statusCode === 200 && res.data) {
              const d = res.data as any;
              if (d.code === 0) {
                resolve(d.data);
              } else {
                reject(new Error(d.msg || '获取邀请信息失败'));
              }
            } else {
              reject(new Error(`请求失败: ${res.statusCode}`));
            }
          },
          fail: reject,
        });
      });

      this.setData({
        inviteCode: data.invite_code || '',
        inviteCount: data.invite_count || 0,
        totalReward: data.total_reward || 0,
        monthReward: data.month_reward || 0,
      });
    } catch (err) {
      console.error('获取邀请信息失败:', err);
      this.setData({ inviteCode: '', inviteCount: 0, totalReward: 0, monthReward: 0 });
    }
  },

  onRedeemCodeInput(e: any) {
    this.setData({ redeemCodeInput: (e.detail.value || '').trim().toUpperCase() });
  },

  async onRedeemCode() {
    const code = this.data.redeemCodeInput.trim();
    if (!code) {
      wx.showToast({ title: '请输入邀请码', icon: 'none' });
      return;
    }
    if (!this.data.token) {
      wx.showToast({ title: '请先登录', icon: 'none' });
      return;
    }
    wx.showLoading({ title: '核销中...', mask: true });
    try {
      const apiPath = '/api/v1/miniprogram/user/invite/bind';
      const headers = this.getAuthHeaders(apiPath, { invite_code: code });
      if (!headers) {
        wx.hideLoading();
        wx.showToast({ title: '请先登录', icon: 'none' });
        return;
      }
      const res = await new Promise<any>((resolve, reject) => {
        wx.request({
          url: `${API_BASE_URL}${apiPath}`,
          method: 'POST',
          header: { ...headers, 'Content-Type': 'application/json' },
          data: { invite_code: code },
          success: (r) => resolve(r.data),
          fail: reject,
        });
      });
      wx.hideLoading();
      const d = res as any;
      if (d.code === 0) {
        wx.showToast({ title: d.msg || '绑定成功', icon: 'success' });
        this.setData({ redeemCodeInput: '' });
        await this.loadInviteData();
      } else {
        wx.showToast({ title: d.msg || '核销失败', icon: 'none' });
      }
    } catch (err) {
      wx.hideLoading();
      wx.showToast({ title: '核销失败', icon: 'none' });
    }
  },

  // 加载邀请记录
  async loadInviteRecords() {
    const token = this.data.token;
    if (!token) return;

    try {
      const apiPath = '/api/v1/miniprogram/user/invite/records';
      const headers = this.getAuthHeaders(apiPath, {});
      if (!headers) return;

      const data = await new Promise<any>((resolve, reject) => {
        wx.request({
          url: `${API_BASE_URL}${apiPath}`,
          method: 'GET',
          header: headers,
          success: (res) => {
            if (res.statusCode === 200 && res.data) {
              const d = res.data as any;
              if (d.code === 0) {
                resolve(d.data);
              } else {
                reject(new Error(d.msg || '获取邀请记录失败'));
              }
            } else {
              reject(new Error(`请求失败: ${res.statusCode}`));
            }
          },
          fail: reject,
        });
      });

      const records: InviteRecord[] = (data.list || []).map((item: any) => ({
        id: item.id || 0,
        name: item.name || '好友',
        avatar: item.avatar || DEFAULT_INVITE_AVATAR,
        desc: item.desc || '已注册',
        reward: item.reward || 0,
        time: item.time || '',
      }));

      this.setData({ records });
    } catch (err) {
      console.error('获取邀请记录失败:', err);
    }
  },

  // 生成邀请码（如果后端没有提供）
  generateInviteCode(): string {
    const userInfo = this.data.userInfo;
    if (userInfo && userInfo.id) {
      // 基于用户ID生成6位邀请码
      const code = String(userInfo.id).padStart(6, '0');
      return code;
    }
    // 随机生成6位数字
    return Math.floor(100000 + Math.random() * 900000).toString();
  },

  // 复制邀请码
  onCopyCode() {
    const code = this.data.inviteCode;
    wx.setClipboardData({
      data: code,
      success: () => {
        wx.showToast({
          title: '邀请码已复制',
          icon: 'success',
        });
      },
    });
  },

  // 生成海报：后端返回 SVG data URL，前端用 canvas 转为 PNG 再展示/保存/分享
  async onGeneratePoster() {
    wx.showLoading({
      title: '生成海报中...',
      mask: true,
    });

    try {
      this.setData({
        showPosterModal: true,
        posterImageUrl: '',
        posterLogoOverlayVisible: false,
        canvasWidth: 750,
        canvasHeight: 1000,
      });
      await new Promise<void>((resolve) => {
        wx.nextTick(() => resolve());
      });
      const posterImageUrl = await this.drawPoster();
      this.setData({
        posterImageUrl,
        shareImageUrl: posterImageUrl,
      });
      void prepareShareCardImage(posterImageUrl).then((shareImageUrl) => {
        if (shareImageUrl) {
          this.setData({ shareImageUrl });
        }
      });
    } catch (err) {
      console.error('生成海报失败:', err);
      wx.showToast({
        title: (err as Error)?.message || '生成海报失败',
        icon: 'none',
      });
    } finally {
      wx.hideLoading();
    }
  },

  // 将 SVG data URL 绘制到 canvas 并导出为 PNG，设置 posterImageUrl 为本地临时路径
  drawSvgToCanvasAndSetPoster(posterSvgDataUrl: string) {
    const width = 750;
    const height = 1000;
    const query = wx.createSelectorQuery().in(this);
    query
      .select('#posterCanvas')
      .fields({ node: true, size: true })
      .exec((res) => {
        if (!res[0] || !res[0].node) {
          wx.showToast({ title: 'Canvas 未就绪', icon: 'none' });
          return;
        }
        const canvas = res[0].node as any;
        const ctx = canvas.getContext('2d');
        if (!ctx) {
          wx.showToast({ title: 'Canvas 2D 不可用', icon: 'none' });
          return;
        }
        const dpr = wx.getSystemInfoSync().pixelRatio || 2;
        canvas.width = width * dpr;
        canvas.height = height * dpr;
        ctx.scale(dpr, dpr);

        const img = canvas.createImage();
        img.onload = () => {
          ctx.drawImage(img as any, 0, 0, width, height);
          wx.canvasToTempFilePath({
            canvas,
            fileType: 'png',
            success: (r) => {
              if (r.tempFilePath) {
                this.setData({ posterImageUrl: r.tempFilePath, canvasWidth: width, canvasHeight: height });
              } else {
                wx.showToast({ title: '导出海报失败', icon: 'none' });
              }
            },
            fail: (err) => {
              console.error('canvasToTempFilePath fail', err);
              wx.showToast({ title: '导出海报失败', icon: 'none' });
            },
          });
        };
        img.onerror = () => {
          wx.showToast({ title: '海报图片加载失败', icon: 'none' });
        };
        img.src = posterSvgDataUrl;
      });
  },

  // 获取二维码图片（仅保留供兼容，海报改由后端生成）
  async getQRCodeImage(): Promise<string | null> {
    try {
      // 尝试从后端获取二维码
      const token = this.data.token;
      if (token) {
        const apiPath = '/api/v1/miniprogram/user/invite/qrcode';
        const headers = this.getAuthHeaders(apiPath, {});
        if (headers) {
          const data = await new Promise<any>((resolve, reject) => {
            wx.request({
              url: `${API_BASE_URL}${apiPath}`,
              method: 'GET',
              header: headers,
              success: (res) => {
                if (res.statusCode === 200 && res.data) {
                  const d = res.data as any;
                  if (d.code === 0) {
                    resolve(d.data);
                  } else {
                    reject(new Error(d.msg || '获取二维码失败'));
                  }
                } else {
                  reject(new Error(`请求失败: ${res.statusCode}`));
                }
              },
              fail: reject,
            });
          });
          return data.qrcode_url || null;
        }
      }
    } catch (err) {
      console.error('获取二维码失败:', err);
    }
    return null;
  },

  // 绘制海报
  async drawPoster(): Promise<string> {
    return new Promise(async (resolve, reject) => {
      try {
        const query = wx.createSelectorQuery().in(this);
        query
          .select('#posterCanvas')
          .fields({ node: true, size: true })
          .exec(async (res) => {
            if (!res[0] || !res[0].node) {
              reject(new Error('Canvas节点不存在'));
              return;
            }

            const canvas = res[0].node as any;
            const ctx = canvas.getContext('2d') as any;

            const dpr = wx.getSystemInfoSync().pixelRatio || 2;
            const width = this.data.canvasWidth;
            const height = this.data.canvasHeight;

            canvas.width = width * dpr;
            canvas.height = height * dpr;
            ctx.scale(dpr, dpr);

            const pageBackground = ctx.createLinearGradient(0, 0, 0, height);
            pageBackground.addColorStop(0, '#e6daca');
            pageBackground.addColorStop(1, '#ece4d9');
            ctx.fillStyle = pageBackground;
            ctx.fillRect(0, 0, width, height);

            // ========== 柔光氛围层 ==========
            ctx.save();
            const topGlow = ctx.createRadialGradient(width / 2, 380, 40, width / 2, 380, 400);
            topGlow.addColorStop(0, 'rgba(255, 251, 245, 0.78)');
            topGlow.addColorStop(0.6, 'rgba(255, 248, 238, 0.26)');
            topGlow.addColorStop(1, 'rgba(255, 248, 238, 0)');
            ctx.fillStyle = topGlow;
            ctx.fillRect(0, 0, width, height);
            ctx.restore();

            this.drawPosterLightBeam(ctx, 108, 200, 68, 280, -20 * Math.PI / 180);
            this.drawPosterLightBeam(ctx, width - 168, 196, 68, 280, 20 * Math.PI / 180);

            // ========== 外边框 ==========
            ctx.save();
            ctx.strokeStyle = 'rgba(255, 255, 255, 0.38)';
            ctx.lineWidth = 2;
            this.drawRoundedRect(ctx, 20, 20, width - 40, height - 40, 28);
            ctx.stroke();
            ctx.restore();

            // ========== 左上角引导文案（加大加粗） ==========
            ctx.save();
            ctx.fillStyle = '#6b4d38';
            ctx.font = '600 30px sans-serif';
            ctx.textAlign = 'left';
            ctx.textBaseline = 'top';
            ctx.fillText('拍照/说需求，一键出方案', 56, 52);
            ctx.restore();

            // ========== Logo 区（居中，整体下移） ==========
            const logoBoxSize = 340;
            const logoBoxX = (width - logoBoxSize) / 2;
            const logoBoxY = 220;

            ctx.save();
            const logoGlow = ctx.createRadialGradient(width / 2, logoBoxY + logoBoxSize / 2, 30, width / 2, logoBoxY + logoBoxSize / 2, 220);
            logoGlow.addColorStop(0, 'rgba(255, 249, 241, 0.92)');
            logoGlow.addColorStop(0.55, 'rgba(255, 245, 233, 0.4)');
            logoGlow.addColorStop(1, 'rgba(255, 245, 233, 0)');
            ctx.fillStyle = logoGlow;
            ctx.fillRect(logoBoxX - 40, logoBoxY - 30, logoBoxSize + 80, logoBoxSize + 60);
            ctx.restore();

            // Logo 绘制（增加多路径回退，确保不丢失）
            try {
              const logoInfo = await this.getImageInfo(INVITE_POSTER_LOGO);
              // 尝试方式1：通过安全路径加载
              let logoImage: any;
              try {
                const safeLogoPath = await this.getCanvasSafeImagePath(INVITE_POSTER_LOGO, 'invite-poster-logo.png');
                logoImage = await this.loadCanvasImage(canvas, safeLogoPath);
              } catch (_e1) {
                console.warn('Logo 安全路径加载失败，尝试原始路径');
                // 尝试方式2：直接用 getImageInfo 返回的 path
                try {
                  logoImage = await this.loadCanvasImage(canvas, logoInfo.path);
                } catch (_e2) {
                  console.warn('Logo 原始路径加载失败，尝试资源路径');
                  // 尝试方式3：直接用资源常量路径
                  logoImage = await this.loadCanvasImage(canvas, INVITE_POSTER_LOGO);
                }
              }
              const logoNaturalWidth = Number(logoInfo.width || logoImage.width || 960);
              const logoNaturalHeight = Number(logoInfo.height || logoImage.height || 960);
              const logoScale = Math.min((logoBoxSize - 60) / logoNaturalWidth, (logoBoxSize - 60) / logoNaturalHeight);
              const logoWidth = logoNaturalWidth * logoScale;
              const logoHeight = logoNaturalHeight * logoScale;
              const logoX = logoBoxX + (logoBoxSize - logoWidth) / 2;
              const logoY = logoBoxY + (logoBoxSize - logoHeight) / 2;
              ctx.save();
              ctx.shadowColor = 'rgba(153, 117, 89, 0.16)';
              ctx.shadowBlur = 12;
              ctx.shadowOffsetY = 4;
              ctx.drawImage(logoImage, logoX, logoY, logoWidth, logoHeight);
              ctx.restore();
              this.setData({ posterLogoOverlayVisible: false });
            } catch (e) {
              console.error('海报 logo 全部加载方式均失败:', e);
              this.setData({ posterLogoOverlayVisible: true });
            }

            // ========== Slogan 胶囊按钮（紧跟 Logo 下方） ==========
            const sloganY = logoBoxY + logoBoxSize + 36;
            const sloganWidth = 380;
            const sloganHeight = 72;
            const sloganX = (width - sloganWidth) / 2;
            const sloganGradient = ctx.createLinearGradient(sloganX, sloganY, sloganX + sloganWidth, sloganY);
            sloganGradient.addColorStop(0, '#e6a24f');
            sloganGradient.addColorStop(1, '#9e7a5d');
            ctx.save();
            ctx.shadowColor = 'rgba(120, 89, 62, 0.14)';
            ctx.shadowBlur = 16;
            ctx.shadowOffsetY = 6;
            this.drawRoundedRect(ctx, sloganX, sloganY, sloganWidth, sloganHeight, sloganHeight / 2);
            ctx.fillStyle = sloganGradient;
            ctx.fill();
            ctx.restore();

            ctx.fillStyle = '#fffdf8';
            ctx.textAlign = 'center';
            ctx.textBaseline = 'middle';
            ctx.font = '600 30px sans-serif';
            ctx.fillText('设计师的口袋神器', width / 2, sloganY + sloganHeight / 2 + 1);

            // ========== 底部区：左文案 + 右二维码（水平对齐） ==========
            const bottomY = height - 180;
            const qrSize = 120;
            const qrFrameSize = 140;
            const qrFrameX = width - 60 - qrFrameSize;
            const qrFrameY = bottomY;
            const qrX = qrFrameX + (qrFrameSize - qrSize) / 2;
            const qrY = qrFrameY + (qrFrameSize - qrSize) / 2;

            // 二维码白底框
            ctx.save();
            ctx.shadowColor = 'rgba(120, 89, 62, 0.1)';
            ctx.shadowBlur = 14;
            ctx.shadowOffsetY = 6;
            this.drawRoundedRect(ctx, qrFrameX, qrFrameY, qrFrameSize, qrFrameSize, 22);
            ctx.fillStyle = 'rgba(255, 252, 247, 0.96)';
            ctx.fill();
            ctx.restore();
            ctx.save();
            this.drawRoundedRect(ctx, qrFrameX, qrFrameY, qrFrameSize, qrFrameSize, 22);
            ctx.strokeStyle = 'rgba(214, 192, 167, 0.5)';
            ctx.lineWidth = 1.5;
            ctx.stroke();
            ctx.restore();

            // 绘制二维码图片
            const qrCodeUrl = await this.getQRCodeImage();
            if (qrCodeUrl) {
              try {
                const safeQrPath = await this.getCanvasSafeImagePath(qrCodeUrl, 'invite-poster-qrcode.png');
                const qrImage = await this.loadCanvasImage(canvas, safeQrPath);
                ctx.drawImage(qrImage, qrX, qrY, qrSize, qrSize);
              } catch (error) {
                console.error('二维码绘制失败，已使用占位图:', error);
                this.drawQRCodePlaceholder(ctx, qrX, qrY, qrSize);
              }
            } else {
              this.drawQRCodePlaceholder(ctx, qrX, qrY, qrSize);
            }

            // 左侧文案（与二维码水平对齐）
            const textLeft = 60;
            const textCenterY = qrFrameY + qrFrameSize / 2;
            ctx.fillStyle = '#6b4d38';
            ctx.textAlign = 'left';
            ctx.textBaseline = 'middle';
            ctx.font = '600 28px sans-serif';
            ctx.fillText('扫码进入小程序', textLeft, textCenterY - 28);
            ctx.font = '400 18px sans-serif';
            ctx.fillStyle = 'rgba(94, 65, 47, 0.68)';
            ctx.fillText('立即体验智能设计方案', textLeft, textCenterY + 8);
            ctx.fillText(`邀请码：${this.data.inviteCode || '******'}`, textLeft, textCenterY + 38);

            let exportSettled = false;
            const exportTimer = setTimeout(() => {
              if (exportSettled) return;
              exportSettled = true;
              reject(new Error('导出图片超时'));
            }, 8000);

            wx.canvasToTempFilePath({
              canvas,
              fileType: 'png',
              success: (res) => {
                if (exportSettled) return;
                exportSettled = true;
                clearTimeout(exportTimer);
                if (res.tempFilePath) {
                  resolve(res.tempFilePath);
                } else {
                  reject(new Error('导出图片失败'));
                }
              },
              fail: (err) => {
                if (exportSettled) return;
                exportSettled = true;
                clearTimeout(exportTimer);
                console.error('导出图片失败:', err);
                reject(err);
              },
            });
          });
      } catch (err) {
        reject(err);
      }
    });
  },

  drawPosterLightBeam(ctx: any, x: number, y: number, width: number, height: number, angle: number) {
    ctx.save();
    ctx.translate(x, y);
    ctx.rotate(angle);
    ctx.globalAlpha = 0.7;
    ctx.shadowColor = 'rgba(255, 248, 237, 0.95)';
    ctx.shadowBlur = 30;
    this.drawRoundedRect(ctx, 0, 0, width, height, width / 2);
    ctx.fillStyle = 'rgba(255, 248, 237, 0.86)';
    ctx.fill();
    ctx.restore();
  },

  getImagePath(src: string): Promise<string> {
    return new Promise((resolve, reject) => {
      wx.getImageInfo({
        src,
        success: (res) => resolve(res.path),
        fail: reject,
      });
    });
  },

  getImageInfo(src: string): Promise<WechatMiniprogram.GetImageInfoSuccessCallbackResult> {
    return new Promise((resolve, reject) => {
      wx.getImageInfo({
        src,
        success: resolve,
        fail: reject,
      });
    });
  },

  loadCanvasImage(canvas: any, src: string): Promise<any> {
    return new Promise((resolve, reject) => {
      const image = canvas.createImage();
      image.onload = () => resolve(image);
      image.onerror = reject;
      image.src = src;
    });
  },

  getCanvasSafeImagePath(src: string, fileName: string): Promise<string> {
    return new Promise(async (resolve) => {
      try {
        const imageInfo = await this.getImageInfo(src);
        const sourcePath = imageInfo.path || src;
        const safePath = `${wx.env.USER_DATA_PATH}/${Date.now()}-${fileName}`;
        wx.getFileSystemManager().copyFile({
          srcPath: sourcePath,
          destPath: safePath,
          success: () => resolve(safePath),
          fail: () => resolve(sourcePath),
        });
      } catch (error) {
        console.error('准备 canvas 本地图片路径失败:', error);
        resolve(src);
      }
    });
  },

  drawRoundedRect(ctx: any, x: number, y: number, width: number, height: number, radius: number) {
    const safeRadius = Math.min(radius, width / 2, height / 2);
    ctx.beginPath();
    ctx.moveTo(x + safeRadius, y);
    ctx.lineTo(x + width - safeRadius, y);
    ctx.arcTo(x + width, y, x + width, y + safeRadius, safeRadius);
    ctx.lineTo(x + width, y + height - safeRadius);
    ctx.arcTo(x + width, y + height, x + width - safeRadius, y + height, safeRadius);
    ctx.lineTo(x + safeRadius, y + height);
    ctx.arcTo(x, y + height, x, y + height - safeRadius, safeRadius);
    ctx.lineTo(x, y + safeRadius);
    ctx.arcTo(x, y, x + safeRadius, y, safeRadius);
    ctx.closePath();
  },

  wrapPosterText(
    ctx: any,
    text: string,
    x: number,
    y: number,
    maxWidth: number,
    lineHeight: number,
    maxLines: number
  ) {
    const lines: string[] = [];
    let currentLine = '';
    for (const char of text) {
      const testLine = currentLine + char;
      if (ctx.measureText(testLine).width > maxWidth && currentLine) {
        lines.push(currentLine);
        currentLine = char;
        if (lines.length === maxLines - 1) {
          break;
        }
      } else {
        currentLine = testLine;
      }
    }
    if (currentLine) {
      lines.push(currentLine);
    }
    const result = lines.slice(0, maxLines).map((line, index, array) => {
      if (index === array.length - 1 && array.length === maxLines && line.length < text.length) {
        return `${line}...`;
      }
      return line;
    });
    result.forEach((line, index) => {
      ctx.fillText(line, x, y + index * lineHeight);
    });
    return result;
  },

  // 绘制二维码占位
  drawQRCodePlaceholder(ctx: any, x: number, y: number, size: number) {
    ctx.fillStyle = '#ffffff';
    ctx.fillRect(x, y, size, size);

    ctx.strokeStyle = '#c69d73';
    ctx.lineWidth = 3;
    ctx.strokeRect(x, y, size, size);

    const cellSize = size / 25;
    ctx.fillStyle = '#7a5a42';
    const cornerSize = 7;
    const corners = [
      [x, y],
      [x + size - cornerSize * cellSize, y],
      [x, y + size - cornerSize * cellSize],
    ];
    corners.forEach(([cx, cy]) => {
      ctx.fillRect(cx, cy, cornerSize * cellSize, cornerSize * cellSize);
      ctx.fillStyle = '#ffffff';
      ctx.fillRect(cx + cellSize, cy + cellSize, (cornerSize - 2) * cellSize, (cornerSize - 2) * cellSize);
      ctx.fillStyle = '#7a5a42';
      ctx.fillRect(cx + 2 * cellSize, cy + 2 * cellSize, (cornerSize - 4) * cellSize, (cornerSize - 4) * cellSize);
    });

    ctx.fillStyle = '#b68d69';
    ctx.font = '18px sans-serif';
    ctx.textAlign = 'center';
    ctx.fillText('小程序码', x + size / 2, y + size / 2 - 6);
    ctx.fillText('扫码进入', x + size / 2, y + size / 2 + 20);
  },

  // 关闭海报弹窗
  onClosePosterModal() {
    this.setData({ showPosterModal: false });
  },

  stopPropagation() {
    // 阻止事件冒泡
  },

  // 保存海报到相册（海报为网络 URL 时先下载到临时文件再保存）
  onSavePoster() {
    const imageUrl = this.data.posterImageUrl;
    if (!imageUrl) {
      wx.showToast({ title: '海报未生成', icon: 'none' });
      return;
    }
    const doSave = (filePath: string) => {
      wx.saveImageToPhotosAlbum({
        filePath,
        success: () => {
          wx.showToast({ title: '已保存到相册', icon: 'success' });
          this.setData({ showPosterModal: false });
        },
        fail: (err: any) => {
          if (err.errMsg && err.errMsg.includes('auth deny')) {
            wx.showModal({
              title: '需要相册权限',
              content: '保存海报需要访问您的相册，请在设置中开启',
              confirmText: '去设置',
              success: (res) => {
                if (res.confirm) wx.openSetting();
              },
            });
          } else {
            wx.showToast({ title: '保存失败', icon: 'none' });
          }
        },
      });
    };
    if (imageUrl.startsWith('http://') || imageUrl.startsWith('https://')) {
      wx.showLoading({ title: '保存中...', mask: true });
      wx.downloadFile({
        url: imageUrl,
        success: (res) => {
          wx.hideLoading();
          if (res.statusCode === 200 && res.tempFilePath) {
            doSave(res.tempFilePath);
          } else {
            wx.showToast({ title: '下载海报失败', icon: 'none' });
          }
        },
        fail: () => {
          wx.hideLoading();
          wx.showToast({ title: '下载海报失败', icon: 'none' });
        },
      });
    } else {
      doSave(imageUrl);
    }
  },

  // 分享海报：使用 wx.showShareImageMenu 打开分享图片弹窗（发好友/朋友圈/收藏/下载）
  onSharePoster() {
    const posterUrl = this.data.posterImageUrl;
    if (!posterUrl) {
      wx.showToast({ title: '海报未生成', icon: 'none' });
      return;
    }
    const showShareMenu = (filePath: string) => {
      if (typeof wx.showShareImageMenu !== 'function') {
        wx.showModal({
          title: '分享海报',
          content: '请点击右上角"..."选择转发，或将海报保存到相册后分享',
          showCancel: false,
        });
        return;
      }
      this.setData({ showPosterModal: false });
      const inviteCode = this.data.inviteCode || '';
      wx.showShareImageMenu({
        path: filePath,
        needShowEntrance: true,
        entrancePath: inviteCode ? `/pages/index/index?invite_code=${inviteCode}` : '/pages/index/index',
        success: () => {},
        fail: (err: any) => {
          wx.showToast({ title: err.errMsg || '分享失败', icon: 'none' });
        },
      });
    };
    if (posterUrl.startsWith('http://') || posterUrl.startsWith('https://')) {
      wx.showLoading({ title: '准备分享...', mask: true });
      wx.downloadFile({
        url: posterUrl,
        success: (res) => {
          wx.hideLoading();
          if (res.statusCode === 200 && res.tempFilePath) {
            showShareMenu(res.tempFilePath);
          } else {
            wx.showToast({ title: '下载海报失败', icon: 'none' });
          }
        },
        fail: () => {
          wx.hideLoading();
          wx.showToast({ title: '下载海报失败', icon: 'none' });
        },
      });
    } else {
      this.setData({ showPosterModal: false });
      showShareMenu(posterUrl);
    }
  },

  // 分享给微信好友
  onShareToFriend() {
    if (!this.data.token) {
      wx.showToast({
        title: '请先登录',
        icon: 'none',
      });
      return;
    }

    // 使用微信分享API - 直接触发分享
    // 注意：小程序中无法直接调用分享，需要通过按钮触发
    // 这里我们显示一个提示，引导用户使用右上角分享
    wx.showModal({
      title: '分享邀请',
      content: '请点击右上角的"..."按钮，选择"转发"分享给微信好友',
      showCancel: false,
      confirmText: '知道了',
    });
  },


  onShareAppMessage() {
    const inviteCode = this.data.inviteCode;
    const userInfo = this.data.userInfo;
    const nickname = userInfo?.nickname || '好友';
    
    // 如果有生成的海报，使用海报作为分享图片
    let imageUrl = this.data.shareImageUrl || this.data.posterImageUrl;
    
    return {
      title: `${nickname}邀请你使用甲第灵光AI，输入邀请码${inviteCode}即可获得50灵石奖励！`,
      path: `/pages/index/index?invite_code=${inviteCode}`,
      imageUrl: imageUrl || '', // 如果为空，微信会使用小程序默认图片
    };
  },

  // 分享到朋友圈（如果支持）
  onShareTimeline() {
    const inviteCode = this.data.inviteCode;
    const userInfo = this.data.userInfo;
    const nickname = userInfo?.nickname || '好友';
    
    return {
      title: `${nickname}邀请你使用甲第灵光AI，输入邀请码${inviteCode}即可获得50灵石奖励！`,
      imageUrl: this.data.shareImageUrl || this.data.posterImageUrl || '',
    };
  },
});
