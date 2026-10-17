# 意向→线索转化通道（share_tracking lead conversion）

> 2026-10-17 P0 交付。管理后台「分享访问记录」的客户视角中，满足条件的访客可一键转为销售线索。
> 需求与分期见工作区《分享访问记录×销售线索闭环打通方案-20261017》。

## 端点

`POST /api/v1/management/tracking/visitors/:key/lead`

- 权限：`tracking_access.can_crm`；随管理组继承 `X-Tracking-Request` 同源校验与 `private, no-store`。
- 响应：`{ lead_id, existed, intent_level, demand_summary }`。

## 门控规则（与前端 identityBadge 同口径）

| 访客状态 | 结果 |
|---|---|
| 匿名 / 已删除（非 identified） | 422，提示引导留资 |
| 已识别但身份未选（identity_type 为空） | 422，提示先补选身份（P1 将改为内容域占比自动放行） |
| 设计师 / 施工队 / 建材商 / 机构用户 | 422，B 端硬排除（渠道不是客户） |
| 未知身份值 | 422，fail-closed |
| 乡墅业主 | 放行 |

CRM 桥启用（`CRM_BRIDGE_URL` 已配置）时返回 409「客户已统一纳管」——与 `docs/customer-management/线索移交与跟进入口约定.md` 一致：加了企微的客户一律在客户档案跟进，线索工作台只读。

## 转化语义（查重）

- 转化前先查 `customer_leads` 中该 `UserID` 的未关闭线索（status IN new/contacted）。
- **已有**：不改动原线索任何字段，仅追加一条 `share_intent_convert` 事件（携带证据快照），响应 `existed=true`。
- **没有**：`CreateLead`（source=`share_tracking`，status=new）。`CreateLead` 内部按 `findLatestOpenLeadID` 兜底 upsert，并发下也只会归并到同一条。
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
