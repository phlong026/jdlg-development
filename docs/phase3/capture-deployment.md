# 公众号视频采集环境部署

## 2026-10-27 实际部署结果

已在 `162.14.210.252` 完成独立采集环境部署，Chrome for Testing Headless Shell 为 `156.0.8078.47`。网络、浏览器、Worker 和出口刷新 timer 均已启用；新增的 API Nginx exact location 已通过 `nginx -t` 并加载。原甲第主业务仍为原有唯一进程，未替换程序或加载采集客户端配置。

实际验收通过：

- `verify.sh --installed`：26 项部署策略测试，以及真实服务、文件权限、Unix socket、本机 HTTPS readiness 和运行时网络探针。
- namespace 内允许的微信 HTTPS 地址可正常完成证书校验；元数据地址、私网、宿主业务端口、非白名单公网、其他 DNS 和 IPv6 均不能连接；宿主无法连接 CDP。
- 通过真实 CDP 创建临时页面并渲染，再读取实际 renderer PID，验证专用非 root UID、seccomp 过滤、NoNewPrivileges、零 capability 及专用 AppArmor label。该 AppArmor profile 只提供该路径所需的 userns 许可，文件隔离由 systemd 约束。
- 使用真实 Go `CaptureWorker`、服务器私有配置、域名 TLS 和本机固定 Dial，readiness/Probe/Available 均通过；外部访问内部 readiness 返回 403。
- 停止 Chrome 后 Worker 保持运行并返回 503；重新启动 Chrome 后自动发现新 CDP 地址并恢复 200，Worker PID 不变，临时 profile 已更新。
- 验收用临时测试二进制按散列匹配清理。发布元数据保存在服务器 `/opt/jdlg-content-capture/releases/20261027/deployment-metadata.json`，配置密钥未复制出服务器。

以上证明环境和接入客户端可用。尚未部署主业务的新代码及配置，也未使用真实含视频文章采集，因此不能据此认定生产公众号导入已开通。用户已说明真实链接后期补充。

证据：[最终验收](evidence/capture-final-acceptance-20261027.png)、[网络隔离](evidence/capture-network-isolation-pass-20261027.png)、[renderer 与真实 HTTPS](evidence/capture-sandbox-and-https-pass-20261027.png)、[重启恢复](evidence/capture-restart-recovery-pass-20261027.png)。实际 Linux 诊断在 `service/contentcapture/live_test.go`，默认跳过，须在上述隔离服务器显式启用；普通测试不读取真实配置。

本部署只新增独立采集资源，不修改现有甲第主服务、全局 DNS、`/etc/hosts` 或全局 user namespace 策略。采集流量通过 `jdlg-content-capture` network namespace 隔离，只允许固定 DNS 和已解析的微信 HTTPS 目标。

## 部署契约

- Chrome for Testing Headless Shell 固定为 Google 官方 Stable `156.0.8078.47`，路径为 `/opt/jdlg-content-capture/browser/chrome-headless-shell-linux64/chrome-headless-shell`。
- Worker 路径为 `/opt/jdlg-content-capture/bin/content-capture-worker`。两个二进制文件必须由 `root` 持有，且不能对 group/other 可写。
- 两个无 home、无 shell 账号：`jdlgcap-browser` 和 `jdlgcap-worker`。Worker 的运行组为现有 `www`。
- Worker 只监听 `/run/jdlg-content-capture/capture.sock`，socket 为 `0660 jdlgcap-worker:www`。systemd 管理的专用 runtime 目录为 `0750 jdlgcap-worker:www`；上层 `/run` 仍由 root 管理。
- CDP 只监听 namespace 内 `127.0.0.1:9222`，不对主机网络或公网开放。Chrome 保留 sandbox，未使用 `--no-sandbox`、`SYS_ADMIN` 或 seccomp 绕过。
- AppArmor 仅给上述 Chrome 绝对路径的 `flags=(unconfined)` profile 增加 `userns,`，不修改全局 sysctl。
- `/opt/jdlg-content-capture/worker.env` 在服务器上生成，为 `0600 root:root`，不进入 Git。
- `/opt/jdlg-content-capture/main-service.env` 也由部署脚本生成为 `0600 root:root`，与 Worker 共用当次随机 secret。这是供未来主业务部署显式加载的独立配置；采集部署不修改现有应用 `.env`，不重启主业务。
- network lifecycle unit 必须在宿主 mount namespace 中运行，否则 `ip netns add` 创建的 `/run/netns/jdlg-content-capture` bind mount 不会传播给宿主，systemd 后续无法加入该 namespace。因此该 root oneshot 不使用 `ProtectSystem`、`ProtectHome`、`PrivateTmp` 或 `ReadWritePaths`，但保留 `NoNewPrivileges=true`，并把 capability 严格限定为 `CAP_NET_ADMIN CAP_SYS_ADMIN`。`CAP_SYS_ADMIN` 不会授予 Browser 或 Worker。
- Browser 和 Worker 均使用 `InaccessiblePaths=/www` 与 `ProtectProc=invisible`。Worker 保留 `Group=www` 以便 Nginx 访问 Unix socket，但专用进程无法借此组身份读取 `/www` 下的其他站点配置。

## 资源与调用链

