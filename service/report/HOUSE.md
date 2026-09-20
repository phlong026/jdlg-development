# 整屋图文报告协议

## 内容与编排

`report-house-v1`沿用家庭画像、宅基地条件、设计目标、整屋方案、逐层平面、体量外观、方案比较、预算边界、设计师任务书、行动计划的大框架。固定四个免费预览章节，完整报告根据二至三个方案、实际楼层和正文长度排版。两个方案、两层的本地合同样例为15页，三层为17页；数字是样例结果，不是固定页数或用户承诺。

Go主编排管理可执行顺序、支付门槛、缓存与发布。诊断、策划、方案、空间、视觉、预算、编辑、审核角色通过已有JSON Chat Completions适配器运行，可以复用同一模型或独立配置。模型不能直接修改订单、权限或任务状态。每阶段结果通过结构校验才保存；无效输出仅允许任务有界重试，已审核拒绝不自动发布。

- 家庭人数/居住频率改变方案使用情景；楼层与卧室决定逐层房间清单和图片数量。
- 必须房间、楼层偏好、首层老人房、拒绝兼用都进入结构约束。完整报告保留前四页事实与诊断及用户确认。
- 方案包含不同策略、收益、代价、风格、成本敏感项及事实引用。空间角色逐层分配房间，视觉任务从同一方案版本派生。
- 每层一张概念平面；同方案体量引用首层平面、外观引用体量。场地自述尺寸、道路朝向、完整楼层结构写入绘图上下文。
- 审核模型收到实际图片，体量/外观审核同时收到参考图。拒绝或无法辨认即停止发布。生成图是概念沟通材料，不能代替专业图纸核验。

## 客户端接口

前缀沿用`/api/v1/miniprogram`及既有登录签名。`GET /reports/config`新增`house_enabled`。

| 接口 | 行为 |
|---|---|
| `GET /reports/house/questions?primary_goal=space` | 公开读取问题集，`preview_pages=4`、`full_pages=null` |
| `POST /reports/house` | 本人登录提交`DecisionInput`和`house`，返回202，不同步调用模型 |
| `GET /reports/:id` | 本人状态、四页预览、确认状态、付款能力、下一步 |
| `POST /reports/:id/confirmation` | 绑定`diagnosis_id`和`input_hash`，choice仅confirm或undecided |
| 现有virtualpay prepare/confirm | 服务端冻结商品、金额、输入和确认并验单；客户端回调不授予权限 |
| `GET /reports/:id/document?mode=full` | 本人、有效已付订单、ready后返回私有短期地址；不要求绑定企微 |

新协议输入保留V5字段，并要求floors为1–3、bedrooms为1–12，新增：

```json
{
  "house": {
    "style": "新闽派",
    "first_floor_elder_room": true,
    "road_side": "南侧道路",
    "orientation": "坐北朝南",
    "neighbor_notes": "西侧邻居，采光待核实",
    "courtyard": "yes",
    "parking": "unknown",
    "start_window": "一年内",
    "members": [
      {"role":"父母","count":2,"residence":"permanent","needs":["首层起居"]},
      {"role":"子女家庭","count":4,"residence":"returning"}
    ],
    "rooms": [{"id":"elder","name":"老人房","count":1,"preferred_floor":1,"required":true}]
  }
}
```

style必填；courtyard/parking为yes/no/unknown；首层老人房允许未定时省略。members可省略，填写时常住/返乡各合计必须等于输入总数。rooms最多12项，ID唯一，preferred_floor=0表示不指定；成员与房间有长度/数量界限。新输入改变冻结hash，不能覆写已购买报告。未知尺寸不补造。

小程序接入位置：`开发版916/JDLG/miniprogram`，该复制目录被JDLG主仓忽略。为保持可回退，改动前文件和最小补丁保存在本仓`docs/report-house-miniprogram-{baseline,patch}`。没有整体导入小程序、账号配置或无关业务代码。

## 异步图片与恢复

报告队列沿用building_reports；文本阶段保存report_agent_stages；确认保存在report_decision_contexts；逐图状态保存在report_house_images。一个新图片完成后主动释放报告租约，后续消费复用已完成阶段和图片，正常续跑不扣减失败重试额度。HTTP请求不等待模型，退出小程序不打断后台任务。

绘图复用小程序SQL `ai_draw`选择和现有运行时GPT-Image配置，2K为原始2048×1152。全部停用、渠道不兼容或配置读取失败会明确报错。只有无ai_draw记录才回退既有环境配置。不创建小程序绘图订单，不扣用户灵石。供应商仍可能产生真实费用。

提交前保存submitting记录；结果未知保留记录并报`image_submission_uncertain`，前端下一步为contact_support，不允许用户或worker盲目重发。已生成私有文件和收据但数据库提交失败时，可校验收据恢复。缺图、错误尺寸、哈希变化、跨方案引用、越界路径或图片审核拒绝不会发布PDF。

