package tue

// VNodeType identifies the kind of virtual node.
type VNodeType string

const (
	VNodeTypeElement  VNodeType = "element"
	VNodeTypeText     VNodeType = "text"
	VNodeTypeFragment VNodeType = "fragment"
)
