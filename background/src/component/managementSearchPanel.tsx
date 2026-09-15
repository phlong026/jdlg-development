import React from 'react';
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
    const rootClassName = ['management-search-panel', 'section-card', className].filter(Boolean).join(' ');

    return (
        <div className={rootClassName}>
            <div className="management-search-panel__header">
                <div className="management-search-panel__title-block">
                    <h3>{title}</h3>
                    {description ? <p>{description}</p> : null}
                </div>
                {actions ? <div className="management-search-panel__actions">{actions}</div> : null}
            </div>
            <div className="management-search-panel__controls">{controls}</div>
            {summary ? <div className="management-search-panel__summary">{summary}</div> : null}
        </div>
    );
};

export default ManagementSearchPanel;