图片根目录首次启动会原子创建0600权限的`.house-image-storage-id`，并与数据库`report_house_image_storage`的唯一存储身份核对。多个worker必须挂载同一个持久共享目录；相同路径但不同磁盘会在启动时拒绝接入。此校验不提供共享存储，不能复制标记来绕过。迁移共享卷时需一起迁移图片、收据和身份标记，再停机核对完整性。

审图请求使用当前图片及必要参考图。对于超过10MiB、但在原始图片32MiB上限内的合法图片，仅将送审副本去除冗余元数据并规范为8位PNG；原始2K图片、报告用图及其哈希保持不变。

PDF会保存校验母版、按内容续页，再栅格化成一个完整图片PDF。平面图按比例排版，长文不截断；所有资产须与方案/楼层/版本关联。私有COS上传及回读校验通过后才ready。

## 启用与运行

1. 保留原迁移，在测试数据库执行`report_house_images_up.sql`与`report_notifications_up.sql`。部署脚本不自行建表。
2. 同步render.py、decision_v5.py、house_v1.py与现有Python依赖和中文字体。
3. 设置REPORT_ENABLED、REPORT_MULTI_AGENT_ENABLED、REPORT_DECISION_ENABLED、REPORT_HOUSE_ENABLED；新开关默认false。
4. REPORT_ASSET_ROOT和REPORT_ARTIFACT_DIR使用私有持久目录，多实例共享；禁止公开静态目录。图片目录0700、文件0600。
5. 审核角色配置支持图片输入的模型。REPORT_AGENT_SPATIAL_MODEL可指定空间模型；其他角色留空复用公共模型。更换提示词/模型契约需更新配置版本。
6. House任务超时至少4分钟；建议沿用env.example的7分钟、租约450秒。每图提交超时180秒，不在剩余时间不足时开启收费调用。
7. 通过真实微信商品验单、独立私有COS、SQL租约、绘图渠道和真实家庭样本验收后启用支付。

嵌入与独立worker保持原命令，不引入新服务依赖。关闭house_enabled可停止新建/新支付，但会阻断仍在队列的House任务，因此回退前必须处理在途报告。保留旧数据库、报告和收据；down SQL仅用于隔离测试，线上不能通过删收据解决未知收费。

## 本地验证与上线边界

```sh
CGO_ENABLED=0 go test ./... -count=1
CGO_ENABLED=0 go vet ./...
REPORT_HOUSE_REAL_PDF_TEST=1 REPORT_PDF_PYTHON=/usr/bin/python3 \
 REPORT_PDF_FONT='/absolute/chinese.ttf' CGO_ENABLED=0 \
 go test ./report -run TestHousePipelineWithRealPDF -count=1
cd reportpdf
/usr/bin/python3 -m unittest test_render test_decision_v5 test_illustrated_demo test_house_v1
```

真实PDF集成覆盖创建、确认、模拟服务端验单、主编排、生产图片持久服务、逐图续跑、实际Python渲染、私有存储适配及下载归属。模型响应、支付、数据库和COS使用替身，测试图片是明确的本地色块，不代表绘图质量验收。真实供应商、数据库迁移、微信设备、支付及企微送达尚需部署环境验证。

## 完成通知与网关合同

reportnotify独立循环会补齐所有已完成、已支付的整屋报告，每份报告只有一个report_ready事件。即使报告生成后进程立即停止，重启也能补齐通知；通知失败不撤销ready。尚未绑定企微的事件等待真实归属绑定，不向猜测的客户发送。

本仓当前只有企微添加好友欢迎消息能力，尚无已确认可用的延时客户主动发送接口。因此完成通知提供可配置企业消息网关适配器；它不是已部署的企微发送服务。REPORT_NOTIFY_ENDPOINT与REPORT_NOTIFY_TOKEN必须同时配置HTTPS地址与凭据，未配置时状态为manual_required。运营可按report_notifications表中的事件联系客户；后续配置网关会恢复尚未发送的待处理事件。

网关合同：

- POST到配置地址，Authorization Bearer，Idempotency-Key为稳定event_id。
- JSON仅包含event_id、report_id、user_id、已验证external_user_id、mini_program_path、通知文案；不包含家庭详情或COS临时链接。
- 网关只有在实际送达确认后返回`{"status":"sent","receipt":"渠道回执"}`；接收队列不能冒充送达。
- GET `配置地址/{event_id}`查询相同事件的最终回执，响应合同相同。
- POST超时、响应未知、进程崩溃后过期sending进入uncertain；后续只查回执，不盲目重发POST。网关须保证事件幂等并将微信侧权限/发送窗口失败如实反馈。

真实企业微信渠道、网关部署及真实回执需要另在凭据环境验收；本次仅本地HTTP替身验证，没有实际向客户发消息。
