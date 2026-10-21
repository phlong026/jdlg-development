export interface FunnelStep {
    key: string;
    label: string;
    count: number;
    /** 相对第一层（进入客服）的转化率（%）。未提供时按 count / 第一层计算。 */
    rate?: number | null;
}
export interface FunnelLayer extends FunnelStep {
    /** 梯形上边宽度占比（%），最窄不小于 MIN_WIDTH，且不会比上一层更宽 */
    width: number;
    /** 相对第一层（进入客服）的转化率（%，四舍五入），与线索页客服漏斗同一口径 */
    rate: number | null;
}

export const MIN_WIDTH = 18;

/**
 * 把各层人数换算成梯形宽度与相对第一层的转化率。
 * 漏斗只表达"进入客服的人有多少走到这一层"，所以各层都除以第一层；
 * 若后端已返回 conversion_rate，则直接用它，避免两个页面各算一套。
 * 口径不一致（下层比上层多）时宽度不放大。
 */
export function funnelLayers(steps: FunnelStep[]): FunnelLayer[] {
    const top = steps[0]?.count ?? 0;
    let previous = 100;
    return steps.map((step, index) => {
        const raw = top > 0 ? (step.count / top) * 100 : index === 0 ? 100 : 0;
        const width = index === 0 ? 100 : Math.min(previous, Math.max(MIN_WIDTH, raw));
        previous = width;
        const provided = typeof step.rate === 'number' && Number.isFinite(step.rate) ? step.rate : null;
        const rate = provided !== null ? Math.round(provided) : index === 0 ? 100 : top > 0 ? Math.round((step.count / top) * 100) : 0;
        return { ...step, width, rate };
    });
}
