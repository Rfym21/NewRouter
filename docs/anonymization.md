# 文本匿名化

全局开关和自定义规则位于 `/system-settings/request-policies/filtering`。全局默认关闭；渠道可跟随全局、强制开启或强制关闭。已开启匿名化的渠道使用当前保存的全局规则。每次请求持有规则快照，保存新规则不会改写正在进行的请求。

系统先在本地识别文本中的敏感值，用可逆假值发送给上游，再还原响应中的对应值。正文和工具参数按独立通道增量处理；JSON 参数先处理转义和 Unicode，再还原假值，最后按 JSON 字符串规则编码。只暂存可能被拆开的尾部，不等待完整回答；结束事件、错误和 EOF 前会排空已有尾部。

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

带签名的 Anthropic thinking/compaction、redacted_thinking，以及签名覆盖的 Gemini part 保持原样。OpenAI 的 encrypted_content 不解密、不改写。只有经过本代理并保留对应映射的历史，才能在后续请求中继续还原；引用未知、过期或不同令牌的状态会在上游提交前被拒绝。

新增规则不会改写已经生成的密文历史。需要让新规则覆盖此前历史文本时，应开始新会话并重新提交明文历史。

映射默认在闲置一小时后过期，内存缓存还受引用数和 32 MiB 总数据限制。启用 Redis 后，映射通过 AES-GCM 加密保存，并绑定用户/令牌对应的缓存键。多实例使用同一 Redis 和 CRYPTO_SECRET；未显式设置 CRYPTO_SECRET 时使用 SESSION_SECRET。未启用 Redis 时，重启或切换到其他实例后，需要开始新会话。

`ANONYMIZATION_CONTEXT_TTL_SECONDS` 控制闲置过期时间（60～86400 秒，默认 3600）；`ANONYMIZATION_MAX_CONTEXTS` 控制引用数（16～65536，默认 1024）。一个 response、output item、密文或签名各计一个引用。单份保存的映射最多 128 KiB，超限会在请求准备阶段返回错误。

## 可选语义识别

内置识别包括凭据、IP、邮箱等格式，以及经过校验的中国大陆手机号、身份证和明确标签的姓名/地址。任意自由文本中的姓名、机构或地址可通过自定义规则或自行部署的 Presidio Analyzer 补充：

```dotenv
ANONYMIZATION_PRESIDIO_URL=http://presidio-analyzer:3000
ANONYMIZATION_PRESIDIO_LANGUAGE=en
```

不配置 URL 时不会调用外部识别服务。Analyzer 需要自行部署并配置对应语言模型/识别器，中文模型不会自动下载。该服务会收到待识别的文本，应配置为受信任的识别服务。超时、非法范围、超量结果或重定向会中止请求，避免将未完成识别的原文继续发给模型。

## 处理边界

规则匹配文本和 JSON 工具载荷中的字符串。模型名、工具名称与 schema、元数据、数值、图片/音频/文件数据保留；此功能不包含 OCR 或媒体内容识别。原值只有在上游返回相同假值时才能还原，模型改写或截断假值后无法推断原值。

有 usage 时以供应商计量为准；缺少 usage 时保留既有输入估算，并基于未还原的输出文本计数。客户端还原后的原文长度不参与输出 Token 估算。
