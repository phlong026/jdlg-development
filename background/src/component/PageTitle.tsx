import React from 'react';

interface PageTitleProps {
    title: string;
    /** 页头里与标题同一行的附加内容（状态、刷新时间、按钮等） */
    children?: React.ReactNode;
    className?: string;
}

// 一级页标题的唯一写法：样式见 App.scss 的 .admin-page-head / .admin-page-title
const PageTitle: React.FC<PageTitleProps> = ({ title, children, className }) => (
    <header className={['admin-page-head', className].filter(Boolean).join(' ')}>
        <h1 className="admin-page-title">{title}</h1>
        {children}
    </header>
);

export default PageTitle;
