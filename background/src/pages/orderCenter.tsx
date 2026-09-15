import { reportPageLoadError } from '../utils/pageLoadError';
import React, { useEffect, useState } from 'react';
import { FiAlertCircle, FiEye, FiRefreshCw, FiRotateCcw, FiSearch } from 'react-icons/fi';
import { useNavigate, useSearchParams } from 'react-router-dom';
import Layout from '../component/layout';
import { createOrderSupportTicket, getOrderDetail, getOrderList, refundVirtualPayOrder, type OrderDetail, type OrderListItem, type OrderListParams, type OrderSummary } from '../api/orders';
import './orderCenter.scss';

const formatOrderType = (value: string) => {
    const map: Record<string, string> = {
        recharge: '充值',
        consume: 'AI消费',
        culture: '内容消费',
        withdraw: '提现',
        certification: '认证费',
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
    };
    return map[value] || value || '-';
};

const formatStatus = (value: string) => {
    if (value === 'refunding') return { label: '退款中', className: 'status-pending' };
    if (value === 'refunded') return { label: '已退款', className: 'status-cancelled' };
    const map: Record<string, { label: string; className: string }> = {
        pending: { label: '待处理', className: 'status-pending' },
        success: { label: '成功', className: 'status-success' },
        failed: { label: '失败', className: 'status-failed' },
        cancelled: { label: '已取消', className: 'status-cancelled' },
    };
    return map[value] || { label: value || '-', className: '' };
};

const formatPayChannel = (value?: string) => {
    const map: Record<string, string> = {
        wechatpay: '微信支付',
        virtualpay: '虚拟支付',
    };
    return map[value || ''] || value || '-';
};

const formatPayMode = (value?: string) => {
    const map: Record<string, string> = {
        short_series_coin: '代币',
        short_series_goods: '道具',
    };
    return map[value || ''] || value || '-';
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
    if (params.onlyExceptions) parts.push('仅看异常单');
    return parts.length > 0 ? parts.join(' ｜ ') : '当前正在查看全部订单数据。';
};

