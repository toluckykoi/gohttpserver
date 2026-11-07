# gohttpserver

- **目标**: 打造最好用的 HTTP 文件服务器
- **特性**: 人性化 UI 界面、支持文件上传、直接为 Apple 和 Android 安装包生成二维码

## 功能特性

- [x] 支持二维码生成
- [x] 面包屑路径快速导航
- [x] 所有资源打包到独立二进制文件中
- [x] 不同文件类型显示不同图标
- [x] 支持显示/隐藏隐藏文件
- [x] 支持上传（通过 Token 或会话认证）
- [x] README.md 预览
- [x] HTTP 基础认证
- [x] 目录变更时局部刷新页面
- [x] 当目录下只有一个目录时，路径会合并显示
- [x] 目录压缩下载
- [x] Apple IPA 自动生成 plist 文件，二维码可被 iPhone 识别（需要 HTTPS）
- [x] Plist 代理
- [x] 支持 CORS
- [x] 离线下载
- [x] 代码文件预览
- [x] 编辑文件支持
- [x] 全局文件搜索
- [x] 在小屏幕上隐藏下载和二维码按钮
- [x] 主题切换支持
- [x] 可在 Nginx 后面正常工作
- [x] 支持 `.ghs.yml` 配置（类似 `.htaccess`）
- [x] 计算 MD5 和 SHA
- [x] 文件夹上传
- [x] 支持按大小或修改时间排序
- [x] 在首页添加版本信息
- [x] 添加 API `/-/info/some.(apk|ipa)` 获取详细信息
- [x] 添加 API `/-/apk/info/some.apk` 获取 Android 包信息
- [x] 自动标记版本
- [x] 支持通过配置文件设置
- [x] 快速复制下载链接
- [x] 显示文件夹大小
- [x] 创建文件夹
- [x] 按住 Alt 键跳过删除确认
- [x] 上传时支持解压 zip 文件（解压文件显示进度）
- [x] 内置登录拦截模式（`--login`，独立于 `--auth-type`）
- [x] 管理面板（个人中心 + 参数设置），支持修改用户名/密码
- [x] 内置 WebDAV 服务器（`/dav/`），支持按账号限制根目录、只读模式、系统文件保护、配额限制、RFC 4331 磁盘容量显示
- [x] WebDAV 账号管理：点击查看基本信息（链接/用户名/密码）、在线编辑、重置密码、删除，兼容 Windows / macOS / Linux 客户端
- [x] 登录失败速率限制（每 IP 15 分钟内最多 5 次失败）

## 安装

### 一键安装（推荐 Linux）

运行官方安装脚本，自动下载二进制、配置 systemd 服务、引导设置端口和参数：

```bash
# Linux 安装命令：
curl -fsSL https://gitee.com/toluckykoi/gohttpserver/raw/main/install-gohttpserver.sh -o install-gohttpserver.sh && sudo bash install-gohttpserver.sh

# Windows 安装命令（PowerShell）
iwr -useb https://gitee.com/toluckykoi/gohttpserver/raw/main/install-gohttpserver.ps1 -o install-gohttpserver.ps1; .\install-gohttpserver.ps1

# Windows 安装命令（CMD）
curl -fsSL https://gitee.com/toluckykoi/gohttpserver/raw/main/install-gohttpserver.bat -o install-gohttpserver.bat && install-gohttpserver.bat
```

脚本支持 TUI 交互（whiptail）、卸载询问、参数配置等，安装完成后即可通过 `systemctl start gohttpserver` 启动。

### 手动下载二进制

