// MiaoSync — 你的 Win / Mac / 移动硬盘三端同步备份猫
//
// 入口占位：当前仅打印 banner 与基础子命令路由骨架。
// 真正的同步逻辑见 internal/sync（设计见 docs/design.md）。
package main

import (
	"fmt"
	"os"
)

func main() {
	if len(os.Args) < 2 {
		printBanner()
		fmt.Println("用法: miaosync <command> [flags]")
		fmt.Println("命令: init | sync | status | restore | version")
		return
	}

	cmd := os.Args[1]
	switch cmd {
	case "version":
		fmt.Println("MiaoSync v0.0.0 (skeleton)")
	case "init", "sync", "status", "restore":
		fmt.Printf("[skeleton] 子命令 '%s' 尚未实现，详见 docs/design.md\n", cmd)
	default:
		fmt.Printf("未知命令: %s\n", cmd)
		os.Exit(1)
	}
}

func printBanner() {
	fmt.Println(`  /\_/\
 ( o.o )  MiaoSync 🐱
  > ^ <   Win / Mac / 移动硬盘 三端同步备份`)
}
