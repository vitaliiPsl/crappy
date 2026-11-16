package service

import (
	"context"
	"time"

	"github.com/vitaliipsl/crappy/harness/event"
	"github.com/vitaliipsl/crappy/harness/execution"
	"github.com/vitaliipsl/crappy/harness/session/state"
	"github.com/vitaliipsl/crappy/internal/schedule"

	"github.com/vitaliiPsl/crappy-adk/kit"
)

type Service interface {
	CreateSession(context.Context, string) (state.Record, error)
	GetSession(context.Context, string) (state.Record, error)
	ListSessions(context.Context) ([]state.Record, error)
	DeleteSession(context.Context, string) error

	Execute(context.Context, string, ...kit.Content) (string, error)
	Steer(context.Context, string, ...kit.Content) error
	Cancel(context.Context, string, string) error
	Resume(context.Context, string) (string, error)
	Compact(context.Context, string) (string, error)
	GetExecutionStatus(context.Context, string, string) (execution.Status, error)
	WaitExecution(context.Context, string, string) (execution.Status, error)

	CreateSchedule(context.Context, string, string, time.Time) (schedule.Record, error)
	GetSchedule(context.Context, string, string) (schedule.Record, error)
	ListSchedules(context.Context, string) ([]schedule.Record, error)
	CancelSchedule(context.Context, string, string) (schedule.Record, error)

	GetModel(context.Context) (ModelSelection, error)
	Context(context.Context, string) (ContextReport, error)
	Usage(context.Context, string) (kit.Usage, error)

	Subscribe(context.Context, string, uint64) (event.Subscription, error)
}

var _ Service = (*Local)(nil)
