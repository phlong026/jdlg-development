# 来源与修改记录

- 上游项目：[源雀 Iyque-SCRM](https://github.com/IYque/Iyque-SCRM)
- 基准提交：`288ef707696c4a43ea5da27e1fefe717c9f1cbd9`
- 本期变更日期：2026-09-10。
- 原项目 [LICENSE](../LICENSE) 及源雀标识保留。交付容器内的 `/app/LICENSE` 和本文件一并保留来源说明。

本目录是精简的第一期启动模块，非上游官方发布版本。没有把上游全部营销、AI、群运营功能启动后隐藏菜单，而是在原仓库内新增客户管理应用入口与页面。

## 直接复用的源码

通过 Maven `build-helper` 引用原 `src/main/java`，编译白名单包括：

- `cn.iyque.domain.IYQueCustomerInfo`：外部联系人与员工的关系实体及原表名。
- `cn.iyque.utils.IYqueCryptUtil` 及 SHA1、ByteGroup、PKCS7、异常依赖：企微回调签名和 AES 解密。
- `IYqueCallBackBaseMsg`：保留的回调基础结构。

沿用上游 Java 17、Spring Boot 2.7、JPA 和 WxJava 企业微信 SDK 技术体系。UI 沿用 Vue 3 / Element Plus 技术体系，本期页面为新编写的响应式工作台，不复用完整上游营销菜单。

## 本期新增与调整

- 新增独立 Spring Boot 启动入口，按本期范围加载依赖与配置。
- 在原客户关系实体之上新增按客户去重的业务档案、负责人、跟进记录、待办、账号权限、持久化回调队列及同步任务。
- 所有正式添加客户事件可入库，不依赖特定渠道 State。
- 新增同源 Cookie 登录和服务端数据范围校验；不使用上游示例管理员密码、JWT 密钥或模型 API Key。
- 修改上游 `IYqueCryptUtil` 解密方法：验证密文尾部的接收企业 ID，防止接受其他企业的加密内容。
- 新增前端、容器部署、独立演示数据、数据库迁移、备份恢复和测试。

升级上游时应单独核对这些直接引用的源码及企业微信 SDK 行为，再运行本期集成测试与真实回调验收。本期只对该精简入口进行构建验证，没有对上游所有原有业务模块做回归测试。
