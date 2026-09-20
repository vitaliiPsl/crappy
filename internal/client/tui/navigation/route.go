package navigation

import "maps"

type RouteName string

type Route struct {
	name   RouteName
	params map[string]string
}

func NewRoute(name RouteName, params map[string]string) Route {
	return Route{
		name:   name,
		params: cloneParams(params),
	}
}

func (r Route) Name() RouteName {
	return r.name
}

func (r Route) Param(name string) string {
	return r.params[name]
}

func cloneParams(params map[string]string) map[string]string {
	if len(params) == 0 {
		return nil
	}

	cloned := make(map[string]string, len(params))
	maps.Copy(cloned, params)

	return cloned
}
