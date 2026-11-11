# 甲第灵光后端服务

基于 Go + MySQL + Redis + Gin + SessionID 的后端服务，使用策略模式实现多种登录方式。

依赖要求 Go 1.25.0 或更高版本。2026-09-16 客户管理发布使用 Go 1.26.8 构建；生产构建应使用已修复安全问题的工具链，验证记录见 [Go 发布安全验证](../docs/customer-management/Go发布安全验证-20260916.md)。

## 项目结构

```
service/
├── config/          # 配置模块
│   └── config.go   # 应用配置（MySQL、Redis、微信、服务器）
├── model/           # 数据模型
│   └── user.go     # 用户模型和数据访问层
├── component/       # 组件（数据库、Session等）
│   ├── database.go # MySQL连接初始化
│   └── session.go  # Redis Session存储初始化
├── function/        # 功能函数
│   ├── password.go # 密码加密/验证
│   └── wechat.go   # 微信API调用
├── processor/       # 处理器（策略模式）
│   ├── auth_strategy.go    # 登录策略接口
│   ├── wechat_auth.go      # 微信code登录策略
│   └── password_auth.go    # 账号密码登录策略
├── route/           # 路由
│   ├── middleware.go       # 中间件（认证、Session管理）
│   ├── miniprogram.go      # 小程序路由
│   └── management.go       # 管理后台路由
└── main.go          # 入口文件
```

## 功能特性

### 1. 策略模式实现登录
- **微信code登录** (`WechatAuthStrategy`)
- **账号密码登录** (`PasswordAuthStrategy`)
- 易于扩展新的登录方式

当前补充说明：

- 小程序微信登录现在会先通过微信 `code2session` 获取真实 `openid/unionid` 再识别用户，保证同一个微信号在不同设备上仍归属于同一个账号
- 小程序当前已支持同账号多设备登录，不再因为设备不一致而拦截登录
- 小程序 `POST /api/v1/miniprogram/login/password` 仍可携带 `device_id`，但不再把它作为设备绑定校验前提
- 旧的设备换绑接口已下线，后端不再提供单独的“换绑设备”能力
- 管理后台登录仍保持原样，不要求 `device_id`

### 2. API路由
- **小程序API**: `/api/v1/miniprogram/*`
  - `POST /api/v1/miniprogram/login` - 微信code登录
  - `POST /api/v1/miniprogram/login/password` - 小程序账号密码登录（`device_id` 可选）
  - `GET /api/v1/miniprogram/me` - 获取当前用户
  - `POST /api/v1/miniprogram/logout` - 登出
  - `GET /api/v1/miniprogram/templates/favorites` - 获取当前登录用户的收藏模板列表
  - `POST /api/v1/miniprogram/user/orders/:id/cancel` - 取消订单（当前仅支持待支付充值单）

当前 AI 视频补充说明：

- `POST /api/v1/miniprogram/ai/video/create` 当前已按老张视频接口实现
- 支持纯文生视频、单图动画视频、首尾帧动画视频
- 首尾帧模式下，后端会把前端传入的 `start_frame_url`、`end_frame_url` 或上传文件转换成老张接口要求的两个 `input_reference`
- 横屏默认模型为 `veo-3.1-landscape-fast-fl`
- 竖屏默认模型为 `veo-3.1-fast-fl`
- 当前视频结果仍由后端统一下载、加水印并上传到自家 COS 后再返回给前端

