#!/usr/bin/env python3
"""Read only: display routing and capability metadata, never credential values."""
from pathlib import Path
import os
import subprocess

pid_list = subprocess.check_output(['pgrep', '-x', 'applinux'], timeout=5).decode().split()
print('PROCESS_COUNT', len(pid_list))
if len(pid_list) != 1:
    raise SystemExit('Expected exactly one application process')
proc = Path('/proc') / pid_list[0]
env = dict(x.split('=', 1) for x in (proc / 'environ').read_bytes().decode().split('\0') if '=' in x)
print('PROCESS', pid_list[0], os.readlink(proc / 'exe'), os.readlink(proc / 'cwd'))
for key in ['CONTENT_WORKBENCH_ENABLED', 'PROJECT_NOTES_ENABLED', 'CONTENT_IMPORT_ENABLED',
            'CONTENT_MEDIA_STORAGE', 'CONTENT_MEDIA_COS_PREFIX', 'SHARE_WEB_ENABLED',
            'PUBLIC_SHARE_ORIGIN', 'NOTIFICATIONS_ENABLED', 'BROWSE_NOTIFICATIONS_ENABLED',
            'OFFICIAL_ACCOUNT_ENABLED', 'OFFICIAL_ACCOUNT_DELIVERY_ENABLED']:
    print(key, env.get(key, 'UNSET'))
for name in ['html_api.jiadilingguang.com.conf', 'html_www.jiadilingguang.com.conf']:
    path = Path('/www/server/panel/vhost/nginx') / name
    print('NGINX', path)
    if not path.is_file():
        print('MISSING')
        continue
    for line in path.read_text().splitlines():
        if line.strip().startswith(('root ', 'alias ', 'location ', 'include ', 'proxy_pass ', 'server_name ')):
            print(line.strip())
for name in ['/www/wwwroot/jiadilingguang/index.html', '/www/wwwroot/jiadilingguang/admin/index.html',
             '/opt/jdlg-content-capture/main-service.env']:
    print('EXISTS', name, Path(name).is_file())
for unit in ['jdlg-content-capture-worker.service', 'jdlg-content-capture-browser.service', 'jdlg-content-capture-network.service']:
    result = subprocess.run(['systemctl', 'is-active', unit], capture_output=True, text=True, timeout=5)
    print('UNIT', unit, result.stdout.strip())
