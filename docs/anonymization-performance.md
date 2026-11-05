# 匿名化性能与复现实验

2026-11-05，Windows amd64，Go 1.27.3，AMD Ryzen 9 9950X3D（16 物理核）。本地测试不调用真实上游、不启用 Redis 或 Presidio。原始实现基于提交 `76c52257c43b76374d261cfcbdf0c4a00e6753d0`；冻结的旧检测器保留在 `pkg/anonymizer/detector_reference_test.go` 中，供差分测试与同进程基准使用。

## 实现

1. **共享前置过滤**：从内置正则语法提取必需字符、字节长度下界、连续数字/十六进制/单词字符的长度下界，以及必需 ASCII 字面串。一次 DFA 扫描同时查找这些字面串。过滤器只排除不可能命中的规则，候选仍交给原始正则处理，保持捕获组、边界、UTF-8 字节位置和规则优先级。大小写过滤保守归一化，包含 Unicode 的 Kelvin sign 和 long s。
2. **有序区间检查**：内置检测器用二分查询已接受区间，每条规则结束后合并有序新增区间。请求侧识别旧假名时复用同样的区间索引，避免对已互不重叠的启发式结果反复两两比较。
3. **跨请求检测缓存**：仅缓存本地检测的范围、类型、规则名称；不缓存原文、最终替换文本、假名或外部 NER 响应。键包含用户/令牌范围摘要、规则指纹、文本 SHA-256 和 UTC 日期。日期参与键是因为身份证出生日期校验依赖当前时间。规则变更自然隔离旧条目，重启自然清空检测器版本的旧结果。每个请求仍独立执行映射生成、旧假名识别、碰撞处理和计数。
   缓存采用 LRU，最多 4096 条、32 MiB 元数据预算，TTL 为 10 分钟；单条结果预算最多 128 KiB，超过 1 MiB 的文本直接检测。负结果也缓存。预算按条目、范围、字符串和容器开销估算，不代表进程 RSS 上限。互斥锁只覆盖索引操作，检测和结果重建在锁外完成。
4. **流式还原复用**：每个响应快照构建一次替换器，JSON 子流共享不可变索引。压缩前缀树用于尾部和实际命中查询，节点数随映射数增长，长假名不会逐字节创建节点。单映射使用 `strings.ReplaceAll`；无匹配块直接返回，避免分配。跨块尾部和完整假名保护逻辑保持原行为。
5. **稳定假名验证**：本地测试覆盖完整 Claude 请求的多轮历史、跨请求相同前缀、跨身份差异、随机模式差异与还原一致性；另附显式启用的上游对照实验。稳定假名开关维持现有默认值。

## 本地结果

纯检测为三次运行的中位数，输入为 150 KiB（153600 字节）；旧、新检测器来自同一次最终基准。完整请求和流式旧值来自修改前实测。语料生成函数保留在 `performance_test.go`，包含无匹配正文、代码、密集匹配和中文正文。

| 场景 | 原实现 | 优化后 | 比值 |
| --- | ---: | ---: | ---: |
| 普通正文纯检测 | 153.42 ms | 21.62 ms | 7.1 倍 |
| 代码正文纯检测 | 158.57 ms | 16.64 ms | 9.5 倍 |
| 密集匹配纯检测 | 181.99 ms | 54.03 ms | 3.4 倍 |
| 中文正文纯检测 | 42.52 ms | 4.13 ms | 10.3 倍 |
| 150 KiB 正文完整请求，冷检测 | 186.12 ms | 43.79 ms | 4.3 倍 |
| 同请求重复历史，检测缓存命中 | 186.12 ms | 0.674 ms | 276 倍 |
| 200 个短文本槽，冷检测 | 11.84 ms | 2.190 ms | 5.4 倍 |
| 200 个短文本槽，检测缓存命中 | 11.84 ms | 0.265 ms | 44.7 倍 |
| 1000 映射，40 B 无命中流块 | 36.06 µs | 0.036 µs | 约 1000 倍 |
| 1000 映射，39 B 含假名流块 | 37.52 µs | 0.117 µs | 约 320 倍 |

缓存命中值针对完全重复的正文；编辑过的文本仍需检测。完整请求仍解析、哈希和改写历史，不等于只读取增量，也不改变累计历史字节量的增长规律。冷请求基准禁用缓存存储，确保每次运行都实际检测。它不包含首次插入缓存的少量元数据开销。

流块测量不含索引初始化。1000 映射的快照与前缀索引初始化中位数为 95.28 µs、约 278 KiB 分配；替换器在首次实际替换时还有一次懒构建成本，表中流块为稳态值。无命中流块零分配，含假名流块从 2600 B/7 次分配降到 120 B/3 次分配。完整请求热缓存基准约 1.32 MB 分配，比原来的约 1.17 MB 略高；当前主要收益是 CPU 时间，并未宣称全面降低内存。

并发基准使用相同的热缓存请求，单次运行 500 ms，最多采样前 4096 次调用。`ns/op` 表示吞吐的倒数；下表的 P50/P95 单独在每次调用外计时。Windows 短时间测量存在时钟粒度影响，结果只作本机观测。

