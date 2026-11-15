# 页面整改工作任务清单与验收标准

2026-11-15 · 设计稿v3 · 32项任务 · 用户已批准实施。

## 当前结论

“文案辅助”在效果图统一改为“AI辅助”。用户已批准按本清单实施，并要求创建PR、合并主分支后部署。实施结果和各级验收证据见本目录交付记录。核对源码为 f33030a4ae29cdd8f294bc9c3d3b03555e8e75e0，仓库 /Users/honor.pei/Documents/订单项目/甲地灵光二期/开发版916/JDLG。

## 目标流程与边界

- 图文：新建→添加图片→标题与可选介绍→保存草稿或发布；预览可选。
- 视频：新建→添加主视频→标题与可选介绍→保存草稿或发布；封面与分享设置按需处理。
- 项目内部记录：已有项目→新建笔记→标题和非空正文→保存即完成。
- 首次建项目：项目名称+有效归属→创建后自动写笔记→保存；关联客户和其他资料后补。
- 发客户：发给客户→系统保存/取得固定版本/取得分享→发送预览→用户选择微信接收人。
- 客户跟踪：查看记录/按客户汇总→客户详情顶部承接；素材跟踪→素材访客→客户详情。

AI辅助只是入口命名统一，沿用当前生成、采纳、权限和计费能力，不扩大为新的AI图片/视频服务。项目笔记当前正文必填，不能为精简界面擅自允许媒体-only。图中已保存仅代表成功保存后的状态，不承诺新增自动保存。

## 已有能力，保留与回归，不重复开发

1. 创建项目后已自动进入新笔记；生成版本和创建分享后的自动跳转也已存在。
2. 内容预览已自动保存，发布后已自动打开作品阅读页。
3. 客户跟踪已有三Tab、单行日期、搜索/意向/来源组合筛选、按客户汇总切换。
4. 已有本人素材跨分享去重人数统计、客户聚合、转线索和关联项目入口。
5. 已有AI辅助生成/回填、私有素材、固定版本、旧分享、反馈/指定确认、长按删除、导入链接等能力。

不因本轮改版新增依赖、重写编辑器或重做AI服务。现有生成/冻结/分享API可复用，但需要正确编排。

## 三项必须按接口实情处理

- 项目创建后改绑客户：既有PATCH没有customer_ref字段，严格绑定会拒绝。必须先补可选字段和可信本人客户校验，再把关联入口从新建后移。旧POST仍支持客户详情发起的新项目。
- 素材访客：my-content-stats只有去重计数；my-visitors虽然参数解析接收内容字段，实际聚合未按素材过滤。必须补真实过滤和明细，不能只在前端传参数。
- 直接发布：服务端仍要求当前修订的preview_token。取消的是用户先进入预览画面的操作，保存/申请token/发布的服务端校验保留。

管理预览还需隔离客户统计；现有contentreader会执行跟踪，不能用任意preview=1假装已经关闭。冻结接口目前不幂等，原始分享token重放不再次返回，所以验收采用检查点和明确恢复，不承诺跨设备任意超时下完全无感恰好一次。

## 任务清单

优先级P0为功能正确性和关键流程，P1为页面结构及交付。每项状态均为待实施；文件组是预计最小范围，不表示每个列出文件都必须修改。


### 共用规范与AI辅助

| 任务 | 页码 | 修改内容 | 验收标准 | 文件组 / 依赖 |
| --- | --- | --- | --- | --- |
| G01 · P0 统一入口名称为AI辅助 | 03、05、09、17、18 | 将图文、文章、视频、项目笔记入口及相应辅助页显示统一为AI辅助；内部路由、source类型和接口标识不随文案更名。 | 页面、弹窗标题和无障碍标签一致；不遗留旧入口名；生成、采纳、返回原稿能力不变。 | F2、F3、F4；无 |
| G02 · P1 统一布局与完整状态 | 全部18页 | 沿用现有米白/棕金与原生字体；统一间距、按钮基线、列表分隔、点击区域；补加载、空态、失败及键盘状态。 | 375宽和390宽无横向溢出；键盘不挡输入和主要动作；关闭键盘后布局恢复；不会为视觉补造统计或封面请求。 | F1、F2、F4、F6、F7；G01 |
| G03 · P0 保留AI辅助上下文与计费边界 | 03、05、09 | 复用contentwriting和项目现有AI能力；仅主动生成才调用模型；返回时按账号/项目/笔记/修订/草稿签名校验采纳。 | 只打开不生成、不扣费；切账号或改稿后迟到结果不覆盖；取消不丢输入；失败可重试；不因改名增加新AI服务。 | F2、F3、F4、F11；G01 |

