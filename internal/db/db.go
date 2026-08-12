package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"

	"autologin-go/internal/models"
)

// 数据库文件路径（exe 同级目录）
var dbPath string

// 旧 JSON 文件路径（用于自动迁移）
var sitesJSONPath string
var guiConfigJSONPath string

// Init 初始化路径并打开/创建数据库
func Init() error {
	exePath, err := os.Executable()
	if err != nil {
		exePath, _ = os.Getwd()
	}
	dir := filepath.Dir(exePath)
	dbPath = filepath.Join(dir, "autologin.db")
	sitesJSONPath = filepath.Join(dir, "sites.json")
	guiConfigJSONPath = filepath.Join(dir, "gui_config.json")

	conn, err := open()
	if err != nil {
		return err
	}
	defer conn.Close()

	schema := `
	CREATE TABLE IF NOT EXISTS sites (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL DEFAULT '',
		url TEXT NOT NULL DEFAULT '',
		username TEXT DEFAULT '',
		password TEXT DEFAULT '',
		otp_mode TEXT DEFAULT 'none',
		captcha_mode TEXT DEFAULT 'auto',
		vpn_enabled INTEGER DEFAULT 0,
		vpn_exe_path TEXT DEFAULT '',
		vpn_window_title TEXT DEFAULT '',
		vpn_connect_wait INTEGER DEFAULT 20,
		selectors_json TEXT DEFAULT '{}',
		sort_order INTEGER DEFAULT 0,
		last_login_at TEXT,
		last_login_status TEXT,
		created_at TEXT DEFAULT (datetime('now', 'localtime')),
		updated_at TEXT DEFAULT (datetime('now', 'localtime'))
	);
	CREATE TABLE IF NOT EXISTS gui_config (
		key TEXT PRIMARY KEY,
		value TEXT
	);
	CREATE TABLE IF NOT EXISTS login_logs (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		site_id INTEGER,
		status TEXT,
		message TEXT,
		created_at TEXT DEFAULT (datetime('now', 'localtime')),
		FOREIGN KEY (site_id) REFERENCES sites(id) ON DELETE CASCADE
	);
	CREATE TABLE IF NOT EXISTS meta (
		key TEXT PRIMARY KEY,
		value TEXT
	);
	`
	_, err = conn.Exec(schema)
	if err != nil {
		return fmt.Errorf("建表失败: %w", err)
	}

	migrateFromJSON()
	return nil
}

func open() (*sql.DB, error) {
	conn, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("打开数据库失败: %w", err)
	}
	conn.Exec("PRAGMA foreign_keys = ON")
	return conn, nil
}

// ============================================================
//  JSON 自动迁移
// ============================================================

func setMeta(key, value string) {
	conn, err := open()
	if err != nil {
		return
	}
	defer conn.Close()
	conn.Exec("INSERT OR REPLACE INTO meta (key, value) VALUES (?, ?)", key, value)
}

func IsInitialized() bool {
	conn, err := open()
	if err != nil {
		return false
	}
	defer conn.Close()
	var val string
	err = conn.QueryRow("SELECT value FROM meta WHERE key = 'initialized'").Scan(&val)
	return err == nil
}

func SetInitialized() {
	setMeta("initialized", "1")
}

func migrateFromJSON() {
	// 迁移 sites.json
	if data, err := os.ReadFile(sitesJSONPath); err == nil {
		var sites []map[string]interface{}
		if err := json.Unmarshal(data, &sites); err == nil && len(sites) > 0 {
			conn, err := open()
			if err == nil {
				var count int
				conn.QueryRow("SELECT COUNT(*) FROM sites").Scan(&count)
				if count == 0 {
					for idx, s := range sites {
						insertSiteMap(conn, s, idx)
					}
				}
				conn.Close()
			}
		}
		SetInitialized()
		os.Rename(sitesJSONPath, sitesJSONPath+".bak")
	}

	// 迁移 gui_config.json
	if data, err := os.ReadFile(guiConfigJSONPath); err == nil {
		var config map[string]interface{}
		if err := json.Unmarshal(data, &config); err == nil {
			conn, err := open()
			if err == nil {
				for key, value := range config {
					valBytes, _ := json.Marshal(value)
					conn.Exec("INSERT OR REPLACE INTO gui_config (key, value) VALUES (?, ?)",
						key, string(valBytes))
				}
				conn.Close()
			}
		}
		os.Rename(guiConfigJSONPath, guiConfigJSONPath+".bak")
	}
}

// ============================================================
//  站点 CRUD
// ============================================================

