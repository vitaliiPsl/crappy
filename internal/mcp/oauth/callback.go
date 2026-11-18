package oauth

import (
	"cmp"
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/modelcontextprotocol/go-sdk/auth"
)

type Callback struct {
	open     func(url string)
	listener net.Listener
	server   *http.Server
	queries  chan url.Values
}

func NewCallback(open func(url string)) (*Callback, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("listen for authorization callback: %w", err)
	}

	callback := &Callback{
		open:     open,
		listener: listener,
		queries:  make(chan url.Values, 1),
	}

	callback.server = &http.Server{
		Handler:           http.HandlerFunc(callback.receive),
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() { _ = callback.server.Serve(listener) }()

	return callback, nil
}

func (c *Callback) URL() string {
	return "http://" + c.listener.Addr().String() + "/callback"
}

func (c *Callback) Fetch(ctx context.Context, args *auth.AuthorizationArgs) (*auth.AuthorizationResult, error) {
	c.open(args.URL)

	select {
	case query := <-c.queries:
		if failure := query.Get("error"); failure != "" {
			return nil, fmt.Errorf("authorization failed: %s", cmp.Or(query.Get("error_description"), failure))
		}

		return &auth.AuthorizationResult{
			Code:  query.Get("code"),
			State: query.Get("state"),
			Iss:   query.Get("iss"),
		}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (c *Callback) Close() error {
	return c.server.Close()
}

func (c *Callback) receive(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/callback" {
		http.NotFound(w, r)

		return
	}

	select {
	case c.queries <- r.URL.Query():
	default:
	}

	_, _ = fmt.Fprintln(w, "You can close this window and return to the terminal.")
}
