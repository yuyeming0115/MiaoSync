package sync

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"time"
)

// PlanTTL 不可变计划的有效期（P1-2）。过期后执行需重新预览。
const PlanTTL = 10 * time.Minute

// Plan 不可变同步计划（P1-2）：预览时生成并绑定当前文件状态指纹，
// 执行时校验指纹是否一致，消除预览与执行之间的 TOCTOU 竞态。
type Plan struct {
	ID          string      // 唯一标识（随机生成）
	Fingerprint string      // 所有节点文件状态的指纹（SHA-256）
	Actions     []Action    // 预计算的动作集合，执行时原样使用
	Options     PlanOptions // 模式与冲突策略（不可变）
	Nodes       []Node      // 参与节点（canonical）
	CreatedAt   time.Time   // 创建时间
	ExpiresAt   time.Time   // 过期时间
}

// newPlanID 生成随机计划 ID。
func newPlanID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return "plan-" + hex.EncodeToString(b)
}

// computeFingerprint 根据各节点当前文件状态计算确定性指纹。
// 任何文件的增删改、mtime 变化都会使指纹不同，从而让计划失效。
func computeFingerprint(nodes []Node, states map[string]map[string]*FileState) string {
	h := sha256.New()
	// 按节点名排序保证确定性
	names := make([]string, 0, len(nodes))
	for _, n := range nodes {
		names = append(names, n.Name)
	}
	sort.Strings(names)
	for _, name := range names {
		// 写入节点身份（名称 + canonical 路径）
		var nodePath string
		for _, n := range nodes {
			if n.Name == name {
				nodePath = n.Path
			}
		}
		fmt.Fprintf(h, "node:%s|%s\n", name, nodePath)
		st, ok := states[name]
		if !ok {
			continue
		}
		// 按 path 排序文件状态
		keys := make([]string, 0, len(st))
		for k := range st {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			f := st[k]
			fmt.Fprintf(h, "  %s|%d|%d|%s\n", f.Path, f.Size, f.Mtime, f.Hash)
		}
	}
	return hex.EncodeToString(h.Sum(nil))
}

// Expired 报告计划是否已过期。
func (p *Plan) Expired() bool {
	return time.Now().After(p.ExpiresAt)
}

// verifyFingerprint 重新扫描所有节点并校验指纹是否与预览时一致（P1-2）。
// 指纹不一致说明文件系统在预览与执行之间发生了变化，计划已失效。
// 返回的 states 可复用（P1-7：避免重复扫描）。
func (p *Plan) verifyFingerprint(ignore []string) (map[string]map[string]*FileState, error) {
	states := map[string]map[string]*FileState{}
	for _, n := range p.Nodes {
		cur, err := New().Scan(n, ignore)
		if err != nil {
			return nil, fmt.Errorf("校验计划时扫描节点 %q 失败: %w", n.Name, err)
		}
		states[n.Name] = cur
	}
	current := computeFingerprint(p.Nodes, states)
	if current != p.Fingerprint {
		return nil, ErrPlanStale
	}
	return states, nil
}

// ErrPlanStale 计划已失效（文件状态变化或过期），需重新预览。
var ErrPlanStale = fmt.Errorf("计划已失效：文件状态在预览后发生了变化，请重新预览")

// ErrPlanExpired 计划已过期。
var ErrPlanExpired = fmt.Errorf("计划已过期（超过 %s），请重新预览", PlanTTL)

// ErrPlanNotFound 计划不存在（可能服务已重启）。
var ErrPlanNotFound = fmt.Errorf("计划不存在或已失效，请重新预览")
