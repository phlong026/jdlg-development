#!/usr/bin/env python3
"""Local UI regression with explicit synthetic API fixtures, never production traffic.
Run Vite on 127.0.0.1:5187 first. Screenshots are test evidence, not live customer data.
"""
import json, os
from urllib.parse import urlparse, parse_qs
from playwright.sync_api import sync_playwright
from PIL import Image, ImageDraw
BASE='http://127.0.0.1:5187'
OUT=os.environ.get('TRACKING_QA_OUT','/tmp/share-tracking-admin-qa')
os.makedirs(OUT,exist_ok=True)
for name,size in [('portrait',(360,640)),('landscape',(640,360))]:
 im=Image.new('RGB',size,'#cfe7df'); d=ImageDraw.Draw(im);d.rectangle((4,4,size[0]-5,size[1]-5),outline='#176759',width=5);d.text((20,30),'SYNTHETIC QA / '+name,fill='#153e38');im.save(OUT+'/'+name+'.png')
asset=lambda name:dict(media_id=name,kind='image',url=BASE+'/qa/'+name+'.png',width=360 if name=='portrait' else 640,height=640 if name=='portrait' else 360,mime='image/png',title='测试授权素材 '+name)
visitor=dict(visitor_key='visitor-fixture-01',nickname='自动化测试账号（虚构）',identity_state='identified',last_visit='2026-10-11T17:00:00Z',page_views=3,visible_ms=50000,session_count=2,intent=dict(level='medium',reasons=['测试夹具：两次独立会话与已确认操作'],rule_version='qa-v1',window_start='2026-09-12T16:00:00Z',window_end='2026-10-12T16:00:00Z'))
content=dict(content_id='content-qa-1',version=1,revision=1,title='测试内容：完整超长标题用于核验缩略图原始比例与窄屏换行，不能作为生产素材或客户数据',type='article',status='published',cover=asset('portrait'),media=[asset('landscape')],body='这是自动化测试正文。\n此夹具仅存在于测试请求拦截器。',source_url='https://example.test/own',rights='own',owner_id='owner-qa',page_views=3,visible_ms=50000)
snapshot=dict(as_of='2026-10-11T17:20:00Z',metric_version='qa-metric-v1',identified_count=1,anonymous_count=1)
pageview=dict(page_view_id='pv-qa-1',route_key='qa/detail',title='测试详情',content_id='content-qa-1',content_version=1,started_at='2026-10-11T17:00:00Z',visible_ms=50000,quality_flags=['incomplete'])
requests=[]; mode={'error':False,'empty':False,'permission':True}
def handle(route):
 path=urlparse(route.request.url).path;query=parse_qs(urlparse(route.request.url).query);requests.append((path,query))
 if path.startswith('/qa/'):
  route.fulfill(path=OUT+'/'+path.split('/')[-1],content_type='image/png');return
 data={};status=200
 if path.endswith('/management/me'): data=dict(id=90001,username='qa_local_only',scope='crm_only',crmEnabled=False)
 elif path.endswith('/tracking/context'):data=dict(can_view=mode['permission'],can_manage=True,owners=[dict(id='owner-qa',name='授权测试归属')])
 elif path.endswith('/tracking/media'):data={**snapshot,'items':[asset('portrait'),asset('landscape')]}
 elif '/tracking/visitors/' in path and path.endswith('/sessions'):data={**snapshot,'items':[dict(session_id='session-qa',started_at='2026-10-11T17:00:00Z',last_active_at='2026-10-11T17:01:00Z',entry_share_id='share-qa',source='share',page_views=[pageview])]}
 elif path.endswith('/events'):data={**snapshot,'items':[dict(event_id='event-qa',event_type='video_progress',occurred_at='2026-10-11T17:00:30Z',received_at='2026-10-11T17:02:00Z',content_version=1,current_time_ms=90000,video_watch_ms=10000,unique_played_ms=10000,video_duration_ms=100000,quality_flags=['late'])]}
 elif path.endswith('/preview'):data={'content':{**content,'status':'draft'},'preview_token':'qa-preview-proof'}
 elif path.endswith('/publish'):data={'content':content,'share_methods':{'message':'测试环境未接通小程序码，不生成假二维码'}}
 elif path.endswith('/revoke'):data={**content,'status':'revoked','revision':2}
 elif path.endswith('/tracking/contents') and route.request.method=='POST':data={**content,'status':'draft'}
 elif path.endswith('/tracking/contents/content-qa-1'):
  data={**content,**snapshot}
 elif path.endswith('/visitors'):
  if mode['error']:status=503
  data={**snapshot,'items':[] if mode['empty'] else [visitor,{**visitor,'visitor_key':'anon-qa-02','nickname':'','identity_state':'anonymous','intent':{**visitor['intent'],'level':'unknown'}}], 'next_cursor':None if query.get('cursor') else 'next-qa'}
 elif path.endswith('/tracking/contents'):data={**snapshot,'items':[{**content,'cover':asset('portrait')},{**content,'content_id':'content-qa-2','cover':asset('landscape')}]}
 route.fulfill(status=status,content_type='application/json',body=json.dumps({'code':0 if status==200 else 503,'msg':'测试服务暂不可用' if status!=200 else '', 'data':data},ensure_ascii=False))