### 内容创作与图文/视频笔记

| 任务 | 页码 | 修改内容 | 验收标准 | 文件组 / 依赖 |
| --- | --- | --- | --- | --- |
| C01 · P1 精简新建内容入口 | 01、02 | 图文/视频为首要二选一；导入链接保留次级入口；文章/文件收更多；私密项目从项目进入。 | 两次明确选择内到空白编辑页；不再多一次平台/样式确认；原导入、文章和文件入口可找到且权限不扩大。 | F1、F11；G02 |
| C02 · P1 工作台卡片按状态显示动作 | 01 | 草稿继续编辑，已发布显示分享，其他动作收更多；保留已有搜索、类型与归属筛选能力。 | 草稿不被误分享为公开内容；已发布可复用有效分享；删除/撤回/失权后的卡片状态正确；不新增逐行网络查询。 | F1、F11；C01 |
| C03 · P1 空白图文页先图片后文字 | 17 | 先显示紧凑添加图片入口，再标题与可选介绍；不在无图片时展示独立封面或一串空素材说明。 | 空白进入无已保存假状态；添加/取消选图均正常；标题与有效图片校验沿用；介绍可空；无素材和无标题不会发布成功。 | F2、F11；C01 |
| C04 · P1 图文缩略图和单组选中操作 | 03、04 | 把大图编辑加逐图卡片改为横向缩略图；只显示选中图的排序/封面/注释/移除；预览复用现有呈现器。 | 9张图不同时出现9组操作；排序、封面和正文回显一致；移除封面后有合理回退；部分上传失败可处理，重试不重复归档成功项。 | F2、F11；C03 |
| C05 · P1 空白视频页先选择主视频 | 18 | 主视频添加为首个动作；未选视频不展示独立封面表单；选后沿用现有封面/缩略图能力，支持替换。 | 只选一个主视频；取消不清空原数据；默认封面可用或明确允许后补，不伪装已生成；失败保留标题和介绍；横竖视频不拉伸。 | F2、F11；C01 |
| C06 · P1 视频选填字段折叠 | 05、06 | 默认保留视频、封面、标题、介绍；私聊/朋友圈文案收分享文案，阅读样式收显示设置；不新增外部平台发布功能。 | 不填营销文案也可保存/发布合格视频；展开编辑、收起、重进数据不丢；旧videoLayout/shareCopy/momentsCopy兼容；样式明确仅作用于小程序阅读。 | F2、F11；C05 |
| C07 · P0 保存草稿与可选预览分开 | 03、05、17、18 | 底部统一保存草稿/发布；预览放轻量入口；保存只是保存，不触发公开或分享；沿用现有不完整视频草稿规则。 | 保存后仍为草稿且可恢复编辑；不调用发布；失败保留输入；只有真正成功才显示已保存；预览不自动公开；不新增默认自动保存。 | F2、F11；C03,C05 |
| C08 · P0 直接发布保留服务端预览校验 | 03—06、17、18 | 用户点发布时内部顺序保存最新稿→申请当前修订preview_token→publish；用户无需先打开预览画面；复用现有幂等键和发布后阅读页跳转。 | 不绕过preview_token或服务端内容检查；保存后改字/切账号则停止；连点不重复发布；token过期、409、超时有明确恢复；草稿、发布、撤回状态真实。 | F2、F11；C07 |

### 项目笔记与发送

