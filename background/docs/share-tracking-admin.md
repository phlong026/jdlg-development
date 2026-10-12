# 分享访问记录后台

## Entry and scope

`/admin/share-tracking` is the single menu entry. The existing management cookie authenticates the page. `crm_only` may enter this route; this does not grant tracking access. `/api/v1/management/tracking/context` must explicitly return `can_view`, `can_manage` and authorized `owners`. Missing permission does not trigger any visitor/content queries.

The concrete request/response contract lives in `src/api/shareTracking.ts`. All responses retain the existing `{code,msg,data}` envelope. JSON writes and multipart upload carry `X-Tracking-Request: 1`; backend authorization and CSRF validation are mandatory, not replaced by hidden buttons.

## Query contract

All list and detail requests use the same `start` / exclusive `end` UTC timestamps derived from Asia/Shanghai natural days. Filters: `identity`, `intent`, `owner`, `q`, `sort`, `direction`, `cursor`, `limit`. Switching to the content view clears visitor search, identity and intent with an explicit notice. Time, owner and sorting remain; content-specific title search is not provided in this version. Sorting ties must be broken by stable server IDs. Cursor pagination must preserve the original query and snapshot, including late events.

`as_of` and `metric_version` are propagated from the listing into detail requests. The server remains the authority for the snapshot, filtered counts, identity, owner scope, rule evidence and quality flags. The frontend never computes visitor totals from the visible page.

For content-specific visitor sessions, `content_id` remains set when `include_related=true`; only sessions that visited that content may expand to their other pages. This flag must not turn into all historical sessions for the account.

## Authoring

Manual plain text and server-sanitized allowlisted rich text are implemented; arbitrary embeds and script attributes are removed. Existing public source cards are read-only metadata snapshots and do not promise historical body playback. Source URLs are retained for review, never fetched. Images and MP4 can be uploaded from the local device to the authenticated `/media` multipart endpoint with `file`, `rights` and explicit authorized `owner_id`; client limits are advisory and the server must validate actual bytes, type, dimensions, authorization and 10MiB image / 50MiB video limits. Authorized media can also be paged and selected.

Publishing requires a saved draft revision, server-generated preview token, explicit preview confirmation and stable idempotency key. Edits invalidate preview. Pending writes are single flight and prevent dismissal. Conflict/errors retain draft input. No fake QR or ordinary HTTPS short link is substituted when a verified share method is unavailable.

A successful upload by itself does not publish. Published updates first create a draft. Revocation is explicit, reasoned and confirmed; existing visits remain tied to historical versions. Raw images/video keep `object-fit: contain` and use only authorized server URLs. Draft media requests from `<img>` / `<video>` carry same-origin management cookies, not custom headers; the media GET handler must apply matching server scope and current WeCom session checks.

## Validation

- `npm run check`: TypeScript and route consistency.
- `npm run test:ui`: existing and new node regression suite.
- `npm run build`: production bundle.
- `python3 e2e/share_tracking.py`: run Vite first on local 127.0.0.1:5187. Uses explicitly synthetic API fixtures only; screenshots and results default to `/tmp/share-tracking-admin-qa`. Tests 320/375/390/768/1024/1440 widths, content ratio, lazy trajectory, preserved filters, retry/empty/permission states, and publishing with no enabled share method.

The current execution sandbox refuses Chromium socket creation, including the reviewed escalation retry. The browser script is prepared but visual QA is NOT passed here. Synthetic UI tests cannot substitute for authenticated backend integration, media authorization, real WeChat sharing, iOS/Android lifecycle/video testing, or production readiness.

## Visitor governance (ID-04 / IN-02 / PR-02)

Visitor details (both visitor and content paths) expose actions only for their explicit context capability: `can_crm`, `can_rules`, and `can_delete`; `can_manage` alone never grants these. CRM linking additionally requires an identified `u:` account, exact customer ID / business number and an explicit relationship basis. The server must independently verify upstream CRM business relationships and owner scope; the UI explains that absent verification means rejection and never matches nicknames. Existing mapped visitors can be explicitly unlinked.

Manual intent correction requires a reason and future expiry no more than 30 days away. Effective result and optional `intent.original_rule` / `intent.override` metadata are displayed separately; missing independent rule evidence is disclosed rather than reconstructed. This UI does not expose arbitrary rule-engine configuration.

Deletion requires a separate per-target confirmation panel, reason, and retyping the full visitor key. It distinguishes visitor data / mapping deletion from content revocation and does not claim immediate backup or exported-copy erasure. Only a server `status: completed` is treated as deletion success. Successful writes require closing and reopening refreshed details before another action; ambiguous/errors preserve inputs and advise checking state before retry. Pending requests are synchronously single-flight, bound to the exact target, protected from detail close/back, and late results are ignored after unmount. Dirty forms warn on close/back and browser unload.
