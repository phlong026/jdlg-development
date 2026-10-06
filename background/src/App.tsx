import React from 'react';
import { BrowserRouter, Routes, Route, Navigate } from 'react-router-dom';
import { BookmarkProvider } from './component/bookmark';
import './App.scss';
import './responsive.scss';
import routes from './routes';
import { ManagementSessionProvider } from './component/ManagementSession';

const App: React.FC = () => {
    return (
        <BrowserRouter basename="/admin">
            <ManagementSessionProvider>
            <BookmarkProvider>
                <div className="App">
                    <Routes>
                        <Route path="/" element={<Navigate to="/dashboard" replace />} />
                        {routes.map((route) => (
                            <Route key={route.path} path={route.path} element={route.element} />
                        ))}
                        {/* 兜底：未知网址不再整页空白，回到总控台（未登录会由 ProtectedRoute 转到登录页） */}
                        <Route path="*" element={<Navigate to="/dashboard" replace />} />
                    </Routes>
                </div>
            </BookmarkProvider>
            </ManagementSessionProvider>
        </BrowserRouter>
    );
};

export default App;
