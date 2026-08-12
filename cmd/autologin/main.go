package main

import (
	"embed"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"autologin-go/internal/api"
	"autologin-go/internal/db"
	"autologin-go/internal/engine"
	"autologin-go/internal/models"
)

//go:embed all:web
var webFS embed.FS

// 固定端口，避免随机端口带来的问题
const fixedPort = 18899

var logFile *os.File

func main() {
	// 捕获 panic，写入日志
	defer func() {
		if r := recover(); r != nil {
			log.Printf("!!! 程序崩溃: %v", r)
			if logFile != nil {
				logFile.Sync()
			}
		}
	}()

	// 命令行参数
	headless := flag.Bool("headless", false, "以无界面模式运行（仅 API 服务）")
	port := flag.Int("port", fixedPort, "API 服务端口")
	flag.Parse()

	// 将日志写入文件（GUI 模式下无控制台输出）
	exePath, err := os.Executable()
	if err != nil {
		// 如果连路径都拿不到，直接用当前目录
		exePath = "."
	}
	exeDir := filepath.Dir(exePath)
	logFile, err = os.OpenFile(filepath.Join(exeDir, "autologin.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err == nil {
		log.SetOutput(logFile)
		defer logFile.Close()
	}
	log.Printf("========== AutoLogin Pro 启动 ==========")
	log.Printf("exe 路径: %s", exePath)
	log.Printf("工作目录: %s", exeDir)
	log.Printf("参数: headless=%v port=%d", *headless, *port)

	// 初始化数据库
	dbPath := filepath.Join(exeDir, "autologin.db")
	log.Printf("数据库路径: %s", dbPath)
	if err := db.Init(); err != nil {
		log.Printf("数据库初始化失败: %v", err)
		if *headless {
			os.Exit(1)
		}
		// GUI 模式下，弹窗提示后退出
		showErrorDialog("数据库初始化失败", fmt.Sprintf("%v", err))
		return
	}
	log.Printf("数据库初始化成功")

	// 播种默认站点（首次运行）
	seedDefaultSites()
	log.Printf("站点初始化完成")

	// 创建 API 服务
	server := api.NewServer()
	mux := http.NewServeMux()
	server.RegisterRoutes(mux)

	// 静态文件服务（前端）
	webRoot, err := fs.Sub(webFS, "web")
	if err != nil {
		log.Fatalf("前端资源加载失败: %v", err)
	}
	mux.Handle("/", http.FileServer(http.FS(webRoot)))
	log.Printf("路由注册完成")

	// 监听端口（如果固定端口被占用，尝试下一个）
	actualPort := *port
	var listener net.Listener
	for i := 0; i < 20; i++ {
		listener, err = net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", actualPort))
		if err == nil {
			break
		}
		log.Printf("端口 %d 被占用，尝试下一个", actualPort)
		actualPort++
	}
	if listener == nil {
		log.Printf("无法找到可用端口")
		if !*headless {
			showErrorDialog("启动失败", "无法找到可用端口")
		}
		return
	}
	addr := fmt.Sprintf("127.0.0.1:%d", actualPort)
	url := fmt.Sprintf("http://%s/", addr)
	log.Printf("API 服务地址: %s", url)

	// 优雅退出通道
	quitCh := make(chan struct{})

	// 注册 /api/quit 端点（支持 POST 和 GET，兼容 sendBeacon）
	quitHandler := func(w http.ResponseWriter, r *http.Request) {
		log.Printf("收到退出请求 (%s)", r.Method)
		w.WriteHeader(http.StatusOK)
		go func() {
			time.Sleep(300 * time.Millisecond)
			close(quitCh)
		}()
	}
	mux.HandleFunc("POST /api/quit", quitHandler)
	mux.HandleFunc("GET /api/quit", quitHandler)

	if *headless {
		log.Printf("以 headless 模式运行")
		log.Fatal(http.Serve(listener, mux))
		return
	}

	// ============= GUI 模式 =============

	// 启动 HTTP 服务（后台）
	serverDone := make(chan struct{})
	go func() {
		err := http.Serve(listener, mux)
		log.Printf("HTTP 服务结束: %v", err)
		close(serverDone)
	}()

	// 健康检查：等待服务就绪
	ready := false
	for i := 0; i < 30; i++ {
		time.Sleep(100 * time.Millisecond)
		resp, err := http.Get(url)
		if err == nil {
			resp.Body.Close()
			ready = true
			log.Printf("服务健康检查通过 (尝试 %d 次)", i+1)
			break
		}
	}
	if !ready {
		log.Printf("警告: 服务健康检查超时（3秒），仍尝试打开浏览器")
	}

	// 使用 Chrome 或 Edge 以 app 模式打开桌面窗口
	browserPath := engine.FindBrowserPath()
	if browserPath == "" {
		log.Printf("未找到 Chrome 或 Edge，尝试使用系统默认浏览器")
		openBrowser(url)
		<-quitCh
		return
	}
	log.Printf("浏览器路径: %s", browserPath)

	// 使用唯一的用户数据目录，避免 Chrome 复用已有实例导致进程立即退出
	// 每次启动使用时间戳，确保创建全新的 Chrome 进程
	userDataDir := filepath.Join(os.TempDir(), fmt.Sprintf("autologin-chrome-%d", time.Now().UnixNano()))
	log.Printf("Chrome 用户数据目录: %s", userDataDir)

	// 以 app 模式启动浏览器（无边框，类似原生窗口）
	cmd := exec.Command(browserPath,
		fmt.Sprintf("--app=%s", url),
		"--window-size=1200,780",
		fmt.Sprintf("--user-data-dir=%s", userDataDir),
		"--disable-extensions",
		"--no-first-run",
		"--no-default-browser-check",
		"--disable-translate",
		"--new-window",
	)

	err = cmd.Start()
	if err != nil {
		log.Printf("浏览器启动失败: %v", err)
		openBrowser(url)
		<-quitCh
		return
	}

	log.Printf("浏览器进程已启动 (PID: %d)", cmd.Process.Pid)

	// Chrome 在 Windows 上的行为：初始进程可能很快退出，但浏览器窗口仍在前台运行
	// 因此不能依赖 cmd.Wait() 判断窗口是否关闭
	// 改为：只在用户点击"退出"按钮（quitCh）或 HTTP 服务意外终止（serverDone）时退出
	go func() { cmd.Wait() }() // 回收进程资源，但不作为退出信号

	select {
	case <-quitCh:
		log.Printf("收到退出信号，关闭浏览器")
		// 尝试关闭 Chrome 窗口
		cmd.Process.Kill()
		time.Sleep(200 * time.Millisecond)
	case <-serverDone:
		log.Printf("HTTP 服务意外终止")
		cmd.Process.Kill()
	}

	// 清理临时用户数据目录
	go func() {
		time.Sleep(2 * time.Second)
		os.RemoveAll(userDataDir)
	}()

	// 关闭 Go 原生 OCR 引擎
	engine.CloseGoOCR()

	log.Printf("========== AutoLogin Pro 退出 ==========")
	if logFile != nil {
		logFile.Sync()
	}
}

// seedDefaultSites 首次运行时播种默认站点
func seedDefaultSites() {
	if db.IsInitialized() {
		return
	}
	sites, _ := db.LoadSites()
	if len(sites) > 0 {
		db.SetInitialized()
		return
	}

	defaults := defaultSites()
	for _, site := range defaults {
		db.AddSite(site)
	}
	db.SetInitialized()
	log.Printf("已播种 %d 个默认站点", len(defaults))
}

// openBrowser 使用系统默认浏览器打开 URL
func openBrowser(url string) {
	exec.Command("cmd", "/c", "start", url).Start()
}

// showErrorDialog 显示错误对话框（GUI 模式）
func showErrorDialog(title, message string) {
	// 使用 PowerShell 弹窗，无需额外依赖
	ps := fmt.Sprintf(`Add-Type -AssemblyName System.Windows.Forms; [System.Windows.Forms.MessageBox]::Show('%s', '%s', 'OK', 'Error')`,
		escapePS(message), escapePS(title))
	exec.Command("powershell", "-Command", ps).Start()
}

func escapePS(s string) string {
	return strings.ReplaceAll(s, "'", "''")
}

// defaultSites 返回默认站点列表
func defaultSites() []models.Site {
	return []models.Site{
		{
			Name:        "阿里云云盾堡垒机",
			URL:         "https://example.com/login",
			Username:    "your_username",
			Password:    "your_password",
			OtpMode:     "manual",
			CaptchaMode: "auto",
			VpnEnabled:  false,
			Selectors: models.Selectors{
				UsernameInput: "input[name='ga_username']",
				PasswordInput: "input[name='ga_pwd']",
				OtpInput:      "input[name='ga_totp']",
				CaptchaInput:  "input[name='ga_captcha']",
				CaptchaImg:    "img.capimg, div.capimg img",
				SubmitButton:  "button[type='submit'], input[type='submit'], .login-btn",
			},
		},
		{
			Name:        "国资云",
			URL:         "https://example.com/login",
			Username:    "your_username",
			Password:    "your_password",
			OtpMode:     "none",
			CaptchaMode: "auto",
			VpnEnabled:  false,
			Selectors: models.Selectors{
				UsernameInput: "input[type='text'], input[name='username'], input[id='username']",
				PasswordInput: "input[type='password'], input[name='password'], input[id='password']",
				OtpInput:      "input[name='totp'], input[name='otp'], input[name='code']",
				CaptchaInput:  "#verifyCode input, input[name='verifyCode'], input[placeholder*='验证码']",
				CaptchaImg:    "div.code-image img, .code-image img, img.captcha",
				SubmitButton:  "button.ant-btn-primary, button[type='submit'], .login-btn, .btn-login, button:has-text('登录'), button:has-text('登 录')",
			},
		},
		{
			Name:        "图书馆",
			URL:         "https://example.com/login",
			Username:    "your_username",
			Password:    "your_password",
			OtpMode:     "none",
			CaptchaMode: "auto",
			VpnEnabled:  true,
			VpnConfig: models.VpnConfig{
				ExePath:     `C:\Program Files (x86)\VONE\TopSecSV\SV_Client.exe`,
				WindowTitle: "SV独立客户端",
				ConnectWait: 20,
			},
			Selectors: models.Selectors{
				UsernameInput: "input[type='text'], input[name='username'], input[id='username'], input[name='userName']",
				PasswordInput: "input[type='password'], input[name='password'], input[id='password'], input[name='passWord']",
				OtpInput:      "input[name='totp'], input[name='otp'], input[name='code']",
				CaptchaInput:  "input[name='captcha'], input[name='verifycode'], input[placeholder*='验证码']",
				CaptchaImg:    "div.b_c img, img.captcha, img.verify, div.captcha img",
				SubmitButton:  "button[type='submit'], input[type='submit'], .login-btn, .btn-login, button:has-text('登录'), button:has-text('登 录')",
			},
		},
	}
}
