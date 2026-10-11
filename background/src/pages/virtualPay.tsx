import { reportPageLoadError } from '../utils/pageLoadError';
import React, { useEffect, useState } from 'react';
import { FiRefreshCw, FiSearch, FiZap } from 'react-icons/fi';
import { useNavigate, useSearchParams } from 'react-router-dom';
import {
    getCalibrationSummary,
    getVirtualPayHealth,
    getXPayLogs,
    replayXPayLog,
    runCalibration,
    type CalibrationSummary,
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
    const navigate = useNavigate();
    const [searchParams] = useSearchParams();
    const [health, setHealth] = useState<VirtualPayHealth | null>(null);
    const [calibration, setCalibration] = useState<CalibrationSummary | null>(null);
    const [calibrating, setCalibrating] = useState(false);
    const [replayingLogId, setReplayingLogId] = useState<number | null>(null);
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
            const [healthData, logData, calibData] = await Promise.all([
                getVirtualPayHealth(),
                getXPayLogs({
                    keyword: keyword.trim(),
                    event: eventFilter,
                    processed: processedFilter,
                    page: nextPage,
                    page_size: pageSize,
                }),
                getCalibrationSummary().catch((error) => {
                    console.error('加载校准汇总失败:', error);
                    return null;
                }),
            ]);
            setHealth(healthData);
            setLogs(logData.list || []);
            setSummary(logData.summary || emptySummary);
            setTotal(Number(logData.total || 0));
            setPage(Number(logData.page || nextPage));
            if (calibData) setCalibration(calibData);
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

    /** 立即校准（G3）：近期有过代币充值的用户批量实测，离线用户自动跳过 */
    const handleCalibrate = async () => {
        setCalibrating(true);
        try {
            const result = await runCalibration();
            alert(`校准完成：检查 ${result.checked} 人，实测 ${result.calibrated} 人（离线跳过 ${result.offline} 人），当前差异用户 ${result.diff_found} 人。`);
            await loadData(page);
        } catch (error: any) {
            console.error('批量校准失败:', error);
            alert(error?.message || '批量校准失败');
        } finally {
            setCalibrating(false);
        }
    };

    /** 重放推送（T6）：幂等，重放不会重复发货/重复退款 */
    const handleReplay = async (item: XPayMessageLogItem) => {
        if (!window.confirm(`确认重放这条推送？\n事件：${item.event}\n订单：${item.out_trade_no}\n\n处理函数均为幂等实现，重放不会重复发货或重复退款。`)) {
            return;
        }
        setReplayingLogId(item.id);
        try {
            await replayXPayLog(item.id);
            alert('重放成功，业务分发已重新执行。');
            await loadData(page);
        } catch (error: any) {
            console.error('重放推送失败:', error);
            alert(error?.message || '重放推送失败');
        } finally {
            setReplayingLogId(null);
        }
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
                    <div className="virtual-pay-stat-item"><span>发货中</span><strong>{health?.order_summary?.delivering_orders || 0}</strong></div>
                    <div className="virtual-pay-stat-item"><span>卡退款（&gt;10 分钟）</span><strong className={(health?.order_summary?.refunding_stuck_orders || 0) > 0 ? 'is-danger' : ''}>{health?.order_summary?.refunding_stuck_orders || 0}</strong></div>
                    <div className="virtual-pay-stat-item"><span>退款订单</span><strong>{health?.order_summary?.refunded_orders || 0}</strong></div>
                    <div className="virtual-pay-stat-item"><span>待上报道具发货</span><strong>{health?.order_summary?.unreported_goods_orders || 0}</strong></div>
                    <div className="virtual-pay-stat-item"><span>推送总数</span><strong>{summary.total_count}</strong></div>
                    <div className="virtual-pay-stat-item"><span>未处理推送</span><strong>{summary.unprocessed_count}</strong></div>
                </div>

                {/* 双账差额监控（G3 / D7）：微信侧实测 vs 本地镜像 */}
                <div className="diff-card">
                    <div className="diff-head">
                        <div>
                            <h3>双账差额监控</h3>
                            <small>用户支付时顺路实测微信侧代币余额，与本地镜像比对；差额用户自动生成核对工单</small>
                        </div>
                        <button className="btn-calibrate" onClick={() => void handleCalibrate()} disabled={calibrating} title="对近期有过代币充值的用户批量实测；离线用户（session_key 过期）无法实测，自动跳过">
                            <FiZap />
                            {calibrating ? '校准中...' : '立即校准'}
                        </button>
                    </div>
                    <div className="diff-body">
                        <div className="diff-stat">
                            <b>{calibration?.calibrated_users ?? '-'}</b>
                            <span>已校准用户</span>
                        </div>
                        <div className="diff-stat">
                            <b className={(calibration?.diff_users || 0) > 0 ? 'is-danger' : 'is-ok'}>{calibration?.diff_users ?? '-'}</b>
                            <span>存在差异</span>
                        </div>
                        {calibration && calibration.diffs.length > 0 ? (
                            <div className="diff-list">
                                {calibration.diffs.slice(0, 5).map((item) => (
                                    <button key={item.user_id} className="diff-row" onClick={() => navigate(`/user-workbench?userId=${item.user_id}`)} title="进入用户360">
                                        <b>{item.username || `用户${item.user_id}`}</b>
                                        <span>ID {item.user_id} · 微信实测 {item.measured_balance.toLocaleString('zh-CN')} / 镜像 {item.local_mirror.toLocaleString('zh-CN')}</span>
                                        <em className={item.diff > 0 ? 'plus' : 'minus'}>{item.diff > 0 ? `+${item.diff}` : item.diff}</em>
                                    </button>
                                ))}
                                {calibration.diffs.length > 5 && <div className="diff-more">仅显示前 5 条，共 {calibration.diffs.length > 20 ? 20 : calibration.diffs.length} 条差异记录，核对工单已生成</div>}
                            </div>
                        ) : (
                            <div className="diff-empty">{calibration ? '暂无差异：已校准用户两侧账本一致' : '校准数据加载中...'}</div>
                        )}
                    </div>
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
                            placeholder="搜索订单号或微信用户标识（openid）"
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
                                <th>客户</th>
                                <th>OpenID</th>
                                <th>处理状态</th>
                                <th>错误</th>
                                <th>时间</th>
                                <th>操作</th>
                            </tr>
                        </thead>
                        <tbody>
                            {logs.length === 0 ? (
                                <tr><td colSpan={8} className="empty-state">暂无推送日志</td></tr>
                            ) : logs.map((item) => (
                                <tr key={item.id}>
                                    <td>{item.event || '-'}</td>
                                    <td className="mono">{item.out_trade_no || '-'}</td>
                                    <td>
                                        {item.user_id > 0 ? (
                                            <button
                                                className="user-link-button"
                                                title={`${item.username || '用户'} · 进入用户360`}
                                                onClick={() => navigate(`/user-workbench?userId=${item.user_id}`)}
                                            >
                                                {item.username || `用户${item.user_id}`}
                                                <span className="user-id-meta"> ID {item.user_id}</span>
                                            </button>
                                        ) : (
                                            <span title="推送未关联到本地订单">-</span>
                                        )}
                                    </td>
                                    <td className="mono">{item.openid || '-'}</td>
                                    <td><span className={`status-pill ${item.processed ? 'ok' : 'warning'}`}>{item.processed ? '已处理' : '未处理'}</span></td>
                                    <td>{item.error_msg || '-'}</td>
                                    <td>{formatDateTime(item.created_at)}</td>
                                    <td>
                                        <div className="op-cell-inline">
                                            <button
                                                className="btn-mini"
                                                title="跳转订单中心按订单号检索"
                                                onClick={() => navigate(`/recharge?orderNo=${encodeURIComponent(item.out_trade_no)}`)}
                                            >
                                                订单
                                            </button>
                                            {!item.processed && (
                                                <button
                                                    className="btn-mini warn"
                                                    disabled={replayingLogId === item.id}
                                                    title="用推送原文重新执行业务分发（幂等，不会重复发货/退款）"
                                                    onClick={() => void handleReplay(item)}
                                                >
                                                    {replayingLogId === item.id ? '重放中...' : '重放处理'}
                                                </button>
                                            )}
                                        </div>
                                    </td>
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
