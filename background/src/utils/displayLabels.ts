// 后台显示用的内部代码翻译表：页面上只出现中文，代码原值只留在详情或 title 提示里。

// AI 场景中文名，与后端 service/route/ai_overview_management.go 的 aiSceneMeta 同源
export const AI_SCENE_LABELS: Record<string, string> = {
    ai_draw_single: 'AI 绘画-单图',
    ai_draw_multi: 'AI 绘画-多图',
    new_minnan_tool_text_to_image: '新闽派工具-文生图',
    new_minnan_tool_image_to_image: '新闽派工具-图生图',
    ai_chat_single: 'AI 对话-单轮',
    ai_chat_multi: 'AI 对话-多轮',
    building_code_chat: '建规快查',
    rural_villa_design: '乡村别墅设计',
    allround_design: '全能设计',
    building_replacement: '建筑换新',
    parent_child_design: '亲子设计',
    ai_cost_doc: 'AI 造价文档',
    ai_video_1: 'AI 视频-1段',
    ai_video_2: 'AI 视频-2段',
    ai_video_3: 'AI 视频-3段',
    ai_video_4: 'AI 视频-4段',
};

export const formatAiScene = (scene?: string | null): string =>
    scene ? AI_SCENE_LABELS[scene] || scene : '-';

// 历史订单标题、工单正文里夹着场景代码（如「AI生成-ai_draw_single-3张」），逐个换成中文名
export const replaceSceneCodes = (text?: string | null): string =>
    String(text || '').replace(/[a-z][a-z0-9]*(?:_[a-z0-9]+)+/g, code => AI_SCENE_LABELS[code] || code);

const TASK_STATUS_LABELS: Record<string, string> = {
    pending: '待处理',
    queued: '排队中',
    running: '处理中',
    processing: '处理中',
    success: '成功',
    completed: '成功',
    failed: '失败',
    cancelled: '已取消',
    timeout: '超时',
};

export const formatTaskStatus = (status?: string | null): string =>
    status ? TASK_STATUS_LABELS[status] || status : '-';

// 主力绘图模型与备用模型前缀；运营只需要知道这次走的是主力还是备用服务
const PRIMARY_DRAW_MODEL = 'gpt-image-2-vip';
const FALLBACK_DRAW_MODELS = ['gemini-3-pro', 'seedream'];
const MODEL_PENDING = '模型待回填';

export const isFallbackModel = (model?: string | null): boolean => {
    const value = String(model || '').trim().toLowerCase();
    if (!value || value === MODEL_PENDING) return false;
    if (value.startsWith(PRIMARY_DRAW_MODEL)) return false;
    return FALLBACK_DRAW_MODELS.some(prefix => value.startsWith(prefix));
};

const isPrimaryModel = (model: string): boolean => model.toLowerCase().startsWith('gpt-image');

/** 已知的主力/备用绘图模型显示成「主力服务 / 备用服务」；认不出的模型原样显示，不冒充主力 */
export const formatAiModel = (model?: string | null): string => {
    const value = String(model || '').trim();
    if (!value || value === '-') return '-';
    if (isFallbackModel(value)) return '备用服务';
    return isPrimaryModel(value) ? '主力服务' : value;
};

/** 32 位任务号、分享号等长编号只显示后 6 位；完整编号放进 title 或详情 */
export const shortCode = (code?: string | number | null, keep = 6): string => {
    const value = String(code ?? '').trim();
    return value.length > 12 ? `…${value.slice(-keep)}` : value;
};

// 只认后端生成的 32 位小写十六进制任务号，避免误伤用户粘贴的订单号、交易号
const TASK_NO = /\b[0-9a-f]{32}\b/g;
const TASK_TYPE_LABELS: Record<string, string> = { image: '图片任务', video: '视频任务' };

// 后端写入工单/告警的任务描述形如「任务状态：failed；场景：ai_draw_single；模型：seedream-…」
export const humanizeTaskText = (text?: string | null): string =>
    replaceSceneCodes(text)
        .replace(/(状态：)([a-z_]+)/g, (_, head: string, status: string) => head + formatTaskStatus(status))
        .replace(/(任务类型：)(image|video)\b/g, (_, head: string, type: string) => head + TASK_TYPE_LABELS[type])
        .replace(/(模型：)([A-Za-z][\w.-]*)/g, (_, head: string, model: string) => head + formatAiModel(model))
        .replace(TASK_NO, code => `尾号 ${code.slice(-6)}`);

// 线索来源友好名；share_tracking = 分享访问记录的意向转化（管理端一键转化生成）。
const LEAD_SOURCE_LABELS: Record<string, string> = {
    share_tracking: '分享意向',
    structured_requirement: '客服需求表单',
    generate_click: '生成方案',
    handoff_human: '转人工留资',
    home: '在线客服',
    // 小程序进入客服页时带的 source（见 miniprogram 各页面 customerservice?source=）
    home_mascot: '首页客服入口',
    generatedetails: '方案详情页',
    aicost: 'AI 造价结果',
    content_reader: '内容阅读页',
};

export const formatLeadSource = (source?: string | null): string =>
    source ? LEAD_SOURCE_LABELS[source] || '其他来源' : '-';
