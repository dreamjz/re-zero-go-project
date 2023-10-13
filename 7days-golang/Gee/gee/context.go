package gee

import (
	"encoding/json"
	"fmt"
	"net/http"
)

type H map[string]any

type Context struct {
	Writer http.ResponseWriter
	Req    *http.Request
	// Request info
	Path   string
	Method string
	Params map[string]string // Dynamic route parameters
	// Response info
	StatusCode int
	// Middleware
	handlers []HandlerFunc
	index    int
	// engine
	engine *Engine
}

func newContext(w http.ResponseWriter, req *http.Request) *Context {
	return &Context{
		Writer: w,
		Req:    req,
		Path:   req.URL.Path,
		Method: req.Method,
		index:  -1,
	}
}

func (c *Context) Next() {
	c.index++
	n := len(c.handlers)
	for c.index < n {
		c.handlers[c.index](c)
		c.index++
	}
}

// PostForm return form value of the key
func (c *Context) PostForm(key string) string {
	return c.Req.FormValue(key)
}

// Query return query value of the key
func (c *Context) Query(key string) string {
	return c.Req.URL.Query().Get(key)
}

// Status set HTTP response status code
func (c *Context) Status(code int) {
	c.StatusCode = code
	c.Writer.WriteHeader(code)
}

// SetHeader set HTTP response header
func (c *Context) SetHeader(key string, value string) {
	c.Writer.Header().Set(key, value)
}

// String write string data into HTTP response
func (c *Context) String(code int, format string, values ...any) {
	c.SetHeader("Content-Type", "text/plain")
	c.Status(code)
	c.Writer.Write([]byte(fmt.Sprintf(format, values...)))
}

// JSON write JSON data into HTTP response
func (c *Context) JSON(code int, obj any) {
	c.SetHeader("Content-Type", "application/json")
	c.Status(code)
	encoder := json.NewEncoder(c.Writer)
	if err := encoder.Encode(obj); err != nil {
		http.Error(c.Writer, err.Error(), 500)
	}
}

// Data write bytes data into HTTP response
func (c *Context) Data(code int, data []byte) {
	c.Status(code)
	c.Writer.Write(data)
}

// HTML write HTML data into HTTP response
func (c *Context) HTML(code int, tmplName string, data any) {
	c.SetHeader("Content-Type", "text/html")
	c.Status(code)
	if err := c.engine.htmlTmpls.ExecuteTemplate(c.Writer, tmplName, data); err != nil {
		c.Fail(500, err.Error())
	}
}

// Param return dynamic route parameter by key
func (c *Context) Param(key string) string {
	val, _ := c.Params[key]
	return val
}

func (c *Context) Fail(code int, errMsg string) {
	c.index = len(c.handlers)
	c.JSON(code, H{"message": errMsg})
}
