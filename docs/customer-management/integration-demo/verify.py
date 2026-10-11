"""Exercise real Go login -> signed Java -> MySQL using isolated public demo fixtures only."""
import json, urllib.request, urllib.error, http.cookiejar, datetime, csv, io, time

BASE='http://127.0.0.1:8091'
for attempt in range(30):
    try:
        with urllib.request.urlopen(BASE+'/health',timeout=2) as response:
            assert json.load(response)['mode']=='local-integration'
        break
    except (OSError, AssertionError):
        if attempt==29: raise
        time.sleep(1)
class Client:
    def __init__(self):
        self.http=urllib.request.build_opener(urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()))
    def call(self,path,body=None,method=None,expected=200,origin=True):
        headers={'Content-Type':'application/json','X-CRM-Request':'1'}
        if origin: headers['Origin']=BASE
        req=urllib.request.Request(BASE+'/api/v1/management'+path,data=json.dumps(body,ensure_ascii=False).encode() if body is not None else None,headers=headers,method=method)
        try: res=self.http.open(req,timeout=30)
        except urllib.error.HTTPError as e: res=e
        raw=res.read()
        assert res.status==expected,(path,res.status,raw.decode(errors='replace'))
        if 'text/csv' in res.headers.get('Content-Type',''): return list(csv.reader(io.StringIO(raw.decode('utf-8-sig'))))
        value=json.loads(raw) if raw else {}
        return value.get('data',value)
    def login(self,name):return self.call('/login',{'username':name,'password':'LocalCRM!2026'})

admin,staff=Client(),Client()
admin.login('crm_admin');staff.login('crm_advisor')
for attempt in range(30):
    if admin.call('/crm/access')['serviceAvailable']: break
    if attempt==29: raise RuntimeError('Java service not ready')
    time.sleep(1)
assert admin.call('/me')['scope']=='platform_admin'
assert staff.call('/me')['scope']=='crm_only'
def provision(uid,**changes):
    data=admin.call('/crm/access');u=next(u for u in data['users'] if u['userId']==uid)
    body=dict(userId=uid,version=u['version'],scope='platform_admin' if uid==1 else 'crm_only',enabled=True,crmEnabled=True,displayName='客户主管' if uid==1 else '李顾问',employeeId='' if uid==1 else 'demo-advisor-2',role='admin' if uid==1 else 'staff')
    body.update(changes)
    return admin.call('/crm/access',body,expected=502 if changes.get('employeeId')=='missing' else 200)
provision(1);provision(2)
assert admin.call('/crm/context')['account']['role']=='admin'
assert staff.call('/crm/context')['account']['role']=='staff'
assert admin.call('/crm/customers')['total']==8
assert staff.call('/crm/customers')['total']==4
for path in ['/users','/crm/access','/crm/sync','/customer-leads','/crm/customers/demo-customer-1']:
    staff.call(path,expected=403)
print('PASS login, managed account binding, platform/module/customer isolation')

d=admin.call('/crm/dashboard')
assert d['overview']['all']==8 and sum(t['total'] for t in d['team'])==8
assert staff.call('/crm/dashboard')['team']==[]
assert admin.call('/crm/customers?queue=added')['total']==d['todayAdded']
assert staff.call('/crm/customers?ownerId=demo-advisor-1')['total']==0
rows=staff.call('/crm/export/customers')
assert len(rows)==5 and all(row[2]=='demo-advisor-2' for row in rows[1:])
rows=admin.call('/crm/export/customers?keyword='+urllib.parse.quote('林女士'))
assert len(rows)==2 and rows[1][0]=='林女士'
print('PASS dashboard queue parity, team aggregation, scoped and filtered CSV')

cid='/crm/customers/demo-customer-1'
original=admin.call(cid)['customer']
def edit(c,**changes):
    body={k:c.get(k) for k in ['version','ownerId','stage','category','intent','phone','demand','nextFollowupAt']}
    body.update(changes)
    return admin.call(cid,body,method='PATCH')
try:
    changed=edit(original,demand=original['demand']+'\n本地联调校验')
    assert changed['nextFollowupAt']==original['nextFollowupAt']
    admin.call(cid,{**{k:original.get(k) for k in ['version','ownerId','stage','category','intent','phone','demand','nextFollowupAt']}},method='PATCH',expected=409)
    admin.call(cid,{},method='PATCH',expected=403,origin=False)
    admin.call(cid+'/followups',dict(version=changed['version'],content='测试必填日期',stage='following',nextFollowupAt=None),expected=400)
    admin.call(cid+'/followups',dict(version=changed['version'],content='本地联调：完成联系',stage='won',nextFollowupAt=None))
    current=admin.call(cid)['customer'];assert current['nextFollowupAt'] is None
    assigned=edit(current,ownerId='demo-advisor-2')
    staff.call(cid)
    edit(assigned,ownerId='demo-advisor-1')
    staff.call(cid,expected=403)
finally:
    # Restoring an overdue schedule is intentionally forbidden by the business API.
    # Restore profile/owner, keep a new future appointment and preserve the visible test history.
    current=admin.call(cid)['customer']
    edit(current,ownerId=original['ownerId'],demand=original['demand'],stage='following',nextFollowupAt=(datetime.datetime.now(datetime.timezone.utc)+datetime.timedelta(days=1)).isoformat())
print('PASS profile edit, optimistic conflict, CSRF, follow-up transition, immediate reassignment isolation')

provision(2,employeeId='missing')
staff.call('/crm/context',expected=403)
provision(2)
provision(2,enabled=False)
staff.call('/me',expected=403)
provision(2)
staff.call('/me')
print('PASS failed provisioning remains disabled, account deactivation/recovery')
assert admin.call('/customer-leads')['list']==[]
print('PASS original lead endpoint retained (isolated empty fixture)')
print('ALL ISOLATED HTTP CHECKS PASSED')
