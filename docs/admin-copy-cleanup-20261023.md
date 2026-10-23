# 管理后台文案清理（2026-10-23）

独立分支 `fix/nickname-tracking-ui-20261023`，工作区 `/tmp/jdlg-ui-cleanup`，基线 `5c6962f`。不属于分享修复 PR39，不推送、不创建 PR、不合并、不部署、不连接生产。

## 清理规则与结果

盘点 `background/src/routes/index.tsx` 的全部 33 个实际路由，以及真实详情、抽屉、编辑和治理入口。删除检索教学、装饰口号、重复指路和重复解释；混合段落只删介绍，保留业务限制。23 个页面文件发生最小文案/展示节点变动，没有修改 CSS、API、事件处理、权限、表单字段、确认流程或品牌。

- 检索页保留标题、筛选字段、操作和统计，删除“先找…再做…”教程。复用 `ManagementSearchPanel.description` 原有可选能力，没有新开关或隐藏 CSS。
- 分享页面删“可观测访问…”口号和重复介绍，保留日期范围、归属、统计定义、匿名/登录身份边界、转发不代表送达和治理说明。
- 工作台删正常状态的重复口号及卡片教学；异常/风险状态解释继续显示。
- 内容分析删没有证据支持的“互动高代表传播”等判断；保留创建者同名核验、榜单展示上限、新模板时间口径及低转化判定条件。
- 模板页合并重复搜索范围提示，明确保留“分类与状态筛选覆盖全部模板；关键词搜索仅覆盖本页。”；分类移动弹层不再重复教授点击动作。
- AI 页删角色装饰、页头说明和重复指路，保留实际成本说明、草稿/线上区别、即时发布后果、图像是否送入模型和密钥保护。
- 价格页保留“价格保存后立即生效”；退款、余额镜像、旧支付兜底等金额/状态语义全部保留。

## 33 路由审查清单

“保留”表示没有发现可安全删除的纯装饰内容，或剩余文字涉及状态、风险、字段约束、用户业务资料；不是漏审。

| 路由 | 源页面 | 处理 |
| --- | --- | --- |
| `/share-tracking` | `shareTracking.tsx` | 删装饰口号、重复页头介绍 |
| `/customer-management` | `customerManagement.tsx` 及报备/流程组件 | 保留：权限、保护期、冲突恢复和实际客户资料 |
| `/login` | `login.tsx` | 保留品牌、登录状态和身份授权说明 |
| `/dashboard` | `dashboard.tsx` | 删正常状态复述及两条卡片教学；异常说明保留 |
| `/users` | `users.tsx` | 检索教程精简为全库统计范围 |
| `/user-workbench` | `userWorkbench.tsx` | 精简选择用户空态；保留搜索、返回、错误和操作 |
| `/designer-center` | `designerCenter.tsx` | 删检索教学；设计师服务介绍属业务资料，保留 |
| `/certification` | `certification.tsx` | 删审核操作教程；保留全库统计口径 |
| `/logs` | `logs.tsx` | 删页头重复解释，空态不再讲解 HTTP 实现 |
| `/distribution` | `distribution.tsx` | 删主观运营教学；奖励统计边界保留 |
| `/content-analytics` | `contentAnalytics.tsx` | 删检索教程、主观推断、重复说明；保留指标限制 |
| `/risk-control` | `riskControl.tsx` | 删同设备账号的猜测教学；时间/搜索范围保留 |
| `/support-tickets` | `supportTickets.tsx` | 保留：工单状态、统计范围、提交结果及冲突反馈 |
| `/sales-overview` | `salesOverview.tsx` | 已简洁；保留实际同步状态与统计 |
| `/customer-leads` | `customerLeads.tsx` | 清理未启用 embedded 分支的装饰 hero；当前列表及 CRM 接管提示保留 |
| `/customer-settings` | `customerSettings.tsx` | 保留接入、身份和权限状态 |
| `/report-center` | `reportCenter.tsx` | 删重复的选项/导出教程；导出范围和状态保留 |
| `/ai-overview` | `aiOverview.tsx` | 删介绍、重复指路和庆祝装饰；实际成本、退款说明保留 |
| `/ai-pricing` | `aiPricing.tsx` | 删“运营/技术”角色装饰；计费和回滚语义保留 |
| `/ai-content-audits` | `aiContentAudits.tsx` | 保留随机抽检、不拦截用户结果、驳回仅记录等处理边界 |
| `/ai-tasks` | `aiTaskCenter.tsx` | 删检索教学；失败、退款、转单和执行状态保留 |
| `/ai-config` | `ai-config.tsx` | 删检索教学；协议、字段映射、密钥和生效范围保留 |
| `/oss` | `oss.tsx` | 删检索/上传流程教学；文件条件、删除确认保留 |
| `/templates` | `templates.tsx` | 去重检索说明、精简案例组约束、删移动弹层教学 |
| `/inspirations` | `inspirations.tsx` | 保留业务描述、上传数量、封面和发布状态 |
| `/recharge` | `orderCenter.tsx` | 保留金额口径、镜像余额、退款预览和异步不确定状态 |
| `/virtualpay` | `virtualPay.tsx` | 删重复链路介绍；配置、回放风险及异常状态保留 |
| `/utility-tools` | `utilityTools.tsx` | 删检索教学；字段和发布状态保留 |
| `/ai-tools` | `aiTools.tsx` | 删页头及使用提示模块的重复教学；字段、发布差异和模型引用约束保留 |
| `/recharge-config` | `rechargeConfig.tsx` | 保留旧支付降级/兜底和唯一启用方式约束 |
| `/pricing-config` | `pricingConfig.tsx` | 去内部发版介绍，保留保存立即生效 |
| `/membership-plans` | `membershipPlans.tsx` | 保留充值档位与会员发放关系及表单状态 |
| `/membership-operations` | `membershipOperations.tsx` | 删检索教学；权益来源、订单和异常状态保留 |

