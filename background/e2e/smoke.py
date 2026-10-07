#!/usr/bin/env python3.11
"""后台冒烟 / 回归检查（仅限本地联调环境）。

用法：
  1) service/localdev/dev.sh up && ./localdev/dev.sh start && ./localdev/dev.sh migrate && ./localdev/dev.sh seed
  2) cd background && VITE_DEV_PROXY_TARGET=http://127.0.0.1:28180 npm run dev -- --port 5180
  3) /opt/homebrew/opt/python@3.11/bin/python3.11 e2e/smoke.py            # API + 浏览器
     /opt/homebrew/opt/python@3.11/bin/python3.11 e2e/smoke.py --api-only  # 只跑 API

安全护栏：本脚本会做写操作（改灵石、改精选），E2E_API / E2E_WEB 不是本机地址时直接拒绝运行，且没有放行开关。
账号是 service/cmd/localseed 写入的虚构账号，口令见该文件。
"""
import http.cookiejar
import json
import os
import re
import sys
import traceback
import urllib.error
import urllib.parse
import urllib.request

API = os.environ.get("E2E_API", "http://127.0.0.1:28180")
WEB = os.environ.get("E2E_WEB", "http://127.0.0.1:5180")
PASSWORD = os.environ.get("E2E_PASSWORD", "LocalAdmin!2026")  # localseed 的虚构本地口令
USER_ID = os.environ.get("E2E_USER_ID", "3")  # localseed 写入的第一个小程序用户
ROUTES_FILE = os.path.join(os.path.dirname(__file__), "..", "src", "routes", "index.tsx")


def guard_local(*urls):
    # 默认只允许本机。会话 Cookie 已强制 Secure，本地 http 的 API 段需要 https 环境；
    # 服务器（https 测试环境）通过 E2E_ALLOW_REMOTE=1 显式放行——脚本会写数据，
    # 请只对可承受写操作的测试环境开启。
    allow_remote = os.environ.get("E2E_ALLOW_REMOTE") == "1"
    for u in urls:
        parsed = urllib.parse.urlparse(u)
        host = parsed.hostname
        if host in ("127.0.0.1", "localhost"):
            continue
        if allow_remote and parsed.scheme == "https":
            print(f"[护栏] E2E_ALLOW_REMOTE=1，放行 https 目标: {u}")
            continue
        sys.exit(f"拒绝运行：{u} 不是本机地址。对服务器 https 测试环境运行请设置 E2E_ALLOW_REMOTE=1（脚本会写数据）")


class Api:
    def __init__(self):
        self.opener = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()))

    def call(self, method, path, body=None):
        data = json.dumps(body).encode() if body is not None else None
        req = urllib.request.Request(API + "/api/v1/management" + path, data=data, method=method,
                                     headers={"Content-Type": "application/json"})
        try:
            with self.opener.open(req, timeout=20) as r:
                return r.status, _json(r.read())
        except urllib.error.HTTPError as e:
            return e.code, _json(e.read())

    def login(self, username, password=PASSWORD):
        return self.call("POST", "/login", {"username": username, "password": password})


def _json(raw):
    try:
        return json.loads(raw or b"{}")
    except ValueError:
        return {"_raw": raw[:200].decode("utf-8", "replace")}


RESULTS = []
STATE = {}  # 浏览器检查之间共享的会话状态（登录一次，避免撞上后端的登录限流）
LIST_KEYS = ("list", "items", "records", "rows", "tasks", "users", "templates", "configs", "plans", "tickets", "groups", "categories", "files")
CONFIG_API_FILE = os.path.join(os.path.dirname(__file__), "..", "src", "config", "api.ts")
SKIP_PATHS = ("/login", "/logout", "/me", "/export", "/cos/sts", "/wecom", "/upload")


def management_get_paths():
    """从前端 config/api.ts 取出所有不带路径参数的管理端接口，作为 GET 契约检查的清单。"""
    src = open(CONFIG_API_FILE, encoding="utf-8").read()
    paths = {m.removeprefix("/api/v1/management") for m in re.findall(r"'(/api/v1/management/[^'$]*)'", src)}
    return sorted(p for p in paths if p and not any(p.startswith(s) or p.endswith(s) for s in SKIP_PATHS))


def check(name):
    """定义即执行：被装饰的函数立刻运行并记录结果（避免"定义了却忘记调用"的假绿）。"""
    def deco(fn):
        try:
            fn()
            RESULTS.append((name, True, ""))
            print(f"  ✔ {name}")
        except AssertionError as e:
            RESULTS.append((name, False, str(e)))
            print(f"  ✘ {name}\n      {e}")
        except Exception as e:  # noqa: BLE001 - 测试脚本需要把任何异常记为失败
            RESULTS.append((name, False, f"{type(e).__name__}: {e}"))
            print(f"  ✘ {name}\n      {type(e).__name__}: {e}")
            traceback.print_exc(limit=2)
    return deco


