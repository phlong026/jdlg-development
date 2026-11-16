# TDesign 图标缺失 tslib 修复

日期：2026-11-16。

## 原因与修改

`pages/contentworkbench/contentworkbench` 等页面直接使用仓库内的 `components/tdesign-miniprogram/icon/icon`。TDesign 1.15.3 的此文件裸导入 `tslib`，但依赖清单和小程序包均未提供它，导致运行时报告 `module 'components/tdesign-miniprogram/icon/tslib.js' is not defined`，随后出现组件未注册的连带错误。

将 `__awaiter` 调用改为原生 `async`/`await`，将 `__decorate([wxComponent()], Icon)` 改为直接执行现有注册装饰器。现有 `wxComponent` 不返回替换构造器，因此注册和默认导出保持原有行为；图标名、颜色、尺寸、图片测量和点击事件也保持原有行为。没有增加依赖或修改页面引用。

## 验证

- 新增 `tests/tdesign-icon-runtime.test.js`，仅允许加载组件目录内的相对依赖，实际执行组件注册、普通图标更新、图片尺寸和点击事件。修复前 4 项均因缺失 `tslib` 失败，修复后全部通过。
- `node --test tests/tdesign-icon-runtime.test.js tests/phase3-wxml-compile.test.js tests/phase3-workbench.test.js tests/phase3-content-writing.test.js`：113 项通过，0 失败、0 跳过；包含微信原生 WXML 编译器检查。
- 修改文件的 JavaScript 语法检查和 `git diff --check` 通过。
- `npm audit --omit=dev --ignore-scripts --json`：0 个已知漏洞；本次没有新增依赖。
- 独立只读代码审查未发现阻塞问题。
- 微信开发者工具重新编译并打开 `pages/contentworkbench/contentworkbench` 和 `?view=import` 成功；控制台过滤 `tslib`、`Component is not found`、`MiniProgramError`、`SyntaxError` 无匹配。导入页截图中“粘贴”和“草稿已生成”的 TDesign 图标正常显示，见[模拟器验证截图](simulator-import.jpg)。未执行粘贴、导入或生成操作。
- 已在隔离副本应用反向补丁，确认本次文件可恢复至提交前内容。

## 边界与回滚

本次修复实际被页面引用的源码组件。忽略入库的 `miniprogram_npm/tdesign-miniprogram` 属本地依赖产物，未来若改用其中其他组件，需要单独验证它们的 npm 依赖闭包。日志中 `login-guard` 的 WXSS 选择器警告属于独立问题。

未上传体验版、未发布、未部署后端，未做真机验收。全部变更为代码、测试和文档，可通过 `git revert <修复提交>` 回退；没有数据库或生产副作用。
