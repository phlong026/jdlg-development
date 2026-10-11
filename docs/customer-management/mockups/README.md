# 客户管理一期效果图

已完成12张静态PNG：9张电脑端、3张手机端。生成方式：内置ImageGen。所有图片为演示数据，不代表功能已实现。

[打开完整图册](<E:/other​/jd/docs/customer-management/mockups/index.html>) · [效果图清单](<E:/other​/jd/docs/customer-management/mockups/效果图清单.md>) · [提示词](<E:/other​/jd/docs/customer-management/mockups/生成提示词.md>)

## 12 客户驾驶舱（首页）

补充客户管理首页，图册置顶。查看新增、待办、未分配和团队跟进情况。详见[自动接入与首页说明](驾驶舱与自动接入补充说明.md)。

## 01 企微客户工作台

主管查看全部企微客户，按分类、负责人和待办队列筛选。导出遵循当前筛选与数据权限。

[查看原图](<E:/other​/jd/docs/customer-management/mockups/01-企微客户工作台.png>)

## 02 客户详情

客户档案与多员工企微关系分别展示；好友删除后仍保留档案与人工跟进记录。

[查看原图](<E:/other​/jd/docs/customer-management/mockups/02-客户详情.png>)

## 03 记录跟进

记录沟通结论、客户阶段和下次联系时间。新客户、跟进中必须安排未来时间；已成交、暂缓、无效清空待办时间。

[查看原图](<E:/other​/jd/docs/customer-management/mockups/03-记录跟进.png>)

## 04 编辑资料与分配负责人

主管修改资料与业务负责人；手机号和需求摘要可后补，负责人调整不转移企微好友关系。实际编辑使用合法号码，图片中的掩码仅用于演示。

[查看原图](<E:/other​/jd/docs/customer-management/mockups/04-编辑资料与分配负责人.png>)

## 05 顾问待办

顾问仅查看本人客户。今日与逾期可能重叠，处理后按新的时间或阶段重新计算队列。

[查看原图](<E:/other​/jd/docs/customer-management/mockups/05-顾问待办.png>)

## 06 企微同步管理

配置更新后的待验证状态与历史同步记录同时展示。图片为演示，并不表示真实企微已接通。

[查看原图](<E:/other​/jd/docs/customer-management/mockups/06-企微同步管理.png>)

## 07 账号与权限

系统管理员建立后台账号、CRM账号和企微员工的对应关系。顾问数据范围固定为本人负责，角色变更时由系统重算，不能任意扩大。

[查看原图](<E:/other​/jd/docs/customer-management/mockups/07-账号与权限.png>)

## 08 小程序咨询线索

保留原小程序咨询线索业务，展示统一客户管理入口；原字段、状态与处理规则仍以现有业务实现为准。

[查看原图](<E:/other​/jd/docs/customer-management/mockups/08-小程序咨询线索.png>)

## 09 手机端客户与待办

同一网页应用的手机适配，便于搜索客户和处理待办；顾问只看本人负责客户。

[查看原图](<E:/other​/jd/docs/customer-management/mockups/09-手机端客户与待办.png>)

## 10 手机端客户详情

手机查看客户档案、关系和跟进历史。顾问可以编辑允许的资料，不能修改负责人。

[查看原图](<E:/other​/jd/docs/customer-management/mockups/10-手机端客户详情.png>)

## 11 手机端记录跟进

手机完成跟进填写，与电脑端共用校验规则和保存接口。实际小屏允许滚动，保存操作保持容易触达。

[查看原图](<E:/other​/jd/docs/customer-management/mockups/11-手机端记录跟进.png>)

## 实施说明

- 图片用于界面布局评审，自动生成的局部文字、字数计数不作为校验规范。需求说明书、开发文档优先。
- 已成交是人工客户阶段，不能据此判断付款或业务交付完成。
- 电脑版不同角色展示不同导航；系统管理员与客户主管是分别授权的角色。
- 手机端是网页适配效果，不是原生应用或企微免登录效果。
- 目标设计视口为电脑1440×1024、手机390×844；交付PNG为对应比例的高分辨率生成图，前端需正常适配而非将图片直接作为页面。

