import type { IntentEvidence } from '../../api/shareTracking';
export type Filters = { range: string; start: string; end: string; identity: string; intent: string; owner: string; q: string; sort: string; direction: string; content_q?: string; visitor_q?: string; visitor?: string };
export const defaultFilters: Filters = { range: '7', start: '', end: '', identity: '', intent: '', owner: '', q: '', sort: 'last_visit', direction: 'desc' };
export function shanghaiDate(now = new Date()): string { return new Date(now.getTime() + 8 * 3600000).toISOString().slice(0, 10); }
export function dateRange(filters: Filters, now = new Date()): { start?: string; end?: string } {
    if (filters.range === 'all') return {};
    let start: string, end: string;
    if (filters.range === 'custom') {
        start = filters.start; end = filters.end;
        const valid = (value: string) => /^\d{4}-\d{2}-\d{2}$/.test(value) && !Number.isNaN(Date.parse(value)) && new Date(value).toISOString().slice(0, 10) === value;
        if (!valid(start) || !valid(end) || start > end) throw new Error('请选择有效的起止日期，开始日期不能晚于结束日期');
    } else {
        const days = Number(filters.range);
        if (![3, 7, 30].includes(days)) throw new Error('无效时间范围');
        end = shanghaiDate(now);
        start = new Date(Date.parse(end) - (days - 1) * 86400000).toISOString().slice(0, 10);
    }
    return { start: new Date(`${start}T00:00:00+08:00`).toISOString(), end: new Date(Date.parse(`${end}T00:00:00+08:00`) + 86400000).toISOString() };
}
export function queryFor(filters: Filters, now = new Date(), view: 'visitors' | 'contents' = 'visitors'): Record<string, unknown> { return { ...dateRange(filters, now), identity: filters.identity, ...(view === 'contents' ? { content_q: (filters.content_q || '').trim(), visitor_q: (filters.visitor_q || '').trim() } : { intent: filters.intent, q: filters.q.trim() }), owner: filters.owner, sort: filters.sort, direction: filters.direction, ...(filters.visitor && view === 'visitors' ? { visitor: filters.visitor } : {}), limit: 20 }; }
export function time(value?: string): string { if (!value) return '未记录'; const date = new Date(value); return Number.isNaN(date.getTime()) ? '时间不可用' : new Intl.DateTimeFormat('zh-CN', { timeZone: 'Asia/Shanghai', year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', second: '2-digit', hour12: false }).format(date); }
export function duration(ms = 0): string { const seconds = Math.floor(Math.max(0, ms) / 1000); return seconds < 60 ? `${seconds}秒` : seconds < 3600 ? `${Math.floor(seconds / 60)}分${seconds % 60}秒` : `${Math.floor(seconds / 3600)}小时${Math.floor(seconds % 3600 / 60)}分`; }
export function safeMediaURL(value?: string): string | undefined { if (!value) return; try { const url = new URL(value, window.location.origin); return (url.protocol === 'https:' || (url.origin === window.location.origin && url.protocol === window.location.protocol)) && !url.username && !url.password ? url.href : undefined; } catch { return; } }
export const intentLabels = { high: '高意向参考', medium: '中意向参考', low: '低关注待观察', unknown: '待判断' };
// 精准客户门控展示口径，与后端 trackingLeadGate 一致：
// 乡墅业主可转销售线索；设计师/施工队/建材商/机构用户是渠道而非客户；未选身份留给人工核实。
const B_SIDE_IDENTITIES = new Set(['设计师', '施工队', '建材商', '机构用户']);
export function identityBadge(identityState: string, identityType?: string): { label: string; tone: 'customer' | 'bside' | 'unset' } | null {
    if (identityState !== 'identified') return null;
    const identity = (identityType || '').trim();
    if (!identity) return { label: '身份未确认', tone: 'unset' };
    if (identity === '乡墅业主') return { label: '乡墅业主', tone: 'customer' };
    if (B_SIDE_IDENTITIES.has(identity)) return { label: `B端 · ${identity}`, tone: 'bside' };
    return { label: identity, tone: 'unset' };
}
export const eventLabels: Record<string, string> = { share_intent: '发起分享（非送达）', page_view: '页面访问', page_end: '页面离开', visible_segment: '可见分段', content_click: '内容点击（非已读）', image_open: '打开原图', outbound_click: '点击外部原文（外部停留未知）', scroll_depth: '滚动到达位置', video_state: '播放状态', video_progress: '播放进度（非观看完成）', business_action_confirmed: '已验证业务行为' };
export function qualityText(flags: string[] = []): string { const names: Record<string, string> = { late: '离线或迟到补报', incomplete: '结束事件缺失，记录不完整', inferred_seek: '推断播放跳转', sampling_gap: '采样间隔缺失', preview: '后台预览', self: '分享人自访', test: '测试流量', sequence_conflict: '分段序号冲突', clock_skew: '客户端时间偏差', clamped: '异常增量已封顶' }; return flags.map(flag => names[flag] || flag).join('、'); }
export function intentMetricText(value: string): string { const labels: Record<string, string> = { sessions: '有效会话', independent_sessions: '独立会话', favorites: '已确认收藏', server_favorite: '服务端确认收藏', business_results: '已确认业务结果', server_consultation: '服务端确认咨询', server_appointment: '服务端确认预约', server_quote: '服务端确认报价' }; return labels[value] || value; }
export function intentExclusionText(value: string): string { const labels: Record<string, string> = { anonymous_identity: '匿名身份', preview: '后台预览', self: '分享人自访', test: '测试流量', abnormal: '异常流量', duplicate: '重复事件', client_click: '客户端点击', unverified_business: '未经服务端确认的业务行为', daily_contribution_over_cap: '超过每日贡献上限的部分', bside_identity: 'B端账号（仅渠道观察）' }; return labels[value] || value; }

export function intentWindowQuery(query: Record<string, unknown>, evidence?: IntentEvidence): Record<string, unknown> | undefined {
    if (!evidence || evidence.scope_required) return;
    const start = Date.parse(evidence.window_start), end = Date.parse(evidence.window_end);
    if (!Number.isFinite(start) || !Number.isFinite(end) || start >= end) return;
    // Let the server include the exact as_of boundary; preserve its timestamp precision.
    return { ...query, start: evidence.window_start, end: undefined, as_of: evidence.window_end, cursor: undefined, content_id: undefined, content_version: undefined, content_q: undefined, visitor_q: undefined, identity: undefined, intent: undefined, q: undefined };
}

// 小程序页面路径 → 客户可读名称；未登记的路径一律显示"其他页面"，不暴露代码路径。
const PAGE_NAMES: Record<string, string> = {
  'pages/index/index': '首页',
  'pages/template/template': '模板广场',
  'pages/distribution/distribution': '设计师中心',
  'pages/my/my': '我的',
  'pages/customerservice/customerservice': '在线客服',
  'pages/aitools/aitools': 'AI 工具',
  'pages/aitooldetail/aitooldetail': 'AI 工具详情',
  'pages/aitoolworkbench/aitoolworkbench': 'AI 工具工作台',
  'pages/specadvisor/specadvisor': '建房顾问',
  'pages/buildingcodedocs/buildingcodedocs': '规范资料',
  'pages/newminnantool/newminnantool': '闽南工艺工具',
  'pages/voicecall/voicecall': '语音通话',
  'pages/login/login': '登录页',
  'pages/passwordrecovery/passwordrecovery': '找回密码',
  'pages/phonebind/phonebind': '绑定手机',
  'pages/privacy/privacy': '隐私政策',
  'pages/aigenerate/aigenerate': 'AI 生成',
  'pages/generateprogress/generateprogress': '生成进度',
  'pages/inspirationdetail/inspirationdetail': '灵感详情',
  'pages/templatepreview/templatepreview': '模板预览',
  'pages/settings/settings': '设置',
  'pages/aicost/aicost': '造价测算',
  'pages/aicostresult/aicostresult': '造价测算结果',
  'pages/utilitytools/utilitytools': '实用工具',
  'pages/utilitytooldetail/utilitytooldetail': '实用工具详情',
  'pages/aivideo/aivideo': 'AI 视频',
  'pages/real/real': '实景案例',
  'pages/report/landing': '报告落地页',
  'pages/report/questionnaire': '需求问卷',
  'pages/report/preview': '报告预览',
  'pages/report/delivery': '报告交付',
  'pages/generatedetails/generatedetails': '作品详情',
  'pages/Invitefriends/Invitefriends': '邀请好友',
  'pages/chat/chat': 'AI 对话',
  'pages/designerhome/designerhome': '设计师主页',
  'pages/myInformationmodification/myInformationmodification': '资料修改',
  'pages/templatesquaredetails/templatesquaredetails': '模板详情',
  'pages/myplan/myplan': '我的方案',
  'pages/myfavorites/myfavorites': '我的收藏',
  'pages/ordermanagement/ordermanagement': '我的订单',
  'pages/wallet/wallet': '钱包',
  'pages/ruralvilladesign/ruralvilladesign': '乡村别墅设计',
  'pages/Parentchildcreativity/Parentchildcreativity': '亲子创意',
  'pages/topupcenter/topupcenter': '充值中心',
  'pages/allrounddesign/allrounddesign': '全案设计',
  'pages/release/release': '我要发布',
  'pages/release/designerworkpublish': '作品发布',
  'pages/buildingreplacement/buildingreplacement': '建筑改造',
  'pages/checkin/checkin': '每日签到',
  'pages/Identityauthen/Identityauthen': '身份认证',
  'pages/generatehistory/generatehistory': '生成记录',
  'pages/lingshidetails/lingshidetails': '灵石明细',
  'pages/orderdetail/orderdetail': '订单详情',
  'pages/messagecenter/messagecenter': '消息中心',
  'pages/adminlogin/adminlogin': '后台登录',
  'pages/adminhome/adminhome': '后台管理',
  'pages/withdrawcenter/withdrawcenter': '提现中心',
  'pages/contentreader/contentreader': '分享内容阅读',
};
export function pageLabel(routeKey?: string, title?: string): string {
  const mapped = PAGE_NAMES[routeKey || ''];
  if (mapped) return mapped;
  // 小程序端可能把路由串当标题上报（如 pages/index/index），这种 title 不算可读名称。
  if (title && !title.startsWith('pages/')) return title;
  return '其他页面';
}
