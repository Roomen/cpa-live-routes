# Live Routes

[English](README.md) | [简体中文](README_CN.md)

Live Routes 是 [CLIProxyAPI](https://github.com/router-for-me/CLIProxyAPI)（CPA）的动态库插件，提供实时请求路由看板。它展示每个请求从客户端（Claude Code、Codex、各类 SDK 等）经客户端 API Key 与路由规则，到上游凭证和模型的完整流向，包括进行中状态、重试与 429 退避、Token 用量，以及会话与子代理的关系。页面由 CPA 自身提供，无需额外进程，所有数据只保存在内存中。

![流向视图](docs/images/flow-light.png)

![时间轴视图（深色）](docs/images/timeline-dark.png)

## 运行要求

- CLIProxyAPI v8 及以上，需支持动态库插件（插件协议 schema 6）。已在 v8.0.29 上测试。
- 支持的平台：macOS arm64/amd64、Linux amd64/arm64、Windows amd64。
- 插件是在 CPA 进程内运行的受信任代码，CPA 默认关闭插件，安装前需在 CPA 配置里设置 `plugins.enabled: true`。
- CPA Home 模式会禁用插件资源页，因此看板在该模式下不可用。

## 安装

### 1. 开启插件并启用本插件

在 CPA 的 `config.yaml` 中加入：

```yaml
plugins:
  enabled: true
  dir: "plugins"          # 相对 CPA 的工作目录；支持 "~/..."
  configs:
    live-routes:
      enabled: true
      priority: -100      # 排在其他请求拦截器之后
```

### 2. 安装动态库

**插件商店**：如果你的 CPA 使用的插件源里已收录 Live Routes，可在管理面板的插件商店直接安装。提交到插件源时使用 `store/registry-entry.json` 这份条目。

**手动下载 Release**：从 [Releases 页面](https://github.com/Roomen/cpa-live-routes/releases) 下载 `live-routes_<version>_<goos>_<goarch>.zip` 和 `checksums.txt`，把动态库放进 `<plugins.dir>/<goos>/<goarch>/`（直接放在 `<plugins.dir>/` 下也能识别）。

macOS / Linux：

```sh
VERSION=0.1.0; OS=darwin; ARCH=arm64          # 也可以是 linux/amd64、linux/arm64、darwin/amd64
ZIP=live-routes_${VERSION}_${OS}_${ARCH}.zip
grep "$ZIP" checksums.txt | shasum -a 256 -c -  # 或：sha256sum -c --ignore-missing checksums.txt
mkdir -p plugins/$OS/$ARCH
unzip -o "$ZIP" -d plugins/$OS/$ARCH           # macOS 为 live-routes.dylib，Linux 为 live-routes.so
xattr -d com.apple.quarantine plugins/$OS/$ARCH/live-routes.dylib 2>/dev/null || true   # 仅 macOS
```

Windows（PowerShell）：

```powershell
$zip = "live-routes_0.1.0_windows_amd64.zip"
(Get-FileHash $zip -Algorithm SHA256).Hash.ToLower()   # 与 checksums.txt 对比
Expand-Archive $zip -DestinationPath plugins\windows\amd64 -Force   # live-routes.dll
```

**从源码安装**：运行 `make install-local PLUGINS_DIR=/path/to/plugins`，详见下文[从源码构建](#从源码构建)。

### 3. 重启 CPA 并打开看板

重启 CPA（或在管理面板重载插件）后，CPA 日志里应能看到 `live-routes` 插件已加载。可以用以下方式打开看板：

- 在本机浏览器访问 `http://127.0.0.1:<port>/v0/resource/plugins/live-routes/live`（CPA 默认端口为 8317）。
- 在 CPA 管理面板或 EasyCLIProxyAPI 的插件页，点击 **Live Routes** 菜单项。
- 在地址后加 `?demo=1`，用模拟流量做离线演示，不读取 CPA 数据。

随便经 CPA 发一个请求，几秒内就会出现在看板上。有请求进行中时页面每 1 秒轮询一次，空闲时逐步放慢到 5 秒。

### 更新与卸载

更新：替换动态库文件后重启 CPA。卸载：把 `plugins.configs.live-routes.enabled` 设为 `false`，或删除动态库后重启。插件不保存任何数据，没有其他需要清理的内容。

## 使用说明

- **流向视图**：显示当前正在发生的请求，路径为客户端 → CPA 规则（alias → model）→ 出口凭证。连线粗细跟随近 60 秒的请求速率。请求进行中时，光点停在 CPA 或出口处等待，完成后飞回。节点在 `active_window_minutes` 内没有流量就会淡出。点击客户端可展开它的会话；鼠标悬停在会话上，可查看完整 ID 和父会话。
- **时间轴视图**：用来回看。每个出口凭证一条泳道，缩放档位为 30 秒、2 分钟、10 分钟，可用滚轮或拖动往回看，最早到内存中仍保留的数据。429 会单独标出，悬停可看到 `Retry-After`。
- **顶部统计**：按所选时间范围统计请求数、进行中、成功率（成功 / (成功 + 失败)）、tokens/s（总 token / 范围秒数）和平均延迟。
- **控件**：暂停、临时动画速度（0.5x / 1x / 2x）、布局（标签页，或流向在上、泳道在下的分栏）。这些偏好只保存在当前浏览器。页面跟随系统的浅色/深色主题，并遵循 `prefers-reduced-motion`。

## 常见问题

| 现象 | 原因 / 处理 |
| --- | --- |
| `403 forbidden_host` | 没有通过回环地址打开页面。请用 `127.0.0.1` 或 `localhost` 访问，或设置 `require_key: true`。 |
| 页面要求输入管理密钥 | 已开启 `require_key: true`。输入 CPA 管理密钥后，会换成只保存在页面内存里的短期令牌。 |
| 没有"排队/路由中"状态，客户端显示未知 | `inflight_tracking` 为 `false`，只展示已完成请求的用量记录。 |
| 看不到客户端 IP | CPA 不把客户端地址传给插件，只有在反向代理设置了 `X-Forwarded-For` / `X-Real-IP` 时才显示。 |
| 数据消失了 | CPA 或插件重启过，或数据超出了 `retention_minutes` / `max_records`。数据只存在内存里，这是设计行为。 |
| macOS 拒绝加载动态库 | 去掉隔离属性（见上方的 `xattr` 命令）。 |

## 配置

所有选项位于 `plugins.configs.live-routes` 下。`enabled` 与 `priority` 由 CPA 自身处理。建议设置 `priority: -100`，让本观察器在其他拦截器之后运行。

| 配置项 | 默认值 | 取值范围 | 说明 |
| --- | --- | --- | --- |
| `retention_minutes` | `30` | 1 - 120 | 已完成请求在内存中保留的时长。 |
| `max_records` | `5000` | 1000 - 100000 | 记录数上限，超出时优先淘汰最旧的记录。 |
| `active_window_minutes` | `10` | 1 - 60 | 流向视图：窗口内没有请求的节点会淡出。 |
| `animation_speed` | `"1x"` | `0.5x`、`1x`、`2x` | 页面默认动画速度（查看者可自行调整）。 |
| `reduced_animation` | `false` | 布尔值 | 关闭飞行圆点，仅保留链路粗细与计数。 |
| `inflight_tracking` | `true` | 布尔值 | 实时跟踪排队中与已路由的请求。为 `false` 时仅在请求完成后回放用量记录。 |
| `layout` | `"tabs"` | `tabs`、`split` | 页面默认布局：可切换的多视图，或上方流向条加下方出口泳道。 |
| `require_key` | `false` | 布尔值 | `false`：数据接口只响应 `Origin`/`Referer` 指向回环地址的请求，仅在 CPA 只监听 `127.0.0.1` 时安全。`true`：页面要求输入管理密钥，并使用短期的内存令牌。 |
| `api_key_labels` | `{}` | 映射 | 客户端 API Key 的友好名称：键为密钥 sha256 十六进制前缀（至少 8 位），值为标签。 |

无需在文件中暴露密钥即可计算前缀：

```sh
printf %s "$KEY" | shasum -a 256 | cut -c1-8
```

```yaml
plugins:
  configs:
    live-routes:
      enabled: true
      priority: -100
      api_key_labels:
        "3f9a12bc": "alice 的笔记本"
        "a1b2c3d4": "CI 机器"
```

## 访问模型

CPA 不会对插件资源页做鉴权，因此插件自己给数据接口加了门禁。

- 默认（`require_key: false`）：`GET /v0/resource/plugins/live-routes/api/{snapshot,events,records}` 只在请求的 `Origin`（没有时看 `Referer`）指向 `127.0.0.1`、`localhost` 或 `::1` 时响应，其他情况返回 `403 forbidden_host`。页面本身是静态的，不携带数据。
- CPA 不会把客户端地址和 `Host` 头传给插件，所以这道检查依赖的请求头可以被任何非浏览器客户端伪造，只能挡住其他机器上的浏览器。只要 CPA 监听的不是 `127.0.0.1`（CPA 默认 `host: ""` 会监听所有网卡），就必须设置 `require_key: true`。
- `require_key: true`：页面首次访问时要求输入管理密钥，并通过 `POST /v0/management/live-routes/session` 换取仅存于内存的令牌，之后以 `X-Live-Routes-Token` 头发送。页面与插件都不会保存密钥。没有有效令牌时，数据接口返回 `401 unauthorized`。通过域名或反向代理访问 CPA 时请使用此模式。
- 脚本可用管理密钥读取同样的快照：`GET /v0/management/live-routes/snapshot`，管理密钥放在 `Authorization` 的 bearer 头中。

客户端 API Key 在插件内部脱敏：只暴露形如 `sk-...3f9a` 的掩码和简短指纹，不会暴露原始密钥。凭证标签中的邮箱地址也会被掩码处理。

## 性能

- `inflight_tracking: true` 时，插件会声明一个请求拦截器。CPA 对每个请求同步调用它两次（认证前，以及认证后的每次尝试），宿主每次调用都会序列化完整请求体，开销约每 MB 请求体 1 - 2 毫秒。插件自身只返回恒定的放行结果，耗时为微秒级。
- 设置 `inflight_tracking: false` 可使请求路径零额外开销，此时只有用量回放：没有排队 / 已路由状态，也无法依据 User-Agent 识别客户端。
- 用量处理与页面轮询的开销可以忽略。没有人查看页面时，插件不做任何后台工作。
- 建议使用 `priority: -100`，让观察器在其他拦截器之后运行。

## 存储保证

仅使用内存。插件不会向磁盘写入任何内容，CPA 或插件重启后数据即清空。页面只在浏览器 `localStorage` 中保存界面偏好（`lr-view`、`lr-layout`、`lr-speed`、`lr-range`）。

`scripts/e2e/run.sh` 会在真实 CPA 进程上对比启用与停用插件时打开的常规文件，以验证这一点。

## 限制

- 只有当 CPA 位于设置了 `X-Forwarded-For` 或 `X-Real-IP` 的反向代理之后，才能显示客户端 IP。
- 客户端 User-Agent（进而客户端类型）仅在 `inflight_tracking: true` 时可知。
- CPA 的 Home 模式会禁用插件资源页，因此该模式下无法使用看板。
- 数据受 `retention_minutes` 与 `max_records` 限制，更早的历史会按设计被丢弃。

## 从源码构建

需要 Go（版本见 `go.mod`）和 C 编译器，因为插件以 `-buildmode=c-shared` 构建。

```sh
make test                      # go vet + go test -race
make build                     # 当前平台 -> bin/live-routes.<ext>
make darwin                    # macOS arm64 + amd64（macOS 上需要 clang）
make dist VERSION=0.1.0        # dist/live-routes_0.1.0_<goos>_<goarch>.zip + checksums.txt
make install-local PLUGINS_DIR=/path/to/plugins
make e2e                       # 对本地 CPA 与模拟上游做端到端测试
```

Linux 与 Windows 版本需要原生工具链；发布构建在原生 GitHub Actions 运行器上完成（`.github/workflows/release.yml`），覆盖 darwin arm64/amd64、linux amd64/arm64 和 windows amd64。推送标签 `vX.Y.Z` 会构建全部目标，并发布包含各平台压缩包与 `checksums.txt` 的 Release，这正是 CPA 插件商店安装所需的布局。`store/registry-entry.json` 是用于提交到插件注册表的条目。

e2e 脚本需要 `node`、`python3`、`curl` 和 `lsof`，并要求 CPA 源码与本仓库并列（可用 `CPA_SRC` 覆盖）。脚本只使用模拟上游，不会触碰 `~/.cli-proxy-api`。

## 许可证

MIT，见 [LICENSE](LICENSE)。
