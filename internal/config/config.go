// Package config 加载 MiaoSync 配置（骨架）。
//
// 配置格式与字段说明见 config/maiosync.example.yaml 与 docs/design.md 第 7 节。
package config

// Config 对应 miaosync.yaml。
type Config struct {
	Nodes    []NodeCfg `yaml:"nodes"`
	Mode     string    `yaml:"mode"`     // "mirror" | "backup"
	Ignore   []string  `yaml:"ignore"`   // 忽略模式（glob）
	Conflict string    `yaml:"conflict"` // "rename" | "keep" | "newer"
}

// NodeCfg 单个端点的配置。同一端点可带多平台路径。
type NodeCfg struct {
	Name    string `yaml:"name"`
	WinPath string `yaml:"win-path"`
	MacPath string `yaml:"mac-path"`
	Role    string `yaml:"role"` // "peer" | "source" | "target"
}

// Load 从路径读取并解析配置。
// TODO: 读取文件 + yaml.Unmarshal + 基础校验（至少 2 个 Node、路径非空）。
func Load(path string) (*Config, error) {
	return nil, nil // 骨架占位
}

// ActivePath 根据当前运行平台返回该端点的实际路径。
func (n NodeCfg) ActivePath() string {
	// TODO: 依据 runtime.GOOS 返回 WinPath 或 MacPath。
	return ""
}