- **管理后台API**: `/api/v1/management/*`
  - `POST /api/v1/management/login` - 账号密码登录
  - `GET /api/v1/management/me` - 获取当前用户
  - `POST /api/v1/management/logout` - 登出
  - `GET /api/v1/management/dashboard/overview` - 后台总控台总览接口，返回今日新增用户、订单、任务、认证与异常摘要
  - `GET /api/v1/management/dashboard/trends` - 后台总控台最近 7 天趋势接口
  - `GET /api/v1/management/dashboard/todos` - 后台总控台待办与异常队列接口
  - `GET /api/v1/management/designers` - 后台设计师中心列表接口，支持按关键词、认证状态、主页公开状态筛选
  - `GET /api/v1/management/designers/:id` - 后台设计师中心详情接口，返回资料、认证、作品、评价与统计摘要
  - `PATCH /api/v1/management/designers/:id/visibility` - 后台切换设计师主页公开状态
  - `PATCH /api/v1/management/designers/:id/service-status` - 后台切换设计师接单开关
  - `GET /api/v1/management/membership-operations/overview` - 后台用户会员运营总览接口，返回会员用户、快到期、已过期和权限异常数量
  - `GET /api/v1/management/membership-operations/users` - 后台用户会员运营列表接口，支持按关键词、会员状态、下载权限筛选
  - `GET /api/v1/management/distribution/overview` - 后台分销邀请中心总览接口，返回邀请人数、邀请注册数、邀请付费数和奖励汇总
  - `GET /api/v1/management/distribution/inviters` - 后台分销邀请中心邀请人排行接口，支持按用户名、昵称、邀请码筛选
  - `GET /api/v1/management/distribution/rewards` - 后台分销邀请中心奖励明细接口，支持按用户名、昵称和奖励描述筛选
  - `GET /api/v1/management/content-analytics/overview` - 后台内容运营分析总览接口，返回模板总量、下载、解锁、互动和精选案例组数量
  - `GET /api/v1/management/content-analytics/download-ranking` - 后台内容运营分析模板下载排行接口
  - `GET /api/v1/management/content-analytics/engagement-ranking` - 后台内容运营分析互动排行接口
  - `GET /api/v1/management/content-analytics/new-templates` - 后台内容运营分析新上架模板表现接口
  - `GET /api/v1/management/content-analytics/low-conversion` - 后台内容运营分析低转化模板识别接口
  - `GET /api/v1/management/content-analytics/featured-cases` - 后台内容运营分析精选案例观察接口
  - `GET /api/v1/management/risk-control/overview` - 后台风控台总览接口，返回同设备账号、设备换绑、支付异常和任务失败概览
  - `GET /api/v1/management/risk-control/device-groups` - 后台风控台多账号同设备识别接口
  - `GET /api/v1/management/risk-control/device-changes` - 后台风控台近期换绑设备提醒接口
  - `GET /api/v1/management/risk-control/alerts` - 后台风控台异常支付 / 异常任务告警接口
  - `GET /api/v1/management/risk-control/users` - 后台风控台用户风险标签接口
  - `GET /api/v1/management/support-tickets/overview` - 后台售后 / 异常工单中心总览接口
  - `GET /api/v1/management/support-tickets` - 后台售后 / 异常工单中心列表接口
  - `GET /api/v1/management/support-tickets/:id` - 后台售后 / 异常工单中心详情接口
  - `POST /api/v1/management/support-tickets` - 后台人工创建投诉工单接口
  - `POST /api/v1/management/support-tickets/:id/assign` - 后台工单分配给当前管理员接口
  - `POST /api/v1/management/support-tickets/:id/status` - 后台工单状态更新接口
  - `POST /api/v1/management/support-tickets/sync-system-exceptions` - 后台一键同步异常订单 / 失败任务为系统工单接口
  - `GET /api/v1/management/report-center/overview` - 后台报表导出中心概览接口
  - `GET /api/v1/management/report-center/reports` - 后台报表导出中心报表预览接口
  - `GET /api/v1/management/report-center/export` - 后台报表导出中心 CSV 导出接口
  - `GET /api/v1/management/orders` - 后台订单中心列表接口，查看充值单、认证费和业务消费单
  - `GET /api/v1/management/ai/tasks` - 后台 AI 图片任务中心列表接口
  - `GET /api/v1/management/ai/tasks/:id` - 后台 AI 图片任务详情接口，返回请求载荷、结果载荷与错误信息
  - `GET /api/v1/management/ai/video-tasks` - 后台 AI 视频任务中心列表接口
  - `GET /api/v1/management/ai/video-tasks/:id` - 后台 AI 视频任务详情接口，返回提示词、外部任务ID、结果地址和错误信息
  - `GET /api/v1/management/users/:id/workbench` - 后台用户360聚合接口，返回会员信息、最近订单、最近任务、灵石摘要、最近流水与设备风险信息

当前后台补充说明：

- 后台前端目录为 `background/`，采用 React + Vite
- 当前已完成并接入真实数据的后台页面包括：
  - `总控台 /dashboard`
  - `设计师中心 /designer-center`
  - `分销 / 邀请中心 /distribution`
  - `内容运营分析 /content-analytics`
  - `风控台 /risk-control`
  - `售后 / 异常工单中心 /support-tickets`
  - `报表导出中心 /report-center`
  - `用户会员运营 /membership-operations`
  - `订单中心 /recharge`
  - `AI任务中心 /ai-tasks`
  - `用户360工作台 /user-workbench`
