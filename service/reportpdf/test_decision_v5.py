"""Contract tests for the facts-driven V5 decision report renderer."""
import copy
import json
import os
from pathlib import Path
import tempfile
import unittest

import fitz
from PIL import Image
from pypdf import PdfReader

from decision_v5 import FONT_SIZES, render_decision_v5, validate_decision_v5
from render import render_report


FONT = Path(os.environ.get(
    "REPORT_PDF_FONT",
    "/System/Library/Fonts/Supplemental/Arial Unicode.ttf",
))


def decision_snapshot():
    pages = [
        {
            "number": 1, "access": "paid", "kind": "recommendation",
            "title": "我家的建房决策报告", "subtitle": "先把一家人的生活想清楚，再决定房子怎么建。",
            "blocks": [{"title": "本次最值得先决定", "body": "固定客房与多功能房之间如何取舍。"}],
            "priorities": [], "table": [], "actions": [],
            "note": "泉州南安｜报告编号 JF20260920-001", "disclaimer": "前期决策参考，不代替专业设计。",
        },
        {
            "number": 2, "access": "paid", "kind": "facts",
            "title": "我们怎样理解你家的情况", "subtitle": "以下判断只使用已提交事实，未知项继续保留。",
            "blocks": [
                {"title": "家庭使用", "body": "父母2人常住，子女一家4人节假日返乡，最多6人团聚。"},
                {"title": "地块与预算", "body": "宅基地约10×15米，可建边界未知；计划100万元，庭院和设备是否包含尚不明确。"},
            ],
            "priorities": [], "table": [], "actions": [], "note": "已填写不等于已核实。", "disclaimer": "",
        },
        {
            "number": 3, "access": "paid", "kind": "criteria",
            "title": "真正的矛盾不在房间数量", "subtitle": "日常两人居住与节日六人团聚，需要不同的空间效率。",
            "blocks": [{"title": "核心诊断", "body": "如果为少量返乡天数长期保留多间空置客房，会挤压父母日常活动与收纳空间。"}],
            "priorities": [
                {"title": "父母舒适", "body": "保证一层老人房和日常起居便利。"},
                {"title": "团聚体验", "body": "返乡时仍能提供有尊严的临时居住。"},
                {"title": "预算可控", "body": "把长期投入放在每天真正使用的空间。"},
            ],
            "table": [], "actions": [], "note": "", "disclaimer": "",
        },
        {
            "number": 4, "access": "paid", "kind": "analysis_primary",
            "title": "请确认你更愿意接受哪种取舍", "subtitle": "确认后，完整报告会按你的选择继续深化。",
            "blocks": [
                {"title": "方向A｜固定客房", "body": "返乡即住，但日常空置更多，建造与维护投入更高。"},
                {"title": "方向B｜一间多功能房", "body": "日常作为书房或活动室，返乡时转换为卧室。"},
            ],
            "priorities": [], "table": [], "actions": [], "note": "建议优先比较使用频率与转换成本。", "disclaimer": "",
        },
        {
            "number": 5, "access": "paid", "kind": "analysis_secondary",
            "title": "更适合你家的空间方向", "subtitle": "依据已确认的优先级给出带条件的建议。",
            "blocks": [
                {"title": "推荐结论", "body": "保留一间固定客房，并设置一间可转换的多功能房。"},
                {"title": "成立条件", "body": "家庭接受节日临时转换，并提前配置收纳和睡眠家具。"},
            ],
            "priorities": [], "table": [], "actions": [], "note": "建议仍需结合可建边界深化。", "disclaimer": "",
        },
        {
            "number": 6, "access": "paid", "kind": "decision_table",
            "title": "两种方向的具体比较", "subtitle": "把获得什么和需要接受什么放在同一张表中。",
            "blocks": [], "priorities": [],
            "table": [
                ["比较项", "固定客房", "多功能房"],
                ["日常利用", "较低", "较高"],
                ["返乡准备", "无需转换", "需要提前整理"],
                ["投入影响", "面积与维护投入较高", "家具与收纳设计要求更高"],
            ],
            "actions": [], "note": "比较结论来自家庭使用频率与优先级。", "disclaimer": "",
        },
        {
            "number": 7, "access": "paid", "kind": "budget_scope",
            "title": "先统一100万元的预算口径", "subtitle": "范围没有说清之前，任何单价比较都可能失真。",
            "blocks": [
                {"title": "必须先确认", "body": "室内装修、庭院、设备、报建与临时设施是否计入100万元。"},
                {"title": "优先保留", "body": "结构安全、一层适老、基础机电和高频使用空间。"},
            ],
            "priorities": [], "table": [], "actions": [], "note": "本页不是工程报价。", "disclaimer": "",
        },
        {
            "number": 8, "access": "paid", "kind": "designer_brief",
            "title": "给设计师的任务书", "subtitle": "把家庭语言转成下一阶段可验证的问题。",
            "blocks": [
                {"title": "必须满足", "body": "一层老人房；两人日常动线简洁；六人团聚时具备临时睡眠能力。"},
                {"title": "需要验证", "body": "可建边界、采光通风、楼梯位置，以及多功能房转换后的私密性。"},
            ],
            "priorities": [], "table": [], "actions": [], "note": "风格偏好为新中式，仅作为沟通方向。", "disclaimer": "",
        },
        {
            "number": 9, "access": "paid", "kind": "action_plan",
            "title": "同口径询价清单", "subtitle": "让不同供应方在同一范围内报价。",
            "blocks": [], "priorities": [], "table": [],
            "actions": [
                {"title": "确认范围", "body": "逐项标明主体、装修、庭院、设备和报建是否包含。"},
                {"title": "确认做法", "body": "要求写清结构、门窗、外墙、机电的材料与规格。"},
                {"title": "确认变更", "body": "约定暂定项、增减项与现场签证的计算方式。"},
            ],
            "note": "只比较总价，无法判断差异来自哪里。", "disclaimer": "",
        },
        {
            "number": 10, "access": "paid", "kind": "confirmation_sheet",
            "title": "接下来30天怎么推进", "subtitle": "先补最影响判断的资料，再进入方案深化。",
            "blocks": [], "priorities": [], "table": [],
            "actions": [
                {"title": "第1周", "body": "确认可建边界与审批条件。"},
                {"title": "第2周", "body": "家庭确认客房与多功能房取舍。"},
                {"title": "第3周", "body": "带任务书与统一清单接洽设计和施工方。"},
                {"title": "第4周", "body": "核对首轮方案是否回应家庭优先级。"},
            ],
            "note": "报告中心会保留本报告与后续版本。",
            "disclaimer": "本报告为建房前期决策参考，不代替测绘、审批、建筑与结构施工图、工程量清单和正式报价。",
        },
    ]
    return {
        "meta": {
            "schema_version": "report-decision-v5",
            "source": "report-orchestrator-v5",
            "report_id": "JF20260920-001",
            "input_hash": "a" * 64,
            "date": "2026年09月20日",
            "generation_mode": "full",
            "protocol_version": "report-decision-v5",
        },
        "facts": {
            "region": "泉州南安", "plot": "约10×15米，可建边界未知",
            "family": "父母2人常住，子女一家4人节假日返乡",
            "space": "拟建2层、4间卧室、一层老人房", "style": "新中式",
            "budget": "计划100万元，庭院与设备范围待确认",
            "timeline": "计划一年内开工", "priority": "父母舒适、团聚体验、预算可控",
        },
        "recipe": "space",
        "diagnosis": {
            "id": "diagnosis-1", "recipe": "space",
            "primary_issue": "低频返乡需求与高频日常空间之间的取舍",
            "topic": "日常居住与节日团聚的空间取舍",
            "reason": "两种使用状态的频率差异明显。",
            "evidence": ["父母2人常住", "节日最多6人团聚", "预算范围尚未统一"],
            "evidence_refs": ["family.permanent_count", "family.returning_count"],
            "options": [{"title": "固定客房", "body": "返乡即住。"}],
            "unknowns": ["可建边界"], "question": "是否接受房间兼用？",
        },
        "confirmation": {
            "diagnosis_id": "diagnosis-1", "input_hash": "a" * 64,
            "choice": "flexible", "priorities": ["父母舒适", "团聚体验", "预算可控"],
        },
        "pages": pages,
    }


