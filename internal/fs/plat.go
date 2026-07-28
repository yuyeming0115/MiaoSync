// Package fs 封装跨平台文件系统差异（骨架）。
//
// 关键坑点见 docs/design.md 第 6 节：
//   - exFAT 不支持符号链接与权限
//   - exFAT 时间精度仅 2 秒，增量比对必须依赖内容 hash
package fs

import "runtime"

// NormalizePath 按当前平台规范化路径分隔符与大小写策略。
// 内部统一使用 "/" 分隔、小写比较（以"不敏感"为准，避免同名不同写）。
func NormalizePath(p string) string {
	// TODO: 将 "\\" 替换为 "/"，并按需小写化用于比较键。
	return p
}

// SupportsSymlink 返回当前文件系统是否支持符号链接。
// exFAT 等移动硬盘格式通常不支持。
func SupportsSymlink() bool {
	// TODO: 实际探测挂载点文件系统类型。
	return runtime.GOOS != "windows" // 占位：Windows 下 exFAT 场景需进一步判断
}
