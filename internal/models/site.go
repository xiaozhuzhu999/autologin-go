package models

// Site 站点配置（与 Python 版 sites.json 结构完全对齐）
type Site struct {
	ID              int          `json:"id"`
	Name            string       `json:"name"`
	URL             string       `json:"url"`
	Username        string       `json:"username"`
	Password        string       `json:"password"`
	OtpMode         string       `json:"otp_mode"`     // none | manual | totp | fixed
	CaptchaMode     string       `json:"captcha_mode"` // auto | manual_input | wait_manual | none
	VpnEnabled      bool         `json:"vpn_enabled"`
	VpnConfig       VpnConfig    `json:"vpn_config"`
	Selectors       Selectors    `json:"selectors"`
	LastLoginAt     *string      `json:"last_login_at"`
	LastLoginStatus *string      `json:"last_login_status"`
}

type VpnConfig struct {
	ExePath      string `json:"exe_path"`
	WindowTitle  string `json:"window_title"`
	ConnectWait  int    `json:"connect_wait"`
}

type Selectors struct {
	UsernameInput string `json:"username_input"`
	PasswordInput string `json:"password_input"`
	OtpInput      string `json:"otp_input"`
	CaptchaInput  string `json:"captcha_input"`
	CaptchaImg    string `json:"captcha_img"`
	SubmitButton  string `json:"submit_button"`
}

// GUIConfig 界面配置
type GUIConfig struct {
	LastSite int     `json:"last_site"`
	Theme    string  `json:"theme"`
	Scale    float64 `json:"scale"`
}

// LoginLog 登录日志
type LoginLog struct {
	ID        int    `json:"id"`
	SiteID    int    `json:"site_id"`
	Status    string `json:"status"`
	Message   string `json:"message"`
	CreatedAt string `json:"created_at"`
}

// LoginStep 登录步骤状态
type LoginStep struct {
	Index int    `json:"index"`
	Name  string `json:"name"`
	State string `json:"state"` // pending | active | done
}

// LoginMessage 实时日志消息（SSE 推送）
type LoginMessage struct {
	Type    string `json:"type"`    // log | step | success | failed | input_request
	Content string `json:"content"`
	Step    *LoginStep `json:"step,omitempty"`
}

// DefaultSite 返回新建站点的默认值
func DefaultSite() Site {
	return Site{
		Name:        "",
		URL:         "",
		Username:    "",
		Password:    "",
		OtpMode:     "none",
		CaptchaMode: "auto",
		VpnEnabled:  false,
		VpnConfig: VpnConfig{
			ExePath:     "",
			WindowTitle: "",
			ConnectWait: 20,
		},
		Selectors: Selectors{
			UsernameInput: "input[type='text'], input[name='username'], input[id='username']",
			PasswordInput: "input[type='password'], input[name='password'], input[id='password']",
			OtpInput:      "input[name='totp'], input[name='otp'], input[name='code']",
			CaptchaInput:  "input[name='captcha'], input[placeholder*='验证码']",
			CaptchaImg:    "img.captcha, .captcha img",
			SubmitButton:  "button[type='submit'], .login-btn, button:has-text('登录')",
		},
	}
}
