# 项目协作工作流 API（2026-10-30）

本文只描述设计师与受邀业主主动操作的项目协作数据。它不新增实名浏览追踪、匿名访客识别或自动浏览通知。

## 项目资料与状态

项目对象新增以下字段：

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `location` | string | 项目地点，最多 200 字节 |
| `building_area` | number | 建筑面积，`0` 表示未填写 |
| `floors` | integer | 层数，`0` 表示未填写 |
| `style` | string | 设计风格，最多 100 字节 |
| `status` | enum | `initial`、`revising`、`confirmed` |
| `current_version_id` | string | 当前有效冻结方案版本；首次新冻结后写入 |
| `revision` | integer | 项目资料乐观锁修订号 |

`POST /api/v1/miniprogram/project-notes/projects` 可在原有 `name`、`owner_id` 基础上提交四个结构字段。新项目的 `status` 固定为 `initial`。

```json
{
  "name": "张宅方案",
  "owner_id": "owner-a",
  "location": "杭州市余杭区",
  "building_area": 328.5,
  "floors": 3,
  "style": "现代中式"
}
```

`PATCH /api/v1/miniprogram/project-notes/projects/:id` 使用部分更新。`expected_revision` 必填；可更新 `name`、`location`、`building_area`、`floors`、`style` 和 `status`。

```json
{
  "expected_revision": 1,
  "location": "杭州市临平区",
  "status": "revising"
}
```

- 仅项目所属设计师且具备既有管理权限时可更新。
- 修订号不一致返回 `409`。
- 设计师只可提交 `initial` 或 `revising`。提交 `confirmed` 返回 `400`。
- `confirmed` 只能由当前指定业主调用既有确认接口成功后由服务端写入。
- 每次冻结新版本会原子更新 `current_version_id` 并把项目推进 `revising`；修改当前方案对应笔记的新修订也会推进 `revising`。
- 历史版本反馈、历史版本确认继续作为不可变协作事实保存，但不会覆盖当前项目状态。
- 只有 `version_id == current_version_id` 且对应笔记尚无更新修订时，该版本反馈或确认才可驱动项目状态。老项目尚无指针时仅有界读取该项目最近一个冻结版本（`limit=1`）作为兼容判断，不扫描全部历史。

## 不可变版本中的项目快照

`POST /notes/:id/versions` 冻结版本时，版本对象新增 `project_snapshot`：

```json
{
  "project_snapshot": {
    "name": "张宅方案",
    "location": "杭州市临平区",
    "building_area": 328.5,
    "floors": 3,
    "style": "现代中式",
    "revision": 2
  }
}
```

快照属于不可变版本。后续修改项目资料不会改写已冻结版本，导出应读取该快照，而不是读取项目当前值。

## 业主满意度反馈

`POST /api/v1/miniprogram/project-notes/shared/:token/feedback` 新增可选字段 `satisfaction`：

- `very_satisfied`
- `needs_adjustment`
- `unsatisfied`

```json
{
  "text": "门厅需要调整",
  "satisfaction": "needs_adjustment",
  "idempotency_key": "feedback-20261030-001"
}
```

`text` 仍然必填，因此旧的纯文字反馈请求继续有效。反馈响应原样返回 `satisfaction`；非法枚举返回 `400`。`needs_adjustment` 或 `unsatisfied` 会把项目状态置为 `revising`，但 `very_satisfied` 不等同于方案确认。

## 指定业主确认

现有 `POST /api/v1/miniprogram/project-notes/shared/:token/confirm` 仍要求：

- 当前账号是有效项目成员；
- 当前账号是设计师指定的确认人；
- 分享固定到要确认的不可变版本；
- 请求包含有效 `idempotency_key`。

确认成功后，服务端将项目 `status` 更新为 `confirmed` 并递增项目 `revision`。设计师 PATCH 不能制造该状态。

## 主动告知设计师已查看

`POST /api/v1/miniprogram/project-notes/shared/:token/open` 现在要求显式提交 `acknowledged: true`：

```json
{
  "event_id": "open-20261030-001",
  "acknowledged": true
}
```

缺少该字段或提交 `false` 时返回 `400`，且不会调用既有打开事件服务。服务层原有成员权限、分享有效性和事件去重规则不变；读页加载本身不应自动调用此接口。

## 业主反馈站内提醒

受邀业主成功提交反馈后，服务端在同一事务内写入 `project_note_feedback` 通知事件，收件人固定为该项目设计师。该事件不依赖浏览提醒开关，也不使用匿名访客或被动浏览身份。

- 通知事件 ID 为确定性的 `feedback:<feedback_id>`，既避免与主动查看事件占用同一 ID，也保证同一幂等反馈只产生一条站内提醒。
- 反馈、项目状态和通知 outbox 同事务提交；outbox 入队失败时反馈整体回滚，客户端可使用原 `idempotency_key` 安全重试。
- 消费者使用现有持久化租约重试；物化前再次校验收件人仍是项目设计师且仍有项目归属权限。
- `project_note_feedback` 只创建站内收件箱记录，明确不创建服务号等外部投递任务。

通知持久化坐标为：

| 通知字段 | 含义 |
| --- | --- |
| `route_key` | `pages/projectnotes/projectnotes?project_id=<project_id>` |
| `content_id` | 精确 `version_id` |
| `notification_id` | `feedback:<feedback_id>`，消息中心校验前缀后取出精确 `feedback_id` |

消息中心只接受上述受限格式，并在本地组合为：

```text
/pages/projectnotes/projectnotes?project_id=<project_id>&version_id=<version_id>&feedback_id=<feedback_id>
```

路由不携带分享令牌、下载地址或正文。项目工作台负责根据 `feedback_id` 选中相应反馈。

## 本轮未包含

- 项目删除、复制、历史分页；
- CRM 引用放开；
- 实名浏览追踪、匿名访客识别、自动浏览通知；
- 生产开关、部署与真实角色验收。


## 2026-10-31 展示名称补充

项目列表、详情及创建/更新成功响应的项目对象增加只读字段 `creator_display_name`，按 `designer_id` 读取用户昵称；缺少昵称时为“项目创建人”。`designer_id`、`owner_id` 仍用于权限和关联，不接受展示字段作为授权依据，不更改数据库结构。

项目列表批量读取创建人昵称，详情合并已有成员资料查询，避免逐项查询。昵称查询失败时列表保留原数据并显示中性名称，创建/更新已成功时仍返回成功；详情既有资料查询失败仍返回服务不可用。错误记录操作上下文，不把数据库错误透出给客户端。

`GET /api/v1/miniprogram/workbench/context` 保留 `owners`，增加 `owner_choices: [{owner_id, display_name}]`。个人归属显示用户昵称，缺省为“个人创作空间”；其他归属显示“团队内容空间”及必要编号。客户端仅在多归属时显示选择器，提交仍使用原 `owner_id`。旧服务缺少 `owner_choices` 时客户端使用“我的内容 / 内容归属 N”等兼容标签，不直接显示内部编号。
