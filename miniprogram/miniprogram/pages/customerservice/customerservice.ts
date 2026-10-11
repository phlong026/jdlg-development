import { generateRequestParams, paramsToHeaders } from '../../utils/parameter'
import { getCachedDeviceFingerprint } from '../../utils/deviceFingerprint'
import { blockUnsafeText } from '../../utils/promptReview'
import {
  DEFAULT_CUSTOMER_SERVICE_CORP_ID,
  DEFAULT_CUSTOMER_SERVICE_URL,
  openEnterpriseCustomerServiceChat,
} from '../../utils/enterpriseWechat'

const API_BASE_URL = 'https://api.jiadilingguang.com'

// 智能客服专用 system prompt，约束 AI 只回答建房相关问题
const CUSTOMER_SERVICE_SYSTEM_PROMPT = `你是"甲第灵光"平台的 AI 建房顾问，专门帮助用户解决自建房设计相关问题。

## 你的能力范围
- 解答乡村自建房的风格选择、户型布局、面宽进深、建房流程等咨询
- 引导用户描述建房需求（层数、风格、面宽、进深、房间功能）
- 介绍平台功能（AI 生成效果图、造价测算、对接设计老师）

## 你的行为规则
1. 回复简短友好，每次不超过 100 字
2. 不给精确报价，造价问题引导用户使用造价测算工具或对接设计老师
3. 不承诺施工可行性，施工问题引导对接设计老师
4. 不回答与建房无关的问题，礼貌告知你只擅长建房设计
5. 不编造地方政策、建材价格等具体数据
6. 尽量在回复末尾引导用户下一步操作（生成方案、填写需求、看案例等）

## 用户当前状态
{user_context}`

type MessageRole = 'ai' | 'user'
type QuickAction = {
  key: string
  label: string
  desc: string
}
type ServiceMessage = {
  id: string
  role: MessageRole
  content: string
}
type ConsultationPlan = {
  visible: boolean
  taskNo: string
  prompt: string
  imageUrl: string
  action: string
  actionText: string
  summary: string
}
type StyleReferenceItem = {
  id: string
  title: string
  imageUrl: string
  styleLabel: string
  styleValue: string
  likeCount: number
}
type StyleReferenceTab = {
  value: string
  label: string
  aliases: string[]
}
type DemandForm = {
  floors: string
  landWidth: string
  landDepth: string
  style: string
  rooms: string
  hasLand: string
  region: string
  startTime: string
}

const emptyDemandForm: DemandForm = {
  floors: '',
  landWidth: '',
  landDepth: '',
  style: '',
  rooms: '',
  hasLand: '',
  region: '',
  startTime: '',
}

const welcomeActions: QuickAction[] = [
  { key: 'build', label: '近期准备建房', desc: '整理宅基地和风格需求' },
  { key: 'browse', label: '先看看案例', desc: '进入模板广场参考' },
  { key: 'fuzzy', label: '还没想清楚', desc: '先生成一套参考方案' },
]

const collectActions: QuickAction[] = [
  { key: 'cost', label: '了解造价', desc: '先做建房造价测算' },
  { key: 'human', label: '找设计老师', desc: '咨询造价、施工或深化' },
]

const floorOptions = ['一层', '两层', '三层', '四层']
const styleReferenceTabs: StyleReferenceTab[] = [
  { value: 'new_minnan', label: '新闽派', aliases: ['新闽派', '闽南', '闽式', '红砖'] },
  { value: 'new_chinese', label: '新中式', aliases: ['新中式', '中式', '合院', '东方'] },
  { value: 'modern', label: '现代风格', aliases: ['现代风格', '现代', '现代简约', '简约', '极简', '奶油', '原木'] },
  { value: 'classic_european', label: '经典欧式', aliases: ['经典欧式', '欧式', '欧式别墅', '法式', '古典'] },
  { value: 'regional', label: '地域特色', aliases: ['地域特色', '地域', '在地', '地方', '本土', '民俗'] },
]
const styleOptions = styleReferenceTabs.map((item) => item.label)
const landStatusOptions = ['已有宅基地', '正在选址', '还未确定']
const startTimeOptions = ['3个月内', '半年内', '一年内', '先了解']

function buildSessionNo() {
  return `cs_${Date.now()}_${Math.random().toString(16).slice(2, 8)}`
}

function containsHighIntent(text: string) {
  if (/造价|报价|多少钱|预算|施工|落地|图纸|施工图|修改|优化|深化|怎么收费|能不能建/.test(text)) {
    return true
  }
  const hasContactIntent = /联系我|加微信|留电话|打电话|电话联系|微信联系|加我|对接|转人工|人工客服|找设计师|设计师联系/.test(text)
  const hasContactChannel = /电话|微信|设计师/.test(text)
  return hasContactIntent && hasContactChannel
}

function detectLiveIntent(text: string) {
  const cleanText = text.trim()
  if (!cleanText) {
    return { level: '', type: '', hint: '' }
  }
  if (containsHighIntent(cleanText)) {
    return { level: 'high', type: 'handoff_human', hint: '涉及造价、施工或深化问题，发送后建议转专业设计老师承接。' }
  }
  if (/案例|模板|参考|看看|风格/.test(cleanText)) {
    return { level: 'medium', type: 'guide_template', hint: '已识别为参考案例需求，可先看案例或直接生成方案。' }
  }
  if (/建房|自建房|翻建|宅基地|面宽|进深|几层|三层|两层|房间|户型|外观/.test(cleanText)) {
    return { level: 'medium', type: 'guide_generate', hint: '已识别为建房方案需求，可一键生成第一版参考方案。' }
  }
  return { level: 'medium', type: 'collect_requirement', hint: '' }
}

