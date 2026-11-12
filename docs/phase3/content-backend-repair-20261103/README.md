# 内容工作台线上故障修复 · 2026-11-03

后续状态：本记录之后已完成开发版 7.2.22 上传和后端新程序部署，见[后续发布记录](../backend-release-20261103/README.md)。下文保留故障修复当时的状态。

当前三个已复现故障已在线修复，并通过开发者工具当前设计师账号连接真实服务器验证。后端配置/数据已生效；本次 Go 防回归代码只完成本地验证，未替换线上二进制；小程序代码在本机开发者工具生效，尚未上传开发版或设置体验版。

## 原因、动作与结果

| 故障 | 核实原因 | 线上动作 | 实测结果 |
| --- | --- | --- | --- |
| 我的项目返回 403 | 运行进程和实际 `.env.local` 均缺少 `PROJECT_NOTES_ENABLED`，不是由现有证据证明的账号权限不足 | 备份后只开启该开关，以原二进制、参数和其他环境重新启动 | 项目列表成功；真实创建项目、保存资料、新建及修改笔记、冻结与查看版本、生成及撤销邀请、查看记录、重进保留数据 |
| AI 文案返回 500 | 应用日志显示原 `api.laozhang.ai` 地址连接拒绝/超时，服务器解析及连通性异常 | `ai_api_config` 的 `ai_chat` 启用行 id=2 切换官方 `api2.laozhang.ai` 同协议接口；保留原密钥；只清除对应 Redis 配置缓存 | 真实返回正文，带入编辑器并保存为私有草稿 |
| 服务端文案乱码 | 数据库 seed 字段与 UTF-8 经 latin1/cp1252 错解后的值精确一致；旧导入未固定连接字符集 | 先导出快照，按原字段和时间比较后更新三条已确认损坏的配置，影响行数分别 1/1/1 | API 和页面均显示“设计说明、项目介绍、推广文案”；生成内容正常 |

