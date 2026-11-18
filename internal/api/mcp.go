package api

import (
	"context"
	"net/http"

	"github.com/vitaliipsl/crappy/internal/service"
)

func (c *Client) ListMCPServers(ctx context.Context) ([]service.MCPServer, error) {
	var servers []service.MCPServer

	err := c.call(ctx, "GET", "/mcp/servers", nil, &servers)

	return servers, err
}

func (h *Handler) listMCPServers(w http.ResponseWriter, r *http.Request) {
	servers, err := h.service.ListMCPServers(r.Context())
	writeResponse(w, servers, err)
}
