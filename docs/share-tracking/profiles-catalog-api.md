# 授权访客资料、素材筛选和访问素材接口

本合同对应本轮 T01、T03、T04。所有接口位于 `/api/v1/management/tracking`，继续使用管理登录、企微会话、tracking_access、tenant/owner 授权、保留期、删除屏蔽及查询预算。以下验证使用隔离内存仓库和 database/sql 测试驱动；没有访问或修改生产数据。

## 访客资料 T01

`GET /visitors` 与 `GET /contents/:id/visitors` 的 Visitor 增加可选 `avatar_url`；`nickname` 使用真实 `user_profiles.nickname`。这是用户提供的账号展示资料，不是实名或自然人身份认证。

- 只在授权且未删除的访问事实中收集规范的 `u:<正整数ID>`，去重后每批最多 200 个 ID。
- 匿名随机标识不解析用户表；其他 owner、已删除访客、不在当前事实中的账号不被补全。
- 生产适配器借用 tracking 查询的同一个 SQL 事务；SQL 只返回 user_id、nickname、avatar，JOIN users 排除已删除账号的孤儿资料。不会读取手机号、设备ID、微信凭据等字段，不创建缺失资料。
- 无资料或空昵称保留“已识别账号”；头像无效时省略。头像仅接受 HTTPS、无凭据/fragment，并屏蔽脚本、文件、本地路径及明显的本地/私网地址。不代用户抓取图片。
- 源查询失败返回服务错误，不伪装成搜索无结果；资料读取计入已有累计行数/字节数预算。
- `q` 保留原访客接口语义：昵称或有权 CRM 业务编号。新增 `visitor_q` 匹配昵称或访客标识。两者同时传入时分别匹配并取 AND。没有独立“备注”搜索源。

昵称/头像是查询时的当前资料，不伪称历史 as_of 资料。资料相关游标附带候选集 `profile_revision`，在昵称过滤前计算，覆盖会由“不匹配”改名成“匹配”的账号。继续 next_cursor 或 first_cursor 时资料变化返回 409，客户端应清空本次分页并刷新。原人工意向修正 revision 保持在同一游标中；不改变意向阈值或历史决策回放。

## 素材列表 T03

`GET /contents` 新增可选参数：

| 参数 | 含义 |
|---|---|
| content_q | 当前授权素材目录标题包含匹配（大小写不敏感） |
| visitor_q | 当前授权访问事实中的账号昵称或匿名/已识别访客标识包含匹配 |
| identity | all / identified / anonymous；默认 all |
| content_id | 可选限定素材ID |
| content_version | 可选正整数，必须同时限定 content_id；只选实际访问版本的事实 |

`content_q AND visitor_q AND identity`。标题选择素材；访客条件限制该素材的统计集合。不同框之间不做 OR，不在浏览器扫全部记录。旧 `q` 与 `intent` 不被偷偷解释为素材条件，仍按原兼容行为忽略；仅有管理权限、没有访问记录权限的账号不可按访客筛选。

Content 增加 `identified_count` 与 `anonymous_count`。PV、可见时长、最近访问、两类 UV 都来自相同的授权/时间/访客筛选事实。UV 按访客标识去重，同账号多个会话只算一个；返回 List 顶层两类计数是整个当前筛选集合去重结果，不是各素材 UV 相加。启用访客条件时，没有命中事实的素材不进入列表。没有访客条件时目录仍可返回零访问素材。

素材详情的访客、会话、事件请求应继承上述参数及 owner、start、end、as_of。素材列表和各详情端点各有自己的游标，不能互相使用。资料或标题搜索候选目录变化导致当前分页不稳定时返回 409。仅修改非搜索标题的展示不改变原目录 keyset 的指标快照。

## 访客访问素材 T04

`GET /visitors/:id/contents` 返回原 List 包装。items 按 `content_id + content_version` 聚合，字段为：

- content_id、content_version、route_key
- title、type、status、content_accessible
- cover（可选，有权且存在的历史封面）
- page_views、visible_ms、last_visit

只返回该访客在当前授权/时间范围实际访问过的素材版本，不把当前最新版本替代访问版本。标题/封面来自不可变访问版本，正文、任意原始源URL及整份媒体列表不进入该响应。