| 任务 | 页码 | 修改内容 | 验收标准 | 文件组 / 依赖 |
| --- | --- | --- | --- | --- |
| P01 · P1 极简新建项目和默认归属 | 15 | 普通新建首屏仅项目名，多合法归属时保留紧凑选择；客户和资料后补；保持当前创建后自动进入新笔记。 | 名称为空不能创建；单归属不重复选择；无权限归属拒绝；创建成功只建一个项目；客户深链保留已有合法预选/创建关联，不丢上游客户意图。 | F4、F8、F11；P02,P03 |
| P02 · P0 补齐项目创建后修改客户关联 | 08、15 | 扩既有项目PATCH的可选customer_ref：未提供保留、空串解除、非空绑定/改绑；复用可信本人客户验证及当前revision检查，保留旧POST契约。 | 合法绑定/改绑/解除可持久化；他人客户/非法ref/旧revision不写；其他项目资料不丢；旧创建客户端可用；不把crm_id当customer_ref；不新增表。 | F8、F12；先写失败契约与权限测试 |
| P03 · P1 详情补绑/更换/解除客户入口 | 08 | 把原候选搜索、分页、更早窗口和取消能力放到项目资料；调用P02，保留现有客户详情发起新项目场景。 | 不填客户也可记笔记；事后可补绑、改绑、解除；取消不写入；切账号不串关联；摘要随当前关联更新，历史访问不被删；不捏造实际绑定时间。 | F4、F8、F11；P02 |
| P04 · P1 项目笔记编辑页减项 | 09 | 只留项目摘要、标题、正文、紧凑素材与AI辅助；单一可选预览入口；删除编辑区重复版本生成和历史卡片。 | 当前标题、非空正文校验不变；素材选填；长正文、图片、横竖视频正常；结构化只读原文不降级；不存在重复主按钮或默认多排管理操作。 | F4、F11；G02,G03 |
| P05 · P0 内部保存即完成 | 09 | 底部稳定保存笔记/发给客户；移除保存后下一步生成版本再分享的强提示；保存后重建当前表单基线。 | 新建/编辑保存只调保存接口，不冻结、不创建分享、不导航发送；只提示已保存；返回无假未保存警告；保存失败和晚回包不清掉新输入。 | F4、F11；P04 |
| P06 · P0 发给客户串联必要步骤 | 09、10 | 新增最小prepareSend编排；捕获账号/项目/笔记/实例/修订与签名；必要时保存，取得匹配固定版本和有效分享，再开既有客户预览。 | 首次新笔记一次准备动作到发送预览；未改稿可靠复用，改稿生成新快照；不串调带切页副作用的三个旧按钮方法；用户仍手动选择微信接收人。 | F4、F5、F11；P05 |
| P07 · P0 发送失败检查点与恢复 | 09、10 | 记录保存/冻结/分享各阶段成功结果；冻结超时先读回核对；分享重试用原key/expiry，token缺失提供明确恢复，不私自撤回旧链接。 | 保存成功不重复建笔记；冻结成功不重复冻结；结果不明不盲重试；分享/导航失败可恢复；切账号/项目、保存中改字、后台返回、401/403/409均不串发。 | F4、F8、F11、F12；P06 |
| P08 · P1 管理能力后置但可达 | 08、16 | 历史版本、项目成员、项目记录、导出及分享撤回收详情/更多；反馈和确认保留按版本查看，不成为创建或普通发送前置。 | 创建不要求邀请成员或指定确认人；正式方案确认仍检查有效成员与指定确认人；导出选择精确版本；撤回仅影响目标分享；返回恢复正确项目和笔记。 | F4、F5、F11、F12；P04 |
| P09 · P0 复用客户预览与旧版本规则 | 10 | 沿用既有projectnoteview；保持私有媒体、固定版本、旧分享、反馈/确认语义，界面按新主流程校正必要显示。 | 分享准备不显示客户已收到；修改草稿不改旧链接内容；撤回/删除/失权后拒绝；未登录按原规则处理；普通有效链接阅读不强制成为项目成员。 | F5、F4、F11、F12；P06,P07 |

### 客户跟踪与素材访客

