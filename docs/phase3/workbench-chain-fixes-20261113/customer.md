# 客户与项目关联整改

## K04 / K05 / K06

- K04：素材热度钻取传递并恢复日期、范围标签及统计快照，显式切换范围/刷新解除旧快照。
- K05：消息中心重新显示时复位旧操作忙状态，旧epoch响应不能覆盖当前偏好。
- K06：首次客户/分享详情沿用上级快照，显式下拉刷新清理as_of并重新分页。
- 失败证据：新增3条行为测试首先失败（范围0≠1、prefsBusy=true、刷新仍旧as_of）。
- 验证：`node --test miniprogram/tests/customer-workflow-rectification.test.js miniprogram/tests/phase3-messagecenter.test.js` 25/25通过；TS/实际运行JS同步；`git diff --check`通过。
- 未验证：微信真机切后台/弱网和真实多账号回归。
