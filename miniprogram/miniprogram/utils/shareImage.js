"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
exports.prepareShareCardImage = prepareShareCardImage;
exports.prefetchShareCardImage = prefetchShareCardImage;
const asset_1 = require("./asset");
const SHARE_CARD_WIDTH = 1000;
const SHARE_CARD_HEIGHT = 800;
const SHARE_CARD_OUTER_PADDING = 44;
const SHARE_CARD_INNER_PADDING = 24;
const SHARE_CARD_RADIUS = 30;
const SHARE_CARD_IMAGE_RADIUS = 22;
const shareImagePromiseCache = {};
const shareImageResultCache = {};
function buildShareImageCacheKey(sourceUrl) {
    let hash = 2166136261;
    for (let index = 0; index < sourceUrl.length; index += 1) {
        hash ^= sourceUrl.charCodeAt(index);
        hash = Math.imul(hash, 16777619);
    }
    return (hash >>> 0).toString(16);
}
function normalizeShareSourceUrl(sourceUrl) {
    return (0, asset_1.normalizeCosUrl)(String(sourceUrl || '').trim());
}
function ensureDirectory(dirPath) {
    return new Promise((resolve, reject) => {
        const fs = wx.getFileSystemManager();
        fs.mkdir({
            dirPath,
            recursive: true,
            success: () => resolve(),
            fail: (error) => {
                const errorMessage = String(error?.errMsg || '');
                if (errorMessage.includes('file already exists')) {
                    resolve();
                    return;
                }
                reject(error);
            },
        });
    });
}
function getImageInfo(src) {
    return new Promise((resolve, reject) => {
        wx.getImageInfo({
            src,
            success: resolve,
            fail: reject,
        });
    });
}
function loadCanvasImage(canvas, src) {
    return new Promise((resolve, reject) => {
        const image = canvas.createImage();
        image.onload = () => resolve(image);
        image.onerror = reject;
        image.src = src;
    });
}
function canvasToDataURL(canvas) {
    return new Promise((resolve, reject) => {
        canvas.toDataURL({
            type: 'image/jpeg',
            quality: 0.92,
            success: (res) => resolve(String(res?.data || '').trim()),
            fail: reject,
        });
    });
}
function writeBase64Image(cacheKey, dataUrl) {
    return new Promise(async (resolve, reject) => {
        try {
            const fs = wx.getFileSystemManager();
            const cleanDataUrl = String(dataUrl || '').trim();
            if (!cleanDataUrl) {
                resolve('');
                return;
            }
            const base64Payload = cleanDataUrl.includes(',') ? cleanDataUrl.split(',')[1] : cleanDataUrl;
            const dirPath = `${wx.env.USER_DATA_PATH}/share-images`;
            const filePath = `${dirPath}/${cacheKey}.jpg`;
            await ensureDirectory(dirPath);
            const base64ToArrayBuffer = wx.base64ToArrayBuffer;
            if (typeof base64ToArrayBuffer !== 'function') {
                resolve('');
                return;
            }
            fs.writeFile({
                filePath,
                data: base64ToArrayBuffer(base64Payload),
                success: () => resolve(filePath),
                fail: reject,
            });
        }
        catch (error) {
            reject(error);
        }
    });
}
function drawRoundedRect(ctx, x, y, width, height, radius) {
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
}
function computeContainSize(srcWidth, srcHeight, maxWidth, maxHeight) {
    if (srcWidth <= 0 || srcHeight <= 0 || maxWidth <= 0 || maxHeight <= 0) {
        return {
            width: maxWidth,
            height: maxHeight,
        };
    }
    const scale = Math.min(maxWidth / srcWidth, maxHeight / srcHeight);
    return {
        width: Math.max(1, Math.round(srcWidth * scale)),
        height: Math.max(1, Math.round(srcHeight * scale)),
    };
}
function computeCoverRect(srcWidth, srcHeight, dstWidth, dstHeight) {
    if (srcWidth <= 0 || srcHeight <= 0 || dstWidth <= 0 || dstHeight <= 0) {
        return {
            x: 0,
            y: 0,
            width: dstWidth,
            height: dstHeight,
        };
    }
    const scale = Math.max(dstWidth / srcWidth, dstHeight / srcHeight);
    const width = Math.max(1, Math.round(srcWidth * scale));
    const height = Math.max(1, Math.round(srcHeight * scale));
    return {
        x: Math.round((dstWidth - width) / 2),
        y: Math.round((dstHeight - height) / 2),
        width,
        height,
    };
}
async function createShareCardImage(sourceUrl) {
    if (typeof wx.createOffscreenCanvas === 'undefined') {
        return '';
    }
    const imageInfo = await getImageInfo(sourceUrl);
    const sourcePath = String(imageInfo.path || sourceUrl).trim();
    if (!sourcePath) {
        return '';
    }
    const createOffscreenCanvas = wx.createOffscreenCanvas;
    const canvas = createOffscreenCanvas({
        type: '2d',
        width: SHARE_CARD_WIDTH,
        height: SHARE_CARD_HEIGHT,
    });
    const ctx = canvas?.getContext('2d');
    if (!canvas || !ctx) {
        return '';
    }
    const image = await loadCanvasImage(canvas, sourcePath);
    const sourceWidth = Number(imageInfo.width || image.width || 0);
    const sourceHeight = Number(imageInfo.height || image.height || 0);
    const backgroundGradient = ctx.createLinearGradient(0, 0, SHARE_CARD_WIDTH, SHARE_CARD_HEIGHT);
    backgroundGradient.addColorStop(0, '#f5ecdf');
    backgroundGradient.addColorStop(1, '#e7d6c1');
    ctx.fillStyle = backgroundGradient;
    ctx.fillRect(0, 0, SHARE_CARD_WIDTH, SHARE_CARD_HEIGHT);
    const backgroundRect = computeCoverRect(sourceWidth, sourceHeight, SHARE_CARD_WIDTH, SHARE_CARD_HEIGHT);
    ctx.save();
    ctx.globalAlpha = 0.16;
    ctx.drawImage(image, backgroundRect.x, backgroundRect.y, backgroundRect.width, backgroundRect.height);
    ctx.restore();
    ctx.fillStyle = 'rgba(255, 255, 255, 0.34)';
    ctx.fillRect(0, 0, SHARE_CARD_WIDTH, SHARE_CARD_HEIGHT);
    const frameX = SHARE_CARD_OUTER_PADDING;
    const frameY = SHARE_CARD_OUTER_PADDING;
    const frameWidth = SHARE_CARD_WIDTH - SHARE_CARD_OUTER_PADDING * 2;
    const frameHeight = SHARE_CARD_HEIGHT - SHARE_CARD_OUTER_PADDING * 2;
    ctx.save();
    ctx.shadowColor = 'rgba(82, 58, 32, 0.16)';
    ctx.shadowBlur = 30;
    ctx.shadowOffsetY = 10;
    drawRoundedRect(ctx, frameX, frameY, frameWidth, frameHeight, SHARE_CARD_RADIUS);
    ctx.fillStyle = 'rgba(255, 250, 244, 0.95)';
    ctx.fill();
    ctx.restore();
    ctx.save();
    drawRoundedRect(ctx, frameX, frameY, frameWidth, frameHeight, SHARE_CARD_RADIUS);
    ctx.strokeStyle = 'rgba(201, 181, 153, 0.72)';
    ctx.lineWidth = 2;
    ctx.stroke();
    ctx.restore();
    const imageX = frameX + SHARE_CARD_INNER_PADDING;
    const imageY = frameY + SHARE_CARD_INNER_PADDING;
    const imageMaxWidth = frameWidth - SHARE_CARD_INNER_PADDING * 2;
    const imageMaxHeight = frameHeight - SHARE_CARD_INNER_PADDING * 2;
    const containSize = computeContainSize(sourceWidth, sourceHeight, imageMaxWidth, imageMaxHeight);
    const finalImageX = imageX + Math.round((imageMaxWidth - containSize.width) / 2);
    const finalImageY = imageY + Math.round((imageMaxHeight - containSize.height) / 2);
    ctx.save();
    ctx.shadowColor = 'rgba(82, 58, 32, 0.14)';
    ctx.shadowBlur = 20;
    ctx.shadowOffsetY = 8;
    drawRoundedRect(ctx, finalImageX, finalImageY, containSize.width, containSize.height, SHARE_CARD_IMAGE_RADIUS);
    ctx.fillStyle = 'rgba(255, 255, 255, 0.82)';
    ctx.fill();
    ctx.restore();
    ctx.save();
    drawRoundedRect(ctx, finalImageX, finalImageY, containSize.width, containSize.height, SHARE_CARD_IMAGE_RADIUS);
    ctx.clip();
    ctx.drawImage(image, finalImageX, finalImageY, containSize.width, containSize.height);
    ctx.restore();
    const dataUrl = await canvasToDataURL(canvas);
    const cacheKey = buildShareImageCacheKey(sourceUrl);
    return writeBase64Image(cacheKey, dataUrl);
}
async function prepareShareCardImage(sourceUrl) {
    const cleanUrl = normalizeShareSourceUrl(sourceUrl);
    if (!cleanUrl) {
        return '';
    }
    if (shareImageResultCache[cleanUrl]) {
        return shareImageResultCache[cleanUrl];
    }
    if (!shareImagePromiseCache[cleanUrl]) {
        shareImagePromiseCache[cleanUrl] = createShareCardImage(cleanUrl)
            .then((result) => {
            const cleanResult = String(result || '').trim();
            if (cleanResult) {
                shareImageResultCache[cleanUrl] = cleanResult;
            }
            return cleanResult;
        })
            .catch((error) => {
            console.warn('生成分享图失败:', cleanUrl, error);
            return '';
        })
            .finally(() => {
            delete shareImagePromiseCache[cleanUrl];
        });
    }
    return shareImagePromiseCache[cleanUrl];
}
function prefetchShareCardImage(sourceUrl) {
    void prepareShareCardImage(sourceUrl);
}
