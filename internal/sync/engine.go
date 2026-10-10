package sync

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/yuyeming0115/MiaoSync/internal/fs"
)

// Engine 同步引擎。
type Engine struct{}

// New 创建引擎实例。
func New() *Engine { return &Engine{} }

// Scan 扫描节点目录，返回当前 FileState 表（key = 小写比较键）。
func (e *Engine) Scan(n Node, ignore []string) (map[string]*FileState, error) {
	root := n.Path
	info, err := os.Stat(root)
	if err != nil {
		return nil, fmt.Errorf("节点 %q 路径不可访问: %w", n.Name, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("节点 %q 路径不是目录: %s", n.Name, root)
	}

	result := map[string]*FileState{}
	// P1-5：检测大小写折叠冲突（区分大小写的 FS 上 A.txt 与 a.txt 落到同一比较键）
	var collisions []string
	walkErr := filepath.WalkDir(root, func(p string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		// P1-4：显式跳过符号链接，避免读取同步根目录之外的数据
		if d.Type()&os.ModeSymlink != 0 {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		rel = fs.NormalizePath(rel)
		// 跳过快照目录
		if rel == SnapshotDir || hasPrefixDir(rel, SnapshotDir) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			if rel != "." && fs.ShouldIgnore(rel, ignore) {
				return filepath.SkipDir
			}
			return nil
		}
		if fs.ShouldIgnore(rel, ignore) {
			return nil
		}
		fi, err := d.Info()
		if err != nil {
			return err
		}
		key := fs.CompareKey(rel)
		// P1-5：同一比较键已存在不同相对路径 → 大小写冲突，记录并阻止执行
		if prev, ok := result[key]; ok && prev.Path != rel {
			collisions = append(collisions, fmt.Sprintf("%q 与 %q 折叠为同一比较键", prev.Path, rel))
		}
		hash, err := HashFile(p)
		if err != nil {
			return fmt.Errorf("哈希失败 %s: %w", p, err)
		}
		result[key] = &FileState{
			Path:  rel,
			Size:  fi.Size(),
			Mtime: fi.ModTime().UnixNano(),
			Hash:  hash,
		}
		return nil
	})
	if walkErr != nil {
		return nil, walkErr
	}
	if len(collisions) > 0 {
		return nil, fmt.Errorf("节点 %q 存在大小写冲突路径，拒绝执行（同步到大小写不敏感的 FS 会丢失数据）：%s",
			n.Name, strings.Join(collisions, "; "))
	}
	return result, nil
}

// Plan 比对两个节点的当前状态与上次快照，生成动作列表。
// 对每个相对路径只生成至多一个决策（P0-1：消除“补齐复制 + 同步删除”并存的问题）。
func (e *Engine) Plan(src, dst Node, srcCur, dstCur map[string]*FileState,
	srcSnap, dstSnap *Snapshot, opt PlanOptions) ([]Action, error) {

	actions := []Action{}
	// 汇总所有出现过的 key（当前状态 + 历史快照），确保已删除文件也能被删除传播覆盖
	keys := map[string]bool{}
	for k := range srcCur {
		keys[k] = true
	}
	for k := range dstCur {
		keys[k] = true
	}
	for k := range srcSnap.Files {
		keys[k] = true
	}
	for k := range dstSnap.Files {
		keys[k] = true
	}

	for k := range keys {
		sNow := srcCur[k]
		dNow := dstCur[k]
		sOld := srcSnap.Files[k]
		dOld := dstSnap.Files[k]
		if act, ok := decidePair(k, sNow, dNow, sOld, dOld, src, dst, opt); ok {
			actions = append(actions, act)
		}
	}
	return actions, nil
}

// decidePair 对单个相对路径生成至多一个同步动作（单路径单决策）。
// 模式分流：backup 由 source 单向驱动；mirror 双向对称。
func decidePair(k string, sNow, dNow *FileState, sOld, dOld *FileState,
	src, dst Node, opt PlanOptions) (Action, bool) {

	if opt.Mode == ModeBackup {
		return decideBackup(k, sNow, dNow, sOld, src, dst, opt)
	}
	return decideMirror(k, sNow, dNow, sOld, dOld, src, dst, opt)
}

// decideBackup：source 权威，target 单向跟随。
//   - source 有：target 缺失则复制；内容不同则按冲突策略处理；相同则无操作
//   - source 无：source 曾有则传播删除到 target；source 从未有过则保留 target
func decideBackup(k string, sNow, dNow, sOld *FileState, src, dst Node, opt PlanOptions) (Action, bool) {
	switch {
	case sNow != nil && dNow == nil:
		return makeCopy(src, dst, sNow, "源端存在，备份到目标端"), true
	case sNow != nil && dNow != nil:
		if sNow.Hash == dNow.Hash {
			return Action{}, false
		}
		return makeConflict(k, sNow, dNow, src, dst, opt.Conflict), true
	case sNow == nil && dNow != nil:
		if sOld != nil {
			return Action{
				Kind:    ActionDelete,
				RelPath: k,
				To:      filepath.Join(dst.Path, dNow.Path),
				Size:    dNow.Size,
				Reason:  "源端已删除，目标端同步删除",
			}, true
		}
		// source 从未有过该文件 → 保留 target，不反向复制
		return Action{}, false
	default:
		return Action{}, false
	}
}

// decideMirror：双向对称决策。
//
// 对“一端存在、另一端缺失”的情况，依据历史快照区分：
//   - 另一端从未有过 → 当作新文件补齐（复制）
//   - 另一端曾有且现存端未修改 → 传播删除（删除现存端）
//   - 另一端曾有但现存端已修改 → 修改/删除冲突，mirror 保留修改（复制回已删除端）
//
// 关键不变量：同一路径在一次 Plan 中只产生上述之一，绝不同时生成复制与删除。
func decideMirror(k string, sNow, dNow *FileState, sOld, dOld *FileState,
	src, dst Node, opt PlanOptions) (Action, bool) {

	switch {
	case sNow != nil && dNow != nil:
		if sNow.Hash == dNow.Hash {
			return Action{}, false
		}
		return makeConflict(k, sNow, dNow, src, dst, opt.Conflict), true

	case sNow != nil && dNow == nil:
		// 仅 src 现存
		if dOld == nil {
			// dst 从未有过 → 新文件 src→dst
			return makeCopy(src, dst, sNow, "源端独有，补齐到目标端"), true
		}
		// dst 曾有，现已删除
		if sOld != nil && sNow.Hash == sOld.Hash {
			// src 未修改 → 传播删除到 src
			return Action{
				Kind:    ActionDelete,
				RelPath: k,
				To:      filepath.Join(src.Path, sNow.Path),
				Size:    sNow.Size,
				Reason:  "目标端已删除且源端未修改，源端同步删除",
			}, true
		}
		// src 已修改而 dst 删除 → 保留修改，恢复 dst
		return makeCopy(src, dst, sNow, "源端已修改而目标端删除，恢复目标端以保留改动"), true

	case sNow == nil && dNow != nil:
		// 仅 dst 现存（对称）
		if sOld == nil {
			return makeCopy(dst, src, dNow, "目标端独有，补齐到源端"), true
		}
		if dOld != nil && dNow.Hash == dOld.Hash {
			return Action{
				Kind:    ActionDelete,
				RelPath: k,
				To:      filepath.Join(dst.Path, dNow.Path),
				Size:    dNow.Size,
				Reason:  "源端已删除且目标端未修改，目标端同步删除",
			}, true
		}
		return makeCopy(dst, src, dNow, "目标端已修改而源端删除，恢复源端以保留改动"), true

	default:
		// 两端均无（可能都已删除）→ 无操作
		return Action{}, false
	}
}

// planPair 扫描一对节点并生成计划，同时返回两侧当前状态（供危险统计）。
func (e *Engine) planPair(src, dst Node, ignore []string, mode Mode, conflict ConflictStrategy) (
	[]Action, map[string]*FileState, map[string]*FileState, error) {

	srcCur, err := e.Scan(src, ignore)
	if err != nil {
		return nil, nil, nil, err
	}
	dstCur, err := e.Scan(dst, ignore)
	if err != nil {
		return nil, nil, nil, err
	}
	srcSnap, err := LoadSnapshot(src.Path)
	if err != nil {
		return nil, nil, nil, err
	}
	dstSnap, err := LoadSnapshot(dst.Path)
	if err != nil {
		return nil, nil, nil, err
	}
	acts, err := e.Plan(src, dst, srcCur, dstCur, srcSnap, dstSnap, PlanOptions{Mode: mode, Conflict: conflict})
	return acts, srcCur, dstCur, err
}

// PlanPairs 对给定节点（至少 2 个）按拓扑生成同步对并合并返回动作列表（用于预览）。
// P1-1：backup 模式只生成 source → target，mirror 生成无序两两对，方向由角色决定。
func (e *Engine) PlanPairs(nodes []Node, ignore []string, mode Mode, conflict ConflictStrategy) ([]Action, error) {
	pairs, err := Pairs(nodes, mode)
	if err != nil {
		return nil, err
	}
	var all []Action
	for _, p := range pairs {
		acts, _, _, err := e.planPair(p.Src, p.Dst, ignore, mode, conflict)
		if err != nil {
			return nil, err
		}
		all = append(all, acts...)
	}
	sort.SliceStable(all, func(i, j int) bool {
		if all[i].Kind != all[j].Kind {
			return all[i].Kind < all[j].Kind
		}
		return all[i].RelPath < all[j].RelPath
	})
	return all, nil
}

// Run 在节点之间按拓扑执行同步。dryRun 时只统计不执行；危险删除超阈值且未确认则中止并返回 RequiresConfirm。
// P0-2：任一动作失败时，相关节点快照不会被提交，避免把失败状态当作成功基线。
// P1-1：执行方向由角色决定（backup: source→target；mirror: 无序两两对）。
func (e *Engine) Run(nodes []Node, ignore []string, opt SyncOptions, progress ProgressFunc) (*SyncSummary, error) {
	summary := &SyncSummary{}
	pairs, err := Pairs(nodes, opt.Mode)
	if err != nil {
		return nil, err
	}
	totalPairs := len(pairs)
	// 记录因失败而不应提交快照的节点路径
	failedNodes := map[string]bool{}

	for _, p := range pairs {
		src, dst := p.Src, p.Dst
		if progress != nil {
			progress("plan", 0, totalPairs, fmt.Sprintf("规划 %s ↔ %s", src.Name, dst.Name))
		}
		acts, srcCur, dstCur, err := e.planPair(src, dst, ignore, opt.Mode, opt.Conflict)
		if err != nil {
			return nil, err
		}

		if opt.DryRun {
			accumulate(summary, acts)
			continue
		}

		// 危险操作保护：删除数超过阈值需二次确认
		deleteCount := 0
		for _, a := range acts {
			if a.Kind == ActionDelete {
				deleteCount++
			}
		}
		totalFiles := len(srcCur) + len(dstCur)
		// 触发条件（二者满足其一即危险）：
		//   1) 绝对阈值：删除数 > 50
		//   2) 相对阈值：删除数 > 当前文件总数的 30%（小目录保护）
		dangerous := false
		threshold := 0
		if deleteCount > 50 {
			dangerous = true
			threshold = 50
		} else if totalFiles > 0 && deleteCount > int(0.3*float64(totalFiles)) {
			dangerous = true
			threshold = int(0.3 * float64(totalFiles))
		}
		if dangerous && !opt.ConfirmDangerous {
			summary.RequiresConfirm = true
			summary.DangerousInfo = fmt.Sprintf(
				"本次将删除 %d 个文件（安全阈值 %d），属危险操作。请确认无误后再次执行。",
				deleteCount, threshold)
			return summary, ErrDangerous
		}

		pairSum, items, err := e.Apply(acts, opt.Conflict, true, progress)
		if err != nil {
			return nil, err
		}
		summary.TotalActions += pairSum.TotalActions
		summary.Copied += pairSum.Copied
		summary.Deleted += pairSum.Deleted
		summary.Conflicts += pairSum.Conflicts
		summary.Skipped += pairSum.Skipped
		summary.Errors += pairSum.Errors
		pairName := src.Name + " ↔ " + dst.Name
		for k := range items {
			items[k].Pair = pairName
		}
		summary.Transferred = append(summary.Transferred, items...)

		// P0-2：仅当本 pair 无错误时才提交两端快照；有错误则标记节点为失败，
		// 后续涉及该节点的 pair 也不再提交快照（避免把半截状态固化成基线）。
		if pairSum.Errors == 0 && !failedNodes[src.Path] && !failedNodes[dst.Path] {
			if err := savePairSnapshot(src, dst, ignore); err != nil {
				return nil, err
			}
		} else {
			failedNodes[src.Path] = true
			failedNodes[dst.Path] = true
		}
	}

	if summary.Errors == 0 {
		summary.Message = "同步完成"
	} else {
		summary.Message = fmt.Sprintf("同步完成，但有 %d 个错误（受影响节点未更新快照）", summary.Errors)
	}
	return summary, nil
}

// Apply 执行动作列表。confirmDangerous 为 true 时允许删除。返回汇总与逐文件明细。
func (e *Engine) Apply(actions []Action, strategy ConflictStrategy, confirmDangerous bool, progress ProgressFunc) (*SyncSummary, []TransferItem, error) {
	sum := &SyncSummary{TotalActions: len(actions)}
	var items []TransferItem

	var deletes []Action
	for _, a := range actions {
		if a.Kind == ActionDelete {
			deletes = append(deletes, a)
		}
	}
	if len(deletes) > 0 && !confirmDangerous {
		sum.RequiresConfirm = true
		sum.DangerousInfo = fmt.Sprintf("本次将删除 %d 个文件，属危险操作。请确认无误后再次执行。", len(deletes))
		return sum, nil, ErrDangerous
	}

	total := len(actions)
	for idx, a := range actions {
		msg := a.Reason
		switch a.Kind {
		case ActionCopy:
			if err := copyFile(a.From, a.To); err != nil {
				sum.Errors++
				items = append(items, TransferItem{Kind: "copy", From: a.From, To: a.To, Size: a.Size, Status: "error"})
				msg = "复制失败: " + err.Error()
			} else {
				sum.Copied++
				sum.BytesCopied += a.Size
				items = append(items, TransferItem{Kind: "copy", From: a.From, To: a.To, Size: a.Size, Status: "ok"})
			}
		case ActionDelete:
			if err := os.Remove(a.To); err != nil {
				sum.Errors++
				items = append(items, TransferItem{Kind: "delete", From: "", To: a.To, Size: a.Size, Status: "error"})
				msg = "删除失败: " + err.Error()
			} else {
				sum.Deleted++
				items = append(items, TransferItem{Kind: "delete", From: "", To: a.To, Size: a.Size, Status: "ok"})
			}
		case ActionRenameConflict:
			if err := handleConflict(a, strategy); err != nil {
				sum.Errors++
				items = append(items, TransferItem{Kind: "conflict", From: a.From, To: a.To, Size: a.Size, Status: "error"})
				msg = "冲突处理失败: " + err.Error()
			} else {
				sum.Conflicts++
				items = append(items, TransferItem{Kind: "conflict", From: a.From, To: a.To, Size: a.Size, Status: "ok"})
			}
		case ActionSkip:
			sum.Skipped++
			items = append(items, TransferItem{Kind: "skip", From: a.From, To: a.To, Size: a.Size, Status: "skip"})
		}
		if progress != nil {
			progress("apply", idx+1, total, msg)
		}
	}
	return sum, items, nil
}

// accumulate 将一组动作统计进 summary（用于 dry-run）。
func accumulate(sum *SyncSummary, acts []Action) {
	sum.TotalActions += len(acts)
	for _, a := range acts {
		switch a.Kind {
		case ActionCopy:
			sum.Copied++
		case ActionDelete:
			sum.Deleted++
		case ActionRenameConflict:
			sum.Conflicts++
		case ActionSkip:
			sum.Skipped++
		}
	}
}

// copyFile 原子复制文件（P0-3 + P2-3）：
//  1. 在目标同目录创建临时文件并流式复制
//  2. fsync 落盘 + 大小校验
//  3. hash 校验：临时文件 SHA-256 必须与源文件一致（P2-3）
//  4. 保留源文件权限与 mtime
//  5. 原子 rename 替换目标（失败时清理临时文件并保留原目标）
//
// 任何失败都不会破坏已存在的目标文件。
func copyFile(from, to string) error {
	dir := filepath.Dir(to)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	in, err := os.Open(from)
	if err != nil {
		return err
	}
	defer in.Close()

	srcFi, statErr := os.Stat(from)
	if statErr != nil {
		return statErr
	}

	// 临时文件与目标同目录，保证 rename 原子
	tmp, err := os.CreateTemp(dir, ".miaosync-tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	cleanup := func(reason error) error {
		tmp.Close()
		_ = os.Remove(tmpName)
		return reason
	}

	if _, err := io.Copy(tmp, in); err != nil {
		return cleanup(err)
	}
	// 保留源文件权限
	if err := tmp.Chmod(srcFi.Mode()); err != nil {
		return cleanup(err)
	}
	// fsync 确保数据落盘
	if err := tmp.Sync(); err != nil {
		return cleanup(err)
	}
	if err := tmp.Close(); err != nil {
		return cleanup(err)
	}
	// 大小校验
	dstFi, derr := os.Stat(tmpName)
	if derr != nil || dstFi.Size() != srcFi.Size() {
		return cleanup(fmt.Errorf("复制后大小校验失败: 期望 %d", srcFi.Size()))
	}
	// P2-3：hash 校验，确保复制内容完整无损
	srcHash, err := HashFile(from)
	if err != nil {
		return cleanup(fmt.Errorf("读取源文件 hash 失败: %w", err))
	}
	tmpHash, err := HashFile(tmpName)
	if err != nil {
		return cleanup(fmt.Errorf("读取临时文件 hash 失败: %w", err))
	}
	if srcHash != tmpHash {
		return cleanup(fmt.Errorf("复制后 hash 校验失败：源 %s 临时 %s", srcHash[:12], tmpHash[:12]))
	}
	// 原子替换目标（Go 的 os.Rename 在 Windows 上使用 MoveFileEx+REPLACE_EXISTING，在 Unix 上为 rename(2)，均可替换已有文件）
	if err := os.Rename(tmpName, to); err != nil {
		return cleanup(err)
	}
	// 保留 mtime（P2-3）
	_ = os.Chtimes(to, srcFi.ModTime(), srcFi.ModTime())
	return nil
}

// handleConflict 按策略处理冲突：From = 旧版本(待备份)绝对路径，To = 新版本(来自胜方端)绝对路径。
// P0-3：所有分支均保证失败时不丢失败方原件。
// P1-6：冲突备份统一归档到 <loserRoot>/.miaosync/conflicts/<relSubPath>/，不再散落在业务目录。
func handleConflict(a Action, strategy ConflictStrategy) error {
	loserPath := a.From
	winnerPath := a.To
	ts := time.Now().Format("20060102-150405")
	ext := filepath.Ext(loserPath)
	base := filepath.Base(loserPath)
	base = base[:len(base)-len(ext)]
	conflictName := fmt.Sprintf("%s.conflict-%s%s", base, ts, ext)

	// P1-6：归档路径 = <loserRoot>/.miaosync/conflicts/<loser 子目录>/
	loserRoot := a.ConflictRoot
	if loserRoot == "" {
		// 兜底：未携带根时退回 loser 同目录（兼容旧调用点）
		loserRoot = filepath.Dir(loserPath)
	}
	relSub := filepath.Dir(strings.TrimPrefix(filepath.ToSlash(loserPath), filepath.ToSlash(loserRoot)))
	relSub = filepath.FromSlash(relSub)
	conflictDir := filepath.Join(loserRoot, SnapshotDir, "conflicts", relSub)
	if err := os.MkdirAll(conflictDir, 0o755); err != nil {
		return err
	}
	conflictPath := filepath.Join(conflictDir, conflictName)

	switch strategy {
	case ConflictKeep:
		// 两端各自保留：旧版本归档为 .conflict 副本，不覆盖（单次 rename，原子）
		return os.Rename(loserPath, conflictPath)
	case ConflictNewer:
		// 原子覆盖败方：copyFile 内部用临时文件+rename，失败时败方原件保留
		return copyFile(winnerPath, loserPath)
	case ConflictRename:
		fallthrough
	default:
		// 旧版本归档备份 + 新版本原子写入：
		//   1. 先把 winner 复制到同目录临时文件（不触碰 loser）
		//   2. loser 重命名归档到 .miaosync/conflicts/（原子）
		//   3. 临时文件 rename 到 loser 原位置（原子）
		// 任一步失败都清理临时文件，且 loser 要么停在原位要么停在归档位置，内容不丢失
		tmp, err := copyToTemp(winnerPath, filepath.Dir(loserPath))
		if err != nil {
			return err
		}
		if err := os.Rename(loserPath, conflictPath); err != nil {
			_ = os.Remove(tmp)
			return err
		}
		if err := os.Rename(tmp, loserPath); err != nil {
			// 临时文件无法就位：尝试把归档还原以保留原始数据
			_ = os.Rename(conflictPath, loserPath)
			return err
		}
		// 保留 loser 原 mtime 到归档副本，便于排查
		return nil
	}
}

// copyToTemp 把 src 复制到 dir 下的临时文件，返回临时文件路径（不替换任何已有文件）。
func copyToTemp(src, dir string) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	in, err := os.Open(src)
	if err != nil {
		return "", err
	}
	defer in.Close()
	srcFi, statErr := os.Stat(src)
	if statErr != nil {
		return "", statErr
	}
	tmp, err := os.CreateTemp(dir, ".miaosync-conf-*")
	if err != nil {
		return "", err
	}
	tmpName := tmp.Name()
	if _, err := io.Copy(tmp, in); err != nil {
		tmp.Close()
		_ = os.Remove(tmpName)
		return "", err
	}
	if err := tmp.Chmod(srcFi.Mode()); err != nil {
		tmp.Close()
		_ = os.Remove(tmpName)
		return "", err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		_ = os.Remove(tmpName)
		return "", err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return "", err
	}
	return tmpName, nil
}

