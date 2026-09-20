package navigation

import tea "charm.land/bubbletea/v2"

type Factory func(Route) tea.Model

type BroadcastMessage interface {
	Broadcast()
}

type entry struct {
	route Route
	page  tea.Model
}

type Model struct {
	factories map[RouteName]Factory
	stack     []entry

	width  int
	height int
}

func New(initial Route, factories map[RouteName]Factory) *Model {
	model := &Model{factories: factories}

	if first, ok := model.build(initial); ok {
		model.stack = append(model.stack, first)
	}

	return model
}

func (m *Model) Init() tea.Cmd {
	active, ok := m.active()
	if !ok {
		return nil
	}

	return active.page.Init()
}

func (m *Model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch message := message.(type) {
	case Action:
		return m, m.apply(message)
	case tea.WindowSizeMsg:
		m.width = message.Width
		m.height = message.Height

		return m, m.updateAll(message)
	}

	if _, ok := message.(BroadcastMessage); ok {
		return m, m.updateAll(message)
	}

	return m, m.updateActive(message)
}

func (m *Model) View() tea.View {
	active, ok := m.active()
	if !ok {
		return tea.NewView("")
	}

	return active.page.View()
}

func (m *Model) Close() {
	for index := len(m.stack) - 1; index >= 0; index-- {
		closePage(m.stack[index].page)
	}

	m.stack = nil
}

func (m *Model) Current() Route {
	active, ok := m.active()
	if !ok {
		return Route{}
	}

	return active.route
}

func (m *Model) apply(action Action) tea.Cmd {
	switch action.operation {
	case OperationPush:
		return m.push(action.route)
	case OperationReset:
		return m.reset(action.route)
	case OperationBack:
		return m.back()
	case OperationToggle:
		return m.toggle(action.route)
	default:
		return nil
	}
}

func (m *Model) push(route Route) tea.Cmd {
	next, ok := m.build(route)
	if !ok {
		return nil
	}

	m.stack = append(m.stack, next)

	return m.initializeActive()
}

func (m *Model) reset(route Route) tea.Cmd {
	next, ok := m.build(route)
	if !ok {
		return nil
	}

	for index := len(m.stack) - 1; index >= 0; index-- {
		closePage(m.stack[index].page)
	}

	m.stack = []entry{next}

	return m.initializeActive()
}

func (m *Model) back() tea.Cmd {
	if len(m.stack) <= 1 {
		return nil
	}

	last := len(m.stack) - 1
	closePage(m.stack[last].page)
	m.stack = m.stack[:last]

	return nil
}

func (m *Model) toggle(route Route) tea.Cmd {
	active, ok := m.active()
	if ok && active.route.Name() == route.Name() {
		return m.back()
	}

	return m.push(route)
}

func (m *Model) initializeActive() tea.Cmd {
	active, ok := m.active()
	if !ok {
		return nil
	}

	var resize tea.Cmd
	if m.width > 0 || m.height > 0 {
		active.page, resize = active.page.Update(tea.WindowSizeMsg{
			Width:  m.width,
			Height: m.height,
		})
	}

	return tea.Batch(resize, active.page.Init())
}

func (m *Model) updateActive(message tea.Msg) tea.Cmd {
	active, ok := m.active()
	if !ok {
		return nil
	}

	updated, command := active.page.Update(message)
	active.page = updated

	return command
}

func (m *Model) updateAll(message tea.Msg) tea.Cmd {
	commands := make([]tea.Cmd, 0, len(m.stack))
	for index := range m.stack {
		updated, command := m.stack[index].page.Update(message)
		m.stack[index].page = updated

		commands = append(commands, command)
	}

	return tea.Batch(commands...)
}

func (m *Model) build(route Route) (entry, bool) {
	if route.Name() == "" {
		return entry{}, false
	}

	factory, ok := m.factories[route.Name()]
	if !ok {
		return entry{}, false
	}

	page := factory(route)
	if page == nil {
		return entry{}, false
	}

	return entry{route: route, page: page}, true
}

func (m *Model) active() (*entry, bool) {
	if len(m.stack) == 0 {
		return nil, false
	}

	return &m.stack[len(m.stack)-1], true
}

func closePage(page tea.Model) {
	if closer, ok := page.(interface{ Close() }); ok {
		closer.Close()
	}
}
