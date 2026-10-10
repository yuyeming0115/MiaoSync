package sync

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/yuyeming0115/MiaoSync/internal/fs"
)

// SnapshotDir 快照目录名（位于各节点根目录内，已被默认忽略）。
const SnapshotDir = ".miaosync"

// SnapshotFile 快照文件名。
const SnapshotFile = "snapshot.json"

// SchemaVersion 快照结构版本（P1-8）。读到更高版本或损坏时进入恢复模式而非静默覆盖。
const SchemaVersion = 2

// Snapshot 节点上一次扫描的快照，用于增量比对与删除检测。
type Snapshot struct {
	Version         int                   `json:"version"` // P1-8：schema 版本
	Node            string                `json:"node"`
	RootFingerprint string                `json:"rootFingerprint"`       // P1-8：根目录规范化路径指纹，用于校验目录身份
	SyncGroupID     string                `json:"syncGroupId,omitempty"` // P1-8：本次同步组（可选）
	RunID           string                `json:"runId,omitempty"`       // P1-8：生成此快照的成功 run ID（可选）
	Updated         int64                 `json:"updated"`               // UnixNano
	Files           map[string]*FileState `json:"files"`                 // key = 小写比较键
}

// SnapshotPath 返回某节点根目录下的快照文件路径。
func SnapshotPath(nodePath string) string {
	return filepath.Join(nodePath, SnapshotDir, SnapshotFile)
}

// fingerprintRoot 计算节点根目录规范化路径的短指纹（SHA-256 前 16 字符）。
func fingerprintRoot(nodePath string) string {
	cp := fs.CanonicalPath(nodePath)
	h := sha256.Sum256([]byte(cp))
	return hex.EncodeToString(h[:])[:16]
}

// LoadSnapshot 读取快照；不存在时返回空快照（不报错）。
// P1-8：版本不兼容或 JSON 损坏时返回错误，调用方可进入只读诊断模式而非直接覆盖。
func LoadSnapshot(nodePath string) (*Snapshot, error) {
	p := SnapshotPath(nodePath)
	data, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return &Snapshot{
				Version:         SchemaVersion,
				Node:            filepath.Base(nodePath),
				RootFingerprint: fingerprintRoot(nodePath),
				Files:           map[string]*FileState{},
			}, nil
		}
		return nil, err
	}
	var s Snapshot
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("快照损坏（%s）: %w；请人工核查或删除该快照后重新全量同步", p, err)
	}
	// 兼容旧版本（无 Version 字段视为 v1）
	if s.Version == 0 {
		s.Version = 1
	}
	if s.Version > SchemaVersion {
		return nil, fmt.Errorf("快照版本 %d 高于当前支持版本 %d（%s）；请升级 MiaoSync 后再操作", s.Version, SchemaVersion, p)
	}
	if s.Files == nil {
		s.Files = map[string]*FileState{}
	}
	// v1 → v2 迁移：补全指纹字段（不阻断，仅记录）
	if s.RootFingerprint == "" {
		s.RootFingerprint = fingerprintRoot(nodePath)
	}
	return &s, nil
}

// SaveSnapshot 原子写入快照（先写临时文件再 rename）。
// P1-8：写入时携带 schema 版本与根目录指纹。
func SaveSnapshot(nodePath string, s *Snapshot) error {
	dir := filepath.Join(nodePath, SnapshotDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	s.Version = SchemaVersion
	s.Updated = time.Now().UnixNano()
	if s.RootFingerprint == "" {
		s.RootFingerprint = fingerprintRoot(nodePath)
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp := SnapshotPath(nodePath) + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, SnapshotPath(nodePath))
}

// VerifyRootFingerprint 报告快照记录的根目录指纹是否与当前节点路径一致（P1-8）。
// 不一致通常意味着盘符复用或目录被移动，调用方应提示用户而非直接覆盖。
func (s *Snapshot) VerifyRootFingerprint(nodePath string) bool {
	if s.RootFingerprint == "" {
		return true // 旧快照无指纹，不阻断
	}
	return s.RootFingerprint == fingerprintRoot(nodePath)
}
