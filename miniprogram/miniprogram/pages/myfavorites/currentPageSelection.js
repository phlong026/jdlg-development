"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
exports.uniqueStrings = uniqueStrings;
exports.getCurrentSelectableIds = getCurrentSelectableIds;
exports.buildSelectedMap = buildSelectedMap;
exports.isEveryCurrentSelected = isEveryCurrentSelected;
exports.toggleCurrentSelection = toggleCurrentSelection;
function uniqueStrings(values) {
    return values.reduce((acc, value) => {
        const normalized = String(value || '').trim();
        if (normalized && acc.indexOf(normalized) === -1) {
            acc.push(normalized);
        }
        return acc;
    }, []);
}
function getCurrentSelectableIds(items, getId, visibleIds = [], fallbackCount = 9) {
    const orderedIds = uniqueStrings((Array.isArray(items) ? items : []).map((item) => getId(item)));
    const orderedIdSet = orderedIds.reduce((acc, id) => {
        acc[id] = true;
        return acc;
    }, {});
    const currentVisibleIds = uniqueStrings(visibleIds).filter((id) => orderedIdSet[id]);
    if (currentVisibleIds.length > 0) {
        return orderedIds.filter((id) => currentVisibleIds.indexOf(id) !== -1);
    }
    return orderedIds.slice(0, Math.max(0, Number(fallbackCount || 0)));
}
function buildSelectedMap(selectedIds) {
    return uniqueStrings(selectedIds).reduce((acc, id) => {
        acc[id] = true;
        return acc;
    }, {});
}
function isEveryCurrentSelected(selectedIds, currentIds) {
    const normalizedCurrentIds = uniqueStrings(currentIds);
    if (!normalizedCurrentIds.length) {
        return false;
    }
    const selectedMap = buildSelectedMap(selectedIds);
    return normalizedCurrentIds.every((id) => selectedMap[id]);
}
function toggleCurrentSelection(selectedIds, currentIds) {
    const normalizedSelectedIds = uniqueStrings(selectedIds);
    const normalizedCurrentIds = uniqueStrings(currentIds);
    const currentMap = buildSelectedMap(normalizedCurrentIds);
    if (isEveryCurrentSelected(normalizedSelectedIds, normalizedCurrentIds)) {
        return normalizedSelectedIds.filter((id) => !currentMap[id]);
    }
    return uniqueStrings([...normalizedSelectedIds, ...normalizedCurrentIds]);
}
