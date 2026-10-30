# C01/C02 PDF 与 DOCX 私有文档资料

## 交付边界

首批只支持：

- PDF：扩展名必须为 `.pdf`，字节必须包含可识别的 PDF 版本头、对象/结束对象标记和文件尾 `%%EOF`。这是有界的基本结构校验，不等同于完整 PDF 解析或恶意文件扫描。
- DOCX：扩展名必须为 `.docx`，ZIP 容器必须包含 `[Content_Types].xml` 和 `word/document.xml`；任意目录下名为 `vbaProject.bin` 的条目、VBA MIME 和 macro-enabled 内容类型均会被拒绝。

不接受 `.doc`、`.docm`、`.xls[xm]`、`.ppt[xm]`、ZIP 压缩包、伪装扩展名或其他任意类型。单个文档限制为 20 MiB；DOCX 同时检查条目数和解压后总体积，体积累加使用溢出安全判断，防止异常压缩包绕过上限。

## 复用的现有模型

未创建独立资料系统。文档原文件继续使用现有私有 `media` 存储和 owner 授权，元数据使用：

```text
Asset {
  kind: "document",
  mime: "application/pdf" | "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
}
```

可检索资料继续使用现有 `Content` 草稿、revision 和标题检索：

```text
Content {
  type: "document",
  status: "draft",
  body_format: "document_v1",
  body: {"schema":1,"media_id":"..."},
  media: [documentAsset]
}
```

`document_v1` 必须且只能引用一个同 owner 的 `document` 资产，不允许封面或附加图片/视频。修订时继续校验 `expected_revision`。

## 小程序流程

1. 用户在内容编辑页明确选择“PDF/DOCX”，并先选择归属、声明权属。
2. 文件通过已有签名 `POST /api/v1/miniprogram/workbench/media` 上传。微信临时路径可能没有扩展名，因此客户端额外发送文件选择器提供的 `original_name`；服务端只把无路径的文件名声明用于扩展名和展示名，最终仍以真实字节校验类型。
3. 上传成功后通过已有 `saveContent` 保存 `document_v1` 草稿；未新增 API helper。同 owner、同媒体 ID 使用服务端 `document_archive` 映射收敛到同一资料，避免建档响应丢失后重试产生重复资料。
4. 内容工作台按 `type=document` 归入“文档”分类，并复用现有 `content_q` 标题检索。
5. 重开文档时只加载元数据，用户点击“鉴权打开原文件”后才通过已有签名 `GET /workbench/media/:id` 取得文件。
6. 若原文件上传成功但建档失败，授权媒体列表仍显示“已上传文档”，用户可直接重试“保存为文档资料”，不会重复上传原文件。未知引用关系下不自动删除媒体。

## 隐私与发布边界

- 文档下载仍执行账号、owner 和 manage/view 授权，返回 `private, no-store`、`nosniff` 和 `Content-Disposition: attachment`。
- 文档不自动下载，不在图文素材库中作为正文块引用。
- 文档页只读，不提供在线编辑。
- 服务端拒绝为 `type=document` 创建发布预览或发布版本，避免原文件进入公开媒体授权链路。

## 未包含

- PDF/DOCX 在线编辑、内容解析、OCR、全文检索、自动发布。
- Office 宏文档、旧版 Office 格式或其他任意附件。
- 完整 PDF 语义解析、杀毒扫描或内容安全判定。
- 真机微信文件选择器和不同手机 Office 预览能力的最终验收。