当前素材撤回、版本记录失效、owner 失权或原业务公开源下线时，保留素材标识和真实统计，并显示 revoked / expired / unavailable 占位；不返回旧封面或正文。旧业务素材只有受控 LegacyRoute 的实际公开源通过复核才可显示历史标题/封面，不把一般页面伪造成文章。

一般无 content_id 页面不制造素材卡。响应可附 `omitted_page_count`，表示本查询有多少个无素材标识的独立页面访问未进入素材清单；普通会话轨迹仍可查看这些页面。该值是页面访问标识数量，不是曝光或自然人数。

列表支持原 start/end/as_of、owner、sort、direction、limit、cursor，并输出 next_cursor / first_cursor。keyset 同时包含素材ID和版本，防止相同素材多个版本错页。点击某访问版本的轨迹需传 content_id 和 content_version。Sessions / Events 在事实层按版本过滤；原 include_related=true 仍是显式扩大到已选会话内相关页面的选项。

## 回归与边界

新增隔离回归覆盖真实 model 最小 SQL 扫描、单连接原事务/请求 context、分批和预算、匿名/删除/跨owner、中文搜索和空资料、非法头像、资料变化 next/first 409、人工修正游标保留、AND 筛选后统计/详情对账、迟到补报 as_of 排除、同素材多版本、撤回/失效占位、Legacy 公开性再核和一般页面排除。

真实 MySQL 查询计划/并发负载、已授权真实用户资料与双端联调尚需独立隔离环境验收。没有新增生产配置开关、阈值变更、部署或数据库迁移。

## 查看客户 / 线索 T02

`GET /visitors/:id/crm-target?owner=<明确归属>` 只用于已识别账号，要求当前 can_view 与 can_crm。没有明确 owner 时只有唯一可访问归属才可继续；不选第一条猜测。服务端重新检查删除墓碑、确切人工映射、当前管理/CRM会话及上游客户ID/业务编号/非合并非删除状态，2秒有界验证，返回 `target_kind=customer`、customer_id、business_number、owner_id；不返回外部URL、客户正文或自由文本依据。响应禁止缓存，读取不写用户或映射记录。

前端单次点击重验后仅跳转本应用 `/customer-management?tab=customers&customer=...`，CRM原详情端点再次鉴权，非法ID或无CRM能力不装载详情。匿名和未关联账号不展示CTA。旧请求遇到换访客/归属/权限、卸载或重复点击不会误导航；上游失败展示可重试错误，不把未核验的旧编号当可用目标。客户主档不是独立leadID，因此使用“查看客户 / 线索”准确说明对象。

### 头像与详情分页补充

头像仅接受可解析的 HTTPS、无用户凭据/片段的 ASCII 主机 URL；本地、私有、链路本地以及浏览器可能重新解释的十六进制/缩写/八进制数字地址不展示。非 ASCII 主机也不展示，可由资料源提供正规 ASCII 域名；此校验不声称能阻止正常公网域名后续 DNS 变更。匿名行永远不取账号头像，图片失败回退占位。

会话和事件详情游标分别绑定端点类型、访客或页面访问标识、完整授权范围和筛选；每次请求仍重新鉴权。返回 first_cursor 后，回到第一页必须回放该 token，以维持资料版本；范围切换、显式刷新丢弃分页栈和 token。旧格式游标返回409要求刷新，跨对象或权限范围游标拒绝，不能把它作为授权凭据。详情事件请求带实际被点击的 content_id/content_version；一般页面不携带虚构版本。

## 第二批：最近一次可见时长

Visitor 与 VisitorContent 的可选 latest_visit 只投影当前授权/筛选/快照内计数的最新page_view：page_view_id、started_at、visible_ms以及可选 content_id/content_version。选择依据是进入时间，不是最后心跳；同进入时间按ID升序。单次visible_ms沿用事实去重及当前窗口裁剪，零保留。只有跨窗carry-over累计而无范围内新访问时省略该对象；客户端缺字段显示未提供。累计visible_ms、last_visit原定义不变，不把最近一次称为当前在线或真人注意力。

Sessions的page_views标题每次重验当前素材访问状态；撤回、移归属、历史版本失效返回占位。原始事件/保留汇总里的旧标题不是当前读取授权。
