# 找设计师与设计师主页修订（2026-11-10）

## 行为修复

- 四入口沿用原图形，改为样式内嵌 SVG，不再引用打包排除的 assets/designer 目录。
- 擅长选项来自全部公开设计师实际填写的专业领域；擅长、所在地、排序在服务端分页前处理，列表和总人数使用同一条件。
- “全部”包含尚无公开作品但有设计师身份/专属资料的设计师；普通账号、隐藏设计师及仅有施工队/企业认证的账号不因认证或提现资格进入列表。作品分类仍要求存在匹配的公开作品。
- 原生地区选择器支持省、市、区及“不限”；设计师资料新增公开所在地填写、回填、保存和清空，未填写地区不作推测。
- 综合排序保留 ID 倒序，另外支持按公开作品总点赞或最近作品发布时间排序。同项顺序以用户 ID 稳定打平。
- 筛选文字和固定三角箭头独立布局，长文字省略；显示接口总人数，触底与“加载更多设计师”均可继续分页。失败保留已有列表、选项及页码。
- 新服务直接使用 popular/latest 双作品预览；旧列表缺少 work_previews 时，最多三路读取对应公开主页，按同类公开作品补全预览。单个补拉超时5秒，失败保留代表作；只有一件公开作品时不复制假作品。
- 列表和资料页头像复用 COS→CDN 地址处理；列表、主页和评价图加载失败分别回退，只影响失败头像。
- 主页移除对列表页样式的依赖，补齐资料卡、圆形头像、标签、操作区、作品、服务和评价独立样式。
- 共享导航 WXSS 由现有 SCSS 重新生成，修复旧 weui 类与当前 uni-nav 模板不匹配的问题；保留现有导航高度算法、返回逻辑和安全区计算。

## API

`GET /api/v1/miniprogram/designers` 保留 keyword/main_tab/page/page_size，新增：

| 参数 | 含义 |
| --- | --- |
| specialty | 实际专业领域关键词，字面值匹配 |
| region | 空格连接的省、市、区前缀，空为全国 |
| sort | comprehensive / popular / latest，非法值回退综合排序 |

返回 data 新增 `filters_applied: true`、`filter_options.specialties: string[]`；每个设计师新增 `designer_region: string`，保留 representative_work 和 work_previews。旧服务缺少筛选能力标识时，前端明确提示暂时无法筛选并恢复上次结果，不伪装成零结果。

`GET /api/v1/miniprogram/profile`、`GET /api/v1/miniprogram/profile/designer`、`GET /api/v1/miniprogram/designers/:id/homepage` 的 profile 中提供所在地。

`PUT /api/v1/miniprogram/profile/designer` 增加可选 `designer_region`：最多128个Unicode字符，空白规范为空格；省略/null保留原值，显式空串清空。旧客户端、后台和服务设置的更新不会擦除所在地。

## 数据库与发布顺序

先应用独立存储迁移提交，再更新服务端，最后更新小程序。启动时 `UserProfileModel.InitTable` 增加 `user_profiles.designer_region VARCHAR(128) NOT NULL DEFAULT ''`；只忽略 MySQL 1060（重复列），其他迁移错误正常返回。已有用户默认为空，不回填推测位置。没有引入依赖或修改认证/请求签名机制。

回退时先回退小程序和服务程序，保留新增列即可无损保留已填地区。若必须删列，先导出 user_id/designer_region，并确认所有读写代码已回退，再人工删列；不在程序回滚时自动删数据。

## 验证边界

本地全量小程序测试、全服务 Go 测试、类型检查、go vet、原生 WXML/WXSS 编译、依赖审计、独立代码审查和反向补丁检查留证于本目录 verification.json。迁移及 SQL 行为使用隔离数据库驱动/内存 SQLite fixture；没有执行线上迁移。

原生视觉包括完整开发项目真实列表/主页，以及长文本、缺图、评价、展开资料等隔离夹具。真实列表中单图设计师逐个主页核对确实仅一件公开作品，不能据此伪造第二件。夹具人数恢复测试只是范围回归，不代表线上设计师总数；人数范围与所在地功能须待服务端发布生效。本次仅提交代码，未推送、上传或部署，真机验收仍待完成。
