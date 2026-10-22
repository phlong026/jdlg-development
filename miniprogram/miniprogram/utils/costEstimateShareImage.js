"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
exports.prepareCostEstimateShareImage = prepareCostEstimateShareImage;
exports.prepareCostEstimateTimelineImage = prepareCostEstimateTimelineImage;
const asset_1 = require("./asset");
const cloudImage_1 = require("./cloudImage");
const CARD_WIDTH = 1000;
const CARD_HEIGHT = 800;
const HOUSE_IMAGE_URL = (0, asset_1.resolveAssetPath)('/assets/images/cost-estimate-share-house.jpg');
const imageCache = {};
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
function cleanAmount(totalCost) {
    return String(totalCost || '').replace(/万元$/, '').trim() || '--';
}
function buildCacheKey(card) {
    return [card.totalCost, card.totalLabel, card.city, card.buildingArea, card.structureType, card.windowStandard].join('|');
}
function canvasToDataURL(canvas) {
    return String(canvas.toDataURL('image/jpeg', 0.98) || '').trim();
}
function loadCanvasImage(canvas, source) {
    return new Promise((resolve, reject) => {
        const image = canvas.createImage();
        image.onload = () => resolve(image);
        image.onerror = reject;
        image.src = source;
    });
}
async function loadHouseImage(canvas) {
    const source = await (0, cloudImage_1.prepareCloudImage)(HOUSE_IMAGE_URL, 'cost-estimate-share-house.jpg');
    if (!source)
        throw new Error('别墅分享主视觉下载失败');
    return loadCanvasImage(canvas, source);
}
async function writeImage(cacheKey, dataUrl) {
    const payload = dataUrl.includes(',') ? dataUrl.split(',')[1] : dataUrl;
    const base64ToArrayBuffer = wx.base64ToArrayBuffer;
    if (!payload || typeof base64ToArrayBuffer !== 'function')
        return '';
    const fileSystem = wx.getFileSystemManager();
    const directory = `${wx.env.USER_DATA_PATH}/cost-estimate-share`;
    const filePath = `${directory}/${encodeURIComponent(cacheKey)}.jpg`;
    await new Promise((resolve, reject) => {
        fileSystem.mkdir({
            dirPath: directory,
            recursive: true,
            success: () => resolve(),
            fail: (error) => String((error === null || error === void 0 ? void 0 : error.errMsg) || '').includes('file already exists') ? resolve() : reject(error),
        });
    });
    await new Promise((resolve, reject) => {
        fileSystem.writeFile({
            filePath,
            data: base64ToArrayBuffer(payload),
            success: () => resolve(),
            fail: reject,
        });
    });
    return filePath;
}
function drawCardHouseImage(ctx, image, hasEstimate) {
    // Soften only the left seam; keep the roof, facade and steps free of haze.
    ctx.save();
    const imageX = hasEstimate ? 410 : 396;
    const imageY = hasEstimate ? 24 : 54;
    const imageSize = hasEstimate ? 610 : 570;
    const blendWidth = hasEstimate ? 140 : 90;
    drawRoundedRect(ctx, 34, imageY, 932, hasEstimate ? 608 : 570, 16);
    ctx.clip();
    ctx.drawImage(image, imageX, imageY, imageSize, imageSize);
    const leftBlend = ctx.createLinearGradient(imageX, 0, imageX + blendWidth, 0);
    leftBlend.addColorStop(0, '#fcf3e3');
    leftBlend.addColorStop(1, 'rgba(252, 243, 227, 0)');
    ctx.fillStyle = leftBlend;
    ctx.fillRect(imageX, imageY, blendWidth, hasEstimate ? 608 : 570);
    ctx.restore();
}
function drawCardFrame(ctx, hasEstimate) {
    ctx.strokeStyle = '#d8c29e';
    ctx.lineWidth = 1.5;
    drawRoundedRect(ctx, 34, hasEstimate ? 24 : 54, 932, hasEstimate ? 608 : 570, 16);
    ctx.stroke();
}
function fitText(ctx, text, weight, fontSize, maxWidth) {
    ctx.font = `${weight} ${fontSize}px sans-serif`;
    while (ctx.measureText(text).width > maxWidth && fontSize > 24) {
        fontSize -= 2;
        ctx.font = `${weight} ${fontSize}px sans-serif`;
    }
    return ctx.measureText(text).width;
}
function drawCardText(ctx, card) {
    ctx.fillStyle = '#704416';
    if (!card.totalCost) {
        // An invitation should explain the action, not imply that form defaults are report facts.
        ctx.font = '600 36px sans-serif';
        ctx.fillText('建房造价测算', 62, 156);
        ctx.font = '700 56px sans-serif';
        ctx.fillText('建房要花', 62, 258);
        ctx.fillStyle = '#e62c12';
        ctx.font = '800 116px sans-serif';
        ctx.fillText('？', 62, 390);
        const questionWidth = ctx.measureText('？').width;
        ctx.fillStyle = '#704416';
        ctx.font = '600 44px sans-serif';
        ctx.fillText('元', 76 + questionWidth, 390);
        ctx.font = '500 30px sans-serif';
        ctx.fillText('填写建房参数', 62, 450);
        ctx.fillText('查看参考造价', 62, 498);
        return;
    }
    ctx.font = '700 46px sans-serif';
    ctx.fillText(card.totalLabel === '已测算部分合计' ? '已测算部分合计' : '建房造价测算', 62, 166);
    const amount = cleanAmount(card.totalCost);
    ctx.font = '700 52px sans-serif';
    const unitWidth = ctx.measureText('万元').width;
    ctx.fillStyle = '#e62c12';
    const amountWidth = fitText(ctx, amount, 800, 132, 480 - unitWidth - 14);
    ctx.fillText(amount, 62, 308);
    ctx.fillStyle = '#704416';
    ctx.font = '700 52px sans-serif';
    ctx.fillText('万元', 76 + amountWidth, 308);
    const areaText = card.buildingArea
        ? `${card.city} · 建筑面积${card.buildingArea.replace(/\s/g, '')}`
        : `${card.city} · 填写参数即可测算`;
    fitText(ctx, areaText, 500, 36, 480);
    ctx.fillText(areaText, 62, 426);
    const specificationText = `${card.structureType} · ${card.windowStandard}门窗`;
    fitText(ctx, specificationText, 500, 34, 480);
    ctx.fillText(specificationText, 62, 486);
}
function drawCallToAction(ctx, label) {
    const button = ctx.createLinearGradient(54, 650, 946, 766);
    // Match the confirmed reference's warm gold button and white label.
    button.addColorStop(0, '#e8b18b');
    button.addColorStop(0.5, '#bf824b');
    button.addColorStop(1, '#a65c25');
    ctx.fillStyle = button;
    drawRoundedRect(ctx, 54, 650, 892, 116, 58);
    ctx.fill();
    ctx.fillStyle = '#ffffff';
    ctx.font = '600 44px sans-serif';
    const textWidth = ctx.measureText(label).width;
    const textX = (CARD_WIDTH - textWidth - 56) / 2;
    ctx.fillText(label, textX, 723);
    const arrowX = textX + textWidth + 34;
    ctx.beginPath();
    ctx.moveTo(arrowX, 692);
    ctx.lineTo(arrowX + 16, 707);
    ctx.lineTo(arrowX, 722);
    ctx.strokeStyle = '#ffffff';
    ctx.lineWidth = 5;
    ctx.stroke();
}
async function createShareImage(card) {
    const createOffscreenCanvas = wx.createOffscreenCanvas;
    if (typeof createOffscreenCanvas !== 'function')
        return '';
    const canvas = createOffscreenCanvas({ type: '2d', width: CARD_WIDTH * 2, height: CARD_HEIGHT * 2 });
    const ctx = canvas === null || canvas === void 0 ? void 0 : canvas.getContext('2d');
    if (!canvas || !ctx)
        return '';
    ctx.scale(2, 2);
    const background = ctx.createLinearGradient(0, 550, 0, 630);
    background.addColorStop(0, '#fcf3e3');
    background.addColorStop(1, '#f6e8ce');
    ctx.fillStyle = background;
    ctx.fillRect(0, 0, CARD_WIDTH, CARD_HEIGHT);
    const houseImage = await loadHouseImage(canvas);
    const hasEstimate = Boolean(card.totalCost);
    drawCardHouseImage(ctx, houseImage, hasEstimate);
    drawCardFrame(ctx, hasEstimate);
    drawCardText(ctx, card);
    drawCallToAction(ctx, hasEstimate ? '点击查看测算报告' : '开始测算');
    return writeImage(buildCacheKey(card), canvasToDataURL(canvas));
}
function prepareCostEstimateShareImage(card) {
    const cacheKey = buildCacheKey(card);
    if (!imageCache[cacheKey]) {
        imageCache[cacheKey] = createShareImage(card)
            .catch((error) => {
            console.warn('生成造价分享封面失败:', error);
            return '';
        })
            .finally(() => delete imageCache[cacheKey]);
    }
    return imageCache[cacheKey];
}
// Moments uses a square thumbnail, so keep its amount and villa inside that frame.
async function prepareCostEstimateTimelineImage(card) {
    if (!card.totalCost || typeof wx.createOffscreenCanvas !== 'function')
        return '';
    try {
        const canvas = wx.createOffscreenCanvas({ type: '2d', width: 1000, height: 1000 });
        const ctx = canvas === null || canvas === void 0 ? void 0 : canvas.getContext('2d');
        if (!ctx)
            return '';
        ctx.fillStyle = '#fcf3e3';
        ctx.fillRect(0, 0, 1000, 1000);
        const image = await loadHouseImage(canvas);
        ctx.drawImage(image, 390, 250, 610, 610);
        const seam = ctx.createLinearGradient(390, 0, 450, 0);
        seam.addColorStop(0, '#fcf3e3');
        seam.addColorStop(1, 'rgba(252, 243, 227, 0)');
        ctx.fillStyle = seam;
        ctx.fillRect(390, 250, 60, 610);
        ctx.fillStyle = '#9c6a2c';
        ctx.font = '700 48px sans-serif';
        ctx.fillText('甲第灵光 ArchLight', 60, 100);
        ctx.fillStyle = '#704416';
        ctx.font = '600 58px sans-serif';
        ctx.fillText(card.totalLabel === '已测算部分合计' ? '已测算部分合计' : '参考造价', 60, 245);
        ctx.fillStyle = '#a65c25';
        fitText(ctx, cleanAmount(card.totalCost), 800, 140, 350);
        ctx.fillText(cleanAmount(card.totalCost), 60, 410);
        ctx.font = '700 64px sans-serif';
        ctx.fillText('万元', 60, 495);
        ctx.fillStyle = '#704416';
        fitText(ctx, `${card.buildingArea.replace(/\s/g, '')} · ${card.city}`, 600, 46, 330);
        ctx.fillText(`${card.buildingArea.replace(/\s/g, '')} · ${card.city}`, 60, 625);
        ctx.font = '500 32px sans-serif';
        ctx.fillText('建房预算测算', 60, 690);
        ctx.font = '600 48px sans-serif';
        ctx.fillText('查看测算报告', 60, 935);
        return await writeImage(`timeline-v2|${buildCacheKey(card)}`, canvasToDataURL(canvas));
    }
    catch (error) {
        console.warn('生成朋友圈预算封面失败:', error);
        return '';
    }
}
