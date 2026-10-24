package core

import (
	"fmt"
	"net/url"
	"strings"
)

// HandlerFunc defines the standard handler signature for Gota.
type HandlerFunc func(ctx *Context) error

// Node represents a path segment in Gota's Trie routing tree.
type Node struct {
	Segment     string
	Children    []*Node
	Handler     HandlerFunc
	IsParam     bool
	IsWild      bool
	ParamKey    string
	RawHandler  HandlerFunc
	Middlewares []MiddlewareFunc
	Method      string
	FullPath    string
	Name        string
	Doc         *RouteDoc
}

// Router maps HTTP methods to Trie routing trees and stores named routes.
type Router struct {
	trees map[string]*Node
	names map[string]*Node
}

// NewRouter initializes a new Router instance.
func NewRouter() *Router {
	return &Router{
		trees: make(map[string]*Node),
		names: make(map[string]*Node),
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
func (r *Router) Add(method, path string, handler HandlerFunc) *Node {
	root, ok := r.trees[method]
	if !ok {
		root = &Node{Segment: "/"}
		r.trees[method] = root
	}

	segments := splitPath(path)
	current := root

	for _, seg := range segments {
		var child *Node
		for _, c := range current.Children {
			if c.Segment == seg {
				child = c
				break
			}
		}
		if child == nil {
			child = &Node{Segment: seg}
			if strings.HasPrefix(seg, ":") {
				child.IsParam = true
				child.ParamKey = seg[1:]
			} else if seg == "*" || strings.HasPrefix(seg, "*") {
				child.IsWild = true
				if len(seg) > 1 {
					child.ParamKey = seg[1:]
				} else {
					child.ParamKey = "*"
				}
			}
			current.Children = append(current.Children, child)
		}
		current = child
	}

	current.Handler = handler
	current.RawHandler = handler
	current.Method = method
	current.FullPath = path
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

	var search func(n *Node, segIdx int) (HandlerFunc, bool)
	search = func(n *Node, segIdx int) (HandlerFunc, bool) {
		if segIdx == len(segments) {
			if n.Handler != nil {
				return n.Handler, true
			}
			return nil, false
		}

		seg := segments[segIdx]

		// 1. Try exact match first
		for _, child := range n.Children {
			if !child.IsParam && !child.IsWild && child.Segment == seg {
				if h, found := search(child, segIdx+1); found {
					return h, true
				}
			}
		}

		// 2. Try named parameter match (:id)
		for _, child := range n.Children {
			if child.IsParam {
				params[child.ParamKey] = seg
				if h, found := search(child, segIdx+1); found {
					return h, true
				}
				delete(params, child.ParamKey)
			}
		}

		// 3. Try wildcard match (*)
		for _, child := range n.Children {
			if child.IsWild {
				wildVal := strings.Join(segments[segIdx:], "/")
				params[child.ParamKey] = wildVal
				if child.Handler != nil {
					return child.Handler, true
				}
				delete(params, child.ParamKey)
			}
		}

		return nil, false
	}

	// Root path "/"
	if len(segments) == 0 {
		if root.Handler != nil {
			return root.Handler, params, true
		}
		return nil, nil, false
	}

	handler, found := search(root, 0)
	return handler, params, found
}

// SetName stores a named route pointer for reverse URL lookups.
func (r *Router) SetName(name string, n *Node) {
	r.names[name] = n
}

// URLFor reconstructs a URL for a named route with parameter replacement.
func (r *Router) URLFor(name string, params map[string]string) (string, error) {
	target, ok := r.names[name]
	if !ok {
		return "", fmt.Errorf("gota: route with name %q not found", name)
	}

	used := make(map[string]bool)
	segments := splitPath(target.FullPath)
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
func (r *Router) Routes() []*Node {
	var all []*Node
	var collect func(n *Node)
	collect = func(n *Node) {
		if n.Handler != nil {
			all = append(all, n)
		}
		for _, c := range n.Children {
			collect(c)
		}
	}
	for _, root := range r.trees {
		collect(root)
	}
	return all
}
