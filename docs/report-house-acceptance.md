# 整屋图文报告开发验收记录

日期：2026-09-20。状态：本地开发、集成测试及独立代码审查完成，尚未部署或进行真实渠道验收。

## 已交付行为

报告保留旧版整屋大框架：家庭与场地、设计目标、方案、逐层平面、体量外观、方案比较、预算边界、设计师任务书和行动计划。主编排分别调用诊断、策划、方案、空间、视觉、预算、编辑和审核角色；用户的家庭人数、常住/返乡模式、楼层、卧室、必需房间、首层老人房、风格和拒绝兼用等约束进入内容和图片任务。四页预览冻结事实与诊断，付费完整报告延续这组事实，不另造一个客户。

新协议按实际方案、楼层和正文长度分页，支持二至三个方案、一至三层。两层和三层合同样例分别输出15、17页，页数不是商品固定承诺。每个方案逐层绘制概念平面，再关联体量与外观，图片通过实际多模态审核后进入一个完整页面图片PDF。

绘图配置复用小程序现有ai_draw渠道及2K客户端，不增加供应商、不新增依赖。后台异步任务保存阶段与逐图收据，可继续使用已完成结果；未知收费提交停止重发。多实例启动会校验图片共享目录的存储身份。

小程序实际修改位于用户指定的`开发版916/JDLG/miniprogram`，包含问卷、确认、服务端报价支付门槛、持续轮询和私有下载。主仓忽略该复制目录，因此原始文件与最小补丁独立归档，未纳入项目账号配置或其他业务。

报告完成通知独立于报告ready状态。已实现事件补齐、真实客户绑定检查、幂等消息网关合同和未知结果查询；未配置网关时保留待人工处理记录。现有企微欢迎消息接口不被冒用为报告完成后主动推送接口。

## 主要文件

| 范围 | 文件 |
|---|---|
| 输入、结构、个性化约束 | `service/report/house_types.go`、`house_input.go`、`house_content.go` |
| 子角色编排与断点 | `service/report/house_orchestrator.go`、`house_worker.go` |
| 2K绘图、收据与存储校验 | `service/component/report_illustration_config.go`、`service/report/house_images*.go` |
| 路由、运行与旧协议兼容 | `service/route/report_house*.go`、`service/report/decision_*.go`、`worker.go`、`service.go` |
| 图文排版与完整图片PDF | `service/reportpdf/house_v1.py`、`render.py` |
| 完成通知 | `service/reportnotify/`、`service/route/report_notifications.go` |
| 数据库 | `service/sql/report_house_images_{up,down}.sql`、`report_notifications_{up,down}.sql` |
| 小程序原件/补丁 | `docs/report-house-miniprogram-baseline/`、`docs/report-house-miniprogram-patch/` |
| 配置与接口合同 | `service/report/HOUSE.md`、`service/env.example` |

复用既有登录签名、服务端验单、数据库任务队列、角色模型适配器、绘图客户端和私有PDF上传；保留旧V5/旧报告行为。没有引入新队列服务、Agent框架、数据库依赖或绘图库。

## 最终验证

| 验证 | 结果 |
|---|---|
| Go全仓测试，启用V5/整屋真实PDF集成 | 414项通过（含子测试）、0失败；3项依赖真实MySQL的既有测试明确跳过 |
| Python报告回归 | 52项通过，覆盖旧模板、V5、插图样稿和整屋动态排版 |
| 实际小程序报告测试 | 44项通过、0跳过，含微信原生WXML编译函数的执行测试 |
| 静态检查/构建 | 44个Go变更文件gofmt检查通过；go vet全仓通过；独立worker构建通过；小程序全项目tsc无错误 |
| 并发专项 | reportnotify竞态检测通过；图片单次提交、共享目录并发初始化及异盘拒绝有测试 |
| 模板/样式 | 原生WXML编译通过；5个报告Sass文件编译通过 |
| 安全与依赖 | 变更文本密钥模式扫描无命中；未改变go.mod/go.sum或新增依赖；本机未安装govulncheck，未完成依赖CVE扫描 |
| 回退与文件一致性 | 小程序18个交付文件SHA与实际源码一致、15个原始备份SHA未变；逆向补丁检查通过 |

Go使用CGO_ENABLED=0规避本机既有go-m1cpu的CGO运行问题；通知包单独执行竞态检测。真实PDF集成确实调用Go子进程适配器和Python排版器，但模型、支付、数据库及COS使用测试替身，图片为合法2K测试色块，不能据此宣称真实绘图质量或真实交易验收通过。

独立审查修复了：下载query参与签名、未指定房间楼层的零值兼容、邻房事实回显、临时断网后恢复轮询、补充文字长度校验、过大合法2K原图的审图副本，以及多主机图片目录误配。

## 上线与回退边界

尚待部署环境完成：MySQL升降级与真实租约并发、现有模型实际响应与图文一致性、真实微信支付、COS私有权限、微信真机页面、企业微信网关权限及实际回执。当前REPORT_HOUSE_ENABLED默认关闭。本次未迁移线上数据库、未调用收费绘图、未付款、未向客户发消息、未发布小程序。

后端变更在`feat/report-orchestrator-v5-20260920`，小程序已修改到指定源码并保存最小补丁。代码可按提交回退；数据库down脚本会删除新表中的收据与通知历史，只可在停止在途任务并备份后用于隔离测试。线上禁止靠删除submitting收据触发再收费。回退业务前须处理已购在途报告；共享资产卷、存储身份和原图应一起保留。

具体配置和接口见[整屋协议说明](../service/report/HOUSE.md)，小程序文件清单及恢复方法见[小程序补丁说明](report-house-miniprogram-patch/README.md)。
