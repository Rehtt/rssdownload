# RSSDownload

一个面向 [Mikan Project](https://mikanani.me/) 的自托管 RSS 自动下载工具。

添加 MyBangumi RSS 后，RSSDownload 会持续发现新条目、获取种子并提交给
aria2。订阅、下载进度和异常处理都可以在浏览器中完成，适合希望把追番下载
流程放到 NAS、家用服务器或个人主机上长期运行的用户。

> RSSDownload 负责订阅与任务管理，实际文件下载由独立运行的 aria2 完成。

## 功能亮点

- **自动追踪更新**：定时检查 Mikan MyBangumi RSS，也可以随时手动同步。
- **安全的首次同步**：默认只记录已有条目，不会一上来下载整个历史列表；也可
  选择立即下载当前条目。
- **aria2 自动下载**：自动获取种子并提交任务，实时展示等待、下载、暂停、完成
  和失败状态。
- **可视化管理**：通过 WebUI 查看运行概览，搜索和筛选条目，手动下载、暂停、
  继续或重试任务。
- **灵活的下载目录**：支持全局下载目录，也可以为每条订阅指定独立目录。
- **订阅生命周期管理**：支持启停、归档和恢复，历史条目与下载记录会继续保留。
- **网络代理**：获取 RSS 和种子时可使用 HTTP(S) 或 SOCKS5 代理。
- **本地保存数据**：配置、订阅和历史记录均保存在自己的数据目录中，方便迁移
  与备份。

## Docker Compose 部署

推荐在 Linux 主机上使用 Docker Engine 和 Docker Compose v2 部署。Compose 会
同时启动 RSSDownload 和 `superng6/aria2:a2b-latest`：

```bash
cp .env.example .env
```

编辑 `.env`，至少填写两个不同的强密码：

```dotenv
RSSDOWNLOAD_ADMIN_PASSWORD=replace-with-a-strong-admin-password
ARIA2_RPC_SECRET=replace-with-a-different-aria2-secret
```

如果希望下载文件由当前宿主机用户管理，同时把 `.env` 中的 `PUID` 和 `PGID`
分别改为 `id -u`、`id -g` 的输出。然后启动服务：

```bash
mkdir -p data/aria2 downloads
docker compose up -d
```

打开 <http://127.0.0.1:8080>，登录后进入“系统设置”并填写：

- aria2 JSON-RPC 地址：`http://aria2:6800/jsonrpc`
- aria2 RPC Secret：`.env` 中的 `ARIA2_RPC_SECRET`
- 默认下载目录：`/downloads`

aria2 RPC 端口只对 Compose 内部网络开放；RSSDownload Web 端口和 aria2 BT
TCP/UDP 端口可以通过 `.env` 调整。A2B 模式需要 `NET_ADMIN` 能力和只读的
`/lib/modules` 挂载，因此该 Compose 配置面向 Linux 主机，不适用于 rootless
Docker 或 Docker Desktop。

数据默认保存在以下位置：

- RSSDownload 数据库：Docker 命名卷 `rssdownload-data`
- aria2 配置：`./data/aria2`
- 下载文件：`./downloads`

更新稳定版镜像并重建容器：

```bash
docker compose pull
docker compose up -d
```

停止 RSSDownload 后，可以使用标准 Docker volume 工具备份或恢复
`rssdownload-data`；aria2 配置和下载文件直接备份对应的宿主机目录即可。

## 从源码运行

### 1. 构建程序

从源码构建需要 Go 1.26.4、Node.js 24+ 和 pnpm 11+：

```bash
pnpm --dir web install --frozen-lockfile
make build
```

构建完成后，可执行文件位于 `dist/rssdownload`。如果已经有构建好的可执行文件，
可以直接进入下一步。

### 2. 启动 aria2

已有 aria2 服务时，只需确认 JSON-RPC 已启用。下面是一份可以直接使用的本机
配置示例：

```bash
aria2c \
  --enable-rpc=true \
  --rpc-listen-all=false \
  --rpc-listen-port=6800 \
  --rpc-secret='replace-with-a-secret' \
  --continue=true
```

请记住这里设置的 RPC Secret，稍后需要在 RSSDownload 中填写。

### 3. 启动 RSSDownload

首次启动必须设置管理员密码，密码至少包含 8 个字符：

```bash
RSSDOWNLOAD_ADMIN_PASSWORD='change-me-now' ./dist/rssdownload
```

打开 <http://127.0.0.1:8080>，使用用户名 `admin` 和刚刚设置的密码登录。
管理员创建成功后，后续启动直接运行 `./dist/rssdownload` 即可。

默认情况下：

- 服务仅监听本机的 `127.0.0.1:8080`；
- 数据保存在 `./data`；
- RSS 每 10 分钟检查一次。

## 首次使用

1. 进入“系统设置”，填写 aria2 JSON-RPC 地址和 RPC Secret，然后点击
   “测试 aria2”。本机默认地址为 `http://127.0.0.1:6800/jsonrpc`。
2. 根据需要设置默认下载目录、轮询间隔和网络代理，并保存设置。
3. 从 Mikan 获取形如
   `https://mikanani.me/RSS/MyBangumi?token=...` 的个人 RSS 地址，在“订阅管理”
   中添加订阅。
4. 在“运行概览”和“条目与下载”中查看新条目、下载进度或错误信息。

### 首次同步怎么选？

- **仅建立基线（默认）**：记录当前已有条目，之后只自动下载新发现的内容。
- **下载当前条目**：首次同步时，也将 RSS 中当前有效的条目加入下载队列。

修改已有订阅的 RSS 地址后，新地址同样会先建立安全基线，避免误下载大量历史
内容。

## 常用运行配置

| 环境变量 | 默认值 | 用途 |
| --- | --- | --- |
| `RSSDOWNLOAD_ADMIN_PASSWORD` | 无 | 首次创建管理员或重置密码时使用 |
| `RSSDOWNLOAD_ADMIN_USERNAME` | `admin` | 管理员用户名 |
| `RSSDOWNLOAD_LISTEN` | `127.0.0.1:8080` | Web 服务监听地址 |
| `RSSDOWNLOAD_DATA_DIR` | `./data` | 配置、订阅和历史记录的数据目录 |
| `RSSDOWNLOAD_COOKIE_SECURE` | `false` | 使用 HTTPS 时设为 `true` |

监听地址和数据目录也可以通过 `--listen`、`--data-dir` 参数指定，命令行参数
优先于环境变量。

### 忘记管理员密码

先停止正在运行的 RSSDownload，再执行：

```bash
RSSDOWNLOAD_DATA_DIR='./data' \
RSSDOWNLOAD_ADMIN_USERNAME='admin' \
RSSDOWNLOAD_ADMIN_PASSWORD='new-password' \
./dist/rssdownload admin reset-password
```

密码重置后，已有登录会话会全部失效。

### 备份与恢复

停止 RSSDownload 后，复制整个数据目录即可完成备份；恢复时将数据目录放回原位
再启动程序。aria2 下载的文件不在此数据目录中，需要按自己的下载目录单独备份。

## 公网访问建议

RSSDownload 默认只允许本机访问。如果需要通过局域网或公网使用，建议放在 HTTPS
反向代理之后，并设置 `RSSDOWNLOAD_COOKIE_SECURE=true`。不要将未加密、无访问
控制的管理页面直接暴露到公网。

## 当前边界

- 目前仅支持 Mikan MyBangumi RSS 地址。
- 需要自行安装和运行 aria2，RSSDownload 不包含下载引擎。
- 不负责解析番剧名、集数、字幕组或画质，也不负责文件改名和媒体库整理。
- 归档订阅不会删除 aria2 任务或已下载文件。
- 当前为单管理员模式。

## 开发

```bash
# 运行全部测试
make test

# 分别启动后端和前端开发服务
RSSDOWNLOAD_ADMIN_PASSWORD='change-me-now' make dev-api
make dev-web
```

前端开发服务默认地址为 <http://127.0.0.1:5173>。

### 本地构建容器镜像

```bash
docker build \
  --build-arg VERSION=dev \
  --build-arg COMMIT="$(git rev-parse HEAD)" \
  --build-arg BUILD_TIME="$(date -u +'%Y-%m-%dT%H:%M:%SZ')" \
  -t rssdownload:dev .
```

正式版本会在推送合法的 `v*` 标签时自动构建 `linux/amd64` 和 `linux/arm64`
镜像，并发布到 `ghcr.io/rehtt/rssdownload`。稳定版本同时维护语义化版本标签和
`latest`；预发布版本不会覆盖 `latest`。GHCR package 首次创建时默认为私有，
维护者需要在 GitHub 的 Package settings 中将其可见性改为 Public，Compose
才能在未登录 GHCR 的主机上直接拉取。
