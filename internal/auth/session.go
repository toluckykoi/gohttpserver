package auth

import (
	"embed"
	"net/http"

	"github.com/gorilla/sessions"
)

// FrontendFS 是内嵌的 frontend/dist 文件系统，用于提供登录 SPA。
var FrontendFS embed.FS

// GetSession 返回请求对应的 gorilla 会话（始终非 nil）。
func GetSession(r *http.Request) (*sessions.Session, error) {
	return Store.Get(r, DefaultSessionName)
}

// UserFromSession 从会话 cookie 提取登录用户；逗号-ok 断言可防御伪造或旧格式 cookie。
func UserFromSession(r *http.Request) *UserInfo {
	session, err := Store.Get(r, DefaultSessionName)
	if err != nil {
		return nil
	}
	val := session.Values["user"]
	if val == nil {
		return nil
	}
	userInfo, ok := val.(*UserInfo)
	if !ok {
		return nil
	}
	return userInfo
}
