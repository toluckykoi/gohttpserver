# Changelog

本文件记录 gohttpserver 各版本的发布说明。GitHub Actions 在打 tag 发版时会自动读取对应版本段落作为 Release 说明。

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