with sync_playwright() as p:
 browser=p.chromium.launch(executable_path='/usr/bin/chromium',headless=True,args=['--no-sandbox'])
 ctx=browser.new_context();page=ctx.new_page();ctx.route('**/api/**',handle);ctx.route('**/qa/**',handle)
 errors=[];page.on('pageerror',lambda err: errors.append(str(err)))
 page.goto(BASE+'/admin/share-tracking');page.get_by_text('已识别账号 1 位',exact=True).wait_for()
 for width in [320,375,390,768,1024,1440]:
  page.set_viewport_size({'width':width,'height':850});page.screenshot(path=f'{OUT}/visitors-{width}.png',full_page=True)
  assert page.evaluate('document.documentElement.scrollWidth <= innerWidth'),f'overflow at {width}'
 page.set_viewport_size({'width':1024,'height':850})
 page.get_by_role('button',name='近3天',exact=True).click();page.get_by_label('身份',exact=True).select_option('anonymous');page.get_by_role('button',name='下一页',exact=True).click()
 page.get_by_role('tab',name='素材视角').click();page.get_by_text('客户筛选不适用于素材列表，已清除',exact=True).wait_for();page.get_by_role('button',name='查看完整标题、内容与访客').first.wait_for()
 for width in [320,375,390,768,1024,1440]:
  page.set_viewport_size({'width':width,'height':850});page.screenshot(path=f'{OUT}/contents-{width}.png',full_page=True)
  assert page.evaluate('document.documentElement.scrollWidth <= innerWidth'),f'content overflow at {width}'
 assert all(x=='contain' for x in page.locator('.st-content-card img').evaluate_all('(imgs)=>imgs.map(i=>getComputedStyle(i).objectFit)'))
 page.get_by_role('button',name='查看完整标题、内容与访客').first.click();page.get_by_role('dialog').wait_for();page.get_by_role('button',name='自动化测试账号（虚构）').click();page.get_by_role('button',name='展开轨迹').click();page.get_by_role('button',name='查看事件').click();page.get_by_text('有效观看墙钟').wait_for()
 page.set_viewport_size({'width':375,'height':660});page.screenshot(path=f'{OUT}/trajectory-375.png',full_page=True)
 assert page.evaluate('document.documentElement.scrollWidth <= innerWidth')
 page.keyboard.press('Escape');assert page.get_by_role('dialog').count()==0
 page.get_by_role('tab',name='客户视角').click();assert page.get_by_label('身份',exact=True).input_value()=='';assert page.get_by_text('第 1 页',exact=True).count()==1
 mode['error']=True;page.get_by_role('button',name='刷新记录').click();page.get_by_text('测试服务暂不可用',exact=True).wait_for();assert page.get_by_label('身份',exact=True).input_value()==''
 mode['error']=False;mode['empty']=True;page.get_by_role('button',name='重试，保留筛选').click();page.get_by_text('尚无符合条件的访问记录',exact=True).wait_for();page.screenshot(path=f'{OUT}/empty-375.png',full_page=True)
 page.get_by_role('button',name='＋ 创建素材分享').click();page.get_by_label('标题（必填，最多80字）').fill('自动化测试草稿');page.get_by_label('已有正文（纯文本，保留换行）').fill('已授权测试正文');page.get_by_label('我确认正文、封面与媒体具有使用权').check();page.get_by_label('用作封面').first.check();page.get_by_label('加入正文媒体').nth(1).check();page.get_by_role('button',name='保存草稿并预览').click();page.get_by_text('发布前预览 · v1').wait_for();page.get_by_label('已核对正文、媒体及归属，确认发布').check();page.get_by_role('button',name='发布并获取分享方式').click();page.get_by_text('测试环境未接通小程序码，不生成假二维码').wait_for();assert page.locator('.st-code').count()==0;page.screenshot(path=f'{OUT}/publish-unavailable-375.png',full_page=True)
 page.keyboard.press('Escape');mode['permission']=False;page.reload();page.get_by_text('当前账号尚未获得访问记录权限。',exact=False).wait_for();assert page.locator('.st-cards').count()==0
 assert not errors,errors
 open(OUT+'/result.json','w').write(json.dumps({'passed':True,'widths':[320,375,390,768,1024,1440],'requests':requests,'browser_errors':errors,'notice':'Synthetic API fixtures only; not live integration or device acceptance.'},ensure_ascii=False,indent=2))
 browser.close();print('PASS: local responsive browser regression and screenshots: '+OUT)
