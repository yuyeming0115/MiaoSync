// Package sync 实现三端同步引擎（骨架）。
//
// 当前仅定义核心类型与接口，具体算法见 docs/design.md。
package sync

import "fmt"

// Node 表示一个同步端点（Win 电脑 / Mac 电脑 / 移动硬盘）。
type Node struct {
	Name string // 端点名称，如 "win-pc"
	Path string // 根目录绝对路径
	Role string // "peer" | "source" | "target"
}

// FileState 记录单个文件的快照状态。
type FileState struct {
	Path  string // 相对根目录的路径，统一用 "/"
	Size  int64
	Mtime int64 // 修改时间（纳秒）
	Hash  string
}

// Action 表示一个待执行的同步动作。
type Action struct {
	Kind string // "copy" | "delete" | "rename-conflict"
	From string
	To   string
}

// Engine 同步引擎接口。
type Engine interface {
	Scan(n Node) (map[string]FileState, error)
	Plan(src, dst Node) ([]Action, error)
	Apply(actions []Action) error
}

// PlaceholderEngine 占位实现，便于骨架编译与测试路由。
type PlaceholderEngine struct{}

func (PlaceholderEngine) Scan(n Node) (map[string]FileState, error) {
	return nil, fmt.Errorf("scan not implemented for node %q", n.Name)
}

func (PlaceholderEngine) Plan(src, dst Node) ([]Action, error) {
	return nil, fmt.Errorf("plan not implemented: %s -> %s", src.Name, dst.Name)
}

func (PlaceholderEngine) Apply(actions []Action) error {
	return fmt.Errorf("apply not implemented (%d actions)", len(actions))
}
