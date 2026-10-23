package main

import (
	"net"
	"net/http"
	"strings"
)

// httpsRedirect 让「域名走 HTTP」的请求重定向到 HTTPS 端口，而用 IP 访问的
// 请求保持原样。这样局域网里可以用 http://<IP>:18180/，域名访问则必须是
// https://<域名>:18181/。
//
// 它只包在 -addr 这个 HTTP 监听上；HTTPS 监听与 unix socket 网关都原样使用
// handler，不受影响。
func httpsRedirect(h http.Handler, tlsAddr string) http.Handler {
	port := httpsPort(tlsAddr)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := hostOnly(r.Host)
		if port == "" || host == "" || net.ParseIP(host) != nil {
			h.ServeHTTP(w, r)
			return
		}
		target := "https://" + host + ":" + port + r.URL.RequestURI()
		http.Redirect(w, r, target, http.StatusMovedPermanently)
	})
}

// httpsPort 从 -tls-addr 里取出端口（如 ":18181" → "18181"）。没有配置
// TLS 时返回空串，重定向随之关闭。
func httpsPort(tlsAddr string) string {
	if tlsAddr == "" {
		return ""
	}
	if i := strings.LastIndex(tlsAddr, ":"); i >= 0 {
		return tlsAddr[i+1:]
	}
	return tlsAddr
}

// hostOnly 去掉 Host 里的端口部分；去掉失败时原样返回。
func hostOnly(hostport string) string {
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		return h
	}
	return hostport
}
