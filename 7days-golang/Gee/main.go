package main

import (
	"gee"
	"net/http"
)

func main() {
	r := gee.Default()

	r.GET("/", func(c *gee.Context) {
		c.String(http.StatusOK, "Welcome")
	})

	r.GET("/panic", func(c *gee.Context) {
		names := []string{}
		c.String(http.StatusOK, names[1])
	})

	r.Run(":8000")
}
