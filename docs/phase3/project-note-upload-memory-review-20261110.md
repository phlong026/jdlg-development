# 私有笔记上传：内存与去重语义核查（T5.2）

- 日期：2026-11-10
- 任务：T5.2「上传内存与去重语义核查（书面结论；不预设 clean）」
- 结论状态：**代码核查完成，实测数据待部署环境补充**
- 关联：P4（图片 ≤20 MiB、视频 ≤500 MiB）、T5.1（超时）、D13/D26

## 一、结论（不预设 clean）

1. **内存峰值随视频体积线性增长，当前实现不是流式的。**
   - `project_notes.go` 上传处理先用 `io.ReadAll(io.LimitReader(source, max+1))` 把整个文件读入内存，再 `sha256`、`PrepareMedia`、`storeContentMedia`。
   - `storeContentMedia` 在超过 `contentstorage.MaxObjectSize`（50 MiB）时，为走 `COSStore.PutFile` 又把整块 `data` 写入临时文件（`putContentMediaObject`）。
   - 因此上传一个 500 MiB 视频的峰值是：**≈500 MiB 堆内存 + 临时文件 500 MiB 磁盘**；并发两个即 ≥1 GiB 堆。P4 把上限放宽到 500 MiB 后，这一路径的 OOM 风险显著上升。
2. **图片路径可接受。** 图片上限 20 MiB，全量读入可接受；`image.DecodeConfig` 只读取头信息。
3. **去重语义按内容摘要，语义正确但有放大。**
   - `ProjectNotePrepareMedia(..., fmt.Sprintf("%x", digest), "upload")` 以 SHA-256 内容摘要做幂等键，返回 `already` 时跳过写存储。
   - 副作用：重复上传同一文件仍会完整读入内存并计算摘要（无早退），大文件重复上传的成本与首次相同。
4. **失败补偿存在缺口（与 D26 一致，非本轮新增）。** 写存储失败可能遗留 pending journal；写成功但 Finalize 失败可能遗留私有孤儿对象。

## 二、证据（代码位置）

| 位置 | 行为 |
|---|---|
| `service/route/project_notes.go` 上传处理 | `ParseMultipartForm` 后 `io.ReadAll(LimitReader(source, max+1))`，整文件驻留 |
| 同上 | `digest := sha256.Sum256(data)`，全量哈希 |
| `service/route/content_media_storage.go` `putContentMediaObject` | >50 MiB 时 `os.CreateTemp` + `tmp.Write(data)`，再 `PutFile`（临时文件放大） |
| `service/contentstorage/cos.go` `putMultipartFile` | 分片上传，片大小由 `cosMultipartPartSize` 控制，不额外整文件驻留 |
| `service/route/project_notes.go` 上传收尾 | `ProjectNotePrepareMedia` 的 `already` 分支跳过写入；Finalize 失败无回滚 |

## 三、实测方案（待部署环境执行）

1. 准备 1 MiB / 50 MiB / 200 MiB / 500 MiB 四个 MP4，上传到私有笔记项目。
2. 上传期间采样：
   - 进程 RSS（`ps -o rss` 或 `cat /proc/<pid>/status`）峰值；
   - goroutine 数（`pprof` 或 debug 端点，若可用）；
   - 临时目录（`$TMPDIR`）峰值占用；
   - 上传耗时与是否 900s 超时。
3. 重复上传同一文件两次，确认第二次命中 `already`（没有新增对象）但仍观察内存峰值。
4. 并发 2 个 500 MiB 上传，确认是否 OOM 或触发 Read/Write 超时。

## 四、整改建议（不属于本任务改动面）

- **改为流式落盘**：multipart 直接 `io.Copy` 到临时文件，边写边算哈希与 MIME 嗅探（前 512 字节 + MP4 尺寸解析可基于文件读取），再走 `PutFile`；避免 `ReadAll`。
- 图片仍可全量读取（≤20 MiB）。
- 重复上传去重可以保留摘要语义，但在流式实现后成本自然下降。
- 补偿：写存储失败清理 pending journal；Finalize 失败回收已写入对象（D26）。

## 五、与 P4 的关系

P4 已把产品上限定为图片 20 MiB / 视频 500 MiB，服务端路由与 COS 大对象写入已对齐（T2.4 批次）。**在流式改造完成前，500 MiB 视频属于「能上传成功、但内存风险高」的区间**，需要部署侧限制并发或先完成流式改造。
