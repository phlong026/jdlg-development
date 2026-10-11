const CARD_WIDTH = 1000;
const CARD_HEIGHT = 800;
const HOUSE_IMAGE_PATHS = [
  '/assets/images/cost-estimate-share-house.jpg',
  'assets/images/cost-estimate-share-house.jpg',
];

export type CostEstimateShareCard = {
  totalCost?: string;
  city: string;
  buildingArea: string;
  structureType: string;
  windowStandard: string;
};

const imageCache: Record<string, Promise<string>> = {};

function drawRoundedRect(ctx: any, x: number, y: number, width: number, height: number, radius: number) {
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

function cleanAmount(totalCost: string): string {
  return String(totalCost || '').replace(/万元$/, '').trim() || '--';
}

function buildCacheKey(card: CostEstimateShareCard): string {
  return [card.totalCost, card.city, card.buildingArea, card.structureType, card.windowStandard].join('|');
}

function canvasToDataURL(canvas: any): string {
  return String(canvas.toDataURL('image/jpeg', 0.98) || '').trim();
}

function loadCanvasImage(canvas: any, source: string): Promise<any> {
  return new Promise((resolve, reject) => {
    const image = canvas.createImage();
    image.onload = () => resolve(image);
    image.onerror = reject;
    image.src = source;
  });
}

async function loadBundledHouseImage(canvas: any): Promise<any> {
  let latestError: unknown;
  for (const source of HOUSE_IMAGE_PATHS) {
    try {
      return await loadCanvasImage(canvas, source);
    } catch (error) {
      latestError = error;
    }
  }
  throw latestError || new Error('别墅分享主视觉加载失败');
}

async function writeImage(cacheKey: string, dataUrl: string): Promise<string> {
  const payload = dataUrl.includes(',') ? dataUrl.split(',')[1] : dataUrl;
  const base64ToArrayBuffer = (wx as any).base64ToArrayBuffer;
  if (!payload || typeof base64ToArrayBuffer !== 'function') return '';

  const fileSystem = wx.getFileSystemManager();
  const directory = `${wx.env.USER_DATA_PATH}/cost-estimate-share`;
  const filePath = `${directory}/${encodeURIComponent(cacheKey)}.jpg`;
  await new Promise<void>((resolve, reject) => {
    fileSystem.mkdir({
      dirPath: directory,
      recursive: true,
      success: () => resolve(),
      fail: (error: any) => String(error?.errMsg || '').includes('file already exists') ? resolve() : reject(error),
    });
  });
  await new Promise<void>((resolve, reject) => {
    fileSystem.writeFile({
      filePath,
      data: base64ToArrayBuffer(payload),
      success: () => resolve(),
      fail: reject,
    });
  });
  return filePath;
}

function drawCardHouseImage(ctx: any, image: any, hasEstimate: boolean) {
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

function drawCardFrame(ctx: any, hasEstimate: boolean) {
  ctx.strokeStyle = '#d8c29e';
  ctx.lineWidth = 1.5;
  drawRoundedRect(ctx, 34, hasEstimate ? 24 : 54, 932, hasEstimate ? 608 : 570, 16);
  ctx.stroke();
}

function fitText(ctx: any, text: string, weight: number, fontSize: number, maxWidth: number): number {
  ctx.font = `${weight} ${fontSize}px sans-serif`;
  while (ctx.measureText(text).width > maxWidth && fontSize > 24) {
    fontSize -= 2;
    ctx.font = `${weight} ${fontSize}px sans-serif`;
  }
  return ctx.measureText(text).width;
}

function drawCardText(ctx: any, card: CostEstimateShareCard) {
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
  ctx.fillText('建房造价测算', 62, 166);
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

function drawCallToAction(ctx: any, label: string) {
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

async function createShareImage(card: CostEstimateShareCard): Promise<string> {
  const createOffscreenCanvas = (wx as any).createOffscreenCanvas;
  if (typeof createOffscreenCanvas !== 'function') return '';

  const canvas = createOffscreenCanvas({ type: '2d', width: CARD_WIDTH * 2, height: CARD_HEIGHT * 2 });
  const ctx = canvas?.getContext('2d');
  if (!canvas || !ctx) return '';

  ctx.scale(2, 2);

  const background = ctx.createLinearGradient(0, 550, 0, 630);
  background.addColorStop(0, '#fcf3e3');
  background.addColorStop(1, '#f6e8ce');
  ctx.fillStyle = background;
  ctx.fillRect(0, 0, CARD_WIDTH, CARD_HEIGHT);

  const houseImage = await loadBundledHouseImage(canvas);
  const hasEstimate = Boolean(card.totalCost);
  drawCardHouseImage(ctx, houseImage, hasEstimate);
  drawCardFrame(ctx, hasEstimate);
  drawCardText(ctx, card);
  drawCallToAction(ctx, hasEstimate ? '点击查看测算报告' : '开始测算');

  return writeImage(buildCacheKey(card), canvasToDataURL(canvas));
}

export function prepareCostEstimateShareImage(card: CostEstimateShareCard): Promise<string> {
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
