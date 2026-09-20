"""Deterministic renderer for the facts-driven V5 decision report.

The orchestrator owns analysis and copy. This module only validates the frozen
snapshot, lays it out, rasterizes complete pages, and packages those images in
one PDF.
"""
from __future__ import annotations

import math
import os
from pathlib import Path
import re
import shutil
import tempfile

import fitz
from pypdf import PdfReader
from reportlab.lib.colors import HexColor
from reportlab.lib.pagesizes import A4
from reportlab.pdfbase import pdfmetrics
from reportlab.pdfbase.ttfonts import TTFont
from reportlab.pdfgen import canvas


W, H = A4
MARGIN = 42
CONTENT_WIDTH = W - 2 * MARGIN
CONTENT_BOTTOM = H - 67

INK = "#5E412F"
MUTED = "#76695D"
GOLD = "#B99763"
JADE = "#557166"
PAPER = "#F4EFE7"
CARD = "#FFFCF7"
SOFT_JADE = "#E5ECE7"
LINE = "#D9C9B4"

FONT_NAME = "CN-V5"
FONT_SIZES = {
    "brand": 9.0,
    "page_number": 23.0,
    "title": 21.0,
    "cover_title": 29.0,
    "subtitle": 10.5,
    "section": 13.0,
    "card_title": 11.7,
    "body": 9.8,
    "note": 8.7,
    "footer": 7.7,
}
PREVIEW_KINDS = ("decision", "evidence", "directions", "confirmation")
FULL_KINDS = (
    "recommendation", "facts", "criteria", "analysis_primary", "analysis_secondary",
    "decision_table", "budget_scope", "designer_brief", "action_plan", "confirmation_sheet",
)


def _plain_text(value, field, *, required=False):
    if not isinstance(value, str):
        raise ValueError(f"{field} must be plain text")
    if required and not value.strip():
        raise ValueError(f"{field} is required")
    if any(ord(char) < 32 and char not in "\n\t" for char in value):
        raise ValueError(f"{field} contains unsupported control characters")
    return value


def _metadata_tree(value, field):
    """Validate bounded JSON metadata that is never rendered as page copy."""
    if value is None or isinstance(value, (bool, int)):
        return
    if isinstance(value, float):
        if not math.isfinite(value):
            raise ValueError(f"{field} contains a non-finite number")
        return
    if isinstance(value, str):
        _plain_text(value, field)
        return
    if isinstance(value, list):
        for index, item in enumerate(value):
            _metadata_tree(item, f"{field}[{index}]")
        return
    if isinstance(value, dict):
        for key, item in value.items():
            _plain_text(key, f"{field} key", required=True)
            _metadata_tree(item, f"{field}.{key}")
        return
    raise ValueError(f"{field} contains unsupported metadata")


def _validate_blocks(blocks, field):
    if not isinstance(blocks, list):
        raise ValueError(f"{field} must be a list")
    for index, block in enumerate(blocks):
        if not isinstance(block, dict):
            raise ValueError(f"{field}[{index}] must be an object")
        _plain_text(block.get("title"), f"{field}[{index}].title", required=True)
        _plain_text(block.get("body"), f"{field}[{index}].body", required=True)


def _validate_string_list(values, field):
    if not isinstance(values, list):
        raise ValueError(f"{field} must be a list")
    for index, value in enumerate(values):
        _plain_text(value, f"{field}[{index}]", required=True)


