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

    useEffect(() => {
        if (!open) return;
        setQuery('');
        setIndex(0);
        inputRef.current?.focus();
        const onKey = (event: KeyboardEvent) => { if (event.key === 'Escape') onClose(); };
        window.addEventListener('keydown', onKey);
        return () => window.removeEventListener('keydown', onKey);
    }, [open, onClose]);

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
