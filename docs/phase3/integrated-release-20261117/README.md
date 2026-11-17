# 2026-11-17 联合部署回执

后端、管理台和数据库迁移已完成。小程序8.0.13已发起上传，当前等待微信开发者工具内确认；不能将此状态计为上传成功、体验版更新或正式发布。真实AI供应商未启用，服务号及公众号同步按用户指示稍后配置。

源码已通过[PR #57](https://github.com/phlong026/jdlg-development/pull/57)合并。候选提交为`f8688da5753dc729d76166bf6dbff3f78f9d3da3`，合并提交为`f75160bc22d2077a33b9c11ea87696749de8ed06`；两者源码树一致。部署使用已冻结的候选产物，没有从旧脏工作树重新打包。

## 已完成与证据

| 项目 | 结果 |
| --- | --- |
| 代码与CI | 7次任务执行成功，覆盖5类检查；含Linux后端race |
| 本地验证 | 小程序1960项、管理台786项通过；Go全包2168个pass事件，AI视频MySQL 22项实际通过；类型诊断0 |
| 后端 | 单一进程PID `3686744`；运行及磁盘ELF SHA256相同，见[机器回执](release-receipt.json) |
| 管理台 | manifest的94个文件全部匹配；公网入口和引用的4个JS、1个CSS匹配，TLS验证通过 |
| 公开巡检 | 配置接口200；私有接口及新润色能力接口401，路由存在 |
| 登录态只读巡检 | 复用既有授权会话，3个HTTPS GET均200/code=0；工作台可读，视频和润色能力均关闭，见[只读回执](production-authenticated-smoke.json) |
| 运行环境 | API及service环境文件保持；启动后无AI/数据库初始化失败或panic/fatal；CRM原Java进程及18087端口持续运行 |

原建房计价文件保持不变。装修动态预算、独立v5分享、AI视频修复和提示词润色均在本次源码内。11个功能文件沿用`@ts-nocheck`，类型诊断0不代表这些文件已完成全部语义类型建模。全包18个其他夹具跳过不包含本次22项AI视频MySQL用例。

## 数据库与开关

迁移前已完成完整MySQL备份：`/root/jdlg-backup-20261117-f8688da/database.sql`，163437593字节，SHA256及完成标记已校验。本次没有在生产执行恢复演练。

候选程序执行`migrate add_ai_video_prompt_polish.sql`，新增可空JSON配置列和3张表，4个业务唯一索引验证通过。默认总并发由1调整为3，配置版本由1变为2。既有MySQL钱包已是权威来源，无需Redis余额搬迁；本次未修改余额。

视频及润色保持关闭，视频供应商密钥未配置，任务、批次、预留费用与未知外部任务均为0。既有文本AI配置没有修改，也未调用任何真实AI接口。后续配置供应商时，需先核对价格和润色提交恢复路径，再开展付费验收。

服务号及公众号同步保持关闭；缺少的Secret、回调参数和模板等配置留待用户后续安排，不阻塞本次服务器部署。

## 小程序待确认

本次精确候选源码985个文件已逐一校验，版本`8.0.13`，AppID `wx94fdc48861913ede`。

- 上传工具：`wechatide -c codex upload`
- 当前任务：`confirmation_upload_f98ca8f2-455f-46aa-8f7f-acf34d4e9667`
- 最后状态：`pending`，等待开发者工具内确认。
- 用户完成确认后，先用`polling_task_result`查询原任务；不得直接重发上传。

目前未确认体验版指向，未提审，未正式发布，也未进行本次联合版本的真机角色验收。

## 回滚及注意事项

本次私有发布包位于`/root/jdlg-release-20261117-f8688da/package`，完整备份位于`/root/jdlg-backup-20261117-f8688da`；11月16日备份继续保留。含认证材料的二进制、环境快照和数据库备份不得提交Git或公开分享。

1. 后端回滚命令为`python3 /root/jdlg-release-20261117-f8688da/package/ops/binary_release.py rollback --backup /root/jdlg-backup-20261117-f8688da/backend`。实际程序位于`/www/wwwroot/jiadilingguang/api/applinux`；不要调用历史runner中的`service/bin`发布路径。
2. 管理台原目录备份位于`/root/jdlg-backup-20261117-f8688da/web/admin/original`。恢复时先核对当前入口及文件没有后续发布，资源先于入口恢复，保留新增哈希资源。
3. 应用回滚保留新增列、任务、费用及核对表。需要停用功能时使用[逻辑回退SQL](../../../service/sql/rollback_ai_video_prompt_polish.sql)，不要删表、清共享Redis或用旧数据库覆盖新业务记录。完整数据库恢复另需维护窗口及新数据处置方案。
4. 装修v5链接依赖本次后端。回退时应与小程序成套考虑，避免新客户端请求旧后端接口。

本轮审查P0/P1为0；P2为功能文件类型覆盖、历史runner路径和[润色提交未达服务端后查询404的恢复路径](https://github.com/phlong026/jdlg-development/pull/57#discussion_r4599525032)。后者需在开放润色前处理。真实供应商、真实扣费、CRM业务回归及真机验收均未执行。
