import React, { useState } from 'react';
import { FiFilter } from 'react-icons/fi';
import { useIsMobile } from './useIsMobile';
import './managementSearchPanel.scss';

interface ManagementSearchPanelProps {
    title: string;
    description?: string;
    actions?: React.ReactNode;
    controls: React.ReactNode;
    summary?: React.ReactNode;
    className?: string;
}

const ManagementSearchPanel: React.FC<ManagementSearchPanelProps> = ({
    title,
    description,
    actions,
    controls,
    summary,
    className = '',
}) => {
    // 手机端默认收起筛选控件与摘要，只留标题 + 「筛选」开关 + 动作按钮横排；
    // 桌面端不渲染开关、全部常显，13 个使用方页面零改动受益。
    const isMobile = useIsMobile();
    const [showFilters, setShowFilters] = useState(false);
    const rootClassName = ['management-search-panel', 'section-card', className].filter(Boolean).join(' ');

    return (
        <div className={rootClassName}>
            <div className="management-search-panel__header">
                <div className="management-search-panel__title-block">
                    <h3>{title}</h3>
                    {description ? <p>{description}</p> : null}
                </div>
                {actions ? <div className="management-search-panel__actions">{actions}</div> : null}
                {isMobile ? (
                    <button
                        type="button"
                        className="btn-secondary management-search-panel__toggle"
                        aria-expanded={showFilters}
                        onClick={() => setShowFilters((value) => !value)}
                    >
                        <FiFilter />
                        {showFilters ? '收起筛选' : '筛选'}
                    </button>
                ) : null}
            </div>
            {isMobile && !showFilters ? null : <div className="management-search-panel__controls">{controls}</div>}
            {isMobile && !showFilters ? null : summary ? <div className="management-search-panel__summary">{summary}</div> : null}
        </div>
    );
};

export default ManagementSearchPanel;
