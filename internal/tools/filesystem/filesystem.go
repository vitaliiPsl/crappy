package filesystem

import "github.com/vitaliiPsl/crappy-adk/kit"

func New(workdir string) []kit.Tool {
	return []kit.Tool{
		NewReadFile(workdir),
		NewWriteFile(workdir),
		NewEditFile(workdir),
	}
}
