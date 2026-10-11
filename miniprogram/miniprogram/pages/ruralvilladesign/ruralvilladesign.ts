// pages/ruralvilladesign/ruralvilladesign.ts
import { generateRequestParams, paramsToHeaders } from '../../utils/parameter';
import { getCachedDeviceFingerprint } from '../../utils/deviceFingerprint';
import { sanitizeAIGenerationErrorMessage } from '../../utils/aiError';

Page({
  /**
   * 页面的初始数据
   */
  data: {
    selectedTemplate: '100',
    templates: [
      { label: '100m²', value: '100' },
      { label: '120m²', value: '120' },
      { label: '150m²', value: '150' },
      { label: '200m²', value: '200' }
    ],
    customLength: '5',
    customWidth: '0',
    customArea: '600',
    sizeImages: [] as string[], // 尺寸选择上传的图片
    selectedStyle: '', // 选中的风格
    styleOptions: [
      { label: '现代简约', value: 'modern' },
      { label: '新中式', value: 'chinese' },
      { label: '新闽派', value: 'min' },
      { label: '传统古建', value: 'traditional' },
      { label: '海派', value: 'shanghai' },
      { label: '田园风', value: 'countryside' }
    ],
    plotDescription: '', // 地块描述
    plotRequirements: '', // 特殊要求
    customPrompt: '', // 自定义提示词
    optimizingPrompt: false,
    referenceImages: [] as string[], // 参考图片
    expandedSections: {
      size: true, // 默认展开第一步
      style: false,
      plot: false,
      reference: false,
      prompt: false
    },
    stepCompleted: {
      size: false,
      style: false,
      plot: false,
      reference: false,
      prompt: false
    },
    allStepsCompleted: false // 所有步骤是否完成
  },

  /**
   * 生命周期函数--监听页面加载
   */
  onLoad() {

  },

  /**
   * 生命周期函数--监听页面初次渲染完成
   */
  onReady() {

  },

  /**
   * 生命周期函数--监听页面显示
   */
  onShow() {

  },

  /**
   * 生命周期函数--监听页面隐藏
   */
  onHide() {

  },

  /**
   * 生命周期函数--监听页面卸载
   */
  onUnload() {

  },

  /**
   * 页面相关事件处理函数--监听用户下拉动作
   */
  onPullDownRefresh() {

  },

  /**
   * 页面上拉触底事件的处理函数
   */
  onReachBottom() {

  },

  /**
   * 用户点击右上角分享
   */
  onShareAppMessage() {
    return {
      title: '乡村别墅AI设计',
      path: '/pages/ruralvilladesign/ruralvilladesign'
    }
  },

  /**
   * 选择模板
   */
  onSelectTemplate(e: any) {
    const value = e.currentTarget.dataset.value
    this.setData({
      selectedTemplate: value
    })
    this.checkStepCompleted('size')
    // 自动展开下一步
    if (!this.data.expandedSections.style) {
      this.setData({
        'expandedSections.style': true
      })
    }
  },

  /**
   * 长度输入
   */
  onLengthInput(e: any) {
    const value = e.detail.value
    this.setData({
      customLength: value
    })
    // 自动计算面积
    this.calculateArea()
    this.checkStepCompleted('size')
  },

  /**
   * 宽度输入
   */
  onWidthInput(e: any) {
    const value = e.detail.value
    this.setData({
      customWidth: value
    })
    // 自动计算面积
    this.calculateArea()
    this.checkStepCompleted('size')
  },

  /**
   * 面积输入
   */
  onAreaInput(e: any) {
    const value = e.detail.value
    this.setData({
      customArea: value
    })
    this.checkStepCompleted('size')
  },

  /**
   * 计算面积
   */
  calculateArea() {
    const length = parseFloat(this.data.customLength) || 0
    const width = parseFloat(this.data.customWidth) || 0
    if (length > 0 && width > 0) {
      const area = length * width
      this.setData({
        customArea: area.toString()
      })
    }
  },

  /**
   * 上传尺寸相关图片
   */
  onUploadSize() {
    wx.chooseMedia({
      count: 9,
      mediaType: ['image'],
      sourceType: ['camera', 'album'],
      success: (res) => {
        const tempFiles = res.tempFiles.map(file => file.tempFilePath)
        const currentImages = this.data.sizeImages || []
        const newImages = [...currentImages, ...tempFiles].slice(0, 9) // 最多9张
        this.setData({
          sizeImages: newImages
        })
        this.checkStepCompleted('size')
      },
      fail: (err) => {
        console.error('选择图片失败', err)
        wx.showToast({
          title: '选择图片失败',
          icon: 'none'
        })
      }
    })
  },

  /**
   * 删除尺寸图片
   */
  onDeleteSizeImage(e: any) {
    const index = e.currentTarget.dataset.index
    const images = this.data.sizeImages
    images.splice(index, 1)
    this.setData({
      sizeImages: images
    })
    this.checkStepCompleted('size')
  },

  /**
   * 选择风格
   */
  onSelectStyle(e: any) {
    const value = e.currentTarget.dataset.value
    this.setData({
      selectedStyle: value
    })
    this.checkStepCompleted('style')
    // 自动展开下一步
    if (!this.data.expandedSections.plot) {
      this.setData({
        'expandedSections.plot': true
      })
    }
  },

  /**
   * 地块描述输入
   */
  onPlotDescriptionInput(e: any) {
    this.setData({
      plotDescription: e.detail.value
    })
    this.checkStepCompleted('plot')
  },

  /**
   * 特殊要求输入
   */
  onPlotRequirementsInput(e: any) {
    this.setData({
      plotRequirements: e.detail.value
    })
    this.checkStepCompleted('plot')
  },

  /**
   * 自定义提示词输入
   */
  onCustomPromptInput(e: any) {
    this.setData({
      customPrompt: e.detail.value
    })
    // 自定义提示词为可选步骤，不需要检查完成状态
  },

  async onOptimizePrompt() {
    if (this.data.optimizingPrompt) return;
    const rawPrompt = (this.data.customPrompt || '').trim();
    if (!rawPrompt) {
      wx.showToast({ title: '请先输入提示词', icon: 'none' });
      return;
    }
    this.setData({ optimizingPrompt: true });
    const { optimizePrompt } = require('../../utils/promptOptimize');
    const result = await optimizePrompt(rawPrompt, 'architecture');
    this.setData({ optimizingPrompt: false });
    if (result.success && result.optimizedPrompt) {
      this.setData({ customPrompt: result.optimizedPrompt });
      wx.showToast({ title: '优化完成', icon: 'success' });
    } else {
      if (result.needMembership) {
        wx.showModal({ title: '会员专属', content: result.error || '请开通会员后使用AI优化', confirmText: '去开通', success: (res) => { if (res.confirm) wx.navigateTo({ url: '/pages/topupcenter/topupcenter' }); } });
      } else {
        wx.showToast({ title: result.error || '优化失败', icon: 'none' });
      }
    }
  },

  /**
   * 上传参考图
   */
  onUploadReference() {
    wx.chooseMedia({
      count: 9,
      mediaType: ['image'],
      sourceType: ['camera', 'album'],
      success: (res) => {
        const tempFiles = res.tempFiles.map(file => file.tempFilePath)
        const currentImages = this.data.referenceImages || []
        const newImages = [...currentImages, ...tempFiles].slice(0, 9) // 最多9张
        this.setData({
          referenceImages: newImages
        })
        this.checkStepCompleted('reference')
      },
      fail: (err) => {
        console.error('选择图片失败', err)
        wx.showToast({
          title: '选择图片失败',
          icon: 'none'
        })
      }
    })
  },

  /**
   * 删除参考图
   */
  onDeleteReferenceImage(e: any) {
    const index = e.currentTarget.dataset.index
    const images = this.data.referenceImages
    images.splice(index, 1)
    this.setData({
      referenceImages: images
    })
    this.checkStepCompleted('reference')
  },

  /**
   * 切换折叠区域
   */
  onToggleSection(e: any) {
    const section = e.currentTarget.dataset.section as keyof typeof this.data.expandedSections
    const expanded = this.data.expandedSections[section]
    this.setData({
      [`expandedSections.${section}`]: !expanded
    })
  },

  /**
   * 检查步骤是否完成
   */
  checkStepCompleted(step: string) {
    let completed = false
    const stepCompleted: any = { ...this.data.stepCompleted }

    if (step === 'size') {
      // 尺寸选择完成：选择了模板或输入了自定义尺寸
      completed = !!this.data.selectedTemplate ||
        (!!this.data.customLength && parseFloat(this.data.customLength) > 0) ||
        (!!this.data.customArea && parseFloat(this.data.customArea) > 0)
    } else if (step === 'style') {
      // 风格选择完成：选择了风格
      completed = !!this.data.selectedStyle
    } else if (step === 'plot') {
      // 地块补充完成：至少填写了描述或要求
      completed = !!(this.data.plotDescription || this.data.plotRequirements)
    } else if (step === 'reference') {
      // 参考图上传完成：至少上传了一张图片（可选，也可以不完成）
      completed = this.data.referenceImages.length > 0
    }

    stepCompleted[step] = completed
    this.setData({
      stepCompleted: stepCompleted
    })

    // 检查所有步骤是否完成
    this.checkAllStepsCompleted()
  },

  /**
   * 检查所有步骤是否完成
   */
  checkAllStepsCompleted() {
    const completed = this.data.stepCompleted
    // 至少完成前两步（尺寸选择和风格选择）才显示AI生成按钮
    const allCompleted = completed.size && completed.style
    this.setData({
      allStepsCompleted: allCompleted
    })
  },

  /**
   * AI生成设计
   */
  async onGenerateDesign() {
    if (!this.data.allStepsCompleted) {
      wx.showToast({
        title: '请完成所有必填步骤',
        icon: 'none'
      })
      return
    }

    const token = wx.getStorageSync('token')
    if (!token) {
      wx.showToast({
        title: '请先登录',
        icon: 'none'
      })
      return
    }

    wx.showLoading({
      title: '正在生成设计...',
    })

    try {
      // 先上传图片到OSS（如果有）
      const sizeImageUrls: string[] = []
      const referenceImageUrls: string[] = []

      // 上传尺寸图片（如果还没有上传到OSS）
      for (const imagePath of this.data.sizeImages) {
        // 如果已经是URL，直接使用；否则上传
        if (imagePath.startsWith('http://') || imagePath.startsWith('https://')) {
          sizeImageUrls.push(imagePath)
        } else {
          const url = await this.uploadImageToOSS(imagePath, token)
          if (url) {
            sizeImageUrls.push(url)
          }
        }
      }

      // 上传参考图片（如果还没有上传到OSS）
      for (const imagePath of this.data.referenceImages) {
        // 如果已经是URL，直接使用；否则上传
        if (imagePath.startsWith('http://') || imagePath.startsWith('https://')) {
          referenceImageUrls.push(imagePath)
        } else {
          const url = await this.uploadImageToOSS(imagePath, token)
          if (url) {
            referenceImageUrls.push(url)
          }
        }
      }

      // 构建请求数据
      const designData = {
        template: this.data.selectedTemplate,
        custom_length: this.data.customLength,
        custom_width: this.data.customWidth,
        custom_area: this.data.customArea,
        size_images: sizeImageUrls,
        style: this.data.selectedStyle,
        plot_description: this.data.plotDescription,
        plot_requirements: this.data.plotRequirements,
        custom_prompt: this.data.customPrompt,
        reference_images: referenceImageUrls
      }

      // 调用后端API
      const API_BASE_URL = 'https://api.jiadilingguang.com'
      const apiPath = '/api/v1/miniprogram/ai/rural-villa-design'

      // 获取设备ID
      const deviceID = getCachedDeviceFingerprint() || '';

      // 生成请求参数
      const params = generateRequestParams(token, designData, apiPath, deviceID);
      const headers = paramsToHeaders(params);

      await new Promise<any>((resolve, reject) => {
        wx.request({
          url: `${API_BASE_URL}${apiPath}`,
          method: 'POST',
          header: {
            ...headers,
            'Content-Type': 'application/json',
          },
          data: designData,
          success: (res) => {
            if (res.statusCode === 200 && res.data) {
              const data = res.data as any
              if (data.code === 0) {
                resolve(data.data)
              } else {
                reject(new Error(data.msg || '生成失败'))
              }
            } else {
              reject(new Error(`请求失败: ${res.statusCode}`))
            }
          },
          fail: (err) => {
            reject(err)
          },
        })
      })

      wx.hideLoading()

      // 显示任务提交成功提示
      wx.showToast({
        title: '任务已提交',
        icon: 'success',
        duration: 2000
      })

    } catch (error: any) {
      wx.hideLoading()
      console.error('生成设计失败:', error)
      wx.showToast({
        title: sanitizeAIGenerationErrorMessage(error.message || '生成失败'),
        icon: 'none',
        duration: 2000
      })
    }
  },

  /**
   * 上传图片到OSS
   */
  async uploadImageToOSS(imagePath: string, token: string): Promise<string | null> {
    try {
      const API_BASE_URL = 'https://api.jiadilingguang.com'
      const apiPath = '/api/v1/miniprogram/ai/upload-reference-image'

      // 获取设备ID
      const deviceID = getCachedDeviceFingerprint() || '';

      // 生成请求参数（文件上传时使用空对象作为body）
      const params = generateRequestParams(token, '{}', apiPath, deviceID);
      const headers = paramsToHeaders(params);

      return new Promise((resolve, reject) => {
        wx.uploadFile({
          url: `${API_BASE_URL}${apiPath}`,
          filePath: imagePath,
          name: 'file',
          header: headers,
          success: (res) => {
            try {
              const data = JSON.parse(res.data) as any
              if (data.code === 0 && data.data && data.data.url) {
                resolve(data.data.url)
              } else {
                reject(new Error(data.msg || '上传失败'))
              }
            } catch (e) {
              reject(new Error('解析响应失败'))
            }
          },
          fail: (err) => {
            reject(err)
          },
        })
      })
    } catch (error) {
      console.error('上传图片失败:', error)
      return null
    }
  }
})