// savePairSnapshot 重新扫描并写回两端快照。
func savePairSnapshot(src, dst Node, ignore []string) error {
	for _, n := range []Node{src, dst} {
		cur, err := New().Scan(n, ignore)
		if err != nil {
			return err
		}
		snap := &Snapshot{Node: n.Name, Files: cur}
		if err := SaveSnapshot(n.Path, snap); err != nil {
			return err
		}
	}
	return nil
}

// makeCopy 构造复制动作。
func makeCopy(from, to Node, st *FileState, reason string) Action {
	return Action{
		Kind:    ActionCopy,
		RelPath: st.Path,
		From:    filepath.Join(from.Path, st.Path),
		To:      filepath.Join(to.Path, st.Path),
		Size:    st.Size,
		Reason:  reason,
	}
}

// makeConflict 构造冲突动作，按 mtime 决定较新一方为胜者。
func makeConflict(key string, sNow, dNow *FileState, src, dst Node, strategy ConflictStrategy) Action {
	winnerIsSrc := sNow.Mtime >= dNow.Mtime
	var winner, loser Node
	var winnerSt, loserSt *FileState
	if winnerIsSrc {
		winner, loser = src, dst
		winnerSt, loserSt = sNow, dNow
	} else {
		winner, loser = dst, src
		winnerSt, loserSt = dNow, sNow
	}
	return Action{
		Kind:         ActionRenameConflict,
		RelPath:      winnerSt.Path,
		From:         filepath.Join(loser.Path, loserSt.Path),
		To:           filepath.Join(winner.Path, winnerSt.Path),
		Size:         loserSt.Size,
		ConflictRoot: loser.Path,
		Reason: fmt.Sprintf("两端版本冲突（策略 %s）：保留 %q 较新版本，%q 旧版本归档到 .miaosync/conflicts/",
			strategy, winner.Name, loser.Name),
	}
}

