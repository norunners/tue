package gogen

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/norunners/tue/internal/compiler/sfc"
)

func TestStyleFromBlock(t *testing.T) {
	block := &sfc.Block{
		Attrs:       []sfc.Attr{{Name: "scoped"}},
		Content:     ".page { color: red; }",
		ContentSpan: sfc.Span{Start: sfc.Position{Line: 2, Column: 1}},
	}
	expected := &Style{Source: block.Content, Scoped: true, Span: block.ContentSpan}
	tests := []struct {
		name     string
		block    *sfc.Block
		expected *Style
		ok       bool
	}{
		{name: "style block", block: block, expected: expected, ok: true},
		{name: "missing style block", block: nil, expected: nil, ok: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			actual, ok := StyleFromBlock(test.block)
			if diff := cmp.Diff(test.ok, ok); diff != "" {
				t.Errorf("mismatch style ok (-expected, +actual):\n%s", diff)
			}
			if diff := cmp.Diff(test.expected, actual); diff != "" {
				t.Errorf("mismatch style (-expected, +actual):\n%s", diff)
			}
		})
	}
}

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
