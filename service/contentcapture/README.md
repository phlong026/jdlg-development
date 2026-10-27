# Content capture worker

This process captures signed `mpvideo.qpic.cn` media URLs observed while an
isolated Chromium instance renders a WeChat article. It does not start Chrome,
attach to a user browser, reuse a user profile, read cookies, or download media.

## Required deployment gates

The HTTP server binds to either a numeric loopback address or the dedicated
Unix socket. Capture remains disabled unless all three runtime gates are
present:

- `CONTENT_CAPTURE_ENABLED=true`
- `CONTENT_CAPTURE_NETWORK_ISOLATED=true`
- `CONTENT_CAPTURE_CDP_DISCOVERY_URL=http://127.0.0.1:9222/json/version`

The discovery endpoint is recommended because Chrome changes the browser UUID
after every restart. The worker reads it immediately before each capture and
readiness probe, accepts only the exact `/json/version` path on a numeric
loopback address, bypasses HTTP proxy configuration, and verifies that the
returned WebSocket remains on the same host and port under
`/devtools/browser/<id>`. `CONTENT_CAPTURE_CDP_URL` remains available for a
legacy static browser WebSocket URL; discovery takes precedence when both are
set.

Authentication additionally requires `CONTENT_CAPTURE_SECRET` with at least 32
characters. The reverse proxy sends it as `Authorization: Bearer ...` and
terminates HTTPS before forwarding to `/v1/capture` through the local socket.
The same bearer protects `GET /health/ready`; readiness performs discovery,
opens CDP, and calls `Browser.getVersion`. It returns HTTP 503 if Chrome cannot
be reached. Only one capture or readiness probe runs at a time; a concurrent
request also returns HTTP 503.

Optional server-owned settings:

- `CONTENT_CAPTURE_LISTEN`, default `127.0.0.1:19091`. Production may use only
  `unix:/run/jdlg-content-capture/capture.sock`; other Unix paths and
  non-loopback TCP binds fail. The runtime directory must already exist and
  must not be a symlink. The worker creates the socket as mode `0660`.
- `CONTENT_CAPTURE_ALLOWED_HOSTS`, comma-separated extra domains required by a
  verified WeChat page revision. Clients cannot add hosts per request.

The worker never removes a pre-existing socket because it cannot safely infer
whether another process owns it. A service manager may remove a stale socket
in `ExecStartPre` only after the prior unit is confirmed stopped. The host Nginx
then proxies to this Unix socket; CDP remains reachable only at
`127.0.0.1:9222` inside the worker's isolated network namespace.

## Browser isolation is an external hard gate

The Chromium runtime must be a dedicated service with a temporary profile and
network namespace. Its firewall must allow only the required public Tencent
destinations and deny private, loopback, link-local and metadata networks.
Application DNS/IP checks reduce mistakes but cannot prevent DNS rebinding or a
browser process bypassing the worker, so they are not a substitute for the
firewall.

Do not use a normal user browser, an existing profile, `--no-sandbox`, or a
remote debugging endpoint reachable outside loopback. The CDP URL must point to
the browser target endpoint supplied by the isolated runtime. If the runtime is
not deployed, leave the capture gates disabled; the API returns HTTP 424.

The worker creates a new `BrowserContext` for every request, intercepts every
page request, rejects non-HTTPS/unknown/private destinations, and disposes the
target and context after success or failure. It never clicks login, captcha, or
verification controls. `format_id` ranks observed candidates only and is not a
claim about pixel resolution.
