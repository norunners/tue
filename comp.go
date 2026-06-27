package tue

// Comp declares generated component fields for compiler discovery.
// T must be an anonymous struct whose fields use prop, event, state, or
// computed tags.
// The marker has no runtime state and does not require initialization.
type Comp[T any] struct{}
