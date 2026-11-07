package admin

import (
	"encoding/json"
	"io"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"

	"github.com/gorilla/mux"

	"github.com/codeskyblue/gohttpserver/internal/auth"
	"github.com/codeskyblue/gohttpserver/internal/frp"
	"github.com/codeskyblue/gohttpserver/internal/util"
	"github.com/codeskyblue/gohttpserver/internal/webdav"
)

// API 汇集 /-/api/admin/* 各处理器所需的共享状态。
type API struct {
	Login  *auth.LoginState
	Webdav *webdav.WebdavAccountState
	// Usage 为按账号的字节计数；可为 nil。
	Usage *webdav.UsageState
	// Root 是 --root 指定的文件系统路径。
	Root string
	// LocalAddr 是程序监听地址（IP:port）。
	LocalAddr string
	// FRP 是 FRP 客户端管理器；可为 nil。
	FRP *frp.FRPClient
	// Version 是构建版本字符串。
	Version string
}

// RegisterRoutes 在 router 上注册全部 /-/api/admin/* 端点。
// router 必须是去掉前缀后的子路由，--prefix 才能透明生效。
func RegisterRoutes(router *mux.Router, api *API) {
	router.HandleFunc("/-/api/admin/profile", api.handleProfile).Methods("GET")
	router.HandleFunc("/-/api/admin/profile/username", api.handleChangeUsername).Methods("PUT")
	// 同时保留旧路径 /-/api/auth/password 以兼容旧客户端。
	router.HandleFunc("/-/api/admin/profile/password", func(w http.ResponseWriter, r *http.Request) {
		auth.HandleChangePassword(api.Login, w, r)
	}).Methods("PUT")

	router.HandleFunc("/-/api/admin/webdav/status", api.handleWebdavStatus).Methods("GET")
	router.HandleFunc("/-/api/admin/webdav/enabled", api.handleWebdavSetEnabled).Methods("PUT")
	router.HandleFunc("/-/api/admin/webdav/accounts", api.handleWebdavList).Methods("GET")
	router.HandleFunc("/-/api/admin/webdav/accounts", api.handleWebdavCreate).Methods("POST")
	router.HandleFunc("/-/api/admin/webdav/accounts/{id}", api.handleWebdavUpdate).Methods("PUT")
	router.HandleFunc("/-/api/admin/webdav/accounts/{id}", api.handleWebdavDelete).Methods("DELETE")
	router.HandleFunc("/-/api/admin/webdav/accounts/{id}/reset-password", api.handleWebdavResetPassword).Methods("POST")
	router.HandleFunc("/-/api/admin/webdav/recalculate-usage", api.handleWebdavRecalculateUsage).Methods("POST")

	router.HandleFunc("/-/api/admin/frp/status", api.handleFRPStatus).Methods("GET")
	router.HandleFunc("/-/api/admin/frp/enabled", api.handleFRPSetEnabled).Methods("PUT")
	router.HandleFunc("/-/api/admin/frp/config", api.handleFRPUpdateConfig).Methods("PUT")
	router.HandleFunc("/-/api/admin/frp/auth-token", api.handleFRPRevealToken).Methods("GET")
}

func (api *API) handleProfile(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromLoginSession(r)
	if user == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"username": user.Name,
		"provider": user.Provider,
		"version":  api.Version,
	})
}

