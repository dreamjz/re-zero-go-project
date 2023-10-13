package gee

import "strings"

type trieNode struct {
	pattern  string      // 待匹配的路由
	part     string      // 当前节点的内容
	children []*trieNode // 子节点
	isWild   bool        // 是否进行精准匹配，默认 false
}

// 寻找第一个匹配成功的节点，用于插入
func (n *trieNode) matchChild(part string) *trieNode {
	for _, child := range n.children {
		if child.part == part || child.isWild {
			return child
		}
	}

	return nil
}

// 寻找所有匹配成功的节点，用于查找
func (n *trieNode) matchChildren(part string) []*trieNode {
	nodes := make([]*trieNode, 0)

	for _, child := range n.children {
		if child.part == part || child.isWild {
			nodes = append(nodes, child)
		}
	}

	return nodes
}

// 向前缀树中插入新节点
func (n *trieNode) insert(pattern string, parts []string, depth int) {
	if len(parts) == depth {
		n.pattern = pattern
		return
	}

	part := parts[depth]
	child := n.matchChild(part)
	if child == nil {
		child = &trieNode{part: part, isWild: part[0] == ':' || part[0] == '*'}
		n.children = append(n.children, child)
	}

	child.insert(pattern, parts, depth+1)
}

// 查找指定路由对应的节点
func (n *trieNode) search(parts []string, depth int) *trieNode {
	if len(parts) == depth || strings.HasPrefix(n.part, "*") {
		if n.pattern == "" {
			return nil
		}
		return n
	}

	part := parts[depth]
	children := n.matchChildren(part)

	for _, child := range children {
		res := child.search(parts, depth+1)
		if res != nil {
			return res
		}
	}

	return nil
}
