package sync

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func defaultIgnore() []string {
	return []string{".DS_Store", "Thumbs.db", ".git", ".miaosync"}
}

func collectFiles(root string) map[string]string {
	m := map[string]string{}
	_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		// 跳过引擎落盘的快照目录，避免统计/比对污染
		if rel == ".miaosync" || strings.HasPrefix(rel, ".miaosync"+string(filepath.Separator)) {
			return nil
		}
		data, _ := os.ReadFile(p)
		m[rel] = string(data)
		return nil
	})
	return m
}

func TestMirrorSync(t *testing.T) {
	base := t.TempDir()
	a := filepath.Join(base, "nodeA")
	b := filepath.Join(base, "nodeB")
	os.MkdirAll(a, 0o755)
	os.MkdirAll(b, 0o755)
	writeFile(t, filepath.Join(a, "f1.txt"), "hello")
	writeFile(t, filepath.Join(a, "sub/f2.txt"), "world")
	writeFile(t, filepath.Join(b, "g1.txt"), "only-in-b")

	eng := New()
	na := Node{Name: "A", Path: a}
	nb := Node{Name: "B", Path: b}

	// PlanPairs：期望 3 个 copy（A 的 2 个文件到 B，B 的 1 个文件到 A）
	acts, err := eng.PlanPairs([]Node{na, nb}, defaultIgnore(), ModeMirror, ConflictRename)
	if err != nil {
		t.Fatal(err)
	}
	copyCount := 0
	for _, act := range acts {
		if act.Kind != ActionCopy {
			t.Errorf("unexpected action %s for %s", act.Kind, act.RelPath)
		}
		copyCount++
	}
	if copyCount != 3 {
		t.Errorf("expected 3 copy actions, got %d", copyCount)
	}

	// DryRun 不应改变磁盘
	sum, err := eng.Run([]Node{na, nb}, defaultIgnore(), SyncOptions{Mode: ModeMirror, Conflict: ConflictRename, DryRun: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if sum.Copied != 3 {
		t.Errorf("dryRun copied expected 3, got %d", sum.Copied)
	}
	if len(collectFiles(a)) != 2 || len(collectFiles(b)) != 1 {
		t.Errorf("dryRun should not modify disk")
	}

	// 真正执行
	sum, err = eng.Run([]Node{na, nb}, defaultIgnore(), SyncOptions{Mode: ModeMirror, Conflict: ConflictRename}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if sum.Copied != 3 || sum.Errors != 0 {
		t.Errorf("run summary unexpected: %+v", sum)
	}

	ma, mb := collectFiles(a), collectFiles(b)
	if len(ma) != 3 || len(mb) != 3 {
		t.Errorf("after sync file counts mismatch: A=%d B=%d", len(ma), len(mb))
	}
	for k, v := range ma {
		if mb[k] != v {
			t.Errorf("content mismatch for %s: A=%q B=%q", k, v, mb[k])
		}
	}
}

func TestBackupMode(t *testing.T) {
	base := t.TempDir()
	src := filepath.Join(base, "src")
	dst := filepath.Join(base, "dst")
	os.MkdirAll(src, 0o755)
	os.MkdirAll(dst, 0o755)
	writeFile(t, filepath.Join(src, "only-src.txt"), "x")
	writeFile(t, filepath.Join(dst, "only-dst.txt"), "y")

	eng := New()
	ns := Node{Name: "S", Path: src, Role: "source"}
	nd := Node{Name: "D", Path: dst, Role: "target"}

	sum, err := eng.Run([]Node{ns, nd}, defaultIgnore(), SyncOptions{Mode: ModeBackup, Conflict: ConflictRename}, nil)
	if err != nil {
		t.Fatal(err)
	}
	// backup：src 独有 → dst（1 copy）；dst 独有不应反向复制
	if sum.Copied != 1 {
		t.Errorf("backup expected 1 copy, got %d", sum.Copied)
	}
	if _, ok := collectFiles(dst)["only-src.txt"]; !ok {
		t.Errorf("backup should copy src file to dst")
	}
	if _, ok := collectFiles(src)["only-dst.txt"]; ok {
		t.Errorf("backup should NOT copy dst-only file back to src")
	}
}

func TestConflictRename(t *testing.T) {
	base := t.TempDir()
	a := filepath.Join(base, "nodeA")
	b := filepath.Join(base, "nodeB")
	os.MkdirAll(a, 0o755)
	os.MkdirAll(b, 0o755)
	writeFile(t, filepath.Join(a, "c.txt"), "versionA")
	writeFile(t, filepath.Join(b, "c.txt"), "versionB")

	// 用显式 mtime 让 A 确定为较新一方（避免快速连续写入时 mtime 落入同 tick 导致胜负不确定）
	now := time.Now()
	older := now.Add(-2 * time.Second)
	newer := now
	if err := os.Chtimes(filepath.Join(b, "c.txt"), older, older); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(filepath.Join(a, "c.txt"), newer, newer); err != nil {
		t.Fatal(err)
	}

	eng := New()
	na := Node{Name: "A", Path: a}
	nb := Node{Name: "B", Path: b}
	// A 较新 → A 胜出：A 版本保留，B 旧版本重命名备份
	sum, err := eng.Run([]Node{na, nb}, defaultIgnore(), SyncOptions{Mode: ModeMirror, Conflict: ConflictRename}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if sum.Conflicts != 1 {
		t.Errorf("expected 1 conflict, got %d", sum.Conflicts)
	}
	da, _ := os.ReadFile(filepath.Join(a, "c.txt"))
	db, _ := os.ReadFile(filepath.Join(b, "c.txt"))
	if string(da) != "versionA" || string(db) != "versionA" {
		t.Errorf("conflict not resolved to winner A: A=%q B=%q", da, db)
	}
	// B 端应有 .conflict 备份副本
	found := false
	_ = filepath.WalkDir(b, func(p string, d os.DirEntry, err error) error {
		if err == nil && strings.Contains(d.Name(), ".conflict-") {
			found = true
		}
		return nil
	})
	if !found {
		t.Errorf("expected a .conflict backup file in B")
	}
}

func TestIgnore(t *testing.T) {
	base := t.TempDir()
	a := filepath.Join(base, "nodeA")
	b := filepath.Join(base, "nodeB")
	os.MkdirAll(a, 0o755)
	os.MkdirAll(b, 0o755)
	writeFile(t, filepath.Join(a, "keep.txt"), "keep")
	writeFile(t, filepath.Join(a, ".DS_Store"), "ignore-me")
	writeFile(t, filepath.Join(a, "tmp/cache.tmp"), "ignore-me-too")

	eng := New()
	na := Node{Name: "A", Path: a}
	nb := Node{Name: "B", Path: b}
	ignore := append(defaultIgnore(), "*.tmp")
	acts, err := eng.PlanPairs([]Node{na, nb}, ignore, ModeMirror, ConflictRename)
	if err != nil {
		t.Fatal(err)
	}
	for _, act := range acts {
		if strings.Contains(act.RelPath, ".DS_Store") || strings.HasSuffix(act.RelPath, ".tmp") {
			t.Errorf("ignored file should not appear in plan: %s", act.RelPath)
		}
	}
}

// TestMirrorDeletePropagatesOnce 验证 P0-1：
// 首次同步后任一端删除文件，再次 mirror 同步应只产生删除传播，最终两端都无该文件，
// 而不是同时生成“恢复复制 + 同步删除”导致状态反复。
func TestMirrorDeletePropagatesOnce(t *testing.T) {
	base := t.TempDir()
	a := filepath.Join(base, "nodeA")
	b := filepath.Join(base, "nodeB")
	os.MkdirAll(a, 0o755)
	os.MkdirAll(b, 0o755)
	writeFile(t, filepath.Join(a, "shared.txt"), "data")

	eng := New()
	na := Node{Name: "A", Path: a}
	nb := Node{Name: "B", Path: b}

	// 首次同步：A 的文件复制到 B，两端一致
	if _, err := eng.Run([]Node{na, nb}, defaultIgnore(), SyncOptions{Mode: ModeMirror, Conflict: ConflictRename}, nil); err != nil {
		t.Fatal(err)
	}

	// 在 A 端删除该文件
	if err := os.Remove(filepath.Join(a, "shared.txt")); err != nil {
		t.Fatal(err)
	}

	// 预览：应只产生 1 个 delete（在 B 端），不应有任何 copy
	acts, err := eng.PlanPairs([]Node{na, nb}, defaultIgnore(), ModeMirror, ConflictRename)
	if err != nil {
		t.Fatal(err)
	}
	deleteCount, copyCount := 0, 0
	for _, act := range acts {
		switch act.Kind {
		case ActionDelete:
			deleteCount++
		case ActionCopy:
			copyCount++
		}
	}
	if deleteCount != 1 || copyCount != 0 {
		t.Errorf("期望 1 delete / 0 copy，实际 delete=%d copy=%d（动作=%+v）", deleteCount, copyCount, acts)
	}

	// 真正执行：两端最终都不存在该文件（小目录删除会触发危险阈值，显式确认）
	if _, err := eng.Run([]Node{na, nb}, defaultIgnore(), SyncOptions{Mode: ModeMirror, Conflict: ConflictRename, ConfirmDangerous: true}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(b, "shared.txt")); !os.IsNotExist(err) {
		t.Errorf("B 端应已删除 shared.txt，err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(a, "shared.txt")); !os.IsNotExist(err) {
		t.Errorf("A 端应保持删除状态，err=%v", err)
	}
}

// TestModifyDeleteConflictPreservesModifiedCopy 验证 P0-1 修改/删除冲突语义：
// 一端修改、另一端删除（曾同步过），mirror 下应保留修改并复制回删除端，不丢数据。
func TestModifyDeleteConflictPreservesModifiedCopy(t *testing.T) {
	base := t.TempDir()
	a := filepath.Join(base, "nodeA")
	b := filepath.Join(base, "nodeB")
	os.MkdirAll(a, 0o755)
	os.MkdirAll(b, 0o755)
	writeFile(t, filepath.Join(a, "f.txt"), "v1")

	eng := New()
	na := Node{Name: "A", Path: a}
	nb := Node{Name: "B", Path: b}
	if _, err := eng.Run([]Node{na, nb}, defaultIgnore(), SyncOptions{Mode: ModeMirror, Conflict: ConflictRename}, nil); err != nil {
		t.Fatal(err)
	}

	// A 删除，B 修改
	os.Remove(filepath.Join(a, "f.txt"))
	writeFile(t, filepath.Join(b, "f.txt"), "v2-modified")

	// 预览：应只产生 1 个 copy（B→A 恢复），不应有 delete
	acts, err := eng.PlanPairs([]Node{na, nb}, defaultIgnore(), ModeMirror, ConflictRename)
	if err != nil {
		t.Fatal(err)
	}
	copyCount, deleteCount := 0, 0
	for _, act := range acts {
		switch act.Kind {
		case ActionCopy:
			copyCount++
		case ActionDelete:
			deleteCount++
		}
	}
	if copyCount != 1 || deleteCount != 0 {
		t.Errorf("期望 1 copy / 0 delete，实际 copy=%d delete=%d（动作=%+v）", copyCount, deleteCount, acts)
	}

	if _, err := eng.Run([]Node{na, nb}, defaultIgnore(), SyncOptions{Mode: ModeMirror, Conflict: ConflictRename}, nil); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(filepath.Join(a, "f.txt"))
	if string(got) != "v2-modified" {
		t.Errorf("A 端应恢复为 B 的修改版本，实际 %q", got)
	}
}

// TestAtomicCopyKeepsOldTargetOnFailure 验证 P0-3：
// 复制失败时（源文件不可读）已存在的目标文件应保持原内容，不被截断破坏。
func TestAtomicCopyKeepsOldTargetOnFailure(t *testing.T) {
	base := t.TempDir()
	dst := filepath.Join(base, "dst")
	os.MkdirAll(dst, 0o755)
	// 已存在的目标文件
	writeFile(t, filepath.Join(dst, "f.txt"), "原内容-不应被破坏")

	// 构造一个 From 指向不存在文件的 copy 动作，必然失败
	acts := []Action{{
		Kind:    ActionCopy,
		RelPath: "f.txt",
		From:    filepath.Join(base, "no-such-file.txt"),
		To:      filepath.Join(dst, "f.txt"),
		Size:    10,
		Reason:  "测试失败路径",
	}}
	eng := New()
	sum, _, err := eng.Apply(acts, ConflictRename, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	if sum.Errors != 1 {
		t.Errorf("期望 1 个错误，实际 %d", sum.Errors)
	}
	// 目标文件原内容应保持完整
	got, _ := os.ReadFile(filepath.Join(dst, "f.txt"))
	if string(got) != "原内容-不应被破坏" {
		t.Errorf("目标文件被破坏，实际 %q", got)
	}
	// 不应残留临时文件
	entries, _ := os.ReadDir(dst)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".miaosync-tmp-") {
			t.Errorf("残留临时文件: %s", e.Name())
		}
	}
}

// TestRunDoesNotCommitSnapshotOnError 验证 P0-2：
// 当某 pair 的动作执行失败时，相关节点不应写入新快照（避免把失败状态固化为基线）。
// 通过在目标端用同名普通文件阻挡子目录创建来制造确定性的复制失败。
func TestRunDoesNotCommitSnapshotOnError(t *testing.T) {
	base := t.TempDir()
	a := filepath.Join(base, "nodeA")
	b := filepath.Join(base, "nodeB")
	os.MkdirAll(a, 0o755)
	os.MkdirAll(b, 0o755)
	// src 有 block/inner.txt；dst 有同名普通文件 block → MkdirAll("block") 必失败
	writeFile(t, filepath.Join(a, "block/inner.txt"), "payload")
	writeFile(t, filepath.Join(b, "block"), "i-am-a-file-not-a-dir")

	eng := New()
	na := Node{Name: "A", Path: a}
	nb := Node{Name: "B", Path: b}
	sum, err := eng.Run([]Node{na, nb}, defaultIgnore(), SyncOptions{Mode: ModeMirror, Conflict: ConflictRename}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if sum.Errors == 0 {
		t.Fatalf("期望复制失败产生错误，实际 summary=%+v", sum)
	}
	// 失败节点的快照更新时间应仍为 0（从未成功提交过）
	snapB, err := LoadSnapshot(b)
	if err != nil {
		t.Fatal(err)
	}
	if snapB.Updated != 0 {
		t.Errorf("失败时不应提交快照，但 B 端快照 Updated=%d", snapB.Updated)
	}
	// 目标文件 block 保持原样（未被替换/破坏）
	got, _ := os.ReadFile(filepath.Join(b, "block"))
	if string(got) != "i-am-a-file-not-a-dir" {
		t.Errorf("阻挡文件被破坏，实际 %q", got)
	}
}

// TestPlanIDRejectedAfterFilesystemChange 验证 P1-2：
// 预览后文件系统发生变化（新增/删除/修改），执行时计划应失效并要求重新预览。
func TestPlanIDRejectedAfterFilesystemChange(t *testing.T) {
	base := t.TempDir()
	a := filepath.Join(base, "nodeA")
	b := filepath.Join(base, "nodeB")
	os.MkdirAll(a, 0o755)
	os.MkdirAll(b, 0o755)
	writeFile(t, filepath.Join(a, "f1.txt"), "hello")

	eng := New()
	na := Node{Name: "A", Path: a}
	nb := Node{Name: "B", Path: b}

	// 预览：生成不可变计划
	plan, err := eng.PlanWithID([]Node{na, nb}, defaultIgnore(), ModeMirror, ConflictRename)
	if err != nil {
		t.Fatal(err)
	}
	if plan.ID == "" {
		t.Fatal("计划 ID 不应为空")
	}
	if plan.Fingerprint == "" {
		t.Fatal("计划指纹不应为空")
	}
	if len(plan.Actions) == 0 {
		t.Fatal("预览应产生动作")
	}

	// 在 A 端新增文件，使文件系统状态与预览时不一致
	writeFile(t, filepath.Join(a, "f2.txt"), "new-file-after-preview")

	// 执行：指纹不匹配应返回 ErrPlanStale
	_, err = eng.RunWithPlan(plan, defaultIgnore(), SyncOptions{
		Mode: ModeMirror, Conflict: ConflictRename, DryRun: false,
	}, nil)
	if err != ErrPlanStale {
		t.Errorf("文件变化后期望 ErrPlanStale，实际 %v", err)
	}
}

// TestPlanIDExecutesPreviewedActions 验证 P1-2：
// 文件系统未变化时，计划正常执行预览时的动作，且幂等（再次预览动作数为 0）。
func TestPlanIDExecutesPreviewedActions(t *testing.T) {
	base := t.TempDir()
	a := filepath.Join(base, "nodeA")
	b := filepath.Join(base, "nodeB")
	os.MkdirAll(a, 0o755)
	os.MkdirAll(b, 0o755)
	writeFile(t, filepath.Join(a, "shared.txt"), "data")

	eng := New()
	na := Node{Name: "A", Path: a}
	nb := Node{Name: "B", Path: b}

	// 预览
	plan, err := eng.PlanWithID([]Node{na, nb}, defaultIgnore(), ModeMirror, ConflictRename)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Actions) != 1 {
		t.Fatalf("预览应产生 1 个 copy 动作，实际 %d", len(plan.Actions))
	}

	// 执行（文件未变化，指纹匹配）
	sum, err := eng.RunWithPlan(plan, defaultIgnore(), SyncOptions{
		Mode: ModeMirror, Conflict: ConflictRename, ConfirmDangerous: false,
	}, nil)
	if err != nil {
		t.Fatalf("执行计划失败: %v", err)
	}
	if sum.Copied != 1 || sum.Errors != 0 {
		t.Errorf("期望 1 copy / 0 errors，实际 %+v", sum)
	}

	// 幂等：再次预览动作数应为 0
	plan2, err := eng.PlanWithID([]Node{na, nb}, defaultIgnore(), ModeMirror, ConflictRename)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan2.Actions) != 0 {
		t.Errorf("幂等检查：再次预览应 0 动作，实际 %d", len(plan2.Actions))
	}
}

// TestBackupUsesRolesNotNodeOrder 验证 P1-1：
// backup 模式下执行方向由角色决定，而非节点数组顺序。
// 无论 source 在前还是 target 在前，结果应一致：只从 source 复制到 target。
func TestBackupUsesRolesNotNodeOrder(t *testing.T) {
	for _, order := range []string{"src-first", "dst-first"} {
		t.Run(order, func(t *testing.T) {
			base := t.TempDir()
			srcDir := filepath.Join(base, "src")
			dstDir := filepath.Join(base, "dst")
			os.MkdirAll(srcDir, 0o755)
			os.MkdirAll(dstDir, 0o755)
			writeFile(t, filepath.Join(srcDir, "only-src.txt"), "x")
			writeFile(t, filepath.Join(dstDir, "only-dst.txt"), "y")

			src := Node{Name: "S", Path: srcDir, Role: "source"}
			dst := Node{Name: "D", Path: dstDir, Role: "target"}
			var nodes []Node
			if order == "src-first" {
				nodes = []Node{src, dst}
			} else {
				nodes = []Node{dst, src}
			}

			eng := New()
			sum, err := eng.Run(nodes, defaultIgnore(), SyncOptions{Mode: ModeBackup, Conflict: ConflictRename}, nil)
			if err != nil {
				t.Fatal(err)
			}
			// backup：src→dst 1 copy；dst 独有不反向复制
			if sum.Copied != 1 {
				t.Errorf("backup expected 1 copy, got %d", sum.Copied)
			}
			if _, ok := collectFiles(dstDir)["only-src.txt"]; !ok {
				t.Errorf("backup should copy src file to dst")
			}
			if _, ok := collectFiles(srcDir)["only-dst.txt"]; ok {
				t.Errorf("backup should NOT copy dst-only file back to src")
			}
		})
	}
}

// TestThreeNodeMirrorIsOrderIndependent 验证 P1-1：
// 三节点 mirror 同步后所有节点内容一致，且结果不受节点选择顺序影响。
func TestThreeNodeMirrorIsOrderIndependent(t *testing.T) {
	base := t.TempDir()
	dirs := make([]string, 3)
	for i := range dirs {
		dirs[i] = filepath.Join(base, "node"+string(rune('A'+i)))
		os.MkdirAll(dirs[i], 0o755)
	}
	// 每个节点有独有文件
	writeFile(t, filepath.Join(dirs[0], "a.txt"), "A")
	writeFile(t, filepath.Join(dirs[1], "b.txt"), "B")
	writeFile(t, filepath.Join(dirs[2], "c.txt"), "C")

	// 第一次同步：A, B, C
	eng := New()
	nodesABC := []Node{
		{Name: "A", Path: dirs[0]},
		{Name: "B", Path: dirs[1]},
		{Name: "C", Path: dirs[2]},
	}
	if _, err := eng.Run(nodesABC, defaultIgnore(), SyncOptions{Mode: ModeMirror, Conflict: ConflictRename}, nil); err != nil {
		t.Fatal(err)
	}

	// 验证三个节点都有 a.txt, b.txt, c.txt
	for i, d := range dirs {
		files := collectFiles(d)
		if len(files) != 3 {
			t.Errorf("node %d 应有 3 个文件，实际 %d: %v", i, len(files), files)
		}
		for _, name := range []string{"a.txt", "b.txt", "c.txt"} {
			if _, ok := files[name]; !ok {
				t.Errorf("node %d 缺少 %s", i, name)
			}
		}
	}
}

// TestRejectCaseCollision 验证 P1-5：
// 同一目录下 A.txt 与 a.txt 在大小写不敏感比较下会折叠，Scan 应报错。
func TestRejectCaseCollision(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows 文件系统本身大小写不敏感，无法创建 A.txt 和 a.txt")
	}
	base := t.TempDir()
	a := filepath.Join(base, "nodeA")
	os.MkdirAll(a, 0o755)
	writeFile(t, filepath.Join(a, "File.txt"), "upper")
	writeFile(t, filepath.Join(a, "file.txt"), "lower")

	eng := New()
	na := Node{Name: "A", Path: a}
	_, err := eng.Scan(na, defaultIgnore())
	if err == nil {
		t.Error("期望大小写冲突报错，但 Scan 返回 nil")
	}
}

// TestSymlinkDoesNotEscapeRoot 验证 P1-4：
// 符号链接应被跳过，不读取同步根目录之外的数据。
func TestSymlinkDoesNotEscapeRoot(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows 上创建符号链接需要管理员权限")
	}
	base := t.TempDir()
	a := filepath.Join(base, "nodeA")
	outside := filepath.Join(base, "outside")
	os.MkdirAll(a, 0o755)
	os.MkdirAll(outside, 0o755)
	// 根目录外的文件
	writeFile(t, filepath.Join(outside, "secret.txt"), "should-not-sync")
	// 节点内文件
	writeFile(t, filepath.Join(a, "real.txt"), "real-content")
	// 符号链接指向根目录外
	_ = os.Symlink(filepath.Join(outside, "secret.txt"), filepath.Join(a, "link.txt"))

	eng := New()
	na := Node{Name: "A", Path: a}
	states, err := eng.Scan(na, defaultIgnore())
	if err != nil {
		t.Fatal(err)
	}
	// 符号链接不应出现在扫描结果中
	for k, st := range states {
		if st.Path == "link.txt" {
			t.Errorf("符号链接 link.txt 不应被扫描: key=%s", k)
		}
	}
	// real.txt 应存在
	found := false
	for _, st := range states {
		if st.Path == "real.txt" {
			found = true
		}
	}
	if !found {
		t.Error("real.txt 应在扫描结果中")
	}
}

// TestConflictArchiveWithForwardSlashRoot 回归测试：
// 节点路径在配置里写成正斜杠（YAML 常见写法）时，冲突归档不得因
// 绝对路径字符串裁剪失配而把盘符拼进目录名（Windows 下 mkdir "...\conflicts\C:" 报错）。
func TestConflictArchiveWithForwardSlashRoot(t *testing.T) {
	base := t.TempDir()
	a := filepath.Join(base, "nodeA")
	b := filepath.Join(base, "nodeB")
	os.MkdirAll(a, 0o755)
	os.MkdirAll(b, 0o755)
	writeFile(t, filepath.Join(a, "c.txt"), "versionA")
	writeFile(t, filepath.Join(b, "c.txt"), "versionB")

	now := time.Now()
	_ = os.Chtimes(filepath.Join(b, "c.txt"), now.Add(-2*time.Second), now.Add(-2*time.Second))
	_ = os.Chtimes(filepath.Join(a, "c.txt"), now, now)

	eng := New()
	// 模拟 YAML 配置里的正斜杠路径（filepath.ToSlash 在 Windows 上产生 C:/... 形态）
	na := Node{Name: "A", Path: filepath.ToSlash(a)}
	nb := Node{Name: "B", Path: filepath.ToSlash(b)}
	sum, err := eng.Run([]Node{na, nb}, defaultIgnore(), SyncOptions{Mode: ModeMirror, Conflict: ConflictRename}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if sum.Errors != 0 {
		t.Fatalf("正斜杠根路径下冲突处理应无错误，实际 errors=%d", sum.Errors)
	}
	// 败方（B 端）应有归档副本且位于 .miaosync/conflicts/ 下
	found := false
	_ = filepath.WalkDir(b, func(p string, d os.DirEntry, err error) error {
		if err == nil && strings.Contains(d.Name(), ".conflict-") && strings.Contains(p, SnapshotDir) {
			found = true
		}
		return nil
	})
	if !found {
		t.Error("B 端 .miaosync/conflicts/ 下应有冲突归档副本")
	}
}

// TestCorruptSnapshotEntersRecoveryMode 验证 P1-8：
// 损坏的快照文件应返回错误，而非静默覆盖。
func TestCorruptSnapshotEntersRecoveryMode(t *testing.T) {
	base := t.TempDir()
	a := filepath.Join(base, "nodeA")
	os.MkdirAll(filepath.Join(a, SnapshotDir), 0o755)
	// 写入损坏的 JSON
	_ = os.WriteFile(SnapshotPath(a), []byte("{broken json"), 0o644)

	_, err := LoadSnapshot(a)
	if err == nil {
		t.Error("损坏的快照应返回错误，而非静默处理")
	}
}
