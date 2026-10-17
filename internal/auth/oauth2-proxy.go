package auth

import (
	"encoding/json"
	"net"
	"net/http"
	"net/url"

	"github.com/gorilla/mux"
)

// Oauth2ProxyMiddleware 将代理提供的身份写入会话；客户端自带的同名请求头必须在上游剥离，
// 因此 main() 默认拒绝公网绑定，除非显式 --allow-public-oauth2。
func Oauth2ProxyMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		email := r.Header.Get("X-Auth-Request-Email")
		if email != "" {
			fullNameMap, _ := url.ParseQuery(r.Header.Get("X-Auth-Request-Fullname"))
			var fullName string
			for k := range fullNameMap {
				fullName = k
				break
			}
			user := &UserInfo{
				Email:    email,
				Name:     fullName,
				NickName: r.Header.Get("X-Auth-Request-User"),
			}
			session, err := Store.Get(r, DefaultSessionName)
			if err == nil {
				session.Values["user"] = user
				_ = session.Save(r, w) // 尽力而为；身份会在下一次请求重新解析
			}
		}
		next.ServeHTTP(w, r)
	})
}

func HandleOauth2(router *mux.Router) {
	router.HandleFunc("/-/user", func(w http.ResponseWriter, r *http.Request) {
		userInfo := UserFromSession(r)
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		data, _ := json.Marshal(userInfo)
		w.Write(data)
	}).Methods("GET")
}

// IsLoopbackAddr 判断监听地址是否仅绑定回环。
func IsLoopbackAddr(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	if host == "" || host == "localhost" {
		if host == "localhost" {
			return true
		}
		return false
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