def validate_decision_v5(data, mode):
    if mode not in ("preview", "full"):
        raise ValueError("invalid report mode")
    if not isinstance(data, dict):
        raise ValueError("report snapshot must be an object")

    meta = data.get("meta")
    if not isinstance(meta, dict):
        raise ValueError("report metadata is required")
    if meta.get("schema_version") != "report-decision-v5":
        raise ValueError("unknown report schema version")
    if meta.get("source") != "report-orchestrator-v5":
        raise ValueError("V5 report requires orchestrator provenance")
    for field in ("report_id", "protocol_version"):
        _plain_text(meta.get(field), f"meta.{field}", required=True)
    for field in ("date", "generation_mode"):
        _plain_text(meta.get(field), f"meta.{field}")
    if meta["protocol_version"] != "report-decision-v5":
        raise ValueError("unknown V5 report protocol")
    if not re.fullmatch(r"[a-f0-9]{64}", meta.get("input_hash", "")):
        raise ValueError("meta.input_hash must be a SHA-256 digest")
    facts = data.get("facts")
    if not isinstance(facts, dict) or not facts:
        raise ValueError("facts must be a non-empty text map")
    for key, value in facts.items():
        _plain_text(key, "facts key", required=True)
        _plain_text(value, f"facts.{key}", required=True)

    recipe = data.get("recipe")
    if recipe not in ("space", "budget", "family", "readiness"):
        raise ValueError("unknown decision recipe")
    diagnosis = data.get("diagnosis")
    if not isinstance(diagnosis, dict):
        raise ValueError("diagnosis must be an object")
    for field in ("id", "recipe", "topic", "reason", "question"):
        _plain_text(diagnosis.get(field), f"diagnosis.{field}", required=True)
    if diagnosis["recipe"] != recipe:
        raise ValueError("diagnosis recipe does not match snapshot recipe")
    _validate_string_list(diagnosis.get("evidence_refs"), "diagnosis.evidence_refs")
    _validate_string_list(diagnosis.get("unknowns"), "diagnosis.unknowns")
    _validate_blocks(diagnosis.get("options"), "diagnosis.options")
    _metadata_tree(diagnosis, "diagnosis")
    if "confirmation" in data and data["confirmation"] is not None:
        confirmation = data["confirmation"]
        if not isinstance(confirmation, dict):
            raise ValueError("confirmation must be an object")
        for field in ("diagnosis_id", "input_hash", "choice"):
            _plain_text(confirmation.get(field), f"confirmation.{field}", required=True)
        if confirmation["diagnosis_id"] != diagnosis["id"] or confirmation["input_hash"] != meta["input_hash"]:
            raise ValueError("confirmation does not match diagnosis and input")
        _validate_string_list(confirmation.get("priorities"), "confirmation.priorities")
        _metadata_tree(confirmation, "confirmation")
    elif mode == "full":
        raise ValueError("full report requires a confirmation snapshot")

    pages = data.get("pages")
    expected_kinds = PREVIEW_KINDS if mode == "preview" else FULL_KINDS
    if not isinstance(pages, list) or len(pages) != len(expected_kinds):
        raise ValueError("V5 report must contain 4 preview pages or 10 full pages")
    rendered_count = len(expected_kinds)
    for index, page in enumerate(pages):
        if not isinstance(page, dict):
            raise ValueError("page must be an object")
        expected_number = index + 1
        if page.get("number") != expected_number:
            raise ValueError("report page order does not match contract")
        expected_access = "free" if mode == "preview" else "paid"
        if page.get("access") != expected_access:
            raise ValueError("report page access does not match contract")
        if page.get("kind") != expected_kinds[index]:
            raise ValueError("report page kind does not match contract")
        for field in ("kind", "title", "subtitle"):
            _plain_text(page.get(field), f"pages[{index}].{field}", required=True)
        _validate_blocks(page.get("blocks", []), f"pages[{index}].blocks")
        _validate_blocks(page.get("priorities", []), f"pages[{index}].priorities")
        _validate_blocks(page.get("actions", []), f"pages[{index}].actions")
        table = page.get("table", [])
        if not isinstance(table, list):
            raise ValueError("page table must be a list")
        if table:
            if not all(isinstance(row, list) and row for row in table):
                raise ValueError("table rows must be non-empty lists")
            columns = len(table[0])
            if columns > 4 or any(len(row) != columns for row in table):
                raise ValueError("table must be rectangular with at most four columns")
            for row_index, row in enumerate(table):
                for column_index, value in enumerate(row):
                    _plain_text(value, f"table[{row_index}][{column_index}]", required=True)
        _plain_text(page.get("note", ""), f"pages[{index}].note")
        _plain_text(page.get("disclaimer", ""), f"pages[{index}].disclaimer")
    if any(size <= 0 or not math.isfinite(size) for size in FONT_SIZES.values()):
        raise ValueError("renderer font sizes must be positive")
    return rendered_count


def _wrapped_lines(text, width, size):
    lines = []
    for paragraph in text.split("\n"):
        if not paragraph:
            lines.append("")
            continue
        line = ""
        for char in paragraph:
            candidate = line + char
            if line and pdfmetrics.stringWidth(candidate, FONT_NAME, size) > width:
                lines.append(line)
                line = char
            else:
                line = candidate
        lines.append(line)
    return lines


