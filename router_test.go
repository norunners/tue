package tue

import (
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestRouterMatchesStaticAndParameterizedRoutes(t *testing.T) {
	router := RouterOf(nil, []Route{
		{Path: "/"},
		{Path: "/users/:id"},
		{Path: "/settings"},
	}, nil)

	expected := RouteMatch{Path: "/", Pattern: "/", Found: true}
	if diff := cmp.Diff(expected, router.Current()); diff != "" {
		t.Errorf("mismatch initial route (-expected, +actual):\n%s", diff)
	}

	router.Navigate("/users/ada%20lovelace")

	expected = RouteMatch{
		Path:    "/users/ada%20lovelace",
		Pattern: "/users/:id",
		Params:  map[string]string{"id": "ada lovelace"},
		Found:   true,
	}
	if diff := cmp.Diff(expected, router.Current()); diff != "" {
		t.Errorf("mismatch parameterized route (-expected, +actual):\n%s", diff)
	}
	if diff := cmp.Diff("ada lovelace", router.Current().Param("id")); diff != "" {
		t.Errorf("mismatch route param (-expected, +actual):\n%s", diff)
	}
}

func TestRouterReportsNotFoundRoute(t *testing.T) {
	router := RouterOf(nil, []Route{{Path: "/"}}, func(match RouteMatch) VNode {
		return Text("missing:" + match.Path)
	})

	router.Navigate("/missing")

	expected := RouteMatch{Path: "/missing"}
	if diff := cmp.Diff(expected, router.Current()); diff != "" {
		t.Errorf("mismatch missing route (-expected, +actual):\n%s", diff)
	}
	if diff := cmp.Diff("missing:/missing", RenderHTML(router.View())); diff != "" {
		t.Errorf("mismatch not-found view (-expected, +actual):\n%s", diff)
	}
}

func TestRouterMatchesPathsWithoutQueryOrFragment(t *testing.T) {
	router := RouterOf(nil, []Route{{Path: "/users/:id"}}, nil)

	router.Navigate("/users/42?tab=profile#bio")

	expected := RouteMatch{
		Path:    "/users/42",
		Pattern: "/users/:id",
		Params:  map[string]string{"id": "42"},
		Found:   true,
	}
	if diff := cmp.Diff(expected, router.Current()); diff != "" {
		t.Errorf("mismatch normalized route without query or fragment (-expected, +actual):\n%s", diff)
	}
}

func TestRouterViewRendersCurrentRoute(t *testing.T) {
	router := RouterOf(nil, []Route{
		{
			Path: "/",
			Render: func(RouteMatch) VNode {
				return Text("home")
			},
		},
		{
			Path: "/users/:id",
			Render: func(match RouteMatch) VNode {
				return Text("user:" + match.Param("id"))
			},
		},
	}, nil)

	if diff := cmp.Diff("home", RenderHTML(router.View())); diff != "" {
		t.Errorf("mismatch initial route view (-expected, +actual):\n%s", diff)
	}

	router.Navigate("/users/42")

	if diff := cmp.Diff("user:42", RenderHTML(router.View())); diff != "" {
		t.Errorf("mismatch parameterized route view (-expected, +actual):\n%s", diff)
	}
}

func TestRouterCurrentIsReactive(t *testing.T) {
	router := RouterOf(nil, []Route{
		{Path: "/"},
		{Path: "/users/:id"},
	}, nil)
	renderCount := 0
	target := newStubDOMTarget()

	mounted, err := mountComponent(CompOf(&routerFixture{router: router}, func(fixture *routerFixture) VNode {
		renderCount++
		match := fixture.router.Current()
		return Text(fmt.Sprintf("%s:%s", match.Pattern, match.Param("id")))
	}), target)
	if err != nil {
		t.Fatalf("mountComponent returned error: %v", err)
	}

	if diff := cmp.Diff("/:", target.html()); diff != "" {
		t.Errorf("mismatch initial router render (-expected, +actual):\n%s", diff)
	}
	if diff := cmp.Diff(1, renderCount); diff != "" {
		t.Errorf("mismatch initial router render count (-expected, +actual):\n%s", diff)
	}

	router.Navigate("/users/42")

	if diff := cmp.Diff("/users/:id:42", target.html()); diff != "" {
		t.Errorf("mismatch navigated router render (-expected, +actual):\n%s", diff)
	}
	if diff := cmp.Diff(2, renderCount); diff != "" {
		t.Errorf("mismatch navigated router render count (-expected, +actual):\n%s", diff)
	}

	if err := mounted.Unmount(); err != nil {
		t.Fatalf("Unmount returned error: %v", err)
	}
}

func TestRouterLinkReturnsHashAnchor(t *testing.T) {
	router := RouterOf(nil, nil, nil)
	actual := RenderHTML(router.Link("/settings", []Attribute{Attr("class", "nav-link")}, []VNode{Text("Settings")}))

	expected := `<a class="nav-link" href="#/settings">Settings</a>`
	if diff := cmp.Diff(expected, actual); diff != "" {
		t.Errorf("mismatch router link (-expected, +actual):\n%s", diff)
	}
}

func TestRouterCurrentReturnsCopyOfParams(t *testing.T) {
	router := RouterOf(nil, []Route{{Path: "/users/:id"}}, nil)
	router.Navigate("/users/42")

	match := router.Current()
	match.Params["id"] = "mutated"

	if diff := cmp.Diff("42", router.Current().Param("id")); diff != "" {
		t.Errorf("mismatch copied route params (-expected, +actual):\n%s", diff)
	}
}

type routerFixture struct {
	router *Router
}
