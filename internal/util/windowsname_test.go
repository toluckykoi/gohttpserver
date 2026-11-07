package util

import "testing"

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
