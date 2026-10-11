"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
exports.getPageCache = getPageCache;
exports.setPageCache = setPageCache;
exports.clearPageCache = clearPageCache;
exports.prefetchImage = prefetchImage;
exports.prefetchImages = prefetchImages;
const pageCacheStore = new Map();
const prefetchedImageStore = new Map();
const MAX_CACHE_ENTRIES = 80;
const MAX_PREFETCHED_IMAGES = 160;
function trimMap(map, maxSize) {
    while (map.size > maxSize) {
        const firstKey = map.keys().next().value;
        if (typeof firstKey === 'undefined') {
            return;
        }
        map.delete(firstKey);
    }
}
const asset_1 = require("./asset");
function normalizeUrl(url) {
    if (typeof url !== 'string') {
        return '';
    }
    return (0, asset_1.normalizeCosUrl)(url.trim());
}
function getPageCache(key) {
    const cacheKey = String(key || '').trim();
    if (!cacheKey) {
        return null;
    }
    const entry = pageCacheStore.get(cacheKey);
    if (!entry) {
        return null;
    }
    if (entry.expiresAt <= Date.now()) {
        pageCacheStore.delete(cacheKey);
        return null;
    }
    return entry.value;
}
function setPageCache(key, value, ttlMs) {
    const cacheKey = String(key || '').trim();
    if (!cacheKey) {
        return;
    }
    pageCacheStore.set(cacheKey, {
        value,
        expiresAt: Date.now() + Math.max(1000, Number(ttlMs || 0)),
    });
    trimMap(pageCacheStore, MAX_CACHE_ENTRIES);
}
function clearPageCache(key) {
    const cacheKey = String(key || '').trim();
    if (!cacheKey) {
        return;
    }
    pageCacheStore.delete(cacheKey);
}
function prefetchImage(url) {
    const imageUrl = normalizeUrl(url);
    if (!imageUrl || prefetchedImageStore.has(imageUrl)) {
        return Promise.resolve();
    }
    prefetchedImageStore.set(imageUrl, Date.now());
    trimMap(prefetchedImageStore, MAX_PREFETCHED_IMAGES);
    return new Promise((resolve) => {
        wx.getImageInfo({
            src: imageUrl,
            success: () => resolve(),
            fail: () => resolve(),
        });
    });
}
function prefetchImages(urls, limit = 2) {
    const maxCount = Math.max(0, Number(limit || 0));
    if (!Array.isArray(urls) || maxCount === 0) {
        return Promise.resolve();
    }
    const uniqueUrls = [];
    urls.forEach((item) => {
        const url = normalizeUrl(item);
        if (!url || uniqueUrls.includes(url)) {
            return;
        }
        uniqueUrls.push(url);
    });
    return Promise.allSettled(uniqueUrls.slice(0, maxCount).map((url) => prefetchImage(url))).then(() => undefined);
}
