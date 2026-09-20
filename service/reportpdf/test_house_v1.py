"""Contract and rendered-artifact tests for variable whole-house reports."""
import copy
import hashlib
import json
import os
from pathlib import Path
import tempfile
import unittest

import fitz
from PIL import Image
from pypdf import PdfReader

from house_v1 import render_house_v1, validate_house_v1
from render import render_report


FONT = Path(os.environ.get(
    "REPORT_PDF_FONT", "/System/Library/Fonts/Supplemental/Arial Unicode.ttf"))


def house_snapshot(asset_root, floors=2, scheme_count=2):
    """Synthetic local fixture; the colored images are test assets, not designs."""
    root = Path(asset_root)
    root.mkdir(parents=True, exist_ok=True)
    data = {
        "meta": {
            "schema_version": "house-report-v1", "protocol_version": "report-house-v1",
            "source": "house-agent-v1", "report_id": "HOUSE-TEST-001",
            "input_hash": "a" * 64, "date": "2026年09月20日",
            "generation_mode": "multi_agent", "demonstration": False,
        },
        "facts": {"intent.floors": str(floors), "intent.bedrooms": str(floors * 2),
                  "site.region": "泉州南安", "intent.priorities": "父母舒适 / 团聚便利"},
        "recipe": "space",
        "diagnosis": {
            "id": "diagnosis-house-test", "recipe": "space", "topic": "整栋房子的居住与团聚安排",
            "reason": "父母日常起居与返乡团聚需要统筹安排。", "question": "先确认家庭共同底线。",
            "evidence_refs": ["intent.floors"], "unknowns": ["可建边界待核实"],
            "options": [{"title": "日常舒适", "body": "优先照顾常住生活。"}],
        },
        "confirmation": {"diagnosis_id": "diagnosis-house-test", "input_hash": "a" * 64,
                         "choice": "confirm", "priorities": ["父母舒适", "团聚便利"]},
        "pages": [],
        "house": {"plan": {"scheme_count": scheme_count, "focus": ["父母舒适", "空间组织"]},
                  "schemes": [], "images": [], "comparison": [], "budget": [],
                  "designer_brief": [], "actions": []},
    }

    def page(kind, title, blocks=None, **extra):
        value = {"number": len(data["pages"]) + 1, "access": "paid", "kind": kind,
                 "title": title, "subtitle": "依据已确认的家庭需求，保留待核实的条件。",
                 "sources": ["intent.floors"], "blocks": blocks or []}
        value.update(extra)
        data["pages"].append(value)

    for kind, title in zip(
            ("house_profile", "house_constraints", "house_goals", "house_confirmation"),
            ("我家的建房规划报告", "家庭与地块的条件", "最重要的设计目标", "确认本次规划方向")):
        page(kind, title, [{"title": "家庭输入", "body": "父母常住，子女节假日返乡。"}])
    page("house_overview", "整屋概念方案总览")
    for index in range(scheme_count):
        scheme_id = chr(ord("A") + index)
        scheme = {
            "id": scheme_id, "version": "v1", "name": "日常舒适" if index == 0 else "团聚共享",
            "strategy": "围绕不同生活优先级安排整屋空间。", "gain": "日常使用更清晰。",
            "tradeoff": "需要接受公共空间与私密空间的不同取舍。",
            "facade": "简洁体量与温润材料表达。", "cost_sensitive": ["屋面与外墙做法"],
            "assumptions": ["实际可建边界待确认"], "evidence_refs": ["intent.floors"], "floors": [],
        }
        data["house"]["schemes"].append(scheme)
        data["pages"][4]["blocks"].append({"title": scheme["name"], "body": scheme["strategy"]})

        def asset(kind, floor=0, reference=""):
            asset_id = f"{scheme_id}-{kind}-{floor}" if floor else f"{scheme_id}-{kind}"
            name = asset_id + ".png"
            path = root / name
            Image.new("RGB", (2048, 1152), (160 + index * 20, 175, 150 + floor * 15)).save(path)
            value = {"id": asset_id, "asset": name, "scheme_id": scheme_id,
                     "scheme_version": "v1", "kind": kind, "width": 2048, "height": 1152,
                     "model": "test-image-model", "sha256": hashlib.sha256(path.read_bytes()).hexdigest()}
            if floor:
                value["floor"] = floor
            if reference:
                value["reference_id"] = reference
            data["house"]["images"].append(value)
            return value

        for number in range(1, floors + 1):
            floor = {"number": number, "name": f"第{number}层", "summary": "卧室与公共空间按生活方式组织。",
                     "rooms": [{"name": "卧室", "kind": "bedroom", "count": 2}],
                     "circulation": "楼梯连接公共活动与私密居住区。", "stair_position": "建筑中部"}
            scheme["floors"].append(floor)
            image = asset("floor_plan", number)
            page("house_floor_plan", f"方案{scheme_id} · 第{number}层空间", [
                {"title": "空间组织", "body": floor["summary"]},
                {"title": "房间与动线", "body": "两间卧室；" + floor["circulation"]},
                {"title": "楼梯位置", "body": "建筑中部"}], scheme=scheme_id,
                disclaimer="本报告中的平面、体量和外观均为前期概念表达，不是施工图、审批图或精确工程测量结果；需由具备资质的专业人员结合实测和当地规范复核。",
                illustrations=[{"asset": image["asset"], "title": f"方案{scheme_id}第{number}层概念平面",
                                "caption": "AI概念示意，仅供方案沟通；尺寸与结构须由设计师核实。"}])
        volume = asset("massing", reference=f"{scheme_id}-floor_plan-1")
        facade = asset("exterior", reference=volume["id"])
        page("house_exterior", f"方案{scheme_id} · 外观与体量", scheme=scheme_id,
             blocks=[{"title": "方案收益", "body": scheme["gain"]},
                     {"title": "方案代价", "body": scheme["tradeoff"]},
                     {"title": "成本敏感项", "body": "屋面与外墙做法"}],
             illustrations=[{"asset": item["asset"], "title": title,
                             "caption": "依据同一方案生成的概念意向，需结合现场条件深化。"}
                            for item, title in ((volume, "建筑体量"), (facade, "正立面意向"))])
    comparison = [["比较维度"] + [scheme["name"] for scheme in data["house"]["schemes"]],
                  ["日常生活"] + ["按常住需求核实动线"] * scheme_count]
    data["house"]["comparison"] = comparison
    page("house_comparison", "整屋方案比较", table=comparison)
    for key, kind, title in (("budget", "house_budget", "预算范围与风险"),
                             ("designer_brief", "house_designer_brief", "给设计师的需求任务书")):
        blocks = [{"title": "需要落实", "body": "核实可建边界与家庭优先级，再统一询价范围。"}]
        data["house"][key] = blocks
        page(kind, title, blocks)
    action = {"task": "核实地块边界", "owner": "家庭与测绘人员", "prerequisite": "准备权属资料",
              "evidence": "可核对的测绘文件"}
    data["house"]["actions"] = [action]
    page("house_actions", "下一步行动", actions=[{
        "title": action["task"], "body": "；".join(action[key] for key in ("owner", "prerequisite", "evidence"))}])
    return data


class HouseReportTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.assets = self.root / "assets"
        self.data = house_snapshot(self.assets)
        self.output = self.root / "house.pdf"
        self.canonical = self.root / "canonical.pdf"

    def render(self, data=None, mode="full"):
        return render_house_v1(data or self.data, self.output, mode, FONT,
                               canonical_output=self.canonical, asset_root=self.assets)

    def test_variable_scheme_and_floor_counts_determine_logical_pages(self):
        self.assertEqual(validate_house_v1(self.data, "full", self.assets), 15)
        three = house_snapshot(self.assets, floors=3)
        self.assertEqual(validate_house_v1(three, "full", self.assets), 17)
        three_schemes = house_snapshot(self.assets, floors=3, scheme_count=3)
        self.assertEqual(validate_house_v1(three_schemes, "full", self.assets), 21)
        self.render(three)
        self.assertEqual(len(PdfReader(self.output).pages), 17)

    def test_complete_pages_are_images_with_readable_uncropped_floor_plan(self):
        self.render()
        with fitz.open(self.output) as final:
            self.assertEqual(final.page_count, 15)
            for page in final:
                self.assertEqual(len(page.get_images(full=True)), 1)
                self.assertFalse(page.get_text().strip())
        with fitz.open(self.canonical) as canonical:
            image = canonical[5].get_image_info()[0]
            box = fitz.Rect(image["bbox"])
            self.assertGreater(box.width, 460)
            self.assertAlmostEqual(box.width / box.height, 2048 / 1152, places=3)
            self.assertIn("空间组织", canonical[5].get_text())
        resources = PdfReader(self.canonical).pages[0]["/Resources"]
        self.assertTrue(resources.get("/Font"))

    def test_long_text_continues_without_truncation(self):
        marker = "最后仍需确认排水及采光边界"
        body = ("父母日常起居需要保证卧室与卫生间之间方便通行，团聚时还需考虑私密性。" * 100) + marker
        self.data["pages"][-2]["blocks"][0]["body"] = body
        self.render()
        canonical = PdfReader(self.canonical)
        self.assertGreater(len(canonical.pages), len(self.data["pages"]))
        text = "".join(page.extract_text() or "" for page in canonical.pages).replace("\n", "")
        self.assertIn(marker, text)
        with fitz.open(self.canonical) as document:
            body_text = "".join(span["text"] for page in document
                                for block in page.get_text("dict")["blocks"] if block["type"] == 0
                                for line in block["lines"] for span in line["spans"]
                                if abs(span["size"] - 10) < .01)
        self.assertEqual(body_text.count("父母日常起居需要保证"), 100)

    def test_preview_requires_no_assets_or_house_payload(self):
        preview = copy.deepcopy(self.data)
        preview.pop("house")
        preview.pop("confirmation")
        preview["pages"] = preview["pages"][:4]
        for page in preview["pages"]:
            page["access"] = "free"
        render_house_v1(preview, self.output, "preview", FONT)
        self.assertEqual(len(PdfReader(self.output).pages), 4)

    def test_missing_required_chinese_glyphs_are_rejected(self):
        latin_font = Path("/System/Library/Fonts/Supplemental/Arial.ttf")
        if not latin_font.is_file():
            self.skipTest("Latin-only test font is unavailable")
        with self.assertRaisesRegex(ValueError, "Chinese glyph"):
            render_house_v1(self.data, self.output, "full", latin_font, asset_root=self.assets)

    def test_wrong_bedroom_total_and_illegal_confirmation_are_rejected(self):
        for key, value in (("intent.bedrooms", "9"), ("intent.floors", "5")):
            data = copy.deepcopy(self.data)
            data["facts"][key] = value
            with self.subTest(fact=key), self.assertRaises(ValueError):
                validate_house_v1(data, "full", self.assets)
        self.data["confirmation"]["choice"] = "invented"
        with self.assertRaises(ValueError):
            validate_house_v1(self.data, "full", self.assets)

    def test_go_generated_snapshot_fixtures_render_with_real_assets(self):
        for floors, count in ((2, 15), (3, 17)):
            fixture = Path(__file__).parent / "testdata" / "house-v1" / f"{floors}-floor.json"
            data = json.loads(fixture.read_text(encoding="utf-8"))
            for image in data["house"]["images"]:
                path = self.assets / image["asset"]
                path.parent.mkdir(parents=True, exist_ok=True)
                Image.new("RGB", (image["width"], image["height"]), (174, 187, 169)).save(path)
                image["sha256"] = hashlib.sha256(path.read_bytes()).hexdigest()
            with self.subTest(floors=floors):
                self.assertEqual(validate_house_v1(data, "full", self.assets), count)
                self.render(data)
                self.assertEqual(len(PdfReader(self.output).pages), count)

    def test_empty_section_and_inconsistent_comparison_are_rejected(self):
        data = copy.deepcopy(self.data)
        data["pages"][5]["blocks"] = []
        with self.assertRaises(ValueError):
            validate_house_v1(data, "full", self.assets)
        data = copy.deepcopy(self.data)
        data["pages"][-4]["table"] = [["错误", "另一个方案"]]
        with self.assertRaises(ValueError):
            validate_house_v1(data, "full", self.assets)

    def test_missing_wrong_scheme_and_bad_reference_assets_are_rejected(self):
        changes = [
            lambda d: d["house"]["images"].pop(),
            lambda d: d["house"]["images"][0].update(scheme_id="B"),
            lambda d: d["house"]["images"][0].update(scheme_version="other"),
            lambda d: d["house"]["images"][2].update(reference_id="B-floor_plan-1"),
            lambda d: d["pages"][5]["illustrations"][0].update(asset="B-floor_plan-1.png"),
            lambda d: d["house"]["images"][0].update(sha256="b" * 64),
            lambda d: d["house"]["images"][0].update(width=1024),
        ]
        for mutate in changes:
            data = copy.deepcopy(self.data)
            mutate(data)
            with self.subTest(mutation=mutate), self.assertRaises(ValueError):
                validate_house_v1(data, "full", self.assets)

    def test_asset_traversal_and_symlink_escape_are_rejected(self):
        outside = self.root / "outside.png"
        Image.new("RGB", (2048, 1152), "white").save(outside)
        (self.assets / "escape.png").symlink_to(outside)
        for name in ("../outside.png", str(outside), "escape.png"):
            data = copy.deepcopy(self.data)
            data["house"]["images"][0]["asset"] = name
            data["pages"][5]["illustrations"][0]["asset"] = name
            with self.subTest(asset=name), self.assertRaises(ValueError):
                validate_house_v1(data, "full", self.assets)

    def test_failed_render_preserves_previous_output(self):
        self.output.write_bytes(b"previous-valid-artifact")
        (self.assets / self.data["house"]["images"][0]["asset"]).unlink()
        with self.assertRaises(ValueError):
            self.render()
        self.assertEqual(self.output.read_bytes(), b"previous-valid-artifact")

    def test_main_renderer_dispatches_house_protocol(self):
        render_report(self.data, self.output, "full", FONT, self.assets)
        self.assertEqual(len(PdfReader(self.output).pages), 15)


if __name__ == "__main__":
    unittest.main()
