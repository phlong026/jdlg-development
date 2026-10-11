# 小程序真实路由与接入盘点

基线 c4e761d7a54cead8b1f43bec9fe391d7bcf3db89，实际配置57个既有页面，另新增1个通用阅读页。表中均为静态代码审计，不是设备验收。统一适配器保留所有既有分享回调与返回值；采集开关及独立告知同意均默认关闭。

代码复核后，20个既有公开页面及1个新增正文页允许在同意、成功就绪、可见三项同时满足后采集；37个账户/私人业务/管理页面明确排除源页面轨迹（逐类依据见下文），其原生分享功能不删除。私有源页面分享至公开目标时仍保留公开目标归因；分享至排除目标保持原生行为、不附加追踪。模板广场、设计师列表、工具列表已采用真实接口成功信号：有效空数组允许页面PV，失败或畸形响应不允许。静态表单只表示打开表单，不采集输入、生成结果或业务成功。首页只表示固定首页已渲染，不推断推荐素材已读。尚无真机证据，不能宣称全部P0验收通过。

| 路由 | 类型 | 既有原生分享入口 | 原代码目标path/业务参数（原样保留） | 成功就绪依据 | 设备证据 |
|---|---|---|---|---|---|
| pages/index/index | home | onShareAppMessage | /pages/index/index | rendered | 未真机验证 |
| pages/customerservice/customerservice | excluded | 无 | 按原回调/无静态path | excluded-private-conversation | 未真机验证 |
| pages/my/my | excluded | onShareAppMessage | /pages/my/my | excluded-private-account | 未真机验证 |
| pages/template/template | list | onShareAppMessage | /pages/template/template | explicit | 未真机验证 |
| pages/aitools/aitools | catalog | onShareAppMessage | /pages/aitools/aitools | rendered | 未真机验证 |
| pages/aitooldetail/aitooldetail | detail | onShareAppMessage | 按原回调/无静态path | nonempty:tool | 未真机验证 |
| pages/aitoolworkbench/aitoolworkbench | form | onShareAppMessage | 按原回调/无静态path | nonempty:tool | 未真机验证 |
| pages/specadvisor/specadvisor | excluded | onShareAppMessage | /pages/specadvisor/specadvisor | excluded-private-conversation | 未真机验证 |
| pages/buildingcodedocs/buildingcodedocs | excluded | 无 | 按原回调/无静态path | excluded-private-documents | 未真机验证 |
| pages/newminnantool/newminnantool | form | onShareAppMessage | /pages/newminnantool/newminnantool?source=template_square | rendered | 未真机验证 |
| pages/distribution/distribution | list | onShareAppMessage | /pages/distribution/distribution | explicit | 未真机验证 |
| pages/voicecall/voicecall | excluded | onShareAppMessage | /pages/voicecall/voicecall | excluded-private-conversation | 未真机验证 |
| pages/login/login | excluded | onShareAppMessage | /pages/login/login | excluded-private-account | 未真机验证 |
| pages/passwordrecovery/passwordrecovery | excluded | 无 | 按原回调/无静态path | excluded-private-account | 未真机验证 |
| pages/phonebind/phonebind | excluded | 无 | 按原回调/无静态path | excluded-private-account | 未真机验证 |
| pages/privacy/privacy | excluded | onShareAppMessage | /pages/privacy/privacy?type=${policyType} | excluded-privacy-consent | 未真机验证 |
| pages/aigenerate/aigenerate | form | onShareAppMessage | /pages/aigenerate/aigenerate | rendered | 未真机验证 |
| pages/generateprogress/generateprogress | excluded | 无 | 按原回调/无静态path | excluded-private-generated-content | 未真机验证 |
| pages/inspirationdetail/inspirationdetail | detail | onShareAppMessage, onShareTimeline | /pages/inspirationdetail/inspirationdetail?id=${this.data.inspirationId} | nonempty:detail | 未真机验证 |
| pages/templatepreview/templatepreview | excluded | onShareAppMessage | /pages/template/template | excluded-private-generated-content | 未真机验证 |
| pages/settings/settings | excluded | onShareAppMessage | /pages/settings/settings | excluded-private-account | 未真机验证 |
| pages/aicost/aicost | form | onShareAppMessage | 按原回调/无静态path | rendered | 未真机验证 |
| pages/aicostresult/aicostresult | excluded | onShareAppMessage | 按原回调/无静态path | excluded-private-estimate | 未真机验证 |
| pages/utilitytools/utilitytools | list | onShareAppMessage | /pages/utilitytools/utilitytools | explicit | 未真机验证 |
| pages/utilitytooldetail/utilitytooldetail | detail | onShareAppMessage | /pages/utilitytooldetail/utilitytooldetail?id=${this.data.toolId 或 0}（按原回调） | nonempty:tool | 未真机验证 |
| pages/aivideo/aivideo | form | onShareAppMessage | /pages/aivideo/aivideo | rendered | 未真机验证 |
| pages/real/real | excluded | onShareAppMessage | /pages/Identityauthen/Identityauthen | excluded-private-identity | 未真机验证 |
| pages/report/landing | landing | onShareAppMessage | /pages/report/landing | nonempty:enabled | 未真机验证 |
| pages/report/questionnaire | excluded | 无 | 按原回调/无静态path | excluded-private-report | 未真机验证 |
| pages/report/preview | excluded | 无 | 按原回调/无静态path | excluded-private-report | 未真机验证 |
| pages/report/delivery | excluded | 无 | 按原回调/无静态path | excluded-private-report | 未真机验证 |
| pages/generatedetails/generatedetails | excluded | onShareAppMessage, onShareTimeline | /pages/aigenerate/aigenerate; /pages/aigenerate/aigenerate?prompt=${encodeURIComponent(prompt)} | excluded-private-generated-content | 未真机验证 |
| pages/Invitefriends/Invitefriends | excluded | onShareAppMessage, onShareTimeline | /pages/index/index?invite_code=${inviteCode} | excluded-private-rewards | 未真机验证 |
| pages/chat/chat | excluded | onShareAppMessage | /pages/chat/chat | excluded-private-conversation | 未真机验证 |
| pages/designerhome/designerhome | detail | onShareAppMessage | /pages/designerhome/designerhome?userId=${this.data.userId 或 0}（按原回调） | explicit + !isSelfHomepage | 未真机验证 |
| pages/myInformationmodification/myInformationmodification | excluded | onShareAppMessage | /pages/my/my | excluded-private-account | 未真机验证 |
| pages/templatesquaredetails/templatesquaredetails | detail | onShareAppMessage, onShareTimeline | /pages/templatesquaredetails/templatesquaredetails?id=${this.data.templateId 或 0}（按原回调） | nonempty:template | 未真机验证 |
| pages/myplan/myplan | excluded | onShareAppMessage, onShareTimeline | /pages/templatesquaredetails/templatesquaredetails?id=${planId}; /pages/myplan/myplan | excluded-private-generated-content | 未真机验证 |
| pages/myfavorites/myfavorites | excluded | onShareAppMessage | /pages/myfavorites/myfavorites | excluded-private-generated-content | 未真机验证 |
| pages/ordermanagement/ordermanagement | excluded | onShareAppMessage | /pages/ordermanagement/ordermanagement | excluded-private-finance | 未真机验证 |
| pages/wallet/wallet | excluded | onShareAppMessage | /pages/wallet/wallet | excluded-private-finance | 未真机验证 |
| pages/ruralvilladesign/ruralvilladesign | form | onShareAppMessage | /pages/ruralvilladesign/ruralvilladesign | rendered | 未真机验证 |
| pages/Parentchildcreativity/Parentchildcreativity | form | onShareAppMessage | /pages/Parentchildcreativity/Parentchildcreativity | rendered | 未真机验证 |
| pages/topupcenter/topupcenter | excluded | onShareAppMessage | /pages/topupcenter/topupcenter | excluded-private-finance | 未真机验证 |
| pages/allrounddesign/allrounddesign | form | onShareAppMessage | /pages/allrounddesign/allrounddesign | rendered | 未真机验证 |
| pages/release/release | excluded | onShareAppMessage | /pages/release/release | excluded-private-publishing | 未真机验证 |
| pages/release/designerworkpublish | excluded | 无 | 按原回调/无静态path | excluded-private-publishing | 未真机验证 |
| pages/buildingreplacement/buildingreplacement | form | onShareAppMessage | /pages/buildingreplacement/buildingreplacement | rendered | 未真机验证 |
| pages/checkin/checkin | excluded | onShareAppMessage | /pages/checkin/checkin | excluded-private-rewards | 未真机验证 |
| pages/Identityauthen/Identityauthen | excluded | onShareAppMessage | /pages/Identityauthen/Identityauthen | excluded-private-identity | 未真机验证 |
| pages/generatehistory/generatehistory | excluded | onShareAppMessage | /pages/generatehistory/generatehistory | excluded-private-generated-content | 未真机验证 |
| pages/lingshidetails/lingshidetails | excluded | onShareAppMessage | /pages/lingshidetails/lingshidetails | excluded-private-finance | 未真机验证 |
| pages/orderdetail/orderdetail | excluded | 无 | 按原回调/无静态path | excluded-private-finance | 未真机验证 |
| pages/messagecenter/messagecenter | excluded | onShareAppMessage | /pages/messagecenter/messagecenter | excluded-private-notifications | 未真机验证 |
| pages/adminlogin/adminlogin | excluded | onShareAppMessage | /pages/adminlogin/adminlogin | excluded-administration | 未真机验证 |
| pages/adminhome/adminhome | excluded | onShareAppMessage | /pages/adminlogin/adminlogin | excluded-administration | 未真机验证 |
| pages/withdrawcenter/withdrawcenter | excluded | onShareAppMessage | /pages/withdrawcenter/withdrawcenter | excluded-private-finance | 未真机验证 |

