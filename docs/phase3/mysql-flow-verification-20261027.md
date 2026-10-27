# 三期内容导入、分享跟踪与消息通知 MySQL 联调记录

日期：2026-10-27

## 结论

三个模块的核心数据链已在隔离的 MySQL 8.4 数据库 `phase3_verify` 中跑通，使用的是项目真实 SQL 迁移、`contentimport.SQLStore`、`tracking.SQLStore` 和 `notification.SQLStore`，没有使用伪 MySQL driver。

已验证的完整链路：

1. 导入任务 `Submit → Claim → BeginResolve → SaveResolution` 真实落库。
2. 合成 PNG 通过实际私有媒体保存逻辑转存，草稿阶段不能被公开读取，文件权限为 `0600`。
3. 原排版 `article_v2` 媒体引用由 `remote-0000` 替换为甲第内部媒体 ID，`SaveImportedDraft → Finish(ready)` 真实落库。
4. 导入提交和草稿保存的幂等重放返回同一资源，相同幂等键修改语义时被拒绝。
5. 两个 worker 并发 `Claim` 同一导入任务时，仅一个取得 lease，另一个获得 `ErrNoJob`。
6. 已发起上游请求但 lease 超时且没有解析结果的任务会进入 `uncertain/provider_result_unknown`，不会被 worker 盲目重新领取并重复计费。
7. 草稿 `Preview → Publish` 后媒体才可经权限校验读取。
8. 实际分享人 B（测试账号 ID `202`）在小程序访问上下文中创建分享，匿名访客带分享参数进入并上报 `page_view`。
9. `event`、`notice_event` 和 `mini_notification_event_jobs` 在访问事件接受后同时可见，验证了跟踪事件与通知投影任务的同事务提交。
10. `notification.Consumer.RunOnce` 真实消费 MySQL 任务后，仅分享人 B 的收件箱出现一条未读消息，其他账号为空；重复消费不会生成第二条。
11. 未读数从 `1` 经 `MarkRead` 变为 `0`。
12. 内容撤回后，已发布版本不再可读，原分享参数也无法再新建访问会话。

## 安全边界

- 测试默认 `Skip`，只有显式设置 `JDLG_PHASE3_MYSQL_LIVE_CONFIG` 才运行。
- 配置文件必须是绝对路径、普通文件，且不能开放任何 group/other 权限。
- DSN 必须同时满足：`tcp`、主机为 `127.0.0.1` 或 `localhost`、数据库名严格等于 `phase3_verify`。
- 容器名必须使用 `jdlg-phase3-verify-` 隔离验证前缀。
- 建连最多等待 45 秒，每次重试间隔 5 秒；测试结束不删除容器，便于后续查验。
- 每次运行只清理固定测试租户 `phase3-mysql-live` 的行，不操作其他租户。
- 测试日志不输出 DSN、密码或配置文件内容。

## 执行证据

默认路径（不接触 MySQL）：

```text
CGO_ENABLED=0 go test ./route -run '^TestPhase3MySQLLiveLifecycle$' -count=1
ok service/route
```

隔离 MySQL 实际联调：

```text
JDLG_PHASE3_MYSQL_LIVE_CONFIG=<private-config> CGO_ENABLED=0 go test ./route -run '^TestPhase3MySQLLiveLifecycle$' -count=1 -v
=== RUN   TestPhase3MySQLLiveLifecycle
--- PASS: TestPhase3MySQLLiveLifecycle
PASS
ok service/route
```

`CGO_ENABLED=0` 是为规避当前本机 Go 1.26.2 加载 `github.com/shoenig/go-m1cpu` 时的 cgo 初始化崩溃；该崩溃发生在测试进程启动阶段，与本次 MySQL 链路代码无关。

## 本次明确未验证的内容

- 内容数据是测试代码内明确标记的合成原排版 HTML 和合成 PNG，不是公众号、小红书、抖音或视频号的真实外部链接。
- 未发起任何第三方解析 API 或付费请求。
- 媒体转存验证的是本地私有文件路径，不是腾讯云 COS 真实网络调用。
- 通知验证到站内收件箱和服务号投递前的持久化边界；未调用微信服务号模板消息 API，也未验证真实关注/绑定账号。
- 未证明真机原生小程序卡片的微信端到端传参；本次只验证服务端收到分享参数后的归因、事件和通知链路。
- 没有将“测试通过”扩大为生产部署、服务号实发或真实外链端到端验收结论。