- 其中 `总控台` 当前会优先调用 `/api/v1/management/dashboard/overview`、`/trends`、`/todos` 三个聚合接口展示真实经营数据与待办入口
- 其中 `设计师中心` 当前会优先调用 `/api/v1/management/designers`、`/:id`、`/:id/visibility`、`/:id/service-status` 做设计师列表、详情、主页公开状态和接单开关管理
- 其中 `分销 / 邀请中心` 当前会优先调用 `/api/v1/management/distribution/overview`、`/inviters`、`/rewards` 做邀请关系总览、邀请人排行和奖励发放排查
- 其中 `内容运营分析` 当前会优先调用 `/api/v1/management/content-analytics/overview`、`/download-ranking`、`/engagement-ranking`、`/new-templates`、`/low-conversion`、`/featured-cases` 做内容表现和转化观察
- 其中 `风控台` 当前会优先调用 `/api/v1/management/risk-control/overview`、`/device-groups`、`/device-changes`、`/alerts`、`/users` 做设备风险识别、异常告警和风险用户汇总
- 其中 `售后 / 异常工单中心` 当前会优先调用 `/api/v1/management/support-tickets/overview`、`/support-tickets`、`/:id`、`/:id/assign`、`/:id/status`、`/sync-system-exceptions` 做工单收口、分配和关闭
- 其中 `报表导出中心` 当前会优先调用 `/api/v1/management/report-center/overview`、`/reports`、`/export` 做报表预览与 CSV 导出
- 其中 `用户会员运营` 当前会优先调用 `/api/v1/management/membership-operations/overview`、`/users` 做会员用户总览、快到期/已过期筛选和来源订单排查
- 其中 `AI任务中心` 当前会优先调用 `/api/v1/management/ai/tasks`、`/ai/tasks/:id`、`/ai/video-tasks`、`/ai/video-tasks/:id` 做任务列表、详情和异常排查
- 其中 `用户360工作台` 当前会优先调用 `/api/v1/management/users/:id/workbench` 做单用户聚合展示，避免前端分别拼多段接口；本轮已补充设备风险信息、同设备账号识别和风险标签返回

### 3. Session管理
- 使用Redis存储Session
- Cookie名: `sessionid`
- 不使用JWT，纯SessionID方案

## 环境变量配置

```bash
# MySQL配置
MYSQL_DSN=root:password@tcp(127.0.0.1:3306)/jiadilinguang?charset=utf8mb4&parseTime=True&loc=Local

# Redis配置
REDIS_ADDR=127.0.0.1:6379
REDIS_PASSWORD=

# 微信配置
WECHAT_APPID=your_appid
WECHAT_APPSECRET=your_appsecret

# 企业客服配置
ENTERPRISE_WECHAT_SERVICE_PHONE=13959877676
ENTERPRISE_WECHAT_CUSTOMER_SERVICE_CORP_ID=ww673b3a4edf114110
ENTERPRISE_WECHAT_CUSTOMER_SERVICE_URL=https://work.weixin.qq.com/kfid/kfccb23bfff32bc9c6f
# ENTERPRISE_WECHAT_DOWNLOAD_4K_QRCODE_URL 继续沿用线上现有二维码配置
# ENTERPRISE_WECHAT_DOWNLOAD_4K_TIP=完成手机号授权验证后，可下载保存高清原图。

# 服务器配置
HTTP_ADDR=:8080
SESSION_SECRET=REPLACE_WITH_LOCAL_SESSION_SECRET

# AI 生图配置
LAOZHANG_API_KEY=your_laozhang_api_key
LAOZHANG_IMAGE_BASE_URL=https://api2.laozhang.ai
LAOZHANG_IMAGE_MODEL=gpt-image-2-vip

# AI 视频配置（与生图网关独立）
AI_VIDEO_API_BASE_URL=https://api.laozhang.ai
AI_VIDEO_MODEL=veo-3.1-landscape-fast-fl
```

生图兼容 provider code 继续使用 `gpt-image-2-vip`，实际请求网关和模型由 `LAOZHANG_IMAGE_BASE_URL` 与 `LAOZHANG_IMAGE_MODEL` 决定；未配置或仅包含空白时使用上述默认值。数据库中的历史 Gemini 生图模板仍会经过兼容转换，后台修改普通生图地址不会覆盖这两项环境变量。修改后需重启 Go 服务生效。

