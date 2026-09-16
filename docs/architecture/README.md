# 甲第灵光技术分析交付

优先打开 [完整图文阅读版](report.html)，或在支持 Mermaid 的编辑器中打开 [Markdown 正文](analysis.md)。

| 文件 | 用途 |
|---|---|
| `report.html` | 可离线浏览的完整图文报告，含章节导航和 12 张图 |
| `analysis.md` | 含 80 处已核对代码定位的 Markdown 分析 |
| `analysis.source.md` | 可编辑源文档；双中括号标记由脚本解析为代码行号 |
| `code-index.html` / `code-index.md` | 小程序页面、管理端路由、建表和原始 API 注册位置 |
| `inventory.json` | 可供后续工具使用的结构化索引 |
| `diagrams/*.mmd` | 12 张可编辑的 Mermaid 原图 |
| `diagrams/*.svg` | 12 张已渲染矢量图，可缩放、插入文档 |
| `diagrams/01-architecture.png` | 总体架构图片 |
| `build_report.py` | 基于当前仓库重新定位证据、生成索引及 HTML；不启动业务服务 |
| `mermaid-config.json` | 图形样式配置 |

阅读 HTML 时保留 `diagrams` 目录的相对位置。点击图可查看原始大小。代码文件链接指向本次工作区；将报告复制到其他电脑后，文字中的仓库路径与行号仍可用于定位，但绝对路径链接需要重新生成。

在仓库内更新报告正文后运行：

```text
python docs/architecture/build_report.py
```

上述脚本重建 Mermaid 文本与 HTML，但不会自动重新渲染 SVG。修改图后，在已有 Mermaid CLI 和浏览器环境中执行，例如：

```text
mmdc -i diagrams/01-architecture.mmd -o diagrams/01-architecture.svg -c mermaid-config.json -b white
```

本次使用 Mermaid CLI 11.12.0 与本机无界面浏览器渲染。12 张图均成功渲染，HTML 的图片路径、目录锚点和 80 处代码引用已检查；另检查了阅读版、总体架构和报告状态图的实际显示。

这是一份源码静态分析。未连接生产数据库、未启动后端、未调用付费 AI/支付接口、未执行迁移，也未运行现有业务测试。当前源码没有 Git 元数据，因此没有提供提交号。
