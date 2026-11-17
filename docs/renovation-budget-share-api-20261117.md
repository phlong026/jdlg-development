# 装修预算公开快照 API

装修预算沿用建房预算的主动公开机制：只有已登录且未被封禁的账号可以创建固定快照；快照公开读取，有效期 30 天，可由创建者撤销，并与建房快照共用每分钟创建限额和有效快照上限。

## 创建装修快照

`POST /api/v1/miniprogram/ai/renovation-cost/shares`

- 使用现有小程序签名、Token 登录认证和封禁拦截。
- 请求体在认证前限制为 64 KiB；未知字段、第二段 JSON、非法数值和不平衡金额返回 `400`，超限返回 `413`。
- 返回 `{ report_id, expires_at }`。`report_id` 是 32 位随机公开标识。
- 原建房接口 `POST /api/v1/miniprogram/ai/cost/shares` 保持 v4 和 16 KiB 限制不变。

请求体为 v5 固定快照：

```json
{
  "version": 5,
  "quoteType": "renovation",
  "estimatedTotalCost": "9.04万元",
  "estimatedUnitCost": "1004.44元/㎡",
  "totalLabel": "装修参考总价",
  "unitLabel": "参考单价",
  "basicInfo": [],
  "costBreakdown": [],
  "dynamicNotes": [],
  "renovation": {
    "pricingVersion": "renovation-dynamic-v2",
    "quantityBasis": "estimated",
    "totalAmount": 90400,
    "spaces": []
  }
}
```

`basicInfo` 顺序固定为：所在地区、装修面积、房屋状态、楼层数、层高、面宽、进深、户型、室内风格、装修标准。只有所在地区携带 `fullWidth: true`。

`pricingVersion` 接受 `renovation-reference-v1`（历史参考量）与 `renovation-dynamic-v2`（空间动态算量）。创建时保留实际版本；读取旧快照不重新计算。v2必须携带 `quantityBasis`，值为 `estimated`（按参数估算）、`measured`（按空间尺寸计算）或 `mixed`（空间尺寸中仍含柜体等估算项）；v1不带该字段。逐间尺寸及门窗扣减原始数据只保存在账户报告中，公开树只携带来源枚举与量价。

存在楼层/空间层高覆盖时，基础信息“层高”显示实际统一高度；高度不一致时显示“分层/空间设置”，不把全屋默认高度误作所有房间实测高度。

`costBreakdown` 顺序固定为：主材费、辅材费、人工费、管理费、设计费、税费。前三项合计等于空间工程基准；后三项分别只能为零或工程基准的 10%、4%、3.4% 四舍五入金额；六项合计等于 `renovation.totalAmount`。

空间固定为 `bedroom`、`living`、`kitchen`、`bathroom`、`balcony`、`common`，依次展示卧室、客餐厅、厨房、卫生间、阳台、全屋公共。分组 ID/名称也使用服务端固定白名单。项目单位仅允许 `㎡`、`m`、`项`、`套`、`个`、`位`、`块`、`根`。最多 128 个项目；所有 ID 在同层级不可重复。排除项目保留工程量和单价，但必须为 `included: false` 且 `amount: 0`。

## 读取与撤销

- `GET /api/v1/miniprogram/ai/cost/shares/:report_id`：匿名读取 v4 或 v5 固定快照，不重新计价；响应不包含创建者信息。
- `DELETE /api/v1/miniprogram/ai/cost/shares/:report_id`：沿用原登录签名接口，仅创建者可撤销 v4 或 v5 快照。

公开追踪仍只使用 `costsnapshot:<report_id>` 和服务端快照版本。地区、户型、面积、价格及完整快照不会写入追踪元数据。
