"""Export local visual-review assets; never run this on private customer reports."""
import argparse
import html
import json
from pathlib import Path
import shutil
import subprocess

from render import HERE, render_report


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--font', type=Path, required=True)
    parser.add_argument('--output', type=Path, required=True)
    args = parser.parse_args()
    data = json.loads((HERE / 'sample.json').read_text(encoding='utf-8'))
    out = args.output.resolve()
    out.mkdir(parents=True, exist_ok=True)
    full = out / '山水版建房报告-14页.pdf'
    preview = out / '山水版建房报告-前4页.pdf'
    boxes = render_report(data, full, 'full', args.font)
    render_report(data, preview, 'preview', args.font)
    binary = shutil.which('pdftoppm')
    if not binary:
        raise RuntimeError('pdftoppm is required for visual verification')
    subprocess.run([binary, '-r', '110', '-png', str(full), str(out/'page')],
                   check=True, timeout=120)
    links, pages = [], []
    for p in data['pages']:
        number, title = p['number'], html.escape(p['title'])
        links.append(f'<a href="#p{number}">{number:02} · {title}</a>')
        pages.append(f'<section id="p{number}"><h2>{number:02} · {title}</h2>'
                     f'<img loading="lazy" src="page-{number:02}.png" alt="{title}"></section>')
    document = '''<!doctype html><html lang="zh-CN"><meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>甲第灵光 · 山水版报告审阅</title>
<style>*{box-sizing:border-box}body{margin:0;background:#ede5d8;color:#654b39;font:15px/1.7 -apple-system,BlinkMacSystemFont,"PingFang SC",sans-serif}header{padding:24px 4vw;background:#faf6ee;border-bottom:1px solid #c9b38e}h1{font-size:25px;margin:0 0 8px}p{margin:8px 0}a{color:#526f63}header a{margin-right:25px}.layout{display:grid;grid-template-columns:270px minmax(0,820px);max-width:1200px;margin:auto;gap:30px;padding:28px}nav{position:sticky;top:20px;align-self:start}nav a{display:block;padding:6px 0;text-decoration:none;font-size:13px}section{scroll-margin-top:18px;margin-bottom:32px}h2{font-size:16px;font-weight:500}img{display:block;width:100%;box-shadow:0 8px 26px #71533220}@media(max-width:800px){.layout{display:block;padding:16px}nav{display:flex;position:static;overflow:auto;gap:12px}nav a{min-width:155px}h1{font-size:21px}}</style>
<header><h1>甲第灵光 · 家庭建房报告</h1><p>A4 山水版式审阅 · 固定 14 页，无手机外框。</p>
<p>本页使用虚构家庭样稿；方案图是概念分区示意，不是已审核设计成果或施工图。</p>
<a href="山水版建房报告-14页.pdf">打开完整 14 页 PDF</a><a href="山水版建房报告-前4页.pdf">打开前 4 页预览</a></header>
<div class="layout"><nav>''' + ''.join(links) + '</nav><main>' + ''.join(pages) + '</main></div></html>'
    (out/'报告审阅.html').write_text(document, encoding='utf-8')
    (out/'版面文字校验.json').write_text(json.dumps(boxes, ensure_ascii=False, indent=2), encoding='utf-8')
    print(f'已生成 {out}，包含 14 页 PDF、前 4 页预览和逐页图片。')


if __name__ == '__main__':
    main()
