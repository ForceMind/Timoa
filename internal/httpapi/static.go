package httpapi

import (
	"io/fs"
	"net/http"
	"path"
	"strings"

	"xiaozhang/internal/webdist"
)

// staticHandler 同源托管前端构建产物：/api 与 /healthz 优先；
// 带指纹的静态资源长缓存，index.html 禁缓存；未知路径回退 SPA。
func staticHandler() http.Handler {
	sub, err := fs.Sub(webdist.Dist, "dist")
	if err != nil {
		return http.NotFoundHandler()
	}
	fileSrv := http.FileServer(http.FS(sub))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			writeErr(w, 405, "method_not_allowed", "method not allowed")
			return
		}
		p := path.Clean(strings.TrimPrefix(r.URL.Path, "/"))
		if p == "." {
			p = "index.html"
		}
		if p == "index.html" {
			w.Header().Set("Cache-Control", "no-store")
		} else if strings.HasPrefix(p, "assets/") {
			// Vite 产物带内容指纹
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "public, max-age=3600")
		}
		if f, err := sub.Open(p); err == nil {
			f.Close()
			fileSrv.ServeHTTP(w, r)
			return
		}
		// SPA 回退
		w.Header().Set("Cache-Control", "no-store")
		r.URL.Path = "/"
		fileSrv.ServeHTTP(w, r)
	})
}
