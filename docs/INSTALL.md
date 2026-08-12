# 安装与登录

`larkctl` 是访问飞书/Lark 的命令行客户端。它有两种工作模式，装完之后按其中一种登录即可。

| 模式 | 认证方式 | 适用场景 |
|------|----------|----------|
| Gateway（推荐） | 通过 `larkctl-gateway` 走设备码流程 | 团队共用、无头机器、MCP 客户端接入 |
| Local | 直接对飞书做 OAuth 授权码流程 | 单人使用，手上有飞书应用的 app_id/app_secret |

Gateway 是**另一个仓库的独立服务**（`larkctl-gateway`），不是 `larkctl` 的子命令。凭据只配在网关侧，开发者机器上不保存飞书 `user_access_token`。

## 1. 安装

网关自带一键安装脚本（把下面的 `larkmcp.example.com` 换成你的网关域名），
它会下载对应平台的二进制、写好网关地址、然后直接进入登录：

```bash
# macOS / Linux / Git Bash
curl -fsSL https://larkmcp.example.com/install | bash
```

```powershell
# Windows PowerShell
irm https://larkmcp.example.com/install.ps1 | iex
```

也可以手动下载单个二进制（`larkctl-darwin-arm64` / `larkctl-linux-amd64` / `larkctl-windows-amd64.exe`），
放到 PATH 上任意目录，重命名为 `larkctl`（Windows 为 `larkctl.exe`）。下载入口见网关首页。

从源码构建：

```bash
cd repos/larkctl && go build -o larkctl .   # 当前平台
make build                                   # 交叉编译三平台到 dist/
```

## 2. 登录（Gateway 模式）

**首次登录必须显式指定网关地址**——二进制内置的默认值是 `http://127.0.0.1:8787`，不是生产地址。
一键脚本已经把地址写进了 `~/.lark/config.json`；手动安装的话自己传一次，登录成功后会被保存下来：

```bash
larkctl --gateway-url https://larkmcp.example.com login
larkctl whoami
```

地址的解析顺序是：`--gateway-url` 参数 → `FEISHU_GATEWAY_URL` 环境变量 →
`~/.lark/config.json` 里的 `gateway_url` → 内置的 `http://127.0.0.1:8787`。

登录时终端会打印授权链接和 user code，并自动拉起浏览器；在浏览器里用飞书账号授权后，
CLI 轮询拿到 session token 存入 `~/.lark/config.json`。无头机器加 `--open-browser=false`，
把链接复制到有浏览器的机器上打开即可。

按需申请权限：`larkctl login docs wiki` 只要文档和知识库的 scope，`larkctl login all` 要全部。
可用的 scope 分组见 `larkctl login --help`。日历、会议室这类需要额外权限的命令，首次使用时会自动触发浏览器补授权。

## 3. 登录（Local 模式）

需要自己有飞书自建应用的凭据，且该应用的重定向地址里要包含 `http://127.0.0.1:19876/callback`：

```bash
larkctl init --app-id <app_id> --app-secret <app_secret>   # 也可改用 FEISHU_APP_ID / FEISHU_APP_SECRET 环境变量
larkctl login
```

凭据写入 `~/.lark/config.json`。

只要 `~/.lark/config.json` 里同时存在 `app_id` 和 `app_secret`，larkctl 就会进入 Local 模式并
**完全绕过网关**。如果你配了 `gateway_url` 却发现请求还是直连飞书，检查配置里是不是残留了这两个字段。
access token 会在过期前 5 分钟自动刷新。

## 4. 本地数据

所有状态都在纯文件里，不使用系统钥匙串：

- `~/.lark/config.json` — 网关地址、session token、Local 模式的 app 凭据与 token

## 5. 验证

```bash
larkctl whoami
larkctl docs info "https://<tenant>.feishu.cn/wiki/<token>" --type wiki
larkctl mcp        # 打印 MCP 接入地址，配到 Claude Desktop 等客户端
```

## 6. 发布

`make update` 会交叉编译并把三个平台的二进制推到 Makefile 里配置的 MinIO 位置
（`MINIO_ALIAS`/`MINIO_BUCKET` 变量），一键安装脚本就是从那里取文件的。
仓库里的 `scripts/release.sh` 另外提供打 tar/zip 包的方式，推 tag 触发
`.github/workflows/release.yml` 时也用它出包。
