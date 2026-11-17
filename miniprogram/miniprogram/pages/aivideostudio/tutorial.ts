// @ts-nocheck
'use strict';

const { lessons, getLesson } = require('../../utils/ai-video-tutorial');

Page({
  data: {
    modes: lessons.map(lesson => ({ id: lesson.id, title: lesson.title })),
    lesson: lessons[0], error: '',
  },
  onLoad(options) {
    this._epoch = (this._epoch || 0) + 1;
    this._unloaded = false;
    this._visible = true;
    this._token = String(wx.getStorageSync('token') || '');
    this.setData({ lesson: getLesson((options || {}).mode), error: '' });
  },
  onShow() { this._visible = true; },
  onHide() { this._visible = false; this._epoch = (this._epoch || 0) + 1; },
  onUnload() { this._unloaded = true; this._epoch = (this._epoch || 0) + 1; },
  selectMode(event) {
    this._epoch = (this._epoch || 0) + 1;
    this.setData({ lesson: getLesson(event.currentTarget.dataset.mode), error: '' });
  },
  copyExample() {
    const epoch = this._epoch = (this._epoch || 0) + 1, token = this._token;
    const current = () => !this._unloaded && this._visible && epoch === this._epoch
      && token === this._token && token === String(wx.getStorageSync('token') || '');
    if (!current()) return;
    this.setData({ error: '' });
    wx.setClipboardData({
      data: this.data.lesson.example.prompt,
      fail: () => { if (current()) this.setData({ error: '复制失败，请重试或长按选择文字' }); },
    });
  },
});
