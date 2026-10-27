# Content capture worker

This process captures signed `mpvideo.qpic.cn` media URLs observed while an
isolated Chromium instance renders a WeChat article. It does not start Chrome,
attach to a user browser, reuse a user profile, read cookies, or download media.

## Required deployment gates

The HTTP server always binds to a numeric loopback address. Capture remains
disabled unless all three runtime gates are present:

- `CONTENT_CAPTURE_ENABLED=true`
- `CONTENT_CAPTURE_NETWORK_ISOLATED=true`
- `CONTENT_CAPTURE_CDP_URL=ws://127.0.0.1:9222/devtools/browser/<id>` (or `wss`)

Authentication additionally requires `CONTENT_CAPTURE_SECRET` with at least 32
characters. The reverse proxy sends it as `Authorization: Bearer ...` and
terminates HTTPS before forwarding to `/v1/capture` on loopback.

Optional server-owned settings:

- `CONTENT_CAPTURE_LISTEN`, default `127.0.0.1:19091`; non-loopback binds fail.
- `CONTENT_CAPTURE_ALLOWED_HOSTS`, comma-separated extra domains required by a
  verified WeChat page revision. Clients cannot add hosts per request.

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
