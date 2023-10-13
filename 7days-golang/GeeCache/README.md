---
title: 'GeeCache 笔记总结'
date: 2023-10-13
category:
 - golang
---

## 1. LRU 缓存策略



![implement lru algorithm with golang](https://raw.githubusercontent.com/dreamjz/pics/main/pics/2023/202310131426770.jpeg)

LRU（[146. LRU 缓存](https://leetcode.cn/problems/lru-cache/)）由两部分组成：

1. 双向链表：key 对应的 value 组成双向链表
2. 哈希表：key 指向双向链表中的节点

算法：

1. 访问节点后，将节点移动到队尾
2. 新增的节点放置在队尾

队首的节点就是 最近最少使用 的节点，当触发淘汰条件时将被删除。

### 1.1 数据结构

```go
// Cache is LRU cache. It is not safe for concurrent cases.
type Cache struct {
	maxBytes int64
	nBytes   int64
	dl       *list.List // doubly linked list
	cache    map[string]*list.Element
	// optional and executed when an entry is purged
	OnEvicted func(key string, value Value)
}

type entry struct {
	key string
	val Value
}

type Value interface {
	Len() int
}
```

`Cache`：

- `maxBytes`：缓存的内存大小上限
- `nBytes`：当前已使用的内存大小
- `dl`：双向链表，
- `cache`：哈希表
- `OnEvicted`：记录被移除时的回调函数

`entry`: 链表元素

- `key`：缓存的 key
- `val`：value，接口类型，方法`Len()`用于返回占用的内存大小

## 2. 设计

### 2.1 ByteView

```go
type ByteView struct {
	b []byte
}
```

`ByteView.b`存储实际的缓存值，`[]byte`可以用于表示任意的数据类型。

缓存值对于用户来说是只读的，当获取缓存时会拷贝一份数据，防止实际缓存被修改。

```go
// ByteSlice returns a copy of the data as a byte slice.
func (v ByteView) ByteSlice() []byte {
	return cloneBytes(v.b)
}
func cloneBytes(b []byte) []byte {
	c := make([]byte, len(b))
	copy(c, b)
	return c
}
```

### 2.2 并发缓存

单纯的 LRU 缓存不是并发安全的，可以在LRU缓存的基础上进行封装，通过互斥锁来保证并发安全：

```go
type cache struct {
	mu         sync.Mutex
	lru        *lru.Cache
	cacheBytes int64
}

func (c *cache) add(key string, val ByteView) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.lru == nil {
		c.lru = lru.New(c.cacheBytes, nil)
	}
	c.lru.Add(key, val)
}

func (c *cache) get(key string) (ByteView, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.lru == nil {
		return ByteView{}, false
	}

	if v, ok := c.lru.Get(key); ok {
		return v.(ByteView), ok
	}

	return ByteView{}, false
}
```

### 2.3 Group

Group 用于管理一组缓存，是最核心的数据结构。

```go
type Group struct {
	name      string
	getter    Getter
	mainCache cache
	peers     PeerPicker
	// use singleflight.Group to make sure that
	// each key is only fetched once
	loader *singleflight.Group
}
```

- `name`：Group 名
- `getter`：回调，当缓存不存在时调用`getter.Get`访问数据源
- `mainCache`：缓存
- `peers`：用于访问其他缓存服务器/节点
- `loader`：抑制重复调用，多次调用只会执行一次，用于避免缓存击穿

#### 获取缓存

```go
func (g *Group) Get(key string) (ByteView, error) {
	if key == "" {
		return ByteView{}, fmt.Errorf("key is required")
	}

	if v, ok := g.mainCache.get(key); ok {
		log.Println("[GeeCache] hit")
		return v, nil
	}

	return g.load(key)
}

func (g *Group) load(key string) (ByteView, error) {
	view, err := g.loader.Do(key, func() (any, error) {
		if g.peers != nil {
			if peer, ok := g.peers.PickPeer(key); ok {
				val, err := g.getFromPeer(peer, key)
				if err == nil {
					return val, nil
				}
				log.Println("[GeeCache] Failed to get from peer", err)
			}
		}

		return g.getLocally(key)
	})

	if err != nil {
		return ByteView{}, err
	}

	return view.(ByteView), nil
}
```

获取缓存流程：

1. 尝试从本地缓存中获取
2. 若本地缓存不存在，则通过节点选择策略，访问其他的缓存服务节点获取
3. 若节点选择的是自身 或 访问其他节点失败，则访问数据源，返回数据并加入本地缓存

![image-20231013172431733](https://raw.githubusercontent.com/dreamjz/pics/main/pics/2023/202310131724612.png)

### 2.4 接口型函数

```go
// Getter loads data for a key
type Getter interface {
	Get(key string) ([]byte, error)
}

var _ Getter = GetterFunc(nil)

// GetterFunc implements Getter with a function
type GetterFunc func(key string) ([]byte, error)

// Get implements Getter interface
func (f GetterFunc) Get(key string) ([]byte, error) {
	return f(key)
}
```

函数`GetterFunc`实现了接口`Getter`，这样做的好处是，`Getter`类型的参数：

1. 可以传入 `GetterFunc`，适用于简单场景
2. 可以传入实现了 `Getter`的结构体，适用于复杂场景

### 2.4 一致性哈希算法

使用一致性哈希算法选择缓存服务节点，可以避免在节点发生变化时出现缓存雪崩。

> 缓存雪崩：缓存在同一时刻全部失效，造成瞬时DB请求量大、压力骤增，引起雪崩。常因为缓存服务器宕机，或缓存设置了相同的过期时间引起。

一致性哈希算法将 key 映射到 $2^{32}$ 的空间中，将这个数字首尾相连，形成一个环。

- 计算节点/机器(通常使用节点的名称、编号和 IP 地址)的哈希值，放置在环上。
- 计算 key 的哈希值，放置在环上，顺时针寻找到的第一个节点，就是应选取的节点/机器。

![一致性哈希添加节点 consistent hashing add peer](https://raw.githubusercontent.com/dreamjz/pics/main/pics/2023/202310131733559.jpeg)

单节点数量发生变化时，只会有一小部分数据受到影响。

#### 虚拟节点

当节点数量比较少时，可能产生**数据倾斜**问题，即大量的数据被分配到某些节点上。

此时可以引入虚拟节点，一个真实节点可以对应多个虚拟节点，以增加节点数量避免数据倾斜。

### 2.5 分布式节点

#### HTTP server/client

```go
type HTTPPool struct {
	self        string     // 自己的地址，主机+端口
	basePath    string     // 节点间通讯地址的前缀
	mu          sync.Mutex // guards peers and httpGetters
	peers       *consistenthash.Map
	httpGetters map[string]*httpGetter
}
```

- `self`：当前节点的地址
- `basePath`：节点间通讯地址前缀；如：“/geecache/”
- `peers`：使用一致性哈希算法，根据 key 选择节点
- `httpGetters`：每个节点对应的客户端

##### server

实现 `http.Handler`接口：

```go
func (p *HTTPPool) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	path := req.URL.Path
	if !strings.HasPrefix(path, p.basePath) {
		panic("HTTPPool serving unexpected path: " + path)
	}
	p.Log("%s %s", req.Method, path)

	// /<basepath>/<groupname>/<key> required
	parts := strings.SplitN(path[len(p.basePath):], "/", 2)
	if len(parts) != 2 {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	groupName := parts[0]
	key := parts[1]

	group := GetGroup(groupName)
	if group == nil {
		http.Error(w, "no such group: "+groupName, http.StatusNotFound)
		return
	}

	view, err := group.Get(key)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	body, err := proto.Marshal(&pb.Response{Value: view.ByteSlice()})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Write(body)
}
```

1. 检查请求路径
2. 解析出 Group 名和 key
3. 获取 value 并返回

##### client

```go
type httpGetter struct {
	baseURL string
}

func (h *httpGetter) Get(in *pb.Request, out *pb.Response) error {
	u := fmt.Sprintf(
		"%v%v/%v",
		h.baseURL,
		url.QueryEscape(in.GetGroup()),
		url.QueryEscape(in.GetKey()),
	)

	res, err := http.Get(u)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("server returned: %v", res.Status)
	}

	bytes, err := io.ReadAll(res.Body)
	if err != nil {
		return fmt.Errorf("reading response body: %v", err)
	}

	if err = proto.Unmarshal(bytes, out); err != nil {
		return fmt.Errorf("decoding respose body: %v", err)
	}

	return nil
}
```

通过 HTTP 访问其他节点

#### PeerPicker / PeerGetter

```go
// PeerPicker is the interface that must be implemented to locate
// the peer that owns a specific key
type PeerPicker interface {
	PickPeer(key string) (PeerGetter, bool)
}

// PeerGetter is the interface that must be implemented by a peer
type PeerGetter interface {
	Get(in *pb.Request, out *pb.Response) error
}
```

- `HTTPPool`实现了 `PeerPicker`接口，通过 key 选取节点
- `httpGetter`实现了 `PeerGetter`接口，向节点发送 HTTP 请求，获取 value

### 2.7 Single Flight

> **缓存雪崩**：缓存在同一时刻全部失效，造成瞬时DB请求量大、压力骤增，引起雪崩。缓存雪崩通常因为缓存服务器宕机、缓存的 key 设置了相同的过期时间等引起。

> **缓存击穿**：一个存在的key，在缓存过期的一刻，同时有大量的请求，这些请求都会击穿到 DB ，造成瞬时DB请求量大、压力骤增。

> **缓存穿透**：查询一个不存在的数据，因为不存在则不会写到缓存中，所以每次都会去请求 DB，如果瞬间流量过大，穿透到 DB，导致宕机。

 当同时向节点发送大量的请求时，可能引发缓存击穿。

因为多次请求的结果和一次请求是一样的，可以只处理一次即可。

```go
type call struct {
	wg  sync.WaitGroup
	val any
	err error
}

type Group struct {
	mu sync.Mutex
	m  map[string]*call
}

func (g *Group) Do(key string, fn func() (any, error)) (any, error) {
	g.mu.Lock()
	if g.m == nil {
		g.m = make(map[string]*call) // 延迟加载
	}
	// 调用已存在
	if c, ok := g.m[key]; ok {
		g.mu.Unlock()
		// 等待调用结束
		c.wg.Wait()
		// 直接返回
		return c.val, c.err
	}
	// 调用不存在，创建新调用
	c := new(call)
	c.wg.Add(1)
	g.m[key] = c
	g.mu.Unlock()

	// 发起调用
	c.val, c.err = fn()
	c.wg.Done()

	// 调用结束，删除调用
	g.mu.Lock()
	delete(g.m, key)
	g.mu.Unlock()

	return c.val, c.err
}
```

1. 一个 key 对应一次请求
2. 若当前 key 的请求已经存在，表示正在处理中，则等待处理并返回结果
3. 若不存在，则创建新请求开始处理，并发计数器加一，这样同时只会有一个请求会被处理
4. 处理完毕后，移除调用，返回结果

## Reference

1. [七天用Go从零实现系列](https://geektutu.com/post/gee.html)