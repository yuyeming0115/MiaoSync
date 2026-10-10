package config

import (
	"path/filepath"
	"testing"

	msync "github.com/yuyeming0115/MiaoSync/internal/sync"
)

// TestRejectSameAndNestedRoots 验证 P1-3：
// 配置校验应拒绝相同路径和祖先/后代嵌套关系。
func TestRejectSameAndNestedRoots(t *testing.T) {
	dir1 := t.TempDir()
	dir2 := t.TempDir()
	nestedDir := filepath.Join(dir1, "sub")

	tests := []struct {
		name    string
		nodes   []NodeCfg
		wantErr bool
	}{
		{
			name: "相同路径",
			nodes: []NodeCfg{
				{Name: "A", WinPath: dir1, Role: "peer"},
				{Name: "B", WinPath: dir1, Role: "peer"},
			},
			wantErr: true,
		},
		{
			name: "嵌套路径",
			nodes: []NodeCfg{
				{Name: "A", WinPath: dir1, Role: "peer"},
				{Name: "B", WinPath: nestedDir, Role: "peer"},
			},
			wantErr: true,
		},
		{
			name: "正常不同路径",
			nodes: []NodeCfg{
				{Name: "A", WinPath: dir1, Role: "peer"},
				{Name: "B", WinPath: dir2, Role: "peer"},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &Config{
				Nodes:    tt.nodes,
				Mode:     msync.ModeMirror,
				Conflict: msync.ConflictRename,
			}
			err := c.Validate()
			if tt.wantErr && err == nil {
				t.Error("期望校验失败，但通过了")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("期望校验通过，但失败: %v", err)
			}
		})
	}
}

// TestRejectUnknownMode 验证 P2-1：
// 未知同步模式应报错，而非静默回退为 mirror。
func TestRejectUnknownMode(t *testing.T) {
	dir1 := t.TempDir()
	dir2 := t.TempDir()
	c := &Config{
		Nodes: []NodeCfg{
			{Name: "A", WinPath: dir1, Role: "peer"},
			{Name: "B", WinPath: dir2, Role: "peer"},
		},
		Mode:     "invalid-mode",
		Conflict: msync.ConflictRename,
	}
	if err := c.Validate(); err == nil {
		t.Error("未知模式应报错")
	}
}

// TestRejectUnknownConflict 验证 P2-1：
// 未知冲突策略应报错。
func TestRejectUnknownConflict(t *testing.T) {
	dir1 := t.TempDir()
	dir2 := t.TempDir()
	c := &Config{
		Nodes: []NodeCfg{
			{Name: "A", WinPath: dir1, Role: "peer"},
			{Name: "B", WinPath: dir2, Role: "peer"},
		},
		Mode:     msync.ModeMirror,
		Conflict: "invalid-conflict",
	}
	if err := c.Validate(); err == nil {
		t.Error("未知冲突策略应报错")
	}
}
