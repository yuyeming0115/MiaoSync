// Package web 提供 MiaoSync 的内置 Web GUI 服务（单页应用 + REST/SSE API）。
package web

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"sync"
	"time"

	"github.com/yuyeming0115/MiaoSync/internal/config"
	msync "github.com/yuyeming0115/MiaoSync/internal/sync"
)

//go:embed static
var staticFS embed.FS

// Server 持有引擎与当前配置。
type Server struct {
	engine     *msync.Engine
	configPath string

	cfgMu  sync.RWMutex // 保护 config 的读写
	config *config.Config

	// startupErr 启动时配置加载/校验发现的问题（降级模式）。
	// 通过 /api/status 下发给前端展示，保存配置成功后清除。
	startupErr string

	// P0-4：运行状态与独占锁
	stateMu sync.Mutex // 保护 running 与 currentRun
	running bool
	current *runInfo

	// P1-2：不可变计划存储（预览时生成，执行时校验）
	planMu sync.Mutex
	plans  map[string]*msync.Plan
}

// runInfo 当前正在执行的任务信息（用于 409 响应展示）。
type runInfo struct {
	startedAt time.Time
	mode      msync.Mode
}

// New 创建 Web 服务；若配置文件不存在则写入一份默认配置。
// 配置存在但有问题时不失败（降级）：尽力载入，问题通过 /api/status 通知前端。
func New(configPath string) (*Server, error) {
	cfg, warn := loadOrCreateConfig(configPath)
	if cfg == nil {
		return nil, fmt.Errorf("%s", warn)
	}
	return &Server{
		engine:     msync.New(),
		configPath: configPath,
		config:     cfg,
		startupErr: warn,
		plans:      map[string]*msync.Plan{},
	}, nil
}

// Routes 返回路由表。
func (s *Server) Routes() *http.ServeMux {
	mux := http.NewServeMux()
	sub, _ := fs.Sub(staticFS, "static")
	staticServer := http.StripPrefix("/static/", http.FileServer(http.FS(sub)))
	mux.Handle("/static/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		w.Header().Set("Pragma", "no-cache")
		w.Header().Set("Expires", "0")
		staticServer.ServeHTTP(w, r)
	}))
	mux.HandleFunc("/api/config", s.handleConfig)
	mux.HandleFunc("/api/status", s.handleStatus)
	mux.HandleFunc("/api/plan", s.handlePlan)
	mux.HandleFunc("/api/pick-folder", s.handlePickFolder)
	mux.HandleFunc("/api/drives", s.handleDrives)
	mux.HandleFunc("/api/sync", s.handleSync)
	mux.HandleFunc("/api/meta", s.handleMeta)
	mux.HandleFunc("/", s.handleIndex)
	return mux
}

// Start 启动 HTTP 服务（阻塞），支持优雅关闭（P2-2）。
// 收到中断信号后等待进行中的同步完成（最多 30 秒），然后关闭服务。
func (s *Server) Start(addr string) error {
	srv := &http.Server{
		Addr:              addr,
		Handler:           s.Routes(),
		ReadHeaderTimeout: 10 * time.Second, // 防 slowloris
		ReadTimeout:       30 * time.Second,
		IdleTimeout:       120 * time.Second,
		// WriteTimeout 不设置：SSE 流式同步可能耗时较长
	}

	// 信号监听：Ctrl+C / kill
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt)

	go func() {
		sig := <-stop
		log.Printf("收到退出信号: %v，开始优雅关闭...", sig)
		// 优雅关闭：等待同步完成
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		for s.isRunning() {
			select {
			case <-ctx.Done():
				break
			case <-time.After(200 * time.Millisecond):
			}
		}
		srv.Shutdown(ctx)
	}()

	err := srv.ListenAndServe()
	log.Printf("ListenAndServe 退出: %v", err)
	if err == http.ErrServerClosed {
		return nil
	}
	return err
}

// ---------- 运行状态管理（P0-4）----------

// tryStartSync 抢占同步执行权。返回 false 表示已有任务在运行。
func (s *Server) tryStartSync(mode msync.Mode) bool {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	if s.running {
		return false
	}
	s.running = true
	s.current = &runInfo{startedAt: time.Now(), mode: mode}
	return true
}

// finishSync 释放同步执行权。
func (s *Server) finishSync() {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	s.running = false
	s.current = nil
}

// isRunning 报告当前是否有同步任务在执行（用于拒绝执行中改配置等）。
func (s *Server) isRunning() bool {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	return s.running
}

// snapshot 返回当前任务信息副本（用于 409 响应），无任务时返回 nil。
func (s *Server) snapshot() *runInfo {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	if s.current == nil {
		return nil
	}
	c := *s.current
	return &c
}

// ---------- 计划存储（P1-2）----------

// storePlan 保存不可变计划，并清理过期计划。
func (s *Server) storePlan(p *msync.Plan) {
	s.planMu.Lock()
	defer s.planMu.Unlock()
	// 清理过期计划
	for id, pl := range s.plans {
		if pl.Expired() {
			delete(s.plans, id)
		}
	}
	s.plans[p.ID] = p
}

// getPlan 查找计划但不删除（P1-2：危险确认需重试同一计划）。
func (s *Server) getPlan(id string) *msync.Plan {
	s.planMu.Lock()
	defer s.planMu.Unlock()
	return s.plans[id]
}

// removePlan 执行完成后删除计划，防止重放。
func (s *Server) removePlan(id string) {
	s.planMu.Lock()
	defer s.planMu.Unlock()
	delete(s.plans, id)
}

// handleMeta 返回运行平台等元信息（前端据此决定展示哪个平台的路径输入框）。
func (s *Server) handleMeta(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]string{"platform": runtime.GOOS})
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	data, err := staticFS.ReadFile("static/index.html")
	if err != nil {
		http.Error(w, "internal", 500)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")
	w.Write(data)
}
