package gee

import (
	"fmt"
	"reflect"
	"testing"
)

func TestParsePattern(t *testing.T) {
	type args struct {
		pattern string
	}
	tests := []struct {
		name string
		args args
		want []string
	}{
		{name: "Empty String", args: args{pattern: ""}, want: []string{}},
		{name: "Empty Pattern", args: args{pattern: "/"}, want: []string{}},
		{name: "Empty Pattern", args: args{pattern: "//"}, want: []string{}},
		{name: "Dynamic route parameter", args: args{pattern: "p/:name"}, want: []string{"p", ":name"}},
		{name: "Wildcard ", args: args{pattern: "p/*"}, want: []string{"p", "*"}},
		{name: "Multiple wildcards", args: args{pattern: "p/*name/*"}, want: []string{"p", "*name"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parsePattern(tt.args.pattern); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("parsePattern(%q) = %v, want: %v", tt.args.pattern, got, tt.want)
			}
		})
	}
}

func newTestRouter() *router {
	r := newRouter()
	r.addRoute("GET", "/", nil)
	r.addRoute("GET", "/hello/:name", nil)
	r.addRoute("GET", "/hello/:name/getAge", nil)
	r.addRoute("GET", "/hello/b/c", nil)
	r.addRoute("GET", "/hi/:name", nil)
	r.addRoute("GET", "/assets/*filepath", nil)
	return r
}

func TestGetRoute(t *testing.T) {
	r := newTestRouter()
	n, params := r.getRoute("GET", "/hello/alice")

	switch {
	case n == nil:
		t.Fatal("TrieNode cannot be nil")
	case n.pattern != "/hello/:name":
		t.Fatal("Route should match /hello/:name")
	case params["name"] != "alice":
		t.Fatal("Route parameter name should be alice")
	default:
		fmt.Printf("mathched path: %q, params['name']: %q\n", n.pattern, params["name"])
	}
}
