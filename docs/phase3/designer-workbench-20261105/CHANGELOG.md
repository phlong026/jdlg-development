# 设计师工作台逐页整改：提交前修改清单

整理时间：2026-11-05。基线 `5d0441f`，实施分支 `feat/designer-workbench-rectification-20261105`。此清单在业务代码提交前生成；部署与上传结果另见发布记录。

## 页面与流程修改

| 页面/任务 | 具体修改 | 主要部位 |
| --- | --- | --- |
| 我的工作台 U01 | 认证设计师四入口2×2；压缩账户区；移除重复说明和独立分享横条；更多仍保留作品发布/素材入口，其他角色沿用权限 | my/my.wxml、my.scss |
| 项目列表/创建/详情 U02–U04 | 项目名称、阶段、更新时间优先；笔记/版本/资料/成员/记录层级收敛；保留归属选择与未保存保护 | projectnotes/projectnotes.wxml、wxss |
| 项目资料 U05 | 地点、面积、层数、风格补常驻标签及平方米/层单位 | projectnotes.wxml |
| 笔记 F04/F05/U06 | 编辑按钮2×2；复制/删除收入更多；新笔记取消后不清掉项目已有版本与分享状态；各页内视图回顶，返回列表恢复位置 | projectnotes-core.js:startNewNote、scrollPage、handleBack |
| 版本/成员/记录 U07–U09 | 分享主操作/Word次操作；成员和邀请行紧凑化；记录、反馈、确认时间本地格式 | projectnotes-core.js、wxml、wxss |
| 私密资料包/接收页 U10–U11 | 选择行全宽、正文与反馈分区、操作紧凑；沿用收件人、固定版本、撤回和确认机制 | projectpackage、projectpackageview、projectnoteview 的样式 |
| 内容创作 C01–C03 | 我的内容/合集/资料库稳定导航；五项新建菜单；移出项目待办与无关请求；分类发送后端type并重置分页快照；草稿不堆零指标 | contentworkbench TS/JS/WXML/WXSS |
| 内容编辑 C04–C06 | 封面独立选择；保留块编辑、文档/原排版；设置和素材管理收起；发布后操作上移；未保存修改不误跳旧发布版本 | contentedit TS/JS/WXML/WXSS |
| 文案与导入 C07–C08 | 精简提示、优化生成前后按钮；导入停用态可返回；保留版权确认、uncertain/失败/重试与轮询控制 | contentwriting、contentworkbench |
| 阅读 C09 | 精简版本与技术说明；微信分享优先；保留分析选择、文档/媒体失败与撤回状态 | contentreader WXML/WXSS |
| 合集 C10–C11 | 低频操作收入更多，回调复核账号/版本/状态；成功态查看分享；预览正文滚动与操作分离；正文底部留足安全区 | projectcollections、collectionreader |
| 资料库 C12 | 搜索框/按钮宽度合理；同一搜索在途合并；新查询可替换旧查询；离开返回可重新读取；保留引用与选择器职责 | nativereferences TS/JS/WXML/WXSS |
| 客户 F01–F03/T01–T03 | 原生URL参数解码一次，修复u%3A4被再次编码导致404；同访客+as_of+时间窗进入详情；顶层字段正确映射查看/内容/停留；时间/意向/排序筛选；全宽卡片 | sharevisits、visitdetail TS/JS/WXML/WXSS |
| 素材/分享 T04–T05 | 真实业务标题代替route；素材热度按去重访客排序；后端真实标题搜索与content+version筛选先于分页；封面用受保护下载且失败降级；分享Tab直达兼容 | sharevisits、tracking/my_shares.go、my_visitors.go |
| 本人转线索 T06 | 新增本人范围签名POST接口；先核验本人分享证据；拒绝匿名/B端/未知身份；已有线索不泄漏管理域ID；单进程同用户串行 | route/share_tracking_lead.go、my.go、tracking.go、phase3-api |
| 消息中心 N01–N02 | 首屏只读站内消息/未读数；提醒设置按需读取；设置错误不污染列表；本地日期分组；已读失败不阻断安全路由；保留私密通知白名单 | messagecenter TS/JS/WXML/SCSS |
| 服务号状态 F06/N03 | 配置禁用时状态查询无需绑定存储；启用但存储不可用仍报错；站内消息与外发保持独立。真实服务号外发需模板、接收身份、订阅及微信回执联验 | route/official_account.go、已有notification/officialaccount实现 |
| 设计师主页 D01–D02 | 编辑/预览明确入口；作品单列；作品/服务分区；灵石单位 | designerhome、myInformationmodification |
| 作品与工具 D03–D04 | 精简作品发布统计副标题与发布说明；保留审核/主页与广场范围；对话/AI设计/生成记录/客服继续复用原流程 | release WXML；D04为只读核查及回归 |
| 管理后台 | 本轮业务页面源码沿用现有实现；修正内容分析测试缺失传播请求fixture，避免测试一直等待；生产重新构建部署 | background/scripts/ui-content-analytics-states.test.mjs |

