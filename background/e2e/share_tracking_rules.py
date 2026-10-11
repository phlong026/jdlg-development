#!/usr/bin/env python3
"""Rule UI regression against explicit synthetic API fixtures, never live writes.
Run Vite on 127.0.0.1:5189; this is browser QA, not backend or device acceptance.
"""
import copy, json, os
from urllib.parse import urlparse, parse_qs
from playwright.sync_api import sync_playwright
BASE = os.environ.get('TRACKING_QA_BASE', 'http://127.0.0.1:5189')
OUT = os.environ.get('TRACKING_RULES_QA_OUT', '/tmp/share-tracking-rules-qa')
os.makedirs(OUT, exist_ok=True)
base = dict(rule_version='followup-v1', revision=1, owner_id='owner-a', effective_at='2026-10-01T00:00:00Z', actor=0, reason='明确合成测试夹具', timezone='Asia/Shanghai', window_days=30, medium_sessions=2, medium_favorites=1, high_business_results=1, daily_session_cap=10, daily_action_cap=1, participating_metrics=['independent_sessions','server_favorite','server_appointment'], exclusions=['anonymous_identity','preview','self','test','client_click','daily_contribution_over_cap'])
rules = {owner:dict(base, owner_id=owner) for owner in ['owner-a','owner-b']}
history = {owner:[copy.deepcopy(rule)] for owner,rule in rules.items()}
state = dict(can_rules=True, save_conflict=True, recompute_fail=True)
requests = []
recompute_ids = []
metrics = dict(as_of='2026-10-11T19:00:00Z', metric_version='synthetic-v1', identified_count=0, anonymous_count=0, items=[])
def computed(version):
    return dict(rule_version=version, evaluated_count=12, changed_count=3, as_of='2026-10-11T19:00:00Z')
def handle(route):
    req = route.request; path = urlparse(req.url).path; query = parse_qs(urlparse(req.url).query); body = req.post_data_json if req.post_data else None
    requests.append(dict(path=path, query=query, method=req.method, body=body)); data={}; status=200; msg=''
    if path.endswith('/management/me'): data=dict(id=90001, username='synthetic_rule_qa', scope='crm_only', crmEnabled=False)
    elif path.endswith('/tracking/context'): data=dict(can_view=True,can_manage=False,can_rules=state['can_rules'],owners=[dict(id='owner-a',name='测试归属 A'),dict(id='owner-b',name='测试归属 B')])
    elif path.endswith('/tracking/intent-rules/recompute'):
        owner=query['owner'][0];recompute_ids.append(body['request_id']);assert body['rule_version']==rules[owner]['rule_version']
        if state['recompute_fail']: status=503;msg='合成夹具：暂时无法确认重算';state['recompute_fail']=False
        else:data=computed(body['rule_version'])
    elif path.endswith('/tracking/intent-rules'):
        owner=query['owner'][0]
        if req.method=='POST':
            if state['save_conflict']:
                state['save_conflict']=False;rules[owner]=dict(rules[owner],rule_version='followup-v2',revision=2);history[owner].insert(0,copy.deepcopy(rules[owner]));status=409;msg='规则版本已变化，请刷新'
            else:
                assert body['expected_rule_version']==rules[owner]['rule_version']
                revision=rules[owner]['revision']+1;rules[owner]=dict(rules[owner],**{k:v for k,v in body.items() if k!='expected_rule_version'},rule_version=f'followup-v{revision}',revision=revision);history[owner].insert(0,copy.deepcopy(rules[owner]));data=dict(current=rules[owner],history=history[owner],recomputed=computed(rules[owner]['rule_version']))
        else:data=dict(current=rules[owner],history=history[owner])
    elif path.endswith('/visitors') or path.endswith('/contents'): data=metrics
    route.fulfill(status=status,content_type='application/json',body=json.dumps(dict(code=0 if status==200 else status,msg=msg,data=data),ensure_ascii=False))
