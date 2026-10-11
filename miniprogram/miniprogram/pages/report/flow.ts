export interface ReportInput {
    site: {
        region: string;
        width_m: number;
        depth_m: number;
        road_side: string;
    };
    household: {
        family_type: string;
        residents: string;
    };
    space: {
        floors: number;
        bedrooms: number;
        first_floor_elder_room: boolean;
    };
    preference: {
        style: string;
        priorities: string[];
        goal: string;
    };
    budget: {
        wan: number;
        scope: string;
    };
}
export function emptyInput(): ReportInput {
    return {
        site: { region: '', width_m: 0, depth_m: 0, road_side: '待确认' },
        household: { family_type: '', residents: '' },
        space: { floors: 0, bedrooms: 0, first_floor_elder_room: false },
        preference: { style: '', priorities: [], goal: '' },
        budget: { wan: 0, scope: '' },
    };
}
export function validateStep(input: ReportInput, step: number): string {
    const positive = (value: number, max: number) => Number.isFinite(value) && value > 0 && value <= max;
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
export function previewSections(snapshot: any): any[] {
    const protocol = snapshot?.meta?.protocol_version;
    const kinds = protocol === 'report-house-v1'
        ? ['house_profile', 'house_constraints', 'house_goals', 'house_confirmation']
        : protocol === 'report-decision-v5' ? ['decision', 'evidence', 'directions', 'confirmation']
            : ['cover', 'evidence', 'profile', 'priorities'];
    if (!snapshot || !snapshot.facts || !Array.isArray(snapshot.pages) || snapshot.pages.length !== 4 ||
        snapshot.pages.some((page: any, index: number) => page.number !== index + 1 || page.kind !== kinds[index])) {
        throw new Error('报告内容尚未完整，请稍后重试。');
    }
    return snapshot.pages;
}
export function shouldPoll(status: string): boolean {
    return ['preview_queued', 'preview_generating', 'full_queued', 'full_generating'].includes(status);
}
export function canOpenFull(report: any): boolean {
    return report.paid === true && (isModernReport(report) || report.bound === true) && report.status === 'ready';
}
export function isModernReport(report: any): boolean {
    return ['report-house-v1', 'report-decision-v5'].includes(report?.protocol_version);
}

export interface HouseDetails {
    permanent_count: number | null;
    returning_count: number | null;
    return_frequency: string;
    buildable_scope: string;
    buildable_constraints: string;
    flexible_use: string;
    elder_room: string;
    courtyard: string;
    parking: string;
    orientation: string;
    neighbor_notes: string;
    start_window: string;
    rooms: { id: string; name: string; count: number; preferred_floor: number; required: boolean }[];
    budget_scope: { item: string; status: string }[];
}
export function emptyHouseDetails(): HouseDetails {
    return {
        permanent_count: null, returning_count: null, return_frequency: 'unknown', buildable_scope: 'unknown',
        buildable_constraints: '', flexible_use: 'unknown', elder_room: 'unknown', courtyard: 'unknown', parking: 'unknown',
        orientation: '', neighbor_notes: '', start_window: '', rooms: [],
        budget_scope: ['主体建造', '外立面装修', '室内装修', '庭院与围墙', '设计及前期资料'].map(item => ({ item, status: 'unknown' })),
    };
}
export function validateHouseStep(input: ReportInput, house: HouseDetails, step: number): string {
    const finite = (value: number, min: number, max: number) => Number.isFinite(value) && value >= min && value <= max;
    if (step === 0) {
        if (!input.site.region.trim() || input.site.region.length > 40) return '请选择建房地区。';
        if ((input.site.width_m || input.site.depth_m) && (!finite(input.site.width_m, 1, 100) || !finite(input.site.depth_m, 1, 150))) return '尺寸可同时留空；填写时请提供完整面宽和进深。';
        if (house.buildable_scope === 'known' && !house.buildable_constraints.trim()) return '请说明已确认的可建边界或限制条件。';
    }
    if (step === 1) {
        const residents = input.household.residents.trim();
        if (residents && (residents.length < 2 || residents.length > 80)) return '居住需求至少填写 2 字，最多 80 字；也可暂时留空。';
        if (house.permanent_count === null || house.returning_count === null || !Number.isInteger(house.permanent_count) || !Number.isInteger(house.returning_count) || !finite(house.permanent_count, 0, 30) || !finite(house.returning_count, 0, 50)) return '请分别填写常住人数与返乡增加的人数，没有返乡成员时填 0。';
        if (house.return_frequency === 'none' && house.returning_count !== 0) return '选择没有返乡成员时，返乡人数应为 0。';
        if (!['none', 'monthly', 'quarterly', 'holidays', 'yearly', 'unknown'].includes(house.return_frequency)) return '请选择返乡频率。';
    }
    if (step === 2) {
        if (![1, 2, 3].includes(input.space.floors) || !Number.isInteger(input.space.bedrooms) || !finite(input.space.bedrooms, 1, 12)) return '请选择计划层数与卧室数量。';
        if (house.rooms.length > 12 || house.rooms.some(room => !room.name.trim() || room.name.length > 40 || !Number.isInteger(room.count) || !finite(room.count, 1, 12) || !Number.isInteger(room.preferred_floor) || !finite(room.preferred_floor, 0, input.space.floors))) return '请补全空间名称、数量和所在楼层；不限定楼层填 0。';
    }
    if (step === 3 && input.preference.goal.trim() && (input.preference.goal.trim().length < 2 || input.preference.goal.trim().length > 80)) return '建房目标至少填写 2 字，最多 80 字；也可暂时留空。';
    if (step === 3 && (!input.preference.style || input.preference.priorities.length < 1 || input.preference.priorities.length > 3 || new Set(input.preference.priorities).size !== input.preference.priorities.length)) return '请选择建筑风格，并按重要性选择一至三项需求。';
    if (step === 4 && input.budget.wan && !finite(input.budget.wan, 1, 10000)) return '预算可留空；填写时应为 1–10000 万元。';
    return '';
}

export function houseRequestInput(input: ReportInput, details: HouseDetails): any {
    for (let step = 0; step < 5; step++) {
        const error = validateHouseStep(input, details, step);
        if (error) throw new Error(error);
    }
    const house: any = {
        style: input.preference.style, road_side: input.site.road_side === '待确认' ? '' : input.site.road_side,
        orientation: details.orientation, neighbor_notes: details.neighbor_notes, start_window: details.start_window,
        courtyard: details.courtyard, parking: details.parking, rooms: details.rooms.map(room => ({ ...room })),
    };
    if (details.elder_room !== 'unknown') house.first_floor_elder_room = details.elder_room === 'yes';
    const data: any = {
        primary_goal: 'space', region: input.site.region, buildable_scope: details.buildable_scope,
        buildable_constraints: details.buildable_constraints, permanent_count: details.permanent_count,
        returning_count: details.returning_count, return_frequency: details.return_frequency,
        floors: input.space.floors, bedrooms: input.space.bedrooms, flexible_use: details.flexible_use,
        priorities: input.preference.priorities.slice(), budget_scope: details.budget_scope.map(item => ({ ...item })), house,
        family_perspectives: [
            { role: '家庭类型', request: input.household.family_type.trim() },
            { role: '居住安排', request: input.household.residents.trim() },
            { role: '建房目标', request: input.preference.goal.trim() },
        ].filter(item => item.request),
    };
    if (input.site.width_m && input.site.depth_m) data.approximate_dimensions = { width_m: input.site.width_m, depth_m: input.site.depth_m };
    if (input.budget.wan) data.budget_wan = input.budget.wan;
    return data;
}
export function statusText(status: string): string {
    const labels: {
        [key: string]: string;
    } = {
        preview_queued: '正在整理您的建房信息', preview_generating: '正在梳理家的方向',
        preview_ready: '您的建房方向已整理好', full_queued: '已收到付款，报告正在排队',
        full_generating: '正在制作您的完整建房报告', ready: '您的完整报告已准备好',
        failed: '这次生成未完成，请重试',
    };
    return labels[status] || '正在查询报告状态';
}

// Read the frozen server facts, not a potentially edited local draft.
export function houseConfirmationInputs(facts: { [key: string]: string }): { title: string; body: string }[] {
    const labels: { [key: string]: string } = {
        'intent.floors': '计划层数', 'intent.bedrooms': '卧室数量', 'house.style': '风格方向',
        'house.first_floor_elder_room': '一层长辈房', 'intent.flexible_use': '房间兼用',
        'house.courtyard': '庭院需求', 'house.parking': '停车需求', 'budget.wan': '预算参考 / 万元',
        'site.buildable_constraints': '可建边界与限制', 'house.neighbors': '相邻建筑条件', 'house.start_window': '计划启动时间',
    };
    const values: { [key: string]: string } = { yes: '需要 / 接受', no: '不需要 / 不接受', unknown: '待确认' };
    const rows = Object.keys(labels).filter(key => facts[key]).map(key => ({ title: labels[key], body: values[facts[key]] || facts[key] }));
    for (const key of Object.keys(facts).sort()) {
        if (key.startsWith('house.room.')) {
            let room: any;
            try { room = JSON.parse(facts[key]); } catch (_) { throw new Error('房间需求信息不完整，请刷新后再确认。'); }
            if (room && room.preferred_floor === undefined) room.preferred_floor = 0;
            if (!room || typeof room.name !== 'string' || !room.name || !Number.isInteger(room.count) || room.count < 1 || !Number.isInteger(room.preferred_floor) || room.preferred_floor < 0 || typeof room.required !== 'boolean') throw new Error('房间需求信息不完整，请刷新后再确认。');
            rows.push({ title: room.name, body: room.count + '间；' + (room.preferred_floor ? room.preferred_floor + '层' : '楼层不限') + '；' + (room.required ? '必须满足' : '可协商') });
        } else if (key.startsWith('family.perspective.')) rows.push({ title: '补充需求', body: facts[key] });
        else if (key.startsWith('budget.scope.')) {
            const [status, ...name] = facts[key].split(': ');
            const scopes: { [key: string]: string } = { included: '需要包含', excluded: '不包含', unknown: '待确认' };
            rows.push({ title: name.join(': ') || '预算范围', body: scopes[status] || '待确认' });
        }
    }
    return rows;
}
