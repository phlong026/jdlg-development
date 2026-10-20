import React from 'react';
import { funnelLayers, type FunnelStep } from './salesFunnelMath';
import './salesFunnel.scss';

interface Props {
    steps: FunnelStep[];
    /** 屏幕阅读器用的整体说明 */
    label: string;
}

const SHADES = ['#8b5e3c', '#a2754f', '#b98c66', '#cfa57f', '#e0bf9f'];

/**
 * 梯形漏斗：每层 [名称 | 梯形 | 人数 + 相对上一层转化率]。
 * 梯形宽度最窄 18%（见 funnelLayers），文字放在梯形两侧，窄层也读得清。
 * 线索页、总览共用这一个组件。
 */
const SalesFunnel: React.FC<Props> = ({ steps, label }) => {
    const layers = funnelLayers(steps);
    return (
        <ol className="sales-funnel" aria-label={label}>
            {layers.map((layer, index) => {
                const next = layers[index + 1];
                // 本层下边收窄到下一层的宽度，末层收窄一点点，整体呈漏斗
                const bottom = next ? next.width / layer.width : 0.86;
                const inset = ((1 - bottom) / 2) * 100;
                return (
                    <li key={layer.key} className="sales-funnel-row">
                        <span className="sales-funnel-label">{layer.label}</span>
                        <span className="sales-funnel-track">
                            <i
                                style={{
                                    width: `${layer.width}%`,
                                    background: SHADES[Math.min(index, SHADES.length - 1)],
                                    clipPath: `polygon(0 0, 100% 0, ${100 - inset}% 100%, ${inset}% 100%)`,
                                }}
                            />
                        </span>
                        <span className="sales-funnel-count">
                            <b>{layer.count}</b>
                            <small>{layer.rate === null ? '' : `${layer.rate}%`}</small>
                        </span>
                    </li>
                );
            })}
        </ol>
    );
};

export default SalesFunnel;
