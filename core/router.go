package core

import (
	"fmt"
	"net/url"
	"sort"
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

	// auto marks handlers registered implicitly (e.g. automatic OPTIONS).
	auto bool
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
	parts := strings.Split(path, "/")
	segments := make([]string, 0, len(parts))
	for _, p := range parts {
		if p != "" {
			segments = append(segments, p)
		}
	}
	return segments
}

func wildcardKey(seg string) string {
	if len(seg) > 1 {
		return seg[1:]
	}
	return "*"
}

// Add registers a route into the Trie for the given HTTP method.
// It panics if the same method and path were already registered explicitly,
// or if a parameter/wildcard name conflicts with an existing sibling.
func (r *Router) Add(method, path string, handler HandlerFunc) *Node {
	current := r.insert(method, path)
	if current.Handler != nil && !current.auto {
		panic(fmt.Sprintf("gota: route %s %s is already registered", method, path))
	}
	if current.auto {
		// An explicit route replaces an automatic one.
		current.auto = false
		current.Doc = nil
	}

	current.Handler = handler
	current.RawHandler = handler
	current.Method = method
	current.FullPath = path
	return current
}

// addAuto registers an implicit route unless one already exists.
// Implicit routes are hidden from the generated documentation.
func (r *Router) addAuto(method, path string, handler HandlerFunc) {
	current := r.insert(method, path)
	if current.Handler != nil {
		return
	}
	current.auto = true
	current.Handler = handler
	current.RawHandler = handler
	current.Method = method
	current.FullPath = path
	current.Doc = &RouteDoc{Hidden: true, Responses: make(map[int]*ResponseDoc)}
}

// Allowed returns the sorted HTTP methods that have a handler matching path.
func (r *Router) Allowed(path string) []string {
	var methods []string
	for method := range r.trees {
		if _, _, ok := r.Match(method, path); ok {
			methods = append(methods, method)
		}
	}
	sort.Strings(methods)
	return methods
}

// insert finds or creates the Trie node for method and path.
func (r *Router) insert(method, path string) *Node {
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
			if (c.IsParam && strings.HasPrefix(seg, ":")) || (c.IsWild && strings.HasPrefix(seg, "*")) {
				panic(fmt.Sprintf("gota: route %s %s: %q conflicts with existing %q", method, path, seg, c.Segment))
			}
		}
		if child == nil {
			child = &Node{Segment: seg}
			if strings.HasPrefix(seg, ":") {
				child.IsParam = true
				child.ParamKey = seg[1:]
			} else if strings.HasPrefix(seg, "*") {
				child.IsWild = true
				child.ParamKey = wildcardKey(seg)
			}
			current.Children = append(current.Children, child)
		}
		current = child
	}

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
		} else if strings.HasPrefix(seg, "*") {
			key := wildcardKey(seg)
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
