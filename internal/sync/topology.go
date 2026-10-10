package sync

import (
	"fmt"
	"sort"
)

// NodePair 一个有向同步对：Src → Dst。
type NodePair struct {
	Src Node
	Dst Node
}

// Pairs 根据模式与角色生成有向同步对（P1-1）：
//   - mirror：所有节点必须为 peer（或空），生成无序两两对（双向语义由 Plan 内部对称决策保证）
//   - backup：必须有且仅有一个 source、一个或多个 target，只生成 source → target
//
// 非法角色组合返回错误，确保执行方向由角色决定而非节点顺序。
func Pairs(nodes []Node, mode Mode) ([]NodePair, error) {
	if len(nodes) < 2 {
		return nil, fmt.Errorf("至少需要 2 个节点才能同步，当前 %d 个", len(nodes))
	}

	// 严格校验每个节点角色
	seen := map[string]bool{}
	for i, n := range nodes {
		r, err := ParseRole(n.Role)
		if err != nil {
			return nil, fmt.Errorf("节点 %q: %w", n.Name, err)
		}
		// 角色规范化写回（空串 → peer）
		nodes[i].Role = string(r)
		if seen[n.Name] {
			return nil, fmt.Errorf("节点名重复: %q", n.Name)
		}
		seen[n.Name] = true
	}

	switch mode {
	case ModeMirror:
		// mirror 只允许 peer 角色
		for _, n := range nodes {
			if Role(n.Role) != RolePeer {
				return nil, fmt.Errorf("mirror 模式要求所有节点为 peer，节点 %q 角色为 %q", n.Name, n.Role)
			}
		}
		// 生成无序两两对（i < j），方向仅用于 Src/Dst 命名，Plan 内部按对称决策处理
		var pairs []NodePair
		for i := 0; i < len(nodes); i++ {
			for j := i + 1; j < len(nodes); j++ {
				pairs = append(pairs, NodePair{Src: nodes[i], Dst: nodes[j]})
			}
		}
		return pairs, nil

	case ModeBackup:
		// backup 必须恰好一个 source、一个或多个 target，其余不允许（不混用 peer）
		var sources, targets []Node
		for _, n := range nodes {
			switch Role(n.Role) {
			case RoleSource:
				sources = append(sources, n)
			case RoleTarget:
				targets = append(targets, n)
			default:
				return nil, fmt.Errorf("backup 模式不允许 peer 角色，节点 %q 应为 source 或 target", n.Name)
			}
		}
		if len(sources) != 1 {
			return nil, fmt.Errorf("backup 模式要求恰好 1 个 source，当前 %d 个", len(sources))
		}
		if len(targets) < 1 {
			return nil, fmt.Errorf("backup 模式要求至少 1 个 target，当前 %d 个", len(targets))
		}
		// 按名称排序保证顺序无关（P1-1：结果不受节点选择顺序影响）
		sort.Slice(targets, func(i, j int) bool { return targets[i].Name < targets[j].Name })
		var pairs []NodePair
		for _, t := range targets {
			pairs = append(pairs, NodePair{Src: sources[0], Dst: t})
		}
		return pairs, nil

	default:
		return nil, fmt.Errorf("未知同步模式 %q", mode)
	}
}
