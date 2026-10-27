# 三期导入素材私有 COS 存储边界

## 结论

三期导入的公众号图片、公众号视频、小红书图文和外部视频使用 `service/contentstorage` 存储。该包只接受服务端调用，不产生、返回或记录 COS 公开 URL。

存储对象键固定为：

```text
<private-prefix>/<sha256(tenant)>/<32-char-base64url-asset-id>
```

默认私有前缀为 `jd-content-private`。构造器会拒绝任何包含独立 `ai_assets` 路径段的配置，避免与历史公有素材路径混用。

## 服务端配置

路由集成层负责读取环境变量，存储包本身不读全局环境：

| 环境变量 | 用途 | 要求 |
| --- | --- | --- |
| `CONTENT_MEDIA_STORAGE` | 选择存储实现 | 只有值为 `cos` 时启用 |
| `CONTENT_MEDIA_COS_BUCKET` | COS 存储桶 | 必须是带 APPID 的完整名称，例如 `example-1250000000` |
| `CONTENT_MEDIA_COS_REGION` | COS 地域 | 例如 `ap-guangzhou` |
| `CONTENT_MEDIA_COS_PREFIX` | 私有对象前缀 | 可选，默认 `jd-content-private`；不得使用 `ai_assets` |
| `COS_SECRET_ID` | 服务端 COS 凭据 | 复用现有服务端配置，不下发客户端 |
| `COS_SECRET_KEY` | 服务端 COS 凭据 | 复用现有服务端配置，不下发客户端 |

最小构造方式：

```go
store, err := contentstorage.NewCOSStore(&contentstorage.Config{
    Enabled:       os.Getenv("CONTENT_MEDIA_STORAGE") == "cos",
    Bucket:        os.Getenv("CONTENT_MEDIA_COS_BUCKET"),
    Region:        os.Getenv("CONTENT_MEDIA_COS_REGION"),
    PrivatePrefix: os.Getenv("CONTENT_MEDIA_COS_PREFIX"),
    SecretID:      os.Getenv("COS_SECRET_ID"),
    SecretKey:     os.Getenv("COS_SECRET_KEY"),
})
```

`Config` 为 `nil` 或 `Enabled=false` 时，构造成功但所有存储操作返回 `ErrUnavailable`。这保证未显式开启时不会意外使用旧公有存储路径。

## 调用契约

```go
type Store interface {
    Put(ctx context.Context, tenant, assetID string, data []byte, mime string) error
    Get(ctx context.Context, tenant, assetID string, max int64) ([]byte, string, error)
    Head(ctx context.Context, tenant, assetID string) (Metadata, error)
}
```

- `tenant` 是服务端已鉴权的租户标识，不得直接使用客户端传入且未校验的值。
- `assetID` 必须为恰好 32 个 base64url 字符。
- 单对象上限为 50MiB。`Get` 的 `max` 还必须是 `(0, 50MiB]` 内的业务上限。
- `Metadata` 只包含 `Size`、`MIME` 和 `SHA256`，不包含对象键或 URL。
- `COSStore.ObjectKey` 只供服务端集成和测试使用，不得放入客户端 DTO、通知载荷或分享参数。

## 写入和隐私验证

`Put` 按以下顺序失败关闭：

1. 在同一存储桶和私有前缀下创建随机 `_privacy-canary` 对象。Canary 只包含固定的非敏感探测字节，使用与真实对象相同的私有 ACL、条件写和元数据校验。
2. 对 canary 执行已签名 `HEAD` 和匿名 `HEAD`。只有匿名返回 403/404 且 canary 成功删除，才允许继续。任一失败都保证真实用户字节上传次数为零。
3. 使用已签名 `HEAD Object` 查询真实目标键。已有对象的长度、MIME 或 SHA256 不一致时返回 `ErrConflict`，不写入。
4. 上传请求同时携带 `x-cos-acl: private`、`If-None-Match: *` 和 `x-cos-forbid-overwrite: true`，并将 SHA256 保存为 `x-cos-meta-sha256`。
5. 如果条件写返回 409/412，重新读取对象。只有元数据完全相同才视为幂等成功，否则返回 `ErrConflict`。
6. 上传后再使用已签名 `HEAD Object` 确认长度、MIME 和 SHA256。
7. 最后使用完全独立、无签名、无 Cookie Jar 的 HTTP 客户端，对 COS 官方存储桶域名执行匿名 `HEAD`。只有 403 或 404 表示隐私验证通过；200 返回 `ErrPublicObject`，重定向或其他状态返回 `ErrUnavailable`。

