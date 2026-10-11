"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
exports.AI_TOOL_LIST = exports.AI_TOOL_CATEGORIES = void 0;
exports.getCategoryLabel = getCategoryLabel;
exports.getAIToolList = getAIToolList;
exports.getAIToolById = getAIToolById;
exports.filterAITools = filterAITools;
exports.splitToolsIntoColumns = splitToolsIntoColumns;
exports.AI_TOOL_CATEGORIES = [
    { label: '建筑', value: 'architecture' },
    { label: '室内', value: 'interior' },
    { label: '景观', value: 'landscape' },
    { label: '规划', value: 'planning' },
];
const CATEGORY_REFERENCE_MAP = {
    architecture: [
        { id: 'arch-ref-1', name: '新中式外观参考', description: '偏温润材质与层次立面表达', promptSuffix: '新中式建筑表达，层次清晰，材质真实，暖色光影' },
        { id: 'arch-ref-2', name: '现代建筑参考', description: '偏简洁比例与干净立面气质', promptSuffix: '现代建筑气质，比例利落，立面干净，空间秩序明确' },
    ],
    interior: [
        { id: 'interior-ref-1', name: '奶油室内参考', description: '偏柔和材质与温暖氛围', promptSuffix: '室内空间柔和奶油风，材质细腻，氛围舒适' },
        { id: 'interior-ref-2', name: '现代简约参考', description: '偏干净收口与清爽布局', promptSuffix: '现代简约室内，动线清晰，材质统一，光影自然' },
    ],
    landscape: [
        { id: 'landscape-ref-1', name: '庭院景观参考', description: '偏植物层次与步道关系', promptSuffix: '庭院景观表达，植物层次丰富，铺装清晰，空间宜人' },
        { id: 'landscape-ref-2', name: '夜景氛围参考', description: '偏灯光与景观层次氛围', promptSuffix: '夜景景观氛围，灯光柔和，层次分明，空间具有引导感' },
    ],
    planning: [
        { id: 'planning-ref-1', name: '总平分析参考', description: '偏总图结构与逻辑表达', promptSuffix: '总平逻辑清晰，分析结构明确，图面整洁专业' },
        { id: 'planning-ref-2', name: '规划彩平参考', description: '偏色彩分区与功能识别', promptSuffix: '规划彩平表达，分区明确，色彩统一，专业清晰' },
    ],
};
const CATEGORY_STYLE_MAP = {
    architecture: [
        { id: 'arch-style-1', name: '专业方案', promptSuffix: '专业方案表现，适合汇报展示' },
        { id: 'arch-style-2', name: '写实效果', promptSuffix: '真实材质与自然光影，贴近落地效果' },
        { id: 'arch-style-3', name: '竞赛表达', promptSuffix: '视觉冲击强，概念表达鲜明' },
    ],
    interior: [
        { id: 'interior-style-1', name: '温暖写实', promptSuffix: '空间温暖自然，材质柔和真实' },
        { id: 'interior-style-2', name: '极简清爽', promptSuffix: '空间克制简洁，构图清晰干净' },
    ],
    landscape: [
        { id: 'landscape-style-1', name: '日景表现', promptSuffix: '日景明亮自然，植物层次清楚' },
        { id: 'landscape-style-2', name: '夜景氛围', promptSuffix: '夜景灯光氛围明显，空间更具故事感' },
    ],
    planning: [
        { id: 'planning-style-1', name: '分析表达', promptSuffix: '分析逻辑突出，结构清楚，利于汇报' },
        { id: 'planning-style-2', name: '彩平汇报', promptSuffix: '彩平清晰专业，配色统一，版面利落' },
    ],
};
function buildTool(id, code, name, category, shortDescription, detailDescription, common = false) {
    return {
        id,
        code,
        name,
        category,
        shortDescription,
        detailDescription,
        common,
        promptPlaceholder: `补充一下你希望 ${name} 重点强化的画面要求`,
        defaultPrompt: `${name}，请保留原图核心结构与空间逻辑，提升图面表达质量与专业感。`,
        uploadHint: '请先上传 1 张原图，再选择 1 张预设参考图或上传 1 张自定义参考图。',
        tags: [getCategoryLabel(category), ...(common ? ['常用'] : [])],
        presetReferences: CATEGORY_REFERENCE_MAP[category].map((item) => ({ ...item })),
        stylePresets: CATEGORY_STYLE_MAP[category].map((item) => ({ ...item })),
    };
}
exports.AI_TOOL_LIST = [
    {
        ...buildTool('tool-new-minnan', 'new_minnan_tool', '新闽派工具', 'architecture', '用文字快速生成新闽派建筑风格画面', '当前支持文生图，后续预留图生图能力。', true),
        promptPlaceholder: '描述你想生成的新闽派建筑、场景、视角、材质和氛围',
        uploadHint: '当前支持文生图，无需上传图片；图生图能力将在接口参数确认后开放。',
        presetReferences: [],
        stylePresets: [],
    },
    buildTool('tool-architecture-1', 'masterplan-coloring', '总平填色', 'architecture', '把总平底图快速整理成专业汇报图面', '适合把总平线稿或草图整理为更清晰、更有层次的表达图。'),
    buildTool('tool-architecture-2', 'plan-coloring', '户型填色', 'architecture', '让平面图更像正式方案汇报效果', '适合平面功能区表达、色彩区分和快速汇报。', true),
    buildTool('tool-architecture-3', 'view-conversion', '视角转换', 'architecture', '让同一方案切换不同观察角度', '适合把已有视角切换成更适合展示的观察方式。', true),
    buildTool('tool-architecture-4', 'style-transfer', '风格迁移', 'architecture', '保留结构基础上快速切换气质', '适合探索同一方案在不同风格下的视觉可能性。', true),
    buildTool('tool-architecture-5', 'multifloor-plan-to-render', '多层平面图转外观效果图', 'architecture', '从平面逻辑快速补成外观方向感', '适合已有平面逻辑但外观表达还没有展开时使用。', true),
    buildTool('tool-architecture-6', 'partial-edit', '局部修改', 'architecture', '只改门头、材质、窗型等局部内容', '适合在不重做整图的情况下做定向细修。', true),
    buildTool('tool-architecture-7', 'night-scene', '一键夜景图', 'architecture', '快速切换为夜景氛围效果', '适合方案汇报时补充夜景版本。'),
    buildTool('tool-architecture-8', 'plan-to-view', '平面图转视图', 'architecture', '从平面关系补出更直观的视角图', '适合前期概念沟通与方向确认。'),
    buildTool('tool-architecture-9', 'perspective-to-elevation', '透视图转立面图', 'architecture', '从透视信息反推更规整的立面表达', '适合补充立面研究与报规前期表达。'),
    buildTool('tool-interior-1', 'interior-plan-coloring', '户型填色', 'interior', '让室内平面图更清晰、更利于讲解', '适合做室内功能分区、动线与氛围表达。', true),
    buildTool('tool-interior-2', 'interior-style-transfer', '风格迁移', 'interior', '快速切换室内风格方向', '适合在同一空间结构基础上尝试多种气质。', true),
    buildTool('tool-interior-3', 'plan-to-interior-render', '平面图转效果图', 'interior', '把平面思路快速转成空间感觉', '适合室内方案初期的方向讨论与体验预览。', true),
    buildTool('tool-interior-4', 'interior-night-scene', '夜景效果图', 'interior', '让室内方案快速拥有夜景氛围', '适合补充灯光方案表达和氛围展示。'),
    buildTool('tool-interior-5', 'furniture-replace', '物品替换', 'interior', '替换家具、软装和局部摆设', '适合快速试不同软装搭配与风格方案。', true),
    buildTool('tool-interior-6', 'render-to-elevation', '效果图转立面图', 'interior', '从空间透视图整理成立面表达', '适合补充施工前的表达整理。'),
    buildTool('tool-landscape-1', 'landscape-coloring', '彩平填色', 'landscape', '让景观彩平更统一、更专业', '适合植物、铺装、水景等信息的图面整理。', true),
    buildTool('tool-landscape-2', 'landscape-view-conversion', '景观视角转换', 'landscape', '切换不同步行或鸟瞰观察方式', '适合快速生成更适合展示的景观视角。', true),
    buildTool('tool-landscape-3', 'landscape-style-transfer', '景观风格迁移', 'landscape', '保留布局基础切换不同景观风格', '适合在方案稳定后做风格探索。'),
    buildTool('tool-landscape-4', 'landscape-plan-to-render', '景观平面图转效果图', 'landscape', '把平面关系快速变成空间场景', '适合庭院、公园、景观节点等前期表达。', true),
    buildTool('tool-landscape-5', 'landscape-partial-edit', '景观局部修改', 'landscape', '只调整节点、植物或铺装局部', '适合不推翻整体布局的精细化修改。', true),
    buildTool('tool-landscape-6', 'landscape-night-scene', '景观一键夜景图', 'landscape', '快速补充景观夜景版本', '适合提升汇报完整度与沉浸氛围。'),
    buildTool('tool-landscape-7', 'landscape-perspective-to-elevation', '景观透视图转立面图', 'landscape', '辅助从透视角度整理节点立面关系', '适合节点深化前的表达辅助。'),
    buildTool('tool-planning-1', 'planning-masterplan-coloring', '规划总平面填色', 'planning', '让规划总图更清楚地表达功能分区', '适合汇报版总平、鸟瞰总图和彩平整理。', true),
    buildTool('tool-planning-2', 'planning-analysis', '规划总图分析图', 'planning', '快速生成更易讲解的分析表达', '适合做结构、动线、功能和策略分析。', true),
    buildTool('tool-planning-3', 'planning-view-conversion', '规划视角转换', 'planning', '切换更适合展示规划策略的角度', '适合从总图延伸出更直观的观察方式。', true),
    buildTool('tool-planning-4', 'planning-plan-to-render', '规划平面图转效果图', 'planning', '从平面逻辑转成场景化效果表达', '适合总体规划、片区概念和前期汇报。', true),
    buildTool('tool-planning-5', 'planning-partial-edit', '规划局部修改', 'planning', '只微调局部节点和画面重点', '适合方向已经明确后的精修调整。', true),
];
function getCategoryLabel(category) {
    return exports.AI_TOOL_CATEGORIES.find((item) => item.value === category)?.label || 'AI工具';
}
function getAIToolList() {
    return exports.AI_TOOL_LIST.map((item) => ({
        ...item,
        tags: [...item.tags],
        presetReferences: item.presetReferences.map((reference) => ({ ...reference })),
        stylePresets: item.stylePresets.map((style) => ({ ...style })),
    }));
}
function getAIToolById(id) {
    return getAIToolList().find((item) => item.id === id);
}
function filterAITools(category, keyword) {
    const normalizedKeyword = String(keyword || '').trim().toLowerCase();
    return getAIToolList().filter((item) => {
        const matchCategory = item.category === category;
        if (!matchCategory) {
            return false;
        }
        if (!normalizedKeyword) {
            return true;
        }
        const text = `${item.name} ${item.shortDescription} ${item.detailDescription} ${item.tags.join(' ')}`.toLowerCase();
        return text.includes(normalizedKeyword);
    });
}
function splitToolsIntoColumns(tools) {
    return tools.reduce((result, item, index) => {
        if (index % 2 === 0) {
            result.left.push(item);
        }
        else {
            result.right.push(item);
        }
        return result;
    }, { left: [], right: [] });
}
