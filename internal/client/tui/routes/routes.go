package routes

import "github.com/vitaliipsl/crappy/internal/client/tui/navigation"

const (
	SessionName    navigation.RouteName = "session"
	SessionsName   navigation.RouteName = "sessions"
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
