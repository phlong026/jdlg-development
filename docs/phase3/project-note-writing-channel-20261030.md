# C07 项目笔记复用 AI 文案页协议

## 发起

项目笔记页使用以下路径打开已有文案页：

```text
/pages/contentwriting/contentwriting?source=project_note
```

在 `navigateTo.success` 取得的 `EventChannel` 上发送：

```text
writingContext {
  source: "project_note",
  project_id: string,
  note_id: string,
  note_revision: number,
  title: string,
  facts: string
}
```

`title` 和 `facts` 由项目笔记页根据当前用户可见、已确认的内容显式组装。文案页不查询账户资料、项目详情或其他隐私数据，也不推断未传入的事实。

## 采纳回传

用户点击“采纳到项目笔记”后，文案页通过同一 `EventChannel` 发送：

```text
prefillContent {
  source: "project_note",
  project_id: string,
  note_id: string,
  note_revision: number,
  title: string,
  text: string
}
```

文案页只负责原样带回发起时的三个身份字段和用户核对后的标题、文案。项目笔记页在回填前必须自行比对账号、`project_id`、`note_id`、`note_revision`、草稿签名、busy 状态和单次回填标记。

## 生成边界

- 收到 `writingContext` 只会预填标题和事实，不会自动请求模型。
- 仅当用户主动点击生成或优化按钮时，才继续使用现有 AI 对话与计费链路。
- 文案页保留既有账号变更检查；账号变更后清空已带入内容，不导出上一账号的文案。
- `source=editor` 的内容草稿协议保持不变。
