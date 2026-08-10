# Changelog

本文件记录 gohttpserver 各版本的发布说明。GitHub Actions 在打 tag 发版时会自动读取对应版本段落作为 Release 说明。

## [v1.2.0] - 2026-08-10

# gohttpserver v1.2.0 🚀

Proxy Protocol 支持、文件排序持久化、项目结构优化。

## ✨ 新功能

### 🌐 Proxy Protocol 支持
- 集成 Proxy Protocol v1/v2，支持通过 FRP 等反向代理获取真实客户端 IP
- 管理界面新增 Proxy Protocol 版本选择下拉框
- 日志自动区分访问来源：`[LOCAL]` 标记内网访问，`[FRP]` 标记公网代理访问
- 输入验证确保只允许空字符串、`v1` 或 `v2`

### 📊 文件排序持久化
- 点击表头排序后，排序偏好自动保存到浏览器 localStorage
- 刷新页面或重新访问时，自动恢复上次的排序方式
- 默认排序改为按名称 A-Z 升序（原为修改时间降序）
- 切换目录时保持排序状态

## 🛠 工程改进

### 项目结构重组
- 优化 Docker 配置，提升容器化部署体验
- 代码组织更加清晰

## 📖 文档

- 更新相关文档以反映新功能

---

## [v1.1.3] - 2026-08-05

# gohttpserver v1.1.3 🔒

安全加固与运维增强版本：集成 FRP 内网穿透、新增 Windows 一键安装脚本、全面修复安全漏洞并提升代码质量。

## ✨ 新功能

### 🌐 FRP 内网穿透
- 集成 FRP 内网穿透功能，支持通过管理面板可视化配置
- 配置持久化到 SQLite 数据库，重启后自动恢复
- 支持自定义 FRP 服务器地址、端口、令牌及远程端口映射

### 📦 Windows 一键安装脚本
- 新增 PowerShell 版与 CMD 版一键安装脚本
- 支持自动注册系统服务、配置防火墙规则

### 🗑️ 卸载改进
- 卸载时支持删除登录数据库
- 不保留数据时自动清空安装目录

## 🔒 安全加固

### P0 严重
- **CSP 策略收紧**：移除 Content-Security-Policy 中的 `unsafe-eval`，消除潜在 XSS 攻击面
- **HTTP 安全响应头**：新增 `X-Content-Type-Options=nosniff`、`X-Frame-Options=DENY`、`Referrer-Policy=strict-origin-when-cross-origin`、`Permissions-Policy`（禁用摄像头/麦克风/地理位置）；TLS 启用时自动添加 `Strict-Transport-Security`
- **上传大小限制**：限制单文件上传不超过 10 GiB，防止磁盘耗尽 DoS
- **Zip 炸弹防护**：zip 解压强制 100:1 最大压缩比检查，超出即拒绝
- **日志注入防护**：新增 `sanitizeLogInput` 函数，所有用户可控输入（路径、URL、IP）写入日志前进行清洗

### P1 高危
- **密码哈希升级**：密码存储从 SHA-256 迁移到 bcrypt，自动检测并迁移旧哈希
- **登录限流**：每个 IP 15 分钟内最多 5 次登录失败，超出后拒绝请求
- **默认凭据提醒**：使用默认 `admin/admin` 登录后提示修改密码

## 🐛 Bug 修复

- **systemd 服务**：`WorkingDirectory` 由数据文件夹改为程序安装目录，修复服务启动路径问题
- **login.go 崩溃**：修复 `db` 为 nil 时调用 `db.QueryRow()` 导致的 panic
- **build.sh 脚本**：添加 `set -euo pipefail` 确保错误时退出，版本获取逻辑优化为 if-elif-else 结构

## 🛠 代码质量

- 错误消息首字母统一大写
- `log.Printf` 无格式化参数的调用替换为 `log.Println`
- 清理注释掉的代码块
- 修复语法错误（如 `contains` 改为 `contain`）

## 📖 文档

