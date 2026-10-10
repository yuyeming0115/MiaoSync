package web

import (
	"sync"
	"testing"

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
