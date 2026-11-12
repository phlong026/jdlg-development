# COS 离线存储盘点

该工具只读取本地导出的 COS 对象清单，输出 JSON 盘点报告。它不连接腾讯云、不读取密钥、不调用删除接口，也不把任何对象标记为“可安全删除”。

## 输入格式

对象清单支持 CSV 或 JSONL，字段名不区分大小写，并忽略下划线和连字符。每行至少包含：

- 对象键：`key` / `object_key` / `object` / `name` / `path`
- 字节数：`size_bytes` / `size` / `content_length` / `contentLength`

可选字段：`content_type`、`storage_class`、`last_modified`以及 `sha256`。重复内容只使用严格的 64 位十六进制 SHA-256 判定；**ETag 不会被当作内容哈希**，因为分片上传的 ETag 不等于文件哈希。若 COS 清单本身不含 SHA-256，应由受控的离线作业计算后再补充，本工具不会下载对象计算。

已知引用文件为可选输入，支持：

- 每行一个对象键的纯文本；
- 包含上述对象键字段的 CSV 或 JSONL。

## 使用

```bash
python3 ops/storage-audit/audit.py objects.csv \
  --references known-references.txt \
  --output storage-report.json
```

只有在引用文件确实来自所有业务表、草稿、已发布内容、用户头像/附件、异步任务与外部消费者的同一时点快照时，才可加上 `--references-complete`：

```bash
python3 ops/storage-audit/audit.py objects.jsonl \
  --references all-known-references.jsonl \
  --references-complete \
  --output storage-report.json
```

该标志仅记录“完整扫描声明”，报告仍把无引用对象称为 `candidate_only`，不会产生删除清单。不加该标志时，缺失于已知引用的对象只能表示“当前引用导出中未出现”。

## 报告内容

- 对象数和总字节数；
- 按键前缀、文件类型、存储类型的数量与容量分布；
- 最大对象 Top N；
- 有可靠 SHA-256 的重复候选组、理论可回收容量和容量最大的 Top N 候选明细；
- 已提供引用文件时的无引用候选统计与最大候选。

若同一 SHA-256 组的文件大小不一致，工具会认定元数据存在矛盾，不估算该组可回收容量。

## 资源边界

清单和引用文件均逐行读取，最大对象使用有界堆。为了保持精确统计，不同 SHA-256、引用键和分布组仍需要内存，因此默认限制为：

- `--top 50`（最大 1000）；
- `--max-hashes 1000000`；
- `--max-references 2000000`；
- `--max-groups 10000`；
- `--duplicate-samples 5`（每组最大 100）。

超限时立即失败，不输出不完整结果。对超大清单，先按 COS 前缀分批导出，或在评估机器内存后显式调高限制。

## 验证

```bash
python3 -m unittest discover -s ops/storage-audit -p 'test_*.py' -v
python3 -m py_compile ops/storage-audit/audit.py ops/storage-audit/test_audit.py
```

实际降本动作应使用独立流程：业务负责人复核候选、隔离/软删除、保留可恢复窗口，最后再根据审计记录决定是否清理。
