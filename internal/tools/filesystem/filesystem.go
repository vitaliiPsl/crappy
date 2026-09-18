package filesystem

import "github.com/vitaliiPsl/crappy-adk/kit"

func New() []kit.Tool {
	return []kit.Tool{
		NewReadFile(),
		NewWriteFile(),
		NewEditFile(),
	}
}
