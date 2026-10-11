"""Rebuild local architecture documentation from source, without starting the app.

Only reads project source and writes files under this documentation directory.
SVGs are rendered separately from diagrams/*.mmd using Mermaid CLI.
"""
from __future__ import annotations

import html
import json
from pathlib import Path
import re

HERE = Path(__file__).resolve().parent
ROOT = HERE.parents[1]
DIAGRAMS = HERE / "diagrams"
DIAGRAMS.mkdir(exist_ok=True)


def read(path: Path) -> str:
    return path.read_text(encoding="utf-8-sig")


def location(path: str, needle: str) -> tuple[str, int]:
    content = read(ROOT / path)
    if needle not in content:
        raise ValueError(f"Unresolved source reference: {path} | {needle}")
    return path, content[:content.index(needle)].count("\n") + 1


def source_link(path: str, line: int) -> str:
    return f"[{path}:{line}](<{(ROOT / path).as_posix()}:{line}>)"


evidence = []


def expand_reference(match: re.Match) -> str:
    path, line = location(match[1], match[2])
    evidence.append({"path": path, "line": line, "symbol": match[2]})
    return source_link(path, line)


source = read(HERE / "analysis.source.md")
analysis = re.sub(r"\[\[([^|\]]+)\|([^\]]+)\]\]", expand_reference, source)
if "[[" in analysis:
    raise ValueError("Unexpanded evidence reference")

diagram_records = []
for number, match in enumerate(re.finditer(r"```mermaid\n(.*?)\n```", analysis, re.S), 1):
    code = match[1]
    name_match = re.search(r"^%% name: ([a-z0-9-]+)$", code, re.M)
    if not name_match:
        raise ValueError("Diagram needs stable name")
    name = f"{number:02d}-{name_match[1]}"
    (DIAGRAMS / f"{name}.mmd").write_text(code + "\n", encoding="utf-8")
    diagram_records.append({"name": name, "mmd": f"diagrams/{name}.mmd", "svg": f"diagrams/{name}.svg"})

(HERE / "analysis.md").write_text(analysis, encoding="utf-8")

app = json.loads(read(ROOT / "miniprogram/miniprogram/app.json"))
subpackages = app.get("subpackages", app.get("subPackages", []))
pages = [{"package": "主包", "page": p} for p in app["pages"]]
for package in subpackages:
    for page in package["pages"]:
        pages.append({"package": package["root"], "page": package["root"] + "/" + page})
for page in pages:
    assert (ROOT / "miniprogram/miniprogram" / (page["page"] + ".ts")).is_file()

admin_text = read(ROOT / "background/src/routes/index.tsx")
admin_routes = []
for match in re.finditer(r"path: '([^']+)',\s*title: '([^']+)'", admin_text):
    admin_routes.append({"path": match[1], "title": match[2], "line": admin_text[:match.start()].count("\n") + 1})

routes = []
tables: dict[str, list[dict]] = {}
go_source = sorted((ROOT / "service").rglob("*.go"))
declarations = [p for p in go_source if not p.name.endswith("_test.go")]
declarations += sorted((ROOT / "service/sql").glob("*_up.sql"))
for path in declarations:
    content = read(path)
    relative = path.relative_to(ROOT).as_posix()
    if path.suffix == ".go":
        for match in re.finditer(r'(?m)^\s*(\w+)\.(GET|POST|PUT|PATCH|DELETE)\("([^"\n]*)"', content):
            # Keep raw group and suffix. Do not pretend this is a fully resolved live API manifest.
            routes.append({"file": relative, "line": content[:match.start()].count("\n") + 1,
                           "group": match[1], "method": match[2], "path_suffix": match[3]})
    for match in re.finditer(r"CREATE TABLE IF NOT EXISTS\s+`?(\w+)", content, re.I):
        tables.setdefault(match[1], []).append({"file": relative, "line": content[:match.start()].count("\n") + 1})

