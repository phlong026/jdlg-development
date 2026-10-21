"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
exports.normalizeCosUrl = normalizeCosUrl;
exports.resolveAssetPath = resolveAssetPath;
// 不可变资源版本与 assets-cloud-manifest.json 保持一致，避免旧缓存覆盖新封面。
const CLOUD_ASSET_RELEASE = 'https://static.jiadilingguang.com/assets/miniprogram-releases/20261022-292ba168e0ef';
const CLOUD_IMAGE_PATHS = new Set([
    '/assets/designer/设计师@4x.png',
    '/assets/home-reference/hd/advisor.png',
    '/assets/home-reference/hd/ai-medallion.png',
    '/assets/home-reference/hd/ai-shell.png',
    '/assets/home-reference/hd/ai-tools.png',
    '/assets/home-reference/hd/backdrop.jpg',
    '/assets/home-reference/hd/brand-en-old.png',
    '/assets/home-reference/hd/brand-name-old.png',
    '/assets/home-reference/hd/brand-symbol-old.png',
    '/assets/home-reference/hd/calculator.png',
    '/assets/home-reference/hd/camera.png',
    '/assets/home-reference/hd/cost-stat-v2.png',
    '/assets/home-reference/hd/courtyard.png',
    '/assets/home-reference/hd/estimate-shell.png',
    '/assets/home-reference/hd/interior.png',
    '/assets/home-reference/hd/location.png',
    '/assets/home-reference/hd/renovation.png',
    '/assets/home-reference/hd/ruler.png',
    '/assets/home-reference/hd/rural-villa.png',
    '/assets/home-reference/hd/sparkles.png',
    '/assets/home-reference/hd/templates.png',
    '/assets/home-reference/hd/urban-building.png',
    '/assets/home-reference/hd/villa.png',
    '/assets/icons/send-plane.svg',
    '/assets/icons/specadvisor/icon-advisor-avatar.svg',
    '/assets/icons/specadvisor/icon-building-height.svg',
    '/assets/icons/specadvisor/icon-fire-escape.svg',
    '/assets/icons/specadvisor/icon-house-rules.svg',
    '/assets/icons/specadvisor/icon-send.svg',
    '/assets/icons/specadvisor/icon-source.svg',
    '/assets/icons/specadvisor/icon-staircase.svg',
    '/assets/images/cost-estimate-share-house.jpg',
    '/assets/images/home.jpg',
    '/assets/share/budget-timeline-v2.jpg',
    '/assets/share/home-chat.jpg',
    '/assets/share/home-timeline-v2.jpg',
    '/assets/share/home-timeline.jpg',
    '/assets/smart-service-icon.png',
    '/assets/smart-service-mascot.png',
    '/assets/tabbar/我的导航栏@2x.png',
    '/assets/tabbar/未选中态@2x.png',
    '/assets/tabbar/模板广场导航栏@2x.png',
    '/assets/tabbar/设计师导航栏@2x.png',
    '/assets/tabbar/首页导航栏@2x.png',
    '/pages/Invitefriends/intro-logo.png',
]);
// 如果你已经把整个 miniprogram/miniprogram/assets 目录上传到了 COS，
// 可以直接把下面这一项改成：
// const ASSET_CDN_BASE = 'https://你的COS自定义域名/miniprogram';
const ASSET_CDN_BASE = 'https://static.jiadilingguang.com';
// COS 原始域名 → CDN 域名映射，确保所有图片 URL 走 CDN（小程序合法域名）
const COS_RAW_HOST = 'jiadilingguangcos-1393500756.cos.ap-chongqing.myqcloud.com';
const COS_CDN_HOST = 'static.jiadilingguang.com';
// 如果你暂时只想先迁移首批大图，就保持 ASSET_CDN_BASE 为空，
// 然后把下面对应图片的完整 COS 地址填进去即可。
const ASSET_CDN_MAP = {
    // 示例：https://你的COS自定义域名/miniprogram/assets/home/logo+背景图.png
    '/assets/home/logo+背景图.png': 'https://static.jiadilingguang.com/assets/home/logo+背景图.png',
    // 示例：https://你的COS自定义域名/miniprogram/assets/home/核心输入区.png
    '/assets/home/核心输入区.png': 'https://static.jiadilingguang.com/assets/home/核心输入区.png',
    // 示例：https://你的COS自定义域名/miniprogram/assets/aigenerate/拍照上传.png
    '/assets/aigenerate/拍照上传.png': 'https://static.jiadilingguang.com/assets/aigenerate/拍照上传.png',
    // 示例：https://你的COS自定义域名/miniprogram/assets/aigenerate/选个风格.png
    '/assets/aigenerate/选个风格.png': 'https://static.jiadilingguang.com/assets/aigenerate/选个风格.png',
    // 示例：https://你的COS自定义域名/miniprogram/assets/aigenerate/图像+视频生成.png
    '/assets/aigenerate/图像+视频生成.png': 'https://static.jiadilingguang.com/assets/aigenerate/图像+视频生成.png',
    // 示例：https://你的COS自定义域名/miniprogram/assets/aivideo/生成漫游视频背景.png
    '/assets/aivideo/生成漫游视频背景.png': 'https://static.jiadilingguang.com/assets/aivideo/生成漫游视频背景.png',
    // 示例：https://你的COS自定义域名/miniprogram/assets/aivideo/首尾帧图像.png
    '/assets/aivideo/首尾帧图像.png': 'https://static.jiadilingguang.com/assets/aivideo/首尾帧图像.png',
    // 示例：https://你的COS自定义域名/miniprogram/assets/aivideo/提示词.png
    '/assets/aivideo/提示词.png': '',
    // 示例：https://你的COS自定义域名/miniprogram/assets/template/页面背景.png
    '/assets/template/页面背景.png': 'https://static.jiadilingguang.com/assets/template/页面背景.png',
    // 首页引导弹窗 Logo
    '/assets/home/intro-logo.png': 'https://static.jiadilingguang.com/assets/home/intro-logo.png',
    // 建规快查页面水墨风格背景图
    '/assets/specadvisor/bg-ink.png': 'https://static.jiadilingguang.com/assets/specadvisor/bg-ink.png',
    // 首页智能客服悬浮吉祥物图标
    '/assets/home/smart-service-mascot.png': 'https://static.jiadilingguang.com/assets/home/smart-service-mascot.png',
    // 示例：https://你的COS自定义域名/miniprogram/assets/企业logo.png
    '/assets/企业logo.png': '',
    // 示例：https://你的COS自定义域名/miniprogram/assets/企业微信二维码.png'
    '/assets/企业微信二维码.png': '',
};
/** 将 COS 原始域名替换为 CDN 域名，确保 URL 在小程序合法域名列表中 */
function normalizeCosUrl(url) {
    const cleanUrl = String(url || '').trim();
    if (!cleanUrl) {
        return cleanUrl;
    }
    return cleanUrl.replace(`://${COS_RAW_HOST}`, `://${COS_CDN_HOST}`);
}
function resolveAssetPath(path) {
    const cleanPath = String(path || '').trim();
    if (!cleanPath) {
        return '';
    }
    if (/^https?:\/\//i.test(cleanPath)) {
        return cleanPath;
    }
    const normalizedPath = cleanPath.startsWith('/') ? cleanPath : `/${cleanPath}`;
    if (CLOUD_IMAGE_PATHS.has(normalizedPath)) {
        return `${CLOUD_ASSET_RELEASE}${normalizedPath}`;
    }
    const mappedUrl = String(ASSET_CDN_MAP[normalizedPath] || '').trim();
    if (mappedUrl) {
        return mappedUrl;
    }
    const cdnBase = String(ASSET_CDN_BASE || '').trim();
    if (!cdnBase) {
        return normalizedPath;
    }
    return `${cdnBase.replace(/\/+$/, '')}${normalizedPath}`;
}
