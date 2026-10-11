// pages/checkin/checkin.ts
const CHECKIN_API_BASE_URL = 'https://api.jiadilingguang.com'; // 根据实际情况修改
const CHECKIN_REWARD_SCHEDULE = [5, 10, 20, 25, 30, 35, 40];
const FIRST_STAGE_DAYS = [1, 2, 3, 4];
const SECOND_STAGE_DAYS = [5, 6, 7];

function getRewardByDay(day: number) {
  if (day <= 0) {
    return CHECKIN_REWARD_SCHEDULE[0];
  }
  const index = Math.min(day, CHECKIN_REWARD_SCHEDULE.length) - 1;
  return CHECKIN_REWARD_SCHEDULE[index];
}

function createProgressItems(days: number[]) {
  return days.map((day) => ({
    day,
    reward: getRewardByDay(day),
    completed: false,
    current: false,
  }));
}

function normalizeConsecutiveDays(data: any): number {
  return Number(data?.consecutive_days ?? data?.consecutiveDays ?? 0);
}

function normalizeCheckedToday(data: any): boolean {
  return Boolean(data?.checked_today ?? data?.checkedToday ?? false);
}

function getHeroBadgeSubText(consecutiveDays: number, checkedToday: boolean) {
  const remainingDays = Math.max(0, 7 - consecutiveDays);
  if (checkedToday) {
    return remainingDays > 0 ? `今日已签到，再坚持 ${remainingDays} 天解锁稳定奖励` : '已进入稳定奖励阶段，明天继续领取 40 灵石';
  }
  return remainingDays > 0 ? `再签到 ${remainingDays} 天可进入稳定奖励阶段` : '今天签到后继续保持每日 40 灵石奖励';
}

interface ProgressItem {
  day: number;
  reward: number;
  completed: boolean;
  current?: boolean;
}

