# 独立 AI 视频工作区：实现与交付边界

本目录记录 2026-11-13 的独立模块开发和操作界面调整。范围依据 `docs/superpowers/plans/2026-11-13-designer-ai-video-taskbook.md`，供应商经用户确认采用秘塔 H3，灵石单价在后台独立配置。

用户先确认 [双屏设计稿](design-approved.png)，随后要求继续贴合现有小程序、首尾帧占位同比例，并将操作改为直接“生成视频”。这些补充优先于初版设计稿中的“查看报价”。

## 当前交付

- “我的”设计师工作台四入口固定为：我的项目、内容创作、AI 视频、客户跟踪；旧 AI 视频页面及任务接口保留。
- 独立分包提供创作首页、生成编辑器、素材、提示词、草稿/记录、任务详情。生成页复用产品现有导航、字体、背景与棕金色；首尾帧使用等宽、等高的 16:9 预览槽。
- 普通与混合参考分仓编辑；选择/移除首尾帧自动推导模式，禁止纯尾帧；参考素材按稳定媒体 ID 绑定，音视频时长读取服务端真实探测值。
- 草稿与提示词持久保存、修订冲突检查；切账号和迟到请求不能回填旧数据。未完成草稿不要求满足完整生成条件。
- 参数和输入变化使旧报价失效。界面在可用条件下自动获取报价，主按钮为“生成视频”；仍需服务端有效报价和幂等键才能创建付费任务。
- 独立后台 `/admin/ai-video-studio` 配置四类模式、限额和价格目录，并查询真实任务及费用表；配置保存使用版本校验。
- 图片、视频、音频上传执行实际类型检查；音视频使用 ffprobe 探测。本人素材查询、短期签名访问、显式留存、内容草稿和项目笔记采用有独立接口与权限验证。

## 真实执行与账务代码

用户确认界面后，本轮补齐秘塔 H3 执行器和灵石账务：共享钱包以 MySQL 为权威，批次、子任务、预留、用户流水同事务；服务端持久提交与查询，明确失败逐条退款，成功只结算一次。有效报价冻结单价和计费单位，后台改价只影响新报价。客户端断网重进先只读查询，显式恢复沿用原请求键。

原固定关闭的执行/账务门禁改为初始化与执行器实际就绪状态。运营开关、独立供应商凭据、有效档位价格和素材依赖仍需同时满足，缺配置不会以零价或假任务放行。当前独立凭据尚待填入，因此不能把代码和隔离测试称为已通过真实供应商生成。

完整费用规则、接口、钱包迁移及回退步骤见 [真实执行与账务交接](paid-execution.md)。**钱包切换必须停止旧实例并核对 Redis/MySQL 差异，不能直接混版本上线或仅替换旧二进制回退。**

## 剩余独立验收

- 本次只补齐真实生成/扣费代码，未实现可计费的 AI 润色，页面仍准确提示未开放。
- 项目笔记采用已有服务端校验，但小程序主动选择项目与笔记的完整入口仍未完成。
- 真实供应商四类输入、真机播放/相册、服务器部署及开发/体验版上传分别验收。原任务书 T01–T32 不能整体勾选通过。

## 接口与配置

新接口前缀：`/api/v1/miniprogram/ai-video-studio`；管理前缀：`/api/v1/management/ai-video-studio`。响应沿用 `{code,msg,data}`。

| 资源 | 接口 |
| --- | --- |
| 能力 | `GET /capabilities`，分别返回运营开关、执行/账务和媒体就绪状态 |
| 草稿 | `GET/POST /drafts`，`GET/PATCH/DELETE /drafts/:id`，写入使用 `expected_revision` |
| 提示词 | `GET/POST /prompts`，`PATCH/DELETE /prompts/:id` |
| 素材 | `GET /media`、`GET /media/:id`、`POST /media/uploads`、`POST /media/register` |
| 报价/批次 | `POST /quotes`、`POST /batches`（按实际配置开放），`GET /batches`、`GET /batches/:id` |
| 提交恢复/取消 | `GET /submissions/:idempotencyKey`（只读），`POST /batches/:id/cancel` |
| 成片 | `POST /results/:id/playback`、`POST /results/:id/retain` |
| 业务采用 | `POST /media/:id/adopt/content`、`POST /media/:id/adopt/project-note` |
| 后台 | `GET/PUT /config`、`GET /tasks`、`GET /tasks/:id`、`GET /ledger`、`POST /results/:id/reconcile` |

素材访问地址为 `/api/v1/ai-video-studio-media/:id?exp=…&sig=…`，使用 `studio:` 签名域，不能使用未配置密钥时的可预测回退值。

| 配置依赖 | 说明 |
| --- | --- |
| `AI_VIDEO_STUDIO_METASO_API_KEY` | 仅服务端读取的独立供应商凭据；不从参考项目复制，不回传后台或小程序 |
| `TRACKING_MEDIA_SIGN_KEY` | 必须显式设置，至少 32 字节；素材签名不使用默认回退密钥 |
| 现有内容媒体存储 | 复用私有 COS 或已配置的私有 `TRACKING_MEDIA_DIR`；不将外部 URL 注册为长期素材 |
| `ffprobe` | 视频/音频上传和留存的真实轨道、尺寸和时长探测 |
| 后台独立价格目录 | `ai_video_studio_prompt_optimize`、`ai_video_studio_768p`、`ai_video_studio_2k`、`ai_video_studio_768p_long`；未配置默认停用 |

秘塔适配固定使用原生 `/api/minimax/v2/video_generation` 与路径式查询接口；不会用只支持单图的 OpenAI 兼容接口承接混合参考。供应商没有已验证的提交幂等保证，超时、5xx 或丢失响应进入待确认，不能盲目重发。核对来源：[秘塔接入说明](https://metaso.cn/minimax-h3/new-api-guide)、[原生适配官方源码](https://github.com/meta-sota/ComfyUI-MiniMaxH3-API/blob/main/api_client.py)。

## 验证与回滚

本轮生成/计费代码的自动化与隔离数据库证据见 [付费执行验证](paid-verification.json)，独立审查见 [代码审查记录](paid-review.md)。早期 `verification.json` 保留界面阶段的历史记录，不代表本轮之后的生成实现状态。

最终界面截图与逐项对照记录见项目根目录 `design-qa.md`。离线预览通过 `node miniprogram/scripts/create-ai-video-studio-preview.js` 重建，目录名明确标记离线：只复制实际页面代码和现有庭院图片，模拟草稿/素材/报价，不读写真实 token、不调用供应商、不扣费。原生音视频使用本机夹具服务时加 `--media-origin=http://127.0.0.1:8769`，不会连接外部网络。截图不作为真实生成或真机验收证据。

新增表由独立 Store 初始化，不覆盖旧任务和钱包数据。回退先执行 `service/sql/disable_ai_video_studio.sql` 关闭新模块运营开关，再按提交逆序回退前后端；**保留全部 `ai_video_studio_*` 数据表、费用事实和私有媒体对象**。代码回退不等于删除用户数据。生产发布须按已有部署通道备份旧前后端并保持版本匹配，本轮未执行。

本机已有 `go-m1cpu` 的 cgo 初始化会崩溃，因此服务端使用 `CGO_ENABLED=0` 做可执行验证；不把该环境限制算作本次业务失败。MySQL 行为另在可丢弃、本机回环地址的 `mysql:8.4` 容器验证，不读取项目生产 DSN。
