---
title: 'Gee 笔记总结'
date: 2023-10-12
category:
 - golang
---

## 1. 核心思想

Gee 的基本原理是实现`http.Handler`接口：

```go
package http

type Handler interface {
    ServeHTTP(w ResponseWriter, r *Request)
}

func ListenAndServe(address string, h Handler) error
```

在函数`ListenAndServe(address string, h Handler) error`中，若`h`不为`nil`，则会将所有的 HTTP 请求交由`handler`的实例进行处理。

## 2. 设计

### 2.1 上下文 Context

Context 的生命周期贯穿整个 HTTP Request 的处理流程，用于：

1. 存储处理请求所需要的数据，如：`http.Request`
2. 存储处理过程中产生的数据，如：解析动态路由后得到的路由参数
3. 封装重复代码，简化代码并降低出错率，如：封装返回类型为 JSON 的数据的代码，使得用户只需调用一个函数即可，无需编写详细的响应报文。
4. 作为中间件和处理函数的参数，使得整个处理流程中共享同一个 Context

#### 数据结构

```go
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
}
```

#### 生命周期

![Context的生命周期](https://raw.githubusercontent.com/dreamjz/pics/main/pics/2023/202310131323215.png)

### 2.2 动态路由

哈希表只能支持静态路由，动态路由需要用到前缀树(Trie)（[LCR 062. 实现 Trie (前缀树)](https://leetcode.cn/problems/QC3q1f/)）。

#### 数据结构

##### 前缀树 Trie

```go
type trieNode struct {
	pattern  string      // 待匹配的路由
	part     string      // 当前节点的内容
	children []*trieNode // 子节点
	isWild   bool        // 是否进行精准匹配，默认 false
}
```

只有`pattern`不为空时，表示当前路径为已注册的路由。

例如：`/hello/:name/age`,`/hello/:name`, `/asset/*filepath`

![image-20231013133618919](https://raw.githubusercontent.com/dreamjz/pics/main/pics/2023/202310131336746.png)

##### 路由表

```go
type router struct {
	trieRoots map[string]*trieNode
	handlers  map[string]HandlerFunc
}
```

- `trieRoots`：每种 HTTP Method 对应一个路由前缀树
- `handlers`：注册的路由对应的处理函数

### 2.3 路由分组

#### 数据结构

```go
type RouterGroup struct {
    prefix      string        // 路由组前缀
    middlewares []HandlerFunc // 中间件
    parent      *RouterGroup  // 父母分组
    engine      *Engine       // 所有分组持有同一个 Engine 实例
}
```

Engine 可以被视作顶层的路由分组：

```go
type Engine struct {
	*RouterGroup
	router *router
	groups []*RouterGroup // store all groups
	// HTML rendering
	htmlTmpls *template.Template
	funcMap   template.FuncMap
}
```

- `RouterGroup`：拥有`RoterGroup`的所有功能
- `groups`：存储所有的分组

路由分组实际上构成了树形结构，子节点存在指向父母节点的指针：

![image-20231013134920473](https://raw.githubusercontent.com/dreamjz/pics/main/pics/2023/202310131349330.png)

### 2.4 中间件

中间件(middlewares)，简单说，就是非业务的技术类组件。Web 框架本身不可能去理解所有的业务，因而不可能实现所有的功能。因此，框架需要有一个插口，允许用户自己定义功能，嵌入到框架中，仿佛这个功能是框架原生支持的一样。

中间件和处理函数是一样的数据结构：

```go
type HandlerFunc func(*Context)
```

中间件应用于路由分组：

1. 每个分组有若干各中间件
2. 子分组共享父母分组的中间件

#### 中间件-处理函数调用链

在收到 HTTP 请求时：

1. 解析请求路径
2. 将路径对应的路由分组中的 中间件 加入到 `Context.Handlers`中
3. 获取路径对应的路由处理函数，加入到 `Context.Handlers`
4. 调用`Context.Next`开始执行

```go
func (c *Context) Next() {
	c.index++
	n := len(c.handlers)
	for c.index < n {
		c.handlers[c.index](c)
		c.index++
	}
}
```

`c.Next()`用于执行下一个函数，例如：

```go
func A(c *Context) {
    part1
    c.Next()
    part2
}
func B(c *Context) {
    part3
    c.Next()
    part4
}

// 执行流程
part1 -> part3 -> Handler -> part 4 -> part2
```

### 2.5 错误恢复

将错误恢复功能作为中间件：

```go
func Recovery() HandlerFunc {
	return func(c *Context) {
		defer func() {
			if err := recover(); err != nil {
				message := fmt.Sprintf("%s", err)
				log.Printf("%s\n\n", trace(message))
				c.Fail(http.StatusInternalServerError, "Internal Server Error")
			}
		}()

		c.Next()
	}
}
```

捕获 panic，调用`c.Fail`跳过剩下的函数，并返回错误

```go
func (c *Context) Fail(code int, errMsg string) {
	c.index = len(c.handlers) // 跳过剩下的函数
	c.JSON(code, H{"message": errMsg})
}
```

## 3. 处理流程

注册路由为`GET /hello/:name`,以处理 `GET /hello/Alice`为例：

1. 创建 Context 实例
2. 将请求路径所在的路由组中的 中间件， 加入`Context.Handlers`
3. 解析路由，获取路由参数`name: Alice`，存入 `Context.Params`
4. 获取对应的处理函数，加入`Context.Handlers`
5. 调用`c.Next()`开始处理

## Reference

1. [七天用Go从零实现系列](https://geektutu.com/post/gee.html)