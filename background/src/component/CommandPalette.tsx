import React, { useEffect, useMemo, useRef, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { FiSearch } from 'react-icons/fi';
import './commandPalette.scss';

export interface PaletteItem {
    path: string;
    label: string;
    group: string;
}

interface CommandPaletteProps {
    open: boolean;
    items: PaletteItem[];
    onClose: () => void;
}

/** ⌘K / Ctrl+K 页面搜索：低频页面收进二级后，仍然可以一步直达。 */
const CommandPalette: React.FC<CommandPaletteProps> = ({ open, items, onClose }) => {
    const navigate = useNavigate();
    const [query, setQuery] = useState('');
    const [index, setIndex] = useState(0);
    const inputRef = useRef<HTMLInputElement>(null);
    const closeRef = useRef(onClose);
    closeRef.current = onClose;

    useEffect(() => {
        if (!open) return;
        setQuery('');
        setIndex(0);
        const previous = document.activeElement as HTMLElement | null;
        inputRef.current?.focus({ preventScroll: true });
        const onKey = (event: KeyboardEvent) => {
            if (event.isComposing) return;
            if (event.key === 'Escape') {
                event.preventDefault();
                event.stopImmediatePropagation();
                closeRef.current();
            } else if (event.key === 'Tab') {
                const panel = inputRef.current?.closest('.cmdk-panel');
                const items = Array.from(panel?.querySelectorAll<HTMLElement>('input, button') || [])
                    .filter(item => !item.matches(':disabled') && item.tabIndex >= 0 && item.getClientRects().length > 0);
                const first = items[0], last = items[items.length - 1];
                if (event.shiftKey && (document.activeElement === first || !panel?.contains(document.activeElement))) {
                    event.preventDefault(); last?.focus({ preventScroll: true });
                } else if (!event.shiftKey && (document.activeElement === last || !panel?.contains(document.activeElement))) {
                    event.preventDefault(); first?.focus({ preventScroll: true });
                }
                event.stopPropagation();
            }
        };
        // This is above dialogs and the mobile sidebar. One Escape closes one layer.
        window.addEventListener('keydown', onKey, true);
        return () => {
            window.removeEventListener('keydown', onKey, true);
            if (previous?.isConnected && !previous.matches(':disabled') && previous.getClientRects().length) {
                previous.focus({ preventScroll: true });
            }
        };
    }, [open]);

    const list = useMemo(() => {
        const q = query.trim().toLowerCase();
        // 只匹配页面名与路径：分组名不参与匹配（避免搜"设计"时命中整个分组的所有页面）
        return items.filter((item) => !q || `${item.label}${item.path}`.toLowerCase().includes(q)).slice(0, 12);
    }, [items, query]);

    if (!open) return null;

    const pick = (item?: PaletteItem) => {
        if (!item) return;
        onClose();
        navigate(item.path);
    };

    const onKeyDown = (event: React.KeyboardEvent) => {
        if (event.key === 'ArrowDown') { event.preventDefault(); setIndex((i) => Math.min(i + 1, list.length - 1)); }
        if (event.key === 'ArrowUp') { event.preventDefault(); setIndex((i) => Math.max(i - 1, 0)); }
        if (event.key === 'Enter') { event.preventDefault(); pick(list[index]); }
    };

    return (
        <div className="cmdk-overlay" onMouseDown={(event) => { if (event.target === event.currentTarget) onClose(); }}>
            <section className="cmdk-panel" role="dialog" aria-modal="true" aria-label="搜索页面">
                <div className="cmdk-input">
                    <FiSearch />
                    <input
                        ref={inputRef}
                        value={query}
                        placeholder="输入页面名称，例如「充值配置」「设计师」"
                        onChange={(event) => { setQuery(event.target.value); setIndex(0); }}
                        onKeyDown={onKeyDown}
                    />
                </div>
                <ul className="cmdk-list">
                    {list.length === 0 ? (
                        <li className="cmdk-empty">没有匹配的页面</li>
                    ) : list.map((item, i) => (
                        <li key={item.path}>
                            <button type="button" className={`cmdk-item ${i === index ? 'on' : ''}`} onMouseEnter={() => setIndex(i)} onClick={() => pick(item)}>
                                <span>{item.label}</span>
                                <span className="cmdk-group">{item.group}</span>
                            </button>
                        </li>
                    ))}
                </ul>
            </section>
        </div>
    );
};

export default CommandPalette;
