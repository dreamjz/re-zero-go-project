package gee

import (
	"log"
	"time"
)

func Logger() HandlerFunc {
	return func(c *Context) {
		start := time.Now()
		c.Next()
		log.Printf("[%d] %s in %vms", c.StatusCode, c.Req.RequestURI, time.Since(start).Milliseconds())
	}
}