| 任务 | 页码 | 修改内容 | 验收标准 | 文件组 / 依赖 |
| --- | --- | --- | --- | --- |
| T01 · P1 保留客户三Tab与单行筛选 | 11 | 复用现有客户查看/素材跟踪/分享记录；默认仍逐次记录；下拉显示当前查看方式；压缩技术窗口文案但保留分页能力。 | 无新增一排Tab或重复筛选；换模式保留日期/搜索/意向/来源；访问次数不当人数；更早范围实际可读，不能把当前分享段称为全历史。 | F6、F11；G02 |
| T02 · P1 访问卡片层级与双入口 | 11 | 统一客户、内容、来源、时间和指标；保留content_id/version/route_key；姓名进客户，封面/标题进可用的精确版本管理预览。 | 图文停留与视频观看含义清楚；进度不超过100%；累计次数口径保留；点标题不误开客户；无合法版本入口不显示假按钮；预览不污染客户统计。 | F6、F7、F10、F11；T07 |
| T03 · P0 客户承接动作前置 | 12 | 转为线索/承接状态与关联项目放身份指标下方；按服务端权限和当前状态显示，不新增没有数据支持的电话/微信动作。 | 授权客户可办理；匿名或无联系授权不能转线索；重复点击不重复建；失败可重试；已转状态真实；关联项目只用可信客户ref，异步已受理不冒充已分配跟进。 | F7、F11；G02 |
| T04 · P1 客户详情记录按需展开 | 12 | 项目动态默认最近2—3条、浏览记录分段加载；移除正文重复返回按钮；返回原入口保留筛选和滚动。 | 多记录不挤走顶部业务动作；查看更多不漏记录；公开浏览与私密项目动态仍分区；从素材/客户/分享进入都返回原位置；迟到响应不串上下文。 | F7、F6、F11；T03 |
| T05 · P0 补指定素材访客过滤 | 13、14 | 扩现有my-visitors及entries对route/content/version的实际过滤；复用MyContentStats归组和visitor_key去重；素材模式排除无浏览的纯项目关联客户。 | 同素材同版本跨两条本人分享的同客计1人、次数累加；不同版本/素材/同事数据不串；匿名识别后按既有身份解析去重；合集归组一致；游标绑定过滤范围；默认无筛选结果和全局意向规则不变。 | F9、F12；先写跨分享/版本/权限/分页失败测试 |
| T06 · P1 素材直达访客列表 | 13、14 | openMaterial进入受素材约束的访客视图，分享批次改为次入口；客户详情继续携带素材、版本、时间窗口和分享游标。 | 素材→访客→客户详情，无强制分享批次中间页；列表/详情人数次数一致；返回不丢范围；无访问空态正确；不以前端截取一页数据冒充全部访客。 | F6、F7、F11；T05 |
| T07 · P0 精确版本管理预览与统计隔离 | 11、14 | 核对并复用精确版本读取，增加受管理权限控制的只读预览模式或最小所需接口；不能仅加preview=1假定跟踪已关闭。 | 本人管理预览不增加客户访问/通知；普通客户访问仍正常计量；无权限不能借预览读私有内容；历史版本不变成最新稿；合集/子项路由正确；本人查看排除和匿名计量回归通过。 | F10、F6、F7、F11；T05中内容与版本上下文契约 |

### 验证

| 任务 | 页码 | 修改内容 | 验收标准 | 文件组 / 依赖 |
| --- | --- | --- | --- | --- |
| V01 · P0 先行为测试后实现并跑完整相关检查 | 全部 | 扩现有测试，不复制大型harness；同步TS/JS产物；运行相关完整JS、类型检查、原生WXML编译、Go相关包及全量/vet，做凭证和依赖扫描。 | 覆盖各任务正反例；零本次回归，不跳失败用例；未执行检查明确标注；新生成物与源一致；不新增无请求依赖；无真实凭据进提交。 | F11、F12；各功能增量 |
| V02 · P0 18页原生视觉与状态验收 | 全部 | 对照18页设计，在开发者工具和手机分别核对布局；补空态、加载、错误、长文本、键盘、不同媒体比例、权限不足。 | 所有主要动作可见可点，未被胶囊/安全区/键盘遮挡；无横向溢出和图片拉伸；折叠字段能找到；图片示意不当功能完成证据。 | F1、F2、F4、F6、F7；G02及对应页面实现 |
| V03 · P0 真实角色端到端验收 | 三条主链 | 用专用验收数据走公开创作保存/发布、私密项目保存/发送、客户跟踪承接/素材访客；记录真实设计师、接收客户和无权限账号结果。 | 设计师与客户看到同一固定版本；仅保存不公开不分享；删除/撤回不越权；统计与数据事实一致；内部计量不当实际送达；模拟器/真机/生产证据分别记录。 | F11、F12；C08,P09,T06,T07 |

### 文档与交付

