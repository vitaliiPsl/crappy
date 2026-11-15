package schedule

import "context"

type Store interface {
	Load(context.Context) ([]Record, error)
	Save(context.Context, []Record) error
}
