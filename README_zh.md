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

## 安装

### 一键安装（推荐，Linux）

运行官方安装脚本，自动下载二进制、配置 systemd 服务、引导设置端口和参数：

```bash
curl -fsSL https://gitee.com/toluckykoi/gohttpserver/raw/main/install-gohttpserver.sh -o install-gohttpserver.sh && sudo bash install-gohttpserver.sh
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

使用 `gohttpserver --help` 查看更多使用选项。

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

- 启用上传功能：

  ```bash
  gohttpserver --upload
  ```

- 启用删除和创建文件夹功能：

  ```bash
  gohttpserver --delete
  ```

- 启用文件编辑功能：

  ```bash
  gohttpserver --edit
  ```

- 启用登录拦截（用户名 / 密码），未登录无法访问任何路径或文件：

  ```bash
  gohttpserver --login
  ```

  默认凭据为 `admin` / `admin`，未修改密码前不会在磁盘上生成任何文件，
  默认凭据仅保留在内存中。登录后可在右上角用户菜单 → 「管理面板」→ 「个人中心」
  中更新密码，新密码会持久化到当前工作目录下的 `./gohttpserver.db`（SQLite
  数据库文件，不会放在 `--root` 目录下，避免被 HTTP 服务暴露）。如需自定义
  数据库位置，可使用 `--db /path/to/gohttpserver.db`。

  登录会话默认保留 12 小时（cookie 持久化，重启软件期间有效）。可通过
  `--session-ttl` 调整（例如 `--session-ttl=24h`，或 `--session-ttl=0`
  设为仅本浏览器会话有效——关闭浏览器即失效）。如需跨服务器重启保持登录
  状态，还需设置环境变量 `GHS_SESSION_KEY`，否则每次重启会重新生成签名密钥，
  旧的 cookie 全部失效。

  注意：`--login` 与 `--auth-type` 完全独立，可单独使用或组合使用（同时叠加两种认证）。
  当显式传入 `--login` 时，`--upload`、`--delete`、`--edit` 会自动启用（已认证的操作员
  应当能够管理文件，无需单独传这些参数）。

## 管理面板

启用 `--login` 后，点击右上角用户头像 → 「管理面板」即可打开全屏管理面板。
面板包含两个 tab：

- **个人中心**：展示当前登录用户的用户名、认证方式、版本号；支持修改用户名
  （需要输入当前密码二次验证）和修改密码
- **参数设置**：WebDAV 服务的总开关、URL 展示、WebDAV 账号管理

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

> 说明：出于安全设计，后端只存密码的 SHA-256 哈希、明文仅在创建/重置时产生一次。
> 为支持「基本信息」中随时查看密码，前端会把明文密码缓存到浏览器 `localStorage`
> （按账号 id）。换浏览器或清除缓存后需通过「重置密码」重新生成才能再次查看。

WebDAV 账号状态持久化到当前工作目录下的 `./webdav-accounts.json`
（不会放在 `--root` 目录下）。如需自定义文件位置，可使用
`--webdav-accounts /path/to/webdav-accounts.json`。

服务端会记录 WebDAV 的**写操作**日志（上传 PUT / 删除 DELETE / 新建目录 MKCOL /
移动 MOVE / 复制 COPY / 属性修改 PROPPATCH），以及认证失败，包含用户名、来源 IP
和请求路径；浏览类请求（PROPFIND / OPTIONS / GET 等）不打印以避免刷屏。

### 磁盘容量显示（RFC 4331）

`golang.org/x/net/webdav` 原生不实现 RFC 4331 配额属性，导致 WebDAV 客户端
（Windows 资源管理器、Cyberduck、rclone）连接后无法显示磁盘容量。gohttpserver
通过响应拦截中间件在 PROPFIND 207 multistatus 响应中注入
`quota-available-bytes` / `quota-used-bytes`：剥离底层返回的 404 quota propstat，
追加 200 真实数值（跨平台磁盘查询：Unix 用 `syscall.Statfs`，Windows 用
`GetDiskFreeSpaceEx`）。仅对 collection（目录）注入，文件响应保持 404 ——
客户端从父目录读取容量值。

curl 使用示例：

```bash
# 列出根目录
curl -u admin:RANDOM_PASSWORD -X PROPFIND http://localhost:8000/dav/ -H "Depth: 1"

# 上传文件
curl -u admin:RANDOM_PASSWORD -T file.txt http://localhost:8000/dav/file.txt
```

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

依赖通过 [govendor](https://github.com/kardianos/govendor) 管理

1. 先编译前端

   ```shell
   cd frontend
   npm run build
   ```

2. 构建开发版本。**frontend/dist** 目录必须存在：

   ```shell
   go build
   ./gohttpserver
   ```

3. 构建单二进制文件发布版：

   ```shell
   # 编译项目
   go build
   
   # 运行
   ./gohttpserver.exe -r ./testdata --addr 127.0.0.1:8000 --upload --delete --edit
   ```

## 支持

该项目是从 **[codeskyblue/gohttpserver](https://github.com/codeskyblue/gohttpserver)** 修改而来 (因为原项目不更新了)，感谢 codeskyblue/gohttpserver 开源支持。

## 许可证

本项目使用 [Apache-2.0](LICENSE) 许可证。

