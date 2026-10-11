export function uniqueStrings(values: string[]): string[] {
  return values.reduce((acc, value) => {
    const normalized = String(value || '').trim();
    if (normalized && acc.indexOf(normalized) === -1) {
      acc.push(normalized);
    }
    return acc;
  }, [] as string[]);
}

export function getCurrentSelectableIds<T>(
  items: T[],
  getId: (item: T) => string,
  visibleIds: string[] = [],
  fallbackCount = 9,
): string[] {
  const orderedIds = uniqueStrings((Array.isArray(items) ? items : []).map((item) => getId(item)));
  const orderedIdSet = orderedIds.reduce((acc, id) => {
    acc[id] = true;
    return acc;
  }, {} as Record<string, boolean>);
  const currentVisibleIds = uniqueStrings(visibleIds).filter((id) => orderedIdSet[id]);

  if (currentVisibleIds.length > 0) {
    return orderedIds.filter((id) => currentVisibleIds.indexOf(id) !== -1);
  }

  return orderedIds.slice(0, Math.max(0, Number(fallbackCount || 0)));
}

export function buildSelectedMap(selectedIds: string[]): Record<string, boolean> {
  return uniqueStrings(selectedIds).reduce((acc, id) => {
    acc[id] = true;
    return acc;
  }, {} as Record<string, boolean>);
}

export function isEveryCurrentSelected(selectedIds: string[], currentIds: string[]): boolean {
  const normalizedCurrentIds = uniqueStrings(currentIds);
  if (!normalizedCurrentIds.length) {
    return false;
  }
  const selectedMap = buildSelectedMap(selectedIds);
  return normalizedCurrentIds.every((id) => selectedMap[id]);
}

export function toggleCurrentSelection(selectedIds: string[], currentIds: string[]): string[] {
  const normalizedSelectedIds = uniqueStrings(selectedIds);
  const normalizedCurrentIds = uniqueStrings(currentIds);
  const currentMap = buildSelectedMap(normalizedCurrentIds);

  if (isEveryCurrentSelected(normalizedSelectedIds, normalizedCurrentIds)) {
    return normalizedSelectedIds.filter((id) => !currentMap[id]);
  }

  return uniqueStrings([...normalizedSelectedIds, ...normalizedCurrentIds]);
}
