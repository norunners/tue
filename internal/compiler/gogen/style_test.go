package gogen

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestRewriteScopedCSSRewritesSelectorsAndNestedAtRules(t *testing.T) {
	project, err := parseProjectFixture("testdata/scoped_css_rewrite/App.tue")
	if err != nil {
		t.Fatalf("parse project fixture: %v", err)
	}

	actual, ok := generatedStyleSource(project.Files)
	if !ok {
		t.Fatal("generatedStyleSource returned no stylesheet")
	}
	expected, err := testFixtureString("testdata/golden/ScopedCSSRewrite_style.css")
	if err != nil {
		t.Fatalf("read expected scoped CSS rewrite fixture: %v", err)
	}
	if diff := cmp.Diff(expected, string(actual)); diff != "" {
		t.Errorf("mismatch scoped CSS rewrite (-expected, +actual):\n%s", diff)
	}
}

func TestRewriteScopedCSSHandlesParserSensitiveCSS(t *testing.T) {
	project, err := parseProjectFixture("testdata/scoped_css_parser_edges/App.tue")
	if err != nil {
		t.Fatalf("parse project fixture: %v", err)
	}

	actual, ok := generatedStyleSource(project.Files)
	if !ok {
		t.Fatal("generatedStyleSource returned no stylesheet")
	}
	expected, err := testFixtureString("testdata/golden/ScopedCSSParserEdges_style.css")
	if err != nil {
		t.Fatalf("read expected parser-sensitive scoped CSS fixture: %v", err)
	}
	if diff := cmp.Diff(expected, string(actual)); diff != "" {
		t.Errorf("mismatch parser-sensitive scoped CSS rewrite (-expected, +actual):\n%s", diff)
	}
}

func TestRewriteScopedCSSPreservesMalformedCSS(t *testing.T) {
	project, err := parseProjectFixture("testdata/malformed_scoped_css/App.tue")
	if err != nil {
		t.Fatalf("parse project fixture: %v", err)
	}

	actual, ok := generatedStyleSource(project.Files)
	if !ok {
		t.Fatal("generatedStyleSource returned no stylesheet")
	}
	expected, err := testFixtureString("testdata/golden/MalformedScopedCSS_style.css")
	if err != nil {
		t.Fatalf("read expected malformed scoped CSS fixture: %v", err)
	}
	if diff := cmp.Diff(expected, string(actual)); diff != "" {
		t.Errorf("mismatch malformed scoped CSS fallback (-expected, +actual):\n%s", diff)
	}
}

func TestScopeAttrForIsStableForPath(t *testing.T) {
	project, err := parseProjectFixture("testdata/scoped_styles/App.tue")
	if err != nil {
		t.Fatalf("parse project fixture: %v", err)
	}

	expected := "data-tue-c-d8d60a14"
	if diff := cmp.Diff(expected, scopeAttrFor(filePath(project.Files[0]))); diff != "" {
		t.Errorf("mismatch scope attribute (-expected, +actual):\n%s", diff)
	}
}
