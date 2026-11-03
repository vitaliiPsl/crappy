package assistant

import (
	"errors"

	"github.com/vitaliipsl/crappy/harness"
	"github.com/vitaliipsl/crappy/harness/agent"
	"github.com/vitaliipsl/crappy/harness/entry"
	"github.com/vitaliipsl/crappy/harness/session/state"
	"github.com/vitaliipsl/crappy/harness/token"
	"github.com/vitaliipsl/crappy/internal/config"
)

var ErrSessionRequired = errors.New("an existing session is required")

type Assistant struct {
	config    config.Config
	harness   *harness.Harness
	sessions  state.Store
	entries   entry.Store
	agents    *agent.Factory
	estimator token.Estimator
}

func New(config config.Config, harness *harness.Harness, store harness.Store, agents *agent.Factory, estimator token.Estimator) *Assistant {
	return &Assistant{
		config:    config,
		harness:   harness,
		sessions:  store.SessionStore(),
		entries:   store.EntryStore(),
		agents:    agents,
		estimator: estimator,
	}
}
