# 客服测试原始基线校核

2026-10-08，本次首页复刻前已复制到 `/tmp/jdlg-home-reference-preview` 的客服页面，被用作原始源码基线。该页面未在首页修改过程中同步覆盖。

当前项目与原始副本逐字节比较：客服页面的 `.ts`、`.js`、`.wxml`、`.scss`、`.json`，以及 `customerServiceKnowledge.ts`、`customerServiceKnowledge.js`、`send-plane.svg`，共 8 个文件全部一致。逐文件 SHA-256 记录位于 `customer-service-baseline/baseline-provenance.json`。

将上述原始页面和依赖复制到独立临时目录，复制未修改的两个测试后执行：

```sh
node --test tests/customer-service-action-placement.test.js tests/customer-service-knowledge.test.js
```

原始基线结果：2 个测试，0 个通过，2 个失败，退出码 1。失败原因与当前全量测试一致：

- `customer-service-action-placement.test.js:22`：需求汇总卡片包含“对接设计老师”，与测试预期不一致。
- `customer-service-knowledge.test.js:49`：源码缺少测试所查找的 `const knowledgeHit = matchKnowledge(text)`。

结论：这两个失败在首页复刻前的原始副本中已存在；此次首页修改没有改变相关源码。未修改客服业务代码或测试。

日志：`/tmp/jdlg-home-reference-work/customer-service-baseline-tests.log`。

生产依赖扫描执行 `npm audit --omit=dev --json`，退出码 0，共 5 个生产依赖，0 个已报告漏洞。原始结果：`/tmp/jdlg-home-reference-work/npm-audit-production.json`。未安装或修改依赖。
