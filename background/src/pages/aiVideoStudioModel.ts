import type { AIVideoStudioConfig, AIVideoStudioMode, PolishReconciliationAction, PolishReconciliationInput, StudioReconciliationAction, StudioReconciliationInput } from '../api/aiVideoStudio';

const studioModes: AIVideoStudioMode[] = ['text', 'first_frame', 'first_last_frame', 'reference'];

export function summarizeAIVideoStudioReadiness(config: Partial<AIVideoStudioConfig>) {
    const modeReadiness = config.mode_readiness || {};
    const modes = studioModes.map((code) => {
        const state = modeReadiness[code];
        return {
            code,
            enabled: state?.enabled === true,
            reason: state?.disabled_reason || (state ? '' : '服务尚未返回该模式的就绪状态'),
        };
    });
    if (!config.enabled) return { ready: false, status: '已停用', reason: '运营主开关已关闭', modes };
    const ready = config.new_tasks_ready === true && modes.some((mode) => mode.enabled);
    return {
        ready,
        status: ready ? '允许新任务' : '等待服务就绪',
        reason: ready ? '至少一种生成模式可用' : (config.disabled_reason || '服务尚未返回完整就绪状态'),
        modes,
    };
}

export function buildStudioReconciliation(action: StudioReconciliationAction, externalID: string, evidence: string): StudioReconciliationInput {
    if (!['query', 'attach_external_id', 'refund_confirmed_rejected'].includes(action)) throw new Error('请选择有效的核对动作');
    const reason = evidence.trim();
    if (action !== 'query' && (reason.length < 10 || reason.length > 500)) throw new Error('请填写10至500字的供应商核对依据');
    if (action === 'attach_external_id') {
        const id = externalID.trim();
        if (!id || id.length > 191) throw new Error('请填写有效的供应商任务标识');
        return { action, external_id: id, evidence: reason };
    }
    return { action, evidence: reason };
}

export function buildPolishReconciliation(action: PolishReconciliationAction, evidence: string, result: string): PolishReconciliationInput {
    if (action !== 'confirmed_rejected' && action !== 'confirmed_result') throw new Error('请选择有效的核对动作');
    const reason = evidence.trim();
    if (Array.from(reason).length < 10 || Array.from(reason).length > 500) throw new Error('请填写10至500字的供应商核对依据');
    if (action === 'confirmed_result') {
        const text = result.trim();
        if (!text || Array.from(text).length > 7000) throw new Error('请填写1至7000字的已核实润色结果');
        return { action, evidence: reason, result: text };
    }
    return { action, evidence: reason };
}

export function validateAIVideoStudioConfig(config: AIVideoStudioConfig): string[] {
    const errors: string[] = [];
    if (config.provider_code !== 'metaso_h3') errors.push('当前版本只允许使用已冻结的秘塔 H3 适配。');
    if (config.enabled && !config.provider_configured) errors.push('供应商未配置，主开关不能开放。');
    if (config.enabled && !Object.values(config.modes).some(Boolean)) errors.push('至少开放一种生成模式。');
    if (config.prices.filter((price) => price.enabled).some((price) => !Number.isSafeInteger(price.unit_stones) || Number(price.unit_stones) <= 0)) {
        errors.push('已启用价格项必须配置大于 0 的整数灵石价格。');
    }
    if (config.prices.some((price) => price.unit_stones !== null && (!Number.isSafeInteger(price.unit_stones) || Number(price.unit_stones) < 0 || Number(price.unit_stones) > 1_000_000_000))) {
        errors.push('灵石单价必须是 0–1000000000 的整数；未配置请留空。');
    }
    if (config.prices.some((price) => !['per_request', 'per_video', 'per_second'].includes(price.billing_unit))) errors.push('价格目录包含不支持的计费单位。');
    const polishPrice = config.prices.find((price) => price.code === 'ai_video_studio_prompt_optimize');
    if (polishPrice && polishPrice.billing_unit !== 'per_request') errors.push('提示词润色必须按次计费。');
    const polishConfig = config.prompt_optimize || { enabled: false, model: '', timeout_seconds: 45 };
    if (polishConfig.enabled && !polishConfig.model.trim()) errors.push('开启提示词润色前请填写模型名称。');
    if (polishConfig.enabled && (!polishPrice?.enabled || !Number.isSafeInteger(polishPrice.unit_stones) || Number(polishPrice.unit_stones) <= 0)) {
        errors.push('开启提示词润色前请启用按次价格并填写正整数单价。');
    }
    if (!Number.isSafeInteger(polishConfig.timeout_seconds) || polishConfig.timeout_seconds < 1 || polishConfig.timeout_seconds > 120) {
        errors.push('提示词润色超时必须为 1–120 秒。');
    }
    if (config.enabled && !config.prices.some((price) => price.enabled && price.code !== 'ai_video_studio_prompt_optimize')) {
        errors.push('至少启用一个已定价的视频生成档位。');
    }
    if (config.limits.max_concurrency < 1 || config.limits.max_concurrency > 20) errors.push('并发任务上限必须为 1–20。');
    if (config.limits.timeout_seconds < 1 || config.limits.timeout_seconds > 3600) errors.push('单任务超时必须为 1–3600 秒。');
    if (config.limits.max_quantity < 1 || config.limits.max_quantity > 10) errors.push('单批数量上限必须为 1–10。');
    if (config.limits.max_duration_768p < 4 || config.limits.max_duration_768p > 30) errors.push('768P 时长上限必须为 4–30 秒。');
    if (config.limits.max_duration_2k > 15) errors.push('2K 时长上限不能超过 15 秒。');
    if (config.limits.max_duration_2k < 4) errors.push('2K 时长上限不能少于 4 秒。');
    if (config.limits.max_reference_images > 8 || config.limits.max_reference_videos > 3 || config.limits.max_reference_audios > 3) errors.push('图片、视频、音频上限分别不能超过 8、3、3。');
    if (config.limits.max_reference_total > 12) errors.push('混合参考素材总数不能超过 12。');
    const referenceLimits = [config.limits.max_reference_images, config.limits.max_reference_videos, config.limits.max_reference_audios, config.limits.max_reference_total];
    if (referenceLimits.some((value) => value < 0)) errors.push('参考图片、视频、音频和总数上限不能小于 0。');
    if (config.limits.max_reference_total < Math.max(config.limits.max_reference_images, config.limits.max_reference_videos, config.limits.max_reference_audios)) {
        errors.push('参考素材总上限不能小于任一单类上限。');
    }
    const longPrice = config.prices.find((price) => price.code === 'ai_video_studio_768p_long');
    if (config.limits.max_duration_768p > 15 && (!longPrice?.enabled || Number(longPrice.unit_stones) <= 0)) errors.push('开放 16–30 秒前必须启用 768P 长档价格。');
    return errors;
}
