"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
exports.emptyInput = emptyInput;
exports.validateStep = validateStep;
exports.previewSections = previewSections;
exports.shouldPoll = shouldPoll;
exports.canOpenFull = canOpenFull;
exports.statusText = statusText;
function emptyInput() {
    return {
        site: { region: '', width_m: 0, depth_m: 0, road_side: '待确认' },
        household: { family_type: '', residents: '' },
        space: { floors: 0, bedrooms: 0, first_floor_elder_room: false },
        preference: { style: '', priorities: [], goal: '' },
        budget: { wan: 0, scope: '' },
    };
}
function validateStep(input, step) {
    const positive = (value, max) => Number.isFinite(value) && value > 0 && value <= max;
    if (step === 0 && (input.site.region.trim().length > 32 || !input.site.region.trim() || !positive(input.site.width_m, 50) || input.site.width_m < 4 || !positive(input.site.depth_m, 80) || input.site.depth_m < 4))
        return '请选择地区，并填写 4–50 米的面宽、4–80 米的进深。';
    if (step === 1 && (!input.household.family_type || !input.household.residents.trim() || input.household.residents.trim().length > 80))
        return '请填写家庭类型，以及 80 字以内的常住成员信息。';
    if (step === 2 && (![1, 2, 3].includes(input.space.floors) || !Number.isInteger(input.space.bedrooms) || !positive(input.space.bedrooms, 12)))
        return '请选择计划层数与卧室数量。';
    if (step === 3 && (!input.preference.style || input.preference.priorities.length !== 3 || new Set(input.preference.priorities).size !== 3 || !input.preference.goal.trim() || input.preference.goal.trim().length > 80))
        return '请选择风格、最在意的三件事，并写下 80 字以内的建房目标。';
    if (step === 4 && (!positive(input.budget.wan, 1000) || input.budget.wan < 10 || !input.budget.scope))
        return '请填写 10–1000 万元预算，并说明这笔钱需要包含哪些项目。';
    return '';
}
function previewSections(snapshot) {
    const kinds = ['cover', 'evidence', 'profile', 'priorities'];
    if (!snapshot || !snapshot.facts || !Array.isArray(snapshot.pages) || snapshot.pages.length !== 4 ||
        snapshot.pages.some((page, index) => page.number !== index + 1 || page.kind !== kinds[index])) {
        throw new Error('报告内容尚未完整，请稍后重试。');
    }
    return snapshot.pages;
}
function shouldPoll(status) {
    return ['preview_queued', 'preview_generating', 'full_queued', 'full_generating'].includes(status);
}
function canOpenFull(report) {
    return report.paid === true && report.bound === true && report.status === 'ready';
}
function statusText(status) {
    const labels = {
        preview_queued: '正在整理您的建房信息', preview_generating: '正在梳理家的方向',
        preview_ready: '您的建房方向已整理好', full_queued: '已收到付款，报告正在排队',
        full_generating: '正在制作您的完整建房报告', ready: '您的完整报告已准备好',
        failed: '这次生成未完成，请重试',
    };
    return labels[status] || '正在查询报告状态';
}
