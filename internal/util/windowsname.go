package util

import (
	"runtime"
	"strings"
)

// WindowsNormaliseName 把文件基名归一化为 Windows 语义的主文件名：剥尾部点/空格、
// 截断 NTFS 数据流（"file::$DATA" 与 "file." 打开的都是 file）。保护名单比较前
// 必须先归一化，否则可被这些变体绕过；返回值仅用于受保护名等价判断，不作为真实路径。
func WindowsNormaliseName(name string) string {
	// 先剥尾部再截断 ADS 后仍需再剥一次："file.::$DATA" 截断后剩 "file."。
	name = strings.TrimRight(name, ". ")
	if idx := strings.IndexByte(name, ':'); idx >= 0 {
		name = name[:idx]
		name = strings.TrimRight(name, ". ")
	}
	return name
}

// MatchesSensitiveShortName 判断 name（应已经 WindowsNormaliseName 归一化）是否为
// Windows 8.3 短名形态的敏感文件：~<数字>.<敏感扩展名>，如 GOHTTP~1.DB、FRP-EN~1.KEY。
// 系统卷默认生成 8.3 短名，凭据库/密钥可经短名绕过精确名单（H2），server/zipx/webdav
// 三包共用本实现避免各自维护导致遗漏；截断规则随卷配置浮动，长度放宽到 14 以覆盖
// ~10 之后的序号。非 Windows 平台 8.3 短名不存在，此类名字是合法文件名
// （如 backup~1.json），必须放行以免误伤正常文件。
func MatchesSensitiveShortName(name string) bool {
	if runtime.GOOS != "windows" {
		return false
	}
	upper := strings.ToUpper(name)
	// 8.3 短名模式：<≤7 位前缀>~<数字>.<截断扩展名>。
	idx := strings.IndexByte(upper, '~')
	if idx <= 0 || idx > 7 || len(upper) > 14 {
		return false
	}
	rest := upper[idx+1:]
	dot := strings.IndexByte(rest, '.')
	if dot < 1 {
		return false
	}
	for _, c := range rest[:dot] {
		if c < '0' || c > '9' {
			return false
		}
	}
	ext := rest[dot+1:]
	// HasPrefix "DB" 覆盖 "gohttpserver.db-wal" 截断出的 DB-/DBW 等形态。
	return strings.HasPrefix(ext, "DB") || ext == "KEY" || ext == "WAL" ||
		ext == "SHM" || ext == "JSO" || ext == "JSON"
}
