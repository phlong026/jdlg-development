# 公众号与短视频导入：依据、实现和验收边界

日期：2026-10-27。工作分支 `feat/phase3-content-sharing`，主仓库 `phlong026/jdlg-development`。主业务未发布、未执行生产新迁移、未读取真实 Cookie/网关密钥、未发送微信消息。现有COS凭据仅在原服务器内用于自生成测试图片，已通过私有读写实测；隔离MySQL核心链路也已通过。独立公众号视频采集环境及仅本机可调用的反向代理已部署，实际网络隔离、renderer sandbox、本机 HTTPS 和重启恢复检查通过；真实来源采集仍待链接验收。最新证据见[采集部署](capture-deployment.md)及[三模块补漏验收](capture-and-module-audit-20261027.md)。

## 1. 已找到的参考文件

| 来源 | 已核对文件 | 用途和边界 |
| --- | --- | --- |
| 用户提供的飞书剪藏分析 | [ZCode 分享报告](https://zcode.z.ai/cn/share/r0S5dfprm5TV2uW3IIpwmZcyciUc1Tnh)，本地 `/Users/honor.pei/Downloads/飞书剪藏插件逆向分析与迁移评估.md` | 报告实际对象为飞书 Web Clipper 1.0.40；第六章是公众号文内视频，第七章是异步采集、转存及来源关联。飞书云端 HTML 转换接口不能作为甲第可直接调用的公共 API |
| 爆款视频复刻 | `/Users/honor.pei/Documents/订单项目/乡墅爆款短视频复刻/server/app/viral_link.py` 的 `DouyidouLinkClient`；`viral_media.py`；`/api/viral/link-resolutions` | 当前可参照的是豆一豆网关，已实现抖音/小红书视频；不能推断它已经支持所有平台或小红书图集。复用协议事实、付费请求幂等和受控媒体思想，不复制源项目的 15 秒业务限制 |
| 元宝视频号协议 | `/Users/honor.pei/Documents/Codex/2026-10-24/new-chat/work/wxchannels-source/wx_channels_download-124f044235bc706fe796c043a14ff442a150ae5d/pkg/scraper/wxchannels/yuanbao.go`；`internal/workers/sph/worker.js` | 服务端 Cookie 换预览地址，再换视频地址；源码许可证为 MIT 加 Commons Clause，本项目没有 vendor 或搬入该项目源码，按协议事实独立实现 |
| 历史视频号核查 | `/Users/honor.pei/Documents/Codex/2026-10-24/new-chat/outputs/视频号服务器解析方案核查-20261024.md`；`mtotech实现与自部署核查-20261024.md` | 匿名解析只得到元数据的结果、第三方平台自有登录态的成功，都不能证明甲第自己的 Cookie 已可用 |
| 旧批量脚本 | `/Users/honor.pei/Documents/订单项目/甲地灵光二期/开发版916/resolve_sph_batch.py` | 短码/元数据辅助，不是完整视频转存实现 |

## 2. 三条导入通道

### 公众号正文、图片和文内视频

1. 提交文章 `/s` 或 `/s/...` 链接，移除来源追踪参数，服务端匿名读取文章。
2. 优先取 `#js_content`，再使用飞书研究中的备用正文容器。只选一个主容器，避免重复正文；清除脚本、微信控件、小程序组件等。`#js_common_share_desc` 只用于分享预览，不作为正文容器；即使位于备用正文容器内，也从结构化正文和排版 HTML 中排除。未取得正文时沿用 `article_body_missing`，不生成只有摘要的成功草稿。
3. 文字、标题、列表、引用、懒加载图片和可识别视频保持正文顺序，转换为既有 `article_v2`。图片使用资源 URL 哈希，不信任文章内可能重复的 DOM 图片 ID。
4. 从 iframe/受支持视频组件读取 `wxv_...`。只有标识、没有视频地址时，调用独立 Capture Worker；它在全新匿名 Chromium Context 中观察真实 `mpvideo.qpic.cn` 视频响应。
5. 视频地址是带签名的临时资源，随即下载并校验、写入受控媒体，再生成草稿。缺少任一图片/视频、超限或未支持的嵌入内容都会阻断完整草稿。
6. 保存安全的 `layout_html`，支持列表、引用和表格中的媒体引用；正文和列表超限明确失败，不截断后标成功。

**当前保真范围**：`article_v2.Body` 持久保存经过标签、属性和完整CSS值白名单校验的 `layout_html`；字号、颜色、对齐、间距、边框和嵌套结构沿原文保留，外部资源全部改为内部Asset引用。`ArchiveHTML`为空不代表排版丢失；本期不保存未清理的危险原HTML。H5二次清理后渲染，后台与小程序原排版正文只读，修改标题和封面时保持正文原始内容。小程序将视频拆成原生组件并闭合/恢复祖先结构；复杂表格内视频的真实显示仍需真机和指定文章比较，不能据此承诺任意CSS、浮动多栏或逐像素复制。

本次对报告样例 `https://mp.weixin.qq.com/s/v0OiILpavyPe4PDHaqf_KQ` 做了一次无凭据匿名 GET，返回 18165 字节的微信风险控制页面；解析器返回 `risk_control`，没有生成伪文章或触发验证码操作。未运行真实 Chromium 采集，因此没有当前文内视频播放成功证据。

### 抖音和小红书视频

- 固定 GET `https://gateway.diadi.cn/api/parse`。
- 仅 `app_id` 和标准化 `url` 参加查询排序编码，`Sign = lowercase(MD5(encoded_query + app_secret))`，与源项目协议一致。MD5 仅为供应商协议，不用于本项目密码保存或登录鉴权。
- 服务端独立保存网关凭据；不复用小程序/服务号 AppSecret。
- 校验平台原生内容 ID，解析标题、作者、视频及封面。没有视频不能冒充完整导入。
- 25 秒超时、2 MiB 响应上限、不自动跟随重定向。已发送付费请求后遇到超时、断流、截断 JSON、无法判断的成功体均保留 `uncertain`，不自动再收费解析。
- 视频仍使用该供应商；小红书纯图文另走公开页SSR适配，不臆造网关图集字段。

### 小红书公开图文

- 保存完整分享链接中的 `xsec_token` 和 `xsec_source`，只读取允许的平台公开页面；不传登录Cookie。
- 读取 `window.__INITIAL_STATE__` 的 `note.noteDetailMap[noteID].note`，核对URL、映射键和正文中的原生ID一致；提取完整描述、作者及有序图集，优先正文图片而非缩略图。
- 最多50张，任一图片缺失、无法读取或媒体校验失败都不生成完整草稿。支持JPEG、PNG、GIF和WebP，不因当前CDN使用WebP丢图。
- 只有确认类型为视频才调用配置的付费解析器；空公开页、未知类型、缺图及风控不会触发收费回退。
- 当次真实样例复验已退化为无完整详情的公开壳页，返回 `invalid_xhs_response`，未标记导入成功。新样例由用户后期补充；详细页面依据见[xhs证据](xhs-gallery-evidence-20261027.md)。

### 独立视频号：元宝实验通道

1. POST `https://yuanbao.tencent.com/api/weixin/get_parse_result`，正文为 `{type:"video_channel_url",url,scene:1}`，Cookie 只发送给这个第一跳。
2. 验证 `playable_url` 是 `channels.weixin.qq.com/finder-preview/...`，取 `token`、`eid`。它是预览页，不是 MP4；第一跳来源 ID 与实际 `eid` 冲突时拒绝归档。
3. POST `https://channels.weixin.qq.com/finder-preview/api/feed/get_feed_info`，传 `baseReq.generalToken`、`exportId`，补 `_rid`、`_pageUrl` 和按查询编码生成的 Referer；第二跳不传元宝 Cookie。
4. 优先 H264，其次普通 `videoUrl`，再 H265。取得媒体地址后仍走同一下载/校验/受控存储链路。
5. `decodeKey` 只接受缺失/null/空串/0/"0" 为无加密；其他值（包括对象、数组和布尔值）都阻断。本期没有混入来源项目的视频解码代码。
6. Cookie 缺失/过期、风控、仅有元数据、加密视频分别明确失败。没有 PC 抓包回退、自动过验证码、循环换 Cookie 或固定用户/设备头。

此通道为实验接入，默认关闭；非公开稳定 API，仍需甲第自己的有效登录态及真实样例验证。

## 3. 复用和新增

| 环节 | 直接复用 | 本增量新增 |
| --- | --- | --- |
| 权限和账号 | 小程序签名认证、用户状态、`tracking_access.can_manage` 与当前 owner 授权 | 任务按 tenant+actor 隔离，读取/重试重新核验当前 owner；不接受客户端指定 actor、tenant、Cookie |
| 导入任务 | 主服务 MySQL 连接 | `content_import_jobs`，幂等键、语义冲突检测、队列、租约、解析证据、下载恢复及受控重试 |
| 平台解析 | 既有项目协议研究 | `service/contentimport` 三条通道；`service/contentcapture` 及独立命令，不新增 Go/Node 依赖 |
| 媒体 | Asset、媒体元信息、草稿访问权限、签名预览与已发布版本访问控制 | 公网连接地址检查、平台 CDN 白名单、大小/完整性校验；0700 目录、0600 文件；同 owner 媒体缓存、缺失对象恢复 |
| 草稿 | 保存、编辑、预览、发布/撤回、`article_v2` 多端阅读 | 任务到草稿的同事务幂等映射、私有来源记录、所有 remote 引用换成已保存的 Asset ID |
| 手机页面 | 现有内容工作台和编辑器 | 链接/内容归属/明确权属选择、平台配置状态、任务状态、可重试动作；账号/页面代次隔离轮询 |
| 分享和通知 | 首批实现的 `Share.Sharer/Parent`、自有域名接收页、原生 st、消息中心和服务号投递链路 | 导入后进入同一草稿发布流程，发布后沿用现有分享和实际分享人浏览提醒；不另造统计/消息数据库 |

COS：已实现私有适配，复用现有服务端COS凭据和桶/地域配置，使用独立私有前缀。写用户内容前先用非敏感canary验证匿名HEAD和单字节GET均被拒绝，写后再核验私有性与SHA256；不返回公共static地址。导入与上传按“数据库待完成记录→私有写入→确认Asset”执行，数据库提交结果不明时保留恢复线索，不删除可能已被引用的对象。授权后读取使用单段Range有界流、8路并发和30秒写入期限。已在原服务器真实验证；存量文件切换前仍须受检转存，旧待完成对象的后台清理尚无自动调度。见[私有COS边界](private-cos-storage.md)。

公众号来源 gh_id/名称仅作为素材溯源保存；不能据此自动绑定服务号或向来源公众号发文章。当前 API 不开放客户端自填 target_mp_appid；发布回公众号是独立范围。服务号“华匠邦建科”与内容来源公众号是不同业务身份，凭据和授权分开。

## 4. API 和状态

入口：`/api/v1/miniprogram/workbench/imports`，全部使用小程序签名认证和明确管理授权。

| 方法 | 路径 | 行为 |
| --- | --- | --- |
| GET | `/capabilities` | 返回 enabled/platforms、配置门禁原因及采集服务短期探测状态；探测失败明确显示未就绪，不代表来源文章已导入成功 |
| POST | 根路径 | source_url、owner_id、rights(own/authorized)、idempotency_key；返回任务，不同步等待视频完成 |
| GET | 根路径 | 当前账号、当前授权归属的最近 20 条任务 |
| GET | `/:id` | 当前账号任务；不暴露租约、内部 Document、媒体临时地址或密钥 |
| POST | `/:id/retry` | 只允许服务端判定的可恢复失败；不接受客户端自定状态 |

常规状态：`queued → resolving → downloading → ready`；异常为 `failed / blocked / uncertain`。ready 只表示所有资源已保存、可编辑草稿已生成，**不表示发布或真机验收通过**。

恢复规则：

- 发起解析前先写 request_sent；解析结果持久化后，重启从已保存结果继续，避免再次收费。
- 已发送但没有结果的过期租约保留 uncertain；同幂等键重复提交返回同一任务。
- 免费公众号读取/采集超时、修复后的账号配置可由用户手动重试；已解析任务的可恢复下载/本地写入失败继续复用快照；用户明确重试 `media_source_expired` 时例外清空旧快照，重新解析一次，可能产生供应商解析费用。
- 超大、无效媒体/正文等确定性失败不反复执行相同输入。
- 媒体临时地址返回401/403/404/410时，标记不可访问/已失效，不反复复用旧签名。服务端返回 `can_retry=true` 时，用户点击重试会清除旧快照并重新排队解析；历史不可重试任务仍可“重新发起”，先填入原链接并清除权属/请求标识，再确认权属提交。结果不明的付费请求禁止自动或手动重试。
- 页面隐藏/卸载/切换账号时丢弃旧请求；重试成功立即更新新状态并维持轮询，旧列表响应不能覆盖新提交结果。

限制：单账号最多20个待处理任务，提交/重试最多20次/分钟；当前进程单worker、任务间隔30秒。图片10MiB、MP4视频500MiB（2026-11-11：生产导入改为私有临时文件流式下载、ReaderAt校验和文件转存，不把500MiB整体读入内存），最多50个独立媒体；超限明确提示失败。未将源视频复刻业务的15秒时长限制带入甲第。

## 5. 默认关闭的运行配置

新迁移：`service/sql/phase3_content_import_up.sql`。不会自动执行。回滚优先关闭开关并保留任务证据；down.sql仅用于经备份确认、尚无有效任务的隔离环境，不能删库重建来重放收费请求。

| 配置 | 含义 |
| --- | --- |
| CONTENT_IMPORT_ENABLED=true | 导入总开关；还需工作台开关、私有媒体实现及schema校验通过 |
| CONTENT_MEDIA_STORAGE / CONTENT_MEDIA_COS_BUCKET / CONTENT_MEDIA_COS_REGION / CONTENT_MEDIA_COS_PREFIX | filesystem或cos；COS桶/地域为空时复用既有配置，私有前缀默认jd-content-private |
| CONTENT_IMPORT_DOUYIDOU_APP_ID / CONTENT_IMPORT_DOUYIDOU_APP_SECRET | 独立解析供应商凭据，只放服务器私有环境 |
| CONTENT_IMPORT_YUANBAO_ENABLED=true | 独立实验通道开关 |
| CONTENT_IMPORT_YUANBAO_COOKIE_FILE | 服务器自有绝对路径、普通文件、权限0600，最大16KiB；不读用户浏览器 Profile |
| CONTENT_IMPORT_CAPTURE_URL / CONTENT_IMPORT_CAPTURE_SECRET | 固定HTTPS Capture Worker入口和至少32字符共享密钥；不从用户链接推断入口 |
| CONTENT_IMPORT_CAPTURE_HEALTH_URL / CONTENT_IMPORT_CAPTURE_DIAL_ADDRESS | 明确的 readiness 路径和可选本机固定 Dial；保持域名 TLS 校验，专用 transport 不走环境代理 |

独立浏览器进程还需 `CONTENT_CAPTURE_ENABLED`、`CONTENT_CAPTURE_NETWORK_ISOLATED`、本机 `CONTENT_CAPTURE_CDP_URL`、`CONTENT_CAPTURE_SECRET`。主服务的 CONTENT_IMPORT_CAPTURE_SECRET 与采集进程的 CONTENT_CAPTURE_SECRET 必须一致。详见 `service/contentcapture/README.md`。必须有独立 Profile、Chromium sandbox、网络命名空间及出口防火墙；DNS预检不等于浏览器连接被钉住，不能用一个环境变量代替实际隔离。Worker仅监听本机地址，经受控HTTPS反代接入；不连接用户当前浏览器、不使用 --no-sandbox。

## 6. 验收与剩余交付

本地验证覆盖：网关签名/请求格式、Cookie不跨host、解码/来源ID异常、安全原排版与媒体完整性、公开图集、浏览器wire协议/隔离门禁、超时和内容上限、任务租约/重启/幂等、当前授权、私有媒体保存、草稿幂等、手机账号切换及轮询恢复。另已通过隔离MySQL真实迁移/并发任务/草稿发布/分享事件/站内消息/撤回链路，以及原服务器真实COS读写测试。数据库内容是合成HTML/PNG，CDP仍为受控测试；它们不等于真实来源链接端到端验收。

启用前还需要：

1. 隔离MySQL核心流程已通过；部署前仍须按真实目标数据库版本、备份和权限核验迁移及回退。
2. 配置供应商账号、受保护的元宝登录态；逐平台真实样例跑通“链接→完整媒体→草稿→播放”，验证无水印结果，不只验证HTTP200。
3. 隔离Chromium环境已部署并通过实机检查；主业务需在发布时显式加载独立 `main-service.env`，再验证真实文内视频采集、签名资源转存及风控状态。
4. 以用户后期补充的真实链接验收原排版、完整图集和COS转存组合；复杂样式逐篇比较，存量素材切换COS前完成受检迁移。
5. 微信开发者工具编译WXML，真机验证文章排版和上传/导入视频播放；原生卡片和自有域名链接分别验证实际分享人、浏览事件、提醒对象。
6. 服务号独立AppID、模板字段、关注/身份绑定和真实投递条件仍需核验；现有“华匠邦建科”账号名称不能代替这些配置。URL Link真实调用、Nginx候选配置加载和服务号送达仍未发生。

正式合同全部确认范围和2026-11-15交付目标保持；本增量不是三期整体完工证明。

本地检查、独立审查与回退记录见 [导入增量验证记录](content-import-verification-20261027.md)。
