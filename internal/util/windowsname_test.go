package util

import (
	"runtime"
	"testing"
)

// WindowsNormaliseName 是第 8 轮审计修复的核心：保护名单比较前必须把 Windows
// 语义下的等价名（尾点/尾空格/ADS 流名）归一化，否则 "gohttpserver.db." 之类
// 变体可绕过全部私有文件拦截。这些用例不依赖运行平台——归一化无条件生效。
func TestWindowsNormaliseName(t *testing.T) {
	cases := map[string]string{
		// 恒等变换
		"file.txt":        "file.txt",
		".ghs.yml":        ".ghs.yml",
		"gohttpserver.db": "gohttpserver.db",
		"plain":           "plain",
		"":                "",
		// 尾部点/空格剥离
		"file.":            "file",
		"file ":            "file",
		"file. . .":        "file",
		".ghs.yml.":        ".ghs.yml",
		"gohttpserver.db.": "gohttpserver.db",
		// ADS 流名截断
		"file::$DATA":     "file",
		"file:stream":     "file",
		"FILE::$data":     "FILE", // 大小写交给调用方 EqualFold
		".ghs.yml::$DATA": ".ghs.yml",
		"db.::$DATA":      "db", // 尾点 + ADS 组合
		// 其它
		"a:b:c": "a", // 截断到首个冒号（比较用途，非路径解析）
		".":     "",
		"..":    "",
	}
	for in, want := range cases {
		if got := WindowsNormaliseName(in); got != want {
			t.Errorf("WindowsNormaliseName(%q) = %q, want %q", in, got, want)
		}
	}
}

// MatchesSensitiveShortName 是第 11 轮审计 H2 修复的核心：系统卷默认生成 8.3 短名，
// 凭据库/密钥可经 GOHTTP~1.DB 之类的短名绕过精确名单。server/zipx/webdav 三包共用
// 本实现。
// 8.3 短名仅存在于 Windows 卷；非 Windows 平台（Linux/macOS 等）上
// "backup~1.json" 之类名字是合法普通文件名，实现已按平台门禁放行，故未命中用例
// 在所有平台都必须成立，命中用例仅 Windows 成立。
func TestMatchesSensitiveShortName(t *testing.T) {
	cases := map[string]bool{
		// 命中（仅 Windows）：敏感扩展的短名形态
		"GOHTTP~1.DB":   true,
		"gohttp~1.db":   true, // 大小写在函数内归一
		"GOHTTP~2.DB":   true,
		"GOHTTP~10.DB":  true,
		"FRP-EN~1.KEY":  true,
		"WEBDAV~1.JSO":  true,
		"AUTH-S~1.JSON": true,
		"GOHTTP~1.WAL":  true,
		"GOHTTP~1.SHM":  true,
		"GOHEA~1.DB-":   true, // db-wal 截断形态（HasPrefix "DB"）
		// 不命中（所有平台）
		"normal.txt":           false,
		"gohttpserver.db":      false, // 长名走精确名单，不归本函数
		"file~1.txt":           false, // 扩展名不敏感
		"notes~1.doc":          false,
		"~1.DB":                false, // 前缀为空
		"abc~.DB":              false, // 无序号
		"GOHTTP~x.DB":          false, // 序号非数字
		"abcdefgh~1.DB":        false, // 前缀超 7 位
		"TOOLONGPREFIX12~1.DB": false, // 总长超 14
		"plain":                false,
	}
	for in, want := range cases {
		got := MatchesSensitiveShortName(in)
		if runtime.GOOS != "windows" {
			// 非 Windows：固定放行（8.3 短名不存在）。
			if got {
				t.Errorf("MatchesSensitiveShortName(%q) 在非 Windows 平台应恒为 false", in)
			}
			continue
		}
		if got != want {
			t.Errorf("MatchesSensitiveShortName(%q) = %v, want %v", in, got, want)
		}
	}
}
