# 验收与发布证据

2026-11-15。最新1701 JS、Go全量/vet、真实MySQL及原生角色结果来自根代理本轮执行记录；本次文档同步核对了最终JS日志，未重跑业务测试。本记录区分本地自动化、原生页面、真实账号和生产交付，不以早期门禁代替后续结果。

## 本地检查快照

| 检查 | 结果 |
| --- | --- |
| 小程序全部测试 | 1701/1701通过，0失败、0跳过；最终日志`/tmp/jdlg-mini-final-20261115.log`；显式指定已安装WXML_COMPILER |
| Go全量测试 | CGO_ENABLED=0 go test -count=1 ./...通过 |
| Go静态检查 | CGO_ENABLED=0 go vet ./...通过 |
| 真实MySQL租户锁回归 | 旧实现24并发触发1213；单条ODKU新实现20轮通过 |
| 管理后台 | npm test 776/776通过；生产构建通过 |
| 依赖安全 | 管理后台和小程序npm audit均0漏洞；未变更依赖锁文件 |
| 全工程TypeScript | 最终刷新仍188行；规范化路径及行列后新增0，不能称全量类型检查通过 |
| 运行产物及路由 | 签名器、TS/JS一致性通过；78路由策略一致（23公开/55排除） |
| 原生语法编译 | 215 WXML + 33 WXS通过；172 WXSS在临时副本解析根导入后通过 |
| 改动语法/格式 | 19个JS语法通过；所有改动Go符合gofmt；diff-check通过 |
| 发布工具 | 受保护构建器1/1、发布工具63/63通过（完整discover复核） |
| 凭证检查 | 改动模式扫描无发现；crypto.go保护保持，无其他隐藏跟踪文件 |
| 独立审查 | 原三个功能提交后端/前端APPROVE；追加13文件视觉收敛P0/P1=0；追加ODKU审查P0/P1/P2=0、正式结论COMMENT（无LSP，compiler/vet及真实SQL替代），不得记为APPROVE；as_of两分支最终审查APPROVE、P0/P1/P2=0 |
| 回退验证 | 原三个功能提交反向补丁及隔离revert通过，恢复f33030a基线树；新增三提交在/tmp/jdlg-revert-native-20261115隔离revert成功，write-tree精确恢复febfe4c树053a70974cb422457200fa1566b7366a69d6ece0 |

表内1701 JS、Go全量/vet和真实MySQL为本轮最新已报告结果；其余编译、扫描和工具结果保留原已执行记录。原生编译和编译树断言不等于18态视觉、键盘或真实手机验收。本机CGO启用时存在m1cpu初始化环境崩溃，因此本地Go检查使用CGO_ENABLED=0；race已额外在本地Linux/arm64官方Go1.26.2容器通过5包检查；GitHub Linux/amd64 CI仍因账号限制未执行，二者分别记录。

## 开发者工具与真实角色门禁

目标AppID `wx94fdc48861913ede` 官方auto成功。开发者工具375/390宽各18态已完成原生截图及button/layout边界检查，无横向溢出；视觉P0=0、P1=0、P2=4，四项均非阻断。用户要求的DevTools验收门禁已通过，可进入创建PR阶段。该结论限定开发者工具，不包含系统键盘、手机相册、微信原生选择收件人或生产验收。

| 本地真实后端/原生分项 | 结果 |
| --- | --- |
| 私密项目 | 设计师账号3创建项目，保存不冻结/不创建分享，单步准备客户预览；客户账号4读取同一固定版本 |
| 客户关联 | 项目详情真实绑定及解除均通过，等待busy完成后核验 |
| 公开图文2 | 真实image上传、保存、排序/封面、可选预览、直接发布通过 |
| 视频 | 真实素材库选择、保存、直接发布、播放2秒通过；营销字段可空 |
| 公开分享浏览 | batch200落库：page_view=1、visible_segment=2、page_end=1，均归因self=0 |
| 素材→访客 | 真实as_of保持一致；customers/shares首次onShow两分支修复红绿回归通过；最终审查APPROVE，P0/P1/P2=0 |
| 管理预览和权限 | 真实管理预览entry/events请求均为0；无权限role5工作台及管理预览均拒绝 |

本地真实后端角色分项已通过；完整真机/生产角色验收仍未执行。浏览落库和分享预览不能证明通知实际送达或客户已通过微信收到分享。

四项非阻断P2：02新建图标对比偏弱；03缩略图封面叠层偏挤；12查看说明视觉权重偏高；18设置3项文案略长。详见[NATIVE-UAT.md](NATIVE-UAT.md)，不将P2登记写成P0/P1阻塞。

