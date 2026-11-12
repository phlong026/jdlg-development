# 访问分析 · `TRACKING_CONSENT_VERSION` 决策与存量迁移规则

- 日期：2026-11-10
- 对应任务：T7.3（批次 7-A 第一项）
- 决策人：产品（用户），2026-11-10 定稿：**不升级**
- 关联：T7.1（默认开启并静默记录）、T7.2（隐私文案同步）

## 一、决策结论

**不升级 `TRACKING_CONSENT_VERSION`（环境变量 `TRACKING_CONSENT_VERSION` 保持现值）。**

本任务只负责**已决状态的读取契约**（`resolved` / `accepted`），不实现默认开启。统一契约：

- **明确关闭优先级最高**：任何 `explicit === true` 且 `accepted !== true` 的记录，跨 version / notice 都保持 `resolved=true, accepted=false`，T7.1 的默认开启不得覆盖。
- **明确开启**：`explicit === true` 且 `accepted === true`，且 version 与 notice 均匹配当前配置，才算当前同意。
- **旧同意（accepted=true 但 version/notice 不匹配）回到未决**：T7.1 已实现并选择「默认重新开启并标记 `source=default`」（见 `ensureDefaultConsent`），不再重新弹窗征询；旧记录不会被当作撤回。
- **从未选择（无 explicit 记录）才是 T7.1「首次默认开启」的对象**；这与「旧同意回到未决」是两条不同规则，不能混为一谈。

## 二、理由

1. **在本任务的迁移规则落地前**，`currentConsentChoice()` 用 version+notice 统一校验，升级 version 会让关闭记录一起回到未决，再叠加默认开启就会静默恢复已撤回用户。本任务已把「关闭跨 version/notice 持续有效」写入代码，但这仍不足以支持随意升级；P7 结论是不升级。
2. 本次改动只是隐私文案口径变化（T7.2），不改变采集范围、字段、保留周期，不构成需要重新征得同意的实质变化。
3. 不升级可以让存量「已关闭」记录继续生效，回归成本最低、可逆性最好。

## 三、存量迁移规则（代码落地）

文件：`miniprogram/miniprogram/utils/tracking/index.ts` → 编译产物 `index.js`（`npm run build:tracking`，不覆盖 components）。

`currentConsentChoice()` 分两类处理：

| 存量记录 | 规则 | 结果 |
|---|---|---|
| `explicit === true` 且 `accepted !== true`（明确关闭；含 `false`、缺字段、非布尔值，按保守关闭处理） | **不看 version / notice**，直接视为已决（resolved） | 保持关闭，默认开启不得覆盖 |
| `explicit === true` 且 `accepted === true` 且 version+notice 均匹配当前配置 | 视为当前同意 | 继续采集 |
| `explicit === true` 且 `accepted === true` 但 version 或 notice 不匹配 | 回到未决（resolved=false） | T7.1 已定：默认重新开启并标记 `source=default`（不重新弹窗） |
| 无 `explicit` 记录（旧 `share_tracking_consent_v1` 也不算 explicit） | 未决 | 只有这一类属于 T7.1「首次默认开启」对象 |
| `config.enabled !== true` 或 config 未就绪 | 不视为已决 | 不采集、不弹窗、不报错 |

## 四、运维约定

1. **只改文案时不要动 version**：调整 `TRACKING_PRIVACY_NOTICE` 内容不需要、也不允许升级 `TRACKING_CONSENT_VERSION`。
2. **不得只升 version 不改文案**：版本升级必须伴随实质告知变化，且需重新评审。本任务已保证关闭记录跨版本有效，但升级会让旧「开启」记录回到未决，影响范围大，必须单独评估。
3. 服务端 `service/tracking/config.go` 已写入同一条约定注释，`LoadConfig` 行为不变。

## 五、验收证据

测试文件：`miniprogram/tests/tracking-consent.test.js`

- `a notice-only change keeps an explicit decline closed and never re-enables collection`：改 notice 不升 version，明确关闭者保持 resolved+关闭，不采集、不弹窗。
- `an explicit decline stays closed even if the consent version changes`：即使版本变化（安全兜底），明确关闭仍保持关闭。
- `a stale explicit acceptance returns to the unresolved state for the default-on migration`：旧的明确同意在告知变化后回到未决，且只读流程不改写存量记录。这条用例只证明当前的 `resolved` 合约，不能证明未来 T7.1 的默认开启行为。
- `only acceptance of the current version and notice enables collection`：无 explicit / 版本不匹配 / 未匹配当前告知的存量记录都不采集。
- `a changed notice collects nothing, asks again, and resumes only after a new explicit acceptance`：版本升级场景下旧的明确同意回到未决，需要新的明确同意才恢复采集。这条只描述当前实现（T7.1 尚未实现），不能证明未来默认开启迁移的行为。

命令：`cd miniprogram && npm run test:tracking`、`npm run check:tracking`。