新增 pages/contentreader/contentreader：公开正文读取不依赖采集同意；必须成功读取已发布指定版本，完成可见渲染且通过采集告知后才计PV。content_id/content_version固定版本；st仅服务端不透明分享令牌。二维码scene仅通过GET /share-entry解析服务端绑定，读取同一通用页面，不按文章增加代码页。朋友圈只使用事先取得且实际目标一致的令牌，未取得时保留原分享不补造归因；卡片分享通过原promise兼容链附加令牌。二维码、单页、热冷启动、平台分享promise超时均待微信真机验证。

## 内容标识与成功信号

- 自有阅读：content_id + content_version；已发布指定版本正文读取成功并可见才就绪。
- inspirationdetail、templatesquaredetails、utilitytooldetail、aitooldetail、aitoolworkbench：原业务options.id映射为legacy:<稳定路由hash>:<id>；不声称旧业务已有不可变内容版本。
- designerhome：实际公开GET /api/v1/miniprogram/designers/{userId}/homepage。收到code=0且具有非空profile.name后才设置contentReady；默认占位profile、缺失ID、空响应、失败及刷新中均不算成功。isSelfHomepage为true时是本人管理模式，不采集源页面。原业务userId（兼容id）作为legacy内容标识；分享仍使用原userId参数、标题和封面。
- template：旧适配器误读不存在的list字段；实际页面是displayCards。本次以成功列表读取后contentReady代替字段猜测，成功内存缓存亦允许就绪；配置失败、双接口失败、畸形列表响应均不就绪。
- distribution、utilitytools：只有HTTP/业务成功且list为数组才就绪；有效空数组算打开列表，既有失败清空或旧数据残留不算成功。
- 首页、公共目录、表单：仅route_key，不把页面列出的每项素材或输入文字作为已读。

