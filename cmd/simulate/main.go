// MiaoSync 模拟测试工具
//
// 自主建立两套临时假的项目资产目录，构造差异/冲突，调用真实同步引擎
// 先预览计划、再实际执行、再复验一致性，最后生成一份自包含 HTML 报告。
package main

import (
	"fmt"
	"html"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/yuyeming0115/MiaoSync/internal/sync"
)

// ---------- 资产构造 ----------

type asset struct {
	rel   string // 相对路径，使用 "/"
	body  string
	mtime time.Time
}

func main() {
	outPath := "reports/sim-report.html"
	if len(os.Args) > 1 {
		outPath = os.Args[1]
	}
	if err := run(outPath); err != nil {
		fmt.Fprintln(os.Stderr, "模拟失败:", err)
		os.Exit(1)
	}
	fmt.Println("报告已生成:", outPath)
}

func run(outPath string) error {
	root := filepath.Join(os.TempDir(), "miaosync-sim")
	_ = os.RemoveAll(root)
	dirA := filepath.Join(root, "A")
	dirB := filepath.Join(root, "B")
	for _, d := range []string{dirA, dirB} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return err
		}
	}

	base := time.Date(2026, 7, 30, 0, 0, 0, 0, time.Local)
	// A 端资产
	assetsA := []asset{
		{"photos/IMG_001.jpg", "JPEG-A-001-content", base.Add(1 * time.Hour)},
		{"docs/shared.md", "# 共享文档\n\n这是 win-pc 的旧版本。\n", base.Add(2 * time.Hour)},
		{"notes/A-only.txt", "仅 A 端拥有的笔记。\n", base.Add(3 * time.Hour)},
		{"music/track01.mp3", "MP3-A-01", base.Add(4 * time.Hour)},
	}
	// B 端资产（docs/shared.md 内容不同 => 冲突；另有独有文件）
	assetsB := []asset{
		{"photos/IMG_002.jpg", "JPEG-B-002-content", base.Add(5 * time.Hour)},
		{"docs/shared.md", "# 共享文档\n\n这是移动硬盘的新版本（更新）。\n", base.Add(6 * time.Hour)},
		{"notes/B-only.txt", "仅 B 端拥有的笔记。\n", base.Add(7 * time.Hour)},
		{"video/clip.mp4", "MP4-B-clip", base.Add(8 * time.Hour)},
	}
	if err := writeAssets(dirA, assetsA); err != nil {
		return err
	}
	if err := writeAssets(dirB, assetsB); err != nil {
		return err
	}
	// 执行前快照（必须在 Run 之前抓取）
	beforeA := listTree(dirA)
	beforeB := listTree(dirB)

	nodes := []sync.Node{
		{Name: "win-pc（笔记本 A）", Path: dirA, Role: "peer"},
		{Name: "移动硬盘（磁盘 B）", Path: dirB, Role: "peer"},
	}
	// 忽略系统垃圾与冲突备份（冲突备份按 rename 策略生成，real 场景也应忽略以免二次传播）
	ignore := []string{".DS_Store", "Thumbs.db", "*.conflict-*"}
	mode := sync.ModeMirror
	conflict := sync.ConflictRename

	engine := sync.New()

	// 1) 预览计划
	plan, err := engine.PlanPairs(nodes, ignore, mode, conflict)
	if err != nil {
		return err
	}

	// 2) 实际执行
	summary, err := engine.Run(nodes, ignore,
		sync.SyncOptions{Mode: mode, Conflict: conflict, DryRun: false, ConfirmDangerous: false}, nil)
	if err != nil {
		return err
	}

	// 3) 复验一致性
	replan, err := engine.PlanPairs(nodes, ignore, mode, conflict)
	if err != nil {
		return err
	}

	// 收集数据生成报告
	afterA := listTree(dirA)
	afterB := listTree(dirB)

	return writeReport(outPath, reportData{
		GenAt:    time.Now().Format("2006-01-02 15:04:05"),
		Root:     root,
		DirA:     dirA,
		DirB:     dirB,
		Mode:     string(mode),
		Conflict: string(conflict),
		Ignore:   ignore,
		BeforeA:  beforeA,
		BeforeB:  beforeB,
		AfterA:   afterA,
		AfterB:   afterB,
		Plan:     plan,
		Summary:  summary,
		Replan:   replan,
	})
}

func writeAssets(dir string, assets []asset) error {
	for _, a := range assets {
		p := filepath.Join(dir, filepath.FromSlash(a.rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(p, []byte(a.body), 0o644); err != nil {
			return err
		}
		if err := os.Chtimes(p, a.mtime, a.mtime); err != nil {
			return err
		}
	}
	return nil
}

// listTree 返回按相对路径排序的文件列表（含大小，字节）。
func listTree(root string) []fileEntry {
	var out []fileEntry
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		rel = filepath.ToSlash(rel)
		if rel == ".miaosync/snapshot.json" {
			return nil
		}
		fi, e := d.Info()
		sz := int64(0)
		if e == nil {
			sz = fi.Size()
		}
		out = append(out, fileEntry{Rel: rel, Size: sz})
		return nil
	})
	sort.Slice(out, func(i, j int) bool { return out[i].Rel < out[j].Rel })
	return out
}

