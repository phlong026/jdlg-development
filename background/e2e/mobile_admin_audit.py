#!/usr/bin/env python3
"""Mobile layout regression for the admin console (audit items F01-F06 and the share-tracking dialog).

Every /api request is answered by explicit synthetic fixtures below; nothing reaches a real backend.
Start Vite (or serve a production build under /admin/) on a local address first:

  npx vite --port 5187 --host 127.0.0.1
  python3 e2e/mobile_admin_audit.py [screenshot-dir]

E2E_WEB overrides the base URL (local addresses only). Exit code is non-zero when any check fails.
Soft keyboards are approximated by a 390x300 viewport; real devices still need a manual pass.
"""
import ipaddress
import json
import os
import sys
from urllib.parse import urlparse

from playwright.sync_api import sync_playwright

BASE = os.environ.get("E2E_WEB", "http://127.0.0.1:5187").rstrip("/")


def is_loopback(url):
    """Only an http(s) URL whose parsed hostname is a real loopback address counts as local."""
    parsed = urlparse(url)
    host = parsed.hostname or ""
    if parsed.scheme not in ("http", "https") or parsed.username or parsed.password:
        return False
    if host == "localhost":
        return True
    try:
        return ipaddress.ip_address(host).is_loopback
    except ValueError:
        return False


if not is_loopback(BASE):
    sys.exit("拒绝运行：只允许本机地址（localhost / 127.0.0.0/8 / ::1）")
OUT = sys.argv[1] if len(sys.argv) > 1 else "/tmp/mobile-admin-audit"
os.makedirs(OUT, exist_ok=True)

SIZES = {
    "320x568": (320, 568), "375x667": (375, 667), "390x844": (390, 844), "414x896": (414, 896),
    "768x1024": (768, 1024), "1440x900": (1440, 900),
    "390x480-short": (390, 480), "844x390-landscape": (844, 390), "390x300-keyboard": (390, 300),
}
PHONES = [n for n, (w, _) in SIZES.items() if w <= 768]

LONG = "甲第灵光超长中文用户昵称用于测试表格列在窄屏下是否逐字换行显示"
LONGTEXT = "上游服务返回了一个很长很长的说明文字，用来验证列表单元格在窄屏下不会逐字换行，也不会把整行撑得过高。" * 3
ok = lambda data, **extra: {"code": 0, "data": data, **extra}
page_of = lambda items: ok({"list": items, "total": len(items), "page": 1, "page_size": 20})

