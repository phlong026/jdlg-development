# 项目与成片采用链整改记录

日期：2026-11-13。分支：`fix/workbench-chain-20261113`。本记录只代表代码与本地自动化检查，未声称生产部署、小程序上传、真实角色或真机验收。

## 逐项交付

| 编号 | 结果与范围 | 行为验证 | 提交 |
| --- | --- | --- | --- |
| P01 | 通用视图清理保留按账号/项目/草稿隔离的持久挂起素材；重开后先过项目权限再接纳 | 新增正常卸载→新实例onLoad/onShow恢复回归先失败后通过；另一账号不接纳 | `9816bc3` |
| P02 | 邀请加入后进入独立成员只读目录及固定方案，不再误入创建人管理页；目录分页，媒体/视频逐次复验活跃成员 | Go成员与非成员/撤销成员测试；视频票据发出后撤销成员立即403；客户端不调用管理详情 | `549142b` |
| P03 | 已加入者重新读取有效邀请时，服务端返回核验后的project_id；未加入预览不暴露项目ID | 路由邀请前/后契约与前端进入入口测试 | `549142b` |
| P04 | 加入请求、错误回写及延迟导航绑定账号与页面代际 | A请求挂起→切B→A返回，不覆盖B项目/不导航旧项目 | `549142b` |
| P05 | 资料包单项素材失败占位，正文/反馈/确认保留；最多4项并发，404重验分享有效性，权限终态清内容 | 单图404仍读正文，可定向重试；返回后迟到版本不得重开；既有权限/媒体测试 | `484cc28`、`31ed64f`、`f08a9b2` |
| P06 | 单方案非成员合法回执的通知资格按当前有效分享及设计师授权核验，不再误要求成员 | 新增非成员回执资格先失败后通过；撤回分享、删项目及权限失效仍被既有检查拦截 | `484cc28` |
| P07 | 单方案提示改为有效链接持有人登录可看；确认人和资料包仍保留成员语义 | 分享提示及不自动创建成员回归 | `484cc28` |
| P08 | 资料包客户侧合并设计师回复并展示处理状态 | 单版反馈与resolutions合并回归；真实页面编译 | `484cc28` |
| G01前端 | 新建项目选填本人客户，昵称搜索、客户分页、更早分享窗口；客户详情路由引用先精确核验后预选，不将URL当授权 | 白名单选择、cursor/as_of/share_cursor保持、非法ref不选、路由有权/无权回归 | `cc0464e`；基础CustomerRef字段在`484cc28`，最终可信校验由主代理/客户通道集成（主代理`2d6ca8d`、客户通道`0836a46`） |
| A01 | 按账号/媒体/采用类型持久保存采用身份，失败重试/重开复用原草稿键；内容采用按原归属恢复，终态拒绝由服务端解除记录 | 响应丢失→新API实例重试同键；目标变更阻断；跨号隔离；原owner撤权仍可恢复原请求；旧账号上下文不得以新账号发请求或解除新操作锁 | `2d478b9`、`9e23d9d` |
| A02 | 从成片明确选择本人项目→分页笔记→服务端规范化块位置→确认插入→精确笔记编辑；plain转换共享算法保留原正文/媒体/封面；article_v2原正文只读预览与标题编辑；新采用拒绝归档写入；幂等返回当前修订 | 新入口行为测试先失败后通过；插入→保存→冻结→有效分享读取保留视频/封面；失效修订/跨号/删除/归档拒绝；取消与迟到选择器不回填；重试与翻页区分 | `63a5ff5`、`cc0464e`、`2d478b9`、`9e23d9d` |

成员只读入口显示固定方案，客户反馈和指定业主确认仍从设计师发出的单方案/资料包入口执行；没有放宽项目创建人的管理域。旧R01归档全链策略不在本次扩大改造，新增采用目标和实际写入已补归档守卫。

## 本地检查证据

- 七个相关小程序测试文件：**238通过、0失败、0跳过**。包括 `private-project-notes.test.js`、`private-project-invitations.test.js`、`private-project-packages-pages.test.js`、`share-member-hint.test.js`、`ai-video-studio-adoption-flow.test.js`、`ai-video-studio-contract.test.js`、`ai-video-studio.test.js`。最后完整输出暂存 `/tmp/jdlg-projects-final-tests-20261113.txt`。
- `CGO_ENABLED=0 go test ./tracking ./route` 通过；`CGO_ENABLED=0 go vet ./tracking ./route` 通过。含现有集成回归与新增采用/成员授权行为；外部环境门禁由主代理完整测试记录分别说明。
- 真实WXML编译器 `/Applications/wechatwebdevtools.app/Contents/Resources/app.asar.unpacked/node_modules/wcc-exec/wcc` 编译项目、邀请、单方案阅读、资料包阅读、AI视频详情及共用模板，退出码0。输出 `/tmp/jdlg-projects-wxml-20261113.js`。不是模拟器或真机视觉验收。
- 修改JS执行 `node --check`，`git diff --check` 通过；新utility保留同源 `.ts/.js`；未新增依赖、凭据或数据库迁移。
- 安全路径沿用既有鉴权分组、来源媒体归属、项目创建人管理及成员资格校验；媒体继续使用现有短时票据与逐次权限检查，复用[Go官方ServeContent语义](https://pkg.go.dev/net/http#ServeContent)，没有另造绕过鉴权的裸媒体链接。

## 新增接口

- `GET /api/v1/miniprogram/project-notes/member-projects/:id`：活跃成员的固定版本目录，`limit/cursor`分页；不含可编辑草稿。
- `GET /api/v1/miniprogram/project-notes/member-projects/:id/versions/:version`：成员精确固定版本。
- `GET /api/v1/miniprogram/project-notes/member-projects/:id/versions/:version/media/:media`：成员媒体下载。
- `POST /api/v1/miniprogram/project-notes/member-projects/:id/versions/:version/media/:media/playback`：短时视频票据；流式读取时复核成员资格。
- `GET /api/v1/miniprogram/ai-video-studio/adoption-targets/projects/:project/notes/:note`：本人可编辑笔记与实际规范化块位置，归档/删除/越权拒绝。
- 采用既有 `POST /media/:id/adopt/project-note` 现在支持安全plain→article_v2转换；最终修订号和插入位置仍由事务校验。

## 回滚与未验证

按后续采用恢复→阅读重试→A02→G01前端→A01→分享修复→成员入口→P01的依赖顺序回退本通道提交（成员媒体重验补充`f08a9b2`位于最前）；没有删除数据库或媒体事实。回退后的旧程序不能编辑新插入块正文时，应继续保留原排版数据，勿强制降为plain。持久采用请求仍可由具有相同契约的服务端安全重试。

未验证：真机选择/键盘/滚动与弱网体验、真实多账号角色UAT、真实服务号投递、供应商付费成片采用、真实生产客户数据绑定。本轮没有执行上传、部署、外发或付费。主代理另行负责完整小程序/Go检查、集成审查与交付门禁。


## 独立审查补正

- `9e23d9d`：内容采用恢复原owner/标题；发起账号+独立操作身份覆盖context、归属选择、采用及锁收尾，旧A回调不能以B身份提交或解除B锁；选择器重新加载与data-more分页区分。
- `f08a9b2`：成员阅读404重验按project/version身份，分享阅读仍按share身份；新增成员图404隔离与真实403撤权清页回归先失败后通过。
- 本通道业务代码已冻结，可由主代理继续完整集成检查；后续仅响应实际回归失败，不扩大旧R01归档策略范围。