# ───────────────────────────── API 检查 ─────────────────────────────
def api_checks():
    print("\n[API] 认证与权限")
    admin, crm, anon = Api(), Api(), Api()

    @check("错误口令登录被拒绝(400)")
    def _():
        status, _body = Api().login("admin_local", "wrong-password")
        assert status == 400, f"期望 400，实际 {status}"

    @check("未登录访问订单列表 → 401")
    def _():
        status, _body = anon.call("GET", "/orders")
        assert status == 401, f"期望 401，实际 {status}"

    @check("平台管理员登录成功，/me 返回 platform_admin")
    def _():
        status, body = admin.login("admin_local")
        assert status == 200, f"登录失败: {status} {body}"
        status, me = admin.call("GET", "/me")
        assert status == 200 and me["data"]["scope"] == "platform_admin", me

    @check("crm_only 账号：/me 为 crm_only，且访问订单被拒(403)")
    def _():
        status, body = crm.login("crm_local")
        assert status == 200, f"登录失败: {status} {body}"
        status, me = crm.call("GET", "/me")
        assert me["data"]["scope"] == "crm_only", me
        status, _body = crm.call("GET", "/orders")
        assert status == 403, f"期望 403，实际 {status}"

    print("\n[API] 销售线索")

    @check("线索列表支持 user_id 过滤：只返回该用户的线索；不带 user_id 返回全部；非法 user_id 视为不过滤")
    def _():
        status, everything = admin.call("GET", "/customer-leads?page=1&page_size=100")
        assert status == 200, status
        items = everything["data"]["list"]
        owned = [i for i in items if i["user_id"] > 0]
        assert owned, "本地种子里应有关联了用户账号的线索（先执行 dev.sh seed）"
        uid = owned[0]["user_id"]
        STATE["lead_user_id"] = uid
        status, filtered = admin.call("GET", f"/customer-leads?page=1&page_size=100&user_id={uid}")
        got = filtered["data"]["list"]
        assert status == 200 and got and all(i["user_id"] == uid for i in got), f"user_id 过滤结果不对: {[i['user_id'] for i in got]}"
        assert filtered["data"]["total"] == len([i for i in items if i["user_id"] == uid])
        assert filtered["data"]["total"] < everything["data"]["total"], "过滤后应少于全部线索"
        status, junk = admin.call("GET", "/customer-leads?page=1&page_size=100&user_id=abc")
        assert status == 200 and junk["data"]["total"] == everything["data"]["total"], "非法 user_id 应当不过滤"

    @check("线索总览里每个队列的数量 = 该队列列表的 total（Tab 角标不能和点进去看到的条数对不上）")
    def _():
        status, ov = admin.call("GET", "/customer-leads/overview")
        assert status == 200, status
        counts = ov["data"]
        queues = [("todo", "todo_count"), ("today", "today_new_count"), ("high", "high_todo_count"),
                  ("overdue", "overdue_count"), ("unassigned", "unassigned_count"), ("all", "total_count")]
        bad = []
        for queue, key in queues:
            status, lst = admin.call("GET", f"/customer-leads?page=1&page_size=1&queue={queue}")
            assert status == 200, status
            if counts.get(key) != lst["data"]["total"]:
                bad.append(f"{queue}: 总览 {key}={counts.get(key)} ≠ 列表 {lst['data']['total']}")
        assert not bad, "; ".join(bad)

    @check("线索总览补充各阶段数量：新线索 / 跟进中 / 已成交 / 无效，加起来等于总数")
    def _():
        status, ov = admin.call("GET", "/customer-leads/overview")
        c = ov["data"]
        parts = [c.get("new_count"), c.get("contacted_count"), c.get("converted_count"), c.get("invalid_count")]
        assert None not in parts, f"缺少阶段字段: {parts}"
        assert sum(parts) == c["total_count"], f"阶段之和 {sum(parts)} ≠ 总数 {c['total_count']}"

    print("\n[API] 接口契约：列表字段不得为 null")

    @check("列表接口在空表 / 搜索无结果时返回 [] 而不是 null（Go 的 nil 切片会序列化成 null，前端 list.length 直接崩溃）")
    def _():
        nulls = []
        for path in management_get_paths():
            for qs in ("?page=1&page_size=20", "?page=1&page_size=20&keyword=__no_match__&q=__no_match__"):
                status, body = admin.call("GET", path + qs)
                if status != 200:
                    continue  # POST-only 或需要路径参数的接口，GET 不适用
                data = body.get("data") if isinstance(body, dict) else None
                if isinstance(body, dict) and "data" in body and data is None:
                    nulls.append(f"{path}(data 整体为 null)")
                if isinstance(data, dict):
                    nulls += [f"{path}.{k}" for k, v in data.items() if v is None and k in LIST_KEYS]
        assert not nulls, f"{len(nulls)} 处: {sorted(set(nulls))}"

    # --- D-3：灵石直接设置 ---
    status, users = admin.call("GET", "/users?page=1&page_size=20")
    uid = users["data"]["list"][0]["id"] if status == 200 and users["data"]["list"] else None

    print("\n[API] D-3 灵石余额直接设置")

    @check("设为 120 → 200")
    def _():
        status, body = admin.call("PUT", f"/users/{uid}/stones", {"stones": 120, "remark": "e2e"})
        assert status == 200 and body["data"]["new_stones"] == 120, f"{status} {body}"

    @check("设为 0 → 200（修复前被 required 误拒为 400）")
    def _():
        status, body = admin.call("PUT", f"/users/{uid}/stones", {"stones": 0, "remark": "e2e"})
        assert status == 200 and body["data"]["new_stones"] == 0, f"{status} {body}"

    @check("设为负数 → 400")
    def _():
        status, _body = admin.call("PUT", f"/users/{uid}/stones", {"stones": -1})
        assert status == 400, f"期望 400，实际 {status}"

    @check("缺少 stones 字段 → 400（不能静默清零）")
    def _():
        status, _body = admin.call("PUT", f"/users/{uid}/stones", {"remark": "x"})
        assert status == 400, f"期望 400，实际 {status}"
        status, body = admin.call("GET", f"/users/{uid}")
        assert status == 200, body

    # --- D-2：精选开关 ---
    print("\n[API] D-2 模板精选开关")
    status, tpls = admin.call("GET", "/templates?page=1&page_size=20")
    featured = next((t for t in tpls["data"]["list"] if t.get("is_featured")), None) if status == 200 else None

    @check("存在可用于测试的精选模板（种子数据）")
    def _():
        assert featured, f"没有找到精选模板: {status} {tpls}"

    @check("取消精选 is_featured=false → 200 且数据生效（修复前必为 400）")
    def _():
        status, body = admin.call("PATCH", f"/templates/{featured['id']}/featured", {"is_featured": False})
        assert status == 200, f"{status} {body}"
        status, detail = admin.call("GET", f"/templates/{featured['id']}")
        assert detail["data"]["is_featured"] is False, f"详情未更新: {detail['data'].get('is_featured')}"

    @check("重新设为精选 is_featured=true → 200")
    def _():
        status, body = admin.call("PATCH", f"/templates/{featured['id']}/featured", {"is_featured": True})
        assert status == 200, f"{status} {body}"

    @check("缺少 is_featured 字段 → 400（不能静默当成 false）")
    def _():
        status, _body = admin.call("PATCH", f"/templates/{featured['id']}/featured", {})
        assert status == 400, f"期望 400，实际 {status}"

    # --- D-5：异常单筛选 ---
    print("\n[API] D-5 订单中心“仅看异常单”（服务端筛选）")
    status, first = admin.call("GET", "/orders?page=1&page_size=50")
    first_page_ex = [o for o in first["data"]["list"] if o["status"] in ("failed", "pending")] if status == 200 else []

    @check("前提：全部订单第 1 页(50 条)里没有异常单（旧的前端过滤会因此显示为 0）")
    def _():
        assert status == 200 and first["data"]["total"] == 118, f"total={first['data'].get('total')}"
        assert len(first_page_ex) == 0, f"第 1 页意外含有 {len(first_page_ex)} 笔异常单，种子顺序可能变了"

    @check("status=exception 返回全量 6 笔异常单，且只含 failed/pending")
    def _():
        status, body = admin.call("GET", "/orders?status=exception&page=1&page_size=50")
        assert status == 200, f"{status} {body}"
        assert body["data"]["total"] == 6, f"total={body['data']['total']}"
        assert {o["status"] for o in body["data"]["list"]} <= {"failed", "pending"}
        assert body["data"]["summary"]["total_count"] == 6, body["data"]["summary"]

    @check("status=exception 的分页有效（每页 2 条，第 2 页）")
    def _():
        status, body = admin.call("GET", "/orders?status=exception&page=2&page_size=2")
        assert status == 200 and len(body["data"]["list"]) == 2 and body["data"]["total"] == 6, body["data"]

    @check("status=exception 可与其他筛选组合（类型=消费 → 0 笔）")
    def _():
        status, body = admin.call("GET", "/orders?status=exception&type=consume&page_size=50")
        assert status == 200 and body["data"]["total"] == 0, body["data"]["total"]

    @check("既有状态筛选不受影响：status=failed → 3 笔")
    def _():
        status, body = admin.call("GET", "/orders?status=failed&page_size=50")
        assert status == 200 and body["data"]["total"] == 3, body["data"]["total"]


