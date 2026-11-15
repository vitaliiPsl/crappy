package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"

	tea "charm.land/bubbletea/v2"

	"github.com/vitaliipsl/crappy/harness/session/state"
	"github.com/vitaliipsl/crappy/internal/client/tui/navigation"
	"github.com/vitaliipsl/crappy/internal/client/tui/routes"
	sessionscreen "github.com/vitaliipsl/crappy/internal/client/tui/screens/session"
	sessionsscreen "github.com/vitaliipsl/crappy/internal/client/tui/screens/sessions"
	"github.com/vitaliipsl/crappy/internal/service"
	"github.com/vitaliipsl/crappy/internal/workdir"
)

func Run(ctx context.Context, app service.Service, directory string) error {
	sessions, err := app.ListSessions(ctx)
	if err != nil {
		return err
	}

	sessionID, err := initialSession(sessions, directory)
	if err != nil {
		return err
	}

	root := newModel(
		routes.Session(sessionID),
		map[navigation.RouteName]navigation.Factory{
			routes.SessionName: func(route navigation.Route) tea.Model {
				return sessionscreen.New(
					ctx,
					app,
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

func initialSession(sessions []state.Record, directory string) (string, error) {
	for _, record := range sessions {
		value, found := record.Values[workdir.StateKey]
		if !found {
			continue
		}

		var saved string
		if err := json.Unmarshal(value, &saved); err != nil {
			return "", fmt.Errorf("read session %s working directory: %w", record.ID, err)
		}

		if saved == filepath.Clean(directory) {
			return record.ID, nil
		}
	}

	return "", nil
}
