# New Router

https://github.com/Rfym21/WorkSpace/tree/main/Projects/NewRouter (Current)

https://github.com/QuantumNous/new-api (Upstream)

当前同步基准：官方 `main` 的 Commit(`7de10f2c602b50122017556e8a09cb7c488c273c`)，同步日期 2026-10-14。

最初基于 Commit(`4e570389dd433a717373ce9c9b822b59f5ed3d5d`) 二次开发。

本次同步包含上次基准 `633b4c5b` 之后的 5 个官方提交，保留 New Router 的渠道标签权限、计费扩展、模型与响应去匿名化、日志异步导出和分享，以及自定义前端。

日志导出统一使用 New Router 的选列弹窗，提供后台压缩 CSV 任务和直接下载 CSV。后台任务保留导出记录、过期清理、分享及管理功能，复用官方逐行读取与日志列表筛选逻辑，避免按 ID 分页在 ClickHouse 下遗漏记录；两种导出均跟随当前视图范围和上游请求 ID 筛选，并补充缓存读写、流式和请求 ID 列及 CSV 公式转义。

- 面板访问令牌支持独立名称、有效期和权限范围；管理员敏感操作加入二次验证，并与现有 Casbin 权限保持一致。
- 任务插件迁移到 moejs，支持 `utils.fetch`、JSON 字段顺序和自动视频时长；更新 Seedream，并加入 xAI 视频插件。
- 加入 TokenKit，修复 Responses、Chat、Claude、Gemini 协议转换和联网搜索计费。
- 日志支持直接 CSV 下载、查询时间范围设置和消息国际化；原有异步压缩导出、分享及管理员管理继续保留。
- 更新日期时间选择、字体、图标、渠道刷新、用户分组筛选、账号独立 Playground 数据及相关依赖。

构建需要 Go 1.26 或更高版本。旧面板访问令牌在新版首次启动后有 30 天迁移期；可在安全设置中创建新的权限令牌。默认消息和服务端日志语言可通过 `DEFAULT_LANGUAGE` 设置。

GitHub Actions 支持手动运行，或在 `main` 分支的 `VERSION` 文件变化时，编译六个平台的可执行文件、发布 GitHub Releases，并推送 Linux 双架构镜像到 GHCR。触发方式、版本格式和镜像使用方法见 [编译与发布说明](.github/README.md)。