class DecisionV5Page:
    def __init__(self, pdf, data, page, total):
        self.pdf = pdf
        self.data = data
        self.page = page
        self.total = total

    def text(self, value, x, top, width, size, color=INK, leading=None, max_lines=None,
             bottom_limit=CONTENT_BOTTOM):
        if size <= 0:
            raise ValueError("font size must be positive")
        leading = leading or size * 1.5
        lines = _wrapped_lines(value, width, size)
        if max_lines is not None and len(lines) > max_lines:
            raise ValueError(f"page {self.page['number']}: text exceeds fixed slot")
        height = len(lines) * leading
        if x < 0 or x + width > W + 0.1 or top < 0 or top + height > bottom_limit:
            raise ValueError(f"page {self.page['number']}: text exceeds page")
        self.pdf.setFont(FONT_NAME, size)
        self.pdf.setFillColor(HexColor(color))
        for index, line in enumerate(lines):
            self.pdf.drawString(x, H - top - size - index * leading, line)
        return height

    def rect(self, x, top, width, height, fill=CARD, stroke=LINE, radius=6):
        if min(width, height) <= 0 or x < 0 or top < 0 or x + width > W + 0.1 or top + height > CONTENT_BOTTOM:
            raise ValueError(f"page {self.page['number']}: component exceeds page")
        self.pdf.setFillColor(HexColor(fill))
        self.pdf.setStrokeColor(HexColor(stroke))
        self.pdf.setLineWidth(0.5)
        self.pdf.roundRect(x, H - top - height, width, height, radius, fill=1, stroke=1)

    def line(self, x1, top1, x2, top2, color=LINE, width=0.6):
        self.pdf.setStrokeColor(HexColor(color))
        self.pdf.setLineWidth(width)
        self.pdf.line(x1, H - top1, x2, H - top2)

    def begin(self):
        self.pdf.setFillColor(HexColor(PAPER))
        self.pdf.rect(0, 0, W, H, fill=1, stroke=0)
        self.text("甲第灵光  ArchLight", MARGIN, 25, 250, FONT_SIZES["brand"], max_lines=1)
        self.text("我家的建房决策报告", W - 184, 25, 142, FONT_SIZES["brand"], MUTED, max_lines=1)
        self.line(MARGIN, 51, W - MARGIN, 51)
        if self.page["number"] == 1:
            self.text("从一家人的生活出发", MARGIN, 82, CONTENT_WIDTH, FONT_SIZES["section"], JADE)
            title_height = self.text(self.page["title"], MARGIN, 116, CONTENT_WIDTH,
                                     FONT_SIZES["cover_title"], max_lines=2)
            subtitle_top = 126 + title_height
            subtitle_height = self.text(self.page["subtitle"], MARGIN, subtitle_top,
                                        CONTENT_WIDTH, FONT_SIZES["subtitle"], MUTED, max_lines=3)
            meta_top = subtitle_top + subtitle_height + 13
            meta_line = "报告编号  " + self.data["meta"]["report_id"]
            if self.data["meta"]["date"]:
                meta_line += "    ·    " + self.data["meta"]["date"]
            meta_height = self.text(meta_line, MARGIN, meta_top, CONTENT_WIDTH,
                                    FONT_SIZES["note"], MUTED, max_lines=1)
            return meta_top + meta_height + 28
        self.text(f"{self.page['number']:02d}", MARGIN, 73, 42, FONT_SIZES["page_number"], GOLD)
        title_height = self.text(self.page["title"], MARGIN + 52, 76, CONTENT_WIDTH - 52,
                                 FONT_SIZES["title"], max_lines=2)
        subtitle_top = 84 + title_height
        subtitle_height = self.text(self.page["subtitle"], MARGIN, subtitle_top, CONTENT_WIDTH,
                                    FONT_SIZES["subtitle"], MUTED, max_lines=3)
        return subtitle_top + subtitle_height + 26

    def card(self, block, top, accent=JADE, x=MARGIN, width=CONTENT_WIDTH):
        title_lines = _wrapped_lines(block["title"], width - 34, FONT_SIZES["card_title"])
        body_lines = _wrapped_lines(block["body"], width - 34, FONT_SIZES["body"])
        if len(title_lines) > 2:
            raise ValueError(f"page {self.page['number']}: card title exceeds fixed slot")
        height = 25 + len(title_lines) * 16 + 8 + len(body_lines) * 14.5 + 18
        self.rect(x, top, width, height)
        title_height = self.text(block["title"], x + 17, top + 16, width - 34,
                                 FONT_SIZES["card_title"], accent, leading=16, max_lines=2)
        self.text(block["body"], x + 17, top + 24 + title_height, width - 34,
                  FONT_SIZES["body"], leading=14.5)
        return top + height + 12

    def block_list(self, items, top, heading, *, numbered=False):
        if not items:
            return top
        top += self.text(heading, MARGIN, top, CONTENT_WIDTH, FONT_SIZES["section"]) + 10
        if len(items) >= 5 and not numbered:
            gap = 12
            width = (CONTENT_WIDTH - gap) / 2
            for index in range(0, len(items), 2):
                bottoms = [self.card(items[index], top, JADE, MARGIN, width)]
                if index + 1 < len(items):
                    bottoms.append(self.card(items[index + 1], top, JADE,
                                             MARGIN + width + gap, width))
                top = max(bottoms)
            return top
        for index, block in enumerate(items):
            if numbered:
                body = {"title": f"{index + 1}. {block['title']}", "body": block["body"]}
            else:
                body = block
            top = self.card(body, top, GOLD if numbered else JADE)
        return top

    def table(self, rows, top):
        if not rows:
            return top
        top += self.text("比较与判断", MARGIN, top, CONTENT_WIDTH, FONT_SIZES["section"]) + 10
        column_width = CONTENT_WIDTH / len(rows[0])
        for row_index, row in enumerate(rows):
            wrapped = [
                _wrapped_lines(value, column_width - 16, FONT_SIZES["body"])
                for value in row
            ]
            row_height = max(34, max(len(lines) for lines in wrapped) * 14 + 18)
            if top + row_height > CONTENT_BOTTOM:
                raise ValueError(f"page {self.page['number']}: table exceeds page")
            for column_index, (value, lines) in enumerate(zip(row, wrapped)):
                x = MARGIN + column_index * column_width
                self.rect(x, top, column_width, row_height,
                          fill=SOFT_JADE if row_index == 0 else CARD, radius=0)
                self.text(value, x + 8, top + 9, column_width - 16, FONT_SIZES["body"],
                          JADE if row_index == 0 else INK, leading=14, max_lines=len(lines))
            top += row_height
        return top + 12

    def finish(self, top):
        note = self.page.get("note", "")
        if note:
            top += self.text("说明｜" + note, MARGIN, top, CONTENT_WIDTH,
                             FONT_SIZES["note"], MUTED, leading=13) + 8
        disclaimer = self.page.get("disclaimer", "")
        if disclaimer:
            top += self.text("专业边界｜" + disclaimer, MARGIN, top, CONTENT_WIDTH,
                             FONT_SIZES["note"], MUTED, leading=13) + 8
        if top > CONTENT_BOTTOM:
            raise ValueError(f"page {self.page['number']}: content exceeds page")
        self.line(MARGIN, H - 48, W - MARGIN, H - 48)
        self.text("决策参考 · 事实与未知项以提交快照为准", MARGIN, H - 39,
                  300, FONT_SIZES["footer"], MUTED, leading=10,
                  bottom_limit=H)
        self.text(_display_report_id(self.data["meta"]["report_id"]),
                  350, H - 39, 140, FONT_SIZES["footer"], INK, leading=10,
                  max_lines=1, bottom_limit=H)
        self.text(f"{self.page['number']:02d} / {self.total:02d}",
                  500, H - 39, 53, FONT_SIZES["footer"], INK, leading=10,
                  max_lines=1, bottom_limit=H)
        self.pdf.showPage()

    def draw(self):
        top = self.begin()
        top = self.block_list(self.page.get("blocks", []), top, "本页结论")
        top = self.block_list(self.page.get("priorities", []), top, "判断优先级")
        top = self.table(self.page.get("table", []), top)
        top = self.block_list(self.page.get("actions", []), top, "下一步行动", numbered=True)
        self.finish(top)


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


