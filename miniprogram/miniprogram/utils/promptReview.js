"use strict";
/**
 * 用户输入本地审核 — 纯前端同步拦截，不依赖后端
 */
Object.defineProperty(exports, "__esModule", { value: true });
exports.reviewUserInputSync = reviewUserInputSync;
exports.showPromptReviewRejected = showPromptReviewRejected;
exports.blockUnsafeText = blockUnsafeText;
exports.blockAnyUnsafeTexts = blockAnyUnsafeTexts;
exports.ensurePromptPassed = ensurePromptPassed;
exports.initPromptReview = initPromptReview;
exports.reviewUserInput = reviewUserInput;
exports.refreshPromptReviewPool = refreshPromptReviewPool;
const INTERCEPT_MODAL_TITLE = '内容拦截';
const INTERCEPT_FALLBACK_CONFIRM_COLOR = '#a9855b';
const DEFAULT_BLOCK_MESSAGE = '内容包含违规信息，请修改后重试';
/** 内置拦截词库（本地硬编码，发送时不请求后端） */
const LOCAL_BLOCK_WORDS = [
    '台独', '藏独', '疆独', '港独', '分裂国家', '颠覆政权', '反动宣传', '法轮功', '反党', '反政府', '政治谣言', '煽动颠覆',
    '色情', '淫秽', '黄片', '裸聊', '援交', '卖淫', '嫖娼', '一夜情', '约炮', '成人视频', '色情网站', '开房', '性交易',
    '买凶', '雇凶', '雇人杀人', '制作炸弹', '爆炸物制作', '恐怖袭击', '血腥虐杀', '人体炸弹',
    '冰毒', '海洛因', '贩毒', '吸毒', '假币', '办证', '刻章', '枪支买卖', '买卖枪支', '洗钱', '诈骗教程', '翻墙软件',
    '网络赌博', '博彩', '六合彩', '时时彩', '赌球', '赌博平台', '线上赌场',
    '傻逼', '傻b', '操你', '草你', '去死吧', '贱人', '狗日的', '妈的', 'nmsl',
    '加微信', '加v', '刷单', '日赚千元', '兼职打字', '免费领取', '点击链接', '扫码领红包',
];
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
const NORMALIZED_BLOCK_WORDS = LOCAL_BLOCK_WORDS
    .map((word) => normalizeText(word))
    .filter((word) => !!word)
    .sort((a, b) => b.length - a.length);
function findBlockedWord(normalized) {
    if (!normalized) {
        return '';
    }
    for (let i = 0; i < NORMALIZED_BLOCK_WORDS.length; i += 1) {
        const keyword = NORMALIZED_BLOCK_WORDS[i];
        if (keyword && normalized.indexOf(keyword) >= 0) {
            return keyword;
        }
    }
    return '';
}
function reviewUserInputSync(text, options = {}) {
    const scene = options.scene || 'general';
    const trimmed = String(text || '').trim();
    if (!trimmed) {
        if (options.allowEmpty) {
            return { passed: true, scene };
        }
        return { passed: false, message: '请输入内容', scene };
    }
    const hit = findBlockedWord(normalizeText(trimmed));
    if (hit) {
        return {
            passed: false,
            message: DEFAULT_BLOCK_MESSAGE,
            keyword: hit,
            scene,
        };
    }
    return { passed: true, scene };
}
function showPromptReviewRejected(result) {
    const message = result.message || DEFAULT_BLOCK_MESSAGE;
    const app = getApp();
    if (app.showInterceptModal?.({
        title: INTERCEPT_MODAL_TITLE,
        message,
        hint: '请修改内容后再发送。',
    })) {
        return;
    }
    wx.showModal({
        title: INTERCEPT_MODAL_TITLE,
        content: `${message}\n\n请修改内容后再发送。`,
        showCancel: false,
        confirmText: '我知道了',
        confirmColor: INTERCEPT_FALLBACK_CONFIRM_COLOR,
    });
}
/** 同步拦截：返回 true = 已拦截，禁止继续发送 */
function blockUnsafeText(text, options = {}) {
    const result = reviewUserInputSync(text, options);
    if (!result.passed) {
        showPromptReviewRejected(result);
        return true;
    }
    return false;
}
/** 批量检查多段文本，任一段命中则拦截 */
function blockAnyUnsafeTexts(texts, options = {}) {
    for (let i = 0; i < texts.length; i += 1) {
        const trimmed = String(texts[i] || '').trim();
        if (trimmed && blockUnsafeText(trimmed, options)) {
            return true;
        }
    }
    return false;
}
function ensurePromptPassed(text, options = {}) {
    return !blockUnsafeText(text, options);
}
async function initPromptReview() {
    // 发送前审核纯本地，无需初始化
}
async function reviewUserInput(text, options = {}) {
    return reviewUserInputSync(text, options);
}
async function refreshPromptReviewPool() {
    // 保留接口，当前不发网络请求
}
