"""Variable whole-house report layout with verified, scheme-bound illustrations.

The snapshot defines semantic sections. Pagination is a renderer concern: long
copy continues on another physical page instead of being shortened or clipped.
"""
from __future__ import annotations

import hashlib
import math
import os
from pathlib import Path
import re
import shutil
import tempfile

import fitz
from PIL import Image
from pypdf import PdfReader
from reportlab.lib.colors import HexColor
from reportlab.pdfbase import pdfmetrics
from reportlab.pdfbase.ttfonts import TTFont
from reportlab.pdfgen import canvas

from decision_v5 import (
    CARD, CONTENT_BOTTOM, CONTENT_WIDTH, FONT_NAME, GOLD, H, INK, JADE,
    LINE, MARGIN, MUTED, PAPER, SOFT_JADE, W, DecisionV5Page,
    _compact_text, _display_report_id, _font_is_embedded,
    _plain_text, _rasterize_and_package, _resolve_illustration_asset, _wrapped_lines,
)


PREVIEW_KINDS = ("house_profile", "house_constraints", "house_goals", "house_confirmation")
ENDING_KINDS = ("house_comparison", "house_budget", "house_designer_brief", "house_actions")
BODY_SIZE, BODY_LEADING = 10, 15
CARD_TITLE_SIZE, CARD_TITLE_LEADING = 12, 17
MAX_PHYSICAL_PAGES = 160


def _text(value, field, required=True, limit=24000):
    value = _plain_text(value, field, required=required)
    if len(value) > limit:
        raise ValueError(f"{field} exceeds text limit")
    return value


def _integer(value, field, lower=1, upper=30):
    if type(value) is not int or not lower <= value <= upper:
        raise ValueError(f"{field} must be a bounded integer")
    return value


def _list(value, field, lower=0, upper=64):
    if not isinstance(value, list) or not lower <= len(value) <= upper:
        raise ValueError(f"{field} has an invalid number of items")
    return value


def _object(value, field):
    if not isinstance(value, dict):
        raise ValueError(f"{field} must be an object")
    return value


def _strings(values, field, lower=0, upper=32):
    for value in _list(values, field, lower, upper):
        _text(value, field, limit=2000)


def _blocks(values, field, lower=0):
    for block in _list(values, field, lower):
        _object(block, field)
        _text(block.get("title"), field + ".title", limit=200)
        _text(block.get("body"), field + ".body")


def _table(rows, field, columns=None):
    _list(rows, field, 0, 64)
    if not rows:
        return
    size = len(_list(rows[0], field, 2, 4))
    if columns is not None and size != columns:
        raise ValueError(f"{field} columns must match the scheme count")
    for row in rows:
        _list(row, field, size, size)
        for value in row:
            _text(value, field, limit=12000)


def _refs(values, facts, field):
    _strings(values, field, 1)
    if len(set(values)) != len(values) or any(value not in facts for value in values):
        raise ValueError(f"{field} contains an unknown or duplicate fact reference")


