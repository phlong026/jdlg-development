'use strict';
const api = require('../../utils/phase3-api');
const { startAIStream } = require('../../utils/ai-stream');

// 本地兜底仅保留中性短指令：完整业务提示词（含示例与措辞）由服务端注册表下发，
// 可运营调整而无需发版；防臆造条款刻意留在本地常量，任何服务端配置都无法移除它。
const writingTypes = [
  { key: 'design', label: '设计说明', heading: '方案设计说明', example: '', instruction: '请撰写方案设计说明。' },
  { key: 'project', label: '项目介绍', heading: '项目内容介绍', example: '', instruction: '请撰写项目内容介绍。' },
  { key: 'promotion', label: '推广文案', heading: '作品推广文案', example: '', instruction: '请撰写作品推广文案。' },
];
function parseVideoCopy(value) {
  const source = String(value || '').trim().replace(/^```(?:json)?\s*/i, '').replace(/\s*```$/, '');
  const result = JSON.parse(source);
  if (!result || Array.isArray(result) || typeof result !== 'object') throw new Error('视频文案格式不正确');
  const copy = {};
  for (const key of ['title', 'share_copy', 'moments_copy']) {
    if (typeof result[key] !== 'string' || !result[key].trim() || result[key].length > (key === 'title' ? 80 : 2000)) throw new Error('视频文案结构或长度不正确，请重新生成');
    copy[key] = result[key].trim();
  }
  return copy;
}

const ANTI_FABRICATION_CLAUSE = '只根据以下已知资料写作；不得编造面积、层数、造价、工期、地名、项目状态、业主评价或其他未提供的事实。缺失的信息请省略。';