| 任务 | 页码 | 修改内容 | 验收标准 | 文件组 / 依赖 |
| --- | --- | --- | --- | --- |
| D01 · P1 接口文档、修改清单与可回退提交 | 全部 | 同步customer_ref PATCH、素材过滤、保存/发布/发送流程文档；按逻辑提交中文Lore；记录实际文件、检查和回退方法。 | 清单与最终diff一致；旧契约兼容说明完整；代码revert可验证；无未经授权的数据迁移；不暂存现有无关部署文档。 | F8、F9及新增本轮规范文档；V01 |
| D02 · P1 兼容发布与上线核验 | 交付 | 确认实施后按同一发布批次先部署兼容后端，再上传小程序；分别核验开发/体验/审核/正式版本和公网健康，保存备份及回执。 | 新旧前端均可用；路由存在不代替登录实测；后端、上传、审核、正式发布分别记录；未通过真实验收不报生产完成；管理后台共用接口做兼容回归。 | 生产发布包、版本记录、回滚说明；V01,V02,V03,D01及相应发布授权 |

## 修改位置索引

### F1 内容工作台

- [miniprogram/miniprogram/pages/contentworkbench/contentworkbench.ts](../../../miniprogram/miniprogram/pages/contentworkbench/contentworkbench.ts)
- [miniprogram/miniprogram/pages/contentworkbench/contentworkbench.js](../../../miniprogram/miniprogram/pages/contentworkbench/contentworkbench.js)
- [miniprogram/miniprogram/pages/contentworkbench/contentworkbench.wxml](../../../miniprogram/miniprogram/pages/contentworkbench/contentworkbench.wxml)
- [miniprogram/miniprogram/pages/contentworkbench/contentworkbench.wxss](../../../miniprogram/miniprogram/pages/contentworkbench/contentworkbench.wxss)

### F2 图文与视频编辑器

- [miniprogram/miniprogram/pages/contentedit/contentedit.ts](../../../miniprogram/miniprogram/pages/contentedit/contentedit.ts)
- [miniprogram/miniprogram/pages/contentedit/contentedit.js](../../../miniprogram/miniprogram/pages/contentedit/contentedit.js)
- [miniprogram/miniprogram/pages/contentedit/contentedit.wxml](../../../miniprogram/miniprogram/pages/contentedit/contentedit.wxml)
- [miniprogram/miniprogram/pages/contentedit/contentedit.wxss](../../../miniprogram/miniprogram/pages/contentedit/contentedit.wxss)

### F3 AI辅助与回填

- [miniprogram/miniprogram/pages/contentwriting/contentwriting.wxml](../../../miniprogram/miniprogram/pages/contentwriting/contentwriting.wxml)
- [miniprogram/miniprogram/pages/contentwriting/contentwriting.ts](../../../miniprogram/miniprogram/pages/contentwriting/contentwriting.ts)
- [miniprogram/miniprogram/pages/contentwriting/contentwriting.js](../../../miniprogram/miniprogram/pages/contentwriting/contentwriting.js)
- [docs/phase3/project-note-writing-channel-20261030.md](../../../docs/phase3/project-note-writing-channel-20261030.md)

### F4 项目笔记主页面

- [miniprogram/miniprogram/pages/projectnotes/projectnotes-core.js](../../../miniprogram/miniprogram/pages/projectnotes/projectnotes-core.js)
- [miniprogram/miniprogram/pages/projectnotes/projectnotes.wxml](../../../miniprogram/miniprogram/pages/projectnotes/projectnotes.wxml)
- [miniprogram/miniprogram/pages/projectnotes/projectnotes.wxss](../../../miniprogram/miniprogram/pages/projectnotes/projectnotes.wxss)

### F5 客户阅读及发送预览（优先复用）

- [miniprogram/miniprogram/pages/projectnoteview/projectnoteview.ts](../../../miniprogram/miniprogram/pages/projectnoteview/projectnoteview.ts)
- [miniprogram/miniprogram/pages/projectnoteview/projectnoteview.js](../../../miniprogram/miniprogram/pages/projectnoteview/projectnoteview.js)
- [miniprogram/miniprogram/pages/projectnoteview/projectnoteview.wxml](../../../miniprogram/miniprogram/pages/projectnoteview/projectnoteview.wxml)

### F6 客户跟踪及素材列表