def _validate_header(data, mode):
    _object(data, "snapshot")
    meta = _object(data.get("meta"), "meta")
    if (meta.get("schema_version") != "house-report-v1"
            or meta.get("protocol_version") != "report-house-v1"
            or meta.get("source") != "house-agent-v1"):
        raise ValueError("house report requires matching schema, protocol and provenance")
    _text(meta.get("report_id"), "meta.report_id", limit=128)
    _text(meta.get("date", ""), "meta.date", required=False, limit=80)
    _text(meta.get("generation_mode", ""), "meta.generation_mode", required=False, limit=80)
    if not isinstance(meta.get("input_hash"), str) or not re.fullmatch(r"[a-f0-9]{64}", meta["input_hash"]):
        raise ValueError("meta.input_hash must be a SHA-256 digest")
    facts = _object(data.get("facts"), "facts")
    if not 1 <= len(facts) <= 256:
        raise ValueError("facts must contain bounded evidence")
    for key, value in facts.items():
        _text(key, "fact key", limit=128)
        _text(value, "fact value", limit=4000)
    if data.get("recipe") not in ("space", "budget", "family", "readiness"):
        raise ValueError("unknown house report recipe")
    diagnosis = _object(data.get("diagnosis"), "diagnosis")
    for field in ("id", "topic", "reason", "question"):
        _text(diagnosis.get(field), "diagnosis." + field)
    if diagnosis.get("recipe") != data["recipe"]:
        raise ValueError("diagnosis recipe mismatch")
    _refs(diagnosis.get("evidence_refs"), facts, "diagnosis.evidence_refs")
    _strings(diagnosis.get("unknowns") or [], "diagnosis.unknowns")
    _blocks(diagnosis.get("options"), "diagnosis.options", 1)
    confirmation = data.get("confirmation")
    if mode == "full":
        _object(confirmation, "confirmation")
        if (confirmation.get("diagnosis_id") != diagnosis["id"]
                or confirmation.get("input_hash") != meta["input_hash"]):
            raise ValueError("confirmation does not match frozen input and diagnosis")
        if confirmation.get("choice") not in ("confirm", "undecided"):
            raise ValueError("house confirmation must preserve undecided or confirm the brief")
        _strings(confirmation.get("priorities"), "confirmation.priorities", 1, 8)
    elif confirmation is not None:
        raise ValueError("preview cannot contain a paid confirmation")
    return facts