asset = dict(media_id="m1", kind="image", url=BASE + "/qa/x.png", width=360, height=640, mime="image/png", title="素材")
content = dict(
    content_id="content-qa-1", version=1, revision=1, title="测试内容：" + LONG, type="article", status="published",
    cover=None, media=[], body=("这是自动化测试正文。\n" * 80), source_url="https://example.test/own", rights="own",
    owner_id="owner-qa", page_views=3, visible_ms=50000,
)
snapshot = dict(as_of="2026-10-11T17:20:00Z", metric_version="qa-metric-v1", identified_count=1, anonymous_count=0)
order = dict(
    id=1, user_id=9, username=LONG, designer_user_id=0, order_no="ORD20261013000000000099", type="recharge",
    order_category="recharge", amount=60, status="paid", review_status="", pay_channel="wechat", pay_mode="",
    product_id="p", amount_fen=6000, deliver_reported=True, deliver_report_attempts=0, title="充值订单" + LONG,
    description=LONGTEXT, completed_at="2026-10-12T10:00:00Z", created_at="2026-10-12T09:00:00Z",
)
ticket = dict(
    id=1, user_id=9, username=LONG, type="complaint", source_type="manual", source_id="", title="工单" + LONG,
    content=LONGTEXT, priority="high", status="open", assignee_id=0, assignee_name="", created_by="qa",
    created_at="2026-10-12T09:00:00Z", updated_at="2026-10-12T09:00:00Z",
)
cert = dict(
    id=1, user_id=9, type="enterprise", real_name="", id_card_no="", company_name="某某" + LONG + "有限公司",
    credit_code="91440300MA5XXXXXXXXXXXXX", legal_person="", aliyun_passed=False, aliyun_msg="", extra_docs_remark="",
    identity_type="企业", status="pending_review", admin_remark="", reviewed_at=None, reviewed_by=0,
    created_at="2026-10-12T09:00:00Z", updated_at="2026-10-12T09:00:00Z",
)
FIXTURES = [
    ("$/management/me", lambda m: ok(dict(id=1, username="qa", scope="crm_only" if m == "share" else "platform_admin", crmEnabled=m != "share"))),
    ("/crm/context", lambda m: ok(dict(account=dict(username="boss", displayName="主管", employeeId=None, role="admin", enabled=True), demo=True))),
    ("/crm/workbench", lambda m: ok(dict(totals=dict(pending=0, historical=0, handover=0, duplicates=0, cases=0), pending=[], historical=[], handover=[], duplicates=[], cases=[], sourceCount=0, customerCount=0))),
    ("/crm/access/employees", lambda m: ok([])),
    ("/crm/access", lambda m: ok(dict(accounts=[], employees=[]))),
    ("/crm/dashboard", lambda m: ok(dict(overview={}, todayAdded=0, unknownAddedTime=0, recent=[], team=[], asOf="2026-10-13T00:00:00Z"))),
    ("/crm/", lambda m: ok(dict(list=[], total=0, page_size=20, runs=[], events=[], accounts=[], employees=[], pending=0, failed=0, configured=False, callbackConfigured=False))),
    ("/tracking/context", lambda m: ok(dict(can_view=True, can_manage=True, owners=[dict(id="owner-qa", name="授权测试归属")]))),
    ("/tracking/contents/content-qa-1/", lambda m: ok({**snapshot, "items": [], "next_cursor": None})),
    ("/tracking/contents/content-qa-1", lambda m: ok({**content, **snapshot})),
    ("/tracking/contents", lambda m: ok({**snapshot, "items": [content], "next_cursor": None})),
    ("/tracking/visitors", lambda m: ok({**snapshot, "items": [], "next_cursor": None})),
    ("/inspirations", lambda m: page_of([dict(id=i, title="灵感%d 现代简约别墅外立面设计参考" % i, description="描述", tags=["现代"], topic="inspiration", sort_order=i, status="pending", view_count=1, like_count=1, created_at="2026-10-01T00:00:00Z", updated_at="2026-10-01T00:00:00Z") for i in (1, 2)])),
    ("/templates/categories", lambda m: ok(dict(list=[dict(id="villa", name="别墅", sort_order=1)]))),
    ("/support-tickets/overview", lambda m: ok(dict(total_count=1, open_count=1, in_progress_count=0, closed_count=0, high_priority_count=1, today_count=1, overdue_count=0))),
    ("/support-tickets", lambda m: page_of([ticket])),
    ("/orders/fund-stats", lambda m: ok(dict(today_recharge_fen=0, today_recharge_count=0, today_recharge_stones=0, wechat_mirror_total=0, local_stones_total=0, exception_count=0))),
    ("/orders", lambda m: ok(dict(list=[order], summary=dict(total_count=1, success_amount=6000, success_count=1, pending_count=0), status_counts={}, total=1, page=1, page_size=20))),
    ("/certification-application-stats", lambda m: ok(dict(pending_review=1, approved=0, rejected=0, pending_payment=0, overdue=0))),
    ("/certification-applications", lambda m: {"code": 0, "data": [cert], "total": 1}),
    ("/pricing/config", lambda m: ok(dict(source="db", coin_ratio=10, cert_title_map={}, membership_note="", recharge_plans=[dict(type="a", title="体验档", amount_fen=600, recommended=False)], cert_prices=[dict(product_id="x", price_fen=100)]))),
    ("/ai/pricing", lambda m: ok([])),
    ("/ai/tasks", lambda m: page_of([dict(id=1, task_no="T20261013000001", user_id=9, username=LONG, scene="villa", model="img-model-long-name-v2", status="failed", stones_used=5, error_message=LONGTEXT, created_at="2026-10-12T10:00:00Z")])),
    ("/membership-operations", lambda m: ok(dict(list=[dict(user_id=7, username="u7", display_name=LONG, plan_title="年度会员计划", plan_code="annual_long_code", status="active", remaining_days=30, template_download_enabled=True, source_order_no="ORD20261013000000000099", latest_recharge_order=dict(order_no="ORD20261013000000000100", status="paid"))], total=1, page=1, page_size=20))),
]
mode = {"value": "admin"}


def handle(route):
    path = urlparse(route.request.url).path
    for needle, make in FIXTURES:
        if (path.endswith(needle[1:]) if needle.startswith("$") else needle in path):
            return route.fulfill(content_type="application/json", body=json.dumps(make(mode["value"]), ensure_ascii=False))
    route.fulfill(content_type="application/json", body=json.dumps(ok(dict(list=[], total=0, page=1, page_size=20))))


failures, checks = [], 0


def check(cond, msg):
    global checks
    checks += 1
    if not cond:
        failures.append(msg)
        print("  FAIL", msg)


def box(page, sel):
    return page.evaluate("s=>{const e=document.querySelector(s);if(!e)return null;const r=e.getBoundingClientRect();return{x:r.x,y:r.y,w:r.width,h:r.height,r:r.right,b:r.bottom}}", sel)