// rowToSite 将数据库行映射为 Site 结构体
func rowToSite(id int, name, url, username, password, otpMode, captchaMode string,
	vpnEnabled int, vpnExePath, vpnWindowTitle string, vpnConnectWait int,
	selectorsJSON string, lastLoginAt, lastLoginStatus sql.NullString) models.Site {

	var selectors models.Selectors
	if selectorsJSON != "" {
		json.Unmarshal([]byte(selectorsJSON), &selectors)
	}

	site := models.Site{
		ID:          id,
		Name:        name,
		URL:         url,
		Username:    username,
		Password:    password,
		OtpMode:     otpMode,
		CaptchaMode: captchaMode,
		VpnEnabled:  vpnEnabled != 0,
		VpnConfig: models.VpnConfig{
			ExePath:     vpnExePath,
			WindowTitle: vpnWindowTitle,
			ConnectWait: vpnConnectWait,
		},
		Selectors: selectors,
	}
	if lastLoginAt.Valid {
		s := lastLoginAt.String
		site.LastLoginAt = &s
	}
	if lastLoginStatus.Valid {
		s := lastLoginStatus.String
		site.LastLoginStatus = &s
	}
	return site
}

func siteToFields(site models.Site) (name, url, username, password, otpMode, captchaMode string,
	vpnEnabled int, vpnExePath, vpnWindowTitle string, vpnConnectWait int, selectorsJSON string) {

	selectorsBytes, _ := json.Marshal(site.Selectors)
	return site.Name, site.URL, site.Username, site.Password,
		site.OtpMode, site.CaptchaMode,
		boolToInt(site.VpnEnabled),
		site.VpnConfig.ExePath, site.VpnConfig.WindowTitle, site.VpnConfig.ConnectWait,
		string(selectorsBytes)
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func insertSiteMap(conn *sql.DB, site map[string]interface{}, sortOrder int) {
	name, _ := site["name"].(string)
	url, _ := site["url"].(string)
	username, _ := site["username"].(string)
	password, _ := site["password"].(string)
	otpMode, _ := site["otp_mode"].(string)
	if otpMode == "" {
		otpMode = "none"
	}
	captchaMode, _ := site["captcha_mode"].(string)
	if captchaMode == "" {
		captchaMode = "auto"
	}
	vpnEnabled := 0
	if v, ok := site["vpn_enabled"].(bool); ok && v {
		vpnEnabled = 1
	}
	vpnExePath, vpnWindowTitle := "", ""
	vpnConnectWait := 20
	if vpnCfg, ok := site["vpn_config"].(map[string]interface{}); ok {
		vpnExePath, _ = vpnCfg["exe_path"].(string)
		vpnWindowTitle, _ = vpnCfg["window_title"].(string)
		if cw, ok := vpnCfg["connect_wait"].(float64); ok {
			vpnConnectWait = int(cw)
		}
	}
	selectorsBytes, _ := json.Marshal(site["selectors"])

	conn.Exec(`INSERT INTO sites
		(name, url, username, password, otp_mode, captcha_mode,
		 vpn_enabled, vpn_exe_path, vpn_window_title, vpn_connect_wait,
		 selectors_json, sort_order)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		name, url, username, password, otpMode, captchaMode,
		vpnEnabled, vpnExePath, vpnWindowTitle, vpnConnectWait,
		string(selectorsBytes), sortOrder)
}

// LoadSites 加载所有站点（按 sort_order 排序）
func LoadSites() ([]models.Site, error) {
	conn, err := open()
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	rows, err := conn.Query(`SELECT id, name, url, username, password, otp_mode, captcha_mode,
		vpn_enabled, vpn_exe_path, vpn_window_title, vpn_connect_wait,
		selectors_json, last_login_at, last_login_status
		FROM sites ORDER BY sort_order, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sites []models.Site
	for rows.Next() {
		var (
			id              int
			name            string
			url             string
			username        string
			password        string
			otpMode         string
			captchaMode     string
			vpnEnabled      int
			vpnExePath      string
			vpnWindowTitle  string
			vpnConnectWait  int
			selectorsJSON   string
			lastLoginAt     sql.NullString
			lastLoginStatus sql.NullString
		)
		err := rows.Scan(&id, &name, &url, &username, &password,
			&otpMode, &captchaMode, &vpnEnabled, &vpnExePath,
			&vpnWindowTitle, &vpnConnectWait,
			&selectorsJSON, &lastLoginAt, &lastLoginStatus)
		if err != nil {
			continue
		}
		s := rowToSite(id, name, url, username, password, otpMode, captchaMode,
			vpnEnabled, vpnExePath, vpnWindowTitle, vpnConnectWait,
			selectorsJSON, lastLoginAt, lastLoginStatus)
		sites = append(sites, s)
	}
	return sites, nil
}

// AddSite 添加站点，返回含 id 的完整站点
func AddSite(site models.Site) (models.Site, error) {
	conn, err := open()
	if err != nil {
		return site, err
	}
	defer conn.Close()

	var maxOrder int
	conn.QueryRow("SELECT COALESCE(MAX(sort_order), -1) FROM sites").Scan(&maxOrder)

	name, url, username, password, otpMode, captchaMode,
		vpnEnabled, vpnExePath, vpnWindowTitle, vpnConnectWait, selectorsJSON := siteToFields(site)

	result, err := conn.Exec(`INSERT INTO sites
		(name, url, username, password, otp_mode, captcha_mode,
		 vpn_enabled, vpn_exe_path, vpn_window_title, vpn_connect_wait,
		 selectors_json, sort_order)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		name, url, username, password, otpMode, captchaMode,
		vpnEnabled, vpnExePath, vpnWindowTitle, vpnConnectWait,
		selectorsJSON, maxOrder+1)
	if err != nil {
		return site, err
	}
	id, _ := result.LastInsertId()
	site.ID = int(id)
	return site, nil
}

// UpdateSite 根据 ID 更新站点
func UpdateSite(site models.Site) error {
	conn, err := open()
	if err != nil {
		return err
	}
	defer conn.Close()

	name, url, username, password, otpMode, captchaMode,
		vpnEnabled, vpnExePath, vpnWindowTitle, vpnConnectWait, selectorsJSON := siteToFields(site)

	_, err = conn.Exec(`UPDATE sites SET
		name = ?, url = ?, username = ?, password = ?, otp_mode = ?, captcha_mode = ?,
		vpn_enabled = ?, vpn_exe_path = ?, vpn_window_title = ?, vpn_connect_wait = ?,
		selectors_json = ?, updated_at = datetime('now', 'localtime')
		WHERE id = ?`,
		name, url, username, password, otpMode, captchaMode,
		vpnEnabled, vpnExePath, vpnWindowTitle, vpnConnectWait,
		selectorsJSON, site.ID)
	return err
}

// DeleteSite 根据 ID 删除站点
func DeleteSite(id int) error {
	conn, err := open()
	if err != nil {
		return err
	}
	defer conn.Close()
	_, err = conn.Exec("DELETE FROM sites WHERE id = ?", id)
	return err
}

// ============================================================
//  GUI 配置
// ============================================================

func LoadGUIConfig() models.GUIConfig {
	config := models.GUIConfig{
		LastSite: 0,
		Theme:    "light",
		Scale:    1.0,
	}
	conn, err := open()
	if err != nil {
		return config
	}
	defer conn.Close()

	rows, err := conn.Query("SELECT key, value FROM gui_config")
	if err != nil {
		return config
	}
	defer rows.Close()

	m := make(map[string]interface{})
	for rows.Next() {
		var key, value string
		rows.Scan(&key, &value)
		var v interface{}
		if json.Unmarshal([]byte(value), &v) == nil {
			m[key] = v
		}
	}

	if v, ok := m["last_site"].(float64); ok {
		config.LastSite = int(v)
	}
	if v, ok := m["theme"].(string); ok {
		config.Theme = v
	}
	if v, ok := m["scale"].(float64); ok {
		config.Scale = v
	}
	return config
}

func SaveGUIConfig(config models.GUIConfig) {
	conn, err := open()
	if err != nil {
		return
	}
	defer conn.Close()

	entries := map[string]interface{}{
		"last_site": config.LastSite,
		"theme":     config.Theme,
		"scale":     config.Scale,
	}
	for key, value := range entries {
		valBytes, _ := json.Marshal(value)
		conn.Exec("INSERT OR REPLACE INTO gui_config (key, value) VALUES (?, ?)",
			key, string(valBytes))
	}
}

// ============================================================
//  登录日志
// ============================================================

// RecordLogin 记录一次登录结果，同时更新站点的 last_login_at / last_login_status
func RecordLogin(siteID int, status, message string) {
	conn, err := open()
	if err != nil {
		return
	}
	defer conn.Close()

	conn.Exec("INSERT INTO login_logs (site_id, status, message) VALUES (?, ?, ?)",
		siteID, status, message)
	conn.Exec(`UPDATE sites SET
		last_login_at = datetime('now', 'localtime'),
		last_login_status = ? WHERE id = ?`, status, siteID)
}

// GetLoginLogs 获取登录日志列表（倒序）
func GetLoginLogs(siteID int, limit int) ([]models.LoginLog, error) {
	conn, err := open()
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	var rows *sql.Rows
	if siteID > 0 {
		rows, err = conn.Query(
			"SELECT id, site_id, status, message, created_at FROM login_logs WHERE site_id = ? ORDER BY id DESC LIMIT ?",
			siteID, limit)
	} else {
		rows, err = conn.Query(
			"SELECT id, site_id, status, message, created_at FROM login_logs ORDER BY id DESC LIMIT ?",
			limit)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var logs []models.LoginLog
	for rows.Next() {
		var log models.LoginLog
		rows.Scan(&log.ID, &log.SiteID, &log.Status, &log.Message, &log.CreatedAt)
		logs = append(logs, log)
	}
	return logs, nil
}
