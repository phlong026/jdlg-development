"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
exports.normalizeCosUrl = normalizeCosUrl;
exports.resolveAssetPath = resolveAssetPath;
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
    '/assets/home/smart-service-mascot.png': 'https://jiadilingguangcos-1393500756.cos.ap-chongqing.myqcloud.com/assets/home/smart-service-mascot.png',
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
