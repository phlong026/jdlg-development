"""Real Go -> Java -> MySQL acceptance, ONLY against the named isolated demo."""
import base64, datetime, http.cookiejar, json, subprocess, time, urllib.request, urllib.error, uuid

BASE = 'http://127.0.0.1:8092'
assert json.load(urllib.request.urlopen(BASE+'/health',timeout=5))['mode']=='local-integration'
class Client:
    def __init__(self):
        self.http=urllib.request.build_opener(urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()))
    def call(self,path,body=None,method=None,expected=200):
        req=urllib.request.Request(BASE+'/api/v1/management'+path,data=None if body is None else json.dumps(body,ensure_ascii=False).encode(),method=method,headers={'Origin':BASE,'Content-Type':'application/json','X-CRM-Request':'1'})
        try: res=self.http.open(req,timeout=30)
        except urllib.error.HTTPError as e: res=e
        raw=res.read();assert res.status==expected,(path,res.status,raw[:300])
        if res.headers.get('Content-Type','').startswith(('application/pdf','image/','text/csv')):return raw
        value=json.loads(raw) if raw else {};return value.get('data',value)
    def login(self,name):self.call('/login',{'username':name,'password':'LocalCRM!2026'})
def sql(query):
    result=subprocess.run(['docker','exec','-e','MYSQL_PWD=LocalRootOnly2026','jd-crm-report-preview-db-1','mysql','-uroot','-N','-B','-e',query],capture_output=True,text=True,encoding='utf-8')
    assert result.returncode==0,result.stderr
    return result.stdout.strip()
admin,staff=Client(),Client();admin.login('crm_admin');staff.login('crm_advisor')
assert admin.call('/crm/context')['account']['role']=='admin'
for path in ['/crm/workbench','/crm/owners','/crm/push-status','/crm/access']:
    staff.call(path,expected=403)
phone='138'+str(time.time_ns())[-8:]
report=dict(name='流程验收客户-'+uuid.uuid4().hex[:6],wechat='验收微信',phone=phone,address='演示省演示县测试镇测试村',buildingArea=180,buildingType='新建',expectedBuildTime='2027年春天',remark='仅用于本地自动验收')
c=staff.call('/crm/customers',report);cid=c['id'];path='/crm/customers/'+cid
def fresh():return admin.call(path)['customer']
def post(action,body):return admin.call(path+'/'+action,body)
deadline=fresh()['protectionExpiresAt']
future=(datetime.datetime.now(datetime.timezone.utc)+datetime.timedelta(days=1)).isoformat()
c=staff.call(path+'/followups',dict(version=fresh()['version'],kind='communication',outcome='no_answer',content='电话未接通',stage='following',nextFollowupAt=future,attachmentIds=[]))
assert fresh()['protectionExpiresAt']==deadline
staff.call(path+'/followups',dict(version=fresh()['version'],kind='quotation',outcome='contacted',content='报价',stage='following',nextFollowupAt=future,attachmentIds=[]),expected=400)
file=staff.call(path+'/attachments',dict(name='验收凭证.pdf',base64=base64.b64encode(b'%PDF-1.7 local acceptance').decode()))
assert staff.call(path+'/attachments/'+file['id']).startswith(b'%PDF-')
c=staff.call(path+'/followups',dict(version=fresh()['version'],kind='quotation',outcome='contacted',content='已交付报价',stage='following',nextFollowupAt=future,attachmentIds=[file['id']]))
assert fresh()['protectionExpiresAt']>deadline
print('PASS manual report, no-answer protection, evidence-required quotation and private download',flush=True)

# The source table is itself the durable outbox: a committed row is picked up even
# if the application crashed before enqueueing a separate in-memory task.
lead_id=sql("INSERT INTO jd_crm_integration.customer_leads(user_id,name,phone,wechat,demand_summary,source) VALUES (0,'自动来源验收','"+phone+"','验收微信','验证三类来源主档','workflow-acceptance');SELECT LAST_INSERT_ID();")
assert lead_id.isdigit()
sql("INSERT INTO jd_crm_integration.customer_lead_followups(lead_id,operator_id,operator_name,remark,status,created_at) VALUES ("+lead_id+",1,'演示顾问','旧跟进导入验收','contacted',DATE_SUB(NOW(),INTERVAL 10 DAY))")
for _ in range(90):
    linked=sql('SELECT customer_id FROM jd_crm_integration.customer_lead_crm_delivery WHERE lead_id='+lead_id)
    if linked:break
    time.sleep(1)
