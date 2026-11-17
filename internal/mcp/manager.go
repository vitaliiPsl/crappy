package mcp

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/vitaliiPsl/crappy-adk/kit"
)

var ErrNotConnected = errors.New("not connected")

type Server struct {
	Name         string
	Tools        []kit.Tool
	Instructions string
	Error        error
}

type connection struct {
	session *mcpsdk.ClientSession
	config  ServerConfig
	server  Server
}

type Manager struct {
	connectTimeout time.Duration
	connections    []*connection
	mu             sync.RWMutex
}

func NewManager(config Config) *Manager {
	manager := &Manager{connectTimeout: config.ConnectTimeout}
	for _, server := range config.Servers {
		manager.connections = append(manager.connections, &connection{
			config: server,
			server: Server{
				Name:  server.Name,
				Error: ErrNotConnected,
			},
		})
	}

	return manager
}

func (m *Manager) Servers() []Server {
	m.mu.RLock()
	defer m.mu.RUnlock()

	servers := make([]Server, len(m.connections))
	for index, current := range m.connections {
		servers[index] = current.server
	}

	return servers
}

func (m *Manager) Refresh(ctx context.Context) error {
	errs := make([]error, len(m.connections))

	var group sync.WaitGroup
	for index, current := range m.connections {
		group.Go(
			func() {
				errs[index] = m.refresh(ctx, current)
			},
		)
	}

	group.Wait()

	return errors.Join(errs...)
}

func (m *Manager) Shutdown(ctx context.Context) error {
	m.mu.Lock()

	var sessions []*mcpsdk.ClientSession
	for _, current := range m.connections {
		if current.session != nil {
			sessions = append(sessions, current.session)
		}

		current.session = nil
		current.server = Server{Name: current.config.Name, Error: ErrNotConnected}
	}
	m.mu.Unlock()

	closed := make(chan error, len(sessions))
	for _, session := range sessions {
		go func() { closed <- session.Close() }()
	}

	var err error
	for range sessions {
		select {
		case closeErr := <-closed:
			err = errors.Join(err, closeErr)
		case <-ctx.Done():
			return errors.Join(err, ctx.Err())
		}
	}

	return err
}

func (m *Manager) refresh(ctx context.Context, current *connection) error {
	ctx, cancel := context.WithTimeout(ctx, m.connectTimeout)
	defer cancel()

	m.mu.RLock()

	session, previous := current.session, current.server.Error

	m.mu.RUnlock()

	if session != nil {
		if session.Ping(ctx, nil) == nil {
			return nil
		}

		_ = session.Close()
	}

	session, tools, err := connect(ctx, current.config)

	server := Server{
		Name:  current.config.Name,
		Tools: tools,
		Error: err,
	}
	if session != nil {
		server.Instructions = session.InitializeResult().Instructions
	}

	m.mu.Lock()
	current.session, current.server = session, server
	m.mu.Unlock()

	// A server that stays down is reported once, not on every refresh.
	if err == nil || (previous != nil && previous.Error() == err.Error()) {
		return nil
	}

	return fmt.Errorf("server %s: %w", current.config.Name, err)
}

func connect(ctx context.Context, config ServerConfig) (*mcpsdk.ClientSession, []kit.Tool, error) {
	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "crappy", Version: "0.1.0"}, nil)

	session, err := client.Connect(ctx, NewTransport(config), nil)
	if err != nil {
		return nil, nil, err
	}

	tools, err := discoverTools(ctx, config, session)
	if err != nil {
		return nil, nil, errors.Join(err, session.Close())
	}

	return session, tools, nil
}
