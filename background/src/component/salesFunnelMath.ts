export interface FunnelStep { key: string; label: string; count: number }
export interface FunnelLayer extends FunnelStep {
    /** 梯形上边宽度占比（%），最窄不小于 MIN_WIDTH，且不会比上一层更宽 */
    width: number;
    /** 相对上一层的转化率（%，四舍五入）；第一层为 null */
    rate: number | null;
}

export const MIN_WIDTH = 18;

/** 把各层人数换算成梯形宽度与相邻层转化率。口径不一致（下层比上层多）时宽度不放大。 */
export function funnelLayers(steps: FunnelStep[]): FunnelLayer[] {
    const top = steps[0]?.count ?? 0;
    let previous = 100;
    return steps.map((step, index) => {
        const raw = top > 0 ? (step.count / top) * 100 : index === 0 ? 100 : 0;
        const width = index === 0 ? 100 : Math.min(previous, Math.max(MIN_WIDTH, raw));
        previous = width;
        const before = steps[index - 1]?.count ?? 0;
        const rate = index === 0 ? null : before > 0 ? Math.round((step.count / before) * 100) : 0;
        return { ...step, width, rate };
    });
}