type fileEntry struct {
	Rel  string
	Size int64
}

// ---------- 报告生成 ----------

type reportData struct {
	GenAt    string
	Root     string
	DirA     string
	DirB     string
	Mode     string
	Conflict string
	Ignore   []string
	BeforeA  []fileEntry
	BeforeB  []fileEntry
	AfterA   []fileEntry
	AfterB   []fileEntry
	Plan     []sync.Action
	Summary  *sync.SyncSummary
	Replan   []sync.Action
}

func writeReport(path string, d reportData) error {
	var b strings.Builder
	b.WriteString(`<!DOCTYPE html><html lang="zh-CN"><head><meta charset="UTF-8"/>
<meta name="viewport" content="width=device-width, initial-scale=1.0"/>
<title>MiaoSync 模拟测试报告</title><style>
:root{--bg:#0f1419;--card:#18212b;--border:#283543;--text:#e6edf3;--dim:#8b98a5;--accent:#2dd4bf;--warm:#f59e0b;--copy:#38bdf8;--del:#f87171;--conf:#fbbf24;}
*{box-sizing:border-box;}body{margin:0;font-family:-apple-system,"PingFang SC","Microsoft YaHei",sans-serif;background:radial-gradient(1000px 500px at 80% -10%,#16323a,#0f1419 55%);color:var(--text);padding:28px;}
.wrap{max-width:1080px;margin:0 auto;}
h1{font-size:24px;margin:0 0 4px;}h2{font-size:17px;margin:26px 0 12px;border-left:4px solid var(--accent);padding-left:10px;}
.sub{color:var(--dim);font-size:13px;margin-bottom:8px;}
.card{background:var(--card);border:1px solid var(--border);border-radius:14px;padding:16px 18px;margin-bottom:14px;}
.cols{display:grid;grid-template-columns:1fr 1fr;gap:14px;}
@media(max-width:760px){.cols{grid-template-columns:1fr;}}
.tree{font-family:ui-monospace,Menlo,Consolas,monospace;font-size:12.5px;line-height:1.7;background:#0d141b;border-radius:8px;padding:12px;white-space:pre;overflow:auto;}
.kv{font-size:13px;color:var(--dim);} .kv b{color:var(--text);}
table{width:100%;border-collapse:collapse;font-size:12.5px;}
th,td{text-align:left;padding:7px 10px;border-bottom:1px solid var(--border);}
th{color:var(--dim);font-weight:600;}
.badge{display:inline-block;padding:3px 9px;border-radius:999px;font-size:11.5px;border:1px solid var(--border);}
.copy{color:var(--copy);border-color:rgba(56,189,248,.4);}
.del{color:var(--del);border-color:rgba(248,113,113,.4);}
.conf{color:var(--conf);border-color:rgba(251,191,36,.4);}
.stats{display:flex;gap:10px;flex-wrap:wrap;margin:6px 0 2px;}
.stat{padding:8px 14px;border-radius:10px;background:#0d141b;border:1px solid var(--border);}
.stat b{font-size:18px;display:block;}
.ok{color:var(--accent);} .warn{color:var(--conf);}
.note{color:var(--dim);font-size:12.5px;line-height:1.6;}
code{background:#0d141b;padding:2px 6px;border-radius:6px;color:var(--accent);}
</style></head><body><div class="wrap">`)

	fmt.Fprintf(&b, `<h1>🐱 MiaoSync 模拟测试报告</h1>
<p class="sub">生成时间：%s ｜ 引擎真实运行（非模拟数据）</p>`, html.EscapeString(d.GenAt))

	// 场景
	b.WriteString(`<div class="card"><h2>📦 测试场景</h2>
<p class="note">程序自主建立两套临时假资产目录，模拟「笔记本 A ↔ 移动硬盘 B」的双端同步。种子文件刻意构造了：<b>独有文件</b>（各自新增）、<b>同名冲突文件</b>（<code>docs/shared.md</code> 两端内容不同，B 端更新）。</p>
<p class="kv">临时根目录：<b>` + html.EscapeString(d.Root) + `</b></p>
<p class="kv">模式：<b>` + html.EscapeString(d.Mode) + `</b> ｜ 冲突策略：<b>` + html.EscapeString(d.Conflict) + `</b> ｜ 忽略：<b>` + html.EscapeString(strings.Join(d.Ignore, ", ")) + `</b></p>
</div>`)

	// 执行前
	b.WriteString(`<h2>① 执行前 · 两端资产快照</h2><div class="cols">`)
	b.WriteString(treeCard("win-pc（A）", d.BeforeA))
	b.WriteString(treeCard("移动硬盘（B）", d.BeforeB))
	b.WriteString(`</div>`)

	// 计划
	counts := map[string]int{}
	for _, a := range d.Plan {
		counts[string(a.Kind)]++
	}
	b.WriteString(`<h2>② 同步计划预览（PlanPairs 输出）</h2>
<div class="card"><div class="stats">
<span class="stat">复制<b class="ok">` + itoa(counts["copy"]) + `</b></span>
<span class="stat">删除<b class="del">` + itoa(counts["delete"]) + `</b></span>
<span class="stat">冲突<b class="conf">` + itoa(counts["rename-conflict"]) + `</b></span>
<span class="stat">合计<b>` + itoa(len(d.Plan)) + `</b></span>
</div>`)
	if len(d.Plan) == 0 {
		b.WriteString(`<p class="note">两端已一致，无操作。</p>`)
	} else {
		b.WriteString(`<table><thead><tr><th>类型</th><th>相对路径</th><th>说明</th></tr></thead><tbody>`)
		for _, a := range d.Plan {
			cls := "copy"
			if a.Kind == "delete" {
				cls = "del"
			} else if a.Kind == "rename-conflict" {
				cls = "conf"
			}
			label := strings.ToUpper(string(a.Kind))
			fmt.Fprintf(&b, `<tr><td><span class="badge %s">%s</span></td><td><code>%s</code></td><td class="note">%s</td></tr>`,
				cls, label, html.EscapeString(a.RelPath), html.EscapeString(a.Reason))
		}
		b.WriteString(`</tbody></table>`)
	}
	b.WriteString(`</div>`)

	// 执行结果
	if d.Summary != nil {
		b.WriteString(`<h2>③ 实际执行结果（Run）</h2><div class="card"><div class="stats">
<span class="stat">复制<b class="ok">` + itoa(d.Summary.Copied) + `</b></span>
<span class="stat">删除<b class="del">` + itoa(d.Summary.Deleted) + `</b></span>
<span class="stat">冲突<b class="conf">` + itoa(d.Summary.Conflicts) + `</b></span>
<span class="stat">错误<b>` + itoa(d.Summary.Errors) + `</b></span>
</div>`)
		if d.Summary.Message != "" {
			fmt.Fprintf(&b, `<p class="kv">%s</p>`, html.EscapeString(d.Summary.Message))
		}
		if d.Summary.DangerousInfo != "" {
			fmt.Fprintf(&b, `<p class="warn">⚠️ %s</p>`, html.EscapeString(d.Summary.DangerousInfo))
		}
		b.WriteString(`</div>`)
	}

	// 执行后
	b.WriteString(`<h2>④ 执行后 · 两端资产快照</h2><div class="cols">`)
	b.WriteString(treeCard("win-pc（A）", d.AfterA))
	b.WriteString(treeCard("移动硬盘（B）", d.AfterB))
	b.WriteString(`</div>`)

	// 复验
	b.WriteString(`<h2>⑤ 一致性复验（再次 PlanPairs）</h2>`)
	if len(d.Replan) == 0 {
		b.WriteString(`<div class="card"><p class="ok" style="margin:0">✅ 复验通过：两端现已在业务文件层面完全一致，剩余待操作数为 <b>0</b>。引擎的增量快照比对工作正常。</p>
<p class="note" style="margin:8px 0 0">注：执行后 A 端会保留一个 <code>docs/shared.conflict-*.md</code> 备份（rename 策略的「旧版本重命名」），该文件已加入忽略列表，因此不计入同步差异，也不会二次传播到 B 端。如需彻底收敛，可手动清理该备份。</p></div>`)
	} else {
		b.WriteString(`<div class="card"><p class="warn" style="margin:0">⚠️ 复验仍存在 ` + itoa(len(d.Replan)) + ` 个差异，需排查。</p></div>`)
	}

	b.WriteString(`<p class="sub" style="margin-top:24px">本报告由 MiaoSync 内置同步引擎真实运行生成 · 临时目录：` + html.EscapeString(d.Root) + `</p>`)
	b.WriteString(`</div></body></html>`)

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(b.String()), 0o644)
}

func treeCard(title string, files []fileEntry) string {
	var sb strings.Builder
	sb.WriteString(`<div class="card"><div class="kv" style="margin-bottom:8px">` + html.EscapeString(title) + ` <b>（` + itoa(len(files)) + ` 个文件）</b></div><div class="tree">`)
	for _, f := range files {
		fmt.Fprintf(&sb, "%s  <span style=\"color:#8b98a5\">(%d B)</span>\n", html.EscapeString(f.Rel), f.Size)
	}
	if len(files) == 0 {
		sb.WriteString(`(空)`)
	}
	sb.WriteString(`</div></div>`)
	return sb.String()
}

func itoa(n int) string { return fmt.Sprintf("%d", n) }
