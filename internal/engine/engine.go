package engine

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	neturl "net/url"

	"github.com/chromedp/chromedp"

	"autologin-go/internal/models"
)

// FindBrowserPath 查找系统已安装的 Chrome 或 Edge 浏览器路径
func FindBrowserPath() string {
	candidates := []string{
		`C:\Program Files\Google\Chrome\Application\chrome.exe`,
		`C:\Program Files (x86)\Google\Chrome\Application\chrome.exe`,
		`C:\Program Files (x86)\Microsoft\Edge\Application\msedge.exe`,
		`C:\Program Files\Microsoft\Edge\Application\msedge.exe`,
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

// LogFunc 日志回调
type LogFunc func(level, msg string)

// StepFunc 步骤进度回调
type StepFunc func(index int, name, state string)

// Engine 登录引擎
type Engine struct {
	Headless bool
	SlowMo   time.Duration
	goOCR    *GoOCRProvider
}

// NewEngine 创建默认引擎
func NewEngine() *Engine {
	return &Engine{
		Headless: false,
		SlowMo:   100 * time.Millisecond,
	}
}

// Login 执行登录流程
func (e *Engine) Login(ctx context.Context, site models.Site, log LogFunc, step StepFunc) error {
	// ============================================================
	//  Step 1: VPN 连接
	// ============================================================
	if site.VpnEnabled {
		step(0, "VPN 连接", "active")
		log("info", fmt.Sprintf("正在连接 VPN: %s", site.VpnConfig.ExePath))
		err := ConnectVPN(site.VpnConfig.ExePath, site.VpnConfig.WindowTitle, site.VpnConfig.ConnectWait)
		if err != nil {
			step(0, "VPN 连接", "done")
			log("error", fmt.Sprintf("VPN 连接失败: %v", err))
			return fmt.Errorf("VPN 连接失败: %w", err)
		}
		step(0, "VPN 连接", "done")
		log("info", "VPN 连接成功")
	} else {
		step(0, "VPN 连接", "done")
		log("info", "无需 VPN，跳过")
	}

	// ============================================================
	//  Step 2: 启动浏览器并导航到登录页
	// ============================================================
	step(1, "打开登录页", "active")
	log("info", fmt.Sprintf("正在打开: %s", site.URL))

	allocOpts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", false),
		chromedp.Flag("ignore-certificate-errors", "true"),
		chromedp.Flag("disable-web-security", "true"),
		chromedp.Flag("disable-popup-blocking", "true"),
		chromedp.UserAgent("Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36"),
	)

	// 设置浏览器路径（Chrome 或 Edge）
	if browserPath := FindBrowserPath(); browserPath != "" {
		allocOpts = append(allocOpts, chromedp.ExecPath(browserPath))
	}

	allocCtx, cancel := chromedp.NewExecAllocator(ctx, allocOpts...)
	browserCtx, cancelBrowser := chromedp.NewContext(allocCtx)

	// 使用较长超时（30 分钟），让用户有足够时间在登录后的页面上操作
	browserCtx, cancelTimeout := context.WithTimeout(browserCtx, 30*time.Minute)

	// 浏览器清理控制：成功时异步清理（保持打开），失败时立即清理
	browserKeepOpen := false
	defer func() {
		if !browserKeepOpen {
			cancelTimeout()
			cancelBrowser()
			cancel()
		}
	}()

	err := chromedp.Run(browserCtx,
		chromedp.Navigate(site.URL),
		chromedp.Sleep(3*time.Second),
	)
	if err != nil {
		step(1, "打开登录页", "done")
		log("error", fmt.Sprintf("打开页面失败: %v", err))
		return fmt.Errorf("打开页面失败: %w", err)
	}
	step(1, "打开登录页", "done")
	log("info", "登录页已打开")

	// ============================================================
	//  Step 3: 填写用户名
	// ============================================================
	step(2, "填写用户名", "active")
	log("info", fmt.Sprintf("填写用户名: %s", site.Username))
	err = fillInput(browserCtx, site.Selectors.UsernameInput, site.Username)
	if err != nil {
		step(2, "填写用户名", "done")
		log("warning", fmt.Sprintf("用户名填写可能失败: %v", err))
	} else {
		log("info", "用户名已填写")
	}
	step(2, "填写用户名", "done")

	// ============================================================
	//  Step 4: 填写密码
	// ============================================================
	step(3, "填写密码", "active")
	log("info", "正在填写密码...")
	err = fillInput(browserCtx, site.Selectors.PasswordInput, site.Password)
	if err != nil {
		step(3, "填写密码", "done")
		log("warning", fmt.Sprintf("密码填写可能失败: %v", err))
	} else {
		log("info", "密码已填写")
	}
	step(3, "填写密码", "done")

	// ============================================================
	//  Step 5: 动态口令
	// ============================================================
	if site.OtpMode != "none" {
		step(4, "动态口令", "active")
		otp, err := e.handleOTP(site, log)
		if err != nil {
			step(4, "动态口令", "done")
			log("warning", fmt.Sprintf("动态口令处理失败: %v", err))
		} else if otp != "" && site.Selectors.OtpInput != "" {
			err = fillInput(browserCtx, site.Selectors.OtpInput, otp)
			if err != nil {
				log("warning", fmt.Sprintf("口令填写失败: %v", err))
			} else {
				log("info", "动态口令已填写")
			}
		}
		step(4, "动态口令", "done")
	} else {
		step(4, "动态口令", "done")
		log("info", "无需动态口令")
	}

	// ============================================================
	//  Step 6: 验证码识别
	// ============================================================
	if site.CaptchaMode == "auto" || site.CaptchaMode == "manual_input" {
		step(5, "验证码识别", "active")
		err = e.handleCaptcha(browserCtx, site, log)
		if err != nil {
			step(5, "验证码识别", "done")
			log("warning", fmt.Sprintf("验证码处理: %v", err))
		}
		step(5, "验证码识别", "done")
	} else {
		step(5, "验证码识别", "done")
		log("info", "无需验证码")
	}

	// ============================================================
	//  Step 7: 提交登录
	// ============================================================
	step(6, "提交登录", "active")
	log("info", "正在提交登录...")
	err = clickSubmit(browserCtx, site.Selectors.SubmitButton)
	if err != nil {
		step(6, "提交登录", "done")
		log("warning", fmt.Sprintf("提交按钮点击失败: %v", err))
	} else {
		log("info", "已点击登录按钮")
	}
	chromedp.Run(browserCtx, chromedp.Sleep(3*time.Second))
	step(6, "提交登录", "done")

	// ============================================================
	//  Step 8: 验证登录结果
	// ============================================================
	step(7, "验证结果", "active")
	log("info", "正在验证登录结果...")
	var currentURL string
	chromedp.Run(browserCtx, chromedp.Location(&currentURL))
	log("info", fmt.Sprintf("当前页面: %s", currentURL))

	step(7, "验证结果", "done")
	log("info", "登录流程完成")

	// 登录成功：保持浏览器打开，异步清理
	browserKeepOpen = true
	go func() {
		// 等待浏览器关闭（用户手动关闭或超时）
		<-browserCtx.Done()
		cancelTimeout()
		cancelBrowser()
		cancel()
	}()
	log("info", "浏览器将保持打开，可继续操作已登录的页面")

	return nil
}

// handleOTP 处理动态口令
func (e *Engine) handleOTP(site models.Site, log LogFunc) (string, error) {
	switch site.OtpMode {
	case "totp":
		log("info", "正在生成 TOTP 动态口令...")
		// 使用全局配置中的 TOTP 密钥
		// TODO: 从全局配置获取 totp_secret
		otp, err := GenerateTOTP("", "base64", 6, 30)
		if err != nil {
			return "", err
		}
		log("info", "TOTP 口令已生成")
		return otp, nil
	case "manual":
		log("info", "需要手动输入动态口令")
		return "", nil
	case "fixed":
		// 返回固定口令
		return "123456", nil
	default:
		return "", nil
	}
}

// handleCaptcha 处理验证码
func (e *Engine) handleCaptcha(ctx context.Context, site models.Site, log LogFunc) error {
	if site.Selectors.CaptchaImg == "" || site.Selectors.CaptchaInput == "" {
		log("info", "未配置验证码选择器，跳过")
		return nil
	}

	if site.CaptchaMode == "manual_input" {
		log("info", "验证码模式为手动输入，等待用户输入...")
		return nil
	}

	// 自动识别模式
	log("info", "正在获取验证码图片...")
	log("debug", fmt.Sprintf("验证码图片选择器: %s", site.Selectors.CaptchaImg))

	// 先等待页面稳定
	chromedp.Run(ctx, chromedp.Sleep(2*time.Second))

	imgBytes, err := getCaptchaImage(ctx, site.Selectors.CaptchaImg, site.URL, log)
	if err != nil || imgBytes == nil {
		log("warning", fmt.Sprintf("获取验证码图片失败: %v", err))
		return fmt.Errorf("获取验证码图片失败: %w", err)
	}
	log("info", fmt.Sprintf("验证码图片已获取，大小: %d 字节", len(imgBytes)))

	log("info", "正在识别验证码...")

	result, err := e.recognizeCaptcha(imgBytes, log)
	if err != nil {
		log("warning", fmt.Sprintf("验证码识别失败: %v", err))
		return fmt.Errorf("验证码识别失败: %w", err)
	}
	log("info", fmt.Sprintf("验证码识别结果: %s", result))

	// 填写验证码
	err = fillInput(ctx, site.Selectors.CaptchaInput, result)
	if err != nil {
		log("warning", fmt.Sprintf("验证码填写失败: %v", err))
		return fmt.Errorf("验证码填写失败: %w", err)
	}
	log("info", "验证码已填写")

	return nil
}

// recognizeCaptcha 识别验证码（仅使用 Go 原生 ONNX Runtime，无 Python 依赖）
func (e *Engine) recognizeCaptcha(imgBytes []byte, log LogFunc) (string, error) {
	if e.goOCR == nil {
		exePath, _ := os.Executable()
		exeDir := filepath.Dir(exePath)
		provider, err := GetGoOCRProvider(exeDir)
		if err != nil {
			return "", fmt.Errorf("Go 原生 OCR 初始化失败: %w", err)
		}
		e.goOCR = provider
		log("info", "使用 Go 原生 ONNX Runtime 进行 OCR 识别")
	}

	result, err := e.goOCR.Recognize(imgBytes)
	if err != nil {
		return "", fmt.Errorf("OCR 识别失败: %w", err)
	}
	return result, nil
}

// fillInput 向输入框填写内容（一次 JS 调用尝试所有选择器，兼容 React/Vue 等框架）
func fillInput(ctx context.Context, selectorStr, value string) error {
	selectors := splitSelectors(selectorStr)
	// 构建 JS 选择器数组，一次调用尝试全部
	var jsSels []string
	for _, sel := range selectors {
		sel = strings.TrimSpace(sel)
		if sel != "" {
			jsSels = append(jsSels, fmt.Sprintf("%q", sel))
		}
	}
	if len(jsSels) == 0 {
		return fmt.Errorf("无效的选择器: %s", selectorStr)
	}
	jsArray := "[" + strings.Join(jsSels, ",") + "]"

	js := fmt.Sprintf(`
		(function() {
			var selectors = %s;
			for (var i = 0; i < selectors.length; i++) {
				var el = document.querySelector(selectors[i]);
				if (el) {
					el.focus();
					var setter = Object.getOwnPropertyDescriptor(window.HTMLInputElement.prototype, 'value');
					if (!setter) setter = Object.getOwnPropertyDescriptor(window.HTMLTextAreaElement.prototype, 'value');
					if (setter && setter.set) {
						setter.set.call(el, %q);
					} else {
						el.value = %q;
					}
					el.dispatchEvent(new Event('input', { bubbles: true }));
					el.dispatchEvent(new Event('change', { bubbles: true }));
					return true;
				}
			}
			return false;
		})()
	`, jsArray, value, value)

	opCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	var result bool
	err := chromedp.Run(opCtx, chromedp.Evaluate(js, &result))
	if err != nil {
		return fmt.Errorf("填写失败: %w", err)
	}
	if !result {
		return fmt.Errorf("未找到匹配的输入框: %s", selectorStr)
	}
	return nil
}

// clickSubmit 点击提交按钮（一次 JS 调用尝试所有选择器，支持 :has-text 语法）
func clickSubmit(ctx context.Context, selectorStr string) error {
	selectors := splitSelectors(selectorStr)

	// 分离标准 CSS 选择器和 :has-text() 选择器
	var cssSels []string
	var hasTexts []string
	for _, sel := range selectors {
		sel = strings.TrimSpace(sel)
		if sel == "" {
			continue
		}
		if strings.Contains(sel, ":has-text(") {
			text := extractHasText(sel)
			if text != "" {
				hasTexts = append(hasTexts, fmt.Sprintf("%q", text))
			}
		} else {
			cssSels = append(cssSels, fmt.Sprintf("%q", sel))
		}
	}
	cssArray := "[]"
	if len(cssSels) > 0 {
		cssArray = "[" + strings.Join(cssSels, ",") + "]"
	}
	textArray := "[]"
	if len(hasTexts) > 0 {
		textArray = "[" + strings.Join(hasTexts, ",") + "]"
	}

	js := fmt.Sprintf(`
		(function() {
			var cssSelectors = %s;
			for (var i = 0; i < cssSelectors.length; i++) {
				var el = document.querySelector(cssSelectors[i]);
				if (el) { el.click(); return true; }
			}
			var texts = %s;
			var elements = document.querySelectorAll('button, input[type="submit"], a, .login-btn, .btn-login');
			for (var j = 0; j < texts.length; j++) {
				for (var k = 0; k < elements.length; k++) {
					if (elements[k].textContent && elements[k].textContent.includes(texts[j])) {
						elements[k].click();
						return true;
					}
				}
			}
			return false;
		})()
	`, cssArray, textArray)

	opCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	var result bool
	err := chromedp.Run(opCtx, chromedp.Evaluate(js, &result))
	if err != nil {
		return fmt.Errorf("点击提交按钮失败: %w", err)
	}
	if !result {
		return fmt.Errorf("未找到提交按钮: %s", selectorStr)
	}
	return nil
}

// getCaptchaImage 获取验证码图片字节数据
func getCaptchaImage(ctx context.Context, imgSelector, pageURL string, log LogFunc) ([]byte, error) {
	selectors := splitSelectors(imgSelector)
	var lastErr error

	for _, sel := range selectors {
		sel = strings.TrimSpace(sel)
		if sel == "" {
			continue
		}

		log("debug", fmt.Sprintf("尝试选择器: %s", sel))

		// 为每个选择器操作设置 10 秒超时
		opCtx, cancel := context.WithTimeout(ctx, 10*time.Second)

		// 方式1：通过 JavaScript 获取图片 src
		jsGetSrc := fmt.Sprintf(`
			(function() {
				var el = document.querySelector(%q);
				if (!el) return {found: false};
				var src = el.getAttribute("src") || el.src || "";
				var bg = window.getComputedStyle(el).backgroundImage || "";
				if (bg && bg !== "none") {
					var match = bg.match(/url\(["']?(.*?)["']?\)/);
					if (match) src = match[1];
				}
				return {found: true, src: src, tag: el.tagName};
			})()
		`, sel)

		var srcInfo struct {
			Found bool   `json:"found"`
			Src   string `json:"src"`
			Tag   string `json:"tag"`
		}
		err := chromedp.Run(opCtx, chromedp.Evaluate(jsGetSrc, &srcInfo))
		if err == nil && srcInfo.Found {
			log("debug", fmt.Sprintf("找到元素 <%s>，src=%s", srcInfo.Tag, truncate(srcInfo.Src, 80)))

			// data URI
			if strings.HasPrefix(srcInfo.Src, "data:image") {
				parts := strings.SplitN(srcInfo.Src, ",", 2)
				if len(parts) == 2 {
					imgBytes, err := base64.StdEncoding.DecodeString(parts[1])
					if err == nil && len(imgBytes) > 0 {
						cancel()
						return imgBytes, nil
					}
				}
			}

			// 相对/绝对 URL → 用浏览器 fetch 获取
			if srcInfo.Src != "" && !strings.HasPrefix(srcInfo.Src, "data:") {
				imgBytes := fetchImageViaBrowser(opCtx, srcInfo.Src, pageURL)
				if len(imgBytes) > 0 {
					cancel()
					return imgBytes, nil
				}
			}
		}

		// 方式2：canvas → toDataURL
		jsCanvas := fmt.Sprintf(`
			(function() {
				var el = document.querySelector(%q);
				if (!el || el.tagName !== 'CANVAS') return "";
				try { return el.toDataURL("image/png"); } catch(e) { return ""; }
			})()
		`, sel)
		var canvasData string
		err = chromedp.Run(opCtx, chromedp.Evaluate(jsCanvas, &canvasData))
		if err == nil && strings.HasPrefix(canvasData, "data:image") {
			parts := strings.SplitN(canvasData, ",", 2)
			if len(parts) == 2 {
				imgBytes, err := base64.StdEncoding.DecodeString(parts[1])
				if err == nil && len(imgBytes) > 0 {
					cancel()
					return imgBytes, nil
				}
			}
		}

		// 方式3：元素截图（最可靠的方式）
		var buf []byte
		err = chromedp.Run(opCtx,
			chromedp.WaitVisible(sel, chromedp.ByQuery),
			chromedp.Screenshot(sel, &buf, chromedp.ByQuery),
		)
		if err == nil && len(buf) > 0 {
			cancel()
			return buf, nil
		}
		lastErr = err
		cancel()
	}

	if lastErr == nil {
		return nil, fmt.Errorf("未找到验证码图片，选择器: %s", imgSelector)
	}
	return nil, lastErr
}

// fetchImageViaBrowser 通过浏览器 JavaScript fetch 获取图片数据
func fetchImageViaBrowser(ctx context.Context, imgSrc, pageURL string) []byte {
	// 构建完整 URL
	fullURL := imgSrc
	if strings.HasPrefix(imgSrc, "/") && !strings.HasPrefix(imgSrc, "//") {
		// 相对路径
		if u, err := neturl.Parse(pageURL); err == nil {
			fullURL = u.Scheme + "://" + u.Host + imgSrc
		}
	} else if !strings.HasPrefix(imgSrc, "http") {
		// 相对路径不带 /
		if u, err := neturl.Parse(pageURL); err == nil {
			fullURL = u.Scheme + "://" + u.Host + "/" + imgSrc
		}
	}

	js := fmt.Sprintf(`
		(async function() {
			try {
				var resp = await fetch(%q, {credentials: 'include'});
				var blob = await resp.blob();
				return await new Promise(function(resolve) {
					var reader = new FileReader();
					reader.onloadend = function() { resolve(reader.result); };
					reader.readAsDataURL(blob);
				});
			} catch(e) { return ""; }
		})()
	`, fullURL)

	var dataURL string
	err := chromedp.Run(ctx, chromedp.Evaluate(js, &dataURL))
	if err != nil || !strings.HasPrefix(dataURL, "data:") {
		return nil
	}
	parts := strings.SplitN(dataURL, ",", 2)
	if len(parts) != 2 {
		return nil
	}
	imgBytes, err := base64.StdEncoding.DecodeString(parts[1])
	if err != nil {
		return nil
	}
	return imgBytes
}

// truncate 截断字符串
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// splitSelectors 将逗号分隔的选择器字符串拆分为列表
func splitSelectors(s string) []string {
	return strings.Split(s, ",")
}

// extractHasText 从 :has-text('xxx') 语法中提取文本
func extractHasText(sel string) string {
	idx := strings.Index(sel, ":has-text(")
	if idx < 0 {
		return ""
	}
	start := idx + len(":has-text(")
	end := strings.LastIndex(sel, ")")
	if end <= start {
		return ""
	}
	text := sel[start:end]
	text = strings.Trim(text, "'\"")
	return text
}
