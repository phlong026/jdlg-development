const fs = require('fs');
const path = require('path');
const assert = require('assert');

const repoRoot = path.resolve(__dirname, '..', '..');

function read(relativePath) {
  return fs.readFileSync(path.join(repoRoot, relativePath), 'utf8');
}

const pageSource = read('miniprogram/miniprogram/pages/newminnantool/newminnantool.ts');

assert.doesNotMatch(pageSource, /fetchAIToolByCode/, 'template-square new minnan tool must not request AI tool configuration by code');
assert.doesNotMatch(pageSource, /fetchAIToolDetail/, 'template-square new minnan tool must not depend on AI tool detail records');
assert.doesNotMatch(pageSource, /aiToolApi/, 'template-square new minnan tool must not import AI tool APIs');
assert.doesNotMatch(pageSource, /getNewMinnanToolFallback/, 'new minnan page must not use a local fallback tool config');
assert.doesNotMatch(pageSource, /getAIToolList/, 'new minnan page must not read local AI tool config');
assert.doesNotMatch(pageSource, /兜底配置/, 'new minnan page must not silently fall back to local config');
assert.match(pageSource, /new_minnan_tool_text_to_image/, 'new minnan page should use the dedicated generation scene');
assert.match(pageSource, /new_minnan_tool_image_to_image/, 'new minnan page should use the dedicated image-to-image scene');
assert.match(pageSource, /\/api\/v1\/miniprogram\/ai\/draw/, 'new minnan page should call the real draw API');
assert.doesNotMatch(pageSource, /图生图待开放|工作流接口和参数确认后开放/, 'new minnan image-to-image mode must be open');
assert.match(pageSource, /original_image_url/, 'image-to-image mode should submit the original image URL');
assert.match(pageSource, /reference_image_url/, 'image-to-image mode should submit the reference image URL');

const routeSource = read('service/route/ai.go');

assert.match(routeSource, /new_minnan_tool_text_to_image/, 'backend should recognize the dedicated new minnan generation scene');
assert.match(routeSource, /new_minnan_tool_image_to_image/, 'backend should recognize the dedicated new minnan image-to-image scene');
assert.doesNotMatch(routeSource, /scene == "new_minnan_tool_text_to_image"[\s\S]{0,80}enrichAIToolPayload/, 'new minnan generation must not require ai_tools records');
assert.doesNotMatch(routeSource, /scene == "new_minnan_tool_image_to_image"[\s\S]{0,80}enrichAIToolPayload/, 'new minnan image-to-image generation must not require ai_tools records');

console.log('new minnan template-square generation checks passed');
