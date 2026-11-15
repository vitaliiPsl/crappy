package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/vitaliipsl/crappy/harness"
	"github.com/vitaliipsl/crappy/harness/event"
	"github.com/vitaliipsl/crappy/harness/execution"
	"github.com/vitaliipsl/crappy/harness/session/state"
	"github.com/vitaliipsl/crappy/internal/service"
	"github.com/vitaliipsl/crappy/internal/utils"

	"github.com/vitaliiPsl/crappy-adk/kit"
)

var _ service.Service = (*Client)(nil)

type Client struct {
	http      *http.Client
	transport *http.Transport
	directory string
}

func NewClient(socketPath, directory string) *Client {
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", socketPath)
		},
		IdleConnTimeout: 90 * time.Second,
	}

	return &Client{
		http:      &http.Client{Transport: transport},
		transport: transport,
		directory: directory,
	}
}

func (c *Client) Close() {
	c.transport.CloseIdleConnections()
}

func (c *Client) Health(ctx context.Context) (Health, error) {
	var value Health

	err := c.call(ctx, "GET", "/health", nil, &value)

	return value, err
}

func (c *Client) Stop(ctx context.Context) error {
	return c.call(ctx, "POST", "/stop", nil, nil)
}

func (c *Client) CreateSession(ctx context.Context, directory string) (state.Record, error) {
	var value state.Record

	if directory == "" {
		directory = c.directory
	}

	directory, err := utils.ResolvePathFromDirectory(c.directory, directory)
	if err != nil {
		return value, err
	}

	err = c.call(ctx, "POST", "/sessions", CreateSessionRequest{Directory: directory}, &value)

	return value, err
}

func (c *Client) GetSession(ctx context.Context, id string) (state.Record, error) {
	var value state.Record

	err := c.session(ctx, id, "GET", "", nil, &value)

	return value, err
}

func (c *Client) ListSessions(ctx context.Context) ([]state.Record, error) {
	var value []state.Record

	err := c.call(ctx, "GET", "/sessions", nil, &value)

	return value, err
}

func (c *Client) DeleteSession(ctx context.Context, id string) error {
	return c.session(ctx, id, "DELETE", "", nil, nil)
}

func (c *Client) Execute(ctx context.Context, id string, content ...kit.Content) (string, error) {
	var value string

	err := c.session(ctx, id, "POST", "/execute", content, &value)

	return value, err
}

func (c *Client) Cancel(ctx context.Context, id, executionID string) error {
	return c.session(ctx, id, "POST", "/executions/"+url.PathEscape(executionID)+"/cancel", nil, nil)
}

func (c *Client) Resume(ctx context.Context, id string) (string, error) {
	var value string

	err := c.session(ctx, id, "POST", "/resume", nil, &value)

	return value, err
}

func (c *Client) Compact(ctx context.Context, id string) (string, error) {
	var value string

	err := c.session(ctx, id, "POST", "/compact", nil, &value)

	return value, err
}

func (c *Client) GetExecutionStatus(ctx context.Context, id, executionID string) (execution.Status, error) {
	var value execution.Status

	err := c.session(ctx, id, "GET", "/executions/"+url.PathEscape(executionID), nil, &value)

	return value, err
}

func (c *Client) WaitExecution(ctx context.Context, id, executionID string) (execution.Status, error) {
	var value execution.Status

	err := c.session(ctx, id, "GET", "/executions/"+url.PathEscape(executionID)+"/wait", nil, &value)

	return value, err
}

func (c *Client) GetModel(ctx context.Context) (service.ModelSelection, error) {
	var value service.ModelSelection

	err := c.call(ctx, "GET", "/model", nil, &value)

	return value, err
}

func (c *Client) Context(ctx context.Context, id string) (service.ContextReport, error) {
	var value service.ContextReport

	err := c.session(ctx, id, "GET", "/context", nil, &value)

	return value, err
}

func (c *Client) Usage(ctx context.Context, id string) (kit.Usage, error) {
	var value kit.Usage

	err := c.session(ctx, id, "GET", "/usage", nil, &value)

	return value, err
}

func (c *Client) Subscribe(ctx context.Context, id string, after uint64) (event.Subscription, error) {
	if id == "" {
		return nil, harness.ErrSessionRequired
	}

	response, err := c.request(ctx, "GET", "/sessions/"+url.PathEscape(id)+"/events?after="+strconv.FormatUint(after, 10), nil)
	if err != nil {
		return nil, err
	}

	return NewSubscription(ctx, response.Body), nil
}

func (c *Client) session(ctx context.Context, id, method, suffix string, input, output any) error {
	if id == "" {
		return harness.ErrSessionRequired
	}

	return c.call(ctx, method, "/sessions/"+url.PathEscape(id)+suffix, input, output)
}

func (c *Client) call(ctx context.Context, method, path string, input, output any) error {
	response, err := c.request(ctx, method, path, input)
	if err != nil {
		return err
	}

	defer func() { _ = response.Body.Close() }()

	if output == nil {
		_, err = io.Copy(io.Discard, response.Body)

		return err
	}

	return json.NewDecoder(response.Body).Decode(output)
}

func (c *Client) request(ctx context.Context, method, path string, input any) (*http.Response, error) {
	var body io.Reader
	if input != nil {
		data, err := json.Marshal(input)
		if err != nil {
			return nil, err
		}

		body = bytes.NewReader(data)
	}

	request, err := http.NewRequestWithContext(ctx, method, "http://crappy"+path, body)
	if err != nil {
		return nil, err
	}

	if input != nil {
		request.Header.Set("Content-Type", "application/json")
	}

	response, err := c.http.Do(request)
	if err != nil {
		return nil, err
	}

	if response.StatusCode >= 200 && response.StatusCode < 300 {
		return response, nil
	}
	defer func() { _ = response.Body.Close() }()

	var remote ResponseError
	if json.NewDecoder(response.Body).Decode(&remote) == nil && remote.Message != "" {
		return nil, &remote
	}

	return nil, fmt.Errorf("assistant returned %s", response.Status)
}