局部媒体存在TLS自签问题：仅在隔离测试副本把真实媒体字节下载到sandbox进行适配；仓库源代码安全逻辑未改。上述结果不证明正式TLS链、线上媒体直连或真机路径通过；该适配不作为生产配置。DevTools多窗口触发hide后，自动化调用真实page.onShow恢复生命周期，不伪造接口响应。

系统键盘、手机相册、原生微信手动选择收件人及真机播放继续单列待验；不回退已经通过的DevTools门禁。

## 代码和发布状态

本地功能及追加修复提交：
- `6c4417598675579350a0a455e7e372b205a2ae9a`：content
- `2fcfc47d037e6a5be4ebea66689b4cd1574423f3`：project
- `1dc6721bc2c265ea1487b28e4780489ef5f023de`：customers
- `7a8479c70fe7bdf43cc6b40ec733cee589c8d409`：租户锁，5文件
- `086535fd49874f4ddd68da1fc9575ffa8d74d81e`：原生布局，13文件
- `c6561eb94abc7b82c6995c60dd300ce4cd0b5c6e`：素材快照，3文件

仓库为phlong026/jdlg-development，开发分支feat/workbench-simplification-20261115。原工作区未跟踪docs/phase3/deployment-20261112/完整保留，未暂存。原分支的分享主体展示和紧凑客户访问两个提交是当前页面依赖，继续保留。

**PR #51已通过5项CI并合并，后端与管理后台已部署，8.0.9已官方上传。** 应用源码55f836d，详见[实际发布回执](release-receipt.json)及[部署与回退](DEPLOYMENT.md)。真机、体验版指向、审核和正式发布单列。

部署前的生产只读基线：原进程1051555，工作目录/www/wwwroot/jiadilingguang/api，8080端口，运行二进制SHA256 e10d26552b4b75a7cd4d22ae98fbdb3e1508e2530471f50b8a3c4068decb1b66。线上ELF与本地受保护源HMAC/SM4均匹配；未输出密钥。本次切换前已重新核对PID、哈希和运行环境，并保留备份与原运行参数；部署后结果见下方最终交付核验。

机器可读结果见[checks.json](checks.json)，修改位置见[CHANGES.md](CHANGES.md)，部署与回退见[DEPLOYMENT.md](DEPLOYMENT.md)。

## PR与CI启动阻塞（已解除，历史过程）

代码及原生验收记录已推送，[PR #51](https://github.com/phlong026/jdlg-development/pull/51)已创建。2026-11-15的Admin quality run 43704533452及Share tracking quality run 43704533465共5项任务均未启动：GitHub官方注释为“recent account payments have failed or your spending limit needs to be increased”。此为平台执行前限制，不能称CI测试通过，也不据此判断代码失败。该限制已由用户解除，全部5项任务重跑成功后才合并部署。

额外完成63项发布工具测试、受保护Linux候选构建、后台候选构建及99文件包哈希检查；该阶段候选源码为eedfcb9；后续已从合并55f836d重建并部署。Linux race本地隔离补验已通过，不能替代GitHubCI终态。

本地Linux race补验：干净eedfcb9源码、官方Go1.26.2 linux/arm64镜像，`go test -p 2 -race -count=1 ./aivideostudio ./contentimport ./tracking ./route ./model`五包全部ok、进程exit 0、无race报告。首轮512MiB tmpfs空间不足导致route/model编译中断；改用专用host目录挂载GOTMPDIR后完整重跑通过，不清理其他项目资源。Linux Go测试使用Git中占位认证源，不注入生产密钥；真实签名/ELF验证由受保护候选构建单独完成。

同一Linux容器（Node24.21.0、GOMAXPROCS=2）随后运行相同五包普通`go test -p 2 -count=1`与`go vet`，全部通过、组合进程exit 0；隔离源码工作树未修改。日志`/tmp/jdlg-linux-go-test-vet-20261115.log`。

## 最终交付核验

GitHub全部5项CI通过，PR #51合并55f836d。生产后端与后台从同一合并源码发布，保留原程序、运行环境及后台完整备份。公网后台index与构建一致，5个入口引用资源均200；真实现有登录账号的工作台、客户访问/素材空态和本人固定版本管理预览成功，管理预览跟踪写请求为0、媒体错误0。没有在生产创建、发布或删除验收内容，没有替客户确认送达。

小程序8.0.9上传命令以干净合并源码为项目，AppID wx94fdc48861913ede；官方CLI exit0、`✔ upload`。该回执仅证明代码上传；体验版指向未独立核验，未在本任务提审或正式发布。完整机器记录见[release-receipt.json](release-receipt.json)。
