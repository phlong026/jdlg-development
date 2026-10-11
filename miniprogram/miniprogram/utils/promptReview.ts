/**
 * 用户输入本地审核 — 纯前端同步拦截，不依赖后端
 */

export type PromptReviewScene =
  | 'prompt'
  | 'nickname'
  | 'comment'
  | 'chat'
  | 'search'
  | 'general';

export interface PromptReviewOptions {
  scene?: PromptReviewScene;
  minLength?: number;
  maxLength?: number;
  allowEmpty?: boolean;
}

export interface PromptReviewResult {
  passed: boolean;
  message?: string;
  keyword?: string;
  scene?: PromptReviewScene;
}

const INTERCEPT_MODAL_TITLE = '内容拦截';
const INTERCEPT_FALLBACK_CONFIRM_COLOR = '#a9855b';
const DEFAULT_BLOCK_MESSAGE = '内容包含违规信息，请修改后重试';

/** 内置拦截词库（本地硬编码，发送时不请求后端） */
const LOCAL_BLOCK_WORDS: string[] = [
  '台独', '藏独', '疆独', '港独', '分裂国家', '颠覆政权', '反动宣传', '法轮功', '反党', '反政府', '政治谣言', '煽动颠覆',
  '色情', '淫秽', '黄片', '裸聊', '援交', '卖淫', '嫖娼', '一夜情', '约炮', '成人视频', '色情网站', '开房', '性交易',
  '买凶', '雇凶', '雇人杀人', '制作炸弹', '爆炸物制作', '恐怖袭击', '血腥虐杀', '人体炸弹',
  '冰毒', '海洛因', '贩毒', '吸毒', '假币', '办证', '刻章', '枪支买卖', '买卖枪支', '洗钱', '诈骗教程', '翻墙软件',
  '网络赌博', '博彩', '六合彩', '时时彩', '赌球', '赌博平台', '线上赌场',
  '傻逼', '傻b', '操你', '草你', '去死吧', '贱人', '狗日的', '妈的', 'nmsl',
  '加微信', '加v', '刷单', '日赚千元', '兼职打字', '免费领取', '点击链接', '扫码领红包',
];

function normalizeText(input: string): string {
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

function findBlockedWord(normalized: string): string {
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

export function reviewUserInputSync(
  text: string,
  options: PromptReviewOptions = {},
): PromptReviewResult {
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

export function showPromptReviewRejected(result: PromptReviewResult): void {
  const message = result.message || DEFAULT_BLOCK_MESSAGE;
  const app = getApp<IAppOption>();
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
export function blockUnsafeText(
  text: string,
  options: PromptReviewOptions = {},
): boolean {
  const result = reviewUserInputSync(text, options);
  if (!result.passed) {
    showPromptReviewRejected(result);
    return true;
  }
  return false;
}

/** 批量检查多段文本，任一段命中则拦截 */
export function blockAnyUnsafeTexts(
  texts: Array<string | undefined | null>,
  options: PromptReviewOptions = {},
): boolean {
  for (let i = 0; i < texts.length; i += 1) {
    const trimmed = String(texts[i] || '').trim();
    if (trimmed && blockUnsafeText(trimmed, options)) {
      return true;
    }
  }
  return false;
}

export function ensurePromptPassed(
  text: string,
  options: PromptReviewOptions = {},
): boolean {
  return !blockUnsafeText(text, options);
}

export async function initPromptReview(): Promise<void> {
  // 发送前审核纯本地，无需初始化
}

export async function reviewUserInput(
  text: string,
  options: PromptReviewOptions = {},
): Promise<PromptReviewResult> {
  return reviewUserInputSync(text, options);
}

export async function refreshPromptReviewPool(): Promise<void> {
  // 保留接口，当前不发网络请求
}