def overlaps(a, b):
    return a["x"] < b["r"] and b["x"] < a["r"] and a["y"] < b["b"] and b["y"] < a["b"]


def no_hscroll(page, where):
    check(page.evaluate("document.documentElement.scrollWidth <= innerWidth + 1"), f"{where}: page scrolls horizontally")


def open_page(browser, name, route, who="admin"):
    mode["value"] = who
    w, h = SIZES[name]
    ctx = browser.new_context(viewport={"width": w, "height": h}, has_touch=w <= 768)
    ctx.route("**/api/**", handle)
    page = ctx.new_page()
    errors = []
    page.on("pageerror", lambda e: errors.append(str(e)))
    page.goto(BASE + "/admin" + route)
    page.wait_for_selector(".layout-content", timeout=15000)
    page.wait_for_timeout(600)
    return ctx, page, errors


def check_editor(page, where, button):
    page.get_by_role("button", name=button).first.click()
    page.wait_for_selector(".modal-content .modal-body", timeout=8000)
    page.wait_for_timeout(250)
    m = page.evaluate("""()=>{const b=document.querySelector('.modal-content .modal-body');const before=b.scrollTop;b.scrollTop=1e6;
      const fields=[...b.querySelectorAll('input,select,textarea')].filter(e=>e.getClientRects().length);const last=fields[fields.length-1];
      const lb=last&&last.getBoundingClientRect(),bb=b.getBoundingClientRect(),fe=document.querySelector('.modal-content .modal-footer'),f=fe?fe.getBoundingClientRect():null;
      return{tag:b.tagName,overflow:b.scrollHeight>b.clientHeight+1,moved:b.scrollTop>before,lastVisible:!lb||lb.bottom<=bb.bottom+1,footerInView:!f||(f.bottom<=innerHeight+1&&f.top>=0),hasFooter:!!f}}""")
    check(m["tag"] == "DIV" or m["tag"] == "SECTION", f"{where}: modal body is a {m['tag']}, a fieldset cannot scroll")
    check(not m["overflow"] or m["moved"], f"{where}: modal body overflows but does not scroll")
    check(m["lastVisible"], f"{where}: last field unreachable after scrolling")
    check(m["footerInView"], f"{where}: cancel/save footer outside viewport")


