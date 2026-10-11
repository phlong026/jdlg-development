// pages/settings/settings.ts
export {};

import { generateRequestParams, paramsToHeaders } from '../../utils/parameter'
import { getCachedDeviceFingerprint } from '../../utils/deviceFingerprint'

const SETTINGS_DARK_MODE_STORAGE_KEY = 'settings_dark_mode'
const PRIVACY_SETTINGS_STORAGE_KEY = 'privacy_settings'
const ACCOUNT_REQUEST_STORAGE_KEY = 'privacy_account_requests'
const API_BASE_URL = 'https://api.jiadilingguang.com'
const USER_IDENTITY_OPTIONS = ['乡墅业主', '设计师', '施工队', '建材商', '机构用户']
const SUPPORT_EMAIL = 'service@jiadilingguang.com'

type PermissionItem = {
  key: string;
  label: string;
  desc: string;
  enabled: boolean;
};

type PersonalInfoItem = {
  label: string;
  value: string;
};

type PrivacySettings = {
  dataSharingEnabled: boolean;
  recommendationEnabled: boolean;
};

const DEFAULT_PRIVACY_SETTINGS: PrivacySettings = {
  dataSharingEnabled: true,
  recommendationEnabled: true,
};

const PERMISSION_CONFIG = [
  { key: 'scope.camera', label: '相机权限', desc: '拍照上传、图像生成时使用' },
  { key: 'scope.album', label: '相册读取权限', desc: '选择本地图片与素材时使用' },
  { key: 'scope.writePhotosAlbum', label: '相册保存权限', desc: '保存生成图纸与视频时使用' },
  { key: 'scope.userLocation', label: '位置权限', desc: '提供位置相关服务时使用' },
  { key: 'scope.record', label: '麦克风权限', desc: '语音输入与客服沟通时使用' },
];

function maskPhone(phone: string) {
  const value = String(phone || '').trim();
  if (!/^1\d{10}$/.test(value)) {
    return value || '未绑定';
  }
  return `${value.slice(0, 3)}****${value.slice(7)}`;
}

function buildDefaultPersonalInfoItems(): PersonalInfoItem[] {
  return [
    { label: '用户名', value: '--' },
    { label: '昵称', value: '--' },
    { label: '手机号', value: '--' },
    { label: '身份类型', value: '--' },
    { label: '会员等级', value: '--' },
    { label: '账号 ID', value: '--' },
  ];
}

