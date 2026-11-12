'use strict';
// Bare WXML imports can disappear from a separately compiled page package.
// Keep one editable template, but ship its definition with every consuming page.
const fs = require('node:fs');
const path = require('node:path');
const root = path.resolve(__dirname, '../miniprogram');
const start = '<!-- content-presentation:generated:start -->';
const end = '<!-- content-presentation:generated:end -->';

try {
  const template = fs.readFileSync(path.join(root, 'components/content-presentation/content-presentation.wxml'), 'utf8').trim();
  for (const page of ['contentreader', 'contentedit']) {
    const file = path.join(root, 'pages', page, `${page}.wxml`);
    const source = fs.readFileSync(file, 'utf8');
    const marker = source.indexOf(start);
    const markerEnd = source.indexOf(end, marker);
    if (marker >= 0 && markerEnd < 0) throw new Error(`生成模板标记不完整：${file}`);
    const pageSource = (marker < 0 ? source : source.slice(0, marker).trimEnd() + source.slice(markerEnd + end.length))
      .split('\n').filter(line => !line.startsWith('<import src=') || !line.includes('components/content-presentation/content-presentation.wxml')).join('\n').trimEnd();
    const gateEnd = pageSource.lastIndexOf('\n</block>');
    if (gateEnd < 0) throw new Error(`页面缺少外层门禁：${file}`);
    const output = `${pageSource.slice(0, gateEnd).trimEnd()}\n\n${start}\n${template}\n${end}${pageSource.slice(gateEnd)}\n`;
    if (process.argv.includes('--check')) {
      if (source !== output) throw new Error(`页面模板未同步：${file}`);
    } else if (source !== output) fs.writeFileSync(file, output);
  }
} catch (error) {
  console.error('内容模板构建失败：', error.message);
  process.exitCode = 1;
}