Page({
  data: { videoBundle: false, videoCopy: null, videoSelections: { title: true, share_copy: true, moments_copy: true }, writingTypes, type: 'design', heading: '方案设计说明', example: writingTypes[0].example, exampleLabel: writingTypes[0].label, instruction: writingTypes[0].instruction, title: '', facts: '', result: '', error: '', notice: '', navigating: false, loginRequired: false, returnLabel: '带入编辑器', voiceActive: false, voicePartial: '', source: '', streaming: false, streamPartial: '', introPresets: [], introPresetIndex: -1 },
  onLoad(options) {
    wx.hideShareMenu();
    this._token = String(wx.getStorageSync('token') || '');
    this._unloaded = false;
    this._source = String(options && options.source || '');
    this.setData({ source: this._source });
    if (this._source === 'project_note') this.setData({ returnLabel: '采纳到项目笔记' });
    else if (this._source === 'editor') this.setData({ returnLabel: '采纳到当前草稿' });
    if ((this._source === 'editor' || this._source === 'project_note') && typeof this.getOpenerEventChannel === 'function') {
      this._returnChannel = this.getOpenerEventChannel();
      if (this._returnChannel && this._returnChannel.on) this._returnChannel.on('writingContext', payload => this.receiveWritingContext(payload));
    }
    if (this._token) this.loadWritingTypes();
  },
  onShow() {
    wx.hideShareMenu();
    const token = String(wx.getStorageSync('token') || '');
    if (token !== this._token) {
      this.cancelStream();
      this._typesLoadedOnce = false;
      this._typesRequest = null;
      this.setData({ introPresets: [], introPresetIndex: -1, videoCopy: null, title: '', facts: '', result: '', notice: '', streaming: false, streamPartial: '', error: '账号已变化，请重新填写创作内容' });
    }
    this._token = token;
    this.setData({ navigating: false, loginRequired: !token });
    if (token) this.loadWritingTypes();
  },
  onUnload() { this._unloaded = true; this.cancelStream(); },
  receiveWritingContext(payload) {
    if ((this._source !== 'editor' && this._source !== 'project_note') || this._contextLoaded || this._token !== String(wx.getStorageSync('token') || '')) return;
    if (!payload || payload.source !== this._source) return;
    if (payload.target === 'video_bundle' && this._source === 'editor') {
      this.setData({ videoBundle: true, heading: '视频推广文案', videoSelections: { title: !String(payload.title || '').trim(), share_copy: !String(payload.share_copy || '').trim(), moments_copy: !String(payload.moments_copy || '').trim() } });
    }
    this._contextLoaded = true;
    if (this._source === 'project_note') {
      this._projectID = String(payload.project_id || '');
      this._noteID = String(payload.note_id || '');
      this._noteRevision = Number(payload.note_revision || 0);
    } else {
      this._contentID = String(payload.content_id || '');
      this._revision = Number(payload.revision || 0);
    }
    this.setData({ title: String(payload.title || '').trim(), facts: String(payload.facts || '').trim().slice(0, 6000), notice: this._source === 'project_note' ? '已带入项目笔记' : '已带入当前草稿', error: '' });
    if (this.data.videoBundle && payload.auto_generate === true) this.quickGenerate();
  },
  selectType(event) {
    const type = this.data.writingTypes.find(item => item.key === event.currentTarget.dataset.type);
    if (type) this.setData({ type: type.key, heading: type.heading, example: type.example || '', exampleLabel: type.label, instruction: type.instruction, error: '' });
  },
  // 拉取服务端 Prompt 注册表；失败静默保持本地兜底，不打扰创作流程。
  loadWritingTypes() {
    if (this._typesLoadedOnce) return Promise.resolve();
    if (this._typesRequest) return this._typesRequest;
    const token = this._token;
    if (!token || token !== String(wx.getStorageSync('token') || '')) return Promise.resolve();
    const current = () => !this._unloaded && token === this._token && token === String(wx.getStorageSync('token') || '') && this._typesRequest === request;
    const request = api.writingPrompts().then(result => {
      if (!current()) return;
      const prompts = ((result && result.prompts) || []).filter(item => item && item.key && item.heading && item.instruction).map(item => {
        // 已有类型的名称属于产品导航；运营仍可调整指令，扩展类型保留服务端命名。
        const productType = writingTypes.find(type => type.key === item.key);
        return {
          key: String(item.key), label: productType ? productType.label : String(item.label || item.heading),
          heading: productType ? productType.heading : String(item.heading),
          example: String(item.example || ''), instruction: String(item.instruction),
        };
      });
      if (this._unloaded) return;
      const update = {};
      if (prompts.length) {
        const current = prompts.find(item => item.key === this.data.type) || prompts[0];
        Object.assign(update, { writingTypes: prompts, type: current.key, heading: current.heading, example: current.example, exampleLabel: current.label, instruction: current.instruction, error: '' });
      }
      // B8: intro presets（名片/品牌介绍），追加到已知资料末尾而非自由 prompt。
      const presets = ((result && result.intro_presets) || []).filter(item => item && item.name && item.content).map(item => ({ id: item.id, name: item.name, content: item.content }));
      update.introPresets = presets;
      if (Object.keys(update).length) this.setData(update);
      this._typesLoadedOnce = true;
    }).catch(() => {}).finally(() => { if (this._typesRequest === request) this._typesRequest = null; });
    this._typesRequest = request;
    return request;
  },
  selectIntroPreset(event) {
    const index = Number(event.detail.value) || 0;
    const preset = this.data.introPresets[index];
    if (!preset) return;
    this.setData({ introPresetIndex: index, error: '' });
  },
  editTitle(event) { this.setData({ title: String(event.detail.value || ''), error: '' }); },
  editFacts(event) { this.setData({ facts: String(event.detail.value || ''), error: '' }); },
  // 语音输入：微信同声传译插件（WechatSI）。插件在小程序后台未添加或低版本基础库不可用时
  // 必须优雅降级为键盘输入，不能阻塞创作流程；识别结果按行追加进已知资料。
  voiceManager() {
    if (this._voiceManager !== undefined) return this._voiceManager;
    this._voiceManager = null;
    try {
      const plugin = requirePlugin('WechatSI');
      const manager = plugin && plugin.getRecordRecognitionManager && plugin.getRecordRecognitionManager();
      if (manager) this._voiceManager = this.attachVoiceHandlers(manager);
    } catch (_) { this._voiceManager = null; }
    return this._voiceManager;
  },
  attachVoiceHandlers(manager) {
    manager.onStart = () => { if (!this._unloaded) this.setData({ voiceActive: true, voicePartial: '', error: '' }); };
    manager.onRecognize = res => { if (!this._unloaded && res && res.result) this.setData({ voicePartial: String(res.result) }); };
    manager.onStop = res => {
      if (this._unloaded) return;
      const text = String(res && res.result || '').trim();
      this.setData({ voiceActive: false, voicePartial: '' });
      if (text) this.appendFacts(text);
    };
    manager.onError = res => {
      if (this._unloaded) return;
      const message = String(res && res.msg || '');
      this.setData({ voiceActive: false, voicePartial: '', error: /permission|auth|denied/i.test(message) ? '未授权麦克风，无法语音输入，可直接打字填写' : '语音识别失败，请重试或改用键盘输入' });
    };
    return manager;
  },
  startVoiceInput() {
    if (this.data.loginRequired) return;
    const manager = this.voiceManager();
    if (!manager) { this.setData({ error: '语音输入组件不可用，可直接打字填写' }); return; }
    manager.start({ duration: 60000, lang: 'zh_CN' });
  },
  stopVoiceInput() {
    if (this._voiceManager && this.data.voiceActive) this._voiceManager.stop();
  },
  appendFacts(text) {
    const current = String(this.data.facts || '').replace(/\s+$/, '');
    this.setData({ facts: ((current ? current + '\n' : '') + String(text)).slice(0, 6000), error: '' });
  },
  editResult(event) { this.setData({ result: String(event.detail.value || ''), notice: '', error: '' }); },
  // B6 就地流式生成：不跳 chat 页，复用 SSE 通道直接在结果区流式回显。
  buildPrompt(action) {
    const facts = String(this.data.facts || (this.data.videoBundle ? this.data.title : '') || '').trim();
    if (!facts) return '';
    const instruction = { concise: '精简下面的文案，保留已知事实和重点。', expand: '适当扩写下面的文案，只补充表达，不增添未经提供的事实。', tone: '将下面的文案改为自然、专业、适合与业主沟通的语气。' }[action || ''];
    // B8: 名片/品牌预设作为额外已知资料注入，不进自由 prompt——维持防臆造边界。
    let extendedFacts = facts;
    const preset = this.data.introPresets[this.data.introPresetIndex];
    if (preset && preset.content) {
      extendedFacts = facts + '\n\n介绍信息（可作为署名或结尾参考）：\n' + String(preset.content).slice(0, 500);
    }
    if (this.data.videoBundle) return [
      '为视频作品生成标题、聊天转发文案和朋友圈文案。只返回一个JSON对象，三个字段均为字符串：title、share_copy、moments_copy。标题最多80字，其他每项最多2000字。不要输出Markdown或其他字段。',
      ANTI_FABRICATION_CLAUSE,
      '已知主题：' + String(this.data.title || ''),
      '已知资料与要求：' + extendedFacts,
      instruction && this.data.result ? instruction + '\n待调整内容：' + this.data.result : '',
    ].filter(Boolean).join('\n\n');
    return [
      `${this.data.instruction || '请撰写' + this.data.heading + '。'}${this.data.title ? '主题：' + this.data.title.trim() + '。' : ''}`,
      ANTI_FABRICATION_CLAUSE,
      '已知资料与要求：\n' + extendedFacts,
      instruction && this.data.result ? instruction + '\n待优化文案：\n' + this.data.result : '请直接输出可编辑的正文，不添加虚构项目或版本。',
    ].join('\n\n');
  },
  quickGenerate(event) {
    if (this.data.streaming || this.data.navigating) return;
    const token = String(wx.getStorageSync('token') || '');
    if (!token || token !== this._token) { this.setData({ loginRequired: !token, error: '请使用当前账号登录后生成文案' }); return; }
    const action = event && event.currentTarget && event.currentTarget.dataset.action || '';
    const prompt = this.buildPrompt(action);
    if (!prompt) { this.setData({ error: '请先填写已知资料和要求，AI 将以这些事实为依据' }); return; }
    this.setData({ streaming: true, streamPartial: '', error: '', notice: '正在生成…' });
    const request = this._streamRequest = (this._streamRequest || 0) + 1;
    let settled = false;
    const current = () => !settled && !this._unloaded && request === this._streamRequest && token === this._token && token === String(wx.getStorageSync('token') || '');
    const onError = message => {
      if (!current()) return;
      settled = true; this._streamTask = null;
      this.setData({ streaming: false, streamPartial: '', notice: '', error: message });
    };
    try {
      const task = startAIStream({
        messages: [{ role: 'user', content: prompt }],
        onChunk: partial => {
          if (current()) this.setData({ streamPartial: partial, notice: '' });
        },
        onDone: full => {
          if (!current()) return;
          settled = true; this._streamTask = null;
          const value = String(full || '').trim();
          if (this.data.videoBundle) {
            try { const videoCopy = parseVideoCopy(value); this.setData({ videoCopy, result: JSON.stringify(videoCopy), streaming: false, streamPartial: '', error: '', notice: '已生成，请勾选要采纳的内容；未勾选项保留原文' }); }
            catch (_) { this.setData({ streaming: false, streamPartial: '', error: '生成结果格式不完整，请重试；原文案已保留', notice: '' }); }
            return;
          }
          this.setData({ streaming: false, streamPartial: '', result: value || this.data.result, error: '', notice: value ? '已生成，请核对后使用' : '生成为空，请调整资料后重试' });
        },
        onError,
      });
      if (current()) this._streamTask = task;
    } catch (error) {
      console.warn('启动文案生成失败:', error);
      onError('生成暂时无法开始，请稍后重试');
    }
  },
  cancelStream() {
    // 先使回调失效，再中止请求；abort 可能同步触发网络失败回调。
    this._streamRequest = (this._streamRequest || 0) + 1;
    const task = this._streamTask;
    this._streamTask = null;
    if (task) task.abort();
  },
  stopStream() {
    if (!this.data.streaming) return;
    this.cancelStream();
    this.setData({ streaming: false, streamPartial: '', notice: '已停止生成' });
  },
  goLogin() { wx.navigateTo({ url: '/pages/login/login', fail: () => this.setData({ error: '登录页面暂时无法打开，请稍后重试' }) }); },
  generateWriting(event) {
    if (this.data.navigating) return;
    const token = String(wx.getStorageSync('token') || '');
    if (!token || token !== this._token) { this.setData({ loginRequired: !token, error: '请使用当前账号登录后生成文案' }); return; }
    const facts = String(this.data.facts || '').trim();
    if (!facts) { this.setData({ error: '请先填写已知资料和要求，AI 将以这些事实为依据' }); return; }
    const action = event && event.currentTarget && event.currentTarget.dataset.action || '';
    const instruction = { concise: '精简下面的文案，保留已知事实和重点。', expand: '适当扩写下面的文案，只补充表达，不增添未经提供的事实。', tone: '将下面的文案改为自然、专业、适合与业主沟通的语气。' }[action];
    const text = [
      `${this.data.instruction || '请撰写' + this.data.heading + '。'}${this.data.title ? '主题：' + this.data.title.trim() + '。' : ''}`,
      ANTI_FABRICATION_CLAUSE,
      '已知资料与要求：\n' + facts,
      instruction && this.data.result ? instruction + '\n待优化文案：\n' + this.data.result : '请直接输出可编辑的正文，不添加虚构项目或版本。',
    ].join('\n\n');
    this.setData({ navigating: true, error: '', notice: '' });
    wx.navigateTo({
      url: '/pages/chat/chat?mode=chat&source=content-writing&title=AI文案',
      events: {
        contentWritingResult: result => {
          if (this._unloaded) return;
          if (token !== String(wx.getStorageSync('token') || '') || token !== this._token) { this.setData({ result: '', error: '账号已变化，本次文案未带入' }); return; }
          const value = String(result && result.text || '').trim();
          if (value) this.setData({ result: value, navigating: false, error: '', notice: this._source === 'project_note' ? '已带入生成文案，可修改后采纳到项目笔记' : '已带入生成文案，可修改后保存到内容草稿' });
        },
      },
      success: result => result.eventChannel.emit('contentWritingTask', { text }),
      fail: () => this.setData({ navigating: false, error: 'AI对话暂时无法打开，已保留填写的资料，请重试' }),
    });
  },
  editVideoCopy(event) {
    const key = String(event.currentTarget.dataset.field || '');
    if (!this.data.videoCopy || !['title', 'share_copy', 'moments_copy'].includes(key)) return;
    const videoCopy = { ...this.data.videoCopy, [key]: String(event.detail.value || '').slice(0, key === 'title' ? 80 : 2000) };
    this.setData({ videoCopy, result: JSON.stringify(videoCopy), error: '' });
  },
  selectVideoFields(event) {
    const selected = new Set(event.detail.value || []);
    this.setData({ videoSelections: { title: selected.has('title'), share_copy: selected.has('share_copy'), moments_copy: selected.has('moments_copy') } });
  },
  saveToDraft() {
    if (this.data.navigating) return;
    if (this.data.videoBundle) {
      if (!this.data.videoCopy) { this.setData({ error: '请先生成完整视频文案' }); return; }
      if (!this._token || this._token !== String(wx.getStorageSync('token') || '')) { this.setData({ error: '账号已变化，请重新登录后创作' }); return; }
      if (!this._returnChannel || typeof this._returnChannel.emit !== 'function') { this.setData({ error: '原页面连接已失效，请保留文案后重试' }); return; }
      const video_copy = {};
      for (const key of ['title', 'share_copy', 'moments_copy']) if (this.data.videoSelections[key]) {
        const value = String(this.data.videoCopy[key] || '').trim();
        if (!value) { this.setData({ error: '请补全勾选的文案' }); return; }
        video_copy[key] = value;
      }
      if (!Object.keys(video_copy).length) { this.setData({ error: '请选择至少一项需要采纳的文案' }); return; }
      this.setData({ navigating: true, error: '' });
      this._returnChannel.emit('prefillContent', { source: 'editor', content_id: String(this._contentID || ''), revision: Number(this._revision || 0), video_copy });
      wx.navigateBack({ delta: 1, fail: () => this.setData({ navigating: false, error: '文案已带回原页面，请返回继续编辑' }) });
      return;
    }
    const text = String(this.data.result || '').trim();
    if (!text) { this.setData({ error: this._source === 'project_note' ? '请先生成或填写文案，再采纳到项目笔记' : '请先生成或填写文案，再保存到内容草稿' }); return; }
    const token = String(wx.getStorageSync('token') || '');
    if (!token || token !== this._token) { this.setData({ error: '账号已变化，请重新登录后创作' }); return; }
    const title = String(this.data.title || '').trim() || this.data.heading;
    this.setData({ navigating: true, error: '' });
    if (this._source === 'editor' || this._source === 'project_note') {
      if (!this._returnChannel || typeof this._returnChannel.emit !== 'function') { this.setData({ navigating: false, error: '原页面连接已失效，请保留文案并返回重试' }); return; }
      const identity = this._source === 'project_note'
        ? { project_id: String(this._projectID || ''), note_id: String(this._noteID || ''), note_revision: Number(this._noteRevision || 0) }
        : { content_id: String(this._contentID || ''), revision: Number(this._revision || 0) };
      this._returnChannel.emit('prefillContent', { source: this._source, title, text, ...identity });
      wx.navigateBack({ delta: 1, fail: () => this.setData({ navigating: false, error: '文案已带回原页面，请返回继续编辑' }) });
      return;
    }
    wx.navigateTo({
      url: '/pages/contentedit/contentedit',
      success: result => result.eventChannel.emit('prefillContent', { title, text }),
      fail: () => this.setData({ navigating: false, error: '内容编辑页暂时无法打开，文案仍保留在此页，请重试' }),
    });
  },
});
