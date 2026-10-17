package main

func init() {
	// "unknown" 表示构建时未通过 -ldflags 注入；此时标记为 "dev"（未打 tag 的构建）
	if VERSION == "unknown" {
		VERSION = "dev"
	}
}
