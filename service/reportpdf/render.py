"""Fixed A4 report renderer. AI supplies bounded copy, never page geometry."""
import argparse
import json
import math
import os
import re
from pathlib import Path
import tempfile

import fitz
from pypdf import PdfReader
from reportlab.lib.colors import HexColor
from reportlab.lib.pagesizes import A4
from reportlab.lib.utils import ImageReader
from reportlab.pdfbase import pdfmetrics
from reportlab.pdfbase.ttfonts import TTFont
from reportlab.pdfgen import canvas

HERE = Path(__file__).resolve().parent
ASSETS = HERE / "assets"
W, H = A4
M, CW = 38, W - 76
INK, MUTED, GOLD, JADE = "#654B39", "#766553", "#C9B38E", "#526F63"
PAPER, WHITE = "#F7F3EC", "#FFFDFA"
FILL = {"bed": "#E3ECE5", "wet": "#E3ECEF", "living": "#F1E5D1",
        "stairs": "#DCD6CA", "hall": "#F4F0E8", "flex": "#EEE7DB"}
KINDS = ["cover", "evidence", "profile", "priorities", "overview", "plans",
         "elevation", "plans", "elevation", "plans", "elevation",
         "comparison", "budget", "actions"]


def _font_is_embedded(font):
    font = font.get_object()
    descendants = font.get("/DescendantFonts", [])
    candidates = [font] + [item.get_object() for item in descendants]
    for candidate in candidates:
        descriptor = candidate.get("/FontDescriptor")
        if descriptor:
            descriptor = descriptor.get_object()
            if any(descriptor.get(key) for key in ("/FontFile", "/FontFile2", "/FontFile3")):
                return True
    return False


def verify_rendered_pdf(path, expected_pages):
    """Reject an incomplete artifact before it replaces the last valid report."""
    path = Path(path)
    try:
        reader = PdfReader(path, strict=True)
        if reader.is_encrypted or len(reader.pages) != expected_pages:
            raise ValueError("rendered PDF page count does not match report mode")

        embedded_font_found = False
        for page in reader.pages:
            width, height = float(page.mediabox.width), float(page.mediabox.height)
            if not math.isclose(width, W, abs_tol=0.75) or not math.isclose(height, H, abs_tol=0.75):
                raise ValueError("rendered PDF page is not A4")
            if not (page.extract_text() or "").strip():
                raise ValueError("rendered PDF contains an empty page")
            resources = page.get("/Resources")
            fonts = resources.get_object().get("/Font", {}) if resources else {}
            embedded_font_found = embedded_font_found or any(
                _font_is_embedded(font) for font in fonts.get_object().values()
            )
        if not embedded_font_found:
            raise ValueError("rendered PDF does not contain an embedded font")

        with fitz.open(path) as document:
            if document.page_count != expected_pages:
                raise ValueError("rendered PDF page count does not match report mode")
            tolerance = 1.5
            for page in document:
                for block in page.get_text("blocks", sort=True):
                    x0, y0, x1, y1, text, _, block_type = block
                    if block_type != 0 or not text.strip():
                        continue
                    if (x0 < -tolerance or y0 < -tolerance
                            or x1 > page.rect.width + tolerance
                            or y1 > page.rect.height + tolerance):
                        raise ValueError("rendered PDF contains text outside the page")
    except ValueError:
        raise
    except Exception as error:
        raise ValueError("rendered PDF cannot be opened and inspected") from error


def wrap(value, width, size):
    lines = []
    for paragraph in str(value).split("\n"):
        line = ""
        for char in paragraph:
            if line and pdfmetrics.stringWidth(line + char, "CN", size) > width:
                if char in "，。；：、？！）】》”" and len(line) > 1:
                    lines.append(line[:-1])
                    line = line[-1] + char
                else:
                    lines.append(line)
                    line = char
            else:
                line += char
        lines.append(line)
    return lines


