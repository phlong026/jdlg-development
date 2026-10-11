import React, { useEffect, useId, useRef, useState } from 'react';
import { Link } from 'react-router-dom';
import { FiArrowLeft, FiChevronRight, FiSettings } from 'react-icons/fi';
import type { NavGroup, NavLocation } from './navConfig';
import { useLeadOverviewCounts } from './leadOverviewStore';
import './subnav.scss';

interface SubNavProps {
    /** 当前账号可见的该分组（已按权限 / CRM 开关裁剪过 Tab） */
    group: NavGroup;
    location: NavLocation;
}

/** Reveal a tab inside this strip only; never move the page vertically. */
export function revealSubNavLink(track: HTMLElement, link: HTMLElement) {
    const viewport = track.getBoundingClientRect();
    const item = link.getBoundingClientRect();
    const gutter = track.scrollWidth > track.clientWidth + 1 ? 44 : 0;
    const left = viewport.left + gutter;
    const right = viewport.left + track.clientWidth - gutter;
    if (item.width > right - left || item.left < left) track.scrollLeft += item.left - left;
    else if (item.right > right) track.scrollLeft += item.right - right;
}

export function subNavScrollEdges(track: HTMLElement) {
    return {
        left: track.scrollLeft > 1,
        right: track.scrollWidth - track.clientWidth - track.scrollLeft > 1,
    };
}

/**
 * 分组内的子导航条，位于顶栏与内容区之间（内容区本身不变，页面样式不受影响）。
 * - 分组页：显示同组页面的 Tab（可带数量角标）；有二级页时右侧给出入口（如 订单与支付 → 支付与会员配置）；
 * - 二级页：显示二级页之间的 Tab 和"返回分组"；
 * - 只有一个页面且没有二级页入口时不渲染；二级页即使只有一个页面也要保留「返回」链接。
 */
const SubNav: React.FC<SubNavProps> = ({ group, location }) => {
    const counts = useLeadOverviewCounts();
    const inSecondary = location.inSecondary && !!group.secondary;
    const tabs = inSecondary ? group.secondary!.pages : group.pages;
    // 详情页（如用户 360）不在 Tab 里：高亮其所属分组的落地 Tab
    const activePath = group.pages.some((p) => p.path === location.page.path) || inSecondary ? location.page.path : group.pages[0].path;
    const showEntry = !inSecondary && !!group.secondary;

    const trackRef = useRef<HTMLDivElement>(null);
    const trackId = useId();
    const [edges, setEdges] = useState({ left: false, right: false });
    const tabPaths = tabs.map(page => page.path).join('|');
    const updateEdges = () => {
        const track = trackRef.current;
        if (!track) return;
        const next = subNavScrollEdges(track);
        setEdges(current => current.left === next.left && current.right === next.right ? current : next);
    };

    useEffect(() => {
        const track = trackRef.current;
        if (!track) return;
        let frame = 0;
        const revealActive = () => {
            const active = track.querySelector<HTMLElement>('[aria-current="page"]');
            if (active) revealSubNavLink(track, active);
            updateEdges();
        };
        const resize = new ResizeObserver(() => {
            cancelAnimationFrame(frame);
            frame = requestAnimationFrame(revealActive);
        });
        resize.observe(track);
        Array.from(track.children).forEach(child => resize.observe(child));
        revealActive();
        return () => { resize.disconnect(); cancelAnimationFrame(frame); };
    }, [activePath, tabPaths, inSecondary]);

    const scroll = (direction: number) => {
        const track = trackRef.current;
        if (!track) return;
        track.scrollBy({
            left: direction * Math.max(120, track.clientWidth * 0.7),
            behavior: window.matchMedia('(prefers-reduced-motion: reduce)').matches ? 'auto' : 'smooth',
        });
    };

    if (!inSecondary && tabs.length <= 1 && !showEntry) return null;

    return (
        <nav className="layout-subnav" aria-label={`${group.label}子导航`}>
            <div
                className="subnav-scroll-track"
                id={trackId}
                ref={trackRef}
                onScroll={updateEdges}
                onFocusCapture={event => {
                    const link = (event.target as HTMLElement).closest('a');
                    if (trackRef.current && link) revealSubNavLink(trackRef.current, link);
                }}
            >
                {inSecondary && (
                    /* 二级页显示面包屑（分组 / 当前页），分组名可点回分组落地页，箭头保留"返回"直觉 */
                    <span className="subnav-breadcrumb">
                        <Link className="subnav-back" to={group.pages[0].path}>
                            <FiArrowLeft />
                            {group.label}
                        </Link>
                        <span className="subnav-crumb-sep" aria-hidden>/</span>
                        <span className="subnav-crumb-current">{location.page.label}</span>
                    </span>
                )}
                {tabs.length > 1 && tabs.map((page) => {
                    const count = page.badgeKey ? counts[page.badgeKey] ?? 0 : 0;
                    return (
                        <Link
                            key={page.path}
                            to={page.path}
                            className={`subnav-tab ${page.path === activePath ? 'active' : ''}`}
                            aria-current={page.path === activePath ? 'page' : undefined}
                        >
                            {page.label}
                            {count > 0 && <span className="subnav-badge" aria-label={`${count} 条待处理`}>{count > 99 ? '99+' : count}</span>}
                        </Link>
                    );
                })}
                <span className="subnav-spacer" />
                {showEntry && (
                    <Link className="subnav-entry" to={group.secondary!.pages[0].path}>
                        <FiSettings />
                        {group.secondary!.label}
                    </Link>
                )}
            </div>
            {(edges.left || edges.right) && <>
                <button className="subnav-scroll-button subnav-scroll-left" type="button" disabled={!edges.left} aria-label="向左滚动，查看更多导航" aria-controls={trackId} onClick={() => scroll(-1)}><FiChevronRight aria-hidden /></button>
                <button className="subnav-scroll-button subnav-scroll-right" type="button" disabled={!edges.right} aria-label="向右滚动，查看更多导航" aria-controls={trackId} onClick={() => scroll(1)}><FiChevronRight aria-hidden /></button>
            </>}
        </nav>
    );
};

export default SubNav;
