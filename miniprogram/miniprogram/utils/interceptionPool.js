"use strict";
/**
 * 本地拦截词库管理
 * - 内置涉政、涉黄、暴力、违法等基础词库
 * - 优先从服务端拉取最新词库，失败时回退内置/缓存
 */
Object.defineProperty(exports, "__esModule", { value: true });
exports.interceptionPool = void 0;
exports.getBuiltinInterceptionPool = getBuiltinInterceptionPool;
exports.initInterceptionPool = initInterceptionPool;
exports.checkInterceptionText = checkInterceptionText;
const API_BASE_URL = 'https://api.jiadilingguang.com';
const INTERCEPTION_POOL_API = '/api/v1/miniprogram/content/interception-pool';
const INTERCEPTION_POOL_CACHE_KEY = 'interception_pool_cache_v1';
const INTERCEPTION_POOL_CACHE_TTL_MS = 24 * 60 * 60 * 1000;
const CATEGORY_LABELS = {
    politics: '涉政',
    porn: '涉黄',
    violence: '暴力',
    illegal: '违法',
    gambling: '赌博',
    abuse: '辱骂',
    spam: '广告引流',
};
const BUILTIN_POOL = {
    version: 'builtin-1.0.0',
    updatedAt: '2026-03-19',
    categories: {
        politics: [
            '台独',
            '藏独',
            '疆独',
            '港独',
            '分裂国家',
            '颠覆政权',
            '反动宣传',
            '法轮功',
            '反党',
            '反政府',
            '政治谣言',
            '煽动颠覆',
        ],
        porn: [
            '色情',
            '淫秽',
            '黄片',
            '裸聊',
            '援交',
            '卖淫',
            '嫖娼',
            '一夜情',
            '约炮',
            '成人视频',
            '色情网站',
            '开房',
            '性交易',
        ],
        violence: [
            '买凶',
            '雇凶',
            '雇人杀人',
            '制作炸弹',
            '爆炸物制作',
            '恐怖袭击',
            '血腥虐杀',
            '人体炸弹',
        ],
        illegal: [
            '冰毒',
            '海洛因',
            '贩毒',
            '吸毒',
            '假币',
            '办证',
            '刻章',
            '枪支买卖',
            '买卖枪支',
            '洗钱',
            '诈骗教程',
            '翻墙软件',
        ],
        gambling: [
            '网络赌博',
            '博彩',
            '六合彩',
            '时时彩',
            '赌球',
            '赌博平台',
            '线上赌场',
        ],
        abuse: [
            '傻逼',
            '傻b',
            '操你',
            '草你',
            '去死吧',
            '贱人',
            '狗日的',
            '妈的',
            'nmsl',
        ],
        spam: [
            '加微信',
            '加v',
            '刷单',
            '日赚千元',
            '兼职打字',
            '免费领取',
            '点击链接',
            '扫码领红包',
        ],
    },
};
function uniqueWords(words) {
    const seen = new Set();
    const result = [];
    words.forEach((word) => {
        const clean = String(word || '').trim().toLowerCase();
        if (!clean || seen.has(clean)) {
            return;
        }
        seen.add(clean);
        result.push(clean);
    });
    return result;
}
function normalizeText(input) {
    return String(input || '')
        .toLowerCase()
        .replace(/[\s\u00a0\u200b\u200c\u200d\u2060\u3000]+/g, '')
        .replace(/[０-９ａ-ｚＡ-Ｚ]/g, (char) => {
        const code = char.charCodeAt(0);
        if (code >= 0xff10 && code <= 0xff19) {
            return String.fromCharCode(code - 0xff10 + 48);
        }
        if (code >= 0xff21 && code <= 0xff3a) {
            return String.fromCharCode(code - 0xff21 + 97);
        }
        if (code >= 0xff41 && code <= 0xff5a) {
            return String.fromCharCode(code - 0xff41 + 97);
        }
        return char;
    })
        .replace(/0/g, 'o')
        .replace(/1/g, 'i')
        .replace(/3/g, 'e')
        .replace(/4/g, 'a')
        .replace(/5/g, 's')
        .replace(/7/g, 't')
        .replace(/[@*·.·•｜|/_\-+]/g, '');
}
function sanitizePoolPayload(raw) {
    if (!raw || typeof raw !== 'object') {
        return null;
    }
    const categories = {};
    const sourceCategories = raw.categories && typeof raw.categories === 'object'
        ? raw.categories
        : raw;
    Object.keys(CATEGORY_LABELS).forEach((category) => {
        const list = sourceCategories[category];
        if (!Array.isArray(list)) {
            return;
        }
        const words = uniqueWords(list.map((item) => String(item || '')));
        if (words.length > 0) {
            categories[category] = words;
        }
    });
    if (Object.keys(categories).length === 0) {
        return null;
    }
    return {
        version: String(raw.version || 'remote-1.0.0'),
        updatedAt: String(raw.updatedAt || raw.updated_at || new Date().toISOString()),
        categories,
    };
}
function mergePoolPayload(base, extra) {
    const categories = {};
    Object.keys(CATEGORY_LABELS).forEach((category) => {
        const merged = uniqueWords([
            ...(base.categories[category] || []),
            ...((extra && extra.categories[category]) || []),
        ]);
        if (merged.length > 0) {
            categories[category] = merged;
        }
    });
    return {
        version: extra?.version || base.version,
        updatedAt: extra?.updatedAt || base.updatedAt,
        categories,
    };
}
function readCachedPool() {
    try {
        const record = wx.getStorageSync(INTERCEPTION_POOL_CACHE_KEY);
        if (!record || !record.payload || !record.cachedAt) {
            return null;
        }
        if (Date.now() - Number(record.cachedAt) > INTERCEPTION_POOL_CACHE_TTL_MS) {
            return null;
        }
        return sanitizePoolPayload(record.payload);
    }
    catch (_error) {
        return null;
    }
}
function writeCachedPool(payload) {
    try {
        const record = {
            cachedAt: Date.now(),
            payload,
        };
        wx.setStorageSync(INTERCEPTION_POOL_CACHE_KEY, record);
    }
    catch (_error) {
        // 缓存失败不影响主流程
    }
}
function requestRemotePool() {
    return new Promise((resolve) => {
        wx.request({
            url: `${API_BASE_URL}${INTERCEPTION_POOL_API}`,
            method: 'GET',
            timeout: 8000,
            success: (res) => {
                const body = res.data || {};
                if (res.statusCode !== 200 || body.code !== 0) {
                    resolve(null);
                    return;
                }
                resolve(sanitizePoolPayload(body.data || body));
            },
            fail: () => resolve(null),
        });
    });
}
class InterceptionPoolManager {
    constructor() {
        this.pool = BUILTIN_POOL;
        this.source = 'builtin';
        this.initialized = false;
        this.initializing = null;
        this.normalizedKeywordMap = [];
    }
    rebuildKeywordMap() {
        const entries = [];
        Object.keys(CATEGORY_LABELS).forEach((category) => {
            (this.pool.categories[category] || []).forEach((keyword) => {
                entries.push({
                    category,
                    keyword: normalizeText(keyword),
                });
            });
        });
        this.normalizedKeywordMap = entries.sort((a, b) => b.keyword.length - a.keyword.length);
    }
    applyPool(payload, source) {
        this.pool = payload;
        this.source = source;
        this.rebuildKeywordMap();
        this.initialized = true;
    }
    async initialize(forceRemote = false) {
        if (this.initializing) {
            await this.initializing;
            return this.getInfo();
        }
        this.initializing = (async () => {
            if (!forceRemote) {
                const cached = readCachedPool();
                if (cached) {
                    this.applyPool(mergePoolPayload(BUILTIN_POOL, cached), 'cache');
                    return;
                }
            }
            const remote = await requestRemotePool();
            if (remote) {
                const merged = mergePoolPayload(BUILTIN_POOL, remote);
                this.applyPool(merged, 'remote');
                writeCachedPool(remote);
                return;
            }
            this.applyPool(BUILTIN_POOL, 'builtin');
        })();
        try {
            await this.initializing;
        }
        finally {
            this.initializing = null;
        }
        return this.getInfo();
    }
    ensureReady() {
        if (!this.initialized) {
            this.applyPool(BUILTIN_POOL, 'builtin');
        }
    }
    getInfo() {
        this.ensureReady();
        const categoryCounts = {};
        let totalWords = 0;
        Object.keys(CATEGORY_LABELS).forEach((category) => {
            const count = (this.pool.categories[category] || []).length;
            if (count > 0) {
                categoryCounts[category] = count;
                totalWords += count;
            }
        });
        return {
            source: this.source,
            version: this.pool.version,
            updatedAt: this.pool.updatedAt,
            totalWords,
            categoryCounts,
        };
    }
    getCategoryLabel(category) {
        if (!category) {
            return '违规';
        }
        return CATEGORY_LABELS[category] || '违规';
    }
    match(text, categories) {
        this.ensureReady();
        const normalized = normalizeText(text);
        if (!normalized) {
            return { matched: false, source: this.source };
        }
        const allowCategories = categories && categories.length > 0
            ? new Set(categories)
            : null;
        for (const item of this.normalizedKeywordMap) {
            if (allowCategories && !allowCategories.has(item.category)) {
                continue;
            }
            if (item.keyword && normalized.includes(item.keyword)) {
                return {
                    matched: true,
                    category: item.category,
                    keyword: item.keyword,
                    source: this.source,
                };
            }
        }
        return { matched: false, source: this.source };
    }
}
exports.interceptionPool = new InterceptionPoolManager();
function getBuiltinInterceptionPool() {
    return BUILTIN_POOL;
}
async function initInterceptionPool(forceRemote = false) {
    return exports.interceptionPool.initialize(forceRemote);
}
function checkInterceptionText(text, categories) {
    return exports.interceptionPool.match(text, categories);
}
