// MiaoSync — 你的 Win / Mac / 移动硬盘 三端同步备份猫
//
// 启动内置 Web GUI（单页应用），自动打开浏览器，无需任何外部依赖。
package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/yuyeming0115/MiaoSync/internal/web"
)

func main() {
	// 日志同时写入文件，便于无控制台（windowsgui 子系统）构建时排查问题。
	if exe, err := os.Executable(); err == nil {
		logPath := filepath.Join(filepath.Dir(exe), "miaosync.log")
		if f, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644); err == nil {
			log.SetOutput(io.MultiWriter(f, os.Stderr))
		}
	}

	configPath := flag.String("config", "miaosync.yaml", "配置文件路径（不存在则写入默认配置）")
	port := flag.String("port", "7480", "Web 服务监听端口")
	noOpen := flag.Bool("no-open", false, "不自动打开浏览器")
	flag.Parse()

	server, err := web.New(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "初始化失败: %v\n", err)
		os.Exit(1)
	}

	addr := "127.0.0.1:" + *port
	url := "http://" + addr + "/"
	fmt.Println("🐱 MiaoSync 已启动")
	fmt.Printf("   浏览器打开：%s\n", url)
	fmt.Printf("   配置文件：%s\n", *configPath)
	fmt.Println("   按 Ctrl+C 退出。")

	if !*noOpen {
		go openBrowser(url)
	}

	if err := server.Start(addr); err != nil {
		fmt.Fprintf(os.Stderr, "服务异常: %v\n", err)
		os.Exit(1)
	}
}

// openBrowser 跨平台打开默认浏览器。
func openBrowser(url string) {
	var cmd string
	var args []string
	switch runtime.GOOS {
	case "windows":
		cmd = "cmd"
		args = []string{"/c", "start", "", url}
	case "darwin":
		cmd = "open"
		args = []string{url}
	default:
		cmd = "xdg-open"
		args = []string{url}
	}
	_ = exec.Command(cmd, args...).Start()
}