# ───────────────────────────── 浏览器检查 ─────────────────────────────
def static_routes():
    src = open(ROUTES_FILE, encoding="utf-8").read()
    return [p for p in re.findall(r"\bpath:\s*'(/[^']*)'", src) if p != "/login"]


# 已知、可接受的噪音：本地没有配置的外部集成（微信 / COS / AI 供应商 / 企微），后端会返回"未配置"类错误
KNOWN_NOISE = []


def browser_checks():
    from playwright.sync_api import sync_playwright

    print("\n[浏览器] 路由巡检（管理员）")
    routes = static_routes()
    findings = []
    with sync_playwright() as p:
        browser = p.chromium.launch()
        ctx = browser.new_context(viewport={"width": 1366, "height": 860})
        page = ctx.new_page()
        console_errors, bad_responses = [], []

        def on_console(msg):
            if msg.type == "error":
                console_errors.append(msg.text)

        def on_response(resp):
            if "/api/" in resp.url and resp.status >= 400:
                bad_responses.append((resp.status, resp.request.method, resp.url.replace(WEB, "")))

        page.on("console", on_console)
        page.on("pageerror", lambda e: console_errors.append(f"pageerror: {e}"))
        page.on("response", on_response)

        @check("UI 登录管理员 → 进入总控台")
        def _():
            page.goto(WEB + "/admin/login")
            page.fill("#login-account", "admin_local")
            page.fill("#login-password", PASSWORD)
            page.click("button[type=submit]")
            try:
                page.wait_for_url(re.compile(r".*/admin/dashboard"), timeout=15000)
            except Exception:
                body = page.inner_text("body")[:120]
                hint = "（被登录限流：每账号 5 分钟内限制次数，请稍等几分钟再跑）" if "频繁" in body else ""
                raise AssertionError(f"登录后没有进入总控台{hint}，页面: {body!r}")
            # 登录落地的总控台会同时发 3 个接口请求；不等它们结束就跳走，旧页面会把被浏览器取消的请求记进控制台，
            # 被下一个路由的检查误认成它自己的错误。
            page.wait_for_load_state("networkidle", timeout=20000)
            STATE["admin"] = ctx.storage_state()  # 后面的上下文复用会话，不再重复登录，省登录限流额度

        for path in routes:
            if True:
                @check(f"路由 {path}")
                def _(path=path):
                    console_errors.clear()
                    bad_responses.clear()
                    page.goto(WEB + "/admin" + path)
                    page.wait_for_selector(".layout-content", timeout=15000)
                    page.wait_for_load_state("networkidle", timeout=20000)
                    assert page.url.rstrip("/").endswith("/admin" + path), f"被重定向到 {page.url}"
                    text = page.inner_text(".layout-content").strip()
                    assert len(text) > 10, "页面内容为空"
                    # 样式作用域：该路径对应的 scss 必须已注入，否则页面会整页失去样式（R-1）
                    # 开发模式是 <style> 标签，生产构建是 CSS 文件：统一从 styleSheets 的规则里找
                    scoped = page.evaluate(
                        """(path) => [...document.styleSheets].some(ss => {
                            try { return [...ss.cssRules].some(r => r.cssText.includes('data-admin-page="' + path + '"')); }
                            catch (e) { return false; }
                        })""", path)
                    needs_scope = path not in ("/customer-management",) or True
                    assert scoped or not needs_scope, f"没有找到 data-admin-page=\"{path}\" 的样式"
                    # 卡片排版：section-card 有内容却没有内边距，说明页面靠别处"泄漏"的样式才正常（文字会贴边）
                    flat = page.evaluate(
                        """() => [...document.querySelectorAll('.layout-content .section-card')]
                            .filter(e => (e.innerText || '').trim() && getComputedStyle(e).padding.split(' ').every(v => v === '0px'))
                            .map(e => e.className.toString().slice(0, 50))""")
                    assert not flat, f"{len(flat)} 张卡片没有内边距: {flat[:3]}"
                    unexpected = [e for e in console_errors if not any(k in e for k in KNOWN_NOISE)]
                    assert not unexpected, f"控制台错误: {unexpected[:2]}"
                    bad = [b for b in bad_responses if b not in findings]
                    if bad:
                        findings.extend(bad)
                        assert False, f"接口返回 4xx/5xx: {bad[:3]}"

        print("\n[浏览器] 修复项的界面行为")

        @check("D-6 未知网址回到总控台（不再整页空白）")
        def _():
            page.goto(WEB + "/admin/this-page-does-not-exist")
            page.wait_for_url(re.compile(r".*/admin/dashboard"), timeout=10000)
            # 布局先渲染、页面代码块再懒加载：重定向瞬间内容区只有"页面加载中…"，需要等内容就绪
            page.wait_for_function("(document.querySelector('.layout-content')?.innerText || '').trim().length > 10", timeout=10000)

        @check("D-5 订单中心：仅看异常单 → 显示全量 6 条异常（旧逻辑会显示 0）")
        def _():
            page.goto(WEB + "/admin/recharge")
            page.wait_for_selector(".layout-content table tbody tr")
            page.get_by_role("button", name="仅看异常单").click()
            page.wait_for_function("document.querySelectorAll('.layout-content table tbody tr').length === 6", timeout=10000)
            banner = page.inner_text(".order-summary-banner-row")
            assert "6 条异常" in banner, f"摘要: {banner}"
            statuses = page.eval_on_selector_all(".layout-content table tbody tr", "rs => rs.map(r => r.className)")
            assert all("row-alert" in c for c in statuses), statuses
            page.get_by_role("button", name="查看全部").click()
            page.wait_for_function("document.querySelectorAll('.layout-content table tbody tr').length === 50", timeout=10000)

        @check("D-1 订单号链接 → /virtualpay?keyword=…（不再是 /virtual-pay 白屏）")
        def _():
            page.goto(WEB + "/admin/recharge")
            page.wait_for_selector(".virtualpay-order-link")
            page.locator(".virtualpay-order-link").first.click()
            page.wait_for_url(re.compile(r".*/admin/virtualpay\?keyword=LOCAL"), timeout=10000)
            page.wait_for_selector(".layout-content")
            assert len(page.inner_text(".layout-content").strip()) > 10

        @check("crm_only 账号登录后直达客户管理，且被限制在该页")
        def _():
            ctx2 = browser.new_context()
            pg = ctx2.new_page()
            pg.goto(WEB + "/admin/login")
            pg.fill("#login-account", "crm_local")
            pg.fill("#login-password", PASSWORD)
            pg.click("button[type=submit]")
            pg.wait_for_url(re.compile(r".*/admin/customer-management"), timeout=15000)
            pg.goto(WEB + "/admin/recharge")
            pg.wait_for_url(re.compile(r".*/admin/customer-management"), timeout=10000)
            pg.wait_for_selector(".layout-sidebar .sidebar-item")
            n = pg.locator(".layout-sidebar .sidebar-item").count()
            assert n == 1, f"crm_only 账号应只看到 1 个导航入口，实际 {n}"
            ctx2.close()
        print("\n[浏览器] 统一布局（布局上移到路由层）的行为")
        ctx3 = browser.new_context(viewport={"width": 1366, "height": 860}, storage_state=STATE.get("admin"))
        pg = ctx3.new_page()
        me_requests = []
        pg.on("request", lambda r: me_requests.append(r.url) if r.url.endswith("/api/v1/management/me") else None)
        pg.goto(WEB + "/admin/dashboard")
        pg.wait_for_selector(".layout-content", timeout=15000)

        def go_via_sidebar(path):
            pg.locator(f'a.sidebar-item[href="/admin{path}"]').click()
            pg.wait_for_url(re.compile(".*/admin" + re.escape(path) + "$"), timeout=10000)
            pg.wait_for_selector(".layout-content", timeout=10000)

        @check("侧栏 / 顶栏只挂载一次：站内跳转后同一个 DOM 节点仍在")
        def _():
            pg.evaluate("document.querySelector('.layout-sidebar').dataset.mark = 'kept'; document.querySelector('.layout-topbar').dataset.mark = 'kept'")
            for path in ("/users", "/recharge", "/templates", "/dashboard"):
                go_via_sidebar(path)
            marks = pg.evaluate("[document.querySelector('.layout-sidebar').dataset.mark, document.querySelector('.layout-topbar').dataset.mark]")
            assert marks == ["kept", "kept"], f"侧栏/顶栏被重建了: {marks}"

        @check("站内跳转不再重复请求 /me（以前每个页面的顶栏都会请求一次）")
        def _():
            before = len(me_requests)
            for path in ("/users", "/ai-tasks", "/support-tickets", "/dashboard"):
                go_via_sidebar(path)
            assert len(me_requests) == before, f"站内跳转期间额外请求了 {len(me_requests) - before} 次 /me"

        @check("切换页面后内容区回到顶部（内容区是同一个元素，必须手动复位）")
        def _():
            go_via_sidebar("/templates")
            # 等页面内容渲染到足够长，否则"页面加载中…"阶段没有滚动条，无法验证复位
            pg.wait_for_function("(() => { const c = document.querySelector('.layout-content'); return c.scrollHeight > c.clientHeight + 100; })()", timeout=15000)
            pg.evaluate("document.querySelector('.layout-content').scrollTo({top: 99999})")
            scrolled = pg.evaluate("document.querySelector('.layout-content').scrollTop")
            assert scrolled > 50, f"前提不成立：页面不够长，无法验证滚动复位（scrollTop={scrolled}）"
            go_via_sidebar("/users")
            pg.wait_for_timeout(300)
            top = pg.evaluate("document.querySelector('.layout-content').scrollTop")
            assert top == 0, f"跳转后内容区停在了旧页面的滚动位置 scrollTop={top}"

        @check("浏览器后退 / 前进正常，且页面内容随之切换")
        def _():
            go_via_sidebar("/users")
            go_via_sidebar("/templates")
            pg.go_back()
            pg.wait_for_url(re.compile(r".*/admin/users$"), timeout=10000)
            pg.wait_for_selector(".users-table")
            pg.go_forward()
            pg.wait_for_url(re.compile(r".*/admin/templates$"), timeout=10000)
            pg.wait_for_selector(".layout-content")

        @check("带参数的深链刷新后仍可用：/user-workbench?userId=…")
        def _():
            pg.goto(WEB + "/admin/user-workbench?userId=" + USER_ID)
            pg.wait_for_selector(".layout-content", timeout=15000)
            pg.wait_for_load_state("networkidle")
            assert "user-workbench" in pg.url and "userId=" in pg.url, pg.url
            pg.wait_for_function("(document.querySelector('.layout-content')?.innerText || '').trim().length > 20", timeout=10000)

        @check("侧栏折叠状态在页面切换后保持（以前每个页面都会重置）")
        def _():
            go_via_sidebar("/dashboard")
            pg.locator(".topbar-menu-btn").click()
            collapsed = pg.evaluate("document.querySelector('.layout-sidebar').classList.contains('collapsed')")
            go_via_sidebar("/users")
            after = pg.evaluate("document.querySelector('.layout-sidebar').classList.contains('collapsed')")
            assert collapsed == after, f"折叠状态在跳转后改变: {collapsed} → {after}"
            pg.locator(".topbar-menu-btn").click()  # 还原

        @check("未登录直接访问受保护页面 → 跳转登录页")
        def _():
            anon = browser.new_context().new_page()
            anon.goto(WEB + "/admin/users")
            anon.wait_for_url(re.compile(r".*/admin/login"), timeout=10000)
            anon.context.close()

        print("\n[浏览器] 新导航（9 个一级入口 + 子导航条，网址不变）")
        EXPECT_GROUPS = ["工作台", "用户账号", "订单与支付", "AI 运营", "内容运营", "异常工单", "销售线索", "风控与增长", "系统设置"]

        def sidebar_labels():
            return pg.eval_on_selector_all(".layout-sidebar .sidebar-item", "els => els.map(e => e.getAttribute('aria-label'))")

        def subnav_tabs():
            return pg.eval_on_selector_all(".layout-subnav .subnav-tab", "els => els.map(e => e.textContent.trim())")

        @check("侧栏只有 9 个一级入口（旧导航 24 项），销售线索是一级入口，系统设置固定在底部")
        def _():
            pg.goto(WEB + "/admin/dashboard")
            pg.wait_for_selector(".layout-sidebar .sidebar-item")
            assert sidebar_labels() == EXPECT_GROUPS, sidebar_labels()
            in_pinned = pg.eval_on_selector_all(".sidebar-pinned .sidebar-item", "els => els.map(e => e.getAttribute('aria-label'))")
            assert in_pinned == ["系统设置"], in_pinned

        @check("每个页面都能从导航到达：打开任一路由，侧栏恰有 1 个分组高亮")
        def _():
            bad = []
            for path in static_routes():
                pg.goto(WEB + "/admin" + path)
                pg.wait_for_selector(".layout-sidebar .sidebar-item")
                n = pg.locator(".layout-sidebar .sidebar-item.active").count()
                if n != 1:
                    bad.append((path, n))
            assert not bad, f"高亮数量不为 1 的页面: {bad}"

        @check("分组页显示同组 Tab；单页分组（工作台）没有子导航条")
        def _():
            pg.goto(WEB + "/admin/users")
            pg.wait_for_selector(".layout-subnav")
            assert subnav_tabs() == ["用户列表", "设计师", "认证审核", "会员运营"], subnav_tabs()
            pg.goto(WEB + "/admin/dashboard")
            pg.wait_for_selector(".layout-content")
            assert pg.locator(".layout-subnav").count() == 0, "工作台不应有子导航条"

        @check("点 Tab 跳转到原有网址并高亮：订单中心 → 虚拟支付监控（/virtualpay）")
        def _():
            pg.goto(WEB + "/admin/recharge")
            pg.wait_for_selector(".layout-subnav")
            pg.locator(".layout-subnav .subnav-tab", has_text="虚拟支付监控").click()
            pg.wait_for_url(re.compile(r".*/admin/virtualpay$"), timeout=10000)
            pg.wait_for_function("document.querySelector('.layout-subnav .subnav-tab.active')?.textContent.trim() === '虚拟支付监控'", timeout=5000)
            active = pg.eval_on_selector(".layout-subnav .subnav-tab.active", "e => e.textContent.trim()")
            assert active == "虚拟支付监控", active
            assert pg.eval_on_selector(".sidebar-item.active", "e => e.getAttribute('aria-label')") == "订单与支付"

        @check("二级页：订单与支付 → 支付与会员配置（/recharge-config）→ 返回")
        def _():
            pg.goto(WEB + "/admin/recharge")
            pg.wait_for_selector(".layout-subnav")
            pg.locator(".layout-subnav .subnav-entry", has_text="支付与会员配置").click()
            pg.wait_for_url(re.compile(r".*/admin/recharge-config$"), timeout=10000)
            pg.wait_for_selector(".layout-subnav .subnav-back", timeout=5000)  # URL 先变、界面后渲染，等二级页的子导航条出现
            assert subnav_tabs() == ["充值配置", "会员计划"], subnav_tabs()
            assert pg.locator(".layout-subnav .subnav-back").count() == 1
            assert pg.eval_on_selector(".sidebar-item.active", "e => e.getAttribute('aria-label')") == "订单与支付"
            pg.locator(".layout-subnav .subnav-back").click()
            pg.wait_for_url(re.compile(r".*/admin/recharge$"), timeout=10000)

        @check("详情页（用户 360）归属用户分组并高亮「用户列表」Tab")
        def _():
            pg.goto(WEB + "/admin/user-workbench?userId=" + USER_ID)
            pg.wait_for_selector(".layout-subnav")
            active = pg.eval_on_selector(".layout-subnav .subnav-tab.active", "e => e.textContent.trim()")
            assert active == "用户列表", active

        @check("「用户账号」与「销售线索」在侧栏里各带一行说明，一眼能分辨")
        def _():
            pg.goto(WEB + "/admin/dashboard")
            pg.wait_for_selector(".layout-sidebar .sidebar-item")
            hints = pg.eval_on_selector_all(".layout-sidebar .sidebar-item", "els => Object.fromEntries(els.map(e => [e.getAttribute('aria-label'), (e.querySelector('.sidebar-hint')||{}).textContent || '']))")
            assert hints["用户账号"] == "已注册的小程序用户", hints
            assert hints["销售线索"] == "留了联系方式，待跟进", hints

        CARD_LABELS = "els => els.map(e => e.querySelector('.subnav-card-label').textContent.trim())"

        @check("销售线索分三类：顶部是入口卡片「客户 · 线索 · 商机」，当前类别高亮；客户未接入 CRM 时灰色标注，「线索」卡片带待处理角标")
        def _():
            pg.goto(WEB + "/admin/customer-leads")
            pg.wait_for_selector(".layout-subnav-cards .subnav-card")
            assert pg.eval_on_selector_all(".layout-subnav-cards .subnav-card", CARD_LABELS) == ["客户", "线索", "商机"]
            assert pg.locator(".layout-subnav").count() == 0, "卡片导航与文字 Tab 二选一，不应同时出现"
            assert pg.eval_on_selector(".subnav-card.is-current .subnav-card-label", "e => e.textContent.trim()") == "线索"
            customer = pg.locator(".subnav-card", has_text="客户")
            assert "tone-gray" in customer.get_attribute("class") and "未接入" in customer.inner_text(), "本地未启用 CRM：客户卡片应为灰色并标注未接入"
            pg.wait_for_selector(".subnav-card .subnav-badge", timeout=8000)
            badge = int(pg.locator(".subnav-card", has_text="线索").locator(".subnav-badge").inner_text())
            assert badge > 0, "种子数据里有待处理线索，角标应大于 0"

        @check("线索队列在页内筛选条里：6 个队列带数量，默认待处理；切换后地址 ?queue= 与列表随之变化")
        def _():
            pg.goto(WEB + "/admin/customer-leads")
            pg.wait_for_selector(".customer-lead-queuepill")
            labels = pg.eval_on_selector_all(".customer-lead-queuepill", "els => els.map(e => e.childNodes[0].textContent.trim())")
            assert labels == ["待处理", "今日新增", "高意向", "超时未跟进", "未分配", "全部线索"], labels
            assert pg.eval_on_selector(".customer-lead-queuepill.active", "e => e.childNodes[0].textContent.trim()") == "待处理", "默认应落在待处理"
            pg.locator(".customer-lead-queuepill", has_text="全部线索").click()
            pg.wait_for_url(re.compile(r".*/admin/customer-leads\?queue=all$"), timeout=10000)
            pg.wait_for_load_state("networkidle")
            all_rows = pg.locator(".layout-content tbody tr:has(input[type=checkbox])").count()
            pg.locator(".customer-lead-queuepill", has_text="高意向").click()
            pg.wait_for_url(re.compile(r".*queue=high$"), timeout=10000)
            high_count = int(pg.locator(".customer-lead-queuepill", has_text="高意向").locator("b").inner_text())
            # URL 先变、列表后刷新：等「数据行」（带勾选框的行，不含「暂无数据」空行）数量稳定到筛选条上的数字再断言
            rows_js = "document.querySelectorAll('.layout-content tbody tr:has(input[type=checkbox])').length"
            pg.wait_for_function(f"{rows_js} === {high_count}", timeout=8000)
            assert 0 < high_count < all_rows, f"高意向 {high_count} 条应少于全部 {all_rows} 条"
            assert pg.locator(".customer-lead-queues").count() == 0, "单独打开时不应再有页内队列卡片"

        @check("商机占位页 /opportunities：只说明未启用，不展示任何数字；入口卡片为禁用态；可返回线索")
        def _():
            pg.goto(WEB + "/admin/customer-leads")
            pg.wait_for_selector(".layout-subnav-cards .subnav-card")
            # 商机未启用：入口卡片应为禁用态（span，不可点），直接以网址访问占位页
            disabled = pg.locator(".subnav-card.is-disabled", has_text="商机")
            assert disabled.count() == 1, "商机卡片应为禁用态"
            assert "未启用" in disabled.inner_text()
            pg.goto(WEB + "/admin/opportunities")
            pg.wait_for_selector(".opportunities-card", timeout=8000)
            assert "暂未启用" in pg.locator(".opportunities-card").inner_text()
            assert pg.eval_on_selector(".sidebar-item.active", "e => e.getAttribute('aria-label')") == "销售线索"
            pg.locator(".opportunities-card a", has_text="返回线索").click()
            pg.wait_for_url(re.compile(r".*/admin/customer-leads$"), timeout=10000)

        @check("销售线索概览区：线索简报 + 线索阶段环形图并排，数字自洽；不再显示、也不再请求智能客服漏斗")
        def _():
            funnel_calls = []
            on_request = lambda req: funnel_calls.append(req.url) if "/customer-leads/funnel" in req.url else None
            pg.on("request", on_request)
            pg.goto(WEB + "/admin/customer-leads")
            pg.wait_for_selector(".lead-hub .lead-hub-donut-center strong", timeout=8000)
            pg.wait_for_load_state("networkidle")
            assert pg.locator(".lead-hub-tile").count() == 0, "入口卡片已移到布局层的导航条，概览区不再重复"
            assert "智能客服漏斗" not in pg.locator(".layout-content").inner_text(), "漏斗已从线索页去掉"
            total = int(pg.locator(".lead-hub-donut-center strong").inner_text())
            legend = pg.eval_on_selector_all(".lead-hub-legend li", "els => els.map(e => [e.querySelector('span').textContent, parseInt(e.querySelector('b').textContent)])")
            assert [n for n, _ in legend] == ["新线索", "跟进中", "已成交", "无效"], legend
            assert sum(c for _, c in legend) == total > 0, f"各阶段之和应等于线索量 {total}: {legend}"
            figures = pg.eval_on_selector_all(".lead-hub-figures > div", "els => Object.fromEntries(els.map(e => [e.querySelector('span').textContent, parseInt(e.querySelector('strong').textContent)]))")
            badge = int(pg.locator(".subnav-card", has_text="线索").locator(".subnav-badge").inner_text())
            assert figures["待处理"] == badge, f"简报的待处理 {figures['待处理']} 应与「线索」卡片角标 {badge} 一致"
            # 简报与线索阶段并排：两张卡片同一行
            tops = pg.eval_on_selector_all(".lead-hub > .lead-hub-card", "els => els.map(e => Math.round(e.getBoundingClientRect().top))")
            assert len(tops) == 2 and tops[0] == tops[1], f"桌面宽度下简报与线索阶段应并排: {tops}"
            assert pg.locator(".lead-hub-donut circle").count() >= 2, "环形图应画出底环和至少一段"
            pg.remove_listener("request", on_request)
            assert not funnel_calls, f"不应再请求漏斗接口: {funnel_calls}"

        @check("线索 ⇄ 用户账号 互相跳转：用户 360「查看关联线索」→ 只看该用户；线索行「查看用户账号」→ 用户 360；可清除过滤")
        def _():
            uid = str(STATE["lead_user_id"])
            pg.goto(WEB + "/admin/user-workbench?userId=" + uid)
            pg.get_by_role("button", name="查看关联线索").click()
            pg.wait_for_url(re.compile(rf".*/admin/customer-leads\?queue=all&user_id={uid}$"), timeout=10000)
            pg.wait_for_selector(".customer-lead-userfilter", timeout=8000)
            pg.wait_for_load_state("networkidle")
            links = pg.eval_on_selector_all(".layout-content tbody .customer-lead-userlink", "els => els.map(e => e.getAttribute('href'))")
            assert links and all(h.endswith("userId=" + uid) for h in links), links
            pg.locator(".layout-content tbody .customer-lead-userlink").first.click()
            pg.wait_for_url(re.compile(rf".*/admin/user-workbench\?userId={uid}$"), timeout=10000)
            pg.go_back()
            pg.wait_for_selector(".customer-lead-userfilter", timeout=8000)
            pg.locator(".customer-lead-userfilter button").click()
            pg.wait_for_function("!location.search.includes('user_id')", timeout=5000)
            assert pg.locator(".customer-lead-userfilter").count() == 0

        @check("启用 CRM 时：「客户」卡片变为蓝色可用（去掉「未接入」），侧栏不单列客户管理；客户 ⇄ 线索可来回切换")
        def _():
            def force_crm(route):
                resp = route.fetch()
                body = resp.json()
                body["data"]["crmEnabled"] = True
                route.fulfill(response=resp, json=body)
            pg.route("**/api/v1/management/me", force_crm)
            try:
                pg.goto(WEB + "/admin/customer-leads")
                pg.wait_for_selector(".layout-subnav-cards .subnav-card", timeout=8000)
                assert pg.eval_on_selector_all(".layout-subnav-cards .subnav-card", CARD_LABELS) == ["客户", "线索", "商机"]
                customer = pg.locator(".subnav-card", has_text="客户")
                assert "tone-blue" in customer.get_attribute("class") and "未接入" not in customer.inner_text(), customer.inner_text()
                assert sidebar_labels() == EXPECT_GROUPS, "侧栏不应多出独立的「客户管理」: " + str(sidebar_labels())
                customer.click()
                pg.wait_for_url(re.compile(r".*/admin/customer-management$"), timeout=10000)
                pg.wait_for_selector(".subnav-card.is-current", timeout=8000)
                assert pg.eval_on_selector(".sidebar-item.active", "e => e.getAttribute('aria-label')") == "销售线索"
                assert pg.eval_on_selector(".subnav-card.is-current .subnav-card-label", "e => e.textContent.trim()") == "客户"
                pg.locator(".subnav-card", has_text="线索").click()
                pg.wait_for_url(re.compile(r".*/admin/customer-leads$"), timeout=10000)
            finally:
                pg.unroute("**/api/v1/management/me")

        @check("未启用 CRM 时「客户」卡片仍可进入客户管理页（线索 / 账号与权限栏目不依赖 CRM），且侧栏高亮「销售线索」")
        def _():
            pg.goto(WEB + "/admin/customer-leads")
            pg.wait_for_selector(".layout-subnav-cards .subnav-card")
            pg.locator(".subnav-card", has_text="客户").click()
            pg.wait_for_url(re.compile(r".*/admin/customer-management$"), timeout=10000)
            pg.wait_for_selector(".subnav-card.is-current", timeout=8000)
            assert pg.eval_on_selector(".sidebar-item.active", "e => e.getAttribute('aria-label')") == "销售线索"
            assert pg.eval_on_selector(".subnav-card.is-current .subnav-card-label", "e => e.textContent.trim()") == "客户"

        @check("⌘K 页面搜索：Ctrl+K → 输入「充值配置」→ 回车 → /recharge-config")
        def _():
            pg.goto(WEB + "/admin/dashboard")
            pg.wait_for_selector(".layout-content")
            pg.keyboard.press("Control+k")
            pg.wait_for_selector(".cmdk-panel", timeout=5000)
            pg.keyboard.type("充值配置")
            pg.wait_for_selector(".cmdk-item.on")
            pg.keyboard.press("Enter")
            pg.wait_for_url(re.compile(r".*/admin/recharge-config$"), timeout=10000)
            assert pg.locator(".cmdk-panel").count() == 0

        @check("工单队列 Tab：待处理/处理中/今日新增/超48小时/全部，带数量，点击切换列表")
        def _():
            pg.goto(WEB + "/admin/support-tickets")
            pg.wait_for_selector(".ticket-queues .ticket-queue-tab", timeout=8000)
            tabs = pg.locator(".ticket-queue-tab")
            labels = pg.eval_on_selector_all(".ticket-queue-tab", "els => els.map(e => e.textContent.trim())")
            for want in ["待处理", "处理中", "今日新增", "超48小时", "全部工单"]:
                assert any(want in t for t in labels), f"缺少队列 {want}: {labels}"
            # 每个队列 Tab 都显示数量
            counts = pg.eval_on_selector_all(".ticket-queue-count", "els => els.map(e => e.textContent.trim())")
            assert len(counts) >= 5, counts
            pg.locator(".ticket-queue-tab", has_text="待处理").first.click()
            pg.wait_for_load_state("networkidle")

        @check("操作审计页（原日志页）：记录写操作，谁/何时/对什么")
        def _():
            pg.evaluate("window.localStorage.setItem('adminNav', 'new')")
            # 先制造一条写操作：空参数的会员计划保存会 400，但请求已通过鉴权，审计中间件照记
            # （400 也是有效审计记录，且不产生真实业务数据）
            # 制造一条写操作：空参数保存会 400，但已通过鉴权，审计中间件照记。
            # 必须用页面内 fetch（浏览器网络栈对 127.0.0.1 接受 Secure Cookie）；
            # pg.request 走独立网络栈遵守 Secure 属性，本地 http 下不带 Cookie 会 401
            fetch_js = "fetch('/api/v1/management/membership-plans', {method: 'POST', headers: {'Content-Type': 'application/json'}, body: '{}', credentials: 'include'}).then(function(r){return r.status;})"
            pg.evaluate(fetch_js)
            pg.goto(WEB + "/admin/logs")
            pg.wait_for_selector(".logs-table", timeout=8000)
            body = pg.locator(".logs-table").inner_text()
            assert "操作人" in body, "审计页表头应包含「操作人」列: " + body[:120]
            assert "admin_local" in body, f"审计页应显示 admin_local 的写操作记录，实际: {body[:200]}"

        @check("旧导航开关：切到旧导航 → 平铺 24 项、无子导航条；刷新后保持；可切回")
        def _():
            pg.goto(WEB + "/admin/recharge")
            pg.wait_for_selector(".layout-subnav")
            pg.get_by_role("button", name="切换到旧导航").click()
            pg.wait_for_function("document.querySelectorAll('.layout-sidebar .sidebar-item').length === 24", timeout=5000)
            assert pg.locator(".layout-subnav").count() == 0
            pg.reload()
            pg.wait_for_selector(".layout-sidebar .sidebar-item")
            assert pg.locator(".layout-sidebar .sidebar-item").count() == 24, "刷新后旧导航没有保持"
            pg.get_by_role("button", name="返回新导航").click()
            pg.wait_for_function("document.querySelectorAll('.layout-sidebar .sidebar-item').length === 9", timeout=5000)

        @check("地址参数 ?nav=old 可直接进入旧导航（灰度回退链接）")
        def _():
            pg.goto(WEB + "/admin/dashboard?nav=old")
            pg.wait_for_function("document.querySelectorAll('.layout-sidebar .sidebar-item').length === 24", timeout=8000)
            pg.goto(WEB + "/admin/dashboard?nav=new")  # 还原
            pg.wait_for_function("document.querySelectorAll('.layout-sidebar .sidebar-item').length === 9", timeout=8000)

        ctx3.close()

        print("\n[浏览器] 移动端宽度（390x844）：抽屉导航")
        mctx = browser.new_context(viewport={"width": 390, "height": 844}, storage_state=STATE.get("admin"))
        mp = mctx.new_page()
        mp.goto(WEB + "/admin/dashboard")
        mp.wait_for_selector(".layout-content", timeout=15000)

        @check("手机宽度：侧栏默认收起；点菜单按钮展开抽屉")
        def _():
            assert mp.evaluate("document.querySelector('.layout-sidebar').classList.contains('mobile')"), "移动端模式未生效"
            assert not mp.evaluate("document.querySelector('.layout-sidebar').classList.contains('open')"), "抽屉默认不应展开"
            mp.locator(".topbar-menu-btn").click()
            assert mp.evaluate("document.querySelector('.layout-sidebar').classList.contains('open')"), "点击后抽屉未展开"

        @check("手机宽度：点导航项后跳转并自动收起抽屉，页面无横向溢出")
        def _():
            mp.locator('a.sidebar-item[href="/admin/users"]').click()
            mp.wait_for_url(re.compile(r".*/admin/users$"), timeout=10000)
            mp.wait_for_selector(".users-table", timeout=10000)
            assert not mp.evaluate("document.querySelector('.layout-sidebar').classList.contains('open')"), "跳转后抽屉没有收起"
            overflow = mp.evaluate("document.documentElement.scrollWidth - window.innerWidth")
            assert overflow <= 1, f"整页出现横向溢出 {overflow}px"

        mctx.close()
        browser.close()


def main():
    guard_local(API, WEB)
    print(f"API={API}  WEB={WEB}")
    api_checks()
    if "--api-only" not in sys.argv:
        browser_checks()
    if not RESULTS:
        sys.exit("没有执行任何检查，视为失败")
    failed = [r for r in RESULTS if not r[1]]
    print(f"\n结果：{len(RESULTS) - len(failed)}/{len(RESULTS)} 通过")
    for name, _ok, why in failed:
        print(f"  ✘ {name}: {why}")
    sys.exit(1 if failed else 0)


if __name__ == "__main__":
    main()