已存在且内容一致的对象也会重新执行第 7 步，不因幂等快速返回而跳过隐私证明。对于本次调用刚刚创建的真实对象，写后完整性或隐私校验失败时会用独立的短超时上下文删除；删除失败统一返回 `ErrUnavailable`。代码不会删除调用前已经存在的对象。

## 读取完整性

`Get` 在下载前先用 `HEAD Object` 取得长度、MIME 和 SHA256；下载时限制实际读取字节数，然后校验：

- 实际字节数必须等于 `HEAD` 返回的长度；
- 实际 SHA256 必须等于对象元数据；
- `GET` 响应包含 `Content-Type` 时，必须与 `HEAD` 一致。

任一校验失败都返回 `ErrIntegrity`，不向上层返回部分内容。

## 服务端范围流读取

`COSStore.Open` 为服务端代理播放提供有界流，不把整个对象读入内存：

```go
type Stream struct {
    Body         io.ReadCloser
    Status       int
    Size         int64
    Length       int64
    MIME         string
    ContentRange string
}

func (s *COSStore) Open(ctx context.Context, tenant, assetID, rangeHeader string) (*Stream, error)
```

- 先执行已签名 `HEAD`，校验完整对象的长度、MIME 和 SHA256 元数据；随后才发起 `GET`。
- 只接受单段 `bytes` 范围，支持闭区间、开放结尾和后缀长度。多段、语法错误或越界返回 `RangeError{Size: fullSize}`，并可通过 `errors.Is(err, ErrRangeNotSatisfiable)` 识别。
- 范围请求会被归一化为明确的 `bytes=start-end`，COS 只返回选中字节；上游必须返回 206、精确 `Content-Range`、精确 `Content-Length` 和一致 MIME。
- 无范围请求要求上游返回 200，读到 EOF 时边计算 SHA256 边与元数据比较，全程不缓存完整对象。
- 返回的 `Body` 会把可读字节严格限制在 `Length`；提前 EOF 或额外字节返回 `ErrIntegrity`。调用方必须持续读到 EOF 并关闭 `Body`，才能完成末端校验。
- 部分范围无法单独重算完整对象 SHA256，因此只校验选中长度与上游范围响应。完整对象一致性依赖 HTTPS 传输、调用前的已签名 `HEAD` 和不可变对象契约。

路由层应使用不大于 32KiB 的固定缓冲区转发 `Body`，并在应用级限制同时打开的 COS 流数量；这两项属于路由集成边界，不在存储包内建立全局状态。

## 上线前必须完成的真实环境验收

当前自动化测试全部使用可控 HTTP transport，没有使用真实凭据或访问真实 COS。部署前需要在非生产私有前缀完成：

1. 触发一次 canary 预检，确认官方 COS 域名匿名 `HEAD` 返回 403 或 404，且 canary 已删除。
2. 用已鉴权服务端执行 `Head` 和 `Get`，确认元数据与字节完整。
3. 用同一 `assetID` 尝试写入不同内容，确认返回冲突且旧对象未改变。
4. 检查存储桶版本控制状态。腾讯云官方 `PUT Object` 文档明确说明：版本控制已开启或已暂停时，`x-cos-forbid-overwrite` 无效。因此必须在目标存储桶上实测 409/412 竞态行为，不能仅以本地模拟测试作为不覆盖验收证据。

本次实现不会修改存储桶策略或存储桶 ACL。如果新对象写后匿名检查发现 200，系统会删除该次调用刚创建的对象，且该次导入不得进入“已就绪”状态。调用前已存在的对象即使隐私检查失败也不会被删除。

## 官方依据

- [腾讯云 COS PUT Object](https://cloud.tencent.com/document/product/436/7749)：`x-cos-acl`、`x-cos-meta-*`、`x-cos-forbid-overwrite` 及版本控制限制。
- [腾讯云 COS ACL](https://cloud.tencent.com/document/product/436/30752)：对象私有权限与官方域名访问语义。
- [腾讯云 COS 访问控制基本概念](https://cloud.tencent.com/document/product/436/30749)：默认私有原则和匿名访问边界。
- [腾讯云 COS 查询对象元数据](https://cloud.tencent.com/document/product/436/112227)：`HEAD Object` 及 `x-cos-meta-*` 元数据。
