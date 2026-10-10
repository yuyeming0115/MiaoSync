package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/yuyeming0115/MiaoSync/internal/config"
	"github.com/yuyeming0115/MiaoSync/internal/fs"
	msync "github.com/yuyeming0115/MiaoSync/internal/sync"
)

// maxBodySize 限制请求体大小（P2-2），防止异常请求拖住进程。
const maxBodySize = 4 << 20 // 4 MiB

type planReq struct {
	Nodes    []string `json:"nodes"`
	Mode     string   `json:"mode"`
	Conflict string   `json:"conflict"`
}

type syncReq struct {
	Nodes            []string `json:"nodes"`
	Mode             string   `json:"mode"`
	Conflict         string   `json:"conflict"`
	ConfirmDangerous bool     `json:"confirmDangerous"`
	PlanID           string   `json:"planId"` // P1-2：预览返回的计划 ID
}

// loadOrCreateConfig 读取配置；不存在则写入默认配置。
// 配置有问题时不报错退出（降级）：尽力载入并返回问题描述，由界面提示用户修复。
func loadOrCreateConfig(path string) (*config.Config, string) {
	if _, err := os.Stat(path); err == nil {
		return config.LoadLenient(path)
	}
	cfg := defaultConfig()
	if err := config.Save(path, cfg); err != nil {
		// 默认配置都写不进去（目录不可写等）属于真致命错误
		return nil, fmt.Sprintf("无法创建默认配置 %s: %v", path, err)
	}
	return cfg, ""
}

func defaultConfig() *config.Config {
	return &config.Config{
		Nodes: []config.NodeCfg{
			{Name: "win-pc", WinPath: `D:\Sync`, MacPath: "", Role: "peer"},
			{Name: "hdd", WinPath: `E:\Sync`, MacPath: "/Volumes/Backup/Sync", Role: "peer"},
		},
		Mode:     msync.ModeMirror,
		Conflict: msync.ConflictRename,
		Ignore:   config.DefaultIgnore(),
	}
}

// decodeBody 解析 JSON 请求体（带大小限制，P2-2）。
func decodeBody(w http.ResponseWriter, r *http.Request, v interface{}) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodySize)
	return json.NewDecoder(r.Body).Decode(v)
}

// ---------- 配置 ----------
func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.cfgMu.RLock()
		cfg := *s.config // 拷贝一份避免调用方持锁
		s.cfgMu.RUnlock()
		writeJSON(w, cfg)
	case http.MethodPost:
		// P0-4：同步执行中拒绝改配置，避免配置在运行中被替换
		if s.isRunning() {
			writeConflict(w, "同步进行中，无法保存配置")
			return
		}
		var c config.Config
		if err := decodeBody(w, r, &c); err != nil {
			writeError(w, "配置解析失败: "+err.Error())
			return
		}
		if err := c.Validate(); err != nil {
			writeError(w, err.Error())
			return
		}
		if err := config.Save(s.configPath, &c); err != nil {
			writeError(w, "保存失败: "+err.Error())
			return
		}
		s.cfgMu.Lock()
		s.config = &c
		s.startupErr = "" // 保存成功即清除启动时的配置问题提示
		s.cfgMu.Unlock()
		writeJSON(w, map[string]bool{"ok": true})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// ---------- 状态 ----------
func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	s.cfgMu.RLock()
	nodes := append([]config.NodeCfg(nil), s.config.Nodes...)
	ignore := append([]string(nil), s.config.Ignore...)
	s.cfgMu.RUnlock()
	// running 由 stateMu 保护，与 cfg 读锁分开
	running := s.isRunning()

	type nodeStatus struct {
		Name       string `json:"name"`
		Path       string `json:"path"`
		Accessible bool   `json:"accessible"`
		FileCount  int    `json:"fileCount"`
		LastSync   int64  `json:"lastSync"`
	}
	list := []nodeStatus{}
	for _, nc := range nodes {
		ns := nodeStatus{Name: nc.Name, Path: nc.ActivePath()}
		n, err := nc.ToNode()
		if err != nil {
			list = append(list, ns)
			continue
		}
		if fi, serr := os.Stat(n.Path); serr == nil && fi.IsDir() {
			ns.Accessible = true
			ns.FileCount = countFiles(n, ignore)
		}
		if snap, lerr := msync.LoadSnapshot(n.Path); lerr == nil {
			ns.LastSync = snap.Updated
		}
		list = append(list, ns)
	}
	writeJSON(w, map[string]interface{}{
		"nodes":       list,
		"running":     running,
		"configError": s.startupErr,
	})
}

func countFiles(n msync.Node, ignore []string) int {
	count := 0
	root := n.Path
	_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		rel = fs.NormalizePath(rel)
		if rel == msync.SnapshotDir || strings.HasPrefix(rel, msync.SnapshotDir+"/") {
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
		count++
		return nil
	})
	return count
}

