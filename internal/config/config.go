// Package config 负责 MiaoSync 配置的加载、校验与保存。
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"gopkg.in/yaml.v3"

	"github.com/yuyeming0115/MiaoSync/internal/fs"
	msync "github.com/yuyeming0115/MiaoSync/internal/sync"
)

// Config 对应 miaosync.yaml。
type Config struct {
	Nodes    []NodeCfg              `yaml:"nodes" json:"nodes"`
	Mode     msync.Mode             `yaml:"mode" json:"mode"`
	Ignore   []string               `yaml:"ignore" json:"ignore"`
	Conflict msync.ConflictStrategy `yaml:"conflict" json:"conflict"`
}

// NodeCfg 单个端点的配置（同一端点可带多平台路径）。
type NodeCfg struct {
	Name    string `yaml:"name" json:"name"`
	WinPath string `yaml:"win-path" json:"win-path"`
	MacPath string `yaml:"mac-path" json:"mac-path"`
	Role    string `yaml:"role" json:"role"` // "peer" | "source" | "target"
}

// ActivePath 按当前运行平台返回该端点的实际路径。
func (n NodeCfg) ActivePath() string {
	if runtime.GOOS == "windows" {
		if n.WinPath != "" {
			return n.WinPath
		}
	}
	if runtime.GOOS != "windows" {
		if n.MacPath != "" {
			return n.MacPath
		}
	}
	// 兜底：返回任一已配置路径
	if n.WinPath != "" {
		return n.WinPath
	}
	return n.MacPath
}

// ToNode 转换为运行期 Node（解析实际路径）。
func (n NodeCfg) ToNode() (msync.Node, error) {
	p := n.ActivePath()
	if p == "" {
		return msync.Node{}, fmt.Errorf("节点 %q 在当前平台(%s)无可用路径", n.Name, runtime.GOOS)
	}
	return msync.Node{Name: n.Name, Path: p, Role: n.Role}, nil
}

// Validate 校验配置（P2-1：严格校验，未知值报错不回退；P1-3：路径重复/嵌套/根目录校验）。
func (c *Config) Validate() error {
	if len(c.Nodes) < 2 {
		return fmt.Errorf("至少需要 2 个节点才能同步，当前 %d 个", len(c.Nodes))
	}
	// P2-1：模式与冲突策略严格解析（空串允许，视为默认）
	mode, err := msync.ParseMode(string(c.Mode))
	if err != nil {
		return err
	}
	c.Mode = mode
	conflict, err := msync.ParseConflict(string(c.Conflict))
	if err != nil {
		return err
	}
	c.Conflict = conflict
	if len(c.Ignore) == 0 {
		c.Ignore = DefaultIgnore()
	}

	seen := map[string]bool{}
	type canonNode struct {
		name string
		path string
	}
	var canonNodes []canonNode
	for _, n := range c.Nodes {
		if n.Name == "" {
			return fmt.Errorf("存在未命名节点")
		}
		if seen[n.Name] {
			return fmt.Errorf("节点名重复: %q", n.Name)
		}
		seen[n.Name] = true
		// P2-1：严格校验角色
		if _, err := msync.ParseRole(n.Role); err != nil {
			return fmt.Errorf("节点 %q: %w", n.Name, err)
		}
		active := n.ActivePath()
		if active == "" {
			return fmt.Errorf("节点 %q 在平台 %s 无可用路径", n.Name, runtime.GOOS)
		}
		// P1-3：规范化路径并检查系统根
		cp := fs.CanonicalPath(active)
		if fs.IsSystemRoot(cp) {
			return fmt.Errorf("节点 %q 路径 %q 为系统根/高风险目录，拒绝作为同步根", n.Name, active)
		}
		canonNodes = append(canonNodes, canonNode{name: n.Name, path: cp})
	}

	// P1-3：拒绝相同路径或祖先/后代嵌套关系（嵌套会造成递归复制与目录膨胀）
	for i := 0; i < len(canonNodes); i++ {
		for j := i + 1; j < len(canonNodes); j++ {
			a, b := canonNodes[i], canonNodes[j]
			if a.path == b.path {
				return fmt.Errorf("节点 %q 与 %q 解析到同一目录: %s", a.name, b.name, a.path)
			}
			if fs.IsAncestor(a.path, b.path) {
				return fmt.Errorf("节点 %q(%s) 是节点 %q(%s) 的祖先目录，嵌套同步会造成递归复制", a.name, a.path, b.name, b.path)
			}
			if fs.IsAncestor(b.path, a.path) {
				return fmt.Errorf("节点 %q(%s) 是节点 %q(%s) 的祖先目录，嵌套同步会造成递归复制", b.name, b.path, a.name, a.path)
			}
		}
	}

	// P1-1：模式与角色一致性预检（拓扑合法性）
	var nodes []msync.Node
	for _, n := range c.Nodes {
		nd, err := n.ToNode()
		if err != nil {
			return err
		}
		nodes = append(nodes, nd)
	}
	if _, err := msync.Pairs(nodes, c.Mode); err != nil {
		return err
	}
	return nil
}

// Load 读取并解析配置文件。P2-1：加载后调用严格 Validate，非法值直接报错而非静默回退。
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var c Config
	if err := yaml.Unmarshal(data, &c); err != nil {
		return nil, err
	}
	applyDefaults(&c)
	if err := c.Validate(); err != nil {
		return nil, fmt.Errorf("配置校验失败: %w", err)
	}
	return &c, nil
}

// LoadLenient 尽力加载配置：yaml 可解析则用解析结果（跳过严格校验），并返回
// 人类可读的问题描述；文件彻底无法解析时退回内置默认配置。
// 用于启动降级——配置有问题也必须能打开界面让用户修复，而不是弹错退出。
func LoadLenient(path string) (*Config, string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return fallbackDefault(), ""
	}
	var c Config
	if err := yaml.Unmarshal(data, &c); err != nil {
		return fallbackDefault(), fmt.Sprintf("配置文件解析失败: %v（已载入内置默认配置）", err)
	}
	applyDefaults(&c)
	if err := c.Validate(); err != nil {
		return &c, err.Error()
	}
	return &c, ""
}

// fallbackDefault 启动降级用的最小默认配置（无节点，等用户在界面里补全）。
func fallbackDefault() *Config {
	return &Config{
		Mode:     msync.ModeMirror,
		Conflict: msync.ConflictRename,
		Ignore:   DefaultIgnore(),
	}
}

// Save 将配置写回文件（P3：原子写并保留 .bak）。
func Save(path string, c *Config) error {
	data, err := yaml.Marshal(c)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	// 原子写：先写临时文件再 rename；rename 成功后保留 .bak
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	// 备份旧配置（若存在）
	if old, rerr := os.ReadFile(path); rerr == nil {
		_ = os.WriteFile(path+".bak", old, 0o644)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func applyDefaults(c *Config) {
	if c.Mode == "" {
		c.Mode = msync.ModeMirror
	}
	if c.Conflict == "" {
		c.Conflict = msync.ConflictRename
	}
	if len(c.Ignore) == 0 {
		c.Ignore = DefaultIgnore()
	}
}

// DefaultIgnore 返回默认忽略规则。
// P1-6：包含 *.conflict-* 以避免冲突副本在多节点间继续传播。
func DefaultIgnore() []string {
	return []string{".DS_Store", "Thumbs.db", ".git", ".miaosync", "*.conflict-*"}
}
