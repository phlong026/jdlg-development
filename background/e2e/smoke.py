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
ROUTES_FILE = os.path.join(os.path.dirname(__file__), "..", "src", "routes", "index.tsx")


def guard_local(*urls):
    for u in urls:
        host = urllib.parse.urlparse(u).hostname
        if host not in ("127.0.0.1", "localhost"):
            sys.exit(f"拒绝运行：{u} 不是本机地址（本脚本会写数据，只允许本地联调环境）")


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
            page.wait_for_url(re.compile(r".*/admin/dashboard"), timeout=15000)

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
                    scoped = page.evaluate(
                        """(path) => [...document.querySelectorAll('style')]
                            .some(s => s.textContent.includes('data-admin-page="' + path + '"'))""", path)
                    needs_scope = path not in ("/customer-management",) or True
                    assert scoped or not needs_scope, f"没有找到 data-admin-page=\"{path}\" 的样式"
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
            assert len(page.inner_text(".layout-content").strip()) > 10

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
            ctx2.close()
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