def _contracted_page_text(page):
    values = [page["title"], page["subtitle"], page.get("note", ""), page.get("disclaimer", "")]
    for field in ("blocks", "priorities", "actions"):
        for block in page.get(field, []):
            values.extend((block["title"], block["body"]))
    for row in page.get("table", []):
        values.extend(row)
    return [value for value in values if value]


def _compact_text(value):
    return re.sub(r"\s+", "", value)


def _display_report_id(report_id):
    if len(report_id) <= 24:
        return report_id
    return report_id[:8] + "…" + report_id[-8:]


def _verify_canonical(path, data, pages):
    reader = PdfReader(path, strict=True)
    if reader.is_encrypted or len(reader.pages) != pages:
        raise ValueError("canonical PDF page count does not match report mode")
    embedded_font_found = False
    for index, page in enumerate(reader.pages):
        if not math.isclose(float(page.mediabox.width), W, abs_tol=0.75):
            raise ValueError("canonical PDF page is not A4")
        if not math.isclose(float(page.mediabox.height), H, abs_tol=0.75):
            raise ValueError("canonical PDF page is not A4")
        text = page.extract_text() or ""
        expected = data["pages"][index]
        compact = _compact_text(text)
        required_text = _contracted_page_text(expected) + [
            _display_report_id(data["meta"]["report_id"])
        ]
        if any(_compact_text(value) not in compact for value in required_text):
            raise ValueError("canonical PDF is missing contracted text")
        resources = page.get("/Resources")
        fonts = resources.get_object().get("/Font", {}) if resources else {}
        embedded_font_found = embedded_font_found or any(
            _font_is_embedded(font) for font in fonts.get_object().values()
        )
    if not embedded_font_found:
        raise ValueError("canonical PDF does not contain an embedded font")
    with fitz.open(path) as document:
        for page in document:
            for block in page.get_text("blocks", sort=True):
                x0, y0, x1, y1, text, _, block_type = block
                if block_type != 0 or not text.strip():
                    continue
                if x0 < -1.5 or y0 < -1.5 or x1 > page.rect.width + 1.5 or y1 > page.rect.height + 1.5:
                    raise ValueError("canonical PDF contains text outside the page")


