package sync

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
)

// HashFile 计算文件内容的 SHA-256 十六进制摘要。
// 使用 1 MiB 缓冲流式读取，避免大文件占用过多内存。
func HashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()

	buf := make([]byte, 1<<20) // 1 MiB
	for {
		n, readErr := f.Read(buf)
		if n > 0 {
			if _, werr := h.Write(buf[:n]); werr != nil {
				return "", werr
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return "", fmt.Errorf("读取 %s 失败: %w", path, readErr)
		}
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
