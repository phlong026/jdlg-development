export type RenovationShareCard = {
  city: string;
  area: string;
  totalCost: string;
  materialCost: string;
  auxiliaryCost: string;
  laborCost: string;
};

const pending: Record<string, Promise<string>> = {};

function cacheKey(card: RenovationShareCard, square: boolean): string {
  return ['renovation-v1', square ? 'timeline' : 'chat', card.city, card.area, card.totalCost, card.materialCost, card.auxiliaryCost, card.laborCost].join('|');
}

function amountText(value: string): string {
  return String(value || '').replace(/万元$/, '').trim() || '--';
}

function roundedRect(ctx: any, x: number, y: number, width: number, height: number, radius: number) {
  ctx.beginPath();
  ctx.moveTo(x + radius, y);
  ctx.lineTo(x + width - radius, y);
  ctx.arcTo(x + width, y, x + width, y + radius, radius);
  ctx.lineTo(x + width, y + height - radius);
  ctx.arcTo(x + width, y + height, x + width - radius, y + height, radius);
  ctx.lineTo(x + radius, y + height);
  ctx.arcTo(x, y + height, x, y + height - radius, radius);
  ctx.lineTo(x, y + radius);
  ctx.arcTo(x, y, x + radius, y, radius);
  ctx.closePath();
  ctx.fill();
}

async function writeCanvas(canvas: any, key: string): Promise<string> {
  const dataUrl = String(canvas.toDataURL('image/jpeg', 0.96) || '');
  const payload = dataUrl.includes(',') ? dataUrl.split(',')[1] : '';
  const convert = (wx as any).base64ToArrayBuffer;
  if (!payload || typeof convert !== 'function') return '';
  const fs = wx.getFileSystemManager();
  const directory = `${wx.env.USER_DATA_PATH}/renovation-share`;
  const filePath = `${directory}/${encodeURIComponent(key)}.jpg`;
  await new Promise<void>((resolve, reject) => fs.mkdir({ dirPath: directory, recursive: true, success: () => resolve(),
    fail: (error: any) => String(error?.errMsg || '').includes('file already exists') ? resolve() : reject(error) }));
  await new Promise<void>((resolve, reject) => fs.writeFile({ filePath, data: convert(payload), success: () => resolve(), fail: reject }));
  return filePath;
}

async function createImage(card: RenovationShareCard, square: boolean): Promise<string> {
  const createCanvas = (wx as any).createOffscreenCanvas;
  if (typeof createCanvas !== 'function') return '';
  const width = 1000, height = square ? 1000 : 800;
  const canvas = createCanvas({ type: '2d', width, height });
  const ctx = canvas?.getContext('2d');
  if (!ctx) return '';

  ctx.fillStyle = '#f4eee5';
  ctx.fillRect(0, 0, width, height);
  ctx.fillStyle = '#fffaf3';
  roundedRect(ctx, 44, 40, 912, square ? 680 : 590, 34);
  ctx.fillStyle = '#8c6944';
  ctx.font = '600 42px sans-serif';
  ctx.fillText('装修报价测算', 86, 116);
  ctx.fillStyle = '#736653';
  ctx.font = '500 30px sans-serif';
  ctx.fillText(`${card.city || '装修'} · 装修面积${card.area || '--'}`, 86, 168);
  ctx.fillStyle = '#a65c25';
  ctx.font = '800 122px sans-serif';
  ctx.fillText(amountText(card.totalCost), 82, 326);
  const widthOfAmount = ctx.measureText(amountText(card.totalCost)).width;
  ctx.fillStyle = '#704416';
  ctx.font = '700 48px sans-serif';
  ctx.fillText('万元', 100 + widthOfAmount, 326);
  ctx.fillStyle = '#8c6944';
  ctx.font = '500 28px sans-serif';
  ctx.fillText('装修参考总价', 86, 376);

  const rows = [['主材', card.materialCost], ['辅材', card.auxiliaryCost], ['人工', card.laborCost]];
  rows.forEach(([label, value], index) => {
    const x = 84 + index * 292;
    ctx.fillStyle = '#efe2cf';
    roundedRect(ctx, x, 420, 260, 126, 18);
    ctx.fillStyle = '#8c6944';
    ctx.font = '500 26px sans-serif';
    ctx.fillText(label, x + 22, 462);
    ctx.fillStyle = '#4a3524';
    ctx.font = '600 30px sans-serif';
    ctx.fillText(value || '0元', x + 22, 510);
  });

  ctx.fillStyle = '#4a3524';
  ctx.font = '600 40px sans-serif';
  ctx.fillText('查看空间与施工项目明细', 70, square ? 818 : 704);
  ctx.fillStyle = '#736653';
  ctx.font = '400 28px sans-serif';
  ctx.fillText('价格为本次测算固定快照', 70, square ? 868 : 750);
  return writeCanvas(canvas, cacheKey(card, square));
}

function prepare(card: RenovationShareCard, square: boolean): Promise<string> {
  const key = cacheKey(card, square);
  if (!pending[key]) {
    pending[key] = createImage(card, square).catch((error) => {
      console.warn('生成装修报价分享封面失败:', error);
      return '';
    }).finally(() => delete pending[key]);
  }
  return pending[key];
}

export function prepareRenovationShareImage(card: RenovationShareCard): Promise<string> {
  return prepare(card, false);
}

export function prepareRenovationTimelineImage(card: RenovationShareCard): Promise<string> {
  return prepare(card, true);
}