def preview_snapshot():
    data = decision_snapshot()
    data["meta"]["generation_mode"] = "preview"
    data.pop("confirmation")
    preview_kinds = ("decision", "evidence", "directions", "confirmation")
    data["pages"] = data["pages"][:4]
    for page, kind in zip(data["pages"], preview_kinds):
        page["access"] = "free"
        page["kind"] = kind
    return data


class DecisionV5Tests(unittest.TestCase):
    def setUp(self):
        self.data = decision_snapshot()
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.output = self.root / "report.pdf"
        self.canonical = self.root / "canonical.pdf"
        self.images = self.root / "pages"

    def render(self, mode="full", data=None, asset_root=None):
        return render_decision_v5(
            data or self.data, self.output, mode, FONT,
            canonical_output=self.canonical,
            page_images_dir=self.images,
            asset_root=asset_root,
        )

    def make_image(self, name, size=(1200, 800), color=(191, 165, 119)):
        asset_root = self.root / "assets"
        asset_root.mkdir(exist_ok=True)
        path = asset_root / name
        Image.new("RGB", size, color).save(path)
        return asset_root, path

    def test_full_report_keeps_searchable_canonical_and_delivers_ten_image_pages(self):
        self.render()
        canonical = PdfReader(self.canonical)
        final = PdfReader(self.output)
        self.assertEqual(len(canonical.pages), 10)
        self.assertEqual(len(final.pages), 10)
        text = "\n".join(page.extract_text() or "" for page in canonical.pages)
        self.assertIn("JF20260920-001", text)
        self.assertIn("2026年09月20日", text)
        self.assertIn("同口径询价清单", text)
        self.assertIn("10 / 10", text)
        self.assertEqual(len(list(self.images.glob("page-*.png"))), 10)
        self.assertTrue(all(not (page.extract_text() or "").strip() for page in final.pages))
        with fitz.open(self.output) as document:
            self.assertTrue(all(len(page.get_images(full=True)) == 1 for page in document))

    def test_preview_is_exactly_first_four_pages(self):
        self.render("preview", preview_snapshot())
        self.assertEqual(len(PdfReader(self.canonical).pages), 4)
        self.assertEqual(len(PdfReader(self.output).pages), 4)
        self.assertEqual(len(list(self.images.glob("page-*.png"))), 4)

    def test_schema_source_hash_page_count_order_and_access_are_enforced(self):
        mutations = (
            lambda d: d["meta"].__setitem__("schema_version", "report-a4-v1"),
            lambda d: d["meta"].__setitem__("source", "report-service-v1"),
            lambda d: d["meta"].__setitem__("input_hash", "bad"),
            lambda d: d["meta"].__setitem__("protocol_version", "report-a4-v1"),
            lambda d: d.__setitem__("recipe", "unknown"),
            lambda d: d["pages"].pop(),
            lambda d: d["pages"][4].__setitem__("number", 99),
            lambda d: d["pages"][4].__setitem__("access", "free"),
        )
        for mutate in mutations:
            with self.subTest(mutation=mutate):
                data = copy.deepcopy(self.data)
                mutate(data)
                with self.assertRaises(ValueError):
                    validate_decision_v5(data, "full")

    def test_diagnosis_and_confirmation_accept_structured_metadata(self):
        self.data["diagnosis"]["requires_follow_up"] = True
        self.data["confirmation"]["revision"] = 2
        validate_decision_v5(self.data, "full")

    def test_preview_and_full_are_separate_page_contracts(self):
        preview = preview_snapshot()
        validate_decision_v5(preview, "preview")
        with self.assertRaises(ValueError):
            validate_decision_v5(preview, "full")
        with self.assertRaises(ValueError):
            validate_decision_v5(self.data, "preview")

    def test_all_copy_must_be_plain_strings(self):
        self.data["pages"][1]["blocks"][0]["body"] = {"unsafe": "value"}
        with self.assertRaises(ValueError):
            validate_decision_v5(self.data, "full")
        self.data = decision_snapshot()
        self.data["facts"]["region"] = ["泉州南安"]
        with self.assertRaises(ValueError):
            validate_decision_v5(self.data, "full")

    def test_overflow_is_rejected_without_replacing_previous_pdf(self):
        self.render()
        original = self.output.read_bytes()
        self.data["pages"][1]["blocks"][0]["body"] = "过长内容" * 1000
        with self.assertRaisesRegex(ValueError, "exceeds"):
            self.render()
        self.assertEqual(self.output.read_bytes(), original)

    def test_ten_short_fact_blocks_use_a_bounded_two_column_layout(self):
        self.data["pages"][1]["blocks"] = [
            {"title": f"fact.{index:02d}", "body": f"已确认事实 {index}"}
            for index in range(10)
        ]
        self.render()
        text = "\n".join(page.extract_text() or "" for page in PdfReader(self.canonical).pages)
        self.assertIn("fact.09", text)

    def test_all_renderer_font_sizes_are_positive(self):
        self.assertTrue(FONT_SIZES)
        self.assertTrue(all(size > 0 for size in FONT_SIZES.values()))

    def test_same_snapshot_produces_identical_image_pdf(self):
        self.render()
        original = self.output.read_bytes()
        self.render()
        self.assertEqual(self.output.read_bytes(), original)

    def test_long_report_ids_are_abbreviated_without_mutating_snapshot(self):
        report_ids = (
            "123e4567-e89b-12d3-a456-426614174000",
            "a" * 64,
        )
        for report_id in report_ids:
            with self.subTest(report_id=report_id):
                data = decision_snapshot()
                data["meta"]["report_id"] = report_id
                self.render(data=data)
                text = "\n".join(
                    page.extract_text() or "" for page in PdfReader(self.canonical).pages
                )
                self.assertEqual(data["meta"]["report_id"], report_id)
                self.assertIn(report_id[:8] + "…" + report_id[-8:], text)
                self.assertIn("01 / 10", text)

    def test_two_controlled_illustrations_render_in_complete_ten_page_report(self):
        asset_root, _ = self.make_image("courtyard.png", size=(1672, 941))
        self.make_image("living-room.jpg", size=(1672, 941), color=(85, 113, 102))
        self.data["pages"][4]["illustrations"] = [
            {
                "asset": "courtyard.png",
                "title": "院落与室内关系示意",
                "caption": "用于说明归家、停留与室内公共空间之间的关系。",
            },
            {
                "asset": "living-room.jpg",
                "title": "家庭共享空间示意",
                "caption": "画面只表达生活场景，不代表已经确定的建筑方案。",
            },
        ]
        self.render(asset_root=asset_root)
        self.assertEqual(len(PdfReader(self.output).pages), 10)
        canonical_text = "\n".join(
            page.extract_text() or "" for page in PdfReader(self.canonical).pages
        )
        self.assertIn("院落与室内关系示意", canonical_text)
        self.assertIn("画面只表达生活场景", canonical_text)
        with fitz.open(self.canonical) as document:
            page = document[4]
            images = sorted(page.get_image_info(), key=lambda item: item["bbox"][0])
            self.assertEqual(len(images), 2)
            blocks = page.get_text("blocks")
            for image, title in zip(images, ("院落与室内关系示意", "家庭共享空间示意")):
                title_top = next(block[1] for block in blocks if title in block[4])
                self.assertLess(title_top - image["bbox"][3], 25)

    def test_cover_with_two_short_blocks_and_one_landscape_illustration_fits(self):
        asset_root, _ = self.make_image("cover.png", size=(1672, 941))
        self.data["pages"][0]["blocks"].append({
            "title": "阅读边界",
            "body": "需要测绘、审批和设计核实的内容已单独标出。",
        })
        self.data["pages"][0]["illustrations"] = [{
            "asset": "cover.png",
            "title": "家庭生活场景示意",
            "caption": "用于建立阅读情境，不代表已确定建筑方案。",
        }]
        self.data["pages"][0]["note"] = ""
        self.data["pages"][0]["disclaimer"] = ""
        self.render(asset_root=asset_root)
        with fitz.open(self.canonical) as document:
            self.assertEqual(len(document[0].get_image_info()), 1)

    def test_illustrations_reject_missing_unsafe_damaged_and_oversized_assets(self):
        asset_root, _ = self.make_image("valid.png")
        outside = self.root / "outside.png"
        Image.new("RGB", (40, 40), (1, 2, 3)).save(outside)
        (asset_root / "escape.png").symlink_to(outside)
        (asset_root / "damaged.png").write_bytes(b"not an image")
        self.make_image("oversized.png", size=(9000, 1))
        cases = (
            ("missing.png", asset_root),
            (str(outside), asset_root),
            ("../outside.png", asset_root),
            ("escape.png", asset_root),
            ("damaged.png", asset_root),
            ("oversized.png", asset_root),
            ("valid.png", None),
        )
        for asset, root in cases:
            with self.subTest(asset=asset, asset_root=root):
                data = decision_snapshot()
                data["pages"][4]["illustrations"] = [{
                    "asset": asset,
                    "title": "受控示意图",
                    "caption": "仅用于验证图片资产读取边界。",
                }]
                with self.assertRaises(ValueError):
                    render_decision_v5(data, self.output, "full", FONT, asset_root=root)
        too_many = decision_snapshot()
        illustration = {
            "asset": "valid.png", "title": "受控示意图", "caption": "验证数量上限。",
        }
        too_many["pages"][4]["illustrations"] = [illustration] * 3
        with self.assertRaises(ValueError):
            render_decision_v5(
                too_many, self.output, "full", FONT, asset_root=asset_root
            )

    def test_illustration_overflow_is_rejected_without_replacing_previous_pdf(self):
        self.render()
        original = self.output.read_bytes()
        asset_root, _ = self.make_image("overflow.png")
        data = decision_snapshot()
        data["pages"][9]["illustrations"] = [{
            "asset": "overflow.png",
            "title": "空间不足时不得裁切",
            "caption": "完整的图片、标题和说明必须同时容纳在页面内。",
        }]
        with self.assertRaisesRegex(ValueError, "exceeds page"):
            self.render(data=data, asset_root=asset_root)
        self.assertEqual(self.output.read_bytes(), original)

    def test_existing_render_entry_dispatches_v5_without_changing_cli_contract(self):
        asset_root, _ = self.make_image("dispatch.png")
        self.data["pages"][4]["illustrations"] = [{
            "asset": "dispatch.png",
            "title": "入口分发示意",
            "caption": "验证统一渲染入口会传递受控资产目录。",
        }]
        render_report(self.data, self.output, "full", FONT, asset_root=asset_root)
        self.assertEqual(len(PdfReader(self.output).pages), 10)

    def test_real_go_snapshots_for_every_recipe_render_preview_and_full(self):
        fixtures = Path(__file__).parents[1] / "report" / "testdata" / "v5-demo"
        for recipe in ("space", "budget", "family", "readiness"):
            for mode, page_count in (("preview", 4), ("full", 10)):
                with self.subTest(recipe=recipe, mode=mode):
                    snapshot = json.loads(
                        (fixtures / f"{recipe}-{mode}.json").read_text(encoding="utf-8")
                    )
                    output = self.root / f"{recipe}-{mode}.pdf"
                    canonical = self.root / f"{recipe}-{mode}-canonical.pdf"
                    render_decision_v5(
                        snapshot,
                        output,
                        mode,
                        FONT,
                        canonical_output=canonical,
                    )
                    self.assertEqual(len(PdfReader(output).pages), page_count)
                    self.assertEqual(len(PdfReader(canonical).pages), page_count)


if __name__ == "__main__":
    unittest.main()