with sync_playwright() as p:
    browser=p.chromium.launch(executable_path='/usr/bin/chromium',headless=True,args=['--no-sandbox']);context=browser.new_context();context.route('**/api/**',handle);page=context.new_page();errors=[];page.on('pageerror',lambda error:errors.append(str(error)))
    page.goto(BASE+'/admin/share-tracking');page.get_by_role('button',name='意向规则与重算',exact=True).click()
    assert page.get_by_label('规则归属范围',exact=True).input_value()==''
    assert not [request for request in requests if request['path'].endswith('/intent-rules')]
    page.get_by_label('规则归属范围',exact=True).select_option('owner-a');page.get_by_role('button',name='保存新版本并重算').wait_for()
    for width in [320,375,390,768,1024,1440]:
        page.set_viewport_size(dict(width=width,height=850));page.screenshot(path=f'{OUT}/rules-{width}.png',full_page=True)
        assert page.evaluate('document.documentElement.scrollWidth <= innerWidth'), f'overflow at {width}'
        assert page.locator('.st-dialog').evaluate('(el)=>el.scrollWidth<=el.clientWidth'), f'dialog overflow at {width}'
    page.set_viewport_size(dict(width=375,height=660));page.get_by_label('评估窗口（天）',exact=True).fill('45');page.get_by_label('变更原因',exact=True).fill('合成验证：保留输入')
    page.once('dialog',lambda dialog:dialog.dismiss());page.keyboard.press('Escape');assert page.get_by_role('dialog').count()==1
    page.get_by_role('button',name='保存新版本并重算').click();page.get_by_text('规则版本已变化，请刷新',exact=False).wait_for();assert page.get_by_label('评估窗口（天）',exact=True).input_value()=='45';assert page.get_by_label('变更原因',exact=True).input_value()=='合成验证：保留输入'
    assert page.get_by_role('button',name='保存新版本并重算').is_disabled()
    page.get_by_role('button',name='读取最新规则核对（保留输入）').click();page.get_by_role('button',name='已核对，保留输入并采用最新版本基线').wait_for();page.screenshot(path=f'{OUT}/conflict-375.png',full_page=True)
    page.get_by_role('button',name='已核对，保留输入并采用最新版本基线').click();page.get_by_role('button',name='保存新版本并重算').click();page.get_by_text('已保存新版本 followup-v3，立即生效并完成重算。',exact=True).wait_for();assert page.get_by_label('变更原因',exact=True).input_value()==''
    page.get_by_label('重算原因',exact=True).fill('合成验证：证据变化');page.get_by_role('button',name='按当前版本重算',exact=True).click();page.get_by_text('合成夹具：暂时无法确认重算',exact=False).wait_for();assert page.get_by_label('重算原因',exact=True).input_value()=='合成验证：证据变化'
    page.get_by_role('button',name='读取最新规则核对（保留输入）').click();page.get_by_role('button',name='按当前版本重算',exact=True).click();page.get_by_text('服务端已完成所选归属的重算。',exact=True).wait_for();assert len(recompute_ids)==2 and recompute_ids[0]==recompute_ids[1];page.screenshot(path=f'{OUT}/recomputed-375.png',full_page=True)
    page.get_by_label('变更原因',exact=True).fill('不应带入 B');page.once('dialog',lambda dialog:dialog.accept());page.get_by_label('规则归属范围',exact=True).select_option('owner-b');page.get_by_label('变更原因',exact=True).wait_for();assert page.get_by_label('变更原因',exact=True).input_value()=='';assert page.get_by_label('评估窗口（天）',exact=True).input_value()=='30'
    page.keyboard.press('Escape');assert page.get_by_role('dialog').count()==0
    state['can_rules']=False;page.reload();page.get_by_role('button',name='意向规则与重算',exact=True).click();page.get_by_label('规则归属范围',exact=True).select_option('owner-a');page.get_by_text('当前账号仅可查看规则与历史，没有规则配置或重算权限。',exact=True).wait_for();assert page.get_by_role('button',name='保存新版本并重算').count()==0;assert page.get_by_role('button',name='按当前版本重算',exact=True).count()==0;page.screenshot(path=f'{OUT}/readonly-375.png',full_page=True)
    assert not errors,errors
    with open(OUT+'/result.json','w') as output:json.dump(dict(passed=True,widths=[320,375,390,768,1024,1440],requests=requests,browser_errors=errors,notice='Synthetic API fixture browser QA only; not live integration or device acceptance.'),output,ensure_ascii=False,indent=2)
    browser.close();print('PASS: rules, scopes, conflict, idempotent retry, readonly and 6 widths: '+OUT)
