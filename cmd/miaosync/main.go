// MiaoSync — 你的 Win / Mac / 移动硬盘 三端同步备份猫
//
// 启动内置 Web GUI（单页应用），自动打开浏览器，无需任何外部依赖。
package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"time"

	"github.com/yuyeming0115/MiaoSync/internal/web"
)

func main() {
	// 日志同时写入文件，便于无控制台（windowsgui 子系统）构建时排查问题。
	if exe, err := os.Executable(); err == nil {
		logPath := filepath.Join(filepath.Dir(exe), "miaosync.log")
		if f, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644); err == nil {
			defer f.Close()
			log.SetOutput(io.MultiWriter(f, os.Stderr))
		}
	}

	configPath := flag.String("config", "miaosync.yaml", "配置文件路径（不存在则写入默认配置）")
	port := flag.String("port", "7480", "Web 服务监听端口")
	noOpen := flag.Bool("no-open", false, "不自动打开浏览器")
	flag.Parse()

	server, err := web.New(*configPath)
	if err != nil {
		fatalExit("初始化失败", err)
	}

	addr := "127.0.0.1:" + *port
	url := "http://" + addr + "/"

	// 已有实例在运行：双击重复启动时直接打开其界面后退出，而不是无声失败。
	conn, dialErr := net.DialTimeout("tcp", addr, 600*time.Millisecond)
	if dialErr == nil {
		conn.Close()
		log.Printf("检测到已在运行的实例（%s），打开界面后退出", addr)
		if !*noOpen {
			if err := openURL(url); err != nil {
				fatalExit("无法打开浏览器", err)
			}
		}
		return
	}

	log.Printf("🐱 MiaoSync 已启动：%s（配置：%s，Ctrl+C 退出）", url, *configPath)

	if !*noOpen {
		go func() {
			// 稍等片刻让监听就绪，再拉起浏览器
			time.Sleep(400 * time.Millisecond)
			if err := openURL(url); err != nil {
				log.Printf("打开浏览器失败: %v", err)
				fatalBox("MiaoSync 需要你的注意",
					"服务已启动，但自动打开浏览器失败。\n请手动复制以下地址到浏览器访问：\n\n"+url)
			}
		}()
	}

	if err := server.Start(addr); err != nil {
		fatalExit("服务异常", err)
	}
}

// fatalExit 记录日志、弹出原生消息框（Windows）后以非零码退出。
// windowsgui 构建没有控制台，直接 os.Exit 会让用户以为"双击没反应"。
func fatalExit(title string, err error) {
	log.Printf("%s: %v", title, err)
	fatalBox("MiaoSync - "+title,
		fmt.Sprintf("%v\n\n若程序目录可写，同目录 miaosync.log 内有详细记录。", err))
	os.Exit(1)
}
