// pages/buildingreplacement/buildingreplacement.ts
import { generateRequestParams, paramsToHeaders } from '../../utils/parameter';
import { getCachedDeviceFingerprint } from '../../utils/deviceFingerprint';
import { sanitizeAIGenerationErrorMessage } from '../../utils/aiError';

// API基础地址
const API_BASE_URL = 'https://api.jiadilingguang.com';

function getTaskStatusPollDelay(attempt: number): number {
  if (attempt < 2) {
    return 3000;
  }
  if (attempt < 6) {
    return 4000;
  }
  return 5000;
}

Page({
  /**
   * 页面的初始数据
   */
  data: {
    facadeImage: '', // 外立面图片
    interiorImage: '', // 内部图片
    selectedDirection: 'facade', // 改造方向
    directions: [
      { label: '外立面改造', value: 'facade' },
      { label: '空间美化', value: 'beautification' },
      { label: '文化创雕复兴', value: 'cultural' },
      { label: '整体改造', value: 'overall' }
    ],
    selectedMaterial: 'modern', // 选中的材料/风格
    materials: [
      { icon: 'lightbulb', label: '现代简约', value: 'modern' },
      { icon: 'window', label: '现代材料', value: 'modern-material' },
      { icon: 'tools', label: '复古砖饰', value: 'retro-brick' },
      { icon: 'tools', label: '节能材料', value: 'energy-saving' },
      { icon: 'tools', label: '金属材料', value: 'metal' },
      { icon: 'building', label: '混凝土', value: 'concrete' },
      { icon: 'tree', label: '木质材料', value: 'wood' },
      { icon: 'brush', label: '装饰材料', value: 'decorative' },
      { icon: 'home', label: '新中式', value: 'chinese' }
    ],
    comparisonImages: [] as any[], // 对比图列表 [{before: '', after: ''}]
    customPrompt: '', // 自定义提示词
    optimizingPrompt: false,
    generating: false // 是否正在生成
  },

  /**
   * 生命周期函数--监听页面加载
   */
  onLoad() {

  },

  /**
   * 上传外立面
   */
  onUploadFacade() {
    wx.chooseMedia({
      count: 1,
      mediaType: ['image'],
      sourceType: ['camera', 'album'],
      success: (res) => {
        const imagePath = res.tempFiles[0].tempFilePath
        this.setData({
          facadeImage: imagePath
        })
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
   * 删除外立面
   */
  onDeleteFacade() {
    this.setData({
      facadeImage: ''
    })
  },

  /**
   * 上传内部
   */
  onUploadInterior() {
    wx.chooseMedia({
      count: 1,
      mediaType: ['image'],
      sourceType: ['camera', 'album'],
      success: (res) => {
        const imagePath = res.tempFiles[0].tempFilePath
        this.setData({
          interiorImage: imagePath
        })
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
   * 删除内部
   */
  onDeleteInterior() {
    this.setData({
      interiorImage: ''
    })
  },

  /**
   * 选择改造方向
   */
  onSelectDirection(e: any) {
    const value = e.currentTarget.dataset.value
    this.setData({
      selectedDirection: value
    })
  },

  /**
   * 选择材料/风格
   */
  onSelectMaterial(e: any) {
    const value = e.currentTarget.dataset.value
    this.setData({
      selectedMaterial: value
    })
  },

  /**
   * 自定义提示词输入
   */
  onCustomPromptInput(e: any) {
    this.setData({
      customPrompt: e.detail.value
    })
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
   * AI生成改造方案
   */
  async onGenerate() {
    // 检查必填项
    if (!this.data.facadeImage && !this.data.interiorImage) {
      wx.showToast({
        title: '请至少上传一张图片',
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

    this.setData({ generating: true })
    wx.showLoading({
      title: '正在生成...',
    })

    try {
      // 上传图片到OSS
      const facadeImageUrl = this.data.facadeImage
        ? (this.data.facadeImage.startsWith('http://') || this.data.facadeImage.startsWith('https://'))
          ? this.data.facadeImage
          : await this.uploadImageToOSS(this.data.facadeImage, token) || ''
        : ''

      const interiorImageUrl = this.data.interiorImage
        ? (this.data.interiorImage.startsWith('http://') || this.data.interiorImage.startsWith('https://'))
          ? this.data.interiorImage
          : await this.uploadImageToOSS(this.data.interiorImage, token) || ''
        : ''

      // 构建请求数据
      const requestData = {
        facade_image: facadeImageUrl,
        interior_image: interiorImageUrl,
        direction: this.data.selectedDirection,
        material: this.data.selectedMaterial,
        custom_prompt: this.data.customPrompt
      }

      // 调用后端API
      const apiPath = '/api/v1/miniprogram/ai/building-replacement'

      // 获取设备ID
      const deviceID = getCachedDeviceFingerprint() || '';

      // 生成请求参数
      const params = generateRequestParams(token, requestData, apiPath, deviceID);
      const headers = paramsToHeaders(params);

      const res = await new Promise<any>((resolve, reject) => {
        wx.request({
          url: `${API_BASE_URL}${apiPath}`,
          method: 'POST',
          header: {
            'token': headers.token,
            'token-signature': headers['token-signature'],
            'sin': headers.sin,
            'md5-signature': headers['md5-signature'],
            'pass': headers.pass,
            'tm': headers.tm,
            'Content-Type': 'application/json',
          },
          data: requestData,
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

      // 保存任务编号，用于后续轮询
      const taskNo = res.task_no
      if (taskNo) {
        // 开始轮询任务状态
        this.pollTaskStatus(taskNo, facadeImageUrl, interiorImageUrl)
      } else {
        this.setData({ generating: false })
        wx.showToast({
          title: '任务提交失败',
          icon: 'none'
        })
      }

    } catch (error: any) {
      wx.hideLoading()
      this.setData({ generating: false })
      console.error('生成失败:', error)
      wx.showToast({
        title: sanitizeAIGenerationErrorMessage(error.message || '生成失败'),
        icon: 'none',
        duration: 2000
      })
    }
  },

  /**
   * 轮询任务状态
   */
  async pollTaskStatus(taskNo: string, facadeImageUrl: string, interiorImageUrl: string) {
    const token = wx.getStorageSync('token')
    let pollCount = 0
    const maxPolls = 30

    const poll = async () => {
      if (pollCount >= maxPolls) {
        this.setData({ generating: false })
        wx.showToast({
          title: '生成超时，请稍后查看',
          icon: 'none'
        })
        return
      }

      try {
        const requestData = {
          task_no: taskNo,
          task_type: 'ai_draw'
        }

        // 获取设备ID
        const deviceID = getCachedDeviceFingerprint() || '';

        // 生成请求参数
        const apiPath = '/api/v1/miniprogram/ai/task/status';
        const params = generateRequestParams(token, requestData, apiPath, deviceID);
        const headers = paramsToHeaders(params);

        const res = await new Promise<any>((resolve, reject) => {
          wx.request({
            url: `${API_BASE_URL}${apiPath}`,
            method: 'POST',
            header: {
              'token': headers.token,
              'token-signature': headers['token-signature'],
              'sin': headers.sin,
              'md5-signature': headers['md5-signature'],
              'pass': headers.pass,
              'tm': headers.tm,
              'Content-Type': 'application/json',
            },
            data: requestData,
            success: (res) => {
              if (res.statusCode === 200 && res.data) {
                const data = res.data as any
                if (data.code === 0) {
                  resolve(data.data)
                } else {
                  reject(new Error(data.msg || '查询失败'))
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

        if (res.status === 'success' && res.result) {
          // 任务完成
          this.setData({ generating: false })
          wx.hideLoading()

          // 解析结果
          let resultUrl = ''
          if (typeof res.result === 'string') {
            try {
              const resultData = JSON.parse(res.result)
              resultUrl = resultData.url || resultData.url_raw || ''
            } catch (e) {
              resultUrl = res.result
            }
          } else if (res.result.url) {
            resultUrl = res.result.url
          } else if (res.result.url_raw) {
            resultUrl = res.result.url_raw
          }

          if (resultUrl) {
            // 添加对比图
            const comparisonImages = this.data.comparisonImages || []
            const beforeImage = facadeImageUrl || interiorImageUrl
            comparisonImages.push({
              before: beforeImage,
              after: resultUrl
            })
            this.setData({
              comparisonImages: comparisonImages
            })

            wx.showToast({
              title: '生成成功',
              icon: 'success'
            })
          } else {
            wx.showToast({
              title: '生成完成，但未获取到结果',
              icon: 'none'
            })
          }
        } else if (res.status === 'failed') {
          // 任务失败
          this.setData({ generating: false })
          wx.hideLoading()
          wx.showToast({
            title: sanitizeAIGenerationErrorMessage(res.error_message || res.error || '生成失败'),
            icon: 'none'
          })
        } else {
          // 继续轮询
          const nextDelay = getTaskStatusPollDelay(pollCount)
          pollCount++
          setTimeout(poll, nextDelay)
        }
      } catch (error: any) {
        // 继续轮询
        const nextDelay = getTaskStatusPollDelay(pollCount)
        pollCount++
        setTimeout(poll, nextDelay)
      }
    }

    poll()
  },

  /**
   * 上传图片到OSS
   */
  async uploadImageToOSS(imagePath: string, token: string): Promise<string | null> {
    try {
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
  },

  /**
   * 用户点击右上角分享
   */
  onShareAppMessage() {
    return {
      title: '城市建筑焕新',
      path: '/pages/buildingreplacement/buildingreplacement'
    }
  }
})
