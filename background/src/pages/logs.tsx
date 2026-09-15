import React, { useState, useEffect } from 'react';
import { FiSearch, FiSettings, FiX, FiRefreshCw } from 'react-icons/fi';
import Layout from '../component/layout';
import ManagementSearchPanel from '../component/managementSearchPanel';
import { API_BASE_URL, API_ENDPOINTS } from '../config/api';
import './logs.scss';

interface Log {
    id: number;
    type: string;
    level: 'info' | 'warning' | 'error';
    message: string;
    user?: string;
    timestamp: string;
    details?: string;
}

interface LogConfig {
    rotate_interval: number; // 日志分割间隔（小时）
    retention_days: number;  // 日志保存天数
}

const Logs: React.FC = () => {
    const [logs, setLogs] = useState<Log[]>([]);
    const [loading, setLoading] = useState(false);
    const [searchInput, setSearchInput] = useState('');
    const [searchKeyword, setSearchKeyword] = useState('');
    const [typeFilter, setTypeFilter] = useState<string>('all');
    const [levelFilter, setLevelFilter] = useState<string>('all');
    const [page, setPage] = useState(1);
    const [pageSize] = useState(20);
    const [total, setTotal] = useState(0);
    const [showConfigModal, setShowConfigModal] = useState(false);
    const [config, setConfig] = useState<LogConfig>({ rotate_interval: 24, retention_days: 30 });
    const [savingConfig, setSavingConfig] = useState(false);

    // 获取日志配置
    const fetchConfig = async () => {
        try {
            const response = await fetch(`${API_BASE_URL}${API_ENDPOINTS.LOGS.CONFIG}`, {
                credentials: 'include',
            });

            if (!response.ok) {
                throw new Error('获取配置失败');
            }

            const result = await response.json();
            if (result.code === 0) {
                setConfig(result.data);
            }
        } catch (error: any) {
            console.error('获取日志配置失败:', error);
        }
    };

    // 获取日志列表
    const fetchLogs = async () => {
        setLoading(true);
        try {
            const params = new URLSearchParams({
                page: page.toString(),
                page_size: pageSize.toString(),
                type: typeFilter,
                level: levelFilter,
                keyword: searchKeyword,
            });

            const response = await fetch(`${API_BASE_URL}${API_ENDPOINTS.LOGS.LIST}?${params}`, {
                credentials: 'include',
            });

            if (!response.ok) {
                throw new Error('获取日志列表失败');
            }

            const result = await response.json();
            if (result.code === 0) {
                setLogs(result.data.list || []);
                setTotal(result.data.total || 0);
            } else {
                alert(result.msg || '获取日志列表失败');
            }
        } catch (error: any) {
            alert('获取日志列表失败: ' + error.message);
        } finally {
            setLoading(false);
        }
    };

    useEffect(() => {
        fetchConfig();
        fetchLogs();
    }, [page, typeFilter, levelFilter, searchKeyword]);

    const filteredLogs = logs;

    const getLevelClass = (level: string) => {
        const levelMap: Record<string, string> = {
            info: 'level-info',
            warning: 'level-warning',
            error: 'level-error',
        };
        return levelMap[level] || '';
    };

    const handleSaveConfig = async () => {
        setSavingConfig(true);
        try {
            const response = await fetch(`${API_BASE_URL}${API_ENDPOINTS.LOGS.CONFIG}`, {
                method: 'POST',
                headers: {
                    'Content-Type': 'application/json',
                },
                credentials: 'include',
                body: JSON.stringify(config),
            });

            if (!response.ok) {
                throw new Error('保存配置失败');
            }

            const result = await response.json();
            if (result.code === 0) {
                alert('配置保存成功');
                setShowConfigModal(false);
            } else {
                alert(result.msg || '保存配置失败');
            }
        } catch (error: any) {
            alert('保存配置失败: ' + error.message);
        } finally {
            setSavingConfig(false);
        }
    };

    const handleSearch = () => {
        setPage(1);
        setSearchKeyword(searchInput.trim());
    };

    const handleReset = () => {
        setSearchInput('');
        setSearchKeyword('');
        setTypeFilter('all');
        setLevelFilter('all');
        setPage(1);
    };

    return (
        <Layout title="日志管理">
            <div className="logs-container">
                <ManagementSearchPanel
                    title="日志检索与配置"
                    description="先按日志类型、级别和关键词缩小范围，再查看详情定位问题。日志导出接口当前未接通，所以先把查询与配置闭环做好。"
                    actions={(
                        <>
                            <button className="btn-secondary" onClick={handleReset}>重置筛选</button>
                            <button className="btn-secondary" onClick={fetchLogs} disabled={loading}>
                                <FiRefreshCw />
                                {loading ? '刷新中...' : '刷新'}
                            </button>
                            <button className="btn-secondary" onClick={() => setShowConfigModal(true)}>
                                <FiSettings />
                                配置
                            </button>
                        </>
                    )}
                    controls={(
                        <>
                            <div className="management-search-searchbox">
                                <FiSearch className="management-search-searchicon" />
                                <input
                                    type="text"
                                    className="management-search-input"
                                    placeholder="搜索日志内容、用户、错误关键字..."
                                    value={searchInput}
                                    onChange={(e) => setSearchInput(e.target.value)}
                                    onKeyDown={(e) => {
                                        if (e.key === 'Enter') {
                                            handleSearch();
                                        }
                                    }}
                                />
                            </div>
                            <select
                                className="management-search-select"
                                value={typeFilter}
                                onChange={(e) => {
                                    setTypeFilter(e.target.value);
                                    setPage(1);
                                }}
                            >
                                <option value="all">全部类型</option>
                                <option value="user_action">用户操作</option>
                                <option value="system">系统日志</option>
                                <option value="api">API请求</option>
                                <option value="error">错误日志</option>
                            </select>
                            <select
                                className="management-search-select"
                                value={levelFilter}
                                onChange={(e) => {
                                    setLevelFilter(e.target.value);
                                    setPage(1);
                                }}
                            >
                                <option value="all">全部级别</option>
                                <option value="info">信息</option>
                                <option value="warning">警告</option>
                                <option value="error">错误</option>
                            </select>
                            <button className="btn-primary" onClick={handleSearch}>搜索日志</button>
                        </>
                    )}
                    summary={(
                        <>
                            <div>
                                当前共 <strong>{total}</strong> 条日志
                            </div>
                            <div className="management-search-tags">
                                {searchKeyword ? <span className="management-search-tag">关键词：{searchKeyword}</span> : null}
                                {typeFilter !== 'all' ? <span className="management-search-tag">类型：{typeFilter}</span> : null}
                                {levelFilter !== 'all' ? <span className="management-search-tag">级别：{levelFilter}</span> : null}
                            </div>
                        </>
                    )}
                />

                {loading ? (
                    <div className="loading-state">加载中...</div>
                ) : (
                    <div className="logs-table-container">
                        <table className="logs-table">
                            <thead>
                                <tr>
                                    <th>时间</th>
                                    <th>类型</th>
                                    <th>级别</th>
                                    <th>用户</th>
                                    <th>消息</th>
                                    <th>操作</th>
                                </tr>
                            </thead>
                            <tbody>
                                {filteredLogs.length === 0 ? (
                                    <tr>
                                        <td colSpan={6} className="empty-state">
                                            暂无日志数据
                                        </td>
                                    </tr>
                                ) : (
                                    filteredLogs.map((log) => (
                                        <tr key={log.id}>
                                            <td>{new Date(log.timestamp).toLocaleString('zh-CN')}</td>
                                            <td>{log.type}</td>
                                            <td>
                                                <span className={`level-badge ${getLevelClass(log.level)}`}>
                                                    {log.level}
                                                </span>
                                            </td>
                                            <td>{log.user || '-'}</td>
                                            <td>{log.message}</td>
                                            <td>
                                                {log.details && (
                                                    <button
                                                        className="btn-action btn-view"
                                                        onClick={() => {
                                                            try {
                                                                const details = JSON.parse(log.details || '{}');
                                                                alert('详情：\n' + JSON.stringify(details, null, 2));
                                                            } catch {
                                                                alert('详情：' + log.details);
                                                            }
                                                        }}
                                                    >
                                                        查看详情
                                                    </button>
                                                )}
                                            </td>
                                        </tr>
                                    ))
                                )}
                            </tbody>
                        </table>
                    </div>
                )}

                {/* 分页 */}
                {total > pageSize && (
                    <div className="pagination">
                        <button
                            className="btn-page"
                            disabled={page === 1}
                            onClick={() => setPage(page - 1)}
                        >
                            上一页
                        </button>
                        <span className="page-info">
                            第 {page} 页，共 {Math.ceil(total / pageSize)} 页
                        </span>
                        <button
                            className="btn-page"
                            disabled={page >= Math.ceil(total / pageSize)}
                            onClick={() => setPage(page + 1)}
                        >
                            下一页
                        </button>
                    </div>
                )}

                {/* 配置模态框 */}
                {showConfigModal && (
                    <div className="modal-overlay" onClick={() => setShowConfigModal(false)}>
                        <div className="modal-content" onClick={(e) => e.stopPropagation()}>
                            <div className="modal-header">
                                <h3>日志配置</h3>
                                <button className="modal-close" onClick={() => setShowConfigModal(false)}>
                                    <FiX />
                                </button>
                            </div>
                            <div className="modal-body">
                                <div className="config-form">
                                    <div className="form-group">
                                        <label>日志分割间隔（小时）</label>
                                        <input
                                            type="number"
                                            min="1"
                                            max="168"
                                            value={config.rotate_interval}
                                            onChange={(e) => setConfig({
                                                ...config,
                                                rotate_interval: parseInt(e.target.value) || 24
                                            })}
                                            className="form-input"
                                        />
                                        <span className="form-hint">范围：1-168小时（1周）</span>
                                    </div>
                                    <div className="form-group">
                                        <label>日志保存天数</label>
                                        <input
                                            type="number"
                                            min="1"
                                            max="365"
                                            value={config.retention_days}
                                            onChange={(e) => setConfig({
                                                ...config,
                                                retention_days: parseInt(e.target.value) || 30
                                            })}
                                            className="form-input"
                                        />
                                        <span className="form-hint">范围：1-365天（1年）</span>
                                    </div>
                                </div>
                            </div>
                            <div className="modal-footer">
                                <button
                                    className="btn-secondary"
                                    onClick={() => setShowConfigModal(false)}
                                    disabled={savingConfig}
                                >
                                    取消
                                </button>
                                <button
                                    className="btn-primary"
                                    onClick={handleSaveConfig}
                                    disabled={savingConfig}
                                >
                                    {savingConfig ? '保存中...' : '保存'}
                                </button>
                            </div>
                        </div>
                    </div>
                )}
            </div>
        </Layout>
    );
};

export default Logs;
