package engine

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
	"unsafe"
)

var (
	user32               = syscall.NewLazyDLL("user32.dll")
	procEnumWindows      = user32.NewProc("EnumWindows")
	procGetWindowTextW   = user32.NewProc("GetWindowTextW")
	procGetWindowTextLen = user32.NewProc("GetWindowTextLengthW")
	procSetForegroundWnd = user32.NewProc("SetForegroundWindow")
	procIsWindowVisible  = user32.NewProc("IsWindowVisible")
)

// ConnectVPN 启动 VPN 客户端并等待连接
func ConnectVPN(exePath, windowTitle string, waitSeconds int) error {
	if exePath == "" {
		return fmt.Errorf("未配置 VPN 客户端路径")
	}
	if _, err := os.Stat(exePath); os.IsNotExist(err) {
		return fmt.Errorf("找不到 VPN 客户端: %s", exePath)
	}

	// 检查 VPN 窗口是否已打开
	if findWindow(windowTitle) != 0 {
		// 窗口已存在，说明 VPN 可能已连接
		return nil
	}

	// 启动 VPN 客户端
	dir := filepath.Dir(exePath)
	cmd := exec.Command(exePath)
	cmd.Dir = dir
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: false}
	err := cmd.Start()
	if err != nil {
		return fmt.Errorf("启动 VPN 客户端失败: %w", err)
	}

	// 等待窗口出现
	deadline := time.Now().Add(time.Duration(waitSeconds) * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(1 * time.Second)
		if findWindow(windowTitle) != 0 {
			// 窗口出现，再等待连接完成
			time.Sleep(3 * time.Second)
			return nil
		}
	}

	return fmt.Errorf("VPN 客户端启动超时，未检测到窗口: %s", windowTitle)
}

// findWindow 查找指定标题的窗口
func findWindow(title string) uintptr {
	if title == "" {
		return 0
	}
	var found uintptr

	cb := syscall.NewCallback(func(hwnd uintptr, lparam uintptr) uintptr {
		length, _, _ := procGetWindowTextLen.Call(hwnd)
		if length == 0 {
			return 1
		}
		buf := make([]uint16, length+1)
		procGetWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(length+1))
		windowTitle := syscall.UTF16ToString(buf)
		if windowTitle == title {
			found = hwnd
			return 0
		}
		return 1
	})

	procEnumWindows.Call(cb, 0)
	return found
}