流式聊天的老张默认接口为 `https://api2.laozhang.ai/v1/chat/completions`（[官方备用域名公告](https://www.laozhang.ai/zh-cn)），模型仍为 `gpt-5.5`。`ai_api_config` 中启用的 `ai_chat` 配置可覆盖接口和密钥；修改数据库后需失效 Redis 的 `ai_api_config:task_type:ai_chat` 缓存。供应商决定模型及环境密钥，接口路径决定 Completions 或 Responses 协议；二者独立，不能根据 Completions 路径推断为老张。显式供应商与已知接口主机冲突时会拒绝请求。未配置老张密钥时继续沿用方舟默认；后台明确配置的原域名不会被强制替换。

## 数据库表结构

### users 表
```sql
CREATE TABLE IF NOT EXISTS users (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
    username VARCHAR(64) NOT NULL,
    password VARCHAR(255) DEFAULT NULL,
    openid VARCHAR(128) DEFAULT NULL,
    unionid VARCHAR(128) DEFAULT NULL,
    user_type VARCHAR(32) NOT NULL DEFAULT 'miniprogram',
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    UNIQUE KEY uk_username_type (username, user_type),
    UNIQUE KEY uk_openid (openid),
    INDEX idx_user_type (user_type)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
```

## 使用示例

### 1. 微信小程序登录
```bash
curl -X POST http://localhost:8080/api/v1/miniprogram/login \
  -H "Content-Type: application/json" \
  -d '{"code": "wx_code_from_miniprogram", "device_id": "device_fingerprint_xxx"}'
```

### 2. 管理后台登录
```bash
curl -X POST http://localhost:8080/api/v1/management/login \
  -H "Content-Type: application/json" \
  -d '{"username": "admin", "password": "password123"}'
```

### 3. 获取当前用户（需要登录）
```bash
curl -X GET http://localhost:8080/api/v1/miniprogram/me \
  -H "Cookie: sessionid=your_session_id"
```

### 4. 小程序账号密码登录（可选携带 device_id）
```bash
curl -X POST http://localhost:8080/api/v1/miniprogram/login/password \
  -H "Content-Type: application/json" \
  -d '{"username": "test_user", "password": "123456", "device_id": "device_fingerprint_xxx"}'
```

### 5. 取消订单
```bash
curl -X POST http://localhost:8080/api/v1/miniprogram/user/orders/123/cancel \
  -H "token: your_token"
```

## 日志

- 后端启动后会在当前运行目录自动创建 `APPlog/` 目录
- 日志文件按自然日拆分，例如：
  - `APPlog/2026-04-03.log`
  - `APPlog/2026-04-04.log`
- 当前接入的日志范围包括：
  - 标准 `log.Printf / log.Println`
  - Gin 访问日志
  - Gin 错误日志
- 如果线上仍通过 `nohup` 启动，建议把标准输出重定向到 `nohup.out`，不要再把主业务日志继续堆到单个 `app.log`
- 如果已经存在历史大文件 `app.log`，可以使用下面这个拆分工具按日期拆分：
  - 代码位置：`service/cmd/split_applog/main.go`
  - 示例命令：`go run ./cmd/split_applog -input app.log -output APPlog`

## 运行

```bash
# 编译
go build -o service.exe .

# 运行
./service.exe
```

## 一次性数据迁移

后端二进制内置了 `service/sql/*.sql` 下的迁移文件，可在部署时单独执行一次：

```bash
./service.exe migrate --list
./service.exe migrate clear_masterplan_coloring_sections.sql
```

- 迁移记录会写入数据库表 `app_data_migrations`
- 同一个迁移文件名只会成功执行一次，重复执行会自动跳过
- 适合这种“上线时顺手清理一批线上配置数据”的场景

## 扩展新的登录策略

1. 实现 `AuthStrategy` 接口：
```go
type MyAuthStrategy struct {
    // ...
}

func (s *MyAuthStrategy) GetStrategyName() string {
    return "my_auth"
}

func (s *MyAuthStrategy) Login(ctx context.Context, req interface{}) (*AuthResult, error) {
    // 实现登录逻辑
}
```

2. 在 `main.go` 中注册：
```go
myStrategy := processor.NewMyAuthStrategy(...)
authProcessor.RegisterStrategy(myStrategy)
```

3. 在路由中使用：
```go
strategy, _ := authProcessor.GetStrategy("my_auth")
result, err := strategy.Login(ctx, req)
```

## 三期内容、分享和通知

首批实现的接口、默认关闭开关、迁移/回退及未完成项见[三期接入说明](../docs/phase3/README.md)。本地构建不代表生产部署或服务号真实验收。

## 三期内容链接导入

导入默认关闭，运行变量、独立迁移、平台适配范围及恢复规则见 [导入方案](../docs/phase3/content-import-plan-20261027.md)。主服务只使用服务端供应商配置和受保护的Cookie文件；公众号视频浏览器采集为独立进程，部署门禁见 [Capture Worker](contentcapture/README.md)。不要把配置已填、单元测试或临时媒体地址当作已导入/已发布/生产验收完成。

三期媒体可通过 `CONTENT_MEDIA_STORAGE=cos` 选用私有COS，Bucket/Region默认复用既有服务端COS配置，或用 `CONTENT_MEDIA_COS_BUCKET`、`CONTENT_MEDIA_COS_REGION` 显式指定；`CONTENT_MEDIA_COS_PREFIX` 默认 jd-content-private。图片/视频先校验并以私有ACL保存，再验证匿名读取被拒绝；客户端继续走原授权媒体接口，不得到COS公开直链。文件系统模式继续使用原本地文件；COS模式只读取私有COS，切换前应完成既有素材受检转存。支持JPEG、PNG、GIF、WebP和MP4；Range读取采用有界流，并发与来源限速独立于浏览事件写入。
# 内容创作呈现契约

`article_v2` 正文继续使用 `schema: 1`、有序 `blocks`，支持可选字段 `presentation: article | gallery | video`。
图集包含连续图片和独立正文，视频包含单个主视频和独立正文；复杂混排、公众号 `layout_html` 保留文章形态。
保存与查询响应中的 `presentation` 由服务端按正文推导，不接受额外客户端归属或权限字段。
工作台上下文返回 `presentation_formats`；小程序仅在后端声明支持时保存该可选字段，旧服务端继续接收原有正文格式。
`type=gallery` 可筛选图集，`type=article` 筛选文章，旧中文 `type=图文` 仍包含两者。无数据库迁移。
导入网关作品的整条文案存入独立正文段落，媒体 `caption` 只承载单个媒体的短说明。

### 视频工作区与可恢复上传

视频编辑沿用 article_v2，新增可选 `video_layout`（`channels`/`douyin`）、`share_copy`、`moments_copy`（分别最多2000字）和 `draft_only`。`draft_only:true` 仅允许私有视频草稿，可以暂缺标题/主视频；授权归属与素材校验不放宽，预览和发布均拒绝未完成草稿。历史版本默认视频号式阅读且不原地改写。客户端必须读取 workbench context 的 `video_editor_fields` / `video_resumable_upload` 能力；不支持时明确提示升级，避免静默丢失新字段或回退到旧50MiB上传路径。

新增 workbench 接口（沿用登录、资料及管理权限门禁）：

- `POST /media-uploads`：`{owner_id,rights,filename,size,fingerprint,reset?}`，fingerprint为原文件SHA-1；按租户/操作者/归属/权属/指纹/大小恢复同一会话。只有状态明确返回 `reset_required:true` 时才发送 `reset:true`，已完成素材永不重置。
- `GET /media-uploads/:id`：查询状态并可恢复后台完成任务。
- `POST /media-uploads/:id/parts/:part`：multipart `file`，分片编号从0开始，4MiB传输单元；只约束单次请求，不限制作品总大小或时长。
- `POST /media-uploads/:id/complete`：分片齐全后返回 `processing:true`，后台流式合并、校验、存储与入库；轮询状态直到 `asset` 或可重试 `error`。重复完成返回同一素材。

状态为 `{upload_id,chunk_size,uploaded_parts,processing,error?,retryable,reset_required,asset?}`。`retryable:true` 只重试 complete，不重传已成功分片；`reset_required:true` 清除客户端会话并用同一原文件发起 `reset:true` 的 begin。客户端保存不含登录令牌的会话标识，重新选择同一原文件后可续传；切换账号/草稿停止回填与后续传输。分片用SHA-256校验，整文件核对声明的SHA-1，入库日志继续使用SHA-256。

配置：文件系统模式要求 `TRACKING_MEDIA_DIR` 是绝对私有目录；可通过 `CONTENT_VIDEO_UPLOAD_DIR` 指定独立绝对暂存目录。COS模式除现有私有Bucket配置外，也必须提供本地私有暂存目录。未完成会话保留7天，完成会话保留30天；扫描清理每小时最多一次，进行中的处理不会被清理，已清理的完成会话会从已登记素材恢复幂等结果。合并期间暂存分片、合并文件与最终对象需要额外磁盘空间，应监控容量。反向代理需允许一个4MiB分片加multipart开销（至少5MiB）；保留请求级超时及防护，不再用旧单文件50MiB阈值判断视频作品。COS每个对象分片写入依分片大小设置独立技术超时，卡住时取消当前请求并Abort未完成multipart，不对整个视频作品设总超时。播放采用已授权Range流式读取，COS响应头和上下游连续30秒无数据会超时，但持续传输不会受到30秒总时长限制。

当前播放格式仍为标准MP4，MOV会明确提示转换为MP4；此次未新增转码服务。没有业务时长与总大小上限，不代表设备、本地磁盘或存储服务拥有无限物理容量。真实COS大文件及微信iOS/Android录制/相册文件仍需部署后的联调验收。


### 固定版本内容海报

`GET /workbench/content-poster?content_id=...&content_version=...` 必须带明确的已发布版本，按当前管理范围校验 owner，再生成可解析的真实 share_token；二维码固定进入内容阅读页。`GET /workbench/shares/:id/poster` 的路径参数必须为真实分享令牌，并验证分享仍有效以及当前实际分享者或授权 owner，不能把不透明 ID 当作权限。客户端默认隐藏海报，只有服务端工作台能力确认当前内容归属可管理后才显示。普通阅读、原生分享不依赖素材管理权限。

### 分享视频播放与链接导入（2026-11-11）

内容阅读页、项目笔记与分享资料接收页使用已鉴权 POST 获取10分钟播放票据，原生播放器经 `/api/v1/shared-video/:ticket` 读取自有存储并使用 Range；每次读取仍复核账号资料和分享权限。无需新增环境变量或数据库迁移；必须在API站点合并 `ops/phase3-release/shared-video.conf` 并执行 `nginx -t`、真实Range及敏感日志校验。沿用 `PROJECT_NOTES_ENABLED` 和现有私有媒体配置。接口及生命周期见 [项目笔记接口](../docs/phase3/private-project-notes.md)。

链接导入生产路径将 MP4 上限由50MiB提高为500MiB，并使用权限为0600的临时文件流式下载与转存；图片保持10MiB。临时目录沿用操作系统 `TMPDIR`，部署需为单个最大视频及转存过程预留磁盘空间。旧字节数组抓取接口仍保留原限额用于现有其他调用，不用于新的大视频导入。任务总窗口20分钟、来源下载最多15分钟，长媒体保存期间心跳续租且单次续租最多5秒；MP4累计最多10000个atom并支持取消。过期媒体仅由用户显式重试后重新解析，结果不明的付费解析仍不可重试。方案和限制见 [导入方案](../docs/phase3/content-import-plan-20261027.md)。

已发布内容仍调用 `POST /api/v1/miniprogram/tracking/media/:id/signed`，响应保持 `{url, expires_at}`，但地址改为统一的 `/api/v1/shared-video/:ticket`；不再把带不了登录头的原生播放器导向全局资料门禁下的旧媒体路径。该POST显式要求登录与未封禁；播放GET逐次要求完整资料及当前有效已发布内容引用，内容撤回后拒绝。公开内容播放依赖 `ContentEnabled` 与 `VideoEnabled`，独立于私人项目开关。

### 首次未登录浏览边界（2026-11-11）

首页、模板广场、找设计师列表只读接口按GET完整路径允许匿名：`/templates/featured`、`/templates/tab-config`、`/templates`、`/templates/search`、`/inspirations`、`/designers`（均位于 `/api/v1/miniprogram`）。详情和写操作仍执行原登录/登记门禁，不按目录前缀放行。

匿名tracking entry/events/shares仅用于这三个公开route，并验证entry临时凭据的匿名归属；登录后的事件仍核对账号和服务器entry route，受保护route保持原资料完整度要求。无需新增环境变量或数据库迁移。修复与发布证据见 [首次浏览审核修复](../docs/phase3/review-guest-cold-start-20261111/README.md)。

### AI 预览与账号鉴权

公开 AI 价格及已发布工具定义采用精确 GET 路径允许匿名；AI 执行、上传和个人任务保留有效账号鉴权，不再要求头像昵称。其他分享内容的完整资料门禁不变，无数据库或环境变量变更。详细路径、状态码与跟踪边界见 [AI 访问契约](../docs/api/ai-preview-access.md)。
