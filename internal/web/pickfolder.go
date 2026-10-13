package web

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

type pickFolderReq struct {
	InitialPath string `json:"initialPath"`
}

// handlePickFolder 弹出系统原生文件夹选择对话框，返回选中的绝对路径。
// 用户取消时返回空 path（视为“未选择”），不报错。
func (s *Server) handlePickFolder(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req pickFolderReq
	_ = decodeBody(w, r, &req)

	path, err := pickFolder(req.InitialPath)
	if err != nil {
		// 用户取消或无法打开对话框：返回空路径，由前端决定是否提示。
		writeJSON(w, map[string]string{"path": ""})
		return
	}
	writeJSON(w, map[string]string{"path": path})
}

// pickFolder 按运行平台调用原生文件夹选择对话框，5 分钟超时自动放弃。
func pickFolder(initialPath string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		// 1. [Console]::OutputEncoding 保证输出 UTF-8
		// 2. Base64 编码传递路径，彻底杜绝中文目录乱码
		// 3. TopMost 隐藏窗体确保对话框前置浮现
		// 4. 若传入 initialPath 则直接定位，避免全盘枚举网络/驱动器造成的卡顿
		initCmd := ""
		cleanInit := strings.ReplaceAll(initialPath, "'", "''")
		if cleanInit != "" {
			initCmd = fmt.Sprintf("if (Test-Path '%s') { $d.SelectedPath = '%s' }; ", cleanInit, cleanInit)
		}
		psScript := "[Console]::OutputEncoding = [System.Text.Encoding]::UTF8; " +
			"Add-Type -AssemblyName System.Windows.Forms; " +
			"$f = New-Object System.Windows.Forms.Form; " +
			"$f.TopMost = $true; " +
			"$f.StartPosition = 'Manual'; " +
			"$f.Location = New-Object System.Drawing.Point(-32000, -32000); " +
			"$f.Show(); " +
			"$f.Activate(); " +
			"$f.BringToFront(); " +
			"$d = New-Object System.Windows.Forms.FolderBrowserDialog; " +
			"$d.Description = '请选择同步文件夹'; " +
			"$d.ShowNewFolderButton = $true; " +
			"$d.RootFolder = [System.Environment+SpecialFolder]::MyComputer; " +
			initCmd +
			"$res = $d.ShowDialog($f); " +
			"$f.Close(); " +
			"$f.Dispose(); " +
			"if ($res -eq [System.Windows.Forms.DialogResult]::OK) { " +
			"  [Convert]::ToBase64String([System.Text.Encoding]::UTF8.GetBytes($d.SelectedPath)) " +
			"}"

		cmd = exec.CommandContext(ctx, "powershell.exe",
			"-NoProfile", "-NonInteractive", "-WindowStyle", "Hidden", "-STA",
			"-Command", psScript)
		setCmdNoWindow(cmd)
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
	raw := strings.TrimSpace(string(out))
	if raw == "" {
		return "", fmt.Errorf("未选择文件夹")
	}
	if runtime.GOOS == "windows" {
		decoded, decErr := base64.StdEncoding.DecodeString(raw)
		if decErr == nil && len(decoded) > 0 {
			return string(decoded), nil
		}
	}
	return raw, nil
}
