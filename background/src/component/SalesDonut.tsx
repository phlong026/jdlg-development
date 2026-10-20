import React from 'react';

export interface DonutSlice { key: string; label: string; count: number; color: string }

const RADIUS = 52;
const CIRCUMFERENCE = 2 * Math.PI * RADIUS;

/** 环形占比图 + 图例；total 为 0 时只画底环。 */
const SalesDonut: React.FC<{ slices: DonutSlice[]; centerLabel: string }> = ({ slices, centerLabel }) => {
    const total = slices.reduce((sum, slice) => sum + slice.count, 0);
    let offset = 0;
    const arcs = slices.filter((slice) => slice.count > 0 && total > 0).map((slice) => {
        const length = (slice.count / total) * CIRCUMFERENCE;
        const arc = { ...slice, dash: `${length} ${CIRCUMFERENCE - length}`, offset: -offset };
        offset += length;
        return arc;
    });
    return (
        <div className="sales-donut">
            <div className="sales-donut-chart">
                <svg viewBox="0 0 140 140" role="img" aria-label={`${centerLabel}共 ${total}`}>
                    <circle cx="70" cy="70" r={RADIUS} fill="none" stroke="#efe7dc" strokeWidth="16" />
                    {arcs.map((arc) => (
                        <circle key={arc.key} cx="70" cy="70" r={RADIUS} fill="none" stroke={arc.color} strokeWidth="16"
                            strokeDasharray={arc.dash} strokeDashoffset={arc.offset} transform="rotate(-90 70 70)" />
                    ))}
                </svg>
                <div className="sales-donut-center"><strong>{total}</strong><span>{centerLabel}</span></div>
            </div>
            <ul className="sales-donut-legend">
                {slices.map((slice) => (
                    <li key={slice.key}>
                        <i style={{ background: slice.color }} />
                        <span>{slice.label}</span>
                        <b>{slice.count}</b>
                        <small>{total > 0 ? `${Math.round((slice.count / total) * 100)}%` : '—'}</small>
                    </li>
                ))}
            </ul>
        </div>
    );
};

export default SalesDonut;
