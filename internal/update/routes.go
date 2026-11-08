package update

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/gorilla/mux"

	"github.com/codeskyblue/gohttpserver/internal/auth"
)

// RegisterRoutes 在 router 上注册全部 /-/api/update/* 端点。
// 保护模型与 admin API 一致：路由挂在登录闸门内的子路由上（由 main.go 保证
// 仅 --login 模式注册），各 handler 自行校验会话，未认证一律 401；
// 写请求的 CSRF 由 mainRouter 上的 OriginCheckMiddleware 统一拦截。
func RegisterRoutes(router *mux.Router, svc *Service) {
	router.HandleFunc("/-/api/update/status", svc.handleStatus).Methods("GET")
	router.HandleFunc("/-/api/update/check", svc.handleCheck).Methods("POST")
	router.HandleFunc("/-/api/update/download", svc.handleDownload).Methods("POST")
	router.HandleFunc("/-/api/update/apply", svc.handleApply).Methods("POST")
	router.HandleFunc("/-/api/update/restart", svc.handleRestart).Methods("POST")
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

// requireUser 校验登录会话；通过返回 true，否则已写好 401 响应。
func requireUser(w http.ResponseWriter, r *http.Request) bool {
	if auth.UserFromLoginSession(r) == nil {
		writeError(w, http.StatusUnauthorized, "需要登录")
		return false
	}
	return true
}

func (svc *Service) handleStatus(w http.ResponseWriter, r *http.Request) {
	if !requireUser(w, r) {
		return
	}
	writeJSON(w, http.StatusOK, svc.Status())
}

// handleCheck 同步触发一次检查（最长约 15s），结果直接随响应返回。
// 并发检查会被 Service 幂等跳过，此时返回当前快照。
func (svc *Service) handleCheck(w http.ResponseWriter, r *http.Request) {
	if !requireUser(w, r) {
		return
	}
	svc.Check()
	writeJSON(w, http.StatusOK, svc.Status())
}

func (svc *Service) handleDownload(w http.ResponseWriter, r *http.Request) {
	if !requireUser(w, r) {
		return
	}
	if err := svc.StartDownload(); err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, svc.Status())
}

func (svc *Service) handleApply(w http.ResponseWriter, r *http.Request) {
	if !requireUser(w, r) {
		return
	}
	if err := svc.Apply(); err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, svc.Status())
}

// handleRestart 先应答再延迟触发优雅停机，确保响应已写回客户端。
// 进程退出后由 systemd(Restart=always) 等守护进程以新版本拉起。
func (svc *Service) handleRestart(w http.ResponseWriter, r *http.Request) {
	if !requireUser(w, r) {
		return
	}
	if svc.opts.Restart == nil {
		writeError(w, http.StatusNotImplemented, "重启能力不可用")
		return
	}
	time.AfterFunc(500*time.Millisecond, svc.opts.Restart)
	writeJSON(w, http.StatusOK, map[string]string{"message": "服务正在重启，页面将自动恢复"})
}
