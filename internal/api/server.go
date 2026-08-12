package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"

	"autologin-go/internal/db"
	"autologin-go/internal/engine"
	"autologin-go/internal/models"
)

// Server HTTP API 服务
type Server struct {
	engine   *engine.Engine
	tasks    sync.Map // taskID -> *LoginTask
	taskSeq  int
	taskMu   sync.Mutex
}

// LoginTask 登录任务
type LoginTask struct {
	ID        string
	SiteID    int
	SiteName  string
	Messages  chan models.LoginMessage
	InputChan chan string // 手动输入通道（验证码/口令）
	Cancel    context.CancelFunc
}

// NewServer 创建 API 服务
func NewServer() *Server {
	return &Server{
		engine: engine.NewEngine(),
	}
}

// RegisterRoutes 注册所有路由
func (s *Server) RegisterRoutes(mux *http.ServeMux) {
	// 站点 CRUD
	mux.HandleFunc("GET /api/sites", s.handleListSites)
	mux.HandleFunc("POST /api/sites", s.handleCreateSite)
	mux.HandleFunc("PUT /api/sites/{id}", s.handleUpdateSite)
	mux.HandleFunc("DELETE /api/sites/{id}", s.handleDeleteSite)

	// 登录
	mux.HandleFunc("POST /api/sites/{id}/login", s.handleStartLogin)
	mux.HandleFunc("GET /api/login/stream/{taskID}", s.handleLoginStream)
	mux.HandleFunc("POST /api/login/{taskID}/input", s.handleManualInput)
	mux.HandleFunc("POST /api/login/{taskID}/cancel", s.handleCancelLogin)

	// GUI 配置
	mux.HandleFunc("GET /api/config", s.handleGetConfig)
	mux.HandleFunc("PUT /api/config", s.handleSaveConfig)

	// 登录日志
	mux.HandleFunc("GET /api/logs", s.handleGetLogs)
}

// ============================================================
//  站点 CRUD
// ============================================================

func (s *Server) handleListSites(w http.ResponseWriter, r *http.Request) {
	sites, err := db.LoadSites()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, sites)
}

func (s *Server) handleCreateSite(w http.ResponseWriter, r *http.Request) {
	var site models.Site
	if err := json.NewDecoder(r.Body).Decode(&site); err != nil {
		writeError(w, http.StatusBadRequest, "无效的请求数据")
		return
	}
	created, err := db.AddSite(site)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, created)
}

func (s *Server) handleUpdateSite(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "无效的站点 ID")
		return
	}
	var site models.Site
	if err := json.NewDecoder(r.Body).Decode(&site); err != nil {
		writeError(w, http.StatusBadRequest, "无效的请求数据")
		return
	}
	site.ID = id
	if err := db.UpdateSite(site); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, site)
}

func (s *Server) handleDeleteSite(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "无效的站点 ID")
		return
	}
	if err := db.DeleteSite(id); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, map[string]bool{"success": true})
}

// ============================================================
//  登录
// ============================================================

func (s *Server) handleStartLogin(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "无效的站点 ID")
		return
	}

	sites, err := db.LoadSites()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	var site *models.Site
	for _, s := range sites {
		if s.ID == id {
			site = &s
			break
		}
	}
	if site == nil {
		writeError(w, http.StatusNotFound, "站点不存在")
		return
	}

	// 创建登录任务
	s.taskMu.Lock()
	s.taskSeq++
	taskID := fmt.Sprintf("task_%d_%d", time.Now().Unix(), s.taskSeq)
	s.taskMu.Unlock()

	ctx, cancel := context.WithCancel(context.Background())
	task := &LoginTask{
		ID:        taskID,
		SiteID:    id,
		SiteName:  site.Name,
		Messages:  make(chan models.LoginMessage, 100),
		InputChan: make(chan string, 1),
		Cancel:    cancel,
	}
	s.tasks.Store(taskID, task)

	// 启动登录协程
	go s.runLogin(ctx, task, *site)

	// 返回 taskID
	writeJSON(w, map[string]string{"task_id": taskID})
}

