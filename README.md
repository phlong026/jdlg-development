# 甲地灵光二期开发源码

本仓库保存 2026-10-11 当前开发源码的独立快照，包含：

- `service/`：Go 后端。
- `background/`：React 管理后台。
- `miniprogram/`：微信小程序完整源码及测试。
- `customer-management/`：Java 客户管理服务、阶段实现和 PC/H5 前端。
- `docs/`：开发说明、集成示例和验证材料。

来源目录：`开发版916/JDLG`；主仓库来源提交：`920526debca32f42b483f900a06133aa27e803c8`。客户管理模块和原先被 Git 忽略的小程序文件按当前工作目录完整纳入。旧仓库历史和本地 Git 元数据未导入。

依赖目录、编译产物、本地 `.env`、证书、私钥、日志、缓存及备份压缩包不入库。各模块的依赖版本由现有清单与锁文件保留，开发与部署请参考模块说明。

## 本地配置

上传副本已将 `service/function/crypto.go` 的默认 SM2/SM4/HMAC 密钥、`customer-management/src/main/java/cn/iyque/utils/JwtUtils.java` 的 JWT 签名密钥，小程序 `utils/parameter.js` 的默认协议密钥、客户管理移动端的固定访问令牌，以及客户管理 `application.yml` 中的密钥和密码替换为占位值或环境变量引用。原始本地开发目录未修改。

这些占位值不能用于生产。克隆后需在受保护的本地配置中提供项目所需凭据；代码快照上传不表示系统已部署或完成业务验收。
