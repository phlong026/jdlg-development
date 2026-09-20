"""Build the explicitly synthetic, illustrated V5 sample; never a customer worker.

AI originals are supplied separately. Diagrams contain only this fixture's facts
or clearly labelled recommendations, and the frozen input is never rewritten.
"""
import argparse
import copy
import io
import json
from pathlib import Path

import fitz
from reportlab.lib.colors import HexColor
from reportlab.pdfbase import pdfmetrics
from reportlab.pdfbase.ttfonts import TTFont
from reportlab.pdfgen import canvas

from decision_v5 import render_decision_v5


HERE = Path(__file__).resolve().parent
SOURCE = HERE.parent / "report/testdata/v5-demo/space-full.json"


def block(title, body):
    return {"title": title, "body": body}


def illustration(asset, title, caption):
    return {"asset": asset, "title": title, "caption": caption}


def illustrated_snapshot(source):
    baseline = json.loads(SOURCE.read_text(encoding="utf-8"))
    if any(source.get(key) != baseline[key] for key in ("facts", "diagnosis", "confirmation")):
        raise ValueError("This authored demonstration only supports its original synthetic case")
    data = copy.deepcopy(source)
    data["meta"]["demonstration"] = True
    data["meta"]["generation_mode"] = "illustrated_demonstration"
    pages = data["pages"]

    pages[0].update(
        title="我家的建房决策报告",
        subtitle="先照顾父母的日常，再安排一家人的团聚。",
        illustrations=[illustration("exterior.png", "家的外观意向", "AI 意向图；外观仅供沟通，入口高差需另行适老化设计。")],
        blocks=[block("承接你的选择：优先深化可转换房间", "父母两人常住、子女四人节日返乡。你已选择方向 B：让房间平时有用，返乡时再转为睡眠空间。")],
        note="演示案例 · 沿用四页预览的家庭事实与确认结果。插图不是已确定的建筑方案。",
    )
    pages[1].update(
        title="这份建议从你家的情况出发",
        subtitle="已填写的事实保持不变，未核实的条件继续保留。",
        note="地块尺寸来自估算，不能直接当作建筑占地；四间卧室是意向，需验证固定与兼用的具体组合。",
    )
    pages[2].update(
        title="先满足每天发生的生活",
        subtitle="确认顺序：父母居住舒适 → 节日团聚 → 投入可控。",
        illustrations=[illustration("elder-room.png", "父母日常生活的空间意向", "建议验证少绕行、少高差、夜间照明与床边通行；图片不表示已满足规范。")],
        priorities=[
            block("父母舒适", "优先验证日常起居与卧室的通行便利，避免把好用空间都留给偶尔返乡。"),
            block("团聚与投入", "返乡时要能休息、有隐私；兼用家具及长期维护也要纳入同口径询价。"),
        ],
        note="一层安置父母卧室是待验证建议，不是已确定的楼层平面。",
    )
    pages[3].update(
        title="方向 A：保留固定客房",
        subtitle="作为对照：返乡即可入住，但要接受平时的使用方式。",
        illustrations=[illustration("fixed-guest.png", "固定客房意向", "常设床具与独立收纳，减少返乡前的布置工作；不是新增卧室数量。")],
        blocks=[
            block("得到什么", "睡眠和收纳始终就位，适合重视随时入住、少做转换的家庭。"),
            block("需要接受什么", "床具会长期占用空间。是否值得，要结合真实返乡频率、日常用途与维护投入判断。"),
        ],
        note="你当前选择方向 B；保留 A 是为了看清取舍，不代表要求你重新购买或重新选择。",
    )
    pages[4].update(
        title="方向 B：同一房间，两种状态",
        subtitle="这是你选择继续深化的方向：日常使用，返乡转换。",
        illustrations=[
            illustration("flex-day.png", "平时：阅读与日常活动", "收起床具，保留阅读、收纳和活动空间。"),
            illustration("flex-night.png", "返乡：临时睡眠", "展开床具，核对通行、遮光、收纳和隐私。"),
        ],
        blocks=[
            block("成立条件", "家庭接受转换，并明确谁整理、床品放哪里、展开后门窗能否正常使用。"),
            block("图中还不能证明的事", "家具尺寸、结构固定和展开后的通行需实测；一张房间图不能证明四位返乡家人的全部床位。"),
        ],
        note="两张 AI 图用于表达同一空间的使用变化。最终床位分配仍需结合四卧意向逐项核对。",
    )
    pages[5].update(
        title="把两种方向放在同一张表里",
        subtitle="选择 B 的理由是使用方式更匹配，不是承诺它一定更便宜。",
        blocks=[block("本次条件化建议", "先深化方向 B；若家庭不接受整理转换，或实测后无法兼顾隐私与通行，再回到固定客房方向。")],
        table=[
            ["比较项", "A：固定客房", "B：可转换房间"],
            ["平时用途", "床具长期保留", "可兼作阅读、收纳或活动"],
            ["返乡准备", "床位常备", "需要布置与整理"],
            ["隐私", "核对独立出入口与隔音", "转换后同样需要核对"],
            ["投入", "核对房间及维护投入", "核对家具、五金及维护"],
            ["最终验证", "实测尺寸和生活需求", "实测两种状态与操作方式"],
        ],
        note="未完成设计与同范围询价前，不给出节省金额或单价结论。",
    )
    pages[6].update(
        title="100 万元，先说清包含什么",
        subtitle="计划投入已填写；这还不是核算后的总造价。",
        illustrations=[illustration("budget-scope.png", "预算范围图", "按已提交信息绘制；不虚构主体、装修和庭院的分配比例。")],
        blocks=[
            block("已明确", "主体、室内装修在本次计划范围内。询价时让不同供应方采用相同的包含项和交付标准。"),
            block("待核实", "庭院是否包含仍未确认；设备、报建等未填写项目也需逐项询问，不能默认为免费或已包含。"),
        ],
        note="可转换房间的家具及五金应单列核对，不将效果图中的配置视为已包含。",
    )
    pages[7].update(
        title="把家庭选择交给设计师验证",
        subtitle="下面表达建议的空间关系，不代替实际户型与尺寸设计。",
        illustrations=[illustration("space-relationships.png", "建议的功能联系", "功能关系示意：不按比例，不表示实际位置、面积、墙体或可建范围。")],
        blocks=[
            block("任务一：核对两种生活状态", "按两人日常与返乡团聚分别列出睡眠、起居、收纳和隐私需求，解释四卧意向如何落地。"),
            block("任务二：提交可核对的回答", "用实测边界、家具尺寸与门窗位置，验证兼用房转换后是否仍然好走、好用。"),
        ],
        note="实际可建范围和庭院预算仍未知；这些条件可能改变方案。",
    )
    pages[8].update(
        title="下一步，按这个顺序推进",
        subtitle="先补最影响判断的资料，再花时间深化方案与比较报价。",
        illustrations=[illustration("next-steps.png", "从决定到验证", "按依赖顺序推进；不承诺固定出图、审批或施工周期。")],
        actions=[
            block("家庭先确认", "列清返乡时每个人怎么住，以及谁负责房间转换。保留一份共同确认的清单。"),
            block("设计师再验证", "核对可建条件与两种状态，给出可对照的方案说明，再用统一范围询价。"),
        ],
        note="已经确认的方向可沿用；新增测绘、家庭意见和正式报价应记录为后续版本。",
    )
    pages[9].update(
        title="把已确认的选择带到下一步",
        subtitle="前四页的判断，已经落到这份报告的图、表和任务书里。",
        blocks=[
            block("最初的问题", "日常居住与节日团聚如何兼顾，避免只按房间数量作决定。"),
            block("你的确认", "优先深化可转换房间；先父母居住舒适，再节日团聚，最后控制投入。"),
            block("报告已展开", "固定与兼用场景、日常与返乡变化、取舍比较、预算范围、设计任务和推进清单。"),
            block("继续保留的未知项", "实际可建范围未核实；庭院是否包含在计划投入内待确认。"),
        ],
        note="演示案例。外观与室内图为 AI 意向表达；示意图由已知信息与建议生成。",
        disclaimer="不代替测绘、审批、建筑结构施工图、工程量清单及正式报价。",
    )
    return data