assert linked,'source delivery did not finish'
incoming=admin.call('/crm/customers/'+linked)['customer'];assert incoming['lifecycle']=='pending' and not incoming['protectionExpiresAt']
current=fresh();merged=admin.call('/crm/customers/'+linked+'/merge',dict(version=incoming['version'],targetId=cid,targetVersion=current['version'],reason='核对姓名、地址及联系电话为同一客户'))
assert merged['ownerUsername']==current['ownerUsername'] and merged['protectionExpiresAt']==current['protectionExpiresAt']
sql("UPDATE jd_crm_integration.customer_leads SET demand_summary='再次咨询验收' WHERE id="+lead_id)
for _ in range(90):
    checkpoint=sql('SELECT customer_id FROM jd_crm_integration.customer_lead_crm_delivery WHERE lead_id='+lead_id)
    if checkpoint==cid:break
    time.sleep(1)
assert checkpoint==cid
detail=admin.call(path);assert any(s['kind']=='miniprogram' and s['externalId']==lead_id for s in detail['sources'])
assert fresh()['ownerUsername']=='managed-2'
assert len([f for f in detail['followups'] if f['kind']=='source_history'])==1
assert not next(f for f in detail['followups'] if f['kind']=='source_history')['effective']
admin.call('/customer-leads/'+lead_id+'/status',{'status':'converted'},method='POST',expected=409)
print('PASS durable miniprogram delivery, same-source replay, merge retains ownership, legacy mutation blocked',flush=True)

deadline=fresh()['protectionExpiresAt'];post('transfer',dict(version=fresh()['version'],ownerUsername='managed-1',reason='主管交接验收',release=False))
assert fresh()['protectionExpiresAt']==deadline
staff.call(path,expected=403);staff.call(path+'/attachments/'+file['id'],expected=403)
case=post('requests',dict(kind='deal',content='验收成交确认',attachmentIds=[file['id']]))
assert fresh()['lifecycle']=='protected'
admin.call('/crm/cases/'+str(case['id'])+'/decision',dict(version=fresh()['version'],approve=True,reason='已核验演示凭证'))
assert fresh()['lifecycle']=='won' and not fresh()['protectionExpiresAt']
post('restore',dict(version=fresh()['version'],reason='验收恢复业务'))
assert fresh()['lifecycle']=='pending'
assert admin.call('/crm/notices')
assert not admin.call('/crm/push-status')['configured']
print('PASS transfer permission revocation, approved deal, restore and notices',flush=True)

# A separate artificial expired customer checks deadline enforcement before the job.
report['phone']='139'+str(time.time_ns())[-8:];report['name']='公海验收客户'
pool=staff.call('/crm/customers',report);pool_id=pool['id']
sql("UPDATE iyque_phase1.crm_customer SET protectionExpiresAt=DATE_SUB(NOW(),INTERVAL 1 SECOND) WHERE id='"+pool_id+"'")
old=admin.call('/crm/customers/'+pool_id)['customer']
staff.call('/crm/customers/'+pool_id+'/followups',dict(version=old['version'],content='到期补跟进',stage='paused',kind='followup'),expected=409)
claimed=admin.call('/crm/customers/'+pool_id+'/claim',{'version':old['version']})
assert claimed['customerNumber']==pool['customerNumber'] and claimed['ownerUsername']=='managed-1'
staff.call('/crm/customers/'+pool_id+'/claim',{'version':old['version']},expected=409)
print('PASS deadline boundary, reclaim preserves number, stale second claimant rejected',flush=True)
print(json.dumps({'result':'PASS','customerId':cid,'sourceLeadId':lead_id,'poolCustomerId':pool_id},ensure_ascii=False),flush=True)
