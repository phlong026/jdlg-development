import React, { useState, useEffect } from 'react';
import { FiSearch, FiDownload, FiRefreshCw, FiEye } from 'react-icons/fi';
import Layout from '../component/layout';
import './recharge.scss';

interface RechargeRecord {
    id: string;
    userId: string;
    username: string;
    amount: number;
    stones: number;
    paymentMethod: string;
    status: 'pending' | 'success' | 'failed' | 'refunded';
    transactionId?: string;
    createdAt: string;
    completedAt?: string;
}

const Recharge: React.FC = () => {
    const [records, setRecords] = useState<RechargeRecord[]>([]);
    const [loading, setLoading] = useState(false);
    const [searchKeyword, setSearchKeyword] = useState('');
    const [statusFilter, setStatusFilter] = useState<string>('all');
    const [paymentMethodFilter, setPaymentMethodFilter] = useState<string>('all');
    const [dateRange, setDateRange] = useState<{ start: string; end: string }>({
        start: '',
        end: '',
    });
    const [showDetailModal, setShowDetailModal] = useState(false);
    const [selectedRecord, setSelectedRecord] = useState<RechargeRecord | null>(null);

    useEffect(() => {
        // TODO: 连接后端充值流水接口
        // 暂时使用模拟数据
        setRecords([
            {
                id: '1',
                userId: '1001',
                username: 'user001',
                amount: 100.00,
                stones: 1000,
                paymentMethod: 'wechat',
                status: 'success',
                transactionId: 'WX20240114001',
                createdAt: new Date().toISOString(),
                completedAt: new Date().toISOString(),
            },
            {
                id: '2',
                userId: '1002',
                username: 'user002',
                amount: 50.00,
                stones: 500,
                paymentMethod: 'alipay',
                status: 'pending',
                transactionId: 'AL20240114002',
                createdAt: new Date().toISOString(),
            },
            {
                id: '3',
                userId: '1003',
                username: 'user003',
                amount: 200.00,
                stones: 2000,
                paymentMethod: 'wechat',
                status: 'failed',
                transactionId: 'WX20240114003',
                createdAt: new Date().toISOString(),
            },
        ]);
    }, []);

    const filteredRecords = records.filter(record => {
        const matchKeyword = !searchKeyword || 
            record.username.toLowerCase().includes(searchKeyword.toLowerCase()) ||
            record.userId.toLowerCase().includes(searchKeyword.toLowerCase()) ||
            record.transactionId?.toLowerCase().includes(searchKeyword.toLowerCase());
        const matchStatus = statusFilter === 'all' || record.status === statusFilter;
        const matchPaymentMethod = paymentMethodFilter === 'all' || record.paymentMethod === paymentMethodFilter;
        
        let matchDate = true;
        if (dateRange.start) {
            matchDate = matchDate && new Date(record.createdAt) >= new Date(dateRange.start);
        }
        if (dateRange.end) {
            matchDate = matchDate && new Date(record.createdAt) <= new Date(dateRange.end + 'T23:59:59');
        }
        
        return matchKeyword && matchStatus && matchPaymentMethod && matchDate;
    });

    const getStatusLabel = (status: string) => {
        const statusMap: Record<string, { label: string; className: string }> = {
            pending: { label: '待处理', className: 'status-pending' },
            success: { label: '成功', className: 'status-success' },
            failed: { label: '失败', className: 'status-failed' },
            refunded: { label: '已退款', className: 'status-refunded' },
        };
        return statusMap[status] || { label: status, className: '' };
    };

    const getPaymentMethodLabel = (method: string) => {
        const methodMap: Record<string, string> = {
            wechat: '微信支付',
            alipay: '支付宝',
            bank: '银行卡',
            other: '其他',
        };
        return methodMap[method] || method;
    };

    const handleViewDetail = (record: RechargeRecord) => {
        setSelectedRecord(record);
        setShowDetailModal(true);
    };

    const stats = {
        total: records.length,
        totalAmount: records.filter(r => r.status === 'success').reduce((sum, r) => sum + r.amount, 0),
        totalStones: records.filter(r => r.status === 'success').reduce((sum, r) => sum + r.stones, 0),
        successCount: records.filter(r => r.status === 'success').length,
        pendingCount: records.filter(r => r.status === 'pending').length,
    };

    return (
        <Layout title="充值流水">
            <div className="recharge-container">
                <div className="recharge-toolbar">
                    <div className="toolbar-left">
                        <div className="search-box">
                            <FiSearch className="search-icon" />
                            <input
                                type="text"
                                placeholder="搜索用户ID、用户名或交易号..."
                                value={searchKeyword}
                                onChange={(e) => setSearchKeyword(e.target.value)}
                                className="search-input"
                            />
                        </div>
                        <div className="filters">
                            <select
                                className="filter-select"
                                value={statusFilter}
                                onChange={(e) => setStatusFilter(e.target.value)}
                            >
                                <option value="all">全部状态</option>
                                <option value="pending">待处理</option>
                                <option value="success">成功</option>
                                <option value="failed">失败</option>
                                <option value="refunded">已退款</option>
                            </select>
                            <select
                                className="filter-select"
                                value={paymentMethodFilter}
                                onChange={(e) => setPaymentMethodFilter(e.target.value)}
                            >
                                <option value="all">全部支付方式</option>
                                <option value="wechat">微信支付</option>
                                <option value="alipay">支付宝</option>
                                <option value="bank">银行卡</option>
                                <option value="other">其他</option>
                            </select>
                            <input
                                type="date"
                                className="filter-date"
                                value={dateRange.start}
                                onChange={(e) => setDateRange({ ...dateRange, start: e.target.value })}
                                placeholder="开始日期"
                            />
                            <input
                                type="date"
                                className="filter-date"
                                value={dateRange.end}
                                onChange={(e) => setDateRange({ ...dateRange, end: e.target.value })}
                                placeholder="结束日期"
                            />
                        </div>
                    </div>
                    <div className="toolbar-right">
                        <button className="btn-secondary" onClick={() => window.location.reload()}>
                            <FiRefreshCw />
                            刷新
                        </button>
                        <button className="btn-primary">
                            <FiDownload />
                            导出数据
                        </button>
                    </div>
                </div>

                <div className="recharge-stats">
                    <div className="stat-item">
                        <span className="stat-label">总记录数</span>
                        <span className="stat-value">{stats.total}</span>
                    </div>
                    <div className="stat-item">
                        <span className="stat-label">总充值金额</span>
                        <span className="stat-value">¥{stats.totalAmount.toFixed(2)}</span>
                    </div>
                    <div className="stat-item">
                        <span className="stat-label">总充值灵石</span>
                        <span className="stat-value">{stats.totalStones.toLocaleString()}</span>
                    </div>
                    <div className="stat-item">
                        <span className="stat-label">成功订单</span>
                        <span className="stat-value">{stats.successCount}</span>
                    </div>
                    <div className="stat-item">
                        <span className="stat-label">待处理订单</span>
                        <span className="stat-value">{stats.pendingCount}</span>
                    </div>
                </div>

                <div className="recharge-table-container">
                    <table className="recharge-table">
                        <thead>
                            <tr>
                                <th>交易号</th>
                                <th>用户ID</th>
                                <th>用户名</th>
                                <th>充值金额</th>
                                <th>充值灵石</th>
                                <th>支付方式</th>
                                <th>状态</th>
                                <th>创建时间</th>
                                <th>完成时间</th>
                                <th>操作</th>
                            </tr>
                        </thead>
                        <tbody>
                            {filteredRecords.length === 0 ? (
                                <tr>
                                    <td colSpan={10} className="empty-state">
                                        暂无充值记录
                                    </td>
                                </tr>
                            ) : (
                                filteredRecords.map((record) => (
                                    <tr key={record.id}>
                                        <td>
                                            <span className="transaction-id">{record.transactionId || '-'}</span>
                                        </td>
                                        <td>{record.userId}</td>
                                        <td>{record.username}</td>
                                        <td>
                                            <span className="amount">¥{record.amount.toFixed(2)}</span>
                                        </td>
                                        <td>
                                            <span className="stones">{record.stones.toLocaleString()}</span>
                                        </td>
                                        <td>{getPaymentMethodLabel(record.paymentMethod)}</td>
                                        <td>
                                            <span className={`status-badge ${getStatusLabel(record.status).className}`}>
                                                {getStatusLabel(record.status).label}
                                            </span>
                                        </td>
                                        <td>{new Date(record.createdAt).toLocaleString('zh-CN')}</td>
                                        <td>
                                            {record.completedAt 
                                                ? new Date(record.completedAt).toLocaleString('zh-CN')
                                                : '-'
                                            }
                                        </td>
                                        <td>
                                            <button
                                                className="btn-action btn-view"
                                                onClick={() => handleViewDetail(record)}
                                                title="查看详情"
                                            >
                                                <FiEye size={14} />
                                            </button>
                                        </td>
                                    </tr>
                                ))
                            )}
                        </tbody>
                    </table>
                </div>

                {/* 详情弹窗 */}
                {showDetailModal && selectedRecord && (
                    <div className="modal-overlay" onClick={() => setShowDetailModal(false)}>
                        <div className="modal-content" onClick={(e) => e.stopPropagation()}>
                            <div className="modal-header">
                                <h3>充值详情</h3>
                                <button className="modal-close" onClick={() => setShowDetailModal(false)}>
                                    ✕
                                </button>
                            </div>
                            <div className="modal-body">
                                <div className="detail-section">
                                    <div className="info-row">
                                        <span className="info-label">交易号:</span>
                                        <span className="info-value">{selectedRecord.transactionId || '-'}</span>
                                    </div>
                                    <div className="info-row">
                                        <span className="info-label">用户ID:</span>
                                        <span className="info-value">{selectedRecord.userId}</span>
                                    </div>
                                    <div className="info-row">
                                        <span className="info-label">用户名:</span>
                                        <span className="info-value">{selectedRecord.username}</span>
                                    </div>
                                    <div className="info-row">
                                        <span className="info-label">充值金额:</span>
                                        <span className="info-value amount">¥{selectedRecord.amount.toFixed(2)}</span>
                                    </div>
                                    <div className="info-row">
                                        <span className="info-label">充值灵石:</span>
                                        <span className="info-value stones">{selectedRecord.stones.toLocaleString()}</span>
                                    </div>
                                    <div className="info-row">
                                        <span className="info-label">支付方式:</span>
                                        <span className="info-value">{getPaymentMethodLabel(selectedRecord.paymentMethod)}</span>
                                    </div>
                                    <div className="info-row">
                                        <span className="info-label">状态:</span>
                                        <span className={`info-value ${getStatusLabel(selectedRecord.status).className}`}>
                                            {getStatusLabel(selectedRecord.status).label}
                                        </span>
                                    </div>
                                    <div className="info-row">
                                        <span className="info-label">创建时间:</span>
                                        <span className="info-value">{new Date(selectedRecord.createdAt).toLocaleString('zh-CN')}</span>
                                    </div>
                                    {selectedRecord.completedAt && (
                                        <div className="info-row">
                                            <span className="info-label">完成时间:</span>
                                            <span className="info-value">{new Date(selectedRecord.completedAt).toLocaleString('zh-CN')}</span>
                                        </div>
                                    )}
                                </div>
                            </div>
                            <div className="modal-footer">
                                <button className="btn-secondary" onClick={() => setShowDetailModal(false)}>
                                    关闭
                                </button>
                            </div>
                        </div>
                    </div>
                )}
            </div>
        </Layout>
    );
};

export default Recharge;
