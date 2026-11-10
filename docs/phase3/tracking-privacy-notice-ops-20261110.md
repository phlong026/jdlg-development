# 访问分析隐私告知 · 运维配置变更单（T7.2）

- 日期：2026-11-10
- 关联任务：T7.2（隐私文案四处同步）、T7.1（默认开启）、T7.3（不升级 CONSENT_VERSION）
- 变更类型：**仅服务端环境变量 `TRACKING_PRIVACY_NOTICE` 文案更新**
- 不变项：`TRACKING_CONSENT_VERSION` **保持现值、不升级**（P7 决策，见 `tracking-consent-version-decision-20261110.md`）

## 一、变更原因

T7.1 已把访问分析改为「默认开启、可在设置关闭」。原告知要求用户先开启，与现状不一致；前端页面（`pages/privacy`、阅读页提示块、设置弹窗）已同步改为默认开启口径，服务端下发的告知正文必须一起更新，否则页面显示的口径仍会误导用户。

## 二、需要下发的 `TRACKING_PRIVACY_NOTICE`（整段替换）

```
为分析分享内容的访问效果，我们默认记录公开页面的访问、可见时长、明确点击、分享发起和可观测媒体播放；使用随机安装标识，登录后仅由服务端关联账号。
不记录输入正文、联系人、位置或设备指纹；分享不代表送达，时长不代表理解。
访问分析可在“设置 → 访问分析”随时关闭，关闭后不再记录，不影响阅读、分享或其他功能。
原始数据保留90天，汇总180天。处理者与删除渠道：<沿用现有运维值，不要改动>。
```

要求：
1. 第 4 行的处理者与删除渠道沿用线上现值，不得删减；
2. 不得出现「开启后才会记录」「分析为自愿选择」等与默认开启冲突的表述；
3. 与 `pages/privacy/privacy.ts` / `privacy.js`、`utils/tracking/index.js` 的设置弹窗文案保持同一口径。

## 三、变更步骤

1. 在服务端环境变量中更新 `TRACKING_PRIVACY_NOTICE`（仅改值，不改 key）。
2. **不要**修改 `TRACKING_CONSENT_VERSION`。
3. 发布 `service` 并确认 `/api/v1/miniprogram/tracking/config` 返回新的 `privacy_notice`。
4. 抽查阅读页/分享页：正文首屏可见、无「是否开启访问分析」提示块；设置入口显示「访问分析已开启 · 设置」。

## 四、回滚

- 恢复旧 `TRACKING_PRIVACY_NOTICE` 值并重新发布即可；前端代码回滚需同步回退 T7.1/T7.2 的文案改动。
- `TRACKING_CONSENT_VERSION` 任何情况下都不得回滚为升级或降级值。

## 五、验收对应

| 验收 | 证据 |
|---|---|
| ① 页面 `consentNotice` 与隐私页口径一致、无「开启后才会记录」 | 前端代码 diff + 本变更单 |
| ② 设置弹窗不再声称「自愿选择 / 未开启」 | `utils/tracking/index.ts` 文案 diff |
| ③ 三处改动均有记录 | 代码 diff（privacy.ts/js、tracking modal）+ 本运维变更单 |
