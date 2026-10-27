# 首批实现验证记录（2026-10-27）

分支feat/phase3-content-sharing，基线73c36d8174fb5e446459468bb9cd692b902abd66。当前仓库为开发版916下JDLG创建的独立worktree，原main与既有CRM修改均未混入。

## 本次可核验的增量

1. 小程序内容工作台：签名认证+明确素材管理授权，草稿编辑、签名媒体、article_v2顺序排版、服务端实际预览、固定版本发布、撤回及幂等重试。旧plain/rich_text稿在手机端只读，避免无提示改格式。
2. 本人分享/访问详情：实际Sharer精确限定、独立can_view、SQL keyset分页快照、历史源权限批量读取、自访客及删除排除。无查看权限的管理者不获取分享统计。
3. 自有域名公开接收：同一Share/token进入网页和原生卡片；图片/视频逐次重验版本归属；GET不产生阅读提醒；明确同意后的页面可见统计；同Share的URL Link生成合并并发且不晚于源Share过期。
4. 消息与服务号：有效page_view同事务notice_event+job，实际分享人收件箱、已读/未读/详情、绑定版本、一次性OAuth、加密回调、模板投递、持久重试/unknown/取消、乱序回执。删除/撤回/保留期清理与通知投影同事务参与，避免孤儿消息或重复投递。

## 验证

| 检查 | 结果 | 证据边界 |
| --- | --- | --- |
| CGO_ENABLED=0 go test ./... -count=1 | 通过 | 所有Go包单元/模拟SQL及HTTP测试；不等于生产数据库验收 |
| CGO_ENABLED=0 go vet ./... | 通过 | 无输出 |
| CGO_ENABLED=0 go build ./ | 通过 | 仅生成/tmp本地检查二进制，未上传 |
| 小程序 node --test tests/*.test.js | 513项：510通过、3既有条件跳过、0失败 | 含账号变化、媒体、预览、幂等、链接及SDK回归 |
| 小程序TypeScript/build一致性/route gate | 通过 | 使用项目已有background/node_modules/.bin/tsc；63路由，22公开/41排除 |
| 后台 npm test / npm run build | 754/754，构建通过 | 含article_v2及既有UI回归；未部署dist |
| H5脚本测试 | 10/10 | 含可见时间、关闭竞态、单飞、完整响应体超时及缺随机源fail-closed |
| gofmt / JS语法 / git diff --check | 通过 | 修改Go均格式正确；23个变更JS/MJS语法检查通过 |
| 凭据静态扫描 | 无真实凭据模式命中 | 扫描变更文件；不是全面渗透测试 |
| npm audit --omit=dev（mini及后台） | 两端均0项 | 无新增/变更依赖；本机未提供govulncheck，不宣称Go依赖审计完成 |

`go test -race ./tracking ./notification ./officialaccount ./shareweb -count=1` 四包全部通过。route race在本机依赖go-m1cpu的CGO初始化阶段曾SIGSEGV，未进入用例；完整route套件在CGO_ENABLED=0下通过，不能将这项环境限制写成race通过。

独立审查发现的问题已逐项修正：素材统计越权、旧稿隐式转换、预览不完整、幂等重试、prepared Share复制确认、H5单调时钟/后台虚增/完整超时、URL Link有效期及并发、投递资格/令牌失败、乱序回执、隐私清理和锁序。审查报告存于本worktree的.superpowers/sdd工作记录；它不是部署验收。

本地手机宽度390×844核对了真实shareweb.Render组件的明确标注测试样例：标题、段落、引用、列表与按钮无横向裁切。该样例没有真实Share/DB/provider，不证明媒体播放或小程序真机通过。

## 未完成与发布前条件

- 生产未执行任何代码部署、迁移、Nginx加载、私有目录创建或真实通知。
- 服务器唯一applinux PID3763918监听8080、cwd/bin及四项基础跟踪开关已核实；独立应用启停管理方式仍待发布核验。
- 生产未配置私有媒体目录，URL Link开关未设置；服务号名称为华匠邦建科，AppID、模板ID/字段、域名、关联及真实接收条件未核实。
- 三平台完整链接导入/原排版保真、AI文案与私人项目协作/导出仍是后续增量；H5 video指标和handoff去重明确不可用。
- 尚无真实MySQL迁移/双worker双事务并发验证；SQL顺序与事务回滚由fake driver/领域用例校验，必须在隔离MySQL环境复核后部署。
- 尚未在微信开发者工具/真机以A制作、B分享、C阅读、C再分享到D逐角色验收。
- 原生旧卡片直接转发不能识别中间转发人，接口accepted或模板回执不能证明用户已读。

回退按README优先保留投递/绑定证据，关闭开关并回退应用；down.sql仅用于空新表或备份后的离线清理。`git apply --reverse --check` 对完整代码补丁检查通过，未改变工作树；数据库回退仍遵守前述数据保留条件。提交按迁移、跟踪领域、后端接线、前端和核对文档分开。

最终独立复审：内容/预览/链接/H5修正通过；通知投递锁序、撤回物化竞态、关闭生成后回执保留窄范围复审APPROVE。此前各轮报告中的失败与瞬时共享树问题均已由最终全量检查覆盖。

实现提交：d7db770（迁移）、1d39c0a（跟踪领域）、a536d93（后端接线）、3477e5d（前端）。核对与验证文档单独提交。未推送、合并或部署。