// hasPrefixDir 判断 rel 是否位于 dir 目录内。
func hasPrefixDir(rel, dir string) bool {
	return rel == dir || (len(rel) > len(dir) && rel[:len(dir)] == dir && rel[len(dir)] == '/')
}

// ---------- P1-2：不可变计划（消除 TOCTOU）----------

// PlanWithID 扫描所有参与节点一次，基于当前状态与快照生成不可变计划。
// 返回的 Plan 带有唯一 ID 和文件状态指纹，执行时校验指纹即可消除预览/执行间的竞态。
// 同时也实现了 P1-7：每节点在一次预览中只扫描一次（缓存复用）。
func (e *Engine) PlanWithID(nodes []Node, ignore []string, mode Mode, conflict ConflictStrategy) (*Plan, error) {
	pairs, err := Pairs(nodes, mode)
	if err != nil {
		return nil, err
	}

	// 每节点只扫描一次（P1-7 扫描缓存）
	scanCache := map[string]map[string]*FileState{}
	scanNode := func(n Node) (map[string]*FileState, error) {
		if st, ok := scanCache[n.Name]; ok {
			return st, nil
		}
		cur, err := e.Scan(n, ignore)
		if err != nil {
			return nil, err
		}
		scanCache[n.Name] = cur
		return cur, nil
	}

	var all []Action
	for _, p := range pairs {
		src, dst := p.Src, p.Dst
		srcCur, err := scanNode(src)
		if err != nil {
			return nil, err
		}
		dstCur, err := scanNode(dst)
		if err != nil {
			return nil, err
		}
		srcSnap, err := LoadSnapshot(src.Path)
		if err != nil {
			return nil, err
		}
		dstSnap, err := LoadSnapshot(dst.Path)
		if err != nil {
			return nil, err
		}
		acts, err := e.Plan(src, dst, srcCur, dstCur, srcSnap, dstSnap, PlanOptions{Mode: mode, Conflict: conflict})
		if err != nil {
			return nil, err
		}
		all = append(all, acts...)
	}

	sort.SliceStable(all, func(i, j int) bool {
		if all[i].Kind != all[j].Kind {
			return all[i].Kind < all[j].Kind
		}
		return all[i].RelPath < all[j].RelPath
	})

	now := time.Now()
	plan := &Plan{
		ID:          newPlanID(),
		Fingerprint: computeFingerprint(nodes, scanCache),
		Actions:     all,
		Options:     PlanOptions{Mode: mode, Conflict: conflict},
		Nodes:       nodes,
		CreatedAt:   now,
		ExpiresAt:   now.Add(PlanTTL),
	}
	return plan, nil
}

