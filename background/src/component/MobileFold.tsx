import React, { useState } from 'react';
import { FiChevronDown, FiFilter } from 'react-icons/fi';
import { useIsMobile } from './useIsMobile';

interface MobileFoldProps {
    /** 收起时按钮上的文字，例如「筛选」「更多操作」 */
    label: string;
    /** 已生效的筛选条件数，>0 时显示在按钮上，提醒列表已被筛过 */
    activeCount?: number;
    /** 为 true 时不折叠，直接显示（例如还没选用户时，搜索框本身就是主内容） */
    disabled?: boolean;
    children: React.ReactNode;
}

/**
 * 手机端把筛选、批量操作这类非核心区块折叠成一个按钮，首屏留给业务数据；
 * 桌面端原样渲染 children，不加任何包裹。
 */
export default function MobileFold({ label, activeCount = 0, disabled = false, children }: MobileFoldProps) {
    const isMobile = useIsMobile();
    const [open, setOpen] = useState(false);
    if (!isMobile || disabled) return <>{children}</>;

    return (
        <div className={`mobile-fold${open ? ' is-open' : ''}`}>
            <button type="button" className="mobile-fold__toggle" aria-expanded={open} onClick={() => setOpen((v) => !v)}>
                <FiFilter />
                <span>{open ? '收起' : label}</span>
                {activeCount > 0 && !open ? <em className="mobile-fold__badge">{activeCount}</em> : null}
                <FiChevronDown className="mobile-fold__chevron" />
            </button>
            {open ? <div className="mobile-fold__body">{children}</div> : null}
        </div>
    );
}
