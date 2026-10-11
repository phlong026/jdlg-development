// logs.ts
// const util = require('../../utils/util.js')
import { formatTime } from '../../utils/util'

Component({
  data: {
    logs: [],
  },
  lifetimes: {
    attached() {
      this.setData({
        logs: (wx.getStorageSync('logs') || []).map((log: string) => {
          return {
            date: formatTime(new Date(log)),
            timeStamp: log
          }
        }),
      })
    }
  },
  methods: {
    onShareAppMessage() {
      return {
        title: '甲第灵光 · 使用日志',
        path: '/pages/logs/logs',
      };
    },
  },
})
