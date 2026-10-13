package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/yuyeming0115/MiaoSync/internal/config"
	msync "github.com/yuyeming0115/MiaoSync/internal/sync"
)

// TestConcurrentSyncReturnsConflict 验证 P0-4：
// 同一时间只允许一个同步任务；第二个抢占请求应被拒绝（返回 false）。
func TestConcurrentSyncReturnsConflict(t *testing.T) {
	s := &Server{}

	// 第一个抢占成功
	if !s.tryStartSync(msync.ModeMirror) {
		t.Fatal("首次抢占应成功")
	}
	// 期间 isRunning 应为 true
	if !s.isRunning() {
		t.Error("运行中 isRunning 应为 true")
	}
	// 第二个抢占应失败
	if s.tryStartSync(msync.ModeMirror) {
		t.Error("运行中第二次抢占应失败")
	}
	// 释放后再次抢占应成功
	s.finishSync()
	if !s.tryStartSync(msync.ModeMirror) {
		t.Error("释放后抢占应成功")
	}
	s.finishSync()
}

// TestConcurrentSyncUnderRace 并发多次 tryStartSync，确保只有一个成功。
func TestConcurrentSyncUnderRace(t *testing.T) {
	s := &Server{}
	var wg sync.WaitGroup
	var success int64
	var mu sync.Mutex
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if s.tryStartSync(msync.ModeMirror) {
				mu.Lock()
				success++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if success != 1 {
		t.Errorf("并发抢占应仅 1 次成功，实际 %d", success)
	}
}

// TestPlanBackupAutoAdaptsPeerRoles 验证核心修复：
// 当配置中所有节点角色为 peer 时，请求 backup 模式不再报错，
// 而是自动推导第一个为 source，第二个为 target 并成功返回 plan。
func TestPlanBackupAutoAdaptsPeerRoles(t *testing.T) {
	base := t.TempDir()
	srcDir := filepath.Join(base, "win-pc")
	dstDir := filepath.Join(base, "hdd")
	os.MkdirAll(srcDir, 0o755)
	os.MkdirAll(dstDir, 0o755)

	srv := &Server{
		config: &config.Config{
			Nodes: []config.NodeCfg{
				{Name: "win-pc", WinPath: srcDir, Role: "peer"},
				{Name: "hdd", WinPath: dstDir, Role: "peer"},
			},
			Mode:     msync.ModeMirror,
			Conflict: msync.ConflictRename,
		},
		plans:  map[string]*msync.Plan{},
		engine: msync.New(),
	}

	body := `{"nodes":["win-pc","hdd"],"mode":"backup","conflict":"rename"}`
	req := httptest.NewRequest("POST", "/api/plan", strings.NewReader(body))
	rec := httptest.NewRecorder()

	srv.handlePlan(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("预期 200 OK，实际 %d, 响应: %s", rec.Code, rec.Body.String())
	}
	var res map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	if res["planId"] == "" {
		t.Errorf("应成功返回 planId")
	}
}

// TestHandleDrivesReturnsList 验证 drives 接口正常返回。
func TestHandleDrivesReturnsList(t *testing.T) {
	srv := &Server{}
	req := httptest.NewRequest("GET", "/api/drives", nil)
	rec := httptest.NewRecorder()

	srv.handleDrives(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("预期 200 OK，实际 %d", rec.Code)
	}
}
