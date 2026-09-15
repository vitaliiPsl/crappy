package checkpoint

import "github.com/vitaliiPsl/crappy-adk/kit"

const EntryType = "checkpoint"

type Checkpoint struct {
	Summary string    `json:"summary"`
	Usage   kit.Usage `json:"usage"`
}
