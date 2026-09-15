# 甲第灵光后端服务

基于 Go + MySQL + Redis + Gin + SessionID 的后端服务，使用策略模式实现多种登录方式。

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
SESSION_SECRET=jiadilinguang-session-secret

# AI 视频配置（老张视频接口）
LAOZHANG_API_KEY=your_laozhang_api_key
AI_VIDEO_API_BASE_URL=https://api.laozhang.ai
AI_VIDEO_MODEL=veo-3.1-landscape-fast-fl
```

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
