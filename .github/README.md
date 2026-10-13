# 编译与发布

工作流位于 [workflows/release.yml](workflows/release.yml)，使用仓库自带的 `GITHUB_TOKEN` 创建 GitHub Releases 和推送 GitHub Container Registry（GHCR）镜像，无需配置 Docker Hub 账号或额外 Token。

## 触发发布

- **自动触发**：向 `main` 分支推送包含根目录 `VERSION` 变更的提交。
- **手动触发**：在 GitHub 仓库的 **Actions → Build and Release → Run workflow** 中选择分支并运行，版本仍读取该分支的 `VERSION`。工作流需要先提交到默认分支，GitHub 才会显示手动运行入口。

`VERSION` 使用 `2.0.0`、`v2.0.0` 或 `2.1.0-rc.1` 等格式，不支持含 `+` 的构建元数据。前端和后端注入文件中的原始版本号，Release 标签统一为 `v<版本>`，例如 `v2.0.0`。每次发布新提交前应增加版本号；同一提交可以重新运行，更新该版本的镜像和 Release 资产。如果已有标签指向不同提交，工作流会在构建前失败，避免发布包与标签内容不一致。

## 发布内容

工作流只用 Bun 1.4.0 和 `web/bun.lock` 编译一次前端，各平台的 Go 1.26.1 构建共用这份前端资源，并保留 PGO、`CGO_ENABLED=0` 和 `GOEXPERIMENT=greenteagc` 配置。构建前先执行 `go mod download`，确保冷缓存环境下也能读取 moejs 的 `default.pgo`。

六个平台各编译一次。Docker 任务直接下载 Linux amd64、arm64 的 Release 压缩包，提取其中已经嵌入前端的二进制，再通过 [docker/Dockerfile](docker/Dockerfile) 组装双架构镜像，无需再次编译前端或后端。镜像内的二进制与对应 Release 包一致。根目录 `Dockerfile` 继续用于本地从源码完整构建。

| 系统 | 架构 | Release 资产 |
| --- | --- | --- |
| Linux | amd64、arm64 | `new-router_<版本>_linux_<架构>.tar.gz` |
| macOS | amd64、arm64 | `new-router_<版本>_darwin_<架构>.tar.gz` |
| Windows | amd64、arm64 | `new-router_<版本>_windows_<架构>.zip` |

每个压缩包包含可执行文件、`VERSION`、`LICENSE`、`NOTICE` 和 `THIRD-PARTY-LICENSES.md`。Release 还提供 `checksums.txt` 和自动生成的发布说明。只有六个平台的二进制构建及镜像推送全部成功，才创建或更新 Release。

镜像发布到固定地址 `ghcr.io/rfym21/new-router`，包含 `linux/amd64` 和 `linux/arm64`，可使用以下标签：

- `<版本>`，例如 `2.0.0`。
- `v<版本>`，例如 `v2.0.0`。
- `sha-<完整提交 SHA>`。
- `latest`，仅默认分支上的正式版本更新此标签。

含 `-rc.1`、`-beta.1` 等后缀的版本标记为 GitHub 预发布，不更新 `latest`。可以通过以下命令拉取镜像：

```sh
docker pull ghcr.io/rfym21/new-router:2.0.0
docker pull ghcr.io/rfym21/new-router:latest
```

## GitHub 设置

仓库需要启用 GitHub Actions，并允许工作流使用 `contents: write` 和 `packages: write` 权限；工作流只为对应发布任务申请这些权限。首次推送后，如需让未登录的用户拉取镜像，在仓库 **Packages → new-router → Package settings** 中将包的可见性设为 **Public**。如果复用已存在的 GHCR 包，需要在包设置中允许本仓库的 Actions 访问。
