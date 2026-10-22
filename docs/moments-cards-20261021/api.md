# 预算报告公开分享接口

预算报告在结果页准备朋友或朋友圈分享时创建公开快照。快照保存当次 v4 参考测算的显示内容，创建后不可修改；重新测算和重新创建分享不会覆盖旧快照。它不是正式工程核价、付费报告或付款依据。

接口根路径：`/api/v1/miniprogram/ai/cost/shares`。

## 创建

`POST /api/v1/miniprogram/ai/cost/shares`

沿用小程序现有签名认证：`token`、`token-signature`、`sin`、`md5-signature`、`pass`、`tm`。前端通过 `utils/parameter` 的 `generateRequestParams`、`paramsToHeaders` 生成头；签名路径使用上述完整 API 路径。用户身份只读取服务端认证上下文，并沿用封禁拦截。每账号每分钟最多创建 10 次。

创建频率和容量由 Redis 原子脚本统一约束：每账号每个 60 秒窗口最多成功创建 10 次，最多持有 100 份有效快照。进程重启或多实例不会重置额度；到达任一上限返回 `429`。过期索引在创建时清理，创建失败不占用额度，本人撤销时原子删除快照并释放容量。旧版本已经创建的快照继续按原有效期读取，新建快照纳入额度。

脚本沿用项目现有 Redis `EVAL` 能力，把容量检查、快照写入和计数放入同一原子执行边界。实现依据见 [Redis Lua 脚本原子执行说明](https://redis.io/docs/latest/develop/programmability/eval-intro/)。

JSON 请求体直接使用 v4 结果 DTO，不包 `result`：

```ts
type CostEstimateShareResult = {
  version: 4;
  estimatedTotalCost: string; // 正数 + 万元
  estimatedUnitCost: string;  // 正数 + 元/㎡
  totalLabel: '参考总造价' | '已测算部分合计';
  unitLabel: '参考单价' | '已测算部分单价';
  basicInfo: Array<{ label: string; value: string; fullWidth?: boolean }>;
  costBreakdown: Array<{
    name: string;
    amount: number;
    amountText: string;
    percent: number;
    note: string;
    estimable: boolean;
  }>;
  dynamicNotes: string[];
};
```

严格限制：

- 请求体在签名认证读取之前限制为最多 16 KiB，超限直接返回 413；解码时再次校验，拒绝未知字段、额外 JSON、旧版本、无效数值和控制字符。不要提交图表显示字段 `color`。
- `basicInfo` 必须依次为 14 项：所在地区、建筑占地面积、总建筑面积、楼层数、首层高度、其他楼层高度、地下室形式、建筑风格、结构形式、屋顶形式、外立面样式、建筑基础、装修标准、门窗标准；每项值最多 100 字符。
- `costBreakdown` 必须依次为地基工程、主体工程、屋顶工程、门窗工程、外墙装饰、水电及其它费用；有地下室时可追加第七项地下室工程。金额为有限非负数，百分比 0–100，金额展示文字最多 40 字符，说明最多 300 字符。
- `dynamicNotes` 最多 10 项，每项最多 300 字符；两组总价、单价标签必须配套。
- 不接受 `user_id`、任务编号、任意附加字段或管理信息。此接口不读取私人 AI 任务、付费报告或其他账号的缓存。

成功 `200`：

```json
{"code":0,"data":{"report_id":"32位随机URL安全标识","expires_at":"2026-11-20T12:00:00Z"}}
```

示例中的 ID 和时间仅说明格式。实际 ID 由服务端生成，192 位随机熵，不可通过账号或序号枚举。

## 读取

`GET /api/v1/miniprogram/ai/cost/shares/:report_id`

无需登录。任何持有效分享 ID 的访客都可只读打开这份报告。响应不会返回创建者、用户 ID、token、私有任务或管理字段。公开读取沿用有界访客限流，每来源每分钟最多 60 次，不信任未配置代理传来的伪造 IP 头。

成功 `200`：

```json
{"code":0,"data":{"report_id":"实际ID","expires_at":"实际过期时间","result":{"version":4}}}
```

上例省略 `result` 的其余字段；实际返回创建时的完整白名单 DTO。前端收到 `report_id` 后必须使用该接口返回的快照，不能回退到接收者本机预算。卡片标题、图片数据和落地页均取同一快照。

## 撤销

`DELETE /api/v1/miniprogram/ai/cost/shares/:report_id`

使用与创建相同的签名认证，仅创建者能撤销。其他账号和不存在的记录统一返回 `404`，避免暴露归属信息。成功 `200`：

```json
{"code":0,"data":{"revoked":true}}
```

## 有效期与错误

快照保存在现有 Redis，键前缀 `cost_estimate:share:`，创建后 30 天自动过期，读取时也核对绝对过期时间。Redis 被清理或丢失时链接会失效；当前实现不承诺永久保存。无需数据库迁移或新增配置。所有成功读写响应设置 `Cache-Control: no-store`，防止撤销后沿用缓存内容。

每次存储操作受请求上下文和 3 秒超时约束；失败记录操作上下文，不记录报告正文或分享 ID。外部响应不返回基础设施错误细节。

| HTTP 状态 | 含义 |
| --- | --- |
| 400 | DTO 字段、版本、数量或内容无效 |
| 401 | 创建或撤销前需要有效登录和现有签名认证 |
| 404 | ID 无效、报告不存在、已过期、已撤销或无撤销权限 |
| 413 | 请求超过 16 KiB |
| 429 | 创建或公开读取过于频繁，或账号有效快照已达上限 |
| 503 | Redis 或存储服务暂不可用；前端不得假装分享成功 |

## 验证

```sh
CGO_ENABLED=0 go test ./model ./route -run 'TestCostEstimateShare' -count=1
```

覆盖独立快照、匿名/其他账号读取一致性、响应隐私、创建认证、本人撤销、他人撤销拒绝、30 天过期、输入白名单及大小限制、跨实例创建限流和容量上限、并发配额竞争、过期索引清理、撤销释放容量、请求取消和 Redis 故障。原有 macOS `go-m1cpu` cgo 初始化在本环境会崩溃，因此验证采用 `CGO_ENABLED=0`；服务逻辑没有修改该依赖。

历史 DTO 契约探针曾以真实 `aicost.ts` 的 `onEstimateCost` 执行 2160 组建房参数组合，将产生的 v4 JSON 逐份送入当时的创建接口，全部通过；其中抽取 6 份匿名读取后与原始快照一致。最大 JSON 为 2114 字节。该探针使用本地内存 Redis，只记录字段兼容性，不代替本次新增的跨实例配额测试，也不代表生产部署或真机验收。
