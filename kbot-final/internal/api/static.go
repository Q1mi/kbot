package api

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// resolveWebRoot 返回当前 React Admin 的构建目录。
func resolveWebRoot() string { return filepath.Join("web", "admin", "dist") }

// spaFileServer 服务静态资源。SPA 模式下,对"看起来像前端路由"的路径(无扩展名且未命中真实文件)
// 回退到 index.html,使 React BrowserRouter 的深链(/agents、/kbs/xxx 刷新)不 404。
// 带扩展名却未命中的资源(如 /assets/x.js)仍走 FileServer 返回 404(正常)。
func spaFileServer(root string) http.HandlerFunc {
	fs := http.FileServer(http.Dir(root))
	index := filepath.Join(root, "index.html")
	return func(w http.ResponseWriter, r *http.Request) {
		clean := filepath.Clean(strings.TrimPrefix(r.URL.Path, "/"))
		full := filepath.Join(root, clean)
		// 命中真实文件(非目录)→ 交给 FileServer 带正确 Content-Type / 缓存头。
		if st, err := os.Stat(full); err == nil && !st.IsDir() {
			fs.ServeHTTP(w, r)
			return
		}
		// SPA 路由回退:无扩展名 → index.html。
		if filepath.Ext(clean) == "" {
			http.ServeFile(w, r, index)
			return
		}
		// 其余(根路径、未命中的带扩展资源)交给 FileServer。
		fs.ServeHTTP(w, r)
	}
}
