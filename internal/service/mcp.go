package service

import (
	"context"
	"errors"

	"github.com/vitaliipsl/crappy/internal/mcp"
)

type MCPServerStatus string

const (
	MCPServerConnecting            MCPServerStatus = "connecting"
	MCPServerConnected             MCPServerStatus = "connected"
	MCPServerAuthorizationRequired MCPServerStatus = "authorization_required"
	MCPServerUnavailable           MCPServerStatus = "unavailable"
)

type MCPServer struct {
	Name   string          `json:"name"`
	Status MCPServerStatus `json:"status"`
	Tools  int             `json:"tools"`
	Error  string          `json:"error,omitempty"`
}

func (s *Local) ListMCPServers(context.Context) ([]MCPServer, error) {
	servers := s.mcpManager.Servers()

	result := make([]MCPServer, 0, len(servers))
	for _, server := range servers {
		status := MCPServer{Name: server.Name}

		switch {
		case server.Error == nil:
			status.Status, status.Tools = MCPServerConnected, len(server.Tools)
		case errors.Is(server.Error, mcp.ErrNotConnected):
			status.Status = MCPServerConnecting
		case errors.Is(server.Error, mcp.ErrAuthorizationRequired):
			status.Status = MCPServerAuthorizationRequired
		default:
			status.Status, status.Error = MCPServerUnavailable, server.Error.Error()
		}

		result = append(result, status)
	}

	return result, nil
}