| GOMAXPROCS / 默认 worker 数 | 聚合吞吐 MB/s | 调用 P50 | 调用 P95 |
| --- | ---: | ---: | ---: |
| 1 | 245.85 | 0.999 ms | 1.504 ms |
| 4 | 770.23 | 1.001 ms | 1.502 ms |
| 16 | 1167.57 | 2.136 ms | 3.195 ms |
| 32 | 1084.52 | 4.531 ms | 6.704 ms |

吞吐和延迟受 CPU、内存分配及 GC 影响；表中 32 worker 已低于 16 worker 的吞吐，不据此作“并发越高越快”或无需限流的结论。

原始输出保留在：

- [修改前](benchmarks/anonymization-before.txt)
- [最终检测、冷请求和热请求](benchmarks/anonymization-after.txt)
- [最终压缩索引与流块](benchmarks/anonymization-stream.txt)
- [并发吞吐与调用延迟](benchmarks/anonymization-parallel.txt)

## 本地复现

在仓库根目录执行：

```powershell
go test ./pkg/anonymizer -count=1
go vet ./pkg/anonymizer
go test ./pkg/anonymizer -run '^$' -bench 'Benchmark(ReferenceDetect|Detect|ColdRequest|Request|Stream)Performance' -benchmem -benchtime=300ms -count=3 -cpu=1
go test ./pkg/anonymizer -run '^$' -bench 'BenchmarkStreamSetupPerformance' -benchmem -benchtime=300ms -count=3 -cpu=1
go test ./pkg/anonymizer -run '^$' -bench '^BenchmarkParallelRequestThroughput$' -benchmem -benchtime=500ms '-cpu=1,4,16,32'
go test ./pkg/anonymizer -run '^$' -fuzz '^FuzzDetectorOptimization$' -fuzztime=30s -parallel=4
go test ./pkg/anonymizer -run '^$' -fuzz '^FuzzStreamOptimization$' -fuzztime=20s -parallel=4
```

差分测试覆盖全部内置规则生成的匹配样本、中文、无效 UTF-8、保留区间及 Unicode 大小写折叠。缓存测试覆盖身份隔离、规则版本、跨日、过期、条目与字节预算、并发访问、负结果、缓存命中后新建随机映射及导入旧假名。流式索引与旧逻辑逐项对比，已有协议流式测试继续覆盖拆块、JSON 转义和 EOF 排空。

竞态检测命令为 `go test -race ./pkg/anonymizer`；本次 Windows 环境 `CGO_ENABLED=0` 且缺少本机 C 编译器，未执行成功。并发访问测试已正常通过，不能替代 race 检测。

## 真实上游对照脚本

按本次约定只完成本地验证与脚本，没有发送真实上游请求，也没有真实命中率或费用结果。普通测试会跳过 `TestLivePromptCacheExperiment`。

设置环境变量 `ANONYMIZATION_EXPERIMENT_API_KEY` 后执行：

```powershell
./scripts/anonymization-cache-experiment.ps1 -Endpoint 'https://YOUR-ENDPOINT/v1/messages' -Model 'YOUR-MODEL'
```

脚本固定发送四次收费请求：随机假名两轮、稳定假名两轮。输入完全合成，每个模式有独立实验前缀；每轮创建独立映射，第二轮增加消息历史，在 64 KiB 系统正文的相同位置设置显式 `cache_control`。只输出 JSON 测量，不输出密钥、正文、假名或模型回答。运行完恢复相关进程环境变量，密钥不放在命令行。HTTPS 端点不得带 URL 凭据、query 或 fragment，且客户端拒绝重定向。

输出包括本地匿名化耗时、从提交上游到首个非空文本 delta 的时间、上游总耗时及原始 `input_tokens`、`cache_read_input_tokens`、`cache_creation_input_tokens`、`output_tokens`。计时不包含响应还原，与客户端看到首字的完整链路延迟有区别。若需要费用估计，可额外传入同币种、每百万 token 的四项现行单价：`-InputPricePerMillion`、`-ReadPricePerMillion`、`-WritePricePerMillion`、`-OutputPricePerMillion`；脚本会输出 `estimated_cost`。未提供单价时不估算费用，结果也不等于最终账单。

直连供应商，或使用关闭二次匿名化、缓存用量重写和缓存比例修正的测试渠道，避免污染对照。不要把实验发送到会重新随机匿名化正文的渠道。检查模型最低缓存长度、权限、路由和 TTL；前缀一致是命中的必要条件，未命中也要记录。四次调用仅验证机制，正式比较延迟时应跨多次实验、交换模式顺序并扩大样本，不能由单次 TTFT 判断因果。

协议依据：[Anthropic 提示词缓存](https://platform.claude.com/docs/en/build-with-claude/prompt-caching)、[流式事件与累计 usage](https://platform.claude.com/docs/en/build-with-claude/streaming)。本地 HTTP fixture 只验证脚本请求和 SSE 测量，不是实际供应商缓存的证据。