## 全部37个源页面排除项及实际代码依据

以下是明确的业务隐私边界，不是“缺少内容版本”或未审计的占位排除。统一适配器保留它们的既有分享回调；其中分享至公开目标可建立目标归因，源页面不会制造PV。改变这些边界须业务/隐私负责人确认采集目的，再增加适用的安全成功信号与验收。

- private-account（6）：my/my读取token、userInfo、余额和私人任务；settings/settings管理隐私设置、账号删除和密码；myInformationmodification/myInformationmodification修改账号/设计师/服务资料；login/login执行登录和短信认证；passwordrecovery/passwordrecovery重置密码；phonebind/phonebind绑定个人手机号。它们不是公开内容阅读页。
- privacy-consent（1）：privacy/privacy虽然包含公开静态法律文案，但同时是登录前协议/隐私同意流程（from=login及loginTip）；为避免把告知/同意操作解释为营销阅读，明确排除。原政策type及分享入口完整保留。
- private-conversation（4）：customerservice/customerservice收集客户需求并调用签名客服接口；chat/chat保存私人AI对话；specadvisor/specadvisor通过带登录签名的building-code/chat发送私人提问和上下文，knowledge_scope=public指知识库而非用户会话公开；voicecall/voicecall属于会话录音/静音界面，当前代码是占位功能，没有真实通话成功信号。均不采集会话页面轨迹或消息内容。
- private-documents（1）：buildingcodedocs/buildingcodedocs使用getAuthHeaders读取/上传/删除建筑规范文档并管理导入状态；属于登录后的文档管理，不是公开文档阅读。
- private-generated-content（6）：generateprogress/generateprogress轮询签名任务状态；generatedetails/generatedetails显示登录任务及个人生成结果，原分享指向公共aigenerate入口并保留prompt；generatehistory/generatehistory读取个人任务和本地草稿；myplan/myplan管理个人作品，指定作品分享仍指向公开templatesquaredetails；myfavorites/myfavorites显示当前用户收藏；templatepreview/templatepreview只解析调用方base64 data载荷及readonly标志，是发布前/个人作品预览，未证明发布状态或公共可读，原分享仍指向template广场。
- private-estimate（1）：aicostresult/aicostresult读取本地aicostEstimateResult（含14项个人建房条件和估价），没有公开结果地址；原分享公共预算表单及原图逻辑保留。
- private-finance（6）：wallet/wallet余额及流水、topupcenter/topupcenter充值、lingshidetails/lingshidetails资产明细、withdrawcenter/withdrawcenter提现、ordermanagement/ordermanagement订单列表、orderdetail/orderdetail订单及本地快照；均是财务/交易界面。
- private-identity（2）：real/real提交认证资料；Identityauthen/Identityauthen查看认证/缴费/提现相关状态；不采集实名及身份流程轨迹。
- private-report（3）：report/questionnaire保存个人建房需求草稿并生成报告；report/preview和report/delivery复用reader.ts，requireReportLogin保护个人报告、支付和下载。无原生分享入口。
- private-rewards（2）：Invitefriends/Invitefriends读取本人邀请码、邀请关系及奖励，仍保留原首页invite_code分享参数；checkin/checkin读取和提交个人签到/奖励。
- private-publishing（2）：release/release管理本人作品、发布范围和审核状态；release/designerworkpublish编辑/发布作品；不能把草稿、仅主页作品、已发布作品的管理列表统称公共阅读。
- private-notifications（1）：messagecenter/messagecenter为个人消息中心（当前空态占位），没有公开内容模型。
- administration（2）：adminlogin/adminlogin和adminhome/adminhome分别加载/admin/login与/admin/管理web-view，禁止把后台操作作为客户阅读轨迹。

