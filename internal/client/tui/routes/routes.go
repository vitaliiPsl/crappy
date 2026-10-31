package routes

import "github.com/vitaliipsl/crappy/internal/client/tui/navigation"

const (
	SessionName    navigation.RouteName = "session"
	SessionsName   navigation.RouteName = "sessions"
	ContextName    navigation.RouteName = "context"
	ParamSessionID                      = "session_id"
)

func Session(sessionID string) navigation.Route {
	return navigation.NewRoute(SessionName, map[string]string{
		ParamSessionID: sessionID,
	})
}

func Sessions(sessionID string) navigation.Route {
	return navigation.NewRoute(SessionsName, map[string]string{
		ParamSessionID: sessionID,
	})
}

func Context(sessionID string) navigation.Route {
	return navigation.NewRoute(ContextName, map[string]string{
		ParamSessionID: sessionID,
	})
}
