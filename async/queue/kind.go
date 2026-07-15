package queue

// Kind binds a job type name to its payload type T. Declare one package-level
// Kind per job type and share it between producers and workers: the name
// string exists in exactly one place, and payload type drift between Push and
// Register becomes a compile error.
//
//	var KindSendWelcome = queue.NewKind[SendWelcome]("email.send_welcome")
type Kind[T any] struct {
	name string
}

// NewKind creates a Kind for payload type T. The name must be non-empty and
// unique across the application (convention: "domain.action"). Panics on an
// empty name: kinds are package-level wiring, not runtime data.
func NewKind[T any](name string) Kind[T] {
	if name == "" {
		panic("queue: NewKind requires a non-empty name")
	}
	return Kind[T]{name: name}
}

// Name returns the job type name.
func (k Kind[T]) Name() string { return k.name }