with sync_playwright() as p:
    browser = p.chromium.launch(args=["--no-sandbox"])

    for name in SIZES:
        phone = name in PHONES
        print("==", name)

        # F01 inspirations toolbar
        ctx, page, _ = open_page(browser, name, "/inspirations")
        parts = {k: box(page, s) for k, s in [("search", ".inspirations-toolbar .search-input"), ("topic", 'select[aria-label="灵感话题"]'), ("status", 'select[aria-label="灵感状态"]'), ("refresh", ".toolbar-actions .btn-secondary"), ("add", ".toolbar-actions .btn-primary")]}
        vals = [v for v in parts.values() if v]
        check(len(vals) == 5, f"F01 {name}: toolbar controls missing")
        check(not any(overlaps(a, b) for i, a in enumerate(vals) for b in vals[i + 1:]), f"F01 {name}: toolbar controls overlap")
        if phone and len(vals) == 5:
            check(parts["search"]["w"] >= 140 and parts["topic"]["w"] >= 140 and parts["status"]["w"] >= 140, f"F01 {name}: search/filter too narrow {parts['search']['w']:.0f}/{parts['topic']['w']:.0f}")
        no_hscroll(page, f"F01 {name}")
        page.screenshot(path=f"{OUT}/F01-{name}.png")
        check_editor(page, f"F04 inspirations {name}", "新增灵感")
        ctx.close()

        # F04 editors
        for key, route, button in [("templates", "/templates", "添加模板"), ("utility-tools", "/utility-tools", "添加内容"), ("support-tickets", "/support-tickets", "新建工单"), ("oss", "/oss", "上传文件")]:
            ctx, page, _ = open_page(browser, name, route)
            check_editor(page, f"F04 {key} {name}", button)
            page.screenshot(path=f"{OUT}/F04-{key}-{name}.png")
            ctx.close()

        # F02 pricing-config header
        ctx, page, _ = open_page(browser, name, "/pricing-config")
        badge, save, refresh = box(page, ".source-badge"), box(page, ".pricing-toolbar-actions .btn-primary"), box(page, ".pricing-toolbar-actions .btn-secondary")
        check(bool(badge and save and refresh), f"F02 {name}: header controls missing")
        if badge and save and refresh:
            check(badge["h"] < 40 and save["h"] < 60 and refresh["h"] < 60, f"F02 {name}: status/buttons stacked vertically ({badge['h']:.0f}/{save['h']:.0f})")
            check(save["w"] >= 60 and refresh["w"] >= 60, f"F02 {name}: buttons too narrow")
        no_hscroll(page, f"F02 {name}")
        page.screenshot(path=f"{OUT}/F02-{name}.png")
        ctx.close()

        # F03 AI pricing notice stays running prose
        ctx, page, _ = open_page(browser, name, "/ai-pricing")
        info = page.evaluate("()=>{const n=document.querySelector('.aip-notice');if(!n)return null;const b=n.querySelector('b').getBoundingClientRect();return{display:getComputedStyle(n).display,boldW:b.width,boldH:b.height}}")
        check(info is not None and info["display"] != "flex", f"F03 {name}: notice is a flex row ({info})")
        check(info is not None and info["boldH"] < 60, f"F03 {name}: bold fragment is stacked into a column")
        ctx.close()

        # F05 CRM active tab visible + fade cue
        for tab in ["notices", "workbench", "sync", "leads"]:
            ctx, page, _ = open_page(browser, name, f"/customer-management?tab={tab}")
            page.wait_for_timeout(300)
            r = page.evaluate("()=>{const a=document.querySelector('.crm-tabs .active'),t=document.querySelector('.crm-tabs');if(!a)return null;const x=a.getBoundingClientRect(),v=t.getBoundingClientRect();return{full:x.left>=v.left-1&&x.right<=v.right+1,overflow:t.scrollWidth>t.clientWidth+1,fade:t.dataset.fade||''}}")
            check(r is not None, f"F05 {tab} {name}: no active tab rendered")
            if r:
                check(r["full"], f"F05 {tab} {name}: active tab not fully visible")
                check(not r["overflow"] or r["fade"] != "", f"F05 {tab} {name}: overflowing tab strip has no edge cue")
            if tab == "sync":
                page.screenshot(path=f"{OUT}/F05-{tab}-{name}.png")
            ctx.close()

        # F06 long-text tables
        for key, route, row_sel, cells in [
            ("ai-tasks", "/ai-tasks", ".task-table tbody tr", [".task-table .user-cell", ".task-table .text-cell"]),
            ("orders", "/recharge", ".order-center-table tbody tr", [".order-user-cell", ".order-title-cell"]),
            ("membership", "/membership-operations", ".membership-table tbody tr", [".membership-table .user-cell", ".membership-table .order-cell"]),
            ("tickets", "/support-tickets", ".support-table tbody tr", [".support-table .identity-cell"]),
            ("certification", "/certification", ".data-table tbody tr", [".cert-subject-cell"]),
        ]:
            ctx, page, errors = open_page(browser, name, route)
            try:
                page.wait_for_selector(cells[0], timeout=8000)
            except Exception:
                pass
            row = box(page, row_sel)
            check(row is not None, f"F06 {key} {name}: no data row rendered ({errors[:1]})")
            if row:
                check(row["h"] <= 280, f"F06 {key} {name}: row is {row['h']:.0f}px tall (text broken per character?)")
                for sel in cells:
                    c = box(page, sel)
                    check(c is not None and c["w"] >= 110, f"F06 {key} {name}: {sel} width {c['w'] if c else 'missing'}")
            if name == "390x844":
                page.screenshot(path=f"{OUT}/F06-{key}-{name}.png", full_page=True)
            ctx.close()

        # Share-tracking dialog keeps its close button reachable while the body scrolls
        ctx, page, _ = open_page(browser, name, "/share-tracking", who="share")
        page.get_by_role("tab", name="素材视角").click()
        page.get_by_role("button", name="查看完整标题、内容与访客").first.click()
        page.get_by_role("dialog").wait_for()
        page.wait_for_timeout(300)
        r = page.evaluate("""()=>{const d=document.querySelector('.st-dialog');d.scrollTop=1e6;const h=d.querySelector('header').getBoundingClientRect(),c=d.getBoundingClientRect(),
          btn=d.querySelector('header button').getBoundingClientRect();return{scrolled:d.scrollTop>0,headerAtTop:Math.abs(h.top-c.top)<2,closeInView:btn.top>=0&&btn.bottom<=innerHeight}}""")
        # shareTracking.scss deliberately un-pins the header at max-height:500px to save vertical space,
        # so the pinned-header check only applies to taller viewports.
        if SIZES[name][1] > 500:
            check(not r["scrolled"] or (r["headerAtTop"] and r["closeInView"]), f"share dialog {name}: header/close scrolled away ({r})")
        page.screenshot(path=f"{OUT}/share-dialog-{name}.png")
        ctx.close()

    browser.close()

print(f"\n{checks} checks, {len(failures)} failed")
sys.exit(1 if failures else 0)
