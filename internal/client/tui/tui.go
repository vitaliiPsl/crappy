package tui

import (
	"context"

	tea "charm.land/bubbletea/v2"
	"github.com/google/uuid"

	"github.com/vitaliipsl/crappy/internal/client/tui/navigation"
	"github.com/vitaliipsl/crappy/internal/client/tui/routes"
	sessionscreen "github.com/vitaliipsl/crappy/internal/client/tui/screens/session"
	sessionsscreen "github.com/vitaliipsl/crappy/internal/client/tui/screens/sessions"
	"github.com/vitaliipsl/crappy/internal/runtime"
)

func Run(ctx context.Context, appRuntime *runtime.Runtime) error {
	sessions, err := appRuntime.Sessions(ctx)
	if err != nil {
		return err
	}

	sessionID := uuid.NewString()
	if len(sessions) > 0 {
		sessionID = sessions[0].ID
	}

	root := newModel(
		routes.Session(sessionID),
		map[navigation.RouteName]navigation.Factory{
			routes.SessionName: func(route navigation.Route) tea.Model {
				return sessionscreen.New(
					ctx,
					appRuntime,
					route.Param(routes.ParamSessionID),
				)
			},
			routes.SessionsName: func(route navigation.Route) tea.Model {
				return sessionsscreen.New(
					ctx,
					appRuntime,
					route.Param(routes.ParamSessionID),
				)
			},
		},
	)

	_, err = tea.NewProgram(root, tea.WithContext(ctx)).Run()

	return err
}
