package daemon

import (
	"context"
	"errors"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/vitaliipsl/crappy/internal/api"
	"github.com/vitaliipsl/crappy/internal/service"
)

const shutdownTimeout = 10 * time.Second

type Daemon struct {
	api    *api.Handler
	server *http.Server

	shutdown func(context.Context) error
	stop     chan struct{}
	stopOnce sync.Once
}

func New(service service.Service, shutdown func(context.Context) error) *Daemon {
	daemon := &Daemon{
		shutdown: shutdown,
		stop:     make(chan struct{}),
	}

	daemon.api = api.NewHandler(service, daemon.requestStop)
	daemon.server = &http.Server{
		Handler:           daemon.api,
		ReadHeaderTimeout: 5 * time.Second,
	}

	return daemon
}

func (d *Daemon) Run(ctx context.Context, listener net.Listener) error {
	var serveErr error

	stopped := make(chan struct{})
	go func() {
		defer close(stopped)

		serveErr = d.server.Serve(listener)
	}()

	select {
	case <-ctx.Done():
	case <-d.stop:
	case <-stopped:
	}

	shutdownErr := d.close()

	<-stopped

	if errors.Is(serveErr, http.ErrServerClosed) {
		serveErr = nil
	}

	return errors.Join(serveErr, shutdownErr)
}

func (d *Daemon) requestStop() {
	d.stopOnce.Do(func() { close(d.stop) })
}

func (d *Daemon) close() error {
	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	d.api.Close()
	runtimeErr := d.shutdown(ctx)
	serverErr := d.server.Shutdown(ctx)
	closeErr := d.server.Close()

	return errors.Join(runtimeErr, serverErr, closeErr)
}
