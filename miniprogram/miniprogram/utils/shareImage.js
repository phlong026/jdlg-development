"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
exports.prepareShareCardImage = prepareShareCardImage;
exports.prepareTimelineShareImage = prepareTimelineShareImage;
exports.prefetchShareCardImage = prefetchShareCardImage;
const asset_1 = require("./asset");
const SHARE_CARD_WIDTH = 1000;
const SHARE_CARD_HEIGHT = 800;
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
    return String(canvas.toDataURL('image/jpeg', 0.92) || '').trim();
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
async function createShareCardImage(sourceUrl, height, fit) {
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
        height: height,
    });
    const ctx = canvas?.getContext('2d');
    if (!canvas || !ctx) {
        return '';
    }
    const image = await loadCanvasImage(canvas, sourcePath);
    const sourceWidth = Number(imageInfo.width || image.width || 0);
    const sourceHeight = Number(imageInfo.height || image.height || 0);
    // JPEG has no alpha channel; transparent source pixels must not turn black.
    ctx.fillStyle = '#ffffff';
    ctx.fillRect(0, 0, SHARE_CARD_WIDTH, height);
    if (fit === 'contain') {
        // Invitation posters must keep their text and QR code fully visible.
        const size = computeContainSize(sourceWidth, sourceHeight, SHARE_CARD_WIDTH, height);
        ctx.drawImage(image, Math.round((SHARE_CARD_WIDTH - size.width) / 2), Math.round((height - size.height) / 2), size.width, size.height);
    }
    else {
        // Native share cards have a fixed ratio; crop the edges instead of adding a frame.
        const rect = computeCoverRect(sourceWidth, sourceHeight, SHARE_CARD_WIDTH, height);
        ctx.drawImage(image, rect.x, rect.y, rect.width, rect.height);
    }
    const dataUrl = await canvasToDataURL(canvas);
    const cacheKey = buildShareImageCacheKey(`${fit}:${height}:${sourceUrl}`);
    return writeBase64Image(cacheKey, dataUrl);
}
async function prepareImage(sourceUrl, height, fit = 'cover') {
    const cleanUrl = normalizeShareSourceUrl(sourceUrl);
    const key = `${fit}:${height}:${cleanUrl}`;
    if (!cleanUrl) {
        return '';
    }
    if (shareImageResultCache[key]) {
        return shareImageResultCache[key];
    }
    if (!shareImagePromiseCache[key]) {
        shareImagePromiseCache[key] = createShareCardImage(cleanUrl, height, fit)
            .then((result) => {
            const cleanResult = String(result || '').trim();
            if (cleanResult) {
                shareImageResultCache[key] = cleanResult;
            }
            return cleanResult;
        })
            .catch((error) => {
            console.warn('生成分享图失败:', cleanUrl, error);
            return '';
        })
            .finally(() => {
            delete shareImagePromiseCache[key];
        });
    }
    return shareImagePromiseCache[key];
}
function prepareShareCardImage(sourceUrl, fit = 'cover') {
    return prepareImage(sourceUrl, SHARE_CARD_HEIGHT, fit);
}
function prepareTimelineShareImage(sourceUrl) {
    return prepareImage(sourceUrl, SHARE_CARD_WIDTH);
}
function prefetchShareCardImage(sourceUrl) {
    void prepareShareCardImage(sourceUrl);
}