const OrderCenter: React.FC = () => {
    const navigate = useNavigate();
    const [searchParams, setSearchParams] = useSearchParams();
    const [records, setRecords] = useState<OrderListItem[]>([]);
    const [summary, setSummary] = useState<OrderSummary>(emptySummary);
    const [loading, setLoading] = useState(false);
    const [searchKeyword, setSearchKeyword] = useState('');
    const [typeFilter, setTypeFilter] = useState('all');
    const [categoryFilter, setCategoryFilter] = useState('all');
    const [statusFilter, setStatusFilter] = useState('all');
    const [payChannelFilter, setPayChannelFilter] = useState('all');
    const [onlyExceptions, setOnlyExceptions] = useState(false);
    const [dateRange, setDateRange] = useState({ start: '', end: '' });
    const [showDetailModal, setShowDetailModal] = useState(false);
    const [selectedDetail, setSelectedDetail] = useState<OrderDetail | null>(null);
    const [creatingTicketOrderId, setCreatingTicketOrderId] = useState<number | null>(null);
    const [refundingOrderId, setRefundingOrderId] = useState<number | null>(null);
    const [currentPage, setCurrentPage] = useState(1);
    const [pageSize] = useState(DEFAULT_PAGE_SIZE);
    const [total, setTotal] = useState(0);
    const displayedRecords = onlyExceptions ? records.filter((record) => record.status === 'failed' || record.status === 'pending') : records;
    const totalPages = Math.max(1, Math.ceil(total / pageSize));

    const buildOrderQueryParams = (overrides?: Partial<OrderListParams>): OrderListParams => ({
        keyword: searchKeyword.trim(),
        type: typeFilter,
        order_category: categoryFilter,
        status: statusFilter,
        pay_channel: payChannelFilter,
        start_date: dateRange.start,
        end_date: dateRange.end,
        page: currentPage,
        page_size: pageSize,
        ...overrides,
    });

    const loadOrders = async (params?: OrderListParams) => {
        setLoading(true);
        try {
            const response = await getOrderList({
                page: currentPage,
                page_size: pageSize,
                ...params,
            });
            setRecords(response.list || []);
            setSummary(response.summary || emptySummary);
            setTotal(Number(response.total || 0));
            setCurrentPage(Number(response.page || params?.page || 1));
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
            return;
        }
        if (selectedDetail?.order?.id && String(selectedDetail.order.id) === orderId) {
            return;
        }
        void (async () => {
            try {
                const detail = await getOrderDetail(orderId);
                setSelectedDetail(detail);
                setShowDetailModal(true);
            } catch (error) {
                console.error('按来源打开订单详情失败:', error);
            }
        })();
    }, [searchParams, selectedDetail?.order?.id]);

    const handleSearch = () => {
        setCurrentPage(1);
        void loadOrders(buildOrderQueryParams({ page: 1 }));
    };

    const handleReset = () => {
        setSearchKeyword('');
        setTypeFilter('all');
        setCategoryFilter('all');
        setStatusFilter('all');
        setPayChannelFilter('all');
        setOnlyExceptions(false);
        setDateRange({ start: '', end: '' });
        setCurrentPage(1);
        void loadOrders({ page: 1, page_size: pageSize, keyword: '', type: 'all', order_category: 'all', status: 'all', pay_channel: 'all', start_date: '', end_date: '' });
    };

    const handleViewDetail = async (record: OrderListItem) => {
        try {
            const detail = await getOrderDetail(String(record.id));
            setSelectedDetail(detail);
            setShowDetailModal(true);
            const nextParams = new URLSearchParams(searchParams);
            nextParams.set('orderId', String(record.id));
            setSearchParams(nextParams);
        } catch (error) {
            console.error('加载订单详情失败:', error);
            reportPageLoadError(error, '加载订单详情失败');
        }
    };

    const handleCloseDetail = () => {
        setShowDetailModal(false);
        setSelectedDetail(null);
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

    const handleRefundVirtualPayOrder = async (order: OrderListItem | OrderDetail['order']) => {
        if (!canRefundVirtualPayOrder(order)) {
            alert('仅支持已成功到账的虚拟支付充值订单退款');
            return;
        }
        const reason = window.prompt(`确认退款订单 ${order.order_no}？\n将先扣回 ${order.amount} 灵石，余额不足时不会发起微信退款。`, '运营后台退款');
        if (reason === null) {
            return;
        }
        setRefundingOrderId(order.id);
        try {
            const result = await refundVirtualPayOrder(String(order.id), reason);
            alert(`退款已提交：${result.order_no}\n扣回灵石：${result.refund_amount}\n余额：${result.old_stones} → ${result.new_stones}`);
            await loadOrders(buildOrderQueryParams());
            if (selectedDetail?.order?.id === order.id) {
                const detail = await getOrderDetail(String(order.id));
                setSelectedDetail(detail);
            }
        } catch (error: any) {
            console.error('虚拟支付退款失败:', error);
            alert(error?.message || '虚拟支付退款失败');
        } finally {
            setRefundingOrderId(null);
        }
    };

    const handlePageChange = (page: number) => {
        if (page < 1 || page > totalPages || page === currentPage || loading) {
            return;
        }
        setCurrentPage(page);
        void loadOrders(buildOrderQueryParams({ page }));
    };

    return (
        <Layout title="订单中心">
            <div className="order-center-container">
                <div className="order-center-toolbar">
                    <div className="toolbar-left">
                        <div className="search-box">
                            <FiSearch className="search-icon" />
                            <input
                                type="text"
                                placeholder="搜索订单号、用户ID、用户名或标题..."
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
                                <option value="withdraw">提现</option>
                            </select>
                            <select className="filter-select" value={categoryFilter} onChange={(event) => setCategoryFilter(event.target.value)}>
                                <option value="all">全部分类</option>
                                <option value="recharge">充值</option>
                                <option value="ai">AI生成</option>
                                <option value="template">模板</option>
                                <option value="certification">认证</option>
                                <option value="service">设计服务</option>
                                <option value="withdraw">提现</option>
                            </select>
                            <select className="filter-select" value={statusFilter} onChange={(event) => setStatusFilter(event.target.value)}>
                                <option value="all">全部状态</option>
                                <option value="pending">待处理</option>
                                <option value="success">成功</option>
                                <option value="refunding">退款中</option>
                                <option value="refunded">已退款</option>
                                <option value="failed">失败</option>
                                <option value="cancelled">已取消</option>
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
                                onChange={(event) => setDateRange({ ...dateRange, start: event.target.value })}
                            />
                            <input
                                type="date"
                                className="filter-date"
                                value={dateRange.end}
                                onChange={(event) => setDateRange({ ...dateRange, end: event.target.value })}
                            />
                        </div>
                    </div>
                    <div className="toolbar-right">
                        <button className={`btn-secondary ${onlyExceptions ? 'is-active' : ''}`} onClick={() => setOnlyExceptions((prev) => !prev)}>
                            <FiAlertCircle />
                            {onlyExceptions ? '查看全部' : '仅看异常单'}
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
                        <strong>{onlyExceptions ? `${displayedRecords.length} 条异常` : `${summary.total_count} 条`}</strong>
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

                <div className="order-center-stats">
                    <div className="stat-item">
                        <span className="stat-label">订单总数</span>
                        <span className="stat-value">{summary.total_count}</span>
                    </div>
                    <div className="stat-item">
                        <span className="stat-label">成功订单数</span>
                        <span className="stat-value">{summary.success_count}</span>
                    </div>
                    <div className="stat-item">
                        <span className="stat-label">待处理订单</span>
                        <span className="stat-value">{summary.pending_count}</span>
                    </div>
                    <div className="stat-item">
                        <span className="stat-label">成功正向订单值</span>
                        <span className="stat-value">{summary.success_amount}</span>
                    </div>
                </div>

                <div className="order-tip-card">
                    <strong>说明：</strong>
                    <span>订单数值会按业务语义展示。充值和 AI 消费通常对应灵石，认证费对应人民币金额，具体以订单标题与描述为准。</span>
                </div>

                <div className="order-center-table-container">
                    <table className="order-center-table">
                        <thead>
                            <tr>
                                <th>订单号</th>
                                <th>用户</th>
                                <th>类型</th>
                                <th>业务分类</th>
                                <th>订单数值</th>
                                <th>支付通道</th>
                                <th>状态</th>
                                <th>标题 / 描述</th>
                                <th>创建时间</th>
                                <th>完成时间</th>
                                <th>操作</th>
                            </tr>
                        </thead>
                        <tbody>
                            {displayedRecords.length === 0 ? (
                                <tr>
                                    <td colSpan={11} className="empty-state">暂无订单记录</td>
                                </tr>
                            ) : (
                                displayedRecords.map((record) => {
                                    const statusMeta = formatStatus(record.status);
                                    return (
                                        <tr key={record.id} className={record.status === 'failed' ? 'row-alert-failed' : record.status === 'pending' ? 'row-alert-pending' : ''}>
                                            <td>
                                                {record.pay_channel === 'virtualpay' ? (
                                                    <button
                                                        className="virtualpay-order-link"
                                                        onClick={() => navigate(`/virtual-pay?keyword=${encodeURIComponent(record.order_no)}`)}
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
                                            <td><span className={`order-value ${isNegativeOrderValue(record) ? 'negative' : 'positive'}`}>{formatOrderValue(record)}</span></td>
                                            <td>
                                                <div className="pay-channel-cell">
                                                    <span className={`pay-channel-badge ${record.pay_channel === 'virtualpay' ? 'is-virtualpay' : ''}`}>
                                                        {formatPayChannel(record.pay_channel)}
                                                    </span>
                                                    {record.pay_mode && <span>{formatPayMode(record.pay_mode)}</span>}
                                                </div>
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
                                                            onClick={() => void handleRefundVirtualPayOrder(record)}
                                                            disabled={refundingOrderId === record.id}
                                                            title="虚拟支付退款"
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

                {showDetailModal && selectedDetail && (
                    <div className="modal-overlay" onClick={handleCloseDetail}>
                        <div className="modal-content" onClick={(event) => event.stopPropagation()}>
                            <div className="modal-header">
                                <h3>订单详情</h3>
                                <button className="modal-close" onClick={handleCloseDetail}>✕</button>
                            </div>
                            <div className="modal-body">
                                <div className="detail-section">
                                    <div className="info-row"><span className="info-label">订单号</span><span className="info-value">{selectedDetail.order.order_no}</span></div>
                                    <div className="info-row"><span className="info-label">用户</span><span className="info-value">{selectedDetail.user?.username || `用户${selectedDetail.order.user_id}`}</span></div>
                                    <div className="info-row"><span className="info-label">用户ID</span><span className="info-value">{selectedDetail.order.user_id}</span></div>
                                    <div className="info-row"><span className="info-label">订单类型</span><span className="info-value">{formatOrderType(selectedDetail.order.type)}</span></div>
                                    <div className="info-row"><span className="info-label">业务分类</span><span className="info-value">{formatOrderCategory(selectedDetail.order.order_category)}</span></div>
                                    <div className="info-row"><span className="info-label">订单数值</span><span className={`info-value ${isNegativeOrderValue(selectedDetail.order) ? 'negative' : 'positive'}`}>{formatOrderValue(selectedDetail.order)}</span></div>
                                    <div className="info-row"><span className="info-label">支付通道</span><span className="info-value">{formatPayChannel(selectedDetail.order.pay_channel)}</span></div>
                                    <div className="info-row"><span className="info-label">支付模式</span><span className="info-value">{formatPayMode(selectedDetail.order.pay_mode)}</span></div>
                                    {selectedDetail.order.pay_channel === 'virtualpay' && (
                                        <>
                                            <div className="info-row"><span className="info-label">虚拟支付商品</span><span className="info-value">{selectedDetail.order.product_id || '-'}</span></div>
                                            <div className="info-row"><span className="info-label">虚拟支付金额</span><span className="info-value">{selectedDetail.order.amount_fen ? `${selectedDetail.order.amount_fen} 分` : '-'}</span></div>
                                            <div className="info-row"><span className="info-label">发货上报</span><span className="info-value">{selectedDetail.order.deliver_reported ? '已上报' : `未上报 / ${selectedDetail.order.deliver_report_attempts || 0} 次`}</span></div>
                                        </>
                                    )}
                                    <div className="info-row"><span className="info-label">状态</span><span className={`info-value ${formatStatus(selectedDetail.order.status).className}`}>{formatStatus(selectedDetail.order.status).label}</span></div>
                                    <div className="info-row"><span className="info-label">标题</span><span className="info-value">{selectedDetail.order.title || '-'}</span></div>
                                    <div className="info-row"><span className="info-label">说明</span><span className="info-value long-text">{selectedDetail.order.description || '-'}</span></div>
                                    <div className="info-row"><span className="info-label">创建时间</span><span className="info-value">{formatDateTime(selectedDetail.order.created_at)}</span></div>
                                    <div className="info-row"><span className="info-label">完成时间</span><span className="info-value">{formatDateTime(selectedDetail.order.completed_at)}</span></div>
                                    {selectedDetail.membership && (
                                        <>
                                            <div className="info-row"><span className="info-label">会员计划</span><span className="info-value">{selectedDetail.membership.plan_title}</span></div>
                                            <div className="info-row"><span className="info-label">会员状态</span><span className="info-value">{selectedDetail.membership.status}</span></div>
                                            <div className="info-row"><span className="info-label">下载权限</span><span className="info-value">{selectedDetail.membership.template_download_enabled ? '已开启' : '未开启'}</span></div>
                                            <div className="info-row"><span className="info-label">会员到期</span><span className="info-value">{selectedDetail.membership.is_lifetime ? '长期有效' : formatDateTime(selectedDetail.membership.expired_at)}</span></div>
                                        </>
                                    )}
                                </div>
                            </div>
                            <div className="modal-footer">
                                <button
                                    className="btn-secondary"
                                    onClick={() => void handleCreateSupportTicket(selectedDetail.order)}
                                    disabled={creatingTicketOrderId === selectedDetail.order.id}
                                >
                                    {creatingTicketOrderId === selectedDetail.order.id ? '转单中...' : '转异常工单'}
                                </button>
                                {canRefundVirtualPayOrder(selectedDetail.order) && (
                                    <button
                                        className="btn-secondary"
                                        onClick={() => void handleRefundVirtualPayOrder(selectedDetail.order)}
                                        disabled={refundingOrderId === selectedDetail.order.id}
                                    >
                                        {refundingOrderId === selectedDetail.order.id ? '退款中...' : '虚拟支付退款'}
                                    </button>
                                )}
                                <button className="btn-secondary" onClick={() => navigate(`/user-workbench?userId=${selectedDetail.order.user_id}`)}>进入用户360</button>
                                <button className="btn-primary" onClick={handleCloseDetail}>关闭</button>
                            </div>
                        </div>
                    </div>
                )}
            </div>
        </Layout>
    );
};

export default OrderCenter;