从 [GitHub Releases](https://github.com/toluckykoi/gohttpserver/releases) 或者 [Gitee Releases](https://gitee.com/toluckykoi/gohttpserver/releases) 下载二进制文件进行安装。

## 使用方法

监听所有接口的 8000 端口，并启用文件上传功能：

```bash
gohttpserver -r ./ --port 8000 --upload
```

启用文件编辑功能：
```bash
gohttpserver -r ./ --port 8000 --edit
```

同时启用上传、删除和编辑：
```bash
gohttpserver -r ./ --port 8000 --upload --delete --edit
```

启用登录功能：

```bash
gohttpserver -r ./ --port 8000 --login
```

使用 `gohttpserver --help` 查看更多使用选项，使用一键安装脚本可以不使用命令行启动。

### 命令行参数

参数按照 `main.go` 中的声明顺序排列；标注「可重复」的参数可以传入多次。
`--conf`（YAML 格式）可以配置同样的选项，命令行参数的优先级高于配置文件。

| 参数 | 默认值 | 说明 |
|------|--------|------|
| `--conf` | | 配置文件路径，YAML 格式 |
| `-r`, `--root` | `./` | 对外提供服务的根目录 |
| `--prefix` | | URL 前缀，例如 `/foo` |
| `--port` | `8000` | 监听端口 |
| `-a`, `--addr` | | 监听地址，例如 `127.0.0.1:8000`（设置后忽略 `--port`） |
| `--cert` | | TLS `cert.pem` 路径 |
| `--key` | | TLS `key.pem` 路径 |
| `--auth-type` | | 认证方式：`http`、`openid` 或 `oauth2-proxy` |
| `--auth-http` | | HTTP 基础认证，格式 `用户名:密码`（可重复） |
| `--auth-openid` | `https://login.netease.com/openid` | OpenID 认证身份 URL |
| `--theme` | `black` | 网页主题，可选 `black`、`green` |
| `--upload` | 关闭 | 启用上传支持 |
| `--delete` | 关闭 | 启用删除支持 |
| `--edit` | 关闭 | 启用文件编辑支持 |
| `--xheaders` | 关闭 | 信任 `X-Forwarded-*` 请求头，在 Nginx 后面运行时使用 |
| `--debug` | 关闭 | 启用调试模式 |
| `-p`, `--plistproxy` | `https://plistproxy.herokuapp.com/plist` | 服务器不是 HTTPS 时使用的 plist 代理 |
| `--title` | `Go HTTP File Server` | 服务器标题 |
| `--google-tracker-id` | | Google Analytics 跟踪 ID，留空表示禁用 |
| `--deep-path-max-depth` | `5` | 自动合并单级子目录的最大深度；`-1` 表示不合并目录 |
| `--no-index` | 关闭 | 禁用目录索引 |
| `--cors-origins` | | 允许的 CORS 来源（可重复；留空表示不启用 CORS，`*` 表示允许所有，不安全） |
| `--allow-public-oauth2` | 关闭 | 允许在非回环地址上使用 oauth2-proxy 模式（不安全；仅限搭配会剥离 `X-Auth-Request-*` 的代理使用） |
| `--login` | 关闭 | 启用用户名/密码登录拦截（独立于 `--auth-type`），同时自动开启上传/删除/编辑。默认账号密码：`admin`/`admin` |
| `--db` | `./gohttpserver.db` | SQLite 数据库文件路径，仅在启用 `--login` 时生效 |
| `--session-ttl` | `12h` | 登录会话的有效期；`0` 表示使用浏览器会话 cookie |

`-h` / `--help` 可以打印上面的参数列表，`--version` 可以打印构建信息。

## 监控

以下两个运维接口始终注册在根路径下（注册在 `--prefix` 路由之前），
这样健康检查和抓取端始终有固定的访问地址：

- `GET /healthz`（同时支持 `/-/healthz`）—— JSON 存活探针：

  ```bash
  curl -s http://localhost:8000/healthz
  {"status":"ok","version":"v1.2.3"}
  ```

- `GET /metrics`（同时支持 `/-/metrics`）—— Prometheus 文本格式
  （`text/plain; version=0.0.4`），同样允许 `HEAD` 请求：

  ```bash
  curl -s http://localhost:8000/metrics
  # HELP gohttpserver_requests_total Total number of HTTP requests served.
  # TYPE gohttpserver_requests_total counter
  gohttpserver_requests_total 42
  # ... response_bytes_total、response_status_total{class="2xx|3xx|4xx|5xx"}、
  #     requests_in_flight、uptime_seconds ...
  gohttpserver_build_info{version="v1.2.3"} 1
  ```

`--prefix` 不影响这两个路径，它们始终位于 `/healthz` 和 `/metrics`。

## Docker 使用方法（开发中）

## 认证选项

- 启用 HTTP 基础认证：

  ```bash
  gohttpserver --auth-type http --auth-http username1:password1 --auth-http username2:password2
  ```

- 使用 OpenID 认证：

  ```bash
  gohttpserver --auth-type openid --auth-openid https://login.example-hostname.com/openid/
  ```

- 使用 OAuth2 代理：

  ```bash
  gohttpserver --auth-type oauth2-proxy
  ```

  你可以配置一个 HTTP 反向代理来处理认证。使用 oauth2-proxy 时，后端会使用请求头 `X-Auth-Request-Email` 中的信息作为用户 ID，`X-Auth-Request-Fullname` 作为用户显示名称。请自行配置 OAuth2 反向代理。更多信息请参考 [oauth2-proxy](https://github.com/oauth2-proxy/oauth2-proxy)。

  所需的请求头列表如下：

  | 请求头 | 值 |
  |--------|-----|
  | X-Auth-Request-Email | 用户 ID |
  | X-Auth-Request-Fullname | 用户显示名称（URL 编码） |
  | X-Auth-Request-User | 用户昵称（通常是邮箱前缀） |

## 管理面板

启用 `--login` 后，点击右上角用户头像 → 「管理面板」即可打开全屏管理面板。
面板包含三个 tab：

- **个人中心**：展示当前登录用户的用户名、认证方式、版本号；支持修改用户名
  （需要输入当前密码二次验证）和修改密码
- **参数设置**：WebDAV 服务的总开关、URL 展示、WebDAV 账号管理
- **内网穿透**：支持设置 frp 内网穿透客户端设置

## WebDAV 服务器

内置 WebDAV 服务挂载在 `/dav/` 路径下，仅在启用 `--login` 时可用。WebDAV
使用独立的 HTTP Basic Auth 账号体系（WebDAV 客户端如 Cyberduck、rclone、
Windows 资源管理器不共享浏览器 cookie，必须用 Basic Auth）。

通过管理面板（参数设置 → 总开关）启用 WebDAV 服务后，即可创建账号。
新建账号表单只需三项：

- **备注名**（必填）：用于标识凭据，方便日后吊销
- **相对根目录**（默认 `/`）：可点击「浏览」从文件树中选择要共享的目录，
  账号被限制在 `<--root>/<root_path>` 下，路径穿越（`../`）会被拒绝
- **高级选项**（可折叠）：
  - **只读模式**：开启后用户只能通过此账号读取文件，所有写操作
    （PUT/DELETE/MKCOL/MOVE/COPY/PROPPATCH）返回 403
  - **阻止删除/上传系统文件**（默认开启）：拒绝写入 `gohttpserver.db`、
    `.ghs.yml`、`favicon.ico`、`favicon.png` 以及任何
    以 `.` 开头的隐藏文件
  - **配额（Quota）**：限制该账号可写入的总字节数，默认单位 GB，留空或 0
    表示不限

**用户名**自动绑定当前登录用户名（不可修改）；**密码**由系统用 `crypto/rand`
自动生成 10 位随机字符串，创建后直接完成，无需手动复制。

### 账号管理

在账号列表中：

- **点击任意条目**即可展开「基本信息」，显示 **链接地址、用户名、密码**
  （密码默认打码，点 👁 图标显示/隐藏，旁边可一键复制）
- 每行右侧的 **三点菜单**（⋮）提供三个操作：
  - **编辑**：修改备注、相对根目录、只读、系统文件保护、配额
  - **重置密码**：生成新的 10 位随机密码，旧密码立即失效
  - **删除**：吊销账号（幂等）

### 从 Windows 资源管理器映射网络驱动器

Windows 内置的 WebDAV mini-redirector 有两个坑需要处理：

1. **它会先发匿名 OPTIONS 预检**，然后再弹密码框。gohttpserver 已
   实现该预检响应（返回 `DAV: 1, 2` + `MS-Author-Via: DAV`），
   Windows 能正确识别为 WebDAV 共享，无需你做任何事。
2. **默认情况下，Windows 不在 HTTP 明文连接上发送 Basic Auth**
   （只有 HTTPS 才允许）。如果你用 `http://` 映射驱动器立即报
   "401 Unauthorized"，需要把注册表中的 `BasicAuthLevel` 从 1
   改为 2：

   ```text
   HKEY_LOCAL_MACHINE\SYSTEM\CurrentControlSet\Services\WebClient\Parameters\BasicAuthLevel
   ```

   把值从 `1`（仅 HTTPS）改为 `2`（HTTP 和 HTTPS 都允许），然后
   重启 WebClient 服务：

   ```powershell
   Set-ItemProperty -Path "HKLM:\SYSTEM\CurrentControlSet\Services\WebClient\Parameters" -Name BasicAuthLevel -Value 2
   Restart-Service WebClient
   ```

   然后映射 `\\192.168.120.141@8000\DavWWWRoot\dav\`（或
   `http://192.168.120.141:8000/dav/`）为网络驱动器，弹出密码框时
   输入 WebDAV 账号凭据即可。生产环境建议使用 HTTPS —— 此时无需
   改注册表。

### 从 Linux 文件管理器连接

GNOME「文件」（Nautilus/gvfs）等 Linux 文件管理器可通过
`dav://主机:端口/dav/`（或 HTTPS 的 `davs://`）连接，弹出对话框时输入
WebDAV 账号的用户名和密码。

> 提示：部分 Linux 客户端在发现阶段会探测不带结尾斜杠的 `/dav` 路径。
> gohttpserver 已对该路径做了兼容处理并加入登录白名单，因此无论地址是否带
> 结尾斜杠都能正常连接，不会再出现「HTTP 错误：Unauthorized」。

## 高级用法

通过在子目录下创建 `.ghs.yml` 文件来添加访问规则。示例：

```yaml
---
upload: false
delete: false
edit: false
users:
- email: "codeskyblue@codeskyblue.com"
  delete: true
  upload: true
  edit: true
  token: 4567gf8asydhf293r23r
```

在这种情况下，如果启用了 OpenID 认证且用户 "codeskyblue@codeskyblue.com" 已登录，他/她可以在存在 `.ghs.yml` 文件的目录下删除/上传/编辑文件。

`token` 用于上传和编辑。请参考 [使用 curl 上传](#使用-curl-上传)。

例如，在以下目录结构中，用户可以在 `foo` 目录下删除/上传文件，但不能在 `bar` 目录下执行这些操作：

```
root -
  |-- foo
  |    |-- .ghs.yml
  |    `-- world.txt 
  `-- bar
       `-- hello.txt
```

用户可以使用 `--conf` 指定配置文件名，请参考 [示例配置文件](testdata/config.yml)。

要指定哪些文件隐藏、哪些文件可见，请在 `.ghs.yml` 中添加以下内容：

```yaml
accessTables:
- regex: block.file
  allow: false
- regex: visual.file
  allow: true
```

### IPA Plist 代理

这用于启用 HTTPS 的服务器。默认使用 <https://plistproxy.herokuapp.com/plist>

```bash
gohttpserver --plistproxy=https://someproxyhost.com/
```

测试代理是否工作：

```bash
http POST https://someproxyhost.com/plist < app.plist
{
	"key": "18f99211"
}
http GET https://someproxyhost.com/plist/18f99211
# 显示 app.plist 内容
```

如果你的 gohttpserver 在 Nginx 后面运行且已配置 HTTPS，plistproxy 会自动禁用。

### 使用 CURL 上传

例如，将名为 `foo.txt` 的文件上传到 `somedir` 目录：

```bash
curl -F file=@foo.txt localhost:8000/somedir
{"destination":"somedir/foo.txt","success":true}
# 使用 token 上传
curl -F file=@foo.txt -F token=12312jlkjafs localhost:8000/somedir
{"destination":"somedir/foo.txt","success":true}

# 上传并更改文件名
curl -F file=@foo.txt -F filename=hi.txt localhost:8000/somedir
{"destination":"somedir/hi.txt","success":true}
```

上传 zip 文件并解压（解压完成后 zip 文件将被删除）：

```bash
curl -F file=@pkg.zip -F unzip=true localhost:8000/somedir
{"success": true}
```

注意：文件名中不允许包含 `\/:*<>|` 字符。

上传整个文件夹（保留目录结构）。每个文件随 `path` 表单字段一起发送相对路径，
服务端会自动创建中间目录：

```bash
# 单个文件 + 相对路径，服务端会创建 MyFolder/ 目录
curl -F file=@a.txt     -F path=MyFolder/a.txt     localhost:8000/somedir
curl -F file=@sub/b.txt -F path=MyFolder/sub/b.txt localhost:8000/somedir
# 落地后：somedir/MyFolder/a.txt 与 somedir/MyFolder/sub/b.txt
```

### 使用 CURL 编辑文件

编辑文件内容（PUT 请求，需要 `--edit` 参数）：

```bash
curl -X PUT -H "X-Token: 12312jlkjafs" -d "新文件内容" localhost:8000/somedir/foo.txt
{"destination":"somedir/foo.txt","success":true,"size":15}
```

注意：`path` 字段不允许包含 `..` 或绝对路径（拒绝目录逃逸），且每个路径段
不允许包含 `\: * < > | "` 字符。前端可以通过文件夹选择按钮自动生成这些
`path`（Chrome / Edge / Firefox 支持，Safari 不支持）。

### 使用 Nginx 部署

推荐配置，假设你的 gohttpserver 监听在 `127.0.0.1:8200`：

```
server {
  listen 80;
  server_name your-domain-name.com;

  location / {
    proxy_pass http://127.0.0.1:8200; # 这里需要修改
    proxy_redirect off;
    proxy_set_header  Host    $host;
    proxy_set_header  X-Real-IP $remote_addr;
    proxy_set_header  X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_set_header  X-Forwarded-Proto $scheme;

    client_max_body_size 0; # 禁用上传大小限制
  }
}
```

在 Nginx 后面运行时，gohttpserver 应该使用 `--xheaders` 参数启动。

参考：<http://nginx.org/en/docs/http/ngx_http_core_module.html#client_max_body_size>

gohttpserver 还支持 `--prefix` 参数，这在根路径 `/` 被其他服务占用时很有用。相关 issue：<https://github.com/codeskyblue/gohttpserver/issues/105>

使用示例：

```bash
# gohttpserver 配置
gohttpserver --prefix /foo --addr :8200 --xheaders
```

**Nginx 配置**：

```
server {
  listen 80;
  server_name your-domain-name.com;

  location /foo {
    proxy_pass http://127.0.0.1:8200; # 这里需要修改
    proxy_redirect off;
    proxy_set_header  Host    $host;
    proxy_set_header  X-Real-IP $remote_addr;
    proxy_set_header  X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_set_header  X-Forwarded-Proto $scheme;

    client_max_body_size 0; # 禁用上传大小限制
  }
}
```

## 常见问题

- [如何使用 openssl 生成自签名证书](http://stackoverflow.com/questions/10175812/how-to-create-a-self-signed-certificate-with-openssl)

### 搜索查询格式

搜索查询遵循类似 Google 的通用格式规则。关键词用空格分隔，带 `-` 前缀的关键词将从搜索结果中排除。

1. `hello world` 表示必须同时包含 `hello` 和 `world`
2. `hello -world` 表示必须包含 `hello` 但不包含 `world`

## 开发者指南

> **注意**：`frontend/dist` **没有**提交到仓库（被 `.gitignore` 中的 `dist/`
> 规则忽略），但 `assets.go` 通过 `//go:embed frontend/dist` 指令依赖该目录。
> 因此在全新克隆的仓库上直接执行 `go build ./...` 会报错
> `pattern frontend/dist: no matching files found`。请先运行 `./build.sh`
> （或在 `frontend/` 目录下执行 `npm ci && npm run build`），然后再编译 Go 代码。

1、构建项目（推荐使用使用构建脚本进行构建）

```bash
./build.sh
```

2、运行程序

```bash
# 产物命名为 gohttpserver-<版本号>-<系统>-<架构>[.exe]
# 命令格式
./dist/gohttpserver-1.3.0-windows-x64.exe -r 共享文件夹 --addr IP:端口 --启动参数

# 例如 windows 中运行
./dist/gohttpserver-1.3.0-windows-x64.exe -r ./testdata --addr 0.0.0.0:8000 --login
```

## 支持

该项目是从 **[codeskyblue/gohttpserver](https://github.com/codeskyblue/gohttpserver)** 修改而来 (因为原项目不更新了)，感谢 codeskyblue/gohttpserver 开源支持。

## 许可证

本项目使用 [Apache-2.0](LICENSE) 许可证。

