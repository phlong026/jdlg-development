type CacheEntry<T> = {
  value: T;
  expiresAt: number;
};

const pageCacheStore = new Map<string, CacheEntry<unknown>>();
const prefetchedImageStore = new Map<string, number>();
const MAX_CACHE_ENTRIES = 80;
const MAX_PREFETCHED_IMAGES = 160;

function trimMap<K, V>(map: Map<K, V>, maxSize: number) {
  while (map.size > maxSize) {
    const firstKey = map.keys().next().value;
    if (typeof firstKey === 'undefined') {
      return;
    }
    map.delete(firstKey);
  }
}

import { normalizeCosUrl } from './asset';

function normalizeUrl(url: unknown): string {
  if (typeof url !== 'string') {
    return '';
  }
  return normalizeCosUrl(url.trim());
}

export function getPageCache<T>(key: string): T | null {
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

  return entry.value as T;
}

export function setPageCache<T>(key: string, value: T, ttlMs: number) {
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

export function clearPageCache(key: string) {
  const cacheKey = String(key || '').trim();
  if (!cacheKey) {
    return;
  }
  pageCacheStore.delete(cacheKey);
}

export function prefetchImage(url: string): Promise<void> {
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

export function prefetchImages(urls: unknown[], limit = 2): Promise<void> {
  const maxCount = Math.max(0, Number(limit || 0));
  if (!Array.isArray(urls) || maxCount === 0) {
    return Promise.resolve();
  }

  const uniqueUrls: string[] = [];
  urls.forEach((item) => {
    const url = normalizeUrl(item);
    if (!url || uniqueUrls.includes(url)) {
      return;
    }
    uniqueUrls.push(url);
  });

  return Promise.allSettled(uniqueUrls.slice(0, maxCount).map((url) => prefetchImage(url))).then(() => undefined);
}