1. `jdlg-content-capture-network.service` 创建专用 namespace 和拒绝优先的防火墙策略。
2. `jdlg-content-capture-browser.service` 在 namespace 中启动 Headless Shell。`RuntimeDirectoryPreserve=no` 保证每次服务生命周期使用新的 profile。
3. `jdlg-content-capture-worker.service` 通过 `http://127.0.0.1:9222/json/version` 发现当次 CDP WebSocket，通过 Unix socket 接收采集请求。
4. Nginx exact location 只允许 `127.0.0.1`/`::1`，把 HTTPS 内部路径代理到 Unix socket，且关闭 access log。
5. 主服务使用 `https://api.jiadilingguang.com/_internal/content-capture/v1/capture`，底层 Dial 固定 `127.0.0.1:443`，仍使用正常域名完成 Host/SNI 和 TLS 校验。不得把该内部路径改为公网可访问接口。

## 部署前准备

在与服务器架构一致的 Linux 环境构建 Worker，然后以 root 拥有、不可被服务账号改写的方式放到约定路径。

```bash
cd service
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -o content-capture-worker ./cmd/content-capture-worker
install -m 0755 -o root -g root content-capture-worker /opt/jdlg-content-capture/bin/content-capture-worker
```

部署包必须包含整个 `ops/content-capture/`，包括现有 `network.py`。首先在代码目录做本地策略检查：

```bash
ops/content-capture/verify.sh
```

脚本支持两种不混用的执行布局：

- 从 Git/临时发布目录执行：脚本要求 `/opt/jdlg-content-capture/ops/{network.py,stale_socket.py,rollback.sh}` 不存在，安装并标记这三个文件为本次部署所有。
- 将完整 `ops/content-capture/` 预置为 `/opt/jdlg-content-capture/ops/` 后就地执行：脚本校验三个运行文件为 root 所有、不是 symlink 且 group/other 不可写，不会把“源文件已在目标位置”误判为覆盖冲突，回滚也不删除预置包。

## 安装

实际 Nginx 扩展目录必须显式传入。甲第当前主配置已 include `/www/server/panel/vhost/nginx/extension/api.jiadilingguang.com/*.conf`：

```bash
sudo ops/content-capture/deploy.sh \
  --nginx-extension-dir /www/server/panel/vhost/nginx/extension/api.jiadilingguang.com
```

脚本会先跑策略测试、Python 语法检查、shell 语法检查、AppArmor parser 检查和 systemd unit 校验。创建 `jdlgcap-browser` 账号后，它还会在该非 root 账号下运行 `ldd` 与 `chrome-headless-shell --version`：缺失动态库时只报告库名并终止，且必须确认实际运行版本为 `156.0.8078.47`。它会拒绝覆盖任何同名 unit、AppArmor profile、worker env 或 Nginx extension。Nginx 文件安装后必须通过 `nginx -t` 才会 reload；任何后续检查失败都会触发专用资源回滚。

安装后手动复验：

```bash
sudo ops/content-capture/verify.sh --installed
sudo systemctl status \
  jdlg-content-capture-network.service \
  jdlg-content-capture-browser.service \
  jdlg-content-capture-worker.service \
  jdlg-content-capture-network-refresh.timer
```

`--installed` 会检查 unit 状态、env/socket 所有权、namespace 内 CDP 监听，并通过本机 TLS + Nginx 内部路由校验严格响应 `{"ready":true}`。该过程不打印 secret。

`main-service.env` 只包含下列四个键，可供真实 Go 客户端完成域名 TLS 校验与本机固定 Dial 诊断：

```dotenv
CONTENT_IMPORT_CAPTURE_URL=https://api.jiadilingguang.com/_internal/content-capture/v1/capture
CONTENT_IMPORT_CAPTURE_HEALTH_URL=https://api.jiadilingguang.com/_internal/content-capture/ready
CONTENT_IMPORT_CAPTURE_DIAL_ADDRESS=127.0.0.1:443
CONTENT_IMPORT_CAPTURE_SECRET=<与 Worker 一致的服务器随机值>
```

主业务只能在自身发布流程中加载该文件，不应把内容复制到 Git、日志或命令行。

## 陈旧 socket 处理

Worker 自身拒绝任何已存在的 socket。systemd `ExecStartPre` 只对 `/run/jdlg-content-capture/capture.sock` 做以下证明后才删除：

- 路径不是 symlink，类型是 Unix socket；
- owner UID 是 `jdlgcap-worker`；
- 连接明确返回 `ECONNREFUSED`；
- 连接检查前后 inode、device、owner 和 mode 都未变化。

活动 socket、所有权不匹配、类型不匹配或竞态变化都会阻止 Worker 启动，不会扩大删除范围。

## 回滚

```bash
sudo /opt/jdlg-content-capture/ops/rollback.sh \
  --nginx-extension-dir /www/server/panel/vhost/nginx/extension/api.jiadilingguang.com
```

回滚要求部署脚本创建的 `/opt/jdlg-content-capture/.deployment-owned` 标记，并校验当初的 Nginx 扩展目录。它只停止并删除 `jdlg-content-capture-*` 专用 unit、专用 namespace/规则、专用 Nginx extension、专用 AppArmor profile、本次生成的 `worker.env`/`main-service.env` 和两个服务账号。它不删除预先放置的 Chrome 或 Worker 二进制，不修改其他 Nginx 文件。Nginx 回滚后仍会先执行 `nginx -t`，检查失败时不 reload。

## 已知的上线门槛

- 实际服务器必须有可用的 AppArmor parser 与 unprivileged user namespace；本方案不会为通过检查而改全局策略。
- 主服务必须使用与 `/opt/jdlg-content-capture/worker.env` 一致的 secret。这一配置由主服务部署流程注入，本脚本不修改现有应用 env。
- 真实公众号含视频文章仍需在部署后做一次端到端验收；readiness 只证明 Chrome/Worker/Nginx 链路就绪，不代表真实素材已采集成功。
