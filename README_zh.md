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

## 安装

从 [GitHub Releases](https://github.com/toluckykoi/gohttpserver/releases) 或者 [Gitee Releases](https://gitee.com/toluckykoi/gohttpserver/releases) 下载二进制文件。

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

