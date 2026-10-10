// Package fs 封装跨平台文件系统差异处理。
package fs

import (
	"path/filepath"
	"runtime"
	"strings"
)

// NormalizePath 统一分隔符为 "/"，供内部存储与比较。
func NormalizePath(p string) string {
	return filepath.ToSlash(p)
}

// CompareKey 返回大小写不敏感的比较键（Windows / macOS 默认不敏感）。
func CompareKey(p string) string {
	return strings.ToLower(filepath.ToSlash(p))
}

// IsHidden 判断是否为隐藏 / 系统文件（.DS_Store / Thumbs.db / 以 "." 开头）。
func IsHidden(name string) bool {
	if name == "" {
		return false
	}
	if name == ".DS_Store" || name == "Thumbs.db" {
		return true
	}
	return strings.HasPrefix(name, ".")
}

// ShouldIgnore 根据 ignore 规则判断相对路径 rel（统一 "/"）是否应忽略。
// 支持：精确匹配、filepath.Match glob、目录前缀（以 "/" 结尾）、
// basename 匹配、**/xxx 形式。
func ShouldIgnore(rel string, patterns []string) bool {
	rel = filepath.ToSlash(rel)
	if rel == "" {
		return false
	}
	base := filepath.Base(rel)
	for _, pat := range patterns {
		pat = filepath.ToSlash(pat)
		if pat == "" {
			continue
		}
		if pat == rel {
			return true
		}
		if strings.HasSuffix(pat, "/") {
			if strings.HasPrefix(rel, pat) {
				return true
			}
			continue
		}
		if matched, _ := filepath.Match(pat, rel); matched {
			return true
		}
		if matched, _ := filepath.Match(pat, base); matched {
			return true
		}
		if strings.HasPrefix(pat, "**/") {
			sub := strings.TrimPrefix(pat, "**/")
			if rel == sub || strings.HasSuffix(rel, "/"+sub) {
				return true
			}
			if matched, _ := filepath.Match(sub, base); matched {
				return true
			}
		}
	}
	return false
}

// SupportsSymlink 当前文件系统是否支持符号链接（简化：Windows 视为不支持）。
func SupportsSymlink() bool {
	return runtime.GOOS != "windows"
}

// CanonicalPath 返回路径的规范化绝对路径（P1-3）：
//   - Abs + Clean 总是执行
//   - 若路径存在则再 EvalSymlinks 解析真实路径，避免大小写/符号链接写出不同形态
//
// 路径不存在时退回到 Abs+Clean，便于校验尚未创建的备份目标。
func CanonicalPath(p string) string {
	if p == "" {
		return ""
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		abs = p
	}
	abs = filepath.Clean(abs)
	if real, err := filepath.EvalSymlinks(abs); err == nil {
		// EvalSymlinks 返回的是已解析路径，再次 Clean 保证一致
		return filepath.Clean(real)
	}
	return abs
}

// IsAncestor 报告 ancestor 是否是 path 的祖先（或相同）。
// 两者必须都是规范化后的绝对路径。
func IsAncestor(ancestor, path string) bool {
	if ancestor == path {
		return true
	}
	a := filepath.Clean(ancestor)
	p := filepath.Clean(path)
	if a == p {
		return true
	}
	// 路径前缀匹配需保证目录边界，避免 "C:\Foo" 被误判为 "C:\Foobar" 的祖先
	if runtime.GOOS == "windows" {
		return strings.HasPrefix(strings.ToLower(p), strings.ToLower(a)+string(filepath.Separator))
	}
	return strings.HasPrefix(p, a+string(filepath.Separator))
}

// IsSystemRoot 报告 path 是否为高风险系统根目录或盘符根（P1-3），
// 用于阻止误选 C:\ / / / /Users / C:\Windows 等作为同步根。
func IsSystemRoot(path string) bool {
	c := filepath.Clean(path)
	lower := strings.ToLower(c)
	// 盘符根，如 C:\ 或 D:/
	if len(c) == 3 && c[1] == ':' && (c[2] == '\\' || c[2] == '/') {
		return true
	}
	// Unix 根
	if c == "/" || c == "\\" {
		return true
	}
	blocklist := []string{
		`c:\windows`, `/windows`,
		`c:\program files`, `/usr`,
		`c:\program files (x86)`,
		`c:\users`, `/home`,
		`c:\programdata`,
		`c:\`,
		`/`,
	}
	for _, b := range blocklist {
		if lower == b {
			return true
		}
	}
	return false
}
