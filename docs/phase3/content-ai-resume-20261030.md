# C06 已保存草稿继续 AI 编辑

## 本次范围

- 已保存的可编辑内容可从内容编辑页再次进入 AI 文案页。
- 通过已有 `EventChannel` 传递当前标题、文字块、列表项和媒体说明；不传递私有媒体 ID、本地路径或原文链接。
- AI 返回文案追加到同一草稿末尾，不覆盖标题，不改写原有图片、视频和文字块。
- 公众号原排版和旧格式内容继续只读，不开放 AI 回填。

## 冲突与安全边界

发起 AI 时记录当前账号 token、`content_id`、`revision` 和草稿签名。回填前逐项比对：

1. 账号未变更；
2. 当前未处于保存或其他 busy 操作；
3. `content_id` 和 `revision` 未变更；
4. 标题、归属、权属、来源、封面和内容块组成的草稿签名未变更；
5. 同一次返回事件只处理一次，相同正文不重复追加。

任一检查失败时保留原草稿，由用户基于最新内容重新发起。本次未改动微信身份、访客追踪、资料门禁或 AI 计费链路。

## 后续项目笔记复用协议建议

项目笔记后续可复用同一文案页，但不在本次改动项目笔记代码。建议路由使用 `source=project_note`，并通过 `EventChannel` 发送：

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

回传仍使用 `prefillContent`，附带 `project_id` / `note_id` / `note_revision` 供发起页核对。发起页应自行保留账号、签名、revision、busy 和单次回填保护；文案页只负责基于显式 `facts` 创作，不查询、推断或补齐项目事实。

## 验证边界

测试使用本地 `EventChannel` 替身，不调用付费模型。真实 AI 计费、生成质量和真机导航仍是独立验收门。
