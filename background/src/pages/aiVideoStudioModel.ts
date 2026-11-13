import type { AIVideoStudioConfig } from '../api/aiVideoStudio';

export function validateAIVideoStudioConfig(config: AIVideoStudioConfig): string[] {
    const errors: string[] = [];
    if (config.provider_code !== 'metaso_h3') errors.push('当前版本只允许使用已冻结的秘塔 H3 适配。');
    if (config.enabled && !config.provider_configured) errors.push('供应商未配置，主开关不能开放。');
    if (config.enabled && !Object.values(config.modes).some(Boolean)) errors.push('至少开放一种生成模式。');
    if (config.prices.filter((price) => price.enabled).some((price) => !Number.isSafeInteger(price.unit_stones) || Number(price.unit_stones) <= 0)) {
        errors.push('已启用价格项必须配置大于 0 的整数灵石价格。');
    }
    if (config.prices.some((price) => !['per_request', 'per_video', 'per_second'].includes(price.billing_unit))) errors.push('价格目录包含不支持的计费单位。');
    if (config.enabled && !config.prices.some((price) => price.enabled && price.code !== 'ai_video_studio_prompt_optimize')) {
        errors.push('至少启用一个已定价的视频生成档位。');
    }
    if (config.limits.max_concurrency < 1 || config.limits.timeout_seconds < 1 || config.limits.max_quantity < 1) errors.push('并发、超时和单批数量上限必须大于 0。');
    if (config.limits.max_duration_768p < 4 || config.limits.max_duration_768p > 30) errors.push('768P 时长上限必须为 4–30 秒。');
    if (config.limits.max_duration_2k > 15) errors.push('2K 时长上限不能超过 15 秒。');
    if (config.limits.max_duration_2k < 4) errors.push('2K 时长上限不能少于 4 秒。');
    if (config.limits.max_reference_images > 8 || config.limits.max_reference_videos > 3 || config.limits.max_reference_audios > 3) errors.push('图片、视频、音频上限分别不能超过 8、3、3。');
    if (config.limits.max_reference_total > 12) errors.push('混合参考素材总数不能超过 12。');
    const longPrice = config.prices.find((price) => price.code === 'ai_video_studio_768p_long');
    if (config.limits.max_duration_768p > 15 && (!longPrice?.enabled || Number(longPrice.unit_stones) <= 0)) errors.push('开放 16–30 秒前必须启用 768P 长档价格。');
    return errors;
}
