# 决策型报告 Agent V5

本模块实现用户输入 → 四页诊断预览 → 用户确认 → 已验证支付 → 异步十页完整报告。十页是完整报告总页数。免费阶段只保存 JSON，付费阶段输出一个由十张完整页面图片组成的 PDF。

## 编排与内容

Go 主编排器 `ReportOrchestrator` 决定调用顺序和状态，不让模型管理订单、权限、队列或发布。五个内容角色可以使用同一个模型，也可通过环境变量独立配置。

1. 规范化输入，冻结输入 JSON 与 SHA-256，建立有稳定 ID 的事实账本。
2. 规则选取 `space / budget / family / readiness` 四个配方；诊断 Agent 在固定事实、未知项与候选方向上补充解释。
3. 独立审核通过后发布四页预览。用户确认诊断 ID、输入哈希、方向与优先级；确认不可静默修改。
4. 服务端验证微信付款并冻结订单合同后进入完整任务。决策分析与预算 Agent 并行处理同一份已确认输入。
5. 分析按两个固定语义章节生成，每章两个内容块；程序组装十页。编辑 Agent 只可改第 4、5 页正文，不能改确认方向、事实、预算账本或未知项。
6. 审核 Agent 通过后才进入渲染；失败最多允许一轮编辑与复审。审核仍不通过则停止发布。
7. 渲染得到可检索校验母版 → 每页 PNG → 单一图片 PDF；验证页数、页面内容、字体、边界和图片后上传私有 COS。上传后检查元数据、大小及回读字节 SHA-256，一致才发布 `ready`。

每个内容结论保留事实引用。空间拒绝兼用时不会生成兼用建议；家庭只有一个已表达视角时不会虚构冲突；准备配方区分阻断与可并行事项。用户确认 A/B 后，推荐方向随之变化；确认后的优先级贯穿评价标准、设计师任务书与沟通确认单。未知金额、地块尺寸不由模型估算。

## 图文 PDF 与 2K 绘图入口

V5 Python 渲染器接受可选 `pages[].illustrations`，每页最多两项：

```json
{"asset":"flex-day.png","title":"平时：阅读与日常活动","caption":"AI 意向图，用于表达空间使用方式。"}
```

`asset` 必须是调用方显式 `--asset-root` 内的相对 PNG/JPEG 文件名。标题与说明由排版程序绘制；图片不拉伸，缺失、损坏、越界或版面溢出会失败并保留原 PDF。检查母版中的每个图片实例后，仍将完整页面栅格化为单一图片型 PDF。未声明插图的旧快照保持无图兼容；这不表示自动绘图已经启用。

本地命令 `cmd/report_illustrations` 复用 `LAOZHANG_API_KEY`、`LAOZHANG_IMAGE_BASE_URL`、`LAOZHANG_IMAGE_MODEL` 和既有 GPT-Image 请求模板。固定请求 16:9、2048×1152，并验证原始响应的真实尺寸；不将低清图放大冒充 2K。计划文件是非空 JSON 数组：

```json
[{"id":"flex-day","prompt":"依据已确认需求生成日常使用场景","reference":"optional-reference.png"}]
```

`reference` 可省略，只能指向计划目录内的本地图片。完成某张图片后，可将它放在后续计划目录中作参考。调用方式（在 `service` 目录）：

```sh
CGO_ENABLED=0 go run ./cmd/report_illustrations --plan /absolute/plan.json --output /absolute/images
```

该命令会产生真实外部调用和费用。先写 `submitting` 收据再发起一次请求；不跟随生成请求的重定向、不自动重试付费请求。完成收据绑定渠道、网关、模型、提示词、尺寸与参考图哈希；只复用验证通过的原图。`submitting` 表示需核对提供商结果，不能直接删除收据重跑。

当前交付边界：2K 客户端和本地命令已测试；生产 `ReportOrchestrator` / worker 尚未自动规划、生成或保存插图，Go `SnapshotPage` 也尚未承载图片字段，不能把 Python 插图支持当作在线异步生图链路完成。后续接入须持久化每张图的任务状态与资产引用，再进入最终渲染。

`reportpdf/build_illustrated_demo.py` 是固定合成案例的可复现排版工具，拒绝其他输入或选择，不用于真实客户生成。它保留原样稿事实、诊断与选择，补充已明确标记的人工样稿文案、AI 意向图及程序示意图。本次样稿使用内置 imagegen，原图实际 1672×941，未声称经过服务端 2K 接口或真实订单联调。

```sh
/usr/bin/python3 reportpdf/build_illustrated_demo.py \
  --output /absolute/demo-directory \
  --font /absolute/chinese-font.ttf
```

