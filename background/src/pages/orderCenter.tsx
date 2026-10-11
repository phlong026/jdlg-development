import { reportPageLoadError } from '../utils/pageLoadError';
import React, { useEffect, useRef, useState } from 'react';
import { FiAlertCircle, FiEye, FiRefreshCw, FiRotateCcw, FiSearch,
    FiDownload,
} from 'react-icons/fi';
import { useNavigate, useSearchParams } from 'react-router-dom';
import {
    advanceVirtualPayOrder,
    createOrderSupportTicket,
    getRefundPreview,
    getOrderDetail,
    getOrderFundStats,
    getOrderList,
    refundVirtualPayOrder,
    verifyRefundAgainstWechat,
    type OrderDetail,
    type OrderFundStats,
    type OrderListItem,
    type OrderListParams,
    type OrderSummary,
    type RefundPreview,
} from '../api/orders';
import { getXPayLogs, type XPayMessageLogItem } from '../api/virtualPay';
import { API_BASE_URL } from '../config/api';
import './orderCenter.scss';

const formatOrderType = (value: string) => {
    const map: Record<string, string> = {
        recharge: '充值',
        consume: 'AI消费',
        culture: '内容消费',
        withdraw: '提现',
        certification: '认证费',
        report: '建房报告',
    };
    return map[value] || value || '-';
};

const formatOrderCategory = (value: string) => {
    const map: Record<string, string> = {
        recharge: '充值',
        ai: 'AI生成',
        template: '模板',
        certification: '认证',
        withdraw: '提现',
        service: '设计服务',
        report: '建房报告',
        enterprise_recharge: '企业充值',
    };
    return map[value] || value || '-';
};

// 状态中文映射（T4）：delivering/refunding 是资金流转的中间态，必须给中文，
// 否则列表直接显示英文原值；卡单由"仅看异常单"的服务端条件兜底捕获。
const formatStatus = (value: string) => {
    const map: Record<string, { label: string; className: string }> = {
        pending: { label: '待处理', className: 'status-pending' },
        delivering: { label: '发货中', className: 'status-pending' },
        success: { label: '成功', className: 'status-success' },
        refunding: { label: '退款中', className: 'status-pending' },
        refunded: { label: '已退款', className: 'status-cancelled' },
        failed: { label: '失败', className: 'status-failed' },
        cancelled: { label: '已取消', className: 'status-cancelled' },
    };
    if (map[value]) return map[value];
    if (value === 'refunding') return { label: '退款中', className: 'status-pending' };
    if (value === 'refunded') return { label: '已退款', className: 'status-cancelled' };
    return { label: value || '-', className: '' };
};

const formatPayChannel = (value?: string) => {
    const map: Record<string, string> = {
        wechatpay: '微信支付',
        virtualpay: '虚拟支付',
    };
    return map[value] || value || '-';
};

const formatPayMode = (value?: string) => {
    const map: Record<string, string> = {
        short_series_coin: '代币',
        short_series_goods: '道具',
    };
    return map[value] || value || '-';
};

const formatXPayEvent = (value: string) => {
    const map: Record<string, string> = {
        xpay_coin_pay_notify: '代币支付',
        xpay_goods_deliver_notify: '道具发货',
        xpay_refund_notify: '退款通知',
        xpay_complaint_notify: '投诉',
    };
    return map[value] || value || '-';
};

const isNegativeOrderValue = (item: Pick<OrderListItem, 'amount' | 'type' | 'order_category'>) => {
    const negativeTypes = new Set(['consume', 'culture', 'withdraw', 'certification']);
    return Number(item.amount || 0) < 0 || negativeTypes.has(item.type);
};

const formatOrderValue = (item: Pick<OrderListItem, 'amount' | 'type' | 'order_category'>) => {
    const amount = Number(item.amount || 0);
    const isCurrency = item.type === 'certification';
    const absAmount = Math.abs(amount);
    const sign = isNegativeOrderValue(item) ? '-' : '';
    if (isCurrency) {
        return `${sign}¥${absAmount}`;
    }
    return `${sign}${absAmount} 灵石`;
};

/** 支付金额（元）：真实人民币金额在 amount_fen（T5 口径；认证费/报告此前显示 0 的问题在此修复） */
const formatAmountYuan = (amountFen?: number) => {
    if (!amountFen || amountFen <= 0) return '-';
    return `¥${(amountFen / 100).toFixed(2)}`;
};

const formatDateTime = (value?: string | null) => {
    if (!value) return '-';
    const date = new Date(value);
    if (Number.isNaN(date.getTime())) return value;
    return date.toLocaleString('zh-CN');
};

const canRefundVirtualPayOrder = (order: Pick<OrderListItem, 'pay_channel' | 'pay_mode' | 'type' | 'status'>) => (
    order.pay_channel === 'virtualpay'
    && order.pay_mode === 'short_series_coin'
    && order.type === 'recharge'
    && order.status === 'success'
);

const emptySummary: OrderSummary = {
    total_count: 0,
    success_amount: 0,
    success_count: 0,
    pending_count: 0,
};

const DEFAULT_PAGE_SIZE = 50;

const getFilterSummary = (params: {
    keyword: string;
    type: string;
    category: string;
    status: string;
    payChannel: string;
    start: string;
    end: string;
    onlyExceptions: boolean;
}) => {
    const parts: string[] = [];
    if (params.keyword) parts.push(`关键词：${params.keyword}`);
    if (params.type !== 'all') parts.push(`类型：${formatOrderType(params.type)}`);
    if (params.category !== 'all') parts.push(`分类：${formatOrderCategory(params.category)}`);
    if (params.status !== 'all') parts.push(`状态：${formatStatus(params.status).label}`);
    if (params.payChannel !== 'all') parts.push(`支付通道：${formatPayChannel(params.payChannel)}`);
    if (params.start || params.end) parts.push(`日期：${params.start || '不限'} ~ ${params.end || '不限'}`);
    if (params.onlyExceptions) parts.push('仅看异常单（含卡发货/卡退款）');
    return parts.length > 0 ? parts.join(' ｜ ') : '当前正在查看全部订单数据。';
};

const formatNumber = (value?: number) => (typeof value === 'number' ? value.toLocaleString('zh-CN') : '-');

const formatDate = (d: Date) => {
    const y = d.getFullYear();
    const m = String(d.getMonth() + 1).padStart(2, '0');
    const day = String(d.getDate()).padStart(2, '0');
    return `${y}-${m}-${day}`;
};

/** 日期快捷档位（G2）：与筛选日期输入双向同步 */
const buildDateRangeByPreset = (preset: string): { start: string; end: string } => {
    const today = new Date();
    if (preset === 'today') return { start: formatDate(today), end: formatDate(today) };
    if (preset === 'yesterday') {
        const y = new Date(today.getTime() - 86400000);
        return { start: formatDate(y), end: formatDate(y) };
    }
    if (preset === '7d') return { start: formatDate(new Date(today.getTime() - 6 * 86400000)), end: formatDate(today) };
    if (preset === '30d') return { start: formatDate(new Date(today.getTime() - 29 * 86400000)), end: formatDate(today) };
    return { start: '', end: '' };
};

