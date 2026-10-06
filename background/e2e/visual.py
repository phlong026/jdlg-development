#!/usr/bin/env python3.11
"""页面视觉回归：对每个路由截取内容区，保存后逐像素比对。仅限本地联调环境。

  python3.11 e2e/visual.py capture /tmp/visual-before     # 在改造前的代码上拍基线
  python3.11 e2e/visual.py capture /tmp/visual-after      # 改造后再拍一次
  python3.11 e2e/visual.py compare /tmp/visual-before /tmp/visual-after

截取的是 .layout-content 的左上角固定 1150x700 区域（不含侧栏、顶栏、书签栏），
因此"去掉书签栏导致内容区变高"不会产生差异；任何样式丢失、布局错位都会被抓到。
"""
import os
import re
import sys

import numpy as np
from PIL import Image

WEB = os.environ.get("E2E_WEB", "http://127.0.0.1:5180")
PASSWORD = os.environ.get("E2E_PASSWORD", "LocalAdmin!2026")
ROUTES_FILE = os.path.join(os.path.dirname(__file__), "..", "src", "routes", "index.tsx")
CLIP_W, CLIP_H = 1150, 700
# 像素差异占比超过该阈值才算"有差异"（抗抗锯齿 / 亚像素噪音）
THRESHOLD = 0.002


def routes():
    src = open(ROUTES_FILE, encoding="utf-8").read()
    return [p for p in re.findall(r"\bpath:\s*'(/[^']*)'", src) if p != "/login"]


def name(path):
    return path.strip("/").replace("/", "_") or "root"


def capture(out_dir):
    from playwright.sync_api import sync_playwright
    if "127.0.0.1" not in WEB and "localhost" not in WEB:
        sys.exit("拒绝运行：只允许本机地址")
    os.makedirs(out_dir, exist_ok=True)
    with sync_playwright() as p:
        browser = p.chromium.launch()
        page = browser.new_context(viewport={"width": 1366, "height": 860}).new_page()
        page.goto(WEB + "/admin/login")
        page.fill("#login-account", "admin_local")
        page.fill("#login-password", PASSWORD)
        page.click("button[type=submit]")
        page.wait_for_url(re.compile(r".*/admin/dashboard"), timeout=15000)
        for path in routes():
            page.goto(WEB + "/admin" + path)
            page.wait_for_selector(".layout-content", timeout=15000)
            page.wait_for_load_state("networkidle", timeout=20000)
            page.wait_for_timeout(400)  # 让图表 / 过渡动画落定
            box = page.locator(".layout-content").bounding_box()
            clip = {"x": box["x"], "y": box["y"], "width": min(box["width"], CLIP_W), "height": min(box["height"], CLIP_H)}
            page.screenshot(path=os.path.join(out_dir, name(path) + ".png"), clip=clip)
            print(f"  已截取 {path}")
        browser.close()


def compare(before, after):
    bad = 0
    for path in routes():
        f = name(path) + ".png"
        pb, pa = os.path.join(before, f), os.path.join(after, f)
        if not (os.path.exists(pb) and os.path.exists(pa)):
            print(f"  ✘ {path}: 缺少截图")
            bad += 1
            continue
        a, b = Image.open(pb).convert("RGB"), Image.open(pa).convert("RGB")
        if a.size != b.size:
            print(f"  ✘ {path}: 尺寸不同 {a.size} → {b.size}")
            bad += 1
            continue
        diff = np.abs(np.asarray(a, dtype=np.int16) - np.asarray(b, dtype=np.int16)).sum(axis=2) > 24
        ratio = diff.mean()
        if ratio > THRESHOLD:
            Image.fromarray((diff * 255).astype("uint8")).save(os.path.join(after, name(path) + ".diff.png"))
            print(f"  ✘ {path}: {ratio:.2%} 的像素不同（差异图 {name(path)}.diff.png）")
            bad += 1
        else:
            print(f"  ✔ {path}: 差异 {ratio:.3%}")
    print(f"\n{'全部一致' if not bad else str(bad) + ' 个页面有差异'}（共 {len(routes())} 个页面）")
    sys.exit(1 if bad else 0)


if __name__ == "__main__":
    if len(sys.argv) >= 3 and sys.argv[1] == "capture":
        capture(sys.argv[2])
    elif len(sys.argv) >= 4 and sys.argv[1] == "compare":
        compare(sys.argv[2], sys.argv[3])
    else:
        sys.exit(__doc__)
