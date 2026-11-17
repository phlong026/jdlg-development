# AI 视频润色 API 与配置

## 小程序接口

前缀 `/api/v1/miniprogram/ai-video-studio/prompt-polish`；沿用签名、登录资料和已认证设计师守卫。所有任务读写按当前账号隔离。

| 方法 | 路径 | 输入或行为 |
| --- | --- | --- |
| GET | `/capabilities` | 返回 enabled、disabled_reason、model、total_stones、config_version |
| POST | `/quotes` | `{draft_id, expected_revision}`；校验已保存草稿、素材归属和最终长度，冻结输入与价格 |
| POST | `/tasks` | `{quote_id, idempotency_key}`；原子预留并创建任务，同请求返回原任务 |
| GET | `/submissions/:key` | 只读查询原请求；结果未知时使用本接口，不发新请求键 |
| GET | `/tasks/:id` | 读取原文、润色结果与费用状态 |
| POST | `/tasks/:id/cancel` | 仅尚未执行的 queued 任务可取消并释放预留 |

报价返回 `quote_id,draft_id,draft_revision,quote_version,input_hash,total_stones,expires_at,input_snapshot`；有效期10分钟，独立按次价格。变价、草稿修订或配置版本变化后提交旧报价返回409。

任务返回 `task_id,quote_id,draft_id,draft_revision,input_hash,input_snapshot,status,fee_status,total_stones,original_prompt,optimized_prompt,error_message,created_at,updated_at`。`input_snapshot` 为草稿公开字段，包含 references；不返回内部供应商地址、绑定模型、凭据或租约。

状态：`queued / submitting / uncertain / completed / failed / cancelled`。费用：`reserved / settled / released`。只有 completed 结果可采用。失败释放预留；unknown 保持预留，不能自动重新调用供应商。

通用 envelope 为 `{code:0,data:...}`；400输入不合法，402余额不足，403方式/身份未开放，404记录不存在或无权读取，409报价/状态变化，503配置未就绪或基础服务失败。503不证明付费请求未受理；客户端先读原请求。

功能关闭不影响原任务读取和原请求幂等重放。结果采用完全在编辑器中完成，不创建视频任务，不另外计费。

## 后台接口

沿用管理登录，前缀 `/api/v1/management/ai-video-studio`。

现有 GET/PUT `/config` 扩展：

```json
{
  "prompt_optimize": {"enabled": false, "model": "", "timeout_seconds": 45},
  "prompt_optimize_ready": false,
  "prompt_optimize_disabled_reason": "AI润色暂未开放"
}
```

PUT 使用原有 `expected_version` 乐观锁，剔除只读 readiness 字段。启用润色必须配置模型、1至120秒超时、启用独立按次正整数价格；全局并发继续使用 `limits.max_concurrency`，默认3。读取既有 `ai_chat` 配置并沿用供应商选择规则，不混用不同供应商凭据。端点必须 HTTPS，不跟随重定向。

| 方法 | 路径 | 行为 |
| --- | --- | --- |
| GET | `/prompt-polish/tasks` | page/page_size/status/keyword 分页查询，最多100条，额外返回 user_id |
| GET | `/prompt-polish/tasks/:id` | 查看任务与原文/结果；仍不返回供应商绑定 |
| POST | `/prompt-polish/tasks/:id/reconcile` | 处理 uncertain；操作与钱包、结果、审计同事务 |

核对输入为 `{action,evidence,result?}`。`action` 仅接受 `confirmed_rejected`（供应商明确未受理，退款）或 `confirmed_result`（核实已取得的结果，录入后结算）。依据10至500字；已核实文本按7000码点及素材引用/用途拼接规则校验。同任务只允许一个决策，同动作同结果幂等，不同动作或不同结果冲突。该接口不发送模型请求。

## 供应商协议

后台独立模型名覆盖文本供应商选择器的默认模型。Chat Completions 发送 system/user messages、stream=false；Responses 发送 instructions/input、stream=false。不启用工具或媒体输入，仅接受完整纯文本结果。

调用使用独立上下文超时、64KiB响应上限、固定任务请求标识，不做自动重试或重定向。5xx、408、409、网络中断进入待核对；明确拒绝和无效结果按失败释放。第三方模型和协议实际可用性留待后续获准的真实联调，本轮只使用本地 HTTP 夹具。
