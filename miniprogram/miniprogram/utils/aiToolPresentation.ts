import { AIToolItem } from './aiTools'

const MINIMAL_PRESENTATION_TOOL_CODES = new Set([
  'masterplan-coloring',
])

function isBlank(value: any): boolean {
  return String(value || '').trim() === ''
}

export function shouldUseMinimalAIToolPresentation(tool: Pick<AIToolItem, 'code' | 'shortDescription' | 'detailDescription' | 'uploadHint' | 'stylePresets'> | null | undefined): boolean {
  if (!tool) {
    return false
  }

  const toolCode = String(tool.code || '').trim()
  if (MINIMAL_PRESENTATION_TOOL_CODES.has(toolCode)) {
    return true
  }

  return isBlank(tool.shortDescription)
    && isBlank(tool.detailDescription)
    && isBlank(tool.uploadHint)
    && (!tool.stylePresets || tool.stylePresets.length === 0)
}
