import { reportPageLoadError, reportPageLoadRecovered } from '../utils/pageLoadError';
import React, { useState, useEffect, useRef } from 'react';
import { FiSearch, FiUpload, FiTrash2, FiDownload, FiEye, FiRefreshCw, FiX } from 'react-icons/fi';
import ManagementSearchPanel from '../component/managementSearchPanel';
import { API_BASE_URL, API_ENDPOINTS } from '../config/api';
import './oss.scss';

interface OSSFile {
    id: number;
    name: string;
    key: string;
    size: number;
    type: string;
    url: string;
    upload_time: string;
    uploader?: string;
    source_type?: string;
    source_name?: string;
}

const OSS: React.FC = () => {
    const [files, setFiles] = useState<OSSFile[]>([]);
    const [loading, setLoading] = useState(false);
    const [searchInput, setSearchInput] = useState('');
    const [searchKeyword, setSearchKeyword] = useState('');
    const [typeFilter, setTypeFilter] = useState<string>('all');
    const [sourceTypeFilter, setSourceTypeFilter] = useState<string>('all');
    const [selectedFiles, setSelectedFiles] = useState<number[]>([]);
    const [page, setPage] = useState(1);
    const [pageSize] = useState(20);
    const [total, setTotal] = useState(0);
    const [showUploadModal, setShowUploadModal] = useState(false);
    const [uploading, setUploading] = useState(false);
    const [deleting, setDeleting] = useState(false);
    const [loadError, setLoadError] = useState('');
    const [loadedScope, setLoadedScope] = useState<string | null>(null);
    const scope = JSON.stringify([page, pageSize, searchKeyword, typeFilter, sourceTypeFilter]);
    const scopeRef = useRef(scope);
    const requestVersion = useRef(0);
    const mounted = useRef(true);
    const deletePending = useRef(false);
    const ready = loadedScope === scope && !loading && !loadError;
    const visibleFiles = ready ? files : [];
    const selectedOnPage = visibleFiles.filter(file => selectedFiles.includes(file.id));
    const allSelected = visibleFiles.length > 0 && selectedOnPage.length === visibleFiles.length;
    const partiallySelected = selectedOnPage.length > 0 && !allSelected;
    const selectionRef = useRef({ ready, files: visibleFiles, ids: selectedOnPage.map(file => file.id) });
    scopeRef.current = scope;
    selectionRef.current = { ready, files: visibleFiles, ids: selectedOnPage.map(file => file.id) };

    // Scope changes invalidate handlers and in-flight reads immediately, before the next render.
    const invalidateList = () => {
        requestVersion.current += 1;
        selectionRef.current = { ready: false, files: [], ids: [] };
        setSelectedFiles([]);
        setLoadedScope(null);
        setLoadError('');
    };

    useEffect(() => {
        mounted.current = true;
        return () => {
            mounted.current = false;
            requestVersion.current += 1;
        };
    }, []);

    // Only the newest successful read for the current scope may expose selectable files.
    const fetchFiles = async () => {
        if (!mounted.current || scopeRef.current !== scope) return;
        invalidateList();
        const version = requestVersion.current;
        const isCurrent = () => mounted.current && version === requestVersion.current && scopeRef.current === scope;
        setLoading(true);
        try {
            const params = new URLSearchParams({
                page: page.toString(), page_size: pageSize.toString(), keyword: searchKeyword,
                file_type: typeFilter, source_type: sourceTypeFilter,
            });
            const response = await fetch(`${API_BASE_URL}${API_ENDPOINTS.OSS.LIST}?${params}`, { credentials: 'include' });
            if (!response.ok) throw new Error('获取文件列表失败');
            const result = await response.json();
            if (!isCurrent()) return;
            if (result.code !== 0) throw new Error(result.msg || '获取文件列表失败');
            if (!result.data || (result.data.list !== null && !Array.isArray(result.data.list))) throw new Error('文件列表响应无效');
            if (!Number.isFinite(result.data.total) || result.data.total < 0) throw new Error('文件总数响应无效');
            const lastPage = Math.max(1, Math.ceil(result.data.total / pageSize));
            if (page > lastPage) {
                invalidateList();
                setPage(lastPage);
                return;
            }
            // Preserve the API's nullable-empty-list compatibility without accepting a missing payload.
            setFiles(result.data.list || []);
            setTotal(result.data.total);
            setLoadedScope(scope);
            reportPageLoadRecovered('获取文件列表失败');
        } catch (error: any) {
            if (!isCurrent()) return;
            setLoadError(error?.message || '获取文件列表失败');
            reportPageLoadError(error, '获取文件列表失败');
        } finally {
            if (isCurrent()) setLoading(false);
        }
    };

    useEffect(() => {
        void fetchFiles();
    }, [page, searchKeyword, sourceTypeFilter, typeFilter]);

    const formatFileSize = (bytes: number): string => {
        if (bytes < 1024) return bytes + ' B';
        if (bytes < 1024 * 1024) return (bytes / 1024).toFixed(2) + ' KB';
        if (bytes < 1024 * 1024 * 1024) return (bytes / (1024 * 1024)).toFixed(2) + ' MB';
        return (bytes / (1024 * 1024 * 1024)).toFixed(2) + ' GB';
    };

    const getFileTypeIcon = (type: string) => {
        const typeMap: Record<string, string> = {
            image: '🖼️',
            video: '🎥',
            audio: '🎵',
            document: '📄',
            other: '📦',
        };
        return typeMap[type] || typeMap.other;
    };

    const handleSelectFile = (fileId: number) => {
        const current = selectionRef.current;
        if (!mounted.current || deletePending.current || !current.ready || scopeRef.current !== scope || !current.files.some(file => file.id === fileId)) return;
        const ids = current.ids.includes(fileId) ? current.ids.filter(id => id !== fileId) : [...current.ids, fileId];
        selectionRef.current = { ...current, ids };
        setSelectedFiles(ids);
    };

    const handleSelectAll = () => {
        const current = selectionRef.current;
        if (!mounted.current || deletePending.current || !current.ready || scopeRef.current !== scope) return;
        const ids = current.files.length > 0 && current.files.every(file => current.ids.includes(file.id))
            ? [] : current.files.map(file => file.id);
        selectionRef.current = { ...current, ids };
        setSelectedFiles(ids);
    };

    const deleteFiles = async (fileId?: number) => {
        const current = selectionRef.current;
        if (!mounted.current || deletePending.current || !current.ready || scopeRef.current !== scope) return;
        // Freeze both the displayed identities and request IDs before confirmation.
        const targets = current.files.filter(file => fileId === undefined ? current.ids.includes(file.id) : file.id === fileId)
            .map(({ id, name, key }) => ({ id, name, key }));
        if (targets.length === 0) return;
        deletePending.current = true;
        const batch = fileId === undefined;
        const targetDescription = targets.map(file => `• ${file.name}（ID: ${file.id}）\n  路径：${file.key}`).join('\n');
        try {
            if (!window.confirm(`确定要删除以下 ${targets.length} 个文件吗？此操作不可恢复！\n\n${targetDescription}`)) return;
            if (!mounted.current || scopeRef.current !== scope || !selectionRef.current.ready) return;
            setDeleting(true);
            const response = await fetch(`${API_BASE_URL}${batch ? API_ENDPOINTS.OSS.BATCH_DELETE : API_ENDPOINTS.OSS.DELETE(targets[0].id.toString())}`, {
                method: batch ? 'POST' : 'DELETE',
                credentials: 'include',
                ...(batch ? { headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ ids: targets.map(file => file.id) }) } : {}),
            });
            if (!response.ok) throw new Error(batch ? '批量删除失败' : '删除失败');
            const result = await response.json();
            if (result.code !== 0) throw new Error(result.msg || '删除失败');
            if (mounted.current) {
                alert(batch ? '批量删除成功' : '删除成功');
                if (scopeRef.current === scope) void fetchFiles();
            }
        } catch (error: any) {
            if (mounted.current) alert('删除失败: ' + (error?.message || '请重试'));
        } finally {
            deletePending.current = false;
            if (mounted.current) setDeleting(false);
        }
    };

    const handleDelete = (fileId: number) => deleteFiles(fileId);
    const handleBatchDelete = () => deleteFiles();

    const handleUpload = async (event: React.ChangeEvent<HTMLInputElement>) => {
        const file = event.target.files?.[0];
        if (!file) return;

        setUploading(true);
        const formData = new FormData();
        formData.append('file', file);

        try {
            const response = await fetch(`${API_BASE_URL}${API_ENDPOINTS.OSS.UPLOAD}`, {
                method: 'POST',
                credentials: 'include',
                body: formData,
            });

            if (!response.ok) {
                throw new Error('上传失败');
            }

            const result = await response.json();
            if (result.code === 0) {
                alert('上传成功');
                setShowUploadModal(false);
                fetchFiles();
            } else {
                alert(result.msg || '上传失败');
            }
        } catch (error: any) {
            alert('上传失败: ' + error.message);
        } finally {
            setUploading(false);
            // 清空input
            event.target.value = '';
        }
    };

    const handleSearch = () => {
        if (page === 1 && searchKeyword === searchInput.trim()) { void fetchFiles(); return; }
        invalidateList();
        setPage(1);
        setSearchKeyword(searchInput.trim());
    };

    const handleReset = () => {
        if (page === 1 && !searchKeyword && typeFilter === 'all' && sourceTypeFilter === 'all') { setSearchInput(''); void fetchFiles(); return; }
        invalidateList();
        setSearchInput('');
        setSearchKeyword('');
        setTypeFilter('all');
        setSourceTypeFilter('all');
        setPage(1);
        setSelectedFiles([]);
    };

    return (
        <>
            <div className="oss-container">
                <ManagementSearchPanel
                    title="OSS 文件检索与资产管理"
                    description="先按文件名、来源和类型找到目标资产，再做上传、预览、下载或删除，避免只在当前页本地过滤。"
                    actions={(
                        <>
                            {selectedOnPage.length > 0 && (
                                <button className="btn-danger" onClick={handleBatchDelete} disabled={deleting}>
                                    <FiTrash2 />
                                    {deleting ? '删除中...' : `批量删除 (${selectedOnPage.length})`}
                                </button>
                            )}
                            <button className="btn-secondary" onClick={handleReset}>重置筛选</button>
                            <button className="btn-secondary" onClick={fetchFiles} disabled={loading}>
                                <FiRefreshCw />
                                {loading ? '刷新中...' : '刷新'}
                            </button>
                            <button className="btn-primary" onClick={() => setShowUploadModal(true)}>
                                <FiUpload />
                                上传文件
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
                                    placeholder="搜索文件名或路径..."
                                    aria-label="文件名或路径"
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
                                aria-label="文件类型"
                                value={typeFilter}
                                onChange={(e) => {
                                    invalidateList();
                                    setTypeFilter(e.target.value);
                                    setPage(1);
                                }}
                            >
                                <option value="all">全部类型</option>
                                <option value="image">图片</option>
                                <option value="video">视频</option>
                                <option value="audio">音频</option>
                                <option value="document">文档</option>
                                <option value="other">其他</option>
                            </select>
                            <select
                                className="management-search-select"
                                aria-label="文件来源"
                                value={sourceTypeFilter}
                                onChange={(e) => {
                                    invalidateList();
                                    setSourceTypeFilter(e.target.value);
                                    setPage(1);
                                }}
                            >
                                <option value="all">全部来源</option>
                                <option value="user_ai">用户AI生成</option>
                                <option value="admin_upload">管理员上传</option>
                            </select>
                            <button className="btn-primary" onClick={handleSearch}>搜索文件</button>
                        </>
                    )}
                    summary={(
                        <>
                            <div>
                                当前共 <strong>{ready ? total : '—'}</strong> 个文件，本页已勾选 <strong>{selectedOnPage.length}</strong> 个（换页、筛选或刷新会清空选择）
                            </div>
                            <div className="management-search-tags">
                                {searchKeyword ? <span className="management-search-tag">关键词：{searchKeyword}</span> : null}
                                {typeFilter !== 'all' ? <span className="management-search-tag">类型：{typeFilter}</span> : null}
                                {sourceTypeFilter !== 'all' ? <span className="management-search-tag">来源：{sourceTypeFilter === 'user_ai' ? '用户AI生成' : '管理员上传'}</span> : null}
                            </div>
                        </>
                    )}
                />

                <div className="oss-stats">
                    <div className="stat-item">
                        <span className="stat-label">文件总数</span>
                        <span className="stat-value">{ready ? total : '—'}</span>
                    </div>
                    <div className="stat-item">
                        <span className="stat-label">本页存储</span>
                        <span className="stat-value">
                            {ready ? formatFileSize(visibleFiles.reduce((sum, f) => sum + f.size, 0)) : '—'}
                        </span>
                    </div>
                    <div className="stat-item">
                        <span className="stat-label">本页图片</span>
                        <span className="stat-value">
                            {ready ? visibleFiles.filter(f => f.type === 'image').length : '—'}
                        </span>
                    </div>
                    <div className="stat-item">
                        <span className="stat-label">本页视频</span>
                        <span className="stat-value">
                            {ready ? visibleFiles.filter(f => f.type === 'video').length : '—'}
                        </span>
                    </div>
                </div>

                {loadError ? (
                    <div className="oss-load-error" role="alert">文件列表加载失败：{loadError}。请刷新重试，当前不能选择或删除文件。</div>
                ) : !ready ? (
                    <div className="loading-state" role="status">加载中...</div>
                ) : (
                    <div className="oss-table-container">
                        <table className="oss-table">
                            <thead>
                                <tr>
                                    <th>
                                        <input
                                            type="checkbox"
                                            aria-label="选择本页全部文件"
                                            checked={allSelected}
                                            aria-checked={partiallySelected ? 'mixed' : allSelected}
                                            ref={(element) => { if (element) element.indeterminate = partiallySelected; }}
                                            disabled={deleting}
                                            onChange={handleSelectAll}
                                        />
                                    </th>
                                    <th>文件名</th>
                                    <th>路径</th>
                                    <th>类型</th>
                                    <th>大小</th>
                                    <th>上传时间</th>
                                    <th>来源</th>
                                    <th>操作</th>
                                </tr>
                            </thead>
                            <tbody>
                                {files.length === 0 ? (
                                    <tr>
                                        <td colSpan={8} className="empty-state">
                                            暂无文件数据
                                        </td>
                                    </tr>
                                ) : (
                                    files.map((file) => (
                                        <tr key={file.id}>
                                            <td>
                                                <input
                                                    type="checkbox"
                                                    aria-label={`选择文件 ${file.name}（ID: ${file.id}）`}
                                                    checked={selectedOnPage.some(selected => selected.id === file.id)}
                                                    disabled={deleting}
                                                    onChange={() => handleSelectFile(file.id)}
                                                />
                                            </td>
                                            <td>
                                                <div className="file-name-cell">
                                                    <span className="file-icon">{getFileTypeIcon(file.type)}</span>
                                                    <span className="file-name">{file.name}</span>
                                                </div>
                                            </td>
                                            <td>
                                                <span className="file-path">{file.key}</span>
                                            </td>
                                            <td>
                                                <span className="file-type-badge">{file.type}</span>
                                            </td>
                                            <td>{formatFileSize(file.size)}</td>
                                            <td>{new Date(file.upload_time).toLocaleString('zh-CN')}</td>
                                            <td>{file.uploader || '-'}</td>
                                            <td>
                                                <div className="action-buttons">
                                                    <button
                                                        className="btn-action btn-view"
                                                        onClick={() => window.open(file.url, '_blank')}
                                                        title="查看"
                                                    >
                                                        <FiEye size={14} />
                                                    </button>
                                                    <button
                                                        className="btn-action btn-download"
                                                        onClick={() => window.open(file.url, '_blank')}
                                                        title="下载"
                                                    >
                                                        <FiDownload size={14} />
                                                    </button>
                                                    <button
                                                        className="btn-action btn-delete"
                                                        onClick={() => handleDelete(file.id)}
                                                        disabled={deleting}
                                                        title="删除"
                                                    >
                                                        <FiTrash2 size={14} />
                                                    </button>
                                                </div>
                                            </td>
                                        </tr>
                                    ))
                                )}
                            </tbody>
                        </table>
                    </div>
                )}

                {/* 分页 */}
                {ready && (total > pageSize || page > 1) && (
                    <div className="pagination">
                        <button
                            className="btn-page"
                            disabled={page === 1}
                            onClick={() => { invalidateList(); setPage(page - 1); }}
                        >
                            上一页
                        </button>
                        <span className="page-info">
                            第 {page} 页，共 {Math.max(1, Math.ceil(total / pageSize))} 页
                        </span>
                        <button
                            className="btn-page"
                            disabled={page >= Math.ceil(total / pageSize)}
                            onClick={() => { invalidateList(); setPage(page + 1); }}
                        >
                            下一页
                        </button>
                    </div>
                )}

                {/* 上传模态框 */}
                {showUploadModal && (
                    <div className="modal-overlay" onClick={() => setShowUploadModal(false)}>
                        <div className="modal-content" onClick={(e) => e.stopPropagation()}>
                            <div className="modal-header">
                                <h3>上传文件</h3>
                                <button className="modal-close" onClick={() => setShowUploadModal(false)}>
                                    <FiX />
                                </button>
                            </div>
                            <div className="modal-body">
                                <input
                                    type="file"
                                    id="file-upload"
                                    onChange={handleUpload}
                                    disabled={uploading}
                                    style={{ display: 'none' }}
                                />
                                <label htmlFor="file-upload" className="upload-label">
                                    {uploading ? '上传中...' : '选择文件'}
                                </label>
                            </div>
                        </div>
                    </div>
                )}
            </div>
        </>
    );
};

export default OSS;
