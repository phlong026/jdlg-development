import { reportPageLoadError } from '../utils/pageLoadError';
import React, { useEffect, useState } from 'react';
import { FiRefreshCw, FiSearch } from 'react-icons/fi';
import { useSearchParams } from 'react-router-dom';
import {
    getVirtualPayHealth,
    getXPayLogs,
    type VirtualPayHealth,
    type XPayMessageLogItem,
    type XPayLogSummary,
} from '../api/virtualPay';
import './virtualPay.scss';

const emptySummary: XPayLogSummary = {
    total_count: 0,
    processed_count: 0,
    unprocessed_count: 0,
};

const formatDateTime = (value?: string) => {
    if (!value) return '-';
    const date = new Date(value);
    if (Number.isNaN(date.getTime())) return value;
    return date.toLocaleString('zh-CN');
};

const VirtualPay: React.FC = () => {
    const [searchParams] = useSearchParams();
    const [health, setHealth] = useState<VirtualPayHealth | null>(null);
    const [logs, setLogs] = useState<XPayMessageLogItem[]>([]);
    const [summary, setSummary] = useState<XPayLogSummary>(emptySummary);
    const [loading, setLoading] = useState(false);
    const [keyword, setKeyword] = useState(() => searchParams.get('keyword') || '');
    const [eventFilter, setEventFilter] = useState('all');
    const [processedFilter, setProcessedFilter] = useState('all');
    const [total, setTotal] = useState(0);
    const [page, setPage] = useState(1);
    const pageSize = 20;

    const loadData = async (nextPage = page) => {
        setLoading(true);
        try {
            const [healthData, logData] = await Promise.all([
                getVirtualPayHealth(),
                getXPayLogs({
                    keyword: keyword.trim(),
                    event: eventFilter,
                    processed: processedFilter,
                    page: nextPage,
                    page_size: pageSize,
                }),
            ]);
            setHealth(healthData);
            setLogs(logData.list || []);
            setSummary(logData.summary || emptySummary);
            setTotal(Number(logData.total || 0));
            setPage(Number(logData.page || nextPage));
        } catch (error) {
            console.error('加载虚拟支付监控失败:', error);
            reportPageLoadError(error, '加载虚拟支付监控失败');
        } finally {
            setLoading(false);
        }
    };

    useEffect(() => {
        void loadData(1);
    }, []);

    const totalPages = Math.max(1, Math.ceil(total / pageSize));

    const handleSearch = () => {
        void loadData(1);
    };

    return (
        <>
            <div className="virtual-pay-container">
                <div className="virtual-pay-toolbar">
                    <div>
                        <h3>小程序虚拟支付链路</h3>
                        <p>检查配置完整性、虚拟支付订单状态、发货上报缺口与微信支付消息推送处理情况。</p>
                    </div>
                    <button className="btn-secondary" onClick={() => void loadData(page)} disabled={loading}>
                        <FiRefreshCw />
                        {loading ? '刷新中...' : '刷新'}
                    </button>
                </div>

                <div className="health-panel">
                    <div className={`health-overview ${health?.ok ? 'is-ok' : 'is-warning'}`}>
                        <span>配置状态</span>
                        <strong>{health?.ok ? '可用' : '需检查'}</strong>
                        <small>{health?.sandbox_mode ? '沙箱模式' : '生产模式'} / iOS {health?.ios_enabled ? '已开放' : '未开放'}</small>
                    </div>
                    <div className="health-grid">
                        {(health?.items || []).map((item) => (
                            <div key={item.key} className="health-item">
                                <span>{item.label}</span>
                                <strong className={item.ok ? 'ok' : 'warning'}>{item.message}</strong>
                            </div>
                        ))}
                    </div>
                </div>

                <div className="virtual-pay-stats">
                    <div className="virtual-pay-stat-item"><span>虚拟支付订单</span><strong>{health?.order_summary?.total_virtualpay_orders || 0}</strong></div>
                    <div className="virtual-pay-stat-item"><span>待处理订单</span><strong>{health?.order_summary?.pending_orders || 0}</strong></div>
                    <div className="virtual-pay-stat-item"><span>退款订单</span><strong>{health?.order_summary?.refunded_orders || 0}</strong></div>
                    <div className="virtual-pay-stat-item"><span>待上报道具发货</span><strong>{health?.order_summary?.unreported_goods_orders || 0}</strong></div>
                    <div className="virtual-pay-stat-item"><span>推送总数</span><strong>{summary.total_count}</strong></div>
                    <div className="virtual-pay-stat-item"><span>未处理推送</span><strong>{summary.unprocessed_count}</strong></div>
                </div>

                <div className="log-toolbar">
                    <div className="search-box">
                        <FiSearch className="search-icon" />
                        <input
                            value={keyword}
                            onChange={(event) => setKeyword(event.target.value)}
                            onKeyDown={(event) => {
                                if (event.key === 'Enter') handleSearch();
                            }}
                            placeholder="搜索订单号、openid 或消息 ID"
                        />
                    </div>
                    <select value={eventFilter} onChange={(event) => setEventFilter(event.target.value)}>
                        <option value="all">全部事件</option>
                        <option value="xpay_goods_deliver_notify">道具发货</option>
                        <option value="xpay_coin_pay_notify">代币支付</option>
                        <option value="xpay_refund_notify">退款通知</option>
                    </select>
                    <select value={processedFilter} onChange={(event) => setProcessedFilter(event.target.value)}>
                        <option value="all">全部处理状态</option>
                        <option value="true">已处理</option>
                        <option value="false">未处理</option>
                    </select>
                    <button className="btn-primary" onClick={handleSearch}><FiSearch />查询</button>
                </div>

                <div className="virtual-pay-table-wrap">
                    <table className="virtual-pay-table">
                        <thead>
                            <tr>
                                <th>事件</th>
                                <th>订单号</th>
                                <th>OpenID</th>
                                <th>处理状态</th>
                                <th>错误</th>
                                <th>时间</th>
                            </tr>
                        </thead>
                        <tbody>
                            {logs.length === 0 ? (
                                <tr><td colSpan={6} className="empty-state">暂无推送日志</td></tr>
                            ) : logs.map((item) => (
                                <tr key={item.id}>
                                    <td>{item.event || '-'}</td>
                                    <td className="mono">{item.out_trade_no || '-'}</td>
                                    <td className="mono">{item.openid || '-'}</td>
                                    <td><span className={`status-pill ${item.processed ? 'ok' : 'warning'}`}>{item.processed ? '已处理' : '未处理'}</span></td>
                                    <td>{item.error_msg || '-'}</td>
                                    <td>{formatDateTime(item.created_at)}</td>
                                </tr>
                            ))}
                        </tbody>
                    </table>
                </div>

                <div className="virtual-pay-pagination">
                    <button className="btn-secondary" disabled={loading || page <= 1} onClick={() => void loadData(page - 1)}>上一页</button>
                    <span>{page} / {totalPages}</span>
                    <button className="btn-secondary" disabled={loading || page >= totalPages} onClick={() => void loadData(page + 1)}>下一页</button>
                </div>
            </div>
        </>
    );
};

export default VirtualPay;