def _validate_house(data, facts, asset_root):
    house = _object(data.get("house"), "house")
    plan = _object(house.get("plan"), "house.plan")
    count = _integer(plan.get("scheme_count"), "scheme_count", 2, 3)
    _strings(plan.get("focus"), "house.plan.focus", 1, 12)
    schemes = _list(house.get("schemes"), "house.schemes", count, count)
    try:
        floor_count = int(facts["intent.floors"])
        bedroom_count = int(facts["intent.bedrooms"])
    except (KeyError, TypeError, ValueError) as error:
        raise ValueError("confirmed floor count is required") from error
    _integer(floor_count, "intent.floors", 1, 6)
    _integer(bedroom_count, "intent.bedrooms", 1, 20)
    by_scheme = {}
    for scheme in schemes:
        _object(scheme, "scheme")
        for field in ("id", "version", "name", "strategy", "gain", "tradeoff", "facade"):
            _text(scheme.get(field), "scheme." + field, limit=2400)
        if scheme["id"] in by_scheme:
            raise ValueError("duplicate scheme ID")
        by_scheme[scheme["id"]] = scheme
        _strings(scheme.get("cost_sensitive"), "scheme.cost_sensitive", 1)
        _strings(scheme.get("assumptions") or [], "scheme.assumptions")
        _refs(scheme.get("evidence_refs"), facts, "scheme.evidence_refs")
        floors = _list(scheme.get("floors"), "scheme.floors", floor_count, floor_count)
        bedroom_total = 0
        for number, floor in enumerate(floors, 1):
            _object(floor, "floor")
            if type(floor.get("number")) is not int or floor["number"] != number:
                raise ValueError("scheme floors must match the confirmed floor sequence")
            for field in ("name", "summary", "circulation", "stair_position"):
                _text(floor.get(field), "floor." + field, limit=2400)
            for room in _list(floor.get("rooms"), "floor.rooms", 1, 32):
                _object(room, "room")
                _text(room.get("name"), "room.name", limit=120)
                _text(room.get("kind"), "room.kind", limit=64)
                _integer(room.get("count"), "room.count", 1, 20)
                if room["kind"] in ("bedroom", "elder_bedroom"):
                    bedroom_total += room["count"]
        if bedroom_total != bedroom_count:
            raise ValueError("scheme bedroom count differs from the confirmed requirement")
    _table(house.get("comparison"), "house.comparison", count + 1)
    if len(house["comparison"]) < 2:
        raise ValueError("whole-house comparison requires a header and evidence rows")
    _blocks(house.get("budget"), "house.budget", 1)
    _blocks(house.get("designer_brief"), "house.designer_brief", 1)
    for action in _list(house.get("actions"), "house.actions", 1, 32):
        _object(action, "action")
        for field in ("task", "owner", "prerequisite", "evidence"):
            _text(action.get(field), "action." + field, limit=2400)

    images = _list(house.get("images"), "house.images", count * (floor_count + 2), count * (floor_count + 2))
    by_id, by_asset, by_slot = {}, {}, {}
    for image in images:
        _object(image, "house image")
        for field in ("id", "asset", "scheme_id", "scheme_version", "model"):
            _text(image.get(field), "image." + field, limit=1024)
        scheme = by_scheme.get(image["scheme_id"])
        if scheme is None or image["scheme_version"] != scheme["version"]:
            raise ValueError("image must belong to the same scheme and version")
        kind = image.get("kind")
        if kind not in ("floor_plan", "massing", "exterior"):
            raise ValueError("unknown whole-house image kind")
        floor = image.get("floor", 0)
        if kind == "floor_plan":
            _integer(floor, "image.floor", 1, floor_count)
            if image.get("reference_id"):
                raise ValueError("floor plan must originate from the structured floor requirements")
        elif floor != 0 or type(floor) is not int:
            raise ValueError("exterior image cannot refer to a floor")
        slot = (scheme["id"], kind, floor)
        if image["id"] in by_id or image["asset"] in by_asset or slot in by_slot:
            raise ValueError("duplicate image identity, path or scheme slot")
        path = _resolve_illustration_asset(asset_root, image["asset"])
        width = _integer(image.get("width"), "image.width", 1, 8192)
        height = _integer(image.get("height"), "image.height", 1, 8192)
        with Image.open(path) as source:
            if source.size != (width, height):
                raise ValueError("image dimensions do not match the frozen asset")
        digest = image.get("sha256")
        if (not isinstance(digest, str) or not re.fullmatch(r"[a-f0-9]{64}", digest)
                or hashlib.sha256(path.read_bytes()).hexdigest() != digest):
            raise ValueError("image checksum does not match the frozen asset")
        by_id[image["id"]] = image
        by_asset[image["asset"]] = image
        by_slot[slot] = image
    expected = list(PREVIEW_KINDS) + ["house_overview"]
    slots_by_page = [None] * 5
    for scheme in schemes:
        sid = scheme["id"]
        for number in range(1, floor_count + 1):
            if (sid, "floor_plan", number) not in by_slot:
                raise ValueError("a confirmed floor plan is missing")
            expected.append("house_floor_plan")
            slots_by_page.append([by_slot[(sid, "floor_plan", number)]])
        for kind, reference_slot in (("massing", (sid, "floor_plan", 1)), ("exterior", (sid, "massing", 0))):
            image = by_slot.get((sid, kind, 0))
            reference = by_slot.get(reference_slot)
            if image is None or reference is None or image.get("reference_id") != reference["id"]:
                raise ValueError("exterior reference chain must stay within the same scheme")
        expected.append("house_exterior")
        slots_by_page.append([by_slot[(sid, "massing", 0)], by_slot[(sid, "exterior", 0)]])
    for image in images:
        ref = image.get("reference_id")
        if ref:
            parent = by_id.get(ref)
            if (parent is None or parent["id"] == image["id"]
                    or parent["scheme_id"] != image["scheme_id"]
                    or parent["scheme_version"] != image["scheme_version"]):
                raise ValueError("invalid image reference identity")
    expected.extend(ENDING_KINDS)
    slots_by_page.extend([None] * len(ENDING_KINDS))
    return expected, slots_by_page