def validate(data, mode, asset_root=None):
    if mode not in ("preview", "full"):
        raise ValueError("invalid report mode")
    count = 4 if mode == "preview" else 14
    pages = data.get("pages", [])
    if len(pages) not in ((4, 14) if count == 4 else (14,)):
        raise ValueError("report must have exactly 4 or 14 contracted pages")
    for i, page in enumerate(pages[:count]):
        if page.get("number") != i + 1 or page.get("kind") != KINDS[i]:
            raise ValueError("report page order does not match contract")
        if not page.get("title") or not page.get("subtitle"):
            raise ValueError("missing page heading")
    demo = data.get("meta", {}).get("demonstration") is True
    if not demo:
        meta = data.get("meta", {})
        if (meta.get("demonstration") is not False or meta.get("source") != "report-service-v1"
                or not meta.get("protocol_version") or not re.fullmatch(r"[a-f0-9]{64}", meta.get("input_hash", ""))):
            raise ValueError("customer report requires a validated service snapshot")
        for name in data.get("assets", {}):
            trusted_asset(data, name, asset_root)
    if data["meta"].get("schema_version") != "report-a4-v1":
        raise ValueError("unknown report schema version")
    block_counts = {2: 2, 3: 2, 4: 2, 5: 1, 12: 1, 13: 2, 14: 1}
    for page in pages[:count]:
        expected = block_counts.get(page["number"], 0)
        if len(page.get("blocks", [])) != expected:
            raise ValueError("copy block count does not match fixed layout")
    if len(pages[3].get("priorities", [])) != 3:
        raise ValueError("exactly three priorities required")
    for field in ("region", "family", "goal", "floors", "bedrooms", "budget_wan",
                  "budget_scope", "style", "width_m", "depth_m", "residents"):
        if field not in data.get("facts", {}):
            raise ValueError("missing confirmed input")
    for field in ("width_m", "depth_m", "floors", "bedrooms", "budget_wan"):
        value = data["facts"][field]
        if isinstance(value, bool) or not isinstance(value, (int, float)) or not math.isfinite(value) or value <= 0:
            raise ValueError("numeric input must be finite and positive")
    if not isinstance(data["facts"].get("missing"), list) or not data["facts"]["missing"]:
        raise ValueError("pending information must remain explicit")
    if count == 14:
        schemes = data.get("schemes", [])
        if [s.get("id") for s in schemes] != ["A", "B", "C"]:
            raise ValueError("three ordered scheme packs required")
        # The original schematic rectangles are review-only, not buildable plans.
        for scheme in schemes:
            if not demo:
                if not data['meta'].get('pack_id') or not data['meta'].get('pack_version'):
                    raise ValueError("reviewed pack version required")
                floors = scheme.get('floors', [])
                if [floor.get('number') for floor in floors] != list(range(1, int(data['facts']['floors']) + 1)):
                    raise ValueError("floor images must match confirmed floor count")
                ids = [floor.get('asset_id') for floor in floors]
                if ids != scheme.get('plan_assets') or len(set(ids)) != len(ids):
                    raise ValueError("same-pack plan asset sequence required")
                for name in ids + [scheme.get('facade_asset'), scheme.get('volume_asset')]:
                    trusted_asset(data, name, asset_root)
                continue
            if scheme.get("review_status") != "illustrative_unreviewed":
                raise ValueError("unknown schematic provenance")
            if len(scheme.get("floors", [])) != 3 or data["facts"]["floors"] != 3:
                raise ValueError("this illustrative plan template requires three floors")
            for floor in scheme["floors"]:
                for room in floor["rooms"]:
                    x, y, w, h = room["rect"]
                    if min(x, y) < 0 or min(w, h) <= 0 or x+w > 1.00001 or y+h > 1.00001:
                        raise ValueError("schematic region outside envelope")
        rows = pages[11].get("table", [])
        if len(rows) != 7 or any(len(row) != 4 for row in rows):
            raise ValueError("comparison table requires exactly seven rows and four columns")
        if len(pages[13].get("actions", [])) != 6:
            raise ValueError("exactly six actions required")
        allocations = data["budget"]["allocations"]
        if len(allocations) != 5 or any(not math.isfinite(row["percent"]) or not 0 < row["percent"] <= 100 for row in allocations):
            raise ValueError("five bounded budget allocations required")
        if not math.isclose(sum(row["percent"] for row in allocations), 100, abs_tol=0.00001):
            raise ValueError("budget allocation must total 100")


