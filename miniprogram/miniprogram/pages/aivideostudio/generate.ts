// @ts-nocheck
'use strict';

const api = require('../../utils/ai-video-studio-api');
const rules = require('../../utils/ai-video-studio');

function mediaPreview(item, type) {
  if (type === 'video') return item.poster_url || item.thumbnail_url || item.cover_url || '';
  return item.preview_url || item.thumbnail_url || item.cover_url || item.url || '';
}

function normalizedMedia(item, type, mediaID) {
  const source = item || {};
  const mediaType = source.media_type || source.kind || type || '';
  return Object.assign({}, source, {
    media_id: source.media_id || mediaID || '', media_type: mediaType,
    duration: Number(source.duration_seconds || source.duration || 0),
    preview_url: mediaPreview(source, mediaType),
    media_url: source.url || (mediaType === 'video' ? source.preview_url : '') || '',
  });
}

Page({
  data: {
    loading: true, loadError: '', error: '', notice: '', saving: false, quoting: false, submitting: false, submissionUncertain: false,
    capabilities: {}, limits: rules.DEFAULT_LIMITS, modeOptions: [], activeTab: 'creation', mode: 'text', selectedModeEnabled: false,
    name: '', prompt: '', reference_prompt: '', draft_id: '', revision: 0,
    first_frame_media_id: '', last_frame_media_id: '', firstFrame: {}, lastFrame: {}, references: [],
    ratio: '16:9', resolution: '768p', duration: 5, quantity: 1,
    ratioOptions: ['16:9', '9:16'], resolutionOptions: ['768p'], durationOptions: [5], quantityOptions: [1],
    ratioIndex: 0, resolutionIndex: 0, durationIndex: 0, quantityIndex: 0,
    ratioLabel: '16:9', resolutionLabel: '768P', durationLabel: '5秒', quantityLabel: '1条',
    quote: null, quoteReady: false, quoteStatus: 'incomplete', quoteHint: '完善内容后显示预计费用',
    playingAudioID: '', loadingAudioID: '', audioPreviewError: '',
  },

  onLoad(options) {
    this._sessionEpoch = (this._sessionEpoch || 0) + 1;
    this._unloaded = false;
    this.options = options || {};
    this._token = String(wx.getStorageSync('token') || '');
    this._editVersion = 0;
    this._tabStates = {};
    this.setData({ saving: false, quoting: false, submitting: false });
    try { if (api.pendingSubmission && api.pendingSubmission()) this.setData({ submissionUncertain: true }); }
    catch (_) { /* Account profile loading must not prevent draft editing. */ }
    this.load();
  },
  onUnload() {
    this._previewEpoch = (this._previewEpoch || 0) + 1;
    this.stopAudioPreview(true);
    this._unloaded = true;
    this._loadEpoch = (this._loadEpoch || 0) + 1;
    if (this._quoteTimer) clearTimeout(this._quoteTimer);
  },
  onHide() { this._previewEpoch = (this._previewEpoch || 0) + 1; this.stopAudioPreview(true); },
  onShow() { if (this._token && this._token !== String(wx.getStorageSync('token') || '')) { this._previewEpoch = (this._previewEpoch || 0) + 1; this.stopAudioPreview(true); } },
  current(epoch) { return !this._unloaded && this._token === String(wx.getStorageSync('token') || '') && (epoch === undefined || epoch === this._loadEpoch); },
  session() { return { epoch: this._sessionEpoch, token: this._token }; },
  ownsSession(session) { return !!session && session.epoch === this._sessionEpoch && session.token === this._token && this.current(); },

  async load() {
    const epoch = this._loadEpoch = (this._loadEpoch || 0) + 1;
    this.setData({ loading: true, loadError: '' });
    try {
      const capabilities = await api.capabilities();
      if (!this.current(epoch)) return;
      const modeOptions = ['text', 'first_frame', 'first_last_frame', 'reference'].map(code => {
        const server = (capabilities.modes || []).find(item => item.code === code) || {};
        return { code, enabled: !!server.enabled, disabled_reason: server.disabled_reason || '' };
      });
      const initialMode = this.options.kind === 'reference' ? 'reference' : 'text';
      const base = this.parameterState(capabilities, initialMode, (capabilities.resolutions || [])[0]);
      this.setData(Object.assign({
        loading: false, capabilities, limits: Object.assign({}, rules.DEFAULT_LIMITS, capabilities.reference_limits || {}),
        modeOptions, activeTab: initialMode === 'reference' ? 'reference' : 'creation', mode: initialMode,
        selectedModeEnabled: this.modeEnabled(initialMode, modeOptions),
      }, base));
      this.seedTabStates();
      if (this.options.draft_id) await this.restoreDraft(this.options.draft_id, epoch);
      else this.scheduleAutoQuote();
      if (!this.current(epoch)) return;
      if (this.data.submissionUncertain) await this.checkSubmission();
    } catch (error) {
      if (!this.current(epoch)) return;
      const base = this.parameterState({}, 'text', '768p');
      this.setData(Object.assign({ loading: false, loadError: error.message || '生成能力加载失败，仍可编辑并保存草稿' }, base));
      this.seedTabStates();
    }
  },

  modeEnabled(mode, options) { return !!((options || this.data.modeOptions).find(item => item.code === mode) || {}).enabled; },
  parameterState(capabilities, mode, resolution, previous) {
    const resolutions = capabilities.resolutions || ['768p'];
    const nextResolution = resolutions.includes(resolution) ? resolution : resolutions[0];
    const ratios = rules.ratioOptionsForMode(mode, capabilities.ratios || ['16:9', '9:16']);
    const durations = (capabilities.durations_by_resolution || {})[nextResolution] || [5];
    const quantities = Array.from({ length: Number(capabilities.quantity_max || 1) }, (_, index) => index + 1);
    const ratio = ratios.includes(previous && previous.ratio) ? previous.ratio : ratios[0] || '';
    const duration = durations.includes(Number(previous && previous.duration)) ? Number(previous.duration) : durations[0];
    const quantity = quantities.includes(Number(previous && previous.quantity)) ? Number(previous.quantity) : quantities[0];
    return {
      ratioOptions: ratios, resolutionOptions: resolutions, durationOptions: durations, quantityOptions: quantities,
      ratio, resolution: nextResolution, duration, quantity,
      ratioIndex: Math.max(0, ratios.indexOf(ratio)), resolutionIndex: Math.max(0, resolutions.indexOf(nextResolution)),
      durationIndex: Math.max(0, durations.indexOf(duration)), quantityIndex: Math.max(0, quantities.indexOf(quantity)),
      ratioLabel: ratio === 'adaptive' ? '原图比例' : ratio, resolutionLabel: String(nextResolution || '').toUpperCase(),
      durationLabel: `${duration}秒`, quantityLabel: `${quantity}条`,
    };
  },
  parameterPatch(mode, resolution) { return this.parameterState(this.data.capabilities, mode, resolution || this.data.resolution, this.data); },
  decorateReferences(references) {
    const counts = { image: 0, video: 0, audio: 0 };
    return (references || []).map((item, index) => {
      const type = item.media_type;
      counts[type] = Number(counts[type] || 0) + 1;
      const videoID = type === 'video' ? `ai-reference-video-${String(item.media_id || index).replace(/[^A-Za-z0-9_-]/g, '-')}` : '';
      return Object.assign({}, item, { position: index + 1, display_index: counts[type], video_dom_id: videoID });
    });
  },

  seedTabStates() {
    this._tabStates.creation = this.captureTab('creation');
    this._tabStates.reference = this.defaultTab('reference');
    if (this.data.activeTab === 'reference') {
      this._tabStates.reference = this.captureTab('reference');
      this._tabStates.creation = this.defaultTab('creation');
    }
  },
  defaultTab(tab) {
    const mode = tab === 'reference' ? 'reference' : 'text';
    return Object.assign({ mode, prompt: '', reference_prompt: '', first_frame_media_id: '', last_frame_media_id: '', firstFrame: {}, lastFrame: {}, references: [] }, this.parameterState(this.data.capabilities, mode, this.data.resolution, this.data));
  },
  captureTab(tab) {
    return {
      mode: tab === 'reference' ? 'reference' : this.derivedCreationMode(), prompt: this.data.prompt, reference_prompt: this.data.reference_prompt,
      first_frame_media_id: this.data.first_frame_media_id, last_frame_media_id: this.data.last_frame_media_id,
      firstFrame: this.data.firstFrame, lastFrame: this.data.lastFrame, references: this.data.references.slice(),
      ratio: this.data.ratio, resolution: this.data.resolution, duration: this.data.duration, quantity: this.data.quantity,
    };
  },
  switchTab(event) {
    const tab = event.currentTarget.dataset.tab;
    if (!['creation', 'reference'].includes(tab) || tab === this.data.activeTab) return;
    this.stopAudioPreview(true);
    this._tabStates[this.data.activeTab] = this.captureTab(this.data.activeTab);
    const saved = this._tabStates[tab] || this.defaultTab(tab);
    const mode = tab === 'reference' ? 'reference' : (saved.mode || 'text');
    const parameters = this.parameterState(this.data.capabilities, mode, saved.resolution, saved);
    this._editVersion += 1;
    this.setData(Object.assign({}, saved, parameters, { activeTab: tab, mode, selectedModeEnabled: this.modeEnabled(mode), quote: null, error: '', notice: '' }));
    this.scheduleAutoQuote();
  },

  async restoreDraft(id, epoch) {
    const editVersion = this._editVersion || 0;
    try {
      const draft = await api.getDraft(id);
      if (!this.current(epoch) || editVersion !== (this._editVersion || 0)) return;
      const activeTab = draft.mode === 'reference' ? 'reference' : 'creation';
      const references = this.decorateReferences((draft.references || []).map((item, index) => Object.assign(normalizedMedia(item, item.media_type, item.media_id), {
        position: Number(item.position || index + 1), label: item.name || `参考素材 ${item.position || index + 1}`,
      })));
      const parameters = this.parameterState(this.data.capabilities, draft.mode, draft.resolution, draft);
      this.setData(Object.assign({}, draft, parameters, {
        loading: false, activeTab, references,
        firstFrame: normalizedMedia(draft.first_frame, 'image', draft.first_frame_media_id),
        lastFrame: normalizedMedia(draft.last_frame, 'image', draft.last_frame_media_id),
        selectedModeEnabled: this.modeEnabled(draft.mode), quote: null,
      }));
      this.seedTabStates();
      await this.hydrateDraftMedia(epoch, editVersion);
      if (!this.current(epoch) || editVersion !== (this._editVersion || 0)) return;
      this.seedTabStates();
      this.scheduleAutoQuote();
    } catch (error) { if (this.current(epoch)) this.setData({ error: error.message || '草稿恢复失败，可重试' }); }
  },
  async hydrateDraftMedia(epoch, editVersion) {
    const requests = [];
    const add = (target, mediaID, item, type) => {
      if (!mediaID) return;
      const lacksPreview = type === 'image' && !item.preview_url;
      const lacksVideo = type === 'video' && (!item.media_url || !Number(item.duration));
      const lacksAudio = type === 'audio' && !Number(item.duration);
      if (!lacksPreview && !lacksVideo && !lacksAudio) return;
      requests.push({ target, mediaID, type, promise: typeof api.getMedia === 'function' ? api.getMedia(mediaID) : Promise.reject(new Error('素材详情接口未就绪')) });
    };
    add('first', this.data.first_frame_media_id, this.data.firstFrame || {}, 'image');
    add('last', this.data.last_frame_media_id, this.data.lastFrame || {}, 'image');
    this.data.references.forEach(item => add('reference', item.media_id, item, item.media_type));
    if (!requests.length) return;
    const settled = await Promise.allSettled(requests.map(item => item.promise));
    if (!this.current(epoch) || editVersion !== (this._editVersion || 0)) return;
    let firstFrame = this.data.firstFrame;
    let lastFrame = this.data.lastFrame;
    let references = this.data.references.slice();
    let failed = false;
    settled.forEach((result, index) => {
      const request = requests[index];
      if (result.status !== 'fulfilled') { failed = true; return; }
      const media = normalizedMedia(result.value, request.type, request.mediaID);
      if (request.target === 'first') firstFrame = media;
      else if (request.target === 'last') lastFrame = media;
      else references = references.map(item => item.media_id === request.mediaID ? Object.assign({}, media, { purpose: item.purpose, position: item.position, display_index: item.display_index }) : item);
    });
    this.setData({ firstFrame, lastFrame, references: this.decorateReferences(references) });
    if (failed) this.setData({ error: '部分素材详情加载失败，素材标识已保留；请重试进入草稿或重新选择素材' });
  },

  markEdited(patch) {
    this._editVersion = (this._editVersion || 0) + 1;
    this.setData(Object.assign({}, patch, { quote: null, quoteReady: false, notice: '', error: '' }));
    this.scheduleAutoQuote();
  },
  inputField(event) { this.markEdited({ [event.currentTarget.dataset.field]: event.detail.value }); },
  derivedCreationMode(firstID, lastID) {
    const first = firstID === undefined ? this.data.first_frame_media_id : firstID;
    const last = lastID === undefined ? this.data.last_frame_media_id : lastID;
    if (first && last) return 'first_last_frame';
    if (first) return 'first_frame';
    return 'text';
  },
  applyCreationMode(firstID, lastID, extra) {
    const mode = this.derivedCreationMode(firstID, lastID);
    this.markEdited(Object.assign({}, extra, this.parameterPatch(mode), { mode, selectedModeEnabled: this.modeEnabled(mode) }));
  },

  chooseMaterial(event) {
    const session = this.session();
    const target = event.currentTarget.dataset.target;
    if (target === 'last_frame' && !this.data.first_frame_media_id) { wx.showToast({ title: '请先选择首帧图片', icon: 'none' }); return; }
    const frame = target === 'first_frame' || target === 'last_frame';
    wx.navigateTo({ url: '/pages/aivideostudio/materials?select=1&target=' + target + '&media_type=' + (frame ? 'image' : '') + '&allowed_type=' + (frame ? 'image' : ''), events: { selected: item => { if (this.ownsSession(session)) this.applyMaterial(target, item); } } });
  },
  applyMaterial(target, source) {
    if (!this.current() || !source || !source.media_id) return;
    const type = source.media_type || source.kind;
    const item = normalizedMedia(source, type, source.media_id);
    if ((target === 'first_frame' || target === 'last_frame') && type !== 'image') {
      wx.showToast({ title: '首尾帧只能选择图片', icon: 'none' });
      return;
    }
    if (target === 'first_frame') { this.applyCreationMode(item.media_id, this.data.last_frame_media_id, { first_frame_media_id: item.media_id, firstFrame: item }); return; }
    if (target === 'last_frame') {
      if (!this.data.first_frame_media_id) { wx.showToast({ title: '请先选择首帧图片', icon: 'none' }); return; }
      this.applyCreationMode(this.data.first_frame_media_id, item.media_id, { last_frame_media_id: item.media_id, lastFrame: item }); return;
    }
    if (this.data.references.some(row => row.media_id === item.media_id)) { wx.showToast({ title: '该素材已添加', icon: 'none' }); return; }
    const position = Math.max(0, ...this.data.references.map(row => Number(row.position || 0))) + 1;
    const references = this.decorateReferences(this.data.references.concat([Object.assign({}, item, { purpose: '', position, label: item.name || `参考素材 ${position}` })]));
    const checked = rules.validateReferences(references, this.data.limits);
    if (!checked.ok) { wx.showToast({ title: checked.message, icon: 'none' }); return; }
    this.markEdited({ references });
  },
  removeFrame(event) {
    const target = event.currentTarget.dataset.target;
    if (target === 'last_frame') { this.applyCreationMode(this.data.first_frame_media_id, '', { last_frame_media_id: '', lastFrame: {} }); return; }
    this.applyCreationMode('', '', { first_frame_media_id: '', last_frame_media_id: '', firstFrame: {}, lastFrame: {} });
  },
  removeReference(event) {
    const removed = this.data.references.find(item => item.media_id === event.currentTarget.dataset.id);
    if (removed && removed.media_id === this._audioMediaID) this.stopAudioPreview(true);
    const references = this.decorateReferences(this.data.references.filter(item => item.media_id !== event.currentTarget.dataset.id));
    const notice = removed && this.data.reference_prompt ? '参考编号已更新，请重新核对画面描述中的素材编号' : '';
    this.markEdited({ references });
    if (notice) this.setData({ notice });
  },
  updateReferencePurpose(event) {
    const id = event.currentTarget.dataset.id;
    this.markEdited({ references: this.data.references.map(item => item.media_id === id ? Object.assign({}, item, { purpose: event.detail.value }) : item) });
  },
  mediaPreviewError(event) {
    const id = event.currentTarget.dataset.id;
    this.setData({ references: this.data.references.map(item => item.media_id === id ? Object.assign({}, item, { preview_error: true }) : item), error: '视频预览加载失败，不影响作为参考素材提交' });
  },
  previewFrame(event) {
    const epoch = this._previewEpoch = (this._previewEpoch || 0) + 1;
    const item = event.currentTarget.dataset.target === 'last_frame' ? this.data.lastFrame : this.data.firstFrame;
    const url = String(item && (item.preview_url || item.url || item.media_url) || '');
    if (!url) return;
    wx.previewImage({ current: url, urls: [url], fail: () => { if (this.current() && epoch === this._previewEpoch) this.setData({ error: '图片预览失败，请稍后重试' }); } });
  },
  previewReference(event) {
    const epoch = this._previewEpoch = (this._previewEpoch || 0) + 1;
    const item = this.data.references.find(row => row.media_id === event.currentTarget.dataset.id);
    if (!item) return;
    if (item.media_type === 'image') {
      const images = this.data.references.filter(row => row.media_type === 'image').map(row => row.preview_url || row.url || row.media_url).filter(Boolean);
      const current = item.preview_url || item.url || item.media_url;
      if (current) wx.previewImage({ current, urls: images, fail: () => { if (this.current() && epoch === this._previewEpoch) this.setData({ error: '图片预览失败，请稍后重试' }); } });
      return;
    }
    if (item.media_type === 'video') {
      this.stopAudioPreview(true);
      const url = item.media_url || item.url;
      if (!url || typeof wx.previewMedia !== 'function') { this.setData({ error: '视频暂时无法预览' }); return; }
      wx.previewMedia({ sources: [{ url, type: 'video', poster: item.preview_url || '' }], current: 0, fail: () => { if (this.current() && epoch === this._previewEpoch) this.setData({ error: '视频预览失败，请稍后重试' }); } });
    }
  },
  pauseReferenceVideos() {
    if (typeof wx.createVideoContext !== 'function') return;
    this.data.references.filter(item => item.media_type === 'video' && item.video_dom_id).forEach(item => wx.createVideoContext(item.video_dom_id, this).pause());
  },
  onReferenceVideoPlay() { this.stopAudioPreview(true); },
  stopAudioPreview(destroy) {
    if (this._audioLoadTimer) clearTimeout(this._audioLoadTimer);
    this._audioLoadTimer = null;
    const player = this._audioPlayer;
    this._audioPlayer = null;
    this._audioMediaID = '';
    this.setData({ playingAudioID: '', loadingAudioID: '' });
    if (!player) return;
    try { player.pause(); } catch (_) { /* The native player may already be closed. */ }
    if (destroy) {
      try { player.destroy(); } catch (_) { /* The native player may already be closed. */ }
    }
  },
  toggleReferenceAudio(event) {
    const session = this.session();
    const id = String(event.currentTarget.dataset.id || '');
    const item = this.data.references.find(row => row.media_id === id && row.media_type === 'audio');
    if (!item) return;
    if (this._audioMediaID === id && this._audioPlayer) { this.stopAudioPreview(true); return; }
    this.stopAudioPreview(true);
    this.pauseReferenceVideos();
    const url = String(item.media_url || item.url || '');
    if (!url || typeof wx.createInnerAudioContext !== 'function') { this.setData({ audioPreviewError: '音频暂时无法试听' }); return; }
    const player = wx.createInnerAudioContext();
    this._audioPlayer = player;
    this._audioMediaID = id;
    this.setData({ playingAudioID: '', loadingAudioID: id, audioPreviewError: '' });
    player.src = url;
    player.onPlay(() => { if (this.ownsSession(session) && this._audioPlayer === player) { if (this._audioLoadTimer) clearTimeout(this._audioLoadTimer); this._audioLoadTimer = null; this.setData({ playingAudioID: id, loadingAudioID: '', audioPreviewError: '' }); } });
    player.onPause(() => { if (this.ownsSession(session) && this._audioPlayer === player) this.setData({ playingAudioID: '', loadingAudioID: '' }); });
    player.onEnded(() => { if (this._audioPlayer === player) this.stopAudioPreview(true); });
    player.onError(() => { if (this.ownsSession(session) && this._audioPlayer === player) { this.setData({ audioPreviewError: '音频试听失败，请稍后重试' }); this.stopAudioPreview(true); } });
    this._audioLoadTimer = setTimeout(() => { if (this.ownsSession(session) && this._audioPlayer === player && this.data.playingAudioID !== id) { this.setData({ audioPreviewError: '音频加载超时，请重试' }); this.stopAudioPreview(true); } }, 8000);
    if (this._audioLoadTimer && this._audioLoadTimer.unref) this._audioLoadTimer.unref();
    player.play();
  },

  changeSetting(event) {
    const field = event.currentTarget.dataset.field;
    const options = this.data[field + 'Options'];
    const index = Number(event.detail.value);
    const value = options[index];
    const patch = { [field]: value, [field + 'Index']: index };
    if (field === 'ratio') patch.ratioLabel = value === 'adaptive' ? '原图比例' : value;
    if (field === 'resolution') Object.assign(patch, this.parameterState(this.data.capabilities, this.data.mode, value, this.data));
    if (field === 'duration') patch.durationLabel = `${value}秒`;
    if (field === 'quantity') patch.quantityLabel = `${value}条`;
    this.markEdited(patch);
  },

  draftInput() {
    const referenceMode = this.data.mode === 'reference';
    return {
      name: String(this.data.name || '').trim() || '未命名创作', mode: this.data.mode,
      prompt: referenceMode ? '' : String(this.data.prompt || '').trim(), reference_prompt: referenceMode ? String(this.data.reference_prompt || '').trim() : '',
      first_frame_media_id: referenceMode ? '' : (this.data.first_frame_media_id || ''), last_frame_media_id: referenceMode ? '' : (this.data.last_frame_media_id || ''),
      references: referenceMode ? this.data.references.map(item => ({ media_id: item.media_id, media_type: item.media_type, purpose: item.purpose || '', position: item.position })) : [],
      ratio: this.data.ratio, resolution: this.data.resolution, duration: Number(this.data.duration), quantity: Number(this.data.quantity),
    };
  },
  validationInput() {
    const input = this.draftInput();
    if (input.mode === 'reference') input.references = this.data.references.map(item => ({
      media_id: item.media_id, media_type: item.media_type, purpose: item.purpose || '', position: item.position,
      duration: Number(item.duration_seconds || item.duration || 0),
    }));
    return input;
  },
  validate() { const checked = rules.validateCreation(this.validationInput(), this.data.capabilities); if (!checked.ok) this.setData({ error: checked.message }); return checked.ok; },
  async persistDraft() {
    const input = this.draftInput();
    if (this.data.draft_id) return api.updateDraft(this.data.draft_id, Object.assign({}, input, { expected_revision: Number(this.data.revision) }));
    return api.createDraft(input);
  },
  async saveDraft() {
    if (this.data.saving || this.data.quoting) return null;
    const session = this.session();
    const version = this._editVersion || 0;
    this.setData({ saving: true, error: '', notice: '' });
    try {
      const saved = await this.persistDraft();
      if (!this.ownsSession(session)) return null;
      const changed = version !== (this._editVersion || 0);
      this.setData({ saving: false, draft_id: saved.draft_id, revision: saved.revision, quote: null, quoteReady: false, notice: changed ? '已保存上一版，当前修改尚未保存' : '草稿已保存' });
      this.scheduleAutoQuote();
      return saved;
    } catch (error) {
      if (this.ownsSession(session)) this.setData({ saving: false, error: error.statusCode === 409 ? '草稿已在别处更新，请返回后重新进入' : (error.message || '草稿保存失败') });
      return null;
    }
  },

  generationReady() {
    return !!this.data.capabilities.enabled && !!this.data.selectedModeEnabled && rules.validateCreation(this.validationInput(), this.data.capabilities).ok;
  },
  scheduleAutoQuote() {
    if (this._quoteTimer) clearTimeout(this._quoteTimer);
    if (this.data.submissionUncertain) {
      this.setData({ quote: null, quoteReady: false, quoteStatus: 'disabled', quoteHint: '提交结果待确认，请先查看创作记录' });
      return;
    }
    if (!this.data.capabilities.enabled || !this.data.selectedModeEnabled) {
      this.setData({ quote: null, quoteReady: false, quoteStatus: 'disabled', quoteHint: this.data.capabilities.disabled_reason || '生成能力尚未开放' });
      return;
    }
    if (!this.generationReady()) {
      this.setData({ quote: null, quoteReady: false, quoteStatus: 'incomplete', quoteHint: '完善内容后显示预计费用' });
      return;
    }
    this.setData({ quote: null, quoteReady: false, quoteStatus: 'waiting', quoteHint: '正在准备价格…' });
    this._quoteTimer = setTimeout(() => {
      this._quoteTimer = null;
      if (this.data.quoting || this.data.saving) { this.scheduleAutoQuote(); return; }
      return this.refreshQuote();
    }, 500);
    if (this._quoteTimer && this._quoteTimer.unref) this._quoteTimer.unref();
  },
  async refreshQuote() {
    if (!this.generationReady() || this.data.quoting || this.data.saving) return null;
    const session = this.session();
    const version = this._editVersion || 0;
    const frozenInput = this.draftInput();
    const frozenFingerprint = rules.billableFingerprint(frozenInput);
    this.setData({ quoting: true, quoteReady: false, quoteStatus: 'loading', quoteHint: '价格计算中…', error: '' });
    try {
      const saved = await this.persistDraft();
      if (!this.ownsSession(session)) return;
      this.setData({ draft_id: saved.draft_id, revision: saved.revision });
      if (version !== (this._editVersion || 0)) {
        this.setData({ quoting: false, quote: null, quoteReady: false });
        this.scheduleAutoQuote();
        return null;
      }
      const quote = await api.quote({ draft_id: saved.draft_id, expected_revision: saved.revision });
      if (!this.ownsSession(session)) return;
      if (version !== (this._editVersion || 0) || frozenFingerprint !== rules.billableFingerprint(this.draftInput())) {
        this.setData({ quoting: false, quote: null, quoteReady: false });
        this.scheduleAutoQuote();
        return null;
      }
      quote.client_fingerprint = frozenFingerprint;
      quote.expires_label = String(quote.expires_at || '').replace('T', ' ').slice(0, 16);
      this.setData({ quoting: false, draft_id: saved.draft_id, revision: saved.revision, quote, quoteReady: true, quoteStatus: 'ready', quoteHint: `预计 ${quote.total_stones} 灵石` });
      return quote;
    } catch (error) {
      if (this.ownsSession(session)) this.setData({ quoting: false, quote: null, quoteReady: false, quoteStatus: 'error', quoteHint: '价格加载失败，点击重试', error: error.message || '报价获取失败' });
      return null;
    }
  },
  retryQuote() { if (this.data.quoteStatus === 'error') this.refreshQuote(); },
  async generateVideo() {
    if (this.data.submissionUncertain) {
      this.setData({ error: '提交结果待确认，请先到创作记录核对，避免重复扣费。' });
      return;
    }
    if (!this.generationReady()) { this.validate(); return; }
    if (!this.data.quoteReady || !rules.isQuoteUsable(this.data.quote, this.draftInput())) {
      this.setData({ quote: null, quoteReady: false, quoteHint: '价格正在更新，请稍候' });
      this.scheduleAutoQuote();
      return;
    }
    await this.submit();
  },
  async submit() {
    if (this.data.submitting || !rules.isQuoteUsable(this.data.quote, this.draftInput())) { this.setData({ quote: null, error: '输入或报价已变化，请重新获取报价' }); return; }
    const built = rules.buildBatchRequest({ draft_id: this.data.draft_id, revision: this.data.revision }, this.data.quote, api.idempotencyKey);
    if (!built.ok) { this.setData({ error: built.message }); return; }
    const session = this.session();
    this.setData({ submitting: true, error: '', notice: '正在安全提交，请勿重复操作…' });
    try {
      const batch = await api.createBatch(built.value);
      if (!this.ownsSession(session)) return;
      this.openSubmittedBatch(batch, session);
    } catch (error) {
      if (!this.ownsSession(session)) return;
      const uncertain = !!error.networkError || !!error.pendingSubmission || Number(error.statusCode) >= 500;
      this.setData({ submitting: false, submissionUncertain: uncertain, quote: uncertain ? this.data.quote : null, quoteReady: uncertain, error: uncertain ? '提交结果待确认，请先到创作记录核对，避免重复扣费。' : (error.message || '提交失败') });
    }
  },
  openSubmittedBatch(batch, session) {
    const owner = session || this.session();
    if (!this.ownsSession(owner)) return;
    this.setData({ submitting: false, submissionUncertain: true, quoteReady: false, notice: '任务已创建，正在打开创作记录…' });
    wx.redirectTo({ url: '/pages/aivideostudio/detail?batch_id=' + encodeURIComponent(batch.batch_id || batch.id), fail: () => { if (this.ownsSession(owner)) this.setData({ error: '任务已创建，请到创作记录查看进度。' }); } });
  },
  async checkSubmission() {
    if (this.data.submitting) return;
    const session = this.session();
    let pending;
    try { pending = api.pendingSubmission && api.pendingSubmission(); }
    catch (error) { if (this.ownsSession(session)) this.setData({ error: error.message }); return; }
    if (!pending) return;
    this.setData({ submitting: true, submissionUncertain: true, notice: '正在查询上次提交结果…' });
    try {
      const batch = await api.getSubmission(pending);
      if (this.ownsSession(session)) this.openSubmittedBatch(batch, session);
    } catch (error) {
      if (this.ownsSession(session)) this.setData({ submitting: false, notice: error.statusCode === 404 ? '尚未找到任务；可按原请求恢复，不会重复扣费。' : '提交记录暂不可查询，可稍后重试或查看创作记录。' });
    }
  },
  async recoverSubmission() {
    if (this.data.submitting) return;
    const session = this.session();
    let pending;
    try { pending = api.pendingSubmission && api.pendingSubmission(); }
    catch (error) { if (this.ownsSession(session)) this.setData({ error: error.message }); return; }
    if (!pending) {
      this.setData({ submissionUncertain: false });
      this.scheduleAutoQuote();
      return;
    }
    this.setData({ submitting: true, submissionUncertain: true, error: '', notice: '正在按原请求恢复提交，不会重复扣费…' });
    try {
      const batch = await api.createBatch(pending);
      if (this.ownsSession(session)) this.openSubmittedBatch(batch, session);
    } catch (error) {
      if (!this.ownsSession(session)) return;
      let unresolved = true;
      try { unresolved = !!api.pendingSubmission(); } catch (_) { /* Keep payment blocked until its account can be checked. */ }
      this.setData({ submitting: false, submissionUncertain: unresolved, notice: '', error: unresolved ? '提交结果仍待核对，可稍后恢复原请求或到创作记录查看。' : (error.message || '该请求未创建任务，可重新生成') });
      if (!unresolved) this.scheduleAutoQuote();
    }
  },

  openMaterials() { wx.navigateTo({ url: '/pages/aivideostudio/materials' }); },
  openPrompts() {
    const session = this.session();
    wx.navigateTo({ url: '/pages/aivideostudio/prompts?select=1&mode=' + encodeURIComponent(this.data.mode), events: { selected: item => { if (this.ownsSession(session)) this.markEdited({ [this.data.activeTab === 'reference' ? 'reference_prompt' : 'prompt']: item.prompt }); } } });
  },
  openRecords() { wx.navigateTo({ url: '/pages/aivideostudio/records' }); },
  openTutorial() { wx.navigateTo({ url: '/pages/aivideostudio/tutorial?mode=' + encodeURIComponent(this.data.mode || 'text') }); },
  polishUnavailable() { wx.showToast({ title: 'AI 润色能力尚未开放', icon: 'none' }); },
});
