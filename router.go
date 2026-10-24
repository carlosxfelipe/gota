package gota

import (
	"fmt"
	"net/url"
	"strings"
)

// HandlerFunc defines the standard handler signature for Gota.
type HandlerFunc func(ctx *Context) error

// node represents a path segment in Gota's Trie routing tree.
type node struct {
	segment     string
	children    []*node
	handler     HandlerFunc
	isParam     bool
	isWild      bool
	paramKey    string
	rawHandler  HandlerFunc
	middlewares []MiddlewareFunc
	method      string
	fullPath    string
	name        string
	doc         *RouteDoc
}

// Router maps HTTP methods to Trie routing trees and stores named routes.
type Router struct {
	trees map[string]*node
	names map[string]*node
}

// newRouter initializes a new Router instance.
func newRouter() *Router {
	return &Router{
		trees: make(map[string]*node),
		names: make(map[string]*node),
	}
}

// splitPath breaks a path string into normalized path segments.
func splitPath(path string) []string {
	var segments []string
	parts := strings.Split(path, "/")
	for _, p := range parts {
		if p != "" {
			segments = append(segments, p)
		}
	}
	return segments
}

// Add registers a route into the Trie for the given HTTP method.
func (r *Router) Add(method, path string, handler HandlerFunc) *node {
	root, ok := r.trees[method]
	if !ok {
		root = &node{segment: "/"}
		r.trees[method] = root
	}

	segments := splitPath(path)
	current := root

	for _, seg := range segments {
		var child *node
		for _, c := range current.children {
			if c.segment == seg {
				child = c
				break
			}
		}
		if child == nil {
			child = &node{segment: seg}
			if strings.HasPrefix(seg, ":") {
				child.isParam = true
				child.paramKey = seg[1:]
			} else if seg == "*" || strings.HasPrefix(seg, "*") {
				child.isWild = true
				if len(seg) > 1 {
					child.paramKey = seg[1:]
				} else {
					child.paramKey = "*"
				}
			}
			current.children = append(current.children, child)
		}
		current = child
	}

	current.handler = handler
	current.rawHandler = handler
	current.method = method
	current.fullPath = path
	return current
}

// Match searches the Trie for a matching handler and extracts path parameters.
func (r *Router) Match(method, path string) (HandlerFunc, map[string]string, bool) {
	root, ok := r.trees[method]
	if !ok {
		return nil, nil, false
	}

	segments := splitPath(path)
	params := make(map[string]string)

	var search func(n *node, segIdx int) (HandlerFunc, bool)
	search = func(n *node, segIdx int) (HandlerFunc, bool) {
		if segIdx == len(segments) {
			if n.handler != nil {
				return n.handler, true
			}
			return nil, false
		}

		seg := segments[segIdx]

		// 1. Try exact match first
		for _, child := range n.children {
			if !child.isParam && !child.isWild && child.segment == seg {
				if h, found := search(child, segIdx+1); found {
					return h, true
				}
			}
		}

		// 2. Try named parameter match (:id)
		for _, child := range n.children {
			if child.isParam {
				params[child.paramKey] = seg
				if h, found := search(child, segIdx+1); found {
					return h, true
				}
				delete(params, child.paramKey)
			}
		}

		// 3. Try wildcard match (*)
		for _, child := range n.children {
			if child.isWild {
				wildVal := strings.Join(segments[segIdx:], "/")
				params[child.paramKey] = wildVal
				if child.handler != nil {
					return child.handler, true
				}
				delete(params, child.paramKey)
			}
		}

		return nil, false
	}

	// Root path "/"
	if len(segments) == 0 {
		if root.handler != nil {
			return root.handler, params, true
		}
		return nil, nil, false
	}

	handler, found := search(root, 0)
	return handler, params, found
}

// setName stores a named route pointer for reverse URL lookups.
func (r *Router) setName(name string, n *node) {
	r.names[name] = n
}

// URLFor reconstructs a URL for a named route with parameter replacement.
func (r *Router) URLFor(name string, params map[string]string) (string, error) {
	target, ok := r.names[name]
	if !ok {
		return "", fmt.Errorf("gota: route with name %q not found", name)
	}

	used := make(map[string]bool)
	segments := splitPath(target.fullPath)
	resolved := make([]string, len(segments))

	for i, seg := range segments {
		if strings.HasPrefix(seg, ":") {
			key := seg[1:]
			val, present := params[key]
			if !present || val == "" {
				return "", fmt.Errorf("gota: missing required param %q for route %q", key, name)
			}
			resolved[i] = val
			used[key] = true
		} else if seg == "*" || strings.HasPrefix(seg, "*") {
			key := "*"
			if len(seg) > 1 {
				key = seg[1:]
			}
			val, present := params[key]
			if !present {
				return "", fmt.Errorf("gota: missing required wildcard param for route %q", name)
			}
			resolved[i] = val
			used[key] = true
		} else {
			resolved[i] = seg
		}
	}

	builtURL := "/" + strings.Join(resolved, "/")

	// Append any leftover parameters as query string
	query := url.Values{}
	for k, v := range params {
		if !used[k] {
			query.Add(k, v)
		}
	}
	if encoded := query.Encode(); encoded != "" {
		builtURL += "?" + encoded
	}

	return builtURL, nil
}

// Routes returns all registered route leaf nodes for OpenAPI generation.
func (r *Router) Routes() []*node {
	var all []*node
	var collect func(n *node)
	collect = func(n *node) {
		if n.handler != nil {
			all = append(all, n)
		}
		for _, c := range n.children {
			collect(c)
		}
	}
	for _, root := range r.trees {
		collect(root)
	}
	return all
}