function buildDemandParts(form: DemandForm) {
  const parts = [
    form.region ? `地区：${form.region}` : '',
    form.hasLand ? `宅基地状态：${form.hasLand}` : '',
    form.floors ? `层数：${form.floors}` : '',
    form.landWidth ? `面宽：${form.landWidth}米` : '',
    form.landDepth ? `进深：${form.landDepth}米` : '',
    form.style ? `风格：${form.style}` : '',
    form.rooms ? `房间功能：${form.rooms}` : '',
    form.startTime ? `计划动工：${form.startTime}` : '',
  ]
  return parts.filter(Boolean)
}

function buildDemandFormSummary(form: DemandForm) {
  return buildDemandParts(form).join('；')
}

function hasDemandFormValue(form: DemandForm) {
  return buildDemandParts(form).length > 0
}

function buildPromptFromText(text: string) {
  const cleanText = text.trim()
  if (!cleanText) {
    return '乡村自建房，三层，新闽派外观，适合福建乡村居住，布局实用，外观大气。'
  }
  return cleanText.includes('自建房') || cleanText.includes('建房')
    ? cleanText
    : `${cleanText}，自建房方案，布局实用，外观适合乡村居住。`
}

function buildLeadRemark(reason: string, form: DemandForm, actionText: string = '') {
  const summary = buildDemandFormSummary(form)
  return [reason, actionText ? `结果页诉求：${actionText}` : '', summary].filter(Boolean).join('；')
}

function normalizeTemplateImageUrl(url: string) {
  const cleanUrl = String(url || '').trim()
  if (!cleanUrl) {
    return ''
  }
  if (/^(https?:\/\/|wxfile:\/\/|file:\/\/|data:)/i.test(cleanUrl)) {
    return cleanUrl
  }
  if (cleanUrl.startsWith('//')) {
    return `https:${cleanUrl}`
  }
  if (cleanUrl.startsWith('/')) {
    return `${API_BASE_URL}${cleanUrl}`
  }
  return `${API_BASE_URL}/${cleanUrl}`
}

function resolveStyleTemplateTab(style: string) {
  const cleanStyle = String(style || '').trim()
  return styleReferenceTabs.find((item) => item.value === cleanStyle || item.aliases.some((alias) => cleanStyle.includes(alias))) || null
}

function inferDemandStyle(text: string) {
  const cleanText = String(text || '').trim()
  if (!cleanText) {
    return ''
  }
  if (/新闽派|闽南|闽式|红砖/.test(cleanText)) {
    return '新闽派'
  }
  if (/中式|合院|东方/.test(cleanText)) {
    return '新中式'
  }
  if (/现代|简约|极简|奶油|原木/.test(cleanText)) {
    return '现代风格'
  }
  if (/欧式|法式|古典/.test(cleanText)) {
    return '经典欧式'
  }
  if (/地域|在地|地方|本土|民俗/.test(cleanText)) {
    return '地域特色'
  }
  return ''
}

function decodeOption(value: unknown) {
  const raw = String(value || '')
  if (!raw) {
    return ''
  }
  try {
    return decodeURIComponent(raw)
  } catch (_error) {
    return raw
  }
}

function buildPlanSummary(prompt: string, taskNo: string) {
  const cleanPrompt = prompt.trim()
  if (cleanPrompt) {
    return cleanPrompt.length > 32 ? `${cleanPrompt.slice(0, 32)}...` : cleanPrompt
  }
  return taskNo ? `任务编号：${taskNo}` : '点击查看生成详情'
}

function buildConsultationPlan(options: Record<string, any>): ConsultationPlan {
  const taskNo = String(options?.task_no || '')
  const prompt = decodeOption(options?.prompt)
  const imageUrl = decodeOption(options?.image_url)
  const action = decodeOption(options?.action) || ''
  const actionText = decodeOption(options?.action_text) || ''
  return {
    visible: !!(taskNo || prompt || imageUrl),
    taskNo,
    prompt,
    imageUrl,
    action,
    actionText,
    summary: buildPlanSummary(prompt, taskNo),
  }
}

function openTemplateSquare() {
  wx.switchTab({
    url: '/pages/template/template',
    fail: () => wx.showToast({ title: '模板广场打开失败', icon: 'none' }),
  })
}

function getErrorStatusCode(error: any) {
  return Number(error?.statusCode || 0)
}