## 自动回归与剩余验收

- tests/share-tracking.test.js共52项通过，包括4个实际页面JS回调的网络失败、HTTP失败、业务失败、畸形成功体、有效空态、缓存成功、本人主页隐私，以及失败无PV/可见成功重试仅1个PV。
- 新增TS/JS就绪转换一致性检查；scripts/build-tracking.js --check验证SDK源产物同步。未修改原分享回调、title、imageUrl、业务参数或按钮结构。
- 上线阻塞仍是微信真机双端：所有既有原生分享入口逐一发起/取消、接收端冷/热启动、朋友圈单页、返回后台、原异步分享promise时限及二维码scene。需要实际设备和部署后的测试环境；自动回归不是设备证据。
- 37项隐私排除必须作为验收范围例外显式交由业务/隐私负责人确认；若要求扩张采集，需要先定义允许记录的页面语义，不能仅打开collect开关。

## 真实公开点击接入（FR06 / UI10）

仅在以下已审计业务回调被调用时观察意图；使用目标路由的同一 `routes.binding` 命名空间生成 `target_content_id`。`component_key` / `action_key` 为固定代码键，不发送姓名、联系方式、搜索词、图片地址或任意 dataset。目标 ID 必须有效；SDK 仍要求采集启用、独立同意、源页面成功就绪及有效 PV。原回调的 this、参数、返回值、异常、跳转和图片预览保持不变；采集异常不会阻断业务。

| 公开源页面 | 实际回调 | 观测事件 / 目标 |
|---|---|---|
| index | onFeaturedCaseTap | content_click → templatesquaredetails |
| template | onCardTap | content_click → templatesquaredetails 或 inspirationdetail，按 targetType 分支 |
| utilitytools | onToolTap | content_click → utilitytooldetail |
| aitools | onToolTap | content_click → aitooldetail（允许实际工具 slug） |
| distribution | onWorkTap / onDesignerTap | content_click → templatesquaredetails 或 designerhome，匹配 publishScope/userId 分支 |
| designerhome | onWorkTap / onReviewRelatedTap | content_click → templatesquaredetails；本人管理模式、仅主页展示图片不生成模板目标 |
| templatesquaredetails | onDesignerTap / onPreviewImage | content_click → designerhome；有效图片数组产生源页面 image_open |
| inspirationdetail | onPreviewImage | 有效图片数组及点击图片产生源页面 image_open |

其余 12 个既有公开路由仍仅使用已定义生命周期/就绪/分享语义；未将普通表单、导航入口或工作台输入伪装为内容项点击。新增通用阅读页保留其显式正文事件。37 个私有源页面不接入这些动作，即使存在同名回调；不替换或拦截任何 wx 导航/预览 API。

列表曝光只产生源列表 PV，不批量生成素材读取；content_click 只代表点击意图，即使导航失败也不声称目标阅读成功。目标页必须独立成功加载、渲染且可见，才有目标 PV。image_open 代表请求打开大图，不推断平台预览成功或图片观看时长。首页仅有精选案例卡片点击，不采集客服二维码、联系按钮或上传输入。

新增 `tests/observed-content-actions.test.js` 运行实际页面 JS 回调及适配器，覆盖上述所有分支、非法/缺失 ID、私有/本人页面、仅主页图片、开关关闭/采集抛错 fail-open、回调契约、无列表曝光伪 PV；真实 SDK 验证源 PV 绑定、同意/就绪门槛和不生成目标 PV。自动测试不替代微信真机点击与跳转验收。
