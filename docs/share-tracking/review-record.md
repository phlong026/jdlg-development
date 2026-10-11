# 独立代码评审与修复记录

范围：`service/tracking`、分享路由、后台管理、小程序 SDK / 阅读页。此记录为离线代码评审，不替代真实数据库、微信、企微、CRM、浏览器视觉或负载验收。

## 已修并有回归覆盖

- 私有草稿媒体读取与管理接口执行相同的企微有效会话检查。
- 未到达的跨页前驱先保留为提示；只有相同会话绑定确认后才显示链路，不因到达顺序永久丢弃合法 PV。
- 删除墓碑阻止已登录用户绕过失效 entry 重新建立分享历史。
- 原始事件过期后，日汇总和归属隔离的注释仍支持 CRM 解除和意向修正。
- 阅读页播放器每次重建使用独立实例标识，后台恢复不再与旧序号冲突。
- 二维码缓存按组织、操作账号、环境和 UTC 日隔离，每日更新，远早于分享令牌 30 天到期；缓存命中前仍验证分享授权。
- 已关联匿名会话不能被旧凭据重放改绑另一账号；相同账号重试保留原关联时间。
- 素材上传必须提交明确的授权归属，不再任取多归属中的第一项。
- 原有业务内容卡只读，使用公开来源语义版本与标题封面快照；读者正文不声称可以回放旧业务版本。
- 阅读滚动深度统一为 0–100 百分比。

对应测试包括 `TestOutOfOrderNavigationPredecessorReconciles`、`TestDeletedLoginCannotCreateNewShares`、`TestPrivateAnnotationsRemainRemovableAfterRawRetention`、`TestAnonymousLinkCannotBeReassignedByReplayedCredential`、`TestTrackingCodeCacheRenewsAndIsolatesAttribution`，以及阅读页播放器与滚动回归。

## SDK 复核项

长后台恢复的会话边界、离线 A→B→A 的连续凭证批次、临时配置失败保留队列、慢 entry 响应遇到内容切换的失效处理，均通过真实内存 SDK 测试验证。公开页面告知和“我的”随时选择/撤回入口另有专门回归；最终自动化数量以 rollout-and-verification.md 的固定快照为准。

## 已知边界及上线门槛

- 朋友圈同步回调无法等待服务端：首次未预备令牌时保留原分享，不能声称该次有追踪归因或证明送达；后续缓存仍需真机验证。
- 57 条原路由均有明确策略，37 条敏感/私有/管理页面不采集。策略清单不是逐页微信真机验收报告。
- 真实组织权限、合法告知、90/180 天保留建议和删除渠道尚需负责人核定；默认关闭采集。
- 原小程序 2 项客服断言失败和 3 项跳过单独保留，不擅自整改其他模块。
- 当前云执行环境拒绝 Chromium socket；没有可冒称通过的页面截图。真实 DB/CRM/微信双端、视频播放、压力、恢复演练仍未验。

## 第二轮真实阅读页复核

新增 `tests/tracking-reader-entry.test.js` 使用真实 reader、adapter、core、routes 和 API options 解析器。固定阶段 8e723e5 对最初12例为4通过/8失败，证明之前的SDK单元通过未覆盖实际reader重入。

修复阅读页普通返回重新注入旧 st、迟到 scene 在撤回/换号/隐藏后恢复归因、旧 scene A 覆盖新入口 B 的 options。入口绑定有独立代次，异步响应在赋值前校验，新同意不抹掉仍有效的首次入口；普通媒体重试也不清除尚未消费的首次 st。最终13例全部通过，加入常规 test:tracking。此证据仍不替代真实微信场景验收。

## SQL与目标隐私下推复核

独立只读评审复核参数化tenant/owner、累计预算sticky错误与事务回滚、跨窗口/as_of/过期汇总，以及定向删除的全部owner检查与匿名会话依赖，未发现新增可复现P1。评审指出的内存fallback无关超大对象P2已补前置标量筛选和回归：无关session/content/share不消耗payload预算；仍需要解析JSON才能判定的超大不可信旧记录保守422，不冒险无界解析。该差异不影响生产SQL先WHERE后传输的路径。真实MySQL语法/EXPLAIN与负载尚未验，模拟driver测试不替代实库。
