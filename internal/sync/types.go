// Package sync 实现 MiaoSync 三端同步引擎：扫描、比对、计划、应用。
package sync

import "fmt"

// Mode 同步模式。
type Mode string

const (
	ModeMirror Mode = "mirror" // 双向同步：两端互相补齐，删除也互相同步
	ModeBackup Mode = "backup" // 单向备份：仅 source -> target
)

// ConflictStrategy 冲突处理策略。
type ConflictStrategy string

const (
	ConflictRename ConflictStrategy = "rename" // 保留较新一方，另一方重命名为 .conflict-<ts>
	ConflictKeep   ConflictStrategy = "keep"   // 两端各自保留，较新端加 .conflict 后缀
	ConflictNewer  ConflictStrategy = "newer"  // 仅保留 mtime 较新者，旧者删除
)

// ActionKind 同步动作类型。
type ActionKind string

const (
	ActionCopy           ActionKind = "copy"            // 复制文件
	ActionDelete         ActionKind = "delete"          // 删除文件
	ActionRenameConflict ActionKind = "rename-conflict" // 冲突处理：重命名旧版本并复制新版本
	ActionSkip           ActionKind = "skip"            // 跳过
)

// Role 节点角色。
type Role string

const (
	RolePeer   Role = "peer"   // 对等节点（mirror 模式）
	RoleSource Role = "source" // 备份源（backup 模式）
	RoleTarget Role = "target" // 备份目标（backup 模式）
)

// Node 表示一个同步端点（Win 电脑 / Mac 电脑 / 移动硬盘）。
type Node struct {
	Name string // 端点名称，如 "win-pc" / "mac-pc" / "hdd"
	Path string // 当前平台下解析出的绝对根路径
	Role string // "peer" | "source" | "target"
}

// FileState 单个文件的快照状态。
type FileState struct {
	Path  string `json:"path"`  // 相对根目录的路径，统一用 "/"
	Size  int64  `json:"size"`  // 字节数
	Mtime int64  `json:"mtime"` // 修改时间（纳秒）
	Hash  string `json:"hash"`  // 内容哈希（SHA-256）
}

// EnsureHash 按需计算或返回文件的 SHA-256 哈希。若已有缓存则直接返回，否则流式读取计算并缓存。
func (f *FileState) EnsureHash(fullPath string) string {
	if f == nil {
		return ""
	}
	if f.Hash != "" {
		return f.Hash
	}
	h, err := HashFile(fullPath)
	if err == nil {
		f.Hash = h
	}
	return f.Hash
}

// Action 一个待执行的同步动作。
type Action struct {
	Kind    ActionKind `json:"kind"`
	RelPath string     `json:"relPath"` // 相对根路径（展示用）
	From    string     `json:"from"`    // 源绝对路径（copy / rename-conflict 时）
	To      string     `json:"to"`      // 目标绝对路径（展示用）
	Size    int64      `json:"size"`
	Reason  string     `json:"reason"` // 人类可读原因

	// ConflictRoot 仅用于冲突动作（P1-6）：败方节点根目录，用于把冲突备份归档到
	// <loserRoot>/.miaosync/conflicts/<relSubPath>/ 下，避免散落在业务目录且不传播。
	// 不序列化到 JSON（内部字段）。
	ConflictRoot string `json:"-"`
}

// ProgressFunc 进度回调：stage 阶段名、done/total 进度、msg 当前信息。
type ProgressFunc func(stage string, done, total int, msg string)

// TransferItem 传输明细中的单条记录（用于报告里的“传输列表”）。
type TransferItem struct {
	Kind   string `json:"kind"`   // copy | delete | conflict | skip
	From   string `json:"from"`   // 源绝对路径（delete 时为空）
	To     string `json:"to"`     // 目标绝对路径（delete 时为被删路径）
	Size   int64  `json:"size"`   // 字节数
	Pair   string `json:"pair"`   // 参与该操作的节点对，如 "win-pc ↔ 移动硬盘"
	Status string `json:"status"` // ok | error | skip
}

// SyncSummary 一次同步的汇总结果。
type SyncSummary struct {
	TotalActions    int            `json:"totalActions"`
	Copied          int            `json:"copied"`
	Deleted         int            `json:"deleted"`
	Conflicts       int            `json:"conflicts"`
	Skipped         int            `json:"skipped"`
	Errors          int            `json:"errors"`
	BytesCopied     int64          `json:"bytesCopied"`     // 本次实际复制的字节数
	DurationMs      int64          `json:"durationMs"`      // 本次同步耗时（毫秒）
	RequiresConfirm bool           `json:"requiresConfirm"` // 存在危险操作，需用户二次确认
	DangerousInfo   string         `json:"dangerousInfo"`   // 危险操作说明
	Message         string         `json:"message"`
	Transferred     []TransferItem `json:"transferred"` // 逐文件传输明细
}

// SyncOptions 同步执行选项。
type SyncOptions struct {
	Mode             Mode             // 同步模式
	Conflict         ConflictStrategy // 冲突策略
	DryRun           bool             // 仅计划不执行
	ConfirmDangerous bool             // 已确认危险操作（删除过多时）
	SelectedPaths    []string         // 用户勾选的文件相对路径列表（为空表示全选）
}

// PlanOptions 计划生成选项。
type PlanOptions struct {
	Mode     Mode
	Conflict ConflictStrategy
}

// ValidateMode 校验模式合法性，非法时回退为 mirror。
// 已弃用：保留以兼容旧调用点；新代码应使用 ParseMode 做严格校验。
func ValidateMode(s string) Mode {
	switch Mode(s) {
	case ModeMirror, ModeBackup:
		return Mode(s)
	default:
		return ModeMirror
	}
}

// ValidateConflict 校验冲突策略，非法时回退为 rename。
// 已弃用：保留以兼容旧调用点；新代码应使用 ParseConflict 做严格校验。
func ValidateConflict(s string) ConflictStrategy {
	switch ConflictStrategy(s) {
	case ConflictRename, ConflictKeep, ConflictNewer:
		return ConflictStrategy(s)
	default:
		return ConflictRename
	}
}

// ParseMode 严格解析同步模式（P2-1）：空串视为默认 mirror；未知值返回错误，不做隐式回退。
func ParseMode(s string) (Mode, error) {
	if s == "" {
		return ModeMirror, nil
	}
	switch Mode(s) {
	case ModeMirror, ModeBackup:
		return Mode(s), nil
	default:
		return "", fmt.Errorf("未知同步模式 %q（合法值：mirror | backup）", s)
	}
}

// ParseConflict 严格解析冲突策略（P2-1）：空串视为默认 rename；未知值返回错误。
func ParseConflict(s string) (ConflictStrategy, error) {
	if s == "" {
		return ConflictRename, nil
	}
	switch ConflictStrategy(s) {
	case ConflictRename, ConflictKeep, ConflictNewer:
		return ConflictStrategy(s), nil
	default:
		return "", fmt.Errorf("未知冲突策略 %q（合法值：rename | keep | newer）", s)
	}
}

// ParseRole 严格解析节点角色（P2-1）：空串视为 peer（默认）；未知值返回错误。
func ParseRole(s string) (Role, error) {
	if s == "" {
		return RolePeer, nil
	}
	switch Role(s) {
	case RolePeer, RoleSource, RoleTarget:
		return Role(s), nil
	default:
		return "", fmt.Errorf("未知节点角色 %q（合法值：peer | source | target）", s)
	}
}

// ErrDangerous 表示存在危险操作需确认。
var ErrDangerous = fmt.Errorf("存在危险操作，需二次确认")