- README 增加 Linux 一键安装脚本命令

## [v1.1.2] - 2026-07-19

# gohttpserver v1.1.2 🚀

新增内置登录系统、管理面板和 WebDAV 服务器。

## ✨ 新功能

### 🔐 内置登录认证
- 新增 `--login` 标志启用用户名/密码登录拦截，独立于现有 `--auth-type`
- 基于 gorilla/sessions 的会话管理，支持 `login`/`openid`/`oauth2-proxy` 多种认证方式
- 默认凭据 `admin/admin`（仅内存，修改密码后持久化到 `auth-state.json`）
- 登录中间件：浏览器导航重定向到登录页，API 请求返回 401
- 白名单机制：静态资源、登录接口、WebDAV 路径无需认证
- 启用 `--login` 时自动开启 `--upload`、`--delete`、`--edit`

### 📋 管理面板
- 全屏管理界面，左侧导航 + 右侧内容，响应式适配移动端
- **个人中心**：查看用户名、认证方式、服务器版本；修改用户名和密码
- **参数设置**：WebDAV 服务总开关、WebDAV URL 展示、账号管理入口

### 📂 WebDAV 服务器
- 基于 `golang.org/x/net/webdav` 扩展，挂载在 `/dav/` 路径
- HTTP Basic Auth 独立认证，兼容 Windows 资源管理器、macOS Finder、Linux 文件管理器
- 每账号 chroot：限制访问范围到指定子目录
- 只读模式：拒绝所有写操作
- 系统文件保护：拒绝写入 `auth-state.json`、`webdav-accounts.json`、`.ghs.yml` 等敏感文件
- 配额系统：每账号字节级配额限制，PROPFIND 返回 RFC 4331 磁盘容量信息
- 账号管理：创建（自动生成随机密码）、编辑、删除、重置密码、使用量重算
- 用户名同步：登录用户改名后自动更新所有绑定的 WebDAV 账号

## 🔒 安全改进

- **Session key 随机化**：优先使用 `GHS_SESSION_KEY` 环境变量，否则生成随机 key，避免源码泄露导致会话伪造
- **Cookie 加固**：`HttpOnly=true`（防 XSS）、`SameSite=Lax`（防 CSRF）
- **开放重定向防护**：`safeNextURL()` 验证 `next` 参数，仅允许同源或相对 URL
- **Scheme 正确性**：`requestScheme()` 优先使用 `r.URL.Scheme`，修复 HTTPS 部署时 OpenID 回调验证失败
- 敏感数据（`auth-state.json`、`webdav-accounts.json`）存储在工作目录而非 `--root`，避免被 HTTP 暴露

## 📖 文档

- README 新增登录认证、管理面板、WebDAV 服务器使用说明
- 新增 Windows 映射网络驱动器注册表配置、Linux 文件管理器连接说明、curl 使用示例

## ✅ 测试

- 新增 `admin_api_test.go`、`login_test.go`、`webdav_quota_test.go` 测试文件
- 全部测试通过

## [v1.0.2] - 2026-07-16

# gohttpserver v1.0.2 🐛

前端代码审计修复版本：修复暗色主题样式失效、事件监听器泄漏、视频播放器逻辑缺陷，并优化文件列表行高。

## 🐛 Bug 修复

### P0 严重
- **修复暗色主题样式失效**：`App.vue` 与 `TextPreviewModal.vue` 中误用 `.theme-black`/`.theme-green` 类选择器，而主题实际通过 `data-theme` 属性应用，导致暗色主题下 GitHub 图标不可见、代码语法高亮颜色不切换。改为 `:global([data-theme="black"])`/`:global([data-theme="green"])` 属性选择器
- **修复 popstate 事件监听器泄漏**：`App.vue` 中 popstate 监听器使用匿名箭头函数注册，`onBeforeUnmount` 未移除，HMR 下监听器累积导致每次导航触发多次 `loadFiles`。提取为具名函数 `handlePopState` 并在卸载时正确移除
- **修复视频播放器 watch 逻辑缺陷**：`VideoPlayer.vue` 中 `videoRef.value` 判空检查在 `await nextTick()` 之前执行，由于 dialog 设置了 `destroy-on-close`，watch 回调在组件重渲染前同步执行时 `videoRef.value` 为 null，导致 `video.load()`/`video.focus()` 永不执行。将判空检查移到 `nextTick` 之后

