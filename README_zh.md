# gohttpserver

最好用的 HTTP 文件服务器：人性化 UI、文件上传、为 iOS/Android 安装包生成二维码。

## 功能特性

- 二维码生成 · 面包屑导航 · 不同类型文件图标 · 显示/隐藏隐藏文件
- 文件上传（Token/会话认证）· 文件夹上传 · zip 上传自动解压 · 目录压缩下载
- 在线编辑 · 代码预览 · 全局搜索 · 快速复制下载链接 · MD5/SHA 计算
- README.md 预览 · 目录变更局部刷新 · 单目录路径合并 · 主题切换 · 小屏适配
- HTTP Basic / OpenID / OAuth2-proxy 认证 · 内置登录（`--login`）· 登录失败限速
- Apple IPA 自动生成 plist · Plist 代理 · CORS · Nginx 部署 · `--prefix` 前缀
- `.ghs.yml` 访问控制（类似 `.htaccess`）· `--conf` YAML 配置
- 管理面板（个人中心 / 参数设置）· frp 内网穿透
- 应用内自我更新（update_core）：检查/断点续传/校验/替换/优雅重启
- 内置 WebDAV（`/dav/`）：按账号限根目录、只读、配额、系统文件保护、账号管理

## 安装

### 一键安装

```bash
# Linux
curl -fsSL https://gitee.com/toluckykoi/gohttpserver/raw/main/install-gohttpserver.sh -o install-gohttpserver.sh && sudo bash install-gohttpserver.sh

# Windows（PowerShell / CMD）
iwr -useb https://gitee.com/toluckykoi/gohttpserver/raw/main/install-gohttpserver.ps1 -o install-gohttpserver.ps1; .\install-gohttpserver.ps1
curl -fsSL https://gitee.com/toluckykoi/gohttpserver/raw/main/install-gohttpserver.bat -o install-gohttpserver.bat && install-gohttpserver.bat
```

脚本支持 TUI 交互、卸载询问、参数配置；Linux 安装后 `systemctl start gohttpserver` 启动。

### 手动下载

