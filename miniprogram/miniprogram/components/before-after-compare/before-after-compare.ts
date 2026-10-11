// 触摸移动节流间隔（毫秒），避免 setData 过于频繁导致低端机卡顿
const THROTTLE_MS = 16

Component({
  properties: {
    beforeImage: { type: String, value: '' },
    afterImage: { type: String, value: '' },
    beforeLabel: { type: String, value: 'Before' },
    afterLabel: { type: String, value: 'After' },
    initialPercent: { type: Number, value: 50 },
    height: { type: String, value: '420rpx' },
  },

  data: {
    dividerPercent: 50,
    containerLeft: 0,
    containerWidth: 0,
    imageError: false,
    computedHeight: '420rpx',
  },

  observers: {
    initialPercent(value: number) {
      this.setData({
        dividerPercent: this.clampPercent(value),
      })
    },
    height(value: string) {
      // height 非 auto 时直接使用传入值
      if (String(value || '').trim() !== 'auto') {
        this.setData({ computedHeight: value || '420rpx' })
      }
    },
  },

  lifetimes: {
    attached() {
      // 非响应式实例属性：拖拽标记 & 节流时间戳
      ;(this as any)._dragged = false
      ;(this as any)._lastMoveTime = 0
      ;(this as any)._lastPageX = 0
    },
    ready() {
      const h = String(this.properties.height || '420rpx').trim()
      this.setData({
        dividerPercent: this.clampPercent(Number(this.properties.initialPercent || 50)),
        computedHeight: h === 'auto' ? '420rpx' : h,
      })
      this.measureContainer()
    },
  },

  methods: {
    clampPercent(value: number) {
      const safeValue = Number.isFinite(Number(value)) ? Number(value) : 50
      if (safeValue < 0) {
        return 0
      }
      if (safeValue > 100) {
        return 100
      }
      return safeValue
    },

    measureContainer() {
      const query = this.createSelectorQuery()
      query.select('.before-after-compare').boundingClientRect()
      query.exec((result) => {
        const rect = result && result[0]
        if (!rect) {
          return
        }
        this.setData({
          containerLeft: Number(rect.left || 0),
          containerWidth: Number(rect.width || 0),
        })
      })
    },

    updateDividerByPageX(pageX: number) {
      const containerWidth = Number(this.data.containerWidth || 0)
      if (containerWidth <= 0) {
        return
      }
      const containerLeft = Number(this.data.containerLeft || 0)
      const relativeX = Number(pageX || 0) - containerLeft
      const percent = (relativeX / containerWidth) * 100
      this.setData({
        dividerPercent: this.clampPercent(percent),
      })
    },

    // touchStart：先异步测量容器，测量完成后再更新滑块位置，避免竞态
    onTouchStart(event: WechatMiniprogram.TouchEvent) {
      ;(this as any)._dragged = false
      ;(this as any)._lastMoveTime = 0
      const touch = event.touches && event.touches[0]
      if (!touch) {
        return
      }
      const pageX = Number(touch.pageX || 0)
      ;(this as any)._lastPageX = pageX
      // 异步测量后再更新位置，确保 containerLeft/containerWidth 是最新值
      const query = this.createSelectorQuery()
      query.select('.before-after-compare').boundingClientRect()
      query.exec((result) => {
        const rect = result && result[0]
        if (!rect) {
          return
        }
        this.setData({
          containerLeft: Number(rect.left || 0),
          containerWidth: Number(rect.width || 0),
        })
        this.updateDividerByPageX(pageX)
      })
    },

    // touchMove：标记为拖拽 + 节流更新
    onTouchMove(event: WechatMiniprogram.TouchEvent) {
      ;(this as any)._dragged = true
      const touch = event.touches && event.touches[0]
      if (!touch) {
        return
      }
      const pageX = Number(touch.pageX || 0)
      ;(this as any)._lastPageX = pageX
      // 节流：THROTTLE_MS 内只处理一次
      const now = Date.now()
      if (now - ((this as any)._lastMoveTime || 0) < THROTTLE_MS) {
        return
      }
      ;(this as any)._lastMoveTime = now
      this.updateDividerByPageX(pageX)
    },

    // touchEnd：用最后一次触摸位置做最终更新（防止节流丢失末尾位置）
    onTouchEnd() {
      const lastPageX = (this as any)._lastPageX
      if (typeof lastPageX === 'number' && lastPageX > 0) {
        this.updateDividerByPageX(lastPageX)
      }
    },

    // 点击预览：仅在没有拖拽时触发，避免拖拽结束误触发预览
    onImageTap() {
      if ((this as any)._dragged) {
        return
      }
      const beforeImage = String(this.properties.beforeImage || '').trim()
      const afterImage = String(this.properties.afterImage || '').trim()
      const urls = [beforeImage, afterImage].filter(Boolean)
      if (urls.length === 0) {
        return
      }
      wx.previewImage({
        current: afterImage || beforeImage,
        urls,
      })
    },

    // 图片加载失败时显示错误提示
    onImageError() {
      this.setData({ imageError: true })
    },

    // 图片加载成功后：重新测量容器 + auto 模式下按图片比例计算高度
    onImageLoaded(event: any) {
      this.measureContainer()
      // auto 模式：根据图片实际宽高比计算容器高度
      if (String(this.properties.height || '').trim() === 'auto') {
        const detail = event && event.detail
        if (detail && detail.width > 0 && detail.height > 0) {
          const containerWidth = Number(this.data.containerWidth || 0)
          if (containerWidth > 0) {
            const ratio = detail.height / detail.width
            const autoHeight = Math.round(containerWidth * ratio)
            this.setData({ computedHeight: autoHeight + 'px' })
          }
        }
      }
    },
  },
})