func (api *API) handleChangeUsername(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromLoginSession(r)
	if user == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1024))
	if err != nil {
		http.Error(w, "Body too large or unreadable", http.StatusBadRequest)
		return
	}
	var req struct {
		NewUsername string `json:"new_username"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}
	newUsername := strings.TrimSpace(req.NewUsername)
	if newUsername == "" {
		http.Error(w, "new_username must not be empty", http.StatusBadRequest)
		return
	}
	if len(newUsername) > 64 {
		http.Error(w, "new_username too long (max 64 chars)", http.StatusBadRequest)
		return
	}
	if newUsername == user.Name {
		http.Error(w, "new_username is the same as the current one", http.StatusBadRequest)
		return
	}
	if err := api.Login.ChangeUsername(user.Name, newUsername); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	// 改名后同步绑定的 webdav 账号用户名以保持一致。
	if synced, syncErr := api.Webdav.SyncUsernamesTo(user.Name, newUsername); syncErr != nil {
		log.Printf("admin: webdav username sync after rename failed: %v", syncErr)
	} else if synced > 0 {
		log.Printf("admin: webdav username sync updated %d account(s)", synced)
	}
	session, _ := auth.Store.Get(r, auth.DefaultSessionName)
	session.Values["user"] = &auth.LoginUser{
		Name:     newUsername,
		Provider: user.Provider,
	}
	if err := session.Save(r, w); err != nil {
		log.Printf("admin: session save after username change failed: %v", err)
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"success":  true,
		"username": newUsername,
	})
}

func (api *API) handleWebdavStatus(w http.ResponseWriter, r *http.Request) {
	if auth.UserFromLoginSession(r) == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	accounts := api.Webdav.List()
	public := make([]map[string]any, 0, len(accounts))
	for _, a := range accounts {
		public = append(public, api.webdavAccountPublic(a))
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"enabled":    api.Webdav.IsEnabled(),
		"accounts":   public,
		"webdav_url": "/dav/",
	})
}

func (api *API) handleWebdavSetEnabled(w http.ResponseWriter, r *http.Request) {
	if auth.UserFromLoginSession(r) == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 256))
	if err != nil {
		http.Error(w, "Body too large or unreadable", http.StatusBadRequest)
		return
	}
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}
	if err := api.Webdav.SetEnabled(req.Enabled); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "enabled": req.Enabled})
}

func (api *API) handleWebdavList(w http.ResponseWriter, r *http.Request) {
	if auth.UserFromLoginSession(r) == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	accounts := api.Webdav.List()
	public := make([]map[string]any, 0, len(accounts))
	for _, a := range accounts {
		public = append(public, api.webdavAccountPublic(a))
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{"accounts": public})
}

// handleWebdavCreate 创建账号；明文密码仅在本次响应中返回一次。
func (api *API) handleWebdavCreate(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromLoginSession(r)
	if user == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 4096))
	if err != nil {
		http.Error(w, "Body too large or unreadable", http.StatusBadRequest)
		return
	}
	var req webdav.CreateAccountRequest
	req.ProtectSystemFiles = true
	req.RootPath = "/"
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}
	acc, plaintext, err := api.Webdav.CreateAccount(req, user.Name)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	public := api.webdavAccountPublic(acc)
	public["password"] = plaintext
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(public)
}

func (api *API) handleWebdavUpdate(w http.ResponseWriter, r *http.Request) {
	if auth.UserFromLoginSession(r) == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	id := mux.Vars(r)["id"]
	if id == "" {
		http.Error(w, "missing id", http.StatusBadRequest)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 4096))
	if err != nil {
		http.Error(w, "Body too large or unreadable", http.StatusBadRequest)
		return
	}
	var req webdav.UpdateAccountRequest
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}
	if err := api.Webdav.UpdateAccount(id, req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{"success": true})
}

func (api *API) handleWebdavDelete(w http.ResponseWriter, r *http.Request) {
	if auth.UserFromLoginSession(r) == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	id := mux.Vars(r)["id"]
	if id == "" {
		http.Error(w, "missing id", http.StatusBadRequest)
		return
	}
	if err := api.Webdav.DeleteAccount(id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{"success": true})
}

func (api *API) handleWebdavResetPassword(w http.ResponseWriter, r *http.Request) {
	if auth.UserFromLoginSession(r) == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	id := mux.Vars(r)["id"]
	if id == "" {
		http.Error(w, "missing id", http.StatusBadRequest)
		return
	}
	plaintext, err := api.Webdav.ResetPassword(id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"success":  true,
		"password": plaintext,
	})
}

func (api *API) handleWebdavRecalculateUsage(w http.ResponseWriter, r *http.Request) {
	if auth.UserFromLoginSession(r) == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	if api.Usage == nil {
		http.Error(w, "usage tracking not configured", http.StatusServiceUnavailable)
		return
	}
	accounts := api.Webdav.List()
	results := api.Usage.RecalculateAll(accounts, api.Root)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"success": true,
		"results": results,
	})
}

func (api *API) webdavAccountPublic(a webdav.WebdavAccount) map[string]any {
	used := int64(0)
	if api.Usage != nil {
		used = api.Usage.Get(a.ID)
	}
	return map[string]any{
		"id":                   a.ID,
		"remark":               a.Remark,
		"username":             a.Username,
		"root_path":            a.RootPath,
		"readonly":             a.ReadOnly,
		"protect_system_files": a.ProtectSystemFiles,
		"quota_bytes":          a.QuotaBytes,
		"used_bytes":           used,
		"created_at":           a.CreatedAtUnix,
		"updated_at":           a.UpdatedAtUnix,
	}
}

func (api *API) handleFRPStatus(w http.ResponseWriter, r *http.Request) {
	if auth.UserFromLoginSession(r) == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	if api.FRP == nil {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"enabled": false,
			"running": false,
		})
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(api.FRP.Status())
}

func (api *API) handleFRPSetEnabled(w http.ResponseWriter, r *http.Request) {
	if auth.UserFromLoginSession(r) == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	if api.FRP == nil {
		http.Error(w, "FRP client not initialized", http.StatusServiceUnavailable)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 256))
	if err != nil {
		http.Error(w, "Body too large or unreadable", http.StatusBadRequest)
		return
	}
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}
	if err := api.FRP.SetEnabled(req.Enabled); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "enabled": req.Enabled})
}

func (api *API) handleFRPUpdateConfig(w http.ResponseWriter, r *http.Request) {
	if auth.UserFromLoginSession(r) == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	if api.FRP == nil {
		http.Error(w, "FRP client not initialized", http.StatusServiceUnavailable)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 4096))
	if err != nil {
		http.Error(w, "Body too large or unreadable", http.StatusBadRequest)
		return
	}
	var req struct {
		ServerAddr           string `json:"serverAddr"`
		ServerPort           int    `json:"serverPort"`
		AuthToken            string `json:"authToken"`
		ProxyName            string `json:"proxyName"`
		ProxyType            string `json:"proxyType"`
		LocalIP              string `json:"localIP"`
		LocalPort            int    `json:"localPort"`
		RemotePort           int    `json:"remotePort"`
		ProxyProtocolVersion string `json:"proxyProtocolVersion"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}
	if req.ProxyProtocolVersion != "" &&
		req.ProxyProtocolVersion != "v1" &&
		req.ProxyProtocolVersion != "v2" {
		http.Error(w, "Invalid proxyProtocolVersion, must be v1 or v2", http.StatusBadRequest)
		return
	}
	// 请求未携带 authToken 时沿用库中旧值（状态接口从不返回其内容）。
	authToken := req.AuthToken
	if authToken == "" {
		if existing, _, err := api.FRP.LoadFromDB(); err == nil && existing != nil {
			authToken = existing.AuthToken
		}
	}

	cfg := &frp.FRPConfig{
		ServerAddr:           req.ServerAddr,
		ServerPort:           req.ServerPort,
		AuthToken:            authToken,
		ProxyName:            req.ProxyName,
		ProxyType:            req.ProxyType,
		LocalIP:              req.LocalIP,
		LocalPort:            req.LocalPort,
		RemotePort:           req.RemotePort,
		ProxyProtocolVersion: req.ProxyProtocolVersion,
	}
	// LocalIP/LocalPort 为默认值时从监听地址自动填充，且只取局域网地址。
	if (cfg.LocalIP == "" || cfg.LocalIP == "127.0.0.1") && api.LocalAddr != "" {
		if host, portStr, err := net.SplitHostPort(api.LocalAddr); err == nil {
			if host != "" && host != "0.0.0.0" && host != "::" {
				if parsed := net.ParseIP(host); parsed != nil && util.IsLANAddress(parsed) {
					cfg.LocalIP = host
				} else if lanIP := util.GetLocalIP(); lanIP != "" {
					cfg.LocalIP = lanIP
				}
			} else if lanIP := util.GetLocalIP(); lanIP != "" {
				cfg.LocalIP = lanIP
			}
			if cfg.LocalPort == 0 {
				if p, err := strconv.Atoi(portStr); err == nil {
					cfg.LocalPort = p
				}
			}
		}
	}
	if err := api.FRP.UpdateConfig(cfg); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{"success": true})
}

// handleFRPRevealToken 向已登录会话返回 FRP auth token 明文。
// 有意为之：--login 目前是单管理员模型，任何已登录会话就是管理员本人，
// 而管理员必须能读回自己保存过的 token 才能修改其它 FRP 配置（否则改一次
// serverAddr 就得连同 token 一起重输）。若未来引入多用户/角色模型，
// 此端点必须改为「仅首次设置时返回」或加二次确认。
func (api *API) handleFRPRevealToken(w http.ResponseWriter, r *http.Request) {
	if auth.UserFromLoginSession(r) == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	if api.FRP == nil {
		http.Error(w, "FRP client not initialized", http.StatusServiceUnavailable)
		return
	}
	cfg, _, err := api.FRP.LoadFromDB()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	token := ""
	if cfg != nil {
		token = cfg.AuthToken
	}
	// 密钥明文不得进入浏览器/共享代理缓存。
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"authToken":    token,
		"authTokenSet": token != "",
	})
}