def validate_house_v1(data, mode, asset_root=None):
    """Validate semantic pages and real local assets before creating any PDF."""
    if mode not in ("preview", "full"):
        raise ValueError("invalid report mode")
    facts = _validate_header(data, mode)
    if mode == "full":
        expected, slots = _validate_house(data, facts, asset_root)
    else:
        if data.get("house") is not None:
            raise ValueError("preview must not contain paid house content")
        expected, slots = list(PREVIEW_KINDS), [None] * 4
    pages = _list(data.get("pages"), "pages", len(expected), len(expected))
    for index, (page, kind, image_slots) in enumerate(zip(pages, expected, slots)):
        _object(page, "page")
        if (type(page.get("number")) is not int or page["number"] != index + 1
                or page.get("kind") != kind or page.get("access") != ("free" if mode == "preview" else "paid")):
            raise ValueError("house section order or access does not match the contract")
        _text(page.get("title"), "page.title", limit=120)
        _text(page.get("subtitle"), "page.subtitle", limit=500)
        for field in ("blocks", "priorities", "actions"):
            _blocks(page.get(field, []), "page." + field)
        _table(page.get("table", []), "page.table")
        if kind not in ("house_comparison", "house_actions") and not page.get("blocks"):
            raise ValueError("house section must contain its explanatory content")
        if kind == "house_comparison" and page.get("table") != data["house"]["comparison"]:
            raise ValueError("comparison page differs from the approved house content")
        for page_kind, field in (("house_budget", "budget"), ("house_designer_brief", "designer_brief")):
            if kind == page_kind and page.get("blocks") != data["house"][field]:
                raise ValueError("section copy differs from the approved house content")
        if kind == "house_actions" and not page.get("actions"):
            raise ValueError("house action page requires an actionable handoff")
        for field in ("fixed", "dynamic", "ai", "note", "disclaimer"):
            _text(page.get(field, ""), "page." + field, required=False)
        _refs(page.get("sources"), facts, "page.sources")
        illustrations = _list(page.get("illustrations", []), "page.illustrations", 0, 2)
        for illustration in illustrations:
            _object(illustration, "illustration")
        if image_slots is None:
            if illustrations:
                raise ValueError("illustrations require a scheme-bound image section")
        else:
            if page.get("scheme") != image_slots[0]["scheme_id"]:
                raise ValueError("page scheme does not match its illustration")
            if [item.get("asset") for item in illustrations] != [item["asset"] for item in image_slots]:
                raise ValueError("page illustrations must exactly match its scheme and floor")
            for illustration in illustrations:
                _object(illustration, "illustration")
                if set(illustration) != {"asset", "title", "caption"}:
                    raise ValueError("illustration fields do not match contract")
                _text(illustration["title"], "illustration.title", limit=200)
                _text(illustration["caption"], "illustration.caption", limit=4000)
    return len(pages)