该目录的 `assets/` 需先准备 `exterior.png`、`elder-room.png`、`fixed-guest.png`、`flex-day.png`、`flex-night.png`。脚本绘制三张说明性图表，并生成十页 PDF；不会自动调用模型。额外测试为 `python3 -m unittest test_illustrated_demo.py`（在 `reportpdf` 目录）。

## 异步任务与恢复

`building_reports` 是持久任务队列，`report_agent_stages` 保存各阶段结果，`report_decision_contexts` 保存确认快照及可展示进度。使用数据库行锁、租约 token、有效租约和状态条件限制写入。多个 worker 可以消费同一数据库，最大本进程并发为 8。

状态链：`preview_queued → preview_generating → preview_ready → full_queued → full_generating → ready`。失败进入受限重试或 `failed`。`stage` 提供诊断、分析、编辑、审核、渲染、上传等细分进度；以 `status` 判断业务终态。任务不依赖 HTTP 请求或用户在线。

运行键绑定输入、确认、模型配置、模板及提示词版本；已成功子阶段可恢复复用。分析成功而预算失败时，仅补预算；上传失败时可复用私有目录中经过校验的 PDF。修订提示词/内容规则需更新 `decisionAgentVersion`，变更模型配置时需更新 `REPORT_AGENT_CONFIG_VERSION`，避免复用旧结果。

不承诺模型输出质量等于事实正确。上线前仍需用真实业务样本评估不同家庭、缺失信息、拒绝项、预算未知及模型输出稳定性。

## API

统一前缀 `/api/v1/miniprogram`。所有返回设置 `no-store`。问卷与 config 公开可读；创建、确认、状态、订单与下载沿用项目 token 鉴权，归属由服务端验证。旧 `POST /reports` 保留原协议。

| 接口 | 作用 | 成功状态 |
|---|---|---|
| `GET /reports/config` | `enabled`、`decision_enabled` | 200 |
| `GET /reports/v5/questions?primary_goal=space` | 登录前读取版本化问题集 | 200 |
| `POST /reports/v5` | 保存输入并入队，不在请求中调用模型 | **202** |
| `GET /reports/:id` | 查询 status/stage/next_action、四页 preview_snapshot、支付 quote | 200 |
| `POST /reports/:id/confirmation` | 确认输入版本、方向及排序 | 200 |
| `POST /virtualpay/prepare` | 现有入口，`plan_type=report`、`report_id`；金额/商品由服务端取值 | 200 |
| `POST /virtualpay/confirm` | 现有服务端查单/授权流程 | 200 |
| `POST /reports/:id/retry` | 有限次数重试本人失败任务 | 200 |
| `GET /reports/:id/document?mode=full` | 本人、有效已付订单、ready 后发短时私有链接 | 200 |
| `POST /reports/:id/wecom` | 现有企微领取入口；独立于 V5 支付与下载 | 200 |

新建请求示例：

```json
{
  "primary_goal": "space",
  "region": "福建泉州",
  "approximate_dimensions": {"width_m": 10, "depth_m": 15},
  "buildable_scope": "unknown",
  "permanent_count": 2,
  "returning_count": 4,
  "return_frequency": "holidays",
  "floors": 2,
  "bedrooms": 4,
  "flexible_use": "yes",
  "priorities": ["老人日常方便", "返乡团聚", "控制维护投入"],
  "budget_wan": 100,
  "budget_scope": [
    {"item": "主体施工", "status": "included"},
    {"item": "庭院与设备", "status": "unknown"}
  ]
}
```

四类通用枚举：`buildable_scope=known/unknown`，known 需 `buildable_constraints`；`return_frequency=none/monthly/quarterly/holidays/yearly/unknown`；`flexible_use=yes/no/unknown`。未提供的数字应省略，不用 0 表示未知。尺寸宽深需同时提供或同时省略。`priorities` 一至三项，保持提交顺序。

| 配方 | 专属输入 |
|---|---|
| space | `permanent_count`、`returning_count` 必填 |
| budget | `budget_scope` 必填，最多八项；金额可未知；每项 `item/status`，status 为 included/excluded/unknown |
| family | `family_perspectives` 必填，每项 `role/request`，只记录家人已表达的意见 |
| readiness | `preparation_stage`、`preparation_items` 必填，最多十项；每项 `item/status`，status 为 existing/missing/unknown |

`preparation_stage`：idea、information、design、approval、bidding、preconstruction、unknown。预算金额与范围可以在所有配方填写；范围未知时逐项标明 unknown。

确认请求中的 ID/hash 必须从本次返回值读取：

```json
{
  "diagnosis_id": "本次preview_snapshot.diagnosis.id",
  "input_hash": "本次input_hash",
  "choice": "option_a",
  "priorities": ["老人日常方便", "返乡团聚", "控制维护投入"]
}
```

