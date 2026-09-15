import React, { useState, useEffect } from 'react';
import { FiSearch, FiUpload, FiTrash2, FiDownload, FiEye, FiRefreshCw, FiX } from 'react-icons/fi';
import Layout from '../component/layout';
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

    // 获取文件列表
    const fetchFiles = async () => {
        setLoading(true);
        try {
            const params = new URLSearchParams({
                page: page.toString(),
                page_size: pageSize.toString(),
                keyword: searchKeyword,
                file_type: typeFilter,
                source_type: sourceTypeFilter,
            });

            const response = await fetch(`${API_BASE_URL}${API_ENDPOINTS.OSS.LIST}?${params}`, {
                credentials: 'include',
            });

            if (!response.ok) {
                throw new Error('获取文件列表失败');
            }

            const result = await response.json();
            if (result.code === 0) {
                setFiles(result.data.list || []);
                setTotal(result.data.total || 0);
            } else {
                alert(result.msg || '获取文件列表失败');
            }
        } catch (error: any) {
            alert('获取文件列表失败: ' + error.message);
        } finally {
            setLoading(false);
        }
    };

    useEffect(() => {
        fetchFiles();
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
        setSelectedFiles(prev => 
            prev.includes(fileId) 
                ? prev.filter(id => id !== fileId)
                : [...prev, fileId]
        );
    };

    const handleSelectAll = () => {
        if (selectedFiles.length === files.length) {
            setSelectedFiles([]);
        } else {
            setSelectedFiles(files.map(f => f.id));
        }
    };

    const handleDelete = async (fileId: number) => {
        if (!window.confirm('确定要删除这个文件吗？此操作不可恢复！')) {
            return;
        }

        try {
            const response = await fetch(`${API_BASE_URL}${API_ENDPOINTS.OSS.DELETE(fileId.toString())}`, {
                method: 'DELETE',
                credentials: 'include',
            });

            if (!response.ok) {
                throw new Error('删除失败');
            }

            const result = await response.json();
            if (result.code === 0) {
                alert('删除成功');
                fetchFiles();
            } else {
                alert(result.msg || '删除失败');
            }
        } catch (error: any) {
            alert('删除失败: ' + error.message);
        }
    };

    const handleBatchDelete = async () => {
        if (selectedFiles.length === 0) return;
        if (!window.confirm(`确定要删除选中的 ${selectedFiles.length} 个文件吗？此操作不可恢复！`)) {
            return;
        }

        try {
            const response = await fetch(`${API_BASE_URL}${API_ENDPOINTS.OSS.BATCH_DELETE}`, {
                method: 'POST',
                headers: {
                    'Content-Type': 'application/json',
                },
                credentials: 'include',
                body: JSON.stringify({ ids: selectedFiles }),
            });

            if (!response.ok) {
                throw new Error('批量删除失败');
            }

            const result = await response.json();
            if (result.code === 0) {
                alert('批量删除成功');
                setSelectedFiles([]);
                fetchFiles();
            } else {
                alert(result.msg || '批量删除失败');
            }
        } catch (error: any) {
            alert('批量删除失败: ' + error.message);
        }
    };

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
        setPage(1);
        setSearchKeyword(searchInput.trim());
    };

    const handleReset = () => {
        setSearchInput('');
        setSearchKeyword('');
        setTypeFilter('all');
        setSourceTypeFilter('all');
        setPage(1);
        setSelectedFiles([]);
    };

    return (
        <Layout title="OSS管理">
            <div className="oss-container">
                <ManagementSearchPanel
                    title="OSS 文件检索与资产管理"
                    description="先按文件名、来源和类型找到目标资产，再做上传、预览、下载或删除，避免只在当前页本地过滤。"
                    actions={(
                        <>
                            {selectedFiles.length > 0 && (
                                <button className="btn-danger" onClick={handleBatchDelete}>
                                    <FiTrash2 />
                                    批量删除 ({selectedFiles.length})
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
                                <option value="image">图片</option>
                                <option value="video">视频</option>
                                <option value="audio">音频</option>
                                <option value="document">文档</option>
                                <option value="other">其他</option>
                            </select>
                            <select
                                className="management-search-select"
                                value={sourceTypeFilter}
                                onChange={(e) => {
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
                                当前共 <strong>{total}</strong> 个文件，已勾选 <strong>{selectedFiles.length}</strong> 个
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
                        <span className="stat-value">{total}</span>
                    </div>
                    <div className="stat-item">
                        <span className="stat-label">总存储</span>
                        <span className="stat-value">
                            {formatFileSize(files.reduce((sum, f) => sum + f.size, 0))}
                        </span>
                    </div>
                    <div className="stat-item">
                        <span className="stat-label">图片文件</span>
                        <span className="stat-value">
                            {files.filter(f => f.type === 'image').length}
                        </span>
                    </div>
                    <div className="stat-item">
                        <span className="stat-label">视频文件</span>
                        <span className="stat-value">
                            {files.filter(f => f.type === 'video').length}
                        </span>
                    </div>
                </div>

                {loading ? (
                    <div className="loading-state">加载中...</div>
                ) : (
                    <div className="oss-table-container">
                        <table className="oss-table">
                            <thead>
                                <tr>
                                    <th>
                                        <input
                                            type="checkbox"
                                            checked={selectedFiles.length === files.length && files.length > 0}
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
                                                    checked={selectedFiles.includes(file.id)}
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
        </Layout>
    );
};

export default OSS;
