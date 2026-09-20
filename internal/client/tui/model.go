package tui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/vitaliipsl/crappy/internal/client/tui/navigation"
	"github.com/vitaliipsl/crappy/internal/client/tui/routes"
)

type model struct {
	navigator *navigation.Model
}

func newModel(
	initial navigation.Route,
	factories map[navigation.RouteName]navigation.Factory,
) model {
	return model{
		navigator: navigation.New(initial, factories),
	}
}

func (m model) Init() tea.Cmd {
	return m.navigator.Init()
}

func (m model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := message.(tea.KeyPressMsg); ok {
		switch key.String() {
		case "ctrl+c":
			m.navigator.Close()

			return m, tea.Quit
		case "ctrl+p":
			current := m.navigator.Current()

			return m.updateNavigator(navigation.Toggle(routes.Sessions(
				current.Param(routes.ParamSessionID),
			)))
		}
	}

	return m.updateNavigator(message)
}

func (m model) View() tea.View {
	view := m.navigator.View()
	view.AltScreen = true
	view.MouseMode = tea.MouseModeCellMotion

	return view
}

func (m model) updateNavigator(message tea.Msg) (tea.Model, tea.Cmd) {
	_, command := m.navigator.Update(message)

	return m, command
}