从 [GitHub Releases](https://github.com/toluckykoi/gohttpserver/releases) 或 [Gitee Releases](https://gitee.com/toluckykoi/gohttpserver/releases) 下载对应平台二进制。

## 快速开始

```bash
gohttpserver -r ./ --port 8000 --upload           # 上传
gohttpserver -r ./ --port 8000 --edit             # 编辑
gohttpserver -r ./ --port 8000 --upload --delete --edit   # 全部开启
gohttpserver -r ./ --port 8000 --login            # 登录（推荐，管理面板/WebDAV/自我更新依赖它）
```

`gohttpserver --help` 查看全部选项。

### 命令行参数

命令行参数优先于 `--conf` YAML 配置；「可重复」参数可传多次。

| 参数 | 默认值 | 说明 |
|------|--------|------|
| `--conf` | | 配置文件路径，YAML 格式 |
| `-r`, `--root` | `./` | 服务根目录 |
| `--prefix` | | URL 前缀，如 `/foo` |
| `--port` | `8000` | 监听端口 |
| `-a`, `--addr` | | 监听地址，如 `127.0.0.1:8000`（覆盖 `--port`） |
| `--cert` / `--key` | | TLS 证书 / 密钥路径 |
| `--auth-type` | | `http`、`openid` 或 `oauth2-proxy` |
| `--auth-http` | | HTTP Basic 认证 `用户名:密码`（可重复） |
| `--auth-openid` | `https://login.netease.com/openid` | OpenID 认证 URL |
| `--theme` | `black` | `black` / `green` |
| `--upload` / `--delete` / `--edit` | 关闭 | 启用上传 / 删除 / 编辑 |
| `--xheaders` | 关闭 | 信任 `X-Forwarded-*`（Nginx 后使用） |
| `--debug` | 关闭 | 调试模式 |
| `-p`, `--plistproxy` | `...herokuapp.com/plist` | 非 HTTPS 时的 plist 代理 |
| `--title` | `Go HTTP File Server` | 服务器标题 |
| `--google-tracker-id` | | Google Analytics ID |
| `--deep-path-max-depth` | `5` | 合并单级子目录最大深度，`-1` 不合并 |
| `--no-index` | 关闭 | 禁用目录索引 |
| `--cors-origins` | | 允许的 CORS 来源（可重复；`*` 允许所有，不安全） |
| `--allow-public-oauth2` | 关闭 | 非回环地址启用 oauth2-proxy（不安全） |
| `--login` | 关闭 | 用户名密码登录，自动开启上传/删除/编辑，默认 `admin`/`admin` |
| `--db` | `./gohttpserver.db` | SQLite 路径（需 `--login`） |
| `--session-ttl` | `12h` | 会话有效期；`0` = 浏览器会话 cookie |
| `--log-level` | `info` | `debug`/`info`/`warn`/`error` |
| `--log-format` | `json` | `json`/`text` |

## 监控

- `GET /healthz`（或 `/-/healthz`）：JSON 存活探针 `{"status":"ok","version":"v1.2.3"}`
- `GET /metrics`（或 `/-/metrics`）：Prometheus 文本格式

两个接口不受 `--prefix` 影响，始终在 `/healthz` 和 `/metrics`。

## 登录与认证

### 内置登录（`--login`）

- 默认账号 `admin`/`admin`，首次登录后请在【管理面板 → 个人中心】修改
- 会话经 Cookie 签名；设置 `GHS_SESSION_KEY` 环境变量可固定签名密钥，否则重启后所有会话失效
- 登录失败每 IP 15 分钟最多 5 次

### 外部认证（`--auth-type`）

```bash
# HTTP Basic
gohttpserver --auth-type http --auth-http username1:password1 --auth-http username2:password2
# OpenID
gohttpserver --auth-type openid --auth-openid https://login.example-hostname.com/openid/
# OAuth2 代理（需自配反向代理，后端读取如下请求头）
gohttpserver --auth-type oauth2-proxy
```

| 请求头 | 值 |
|--------|-----|
| `X-Auth-Request-Email` | 用户 ID |
| `X-Auth-Request-Fullname` | 用户显示名称（URL 编码） |
| `X-Auth-Request-User` | 用户昵称（通常为邮箱前缀） |

## 管理面板

`--login` 后点击右上角头像 →「管理面板」：

- **个人中心**：用户名、认证方式、版本号；修改用户名（需密码验证）/ 密码
- **参数设置**：WebDAV 总开关、URL 展示、账号管理
- **内网穿透**：frp 客户端设置

## 自我更新（update_core）

`--login` 后点击页脚版本号打开「软件更新」对话框；有新版本时版本号旁显示彩色徽标。

- 更新服务器信息编译进二进制（[main.go](main.go) 的 `updateServerConfig`）；`BaseURL` 置空可禁用
- 上报的 `package_type` 必须与后台登记**完全一致**（Windows=`exe`，Linux/macOS=`bin`）
- 私有软件设置 `GHS_UPDATE_TOKEN`；令牌不进日志/URL，下载不跟随重定向
- 更新 API 受登录保护；安装前重新校验 SHA-256，旧二进制保留 `.bak`，失败自动回滚
- 支持 HTTP/HTTPS 源；建议 HTTPS（HTTP 明文有中间人风险，启动会警告）
- 下载支持 Range 断点续传（If-Range 绑定 SHA-256）

更新 API（需登录）：`GET /-/api/update/status`、`POST /-/api/update/{check,download,apply,restart}`

## WebDAV（`--login` 后启用）

挂载于 `/dav/`，使用独立 Basic Auth 账号体系（客户端不共享浏览器 Cookie）。管理面板 → 参数设置 → 总开关启用后创建账号：

- **备注名**（必填）；**相对根目录**（默认 `/`，防路径穿越）；**高级选项**：只读模式、系统文件保护（默认开）、配额（GB）
- 用户名绑定当前登录用户；密码由系统生成 10 位随机串
- 账号列表：展开查看链接/用户名/密码（👁 显示），⋮ 菜单可编辑、重置密码、删除

### Windows 映射网络驱动器

1. OPTIONS 预检已内置处理，无需配置
2. HTTP 明文默认不发 Basic Auth。若提示 "401 Unauthorized"，将注册表 `HKLM\SYSTEM\CurrentControlSet\Services\WebClient\Parameters\BasicAuthLevel` 从 1 改为 2 后重启 WebClient：

```powershell
Set-ItemProperty -Path "HKLM:\SYSTEM\CurrentControlSet\Services\WebClient\Parameters" -Name BasicAuthLevel -Value 2
Restart-Service WebClient
```

然后映射 `http://IP:8000/dav/`（或 `\\IP@8000\DavWWWRoot\dav\`）。生产环境建议用 HTTPS 免改注册表。

### Linux 连接

文件管理器连接 `dav://主机:端口/dav/`（HTTPS 用 `davs://`）。无尾斜杠 `/dav` 路径已兼容。

## 高级用法

### `.ghs.yml` 访问控制

在子目录放置 `.ghs.yml` 控制该目录权限：

```yaml
---
upload: false    # 目录级默认权限
delete: false
edit: false
users:                          # 用户级覆盖
- email: "codeskyblue@codeskyblue.com"
  delete: true
  upload: true
  edit: true
  token: 4567gf8asydhf293r23r
accessTables:                   # 文件级显隐
- regex: block.file
  allow: false
- regex: visual.file
  allow: true
```

### IPA Plist 代理

```bash
gohttpserver --plistproxy=https://someproxyhost.com/
```

Nginx 后已配置 HTTPS 时自动禁用。

### curl 上传 / 编辑

```bash
# 上传（token 鉴权时加 -F token=xxx）
curl -F file=@foo.txt localhost:8000/somedir
# 重命名
curl -F file=@foo.txt -F filename=hi.txt localhost:8000/somedir
# 上传并解压 zip（完成后删除原 zip）
curl -F file=@pkg.zip -F unzip=true localhost:8000/somedir
# 文件夹（保留结构，path 为相对路径）
curl -F file=@sub/b.txt -F path=MyFolder/sub/b.txt localhost:8000/somedir
# 编辑（PUT，需 --edit）
curl -X PUT -H "X-Token: 12312jlkjafs" -d "新内容" localhost:8000/somedir/foo.txt
```

注意：文件名不允许 `\/:*<>|`；`path` 不允许 `..` 或绝对路径。

### Nginx 部署

gohttpserver 用 `--xheaders` 启动；`proxy_set_header` 见下（重点 `client_max_body_size 0;` 禁上传限制）：

```nginx
server {
  listen 80;
  server_name your-domain-name.com;
  location / {
    proxy_pass http://127.0.0.1:8200; # 修改
    proxy_redirect off;
    proxy_set_header  Host    $host;
    proxy_set_header  X-Real-IP $remote_addr;
    proxy_set_header  X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_set_header  X-Forwarded-Proto $scheme;
    client_max_body_size 0;
  }
}
```

根路径被占用时用 `--prefix /foo --addr :8200 --xheaders`，Nginx `location /foo` 同理（参考 [issue#105](https://github.com/codeskyblue/gohttpserver/issues/105)）。

## 开发者指南

> **注意**：`frontend/dist` 未提交（被 `.gitignore` 忽略），但 `assets.go` 通过 `//go:embed` 依赖它。
> 全新克隆后直接 `go build` 会报 `pattern frontend/dist: no matching files found`，请先运行 `./build.sh`（或 `frontend/` 下 `npm ci && npm run build`）。

```bash
./build.sh   # 构建，产物：gohttpserver-<版本>-<系统>-<架构>[.exe]
./dist/gohttpserver-1.4.0-windows-x64.exe -r ./testdata --addr 0.0.0.0:8000 --login
```

## 支持与许可

本项目由 [codeskyblue/gohttpserver](https://github.com/codeskyblue/gohttpserver) 修改而来（原项目已停止更新），感谢开源支持。

使用 [Apache-2.0](LICENSE) 许可证。