const matchDatePreset = (range: { start: string; end: string }): string => {
    for (const preset of ['today', 'yesterday', '7d', '30d']) {
        const candidate = buildDateRangeByPreset(preset);
        if (candidate.start === range.start && candidate.end === range.end) return preset;
    }
    return '';
};

/** 状态 Tab 定义（G2）：顺序即展示顺序，计数来自服务端按当前筛选聚合 */
const STATUS_TABS: { value: string; label: string }[] = [
    { value: 'all', label: '全部' },
    { value: 'success', label: '成功' },
    { value: 'pending', label: '待处理' },
    { value: 'delivering', label: '发货中' },
    { value: 'refunding', label: '退款中' },
    { value: 'refunded', label: '已退款' },
    { value: 'failed', label: '失败' },
    { value: 'cancelled', label: '已取消' },
];

const OrderCenter: React.FC = () => {
    const navigate = useNavigate();
    const [searchParams, setSearchParams] = useSearchParams();
    const latestSearchParams = useRef(searchParams);
    latestSearchParams.current = searchParams;
    const [records, setRecords] = useState<OrderListItem[]>([]);
    const [summary, setSummary] = useState<OrderSummary>(emptySummary);
    const [loading, setLoading] = useState(false);
    // 支持从其它页面带 orderNo 参数直达（如虚拟支付监控的"订单"跳转）：作为初始关键词检索
    const [searchKeyword, setSearchKeyword] = useState(() => searchParams.get('orderNo') || searchParams.get('kw') || '');
    const [typeFilter, setTypeFilter] = useState(() => searchParams.get('type') || 'all');
    const [categoryFilter, setCategoryFilter] = useState(() => searchParams.get('cat') || 'all');
    const [statusFilter, setStatusFilter] = useState(() => searchParams.get('st') || 'all');
    const [payChannelFilter, setPayChannelFilter] = useState(() => searchParams.get('ch') || 'all');
    const [onlyExceptions, setOnlyExceptions] = useState(() => searchParams.get('ex') === '1');
    const [dateRange, setDateRange] = useState(() => ({ start: searchParams.get('s') || '', end: searchParams.get('e') || '' }));
    const [datePreset, setDatePreset] = useState(() => matchDatePreset({ start: searchParams.get('s') || '', end: searchParams.get('e') || '' }));
    const [fundStats, setFundStats] = useState<OrderFundStats | null>(null);
    const [statusCounts, setStatusCounts] = useState<Record<string, number>>({});
    const [showDetailModal, setShowDetailModal] = useState(false);
    const [selectedDetail, setSelectedDetail] = useState<OrderDetail | null>(null);
    const [detailLoading, setDetailLoading] = useState(false);
    const [detailError, setDetailError] = useState('');
    const detailRequestId = useRef(0);
    const detailTargetId = useRef<string | null>(null);
    const [detailLogs, setDetailLogs] = useState<XPayMessageLogItem[]>([]);
    const [creatingTicketOrderId, setCreatingTicketOrderId] = useState<number | null>(null);
    const [refundingOrderId, setRefundingOrderId] = useState<number | null>(null);
    const refundSubmitting = useRef(false);
    const [actingOrderId, setActingOrderId] = useState<number | null>(null);
    // 退款确认弹窗（T8）：先取影响预览，确认后才真正发起退款
    const [refundPreviewOpen, setRefundPreviewOpen] = useState(false);
    const [refundPreviewOrder, setRefundPreviewOrder] = useState<Pick<OrderListItem, 'id' | 'order_no'> | null>(null);
    const [refundPreview, setRefundPreview] = useState<RefundPreview | null>(null);
    const [refundPreviewLoading, setRefundPreviewLoading] = useState(false);
    const [refundPreviewError, setRefundPreviewError] = useState('');
    const refundPreviewRequestId = useRef(0);
    const refundPreviewTargetId = useRef<number | null>(null);
    const renderedRefundPreviewRequestId = refundPreviewRequestId.current;
    const [currentPage, setCurrentPage] = useState(1);
    const [pageSize] = useState(DEFAULT_PAGE_SIZE);
    const [total, setTotal] = useState(0);
    const totalPages = Math.max(1, Math.ceil(total / pageSize));

    // 关闭只作废预览请求，不取消或触发实际退款。
    const resetRefundPreview = () => {
        refundPreviewRequestId.current += 1;
        refundPreviewTargetId.current = null;
        setRefundPreviewOpen(false);
        setRefundPreviewOrder(null);
        setRefundPreview(null);
        setRefundPreviewLoading(false);
        setRefundPreviewError('');
    };

    const closeRefundPreview = () => {
        if (refundingOrderId || refundSubmitting.current) return;
        resetRefundPreview();
    };

    useEffect(() => () => {
        // 卸载后不得由迟到的详情/预览成功、失败或 finally 回写状态。
        detailRequestId.current += 1;
        detailTargetId.current = null;
        refundPreviewRequestId.current += 1;
        refundPreviewTargetId.current = null;
    }, []);

    const resetOrderDetail = () => {
        detailRequestId.current += 1;
        detailTargetId.current = null;
        resetRefundPreview();
        setShowDetailModal(false);
        setSelectedDetail(null);
        setDetailLogs([]);
        setDetailLoading(false);
        setDetailError('');
    };

    // 列表、URL 与退款后刷新共用一条加载路径，响应必须仍属于当前详情。
    const loadOrderDetail = async (orderId: string) => {
        const requestId = ++detailRequestId.current;
        detailTargetId.current = orderId;
        resetRefundPreview();
        setSelectedDetail(null);
        setDetailLogs([]);
        setDetailError('');
        setDetailLoading(true);
        setShowDetailModal(true);
        try {
            const detail = await getOrderDetail(orderId);
            if (requestId !== detailRequestId.current || detailTargetId.current !== orderId) return;
            if (String(detail?.order?.id) !== orderId) throw new Error('返回的订单详情与当前订单不一致，请重新加载');
            setSelectedDetail(detail);
        } catch (error: any) {
            if (requestId !== detailRequestId.current || detailTargetId.current !== orderId) return;
            console.error('加载订单详情失败:', error);
            setDetailError(error?.message || '加载订单详情失败，请重新加载');
        } finally {
            if (requestId === detailRequestId.current) setDetailLoading(false);
        }
    };

    const buildOrderQueryParams = (overrides?: Partial<OrderListParams>): OrderListParams => ({
        keyword: searchKeyword.trim(),
        type: typeFilter,
        order_category: categoryFilter,
        // "仅看异常单"交给后端对全量数据筛选（status=exception = 失败 + 待处理 + 卡发货/卡退款）；
        // 只在当前页前端过滤会漏掉其他页的异常单，计数也会失真。
        status: onlyExceptions ? 'exception' : statusFilter,
        pay_channel: payChannelFilter,
        start_date: dateRange.start,
        end_date: dateRange.end,
        page: currentPage,
        page_size: pageSize,
        ...overrides,
    });

    // 筛选条件同步进 URL（G2）：刷新/转发不丢失；orderId 详情参数保持不动
    const syncFiltersToUrl = (params: OrderListParams) => {
        const next = new URLSearchParams();
        // 列表/退款刷新可能晚于详情切换，保留最新 URL 的目标，不能恢复旧详情。
        const orderId = latestSearchParams.current.get('orderId');
        if (orderId) next.set('orderId', orderId);
        if (params.keyword) next.set('kw', params.keyword);
        if (params.type && params.type !== 'all') next.set('type', params.type);
        if (params.order_category && params.order_category !== 'all') next.set('cat', params.order_category);
        if (params.status && params.status !== 'all' && params.status !== 'exception') next.set('st', params.status);
        if (params.pay_channel && params.pay_channel !== 'all') next.set('ch', params.pay_channel);
        if (params.start_date) next.set('s', params.start_date);
        if (params.end_date) next.set('e', params.end_date);
        if (onlyExceptions) next.set('ex', '1');
        setSearchParams(next, { replace: true });
    };

    const loadOrders = async (params?: OrderListParams) => {
        setLoading(true);
        try {
            const effective: OrderListParams = {
                page: currentPage,
                page_size: pageSize,
                ...params,
            };
            const [response] = await Promise.all([
                getOrderList(effective),
                // 全站资金统计与列表同刷（口径全局，不随筛选变化）
                getOrderFundStats().then(setFundStats).catch((error) => {
                    console.error('加载资金统计失败:', error);
                    return null;
                }),
            ]);
            setRecords(response.list || []);
            setSummary(response.summary || emptySummary);
            setStatusCounts(response.status_counts || {});
            setTotal(Number(response.total || 0));
            setCurrentPage(Number(response.page || params?.page || 1));
            syncFiltersToUrl(effective);
        } catch (error) {
            console.error('加载订单中心失败:', error);
            reportPageLoadError(error, '加载订单中心失败');
        } finally {
            setLoading(false);
        }
    };

    useEffect(() => {
        void loadOrders(buildOrderQueryParams({ page: 1 }));
    }, []);

    useEffect(() => {
        const orderId = searchParams.get('orderId');
        if (!orderId) {
            if (detailTargetId.current !== null) resetOrderDetail();
            return;
        }
        if (detailTargetId.current === orderId) return;
        void loadOrderDetail(orderId);
    }, [searchParams]);

    // 详情抽屉打开时拉取该订单的微信推送日志（关联记录，一次轻量查询）
    useEffect(() => {
        if (!showDetailModal || !selectedDetail?.order?.order_no) {
            return;
        }
        let cancelled = false;
        void (async () => {
            try {
                const data = await getXPayLogs({ keyword: selectedDetail.order.order_no, page: 1, page_size: 10 });
                if (!cancelled) setDetailLogs(data.list || []);
            } catch (error) {
                console.error('加载订单推送日志失败:', error);
                if (!cancelled) setDetailLogs([]);
            }
        })();
        return () => { cancelled = true; };
    }, [showDetailModal, selectedDetail?.order?.order_no]);

    const handleSearch = () => {
        setCurrentPage(1);
        void loadOrders(buildOrderQueryParams({ page: 1 }));
    };

    const handleToggleExceptions = () => {
        const next = !onlyExceptions;
        setOnlyExceptions(next);
        setCurrentPage(1);
        void loadOrders(buildOrderQueryParams({ page: 1, status: next ? 'exception' : statusFilter }));
    };

    const handleReset = () => {
        setSearchKeyword('');
        setTypeFilter('all');
        setCategoryFilter('all');
        setStatusFilter('all');
        setPayChannelFilter('all');
        setOnlyExceptions(false);
        setDateRange({ start: '', end: '' });
        setDatePreset('');
        setCurrentPage(1);
        void loadOrders({ page: 1, page_size: pageSize, keyword: '', type: 'all', order_category: 'all', status: 'all', pay_channel: 'all', start_date: '', end_date: '' });
    };

    /** 状态 Tab 点击（G2）：仅看异常单模式下禁用 */
    const handleStatusTabClick = (value: string) => {
        if (onlyExceptions || value === statusFilter) return;
        setStatusFilter(value);
        setCurrentPage(1);
        void loadOrders(buildOrderQueryParams({ page: 1, status: value }));
    };

    /** 日期快捷档位（G2） */
    const handleDatePresetClick = (preset: string) => {
        const range = buildDateRangeByPreset(preset);
        setDatePreset(preset);
        setDateRange(range);
        setCurrentPage(1);
        void loadOrders(buildOrderQueryParams({ page: 1, start_date: range.start, end_date: range.end }));
    };

    const handleViewDetail = (record: OrderListItem) => {
        const nextParams = new URLSearchParams(searchParams);
        nextParams.set('orderId', String(record.id));
        setSearchParams(nextParams);
        void loadOrderDetail(String(record.id));
    };

    const handleCloseDetail = () => {
        resetOrderDetail();
        const nextParams = new URLSearchParams(searchParams);
        nextParams.delete('orderId');
        setSearchParams(nextParams);
    };

    const handleCreateSupportTicket = async (order: Pick<OrderListItem, 'id' | 'order_no'>) => {
        setCreatingTicketOrderId(order.id);
        try {
            const result = await createOrderSupportTicket(String(order.id));
            alert(result.existed ? `该订单已有未关闭工单 #${result.id}，现在带你去工单中心继续跟进。` : `已为订单 ${order.order_no} 创建异常工单 #${result.id}。`);
            navigate('/support-tickets');
        } catch (error) {
            console.error('创建订单异常工单失败:', error);
            alert('创建订单异常工单失败');
        } finally {
            setCreatingTicketOrderId(null);
        }
    };

    // 退款（T8）：先取影响预览（扣回灵石/余额/会员回收计划），用户在弹窗中确认后才执行
    const openRefundPreview = async (order: Pick<OrderListItem, 'id' | 'order_no'>) => {
        if (refundSubmitting.current) return;
        const requestId = ++refundPreviewRequestId.current;
        refundPreviewTargetId.current = order.id;
        setRefundPreviewOpen(true);
        setRefundPreviewOrder(order);
        setRefundPreviewLoading(true);
        setRefundPreview(null);
        setRefundPreviewError('');
        try {
            const preview = await getRefundPreview(String(order.id));
            if (requestId !== refundPreviewRequestId.current) return;
            if (preview?.order_id !== order.id) throw new Error('返回的退款预览与当前订单不一致，请重新加载');
            setRefundPreview(preview);
        } catch (error: any) {
            if (requestId !== refundPreviewRequestId.current) return;
            console.error('获取退款影响预览失败:', error);
            setRefundPreviewError(error?.message || '获取退款影响预览失败，请重试');
        } finally {
            if (requestId === refundPreviewRequestId.current) setRefundPreviewLoading(false);
        }
    };

    const handleConfirmRefund = async () => {
        if (!refundPreview || refundSubmitting.current) return;
        // 旧弹窗保存的回调不可提交；当前响应还需在最终提交前再核对订单归属。
        if (renderedRefundPreviewRequestId !== refundPreviewRequestId.current) return;
        if (!refundPreviewOrder || refundPreviewOrder.id !== refundPreviewTargetId.current) return;
        if (refundPreview.order_id !== refundPreviewTargetId.current) {
            setRefundPreview(null);
            setRefundPreviewError('退款预览与当前订单不一致，请重新加载');
            return;
        }
        const orderId = refundPreview.order_id;
        const reason = window.prompt('请填写退款原因（记入审计日志）', '运营后台退款');
        if (reason === null) return;
        refundSubmitting.current = true;
        setRefundingOrderId(orderId);
        try {
            const result = await refundVirtualPayOrder(String(orderId), reason);
            alert(`退款已提交：${result.order_no}\n扣回灵石：${result.refund_amount}\n余额：${result.old_stones} → ${result.new_stones}\n微信确认退款后，本单授予的会员将自动回收。`);
            resetRefundPreview();
            await loadOrders(buildOrderQueryParams());
            if (detailTargetId.current === String(orderId)) await loadOrderDetail(String(orderId));
        } catch (error: any) {
            console.error('虚拟支付退款失败:', error);
            alert(error?.message || '虚拟支付退款失败');
        } finally {
            refundSubmitting.current = false;
            setRefundingOrderId(null);
        }
    };

    // 卡退款核对微信侧（T6）
    const handleVerifyRefund = async (order: OrderListItem) => {
        setActingOrderId(order.id);
        try {
            const result = await verifyRefundAgainstWechat(String(order.id));
            if (result.settled) {
                alert(`微信侧已确认退款（REFUND），订单 ${result.order_no} 已落终态"已退款"，关联会员已同步回收。`);
            } else {
                alert(`微信侧当前状态：${result.wechat_state}（退款未在微信侧生效）。\n订单保持"退款中"，请核对该用户的扣回灵石与退款是否一致，必要时转人工处理。`);
            }
            await loadOrders(buildOrderQueryParams());
        } catch (error: any) {
            console.error('核对微信侧失败:', error);
            alert(error?.message || '核对微信侧失败');
        } finally {
            setActingOrderId(null);
        }
    };

    // 卡发货立即推进（T6）
    const handleAdvanceOrder = async (order: OrderListItem) => {
        setActingOrderId(order.id);
        try {
            const result = await advanceVirtualPayOrder(String(order.id));
            alert(`推进完成，微信侧状态：${result.state}。`);
            await loadOrders(buildOrderQueryParams());
        } catch (error: any) {
            console.error('推进订单失败:', error);
            alert(error?.message || '推进订单失败');
        } finally {
            setActingOrderId(null);
        }
    };

    const handlePageChange = (page: number) => {
        if (page < 1 || page > totalPages || page === currentPage || loading) {
            return;
        }
        setCurrentPage(page);
        void loadOrders(buildOrderQueryParams({ page }));
    };

    // 对账单导出（G4）：把订单 + 资金视图 + 关联推送就地生成 CSV（含 BOM），财务核对单笔订单用
    const handleExportStatement = () => {
        if (!selectedDetail) return;
        const o = selectedDetail.order;
        const fv = selectedDetail.fund_view;
        const rows: string[][] = [
            ['项目', '内容'],
            ['订单号', o.order_no],
            ['用户', `${selectedDetail.user?.username || `用户${o.user_id}`}（ID ${o.user_id}）`],
            ['业务类型', `${formatOrderType(o.type)} / ${formatOrderCategory(o.order_category)}`],
            ['支付金额(元)', o.amount_fen > 0 ? (o.amount_fen / 100).toFixed(2) : '-'],
            ['灵石变动', String(o.amount)],
            ['支付通道', `${formatPayChannel(o.pay_channel)}${o.pay_mode ? ` · ${formatPayMode(o.pay_mode)}` : ''}`],
            ['状态', formatStatus(o.status).label],
            ['创建时间', formatDateTime(o.created_at)],
            ['完成时间', formatDateTime(o.completed_at)],
        ];
        if (fv && typeof fv.wechat_mirror === 'number') {
            rows.push(
                ['— 资金视图 —', ''],
                ['微信侧在账（镜像）', `${formatNumber(fv.wechat_mirror)} 枚`],
                ['本地可用灵石', `${formatNumber(fv.local_stones)} 枚`],
                ['已本地消费（净差额）', `${formatNumber(fv.consumed_net)} 枚`],
            );
            if (fv.calibrated_at) {
                rows.push(['最近校准', `${formatDateTime(fv.calibrated_at)}（微信实测 ${formatNumber(fv.measured_balance)}，差额 ${formatNumber(fv.diff)}）`]);
            }
        }
        rows.push(['— 关联推送 —', '']);
        detailLogs.forEach((logItem) => {
            rows.push([formatXPayEvent(logItem.event), `${logItem.processed ? '已处理' : '未处理'} · ${formatDateTime(logItem.created_at)}`]);
        });
        const csv = rows.map((cells) => cells.map((cell) => {
            const text = String(cell ?? '');
            const guarded = /^[=+\-@]/.test(text) ? `'${text}` : text;
            return /[",\n]/.test(guarded) ? `"${guarded.replace(/"/g, '""')}"` : guarded;
        }).join(',')).join('\n');
        const blob = new Blob(['\uFEFF' + csv], { type: 'text/csv;charset=utf-8' });
        const url = URL.createObjectURL(blob);
        const link = document.createElement('a');
        link.href = url;
        link.download = `对账单-${o.order_no}.csv`;
        link.click();
        URL.revokeObjectURL(url);
    };

    const detailOrder = selectedDetail?.order;

    return (
        <>
            <div className="order-center-container">
                <div className="order-center-toolbar">
                    <div className="toolbar-left">
                        <div className="search-box">
                            <FiSearch className="search-icon" />
                            <input
                                type="text"
                                placeholder="搜索订单号、用户名或商品标题..."
                                value={searchKeyword}
                                onChange={(event) => setSearchKeyword(event.target.value)}
                                onKeyDown={(event) => {
                                    if (event.key === 'Enter') {
                                        handleSearch();
                                    }
                                }}
                                className="search-input"
                            />
                        </div>
                        <div className="filters">
                            <select className="filter-select" value={typeFilter} onChange={(event) => setTypeFilter(event.target.value)}>
                                <option value="all">全部类型</option>
                                <option value="recharge">充值</option>
                                <option value="consume">AI消费</option>
                                <option value="culture">内容消费</option>
                                <option value="certification">认证费</option>
                                <option value="report">建房报告</option>
                                <option value="withdraw">提现</option>
                            </select>
                            <select className="filter-select" value={categoryFilter} onChange={(event) => setCategoryFilter(event.target.value)}>
                                <option value="all">全部分类</option>
                                <option value="recharge">充值</option>
                                <option value="ai">AI生成</option>
                                <option value="template">模板</option>
                                <option value="certification">认证</option>
                                <option value="report">建房报告</option>
                                <option value="enterprise_recharge">企业充值</option>
                                <option value="service">设计服务</option>
                                <option value="withdraw">提现</option>
                            </select>
                            <select className="filter-select" value={payChannelFilter} onChange={(event) => setPayChannelFilter(event.target.value)}>
                                <option value="all">全部通道</option>
                                <option value="virtualpay">虚拟支付</option>
                                <option value="wechatpay">微信支付</option>
                            </select>
                            <input
                                type="date"
                                className="filter-date"
                                value={dateRange.start}
                                onChange={(event) => { setDateRange({ ...dateRange, start: event.target.value }); setDatePreset(''); }}
                            />
                            <input
                                type="date"
                                className="filter-date"
                                value={dateRange.end}
                                onChange={(event) => { setDateRange({ ...dateRange, end: event.target.value }); setDatePreset(''); }}
                            />
                        </div>
                    </div>
                    <div className="toolbar-right">
                        <button className={`btn-secondary ${onlyExceptions ? 'is-active' : ''}`} onClick={handleToggleExceptions} disabled={loading} aria-pressed={onlyExceptions}>
                            <FiAlertCircle />
                            {onlyExceptions ? '查看全部' : '仅看异常单'}
                        </button>
                        <button
                            className="btn-secondary"
                            title="按当前筛选条件导出 CSV（最多 1 万条，含 BOM 可直接用 Excel 打开）。含支付金额(元)/灵石变动/完成时间列，财务可直接对账"
                            onClick={() => {
                                const params = buildOrderQueryParams();
                                const qs = new URLSearchParams({
                                    keyword: params.keyword || '',
                                    type: params.type,
                                    order_category: params.order_category,
                                    status: params.status,
                                    pay_channel: params.pay_channel,
                                    start_date: params.start_date || '',
                                    end_date: params.end_date || '',
                                });
                                window.open(`${API_BASE_URL}/api/v1/management/orders/export?${qs}`, '_blank');
                            }}
                        >
                            <FiDownload />
                            导出 CSV
                        </button>
                        <button className="btn-primary" onClick={handleSearch}>
                            <FiSearch />
                            查询订单
                        </button>
                        <button className="btn-secondary" onClick={handleReset} disabled={loading}>
                            <FiRefreshCw />
                            {loading ? '刷新中...' : '重置并刷新'}
                        </button>
                    </div>
                </div>

                <div className="order-summary-banner">
                    <div className="order-summary-banner-row">
                        <span>当前筛选摘要</span>
                        <strong>{onlyExceptions ? `${summary.total_count} 条异常` : `${summary.total_count} 条`}</strong>
                    </div>
                    <div className="order-summary-banner-meta">
                        {getFilterSummary({
                            keyword: searchKeyword.trim(),
                            type: typeFilter,
                            category: categoryFilter,
                            status: statusFilter,
                            payChannel: payChannelFilter,
                            start: dateRange.start,
                            end: dateRange.end,
                            onlyExceptions,
                        })}
                    </div>
                    <div className="order-summary-banner-pagination">
                        当前第 {currentPage} / {totalPages} 页，共 {total} 条记录，每页 {pageSize} 条
                    </div>
                </div>

                {/* 全站资金统计带（G1）：口径全局，不随列表筛选变化；已消费 = 微信侧在账 − 本地可用 */}
                <div className="order-center-stats fund-kpi-band">
                    <div className="stat-item">
                        <span className="stat-label">今日充值（元）</span>
                        <span className="stat-value">¥{(((fundStats?.today_recharge_fen) || 0) / 100).toLocaleString('zh-CN', { minimumFractionDigits: 2, maximumFractionDigits: 2 })}</span>
                        <span className="stat-sub">{fundStats?.today_recharge_count || 0} 笔成功 · 虚拟支付</span>
                    </div>
                    <div className="stat-item">
                        <span className="stat-label">今日到账灵石</span>
                        <span className="stat-value">{formatNumber(fundStats?.today_recharge_stones)}</span>
                        <span className="stat-sub">实时进入本地账本</span>
                    </div>
                    <div className="stat-item">
                        <details className="finance-help">
                            <summary className="stat-label">微信侧代币在账 · 口径说明</summary>
                            <p>全站用户的微信虚拟支付代币余额合计 = 累计成功充值 − 已退款（本地镜像口径）</p>
                        </details>
                        <span className="stat-value">{formatNumber(fundStats?.wechat_mirror_total)}</span>
                        <span className="stat-sub">枚 · 资金押在微信账上</span>
                    </div>
                    <div className="stat-item">
                        <span className="stat-label">本地灵石流通</span>
                        <span className="stat-value">{formatNumber(fundStats?.local_stones_total)}</span>
                        <span className="stat-sub">用户当前可用合计</span>
                    </div>
                    <div className="stat-item">
                        <details className="finance-help">
                            <summary className="stat-label">已本地消费（净）· 口径说明</summary>
                            <p>已本地消费 = 微信侧在账 − 本地可用，为净差额：含邀请/签到/手工调整等非虚拟支付来源灵石的净影响</p>
                        </details>
                        <span className="stat-value">{formatNumber((fundStats?.wechat_mirror_total || 0) - (fundStats?.local_stones_total || 0))}</span>
                        <span className="stat-sub">两账净差额 · 点击查看口径</span>
                    </div>
                    <button type="button" className="stat-item stat-item-action" onClick={() => { if (!onlyExceptions) handleToggleExceptions(); }} title="点击查看全部异常单">
                        <span className="stat-label">待处理异常</span>
                        <span className="stat-value is-danger">{fundStats?.exception_count ?? '-'}</span>
                        <span className="stat-sub">失败 + 待处理 + 卡发货/卡退款</span>
                    </button>
                </div>

                {/* 状态 Tab（G2）：计数为当前筛选（忽略状态维度）下的各状态数量 */}
                <div className="status-tabs" role="tablist">
                    {STATUS_TABS.map((tab) => {
                        const count = tab.value === 'all'
                            ? Object.values(statusCounts).reduce((sum, n) => sum + (n || 0), 0)
                            : (statusCounts[tab.value] || 0);
                        const active = !onlyExceptions && statusFilter === tab.value;
                        return (
                            <button
                                key={tab.value}
                                role="tab"
                                aria-selected={active}
                                className={`status-tab ${active ? 'on' : ''}`}
                                disabled={onlyExceptions && tab.value !== statusFilter}
                                title={onlyExceptions ? '“仅看异常单”已限定为失败、待处理与卡单，取消后可切换状态' : undefined}
                                onClick={() => handleStatusTabClick(tab.value)}
                            >
                                {tab.label}
                                <span className="status-tab-count">{formatNumber(count)}</span>
                            </button>
                        );
                    })}
                </div>

                {/* 日期快捷（G2）：与起止日期输入双向同步 */}
                <div className="date-chips">
                    <span className="date-chips-cap">日期</span>
                    {[{ key: 'today', label: '今天' }, { key: 'yesterday', label: '昨天' }, { key: '7d', label: '近 7 天' }, { key: '30d', label: '近 30 天' }].map((item) => (
                        <button
                            key={item.key}
                            className={`date-chip ${datePreset === item.key ? 'on' : ''}`}
                            onClick={() => handleDatePresetClick(item.key)}
                        >
                            {item.label}
                        </button>
                    ))}
                    {datePreset === '' && (dateRange.start || dateRange.end) ? (
                        <button className="date-chip on" title="自定义区间，清空输入框即恢复全部">自定义</button>
                    ) : null}
                </div>

                <div className="order-tip-card">
                    <strong>说明：</strong>
                    <span>订单数值按业务语义展示：支付金额列（元）为用户实付人民币；灵石列为到账/消耗数量。"微信侧在账"是该用户在微信虚拟支付账本中的代币余额（本地镜像），点击订单详情可看完整资金视图。</span>
                </div>

                <div className="order-center-table-container">
                    <table className="order-center-table">
                        <thead>
                            <tr>
                                <th>订单号</th>
                                <th>用户</th>
                                <th>类型</th>
                                <th>业务分类</th>
                                <th>支付金额 / 订单数值</th>
                                <th>
                                    <details className="finance-help">
                                        <summary>微信实付</summary>
                                        <p>微信侧实际扣款金额（query_order 回读取证），与「支付金额」逐单对平</p>
                                    </details>
                                </th>
                                <th>
                                    <details className="finance-help">
                                        <summary>微信侧在账</summary>
                                        <p>该用户当前在微信虚拟支付账本中的代币余额（本地镜像 = 累计成功充值 − 已退款）</p>
                                    </details>
                                </th>
                                <th>状态</th>
                                <th>标题 / 描述</th>
                                <th>创建时间</th>
                                <th>完成时间</th>
                                <th>操作</th>
                            </tr>
                        </thead>
                        <tbody>
                            {records.length === 0 ? (
                                <tr>
                                    <td colSpan={12} className="empty-state">暂无订单记录</td>
                                </tr>
                            ) : (
                                records.map((record) => {
                                    const statusMeta = formatStatus(record.status);
                                    const isStuckRefunding = record.status === 'refunding';
                                    const isAdvancable = record.pay_channel === 'virtualpay' && (record.status === 'pending' || record.status === 'delivering');
                                    return (
                                        <tr key={record.id} className={record.status === 'failed' ? 'row-alert-failed' : record.status === 'pending' || record.status === 'delivering' || record.status === 'refunding' ? 'row-alert-pending' : ''}>
                                            <td>
                                                {record.pay_channel === 'virtualpay' ? (
                                                    <button
                                                        className="virtualpay-order-link"
                                                        onClick={() => navigate(`/virtualpay?keyword=${encodeURIComponent(record.order_no)}`)}
                                                        title="查看虚拟支付详情"
                                                    >
                                                        {record.order_no}
                                                    </button>
                                                ) : (
                                                    <span className="transaction-id">{record.order_no}</span>
                                                )}
                                            </td>
                                            <td>
                                                <div className="order-user-cell">
                                                    <button
                                                        className="user-link-button"
                                                        onClick={() => navigate(`/user-workbench?userId=${record.user_id}`)}
                                                    >
                                                        {record.username || `用户${record.user_id}`}
                                                    </button>
                                                    <span className="user-meta">ID {record.user_id}</span>
                                                </div>
                                            </td>
                                            <td>{formatOrderType(record.type)}</td>
                                            <td>{formatOrderCategory(record.order_category)}</td>
                                            <td>
                                                <div className="order-amount-cell">
                                                    <strong>{formatAmountYuan(record.amount_fen)}</strong>
                                                    <span className={`order-value ${isNegativeOrderValue(record) ? 'negative' : 'positive'}`}>{formatOrderValue(record)}</span>
                                                </div>
                                            </td>
                                            <td>
                                                {record.wechat_paid_fen != null && record.wechat_paid_fen > 0 ? (
                                                    <div className="paid-evidence-cell">
                                                        <strong>{formatAmountYuan(record.wechat_paid_fen)}</strong>
                                                        {record.wechat_pay_match === 'mismatch'
                                                            ? <span className="match-badge is-bad">不一致</span>
                                                            : <span className="match-badge is-ok">一致</span>}
                                                    </div>
                                                ) : (
                                                    <details className="finance-help">
                                                        <summary className="coin-mirror-na" aria-label="微信实付未回读，查看说明">—</summary>
                                                        <p>尚未回读微信实付（旧通道订单或支付确认中）</p>
                                                    </details>
                                                )}
                                            </td>
                                            <td>
                                                {record.pay_channel === 'virtualpay' && record.pay_mode === 'short_series_coin' ? (
                                                    <details className="finance-help">
                                                        <summary className="coin-mirror">{formatNumber(record.wechat_coin_mirror)} 枚</summary>
                                                        <p>该用户微信侧代币余额（本地镜像）</p>
                                                    </details>
                                                ) : (
                                                    <span className="coin-mirror-na">—</span>
                                                )}
                                            </td>
                                            <td>
                                                <span className={`status-badge ${statusMeta.className}`}>{statusMeta.label}</span>
                                            </td>
                                            <td>
                                                <div className="order-title-cell">
                                                    <strong>{record.title || '-'}</strong>
                                                    <span>{record.description || '-'}</span>
                                                </div>
                                            </td>
                                            <td>{formatDateTime(record.created_at)}</td>
                                            <td>{formatDateTime(record.completed_at)}</td>
                                            <td>
                                                <div className="order-action-group">
                                                    <button className="btn-action btn-view" onClick={() => void handleViewDetail(record)} title="查看详情">
                                                        <FiEye size={14} />
                                                        <span>详情</span>
                                                    </button>
                                                    {isStuckRefunding && (
                                                        <button
                                                            className="btn-action btn-ticket"
                                                            onClick={() => void handleVerifyRefund(record)}
                                                            disabled={actingOrderId === record.id}
                                                            title="查询微信侧真实退款状态：已 REFUND 则自动落终态"
                                                        >
                                                            <FiRefreshCw size={14} />
                                                            <span>{actingOrderId === record.id ? '核对中...' : '核对微信侧'}</span>
                                                        </button>
                                                    )}
                                                    {isAdvancable && (
                                                        <button
                                                            className="btn-action btn-ticket"
                                                            onClick={() => void handleAdvanceOrder(record)}
                                                            disabled={actingOrderId === record.id}
                                                            title="手动触发一次发货推进（补偿任务每 60 秒也会自动推进）"
                                                        >
                                                            <FiRefreshCw size={14} />
                                                            <span>{actingOrderId === record.id ? '推进中...' : '立即推进'}</span>
                                                        </button>
                                                    )}
                                                    <button
                                                        className="btn-action btn-ticket"
                                                        onClick={() => void handleCreateSupportTicket(record)}
                                                        disabled={creatingTicketOrderId === record.id}
                                                        title="转异常工单"
                                                    >
                                                        <FiAlertCircle size={14} />
                                                        <span>{creatingTicketOrderId === record.id ? '转单中...' : '转工单'}</span>
                                                    </button>
                                                    {canRefundVirtualPayOrder(record) && (
                                                        <button
                                                            className="btn-action btn-ticket"
                                                            onClick={() => void openRefundPreview(record)}
                                                            disabled={refundingOrderId === record.id}
                                                            title="虚拟支付退款（先展示影响清单）"
                                                        >
                                                            <FiRotateCcw size={14} />
                                                            <span>{refundingOrderId === record.id ? '退款中...' : '退款'}</span>
                                                        </button>
                                                    )}
                                                </div>
                                            </td>
                                        </tr>
                                    );
                                })
                            )}
                        </tbody>
                    </table>
                </div>

                <div className="order-pagination">
                    <button className="btn-secondary" onClick={() => handlePageChange(currentPage - 1)} disabled={loading || currentPage <= 1}>上一页</button>
                    <div className="order-pagination-info">
                        <strong>{currentPage}</strong>
                        <span>/ {totalPages}</span>
                    </div>
                    <button className="btn-secondary" onClick={() => handlePageChange(currentPage + 1)} disabled={loading || currentPage >= totalPages}>下一页</button>
                </div>

                {showDetailModal && (
                    <>
                        <div className="drawer-overlay" onClick={handleCloseDetail}></div>
                        <aside className="order-drawer" role="dialog" aria-label="订单详情">
                            <div className="drawer-header">
                                <div>
                                    <h3>
                                        订单详情
                                        {detailOrder && <span className={`status-badge ${formatStatus(detailOrder.status).className}`}>{formatStatus(detailOrder.status).label}</span>}
                                    </h3>
                                    <div className="drawer-subtitle">{detailOrder ? `${detailOrder.order_no} · 创建于 ${formatDateTime(detailOrder.created_at)}` : `订单 ID ${detailTargetId.current || '-'}`}</div>
                                </div>
                                <button className="drawer-close" onClick={handleCloseDetail}>✕</button>
                            </div>
                            <div className="drawer-body">
                                {detailLoading ? (
                                    <div className="detail-empty" role="status">正在加载订单详情...</div>
                                ) : detailError ? (
                                    <div className="detail-empty" role="alert">
                                        <p>加载订单详情失败：{detailError}</p>
                                        <button className="btn-secondary" onClick={() => { if (detailTargetId.current) void loadOrderDetail(detailTargetId.current); }}>重新加载</button>
                                    </div>
                                ) : selectedDetail && detailOrder ? (
                                <>
                                {selectedDetail.fund_view && (
                                    <div className="fund-view-card">
                                        <div className="fund-view-title">
                                            资金视图 · 充值与微信虚拟支付账本
                                        </div>
                                        <details className="finance-help">
                                            <summary>口径说明</summary>
                                            <p>微信侧在账 = 本地镜像（累计成功充值 − 已退款）；本地可用 = 用户当前灵石余额；已消费 = 两账净差额，含邀请/签到/手工调整等非虚拟支付来源灵石的净影响</p>
                                        </details>
                                        <div className="fund-view-grid">
                                            <div className="fund-item">
                                                <span className="fund-k">微信侧在账</span>
                                                <strong className="fund-v">{formatNumber(selectedDetail.fund_view.wechat_mirror)}<small>枚代币</small></strong>
                                                <span className="fund-s">资金押在微信账上</span>
                                            </div>
                                            <div className="fund-item highlight">
                                                <span className="fund-k">本地可用</span>
                                                <strong className="fund-v">{formatNumber(selectedDetail.fund_view.local_stones)}<small>灵石</small></strong>
                                                <span className="fund-s">当前实际可用余额</span>
                                            </div>
                                            <div className="fund-item">
                                                <span className="fund-k">已本地消费</span>
                                                <strong className="fund-v">{formatNumber(selectedDetail.fund_view.consumed_net)}<small>灵石</small></strong>
                                                <span className="fund-s">两账净差额（见口径说明）</span>
                                            </div>
                                        </div>
                                        {selectedDetail.fund_view.calibrated_at && (
                                            <div className={`fund-view-cal ${((selectedDetail.fund_view.diff || 0) !== 0) ? 'is-diff' : ''}`}>
                                                {((selectedDetail.fund_view.diff || 0) !== 0)
                                                    ? `最近校准 ${formatDateTime(selectedDetail.fund_view.calibrated_at)}：微信实测 ${formatNumber(selectedDetail.fund_view.measured_balance)} 与镜像差 ${formatNumber(selectedDetail.fund_view.diff)}，建议核对`
                                                    : `最近校准 ${formatDateTime(selectedDetail.fund_view.calibrated_at)}：与微信实测一致（用户支付时顺路核对）`}
                                            </div>
                                        )}
                                    </div>
                                )}

                                {selectedDetail.wechat_evidence && (
                                    <div className="detail-section wechat-evidence-section">
                                        <h4>
                                            微信侧对账（实付证据）
                                            {selectedDetail.wechat_evidence.amount_match === 'mismatch'
                                                ? <span className="match-badge is-bad">实付不一致</span>
                                                : selectedDetail.wechat_evidence.amount_match === 'match'
                                                    ? <span className="match-badge is-ok">已对平</span>
                                                    : <span className="match-badge">未比对该通道</span>}
                                        </h4>
                                        <div className="detail-grid">
                                            <div className="info-row"><span className="info-label">我方应付</span><span className="info-value mono">{formatAmountYuan(detailOrder.amount_fen)}</span></div>
                                            <div className="info-row"><span className="info-label">微信实付</span><span className="info-value mono">{formatAmountYuan(selectedDetail.wechat_evidence.paid_fee_fen ?? undefined)}</span></div>
                                            <div className="info-row"><span className="info-label">微信支付单号</span><span className="info-value mono">{selectedDetail.wechat_evidence.wxpay_order_id || '-'}</span></div>
                                            <div className="info-row"><span className="info-label">微信侧状态 / 核对时间</span><span className="info-value">{selectedDetail.wechat_evidence.wechat_state || '-'} · {formatDateTime(selectedDetail.wechat_evidence.queried_at)}</span></div>
                                            {selectedDetail.wechat_evidence.refund_fee_fen != null && selectedDetail.wechat_evidence.refund_fee_fen > 0 && (
                                                <div className="info-row"><span className="info-label">微信侧退款 / 剩余</span><span className="info-value mono">{formatAmountYuan(selectedDetail.wechat_evidence.refund_fee_fen)} / {formatAmountYuan(selectedDetail.wechat_evidence.left_fee_fen ?? undefined)}</span></div>
                                            )}
                                            {selectedDetail.wechat_evidence.sett_state != null && (
                                                <div className="info-row"><span className="info-label">结算状态</span><span className="info-value">{['未开始', '结算中', '结算成功', '待结算'][selectedDetail.wechat_evidence.sett_state] ?? selectedDetail.wechat_evidence.sett_state}{selectedDetail.wechat_evidence.platform_fee_fen != null ? ` · 技术服务费 ${formatAmountYuan(selectedDetail.wechat_evidence.platform_fee_fen)}` : ''}</span></div>
                                            )}
                                        </div>
                                        <div className="evidence-note">实付金额来自微信 query_order 回读（支付确认/补偿任务顺路取证），与"支付金额"不一致时已自动生成核对工单。</div>
                                    </div>
                                )}

                                <div className="detail-section">
                                    <h4>客户与订单</h4>
                                    <div className="detail-grid">
                                        <div className="info-row"><span className="info-label">用户</span><span className="info-value">{selectedDetail.user?.username || `用户${detailOrder.user_id}`}（ID {detailOrder.user_id}）</span></div>
                                        <div className="info-row"><span className="info-label">订单类型</span><span className="info-value">{formatOrderType(detailOrder.type)}</span></div>
                                        <div className="info-row"><span className="info-label">业务分类</span><span className="info-value">{formatOrderCategory(detailOrder.order_category)}</span></div>
                                        <div className="info-row"><span className="info-label">支付金额</span><span className="info-value">{formatAmountYuan(detailOrder.amount_fen)}</span></div>
                                        <div className="info-row"><span className="info-label">订单数值</span><span className={`info-value ${isNegativeOrderValue(detailOrder) ? 'negative' : 'positive'}`}>{formatOrderValue(detailOrder)}</span></div>
                                        <div className="info-row"><span className="info-label">支付通道</span><span className="info-value">{formatPayChannel(detailOrder.pay_channel)}{detailOrder.pay_mode ? ` · ${formatPayMode(detailOrder.pay_mode)}` : ''}</span></div>
                                        {detailOrder.pay_channel === 'virtualpay' && (
                                            <>
                                                <div className="info-row"><span className="info-label">虚拟支付商品</span><span className="info-value">{detailOrder.product_id || '-'}</span></div>
                                                <div className="info-row"><span className="info-label">发货上报</span><span className="info-value">{detailOrder.deliver_reported ? '已上报' : `未上报 / ${detailOrder.deliver_report_attempts || 0} 次`}</span></div>
                                            </>
                                        )}
                                        <div className="info-row"><span className="info-label">标题</span><span className="info-value">{detailOrder.title || '-'}</span></div>
                                        <div className="info-row"><span className="info-label">完成时间</span><span className="info-value">{formatDateTime(detailOrder.completed_at)}</span></div>
                                    </div>
                                    {detailOrder.description && (
                                        <div className="info-row info-row-full"><span className="info-label">说明</span><span className="info-value long-text">{detailOrder.description}</span></div>
                                    )}
                                </div>

                                {selectedDetail.membership && (
                                    <div className="detail-section">
                                        <h4>关联会员</h4>
                                        <div className="detail-grid">
                                            <div className="info-row"><span className="info-label">会员计划</span><span className="info-value">{selectedDetail.membership.plan_title}</span></div>
                                            <div className="info-row"><span className="info-label">会员状态</span><span className="info-value">{selectedDetail.membership.status}</span></div>
                                            <div className="info-row"><span className="info-label">下载权限</span><span className="info-value">{selectedDetail.membership.template_download_enabled ? '已开启' : '未开启'}</span></div>
                                            <div className="info-row"><span className="info-label">会员到期</span><span className="info-value">{selectedDetail.membership.is_lifetime ? '长期有效' : formatDateTime(selectedDetail.membership.expired_at)}</span></div>
                                            <div className="info-row info-row-full"><span className="info-label">授予订单</span><span className="info-value">{selectedDetail.membership.source_order_no || '-'}</span></div>
                                        </div>
                                    </div>
                                )}

                                <div className="detail-section">
                                    <h4>关联推送（微信 → 我方）</h4>
                                    {detailLogs.length === 0 ? (
                                        <div className="detail-empty">该订单暂无推送记录（推送丢失时补偿任务会自动推进，无需担心）</div>
                                    ) : (
                                        <div className="related-logs">
                                            {detailLogs.map((logItem) => (
                                                <div key={logItem.id} className="related-log-item">
                                                    <span className="related-log-event">{formatXPayEvent(logItem.event)}</span>
                                                    <span className={`status-pill ${logItem.processed ? 'ok' : 'warning'}`}>{logItem.processed ? '已处理' : '未处理'}</span>
                                                    <span className="related-log-time">{formatDateTime(logItem.created_at)}</span>
                                                </div>
                                            ))}
                                        </div>
                                    )}
                                </div>
                                </>
                                ) : null}
                            </div>
                            <div className="drawer-footer">
                                {selectedDetail && detailOrder && (
                                <>
                                <button
                                    className="btn-secondary"
                                    onClick={() => void handleCreateSupportTicket(detailOrder)}
                                    disabled={creatingTicketOrderId === detailOrder.id}
                                >
                                    {creatingTicketOrderId === detailOrder.id ? '转单中...' : '转异常工单'}
                                </button>
                                {canRefundVirtualPayOrder(detailOrder) && (
                                    <button
                                        className="btn-secondary"
                                        onClick={() => void openRefundPreview(detailOrder)}
                                        disabled={refundingOrderId === detailOrder.id}
                                    >
                                        {refundingOrderId === detailOrder.id ? '退款中...' : '虚拟支付退款'}
                                    </button>
                                )}
                                <button className="btn-secondary" onClick={() => navigate(`/user-workbench?userId=${detailOrder.user_id}`)}>进入用户详情</button>
                                <button className="btn-secondary" onClick={handleExportStatement} title="导出该订单的对账单 CSV（订单信息 + 资金视图 + 关联推送）">对账单</button>
                                </>
                                )}
                                <span style={{ flex: 1 }}></span>
                                <button className="btn-primary" onClick={handleCloseDetail}>关闭</button>
                            </div>
                        </aside>
                    </>
                )}

                {/* 退款确认弹窗（T8）：展示全部影响后再执行 */}
                {refundPreviewOpen && (
                    <div className="modal-overlay" onClick={closeRefundPreview}>
                        <div className="modal-content refund-preview-modal" role="dialog" aria-modal="true" aria-labelledby="refund-preview-title" onClick={(event) => event.stopPropagation()}>
                            <div className="modal-header">
                                <h3 id="refund-preview-title">退款确认{refundPreviewOrder ? ` · ${refundPreviewOrder.order_no}` : ''}</h3>
                                <button className="modal-close" onClick={closeRefundPreview} disabled={!!refundingOrderId} aria-label="关闭退款预览">✕</button>
                            </div>
                            <div className="modal-body">
                                {refundPreviewLoading ? (
                                    <div className="detail-empty" role="status">正在获取退款影响清单...</div>
                                ) : refundPreviewError ? (
                                    <div className="detail-empty" role="alert">
                                        <p>获取退款影响预览失败：{refundPreviewError}</p>
                                        <button className="btn-secondary" onClick={() => { if (refundPreviewOrder) void openRefundPreview(refundPreviewOrder); }}>重新加载</button>
                                    </div>
                                ) : refundPreview ? (
                                    <>
                                        <ul className="refund-impact-list">
                                            <li>
                                                <strong>扣回灵石：</strong>{formatNumber(refundPreview.stones_to_deduct)} 枚（实付 {formatAmountYuan(refundPreview.amount_fen)}）
                                            </li>
                                            <li>
                                                <strong>用户当前余额：</strong>{formatNumber(refundPreview.current_stones)} 枚
                                                {refundPreview.sufficient
                                                    ? ' — 余额充足，可发起'
                                                    : <span className="refund-impact-danger"> — 余额不足，无法发起（微信代币可能未被消费，需先人工核对差额）</span>}
                                            </li>
                                            {refundPreview.membership && (
                                                <li>
                                                    <strong>会员处理：</strong>{refundPreview.membership.plan_title}
                                                    （{refundPreview.membership.recover_detail || '该笔订单未授予会员，无需处理'}）
                                                </li>
                                            )}
                                            <li>
                                                <strong>执行顺序：</strong>先扣回灵石 → 发起微信退款 → 微信确认后订单落"已退款"并自动回收会员（如上）。全程记审计日志。
                                            </li>
                                        </ul>
                                        <div className="refund-impact-note">微信退款"超时不代表失败"：提交后订单进入"退款中"，由补偿任务按微信侧真实状态自动收尾；卡超 10 分钟会出现"核对微信侧"按钮。</div>
                                    </>
                                ) : null}
                            </div>
                            <div className="modal-footer">
                                <button className="btn-secondary" onClick={closeRefundPreview} disabled={!!refundingOrderId}>取消</button>
                                <button
                                    className="btn-primary"
                                    onClick={() => void handleConfirmRefund()}
                                    disabled={refundPreviewLoading || !refundPreview || refundPreview.order_id !== refundPreviewOrder?.id || !refundPreview.sufficient || !!refundingOrderId}
                                >
                                    {refundingOrderId ? '退款提交中...' : '确认发起退款'}
                                </button>
                            </div>
                        </div>
                    </div>
                )}
            </div>
        </>
    );
};

export default OrderCenter;
