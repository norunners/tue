package tue

import (
	"net/url"
	pathpkg "path"
	"strings"
)

// Route is one explicit router table entry.
type Route struct {
	Path   string
	Render func(RouteMatch) VNode
}

// RouteMatch is the current matched route state. The first router matches path
// segments only; query strings and fragments are not preserved in RouteMatch.
type RouteMatch struct {
	Path    string
	Pattern string
	Params  map[string]string
	Found   bool
}

// Param returns a matched path parameter by name.
func (m RouteMatch) Param(name string) string {
	if m.Params == nil {
		return ""
	}
	return m.Params[name]
}

// Router keeps reactive route state for a small explicit route table.
type Router struct {
	routes   []compiledRoute
	notFound func(RouteMatch) VNode
	current  *RefValue[RouteMatch]
	location routeLocation
}

type compiledRoute struct {
	route    Route
	pattern  string
	segments []routeSegment
}

type routeSegment struct {
	static string
	param  string
}

type routeLocation interface {
	Path() string
	SetPath(string)
	Href(string) string
	Watch(func(string)) func()
}

// RouterOf returns a router backed by the current browser hash path under
// js/wasm and an in-memory path elsewhere. Prefer creating routers from a
// component Init method with the provided Context so browser hash listeners are
// released when the component unmounts.
func RouterOf(ctx Context, routes []Route, notFound func(RouteMatch) VNode) *Router {
	router := &Router{
		routes:   compileRoutes(routes),
		notFound: notFound,
		location: newRouteLocation(),
	}
	initialPath := "/"
	if router.location != nil {
		initialPath = router.location.Path()
	}
	router.current = RefOf(router.match(initialPath))
	if router.location != nil {
		stop := router.location.Watch(router.setPath)
		if ctx != nil {
			ctx.OnCleanup(stop)
		}
	}
	return router
}

// Current returns the current route match and tracks it as a reactive read.
func (r *Router) Current() RouteMatch {
	if r == nil || r.current == nil {
		return RouteMatch{Path: "/"}
	}
	return cloneRouteMatch(r.current.Get())
}

// Navigate updates the current route path.
func (r *Router) Navigate(path string) {
	if r == nil {
		return
	}
	path = normalizeRoutePath(path)
	if r.location != nil {
		r.location.SetPath(path)
	}
	r.setPath(path)
}

// Href returns the link target for a route path.
func (r *Router) Href(path string) string {
	path = normalizeRoutePath(path)
	if r == nil || r.location == nil {
		return "#" + path
	}
	return r.location.Href(path)
}

// Link returns an anchor VNode for a route path.
func (r *Router) Link(path string, attrs []Attribute, children []VNode) VNode {
	return Element("a", attrsWithAttr(attrs, Attr("href", r.Href(path))), children)
}

// View renders the current route handler or the not-found handler.
func (r *Router) View() VNode {
	if r == nil {
		return Fragment(nil)
	}
	match := r.Current()
	if match.Found {
		route := r.routeForPattern(match.Pattern)
		if route != nil && route.Render != nil {
			return route.Render(match)
		}
		return Fragment(nil)
	}
	if r.notFound != nil {
		return r.notFound(match)
	}
	return Fragment(nil)
}

func (r *Router) setPath(path string) {
	if r == nil || r.current == nil {
		return
	}
	next := r.match(path)
	current := r.current.Get()
	if sameRouteMatch(current, next) {
		return
	}
	r.current.Set(next)
}

func (r *Router) match(path string) RouteMatch {
	path = normalizeRoutePath(path)
	for _, route := range r.routes {
		if params, ok := matchRouteSegments(route.segments, routeSegments(path)); ok {
			return RouteMatch{
				Path:    path,
				Pattern: route.pattern,
				Params:  params,
				Found:   true,
			}
		}
	}
	return RouteMatch{Path: path}
}

func (r *Router) routeForPattern(pattern string) *Route {
	for i := range r.routes {
		if r.routes[i].pattern == pattern {
			return &r.routes[i].route
		}
	}
	return nil
}

func compileRoutes(routes []Route) []compiledRoute {
	compiled := make([]compiledRoute, 0, len(routes))
	for _, route := range routes {
		pattern := normalizeRoutePath(route.Path)
		compiled = append(compiled, compiledRoute{
			route:    route,
			pattern:  pattern,
			segments: routePatternSegments(pattern),
		})
	}
	return compiled
}

func routePatternSegments(pattern string) []routeSegment {
	parts := routeSegments(pattern)
	segments := make([]routeSegment, len(parts))
	for i, part := range parts {
		if strings.HasPrefix(part, ":") && len(part) > 1 {
			segments[i] = routeSegment{param: part[1:]}
			continue
		}
		segments[i] = routeSegment{static: part}
	}
	return segments
}

func matchRouteSegments(pattern []routeSegment, path []string) (map[string]string, bool) {
	if len(pattern) != len(path) {
		return nil, false
	}
	var params map[string]string
	for i, segment := range pattern {
		value := path[i]
		if segment.param != "" {
			if value == "" {
				return nil, false
			}
			if params == nil {
				params = make(map[string]string)
			}
			params[segment.param] = decodeRouteSegment(value)
			continue
		}
		if segment.static != value {
			return nil, false
		}
	}
	return params, true
}

func normalizeRoutePath(path string) string {
	path = strings.TrimSpace(path)
	path = strings.TrimPrefix(path, "#")
	if path == "" {
		return "/"
	}
	if parsed, err := url.Parse(path); err == nil && parsed.Path != "" && parsed.Scheme != "" {
		path = parsed.Path
	} else {
		if index := strings.IndexAny(path, "?#"); index >= 0 {
			path = path[:index]
		}
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	cleaned := pathpkg.Clean(path)
	if cleaned == "." || cleaned == "" {
		return "/"
	}
	return cleaned
}

func routeSegments(path string) []string {
	path = strings.Trim(normalizeRoutePath(path), "/")
	if path == "" {
		return nil
	}
	return strings.Split(path, "/")
}

func decodeRouteSegment(segment string) string {
	decoded, err := url.PathUnescape(segment)
	if err != nil {
		return segment
	}
	return decoded
}

func cloneRouteMatch(match RouteMatch) RouteMatch {
	match.Params = cloneRouteParams(match.Params)
	return match
}

func cloneRouteParams(params map[string]string) map[string]string {
	if len(params) == 0 {
		return nil
	}
	cloned := make(map[string]string, len(params))
	for name, value := range params {
		cloned[name] = value
	}
	return cloned
}

func sameRouteMatch(left RouteMatch, right RouteMatch) bool {
	if left.Path != right.Path || left.Pattern != right.Pattern || left.Found != right.Found {
		return false
	}
	if len(left.Params) != len(right.Params) {
		return false
	}
	for name, value := range left.Params {
		if right.Params[name] != value {
			return false
		}
	}
	return true
}

func attrsWithAttr(attrs []Attribute, attr Attribute) []Attribute {
	next := append([]Attribute(nil), attrs...)
	for i, existing := range next {
		if existing.Name == attr.Name {
			next[i] = attr
			return next
		}
	}
	return append(next, attr)
}