- [miniprogram/miniprogram/pages/sharevisits/sharevisits.ts](../../../miniprogram/miniprogram/pages/sharevisits/sharevisits.ts)
- [miniprogram/miniprogram/pages/sharevisits/sharevisits.js](../../../miniprogram/miniprogram/pages/sharevisits/sharevisits.js)
- [miniprogram/miniprogram/pages/sharevisits/sharevisits.wxml](../../../miniprogram/miniprogram/pages/sharevisits/sharevisits.wxml)
- [miniprogram/miniprogram/pages/sharevisits/sharevisits.wxss](../../../miniprogram/miniprogram/pages/sharevisits/sharevisits.wxss)

### F7 客户详情

- [miniprogram/miniprogram/pages/visitdetail/visitdetail.ts](../../../miniprogram/miniprogram/pages/visitdetail/visitdetail.ts)
- [miniprogram/miniprogram/pages/visitdetail/visitdetail.js](../../../miniprogram/miniprogram/pages/visitdetail/visitdetail.js)
- [miniprogram/miniprogram/pages/visitdetail/visitdetail.wxml](../../../miniprogram/miniprogram/pages/visitdetail/visitdetail.wxml)
- [miniprogram/miniprogram/pages/visitdetail/visitdetail.wxss](../../../miniprogram/miniprogram/pages/visitdetail/visitdetail.wxss)

### F8 项目客户关联后端与SDK

- [service/tracking/project_notes.go](../../../service/tracking/project_notes.go)
- [service/route/project_notes.go](../../../service/route/project_notes.go)
- [service/route/project_customer_link.go](../../../service/route/project_customer_link.go)
- [miniprogram/miniprogram/utils/phase3-api.ts](../../../miniprogram/miniprogram/utils/phase3-api.ts)
- [miniprogram/miniprogram/utils/phase3-api.js](../../../miniprogram/miniprogram/utils/phase3-api.js)
- [miniprogram/miniprogram/utils/customer-link-api.ts](../../../miniprogram/miniprogram/utils/customer-link-api.ts)
- [miniprogram/miniprogram/utils/customer-link-api.js](../../../miniprogram/miniprogram/utils/customer-link-api.js)

### F9 素材访客过滤后端

- [service/tracking/my_visitors.go](../../../service/tracking/my_visitors.go)
- [service/route/share_tracking_my.go](../../../service/route/share_tracking_my.go)
- [service/route/share_tracking.go](../../../service/route/share_tracking.go)
- [service/tracking/my_shares.go](../../../service/tracking/my_shares.go)

### F10 内容读取及预览（按核对结果最小修改）

- [miniprogram/miniprogram/pages/contentreader/contentreader.ts](../../../miniprogram/miniprogram/pages/contentreader/contentreader.ts)
- [miniprogram/miniprogram/pages/contentreader/contentreader.js](../../../miniprogram/miniprogram/pages/contentreader/contentreader.js)
- [miniprogram/miniprogram/pages/contentreader/contentreader.wxml](../../../miniprogram/miniprogram/pages/contentreader/contentreader.wxml)
- [service/route/content_workbench.go](../../../service/route/content_workbench.go)

### F11 现有前端行为测试

- [miniprogram/tests/phase3-content-studio.test.js](../../../miniprogram/tests/phase3-content-studio.test.js)
- [miniprogram/tests/phase3-content-editor-ui.test.js](../../../miniprogram/tests/phase3-content-editor-ui.test.js)
- [miniprogram/tests/phase3-content-writing.test.js](../../../miniprogram/tests/phase3-content-writing.test.js)
- [miniprogram/tests/content-ai-resume.test.js](../../../miniprogram/tests/content-ai-resume.test.js)
- [miniprogram/tests/project-note-media.test.js](../../../miniprogram/tests/project-note-media.test.js)
- [miniprogram/tests/private-project-notes.test.js](../../../miniprogram/tests/private-project-notes.test.js)
- [miniprogram/tests/private-project-invitations.test.js](../../../miniprogram/tests/private-project-invitations.test.js)
- [miniprogram/tests/private-project-packages-pages.test.js](../../../miniprogram/tests/private-project-packages-pages.test.js)
- [miniprogram/tests/customer-visit-feed.test.js](../../../miniprogram/tests/customer-visit-feed.test.js)
- [miniprogram/tests/sharevisits-display.test.js](../../../miniprogram/tests/sharevisits-display.test.js)
- [miniprogram/tests/customer-workflow-rectification.test.js](../../../miniprogram/tests/customer-workflow-rectification.test.js)
- [miniprogram/tests/visitdetail-lifecycle.test.js](../../../miniprogram/tests/visitdetail-lifecycle.test.js)
- [miniprogram/tests/content-reader-actions.test.js](../../../miniprogram/tests/content-reader-actions.test.js)
- [miniprogram/tests/content-reader-release.test.js](../../../miniprogram/tests/content-reader-release.test.js)

