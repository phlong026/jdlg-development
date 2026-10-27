# 私人方案固定版本 Word 导出

2026-10-27。本增量新增本地服务端能力，未发布主业务、未上传小程序，也未使用真实业主或生产数据库验收。

## 结果与复用

管理设计师和受邀账号可以请求一个精确冻结版本的 DOCX。源草稿后续更新不会改变导出内容。复用现有项目权限、分享撤销和有效期、独立私有媒体、COS/文件读取；使用 Go 标准库 ZIP/XML 组装 OOXML，未增加依赖或数据库结构。

图片嵌入文件，正文和图片按该版本的语义顺序输出。Word 文件不包含反馈、处理记录、确认记录、CRM 附加资料、分享 token、媒体原始地址或外部关系。原有正文内用户主动填写的文字仍属于方案正文，不会被推断成额外客户资料。

## API

所有路径都属于 `/api/v1/miniprogram/project-notes`，沿用登录签名、账号状态、限流、默认关闭的 `PROJECT_NOTES_ENABLED` 与 private/no-store 响应。

| 方法 | 路径 | 返回与权限 |
| --- | --- | --- |
| GET | `/projects/:id/versions/:version` | 现有 `ProjectNoteVersion` 精确快照。只允许当前项目设计师，需 `can_manage`、owner 授权以及当前有效设计师权限。跨项目版本与越权均拒绝 |
| GET、HEAD | `/projects/:id/versions/:version/export.docx` | 管理设计师导出精确版本，完整生成后再复核相同权限 |
| GET、HEAD | `/shared/:token/export.docx` | 当前登录账号仍为受邀有效成员，分享未撤回/未过期，读取该分享固定版本；转发 token 不增加权限 |

`HEAD` 执行相同生成和二次权限检查，只返回头。成功 MIME 为 `application/vnd.openxmlformats-officedocument.wordprocessingml.document`，文件名固定 `jiadi-project-note.docx`，设置真实 Content-Length、private/no-store、nosniff 与禁止执行内容的 CSP。文件不持久化到服务器，不创建公开下载 URL，不支持 Range。

## 生成规则

- `plain`：保留段落与换行，随后附上版本保存的媒体。
- `rich_text`：把安全 HTML 的标题、段落、引用、列表转换成 Word 结构，随后附上版本媒体。
- `article_v2`：优先使用 `layout_html` 的安全 DOM 顺序，否则使用 blocks。依次输出正文、图片与标题说明；正文之外的封面会插入正文前，已经位于正文中的封面不重复插入。
- PNG、JPEG、GIF 保留原始图片字节；WebP 在受限尺寸下转换成 PNG。Word 中按图片真实尺寸和纵横比适配 A4 页宽/页高。
- 视频输出“视频请在甲第方案中查看”和版本中的说明文字。不会读取或嵌入 MP4，也不生成带私密凭据的视频链接。原视频仍通过方案内受鉴权的媒体读取入口查看。
- HTML/CSS 只进行语义转换，不承诺公众号 CSS 的像素级复刻。脚本、外部图片或无法识别的结构拒绝导出；缺图不会以成功文件掩盖。

包结构与图片关系依据微软官方说明：[WordprocessingML 文档结构](https://learn.microsoft.com/en-us/office/open-xml/word/structure-of-a-wordprocessingml-document)、[向文档插入图片](https://learn.microsoft.com/en-us/office/open-xml/word/how-to-insert-a-picture-into-a-word-processing-document)。

## 资源与失败边界

单图片最大 10 MiB，图片解码像素最大 16,777,216；聚合图片原始字节及最终 DOCX 各不超过 32 MiB。每个进程最多同时执行两个导出，总处理和响应写入期限为 20 秒。图片转换和 ZIP 输出均有大小及取消检查，输出在内存中完整生成，失败不留下服务器半成品。

输出前再次读取同一版本并重新授权，拒绝成员撤销、分享撤回/过期、设计师权限变化或版本异常漂移。二次复核之后的网络中断只能按传输错误记录，不能向已经开始的响应改写成功状态；客户端需确认下载完整。

| 状态 | 含义 |
| --- | --- |
| 403/404 | 无当前访问权、分享过期/撤回或版本不属于项目 |
| 413 | 聚合素材或导出文件超过限额 |
| 429 | 当前进程导出并发已满，可稍后重试 |
| 503 | 缺图、文件异常、存储或输出环境暂不可用 |
| 504 | 导出超时或请求取消 |

## 验证

已按失败测试先行实现，并执行 `CGO_ENABLED=0 go test ./notedocx ./tracking ./route -count=1` 与相同三包 `go vet`，均通过。相关回归覆盖：

- 完整 ZIP、可解析 XML、Main/Styles/Image 关系、正文/图片/视频说明顺序与富文本换行/有序列表。
- 特殊字符转义、外部 HTML/脚本拒绝、缺图、类型不匹配、坏图、像素炸弹、输出上限及取消。
- V1 冻结后更新草稿，精确读取和导出仍为 V1。
- 未邀请、移除成员、过期、撤回、同 owner 其他设计师、跨项目版本均被拒绝。
- 图片读取期间撤销成员或设计师权限，生成完成后拒绝写出；视频字节不会被读取。
- GET/HEAD 下载头、空 HEAD 正文、聚合限额、并发限制。

另外用当前生成器制作合成中文示例，经 bundled LibreOffice 打开、转 PDF 并渲染为单页 PNG，已人工检查标题、正文、列表和嵌入图无溢出。首次渲染的中文缺字来自本机字体 fallback；用仅对该次命令生效的临时 Fontconfig 指向已有系统中文字体后复验通过，没有安装字体或改变全局环境。示例与渲染仅用于内部检查，不能代替真实方案、真实 COS 与微信设备验收。

合成证据：[DOCX](evidence/private-note-export/sample.docx)、[PDF](evidence/private-note-export/sample.pdf)、[单页预览](evidence/private-note-export/page-1.png)、[散列与验证清单](evidence/private-note-export/verification.json)。示例只含程序生成的测试图及明确标识的测试文字，ZIP 完整，无外部关系。

小程序下载按钮及历史版本预览由主任务并行接入，本文只证明服务端与合成文件检查。真实 Word/WPS/微信打开、多角色账号与真实 COS 图片组合仍需验收。本增量没有新增 PDF 导出接口。
