//go:build js && wasm

package vue

import (
	"fmt"
	"syscall/js"
)

// Mount attaches a component's rendered DOM to a browser element.
func Mount(selector string, component *Comp) error {
	document := js.Global().Get("document")
	if document.IsUndefined() || document.IsNull() {
		return fmt.Errorf("mount %q: document is unavailable", selector)
	}

	doc := wrapDocument(document)
	element := doc.QuerySelector(selector)
	if element == nil {
		return fmt.Errorf("mount %q: target not found", selector)
	}

	return component.mount(jsMountTarget{
		doc:      doc,
		rootNode: element,
	})
}

type jsMountTarget struct {
	doc      *domDocument
	rootNode *domNode
}

func (target jsMountTarget) document() domDocumentAccess {
	if target.doc == nil {
		return nil
	}
	return target.doc
}

func (target jsMountTarget) root() domNodeAccess {
	if target.rootNode == nil {
		return nil
	}
	return target.rootNode
}