`/`、`/opportunities` 和未知地址是重定向，不算额外后台页面。

## 二、三级入口

静态复核覆盖以下真实入口，风险、隐私和确认内容均未删除：

- 分享访客/素材详情、会话页面轨迹、转发记录、意向规则、CRM 映射、人工修正、删除治理、素材编辑发布撤回、公众号同步。
- 客户详情/资料/报备/跟进抽屉，主管交接/合并/审核，公海认领；线索详情、历史和旧跟进。未恢复任何暂停功能或旧写入口。
- 用户封禁/解封、合并预检；用户详情的认证、安全、权限修正和灵石调整；认证审核抽屉。
- 设计师详情、工单新建/详情、订单详情/退款、OSS 上传、模板编辑/分类移动/案例组、灵感编辑。
- AI 任务详情、抽检/驳回、价格历史/回滚、工具编辑/版本/发布差异、接口配置；会员和充值配置、虚拟支付回放。

只抽样渲染了下表，未将静态审查冒充全部弹层的视觉或业务验收。

## 本地真实渲染抽样

使用独立 Vite `127.0.0.1:4174/admin/`、Playwright 和 Chromium。全部 API 为明示合成数据，所有非 GET/HEAD 请求直接拒绝；外网资源阻止，未发起生产请求。桌面 1440×1000、390×844。

先采样前图，再改文案。输出 34 张前图、38 张后图；其中 AI 工具编辑/使用提示为新增后图，无对应前图。报告选用实际查看的关键画面，其他原始截图保留供复核。审计后图首次拍到路由加载占位，已拒绝并在明确等待空态后重拍。

| 步骤 | 样本 | 健康/结果 |
| --- | --- | --- |
| 1 | 工作台 | 正常状态更简洁；刷新、统计及必要空态仍在 |
| 2 | 分享访问列表 | 标题和动作直接进入筛选；390px 首屏显示更多实际访问数据 |
| 3 | 访客/素材详情、页面轨迹、转发记录 | 真实详情和统计限制保留，未出现横向溢出 |
| 4 | 客户列表及详情、报备、跟进抽屉 | 字段/提示/保存动作保留；只读截图没有提交 |
| 5 | 线索列表、详情、内嵌访问轨迹 | CRM 接管及客户档案去向保留 |
| 6 | 操作审计 | 检索动作和表格仍在，空态直接显示“暂无操作记录” |
| 7 | AI 工具列表 | 去掉教程，保留检索、新增及必要空态 |
| 8 | AI 工具编辑/使用提示模块 | 标题、勾选、字段和草稿按钮完整；未上传、保存或发布 |

最终日志：主流程 30 状态、补充流程 8 状态，页面 JS 错误 0、外网或写请求尝试 0。主流程记录页面宽度和弹层范围，补充流程检查横向溢出。截图不能替代完整无障碍、真实数据、软键盘或提交权限验收。

证据目录 `/workspace/jdlg-fix-evidence/UI-cleanup/` 提供 `index.html`、`before/`、`after/`、采集脚本、路由和源码清单、测试/构建日志。代表文件：

- `before/390-12-tracking-main.png` / `after/390-12-tracking-main.png`
- `before/desktop-15-audit-main.png` / `after/desktop-15-audit-main.png`
- `before/desktop-16-ai-tools-main.png` / `after/desktop-16-ai-tools-main.png`
- `after/390-05-crm-follow.png`、`after/390-18-ai-tool-tips.png`

## 验证

工作目录 `/tmp/jdlg-ui-cleanup/background`：

- `npm test`：类型、路由检查及全部 UI 测试通过，746 pass，0 fail/skip/cancelled。
- `npm run build`：通过。
- `git diff --check`：通过。

未改测试文件。初次回归的一个模板源码断言要求原“关键词搜索仅覆盖本页”文字；去重后保留同一句必要范围说明，重新完整回归通过，没有删约束、跳过测试或放宽断言。

复跑：启动 4174 的 Vite 后执行证据目录中的 `capture.py after` 与 `ops_capture.py after`。脚本只允许本地应用资源、合成 API，拒绝写请求和外网。静态审查覆盖全部 33 路由，真实渲染仅限上述样本，不声称所有业务状态已经验收。
