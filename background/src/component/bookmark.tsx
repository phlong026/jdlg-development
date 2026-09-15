import React, { createContext, useContext, useState, useEffect, useCallback } from 'react';
import { useLocation, useNavigate } from 'react-router-dom';
import { FiX, FiRefreshCw, FiMaximize2 } from 'react-icons/fi';
import { routes } from '../routes';
import './bookmark.scss';
import { useManagementSession } from './ManagementSession';

interface BookmarkItem {
    path: string;
    title: string;
    key: string; // 唯一标识，用于区分相同路径的不同标签页
    icon?: React.ComponentType<any>;
}

interface BookmarkContextType {
    bookmarks: BookmarkItem[];
    addBookmark: (path: string, title: string) => void;
    removeBookmark: (key: string) => void;
    activeKey: string;
    setActiveKey: (key: string) => void;
    getActiveBookmark: () => BookmarkItem | undefined;
    refreshKey: number;
    triggerRefresh: () => void;
}

const BookmarkContext = createContext<BookmarkContextType | undefined>(undefined);

export const useBookmark = () => {
    const context = useContext(BookmarkContext);
    if (!context) {
        throw new Error('useBookmark must be used within BookmarkProvider');
    }
    return context;
};

// Bookmark Provider 组件
export const BookmarkProvider: React.FC<{ children: React.ReactNode }> = ({ children }) => {
    const session = useManagementSession();
    const [bookmarks, setBookmarks] = useState<BookmarkItem[]>([]);
    const [activeKey, setActiveKey] = useState<string>('');
    const [refreshKey, setRefreshKey] = useState<number>(0);
    useEffect(() => { setBookmarks([]); setActiveKey(''); }, [session.user?.id]);
    const location = useLocation();
    const navigate = useNavigate();

    // 根据路径查找路由信息
    const getRouteInfo = useCallback((path: string): { title: string; icon?: React.ComponentType<any> } => {
        const route = routes.find(r => r.path === path);
        return {
            title: route?.title || path,
            icon: route?.icon as React.ComponentType<any> | undefined,
        };
    }, []);

    // 生成唯一key
    const generateKey = useCallback((path: string): string => {
        return `${path}_${Date.now()}_${Math.random().toString(36).substr(2, 9)}`;
    }, []);

    // 添加书签
    const addBookmark = useCallback((path: string, title?: string) => {
        setBookmarks(prev => {
            // 检查是否已存在相同路径的书签
            const existing = prev.find(b => b.path === path);
            if (existing) {
                // 如果已存在，激活它
                setActiveKey(existing.key);
                return prev;
            }

            // 获取路由信息
            const routeInfo = getRouteInfo(path);

            // 创建新书签
            const newBookmark: BookmarkItem = {
                path,
                title: title || routeInfo.title,
                key: generateKey(path),
                icon: routeInfo.icon,
            };

            setActiveKey(newBookmark.key);
            return [...prev, newBookmark];
        });
    }, [getRouteInfo, generateKey]);

    // 移除书签
    const removeBookmark = useCallback((key: string) => {
        setBookmarks(prev => {
            // 至少保留一个书签
            if (prev.length <= 1) {
                return prev;
            }

            const filtered = prev.filter(b => b.key !== key);

            // 如果关闭的是当前激活的书签，切换到其他书签
            if (activeKey === key) {
                const currentIndex = prev.findIndex(b => b.key === key);
                let newActiveKey: string;

                if (currentIndex > 0) {
                    // 切换到前一个
                    newActiveKey = prev[currentIndex - 1].key;
                } else {
                    // 切换到第一个
                    newActiveKey = filtered[0].key;
                }

                setActiveKey(newActiveKey);
                navigate(filtered.find(b => b.key === newActiveKey)?.path || '/users');
            }

            return filtered;
        });
    }, [activeKey, navigate]);

    // 监听路由变化，自动添加书签
    useEffect(() => {
        // 排除登录页
        if (location.pathname === '/login' || location.pathname === '/') {
            return;
        }

        const routeInfo = getRouteInfo(location.pathname);
        addBookmark(location.pathname, routeInfo.title);
    }, [location.pathname, addBookmark, getRouteInfo]);

    // 初始化：如果没有书签，添加当前页面的书签
    useEffect(() => {
        if (bookmarks.length === 0 && location.pathname !== '/login' && location.pathname !== '/') {
            const routeInfo = getRouteInfo(location.pathname);
            addBookmark(location.pathname, routeInfo.title);
        }
    }, []); // 只在组件挂载时执行一次

    // 获取当前激活的书签
    const getActiveBookmark = useCallback((): BookmarkItem | undefined => {
        if (!activeKey) {
            // 如果没有激活的key，尝试根据路径查找
            const bookmark = bookmarks.find(b => b.path === location.pathname);
            return bookmark;
        }
        return bookmarks.find(b => b.key === activeKey);
    }, [activeKey, bookmarks, location.pathname]);

    // 触发刷新（通过改变 refreshKey 来强制重新渲染）
    const triggerRefresh = useCallback(() => {
        setRefreshKey(prev => prev + 1);
    }, []);

    const value: BookmarkContextType = {
        bookmarks,
        addBookmark,
        removeBookmark,
        activeKey,
        setActiveKey,
        getActiveBookmark,
        refreshKey,
        triggerRefresh,
    };

    return (
        <BookmarkContext.Provider value={value}>
            {children}
        </BookmarkContext.Provider>
    );
};

