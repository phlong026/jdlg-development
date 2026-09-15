水印 logo 图片目录

- 将 logo.png（PNG 格式）放在此目录，生成图片水印时会在文字「ArchLight 3.0 Ai设计」左侧显示该 logo。

邀请海报 SVG 模板（invite_poster.svg）

- 将设计师导出的邀请海报 SVG 放在此目录，文件名为 invite_poster.svg。
- 程序只做字符串替换，不解析 SVG 结构。约定三个占位符：
  - {qrcodebs64}：替换为小程序码图片的 data URL（base64）
  - {id}：用户昵称
  - {invi}：邀请码
- SVG→PNG：模板含 <text> 时用 rsvg-convert 或 ImageMagick。Ubuntu 建议安装 librsvg（apt install librsvg2-bin）；若用 ImageMagick 且报错/无输出，需放宽策略：编辑 /etc/ImageMagick-6/policy.xml，将 SVG/PDF 相关 deny 改为 allow 或注释掉。
- 打包部署时：请将 assets 目录（内含 logo.png、invite_poster.svg 等）与可执行文件放在同一目录下，例如：
    your_app.exe
    assets/
      logo.png
  程序会优先从「可执行文件同目录/assets/logo.png」加载。
