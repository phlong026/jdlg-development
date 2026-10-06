#!/usr/bin/env python3
"""从 Go 源码还原管理端路由表：METHOD PATH（路径参数统一为 {}）。"""
import re, sys, json, glob, os

ROOT = '/Users/honor.pei/Documents/订单项目/甲地灵光二期/开发版916/JDLG/service/route'
BASE = '/api/v1/management'

func_re = re.compile(r'^func\s+(?:\([^)]*\)\s*)?(\w+)\(([^)]*)\)', re.M)
group_re = re.compile(r'(\w+)\s*:?=\s*(\w+)\.Group\(\s*"([^"]*)"\s*\)')
route_re = re.compile(r'(\w+)\.(GET|POST|PUT|PATCH|DELETE)\(\s*"([^"]*)"')
root_param_re = re.compile(r'(\w+)\s+\*gin\.RouterGroup')

def norm(p):
    return re.sub(r':\w+', '{}', p)

routes = []
for path in sorted(glob.glob(os.path.join(ROOT, '*.go'))):
    if path.endswith('_test.go'):
        continue
    src = open(path, encoding='utf-8').read()
    funcs = list(func_re.finditer(src))
    for i, m in enumerate(funcs):
        name, params = m.group(1), m.group(2)
        is_mgmt = ('Management' in name and name.startswith('Register')) or name == 'registerWecomLogin'
        if not is_mgmt or 'CRMManagement' in name:
            continue
        rp = root_param_re.search(params)
        if not rp:
            continue
        body = src[m.end(): funcs[i + 1].start() if i + 1 < len(funcs) else len(src)]
        prefix = {rp.group(1): BASE}
        for g in group_re.finditer(body):
            var, parent, sub = g.groups()
            if parent in prefix:
                prefix[var] = prefix[parent] + sub
        for r in route_re.finditer(body):
            var, method, sub = r.groups()
            if var in prefix:
                full = (prefix[var] + sub).replace('//', '/')
                if full.endswith('/') and len(full) > 1:
                    full = full[:-1]
                routes.append({'method': method, 'path': norm(full), 'file': os.path.basename(path), 'func': name})

# 也把 management.go 里的 auth 分组（登录/me/logout 等）按同样方式处理已覆盖；去重
seen, out = set(), []
for r in routes:
    k = (r['method'], r['path'])
    if k not in seen:
        seen.add(k); out.append(r)
json.dump(out, open('/tmp/contract/backend_routes.json', 'w'), ensure_ascii=False, indent=1)
print(f'backend routes: {len(out)}')
