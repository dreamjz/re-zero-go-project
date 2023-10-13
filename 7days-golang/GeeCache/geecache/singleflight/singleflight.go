package singleflight

import "sync"

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
