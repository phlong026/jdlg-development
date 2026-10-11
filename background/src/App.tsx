import React from 'react';
import { BrowserRouter, Routes, Route, Navigate } from 'react-router-dom';
import './App.scss';
import './responsive.scss';
import './responsive-priority.scss';
import routes from './routes';
import AdminLayout from './component/layout';
import ProtectedRoute from './component/ProtectedRoute';
import { ManagementSessionProvider } from './component/ManagementSession';

// 登录页是唯一不需要鉴权与布局的页面；其余页面共用同一个受保护的布局，侧栏 / 顶栏只挂载一次。
const PUBLIC_PATHS = ['/login'];

const App: React.FC = () => {
    return (
        <BrowserRouter basename="/admin">
            <ManagementSessionProvider>
                <div className="App">
                    <Routes>
                        <Route path="/" element={<Navigate to="/dashboard" replace />} />
                        {routes.filter((route) => PUBLIC_PATHS.includes(route.path)).map((route) => (
                            <Route key={route.path} path={route.path} element={route.element} />
                        ))}
                        <Route element={<ProtectedRoute><AdminLayout /></ProtectedRoute>}>
                            {routes.filter((route) => !PUBLIC_PATHS.includes(route.path)).map((route) => (
                                <Route key={route.path} path={route.path} element={route.element} />
                            ))}
                        </Route>
                        {/* 兜底：未知网址不再整页空白，回到总控台（未登录会由 ProtectedRoute 转到登录页） */}
                        <Route path="*" element={<Navigate to="/dashboard" replace />} />
                    </Routes>
                </div>
            </ManagementSessionProvider>
        </BrowserRouter>
    );
};

export default App;