def trusted_asset(data, name, asset_root):
    value = data.get('assets', {}).get(name)
    if not asset_root or not isinstance(value, str) or not Path(value).is_absolute():
        raise ValueError("trusted asset root and catalog reference required")
    root, path = Path(asset_root).resolve(), Path(value).resolve()
    if not path.is_relative_to(root) or path.suffix.lower() not in ('.png', '.jpg', '.jpeg'):
        raise ValueError("report asset outside trusted image catalog")
    if not path.is_file() or path.stat().st_size > 12 * 1024 * 1024:
        raise ValueError("report image unavailable or oversized")
    image = ImageReader(str(path))
    width, height = image.getSize()
    if not 0 < width <= 6000 or not 0 < height <= 6000:
        raise ValueError("report image dimensions exceed limit")
    return path


class Report:
    def __init__(self, data, target, mode, asset_root=None):
        self.data, self.mode = data, mode
        self.asset_root = asset_root
        self.demo = data['meta']['demonstration'] is True
        self.c = canvas.Canvas(str(target), pagesize=A4, invariant=1)
        self.c.setTitle("甲第灵光 · 家庭建房规划报告")
        self.c.setAuthor("甲第灵光 ArchLight")
        self.boxes = []

    def text(self, value, x, top, width=CW, size=11, color=INK, leading=None, max_lines=None):
        leading = leading or size * 1.55
        lines = wrap(value, width, size)
        height = len(lines) * leading
        if (max_lines and len(lines) > max_lines) or x < 0 or x + width > W + 0.1 or top < 0 or top + height > H - 24:
            raise ValueError(f"page {self.page}: text exceeds fixed slot")
        self.c.setFont("CN", size)
        self.c.setFillColor(HexColor(color))
        for i, line in enumerate(lines):
            self.c.drawString(x, H - top - size - i * leading, line)
        self.boxes.append({"page": self.page, "text": value, "x": x, "top": top, "width": width, "height": height})
        return height

    def rect(self, x, top, width, height, fill=WHITE, stroke=GOLD, radius=5):
        self.c.setFillColor(HexColor(fill))
        self.c.setStrokeColor(HexColor(stroke))
        self.c.setLineWidth(0.45)
        self.c.roundRect(x, H-top-height, width, height, radius, fill=1, stroke=1)

    def line(self, x1, y1, x2, y2, color=GOLD, width=0.6):
        self.c.setStrokeColor(HexColor(color))
        self.c.setLineWidth(width)
        self.c.line(x1, H-y1, x2, H-y2)

    def polygon(self, points, fill, stroke=GOLD):
        path = self.c.beginPath()
        path.moveTo(points[0][0], H-points[0][1])
        for x, y in points[1:]:
            path.lineTo(x, H-y)
        path.close()
        self.c.setFillColor(HexColor(fill))
        self.c.setStrokeColor(HexColor(stroke))
        self.c.drawPath(path, fill=1, stroke=1)

    def card(self, block, x, top, width, height, accent=JADE):
        self.rect(x, top, width, height)
        title_h = self.text(block["title"], x+18, top+17, width-36, 12.5,
                            INK if accent == GOLD else accent, max_lines=2)
        body_top = top + 17 + title_h + 12
        self.text(block["body"], x+18, body_top, width-36, 10.3,
                  max_lines=max(1, int((top+height-16-body_top)/16)))

    def picture(self, name, x, top, width, height):
        # Filenames come from a trusted local asset pack, never a remote URL.
        if Path(name).name != name or name not in ("ink-landscape.png", "cover-reference.png"):
            raise ValueError("unknown report asset")
        self.c.drawImage(ImageReader(str(ASSETS/name)), x, H-top-height,
                         width=width, height=height, preserveAspectRatio=True,
                         anchor="c", mask="auto")

    def catalog_picture(self, name, x, top, width, height):
        path = trusted_asset(self.data, name, self.asset_root)
        self.c.drawImage(ImageReader(str(path)), x, H-top-height, width=width,
                         height=height, preserveAspectRatio=True, anchor='c', mask='auto')

    def begin(self, page):
        self.page = page["number"]
        self.c.setFillColor(HexColor(PAPER))
        self.c.rect(0, 0, W, H, fill=1, stroke=0)
        self.c.saveState()
        self.c.setFillAlpha(1 if self.page == 1 else 0.25)
        self.c.drawImage(str(ASSETS/"ink-landscape.png"), 0, 0, width=W, height=H)
        self.c.restoreState()
        self.text("甲第灵光  ArchLight", M, 24, 250, 11)
        self.text("家庭建房前期规划", W-170, 27, 132, 8.5, MUTED)
        self.line(M, 51, W-M, 51)
        if page["kind"] != "cover":
            self.text(f"{self.page:02d}", M, 74, 42, 25, GOLD)
            self.text(page["title"], M+52, 77, CW-52, 20, max_lines=1)
            self.text(page["subtitle"], M, 118, CW, 10.5, MUTED, max_lines=2)

    def end(self):
        self.line(M, H-45, W-M, H-45)
        label = "虚构家庭样稿 · 概念示意，非施工图" if self.data["meta"]["demonstration"] else "前期规划参考 · 非施工图"
        self.text(label, M, H-36, CW-65, 7.5, MUTED, leading=10)
        # Same footer in preview and full; the first four pages are frozen.
        self.text(f"{self.page:02d} / 14", W-88, H-37, 50, 9, INK, leading=11)
        self.c.showPage()

    def cover(self, page):
        f = self.data["facts"]
        self.text("为一家人的日常，认真规划一座家", M, 84, CW, 11, JADE)
        self.text(page["title"], M, 116, CW, 29, max_lines=1)
        self.text(f["goal"], M, 168, CW, 10.5, MUTED, leading=15, max_lines=4)
        x, width = M+270, CW-270
        self.rect(x, 240, width, 330, fill="#F0E7D8")
        if self.demo:
            self.picture("cover-reference.png", x+5, 245, width-10, 320)
            self.text("所选风格参考 · 并非本地块设计成果", x, 582, width, 8, MUTED, max_lines=1)
        else:
            self.text(f["style"], x+20, 314, width-40, 23, JADE, max_lines=2)
            self.text("先把一家人的需要想清楚，\n再选择家的模样。", x+20, 411, width-40, 12, max_lines=4)
        fields = [("建房地区", f["region"]), ("家庭类型", f["family"]),
                  ("空间需求", f'{f["floors"]} 层 / {f["bedrooms"]} 间卧室'),
                  ("预算目标", f'{f["budget_wan"]} 万元 · 口径见内页')]
        for i, (label, value) in enumerate(fields):
            y = 249 + i*78
            self.text(label, M, y, 245, 9, JADE)
            self.text(value, M, y+22, 245, 13, max_lines=2)
            self.line(M, y+61, M+238, y+61, "#D9CBB6", .4)
        self.text("从一家人的需要，到三种可讨论的选择", M, 639, CW, 17, max_lines=1)
        self.text(page["subtitle"], M, 677, CW, 10.5, MUTED, max_lines=2)
        self.text("报告编号  "+self.data["meta"]["report_id"], M, 735, CW, 8.5, MUTED, max_lines=1)
        self.text(self.data["meta"]["date"], M, 756, CW, 8, MUTED, max_lines=1)

    def evidence(self, page):
        half = (CW-16)/2
        f = self.data["facts"]
        confirmed = (f'地区：{f["region"]}\n地块：{f["width_m"]} × {f["depth_m"]} 米\n'
                     f'家庭：{f["residents"]}\n需求：{f["floors"]} 层 / {f["bedrooms"]} 间卧室\n'
                     f'风格：{f["style"]}\n预算：{f["budget_wan"]} 万元')
        pending = '\n'.join(f["missing"]) + '\n\n已填写不等于已核实。地块尺寸来自自报，不是测绘结论。'
        self.card({"title": "你已提供的条件", "body": confirmed}, M, 165, half, 270)
        self.card({"title": "仍需现场确认", "body": pending}, M+half+16, 165, half, 270, GOLD)
        self.card(page["blocks"][0], M, 454, CW, 139)
        self.card(page["blocks"][1], M, 612, CW, 139, GOLD)

    def profile(self, page):
        self.card(page["blocks"][0], M, 163, CW, 140)
        self.card(page["blocks"][1], M, 320, CW, 140)
        f = self.data["facts"]
        cells = [("地块输入", f'{f["width_m"]} × {f["depth_m"]} 米\n自报尺寸，尚未实测'),
                 ("空间需求", f'{f["floors"]} 层 / {f["bedrooms"]} 间卧室'),
                 ("风格偏好", f["style"]), ("预算包含范围", f["budget_scope"])]
        half = (CW-16)/2
        for i, (title, body) in enumerate(cells):
            self.card({"title": title, "body": body}, M+(i%2)*(half+16), 480+(i//2)*142, half, 127)

    def priorities(self, page):
        self.card(page["blocks"][0], M, 163, CW, 153)
        self.text("先决定什么，再比较什么", M, 342, CW, 15)
        for i, block in enumerate(page["priorities"]):
            y = 380+i*69
            self.rect(M, y, 34, 34, fill="#E2E9E1", stroke="#CED8CB", radius=17)
            self.text(str(i+1), M+11, y+7, 20, 14)
            self.text(block["title"], M+53, y, CW-53, 12.5, max_lines=1)
            self.text(block["body"], M+53, y+26, CW-53, 10, MUTED, max_lines=1)
        self.card(page["blocks"][1], M, 611, CW, 141)

    def overview(self, page):
        width = (CW-24)/3
        for i, s in enumerate(self.data["schemes"]):
            x = M+i*(width+12)
            self.rect(x, 166, width, 462)
            self.text(s["id"], x+15, 182, width-30, 30, JADE)
            self.text(s["name"], x+15, 231, width-30, 13.5, max_lines=1)
            self.facade(s, x+15, 279, width-30, 87, iso=True)
            self.text("更看重", x+15, 396, width-30, 9, JADE)
            self.text(s["tagline"], x+15, 420, width-30, 10.2, max_lines=4)
            self.text("需要接受", x+15, 499, width-30, 9, MUTED)
            self.text(s["tradeoff"], x+15, 523, width-30, 10.2, max_lines=5)
        self.card(page["blocks"][0], M, 649, CW, 120)

    def plan(self, floor, x, top, width, height):
        self.rect(x, top, width, height, fill=WHITE, stroke=INK, radius=0)
        for room in floor["rooms"]:
            rx, ry, rw, rh = room["rect"]
            px, py, pw, ph = x + rx * width, top + ry * height, rw * width, rh * height
            self.rect(px + 0.5, py + 0.5, pw - 1, ph - 1, fill=FILL[room["kind"]], stroke="#A89781", radius=0)
            if room["kind"] == "stairs":
                for step in range(1, 8):
                    self.line(px + 4, py + step * ph / 9, px + pw - 4, py + step * ph / 9, "#B7AA96", 0.35)
            size = 8 if pw > 45 else 7
            lines = wrap(room["name"], pw - 6, size)
            label_height = len(lines) * size * 1.25
            self.text(room["name"], px + 3, py + (ph - label_height) / 2, pw - 6, size, INK, size * 1.25, max_lines=4)


    def plans(self, page):
        s = self.scheme(page)
        for i, floor in enumerate(s["floors"]):
            top = 162+i*193
            self.rect(M, top, CW, 179)
            if self.demo:
                self.plan(floor, M+10, top+8, 196, 163)
            else:
                self.catalog_picture(floor['asset_id'], M+10, top+8, 196, 163)
            x = M+225
            self.text(floor["name"], x, top+12, CW-247, 19, JADE, max_lines=1)
            self.text(floor["summary"], x, top+51, CW-247, 11, max_lines=3)
            self.text("功能分区示意。门窗、墙厚、交通净宽与结构尚未设计，不能据此施工。", x, top+118, CW-247, 8.4, MUTED, max_lines=3)
        label = "图例：绿=卧室  蓝=厨卫  米金=活动  灰=楼梯  |  非比例概念分区，下侧为入口示意" if self.demo else "审核素材匹配参考；图中尺寸不代表本地块实测结果，须经现场适配与设计深化。"
        self.text(label, M, 755, CW, 8, MUTED, max_lines=1)

    def facade(self, scheme, x, top, width, height, iso=False):
        if not self.demo:
            self.catalog_picture(scheme['volume_asset' if iso else 'facade_asset'], x, top, width, height)
            return
        fw = width * (0.72 if iso else 1)
        shift = width * 0.25 if iso else 0
        dy = -height * 0.14 if iso else 0
        roof_h = height * 0.13
        body_y = top + roof_h
        body_h = height - roof_h
        if iso:
            self.polygon([(x + fw, body_y), (x + fw + shift, body_y + dy), (x + fw + shift, top + height + dy), (x + fw, top + height)], "#D6C6AD")
            self.polygon([(x, body_y), (x + shift, body_y + dy), (x + fw + shift, body_y + dy), (x + fw, body_y)], "#EEE3CF")
        fill = "#D5AA8B" if scheme["accent"] != "plain" else "#EEE7DA"
        self.rect(x, body_y, fw, body_h, fill=fill, radius=0)
        for level, floor in enumerate(scheme["floors"]):
            y = body_y + (2 - level) * body_h / 3
            self.rect(x, y, fw, max(height * 0.018, 1.5), fill="#EFE2C8", radius=0)
            front = [r for r in floor["rooms"] if abs(r["rect"][1] + r["rect"][3] - 1) < 0.00001 and r["kind"] != "hall"]
            for room in front:
                rx, _, rw, _ = room["rect"]
                wx = x + (rx + rw * 0.18) * fw
                ww = rw * fw * 0.64
                wy = y + body_h * 0.07
                wh = body_h * 0.16
                self.rect(wx, wy, ww, wh, fill="#AAC0BE", stroke="#8C7B63", radius=0)
                self.line(wx + ww / 2, wy, wx + ww / 2, wy + wh, "#F6EFDE", 0.6)
            if iso:
                self.line(x + fw, y, x + fw + shift, y + dy, "#EFE2C8", 1.2)
        door_x = x + scheme["entry_x"] * fw - fw * 0.045
        self.rect(door_x, top + height - body_h * 0.23, fw * 0.09, body_h * 0.23, fill="#785C43", radius=0)
        if scheme["roof"] == "pitched":
            self.polygon([(x - 3, body_y), (x + fw * 0.5, top), (x + fw + 3, body_y)], "#D8C8AD")
        else:
            self.rect(x - 2, body_y - height * 0.035, fw + 4, height * 0.035, fill="#EADDC5", radius=0)
        if scheme["accent"] == "portico":
            for offset in [-0.10, 0.095]:
                self.rect(x + (scheme["entry_x"] + offset) * fw, top + height - body_h * 0.27, fw * 0.025, body_h * 0.27, fill="#F5E7D0", radius=0)
            self.rect(x + (scheme["entry_x"] - 0.13) * fw, top + height - body_h * 0.28, fw * 0.28, body_h * 0.045, fill="#E9D7B9", radius=0)


    def scheme(self, page):
        expected = "ABC"[(self.page-6)//2]
        if page.get("scheme") != expected:
            raise ValueError("scheme does not match fixed page pair")
        return self.data["schemes"]["ABC".index(expected)]

    def elevation(self, page):
        s = self.scheme(page)
        self.rect(M, 163, CW, 250)
        self.text("正立面方向", M+18, 180, CW-36, 10, JADE)
        self.facade(s, M+82, 221, CW-164, 167)
        self.rect(M, 432, 220, 162)
        self.text("同方案体量示意", M+16, 448, 190, 10, JADE)
        self.facade(s, M+30, 484, 168, 88, iso=True)
        self.text(s["facade"], M+243, 439, CW-243, 11.5, max_lines=4)
        self.text(s["facade_caution"], M+243, 523, CW-243, 9, MUTED, max_lines=4)
        half = (CW-16)/2
        self.card({"title":"得到什么", "body":s["gain"]}, M, 615, half, 139)
        self.card({"title":"需要接受什么", "body":s["tradeoff"]}, M+half+16, 615, half, 139, GOLD)
        label = "平面与立面使用同一演示方案参数；未完成开窗、构造及现场适配校核。" if self.demo else "平面、正立面与体量来自同一审核方案；选用并不代表已完成本地块的设计适配。"
        self.text(label, M, 766, CW, 7.5, MUTED, max_lines=1)

    def comparison(self, page):
        widths = [99, 140, 140, CW-379]
        y = 163
        for i, row in enumerate(page["table"]):
            x = M
            height = 42 if i == 0 else 54
            for width, value in zip(widths, row):
                self.rect(x, y, width, height, fill="#E2E9E1" if i == 0 else WHITE, radius=0)
                self.text(value, x+9, y+11, width-18, 9.6, max_lines=2 if i == 0 else 3)
                x += width
            y += height
        self.card(page["blocks"][0], M, 552, CW, 145)
        self.text(page["note"], M, 722, CW, 9, MUTED, max_lines=3)

    def budget_page(self, page):
        f = self.data["facts"]
        self.text(f'{f["budget_wan"]} 万元，先分清要覆盖什么', M, 162, CW, 17)
        self.text(page["note"], M, 199, CW, 9, MUTED, max_lines=2)
        self.rect(M, 244, CW, 220)
        for i, row in enumerate(self.data["budget"]["allocations"]):
            y = 264+i*38
            amount = f["budget_wan"]*row["percent"]/100
            self.text(row["name"], M+17, y, 175, 10)
            self.rect(M+200, y+4, 120, 8, fill="#EDE8DE", stroke="#EDE8DE", radius=4)
            self.rect(M+200, y+4, 120*row["percent"]/100, 8, fill=JADE, stroke=JADE, radius=4)
            self.text(f'{row["percent"]:g}%', M+340, y, 45, 10)
            self.text(f"{amount:g} 万元", M+413, y, 88, 11)
        self.card(page["blocks"][0], M, 487, CW, 125)
        self.card(page["blocks"][1], M, 632, CW, 131, GOLD)

    def actions(self, page):
        for i, block in enumerate(page["actions"]):
            y = 161+i*64
            self.rect(M, y, 29, 29, fill="#E2E9E1", stroke="#CED8CB", radius=14)
            self.text(str(i+1), M+9, y+6, 20, 11)
            self.text(block["title"], M+45, y, CW-45, 12, max_lines=1)
            self.text(block["body"], M+45, y+25, CW-45, 9.5, MUTED, max_lines=1)
        self.card(page["blocks"][0], M, 559, CW, 131)
        self.text("专业边界", M, 715, CW, 10, JADE)
        self.text(page["disclaimer"], M, 738, CW, 8.5, MUTED, max_lines=3)

    def render(self):
        count = 4 if self.mode == "preview" else 14
        for page in self.data["pages"][:count]:
            self.begin(page)
            method = "budget_page" if page["kind"] == "budget" else page["kind"]
            getattr(self, method)(page)
            self.end()
        self.c.save()


def render_report(data, output, mode, font, asset_root=None):
    if data.get("meta", {}).get("schema_version") == "report-decision-v5":
        from decision_v5 import render_decision_v5
        return render_decision_v5(data, output, mode, font)
    validate(data, mode, asset_root)
    pdfmetrics.registerFont(TTFont("CN", str(font)))
    output = Path(output)
    # Publish only a completely rendered file; overflow must never expose half a PDF.
    output.parent.mkdir(parents=True, exist_ok=True)
    fd, name = tempfile.mkstemp(prefix=".report-", suffix=".pdf", dir=output.parent)
    os.close(fd)
    try:
        report = Report(data, name, mode, asset_root)
        report.render()
        verify_rendered_pdf(name, expected_pages=4 if mode == "preview" else 14)
        os.replace(name, output)
        return report.boxes
    finally:
        if os.path.exists(name):
            os.unlink(name)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--input", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--mode", choices=("preview", "full"), required=True)
    parser.add_argument("--font", type=Path, required=True)
    parser.add_argument("--asset-root", type=Path)
    args = parser.parse_args()
    try:
        data = json.loads(args.input.read_text(encoding="utf-8"))
        render_report(data, args.output, args.mode, args.font, args.asset_root)
    except Exception:
        # Do not expose user content, paths or provider output through caller logs.
        parser.exit(1, "Report rendering failed; check contract, font and local assets.\n")


if __name__ == "__main__":
    main()
