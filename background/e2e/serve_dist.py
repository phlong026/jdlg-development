#!/usr/bin/env python3
"""把生产构建产物挂在 /admin/ 下，/api 代理到本地后端；未知的 /admin/* 回退到 index.html（SPA）。
用来在"真正上线的那份构建"上跑 e2e/smoke.py。仅限本机。

  npx vite build --outDir /tmp/admin-dist --emptyOutDir
  python3 e2e/serve_dist.py /tmp/admin-dist 5181 http://127.0.0.1:28180
  E2E_WEB=http://127.0.0.1:5181 python3.11 e2e/smoke.py
"""
import http.server
import mimetypes
import os
import sys
import urllib.error
import urllib.request

DIST, PORT, BACKEND = sys.argv[1], int(sys.argv[2]), sys.argv[3].rstrip("/")
if not BACKEND.startswith(("http://127.0.0.1", "http://localhost")):
    sys.exit("拒绝运行：后端必须是本机地址")


class Handler(http.server.BaseHTTPRequestHandler):
    def _send(self, status, body, headers):
        self.send_response(status)
        for k, v in headers.items():
            self.send_header(k, v)
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def _proxy(self):
        length = int(self.headers.get("Content-Length") or 0)
        data = self.rfile.read(length) if length else None
        req = urllib.request.Request(BACKEND + self.path, data=data, method=self.command)
        for k in ("Content-Type", "Cookie", "Authorization", "X-CRM-Request"):
            if self.headers.get(k):
                req.add_header(k, self.headers[k])
        try:
            resp = urllib.request.urlopen(req, timeout=30)
            status, body, hdrs = resp.status, resp.read(), resp.headers
        except urllib.error.HTTPError as e:
            status, body, hdrs = e.code, e.read(), e.headers
        out = {k: v for k, v in hdrs.items() if k.lower() in ("content-type", "set-cookie", "cache-control")}
        self._send(status, body, out)

    def _static(self):
        rel = self.path.split("?")[0].removeprefix("/admin").lstrip("/")
        path = os.path.normpath(os.path.join(DIST, rel))
        if not path.startswith(os.path.abspath(DIST)) or not os.path.isfile(path):
            path = os.path.join(DIST, "index.html")  # SPA 回退
        ctype = mimetypes.guess_type(path)[0] or "application/octet-stream"
        with open(path, "rb") as f:
            self._send(200, f.read(), {"Content-Type": ctype})

    def handle_any(self):
        if self.path.startswith("/api/"):
            self._proxy()
        elif self.path == "/admin" or self.path.startswith("/admin/"):
            self._static()
        else:
            self._send(404, b"not found", {"Content-Type": "text/plain"})

    do_GET = do_POST = do_PUT = do_PATCH = do_DELETE = handle_any

    def log_message(self, *a):
        pass


http.server.ThreadingHTTPServer(("127.0.0.1", PORT), Handler).serve_forever()
