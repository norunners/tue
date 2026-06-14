package vue

import "testing"

func TestRenderHTMLEscapesTextAndAttributes(t *testing.T) {
	node := Element("p", []Attribute{
		Attr("title", `A & "B"`),
		BoolAttr("hidden"),
	}, Text("<hello> & goodbye"))

	got := RenderHTML(node)
	want := `<p title="A &amp; &#34;B&#34;" hidden>&lt;hello&gt; &amp; goodbye</p>`
	if got != want {
		t.Fatalf("RenderHTML() = %q, want %q", got, want)
	}
}

func TestRenderHTMLFragmentsAndVoidElements(t *testing.T) {
	node := Fragment(
		Text("before"),
		Element("input", []Attribute{Attr("value", "Ada")}, Text("ignored")),
		Text("after"),
	)

	got := RenderHTML(node)
	want := `before<input value="Ada">after`
	if got != want {
		t.Fatalf("RenderHTML() = %q, want %q", got, want)
	}
}
