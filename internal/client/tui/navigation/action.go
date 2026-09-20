package navigation

type Operation uint8

const (
	OperationPush Operation = iota
	OperationReset
	OperationBack
	OperationToggle
)

type Action struct {
	operation Operation
	route     Route
}

func (a Action) Operation() Operation {
	return a.operation
}

func (a Action) Route() Route {
	return a.route
}

func Push(route Route) Action {
	return Action{operation: OperationPush, route: route}
}

func Reset(route Route) Action {
	return Action{operation: OperationReset, route: route}
}

func Back() Action {
	return Action{operation: OperationBack}
}

func Toggle(route Route) Action {
	return Action{operation: OperationToggle, route: route}
}
