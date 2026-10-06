#!/usr/bin/env python3
"""把前端接口调用与后端管理端路由逐条比对。
用法：python3 backend_routes.py && node frontend_calls.mjs && python3 contract_diff.py
（脚本内路径为作者本机路径，使用前请改为本机仓库位置；输出在 /tmp/contract/）"""
import json
be = json.load(open('/tmp/contract/backend_routes.json'))
fe = json.load(open('/tmp/contract/frontend_calls.json'))
beset = {(r['method'], r['path']) for r in be}

def matches(m, p):
    for bm, bp in beset:
        if bm != m:
            continue
        a, b = bp.split('/'), p.split('/')
        if len(a) == len(b) and all(x == y or x == '{}' or y == '{}' for x, y in zip(a, b)):
            return True
    return False

bad = [c for c in fe['calls'] if c['path'].startswith('/api/') and not matches(c['method'], c['path'])]
print(f"前端调用 {len(fe['calls'])} 条，未在后端找到的 {len(bad)} 条")
for c in bad:
    print(f"  {c['method']:6} {c['path']}  <- {c['file']}:{c['line']}")
