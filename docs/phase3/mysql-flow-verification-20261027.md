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
- 测试还必须通过 Docker 核验 `codex.phase3.verify=true` 标签、running 状态，以及唯一的 IPv4 回环端口；每次从该指定容器解析当前端口，不能把配置中的地址当作生产数据库入口。
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

## 私人项目补漏后的实际 MySQL 复验

2026-10-27 已在同一个隔离容器补跑 `exerciseProjectNoteMySQLLifecycle`，命令 `JDLG_PHASE3_MYSQL_LIVE_CONFIG=<private-config> CGO_ENABLED=0 go test ./route -run 'TestPhase3MySQLLive|TestProjectNote.*MySQL' -count=1` 返回 `ok service/route 1.943s`。完成后容器恢复为 `exited`，验证标签保留。

新增验证结果：两个受邀账号中只有当前指定业主可确认；修改草稿并冻结 V2 后，V1 正文、媒体快照和确认均不改变；更换确认人也不会给 V1 新增第二条确认，V2 可由新确认人独立确认。设计师反馈处理保持独立记录。项目媒体元数据不能经普通素材目录读取，分享 raw token 不出现在数据库 payload。

受邀账号打开 V2 后，`note_open`、`notice_event`、通知队列各有一条持久记录；消费者生成的消息只发给实际分享人，路由精确携带项目与版本。原子失败回滚另由领域回归测试覆盖。本次项目媒体验证只覆盖元数据和授权隔离，未调用 COS 写真实字节；没有微信外发或生产数据操作。

## 原生引用与跨端交接复验

同日新增 `exerciseNativeReferenceHandoffMySQL`，使用新建的合成已发布正文，真实保存并读取当前账号的精确版本引用；实际分享人再创建原生 Share。H5 匿名 Entry 的已接受 page_view 生成五分钟证明，小程序账号兑换并接受自己的 page_view，数据库保留两条 event，仅一条 notice_event；消费者随后仅向原实际分享人写一条消息中心记录。两端 session 均没有 identity 合并记录。

实际命令 `JDLG_PHASE3_MYSQL_LIVE_CONFIG=<private-config> CGO_ENABLED=0 go test ./route -run '^TestPhase3MySQLLiveLifecycle$' -count=1 -v` 返回 `PASS / ok service/route 3.160s`。只启动已有且带验证标签的回环容器，完成后恢复 exited。没有调用真实微信、平台解析、COS 或生产数据库；URL Link 内部 query 和缓存隔离另通过本地 HTTP 假服务验证。
