# 视频续传生产部署记录

2026-11-08 已通过用户打开的宝塔面板部署新版后端，并切换生产进程。源代码固定为 `0030d7f9a8d7f8275c33ddb1193f840c2c5ffd40`。本记录不代表微信小程序正式版发布或手机大文件上传验收。

## 程序与切换结果

| 项目 | 核验值 |
| --- | --- |
| 生产程序 | `/www/wwwroot/jiadilingguang/api/applinux`，端口 8080 |
| 原进程 / 新进程 | `1154047` / `1078970` |
| 原运行及磁盘 SHA256 | `d05c55fecd339383bd9ed4660f604bd93e0b1e2292b03c0701b9526f55d61e6a` |
| 新运行及磁盘 SHA256 | `4504e1f6ddc3655444d18a926b72075dec680ba7cac897c920c8202188e00385` |
| 上传归档 SHA256 | `c9be178f1efc3d6a21493455071b57c1ab45252812cd17d379e1acca56c83fa6` |
| 服务器候选与发布器 | `/root/jdlg-video-deploy-0030d7f` |
| 程序、原参数及运行环境备份 | `/root/jdlg-video-backup-20261108-0030d7f` |
| 配置变更前独立备份 | `/root/jdlg-video-config-backup-20261108-0030d7f` |

沿用受保护鉴权构建流程，Linux amd64 ELF、客户端签名契约及 HMAC/SM4 一致性校验通过。受保护鉴权源码哈希与 11 月 5 日发布构建相同。归档只包含压缩程序、构建清单、原有受控发布器与校验清单，没有认证源码或环境文件。

服务器现场核对原进程身份、工作目录和运行/磁盘程序哈希；以明确的 PID 与旧哈希执行 `binary_release.py prepare`，返回 `prepared`。随后执行 `activate`，返回 `active`、新进程 `1078970`。激活后的配置快照、进程参数和初始运行环境核验均通过，终端最终返回 `DEPLOY_OK`。

## 必要配置补齐

现场没有设置 `CONTENT_VIDEO_UPLOAD_DIR` 或 `TRACKING_MEDIA_DIR`，因此单独更新程序不足以开启续传。确认进程环境没有同名覆盖项、实际配置文件为 `/www/wwwroot/jiadilingguang/api/.env.local` 后，将原文件私有备份并原子追加唯一一项：

```text
CONTENT_VIDEO_UPLOAD_DIR=/var/lib/jdlg-video-uploads
```

目录位于 Web 根目录之外，root 所有、权限 `0700`。原配置的其他内容、权限和所有者保持，备份目录 `0700`、备份文件 `0600`。追加完成后再执行程序发布准备，使新配置进入发布器的受保护快照。没有数据库迁移、鉴权密钥调整、代理配置修改或后台前端部署。

## 已验证

- 生产本机与公网 `tracking/config` 返回 200；未认证工作台与新增上传路由返回预期 401。
- 公网提交 4MiB 分片加 multipart 开销，共 4,194,467 字节，返回预期 401，未被代理以 413 拒绝。该探针没有认证或业务写入，不等同于真实文件上传成功。
- 用微信开发者工具中已登录的设计师账号正常进入内容工作台，从实际生产接口取得：`media_upload_configured=true`、`video_editor_fields=true`、`video_resumable_upload=true`、`video_upload_chunk_size=4194304`。
- 保留原编辑页未保存内容，刷新能力后“不支持本地上传”的旧提示消失；未保存或发布业务内容。

宝塔终端和开发者工具的可见回执转录在[部署观察数据](deployment-observation.json)。转录不是下载的原始服务器日志或原始认证响应；没有保存令牌、Cookie、环境内容或私有媒体 URL。

## 验收边界与回滚

真实 COS 大文件完整上传、断网续传及微信 iOS/Android 相册文件仍需手机验收。此次没有上传小程序、设置体验版、提审或正式发布。模拟器控制台已有历史错误和警告，本轮没有作出全量 UI 零错误的结论。

快速程序回退使用本轮备份：

```bash
python3 /root/jdlg-video-deploy-0030d7f/binary_release.py rollback \
  --backup /root/jdlg-video-backup-20261108-0030d7f
```

严格恢复部署前全部配置时，先回退程序并确认旧服务健康，再使用独立配置备份和其元数据原子恢复 `.env.local`，最后按原参数和环境受控重启旧程序。不能先恢复配置，否则发布器会因配置漂移而拒绝程序回退。旧程序忽略新增目录配置；不删除暂存目录、用户媒体或已登记素材。本轮没有实际执行生产回滚演练。
