# 小红书图文公开页导入证据与边界（2026-10-27）

## 已验证的真实公开页

本轮只做匿名只读验证，没有使用 Cookie、登录态、AppSecret、付费解析请求或批量采集。

- 公开 feed：`https://www.xiaohongshu.com/explore`
- 图文笔记：`https://www.xiaohongshu.com/explore/6a9206bc000000001e0160eb?xsec_token=ABDf7_xKlR3V8Dje84mYHPe1WajVdiegmUy2LvQTOOtCw%3D&xsec_source=pc_feed`
- 图文原生 ID：`6a9206bc000000001e0160eb`
- 视频笔记：`https://www.xiaohongshu.com/explore/6a77dfe10000000029032ba0?xsec_token=ABz6eqgPea4GipiUrQ9FA4zdhu7i0pcFijBn55-PpgM-w%3D&xsec_source=pc_feed`
- 视频原生 ID：`6a77dfe10000000029032ba0`

这些链接来自小红书匿名公开 feed，`xsec_token` 可能过期，不能作为长期回归依赖。自动化测试使用脱敏的本地 HTML fixture，只复现已观察到的字段结构，不保存真实作者的图片、完整正文或媒体地址。

同一图文样本经匿名 GET 实测：同时保留 `xsec_token` 和 `xsec_source=pc_feed` 时返回完整 `noteDetailMap`；仅保留 `xsec_token` 时详情映射为空。因此公开页抓取会保留这两个分享参数，豆一豆视频请求仍沿用既有规范链接，不扩展未知供应商字段。

## 已观察到的页面契约

公开详情页在 `window.__INITIAL_STATE__` 中提供：

```text
note.noteDetailMap[noteID].note
  noteId
  type
  title
  desc
  user.nickname
  imageList[]
    width / height
    urlDefault / urlPre / url
    infoList[] { imageScene, url }
```

图文样本的 `type` 为 `normal`，`imageList` 有 3 项，顺序与页面展示顺序一致。每项同时提供 `WB_PRV` 和 `WB_DFT`；`urlDefault` 与 `WB_DFT` 对应，测试因此选择 `WB_DFT`，缺失时才使用 `urlDefault`，不使用低清预览 `WB_PRV`。公开页当前返回 `http://*.xhscdn.com/...`，导入端只接受 `xhscdn.com` 及其子域，校验后统一升级为 HTTPS。

视频样本的 `type` 为 `video`，同时也有 1 张 `imageList` 封面。因此实现必须先判断 `type`：不能把视频封面误判成图集。只有明确的 `video` 才交给豆一豆视频解析器一次；页面风控、响应不完整、未知类型和图集任一图片无效都直接失败，不能盲目触发可能计费的解析。

## 与爆款视频复刻项目的关系

已核对 `/Users/honor.pei/Documents/订单项目/乡墅爆款短视频复刻/server/app/viral_link.py` 及其测试。现有真实实现只验证了豆一豆返回的 `video`、`audio`、`note_id`、`title/text`、`author`、`cover` 等视频字段，没有图集字段样本。因此本模块没有增加猜测性的 `image_list` 或 `images` 供应商协议；豆一豆继续只负责视频。

## 自动化 fixture 覆盖

- 三图顺序、重复图片仍按原顺序保留；
- 完整 `desc` 进入 `Source.Text`，标题和作者独立保留；
- 原生 24 位十六进制 ID 与请求 URL、`noteDetailMap` 键和 `noteId` 三方一致；
- `xsec_token` 与 `xsec_source` 规范化后原样保留；
- 任一图片缺失、非法地址、未知类型、无 initial-state、超过 50 张均失败关闭；
- 视频页只调用一次视频解析器；页面风险或未知结果不调用付费解析；
- 页面获取不发送 Cookie 或 Authorization，并限制主机、路径、重定向、响应类型和响应大小。

## 尚未形成的生产证据

- 未使用甲第生产凭据或真实用户 Cookie；
- 未调用真实豆一豆付费接口；
- 未用用户提供的当期小红书分享链接完成 COS 转存与真机验收；
- 小红书公开页字段和临时 CDN 地址可能变化，生产验收必须使用当期合法分享链接，并保留失败关闭行为；
- `WB_DFT` 是公开网页提供的最高默认展示资源，不等于已证明的源文件原图，也不声明像素级去水印。