// ---------- 计划预览 ----------
func (s *Server) handlePlan(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req planReq
	if err := decodeBody(w, r, &req); err != nil {
		writeError(w, "请求解析失败")
		return
	}
	s.cfgMu.RLock()
	cfgNodes := append([]config.NodeCfg(nil), s.config.Nodes...)
	ignore := append([]string(nil), s.config.Ignore...)
	s.cfgMu.RUnlock()

	nodes, err := buildNodes(cfgNodes, req.Nodes)
	if err != nil {
		writeError(w, err.Error())
		return
	}
	// P2-1：严格解析模式与冲突策略
	mode, err := msync.ParseMode(req.Mode)
	if err != nil {
		writeError(w, err.Error())
		return
	}
	conflict, err := msync.ParseConflict(req.Conflict)
	if err != nil {
		writeError(w, err.Error())
		return
	}
	// P1-2：生成不可变计划并存储，返回计划 ID 供执行时校验
	plan, err := s.engine.PlanWithID(nodes, ignore, mode, conflict)
	if err != nil {
		writeError(w, err.Error())
		return
	}
	s.storePlan(plan)

	counts := map[msync.ActionKind]int{}
	for _, a := range plan.Actions {
		counts[a.Kind]++
	}
	writeJSON(w, map[string]interface{}{
		"planId":    plan.ID,
		"actions":   plan.Actions,
		"copied":    counts[msync.ActionCopy],
		"deleted":   counts[msync.ActionDelete],
		"conflicts": counts[msync.ActionRenameConflict],
		"total":     len(plan.Actions),
	})
}

// ---------- 同步（SSE 流式）----------
func (s *Server) handleSync(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req syncReq
	if err := decodeBody(w, r, &req); err != nil {
		writeError(w, "请求解析失败")
		return
	}
	// P1-2：必须携带预览返回的计划 ID，消除 TOCTOU
	if req.PlanID == "" {
		writeError(w, "缺少计划 ID，请先预览同步计划")
		return
	}
	plan := s.getPlan(req.PlanID)
	if plan == nil {
		writeError(w, msync.ErrPlanNotFound.Error())
		return
	}
	if plan.Expired() {
		s.removePlan(req.PlanID)
		writeError(w, msync.ErrPlanExpired.Error())
		return
	}

	mode := plan.Options.Mode
	// P0-4：同一时间只允许一个执行任务，否则多请求会并发改同一批文件和快照
	if !s.tryStartSync(mode) {
		info := s.snapshot()
		writeConflict(w, fmt.Sprintf("已有同步任务正在执行（开始于 %s），请稍后再试",
			info.startedAt.Format("15:04:05")))
		return
	}
	defer s.finishSync()

	s.cfgMu.RLock()
	ignore := append([]string(nil), s.config.Ignore...)
	s.cfgMu.RUnlock()

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, "服务器不支持流式响应")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	progress := func(stage string, done, total int, msg string) {
		sendEvent(w, flusher, map[string]interface{}{
			"type": "progress", "stage": stage, "done": done, "total": total, "msg": msg,
		})
	}

	start := time.Now()
	summary, runErr := s.engine.RunWithPlan(plan, ignore, msync.SyncOptions{
		Mode:             mode,
		Conflict:         plan.Options.Conflict,
		DryRun:           false,
		ConfirmDangerous: req.ConfirmDangerous,
	}, progress)
	if summary != nil {
		summary.DurationMs = time.Since(start).Milliseconds()
	}

	if runErr != nil {
		if runErr == msync.ErrDangerous && summary != nil {
			// 危险操作需确认：保留计划，用户确认后重试同一计划
			sendEvent(w, flusher, map[string]interface{}{"type": "done", "summary": summary})
			return
		}
		// 其他错误（含指纹过期/失效）：计划已不可用，删除防止重放
		s.removePlan(req.PlanID)
		sendEvent(w, flusher, map[string]interface{}{"type": "error", "message": runErr.Error()})
		return
	}
	// 执行成功：删除已消费的计划
	s.removePlan(req.PlanID)
	sendEvent(w, flusher, map[string]interface{}{"type": "done", "summary": summary})
}

// ---------- 工具 ----------
func buildNodes(cfgNodes []config.NodeCfg, names []string) ([]msync.Node, error) {
	if len(names) < 2 {
		return nil, fmt.Errorf("至少需要选择 2 个节点")
	}
	var nodes []msync.Node
	for _, name := range names {
		var found *config.NodeCfg
		for i := range cfgNodes {
			if cfgNodes[i].Name == name {
				found = &cfgNodes[i]
				break
			}
		}
		if found == nil {
			return nil, fmt.Errorf("节点 %q 不存在于配置", name)
		}
		n, err := found.ToNode()
		if err != nil {
			return nil, err
		}
		nodes = append(nodes, n)
	}
	return nodes, nil
}

func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusBadRequest)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

// writeConflict 返回 409，用于并发同步或执行中改配置等场景（P0-4）。
func writeConflict(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusConflict)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func sendEvent(w http.ResponseWriter, f http.Flusher, evt interface{}) {
	data, _ := json.Marshal(evt)
	fmt.Fprintf(w, "data: %s\n\n", data)
	f.Flush()
}
