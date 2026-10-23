package tui

import (
	"context"

	tea "charm.land/bubbletea/v2"

	"github.com/vitaliipsl/crappy/harness"
	"github.com/vitaliipsl/crappy/harness/session"
	"github.com/vitaliipsl/crappy/internal/client/tui/navigation"
	"github.com/vitaliipsl/crappy/internal/client/tui/routes"
	sessionscreen "github.com/vitaliipsl/crappy/internal/client/tui/screens/session"
	sessionsscreen "github.com/vitaliipsl/crappy/internal/client/tui/screens/sessions"
)

func Run(ctx context.Context, appHarness *harness.Harness, store session.Store) error {
	sessions, err := store.List(ctx)
	if err != nil {
		return err
	}

	var sessionID string
	if len(sessions) > 0 {
		sessionID = sessions[0].ID
	}

	root := newModel(
		routes.Session(sessionID),
		map[navigation.RouteName]navigation.Factory{
			routes.SessionName: func(route navigation.Route) tea.Model {
				return sessionscreen.New(
					ctx,
					appHarness,
					route.Param(routes.ParamSessionID),
				)
			},
			routes.SessionsName: func(route navigation.Route) tea.Model {
				return sessionsscreen.New(
					ctx,
					appHarness,
					store,
					route.Param(routes.ParamSessionID),
				)
			},
		},
	)

	_, err = tea.NewProgram(root, tea.WithContext(ctx)).Run()

	return err
}