## 已有能力与验收边界

- 公开内容/合集、设计师作品、项目私密资料包继续使用各自发布与权限链路。
- 本次代码不需要数据库结构迁移；服务端发布保留原运行配置。
- 真实登录设计师原生核验：工作台2×2；项目列表/详情/资料/笔记；取消新笔记后版本仍为1；内容创作/编辑/文案/资料库/合集；客户详情正常显示4次查看31秒；客户卡及分享卡在430px视口宽398px；主页/消息/作品页。
- 导入服务处理成功态、非空接收人多成员状态、有效已发布内容/合集的完整阅读、真实服务号外发及手机键盘/大字体尚待外部条件或真机验收。回归测试及当前原生空/错误态不能代替这些证据。
- 转线索当前依赖生产单一主进程；数据库没有开放线索唯一约束。跨实例/其他创建入口同时创建时的严格防重仍需数据库方案，见接口文档。

## 验证

最终结果见本目录 `verification.json`；包括小程序完整测试、WXML编译、TypeScript、签名与跟踪产物；管理后台完整测试/构建；Go完整测试/vet；发布器隔离测试；依赖审计与新增行凭据扫描。远端CI、服务器激活、小程序上传及正式发布分别记录。

## 逐文件修改部位

下表列出本轮变动文件与diff新增侧行号。TS为维护源，配套JS由已有构建脚本同步；SCSS由微信编译插件处理。完整语义见上表及实际代码差异。

