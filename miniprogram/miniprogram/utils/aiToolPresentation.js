"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
exports.shouldUseMinimalAIToolPresentation = shouldUseMinimalAIToolPresentation;
const MINIMAL_PRESENTATION_TOOL_CODES = new Set([
    'masterplan-coloring',
]);
function isBlank(value) {
    return String(value || '').trim() === '';
}
function shouldUseMinimalAIToolPresentation(tool) {
    if (!tool) {
        return false;
    }
    const toolCode = String(tool.code || '').trim();
    if (MINIMAL_PRESENTATION_TOOL_CODES.has(toolCode)) {
        return true;
    }
    return isBlank(tool.shortDescription)
        && isBlank(tool.detailDescription)
        && isBlank(tool.uploadHint)
        && (!tool.stylePresets || tool.stylePresets.length === 0);
}