## 🎨 UI 优化

- **文件列表行高调整**：桌面端表格行高从 35.33px 增加到 41px（内容区 line-height 18.33px → 24px），改善 Actions 操作按钮的显示，避免控件被挤压
- **操作按钮高度同步**：Actions 列按钮高度从 22px 调整为 24px，适配新的行高

## ✅ 测试

- `vue-tsc` 类型检查通过
- `vite build` 构建成功（1793 个模块）

## [v1.0.1] - 2026-07-12

# gohttpserver v1.0.1 🔒

代码审计修复版本：修复 IPA 图标失效、模板缓存数据竞争，并消除多处服务器绝对路径泄露。

## 🔒 安全修复

### P0 严重
- **修复 IPA 图标 URL 404**：注册 `/-/unzip/{zip_path}/-/{path}` 路由，此前 `hUnzip` 处理函数从未注册路由，导致 iOS 安装清单中的图标 URL 全部失效
- **修复模板缓存数据竞争**：`_tmpls` 由裸 map 改为 `sync.Map`，消除 `-race` 下的并发读写竞争

### P1 高危
- **错误信息脱敏**：`hJSONList`/`hUploadOrMkdir`/`hEdit`/`hFetch`/`hPlist`/`hIpaLink` 等 handler 不再向客户端返回包含服务器绝对路径的错误消息
- **响应字段脱敏**：上传/编辑/下载 JSON 响应的 `destination` 字段改为相对路径，避免泄露服务器目录结构
- **Slowloris 防护**：`http.Server` 添加 `ReadHeaderTimeout` 和 `IdleTimeout`

## 🛠 代码质量

### P2
- **修复 zip 解压文件句柄累积**：`unzipFile` 循环内的 `defer rc.Close()` 抽取为 `extractZipEntry` 函数，每次迭代即时释放句柄
- **修复 IPv6 地址解析**：`getRealIP` 改用 `net.SplitHostPort`，此前 IPv6 地址 `[::1]:8080` 被错误解析为 `[`

### P3
- 清理死代码 `hFileOrDirectory`
- 清理 `hUploadOrMkdir` 中大文件 rename 方案的注释代码块

## ✅ 测试

- 新增 `getRealIP` IPv6 测试用例
- 全部测试通过 `go test -race`

## [v1.0.0] - 2026-07-11

# gohttpserver v1.0.0 🎉

构建最好用的 HTTP 文件服务器。

## ✨ 本次更新

### 🎨 前端重构
- 使用 Vue 3 + Element Plus + Vite 全面重写
- 完整移动端适配，浅色/深色主题切换

### 🚀 新功能
- 📁 文件夹上传（保留目录结构）
- ✏️ 在线文件编辑（支持 curl PUT）
- 🔐 MD5 / SHA 计算
- 📥 URL 下载
- ☑️ 批量多选操作
- 📊 按大小/时间排序
- 📦 上传 zip 自动解压
- 🗂️ 文件夹大小显示
- ➕ 创建文件夹
- ⚡ Alt 跳过删除确认
- 🔍 全局文件搜索

### 🛠 工程
- Go 1.26，代码现代化

## 📥 下载

| 平台 | 文件 |
| --- | --- |
| Linux amd64 | `gohttpserver-linux-amd64` |
| Linux arm64 | `gohttpserver-linux-arm64` |
| macOS amd64 | `gohttpserver-mac-amd64` |
| macOS arm64 | `gohttpserver-mac-arm64` |
| Windows amd64 | `gohttpserver-win-amd64.exe` |
| Windows arm64 | `gohttpserver-win-arm64.exe` |