Page({
  data: {
    darkMode: false,
    cacheSize: '0MB',
    version: '1.0.0',
    identityOptions: USER_IDENTITY_OPTIONS,
    identityType: '',
    identityTypeIndex: 0,
    identitySaving: false,
    dataSharingEnabled: true,
    recommendationEnabled: true,
    permissionItems: [] as PermissionItem[],
    personalInfoItems: buildDefaultPersonalInfoItems(),
    personalInfoVisible: false,
    accountRequestVisible: false,
    accountRequestType: 'delete',
    accountRequestReason: '',
    accountRequestSubmitting: false,
    scrollIntoView: '',
  },

  onLoad(options: Record<string, string>) {
    this.initThemeSetting()
    this.initCacheSize()
    this.initPrivacySettings()
    this.loadProfileIdentity()
    this.loadPersonalInfo()
    const tab = String(options?.tab || '').trim()
    if (tab === 'privacy') {
      this.setData({ scrollIntoView: 'privacy-section' })
    }
  },

  onShow() {
    this.loadProfileIdentity()
    this.loadPersonalInfo()
    this.syncPermissionSettings()
  },

  ensureLoggedIn() {
    const token = wx.getStorageSync('token')
    if (!token) {
      wx.showToast({ title: '请先登录', icon: 'none' })
      return false
    }
    return true
  },

  initThemeSetting() {
    try {
      const savedDarkMode = wx.getStorageSync(SETTINGS_DARK_MODE_STORAGE_KEY)
      this.setData({ darkMode: savedDarkMode === true })
    } catch (e) {
      console.error('初始化主题设置失败:', e)
    }
  },

  initPrivacySettings() {
    try {
      const saved = wx.getStorageSync(PRIVACY_SETTINGS_STORAGE_KEY) || {}
      this.setData({
        dataSharingEnabled: saved.dataSharingEnabled !== false,
        recommendationEnabled: saved.recommendationEnabled !== false,
      })
    } catch (error) {
      console.error('初始化隐私设置失败:', error)
      this.setData({ ...DEFAULT_PRIVACY_SETTINGS })
    }
    this.syncPermissionSettings()
  },

  savePrivacySettings(partial: Partial<PrivacySettings>) {
    const nextSettings: PrivacySettings = {
      dataSharingEnabled: partial.dataSharingEnabled ?? this.data.dataSharingEnabled,
      recommendationEnabled: partial.recommendationEnabled ?? this.data.recommendationEnabled,
    }
    this.setData(nextSettings)
    wx.setStorageSync(PRIVACY_SETTINGS_STORAGE_KEY, nextSettings)
  },

  syncPermissionSettings() {
    wx.getSetting({
      success: (res) => {
        const authSetting = res.authSetting || {}
        const permissionItems = PERMISSION_CONFIG.map((item) => ({
          ...item,
          enabled: (authSetting as WechatMiniprogram.AuthSetting & Record<string, boolean | undefined>)[item.key] === true,
        }))
        this.setData({ permissionItems })
      },
      fail: () => {
        this.setData({
          permissionItems: PERMISSION_CONFIG.map((item) => ({
            ...item,
            enabled: false,
          })),
        })
      },
    })
  },

  onToggleDataSharing(e: any) {
    this.savePrivacySettings({ dataSharingEnabled: !!e.detail.value })
    wx.showToast({
      title: e.detail.value ? '已开启数据共享' : '已关闭数据共享',
      icon: 'none',
    })
  },

  onToggleRecommendation(e: any) {
    this.savePrivacySettings({ recommendationEnabled: !!e.detail.value })
    wx.showToast({
      title: e.detail.value ? '已开启推荐优化' : '已关闭推荐优化',
      icon: 'none',
    })
  },

  onPermissionSwitchTap() {
    this.openSystemPermissionSetting('请在系统设置中管理相应权限')
  },

  onCloseAllPermissions() {
    wx.showModal({
      title: '一键关闭权限',
      content: '小程序权限需在系统设置页中关闭。点击后将前往微信权限管理页，你可以逐项关闭相机、相册、位置等授权。',
      confirmText: '去设置',
      success: (res) => {
        if (res.confirm) {
          this.openSystemPermissionSetting()
        }
      },
    })
  },

  openSystemPermissionSetting(content?: string) {
    wx.openSetting({
      success: () => {
        this.syncPermissionSettings()
      },
      fail: () => {
        if (content) {
          wx.showToast({ title: '请前往系统设置管理权限', icon: 'none' })
        }
      },
    })
  },

  loadProfileIdentity() {
    const token = wx.getStorageSync('token')
    if (!token) {
      this.setData({ identityType: '', identityTypeIndex: 0 })
      return
    }
    const deviceID = getCachedDeviceFingerprint() || ''
    const apiPath = '/api/v1/miniprogram/profile'
    const params = generateRequestParams(token, {}, apiPath, deviceID)
    wx.request({
      url: `${API_BASE_URL}${apiPath}`,
      method: 'GET',
      header: paramsToHeaders(params),
      success: (res: any) => {
        const body = res.data || {}
        if (res.statusCode !== 200 || body.code !== 0) {
          return
        }
        const identityType = String((body.data || {}).identity_type || '')
        const index = USER_IDENTITY_OPTIONS.indexOf(identityType)
        this.setData({
          identityType,
          identityTypeIndex: index >= 0 ? index : 0,
        })
      },
    })
  },

  loadPersonalInfo(): Promise<void> {
    if (!this.ensureLoggedIn()) {
      this.setData({ personalInfoItems: buildDefaultPersonalInfoItems() })
      return Promise.resolve()
    }

    const token = String(wx.getStorageSync('token') || '').trim()
    const userInfo = wx.getStorageSync('userInfo') || {}
    const deviceID = getCachedDeviceFingerprint() || ''
    const apiPath = '/api/v1/miniprogram/profile'
    const params = generateRequestParams(token, {}, apiPath, deviceID)

    return new Promise((resolve) => {
      wx.request({
        url: `${API_BASE_URL}${apiPath}`,
        method: 'GET',
        header: paramsToHeaders(params),
        success: (res: any) => {
          const body = res.data || {}
          const profile = body.code === 0 ? (body.data || {}) : {}
          const personalInfoItems: PersonalInfoItem[] = [
            { label: '用户名', value: String(userInfo.username || profile.username || '--') },
            { label: '昵称', value: String(profile.nickname || userInfo.nickname || '--') },
            { label: '手机号', value: maskPhone(String(profile.phone || userInfo.phone || '')) },
            { label: '身份类型', value: String(profile.identity_type || this.data.identityType || '--') },
            { label: '会员等级', value: String(userInfo.vip_level || userInfo.member_level || profile.vip_level || '--') },
            { label: '账号 ID', value: String(userInfo.id || profile.user_id || '--') },
            { label: '是否设置密码', value: profile.has_password ? '已设置' : '未设置' },
            { label: '企业微信验证', value: profile.enterprise_wechat_verified ? '已验证' : '未验证' },
          ]
          this.setData({ personalInfoItems })
          resolve()
        },
        fail: (error) => {
          console.error('加载个人信息失败:', error)
          resolve()
        },
      })
    })
  },

  onViewPersonalInfo() {
    if (!this.ensureLoggedIn()) {
      return
    }
    this.loadPersonalInfo()
    this.setData({ personalInfoVisible: true })
  },

  closePersonalInfo() {
    this.setData({ personalInfoVisible: false })
  },

  buildPersonalInfoText() {
    return (this.data.personalInfoItems || [])
      .map((item) => `${item.label}：${item.value}`)
      .join('\n')
  },

  onCopyPersonalInfo() {
    if (!this.ensureLoggedIn()) {
      return
    }
    const text = this.buildPersonalInfoText()
    wx.setClipboardData({
      data: text,
      success: () => {
        wx.showToast({ title: '个人信息已复制', icon: 'success' })
      },
    })
  },

  async onDownloadPersonalPackage() {
    if (!this.ensureLoggedIn()) {
      return
    }

    wx.showLoading({ title: '打包中...' })
    try {
      await this.loadPersonalInfo()
      const userInfo = wx.getStorageSync('userInfo') || {}
      const privacySettings = wx.getStorageSync(PRIVACY_SETTINGS_STORAGE_KEY) || DEFAULT_PRIVACY_SETTINGS
      const packageData = {
        exported_at: new Date().toISOString(),
        company: '甲第灵光（福建）科技有限公司',
        personal_info: this.data.personalInfoItems,
        account_snapshot: {
          username: userInfo.username || '',
          user_id: userInfo.id || '',
          identity_type: this.data.identityType || '',
        },
        privacy_settings: privacySettings,
        history_entry_points: {
          generate_records: '/pages/generatehistory/generatehistory',
          order_records: '/pages/ordermanagement/ordermanagement',
          wallet_records: '/pages/lingshidetails/lingshidetails',
        },
        support_email: SUPPORT_EMAIL,
      }
      const content = JSON.stringify(packageData, null, 2)
      await new Promise<void>((resolve, reject) => {
        wx.setClipboardData({
          data: content,
          success: () => resolve(),
          fail: reject,
        })
      })
      wx.showModal({
        title: '打包完成',
        content: '你的个人信息摘要已复制到剪贴板，可粘贴保存。历史图纸、视频和账单请通过页面入口继续查阅。',
        showCancel: false,
      })
    } catch (error) {
      console.error('打包个人信息失败:', error)
      wx.showToast({ title: '打包失败，请稍后重试', icon: 'none' })
    } finally {
      wx.hideLoading()
    }
  },

  onOpenGenerateHistory() {
    if (!this.ensureLoggedIn()) {
      return
    }
    wx.navigateTo({
      url: '/pages/generatehistory/generatehistory',
      fail: () => wx.showToast({ title: '页面跳转失败', icon: 'none' }),
    })
  },

  onOpenOrderRecords() {
    if (!this.ensureLoggedIn()) {
      return
    }
    wx.navigateTo({
      url: '/pages/ordermanagement/ordermanagement',
      fail: () => wx.showToast({ title: '页面跳转失败', icon: 'none' }),
    })
  },

  onOpenWalletRecords() {
    if (!this.ensureLoggedIn()) {
      return
    }
    wx.navigateTo({
      url: '/pages/lingshidetails/lingshidetails',
      fail: () => wx.showToast({ title: '页面跳转失败', icon: 'none' }),
    })
  },

  openAccountRequest(type: 'delete' | 'deactivate') {
    if (!this.ensureLoggedIn()) {
      return
    }
    this.setData({
      accountRequestVisible: true,
      accountRequestType: type,
      accountRequestReason: '',
    })
  },

  onOpenDeleteRequest() {
    this.openAccountRequest('delete')
  },

  onOpenDeactivateRequest() {
    this.openAccountRequest('deactivate')
  },

  closeAccountRequest() {
    this.setData({
      accountRequestVisible: false,
      accountRequestReason: '',
      accountRequestSubmitting: false,
    })
  },

  onAccountRequestInput(e: any) {
    this.setData({ accountRequestReason: String(e.detail.value || '') })
  },

  onSubmitAccountRequest() {
    if (!this.ensureLoggedIn() || this.data.accountRequestSubmitting) {
      return
    }

    const reason = String(this.data.accountRequestReason || '').trim()
    if (!reason) {
      wx.showToast({ title: '请填写申请说明', icon: 'none' })
      return
    }

    const userInfo = wx.getStorageSync('userInfo') || {}
    const requestType = this.data.accountRequestType
    const requestRecord = {
      type: requestType,
      reason,
      username: String(userInfo.username || ''),
      user_id: String(userInfo.id || ''),
      submitted_at: new Date().toISOString(),
      status: 'pending',
    }

    this.setData({ accountRequestSubmitting: true })
    try {
      const existing = wx.getStorageSync(ACCOUNT_REQUEST_STORAGE_KEY) || []
      const nextRecords = Array.isArray(existing) ? existing : []
      nextRecords.unshift(requestRecord)
      wx.setStorageSync(ACCOUNT_REQUEST_STORAGE_KEY, nextRecords.slice(0, 20))

      const title = requestType === 'delete' ? '删除申请已提交' : '注销申请已提交'
      wx.showModal({
        title,
        content: `我们已记录你的申请，客服将在 1-3 个工作日内处理。你也可以发送邮件至 ${SUPPORT_EMAIL} 补充说明。`,
        showCancel: false,
        success: () => {
          this.closeAccountRequest()
        },
      })
    } catch (error) {
      console.error('提交账号申请失败:', error)
      wx.showToast({ title: '提交失败，请稍后重试', icon: 'none' })
      this.setData({ accountRequestSubmitting: false })
    }
  },

  onIdentityPickerChange(e: any) {
    const index = Number(e.detail.value || 0)
    const identityType = USER_IDENTITY_OPTIONS[index]
    if (!identityType || this.data.identitySaving) {
      return
    }
    const token = wx.getStorageSync('token')
    if (!token) {
      wx.showToast({ title: '请先登录', icon: 'none' })
      return
    }
    this.setData({ identitySaving: true })
    const deviceID = getCachedDeviceFingerprint() || ''
    const apiPath = '/api/v1/miniprogram/profile/identity'
    const body = { identity_type: identityType }
    const params = generateRequestParams(token, body, apiPath, deviceID)
    const headers = {
      ...paramsToHeaders(params),
      'Content-Type': 'application/json',
    }
    wx.request({
      url: `${API_BASE_URL}${apiPath}`,
      method: 'PUT',
      data: body,
      header: headers,
      success: (res: any) => {
        const body = res.data || {}
        if (res.statusCode === 200 && body.code === 0) {
          const userInfo = wx.getStorageSync('userInfo') || {}
          wx.setStorageSync('userInfo', { ...userInfo, identity_type: identityType })
          this.setData({ identityType, identityTypeIndex: index })
          this.loadPersonalInfo()
          wx.showToast({ title: '身份已更新', icon: 'success' })
          return
        }
        wx.showToast({ title: body.msg || '保存失败', icon: 'none' })
      },
      fail: () => {
        wx.showToast({ title: '保存失败', icon: 'none' })
      },
      complete: () => {
        this.setData({ identitySaving: false })
      },
    })
  },

  initCacheSize() {
    try {
      wx.getStorageInfo({
        success: (res) => {
          const sizeKB = Number(res?.currentSize || 0)
          const sizeMB = (sizeKB / 1024).toFixed(2)
          this.setData({
            cacheSize: `${sizeMB}MB`,
          })
        },
        fail: (e) => {
          console.error('获取缓存大小失败:', e)
        },
      })
    } catch (e) {
      console.error('获取缓存大小失败:', e)
    }
  },

  goToProfile() {
    if (!this.ensureLoggedIn()) {
      return
    }
    wx.navigateTo({
      url: '/pages/myInformationmodification/myInformationmodification?source=settings&section=profile',
      fail: () => {
        wx.showToast({
          title: '页面跳转失败',
          icon: 'none',
        })
      },
    })
  },

  goToPasswordSecurity() {
    if (!this.ensureLoggedIn()) {
      return
    }
    wx.navigateTo({
      url: '/pages/myInformationmodification/myInformationmodification?source=settings&section=profile',
      fail: () => {
        wx.showToast({
          title: '页面跳转失败',
          icon: 'none',
        })
      },
    })
  },

  goToPasswordRecovery() {
    const userInfo = wx.getStorageSync('userInfo') || {}
    const username = String(userInfo.username || '')
    const query = username ? `?username=${encodeURIComponent(username)}` : ''
    wx.navigateTo({
      url: `/pages/passwordrecovery/passwordrecovery${query}`,
      fail: () => {
        wx.showToast({
          title: '页面跳转失败',
          icon: 'none',
        })
      },
    })
  },

  onToggleDarkMode(e: any) {
    const value = e.detail.value
    this.setData({ darkMode: value })
    wx.setStorageSync(SETTINGS_DARK_MODE_STORAGE_KEY, value)
    wx.showToast({
      title: value ? '深色模式已开启' : '深色模式已关闭',
      icon: 'none',
    })
  },

  onClearCache() {
    wx.showModal({
      title: '清理缓存',
      content: '确定要清理临时缓存吗？系统会保留登录状态、账号资料和主题设置。',
      success: (res) => {
        if (res.confirm) {
          const token = wx.getStorageSync('token')
          const userInfo = wx.getStorageSync('userInfo')
          const darkMode = wx.getStorageSync(SETTINGS_DARK_MODE_STORAGE_KEY)
          const privacySettings = wx.getStorageSync(PRIVACY_SETTINGS_STORAGE_KEY)

          wx.clearStorage({
            success: () => {
              if (token) {
                wx.setStorageSync('token', token)
              }
              if (userInfo) {
                wx.setStorageSync('userInfo', userInfo)
              }
              if (darkMode !== '') {
                wx.setStorageSync(SETTINGS_DARK_MODE_STORAGE_KEY, darkMode)
              }
              if (privacySettings) {
                wx.setStorageSync(PRIVACY_SETTINGS_STORAGE_KEY, privacySettings)
              }

              wx.showToast({
                title: '缓存已清理',
                icon: 'none',
              })

              this.initCacheSize()
            },
          })
        }
      },
    })
  },

  openPolicyPage(type: 'privacy' | 'agreement' | 'llm') {
    wx.navigateTo({
      url: `/pages/privacy/privacy?from=settings&type=${type}`,
      fail: () => {
        wx.showToast({
          title: '页面跳转失败',
          icon: 'none',
        })
      },
    })
  },

  onViewPrivacy() {
    this.openPolicyPage('privacy')
  },

  onViewLlmAgreement() {
    this.openPolicyPage('llm')
  },

  onViewAbout() {
    wx.showModal({
      title: '关于甲第灵光',
      content:
        '甲第灵光（福建）科技有限公司，专注于设计智能方案与AI创意工具，为设计师和业主提供高效、专业的设计体验。',
      showCancel: false,
    })
  },

  onShareAppMessage() {
    return {
      title: '设置',
      path: '/pages/settings/settings',
    }
  },
})