def _card_fragments(title, body, max_height):
    """Split at measured lines while preserving every character of the body."""
    width = CONTENT_WIDTH - 32
    title_lines = _wrapped_lines(title, width, CARD_TITLE_SIZE)
    lines = _wrapped_lines(body, width, BODY_SIZE)
    fixed = 31 + len(title_lines) * CARD_TITLE_LEADING
    capacity = int((max_height - fixed) // BODY_LEADING)
    if capacity < 1:
        raise ValueError("card heading leaves no room for readable content")
    for offset in range(0, len(lines), capacity):
        chunk = lines[offset:offset + capacity]
        yield {"type": "card", "title": title, "body": "\n".join(chunk),
               "height": fixed + len(chunk) * BODY_LEADING, "continued": offset > 0}


def _heading_top(page):
    title_height = len(_wrapped_lines(page["title"], CONTENT_WIDTH - 50, 21)) * 29
    subtitle_height = len(_wrapped_lines(page["subtitle"], CONTENT_WIDTH, 10.5)) * 16
    return 82 + title_height + 14 + subtitle_height + 18


def _paginate(data, asset_root):
    physical = []
    for logical in data["pages"]:
        top_start = _heading_top(logical)
        max_height = CONTENT_BOTTOM - top_start
        if max_height < 180:
            raise ValueError("page heading leaves too little room for readable content")
        sheets = [{"logical": logical, "items": [], "top": top_start, "continuation": 0}]

        def place(item):
            current = sheets[-1]
            if current["top"] + item["height"] > CONTENT_BOTTOM:
                sheets.append({"logical": logical, "items": [], "top": top_start,
                               "continuation": len(sheets)})
                current = sheets[-1]
            if item["height"] > max_height:
                raise ValueError("layout item exceeds a complete page")
            item["top"] = current["top"]
            current["items"].append(item)
            current["top"] += item["height"] + 12

        # Plans stay wide. Exterior views share a row so gains and tradeoffs can
        # stay beside their images on the same page.
        paired = logical["kind"] == "house_exterior"
        panel_width = (CONTENT_WIDTH - 12) / 2 if paired else CONTENT_WIDTH
        figures, caption_continuations = [], []
        for illustration in logical.get("illustrations", []):
            path = _resolve_illustration_asset(asset_root, illustration["asset"])
            with Image.open(path) as source:
                width, height = source.size
            image_width = panel_width - 24
            image_height = min(image_width * height / width, max_height - 80)
            if logical["kind"] == "house_floor_plan" and image_height * width / height < 460:
                raise ValueError("floor plan cannot fit at a readable width")
            title_lines = _wrapped_lines(illustration["title"], panel_width - 24, 11.5)
            caption_lines = _wrapped_lines(illustration["caption"], panel_width - 24, 9)
            caption_limit = max(1, int((max_height - image_height - 40 - len(title_lines) * 16) // 13))
            visible_caption = caption_lines[:caption_limit]
            figures.append({"type": "image", "path": path, "asset": illustration["asset"],
                            "image_height": image_height, "title": illustration["title"],
                            "caption": "\n".join(visible_caption),
                            "height": image_height + 34 + len(title_lines) * 16 + len(visible_caption) * 13})
            if len(caption_lines) > caption_limit:
                caption_continuations.extend(_card_fragments(
                    "图示说明（续）", "\n".join(caption_lines[caption_limit:]), max_height))
        if paired:
            place({"type": "image_pair", "figures": figures,
                   "height": max(figure["height"] for figure in figures)})
        else:
            for figure in figures:
                place(figure)
        for item in caption_continuations:
            place(item)
        for field in ("fixed", "dynamic", "ai"):
            if logical.get(field):
                for item in _card_fragments("方案说明", logical[field], max_height):
                    place(item)
        for field in ("blocks", "priorities", "actions"):
            for block in logical.get(field, []):
                for item in _card_fragments(block["title"], block["body"], max_height):
                    place(item)
        rows = logical.get("table", [])
        if rows:
            column_width = CONTENT_WIDTH / len(rows[0])
            for index, row in enumerate(rows):
                lines = [_wrapped_lines(value, column_width - 16, 9.6) for value in row]
                capacity = max(1, int((max_height - 20) // 14))
                for offset in range(0, max(map(len, lines)), capacity):
                    chunks = [part[offset:offset + capacity] for part in lines]
                    place({"type": "row", "values": ["\n".join(chunk) for chunk in chunks],
                           "header": index == 0, "height": 20 + max(map(len, chunks)) * 14})
        for field, title in (("note", "补充说明"), ("disclaimer", "专业边界")):
            if logical.get(field):
                lines = _wrapped_lines(title + "｜" + logical[field], CONTENT_WIDTH, 9)
                capacity = max(1, int(max_height // 13))
                for offset in range(0, len(lines), capacity):
                    chunk = lines[offset:offset + capacity]
                    place({"type": "note", "body": "\n".join(chunk), "height": len(chunk) * 13})
        physical.extend(sheets)
        if len(physical) > MAX_PHYSICAL_PAGES:
            raise ValueError("report exceeds bounded physical page count")
    return physical


def _draw_figure(painter, pdf, item, x, top, width):
    painter.rect(x, top, width, item["height"])
    pdf.drawImage(str(item["path"]), x + 12, H - top - 12 - item["image_height"],
                  width=width - 24, height=item["image_height"],
                  preserveAspectRatio=True, anchor="c", mask="auto")
    copy_top = top + 22 + item["image_height"]
    title_height = painter.text(item["title"], x + 12, copy_top, width - 24, 11.5, JADE, leading=16)
    painter.text(item["caption"], x + 12, copy_top + title_height + 5,
                 width - 24, 9, MUTED, leading=13)
    return [item["title"], item["caption"]]


def _draw_sheet(pdf, data, sheet, number, total):
    logical = sheet["logical"]
    painter = DecisionV5Page(pdf, data, {"number": number}, total)
    pdf.setFillColor(HexColor(PAPER))
    pdf.rect(0, 0, W, H, fill=1, stroke=0)
    painter.text("甲第灵光  ArchLight", MARGIN, 25, 250, 9)
    painter.text("家庭建房规划报告", W - 184, 25, 142, 9, MUTED)
    painter.line(MARGIN, 51, W - MARGIN, 51)
    painter.text(f"{logical['number']:02d}", MARGIN, 76, 42, 23, GOLD)
    title_height = painter.text(logical["title"], MARGIN + 50, 79, CONTENT_WIDTH - 50, 21, leading=29)
    painter.text(logical["subtitle"], MARGIN, 93 + title_height, CONTENT_WIDTH, 10.5, MUTED, leading=16)
    required = [logical["title"], logical["subtitle"]]
    if sheet["continuation"]:
        painter.text(f"本节续页 {sheet['continuation']}", W - 155, 58, 110, 8, JADE)
    for item in sheet["items"]:
        top = item["top"]
        if item["type"] == "card":
            painter.rect(MARGIN, top, CONTENT_WIDTH, item["height"])
            height = painter.text(item["title"], MARGIN + 16, top + 12, CONTENT_WIDTH - 32,
                                  CARD_TITLE_SIZE, JADE, leading=CARD_TITLE_LEADING)
            painter.text(item["body"], MARGIN + 16, top + 19 + height, CONTENT_WIDTH - 32,
                         BODY_SIZE, INK, leading=BODY_LEADING)
            required.extend((item["title"], item["body"]))
        elif item["type"] == "image":
            required.extend(_draw_figure(painter, pdf, item, MARGIN, top, CONTENT_WIDTH))
        elif item["type"] == "image_pair":
            width = (CONTENT_WIDTH - 12) / 2
            for index, figure in enumerate(item["figures"]):
                required.extend(_draw_figure(painter, pdf, figure, MARGIN + index * (width + 12), top, width))
        elif item["type"] == "note":
            painter.text(item["body"], MARGIN, top, CONTENT_WIDTH, 9, MUTED, leading=13)
            required.append(item["body"])
        elif item["type"] == "row":
            width = CONTENT_WIDTH / len(item["values"])
            for index, value in enumerate(item["values"]):
                x = MARGIN + width * index
                painter.rect(x, top, width, item["height"],
                             fill=SOFT_JADE if item["header"] else CARD, radius=0)
                painter.text(value, x + 8, top + 10, width - 16, 9.6,
                             JADE if item["header"] else INK, leading=14)
            required.extend(item["values"])
    painter.line(MARGIN, H - 48, W - MARGIN, H - 48)
    date = data["meta"].get("date", "")
    if number == 1 and date:
        painter.text(date, MARGIN, H - 63, 240, 7.7, MUTED, leading=10, bottom_limit=H)
        required.append(date)
    painter.text("概念规划参考 · 需经现场与专业设计核实", MARGIN, H - 39, 280, 7.7, MUTED,
                 leading=10, bottom_limit=H)
    report_id = _display_report_id(data["meta"]["report_id"])
    painter.text(report_id, 340, H - 39, 155, 7.7, leading=10, bottom_limit=H)
    painter.text(f"{number:02d} / {total:02d}", 500, H - 39, 53, 7.7, leading=10, bottom_limit=H)
    pdf.showPage()
    return required + [report_id]


def _verify_canonical(path, physical, required):
    reader = PdfReader(path, strict=True)
    if reader.is_encrypted or len(reader.pages) != len(physical):
        raise ValueError("canonical page count does not match pagination")
    for index, page in enumerate(reader.pages):
        if (not math.isclose(float(page.mediabox.width), W, abs_tol=.75)
                or not math.isclose(float(page.mediabox.height), H, abs_tol=.75)):
            raise ValueError("canonical page is not A4")
        text = _compact_text(page.extract_text() or "")
        if any(_compact_text(value) not in text for value in required[index] if value):
            raise ValueError("canonical page is missing complete contracted text")
        fonts = page["/Resources"].get_object().get("/Font", {}).get_object()
        if not any(_font_is_embedded(font) for font in fonts.values()):
            raise ValueError("canonical page lacks an embedded font")
    with fitz.open(path) as document:
        for page, sheet in zip(document, physical):
            expected_images = sum(2 if item["type"] == "image_pair" else int(item["type"] == "image")
                                  for item in sheet["items"])
            if len(page.get_image_info()) != expected_images:
                raise ValueError("canonical illustrations do not match their section")
            for block in page.get_text("blocks", sort=True):
                x0, y0, x1, y1, text, _, kind = block
                if kind == 0 and text.strip() and (x0 < -1.5 or y0 < -1.5 or x1 > W + 1.5 or y1 > H + 1.5):
                    raise ValueError("canonical text exceeds the page")


def render_house_v1(data, output, mode, font, *, canonical_output=None,
                    page_images_dir=None, asset_root=None):
    """Publish one image PDF atomically after text, font and asset checks pass."""
    validate_house_v1(data, mode, asset_root)
    output, font = Path(output), Path(font)
    if not font.is_file():
        raise ValueError("report font is unavailable")
    registered_font = TTFont(FONT_NAME, str(font))
    pdfmetrics.registerFont(registered_font)
    physical = _paginate(data, asset_root)
    output.parent.mkdir(parents=True, exist_ok=True)
    temp = Path(tempfile.mkdtemp(prefix=".house-report-", dir=output.parent))
    try:
        canonical, image_pdf, images = temp / "canonical.pdf", temp / "report.pdf", temp / "pages"
        pdf = canvas.Canvas(str(canonical), pagesize=(W, H), invariant=1)
        pdf.setTitle("甲第灵光 · 家庭建房规划报告")
        pdf.setAuthor("甲第灵光 ArchLight")
        required = [_draw_sheet(pdf, data, sheet, index, len(physical))
                    for index, sheet in enumerate(physical, 1)]
        pdf.save()
        characters = set("".join(value for page in required for value in page))
        if any(0x3400 <= ord(char) <= 0x9fff and not registered_font.face.charToGlyph.get(ord(char))
               for char in characters):
            raise ValueError("selected font lacks a required Chinese glyph")
        _verify_canonical(canonical, physical, required)
        _rasterize_and_package(canonical, image_pdf, images, len(physical))
        with fitz.open(image_pdf) as final:
            final.set_metadata({"title": "甲第灵光 · 家庭建房规划报告", "author": "甲第灵光 ArchLight",
                                "subject": "personalized whole-house concept report",
                                "creator": "house-report-v1", "producer": "house-report-v1"})
            final.saveIncr()
        if canonical_output is not None:
            target = Path(canonical_output)
            target.parent.mkdir(parents=True, exist_ok=True)
            os.replace(canonical, target)
        if page_images_dir is not None:
            target = Path(page_images_dir)
            target.mkdir(parents=True, exist_ok=True)
            for old in target.glob("page-*.png"):
                old.unlink()
            for image in images.glob("page-*.png"):
                os.replace(image, target / image.name)
        os.replace(image_pdf, output)
        return {"logical_pages": len(data["pages"]), "physical_pages": len(physical)}
    finally:
        shutil.rmtree(temp, ignore_errors=True)
