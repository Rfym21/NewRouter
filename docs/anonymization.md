# 文本匿名化

全局开关和自定义规则位于 `/system-settings/request-policies/filtering`。全局默认关闭；渠道可跟随全局、强制开启或强制关闭。已开启匿名化的渠道使用当前保存的全局规则。每次请求持有规则快照，保存新规则不会改写正在进行的请求。

系统先在本地识别文本中的敏感值，用可逆假值发送给上游，再还原响应中的对应值。正文和工具参数按独立通道增量处理；JSON 参数先处理转义和 Unicode，再还原假值，最后按 JSON 字符串规则编码。只暂存可能被拆开的尾部，不等待完整回答；结束事件、错误和 EOF 前会排空已有尾部。

用量与错误日志在请求实际完成匿名化替换时记录 `other.anonymization_count`，界面和 CSV 导出可查看匿名化数量。相同敏感值在不同位置出现时分别计数，预扫描、重试和协议转换不会重复累加；计数取各次成功处理的完整请求中的最大数量，包含后续加入的系统提示词或参数覆盖。仅导入历史映射不会增加计数。新增日志元数据只记录整数，不保存匹配文本、实体值、规则名称或原值与假值映射。匿名化请求的参数覆盖说明仅保留操作和字段路径，具体值用 `[REDACTED]` 代替，避免嵌套 JSON 工具参数泄漏原文；流式诊断和错误日志中的已知敏感值及假值也会遮蔽。

| 接口 | 处理的文本 |
| --- | --- |
| OpenAI `/v1/completions` | prompt、suffix、各 choice 的 text |
| OpenAI `/v1/chat/completions` | 所有角色的正文、推理/refusal、旧 function_call 和新 tool_calls 参数 |
| OpenAI `/v1/responses` | instructions、输入/输出文本、推理摘要、函数与自定义工具输入/结果、增量参数 |
| OpenAI `/v1/responses/compact` | 压缩前的明文输入；加密压缩项保持原样并关联原映射 |
| Anthropic Messages | system、消息正文、tool_use.input、tool_result.content；按 block 处理流式工具 JSON |
| Gemini | 文本、函数参数/结果、partialArgs；按 candidate、part、参数路径隔离 |
| Embeddings | 文本 input，以及 Gemini 单次/批量 embedding 文本 |

## 自定义规则

规则支持关键词精确匹配和 Go/RE2 正则，不支持回溯引用或前后查找。每条规则可以设置名称、实体类型、启用状态和捕获组；捕获组 `0` 替换整个匹配，其他值只替换指定组。规则按列表顺序处理重叠，自定义规则优先于内置识别，之后可补充外部 NER 识别。

最多 128 条规则，每个 pattern 最多 4096 字节，完整规则 JSON 最多 128 KiB。名称不可重复；可匹配空值的正则或捕获组会在保存前被拒绝。停用规则也需要保持合法语法。

例如，只替换工单正文中的内部编号：

```json
[
  {
    "name": "内部工单编号",
    "mode": "regex",
    "pattern": "工单编号[:：]\\s*(INC-[0-9]+)",
    "entity_type": "CUSTOM",
    "capture_group": 1,
    "enabled": true
  }
]
```

设置页提供草稿匹配与匿名化预览，预览不会保存文本、规则或会话映射。保存使用原子的请求策略接口，校验或数据库写入失败时继续使用旧规则。

## 会话与压缩上下文

普通请求使用独立映射。需要重放状态时，按已认证的用户和 API 令牌查找 `previous_response_id`、conversation、item_reference、密文指纹或签名。不同用户或令牌不能借助同一个上游 ID 获取其他映射；导入的映射是副本，不共享可变 Vault。

带签名的 Anthropic thinking/compaction、redacted_thinking，以及签名覆盖的 Gemini part 保持原样。OpenAI 的 encrypted_content 不解密、不改写。引用未知、过期或当前令牌无法读取的映射时，默认跳过缺失引用，继续导入可用映射并匿名化当前明文，服务日志只记录跳过的引用数量。缺失的历史映射可能导致旧假名无法还原，不透明历史也无法重新检查。在 `/system-settings/request-policies/filtering` 开启“严格校验匿名化上下文”（`AnonymizationStrictContextEnabled`）后，缺失映射会在上游提交前被拒绝。开关保存后对新请求热生效，映射冲突仍会返回错误。

新增规则不会改写已经生成的密文历史。需要让新规则覆盖此前历史文本时，应开始新会话并重新提交明文历史。