Page({
  /**
   * 页面的初始数据
   */
  data: {
    firstProgress: createProgressItems(FIRST_STAGE_DAYS) as ProgressItem[],
    secondProgress: createProgressItems(SECOND_STAGE_DAYS) as ProgressItem[],
    currentConsecutiveDays: 0,
    canCheckin: true,
    checkedToday: false,
    todayReward: CHECKIN_REWARD_SCHEDULE[0],
    nextReward: CHECKIN_REWARD_SCHEDULE[1],
    progressPercent: 0,
    progressText: '再签到 7 天可进入稳定奖励阶段',
    heroBadgeSubText: '连续签到解锁更高奖励',
    loading: false,
    returnPage: '/pages/index/index',
    checkinResultVisible: false,
    lastCheckinReward: 0,
    lastCheckinDays: 0,
  },

  /**
   * 生命周期函数--监听页面加载
   */
  onLoad(options: Record<string, string>) {
    this.setData({
      returnPage: this.resolveReturnPage(options?.source),
    });
    this.loadCheckinStatus();
  },

  /**
   * 生命周期函数--监听页面显示
   */
  onShow() {
    this.loadCheckinStatus();
  },

  resolveReturnPage(source?: string) {
    const pageMap: Record<string, string> = {
      index: '/pages/index/index',
      my: '/pages/my/my',
      topupcenter: '/pages/topupcenter/topupcenter',
    };

    return pageMap[source || ''] || '/pages/index/index';
  },

  /**
   * 加载签到状态
   */
  async loadCheckinStatus() {
    try {
      const token = wx.getStorageSync('token');
      if (!token) {
        wx.showToast({
          title: '请先登录',
          icon: 'none',
        });
        return;
      }

      // 调用后端API获取签到状态
      const res = await new Promise<any>((resolve, reject) => {
        wx.request({
          url: `${CHECKIN_API_BASE_URL}/api/v1/miniprogram/checkin/status`,
          method: 'GET',
          header: {
            'token': token,
            'Content-Type': 'application/json',
          },
          success: (res) => {
            if (res.statusCode === 200 && res.data) {
              const data = res.data as any;
              if (data.code === 0) {
                resolve(data.data);
              } else {
                reject(new Error(data.msg || '获取签到状态失败'));
              }
            } else {
              const msg = (res.data as any)?.msg || `请求失败: ${res.statusCode}`;
              reject(new Error(msg));
            }
          },
          fail: (err) => {
            reject(err);
          },
        });
      });

      const consecutiveDays = normalizeConsecutiveDays(res);
      const checkedToday = normalizeCheckedToday(res);

      // 更新进度条状态
      this.updateProgressStatus(consecutiveDays, checkedToday);
      this.updateCheckinOverview(consecutiveDays, checkedToday);
      this.setData({
        currentConsecutiveDays: consecutiveDays,
        checkedToday,
        canCheckin: !checkedToday,
        checkinResultVisible: false,
      });
    } catch (error: any) {
      console.error('加载签到状态失败:', error);
      // 如果API不存在，使用默认数据
      this.updateProgressStatus(0, false);
      this.updateCheckinOverview(0, false);
    }
  },

  /**
   * 更新进度条状态
   */
  updateProgressStatus(consecutiveDays: number, checkedToday?: boolean) {
    const currentCheckedToday = typeof checkedToday === 'boolean' ? checkedToday : this.data.checkedToday;
    const currentPendingDay = currentCheckedToday ? -1 : consecutiveDays + 1;
    const firstProgress = createProgressItems(FIRST_STAGE_DAYS).map((item) => {
      return {
        ...item,
        completed: consecutiveDays >= item.day,
        current: item.day === currentPendingDay,
      };
    });

    const secondProgress = createProgressItems(SECOND_STAGE_DAYS).map((item) => {
      return {
        ...item,
        completed: consecutiveDays >= item.day,
        current: item.day === currentPendingDay,
      };
    });

    this.setData({
      firstProgress,
      secondProgress,
    });
  },

  updateCheckinOverview(consecutiveDays: number, checkedToday: boolean) {
    const todayReward = checkedToday
      ? getRewardByDay(Math.max(consecutiveDays, 1))
      : getRewardByDay(consecutiveDays + 1);
    const nextReward = getRewardByDay((checkedToday ? consecutiveDays : consecutiveDays + 1) + 1);
    const progressPercent = Math.min(100, (Math.min(consecutiveDays, 7) / 7) * 100);
    const remainingDays = Math.max(0, 7 - consecutiveDays);
    const progressText = remainingDays > 0
      ? `再签到 ${remainingDays} 天可进入每日 40 灵石稳定奖励`
      : '已进入稳定奖励阶段，后续每日签到可得 40 灵石';

    this.setData({
      todayReward,
      nextReward,
      progressPercent,
      progressText,
      heroBadgeSubText: getHeroBadgeSubText(consecutiveDays, checkedToday),
    });
  },

  closeCheckinResult() {
    this.setData({
      checkinResultVisible: false,
    });
  },

  /**
   * 执行签到
   */
  async onCheckin() {
    if (!this.data.canCheckin || this.data.loading) {
      return;
    }

    this.setData({ loading: true });

    try {
      const token = wx.getStorageSync('token');
      if (!token) {
        wx.showToast({
          title: '请先登录',
          icon: 'none',
        });
        this.setData({ loading: false });
        return;
      }

      // 调用后端API执行签到
      const res = await new Promise<any>((resolve, reject) => {
        wx.request({
          url: `${CHECKIN_API_BASE_URL}/api/v1/miniprogram/checkin`,
          method: 'POST',
          header: {
            'token': token,
            'Content-Type': 'application/json',
          },
          success: (res) => {
            if (res.statusCode === 200 && res.data) {
              const data = res.data as any;
              if (data.code === 0) {
                resolve(data.data);
              } else {
                reject(new Error(data.msg || '签到失败'));
              }
            } else {
              const msg = (res.data as any)?.msg || `请求失败: ${res.statusCode}`;
              reject(new Error(msg));
            }
          },
          fail: (err) => {
            reject(err);
          },
        });
      });

      const consecutiveDays = normalizeConsecutiveDays(res);
      const reward = Number(res?.reward ?? 0);

      // 显示签到成功提示
      wx.showToast({
        title: `签到成功！获得${reward}灵石`,
        icon: 'success',
        duration: 2000,
      });

      // 更新签到状态
      this.setData({
        currentConsecutiveDays: consecutiveDays,
        checkedToday: true,
        canCheckin: false,
        checkinResultVisible: true,
        lastCheckinReward: reward,
        lastCheckinDays: consecutiveDays,
      });

      // 更新进度条显示
      this.updateProgressStatus(consecutiveDays, true);
      this.updateCheckinOverview(consecutiveDays, true);
    } catch (error: any) {
      console.error('签到失败:', error);
      wx.showToast({
        title: error.message || '签到失败',
        icon: 'none',
        duration: 2000,
      });
    } finally {
      this.setData({ loading: false });
    }
  },

  /**
   * 页面相关事件处理函数--监听用户下拉动作
   */
  onPullDownRefresh() {
    this.loadCheckinStatus().finally(() => {
      wx.stopPullDownRefresh();
    });
  },

  onShareAppMessage() {
    return {
      title: '甲第灵光 · 每日签到',
      path: '/pages/checkin/checkin',
    };
  },
});
