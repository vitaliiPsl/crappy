package service

import (
	"github.com/vitaliipsl/crappy/harness"
	"github.com/vitaliipsl/crappy/harness/agent"
	"github.com/vitaliipsl/crappy/harness/entry"
	"github.com/vitaliipsl/crappy/harness/session/state"
	"github.com/vitaliipsl/crappy/harness/token"
	"github.com/vitaliipsl/crappy/internal/config"
	"github.com/vitaliipsl/crappy/internal/schedule"
)

type Local struct {
	config    config.Config
	harness   *harness.Harness
	sessions  state.Store
	entries   entry.Store
	agents    *agent.Factory
	estimator token.Estimator
	schedules *schedule.Manager
}

func NewLocal(config config.Config, harness *harness.Harness, store harness.Store, agents *agent.Factory, estimator token.Estimator, schedules *schedule.Manager) *Local {
	return &Local{
		config:    config,
		harness:   harness,
		sessions:  store.SessionStore(),
		entries:   store.EntryStore(),
		agents:    agents,
		estimator: estimator,
		schedules: schedules,
	}
}