| 文件 | 修改侧行号 | 作用 |
| --- | --- | --- |
| [background/scripts/ui-content-analytics-states.test.mjs](../../../background/scripts/ui-content-analytics-states.test.mjs) | 20、44、123、215、218、241、288 | 行为/界面/构建回归与测试契约 |
| [docs/share-tracking/lead-conversion.md](../../../docs/share-tracking/lead-conversion.md) | 13、42、44、47、53 | 接口及实施说明 |
| [miniprogram/miniprogram/pages/collectionreader/collectionreader.wxml](../../../miniprogram/miniprogram/pages/collectionreader/collectionreader.wxml) | 8、11 | 页面结构、条件状态、字段标签与操作布局 |
| [miniprogram/miniprogram/pages/collectionreader/collectionreader.wxss](../../../miniprogram/miniprogram/pages/collectionreader/collectionreader.wxss) | 4 | 页面宽度、层级、留白、按钮与安全区 |
| [miniprogram/miniprogram/pages/contentedit/contentedit.js](../../../miniprogram/miniprogram/pages/contentedit/contentedit.js) | 50、131、150、152、160、564 | 同步对应TypeScript运行产物 |
| [miniprogram/miniprogram/pages/contentedit/contentedit.ts](../../../miniprogram/miniprogram/pages/contentedit/contentedit.ts) | 51、132、151、153、161、565 | 状态、查询、跳转与操作流程 |
| [miniprogram/miniprogram/pages/contentedit/contentedit.wxml](../../../miniprogram/miniprogram/pages/contentedit/contentedit.wxml) | 11、26、44、59、78、81 | 页面结构、条件状态、字段标签与操作布局 |
| [miniprogram/miniprogram/pages/contentedit/contentedit.wxss](../../../miniprogram/miniprogram/pages/contentedit/contentedit.wxss) | 485 | 页面宽度、层级、留白、按钮与安全区 |
| [miniprogram/miniprogram/pages/contentreader/contentreader.wxml](../../../miniprogram/miniprogram/pages/contentreader/contentreader.wxml) | 7、9、28、30、33 | 页面结构、条件状态、字段标签与操作布局 |
| [miniprogram/miniprogram/pages/contentreader/contentreader.wxss](../../../miniprogram/miniprogram/pages/contentreader/contentreader.wxss) | 442 | 页面宽度、层级、留白、按钮与安全区 |
| [miniprogram/miniprogram/pages/contentworkbench/contentworkbench.js](../../../miniprogram/miniprogram/pages/contentworkbench/contentworkbench.js) | 57、63、75、89、126、167、212、213、218、224 | 同步对应TypeScript运行产物 |
| [miniprogram/miniprogram/pages/contentworkbench/contentworkbench.ts](../../../miniprogram/miniprogram/pages/contentworkbench/contentworkbench.ts) | 58、64、76、90、127、168、213、214、219、225 | 状态、查询、跳转与操作流程 |
| [miniprogram/miniprogram/pages/contentworkbench/contentworkbench.wxml](../../../miniprogram/miniprogram/pages/contentworkbench/contentworkbench.wxml) | 12、25、31、33、35、42 | 页面结构、条件状态、字段标签与操作布局 |
| [miniprogram/miniprogram/pages/contentworkbench/contentworkbench.wxss](../../../miniprogram/miniprogram/pages/contentworkbench/contentworkbench.wxss) | 21、98、258、468、508 | 页面宽度、层级、留白、按钮与安全区 |
| [miniprogram/miniprogram/pages/contentwriting/contentwriting.js](../../../miniprogram/miniprogram/pages/contentwriting/contentwriting.js) | 54、182 | 同步对应TypeScript运行产物 |
| [miniprogram/miniprogram/pages/contentwriting/contentwriting.ts](../../../miniprogram/miniprogram/pages/contentwriting/contentwriting.ts) | 55、183 | 状态、查询、跳转与操作流程 |
| [miniprogram/miniprogram/pages/contentwriting/contentwriting.wxml](../../../miniprogram/miniprogram/pages/contentwriting/contentwriting.wxml) | 11、26、33 | 页面结构、条件状态、字段标签与操作布局 |
| [miniprogram/miniprogram/pages/contentwriting/contentwriting.wxss](../../../miniprogram/miniprogram/pages/contentwriting/contentwriting.wxss) | 356 | 页面宽度、层级、留白、按钮与安全区 |
| [miniprogram/miniprogram/pages/designerhome/designerhome.js](../../../miniprogram/miniprogram/pages/designerhome/designerhome.js) | 114、170、182、194、227、435、682 | 同步对应TypeScript运行产物 |
| [miniprogram/miniprogram/pages/designerhome/designerhome.scss](../../../miniprogram/miniprogram/pages/designerhome/designerhome.scss) | 14 | 页面宽度、层级、留白、按钮与安全区 |
| [miniprogram/miniprogram/pages/designerhome/designerhome.ts](../../../miniprogram/miniprogram/pages/designerhome/designerhome.ts) | 154、210、223、235、273、494、758 | 状态、查询、跳转与操作流程 |
| [miniprogram/miniprogram/pages/designerhome/designerhome.wxml](../../../miniprogram/miniprogram/pages/designerhome/designerhome.wxml) | 34、45、47、114、120、133、138、145 | 页面结构、条件状态、字段标签与操作布局 |
| [miniprogram/miniprogram/pages/messagecenter/messagecenter.js](../../../miniprogram/miniprogram/pages/messagecenter/messagecenter.js) | 5、19、23、35、42、45、50、70、74 | 同步对应TypeScript运行产物 |
| [miniprogram/miniprogram/pages/messagecenter/messagecenter.scss](../../../miniprogram/miniprogram/pages/messagecenter/messagecenter.scss) | 217 | 页面宽度、层级、留白、按钮与安全区 |
| [miniprogram/miniprogram/pages/messagecenter/messagecenter.ts](../../../miniprogram/miniprogram/pages/messagecenter/messagecenter.ts) | 6、20、24、36、43、46、51、71、75 | 状态、查询、跳转与操作流程 |
| [miniprogram/miniprogram/pages/messagecenter/messagecenter.wxml](../../../miniprogram/miniprogram/pages/messagecenter/messagecenter.wxml) | 4、7、20、25 | 页面结构、条件状态、字段标签与操作布局 |
| [miniprogram/miniprogram/pages/my/my.scss](../../../miniprogram/miniprogram/pages/my/my.scss) | 913、940、955、957、959、969、976 | 页面宽度、层级、留白、按钮与安全区 |
| [miniprogram/miniprogram/pages/my/my.wxml](../../../miniprogram/miniprogram/pages/my/my.wxml) | 60、62 | 页面结构、条件状态、字段标签与操作布局 |
| [miniprogram/miniprogram/pages/myInformationmodification/myInformationmodification.scss](../../../miniprogram/miniprogram/pages/myInformationmodification/myInformationmodification.scss) | 330 | 页面宽度、层级、留白、按钮与安全区 |
| [miniprogram/miniprogram/pages/myInformationmodification/myInformationmodification.wxml](../../../miniprogram/miniprogram/pages/myInformationmodification/myInformationmodification.wxml) | 135 | 页面结构、条件状态、字段标签与操作布局 |
| [miniprogram/miniprogram/pages/nativereferences/nativereferences.js](../../../miniprogram/miniprogram/pages/nativereferences/nativereferences.js) | 4、24、28、32、34、49、55、108、125 | 同步对应TypeScript运行产物 |
| [miniprogram/miniprogram/pages/nativereferences/nativereferences.ts](../../../miniprogram/miniprogram/pages/nativereferences/nativereferences.ts) | 5、25、29、33、35、50、56、109、126 | 状态、查询、跳转与操作流程 |
| [miniprogram/miniprogram/pages/nativereferences/nativereferences.wxml](../../../miniprogram/miniprogram/pages/nativereferences/nativereferences.wxml) | 9 | 页面结构、条件状态、字段标签与操作布局 |
| [miniprogram/miniprogram/pages/nativereferences/nativereferences.wxss](../../../miniprogram/miniprogram/pages/nativereferences/nativereferences.wxss) | 21 | 页面宽度、层级、留白、按钮与安全区 |
| [miniprogram/miniprogram/pages/projectcollections/projectcollections.js](../../../miniprogram/miniprogram/pages/projectcollections/projectcollections.js) | 248、285 | 同步对应TypeScript运行产物 |
| [miniprogram/miniprogram/pages/projectcollections/projectcollections.ts](../../../miniprogram/miniprogram/pages/projectcollections/projectcollections.ts) | 249、286 | 状态、查询、跳转与操作流程 |
| [miniprogram/miniprogram/pages/projectcollections/projectcollections.wxml](../../../miniprogram/miniprogram/pages/projectcollections/projectcollections.wxml) | 17、44 | 页面结构、条件状态、字段标签与操作布局 |
| [miniprogram/miniprogram/pages/projectcollections/projectcollections.wxss](../../../miniprogram/miniprogram/pages/projectcollections/projectcollections.wxss) | 1、31 | 页面宽度、层级、留白、按钮与安全区 |
| [miniprogram/miniprogram/pages/projectnotes/projectnotes-core.js](../../../miniprogram/miniprogram/pages/projectnotes/projectnotes-core.js) | 30、66、107、119、126、133、144、343、364、367、393、425 | 接口及实施说明 |
| [miniprogram/miniprogram/pages/projectnotes/projectnotes.wxml](../../../miniprogram/miniprogram/pages/projectnotes/projectnotes.wxml) | 14、30、42、45、47、55 | 页面结构、条件状态、字段标签与操作布局 |
| [miniprogram/miniprogram/pages/projectnotes/projectnotes.wxss](../../../miniprogram/miniprogram/pages/projectnotes/projectnotes.wxss) | 21 | 页面宽度、层级、留白、按钮与安全区 |
| [miniprogram/miniprogram/pages/projectnoteview/projectnoteview.wxss](../../../miniprogram/miniprogram/pages/projectnoteview/projectnoteview.wxss) | 5 | 页面宽度、层级、留白、按钮与安全区 |
| [miniprogram/miniprogram/pages/projectpackage/projectpackage.wxss](../../../miniprogram/miniprogram/pages/projectpackage/projectpackage.wxss) | 4 | 页面宽度、层级、留白、按钮与安全区 |
| [miniprogram/miniprogram/pages/projectpackageview/projectpackageview.wxss](../../../miniprogram/miniprogram/pages/projectpackageview/projectpackageview.wxss) | 1 | 页面宽度、层级、留白、按钮与安全区 |
| [miniprogram/miniprogram/pages/release/designerworkpublish.wxml](../../../miniprogram/miniprogram/pages/release/designerworkpublish.wxml) | 4、8 | 页面结构、条件状态、字段标签与操作布局 |
| [miniprogram/miniprogram/pages/release/release.wxml](../../../miniprogram/miniprogram/pages/release/release.wxml) | 5、11 | 页面结构、条件状态、字段标签与操作布局 |
| [miniprogram/miniprogram/pages/sharevisits/sharevisits.js](../../../miniprogram/miniprogram/pages/sharevisits/sharevisits.js) | 3、25、43、52、100、114、130、159、167、188、202、209、222、229、257、264、278 | 同步对应TypeScript运行产物 |
| [miniprogram/miniprogram/pages/sharevisits/sharevisits.ts](../../../miniprogram/miniprogram/pages/sharevisits/sharevisits.ts) | 4、26、44、53、101、115、131、160、168、189、203、210、223、230、258、265、279 | 状态、查询、跳转与操作流程 |
| [miniprogram/miniprogram/pages/sharevisits/sharevisits.wxml](../../../miniprogram/miniprogram/pages/sharevisits/sharevisits.wxml) | 3、6、12、16、22、25、31、35 | 页面结构、条件状态、字段标签与操作布局 |
| [miniprogram/miniprogram/pages/sharevisits/sharevisits.wxss](../../../miniprogram/miniprogram/pages/sharevisits/sharevisits.wxss) | 1 | 页面宽度、层级、留白、按钮与安全区 |
| [miniprogram/miniprogram/pages/visitdetail/visitdetail.js](../../../miniprogram/miniprogram/pages/visitdetail/visitdetail.js) | 3、14、16、19、27、53、82、85、90、95、100、109、124 | 同步对应TypeScript运行产物 |
| [miniprogram/miniprogram/pages/visitdetail/visitdetail.ts](../../../miniprogram/miniprogram/pages/visitdetail/visitdetail.ts) | 4、15、17、20、28、54、83、86、91、96、101、110、125 | 状态、查询、跳转与操作流程 |
| [miniprogram/miniprogram/pages/visitdetail/visitdetail.wxml](../../../miniprogram/miniprogram/pages/visitdetail/visitdetail.wxml) | 3、5、6、8、15、19、20、22 | 页面结构、条件状态、字段标签与操作布局 |
| [miniprogram/miniprogram/pages/visitdetail/visitdetail.wxss](../../../miniprogram/miniprogram/pages/visitdetail/visitdetail.wxss) | 1、46、61、229、231、235、241、244、248、252、254、264、266、269、271、274、276、277 | 页面宽度、层级、留白、按钮与安全区 |
| [miniprogram/miniprogram/utils/phase3-api.js](../../../miniprogram/miniprogram/utils/phase3-api.js) | 74、205 | 同步对应TypeScript运行产物 |
| [miniprogram/miniprogram/utils/phase3-api.ts](../../../miniprogram/miniprogram/utils/phase3-api.ts) | 75、206 | 状态、查询、跳转与操作流程 |
| [miniprogram/tests/my-workbench-access.test.js](../../../miniprogram/tests/my-workbench-access.test.js) | 219 | 行为/界面/构建回归与测试契约 |
| [miniprogram/tests/native-references.test.js](../../../miniprogram/tests/native-references.test.js) | 152 | 行为/界面/构建回归与测试契约 |
| [miniprogram/tests/phase3-content-editor-ui.test.js](../../../miniprogram/tests/phase3-content-editor-ui.test.js) | 430 | 行为/界面/构建回归与测试契约 |
| [miniprogram/tests/phase3-content-studio.test.js](../../../miniprogram/tests/phase3-content-studio.test.js) | 57、65、71、113、212、216、224、231、273、292 | 行为/界面/构建回归与测试契约 |
| [miniprogram/tests/phase3-messagecenter.test.js](../../../miniprogram/tests/phase3-messagecenter.test.js) | 63、65、81、84、93、96、108、115 | 行为/界面/构建回归与测试契约 |
| [miniprogram/tests/phase3-my-layout.test.js](../../../miniprogram/tests/phase3-my-layout.test.js) | 21、25、29 | 行为/界面/构建回归与测试契约 |
| [miniprogram/tests/phase3-workbench.test.js](../../../miniprogram/tests/phase3-workbench.test.js) | 41 | 行为/界面/构建回归与测试契约 |
| [miniprogram/tests/private-project-notes.test.js](../../../miniprogram/tests/private-project-notes.test.js) | 442、569、579 | 行为/界面/构建回归与测试契约 |
| [miniprogram/tests/project-collections-pages.test.js](../../../miniprogram/tests/project-collections-pages.test.js) | 15、23、42、372 | 行为/界面/构建回归与测试契约 |
| [miniprogram/tests/workbench-share-access.test.js](../../../miniprogram/tests/workbench-share-access.test.js) | 68、84、154、168 | 行为/界面/构建回归与测试契约 |
| [service/route/official_account.go](../../../service/route/official_account.go) | 43、47 | 接口查询、授权、统计/状态与线索契约 |
| [service/route/official_account_test.go](../../../service/route/official_account_test.go) | 246 | 行为/界面/构建回归与测试契约 |
| [service/route/share_tracking.go](../../../service/route/share_tracking.go) | 704 | 接口查询、授权、统计/状态与线索契约 |
| [service/route/share_tracking_lead.go](../../../service/route/share_tracking_lead.go) | 6、10、24、31、195、247、257、276、283、297、300、322、333 | 接口查询、授权、统计/状态与线索契约 |
| [service/route/share_tracking_lead_test.go](../../../service/route/share_tracking_lead_test.go) | 36、208 | 行为/界面/构建回归与测试契约 |
| [service/route/share_tracking_my.go](../../../service/route/share_tracking_my.go) | 100 | 接口查询、授权、统计/状态与线索契约 |
| [service/tracking/my_shares.go](../../../service/tracking/my_shares.go) | 181、228、234、243、245、260、264、288 | 接口查询、授权、统计/状态与线索契约 |
| [service/tracking/my_shares_test.go](../../../service/tracking/my_shares_test.go) | 306 | 行为/界面/构建回归与测试契约 |
| [service/tracking/my_visitors.go](../../../service/tracking/my_visitors.go) | 66、77、716、738、812、898、909、932 | 接口查询、授权、统计/状态与线索契约 |
| [service/tracking/my_visitors_test.go](../../../service/tracking/my_visitors_test.go) | 5、147、190 | 行为/界面/构建回归与测试契约 |
| [miniprogram/tests/customer-workflow-rectification.test.js](../../../miniprogram/tests/customer-workflow-rectification.test.js) | 新增文件 | 行为/界面/构建回归与测试契约 |
| [miniprogram/tests/designer-workbench-rectification.test.js](../../../miniprogram/tests/designer-workbench-rectification.test.js) | 新增文件 | 行为/界面/构建回归与测试契约 |

## 部署后小幅复核修正

消息中心提醒设置的小按钮增加明确宽度上限，避免原生button默认宽度导致半行按钮；原生实测刷新64px、收起41px。对应messagecenter.scss与visual-verdict.json。代码提交和部署回执见 RELEASE.md。交付目录还新增本清单、原生证据、验证摘要与视觉复核记录，实施计划同步更新。
