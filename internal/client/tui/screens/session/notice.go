package session

import "github.com/vitaliipsl/crappy/harness/execution"

type noticeKind uint8

const (
	noticeNone noticeKind = iota
	noticeError
	noticeInterrupted
)

type notice struct {
	kind    noticeKind
	message string
}

func errorNotice(err error) notice {
	return notice{kind: noticeError, message: err.Error()}
}

func executionNotice(finished execution.Result) notice {
	switch finished.Outcome {
	case execution.Failed:
		return notice{kind: noticeError, message: finished.Error}
	case execution.Cancelled, execution.Interrupted:
		return notice{kind: noticeInterrupted}
	}

	return notice{}
}
