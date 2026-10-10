package web

import (
	"context"
	"fmt"
	"net/http"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// handlePickFolder 弹出系统原生文件夹选择对话框，返回选中的绝对路径。
// 用户取消时返回空 path（视为“未选择”），不报错。
func (s *Server) handlePickFolder(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	path, err := pickFolder()
	if err != nil {
		// 用户取消或无法打开对话框：返回空路径，由前端决定是否提示。
		writeJSON(w, map[string]string{"path": ""})
		return
	}
	writeJSON(w, map[string]string{"path": path})
}

// pickFolder 按运行平台调用原生文件夹选择对话框，5 分钟超时自动放弃。
func pickFolder() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		// -STA 是 WinForms 对话框必需的单线程单元；-WindowStyle Hidden 避免闪现控制台。
		cmd = exec.CommandContext(ctx, "powershell.exe",
			"-NoProfile", "-NonInteractive", "-WindowStyle", "Hidden", "-STA",
			"-Command",
			"Add-Type -AssemblyName System.Windows.Forms; "+
				"$d=New-Object System.Windows.Forms.FolderBrowserDialog; "+
				"$d.Description='选择同步文件夹'; "+
				"$d.ShowNewFolderButton=$true; "+
				"if($d.ShowDialog() -eq [System.Windows.Forms.DialogResult]::OK){$d.SelectedPath}")
	case "darwin":
		cmd = exec.CommandContext(ctx, "osascript", "-e",
			`POSIX path of (choose folder with prompt "选择同步文件夹")`)
	default:
		cmd = exec.CommandContext(ctx, "zenity", "--file-selection", "--directory",
			"--title=选择同步文件夹", "--filename=$HOME/")
	}

	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("打开选择对话框失败: %w", err)
	}
	path := strings.TrimSpace(string(out))
	if path == "" {
		return "", fmt.Errorf("未选择文件夹")
	}
	return path, nil
}
