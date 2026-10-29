package tui

import (
	"context"

	tea "charm.land/bubbletea/v2"

	"github.com/vitaliipsl/crappy/internal/assistant"
	"github.com/vitaliipsl/crappy/internal/client/tui/navigation"
	"github.com/vitaliipsl/crappy/internal/client/tui/routes"
	sessionscreen "github.com/vitaliipsl/crappy/internal/client/tui/screens/session"
	sessionsscreen "github.com/vitaliipsl/crappy/internal/client/tui/screens/sessions"
	"github.com/vitaliipsl/crappy/internal/config"
)

func Run(ctx context.Context, app *assistant.Assistant, config config.Config) error {
	sessions, err := app.Sessions(ctx)
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
					app,
					config,
					route.Param(routes.ParamSessionID),
				)
			},
			routes.SessionsName: func(route navigation.Route) tea.Model {
				return sessionsscreen.New(
					ctx,
					app,
					route.Param(routes.ParamSessionID),
				)
			},
		},
	)

	_, err = tea.NewProgram(root, tea.WithContext(ctx)).Run()

	return err
}
