// @ts-nocheck
'use strict';

const DEFAULT_LIMITS = Object.freeze({
  image_max: 8, video_max: 3, audio_max: 3, total_max: 12,
  media_duration_min: 2, media_duration_max: 15,
  video_total_duration_max: 15, audio_total_duration_max: 15,
});

function result(code, message) { return { ok: false, code, message }; }
function normalizedReferences(references) {
  return (Array.isArray(references) ? references : []).map((item, index) => ({
    id: String(item.media_id || item.id || '').trim(),
    type: String(item.media_type || item.type || '').trim(),
    duration: Number(item.duration_seconds !== undefined ? item.duration_seconds : (item.duration || 0)),
    purpose: String(item.purpose || '').trim(),
    position: Number.isFinite(Number(item.position)) && Number(item.position) > 0 ? Number(item.position) : index + 1,
  }));
}

function validateReferences(references, configuredLimits) {
  const limits = Object.assign({}, DEFAULT_LIMITS, configuredLimits || {});
  const items = normalizedReferences(references);
  if (!items.length) return result('reference_required', '请至少选择一项参考素材');
  if (items.length > limits.total_max) return result('reference_total_limit', `参考素材最多 ${limits.total_max} 项`);
  const ids = new Set();
  const counts = { image: 0, video: 0, audio: 0 };
  const durations = { video: 0, audio: 0 };
  for (const item of items) {
    if (!item.id) return result('reference_id_required', '参考素材缺少有效标识，请重新选择');
    if (ids.has(item.id)) return result('reference_duplicate', '同一素材不能重复引用');
    ids.add(item.id);
    if (!(item.type in counts)) return result('reference_type', '参考素材类型不受支持');
    if (Array.from(item.purpose).length > 120) return result('reference_purpose_length', '单项素材用途不能超过120字');
    counts[item.type] += 1;
    if (item.type !== 'image') {
      if (!Number.isFinite(item.duration) || item.duration < limits.media_duration_min || item.duration > limits.media_duration_max) {
        return result('reference_duration', `音视频单条时长须为 ${limits.media_duration_min}–${limits.media_duration_max} 秒`);
      }
      durations[item.type] += item.duration;
    }
  }
  if (counts.image > limits.image_max || counts.video > limits.video_max || counts.audio > limits.audio_max) {
    return result('reference_type_limit', `图片最多 ${limits.image_max} 项，视频和音频各最多 ${limits.video_max}/${limits.audio_max} 项`);
  }
  if (durations.video > limits.video_total_duration_max || durations.audio > limits.audio_total_duration_max) {
    return result('reference_duration_total', '视频或音频累计时长超过当前能力限制');
  }
  return { ok: true, value: items };
}

function providerVisibleReferencePrompt(prompt, references) {
  const counts = { image: 0, video: 0, audio: 0 };
  const bindings = normalizedReferences(references).flatMap((item, index) => {
    counts[item.type] = Number(counts[item.type] || 0) + 1;
    if (!item.purpose) return [];
    const label = ({ image: '图片', video: '视频', audio: '音频' }[item.type] || '素材') + counts[item.type];
    return [`${label}（引用编号${index + 1}）：${item.purpose}`];
  });
  return bindings.length ? `${String(prompt || '').trim()}\n\n参考素材用途：\n${bindings.join('\n')}` : String(prompt || '').trim();
}

function validateCreation(input, capabilities) {
  const value = input || {};
  const mode = String(value.mode || '');
  if (!['text', 'first_frame', 'first_last_frame', 'reference'].includes(mode)) return result('mode', '请选择生成方式');
  if (mode !== 'reference' && !String(value.prompt || '').trim()) return result('prompt_required', '请输入画面描述');
  if ((mode === 'first_frame' || mode === 'first_last_frame') && !String(value.first_frame_media_id || value.first_frame_id || '').trim()) return result('first_frame_required', '请选择首帧图片');
  if (mode === 'first_last_frame' && !String(value.last_frame_media_id || value.last_frame_id || '').trim()) return result('last_frame_required', '请选择尾帧图片');
  if (mode === 'reference') {
    if (!String(value.reference_prompt || '').trim()) return result('reference_prompt_required', '请输入参考素材的使用说明');
    const checked = validateReferences(value.references, capabilities && capabilities.reference_limits);
    if (!checked.ok) return checked;
    if (Array.from(providerVisibleReferencePrompt(value.reference_prompt, value.references)).length > 7000) {
      return result('reference_prompt_total_length', '画面描述与素材用途合计不能超过7000字');
    }
  }
  const parameters = value.parameters || value;
  if (!String(parameters.ratio || '').trim() || !String(parameters.resolution || '').trim()) return result('parameters', '请选择画幅和清晰度');
  const duration = Number(parameters.duration);
  const quantity = Number(parameters.count !== undefined ? parameters.count : parameters.quantity);
  if (!Number.isSafeInteger(duration) || duration <= 0 || !Number.isSafeInteger(quantity) || quantity <= 0) return result('parameters', '请选择时长和生成数量');
  return { ok: true };
}

function stable(value) {
  if (Array.isArray(value)) return value.map(stable);
  if (value && typeof value === 'object') return Object.keys(value).sort().reduce((out, key) => { out[key] = stable(value[key]); return out; }, {});
  return value;
}

function billableFingerprint(draft) {
  const source = draft || {};
  return JSON.stringify(stable({
    mode: source.mode, prompt: source.prompt, reference_prompt: source.reference_prompt,
    first_frame_media_id: source.first_frame_media_id || source.first_frame_id,
    last_frame_media_id: source.last_frame_media_id || source.last_frame_id,
    references: normalizedReferences(source.references), parameters: source.parameters || {
      ratio: source.ratio, resolution: source.resolution, duration: source.duration, quantity: source.quantity,
    },
  }));
}

function isQuoteUsable(quote, draft, now) {
  if (!quote || !quote.quote_id || !quote.expires_at) return false;
  const expected = quote.input_fingerprint || quote.client_fingerprint;
  return expected === billableFingerprint(draft) && Date.parse(quote.expires_at) > Number(now || Date.now());
}

function buildBatchRequest(draft, quote, createKey) {
  if (!draft || !draft.draft_id || !Number(draft.revision)) return result('draft_required', '请先保存当前草稿');
  if (!quote || !quote.quote_id) return result('quote_required', '请先获取最新报价');
  const key = typeof createKey === 'function' ? String(createKey() || '') : '';
  if (!key) return result('idempotency_required', '无法创建安全提交标识，请重试');
  return { ok: true, value: {
    draft_id: draft.draft_id, expected_revision: Number(draft.revision), quote_id: quote.quote_id,
    input_fingerprint: quote.input_fingerprint || quote.client_fingerprint || '', idempotency_key: key,
  } };
}

function ratioOptionsForMode(mode, serverRatios) {
  const ratios = Array.isArray(serverRatios) ? serverRatios : [];
  if (mode === 'first_frame' || mode === 'first_last_frame') return ['adaptive'];
  if (mode === 'text') return ratios.filter(item => item !== 'adaptive');
  return ratios;
}

module.exports = { DEFAULT_LIMITS, normalizedReferences, validateReferences, providerVisibleReferencePrompt, validateCreation, billableFingerprint, isQuoteUsable, buildBatchRequest, ratioOptionsForMode };
