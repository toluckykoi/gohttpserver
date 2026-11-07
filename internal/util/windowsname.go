package util

import "strings"

// WindowsNormaliseName 把文件基名归一化为 Windows 语义下的「主文件名」，供
// 保护名单类比较使用。Win32 在解析最终路径组件时会：
//
//  1. 剥离尾部的点与空格——"file."、"file "、"file . ." 打开的都是 file；
//  2. 把 "file:stream" / "file::$DATA" 形式的 NTFS 备用数据流语法落到主文件上，
//     对流的读写等同于对主文件默认数据流的读写。
//
// 因此保护名单仅做 EqualFold（哪怕加上 8.3 短名）仍可被 ".ghs.yml." 或
// "gohttpserver.db::$DATA" 绕过（第 8 轮审计实证）。比较前先归一化，一次封死
// 整类变体。
//
// 返回值只用于「是否为某受保护名的等价名」的判断，不作为真实路径使用。
// 对不含冒号、尾部干净的普通名字是恒等变换；在 Linux 上会把 "db:x" 之类
// （ext4 中与 "db" 不同的真实文件）误判为等价名，属于可接受的过度拒绝。
func WindowsNormaliseName(name string) string {
	// 先剥尾部（点/空格），再截断 ADS，最后再剥一次：
	// "file.::$DATA" 第一步剥不动（尾部是 A），截断后剩 "file."，需再剥。
	name = strings.TrimRight(name, ". ")
	if idx := strings.IndexByte(name, ':'); idx >= 0 {
		name = name[:idx]
		name = strings.TrimRight(name, ". ")
	}
	return name
}
