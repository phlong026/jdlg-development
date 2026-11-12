# 意向→线索转化通道（share_tracking lead conversion）

> 2026-11-13 更新。管理后台「分享访问记录」中，已登录访客可按服务端账号身份转为销售、合作或身份待确认线索。
> 需求与分期见工作区《分享访问记录×销售线索闭环打通方案-20261017》。

## 端点

`POST /api/v1/management/tracking/visitors/:key/lead`

- 权限：`tracking_access.can_crm`；随管理组继承 `X-Tracking-Request` 同源校验与 `private, no-store`。
- 响应：`{ lead_id, existed, lead_type, intent_level, demand_summary }`。

### 设计师本人客户转线索

`POST /api/v1/miniprogram/tracking/my-visitors/lead`

- 认证：沿用小程序完整签名登录；账号 ID 只取服务端已验证的 code session，不接受请求参数声明用户。
- 范围：服务端先按当前账号加载本人分享 owner，再调用本人访客详情校验目标访客确实访问过 `Sharer=u:<当前账号ID>` 的分享。不得借用管理端 `can_crm`、全租户访客列表或他人的分享证据。
- 请求体：`{"visitor":"u:77"}`。`visitor` 必须是当前本人客户列表返回的规范 `visitor_key`。
- 成功响应：`{"lead_id":123,"existed":false}` 表示本次新建线索；`{"lead_id":0,"existed":true}` 表示该账号已有未关闭线索。已有线索属于管理域，设计师端只获得幂等结果，不返回其真实管理域 ID。
- `GET /api/v1/miniprogram/tracking/my-visitors/entries?visitor=u:77` 同时返回 `can_convert_lead`，仅表示当前访客满足身份门控；最终写入仍重新执行本人范围、身份和线索服务检查。
- 响应继续使用 `Cache-Control: private, no-store`。

常见错误：

| HTTP | 场景 |
|---|---|
| 400 | 请求体缺少或使用非法 `visitor` |
| 401 | 小程序签名登录无效或已失效 |
| 403 | 当前账号没有本人分享查看范围，或账号状态不再允许访问 |
| 404 | 目标访客不存在，或没有访问当前账号本人分享的证据 |
| 422 | 匿名/已删除访客；设计师、施工队、建材商、机构用户等 B 端身份；企业主及其他未知身份值均 fail-closed |
| 503 | 本人范围或客服线索服务暂不可用 |

本人转换按 `userID` 在当前服务进程内串行，并由 `CreateLead` 再次查重。`customer_leads` 当前没有“每个用户只能有一条未关闭线索”的数据库唯一约束，因此该保证不覆盖多实例并发，也不覆盖另一个未采用同一锁的线索写入口同时插入。2026-11-05 发布前现场只检测到一个 `/api/applinux` 主进程（PID `3993339`，二进制摘要前缀 `45ef83`），符合本接口当前的单进程前提；每次发布仍须重新核验唯一进程，不能把这条现场记录当作长期架构保证。

## 门控规则（与前端 identityBadge 同口径）

| 访客状态 | 结果 |
|---|---|
| 匿名 / 已删除（非 identified） | 422，提示引导留资 |
| 已识别但身份未选（identity_type 为空） | 放行为 `identity_pending`，进入独立待确认队列，不计入销售漏斗 |
| 设计师 / 施工队 / 建材商 / 机构用户 | 放行为 `partner`，进入合作队列，不计入销售漏斗 |
| 企业主及其他未知身份值 | 422，fail-closed |
| 乡墅业主 | 放行 |

三类转化均复用同一个业务联系授权事务：未得到当前版本授权或账号无可用绑定手机号时均不创建线索；`account_bound` 手机号也不会绕过授权进入后台或 CRM。

CRM 桥启用后不阻断转化：线索作为投递记录写入，由 outbox 交给 CRM 承接；线索工作台保持只读，销售跟进仍统一在 CRM 客户档案中完成。

## 转化语义（查重）

- 转化前先查 `customer_leads` 中该 `UserID` 的未关闭线索（status IN new/contacted）。
- **已有**：不改动原线索任何字段，仅追加一条 `share_intent_convert` 事件（携带证据快照），响应 `existed=true`。
- **没有**：`CreateLead`（source=`share_tracking`，status=new）。`CreateLead` 内部会再次调用 `findLatestOpenLeadID` 缩小重复窗口，但数据库没有未关闭线索唯一约束，不能据此宣称跨进程并发下必然只产生一条。
- 意向等级：tracking Intent 传入 `AnalyzeLeadIntentFromInput`，其"外部判定高意向不降级"保护使最终等级 = max(关键词打分, tracking 意向)。

## 证据快照（customer_service_events.payload）

- `visitor_key` / `intent_level` / `intent_reasons` / `intent_evidence`（截断至 20 条）
- `metrics`：sessions / page_views / visible_ms
- `top_contents`：最近消费的素材（≤3 条，title/type/page_views/visible_ms）
- `converted_by` / `converted_at`

`demand_summary` 由同一事实生成（"近30天到访 N 次…分享追踪判定意向：X"），只含指标与素材标题，不虚构需求。

## 隐私治理联动

- 访客行使删除（`DeleteVisitorProgress`）后：tracking 侧记录消失，该访客不再出现在候选列表。
- 已转化线索的**联系方式等业务字段保留**（线索是承接载体）；行为证据快照属派生数据，随访客删除时**不主动清除**存量事件快照——快照不含个人身份标识（仅 visitor_key 与行为计数），如需彻底清除按事件清理流程处理。此口径与 `intent_override_history` 的派生数据策略一致。

## 不伪造业务源

`share_tracking` 来源的线索是**运营判断**，不是服务端验证的业务事实：不写 tracking `business` 事实、不提升 Intent 指标（`docs/share-tracking/business-sources.md` 原则）。全自动转化（`share_tracking_auto`）属 P1，默认关闭，需规则阈值在线校准后再评估。