Page({
  data: {
    sessionNo: '',
    source: 'home',
    sourceTaskNo: '',
    inputText: '',
    demandText: '',
    demandForm: { ...emptyDemandForm },
    demandFormVisible: false,
    demandStep: 1,
    demandSummary: '',
    demandSummaryVisible: false,
    styleReferenceList: [] as StyleReferenceItem[],
    styleReferenceLoading: false,
    selectedStyleReference: null as StyleReferenceItem | null,
    matchedStyleLabel: '',
    matchedStyleValue: '',
    styleReferenceEmptyText: '',
    styleReferenceTabs,
    scrollAnchorId: 'messageBottom',
    scrollIntoView: '',
    floorOptions,
    styleOptions,
    landStatusOptions,
    startTimeOptions,
    intentLevel: 'medium',
    messages: [] as ServiceMessage[],
    quickActions: welcomeActions,
    liveIntentHint: '',
    liveIntentType: '',
    liveIntentLevel: '',
    keyboardHeight: 0,
    consultationPlan: {
      visible: false,
      taskNo: '',
      prompt: '',
      imageUrl: '',
      summary: '',
    } as ConsultationPlan,
    submitting: false,
    /** AI 流式回复相关状态 */
    aiStreaming: false,
    aiStreamingMessageId: '',
    waitingFirstChunk: false,
  },

  onLoad(options: Record<string, any>) {
    const source = String(options?.source || 'home')
    const sourceTaskNo = String(options?.task_no || '')
    const consultationPlan = buildConsultationPlan(options || {})
    const prompt = consultationPlan.prompt
    const actionText = consultationPlan.actionText
    const sessionNo = buildSessionNo()
    const messages: ServiceMessage[] = [{
      id: `${Date.now()}_welcome`,
      role: 'ai',
      content: sourceTaskNo
        ? `你的方案已经生成。${actionText ? `你刚选择了“${actionText}”，` : ''}我可以继续帮你优化户型、外观和造价思路，也可以为你对接设计老师。`
        : '你好，我是甲第灵光 AI 建房顾问。我可以用 1 分钟帮你生成户型参考、外观效果图和初步造价思路。请问你近期有自建房或翻建计划吗？',
    }]
    this.setData({
      sessionNo,
      source,
      sourceTaskNo,
      demandText: prompt,
      messages,
      quickActions: sourceTaskNo ? collectActions : welcomeActions,
      consultationPlan,
    })
    this.recordEvent('welcome_shown', {
      source,
      source_task_no: sourceTaskNo,
      has_plan: consultationPlan.visible,
      image_url: consultationPlan.imageUrl,
      action: consultationPlan.action,
      action_text: consultationPlan.actionText,
    })
    this.resetSilenceTimers()
  },

  onUnload() {
    this.clearSilenceTimers()
    const page = this as any
    if (page._csScrollTimer) {
      clearTimeout(page._csScrollTimer)
    }
  },

  onHide() {
    this.clearSilenceTimers()
  },

  onShow() {
    if (this.data.sessionNo) {
      this.resetSilenceTimers()
    }
  },

  clearSilenceTimers() {
    const page = this as any
    if (page._csSilence30Timer) {
      clearTimeout(page._csSilence30Timer)
    }
    if (page._csSilence60Timer) {
      clearTimeout(page._csSilence60Timer)
    }
    page._csSilence30Timer = null
    page._csSilence60Timer = null
  },

  resetSilenceTimers() {
    this.clearSilenceTimers()
    const page = this as any
    page._csSilence30Timer = setTimeout(() => {
      this.pushAIMessage('如果你近期准备建房，可以点“近期准备建房”，我会分几步帮你整理需求。')
      this.recordEvent('silence_remind_30', { action: 'collect_requirement' })
    }, 30000)
    page._csSilence60Timer = setTimeout(() => {
      this.pushAIMessage('如果暂时不确定，也可以先点击一键生成，先看第一版方案再调整。')
      this.setData({ quickActions: collectActions })
      this.recordEvent('silence_remind_60', { action: 'guide_generate' })
    }, 60000)
  },

  touchFlow() {
    this.resetSilenceTimers()
  },

  showDemandCollector(step = 1) {
    this.setData({
      demandFormVisible: true,
      demandStep: Math.min(4, Math.max(1, step)),
      demandSummaryVisible: false,
    })
    this.scrollToBottom()
  },

  closeDemandCollector() {
    this.touchFlow()
    this.setData({ demandFormVisible: false })
  },

  onDemandStepNext() {
    this.touchFlow()
    const nextStep = Number(this.data.demandStep || 1) + 1
    if (nextStep > 4) {
      this.onSubmitDemandForm()
      return
    }
    this.setData({ demandStep: nextStep })
  },

  onDemandStepPrev() {
    this.touchFlow()
    const prevStep = Number(this.data.demandStep || 1) - 1
    if (prevStep < 1) {
      this.setData({ demandFormVisible: false })
      return
    }
    this.setData({ demandStep: prevStep })
  },

  onContinueDemandSupplement() {
    this.touchFlow()
    this.showDemandCollector(1)
  },

  onPlanCardTap() {
    this.touchFlow()
    const taskNo = String(this.data.consultationPlan?.taskNo || this.data.sourceTaskNo || '')
    if (!taskNo) {
      wx.showToast({ title: '暂无可查看的方案详情', icon: 'none' })
      return
    }
    wx.navigateTo({
      url: `/pages/generatedetails/generatedetails?task_no=${encodeURIComponent(taskNo)}`,
      fail: () => wx.showToast({ title: '方案详情打开失败', icon: 'none' }),
    })
  },

  onDemandInput(e: any) {
    this.touchFlow()
    const field = String(e.currentTarget.dataset.field || '')
    const value = String(e.detail.value || '')
    if (!field) {
      return
    }
    this.setData({ [`demandForm.${field}`]: value } as Record<string, any>)
  },

  onDemandOptionTap(e: any) {
    this.touchFlow()
    const field = String(e.currentTarget.dataset.field || '')
    const value = String(e.currentTarget.dataset.value || '')
    if (!field) {
      return
    }
    this.setData({ [`demandForm.${field}`]: value } as Record<string, any>)
    if (field === 'style') {
      this.loadStyleReferences(value)
    }
  },

  onSubmitDemandForm() {
    this.touchFlow()
    const form = this.data.demandForm as DemandForm
    const summary = buildDemandFormSummary(form)
    if (!summary) {
      wx.showToast({ title: '先补充至少一项建房需求', icon: 'none' })
      return
    }
    this.pushUserChoice(summary)
    this.pushAIMessage('需求已整理好。你可以先一键生成专属方案；如果想先了解预算或继续深化，下方可以了解造价或对接设计老师。')
    this.setData({
      intentLevel: form.startTime && form.startTime !== '先了解' ? 'high' : this.data.intentLevel,
      quickActions: collectActions,
      demandFormVisible: false,
      demandSummary: summary,
      demandSummaryVisible: true,
    })
    this.loadStyleReferences(form.style)
    this.scrollToBottom()
    this.recordEvent('structured_requirement_collected', {
      demand_form: form,
      summary,
    })
    this.createLead('structured_requirement', { silent: true }).catch((error) => {
      if (getErrorStatusCode(error) !== 404) {
        console.warn('创建结构化需求线索失败', error)
      }
    })
  },

  onInput(e: any) {
    this.touchFlow()
    const inputText = String(e.detail.value || '')
    const intent = detectLiveIntent(inputText)
    this.setData({
      inputText,
      liveIntentHint: intent.hint,
      liveIntentType: intent.type,
      liveIntentLevel: intent.level,
    })
  },

  onComposerFocus() {
    this.touchFlow()
  },

  onComposerBlur() {
    if (Number(this.data.keyboardHeight || 0) !== 0) {
      this.setData({ keyboardHeight: 0 })
    }
  },

  onKeyboardHeightChange(e: any) {
    const keyboardHeight = Number(e?.detail?.height || 0)
    const nextKeyboardHeight = Math.max(0, keyboardHeight)
    const currentKeyboardHeight = Number(this.data.keyboardHeight || 0)
    if (Math.abs(nextKeyboardHeight - currentKeyboardHeight) < 8) {
      return
    }
    this.setData({ keyboardHeight: nextKeyboardHeight })
  },

  onComposerAdd() {
    this.touchFlow()
    wx.showToast({ title: '资料补充功能规划中', icon: 'none' })
  },

  onQuickAction(e: any) {
    this.touchFlow()
    const key = String(e.currentTarget.dataset.key || '')
    if (key === 'build') {
      this.pushUserChoice('近期准备建房')
      this.pushAIMessage('太好了，我会分几步帮你整理建房需求，先从层数和风格开始。')
      this.setData({ intentLevel: 'high', quickActions: collectActions })
      this.showDemandCollector(1)
      this.recordEvent('intent_classified', { intent_type: 'build', intent_level: 'high' })
      return
    }
    if (key === 'browse') {
      this.pushUserChoice('先看看案例')
      this.recordEvent('guide_template', { intent_type: 'browse' })
      openTemplateSquare()
      return
    }
    if (key === 'fuzzy') {
      this.pushUserChoice('还没想清楚')
      this.pushAIMessage('很多业主一开始也不确定怎么建。可以先补充一两项基础信息，生成一套参考方案后再慢慢调整。')
      this.setData({ intentLevel: 'medium', quickActions: collectActions })
      this.recordEvent('intent_classified', { intent_type: 'fuzzy', intent_level: 'medium' })
      return
    }
    if (key === 'generate') {
      this.goGenerate()
      return
    }
    if (key === 'cost') {
      this.pushUserChoice('了解造价')
      this.recordEvent('guide_cost', { from: 'collect_actions' })
      wx.navigateTo({
        url: '/pages/aicost/aicost',
        fail: () => wx.showToast({ title: '造价测算页面打开失败', icon: 'none' }),
      })
      return
    }
    if (key === 'template') {
      this.recordEvent('guide_template', { from: 'collect_actions' })
      openTemplateSquare()
      return
    }
    if (key === 'human') {
      this.handoffHuman()
    }
  },

  onSend() {
    this.touchFlow()
    const text = String(this.data.inputText || '').trim()
    if (!text) {
      return
    }
    if (blockUnsafeText(text, { scene: 'chat' })) {
      return
    }
    // 如果 AI 正在流式回复中，阻止重复发送
    if (this.data.aiStreaming) {
      return
    }
    const nextDemand = [this.data.demandText, text].filter(Boolean).join('；')
    const inferredStyle = inferDemandStyle(text)
    const nextData: Record<string, any> = {
      inputText: '',
      demandText: nextDemand,
      quickActions: collectActions,
      liveIntentHint: '',
      liveIntentType: '',
      liveIntentLevel: '',
    }
    if (inferredStyle) {
      nextData['demandForm.style'] = inferredStyle
    }
    this.pushUserChoice(text)
    this.setData(nextData)
    if (inferredStyle) {
      this.loadStyleReferences(inferredStyle)
    }

    // 统一走后端意图识别流式接口
    void this.sendIntentStream(text)
  },

  buildCompleteDemandText() {
    const demandText = String(this.data.demandText || '').trim()
    const formSummary = buildDemandFormSummary(this.data.demandForm as DemandForm)
    if (!demandText) {
      return formSummary
    }
    if (!formSummary || demandText.includes(formSummary)) {
      return demandText
    }
    return [demandText, formSummary].join('；')
  },

  loadStyleReferences(style: string) {
    const matchedStyle = resolveStyleTemplateTab(style)
    if (!matchedStyle) {
      this.setData({
        styleReferenceList: [],
        styleReferenceLoading: false,
        selectedStyleReference: null,
        matchedStyleLabel: '',
        matchedStyleValue: '',
        styleReferenceEmptyText: '',
      })
      return
    }
    const requestStyleValue = matchedStyle.value
    this.setData({
      styleReferenceLoading: true,
      matchedStyleLabel: matchedStyle.label,
      matchedStyleValue: requestStyleValue,
      selectedStyleReference: null,
      styleReferenceEmptyText: '',
    })
    wx.request({
      url: `${API_BASE_URL}/api/v1/miniprogram/templates`,
      method: 'GET',
      data: {
        main_tab: 'style',
        sub_tab: requestStyleValue,
        page: 1,
        page_size: 8,
      },
      success: (res: any) => {
        if (String(this.data.matchedStyleValue || '') !== requestStyleValue) {
          return
        }
        const response = res.data || {}
        const list = Array.isArray(response?.data?.list) ? response.data.list : []
        const styleReferenceList = list
          .filter((item: any) => String(item.main_tab || '') === 'style' && String(item.sub_tab || '') === requestStyleValue)
          .map((item: any) => {
            const imageUrl = normalizeTemplateImageUrl(String(item.thumbnail || item.preview_url || ''))
            return {
              id: String(item.id || ''),
              title: String(item.name || item.description || matchedStyle.label).trim() || matchedStyle.label,
              imageUrl,
              styleLabel: matchedStyle.label,
              styleValue: requestStyleValue,
              likeCount: Number(item.like_count || 0),
            } as StyleReferenceItem
          })
          .filter((item: StyleReferenceItem) => item.id && item.imageUrl)
          .slice(0, 8)
        this.setData({
          styleReferenceList,
          selectedStyleReference: styleReferenceList[0] || null,
          styleReferenceLoading: false,
          styleReferenceEmptyText: styleReferenceList.length ? '' : '该风格暂无参考图，将按文字需求生成。',
        })
        this.recordEvent('style_reference_loaded', {
          style,
          sub_tab: requestStyleValue,
          count: styleReferenceList.length,
          order: 'like_count_desc',
        })
      },
      fail: () => {
        if (String(this.data.matchedStyleValue || '') !== requestStyleValue) {
          return
        }
        this.setData({
          styleReferenceList: [],
          selectedStyleReference: null,
          styleReferenceLoading: false,
          styleReferenceEmptyText: '参考图加载失败，将按文字需求生成。',
        })
      },
    })
  },

  onStyleReferenceTabTap(e: any) {
    this.touchFlow()
    const value = String(e.currentTarget.dataset.value || '')
    const selectedTab = styleReferenceTabs.find((item) => item.value === value)
    if (!selectedTab) {
      return
    }
    this.setData({ 'demandForm.style': selectedTab.label } as Record<string, any>)
    this.loadStyleReferences(selectedTab.label)
  },

  onStyleReferenceTap(e: any) {
    this.touchFlow()
    const id = String(e.currentTarget.dataset.id || '')
    const selected = (this.data.styleReferenceList as StyleReferenceItem[]).find((item) => item.id === id)
    if (!selected) {
      return
    }
    this.setData({ selectedStyleReference: selected })
    this.recordEvent('style_reference_selected', {
      template_id: selected.id,
      title: selected.title,
      image_url: selected.imageUrl,
      style_label: selected.styleLabel,
      style_value: selected.styleValue,
      like_count: selected.likeCount,
    })
  },

  goGenerate() {
    this.touchFlow()
    const prompt = buildPromptFromText(this.buildCompleteDemandText() || this.data.inputText)
    const selectedReference = this.data.selectedStyleReference as StyleReferenceItem | null
    const referenceImageUrl = String(selectedReference?.imageUrl || '').trim()
    const matchedStyleValue = String(this.data.matchedStyleValue || '')
    const hasStyleReferences = (this.data.styleReferenceList as StyleReferenceItem[]).length > 0
    if (matchedStyleValue && hasStyleReferences && (!selectedReference || !referenceImageUrl || selectedReference.styleValue !== matchedStyleValue)) {
      wx.showToast({ title: '请先选择当前风格下的参考图', icon: 'none' })
      return
    }
    this.recordEvent('generate_click', {
      prompt,
      demand_form: this.data.demandForm,
      style_reference: selectedReference,
    })
    this.createLead('generate_click', { silent: true }).catch((error) => {
      if (getErrorStatusCode(error) !== 404) {
        console.warn('创建生成点击线索失败', error)
      }
    })
    const referenceQuery = referenceImageUrl ? `&reference_image_url=${encodeURIComponent(referenceImageUrl)}` : ''
    const styleQuery = selectedReference?.styleValue ? `&style_code=${encodeURIComponent(selectedReference.styleValue)}&style_label=${encodeURIComponent(selectedReference.styleLabel)}` : ''
    wx.navigateTo({
      url: `/pages/aigenerate/aigenerate?source=customer_style_reference&showSceneTabs=1&tab=exterior&prompt=${encodeURIComponent(prompt)}${referenceQuery}${styleQuery}`,
      success: (navRes: any) => {
        if (!referenceImageUrl || !navRes?.eventChannel) {
          return
        }
        navRes.eventChannel.emit('prefillGenerateData', {
          prompt,
          reference_image_url: referenceImageUrl,
          reference_image_urls: [referenceImageUrl],
          style_reference_mode: true,
          style_reference_priority: 'high',
          style_code: selectedReference?.styleValue || '',
          style_label: selectedReference?.styleLabel || '',
          reference_template_id: selectedReference?.id || '',
        })
      },
      fail: () => wx.showToast({ title: '页面跳转失败', icon: 'none' }),
    })
  },

  async handoffHuman() {
    this.touchFlow()
    try {
      await this.createLead('handoff_human')
    } catch (error) {
      if (getErrorStatusCode(error) !== 404) {
        console.warn('创建客服线索失败', error)
      }
    }
    this.recordEvent('human_handoff_triggered', {
      source_task_no: this.data.sourceTaskNo,
      demand_form: this.data.demandForm,
    })
    const result = await openEnterpriseCustomerServiceChat({
      customerServiceCorpId: DEFAULT_CUSTOMER_SERVICE_CORP_ID,
      customerServiceUrl: DEFAULT_CUSTOMER_SERVICE_URL,
    })
    if (!result.opened) {
      wx.showToast({ title: '已为你记录需求，可在首页联系服务商继续沟通', icon: 'none' })
    }
  },

  async createLead(reason: string, options: { silent?: boolean } = {}) {
    const demandForm = this.data.demandForm as DemandForm
    const completeDemand = this.buildCompleteDemandText()
    const demandSummary = buildPromptFromText(completeDemand || this.data.inputText)
    const consultationPlan = this.data.consultationPlan || {}
    const selectedStyleReference = this.data.selectedStyleReference
    const token = wx.getStorageSync('token')
    if (!token) {
      if (!options.silent) {
        wx.showToast({ title: '请先登录后留资', icon: 'none' })
      }
      return
    }
    const apiPath = '/api/v1/miniprogram/customer-service/leads'
    const body = {
      session_no: this.data.sessionNo,
      source: this.data.source,
      source_task_no: this.data.sourceTaskNo,
      demand_summary: demandSummary,
      house_floors: demandForm.floors,
      house_style: demandForm.style,
      land_width: demandForm.landWidth,
      land_depth: demandForm.landDepth,
      room_requirement: demandForm.rooms,
      intent_level: this.data.intentLevel || (hasDemandFormValue(demandForm) ? 'medium' : 'low'),
      status: 'new',
      remark: buildLeadRemark(reason, demandForm, String((consultationPlan as ConsultationPlan).actionText || '')),
      payload: {
        reason,
        demand_form: demandForm,
        consultation_plan: consultationPlan,
        style_reference: selectedStyleReference,
      },
    }
    await this.postCustomerService(apiPath, body)
  },

  async recordEvent(eventType: string, payload: Record<string, any> = {}) {
    const token = wx.getStorageSync('token')
    const anonymous = !token && eventType === 'welcome_shown'
    if (!token && !anonymous) {
      return
    }
    const apiPath = anonymous ? '/api/v1/miniprogram/customer-service/events/anonymous' : '/api/v1/miniprogram/customer-service/events'
    const body = {
      session_no: this.data.sessionNo,
      event_type: eventType,
      source: this.data.source,
      source_task_no: this.data.sourceTaskNo,
      intent_level: this.data.intentLevel,
      demand_summary: this.buildCompleteDemandText(),
      payload,
    }
    try {
      if (anonymous) {
        await this.postCustomerServiceAnonymous(apiPath, body)
      } else {
        await this.postCustomerService(apiPath, body)
      }
    } catch (error) {
      if (getErrorStatusCode(error) === 404) {
        return
      }
      console.warn('记录智能客服事件失败', error)
    }
  },

  postCustomerServiceAnonymous(apiPath: string, body: Record<string, any>) {
    return new Promise((resolve, reject) => {
      wx.request({
        url: `${API_BASE_URL}${apiPath}`,
        method: 'POST',
        data: body,
        header: {
          'Content-Type': 'application/json',
        },
        success: (res: any) => {
          const data = res.data || {}
          if (res.statusCode === 200 && data.code === 0) {
            resolve(data.data)
            return
          }
          const requestError = new Error(data.msg || '智能客服请求失败') as Error & { statusCode?: number }
          requestError.statusCode = res.statusCode
          reject(requestError)
        },
        fail: reject,
      })
    })
  },

  postCustomerService(apiPath: string, body: Record<string, any>) {
    return new Promise((resolve, reject) => {
      const token = wx.getStorageSync('token')
      const deviceID = getCachedDeviceFingerprint() || ''
      const params = generateRequestParams(token, body, apiPath, deviceID)
      wx.request({
        url: `${API_BASE_URL}${apiPath}`,
        method: 'POST',
        header: {
          ...paramsToHeaders(params),
          'Content-Type': 'application/json',
        },
        data: body,
        success: (res: any) => {
          const data = res.data || {}
          if (res.statusCode === 200 && data.code === 0) {
            resolve(data.data)
            return
          }
          const requestError = new Error(data.msg || (res.statusCode === 404 ? '智能客服接口未部署' : '智能客服请求失败')) as Error & { statusCode?: number }
          requestError.statusCode = res.statusCode
          reject(requestError)
        },
        fail: reject,
      })
    })
  },

  // ==================== AI 大模型流式回复 ====================

  /**
   * 构建注入到 system prompt 的用户上下文
   */
  buildUserContext() {
    const form = this.data.demandForm as DemandForm
    const parts: string[] = []
    if (form.floors) parts.push(`层数：${form.floors}`)
    if (form.style) parts.push(`风格偏好：${form.style}`)
    if (form.landWidth) parts.push(`面宽：${form.landWidth}米`)
    if (form.landDepth) parts.push(`进深：${form.landDepth}米`)
    if (form.rooms) parts.push(`房间功能：${form.rooms}`)
    if (form.region) parts.push(`地区：${form.region}`)
    if (form.hasLand) parts.push(`宅基地状态：${form.hasLand}`)
    if (form.startTime) parts.push(`计划动工：${form.startTime}`)
    const demandText = String(this.data.demandText || '').trim()
    if (demandText) parts.push(`累积需求描述：${demandText}`)
    if (this.data.sourceTaskNo) parts.push(`已关联方案任务号：${this.data.sourceTaskNo}`)
    return parts.length > 0 ? parts.join('\n') : '用户尚未填写具体建房需求'
  },

  /**
   * 调用后端意图识别流式接口，根据返回的意图类型执行对应路由
   */
  async sendIntentStream(text: string) {
    if (blockUnsafeText(text, { scene: 'chat' })) {
      return
    }

    const token = wx.getStorageSync('token')
    if (!token) {
      this.pushAIMessage('请先登录后再继续咨询，登录后我可以更好地为你服务。')
      return
    }

    const requestBody = {
      text,
      session_no: this.data.sessionNo || '',
      demand_context: this.buildCompleteDemandText(),
    }

    const deviceID = getCachedDeviceFingerprint() || ''
    const apiPath = '/api/v1/miniprogram/ai/intent/stream'
    const params = generateRequestParams(token, JSON.stringify(requestBody), apiPath, deviceID)
    const headers = paramsToHeaders(params)

    // 先添加一个空的 AI 消息占位，用于流式更新
    const aiMessageId = `${Date.now()}_intent_stream`
    const aiMessage: ServiceMessage = {
      id: aiMessageId,
      role: 'ai',
      content: '',
    }
    this.setData({
      messages: this.data.messages.concat([aiMessage]),
      aiStreaming: true,
      aiStreamingMessageId: aiMessageId,
      waitingFirstChunk: true,
    })
    this.scrollToBottom()

    let fullContent = ''
    let intentHandled = false
    let lastGuideAction = ''
    const that = this

    const requestTask = wx.request({
      url: `${API_BASE_URL}${apiPath}`,
      method: 'POST',
      header: {
        ...headers,
        'Content-Type': 'application/json',
        'Accept': 'text/event-stream',
      },
      data: requestBody,
      enableChunked: true,
      success: (res: any) => {
        that.setData({ waitingFirstChunk: false, aiStreaming: false, aiStreamingMessageId: '' })
        if (res.statusCode !== 200 && !intentHandled) {
          that.updateStreamingMessage(aiMessageId, '抱歉，暂时无法回复。你可以点击"找设计老师"获取人工帮助。')
        }
      },
      fail: (_err: any) => {
        that.setData({ waitingFirstChunk: false, aiStreaming: false, aiStreamingMessageId: '' })
        if (!intentHandled) {
          // 接口失败，兜底走 AI 大模型
          that.removeStreamingMessage(aiMessageId)
          that.sendAIChatMessage(text)
        }
      },
    } as any) as any

    requestTask.onChunkReceived((res: any) => {
      try {
        const chunkText = that.arrayBufferToString(res.data)
        const lines = chunkText.split('\n')
        for (const line of lines) {
          if (!line.startsWith('data: ')) continue
          const jsonStr = line.substring(6).trim()
          if (!jsonStr || jsonStr === '[DONE]') continue
          try {
            const data = JSON.parse(jsonStr)
            if (data.type === 'intent') {
              intentHandled = true
              that.recordEvent('intent_classified', {
                intent_type: data.intent_type,
                intent_level: data.intent_level,
                guide_action: data.guide_action,
                user_text: text,
              })
              that.setData({ intentLevel: data.intent_level || that.data.intentLevel })

              // 根据意图类型执行路由（非 knowledge_hit 和 chat 的直接动作）
              if (data.intent_type === 'handoff_human') {
                that.setData({ waitingFirstChunk: false, aiStreaming: false, aiStreamingMessageId: '' })
                that.updateStreamingMessage(aiMessageId, '这个问题已经涉及造价、施工落地或方案深化。我马上为你对接专业设计老师一对一沟通。')
                that.setData({ demandFormVisible: false })
                that.handoffHuman()
                return
              }
              if (data.intent_type === 'guide_generate') {
                that.setData({ waitingFirstChunk: false, aiStreaming: false, aiStreamingMessageId: '' })
                that.updateStreamingMessage(aiMessageId, '收到你的建房需求，我来帮你整理一下具体信息，生成贴合你需求的方案。')
                that.showDemandCollector(1)
                return
              }
              if (data.intent_type === 'guide_template') {
                that.setData({ waitingFirstChunk: false, aiStreaming: false, aiStreamingMessageId: '' })
                that.updateStreamingMessage(aiMessageId, '帮你跳转到模板广场，里面有大量真实建房案例可以参考。')
                openTemplateSquare()
                return
              }
              if (data.intent_type === 'chat') {
                // AI 兜底：移除占位消息，走正常 AI 聊天流
                that.setData({ waitingFirstChunk: false, aiStreaming: false, aiStreamingMessageId: '' })
                that.removeStreamingMessage(aiMessageId)
                that.sendAIChatMessage(text)
                return
              }
              // knowledge_hit：继续接收流式内容
            } else if (data.type === 'content' && data.content) {
              if (that.data.waitingFirstChunk) {
                that.setData({ waitingFirstChunk: false })
              }
              fullContent += data.content
              that.updateStreamingMessage(aiMessageId, fullContent)
            } else if (data.type === 'done') {
              that.setData({ waitingFirstChunk: false, aiStreaming: false, aiStreamingMessageId: '' })
              // 知识库命中后执行引导动作
              if (intentHandled) {
                that.executeGuideAction(lastGuideAction || '')
              }
            } else if (data.type === 'error') {
              that.setData({ waitingFirstChunk: false, aiStreaming: false, aiStreamingMessageId: '' })
              that.updateStreamingMessage(aiMessageId, `抱歉，${data.error || '发生错误'}。你可以换个方式描述你的需求。`)
            }

            // 记录 guide_action 供 done 时使用
            if (data.type === 'intent' && data.guide_action) {
              lastGuideAction = data.guide_action
            }
          } catch (_parseErr) {
            // 忽略单行解析错误
          }
        }
      } catch (_err) {
        // 忽略 chunk 解析错误
      }
    })
  },

  /**
   * 根据引导动作执行对应操作
   */
  executeGuideAction(action: string) {
    switch (action) {
      case 'collect':
        this.showDemandCollector(1)
        break
      case 'generate':
        // 不自动跳转，仅展示回复
        break
      case 'cost':
        // 不自动跳转，仅展示回复
        break
      case 'human':
        // 不自动转人工，仅展示回复（用户可手动点击）
        break
      case 'template':
        // 不自动跳转，仅展示回复
        break
      default:
        break
    }
  },

  /**
   * 移除流式占位消息（用于兜底切换到其他流）
   */
  removeStreamingMessage(messageId: string) {
    const messages = this.data.messages.filter((msg: ServiceMessage) => msg.id !== messageId)
    this.setData({ messages })
  },

  /**
   * 调用 AI 聊天接口获取流式回复（作为知识库未命中时的兜底）
   */
  async sendAIChatMessage(text: string) {
    const token = wx.getStorageSync('token')
    if (!token) {
      this.pushAIMessage('请先登录后再继续咨询，登录后我可以更好地为你服务。')
      return
    }

    // 构建聊天历史（最近 8 轮）
    const history = this.data.messages
      .filter((msg: ServiceMessage) => msg.role === 'user' || msg.role === 'ai')
      .map((msg: ServiceMessage) => ({
        role: msg.role === 'user' ? 'user' : 'assistant',
        content: msg.content,
      }))
    const last = history[history.length - 1]
    const dedupedHistory = last?.role === 'user' && last.content === text ? history.slice(0, -1) : history
    const messages = [...dedupedHistory.slice(-15), { role: 'user', content: text }]

    // 注入用户上下文到 system prompt
    const systemPrompt = CUSTOMER_SERVICE_SYSTEM_PROMPT.replace('{user_context}', this.buildUserContext())

    const requestBody = {
      messages,
      system_prompt: systemPrompt,
    }

    const deviceID = getCachedDeviceFingerprint() || ''
    const apiPath = '/api/v1/miniprogram/ai/chat/stream'
    const params = generateRequestParams(token, JSON.stringify(requestBody), apiPath, deviceID)
    const headers = paramsToHeaders(params)

    // 先添加一个空的 AI 消息占位，用于流式更新
    const aiMessageId = `${Date.now()}_ai_stream`
    const aiMessage: ServiceMessage = {
      id: aiMessageId,
      role: 'ai',
      content: '',
    }
    this.setData({
      messages: this.data.messages.concat([aiMessage]),
      aiStreaming: true,
      aiStreamingMessageId: aiMessageId,
      waitingFirstChunk: true,
    })
    this.scrollToBottom()

    // 记录 AI 兜底回复事件
    this.recordEvent('ai_chat_fallback', { user_text: text })

    let fullContent = ''
    const requestTask = wx.request({
      url: `${API_BASE_URL}${apiPath}`,
      method: 'POST',
      header: {
        ...headers,
        'Content-Type': 'application/json',
        'Accept': 'text/event-stream',
      },
      data: requestBody,
      enableChunked: true,
      success: (res: any) => {
        this.setData({ waitingFirstChunk: false, aiStreaming: false, aiStreamingMessageId: '' })
        if (res.statusCode === 402) {
          this.updateStreamingMessage(aiMessageId, '当前咨询服务暂时不可用，你可以直接点击下方"找设计老师"按钮联系人工客服。')
          return
        }
        // 非流式 fallback 响应
        if (res.statusCode === 200 && res.data) {
          if (typeof res.data === 'object' && res.data.code !== undefined && res.data.code !== 0) {
            this.updateStreamingMessage(aiMessageId, `抱歉，${res.data.msg || '请求失败'}。你可以点击下方按钮继续操作。`)
          }
        } else if (res.statusCode !== 200) {
          this.updateStreamingMessage(aiMessageId, '抱歉，暂时无法回复。你可以点击"找设计老师"获取人工帮助。')
        }
      },
      fail: (err: any) => {
        this.setData({ waitingFirstChunk: false, aiStreaming: false, aiStreamingMessageId: '' })
        console.error('[智能客服] AI 流式请求失败:', err)
        this.updateStreamingMessage(aiMessageId, '网络不太稳定，暂时无法回复。你可以稍后再试，或直接点击"找设计老师"联系人工客服。')
      },
    } as any) as any

    // 监听分块数据（SSE 格式）
    requestTask.onChunkReceived((res: any) => {
      try {
        const chunkText = this.arrayBufferToString(res.data)
        const lines = chunkText.split('\n')
        for (const line of lines) {
          if (line.startsWith('data: ')) {
            const jsonStr = line.substring(6).trim()
            if (jsonStr && jsonStr !== '[DONE]') {
              try {
                const data = JSON.parse(jsonStr)
                if (data.type === 'content' && data.content) {
                  if (this.data.waitingFirstChunk) {
                    this.setData({ waitingFirstChunk: false })
                  }
                  fullContent += data.content
                  this.updateStreamingMessage(aiMessageId, fullContent)
                } else if (data.type === 'done') {
                  this.setData({ waitingFirstChunk: false, aiStreaming: false, aiStreamingMessageId: '' })
                } else if (data.type === 'error') {
                  this.setData({ waitingFirstChunk: false, aiStreaming: false, aiStreamingMessageId: '' })
                  this.updateStreamingMessage(aiMessageId, `抱歉，${data.error || '发生错误'}。你可以换个方式描述你的需求。`)
                }
              } catch (_parseErr) {
                // 忽略单行解析错误
              }
            }
          }
        }
      } catch (e) {
        console.error('[智能客服] 解析分块数据失败:', e)
      }
    })
  },

  /**
   * ArrayBuffer 转字符串（处理 UTF-8）
   */
  arrayBufferToString(buffer: ArrayBuffer): string {
    const uint8Array = new Uint8Array(buffer)
    let result = ''
    for (let i = 0; i < uint8Array.length; i++) {
      result += String.fromCharCode(uint8Array[i])
    }
    try {
      return decodeURIComponent(escape(result))
    } catch (_e) {
      return result
    }
  },

  /**
   * 更新流式消息内容（按 messageId 定位）
   */
  updateStreamingMessage(messageId: string, content: string) {
    const messages = this.data.messages.map((msg: ServiceMessage) => {
      if (msg.id === messageId) {
        return { ...msg, content }
      }
      return msg
    })
    this.setData({ messages })
    this.scrollToBottom()
  },

  // ==================== 基础消息方法 ====================

  pushUserChoice(content: string) {
    this.addMessage('user', content)
  },

  pushAIMessage(content: string) {
    this.addMessage('ai', content)
  },

  addMessage(role: MessageRole, content: string) {
    const messages = this.data.messages.concat([{ id: `${Date.now()}_${messagesLength(this.data.messages)}`, role, content }])
    this.setData({ messages })
    this.scrollToBottom()
  },

  scrollToBottom() {
    const page = this as any
    if (page._csScrollTimer) {
      clearTimeout(page._csScrollTimer)
    }
    const scrollAnchorId = `messageBottom${Date.now()}`
    this.setData({ scrollAnchorId, scrollIntoView: '' })
    page._csScrollTimer = setTimeout(() => {
      this.setData({ scrollIntoView: scrollAnchorId })
    }, 50)
  },
})

function messagesLength(messages: ServiceMessage[]) {
  return Array.isArray(messages) ? messages.length : 0
}
