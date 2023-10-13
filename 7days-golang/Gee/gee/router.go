package gee

import (
	"net/http"
	"strings"
)

type router struct {
	trieRoots map[string]*trieNode
	handlers  map[string]HandlerFunc
}

func newRouter() *router {
	return &router{
		trieRoots: map[string]*trieNode{},
		handlers:  map[string]HandlerFunc{},
	}
}

func parsePattern(pattern string) []string {
	strs := strings.Split(pattern, "/")

	parts := make([]string, 0)
	for _, str := range strs {
		if str != "" {
			parts = append(parts, str)
			if str[0] == '*' { // Only one '*' allowed
				break
			}
		}
	}

	return parts
}

func (r *router) addRoute(method string, pattern string, handler HandlerFunc) {
	parts := parsePattern(pattern)

	_, ok := r.trieRoots[method]
	if !ok {
		r.trieRoots[method] = &trieNode{}
	}
	r.trieRoots[method].insert(pattern, parts, 0)

	key := method + "-" + pattern
	r.handlers[key] = handler
}

func (r *router) handle(c *Context) {
	n, params := r.getRoute(c.Method, c.Path)
	var handler HandlerFunc
	if n != nil {
		c.Params = params
		key := c.Method + "-" + n.pattern
		handler = r.handlers[key]
	} else {
		handler = func(c *Context) {
			c.String(http.StatusNotFound, "404 NOT FOUND: %s\n", c.Path)
		}
	}

	c.handlers = append(c.handlers, handler)
	c.Next()
}

func (r *router) getRoute(method string, path string) (*trieNode, map[string]string) {
	root, ok := r.trieRoots[method]
	if !ok {
		return nil, nil
	}

	searchParts := parsePattern(path)
	n := root.search(searchParts, 0)
	if n != nil {
		parts := parsePattern(n.pattern)
		params := make(map[string]string)

		for i, part := range parts {
			if part[0] == ':' {
				params[part[1:]] = searchParts[i]
			}
			if part[0] == '*' && len(part) > 1 {
				params[part[1:]] = strings.Join(searchParts[i:], "/")
				break
			}
		}

		return n, params
	}

	return nil, nil
}