空间配方 choice 为 option_a/option_b/undecided，其余为 confirm/undecided。优先级须为原输入项目，只可重排；变更家庭事实需创建新输入版本。相同用户、输入和协议重复提交返回同一任务；相同确认可幂等提交，改动确认返回 409。

前端按 `next_action` 驱动：wait_preview、confirm_decision、pay、wait_report、download、retry。建议前台轮询 2—5 秒，后台暂停，重新进入后按 report ID 恢复。不展示伪造进度百分比。V5 `preview_pdf_available=false`，不会在 GET 中同步生成免费 PDF；完整快照不会从状态 API 泄露。

## 配置与运行

所有开关默认不启用 V5。参考 `service/env.example` 末尾的 REPORT 配置段，凭据通过部署环境注入，不写进仓库。

部署顺序：

1. 备份并在测试 MySQL 验证。主表未部署时先执行 `sql/report_module_up.sql`，再执行 `report_agent_stages_up.sql`、`report_decision_v5_up.sql`。企微能力沿用 `report_wecom_up.sql` 及项目既有配置。
2. Python 沿用现有 ReportLab、pypdf、PyMuPDF、Pillow 依赖，字体需可嵌入中文 TTF。新 `decision_v5.py` 必须与 `render.py` 同目录部署。无需新增 Go 依赖。
3. 配置 HTTPS JSON Chat Completions 兼容模型接口、角色模型、中文字体和绝对脚本路径；`REPORT_ARTIFACT_DIR` 为权限 0700 的私有持久目录，不放 Web 静态目录。多实例若要复用 PDF，应挂同一目录。
4. 设置独立私有 COS bucket，确认私有策略。完整报告仅签发短时 URL，不可复用公开素材 bucket。
5. 先开启 REPORT_ENABLED/REPORT_MULTI_AGENT_ENABLED/REPORT_DECISION_ENABLED 验证免费流程，再在真实支付商品及价格配置验证后开启 REPORT_PAYMENT_ENABLED。

嵌入 API 进程：`REPORT_WORKER_EMBEDDED=true`。独立 worker：API 设置 false，在同一 service 工作目录和环境执行 `go run ./cmd/report_worker`，或构建 `go build -o report-worker ./cmd/report_worker` 后运行。独立进程只连接已存在的数据库，不自动建库。SIGINT/SIGTERM 取消处理，未完成任务通过租约恢复。进程/数据库时钟应由运维保持同步。

运行日志仅保留 report_id、阶段、耗时、命中和错误码，不输出家庭输入、密钥或签名 URL。模型调用限时 45 秒，任务默认 5 分钟、租约 330 秒、最多 3 次尝试。完整任务最坏包含审核修复时，应按真实提供商延迟测试后调整有界超时。

## 验证与已知边界

本地验证命令：

```sh
CGO_ENABLED=0 go test ./... -count=1
CGO_ENABLED=0 go vet ./...
cd reportpdf
/usr/bin/python3 -m unittest test_render.py test_decision_v5.py
```

当前开发 Mac 的既有 go-m1cpu CGO 初始化会崩溃，因此 Go 检查关闭 CGO；这不替代部署架构上的 race/压力测试。数据库锁、模型响应及 COS 失败注入由测试替身覆盖；没有把这些测试称为真实 MySQL、真实模型计费、微信支付或企微实测。

`TestDecisionPipelineWithRealPDF` 额外串联创建、真实 Go 主编排、确认、测试支付授权、Go 子进程适配器、正式 Python 渲染及下载归属校验。设置 `REPORT_V5_REAL_PDF_TEST=1`、`REPORT_PDF_PYTHON`、`REPORT_PDF_FONT` 后运行该测试；数据库、模型和存储仍使用测试替身，不会产生真实支付或外部通知。

此仓库包含服务端与后台；小程序源码在另一份独立 checkout。本模块已提供前端接口合同，尚需在那里接入：四类表单、HTTP 202、诊断确认、状态轮询、十页文案和取消 V5 企微绑定下载门槛；还需核实带 query 的请求签名。

现有企微只支持添加好友时通过 welcome_code 发领取小程序入口，并有入站回调补偿。尚未实现报告完成后的主动消息 outbox/dispatcher；不能把一次性欢迎消息当成可无限重试的完成通知。自动推送须先确认企业已有可用的发送 API/权限。报告 ready 与通知成功是不同状态，用户可直接从报告中心下载，不发送会过期的 COS URL 作为长期入口。

回滚应先关闭新任务入口并停止 V5 worker，保留已购买任务及报告可读性。两个新增表的 down SQL 会删除确认/断点数据，只适用于隔离测试；线上有订单后不要直接删表。业务代码回退前应处理或停留所有 V5 在途任务。
