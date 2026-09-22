package model

import (
	"github.com/vitaliiPsl/crappy-adk/kit"
	"github.com/vitaliiPsl/crappy-adk/x/retry"
)

type Middleware func(kit.Model) kit.Model

func Retry(config retry.Config) Middleware {
	return func(resolved kit.Model) kit.Model {
		return retry.Wrap(resolved, config)
	}
}