inventory = {
    "date": "2026-09-10",
    "scope": "Static source inventory; excludes vendored dependencies and runtime validation",
    "counts": {"miniprogram_main_pages": len(app["pages"]), "miniprogram_subpackages": len(subpackages),
               "miniprogram_total_pages": len(pages), "admin_routes_including_login": len(admin_routes),
               "route_registration_statements": len(routes), "distinct_declared_business_tables": len(tables),
               "go_test_files": len(list((ROOT / "service").rglob("*_test.go"))),
               "miniprogram_test_files": len(list((ROOT / "miniprogram/tests").glob("*.test.js")))},
    "pages": pages, "admin_routes": admin_routes, "route_declarations": routes,
    "table_declarations": dict(sorted(tables.items())), "evidence": evidence, "diagrams": diagram_records,
}
(HERE / "inventory.json").write_text(json.dumps(inventory, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")

index = ["# 源码与功能索引", "", "由当前源码静态生成，分析日期 2026-09-10。", "",
         "路由表保留注册变量与局部路径后缀，并非全部已拼接的可调用 URL；正式调用必须沿 RegisterRoutes 和 Group 继续确认。",
         "建表清单包含兼容/未接入模型，不代表线上表已存在。", "", "## 小程序页面", "",
         "| 包 | 页面源码 |", "|---|---|"]
for page in pages:
    path = "miniprogram/miniprogram/" + page["page"] + ".ts"
    index.append(f"| {page['package']} | {source_link(path, 1)} |")
index += ["", "## 管理端路由", "", "统一部署在 /admin 基路径下。", "", "| 路由 | 标题 | 注册位置 |", "|---|---|---|"]
for route in admin_routes:
    index.append(f"| {route['path']} | {route['title']} | {source_link('background/src/routes/index.tsx', route['line'])} |")
index += ["", "## 业务建表声明", "", "| 表名 | 声明位置 |", "|---|---|"]
for table, refs in sorted(tables.items()):
    links = "；".join(source_link(item["file"], item["line"]) for item in refs)
    index.append(f"| {table} | {links} |")
index += ["", "## 原始路由注册语句", "", "| 方法 | 分组变量 | 局部路径后缀 | 注册位置 |", "|---|---|---|---|"]
for route in routes:
    index.append(f"| {route['method']} | {route['group']} | `{route['path_suffix'] or '(空路径)'}` | {source_link(route['file'], route['line'])} |")
index_text = "\n".join(index) + "\n"
(HERE / "code-index.md").write_text(index_text, encoding="utf-8")


def inline(value: str) -> str:
    protected = []
    def protect(fragment: str) -> str:
        protected.append(fragment)
        return f"@@TOKEN{len(protected)-1}@@"
    value = re.sub(r"`([^`]+)`", lambda m: protect("<code>" + html.escape(m[1]) + "</code>"), value)
    def link(match: re.Match) -> str:
        label, url = match[1], match[2].strip("<>")
        # Local source files are opened without the editor-only :line suffix in the HTML reading copy.
        if re.match(r"^[A-Za-z]:/", url):
            url = re.sub(r":\d+$", "", url)
            url = Path(url).as_uri()
        return protect('<a href="' + html.escape(url, quote=True) + '">' + html.escape(label) + '</a>')
    value = re.sub(r"\[([^\]]+)\]\((<[^>]+>|[^)]+)\)", link, value)
    value = html.escape(value)
    value = re.sub(r"\*\*(.+?)\*\*", r"<strong>\1</strong>", value)
    for i, fragment in enumerate(protected):
        value = value.replace(f"@@TOKEN{i}@@", fragment)
    return value


def render_markdown(text: str) -> tuple[str, str]:
    """Small deterministic renderer for the report's restricted Markdown vocabulary."""
    lines, out, toc = text.splitlines(), [], []
    i, section, diagram = 0, 0, 0
    while i < len(lines):
        line = lines[i]
        if not line.strip():
            i += 1
            continue
        if line.startswith("```"):
            language, code = line[3:], []
            i += 1
            while i < len(lines) and not lines[i].startswith("```"):
                code.append(lines[i]); i += 1
            if language == "mermaid":
                record = diagram_records[diagram]; diagram += 1
                caption = record["name"].split("-", 1)[1]
                out.append(f'<figure><a href="{record["svg"]}" target="_blank"><img src="{record["svg"]}" alt="{caption}" loading="lazy"></a><figcaption>图 {diagram} · <a href="{record["svg"]}">查看原图</a> · <a href="{record["mmd"]}">Mermaid 源码</a></figcaption></figure>')
            else:
                out.append('<pre><code>' + html.escape("\n".join(code)) + '</code></pre>')
            i += 1
            continue
        heading = re.match(r"^(#{1,3}) (.+)$", line)
        if heading:
            level = len(heading[1])
            anchor = ""
            if level == 2:
                section += 1; anchor = f' id="section-{section}"'
                toc.append(f'<a href="#section-{section}">{html.escape(heading[2])}</a>')
            out.append(f'<h{level}{anchor}>{inline(heading[2])}</h{level}>')
            i += 1; continue
        if line.startswith("| "):
            rows = []
            while i < len(lines) and lines[i].startswith("|"):
                cells = [c.strip() for c in lines[i].strip().strip("|").split("|")]
                if not all(re.fullmatch(r":?-+:?", c) for c in cells): rows.append(cells)
                i += 1
            table = '<div class="table-wrap"><table><thead><tr>' + ''.join('<th>'+inline(c)+'</th>' for c in rows[0]) + '</tr></thead><tbody>'
            for row in rows[1:]: table += '<tr>'+''.join('<td>'+inline(c)+'</td>' for c in row)+'</tr>'
            out.append(table+'</tbody></table></div>'); continue
        if re.match(r"^(?:- |\d+\. )", line):
            tag = "ul" if line.startswith("- ") else "ol"
            items = []
            while i < len(lines) and re.match(r"^(?:- |\d+\. )", lines[i]):
                items.append('<li>'+inline(re.sub(r"^(?:- |\d+\. )", "", lines[i]))+'</li>'); i += 1
            out.append(f'<{tag}>'+''.join(items)+f'</{tag}>'); continue
        paragraphs = [line]; i += 1
        while i < len(lines) and lines[i].strip() and not re.match(r"^(?:#|```|\| |\d+\. |- )", lines[i]):
            paragraphs.append(lines[i]); i += 1
        out.append('<p>'+inline(" ".join(paragraphs))+'</p>')
    return "\n".join(out), "\n".join(toc)


CSS = """
:root{color-scheme:light;--ink:#24302f;--muted:#60706d;--line:#dce5e1;--accent:#12685b;--paper:#fff;--soft:#f3f7f5}
*{box-sizing:border-box}html{scroll-behavior:smooth;scroll-padding-top:36px}body{margin:0;background:var(--paper);color:var(--ink);font:16px/1.85 'Segoe UI','Microsoft YaHei',sans-serif}
header{border-bottom:1px solid var(--line);padding:22px 5vw;display:flex;flex-wrap:wrap;gap:20px;align-items:baseline}header strong{font-size:20px}header span{color:var(--muted)}header a{margin-left:auto}
.shell{display:grid;grid-template-columns:230px minmax(0,1fr);gap:52px;max-width:1480px;margin:auto;padding:40px 5vw 90px}nav{position:sticky;top:24px;align-self:start;font-size:13px;line-height:1.65}nav a{display:block;padding:7px 0;color:var(--muted)}main{min-width:0}
h1{font-size:34px;line-height:1.5;font-weight:650;margin:0 0 24px}h2{font-size:25px;line-height:1.55;font-weight:600;margin:54px 0 18px;border-top:1px solid var(--line);padding-top:28px}h3{font-size:19px;line-height:1.6;margin:28px 0 12px}p{margin:13px 0}a{color:var(--accent);text-decoration:none;overflow-wrap:anywhere}a:hover{text-decoration:underline}strong{font-weight:650}
ul,ol{padding-left:24px}li{margin:7px 0}code{font:0.9em/1.65 Consolas,'Microsoft YaHei',monospace;background:var(--soft);padding:2px 4px;overflow-wrap:anywhere}pre{background:var(--soft);padding:22px;overflow:auto;line-height:1.5}pre code{padding:0}
.table-wrap{max-width:100%;overflow-x:auto;margin:18px 0 24px}table{border-collapse:collapse;width:100%;font-size:14px;line-height:1.75}th,td{border-bottom:1px solid var(--line);padding:12px 10px;text-align:left;vertical-align:top;min-width:105px}th{background:var(--soft);font-weight:600}td:first-child{min-width:130px}
figure{margin:26px 0;padding:12px 0 22px;border-bottom:1px solid var(--line)}figure img{display:block;max-width:100%;height:auto;margin:auto}figcaption{margin-top:14px;color:var(--muted);font-size:13px;text-align:center}
@media(max-width:1000px){.shell{display:block;padding:24px 5vw 60px}nav{position:static;columns:2;margin:0 0 32px}h1{font-size:29px}}
@media(max-width:540px){body{font-size:15px}nav{columns:1}h1{font-size:25px}h2{font-size:22px}.shell{padding:20px 18px 50px}header{padding:18px}th,td{min-width:120px}}
@media print{header,nav{display:none}.shell{display:block;padding:0}h1{font-size:25px}h2{break-before:page;font-size:21px}h3{break-after:avoid}figure,tr{break-inside:avoid}a{color:inherit}body{font-size:11px}.table-wrap{overflow:visible}}
"""


def write_html(text: str, filename: str, label: str) -> None:
    body, toc = render_markdown(text)
    document = f'<!doctype html>\n<html lang="zh-CN"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><title>{label}</title><style>{CSS}</style></head><body><header><strong>甲第灵光 · 技术阅读档案</strong><span>源码分析 / 2026.09.10</span><a href="report.html">正文</a><a href="code-index.html">源码索引</a></header><div class="shell"><nav aria-label="目录">{toc}</nav><main>{body}</main></div></body></html>\n'
    (HERE / filename).write_text(document, encoding="utf-8")


write_html(analysis, "report.html", "甲第灵光 · 架构与业务实现分析")
write_html(index_text, "code-index.html", "甲第灵光 · 源码索引")
print(json.dumps({"counts": inventory["counts"], "verified_source_references": len(evidence),
                  "diagrams": len(diagram_records), "missing_svg": [r['svg'] for r in diagram_records if not (HERE / r['svg']).exists()]}, ensure_ascii=False))