def draw_diagrams(asset_root, font):
    """Draw semantic diagrams directly, without invented measured geometry."""
    pdfmetrics.registerFont(TTFont("Demo-CN", str(font)))
    ink, jade, gold = "#5E412F", "#557166", "#B99763"

    def make(name, draw):
        stream = io.BytesIO()
        pdf = canvas.Canvas(stream, pagesize=(720, 300))
        pdf.setFillColor(HexColor("#FFFCF7"))
        pdf.rect(0, 0, 720, 300, fill=1, stroke=0)

        def text(value, x, y, size=17, color=ink):
            pdf.setFont("Demo-CN", size)
            pdf.setFillColor(HexColor(color))
            pdf.drawCentredString(x, y, value)

        def box(x, y, w, h, title, sub="", fill="#E5ECE7"):
            pdf.setFillColor(HexColor(fill))
            pdf.setStrokeColor(HexColor("#D9C9B4"))
            pdf.roundRect(x, y, w, h, 10, fill=1, stroke=1)
            text(title, x + w / 2, y + h / 2 + (6 if sub else -5), 18, jade)
            if sub:
                text(sub, x + w / 2, y + h / 2 - 19, 13)

        def arrow(x1, y1, x2, y2):
            pdf.setStrokeColor(HexColor(gold))
            pdf.setLineWidth(2)
            pdf.line(x1, y1, x2, y2)
            if y1 == y2:
                pdf.line(x2 - 7, y2 + 5, x2, y2)
                pdf.line(x2 - 7, y2 - 5, x2, y2)
            else:
                pdf.line(x2 - 5, y2 + 7, x2, y2)
                pdf.line(x2 + 5, y2 + 7, x2, y2)

        draw(text, box, arrow)
        pdf.showPage()
        pdf.save()
        with fitz.open(stream=stream.getvalue(), filetype="pdf") as document:
            document[0].get_pixmap(matrix=fitz.Matrix(2048 / 720, 2048 / 720), alpha=False).save(asset_root / name)

    def budget(text, box, arrow):
        text("计划投入 100 万元", 360, 251, 28)
        text("范围先明确 · 金额待同口径核算", 360, 221, 14)
        box(35, 103, 200, 85, "主体", "用户已勾选包含")
        box(260, 103, 200, 85, "室内装修", "用户已勾选包含")
        box(485, 103, 200, 85, "庭院", "是否包含待确认", "#F4EFE7")
        text("其他未填写项目：另列清单核对，不默认为已包含", 360, 50, 15)

    def spaces(text, box, arrow):
        text("建议联系 · 非实际平面", 360, 263, 23)
        box(265, 134, 190, 72, "日常起居", "高频使用")
        box(30, 134, 190, 72, "父母卧室", "建议少绕行")
        box(500, 134, 190, 72, "可转换房间", "兼顾两种状态")
        arrow(220, 170, 265, 170)
        arrow(455, 170, 500, 170)
        box(265, 30, 190, 70, "返乡团聚", "另核对全部床位", "#F4EFE7")
        arrow(360, 134, 360, 105)

    def steps(text, box, arrow):
        text("资料与家庭确认在前，方案与询价在后", 360, 250, 21)
        box(20, 95, 200, 100, "补齐条件", "可建范围与预算口径")
        box(260, 95, 200, 100, "验证方案", "日常与返乡两种状态")
        box(500, 95, 200, 100, "同范围询价", "材料、配置与变更规则")
        arrow(220, 145, 260, 145)
        arrow(460, 145, 500, 145)
        text("每一步保留书面结果，再决定是否进入下一步", 360, 45, 15)

    make("budget-scope.png", budget)
    make("space-relationships.png", spaces)
    make("next-steps.png", steps)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--font", type=Path, required=True)
    parser.add_argument("--prepare-only", action="store_true")
    args = parser.parse_args()
    root = args.output.resolve()
    assets = root / "assets"
    assets.mkdir(parents=True, exist_ok=True)
    data = illustrated_snapshot(json.loads(SOURCE.read_text(encoding="utf-8")))
    (root / "illustrated-snapshot.json").write_text(json.dumps(data, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    draw_diagrams(assets, args.font)
    if not args.prepare_only:
        render_decision_v5(data, root / "我家的建房决策报告-10页图文版.pdf", "full", args.font,
                           asset_root=assets, canonical_output=root / "qa/canonical.pdf",
                           page_images_dir=root / "qa/pages")


if __name__ == "__main__":
    main()