func (s *Server) runLogin(ctx context.Context, task *LoginTask, site models.Site) {
	defer close(task.Messages)

	logFn := func(level, msg string) {
		select {
		case task.Messages <- models.LoginMessage{
			Type:    "log",
			Content: fmt.Sprintf("[%s] %s", level, msg),
		}:
		default:
			// 通道满时丢弃日志，避免阻塞登录流程
		}
	}

	stepFn := func(index int, name, state string) {
		select {
		case task.Messages <- models.LoginMessage{
			Type: "step",
			Step: &models.LoginStep{
				Index: index,
				Name:  name,
				State: state,
			},
		}:
		default:
		}
	}

	err := s.engine.Login(ctx, site, logFn, stepFn)

	if err != nil {
		task.Messages <- models.LoginMessage{
			Type:    "failed",
			Content: err.Error(),
		}
		db.RecordLogin(site.ID, "failed", err.Error())
	} else {
		task.Messages <- models.LoginMessage{
			Type:    "success",
			Content: "登录成功",
		}
		db.RecordLogin(site.ID, "success", "登录成功")
	}
}

func (s *Server) handleLoginStream(w http.ResponseWriter, r *http.Request) {
	taskID := r.PathValue("taskID")
	val, ok := s.tasks.Load(taskID)
	if !ok {
		writeError(w, http.StatusNotFound, "任务不存在")
		return
	}
	task := val.(*LoginTask)

	// SSE
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "不支持 SSE")
		return
	}

	// 发送任务信息
	fmt.Fprintf(w, "event: task\ndata: {\"task_id\":\"%s\",\"site_name\":\"%s\"}\n\n", task.ID, task.SiteName)
	flusher.Flush()

	for msg := range task.Messages {
		data, _ := json.Marshal(msg)
		fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()

		if msg.Type == "success" || msg.Type == "failed" {
			break
		}
	}

	// 清理任务
	s.tasks.Delete(taskID)
}

func (s *Server) handleManualInput(w http.ResponseWriter, r *http.Request) {
	taskID := r.PathValue("taskID")
	val, ok := s.tasks.Load(taskID)
	if !ok {
		writeError(w, http.StatusNotFound, "任务不存在")
		return
	}
	task := val.(*LoginTask)

	var body struct {
		Value string `json:"value"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "无效的请求数据")
		return
	}

	select {
	case task.InputChan <- body.Value:
		writeJSON(w, map[string]bool{"success": true})
	default:
		writeError(w, http.StatusConflict, "输入通道已满")
	}
}

func (s *Server) handleCancelLogin(w http.ResponseWriter, r *http.Request) {
	taskID := r.PathValue("taskID")
	val, ok := s.tasks.Load(taskID)
	if !ok {
		writeError(w, http.StatusNotFound, "任务不存在")
		return
	}
	task := val.(*LoginTask)
	task.Cancel()
	writeJSON(w, map[string]bool{"success": true})
}

// ============================================================
//  GUI 配置
// ============================================================

func (s *Server) handleGetConfig(w http.ResponseWriter, r *http.Request) {
	config := db.LoadGUIConfig()
	writeJSON(w, config)
}

func (s *Server) handleSaveConfig(w http.ResponseWriter, r *http.Request) {
	var config models.GUIConfig
	if err := json.NewDecoder(r.Body).Decode(&config); err != nil {
		writeError(w, http.StatusBadRequest, "无效的请求数据")
		return
	}
	db.SaveGUIConfig(config)
	writeJSON(w, config)
}

// ============================================================
//  登录日志
// ============================================================

func (s *Server) handleGetLogs(w http.ResponseWriter, r *http.Request) {
	siteIDStr := r.URL.Query().Get("site_id")
	limitStr := r.URL.Query().Get("limit")

	siteID := 0
	limit := 50

	if siteIDStr != "" {
		siteID, _ = strconv.Atoi(siteIDStr)
	}
	if limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
			limit = l
		}
	}

	logs, err := db.GetLoginLogs(siteID, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, logs)
}

// ============================================================
//  工具函数
// ============================================================

func writeJSON(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

func writeError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
