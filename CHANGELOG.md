# Changelog

本文件记录 gohttpserver 各版本的发布说明。GitHub Actions 在打 tag 发版时会自动读取对应版本段落作为 Release 说明。

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