// RunWithPlan 校验计划指纹后执行预计算的动作（P1-2）。
// 与 Run 的区别：
//   - 不重新规划，直接执行预览时生成的动作，消除 TOCTOU
//   - 指纹不匹配或过期时报错，要求重新预览
//   - 全局提交：所有动作成功才保存快照，任一失败则全部不提交（更符合事务语义）
//   - P1-7：指纹校验阶段的扫描结果复用于危险阈值统计，避免重复扫描
func (e *Engine) RunWithPlan(plan *Plan, ignore []string, opt SyncOptions, progress ProgressFunc) (*SyncSummary, error) {
	if plan.Expired() {
		return nil, ErrPlanExpired
	}
	// 校验文件状态指纹（重新扫描所有节点，P1-7：复用扫描结果）
	nodeStates, err := plan.verifyFingerprint(ignore)
	if err != nil {
		return nil, err
	}

	summary := &SyncSummary{TotalActions: len(plan.Actions)}

	if opt.DryRun {
		accumulate(summary, plan.Actions)
		return summary, nil
	}

	// 危险操作保护（全局统计，P1-7：复用校验阶段的扫描结果）
	deleteCount := 0
	totalFiles := 0
	for _, a := range plan.Actions {
		if a.Kind == ActionDelete {
			deleteCount++
		}
	}
	for _, n := range plan.Nodes {
		if cur, ok := nodeStates[n.Name]; ok {
			totalFiles += len(cur)
		}
	}
	dangerous := false
	threshold := 0
	if deleteCount > 50 {
		dangerous = true
		threshold = 50
	} else if totalFiles > 0 && deleteCount > int(0.3*float64(totalFiles)) {
		dangerous = true
		threshold = int(0.3 * float64(totalFiles))
	}
	if dangerous && !opt.ConfirmDangerous {
		summary.RequiresConfirm = true
		summary.DangerousInfo = fmt.Sprintf(
			"本次将删除 %d 个文件（安全阈值 %d），属危险操作。请确认无误后再次执行。",
			deleteCount, threshold)
		return summary, ErrDangerous
	}

	// 执行预计算的动作
	pairSum, items, err := e.Apply(plan.Actions, opt.Conflict, true, progress)
	if err != nil {
		return nil, err
	}
	summary.TotalActions = pairSum.TotalActions
	summary.Copied = pairSum.Copied
	summary.Deleted = pairSum.Deleted
	summary.Conflicts = pairSum.Conflicts
	summary.Skipped = pairSum.Skipped
	summary.Errors = pairSum.Errors
	summary.Transferred = items

	// P0-2：仅当无错误时才提交快照（全局提交语义）
	// 执行后需重新扫描以获取最终状态（P1-7：此处扫描不可复用，因为执行改变了文件）
	if pairSum.Errors == 0 {
		for _, n := range plan.Nodes {
			if err := saveNodeSnapshot(n, ignore); err != nil {
				return nil, err
			}
		}
	}

	if summary.Errors == 0 {
		summary.Message = "同步完成"
	} else {
		summary.Message = fmt.Sprintf("同步完成，但有 %d 个错误（快照未更新）", summary.Errors)
	}
	return summary, nil
}

// saveNodeSnapshot 重新扫描单节点并写回快照。
func saveNodeSnapshot(n Node, ignore []string) error {
	cur, err := New().Scan(n, ignore)
	if err != nil {
		return err
	}
	snap := &Snapshot{Node: n.Name, Files: cur}
	return SaveSnapshot(n.Path, snap)
}
