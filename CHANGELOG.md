# Changelog

## v0.1.1

- 修复 aria2 未知 GID 报错识别与真实文案不匹配的问题，此前新任务始终无法提交给 aria2。
- 新增 `RSSDOWNLOAD_TRUSTED_PROXIES` 配置：仅信任所配置反向代理的 `X-Forwarded-*` 请求头，反代部署下登录限流按真实客户端 IP 计数。
- 新增 Dockerfile、包含 aria2 的 Docker Compose 部署示例，以及向 GHCR 发布多架构镜像的自动化流程。

## v0.1.0

- 支持订阅 Mikan MyBangumi RSS、定时发现新条目，并通过安全基线避免首次同步误下载历史内容。
- 集成 aria2 JSON-RPC，支持提交下载以及暂停、继续、重试和状态查看。
- 提供 Web 管理界面，用于查看运行概览以及管理订阅、条目、下载任务和系统设置。
- 支持自定义下载目录、HTTP(S) 与 SOCKS5 代理、本地 SQLite 数据存储和单管理员认证。