### F12 后端行为测试

- [service/route/project_customer_link_gate_test.go](../../../service/route/project_customer_link_gate_test.go)
- [service/route/project_customer_relationship_test.go](../../../service/route/project_customer_relationship_test.go)
- [service/tracking/project_notes_workflow_test.go](../../../service/tracking/project_notes_workflow_test.go)
- [service/tracking/project_notes_version_test.go](../../../service/tracking/project_notes_version_test.go)
- [service/tracking/my_project_customers_test.go](../../../service/tracking/my_project_customers_test.go)
- [service/tracking/my_visitors_test.go](../../../service/tracking/my_visitors_test.go)
- [service/tracking/my_visit_feed_test.go](../../../service/tracking/my_visit_feed_test.go)
- [service/route/share_tracking_my_test.go](../../../service/route/share_tracking_my_test.go)
- [service/route/content_workbench_test.go](../../../service/route/content_workbench_test.go)

## 实施顺序

1. G01—G03与P02：统一用语与规则，先补客户后绑定契约和失败测试。
2. C01—C07、P01/P03—P05/P08：简化创建、编辑、独立保存和管理入口，每个增量可独立验收。
3. C08与P06—P09：发布/发送编排，补迟到回包、冲突和部分成功恢复。
4. T05→T06、T07→T02，T01/T03/T04可独立推进：补真实素材访客与受控预览，再接前端。
5. V01—V03、D01—D02：完成检查、原生与真实角色验收，再进行相应已授权发布。

## 必须留证的验收场景

| 场景 | 必须观察到的结果 |
| --- | --- |
| U01 图文保存与发布 | 保存仅产生草稿；单独发布后阅读正常；介绍可空；选图/排序/封面一致 |
| U02 视频保存与发布 | 主视频和标题满足发布规则；营销文案可空；取消/替换/失败不丢稿；封面/横竖比例正确 |
| U03 已有项目内部记录 | 标题+正文保存成功；固定版本和分享数量不增加；不会提示还必须发送 |
| U04 新项目内部记录 | 不填客户及其他资料即可创建并进入笔记；保存完成；多归属按原权限正确处理 |
| U05 创建后客户关联 | 合法补绑/改绑/解除可用；非法客户和旧revision不写；客户深链原场景继续可用 |
| U06 一次准备发送 | 保存/冻结/分享顺序执行；只到发送预览；最终用户选择微信接收人 |
| U07 版本与失败恢复 | 修改后新版本正确且旧链接不变；每阶段失败可恢复；冻结不明结果不盲重试；token缺失不私自撤回 |
| U08 客户访问模式 | 默认逐次记录；按客户汇总不把访问条数当人数；日期、搜索、意向、来源与返回位置保持 |
| U09 客户承接权限 | 授权客户可转线索/关联项目；匿名或无授权不可转；重复请求幂等；状态不伪装实际跟进 |
| U10 素材跨分享访客 | 同素材同版本两条分享同一访客计1人；次数累加；异版本/同事/纯关联客户不串入 |
| U11 管理预览 | 看对应历史版本；不增加客户访问和通知；普通客户跟踪仍有效，权限不被预览参数绕过 |
| U12 18页与异常状态 | 手机和开发者工具分别留截图；空/忙/失败/键盘/长文/横竖媒体都可操作，无遮挡和横向溢出 |

## 完成定义

每项完成须有修改文件、行为检查和对应验收证据；截图/fixture不代替真实接口，上传不代替正式发布。全部相关检查通过，真实角色关键链路通过，已知缺口明确后才可报告对应阶段完成。

本清单为用户批准的实施基线。当前实现、自动化、原生验收、PR、合并和部署状态以本目录 VERIFICATION.md、REVIEW.md 和 DEPLOYMENT.md 的实际证据为准，未执行阶段不作完成声明。
