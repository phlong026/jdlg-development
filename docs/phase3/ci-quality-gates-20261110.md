# CI 质量门禁方案（D5）

- 日期：2026-11-10
- 背景：本机缺少 `tsc`/`eslint`，Go 单测已通过 `CGO_ENABLED=0` 解除限制；tsc/eslint 需入 CI。

## 一、当前可执行门禁（已在本机验证）

```bash
# 小程序 Node 测试
cd miniprogram && npm run test:tracking
cd miniprogram && node --test tests/private-project-notes.test.js tests/content-ai-resume.test.js \
  tests/phase3-content-editor-ui.test.js tests/phase3-content-writing.test.js \
  tests/build-artifacts-sync.test.js tests/share-error-semantics.test.js tests/share-member-hint.test.js
# 产物/Tracking 同步
cd miniprogram && npm run check:tracking
# Go
cd service && CGO_ENABLED=0 GOCACHE=/tmp/jdlg-gocache GOMODCACHE=/tmp/jdlg-gomod go test ./...
cd service && GOCACHE=/tmp/jdlg-gocache GOMODCACHE=/tmp/jdlg-gomod go vet ./route
```

## 二、CI 需补充（依赖安装后启用）

```bash
cd miniprogram && npm ci
cd miniprogram && npx tsc --noEmit            # 无 tsconfig 时以各 .ts 源为准
cd miniprogram && npx eslint . --ext .js,.ts  # 需提交 eslint 配置
cd background && npm ci && npm run check       # 管理台 typecheck + routes
```

## 三、约束

- 微信 WXML 原生编译检查需 `WXML_COMPILER`，CI 需安装开发者工具或跳过并在报告中标注。
- Go 测试必须设置 `CGO_ENABLED=0`（go-m1cpu 的 cgo 初始化在本机/部分 CI 会崩溃）。
- 任何门禁失败不得部署；部署前必须通过第一、二节全部命令。