备用域名来自[老张官方公告](https://www.laozhang.ai/zh-cn)。服务器对备用域名 TLS 验证通过，原配置密钥请求模型列表返回 200。只确认原域的连接/解析异常，不据此断言具体网络运营商或 DNS 污染来源。

## 真实使用中补齐的前端修复

1. 保存项目资料后，“收起”恢复旧表单快照，造成误报未保存、阻挡新建笔记。现在只刷新和恢复项目字段快照，同时保留未保存笔记的确认保护。
2. 新建笔记错误发送编辑专用 `expected_revision`，严格后端解码拒绝并返回 400。创建请求现在去掉该字段，修改请求仍保留并发版本检查。

本地同时修复 AI 配置覆盖后的潜在协议混用：供应商决定模型和密钥，接口路径决定协议；支持官方备用域，拒绝明确供应商与已知主机冲突。此潜在问题不是本次线上 500 的已确认直接原因，且这段新 Go 代码尚未部署。

## 验收对象与证据

- 项目：`验收测试-工作台修复-1103`，`eLVvtwzakK4uPz4SGj4oUQSF9Zq-sfCy`。
- 笔记：`验收测试-方案沟通记录`，`h4zOmDpMKCz8zh-XE4JvUzEqXHUdMOP3`，已保存到 revision 2。
- 冻结版本：`ukrT0v1QdAyCTSKP51tZCaSkGiW0go16`。
- AI 草稿：`验收测试-AI文案修复-1103`，`AZoXZZ3UFhZdJZBDWhyWYOGAdfLQnRMk`，状态 draft，revision 2。
- 成员邀请只生成后撤销，没有发送给任何人，最终有效邀请数 0。
- [首次创建项目](project-created-first.json)、[项目流程](project-real-flow.json)、[AI 生成及保存](ai-real-flow.json) 是实际页面数据记录，无 mock 数据或登录令牌。
- 业务按钮通过自动化真实点击、字段通过 input 事件填写；项目页返回导航因自动化未取得自定义导航组件，调用该页面绑定的 `handleBack` 处理函数。其他业务请求均走当前登录账号和真实服务器。
- 模拟器截图存储报 `saveFile:fail exceeded maximum size of file storage limit`，本轮未获得新的完整截图，不将状态数据冒充视觉截图验收，也未清除用户素材。

## 实际部署与回滚

本轮通过用户已登录的宝塔终端执行。实际运行路径为 `/www/wwwroot/jiadilingguang/api/applinux`，不是历史说明中的 service/bin 路径；工作目录 `/www/wwwroot/jiadilingguang/api`，服务端口 8080。原 PID 1295144，重启后 PID 1483740；二进制 SHA-256 始终为 `04b378115bc10dd4df25144a409986e361e743597f065ae902c80e34018d277e`。

二进制内构建信息为 revision `a7aee455e6e6ca81bf6d7ba69ccd9d6d6d1889da`、modified=true，因此没有用当前工作区编译产物覆盖来源不同的线上程序。配置健康请求 200，未认证受保护接口 401；项目开关 true、配置文件仅一条开关、其他原进程环境逐项相同。未改动其他业务服务。

服务器私密备份（含原配置，不可复制进 Git 或公开分享）：

- `/root/jdlg-project-flag-backup-20261103/`：原二进制、进程参数/环境、`.env.local`、操作状态。运行脚本具备进程/二进制一致性检查、锁、健康失败回滚；14 项脚本测试通过。
- `/root/jdlg-content-repair-20261103/private-backup/`：AI 配置快照、提示词快照、应用及回滚 SQL。AI endpoint 回滚也检查修改时间；提示词应用和回滚均比较完整快照，后续运营改动不覆盖。

项目开关回滚命令（会恢复原配置并重启；执行前核对仍是本轮部署状态）：

```sh
python3 /root/jdlg-content-repair-20261103/repair_runtime.py rollback --backup /root/jdlg-project-flag-backup-20261103
```

AI/提示词回滚需通过既有安全数据库连接执行上述私密目录中的对应回滚 SQL，检查每条影响行数；AI 配置回滚后仅失效 Redis `ai_api_config:task_type:ai_chat`。任何 0 行表示已变化或不再匹配，不能视为回滚成功；不得删掉条件强行覆盖。

## 字符集修复工具

`service/sql/phase3_writing_prompts_up.sql` 和 `ops/b18a-deployment-checklist.sh` 均固定 utf8mb4 连接。存量修复先使用 `service/sql/phase3_writing_prompts_snapshot.sql` 只读导出 JSONL，再执行：

```sh
python3 ops/plan_writing_prompt_repair.py snapshot.jsonl NEW_OUTPUT_DIR
```

该命令只生成计划和带比较条件的 SQL，不连接数据库。审阅 `plan.json`、`apply.sql` 后通过既有安全连接应用；保留 `rollback.sql` 和快照。只修复与已知 seed 精确相符的乱码字段；正常运营内容、未知乱码和有损问号需人工核对。不得重新导入整套 seed 覆盖运营内容。

## 验证与边界

- 小程序全量 976/976 通过，0 跳过，包含原生 WXML 编译；TypeScript noEmit、变更 JS 语法检查通过。
- 后端 `CGO_ENABLED=0 go test ./... -count=1`、`go vet ./...`、`go build` 通过；Go 格式和 Shell 语法检查通过。
- 字符集修复单元测试 4/4；独立执行代理使用隔离 MySQL 8 跑完含真实乱码重现、修复、幂等、回滚、过期快照保护的 5/5 测试并移除容器。主代理最后单元复查未提供容器，故该次 1 项集成测试明确跳过。
- 独立只读代码复查通过；新增及修改行凭据检查未发现真实密码、Token、私钥或 API Key。本轮没有引入依赖；未安装新的依赖漏洞扫描器。
- 未验证其他账号参与的邀请接受/业主反馈、手机录音及真机全流程。此记录不代表所有内容服务都已配置完成。
- 未上传小程序开发版、未切换体验版；本次代码提交与线上配置生效分别记录。

## 本地提交

- `949d502`：AI 供应商/协议选择、官方备用域默认值及配置说明。
- `ab9d2af`：导入字符集、只读快照 SQL、存量乱码修复计划工具及测试。
- `d239944`：项目资料快照和新建笔记请求修复及回归测试。

以上每组提交前完成差异及反向补丁检查；本地代码可按组 revert。线上配置/数据不受 Git revert 自动恢复，须使用前述服务器备份。
