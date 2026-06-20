package tue

// Comp declares a component contract for compiler discovery.
// T must be an anonymous struct whose fields use prop, event, or state tags.
// The marker has no runtime state and does not require initialization.
type Comp[T any] struct{}