// Bookmark 组件（标签页栏）
const Bookmark: React.FC = () => {
    const { bookmarks, removeBookmark, activeKey, setActiveKey, triggerRefresh } = useBookmark();
    const navigate = useNavigate();
    const location = useLocation();
    const [isFullscreen, setIsFullscreen] = useState(false);

    // 点击标签页切换
    const handleTabClick = (bookmark: BookmarkItem) => {
        setActiveKey(bookmark.key);
        navigate(bookmark.path);
    };

    // 关闭标签页
    const handleClose = (e: React.MouseEvent, bookmark: BookmarkItem) => {
        e.stopPropagation();
        removeBookmark(bookmark.key);
    };

    // 处理刷新
    const handleRefresh = (e: React.MouseEvent) => {
        e.stopPropagation();
        triggerRefresh();
    };

    // 处理全屏
    const handleFullscreen = (e: React.MouseEvent) => {
        e.stopPropagation();
        if (!document.fullscreenElement) {
            document.documentElement.requestFullscreen().then(() => {
                setIsFullscreen(true);
            }).catch(err => {
                console.error('无法进入全屏模式:', err);
            });
        } else {
            document.exitFullscreen().then(() => {
                setIsFullscreen(false);
            }).catch(err => {
                console.error('无法退出全屏模式:', err);
            });
        }
    };

    // 监听全屏状态变化
    useEffect(() => {
        const handleFullscreenChange = () => {
            setIsFullscreen(!!document.fullscreenElement);
        };

        document.addEventListener('fullscreenchange', handleFullscreenChange);
        return () => {
            document.removeEventListener('fullscreenchange', handleFullscreenChange);
        };
    }, []);

    // 排除登录页不显示书签
    if (location.pathname === '/login' || location.pathname === '/') {
        return null;
    }

    // 如果没有书签，不显示
    if (bookmarks.length === 0) {
        return null;
    }

    return (
        <div className="bookmark-container">
            <div className="bookmark-tabs">
                {bookmarks.map((bookmark, index) => {
                    const isActive = bookmark.key === activeKey || bookmark.path === location.pathname;
                    return (
                        <React.Fragment key={bookmark.key}>
                            {index > 0 && <div className="bookmark-tab-divider"></div>}
                            <div
                                className={`bookmark-tab ${isActive ? 'active' : ''}`}
                                onClick={() => handleTabClick(bookmark)}
                            >
                                {bookmark.icon && (
                                    <bookmark.icon
                                        size={14}
                                        className="bookmark-icon"
                                    />
                                )}
                                <span className="bookmark-title">{bookmark.title}</span>
                                {bookmarks.length > 1 && (
                                    <button
                                        className="bookmark-close"
                                        onClick={(e) => handleClose(e, bookmark)}
                                        title="关闭"
                                        type="button"
                                    >
                                        <FiX size={12} />
                                    </button>
                                )}
                            </div>
                        </React.Fragment>
                    );
                })}
            </div>
            <div className="bookmark-divider"></div>
            <div className="bookmark-actions">
                <button
                    className="bookmark-action-btn"
                    onClick={handleRefresh}
                    title="刷新"
                    type="button"
                >
                    <FiRefreshCw size={16} />
                </button>
                <button
                    className="bookmark-action-btn"
                    onClick={handleFullscreen}
                    title={isFullscreen ? "退出全屏" : "全屏"}
                    type="button"
                >
                    <FiMaximize2 size={16} />
                </button>
            </div>
        </div>
    );
};

export default Bookmark;
