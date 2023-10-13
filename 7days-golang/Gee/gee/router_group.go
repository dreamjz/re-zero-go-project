package gee

import (
	"log"
	"net/http"
	"path"
)

type RouterGroup struct {
	prefix      string        // 路由组前缀
	middlewares []HandlerFunc // 中间件
	parent      *RouterGroup  // 父母分组
	engine      *Engine       // 所有分组持有同一个 Engine 实例
}

// Use add middleware to RouterGroup
func (group *RouterGroup) Use(middlewares ...HandlerFunc) {
	group.middlewares = append(group.middlewares, middlewares...)
}

// Group create a new RouterGroup
func (group *RouterGroup) Group(prefix string) *RouterGroup {
	engine := group.engine
	newGroup := &RouterGroup{
		prefix: group.prefix + prefix,
		parent: group,
		engine: engine, // All group share one Engine instance
	}
	// Add to group lists
	engine.groups = append(engine.groups, newGroup)
	return newGroup
}

func (group *RouterGroup) addRoute(method string, path string, handler HandlerFunc) {
	pattern := group.prefix + path
	log.Printf("Route %4s - %s", method, pattern)
	group.engine.router.addRoute(method, pattern, handler)
}

func (group *RouterGroup) GET(pattern string, handler HandlerFunc) {
	group.addRoute("GET", pattern, handler)
}

func (group *RouterGroup) POST(pattern string, handler HandlerFunc) {
	group.addRoute("POST", pattern, handler)
}

func (group *RouterGroup) createStaticHandler(relPath string, fs http.FileSystem) HandlerFunc {
	absPath := path.Join(group.prefix, relPath)
	fileServer := http.StripPrefix(absPath, http.FileServer(fs))
	return func(c *Context) {
		file := c.Param("filepath")

		if _, err := fs.Open(file); err != nil {
			c.Status(http.StatusNotFound)
			return
		}
		fileServer.ServeHTTP(c.Writer, c.Req)
	}
}

func (group *RouterGroup) Static(relPath string, root string) {
	handler := group.createStaticHandler(relPath, http.Dir(root))
	urlPattern := path.Join(relPath, "/*filepath")

	group.GET(urlPattern, handler)
}