映射默认在闲置一小时后过期，内存缓存还受引用数和 32 MiB 总数据限制。启用 Redis 后，映射通过 AES-GCM 加密保存，并绑定用户/令牌对应的缓存键。多实例使用同一 Redis 和 CRYPTO_SECRET；未显式设置 CRYPTO_SECRET 时使用 SESSION_SECRET。未启用 Redis 时，重启或切换到其他实例后会丢失历史映射，后续请求按严格校验开关处理。

闲置过期时间、引用数上限和单份映射大小可在 `/system-settings/operations/runtime` 的“匿名化上下文缓存”中调整，保存后热生效（Redis 中已写入条目按原 TTL 自然过期）。`ANONYMIZATION_CONTEXT_TTL_SECONDS`（60～86400 秒，默认 3600）、`ANONYMIZATION_MAX_CONTEXTS`（16～65536，默认 1024）和 `ANONYMIZATION_MAX_CONTEXT_KB`（64～16384 KiB，默认 4096）仅作为初始默认值。一个 response、output item、密文或签名各计一个引用。单份映射超过大小上限时，本次请求仍正常完成，但该轮续聊映射不会保存，服务日志会记录降级原因。

## 稳定假名模式

默认模式下，每个请求都为敏感值随机生成新假值；完整重发历史的客户端（如 Claude Code 等 Anthropic Messages 协议工具）每轮前缀都会变化，上游提示词缓存无法命中。在 `/system-settings/request-policies/filtering` 开启“启用稳定假名模式”后，同一敏感值在同一用户和令牌内始终映射到同一假值：假值由 `HMAC(以 CRYPTO_SECRET 派生的用户/令牌盐, 实体值)` 确定性生成，跨请求、跨实例一致，前缀缓存恢复正常命中；Responses 式续聊的响应还原仍依赖上面的上下文缓存。

该模式的安全性边界：上游可为同一用户会话内的同一实体建立稳定关联（上游本来就能通过 API Key 关联同用户请求，属既有暴露面）；不同用户或令牌之间的假值互不相同，无法跨身份关联；更换 CRYPTO_SECRET 后全部假值随之改变。追求最大不可关联性时保持默认的随机模式即可。

## 可选语义识别

内置识别包括凭据、IP、邮箱等格式，以及经过校验的中国大陆手机号、身份证和明确标签的姓名/地址。任意自由文本中的姓名、机构或地址可通过自定义规则、本地 AstrLink Guard 模型或自行部署的 Presidio Analyzer 补充。

在 `/system-settings/operations/runtime` 的“匿名化识别器”中配置本地模型目录或 Presidio URL，两项只能填写一项。保存后持久化并对新请求热生效，两个字段均清空时使用内置检测和自定义规则。环境变量仅提供初始默认值，保存的空值也会覆盖环境变量。

本地目录填写服务器或容器内的路径，需预先放入 `QuantumNous/astrlink-guard` 的 `7216b6a3b897ef903d4b1ba367ae6fb95b0fa5e4` 版本文件 `model_int8.onnx`、`model_int8.onnx_data` 和 `tokenizer.json`。加载时校验文件大小和 SHA-256。模型在进程内运行，首次使用时加载。

环境变量配置示例，两种识别器选用其中一种。

```dotenv
# ANONYMIZATION_LOCAL_NER_MODEL_DIR=/opt/models/astrlink-guard
ANONYMIZATION_PRESIDIO_URL=http://presidio-analyzer:3000
ANONYMIZATION_PRESIDIO_LANGUAGE=en
```

Analyzer 需要自行部署并配置对应语言模型/识别器，中文模型不会自动下载。该服务会收到待识别的文本，应配置为受信任的识别服务。超时、非法范围、超量结果或重定向会中止请求，避免将未完成识别的原文继续发给模型。

## 处理边界

本地性能优化、检测缓存边界、基准原始输出和稳定假名对照脚本见 [匿名化性能与复现实验](anonymization-performance.md)。

规则匹配文本和 JSON 工具载荷中的字符串。模型名、工具名称与 schema、元数据、数值、图片/音频/文件数据保留；此功能不包含 OCR 或媒体内容识别。原值只有在上游返回相同假值时才能还原，模型改写或截断假值后无法推断原值。

有 usage 时以供应商计量为准；缺少 usage 时保留既有输入估算，并基于未还原的输出文本计数。客户端还原后的原文长度不参与输出 Token 估算。