def _render_canonical(data, target, mode, font):
    count = 4 if mode == "preview" else 10
    pdfmetrics.registerFont(TTFont(FONT_NAME, str(font)))
    pdf = canvas.Canvas(str(target), pagesize=A4, invariant=1)
    pdf.setTitle("甲第灵光 · 我家的建房决策报告")
    pdf.setAuthor("甲第灵光 ArchLight")
    for page in data["pages"][:count]:
        DecisionV5Page(pdf, data, page, count).draw()
    pdf.save()
    _verify_canonical(target, data, count)


def _rasterize_and_package(canonical, target, image_dir, expected_pages):
    image_dir.mkdir(parents=True, exist_ok=True)
    final = fitz.open()
    try:
        with fitz.open(canonical) as source:
            if source.page_count != expected_pages:
                raise ValueError("canonical PDF page count does not match report mode")
            for index, page in enumerate(source):
                pixmap = page.get_pixmap(matrix=fitz.Matrix(2, 2), alpha=False)
                png = pixmap.tobytes("png")
                (image_dir / f"page-{index + 1:02d}.png").write_bytes(png)
                output_page = final.new_page(width=W, height=H)
                output_page.insert_image(output_page.rect, stream=png, keep_proportion=False)
        final.set_metadata({
            "title": "甲第灵光 · 我家的建房决策报告",
            "author": "甲第灵光 ArchLight",
            "subject": "facts-driven decision report",
            "creator": "report-decision-v5",
            "producer": "report-decision-v5",
        })
        final.save(target, garbage=4, deflate=True, no_new_id=True)
    finally:
        final.close()
    with fitz.open(target) as document:
        if document.page_count != expected_pages:
            raise ValueError("image PDF page count does not match report mode")
        for page in document:
            if page.get_text().strip() or len(page.get_images(full=True)) != 1:
                raise ValueError("image PDF must contain exactly one complete image per page")


def render_decision_v5(data, output, mode, font, *, canonical_output=None, page_images_dir=None):
    """Render atomically; failed validation or overflow preserves prior output."""
    expected_pages = validate_decision_v5(data, mode)
    output = Path(output)
    font = Path(font)
    if not font.is_file():
        raise ValueError("report font is unavailable")
    output.parent.mkdir(parents=True, exist_ok=True)

    temp_root = Path(tempfile.mkdtemp(prefix=".report-v5-", dir=output.parent))
    vector_pdf = temp_root / "canonical.pdf"
    image_dir = temp_root / "pages"
    image_pdf = temp_root / "report.pdf"
    try:
        _render_canonical(data, vector_pdf, mode, font)
        _rasterize_and_package(vector_pdf, image_pdf, image_dir, expected_pages)
        if canonical_output is not None:
            canonical_output = Path(canonical_output)
            canonical_output.parent.mkdir(parents=True, exist_ok=True)
            os.replace(vector_pdf, canonical_output)
        if page_images_dir is not None:
            page_images_dir = Path(page_images_dir)
            page_images_dir.parent.mkdir(parents=True, exist_ok=True)
            page_images_dir.mkdir(parents=True, exist_ok=True)
            for old_page in page_images_dir.glob("page-*.png"):
                old_page.unlink()
            for rendered_page in image_dir.glob("page-*.png"):
                os.replace(rendered_page, page_images_dir / rendered_page.name)
        os.replace(image_pdf, output)
    finally:
        shutil.rmtree(temp_root, ignore_errors=True)
