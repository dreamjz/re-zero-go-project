package lru

import (
	"container/list"
)

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

func New(maxBytes int64, onEvicted func(string, Value)) *Cache {
	return &Cache{
		maxBytes:  maxBytes,
		dl:        list.New(),
		cache:     make(map[string]*list.Element),
		OnEvicted: onEvicted,
	}
}

// Len return the number of cache entries
func (c *Cache) Len() int {
	return c.dl.Len()
}

// Get find key's value
func (c *Cache) Get(key string) (val Value, ok bool) {
	if ele, ok := c.cache[key]; ok {
		c.dl.MoveToFront(ele)
		kv := ele.Value.(*entry)
		return kv.val, true
	}

	return nil, false
}

// RemoveOldest remove the oldest item
func (c *Cache) RemoveOldest() {
	ele := c.dl.Back()
	if ele != nil {
		c.dl.Remove(ele)
		kv := ele.Value.(*entry)
		delete(c.cache, kv.key)
		c.nBytes -= int64(len(kv.key)) + int64(kv.val.Len())
		if c.OnEvicted != nil {
			c.OnEvicted(kv.key, kv.val)
		}
	}
}

// Add adds a value to the cache.
func (c *Cache) Add(key string, val Value) {
	if ele, ok := c.cache[key]; ok {
		c.dl.MoveToFront(ele)
		kv := ele.Value.(*entry)
		c.nBytes += int64(val.Len()) - int64(kv.val.Len())
		kv.val = val
	} else {
		ele := c.dl.PushFront(&entry{key, val})
		c.cache[key] = ele
		c.nBytes += int64(len(key)) + int64(val.Len())
	}

	for c.maxBytes != 0 && c.maxBytes < c.nBytes {
		c.RemoveOldest()
	}
}
