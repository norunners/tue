package tue

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestRenderHTMLEscapesTextAndAttributes(t *testing.T) {
	node := Element("main", []Attribute{
		Attr("title", `A "quoted" & <tag>`),
		BoolAttr("hidden"),
	}, []VNode{
		Text(`Hello <Tue> & "friends"`),
	})

	if diff := cmp.Diff(`<main title="A &#34;quoted&#34; &amp; &lt;tag&gt;" hidden>Hello &lt;Tue&gt; &amp; &#34;friends&#34;</main>`, RenderHTML(node)); diff != "" {
		t.Errorf("mismatch rendered HTML (-expected, +actual):\n%s", diff)
	}
}

func TestCompOfCallsOptionalInit(t *testing.T) {
	component := &initFixture{}

	comp := CompOf(component, func(fixture *initFixture) VNode {
		return Text(fixture.value)
	})

	if diff := cmp.Diff("initialized", component.value); diff != "" {
		t.Errorf("mismatch component value (-expected, +actual):\n%s", diff)
	}
	if diff := cmp.Diff("initialized", comp.Render().Text); diff != "" {
		t.Errorf("mismatch rendered text (-expected, +actual):\n%s", diff)
	}
}

type initFixture struct {
	value string
}

func (f *initFixture) Init(Context) {
	f.value = "initialized"
}
